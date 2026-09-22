// VENDORED + ADAPTED from pc-bridge/internal/bridge/server.go
// upstream/master @ dafa6809fdf85fe44445b3ce2735605e0f884320
//
// Changes from upstream (keep this list current for re-sync):
//   - package bridge -> package httpapi
//   - k7tcp import -> pi-bridge vendored copy
//   - capabilities are injected (Options.Capabilities) not hard-coded,
//     so Phase 3 can flip the 9 always-on flags true
//   - store path + lamp identity come from Options (ConfigPath, LampHost/Port)
//   - New() signature takes Options
//   - added getters: LampName(), LegacyProfiles(), StateSnapshot(), Device(),
//     SetCapability()
//   - Options.LampGate: a shared mutex locked around every lamp TCP op so this
//     server, the always-on engine and the proxy never collide (the lamp takes
//     one connection at a time)
// tools/check_httpapi_sync.py reports upstream drift (advisory).

package httpapi

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cp296944/reeftank-hub/internal/k7tcp"
	"github.com/cp296944/reeftank-hub/internal/tally"
)

//go:embed static/*.html static/vendor/* diagnostic/*.html presets.json presets-oem.json
var staticFiles embed.FS

type Config struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Device string `json:"device"`
}

type State struct {
	Name                 string            `json:"name"`
	Mode                 string            `json:"mode"`
	Manual               []int             `json:"manual"`
	Schedule             [][]int           `json:"schedule"`
	ActivePreset         string            `json:"active_preset"`
	ScheduleShiftMinutes int               `json:"schedule_shift_minutes"`
	MasterBrightness     int               `json:"master_brightness"`
	Siesta               SiestaConfig      `json:"siesta"`
	Lunar                LunarConfig       `json:"lunar"`
	LastReadAt           string            `json:"last_read_at,omitempty"`
	LastPushedAt         string            `json:"last_pushed_at,omitempty"`
	Extras               map[string]string `json:"extras,omitempty"`
}

type SiestaConfig struct {
	Enabled      bool   `json:"enabled"`
	Active       bool   `json:"active"`
	Start        string `json:"start"`
	Duration     int    `json:"duration"`
	DurationMins int    `json:"duration_mins"`
	Intensity    int    `json:"intensity"`
}

type LunarConfig struct {
	Enabled       bool   `json:"enabled"`
	Active        bool   `json:"active"`
	Phase         int    `json:"phase"`
	Illumination  int    `json:"illumination"`
	PhaseName     string `json:"phase_name"`
	Start         string `json:"start"`
	End           string `json:"end"`
	ClampStart    string `json:"clamp_start"`
	ClampEnd      string `json:"clamp_end"`
	MaxIntensity  int    `json:"max_intensity"`
	DayThreshold  int    `json:"day_threshold"`
	TrackMoonrise bool   `json:"track_moonrise"`
}

type storeFile struct {
	Kind       string                     `json:"kind"`
	Schema     int                        `json:"schema"`
	Config     Config                     `json:"config"`
	State      State                      `json:"state"`
	Profiles   map[string]json.RawMessage `json:"profiles"`
	ExportedAt string                     `json:"exported_at,omitempty"`
}

type Server struct {
	mu           sync.RWMutex
	config       Config
	state        State
	profiles     map[string]json.RawMessage
	configPath   string
	timeout      time.Duration
	version      string
	firstRun     bool
	bridgeName   string
	platformName string
	capabilities map[string]bool
	lampGate     *sync.Mutex
	tally        *tally.Counter // shared restart-surviving lamp-write counter (manual side)
}

// Options configures a Server. pi-bridge injects its own identity and the full
// capability set (upstream pc-bridge hard-codes both).
type Options struct {
	ConfigPath   string
	Timeout      time.Duration
	Version      string
	BridgeName   string          // e.g. "pi-bridge"
	PlatformName string          // e.g. "reeftank_hub"
	Device       string          // "k7mini" | "k7pro" default when store is fresh
	Capabilities map[string]bool // the 18 capability flags; nil -> DefaultCapabilities()

	// LampHost/LampPort, when set, are authoritative — pi-bridge's lamp address
	// is fixed by the hardware wiring, so the daemon config wins over whatever
	// is in the store. (A UI POST /api/config still works until the next
	// restart.)
	LampHost string
	LampPort int

	// LampGate, when set, is locked around every lamp TCP op so this server,
	// the always-on engine and the proxy never talk to the lamp at once.
	LampGate *sync.Mutex

	// Tally, when set, receives one AddManual() per user-initiated lamp write
	// (push / hand / preview / re-arm) — shared with the engine, survives OTA.
	Tally *tally.Counter
}

