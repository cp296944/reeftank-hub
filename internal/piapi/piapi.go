// Package piapi hosts pi-bridge's always-on engine and the HTTP endpoints the
// upstream UI needs but pc-bridge never implemented (the "always-on" half of
// the capability ledger). It is mounted as a middleware in front of the
// httpapi handler, claiming its own routes and passing everything else through.
//
// hub-v0.5.0 scope: the engine tick loop driving the lamp from httpapi's stored
// schedule, plus /api/time, /api/output/status, /api/wifi/signal, /api/logs.
// Later tags add ramp / feed / maintenance / acclimation / seasonal.
package piapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cp296944/reeftank-hub/internal/engine"
	"github.com/cp296944/reeftank-hub/internal/httpapi"
	"github.com/cp296944/reeftank-hub/internal/lamp"
	"github.com/cp296944/reeftank-hub/internal/ringlog"
	"github.com/cp296944/reeftank-hub/internal/tally"
)

type Deps struct {
	Next    http.Handler
	API     *httpapi.Server
	Engine  *engine.Engine
	Lamp    *lamp.Lamp
	Log     *ringlog.Ring
	TZ      *time.Location
	TZName  string         // configured timezone name, for a mismatch warning
	WlanIf  string         // e.g. "wlan0"
	DataDir string         // fallback if FX is nil
	FX      *EffectsStore  // share the same store the Provider uses
	Tally   *tally.Counter // shared lamp-write counter for /api/output/status
}

type handler struct {
	Deps
	fx *EffectsStore
}

func Wrap(d Deps) http.Handler {
	if d.WlanIf == "" {
		d.WlanIf = "wlan0"
	}
	fx := d.FX
	if fx == nil {
		fx = NewEffectsStore(d.DataDir)
	}
	h := &handler{Deps: d, fx: fx}
	h.applyRampCadence() // restore the tick cadence for a persisted ramp state
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/time":
		h.time(w, r)
	case "/api/output/status":
		h.outputStatus(w, r)
	case "/api/wifi/signal":
		h.wifiSignal(w, r)
	case "/api/logs":
		h.logs(w, r)
	case "/api/warnings/status":
		h.warnings(w, r)

	case "/api/ramp/status":
		h.rampStatus(w, r)
	case "/api/ramp/start":
		h.rampStart(w, r)
	case "/api/ramp/stop":
		h.rampStop(w, r)
	case "/api/ramp/tick":
		h.rampTick(w, r)
	case "/api/ramp/config":
		h.rampConfig(w, r)

	case "/api/feed/status":
		h.feedStatus(w, r)
	case "/api/feed/start":
		h.feedStart(w, r)
	case "/api/feed/stop":
		h.feedStop(w, r)

	case "/api/maintenance/status":
		h.maintStatus(w, r)
	case "/api/maintenance/start":
		h.maintStart(w, r)
	case "/api/maintenance/stop":
		h.maintStop(w, r)

	case "/api/acclimation/config":
		h.acclimationConfig(w, r)
	case "/api/acclimation/status":
		h.acclimationStatus(w, r)
	case "/api/seasonal/config":
		h.seasonalConfig(w, r)
	case "/api/seasonal/status":
		h.seasonalStatus(w, r)

	case "/api/push":
		// Smooth ramp off → the engine won't be live-driving, so fold every
		// time-varying effect (acclimation, seasonal, tracked lunar, siesta,
		// master) into the 24 rows now, as a snapshot for today, before the
		// vendored handler sends the 0x1007 schedule the lamp will run itself.
		if r.Method == http.MethodPost {
			h.prebakePush(r)
		}
		h.Next.ServeHTTP(w, r)
		if r.Method == http.MethodPost && h.Engine != nil {
			h.Engine.Kick()
		}
	case "/api/master":
		h.Next.ServeHTTP(w, r)
		if r.Method == http.MethodPost && h.Engine != nil {
			h.Engine.Kick()
		}
	default:
		h.Next.ServeHTTP(w, r)
	}
}

