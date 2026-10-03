package jebao

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type historyStoreStub struct {
	recorded []string
	points   map[string][]HistoryPoint
}

func (s *historyStoreStub) RecordJebao(_ context.Context, device Device) error {
	s.recorded = append(s.recorded, device.ID)
	return nil
}

func (s *historyStoreStub) JebaoHistory(_ context.Context, deviceID string, _ int) ([]HistoryPoint, error) {
	return s.points[deviceID], nil
}

func TestMonitorMACGuardAndFailurePreservesLastValue(t *testing.T) {
	m, err := Open(filepath.Join(t.TempDir(), "jebao.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.settings.Hosts["return-pump-1"] = "192.168.0.2"
	id := Identity{IP: "192.168.0.2", MAC: m.devices[0].MAC, ProductKey: "02039876751049deb404d1d89221ec4b"}
	m.discover = func(context.Context, string) ([]Identity, error) { return []Identity{id}, nil }
	calls := 0
	m.read = func(context.Context, Identity) (map[string]any, error) {
		calls++
		return map[string]any{"Motor_Speed": uint64(75)}, nil
	}
	m.Refresh(context.Background(), false)
	if calls != 1 || !m.Snapshot().Devices[0].Connected {
		t.Fatal("expected successful exact-MAC read")
	}
	m.read = func(context.Context, Identity) (map[string]any, error) { return nil, errors.New("offline") }
	m.Refresh(context.Background(), false)
	d := m.Snapshot().Devices[0]
	if d.Connected || !d.Stale || d.Attributes["Motor_Speed"] != uint64(75) {
		t.Fatalf("last value semantics %+v", d)
	}
	id.MAC = "00:00:00:00:00:01"
	m.read = func(context.Context, Identity) (map[string]any, error) { t.Fatal("read wrong MAC"); return nil, nil }
	m.Refresh(context.Background(), false)
	if m.Snapshot().Devices[0].Connected {
		t.Fatal("wrong-MAC target marked connected")
	}
	old := time.Now().Add(-2 * time.Minute)
	m.devices[0].LastSuccess = &old
	m.devices[0].Connected = true
	if !m.Snapshot().Devices[0].Stale {
		t.Fatal("expired status marked fresh")
	}
}
func TestSettingsValidationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jebao.json")
	m, _ := Open(path)
	mux := http.NewServeMux()
	m.Register(mux)
	for _, body := range []string{`{"hosts":{"return-pump-1":"8.8.8.8"}}`, `{"hosts":{"other":"192.168.0.2"}}`, `{"hosts":{"return-pump-1":"localhost"}}`} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("PUT", "/api/hub/jebao/settings", strings.NewReader(body)))
		if rr.Code != 400 {
			t.Fatal(rr.Code, rr.Body)
		}
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("PUT", "/api/hub/jebao/settings", strings.NewReader(`{"hosts":{"return-pump-1":"192.168.0.2"}}`)))
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body)
	}
	reloaded, err := Open(path)
	if err != nil || reloaded.settings.Hosts["return-pump-1"] != "192.168.0.2" {
		t.Fatal("settings not persisted", err)
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/hub/jebao/control", nil))
	if rr.Code != 404 {
		t.Fatal("unexpected control endpoint")
	}
}

func TestHistoryRecordsAllDevicesAndServesAPI(t *testing.T) {
	m, _ := Open(filepath.Join(t.TempDir(), "jebao.json"))
	store := &historyStoreStub{points: map[string][]HistoryPoint{
		"return-pump-1": {{Time: time.Now().UTC().Format(time.RFC3339Nano), Connected: true}},
	}}
	m.SetHistoryStore(store)
	m.discover = func(context.Context, string) ([]Identity, error) { return nil, errors.New("offline") }
	if !m.Refresh(context.Background(), false) || len(store.recorded) != 4 {
		t.Fatalf("recorded devices = %v", store.recorded)
	}
	mux := http.NewServeMux()
	m.Register(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/hub/jebao/history?hours=168", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var response struct {
		Hours   int                       `json:"hours"`
		Devices map[string][]HistoryPoint `json:"devices"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil || response.Hours != 168 || len(response.Devices) != 4 || len(response.Devices["return-pump-1"]) != 1 {
		t.Fatalf("response=%+v err=%v", response, err)
	}
}