// DefaultCapabilities is the "free" set — everything pc-bridge already serves,
// with the 9 always-on features off. Phase 3 flips those to true.
func DefaultCapabilities() map[string]bool {
	return map[string]bool{
		"read_lamp": true, "push_schedule": true, "manual_preview": true,
		"profiles": true, "community_presets": true, "community_presets_browse": true,
		"backup_restore": true, "fixed_lunar": true, "siesta_baked_schedule": true,
		"smooth_ramp": false, "tracked_lunar": false, "acclimation": false,
		"seasonal_daylength": false, "feed_mode": false, "maintenance_mode": false,
		"setup_portal": false, "logs": false, "persistent_controller_clock": false,
	}
}

func New(o Options) (*Server, error) {
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.BridgeName == "" {
		o.BridgeName = "pi-bridge"
	}
	if o.PlatformName == "" {
		o.PlatformName = "reeftank_hub"
	}
	if o.Device == "" {
		o.Device = "k7pro"
	}
	if o.Capabilities == nil {
		o.Capabilities = DefaultCapabilities()
	}
	_, statErr := os.Stat(o.ConfigPath)
	s := &Server{
		config: Config{
			Host:   k7tcp.DefaultHost,
			Port:   k7tcp.DefaultPort,
			Device: o.Device,
		},
		state:        defaultState(),
		profiles:     map[string]json.RawMessage{},
		configPath:   o.ConfigPath,
		timeout:      o.Timeout,
		version:      o.Version,
		firstRun:     os.IsNotExist(statErr),
		bridgeName:   o.BridgeName,
		platformName: o.PlatformName,
		capabilities: o.Capabilities,
		lampGate:     o.LampGate,
		tally:        o.Tally,
	}
	if err := s.loadStore(); err != nil {
		return nil, err
	}
	if o.LampHost != "" {
		s.config.Host = o.LampHost
	}
	if o.LampPort > 0 {
		s.config.Port = o.LampPort
	}
	return s, nil
}

func (s *Server) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(mustSub(staticFiles, "static")))))
	mux.Handle("/diagnostic/", http.StripPrefix("/diagnostic/", http.FileServer(http.FS(mustSub(staticFiles, "diagnostic")))))
	mux.HandleFunc("/diagnostic", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/diagnostic/test.html", http.StatusFound)
	})
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/capabilities", s.handleCapabilities)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/devices", s.handleDevices)
	mux.HandleFunc("/api/master", s.handleMaster)
	mux.HandleFunc("/api/lamp/read", s.handleLampRead)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/presets", s.handlePresets)
	mux.HandleFunc("/api/profiles", s.handleProfiles)
	mux.HandleFunc("/api/profiles/", s.handleProfileByName)
	mux.HandleFunc("/api/backup", s.handleBackup)
	mux.HandleFunc("/api/preview", s.handlePreview)
	mux.HandleFunc("/api/hand", s.handleHand)
	mux.HandleFunc("/api/push", s.handlePush)
	mux.HandleFunc("/api/siesta/status", s.handleSiestaStatus)
	mux.HandleFunc("/api/siesta/schedule", s.handleSiestaSchedule)
	mux.HandleFunc("/api/lunar/status", s.handleLunarStatus)
	mux.HandleFunc("/api/lunar/schedule", s.handleLunarSchedule)
	mux.HandleFunc("/api/lunar/start", s.handleLunarStart)
	mux.HandleFunc("/api/lunar/stop", s.handleLunarStop)
	mux.HandleFunc("/api/community-presets", s.handleCommunityPresets)
	return withCORS(mux)
}

func (s *Server) client() k7tcp.Client {
	cfg := s.Config()
	return k7tcp.New(cfg.Host, cfg.Port, s.timeout)
}

// countManualWrite tallies a user-initiated lamp write (push / hand / preview /
// re-arm) into the shared restart-surviving counter.
func (s *Server) countManualWrite() {
	if s.tally != nil {
		s.tally.AddManual()
	}
}