// FX exposes the effects store so the Provider can read acclimation/seasonal.
func (h *handler) FX() *EffectsStore { return h.fx }

// prebakePush, when smooth ramp is off, folds every time-varying effect into the
// 24 rows of a POST /api/push body so the lamp can run the schedule itself. It
// reuses the engine's own Compute() hour-by-hour, so the baked schedule matches
// exactly what the live engine would have driven. Adds "prebaked":true so the
// vendored handler sends the rows verbatim instead of baking a second time.
//
// No-op (leaves the body untouched) when: ramp is on, the request isn't an auto
// push, the body doesn't parse, or the engine/deps are missing.
func (h *handler) prebakePush(r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	restore := func() { r.Body = io.NopCloser(bytes.NewReader(body)); r.ContentLength = int64(len(body)) }
	if err != nil {
		restore()
		return
	}
	restore()

	h.fx.mu.Lock()
	rampOn := h.fx.Ramp.Active
	h.fx.mu.Unlock()
	if rampOn || h.API == nil {
		return
	}

	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return
	}
	var rows [][]int
	if raw, ok := m["schedule"]; !ok || json.Unmarshal(raw, &rows) != nil || len(rows) != engine.Slots {
		return
	}
	var mode string
	if raw, ok := m["mode"]; ok {
		_ = json.Unmarshal(raw, &mode)
	}
	if mode == "manual" {
		return // manual push: no schedule effects to bake
	}
	var manual [engine.Channels]int
	if raw, ok := m["manual"]; ok {
		var mm []int
		if json.Unmarshal(raw, &mm) == nil {
			for i := 0; i < engine.Channels && i < len(mm); i++ {
				manual[i] = mm[i]
			}
		}
	}

	tz := h.TZ
	if tz == nil {
		tz = time.Local
	}
	cfg := NewProvider(h.API, h.fx, tz).EngineSnapshot().Config
	cfg.ScheduleShiftMinutes = 0 // piweb already rotated the rows

	base := scheduleFromRows(rows)
	day := time.Now().In(tz)
	baked := make([][]int, engine.Slots)
	for hh := 0; hh < engine.Slots; hh++ {
		at := time.Date(day.Year(), day.Month(), day.Day(), hh, 0, 0, 0, tz)
		out := cfg.Compute(base, manual, true, at)
		row := []int{hh, 0, 0, 0, 0, 0, 0, 0}
		for c := 0; c < engine.Channels; c++ {
			row[2+c] = out.Channels[c]
		}
		baked[hh] = row
	}
	if nb, err := json.Marshal(baked); err == nil {
		m["schedule"] = nb
	}
	m["prebaked"] = json.RawMessage("true")
	if nb, err := json.Marshal(m); err == nil {
		r.Body = io.NopCloser(bytes.NewReader(nb))
		r.ContentLength = int64(len(nb))
		slog.Debug("piapi: pre-baked push schedule (smooth ramp off)")
	}
}

// scheduleFromRows converts a 24×8 [][]int body schedule to engine.Schedule.
func scheduleFromRows(rows [][]int) engine.Schedule {
	var s engine.Schedule
	for i := 0; i < engine.Slots && i < len(rows); i++ {
		for j := 0; j < 8 && j < len(rows[i]); j++ {
			s[i][j] = rows[i][j]
		}
	}
	return s
}

// Provider implements engine.Provider by combining httpapi's stored working
// state with pi-bridge's own effect configs (acclimation, seasonal). Standalone
// so the engine can be constructed before the HTTP middleware.
type Provider struct {
	API *httpapi.Server
	FX  *EffectsStore
	TZ  *time.Location
}

func NewProvider(api *httpapi.Server, fx *EffectsStore, tz *time.Location) *Provider {
	if tz == nil {
		tz = time.Local
	}
	return &Provider{API: api, FX: fx, TZ: tz}
}

