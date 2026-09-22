// Package profiles stores UI "profiles" (saved schedules) per-lamp on disk,
// isolated from OTA updates.
//
// Layout:  <dataDir>/profiles/<lampID>/<name>.json
//
// lampID is the lamp's MAC (from the ARP/neighbour table for the configured
// lamp IP), else the lamp's advertised name, else "default". Keying by MAC
// means swapping lamps, or running two, never mixes their saved profiles; and
// because everything lives under <dataDir> (never touched by the updater), a
// version bump can't lose them.
package profiles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Store struct {
	root     string // <dataDir>/profiles
	lampHost string

	mu       sync.Mutex
	cachedID string
	cachedAt time.Time
	// NameHint is set by the daemon after a successful lamp read (the lamp's
	// advertised name), used when ARP can't resolve a MAC.
	NameHint func() string
}

var safeSeg = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func New(dataDir, lampHost string) *Store {
	return &Store{root: filepath.Join(dataDir, "profiles"), lampHost: lampHost}
}

func sanitize(s string) string {
	s = safeSeg.ReplaceAllString(strings.TrimSpace(s), "_")
	s = strings.Trim(s, "._-")
	if s == "" {
		return "default"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// LampID resolves and caches the per-lamp storage key (~5 min TTL).
func (s *Store) LampID(ctx context.Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachedID != "" && time.Since(s.cachedAt) < 5*time.Minute {
		return s.cachedID
	}
	id := s.resolveMAC(ctx)
	if id == "" && s.NameHint != nil {
		id = sanitize(s.NameHint())
	}
	if id == "" || id == "default" {
		// keep any previously resolved good ID rather than downgrading
		if s.cachedID != "" {
			return s.cachedID
		}
		id = "default"
	}
	s.cachedID, s.cachedAt = id, time.Now()
	return id
}

func (s *Store) resolveMAC(ctx context.Context) string {
	if s.lampHost == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "neigh", "show", s.lampHost).CombinedOutput()
	if err != nil {
		return ""
	}
	// "192.168.4.1 dev wlan0 lladdr 44:1d:64:f4:6c:e4 REACHABLE"
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "lladdr" && i+1 < len(fields) {
			return "mac-" + sanitize(fields[i+1])
		}
	}
	return ""
}

func (s *Store) dir(lampID string) string {
	return filepath.Join(s.root, sanitize(lampID))
}

// ServeHTTP handles /api/profiles and /api/profiles/<name> for one lamp.
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request, lampID string) {
	dir := s.dir(lampID)
	switch {
	case r.URL.Path == "/api/profiles" && r.Method == http.MethodGet:
		s.list(w, dir)
	case r.URL.Path == "/api/profiles" && r.Method == http.MethodPost:
		s.save(w, r, dir)
	case strings.HasPrefix(r.URL.Path, "/api/profiles/") && r.Method == http.MethodDelete:
		name := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
		s.del(w, dir, name)
	default:
		http.Error(w, `{"ok":false,"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Store) list(w http.ResponseWriter, dir string) {
	out := map[string]json.RawMessage{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out[strings.TrimSuffix(e.Name(), ".json")] = json.RawMessage(b)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Store) save(w http.ResponseWriter, r *http.Request, dir string) {
	var raw json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, errObj("Bad JSON"))
		return
	}
	var probe struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &probe) != nil || strings.TrimSpace(probe.Name) == "" {
		writeJSON(w, http.StatusBadRequest, errObj("Name required"))
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, errObj(err.Error()))
		return
	}
	pretty, _ := json.MarshalIndent(json.RawMessage(raw), "", "  ")
	tmp := filepath.Join(dir, "."+sanitize(probe.Name)+".tmp")
	final := filepath.Join(dir, sanitize(probe.Name)+".json")
	if err := os.WriteFile(tmp, append(pretty, '\n'), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, errObj(err.Error()))
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		writeJSON(w, http.StatusInternalServerError, errObj(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Store) del(w http.ResponseWriter, dir, name string) {
	name = strings.TrimSpace(name)
	if dec, err := url.PathUnescape(name); err == nil {
		name = dec
	}
	if name == "" {
		writeJSON(w, http.StatusBadRequest, errObj("Profile name required"))
		return
	}
	_ = os.Remove(filepath.Join(dir, sanitize(name)+".json"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Migrate copies any profiles from the legacy store.json map into the
// per-lamp directory the first time (called once at startup).
func (s *Store) Migrate(lampID string, legacy map[string]json.RawMessage) {
	if len(legacy) == 0 {
		return
	}
	dir := s.dir(lampID)
	if entries, _ := os.ReadDir(dir); len(entries) > 0 {
		return // already populated
	}
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	for name, raw := range legacy {
		pretty, _ := json.MarshalIndent(raw, "", "  ")
		_ = os.WriteFile(filepath.Join(dir, sanitize(name)+".json"), append(pretty, '\n'), 0o644)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errObj(msg string) map[string]any { return map[string]any{"ok": false, "error": msg} }