// Republish re-sends the last-pushed schedule + manual state to the lamp
// (0x1007), handing autonomous scheduling back to the lamp. Called when the
// engine stops being the live driver (smooth ramp turned off) or a timed
// Feed/Maintenance override ends while the engine is dormant.
func (s *Server) Republish() error {
	s.mu.RLock()
	manualInts := append([]int(nil), s.state.Manual...)
	schedInts := make([][]int, len(s.state.Schedule))
	for i, row := range s.state.Schedule {
		schedInts[i] = append([]int(nil), row...)
	}
	auto := s.state.Mode != "manual"
	s.mu.RUnlock()

	manual, err := normalizeManual(manualInts)
	if err != nil {
		return err
	}
	sched, err := normalizeSchedule(schedInts)
	if err != nil {
		return err
	}
	unlock := s.lampLock()
	err = s.client().PushSchedule(manual, sched, auto)
	unlock()
	if err == nil {
		s.countManualWrite()
	}
	return err
}

// lampLock serialises this server's lamp I/O with the always-on engine and the
// raw proxy (the lamp accepts one TCP connection at a time). pi-bridge injects
// the shared gate via Options.LampGate; without it this is a no-op.
func (s *Server) lampLock() func() {
	if s.lampGate == nil {
		return func() {}
	}
	s.lampGate.Lock()
	return s.lampGate.Unlock
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	view := r.URL.Query().Get("view")
	ua := r.Header.Get("User-Agent")
	mobile := view == "mobile" || (view != "desktop" && (strings.Contains(ua, "Mobile") ||
		strings.Contains(ua, "Android") || strings.Contains(ua, "iPhone") ||
		strings.Contains(ua, "iPad")))
	if mobile {
		http.Redirect(w, r, "/static/mobile.html", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/static/", http.StatusFound)
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

const communityPresetsURL = "https://bitbarista.github.io/k7-led-controller/community-presets/index.json"

func (s *Server) handleCommunityPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(communityPresetsURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Community presets fetch failed: %v", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Community presets fetch failed: HTTP %d", resp.StatusCode))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	io.Copy(w, resp.Body) //nolint:errcheck
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bridge":    s.bridgeName,
		"platform":  s.platformName,
		"transport": "direct_lamp",
		"firmware":  s.version,
		"version":   s.version,
	})
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.mu.RLock()
	caps := make(map[string]bool, len(s.capabilities))
	for k, v := range s.capabilities {
		caps[k] = v
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"platform":     s.platformName,
		"transport":    "direct_lamp",
		"capabilities": caps,
	})
}

// SetCapability flips one capability flag at runtime (Phase 3 uses this as
// features come online).
func (s *Server) SetCapability(name string, on bool) {
	s.mu.Lock()
	s.capabilities[name] = on
	s.mu.Unlock()
}

// LampName returns the lamp's advertised name from the last successful read
// ("" if never read). pi-bridge uses it as a profile-storage key fallback.
func (s *Server) LampName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Name
}

// LegacyProfiles returns a copy of the profiles held in the pc-bridge-style
// store, so pi-bridge can migrate them into its per-lamp layout once.
func (s *Server) LegacyProfiles() map[string]json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneProfiles(s.profiles)
}

// StateSnapshot returns a copy of the working state (schedule, manual, mode,
// master, shift, siesta, lunar) — the always-on engine reads this each tick.
func (s *Server) StateSnapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.state
	st.Manual = append([]int(nil), s.state.Manual...)
	st.Schedule = make([][]int, len(s.state.Schedule))
	for i, row := range s.state.Schedule {
		st.Schedule[i] = append([]int(nil), row...)
	}
	return st
}

// Device returns the configured lamp model ("k7mini"|"k7pro").
func (s *Server) Device() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Device
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Config())
	case http.MethodPost:
		var in Config
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "Bad JSON")
			return
		}
		s.mu.Lock()
		if strings.TrimSpace(in.Host) != "" {
			s.config.Host = strings.TrimSpace(in.Host)
		}
		if in.Port > 0 && in.Port <= 65535 {
			s.config.Port = in.Port
		}
		if in.Device == "k7mini" || in.Device == "k7pro" {
			s.config.Device = in.Device
		}
		cfg := s.config
		s.mu.Unlock()

		if err := s.saveStore(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save config failed: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"k7mini": map[string]any{
			"label":    "K7 Mini",
			"channels": []string{"white", "royal_blue", "blue"},
		},
		"k7pro": map[string]any{
			"label":    "K7 Pro",
			"channels": []string{"white", "royal_blue", "green", "uv", "cyan", "red"},
		},
	})
}