func (p *Provider) EngineSnapshot() engine.Snapshot {
	st := p.API.StateSnapshot()

	var base engine.Schedule
	for i := 0; i < engine.Slots && i < len(st.Schedule); i++ {
		for j := 0; j < 8 && j < len(st.Schedule[i]); j++ {
			base[i][j] = st.Schedule[i][j]
		}
	}
	var manual [engine.Channels]int
	for i := 0; i < engine.Channels && i < len(st.Manual); i++ {
		manual[i] = st.Manual[i]
	}

	cfg := engine.Config{
		Device:               p.API.Device(),
		MasterBrightness:     nz(st.MasterBrightness, 100),
		ScheduleShiftMinutes: st.ScheduleShiftMinutes,
	}
	cfg.Siesta.Enabled = st.Siesta.Enabled
	cfg.Siesta.Start = orDef(st.Siesta.Start, "13:00")
	cfg.Siesta.DurationMins = nz(st.Siesta.DurationMins, st.Siesta.Duration)
	cfg.Siesta.Intensity = st.Siesta.Intensity
	cfg.Lunar.Enabled = st.Lunar.Enabled
	cfg.Lunar.Start = orDef(st.Lunar.Start, "18:30")
	cfg.Lunar.End = orDef(st.Lunar.End, "06:30")
	cfg.Lunar.ClampStart = orDef(st.Lunar.ClampStart, "18:00")
	cfg.Lunar.ClampEnd = orDef(st.Lunar.ClampEnd, "08:00")
	cfg.Lunar.MaxIntensity = nz(st.Lunar.MaxIntensity, 15)
	cfg.Lunar.DayThreshold = st.Lunar.DayThreshold
	cfg.Lunar.TrackMoonrise = st.Lunar.TrackMoonrise

	if p.FX != nil {
		p.FX.mu.Lock()
		a, se := p.FX.Acclimation, p.FX.Seasonal
		p.FX.mu.Unlock()
		cfg.Acclimation.Enabled = a.Enabled
		cfg.Acclimation.StartPercent = a.StartPercent
		cfg.Acclimation.DurationDays = a.DurationDays
		if t, err := time.Parse(time.RFC3339, a.StartISO); err == nil {
			cfg.Acclimation.StartEpoch = t.Unix()
		}
		cfg.Seasonal.Enabled = se.Enabled
		cfg.Seasonal.MaxShiftMinutes = se.MaxShiftMinutes
	}

	return engine.Snapshot{
		Base:     base,
		Manual:   manual,
		AutoMode: st.Mode != "manual",
		Config:   cfg,
	}
}

// ---- endpoints ----------------------------------------------------------

func (h *handler) time(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// The Pi's clock is owned by NTP; accept and ignore the UI's set.
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "clock_set": engine.ClockSane()})
		return
	}
	now := time.Now().In(h.TZ)
	writeJSON(w, http.StatusOK, map[string]any{
		"clock_set": engine.ClockSane(),
		"now":       now.Format(time.RFC3339),
		"epoch":     now.Unix(),
		"tz":        h.TZ.String(),
	})
}

type writesToday struct {
	Auto   int    `json:"auto"`
	Manual int    `json:"manual"`
	Date   string `json:"date"`
}

type outputStatusResp struct {
	engine.OutputStatus
	Live        bool        `json:"live"`         // is the engine the live driver? (smooth ramp on)
	WritesToday writesToday `json:"writes_today"` // lamp writes since local midnight
}

func (h *handler) outputStatus(w http.ResponseWriter, r *http.Request) {
	var auto, manual int
	var day string
	if h.Tally != nil {
		auto, manual, day = h.Tally.Today()
	}
	if day == "" {
		day = time.Now().Format("2006-01-02")
	}
	writeJSON(w, http.StatusOK, outputStatusResp{
		OutputStatus: h.Engine.Status(),
		Live:         h.Engine.Live(),
		WritesToday:  writesToday{Auto: auto, Manual: manual, Date: day},
	})
}

