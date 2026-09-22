package piweb

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRewritePushShift(t *testing.T) {
	// 24 rows, column 2 (first channel) = the hour, so rotation is easy to see.
	rows := make([][]int, 24)
	for h := 0; h < 24; h++ {
		rows[h] = []int{h, 0, h, 0, 0, 0, 0, 0}
	}
	body, _ := json.Marshal(map[string]any{
		"manual":                 []int{0, 0, 0, 0, 0, 0},
		"schedule":               rows,
		"mode":                   "auto",
		"schedule_shift_minutes": 360, // +6h
	})
	r := httptest.NewRequest(http.MethodPost, "/api/push", bytes.NewReader(body))
	(&handler{}).rewritePush(r)

	got, _ := io.ReadAll(r.Body)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("bad rewritten body: %v", err)
	}
	if _, ok := m["schedule_shift_minutes"]; ok {
		t.Error("schedule_shift_minutes should have been stripped")
	}
	var out [][]int
	_ = json.Unmarshal(m["schedule"], &out)
	if len(out) != 24 {
		t.Fatalf("want 24 rows, got %d", len(out))
	}
	// row 6 should now carry the channel value that was at hour 0 (0), and
	// column 0 must stay the row's own hour.
	if out[6][0] != 6 {
		t.Errorf("row 6 hour col = %d, want 6", out[6][0])
	}
	if out[6][2] != 0 {
		t.Errorf("row 6 ch after +6h shift = %d, want 0 (from hour 0)", out[6][2])
	}
	if out[0][2] != 18 {
		t.Errorf("row 0 ch after +6h shift = %d, want 18 (from hour 18)", out[0][2])
	}
}

func TestRewritePushNoShiftIsPassthrough(t *testing.T) {
	body := []byte(`{"schedule":[[1,2,3]],"mode":"auto"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/push", bytes.NewReader(body))
	(&handler{}).rewritePush(r)
	got, _ := io.ReadAll(r.Body)
	var a, b map[string]json.RawMessage
	_ = json.Unmarshal(body, &a)
	_ = json.Unmarshal(got, &b)
	if string(b["schedule"]) != string(a["schedule"]) {
		t.Errorf("schedule changed with no shift: %s", got)
	}
}

func TestHTMLInjection(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><head><title>x</title></head><body>hi</body></html>")
	})
	h := &handler{Deps: Deps{Next: next}, overlayJS: []byte("//x")}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rr.Body.String(), `/pi/overlay.js`) {
		t.Errorf("overlay script not injected: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "</head>") {
		t.Error("head tag lost")
	}
}