func (s *Server) handleMaster(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		value := s.state.MasterBrightness
		s.mu.RUnlock()
		if value <= 0 {
			value = 100
		}
		writeJSON(w, http.StatusOK, map[string]int{"value": value})
	case http.MethodPost:
		var in struct {
			Value int `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "Bad JSON")
			return
		}
		value := clamp(in.Value, 0, 200)
		s.mu.Lock()
		s.state.MasterBrightness = value
		s.mu.Unlock()
		if err := s.saveStore(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save state failed: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"value": value})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleLampRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	unlock := s.lampLock()
	state, err := k7tcp.ReadAllRobust(s.client()) // retrying, frame-aware
	unlock()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := s.saveStateFromLamp(state, true, false); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save state failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.mu.RLock()
	state := s.state
	firstRun := s.firstRun
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, struct {
		State
		FirstRun bool `json:"first_run,omitempty"`
	}{State: state, FirstRun: firstRun})
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	presets, err := staticFiles.ReadFile("presets.json")
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Load presets failed: %v", err))
		return
	}
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(presets, &catalog); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Parse presets failed: %v", err))
		return
	}
	s.mu.RLock()
	device := s.config.Device
	s.mu.RUnlock()
	if device == "" {
		device = "k7mini"
	}
	payload, ok := catalog[device]
	if !ok {
		payload = catalog["k7mini"]
		device = "k7mini"
	}

	// Merge the transcribed Noo-Psyche factory curves (preset:oem-*), so users
	// have a starting point identical to the OEM app.
	if oemRaw, e := staticFiles.ReadFile("presets-oem.json"); e == nil {
		var oem map[string]map[string]json.RawMessage
		if json.Unmarshal(oemRaw, &oem) == nil {
			var cur map[string]json.RawMessage
			if json.Unmarshal(payload, &cur) == nil {
				for k, v := range oem[device] {
					cur[k] = v
				}
				if merged, e := json.Marshal(cur); e == nil {
					payload = merged
				}
			}
		}
	}
	writeRawJSON(w, http.StatusOK, payload)
}

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		profiles := cloneProfiles(s.profiles)
		s.mu.RUnlock()
		writeJSON(w, http.StatusOK, profiles)
	case http.MethodPost:
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeError(w, http.StatusBadRequest, "Bad JSON")
			return
		}
		var probe struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			writeError(w, http.StatusBadRequest, "Bad JSON")
			return
		}
		name := strings.TrimSpace(probe.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "Name required")
			return
		}
		s.mu.Lock()
		s.profiles[name] = raw
		s.mu.Unlock()
		if err := s.saveStore(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save profiles failed: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handleProfileByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	name, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/api/profiles/"))
	if err != nil || strings.TrimSpace(name) == "" {
		writeError(w, http.StatusBadRequest, "Profile name required")
		return
	}
	s.mu.Lock()
	delete(s.profiles, name)
	s.mu.Unlock()
	if err := s.saveStore(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save profiles failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		backup := s.snapshotStore()
		backup.Kind = "k7_pc_bridge_backup"
		backup.ExportedAt = time.Now().Format(time.RFC3339)
		w.Header().Set("Content-Disposition", `attachment; filename="k7-pc-bridge-backup.json"`)
		writeJSON(w, http.StatusOK, backup)
	case http.MethodPost:
		var backup storeFile
		if err := json.NewDecoder(r.Body).Decode(&backup); err != nil {
			writeError(w, http.StatusBadRequest, "Bad JSON")
			return
		}
		if backup.Kind != "k7_pc_bridge_backup" || backup.Schema != 1 {
			writeError(w, http.StatusBadRequest, "Unsupported backup format")
			return
		}
		if backup.Profiles == nil {
			backup.Profiles = map[string]json.RawMessage{}
		}
		normalizeConfig(&backup.Config)
		normalizeState(&backup.State)
		s.mu.Lock()
		s.config = backup.Config
		s.state = backup.State
		s.profiles = cloneProfiles(backup.Profiles)
		s.mu.Unlock()
		if err := s.saveStore(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save backup failed: %v", err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ch, err := decodeChannels(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	unlock := s.lampLock()
	lerr := s.client().PreviewBrightness(ch)
	unlock()
	if lerr != nil {
		writeError(w, http.StatusBadGateway, lerr.Error())
		return
	}
	s.countManualWrite()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleHand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ch, err := decodeChannels(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	unlock := s.lampLock()
	lerr := s.client().HandLuminance(ch)
	unlock()
	if lerr != nil {
		writeError(w, http.StatusBadGateway, lerr.Error())
		return
	}
	s.countManualWrite()
	if err := s.saveManualState(ch); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save state failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	var in struct {
		Manual       []int   `json:"manual"`
		Schedule     [][]int `json:"schedule"`
		Mode         string  `json:"mode"`
		ActivePreset string  `json:"active_preset"`
		// Prebaked is set by the piapi middleware when smooth ramp is off: it has
		// already folded every effect (acclimation, seasonal, tracked lunar,
		// siesta, master) into the 24 rows via the engine, so this handler must
		// send them verbatim rather than baking a second time.
		Prebaked bool `json:"prebaked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Bad JSON")
		return
	}

	manual, err := normalizeManual(in.Manual)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	schedule, err := normalizeSchedule(in.Schedule)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	autoMode := in.Mode != "manual"

	var manualForLamp [k7tcp.Channels]uint8
	var scheduleForLamp [k7tcp.Slots][8]uint8
	if in.Prebaked {
		manualForLamp, scheduleForLamp = manual, schedule
	} else {
		s.mu.RLock()
		master := s.state.MasterBrightness
		siesta := s.state.Siesta
		lunar := s.state.Lunar
		s.mu.RUnlock()
		if presetDisablesLunar(in.ActivePreset) {
			lunar.Enabled = false
			lunar.Active = false
		}
		schedule = bakePCBridgeEffects(schedule, siesta, lunar)
		manualForLamp, scheduleForLamp = applyMasterToLampState(manual, schedule, master)
	}

	unlock := s.lampLock()
	lerr := s.client().PushSchedule(manualForLamp, scheduleForLamp, autoMode)
	unlock()
	if lerr != nil {
		writeError(w, http.StatusBadGateway, lerr.Error())
		return
	}
	s.countManualWrite()
	if err := s.savePushedState(manualForLamp, scheduleForLamp, autoMode, in.ActivePreset, presetDisablesLunar(in.ActivePreset)); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save state failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSiestaStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.mu.RLock()
	cfg := normalizeSiesta(s.state.Siesta)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSiestaSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var cfg SiestaConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "Bad JSON")
		return
	}
	cfg = normalizeSiesta(cfg)
	s.mu.Lock()
	s.state.Siesta = cfg
	s.mu.Unlock()
	if err := s.saveStore(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save state failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleLunarStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	s.mu.RLock()
	cfg := normalizeLunar(s.state.Lunar)
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleLunarSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var cfg LunarConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "Bad JSON")
		return
	}
	cfg = normalizeLunar(cfg)
	s.mu.Lock()
	s.state.Lunar = cfg
	s.mu.Unlock()
	if err := s.saveStore(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save state failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleLunarStart(w http.ResponseWriter, r *http.Request) {
	s.setLunarEnabled(w, r, true)
}

func (s *Server) handleLunarStop(w http.ResponseWriter, r *http.Request) {
	s.setLunarEnabled(w, r, false)
}

func (s *Server) setLunarEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	s.mu.Lock()
	cfg := normalizeLunar(s.state.Lunar)
	cfg.Enabled = enabled
	cfg.Active = enabled
	s.state.Lunar = cfg
	s.mu.Unlock()
	if err := s.saveStore(); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Save state failed: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) loadStore() error {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if _, ok := raw["config"]; ok {
		var store storeFile
		if err := json.Unmarshal(data, &store); err != nil {
			return err
		}
		normalizeConfig(&store.Config)
		normalizeState(&store.State)
		s.config = store.Config
		s.state = store.State
		if store.Profiles != nil {
			s.profiles = cloneProfiles(store.Profiles)
		}
		return nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	normalizeConfig(&cfg)
	s.config = cfg
	return nil
}

func (s *Server) saveStore() error {
	store := s.snapshotStore()
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath, append(data, '\n'), 0644)
}

