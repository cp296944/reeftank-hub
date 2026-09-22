package piapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cp296944/reeftank-hub/internal/engine"
)

// EffectsStore persists the pi-bridge-side effect configs (the ones the vendored
// httpapi doesn't know about) in one JSON file under DataDir. Everything the
// engine needs for a tick is assembled in Provider.EngineSnapshot from this
// plus httpapi's stored working state.
type EffectsStore struct {
	path string
	mu   sync.Mutex

	Ramp struct {
		Active      bool `json:"active"`
		Consent     bool `json:"consent"`
		IntervalMin int  `json:"interval_min"` // send cadence when ramp on; 0 = default
	} `json:"ramp"`
	Feed struct {
		DurationMins int `json:"duration"`
		Intensity    int `json:"intensity"`
	} `json:"feed"`
	Maintenance struct {
		DurationMins int `json:"duration"`
		Intensity    int `json:"intensity"`
	} `json:"maintenance"`
	Acclimation struct {
		Enabled      bool   `json:"enabled"`
		StartPercent int    `json:"start_percent"`
		DurationDays int    `json:"duration_days"`
		StartISO     string `json:"start_iso"`
	} `json:"acclimation"`
	Seasonal struct {
		Enabled         bool `json:"enabled"`
		MaxShiftMinutes int  `json:"max_shift_minutes"`
	} `json:"seasonal"`
}

func NewEffectsStore(dataDir string) *EffectsStore {
	s := &EffectsStore{path: filepath.Join(dataDir, "effects.json")}
	s.Feed.DurationMins, s.Feed.Intensity = 15, 80
	s.Maintenance.DurationMins, s.Maintenance.Intensity = 30, 70
	s.Acclimation.StartPercent, s.Acclimation.DurationDays = 70, 21
	s.Seasonal.MaxShiftMinutes = 60
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, s)
	}
	return s
}

func (s *EffectsStore) save() {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(s.path, append(b, '\n'), 0o644)
}

// ---- ramp: the live-driver switch --------------------------------------------
// Smooth ramp on  → the engine is the live driver: every rampOnInterval it
//
//	computes the interpolated output and pushes 0x1005 on change.
//
// Smooth ramp off → the engine is dormant. /api/push writes the whole 24-slot
//
//	schedule to the lamp once (0x1007, effects pre-baked) and
//	the lamp runs it itself; the engine only steps in for timed
//	Feed/Maintenance overrides.
const (
	rampDefaultMin  = 10               // send cadence when smooth ramp is on
	rampOffInterval = 10 * time.Minute // housekeeping only; the engine isn't writing
	rampMinMin      = 2
	rampMaxMin      = 60
)

func rampIntervalMin(v int) int {
	if v <= 0 {
		return rampDefaultMin
	}
	return clampi(v, rampMinMin, rampMaxMin)
}

// applyRampCadence syncs the engine's live-driving mode + tick cadence to the
// persisted smooth-ramp state. Safe to call on startup (no lamp I/O).
func (h *handler) applyRampCadence() {
	h.fx.mu.Lock()
	on := h.fx.Ramp.Active
	every := rampIntervalMin(h.fx.Ramp.IntervalMin)
	h.fx.mu.Unlock()
	if h.Engine == nil {
		return
	}
	h.Engine.SetLive(on)
	if on {
		h.Engine.SetInterval(time.Duration(every) * time.Minute)
	} else {
		h.Engine.SetInterval(rampOffInterval)
	}
}

func (h *handler) rampStatus(w http.ResponseWriter, r *http.Request) {
	h.fx.mu.Lock()
	st := map[string]any{
		"active":       h.fx.Ramp.Active,
		"consent":      h.fx.Ramp.Consent,
		"last_tick":    h.Engine.LastPush().Unix(),
		"interval_s":   int(h.Engine.Interval().Seconds()),
		"interval_min": rampIntervalMin(h.fx.Ramp.IntervalMin),
	}
	h.fx.mu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

// rampConfig sets the smooth-ramp send cadence (minutes) without toggling it.
func (h *handler) rampConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IntervalMin int `json:"interval_min"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad JSON"})
		return
	}
	h.fx.mu.Lock()
	h.fx.Ramp.IntervalMin = clampi(in.IntervalMin, rampMinMin, rampMaxMin)
	h.fx.save()
	h.fx.mu.Unlock()
	h.applyRampCadence()
	h.rampStatus(w, r)
}

func (h *handler) rampStart(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Consent bool `json:"consent"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	h.fx.mu.Lock()
	h.fx.Ramp.Active = true
	if in.Consent {
		h.fx.Ramp.Consent = true
	}
	h.fx.save()
	h.fx.mu.Unlock()
	h.applyRampCadence()
	h.Engine.Kick()
	h.rampStatus(w, r)
}

