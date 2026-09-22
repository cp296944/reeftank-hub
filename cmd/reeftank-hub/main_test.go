package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cp296944/reeftank-hub/internal/config"
	"github.com/cp296944/reeftank-hub/internal/httpapi"
	"github.com/cp296944/reeftank-hub/internal/lamp"
	"github.com/cp296944/reeftank-hub/internal/updater"
)

// A POST /api/update/apply without {"confirm":true} (stray click, replay,
// script) must be rejected — applying restarts the service.
func TestUpdateApplyNeedsConfirmation(t *testing.T) {
	up := updater.New(updater.Options{Repo: "cp296944/x", CurrentTag: "dev"})
	var auto atomic.Bool
	h := routes(config.Config{}, "", up, &auto, http.NotFoundHandler())

	for _, body := range []string{"", "{}", `{"confirm":false}`, `{"tag":"hub-v9.9.9"}`} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/update/apply", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: got %d, want 400", body, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "confirm") {
			t.Errorf("body %q: response %q missing the confirmation hint", body, rr.Body.String())
		}
	}
}

func newSetupTestRoutes(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	cfg := config.Defaults()
	cfg.DataDir = dir
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}
	api, err := httpapi.New(httpapi.Options{ConfigPath: filepath.Join(dir, "store.json")})
	if err != nil {
		t.Fatal(err)
	}
	var auto atomic.Bool
	su := &setupAPI{
		cfg: cfg, cfgPath: cfgPath, dataDir: dir,
		autoUpdate: &auto, api: api, lamp: lamp.New("127.0.0.1", 1), started: time.Now(),
	}
	up := updater.New(updater.Options{Repo: "cp296944/x", CurrentTag: "dev"})
	return routes(cfg, cfgPath, up, &auto, http.NotFoundHandler(), su.register), cfgPath
}

func TestSetupGetAndPost(t *testing.T) {
	h, cfgPath := newSetupTestRoutes(t)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/setup", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"timezone"`) {
		t.Fatalf("GET /api/setup = %d %s", rr.Code, rr.Body.String())
	}

	// bad timezone rejected
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/setup",
		strings.NewReader(`{"location":{"timezone":"Mars/Olympus"}}`)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad tz: got %d, want 400", rr.Code)
	}

	// valid channel change persists + asks for a restart
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/setup",
		strings.NewReader(`{"update":{"channel":"prerelease"}}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("channel change: %d %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		OK              bool
		RestartRequired []string `json:"restart_required"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.OK || len(resp.RestartRequired) == 0 {
		t.Errorf("want ok + restart_required, got %+v", resp)
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), `"prerelease"`) {
		t.Errorf("channel not persisted to config.json: %s", b)
	}
}

func TestSetupFactoryResetNeedsConfirm(t *testing.T) {
	h, _ := newSetupTestRoutes(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/setup/factory-reset", strings.NewReader("{}")))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "confirm") {
		t.Errorf("factory-reset without confirm: %d %s", rr.Code, rr.Body.String())
	}
}

// After resolveTimezone + time.Local pin, the clock k7tcp sends to the lamp
// (time.Now(), i.e. time.Local) must be the same wall time the engine schedules
// against (time.Now().In(tz)).
func TestTimezonePinnedForLampClock(t *testing.T) {
	orig := time.Local
	t.Cleanup(func() { time.Local = orig })

	loc, ok := resolveTimezone("America/New_York")
	if !ok || loc.String() != "America/New_York" {
		t.Fatalf("resolveTimezone: ok=%v loc=%v", ok, loc)
	}
	time.Local = loc

	if time.Now().Hour() != time.Now().In(loc).Hour() {
		t.Errorf("time.Now() hour %d != engine tz hour %d — lamp clock would disagree",
			time.Now().Hour(), time.Now().In(loc).Hour())
	}

	if _, ok := resolveTimezone("Mars/Olympus"); ok {
		t.Error("resolveTimezone accepted a bogus zone")
	}
	if _, ok := resolveTimezone(""); ok {
		t.Error(`resolveTimezone("") should report not-ok`)
	}
}

func TestScheduleHelpers(t *testing.T) {
	zero := make([][]int, 24)
	for i := range zero {
		zero[i] = []int{i, 0, 0, 0, 0, 0, 0, 0}
	}
	if !scheduleAllZero(zero) {
		t.Error("all-zero schedule not detected")
	}
	zero[12][4] = 30
	if scheduleAllZero(zero) {
		t.Error("non-zero schedule reported as all-zero")
	}

	var lamp [24][8]int
	for h := 0; h < 24; h++ {
		lamp[h] = [8]int{h, 0, 0, 0, 30, 0, 0, 0} // matches zero after the edit above (col 4 = 30 at h12? no)
	}
	if schedulesEqual(zero, lamp) {
		t.Error("different schedules reported equal")
	}
	// make them match on channels 2..7
	for h := 0; h < 24; h++ {
		for c := 2; c < 8; c++ {
			lamp[h][c] = zero[h][c]
		}
	}
	if !schedulesEqual(zero, lamp) {
		t.Error("matching schedules reported unequal")
	}
}
