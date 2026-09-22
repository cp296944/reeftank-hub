package sheets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Recorder interface {
	ReplaceWaterQuality(context.Context, time.Time, map[string]float64) error
	WaterHistory(context.Context) (map[string][]map[string]any, error)
	SetSourceStatus(context.Context, string, bool, string, time.Time) error
}
type Snapshot struct {
	Configured  bool               `json:"configured"`
	Stale       bool               `json:"stale"`
	LastSuccess time.Time          `json:"last_success"`
	LastError   string             `json:"last_error,omitempty"`
	Time        string             `json:"time"`
	Values      map[string]float64 `json:"values"`
	WaterChange bool               `json:"water_change"`
	Note        string             `json:"note"`
}
type Syncer struct {
	mu   sync.RWMutex
	base string
	http *http.Client
	db   Recorder
	snap Snapshot
}

func New(raw string, db Recorder) *Syncer {
	return &Syncer{base: raw, http: &http.Client{Timeout: 30 * time.Second}, db: db, snap: Snapshot{Configured: raw != "", Stale: true, Values: map[string]float64{}}}
}
func (s *Syncer) Snapshot() Snapshot { s.mu.RLock(); defer s.mu.RUnlock(); return s.snap }
func (s *Syncer) Run(ctx context.Context) {
	if s.base == "" {
		return
	}
	s.Sync(ctx)
	s.importAll(ctx)
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sync(ctx)
		}
	}
}
func (s *Syncer) get(ctx context.Context, raw string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
	resp, e := s.http.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var v map[string]any
	e = json.NewDecoder(resp.Body).Decode(&v)
	return v, e
}
func (s *Syncer) Sync(ctx context.Context) error {
	v, e := s.get(ctx, s.base)
	if e != nil {
		s.fail(e)
		return e
	}
	vals := numbers(v)
	if len(vals) == 0 {
		e = errors.New("Google response contains no water values")
		s.fail(e)
		return e
	}
	ts, _ := v["time"].(string)
	at := parseTime(ts)
	if e = s.db.ReplaceWaterQuality(ctx, at, vals); e != nil {
		s.fail(e)
		return e
	}
	snap := Snapshot{Configured: true, Values: vals, Time: ts, LastSuccess: time.Now().UTC()}
	snap.WaterChange = truth(v["water_change"])
	snap.Note, _ = v["note"].(string)
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
	_ = s.db.SetSourceStatus(ctx, "google_sheets", true, "", time.Now())
	return nil
}
func (s *Syncer) importAll(ctx context.Context) {
	u, e := url.Parse(s.base)
	if e != nil {
		return
	}
	q := u.Query()
	q.Set("mode", "all")
	u.RawQuery = q.Encode()
	v, e := s.get(ctx, u.String())
	if e != nil {
		return
	}
	rows, _ := v["rows"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		vals := numbers(row)
		if len(vals) > 0 {
			ts, _ := row["time"].(string)
			_ = s.db.ReplaceWaterQuality(ctx, parseTime(ts), vals)
		}
	}
}
func (s *Syncer) Append(ctx context.Context, values map[string]any) error {
	if s.base == "" {
		return errors.New("Google Sheets not configured")
	}
	u, e := url.Parse(s.base)
	if e != nil {
		return e
	}
	key := u.Query().Get("key")
	payload := map[string]any{"action": "append_quality", "key": key, "timestamp": time.Now().Format("2006-01-02 15:04:05")}
	for k, v := range values {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	u.RawQuery = ""
	req, _ := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, e := s.http.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	var result map[string]any
	if e = json.NewDecoder(resp.Body).Decode(&result); e != nil {
		return e
	}
	if result["status"] != "ok" {
		return fmt.Errorf("Google write failed: %v", result["message"])
	}
	return s.Sync(ctx)
}
func numbers(v map[string]any) map[string]float64 {
	out := map[string]float64{}
	for _, k := range []string{"no3", "po4", "ph", "sg", "kh", "ca", "mg"} {
		switch x := v[k].(type) {
		case float64:
			out[k] = x
		case string:
			if n, e := strconv.ParseFloat(strings.TrimSpace(x), 64); e == nil {
				out[k] = n
			}
		}
	}
	return out
}
func truth(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "V" || x == "v" || x == "true"
	}
	return false
}
func parseTime(v string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006/01/02 15:04:05", "2006-01-02"} {
		if t, e := time.ParseInLocation(layout, v, time.Local); e == nil {
			return t
		}
	}
	return time.Now()
}
func (s *Syncer) fail(e error) {
	s.mu.Lock()
	s.snap.Configured = true
	s.snap.Stale = true
	s.snap.LastError = e.Error()
	s.mu.Unlock()
	_ = s.db.SetSourceStatus(context.Background(), "google_sheets", false, e.Error(), time.Now())
}
func (s *Syncer) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/water", func(w http.ResponseWriter, r *http.Request) {
		hist, _ := s.db.WaterHistory(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": s.Snapshot(), "history": hist})
	})
	mux.HandleFunc("POST /api/hub/water/sync", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Sync(r.Context()); e != nil {
			http.Error(w, e.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Snapshot())
	})
	mux.HandleFunc("POST /api/hub/water/records", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&in) != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		for k := range in {
			switch k {
			case "no3", "po4", "ph", "sg", "kh", "ca", "mg", "water_change", "note":
			default:
				http.Error(w, "unknown field", 400)
				return
			}
		}
		if e := s.Append(r.Context(), in); e != nil {
			http.Error(w, e.Error(), 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Snapshot())
	})
}
