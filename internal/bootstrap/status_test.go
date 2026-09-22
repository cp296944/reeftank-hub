package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspectMissingAndValidMarker(t *testing.T) {
	dir := t.TempDir()
	got := Inspect(dir)
	if got.Ready || !got.Required || got.InstallCommand == "" {
		t.Fatalf("missing marker status = %+v", got)
	}

	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := `{"schema":1,"installed_at":"2026-09-22T00:00:00Z","version":"test"}`
	if err := os.WriteFile(filepath.Join(state, MarkerName), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	got = Inspect(dir)
	if !got.Ready || got.Required || got.Marker == nil {
		t.Fatalf("valid marker status = %+v", got)
	}
	if !got.Marker.InstalledAt.Equal(time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("installed_at = %v", got.Marker.InstalledAt)
	}
}

func TestInspectRejectsBrokenMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state", MarkerName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Inspect(dir)
	if got.Ready || got.Error == "" {
		t.Fatalf("broken marker status = %+v", got)
	}
}
