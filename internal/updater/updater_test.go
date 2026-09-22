package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCmpTag(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"hub-v0.1.0", "hub-v0.1.0", 0},
		{"hub-v0.2.0", "hub-v0.1.0", 1},
		{"hub-v0.1.0", "hub-v0.2.0", -1},
		{"hub-v1.0.0", "hub-v0.9.0", 1},
		{"hub-v0.1.10", "hub-v0.1.2", 1},
		{"hub-v0.4.0-rc.1", "hub-v0.4.0", 0}, // suffix dropped
	}
	for _, c := range cases {
		if got := cmpTag(c.a, c.b); got != c.want {
			t.Errorf("cmpTag(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestShaFor(t *testing.T) {
	sums := []byte("abc123  reeftank-hub-linux-arm64\ndeadbeef *SHA256SUMS\n")
	got, err := shaFor(sums, "reeftank-hub-linux-arm64")
	if err != nil || got != "abc123" {
		t.Fatalf("shaFor = %q, %v", got, err)
	}
	if _, err := shaFor(sums, "missing"); err == nil {
		t.Fatal("expected error for missing asset")
	}
}

func TestCheckAndApply(t *testing.T) {
	root := t.TempDir()
	binContent := []byte("#!/bin/true\nnew-binary\n")
	sum := sha256.Sum256(binContent)
	sums := hex.EncodeToString(sum[:]) + "  " + assetName + "\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/k7/releases", func(w http.ResponseWriter, r *http.Request) {
		self := "http://" + r.Host
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"tag_name": "hub-v0.2.0", "prerelease": false, "body": "notes",
				"assets": []map[string]any{
					{"name": assetName, "browser_download_url": self + "/bin"},
					{"name": sumsName, "browser_download_url": self + "/sums"},
				},
			},
			{"tag_name": "hub-v0.1.0", "prerelease": false, "assets": []map[string]any{
				{"name": assetName, "browser_download_url": self + "/bin"},
				{"name": sumsName, "browser_download_url": self + "/sums"},
			}},
		})
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(binContent) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sums)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	restarted := false
	u := New(Options{
		Repo: "acme/k7", InstallRoot: root, CurrentTag: "hub-v0.1.0",
		RestartFunc: func(context.Context) error { restarted = true; return nil },
	})
	// redirect API base for the test
	u.o.HTTPClient = srv.Client()
	oldBase := apiBaseForTest
	apiBaseForTest = srv.URL
	defer func() { apiBaseForTest = oldBase }()

	rel, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rel == nil || rel.Tag != "hub-v0.2.0" {
		t.Fatalf("Check picked %+v", rel)
	}

	if err := u.Apply(context.Background(), rel); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !restarted {
		t.Error("service was not restarted")
	}
	link, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil || filepath.Base(link) != "hub-v0.2.0" {
		t.Fatalf("current -> %q, %v", link, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "releases/hub-v0.2.0/reeftank-hub")); string(b) != string(binContent) {
		t.Error("installed binary content wrong")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "state/PREVIOUS")); string(b) != "hub-v0.1.0\n" {
		t.Errorf("PREVIOUS = %q", b)
	}
}

func TestConfirmAfterStart(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "state"), 0o755)
	u := New(Options{InstallRoot: root, Repo: "x/y"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	u.ConfirmAfterStart(ctx, "hub-v0.2.0", 10*time.Millisecond, func() bool { return true })
	time.Sleep(80 * time.Millisecond)
	if b, _ := os.ReadFile(filepath.Join(root, "state/CONFIRMED")); string(b) != "hub-v0.2.0\n" {
		t.Errorf("CONFIRMED = %q", b)
	}
}
