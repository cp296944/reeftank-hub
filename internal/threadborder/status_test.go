package threadborder

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotReadsRCPAndOTBR(t *testing.T) {
	dir := t.TempDir()
	rcp := filepath.Join(dir, "rcp")
	if err := os.WriteFile(rcp, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("RCP_DEVICE="+rcp+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/node" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"role":"leader","networkName":"ha-thread-test"}`))
	}))
	defer srv.Close()
	m := &Monitor{EnvPath: env, RESTURL: srv.URL + "/api/node", Client: srv.Client()}
	s := m.Snapshot()
	if !s.Configured || !s.RCPPresent || !s.OTBR || s.Role != "leader" || s.Network != "ha-thread-test" {
		t.Fatalf("status = %+v", s)
	}
}
