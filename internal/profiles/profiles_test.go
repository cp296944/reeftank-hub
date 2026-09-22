package profiles

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSaveListDeletePerLamp(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "")

	post := func(lamp, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/profiles", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r, lamp)
		return w
	}
	list := func(lamp string) map[string]json.RawMessage {
		r := httptest.NewRequest("GET", "/api/profiles", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r, lamp)
		var m map[string]json.RawMessage
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return m
	}

	if w := post("mac-aa", `{"name":"reef","x":1}`); w.Code != 200 {
		t.Fatalf("save A: %d %s", w.Code, w.Body)
	}
	if w := post("mac-bb", `{"name":"fish","x":2}`); w.Code != 200 {
		t.Fatalf("save B: %d", w.Code)
	}

	if a := list("mac-aa"); len(a) != 1 || a["reef"] == nil {
		t.Errorf("lamp A profiles = %v", a)
	}
	if b := list("mac-bb"); len(b) != 1 || b["fish"] == nil {
		t.Errorf("lamp B profiles = %v (should not see lamp A's)", b)
	}

	// delete on A must not touch B
	r := httptest.NewRequest("DELETE", "/api/profiles/reef", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r, "mac-aa")
	if len(list("mac-aa")) != 0 {
		t.Error("profile not deleted for lamp A")
	}
	if len(list("mac-bb")) != 1 {
		t.Error("lamp B profile affected by lamp A delete")
	}
}

func TestMigrateOnce(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, "")
	legacy := map[string]json.RawMessage{
		"old1": json.RawMessage(`{"name":"old1"}`),
		"old2": json.RawMessage(`{"name":"old2"}`),
	}
	s.Migrate("mac-cc", legacy)
	r := httptest.NewRequest("GET", "/api/profiles", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r, "mac-cc")
	var m map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	if len(m) != 2 {
		t.Fatalf("migrated profiles = %v", m)
	}
	// second call is a no-op (dir already populated)
	s.Migrate("mac-cc", map[string]json.RawMessage{"new": json.RawMessage(`{"name":"new"}`)})
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, httptest.NewRequest("GET", "/api/profiles", nil), "mac-cc")
	_ = json.Unmarshal(w2.Body.Bytes(), &m)
	if len(m) != 2 {
		t.Errorf("second migrate should be a no-op, got %v", m)
	}
}
