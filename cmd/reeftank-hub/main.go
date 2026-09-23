// Command reeftank-hub is the 24/7 K7 Pro controller daemon for a Raspberry Pi.
//
// It bridges the lamp's Wi-Fi AP (reached on wlan0) to the home LAN (eth0):
// serves the shared web UI + REST API, proxies the raw 8266 protocol, runs the
// always-on lighting engine, and updates itself over the air from GitHub
// releases.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cp296944/reeftank-hub/internal/bootstrap"
	"github.com/cp296944/reeftank-hub/internal/config"
	"github.com/cp296944/reeftank-hub/internal/dosing"
	"github.com/cp296944/reeftank-hub/internal/engine"
	"github.com/cp296944/reeftank-hub/internal/equipment"
	"github.com/cp296944/reeftank-hub/internal/homeassistant"
	"github.com/cp296944/reeftank-hub/internal/httpapi"
	"github.com/cp296944/reeftank-hub/internal/hubweb"
	"github.com/cp296944/reeftank-hub/internal/lamp"
	"github.com/cp296944/reeftank-hub/internal/piapi"
	"github.com/cp296944/reeftank-hub/internal/piweb"
	"github.com/cp296944/reeftank-hub/internal/profiles"
	"github.com/cp296944/reeftank-hub/internal/proxy"
	"github.com/cp296944/reeftank-hub/internal/ringlog"
	"github.com/cp296944/reeftank-hub/internal/storage"
	"github.com/cp296944/reeftank-hub/internal/tally"
	"github.com/cp296944/reeftank-hub/internal/temperature"
	"github.com/cp296944/reeftank-hub/internal/updater"
	"github.com/cp296944/reeftank-hub/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "reeftank-hub:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "bootstrap" {
		return runBootstrap(args[1:])
	}
	if len(args) > 0 && args[0] == "maintenance" {
		return runMaintenance(args[1:])
	}
	cfg, err := config.Load(args)
	if errors.Is(err, config.ErrVersionRequested) {
		fmt.Println(version.String())
		return nil
	}
	if err != nil {
		return err
	}

	rlog := ringlog.New(500)
	logger := slog.New(ringlog.NewHandler(rlog, newLogger(cfg.LogLevel).Handler()))
	slog.SetDefault(logger)
	logger.Info("starting", "version", version.String(), "listen", cfg.Listen,
		"lamp", fmt.Sprintf("%s:%d", cfg.LampHost, cfg.LampPort),
		"install_root", cfg.InstallRoot, "data_dir", cfg.DataDir)

	// One clock for the whole process. The engine computes the schedule in
	// `tz`, but k7tcp.SyncTimeLocal / PushSchedule send time.Now() (== time.Local)
	// to the lamp — so if the Pi's OS timezone differs from config (a stock
	// headless Pi OS Lite is UTC) the lamp runs the photoperiod at the wrong
	// hour. Pin time.Local to the configured zone so every path agrees.
	tz, tzOK := resolveTimezone(cfg.Timezone)
	if cfg.Timezone != "" && !tzOK {
		logger.Warn("bad timezone in config, using system zone", "tz", cfg.Timezone)
	}
	time.Local = tz
	if tzOK {
		_ = os.Setenv("TZ", cfg.Timezone)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	equipmentStore, err := equipment.Open(filepath.Join(cfg.DataDir, "equipment.json"))
	if err != nil {
		return fmt.Errorf("equipment map: %w", err)
	}
	hubDB, err := storage.Open(filepath.Join(cfg.DataDir, "reeftank-hub.db"))
	if err != nil {
		return fmt.Errorf("open Hub database: %w", err)
	}
	defer hubDB.Close()
	if err := hubDB.SyncEquipment(context.Background(), equipmentStore.Snapshot()); err != nil {
		return fmt.Errorf("store equipment map: %w", err)
	}
	equipmentStore.SetOnChange(func(snap equipment.Snapshot) {
		if err := hubDB.SyncEquipment(context.Background(), snap); err != nil {
			slog.Warn("store equipment map", "err", err)
		}
	})
	haClient := homeassistant.New(cfg.HAURL, cfg.HAToken)
	haSync := homeassistant.NewSyncer(haClient, hubDB, homeassistant.TrackedEntities(equipmentStore.Snapshot()))
	haAPI := &homeassistant.API{Client: haClient, Equipment: equipmentStore, Sync: haSync}
	temperaturePoller := temperature.New(cfg.XiaoyuURL, hubDB)
	if err := hubDB.SeedWaterRecords(context.Background(), storage.BuiltinWaterSeed()); err != nil {
		return fmt.Errorf("import built-in water records: %w", err)
	}
	dosingStore, err := dosing.Open(filepath.Join(cfg.DataDir, "dosing.json"))
	if err != nil {
		return fmt.Errorf("open dosing state: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go haSync.Run(ctx)
	go temperaturePoller.Run(ctx)
	go func() {
		backfillCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
		defer cancel()
		if err := haSync.Backfill(backfillCtx, 3650); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("HA history backfill failed", "err", err)
		}
	}()
	backupDB := func() {
		dest := filepath.Join(cfg.DataDir, "backups", "automatic-"+time.Now().UTC().Format("20060102")+".db")
		if err := hubDB.Backup(dest); err != nil {
			slog.Warn("database backup failed", "err", err)
		}
	}
	backupDB()
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				backupDB()
			}
		}
	}()

	var healthy atomic.Bool
	healthy.Store(true)

	up := updater.New(updater.Options{
		Repo:        cfg.UpdateRepo,
		Channel:     cfg.UpdateChannel,
		InstallRoot: cfg.InstallRoot,
		CurrentTag:  version.Version,
		PreApply: func(_ context.Context, targetTag string) error {
			return hubDB.Backup(filepath.Join(cfg.DataDir, "backups", "pre-ota-"+targetTag+"-"+time.Now().UTC().Format("20060102T150405Z")+".db"))
		},
	})
	up.ConfirmAfterStart(ctx, version.Version, 45*time.Second, func() bool { return healthy.Load() })
	cfgPath := os.Getenv("REEFTANK_CONFIG")
	if cfgPath == "" {
		cfgPath = filepath.Join(cfg.DataDir, "config.json")
	}
	var autoUpdate atomic.Bool
	autoUpdate.Store(cfg.AutoUpdate)
	if d := parseInterval(cfg.UpdateInterval); d > 0 {
		go updateLoop(ctx, up, d, &autoUpdate)
	}

	// The always-on engine drives the lamp and every ESP32 feature is live,
	// including the setup portal (pi-bridge's own settings page — see setup.go).
	caps := httpapi.DefaultCapabilities()
	for k := range caps {
		caps[k] = true
	}

	lampConn := lamp.New(cfg.LampHost, cfg.LampPort)
	lampConn.SetDemandOnly(true)
	fx := piapi.NewEffectsStore(cfg.DataDir)

	// Shared lamp-write counter (今日上傳次數). Persists across restart / OTA so
	// a mid-day update doesn't zero it.
	writeTally := tally.Load(filepath.Join(cfg.DataDir, "writes.json"), tz)
	go writeTally.Run(ctx, 5*time.Minute)
	defer func() { _ = writeTally.Save() }()

	api, err := httpapi.New(httpapi.Options{
		ConfigPath:   filepath.Join(cfg.DataDir, "store.json"),
		Version:      version.Version,
		Timeout:      5 * time.Second,
		Device:       "k7pro",
		LampHost:     cfg.LampHost,
		LampPort:     cfg.LampPort,
		LampGate:     lampConn.Gate(),
		Capabilities: caps,
		Tally:        writeTally,
	})
	if err != nil {
		return fmt.Errorf("http api: %w", err)
	}

	eng := engine.New(piapi.NewProvider(api, fx, tz), lampConn, tz, 10*time.Minute)
	// When the engine stops live-driving (smooth ramp off) or a timed override
	// ends, hand the lamp back its own 0x1007 schedule.
	eng.SetRepushFn(api.Republish)
	eng.SetTally(writeTally)
	// After the daily time-sync (and after a reconnect), check the lamp still
	// holds the schedule we last pushed and re-push if it drifted — but never
	// push a blank schedule over a real one.
	eng.SetDriftCheck(func() bool {
		st := api.StateSnapshot()
		if st.LastPushedAt == "" || scheduleAllZero(st.Schedule) {
			return false
		}
		lampState, err := lampConn.ReadAll()
		if err != nil {
			slog.Warn("drift check: lamp read failed", "err", err)
			return false
		}
		if schedulesEqual(st.Schedule, lampState.Schedule) {
			return false
		}
		slog.Info("drift check: lamp schedule differs from last push — re-pushing")
		if err := api.Republish(); err != nil {
			slog.Warn("drift check: re-push failed", "err", err)
			return false
		}
		return true
	})
	// A lamp power-cycle drops the link; when it comes back, re-sync the clock
	// (+ drift check) immediately instead of waiting for the daily pass.
	lampConn.SetOnReconnect(eng.MaintNow)
	// Persisted smooth-ramp state decides whether the engine drives live; piapi
	// re-asserts this in Wrap(), this just avoids a momentary wrong mode on boot.
	eng.SetLive(fx.Ramp.Active)
	go eng.Run(ctx)

	// Per-lamp profile store (isolated from OTA; keyed by lamp MAC/name).
	profStore := profiles.New(cfg.DataDir, cfg.LampHost)
	profStore.NameHint = api.LampName
	profStore.Migrate(profStore.LampID(ctx), api.LegacyProfiles())

	// pi-bridge UX layer over the unmodified upstream UI.
	uiHandler := piweb.Wrap(piweb.Deps{
		Next: piapi.Wrap(piapi.Deps{
			Next:    api.Routes(),
			API:     api,
			Engine:  eng,
			Lamp:    lampConn,
			Log:     rlog,
			TZ:      tz,
			TZName:  cfg.Timezone,
			DataDir: cfg.DataDir,
			FX:      fx,
			Tally:   writeTally,
		}),
		Profiles:   profStore,
		Version:    version.Version,
		UpdateRepo: cfg.UpdateRepo,
	})
	hubHandler := hubweb.New(hubweb.Options{
		K7: uiHandler, Version: version.Version, InstallRoot: cfg.InstallRoot,
	})

	started := time.Now()
	setup := &setupAPI{
		cfg: cfg, cfgPath: cfgPath, dataDir: cfg.DataDir,
		autoUpdate: &autoUpdate, api: api, lamp: lampConn, started: started,
	}
	diag := &diagAPI{
		dataDir: cfg.DataDir, started: started,
		eng: eng, lamp: lampConn, tally: writeTally, version: version.Version,
	}
	go diag.run(ctx, time.Hour)

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: routes(cfg, cfgPath, up, &autoUpdate, hubHandler, setup.register, diag.register, equipmentStore.Register, haAPI.Register, temperaturePoller.Register, dosingStore.Register, hubDB.Register, func(mux *http.ServeMux) {
			mux.HandleFunc("POST /api/hub/k7/session", func(w http.ResponseWriter, r *http.Request) {
				lampConn.Touch(2 * time.Minute)
				_, err := lampConn.ReadAll()
				active, until := lampConn.DemandActive()
				status := http.StatusOK
				errMessage := ""
				if err != nil {
					status = http.StatusBadGateway
					errMessage = err.Error()
				}
				writeJSON(w, status, map[string]any{"active": active, "until": until, "connected": err == nil, "health": lampConn.Health(), "error": errMessage})
			})
			mux.HandleFunc("GET /api/hub/k7/session", func(w http.ResponseWriter, r *http.Request) {
				active, until := lampConn.DemandActive()
				h := lampConn.Health()
				writeJSON(w, http.StatusOK, map[string]any{"active": active, "until": until, "connected": active && h.OK, "health": h})
			})
			mux.HandleFunc("DELETE /api/hub/k7/session", func(w http.ResponseWriter, r *http.Request) {
				lampConn.Sleep()
				writeJSON(w, http.StatusOK, map[string]any{"active": false, "connected": false})
			})
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	if cfg.Proxy != "" {
		px := &proxy.Proxy{
			Listen:   cfg.Proxy,
			LampAddr: fmt.Sprintf("%s:%d", cfg.LampHost, cfg.LampPort),
			Gate:     lampConn.Gate(),
		}
		go func() {
			if err := px.Run(ctx); err != nil {
				slog.Error("proxy stopped", "err", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

func runMaintenance(args []string) error {
	if len(args) == 0 || args[0] != "apply" {
		return fmt.Errorf("maintenance: expected apply --request <fixed-path>")
	}
	requestPath := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--request" && i+1 < len(args) {
			requestPath = args[i+1]
			i++
			continue
		}
		return fmt.Errorf("maintenance: unknown argument %q", args[i])
	}
	result, err := bootstrap.ApplyMaintenance(bootstrap.MaintenanceOptions{
		InstallRoot: config.Defaults().InstallRoot,
		RequestPath: requestPath,
		Version:     version.Version,
	})
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(b))
	return nil
}

func runBootstrap(args []string) error {
	if len(args) == 0 || args[0] == "status" {
		b, _ := json.MarshalIndent(bootstrap.Inspect(config.Defaults().InstallRoot), "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if args[0] != "install" {
		return fmt.Errorf("bootstrap: expected status or install")
	}
	result, err := bootstrap.Install(bootstrap.InstallOptions{
		InstallRoot: config.Defaults().InstallRoot,
		Version:     version.Version,
	})
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(b))
	return nil
}

// schedulesEqual compares the 6 channel columns (2..7) of the stored 24-row
// schedule against the lamp's decoded one; hour/minute columns are ignored.
func schedulesEqual(want [][]int, got [24][8]int) bool {
	if len(want) != 24 {
		return false
	}
	for h := 0; h < 24; h++ {
		if len(want[h]) < 8 {
			return false
		}
		for c := 2; c < 8; c++ {
			if want[h][c] != got[h][c] {
				return false
			}
		}
	}
	return true
}

func scheduleAllZero(s [][]int) bool {
	for _, row := range s {
		for c := 2; c < len(row); c++ {
			if row[c] != 0 {
				return false
			}
		}
	}
	return true
}

// resolveTimezone returns the configured zone (and true) or time.Local (and
// false) when the name is empty or unknown.
func resolveTimezone(name string) (*time.Location, bool) {
	if name == "" {
		return time.Local, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local, false
	}
	return loc, true
}

func routes(cfg config.Config, cfgPath string, up *updater.Updater, autoUpdate *atomic.Bool, ui http.Handler, extra ...func(*http.ServeMux)) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, "ok")
	})

	for _, reg := range extra {
		if reg != nil {
			reg(mux)
		}
	}

	mux.HandleFunc("GET /api/update/status", func(w http.ResponseWriter, r *http.Request) {
		rel, err := up.Check(r.Context())
		resp := map[string]any{
			"current": version.Version, "channel": cfg.UpdateChannel, "repo": cfg.UpdateRepo,
			"auto_update": autoUpdate.Load(),
		}
		if err != nil {
			resp["error"] = err.Error()
			writeJSON(w, http.StatusBadGateway, resp)
			return
		}
		if rel == nil {
			resp["up_to_date"] = true
		} else {
			resp["up_to_date"] = false
			resp["available"] = rel.Tag
			resp["notes"] = rel.Notes
		}
		writeJSON(w, http.StatusOK, resp)
	})

	// Toggle automatic OTA. Persists to config.json so it survives restarts.
	mux.HandleFunc("POST /api/update/config", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AutoUpdate *bool `json:"auto_update"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.AutoUpdate == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "auto_update (bool) required"})
			return
		}
		autoUpdate.Store(*in.AutoUpdate)
		c := cfg
		c.AutoUpdate = *in.AutoUpdate
		if err := c.Save(cfgPath); err != nil {
			slog.Warn("save config", "err", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"auto_update": *in.AutoUpdate})
	})

	// Release history for the version-chip changelog popup. Fetched server-side
	// (no browser CORS / rate-limit worries), cached ~10 min.
	var histMu sync.Mutex
	var histAt time.Time
	var histCache []byte
	mux.HandleFunc("GET /api/update/history", func(w http.ResponseWriter, r *http.Request) {
		histMu.Lock()
		fresh := time.Since(histAt) < 10*time.Minute && histCache != nil
		body := histCache
		histMu.Unlock()
		if !fresh {
			req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet,
				"https://api.github.com/repos/"+cfg.UpdateRepo+"/releases?per_page=40", nil)
			req.Header.Set("Accept", "application/vnd.github+json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				var raw []struct {
					TagName     string `json:"tag_name"`
					Name        string `json:"name"`
					Body        string `json:"body"`
					PublishedAt string `json:"published_at"`
					Prerelease  bool   `json:"prerelease"`
					HTMLURL     string `json:"html_url"`
				}
				if json.NewDecoder(resp.Body).Decode(&raw) == nil {
					out := make([]map[string]any, 0, len(raw))
					for _, x := range raw {
						if !strings.HasPrefix(x.TagName, "hub-v") {
							continue
						}
						out = append(out, map[string]any{
							"tag": x.TagName, "name": x.Name, "notes": x.Body,
							"published_at": x.PublishedAt, "prerelease": x.Prerelease, "url": x.HTMLURL,
						})
					}
					body, _ = json.Marshal(map[string]any{"current": version.Version, "releases": out})
					histMu.Lock()
					histCache, histAt = body, time.Now()
					histMu.Unlock()
				}
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
		}
		if body == nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "history unavailable"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	mux.HandleFunc("POST /api/update/apply", func(w http.ResponseWriter, r *http.Request) {
		// Applying restarts the service and swaps the running binary, so it must
		// be a deliberate act: require {"confirm": true}. A bare POST (stray
		// click, replayed request, misbehaving script) is rejected.
		//
		// "tag" is an advisory hint of what the caller saw as available. If a
		// newer release has landed since, we apply that newer one (the intent is
		// "update"); the response's "applying" says what actually got installed
		// so the client polls for the right version.
		var in struct {
			Confirm bool   `json:"confirm"`
			Tag     string `json:"tag"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !in.Confirm {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": `update apply needs {"confirm": true} — use the "Update now" button`,
			})
			return
		}

		rel, err := up.Check(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		if rel == nil {
			writeJSON(w, http.StatusOK, map[string]any{"applied": false, "reason": "up to date"})
			return
		}
		slog.Info("update apply requested via API", "tag", rel.Tag, "requested", in.Tag, "remote", r.RemoteAddr)
		// Respond before the restart cuts the connection.
		writeJSON(w, http.StatusAccepted, map[string]any{"applying": rel.Tag, "requested": in.Tag})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			if err := up.Apply(context.Background(), rel); err != nil {
				slog.Error("update apply failed", "err", err)
			}
		}()
	})

	// Everything else — the shared UI (with the pi-bridge overlay), /api/version,
	// /api/capabilities, the pc-bridge endpoint set, /pi/*, and the per-lamp
	// profile store — is the piweb-wrapped httpapi handler. Go 1.22 ServeMux
	// gives the specific patterns above precedence over this "/".
	mux.Handle("/", ui)

	return logRequests(mux)
}

func updateLoop(ctx context.Context, up *updater.Updater, every time.Duration, auto *atomic.Bool) {
	// small initial delay so a broken release doesn't insta-loop on boot
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if rel, err := up.Check(ctx); err != nil {
			slog.Warn("update check failed", "err", err)
		} else if rel != nil {
			if auto.Load() {
				slog.Info("update available, auto-applying", "tag", rel.Tag)
				if err := up.Apply(ctx, rel); err != nil {
					slog.Error("update apply failed", "err", err)
				}
			} else {
				slog.Info("update available (auto_update off — apply from the UI)", "tag", rel.Tag)
			}
		}
		timer.Reset(every)
	}
}

func parseInterval(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute {
		return time.Hour
	}
	return d
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Debug("http", "method", r.Method, "path", r.URL.Path,
			"remote", r.RemoteAddr, "dur", time.Since(start).String())
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode json", "err", err)
	}
}