func (s *Server) snapshotStore() storeFile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return storeFile{
		Kind:     "k7_pc_bridge_store",
		Schema:   1,
		Config:   s.config,
		State:    s.state,
		Profiles: cloneProfiles(s.profiles),
	}
}

func (s *Server) saveStateFromLamp(lamp k7tcp.LampState, read bool, pushed bool) error {
	state := State{
		Name:                 lamp.Name,
		Mode:                 "auto",
		Manual:               intsFromManual(lamp.Manual),
		Schedule:             intsFromSchedule(lamp.Schedule),
		ScheduleShiftMinutes: 0,
		MasterBrightness:     100,
	}
	now := time.Now().Format(time.RFC3339)
	if read {
		state.LastReadAt = now
	}
	if pushed {
		state.LastPushedAt = now
	}
	s.mu.Lock()
	if s.state.Mode == "manual" {
		state.Mode = "manual"
	}
	state.ActivePreset = s.state.ActivePreset
	state.Siesta = s.state.Siesta
	state.Lunar = s.state.Lunar
	if !pushed {
		state.LastPushedAt = s.state.LastPushedAt
	}
	s.state = state
	// The lamp's own name tells us the model (k7m… = Mini/3ch, k7_/k7p… =
	// Pro/6ch). Auto-correct a wrong config.Device so presets + channel
	// semantics match the hardware. (OEM app keys off the same prefix.)
	if dev := deviceFromLampName(lamp.Name); dev != "" && dev != s.config.Device {
		slog.Info("lamp reports a different model than config — correcting",
			"lamp_name", lamp.Name, "was", s.config.Device, "now", dev)
		s.config.Device = dev
	}
	s.mu.Unlock()
	return s.saveStore()
}

