package homeassistant

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cp296944/reeftank-hub/internal/equipment"
)

func TestSwitchEndpointIsRestrictedToEquipmentMap(t *testing.T) {
	store, err := equipment.Open(filepath.Join(t.TempDir(), "equipment.json"))
	if err != nil {
		t.Fatal(err)
	}
	api := &API{Client: New("http://ha.invalid", "secret"), Equipment: store}
	mux := http.NewServeMux()
	api.Register(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/hub/ha/switch", strings.NewReader(`{"entity_id":"switch.not_in_aquarium","on":true}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unmapped switch status=%d body=%s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/hub/ha/switch", strings.NewReader(`{"entity_id":"switch.tp_link_power_strip_3a31_4_zhu_ma","on":false}`)))
	if rr.Code != http.StatusConflict {
		t.Fatalf("critical switch without confirmation status=%d body=%s", rr.Code, rr.Body.String())
	}
}
