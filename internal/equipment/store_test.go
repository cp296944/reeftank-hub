package equipment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAndSwapPersist(t *testing.T) {
	p := filepath.Join(t.TempDir(), "equipment.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	if len(before.Devices) != 18 || before.Devices[5].DisplayName != "GMP30R" || before.Devices[10].DisplayName != "JNS蛋白機" {
		t.Fatalf("unexpected defaults: %+v", before.Devices)
	}
	a, b := before.Devices[3].SwitchEntity, before.Devices[14].SwitchEntity
	after, err := s.Assign("outlet_04", b)
	if err != nil {
		t.Fatal(err)
	}
	if after.Devices[3].SwitchEntity != b || after.Devices[14].SwitchEntity != a {
		t.Fatalf("entities were not swapped: %+v", after.Devices)
	}
	reopened, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Devices[3].SwitchEntity != b {
		t.Fatal("assignment did not persist")
	}
}

func TestEquipmentHTTP(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "equipment.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/hub/equipment", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "冷水機馬達") {
		t.Fatalf("GET = %d %s", rr.Code, rr.Body.String())
	}

	body := `{"display_name":"主循環馬達"}`
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/hub/equipment/outlet_04", strings.NewReader(body)))
	if rr.Code != 200 {
		t.Fatalf("PUT = %d %s", rr.Code, rr.Body.String())
	}
	var got Snapshot
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.Devices[3].DisplayName != "主循環馬達" {
		t.Fatalf("rename failed: %+v", got.Devices[3])
	}
}

func TestRejectsInvalidEntityAndUnknownFields(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "equipment.json"))
	if _, err := s.Assign("outlet_01", "light.not_a_switch"); err == nil {
		t.Fatal("invalid entity accepted")
	}
	mux := http.NewServeMux()
	s.Register(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/hub/equipment/outlet_01", strings.NewReader(`{"shell":"bad"}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", rr.Code)
	}
}