// deviceFromLampName maps a lamp's advertised name to "k7mini" / "k7pro", or ""
// if it doesn't look like a K7.
func deviceFromLampName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(n, "k7m"):
		return "k7mini"
	case strings.HasPrefix(n, "k7_"), strings.HasPrefix(n, "k7p"), strings.HasPrefix(n, "k7pro"):
		return "k7pro"
	default:
		return ""
	}
}

func (s *Server) saveManualState(ch [k7tcp.Channels]uint8) error {
	s.mu.Lock()
	s.state.Mode = "manual"
	s.state.Manual = intsFromChannels(ch)
	s.mu.Unlock()
	return s.saveStore()
}

func (s *Server) savePushedState(manual [k7tcp.Channels]uint8, schedule [k7tcp.Slots][8]uint8, autoMode bool, activePreset string, disableLunar bool) error {
	mode := "manual"
	if autoMode {
		mode = "auto"
	}
	s.mu.Lock()
	s.state.Mode = mode
	s.state.Manual = intsFromChannels(manual)
	s.state.Schedule = intsFromUintSchedule(schedule)
	s.state.ActivePreset = strings.TrimSpace(activePreset)
	if disableLunar {
		lunar := normalizeLunar(s.state.Lunar)
		lunar.Enabled = false
		lunar.Active = false
		s.state.Lunar = lunar
	}
	s.state.LastPushedAt = time.Now().Format(time.RFC3339)
	s.mu.Unlock()
	return s.saveStore()
}

