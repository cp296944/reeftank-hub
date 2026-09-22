package piapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cp296944/reeftank-hub/internal/httpapi"
	"github.com/cp296944/reeftank-hub/internal/lamp"
)

func newTestServer(t *testing.T) *httpapi.Server {
	t.Helper()
	s, err := httpapi.New(httpapi.Options{ConfigPath: filepath.Join(t.TempDir(), "store.json")})
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	return s
}

// With smooth ramp off, prebakePush folds acclimation into the 24 rows so the
// lamp's own 0x1007 schedule reflects today's snapshot.
func TestPrebakePushBakesAcclimation(t *testing.T) {
	fx := NewEffectsStore(t.TempDir())
	fx.Ramp.Active = false
	fx.Acclimation.Enabled = true
	fx.Acclimation.StartPercent = 50
	fx.Acclimation.DurationDays = 10
	fx.Acclimation.StartISO = time.Now().Format(time.RFC3339) // day 0 -> 50%

	h := &handler{Deps: Deps{API: newTestServer(t), TZ: time.UTC}, fx: fx}

	rows := make([][]int, 24)
	for i := range rows {
		rows[i] = []int{i, 0, 80, 80, 80, 80, 80, 80}
	}
	body, _ := json.Marshal(map[string]any{
		"manual": []int{0, 0, 0, 0, 0, 0}, "schedule": rows, "mode": "auto",
	})
	r := httptest.NewRequest(http.MethodPost, "/api/push", bytes.NewReader(body))
	h.prebakePush(r)

	got, _ := io.ReadAll(r.Body)
	var m map[string]json.RawMessage
	if json.Unmarshal(got, &m) != nil {
		t.Fatalf("rewritten body not JSON: %s", got)
	}
	if _, ok := m["prebaked"]; !ok {
		t.Error(`"prebaked" flag not added`)
	}
	var out [][]int
	if json.Unmarshal(m["schedule"], &out) != nil || len(out) != 24 {
		t.Fatalf("schedule not 24 rows: %s", m["schedule"])
	}
	if out[12][2] != 40 {
		t.Errorf("hour 12 ch0 = %d, want 40 (80 scaled by 50%% acclimation)", out[12][2])
	}
}

func TestPrebakePushNoOpWhenRampOn(t *testing.T) {
	fx := NewEffectsStore(t.TempDir())
	fx.Ramp.Active = true
	h := &handler{Deps: Deps{API: newTestServer(t), TZ: time.UTC}, fx: fx}

	body := []byte(`{"schedule":[[0,0,80,0,0,0,0,0]],"mode":"auto"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/push", bytes.NewReader(body))
	h.prebakePush(r)

	got, _ := io.ReadAll(r.Body)
	if string(got) != string(body) {
		t.Errorf("smooth ramp on should pass the body through unchanged, got %s", got)
	}
}

func TestPrebakePushNoOpForManualMode(t *testing.T) {
	fx := NewEffectsStore(t.TempDir())
	fx.Ramp.Active = false
	h := &handler{Deps: Deps{API: newTestServer(t), TZ: time.UTC}, fx: fx}

	rows := make([][]int, 24)
	for i := range rows {
		rows[i] = []int{i, 0, 10, 0, 0, 0, 0, 0}
	}
	body, _ := json.Marshal(map[string]any{"schedule": rows, "mode": "manual"})
	r := httptest.NewRequest(http.MethodPost, "/api/push", bytes.NewReader(body))
	h.prebakePush(r)

	got, _ := io.ReadAll(r.Body)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(got, &m)
	if _, ok := m["prebaked"]; ok {
		t.Error("manual-mode push must not be pre-baked")
	}
}

func TestWarningsFlagsDarkScheduleAndNoLamp(t *testing.T) {
	h := &handler{Deps: Deps{
		API:  newTestServer(t),         // fresh store → all-zero schedule, mode "auto"
		Lamp: lamp.New("127.0.0.1", 1), // never contacted
		TZ:   time.UTC, WlanIf: "nonexistent0",
	}, fx: NewEffectsStore(t.TempDir())}

	rr := httptest.NewRecorder()
	h.warnings(rr, httptest.NewRequest(http.MethodGet, "/api/warnings/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var out struct {
		Items []struct{ Level, Message string }
		Count int
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad body: %s", rr.Body.String())
	}
	joined := ""
	for _, it := range out.Items {
		joined += it.Message + "\n"
	}
	if !bytes.Contains([]byte(joined), []byte("排程整天都是 0")) {
		t.Errorf("want an all-zero-schedule warning, got: %q", joined)
	}
	if !bytes.Contains([]byte(joined), []byte("尚未成功連上燈具")) {
		t.Errorf("want a no-lamp-contact warning, got: %q", joined)
	}
	if out.Count != len(out.Items) {
		t.Errorf("count %d != len(items) %d", out.Count, len(out.Items))
	}
}