func (h *handler) rampStop(w http.ResponseWriter, r *http.Request) {
	h.fx.mu.Lock()
	h.fx.Ramp.Active = false
	h.fx.save()
	h.fx.mu.Unlock()
	h.applyRampCadence()
	// Smooth ramp was the live driver; hand the lamp back its own 0x1007
	// schedule so it keeps running a curve now that the engine went dormant.
	if h.API != nil {
		if err := h.API.Republish(); err != nil {
			slog.Warn("piapi: re-arm lamp schedule on ramp stop failed", "err", err)
		}
	}
	h.rampStatus(w, r)
}

func (h *handler) rampTick(w http.ResponseWriter, r *http.Request) {
	h.Engine.Kick()
	h.rampStatus(w, r)
}

// ---- feed / maintenance: timed overrides -------------------------------

// Channel tables ported from arduino/src/Effects.cpp (k7pro rows).
var (
	feedPro        = [engine.Channels]int{80, 10, 40, 5, 10, 0}
	maintenancePro = [engine.Channels]int{100, 30, 55, 15, 40, 5}
)

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (h *handler) feedStatus(w http.ResponseWriter, r *http.Request) {
	active, src, until := h.Engine.OverrideActive()
	h.fx.mu.Lock()
	dur, inten := h.fx.Feed.DurationMins, h.fx.Feed.Intensity
	h.fx.mu.Unlock()
	rem := 0
	if active && src == "feed" {
		rem = int(time.Until(until).Seconds())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active": active && src == "feed", "remaining": rem,
		"duration": dur, "intensity": inten,
	})
}

func (h *handler) feedStart(w http.ResponseWriter, r *http.Request) {
	var in struct{ Minutes, Intensity, Duration int }
	_ = json.NewDecoder(r.Body).Decode(&in)
	h.fx.mu.Lock()
	if in.Minutes > 0 {
		h.fx.Feed.DurationMins = clampi(in.Minutes, 1, 60)
	} else if in.Duration > 0 {
		h.fx.Feed.DurationMins = clampi(in.Duration, 1, 60)
	}
	if in.Intensity > 0 {
		h.fx.Feed.Intensity = clampi(in.Intensity, 1, 100)
	}
	dur, inten := h.fx.Feed.DurationMins, h.fx.Feed.Intensity
	h.fx.save()
	h.fx.mu.Unlock()

	ch := feedPro
	ch[3] = inten // matches Effects.cpp feedTask: ch[isPro?3:0] = intensity
	h.Engine.SetOverride(&engine.Override{
		Channels: ch, Source: "feed",
		Until: time.Now().Add(time.Duration(dur) * time.Minute),
	})
	h.feedStatus(w, r)
}

func (h *handler) feedStop(w http.ResponseWriter, r *http.Request) {
	if a, s, _ := h.Engine.OverrideActive(); a && s == "feed" {
		h.Engine.SetOverride(nil)
	}
	h.feedStatus(w, r)
}

func (h *handler) maintStatus(w http.ResponseWriter, r *http.Request) {
	active, src, until := h.Engine.OverrideActive()
	h.fx.mu.Lock()
	dur, inten := h.fx.Maintenance.DurationMins, h.fx.Maintenance.Intensity
	h.fx.mu.Unlock()
	rem := 0
	if active && src == "maintenance" {
		rem = int(time.Until(until).Seconds())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active": active && src == "maintenance", "remaining": rem,
		"duration": dur, "intensity": inten,
	})
}

func (h *handler) maintStart(w http.ResponseWriter, r *http.Request) {
	var in struct{ Minutes, Intensity, Duration int }
	_ = json.NewDecoder(r.Body).Decode(&in)
	h.fx.mu.Lock()
	if in.Minutes > 0 {
		h.fx.Maintenance.DurationMins = clampi(in.Minutes, 1, 180)
	} else if in.Duration > 0 {
		h.fx.Maintenance.DurationMins = clampi(in.Duration, 1, 180)
	}
	if in.Intensity > 0 {
		h.fx.Maintenance.Intensity = clampi(in.Intensity, 1, 100)
	}
	dur, inten := h.fx.Maintenance.DurationMins, h.fx.Maintenance.Intensity
	h.fx.save()
	h.fx.mu.Unlock()

	var ch [engine.Channels]int
	for i := 0; i < engine.Channels; i++ {
		ch[i] = clampi(maintenancePro[i]*inten/100, 0, 100)
	}
	h.Engine.SetOverride(&engine.Override{
		Channels: ch, Source: "maintenance",
		Until: time.Now().Add(time.Duration(dur) * time.Minute),
	})
	h.maintStatus(w, r)
}

