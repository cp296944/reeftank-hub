package updater

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApplySerializesAllCallers(t *testing.T) {
	bin := []byte("test binary")
	sum := sha256.Sum256(bin)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bin" {
			w.Write(bin)
		} else {
			fmt.Fprintf(w, "%x  %s\n", sum, assetName)
		}
	}))
	defer srv.Close()
	entered := make(chan struct{})
	u := New(Options{CurrentTag: "hub-v0.11.2", PreApply: func(ctx context.Context, _ string) error {
		close(entered)
		<-ctx.Done()
		return fmt.Errorf("test stopped before installation")
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rel := &Release{Tag: "hub-v0.11.3", AssetURL: srv.URL + "/bin", SumsURL: srv.URL + "/sums"}
	done := make(chan error, 1)
	go func() { done <- u.Apply(ctx, rel) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first apply did not start")
	}
	before := u.Progress()
	if err := u.Apply(ctx, rel); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("concurrent apply accepted: %v", err)
	}
	if after := u.Progress(); after.Stage != before.Stage || !after.Running {
		t.Fatal("rejected caller overwrote active progress")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("expected pre-apply stop")
	}
}

func TestUnhealthyVersionIsNotConfirmed(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "state"), 0755)
	u := New(Options{InstallRoot: root})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checked := make(chan struct{})
	u.ConfirmAfterStart(ctx, "hub-v0.11.3", time.Millisecond, func() bool { close(checked); return false })
	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("health callback not called")
	}
	if _, err := os.Stat(filepath.Join(root, "state", "CONFIRMED")); !os.IsNotExist(err) {
		t.Fatalf("unhealthy version confirmed: %v", err)
	}
}