// warnings backs the shared UI's "Checks" card: a live list of things that
// would keep the tank from being lit the way the schedule says. Phase 4 may
// grow this; the shape is {items:[{level,message}], count}.
func (h *handler) warnings(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	var items []item
	add := func(level, msg string) { items = append(items, item{level, msg}) }

	if !engine.ClockSane() {
		add("error", "控制器時鐘尚未設定 — 排程不會執行 (controller clock not set)")
	}
	if h.TZName != "" && time.Now().Location().String() != h.TZName {
		add("warn", fmt.Sprintf("時區不一致:設定 %s,系統實際 %s — 燈可能跑錯時段 (run `sudo timedatectl set-timezone %s` and restart)",
			h.TZName, time.Now().Location().String(), h.TZName))
	}

	if h.Lamp != nil {
		lh := h.Lamp.Health()
		if lh.LastOKAt.IsZero() {
			add("warn", "尚未成功連上燈具 (no successful contact with the lamp yet)")
		} else if !lh.OK {
			msg := "最近一次連燈失敗 (last lamp contact failed)"
			if lh.LastErr != "" {
				msg += ": " + lh.LastErr
			}
			add("warn", msg)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if b, err := exec.CommandContext(ctx, "iw", "dev", h.WlanIf, "link").CombinedOutput(); err == nil {
		if m := reSignal.FindStringSubmatch(string(b)); m != nil {
			if n, e := strconv.Atoi(m[1]); e == nil && rssiToQuality(n) < 35 {
				add("warn", fmt.Sprintf("Wi-Fi 到燈具訊號偏弱 (%d%%, %d dBm) — 把 Pi 移近水槽可改善", rssiToQuality(n), n))
			}
		}
	}

	if h.API != nil {
		st := h.API.StateSnapshot()
		if st.Mode != "manual" && scheduleAllZero(st.Schedule) {
			add("warn", "目前排程整天都是 0 — 燈會全暗。載入預設或設定檔後按 ⬆ 推送")
		}
	}

	if h.Engine != nil {
		if s := h.Engine.Status(); s.SentMs > 0 && !s.LastWriteOK {
			add("warn", "最近一次送給燈具的指令失敗 (last write to the lamp failed)")
		}
	}

	if items == nil {
		items = []item{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func scheduleAllZero(rows [][]int) bool {
	for _, row := range rows {
		for i := 2; i < len(row); i++ {
			if row[i] != 0 {
				return false
			}
		}
	}
	return true
}

func (h *handler) logs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": h.Log.Entries(limit)})
}

var reSignal = regexp.MustCompile(`signal:\s*(-?\d+)\s*dBm`)
var reBitrate = regexp.MustCompile(`tx bitrate:\s*([\d.]+)`)

func (h *handler) wifiSignal(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"interface": h.WlanIf, "lamp": h.Lamp.Health()}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if b, err := exec.CommandContext(ctx, "iw", "dev", h.WlanIf, "link").CombinedOutput(); err == nil {
		s := string(b)
		out["associated"] = !strings.Contains(s, "Not connected")
		if m := reSignal.FindStringSubmatch(s); m != nil {
			if n, e := strconv.Atoi(m[1]); e == nil {
				out["rssi_dbm"] = n
				out["quality"] = rssiToQuality(n)
			}
		}
		if m := reBitrate.FindStringSubmatch(s); m != nil {
			out["tx_bitrate_mbps"], _ = strconv.ParseFloat(m[1], 64)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func rssiToQuality(dbm int) int {
	// -50 or better = 100%, -100 = 0%
	q := 2 * (dbm + 100)
	if q < 0 {
		q = 0
	}
	if q > 100 {
		q = 100
	}
	return q
}

// ---- small helpers ----------------------------------------------------------

func nz(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
func orDef(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errObj(msg string) map[string]any { return map[string]any{"ok": false, "error": msg} }