func decodeChannels(r *http.Request) ([k7tcp.Channels]uint8, error) {
	var in struct {
		Channels []int `json:"channels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return [k7tcp.Channels]uint8{}, fmt.Errorf("Bad JSON")
	}
	return normalizeManual(in.Channels)
}

func normalizeManual(values []int) ([k7tcp.Channels]uint8, error) {
	var out [k7tcp.Channels]uint8
	if len(values) == 0 {
		return out, fmt.Errorf("manual/channels required")
	}
	for i := 0; i < len(values) && i < k7tcp.Channels; i++ {
		if values[i] < 0 || values[i] > 100 {
			return out, fmt.Errorf("channel %d out of range 0-100", i)
		}
		out[i] = uint8(values[i])
	}
	return out, nil
}

func normalizeSchedule(values [][]int) ([k7tcp.Slots][8]uint8, error) {
	var out [k7tcp.Slots][8]uint8
	if len(values) != k7tcp.Slots {
		return out, fmt.Errorf("schedule must contain exactly %d rows", k7tcp.Slots)
	}
	for h, row := range values {
		if len(row) < 8 {
			return out, fmt.Errorf("schedule row %d must contain hour, minute, and six channels", h)
		}
		for i := 0; i < 8; i++ {
			if row[i] < 0 || row[i] > 100 {
				return out, fmt.Errorf("schedule row %d value %d out of range 0-100", h, i)
			}
			out[h][i] = uint8(row[i])
		}
		if out[h][0] > 23 || out[h][1] > 59 {
			return out, fmt.Errorf("schedule row %d has invalid time", h)
		}
	}
	return out, nil
}

func applyMasterToLampState(manual [k7tcp.Channels]uint8, schedule [k7tcp.Slots][8]uint8, master int) ([k7tcp.Channels]uint8, [k7tcp.Slots][8]uint8) {
	if master <= 0 {
		master = 0
	}
	if master == 100 {
		return manual, schedule
	}
	var scaledManual [k7tcp.Channels]uint8
	var scaledSchedule [k7tcp.Slots][8]uint8
	for i := 0; i < k7tcp.Channels; i++ {
		scaledManual[i] = scalePercent(manual[i], master)
	}
	for h := 0; h < k7tcp.Slots; h++ {
		scaledSchedule[h][0] = schedule[h][0]
		scaledSchedule[h][1] = schedule[h][1]
		for c := 2; c < 8; c++ {
			scaledSchedule[h][c] = scalePercent(schedule[h][c], master)
		}
	}
	return scaledManual, scaledSchedule
}

func bakePCBridgeEffects(schedule [k7tcp.Slots][8]uint8, siesta SiestaConfig, lunar LunarConfig) [k7tcp.Slots][8]uint8 {
	out := schedule
	siesta = normalizeSiesta(siesta)
	lunar = normalizeLunar(lunar)
	if siesta.Enabled {
		start := parseHHMM(siesta.Start, 13*60)
		end := start + siesta.Duration
		factor := 100 - siesta.Intensity
		for h := 0; h < k7tcp.Slots; h++ {
			mins := int(out[h][0])*60 + int(out[h][1])
			if mins >= start && mins < end {
				for c := 2; c < 8; c++ {
					out[h][c] = scalePercent(out[h][c], factor)
				}
			}
		}
	}
	if lunar.Enabled && lunar.MaxIntensity > 0 {
		start := parseHHMM(lunar.Start, 18*60+30)
		end := parseHHMM(lunar.End, 6*60+30)
		for h := 0; h < k7tcp.Slots; h++ {
			mins := int(out[h][0])*60 + int(out[h][1])
			if !minsInWindow(mins, start, end) || scheduleDaylightLevel(out[h]) > lunar.DayThreshold {
				continue
			}
			if out[h][3] < uint8(lunar.MaxIntensity) {
				out[h][3] = uint8(lunar.MaxIntensity)
			}
			cyan := uint8((lunar.MaxIntensity*7 + 5) / 10)
			if out[h][6] < cyan {
				out[h][6] = cyan
			}
		}
	}
	return out
}

func presetDisablesLunar(activePreset string) bool {
	return strings.EqualFold(strings.TrimSpace(activePreset), "preset:dino")
}

func scheduleDaylightLevel(row [8]uint8) int {
	maxValue := 0
	for c := 2; c < 8; c++ {
		if int(row[c]) > maxValue {
			maxValue = int(row[c])
		}
	}
	return maxValue
}

func scalePercent(value uint8, master int) uint8 {
	scaled := int(value)*master + 50
	scaled /= 100
	if scaled < 0 {
		return 0
	}
	if scaled > 100 {
		return 100
	}
	return uint8(scaled)
}

func defaultState() State {
	return State{
		Name:                 "",
		Mode:                 "auto",
		Manual:               make([]int, k7tcp.Channels),
		Schedule:             defaultSchedule(),
		ScheduleShiftMinutes: 0,
		MasterBrightness:     100,
		Siesta:               defaultSiesta(),
		Lunar:                defaultLunar(),
	}
}

func defaultSchedule() [][]int {
	out := make([][]int, k7tcp.Slots)
	for h := 0; h < k7tcp.Slots; h++ {
		out[h] = []int{h, 0, 0, 0, 0, 0, 0, 0}
	}
	return out
}

func normalizeConfig(cfg *Config) {
	if strings.TrimSpace(cfg.Host) == "" {
		cfg.Host = k7tcp.DefaultHost
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = k7tcp.DefaultPort
	}
	if cfg.Device != "k7mini" && cfg.Device != "k7pro" {
		cfg.Device = "k7mini"
	}
}

func normalizeState(state *State) {
	if state.Mode != "manual" {
		state.Mode = "auto"
	}
	if len(state.Manual) == 0 {
		state.Manual = make([]int, k7tcp.Channels)
	}
	for len(state.Manual) < k7tcp.Channels {
		state.Manual = append(state.Manual, 0)
	}
	if len(state.Manual) > k7tcp.Channels {
		state.Manual = state.Manual[:k7tcp.Channels]
	}
	for i := range state.Manual {
		state.Manual[i] = clamp(state.Manual[i], 0, 100)
	}
	if len(state.Schedule) != k7tcp.Slots {
		state.Schedule = defaultSchedule()
	}
	for h := 0; h < k7tcp.Slots; h++ {
		if len(state.Schedule[h]) < 8 {
			state.Schedule[h] = []int{h, 0, 0, 0, 0, 0, 0, 0}
		}
		state.Schedule[h] = state.Schedule[h][:8]
		state.Schedule[h][0] = clamp(state.Schedule[h][0], 0, 23)
		state.Schedule[h][1] = clamp(state.Schedule[h][1], 0, 59)
		for i := 2; i < 8; i++ {
			state.Schedule[h][i] = clamp(state.Schedule[h][i], 0, 100)
		}
	}
	state.ScheduleShiftMinutes = 0
	if state.MasterBrightness <= 0 {
		state.MasterBrightness = 100
	}
	state.Siesta = normalizeSiesta(state.Siesta)
	state.Lunar = normalizeLunar(state.Lunar)
}

func defaultSiesta() SiestaConfig {
	return SiestaConfig{Start: "13:00", Duration: 90, DurationMins: 90, Intensity: 25}
}

func normalizeSiesta(cfg SiestaConfig) SiestaConfig {
	if cfg.Start == "" {
		cfg.Start = "13:00"
	}
	if cfg.Duration <= 0 {
		cfg.Duration = cfg.DurationMins
	}
	if cfg.Duration <= 0 {
		cfg.Duration = 90
	}
	cfg.Duration = clamp(cfg.Duration, 15, 480)
	cfg.DurationMins = cfg.Duration
	cfg.Intensity = clamp(cfg.Intensity, 1, 90)
	cfg.Active = cfg.Enabled
	return cfg
}

func defaultLunar() LunarConfig {
	return LunarConfig{
		Start:        "18:30",
		End:          "06:30",
		ClampStart:   "18:00",
		ClampEnd:     "08:00",
		MaxIntensity: 15,
		DayThreshold: 2,
		PhaseName:    "Fixed",
		Illumination: 100,
	}
}

func normalizeLunar(cfg LunarConfig) LunarConfig {
	if cfg.Start == "" {
		cfg.Start = "18:30"
	}
	if cfg.End == "" {
		cfg.End = "06:30"
	}
	if cfg.ClampStart == "" {
		cfg.ClampStart = "18:00"
	}
	if cfg.ClampEnd == "" {
		cfg.ClampEnd = "08:00"
	}
	if cfg.MaxIntensity <= 0 {
		cfg.MaxIntensity = 15
	}
	cfg.MaxIntensity = clamp(cfg.MaxIntensity, 0, 100)
	cfg.DayThreshold = clamp(cfg.DayThreshold, 0, 100)
	cfg.TrackMoonrise = false
	cfg.Active = cfg.Enabled
	cfg.Phase = 4
	cfg.Illumination = 100
	cfg.PhaseName = "Fixed"
	return cfg
}

func parseHHMM(value string, fallback int) int {
	var hour, minute int
	if _, err := fmt.Sscanf(value, "%d:%d", &hour, &minute); err != nil {
		return fallback
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return fallback
	}
	return hour*60 + minute
}

func minsInWindow(mins int, start int, end int) bool {
	if start == end {
		return true
	}
	if start < end {
		return mins >= start && mins < end
	}
	return mins >= start || mins < end
}

func clamp(v int, min int, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func cloneProfiles(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

func intsFromManual(in [k7tcp.Channels]int) []int {
	out := make([]int, k7tcp.Channels)
	for i := range out {
		out[i] = in[i]
	}
	return out
}

func intsFromChannels(in [k7tcp.Channels]uint8) []int {
	out := make([]int, k7tcp.Channels)
	for i := range out {
		out[i] = int(in[i])
	}
	return out
}

func intsFromSchedule(in [k7tcp.Slots][8]int) [][]int {
	out := make([][]int, k7tcp.Slots)
	for h := range out {
		out[h] = make([]int, 8)
		for i := range out[h] {
			out[h][i] = in[h][i]
		}
	}
	return out
}

func intsFromUintSchedule(in [k7tcp.Slots][8]uint8) [][]int {
	out := make([][]int, k7tcp.Slots)
	for h := range out {
		out[h] = make([]int, 8)
		for i := range out[h] {
			out[h][i] = int(in[h][i])
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeRawJSON(w http.ResponseWriter, status int, value []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(value)
}

func writeText(w http.ResponseWriter, status int, value string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(value))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": message})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://127.0.0.1:8787")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
