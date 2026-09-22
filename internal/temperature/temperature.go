package temperature

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Recorder interface {
	RecordTemperature(context.Context, float64, time.Time, time.Time, time.Duration) error
	SetSourceStatus(context.Context, string, bool, string, time.Time) error
}

type Reading struct {
	Value        float64       `json:"value"`
	Unit         string        `json:"unit"`
	SourceTime   time.Time     `json:"source_time"`
	ReceivedTime time.Time     `json:"received_time"`
	Latency      time.Duration `json:"-"`
	LatencyMS    int64         `json:"latency_ms"`
	Configured   bool          `json:"configured"`
	Stale        bool          `json:"stale"`
	LastError    string        `json:"last_error,omitempty"`
}

type Poller struct {
	url     string
	http    *http.Client
	record  Recorder
	mu      sync.RWMutex
	reading Reading
}

func New(url string, record Recorder) *Poller {
	return &Poller{url: url, record: record, http: &http.Client{Timeout: 10 * time.Second}, reading: Reading{Unit: "°C", Configured: url != "", Stale: true}}
}

func (p *Poller) Snapshot() Reading { p.mu.RLock(); defer p.mu.RUnlock(); return p.reading }

func (p *Poller) Run(ctx context.Context) {
	if p.url == "" {
		return
	}
	p.poll(ctx)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err == nil {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 ReefTank-Hub/1.0 MicroMessenger/7.0")
		req.Header.Set("Referer", "https://servicewechat.com/")
		var resp *http.Response
		resp, err = p.http.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				err = fmt.Errorf("HTTP %d", resp.StatusCode)
			} else {
				var payload struct {
					Datas []map[string]any `json:"datas"`
				}
				if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr != nil {
					err = decodeErr
				} else if len(payload.Datas) == 0 {
					err = errors.New("temperature response has no data")
				} else {
					v, ok := number(payload.Datas[0]["WaterTemperature"])
					if !ok {
						err = errors.New("WaterTemperature missing")
					} else {
						now := time.Now().UTC()
						latency := time.Since(start)
						r := Reading{Value: v, Unit: "°C", SourceTime: now, ReceivedTime: now, Latency: latency, LatencyMS: latency.Milliseconds(), Configured: true}
						p.mu.Lock()
						p.reading = r
						p.mu.Unlock()
						if p.record != nil {
							_ = p.record.RecordTemperature(ctx, v, now, now, latency)
							_ = p.record.SetSourceStatus(ctx, "xiaoyu_temperature", true, "", now)
						}
						return
					}
				}
			}
		}
	}
	p.mu.Lock()
	p.reading.Configured = true
	p.reading.Stale = true
	p.reading.LastError = err.Error()
	p.mu.Unlock()
	if p.record != nil {
		_ = p.record.SetSourceStatus(context.Background(), "xiaoyu_temperature", false, err.Error(), time.Now())
	}
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, e := strconv.ParseFloat(n, 64)
		return f, e == nil
	}
	return 0, false
}

func (p *Poller) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/temperature", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.Snapshot())
	})
}