func (h *handler) maintStop(w http.ResponseWriter, r *http.Request) {
	if a, s, _ := h.Engine.OverrideActive(); a && s == "maintenance" {
		h.Engine.SetOverride(nil)
	}
	h.maintStatus(w, r)
}

// ---- acclimation / seasonal --------------------------------------------

func (h *handler) acclimationConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var in struct {
			Enabled      *bool `json:"enabled"`
			StartPercent *int  `json:"start_percent"`
			DurationDays *int  `json:"duration_days"`
			Restart      bool  `json:"restart"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			writeJSON(w, http.StatusBadRequest, errObj("bad json"))
			return
		}
		h.fx.mu.Lock()
		if in.Enabled != nil {
			h.fx.Acclimation.Enabled = *in.Enabled
			if *in.Enabled && (h.fx.Acclimation.StartISO == "" || in.Restart) {
				h.fx.Acclimation.StartISO = time.Now().Format(time.RFC3339)
			}
		}
		if in.StartPercent != nil {
			h.fx.Acclimation.StartPercent = clampi(*in.StartPercent, 1, 100)
		}
		if in.DurationDays != nil {
			h.fx.Acclimation.DurationDays = clampi(*in.DurationDays, 1, 120)
		}
		h.fx.save()
		h.fx.mu.Unlock()
		h.Engine.Kick()
	}
	h.acclimationStatus(w, r)
}

func (h *handler) acclimationStatus(w http.ResponseWriter, r *http.Request) {
	h.fx.mu.Lock()
	a := h.fx.Acclimation
	h.fx.mu.Unlock()
	cfg := engine.Config{}
	cfg.Acclimation.Enabled = a.Enabled
	cfg.Acclimation.StartPercent = a.StartPercent
	cfg.Acclimation.DurationDays = a.DurationDays
	if t, err := time.Parse(time.RFC3339, a.StartISO); err == nil {
		cfg.Acclimation.StartEpoch = t.Unix()
	}
	now := time.Now().In(h.TZ)
	daysLeft := 0
	if a.Enabled && cfg.Acclimation.StartEpoch > 0 {
		elapsed := int(now.Sub(time.Unix(cfg.Acclimation.StartEpoch, 0)).Hours() / 24)
		daysLeft = a.DurationDays - elapsed
		if daysLeft < 0 {
			daysLeft = 0
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": a.Enabled, "current_percent": cfg.AcclimationPercent(now),
		"days_remaining": daysLeft, "start_percent": a.StartPercent,
		"duration_days": a.DurationDays, "start_iso": a.StartISO,
	})
}

func (h *handler) seasonalConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var in struct {
			Enabled         *bool `json:"enabled"`
			MaxShiftMinutes *int  `json:"max_shift_minutes"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			writeJSON(w, http.StatusBadRequest, errObj("bad json"))
			return
		}
		h.fx.mu.Lock()
		if in.Enabled != nil {
			h.fx.Seasonal.Enabled = *in.Enabled
		}
		if in.MaxShiftMinutes != nil {
			h.fx.Seasonal.MaxShiftMinutes = clampi(*in.MaxShiftMinutes, 0, 180)
		}
		h.fx.save()
		h.fx.mu.Unlock()
		h.Engine.Kick()
	}
	h.seasonalStatus(w, r)
}

func (h *handler) seasonalStatus(w http.ResponseWriter, r *http.Request) {
	h.fx.mu.Lock()
	se := h.fx.Seasonal
	h.fx.mu.Unlock()
	cfg := engine.Config{}
	cfg.Seasonal.Enabled = se.Enabled
	cfg.Seasonal.MaxShiftMinutes = se.MaxShiftMinutes
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": se.Enabled, "max_shift_minutes": se.MaxShiftMinutes,
		"current_shift_minutes": cfg.SeasonalShiftMinutes(time.Now().In(h.TZ)),
	})
}
