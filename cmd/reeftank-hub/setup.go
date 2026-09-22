package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cp296944/reeftank-hub/internal/config"
	"github.com/cp296944/reeftank-hub/internal/httpapi"
	"github.com/cp296944/reeftank-hub/internal/lamp"
	"github.com/cp296944/reeftank-hub/internal/version"
)

// setupAPI backs the `setup_portal` capability: the ESP32's Wi-Fi onboarding
// portal becomes a Pi settings surface. It reads/writes the daemon config file
// (location, update channel) and can restart or factory-reset the service.
// Lamp host/port/device stay on the existing /api/config endpoint.
type setupAPI struct {
	cfg        config.Config
	cfgPath    string
	dataDir    string
	autoUpdate *atomic.Bool
	api        *httpapi.Server
	lamp       *lamp.Lamp
	started    time.Time
}

func (s *setupAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup", s.handleGet)
	mux.HandleFunc("POST /api/setup", s.handlePost)
	mux.HandleFunc("POST /api/setup/restart", s.handleRestart)
	mux.HandleFunc("POST /api/setup/factory-reset", s.handleFactoryReset)
}

func (s *setupAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	lc := s.api.Config()
	writeJSON(w, http.StatusOK, map[string]any{
		"lamp": map[string]any{"host": lc.Host, "port": lc.Port, "device": lc.Device},
		"location": map[string]any{
			"latitude": s.cfg.Latitude, "longitude": s.cfg.Longitude, "timezone": s.cfg.Timezone,
		},
		"update": map[string]any{
			"channel": s.cfg.UpdateChannel, "auto_update": s.autoUpdate.Load(),
			"repo": s.cfg.UpdateRepo, "current": version.Version,
		},
		"wifi": s.lamp.Health(),
		"system": map[string]any{
			"data_dir": s.dataDir, "install_root": s.cfg.InstallRoot,
			"listen": s.cfg.Listen, "uptime_s": int(time.Since(s.started).Seconds()),
		},
		// location.latitude/longitude are stored for future sun-time features;
		// nothing consumes them yet (moon math is date-only).
		"notes": map[string]any{"lat_lon_used": false, "timezone_used": true},
	})
}

func (s *setupAPI) handlePost(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Location *struct {
			Latitude  *float64 `json:"latitude"`
			Longitude *float64 `json:"longitude"`
			Timezone  *string  `json:"timezone"`
		} `json:"location"`
		Update *struct {
			Channel *string `json:"channel"`
		} `json:"update"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad JSON"})
		return
	}

	next := s.cfg
	var restart []string

	if in.Location != nil {
		if v := in.Location.Latitude; v != nil {
			if *v < -90 || *v > 90 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "latitude out of range"})
				return
			}
			next.Latitude = *v
		}
		if v := in.Location.Longitude; v != nil {
			if *v < -180 || *v > 180 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "longitude out of range"})
				return
			}
			next.Longitude = *v
		}
		if v := in.Location.Timezone; v != nil {
			tz := strings.TrimSpace(*v)
			if _, err := time.LoadLocation(tz); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown timezone: " + tz})
				return
			}
			if tz != next.Timezone {
				next.Timezone = tz
				restart = append(restart, "timezone")
			}
		}
	}
	if in.Update != nil && in.Update.Channel != nil {
		ch := strings.TrimSpace(*in.Update.Channel)
		if ch != "stable" && ch != "prerelease" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": `channel must be "stable" or "prerelease"`})
			return
		}
		if ch != next.UpdateChannel {
			next.UpdateChannel = ch
			restart = append(restart, "update_channel")
		}
	}

	if err := next.Save(s.cfgPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "save config: " + err.Error()})
		return
	}
	s.cfg = next
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restart_required": restart})
}

func (s *setupAPI) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !confirmed(r) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": `needs {"confirm": true}`})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"restarting": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		if err := restartService(context.Background()); err != nil {
			// nothing left to report to — the connection is already closing
			fmt.Fprintln(os.Stderr, "setup: restart failed:", err)
		}
	}()
}

func (s *setupAPI) handleFactoryReset(w http.ResponseWriter, r *http.Request) {
	if !confirmed(r) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": `needs {"confirm": true} — this wipes the schedule store, effect configs and saved profiles`,
		})
		return
	}
	// Keep config.json (network + repo); wipe everything else the app owns.
	var removed, failed []string
	for _, name := range []string{"store.json", "effects.json", "profiles"} {
		p := filepath.Join(s.dataDir, name)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			failed = append(failed, name)
		} else {
			removed = append(removed, name)
		}
	}
	if len(failed) > 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"removed": removed, "failed": failed})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"removed": removed, "restarting": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		if err := restartService(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "setup: restart after factory-reset failed:", err)
		}
	}()
}

func confirmed(r *http.Request) bool {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	return in.Confirm
}

func restartService(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "restart", "reeftank-hub").CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
