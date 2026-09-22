package bootstrap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyMaintenanceHealthcheck(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 5, 0, 0, 0, time.UTC)
	req := MaintenanceRequest{Schema: 1, Action: "healthcheck", Nonce: "abcdefghijklmnop", CreatedAt: now}
	b, _ := json.Marshal(req)
	reqPath := filepath.Join(state, "maintenance-request.json")
	if err := os.WriteFile(reqPath, b, 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := ApplyMaintenance(MaintenanceOptions{InstallRoot: root, RequestPath: reqPath, Version: "test", Now: func() time.Time { return now }})
	if err != nil || !got.OK || got.Nonce != req.Nonce {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	if _, err := os.Stat(reqPath); !os.IsNotExist(err) {
		t.Fatalf("request was not consumed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "maintenance-result.json")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyMaintenanceRejectsUnsafeInputs(t *testing.T) {
	now := time.Date(2026, 9, 22, 5, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		req  MaintenanceRequest
	}{
		{"unknown action", MaintenanceRequest{Schema: 1, Action: "shell", Nonce: "abcdefghijklmnop", CreatedAt: now}},
		{"short nonce", MaintenanceRequest{Schema: 1, Action: "healthcheck", Nonce: "short", CreatedAt: now}},
		{"expired", MaintenanceRequest{Schema: 1, Action: "healthcheck", Nonce: "abcdefghijklmnop", CreatedAt: now.Add(-11 * time.Minute)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(root, "state")
			_ = os.MkdirAll(state, 0o755)
			b, _ := json.Marshal(tc.req)
			p := filepath.Join(state, "maintenance-request.json")
			_ = os.WriteFile(p, b, 0o640)
			if _, err := ApplyMaintenance(MaintenanceOptions{InstallRoot: root, RequestPath: p, Now: func() time.Time { return now }}); err == nil {
				t.Fatal("unsafe request accepted")
			}
		})
	}
}

func TestApplyMaintenanceRejectsAlternatePath(t *testing.T) {
	root := t.TempDir()
	if _, err := ApplyMaintenance(MaintenanceOptions{InstallRoot: root, RequestPath: filepath.Join(root, "other.json")}); err == nil {
		t.Fatal("alternate request path accepted")
	}
}
