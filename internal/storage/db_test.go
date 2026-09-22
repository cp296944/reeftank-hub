package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cp296944/reeftank-hub/internal/homeassistant"
)

func TestMigrateRecordAndBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state := homeassistant.State{EntityID: "sensor.power", State: "12.5", LastUpdated: "2026-09-22T07:00:00Z", Attributes: map[string]any{"unit_of_measurement": "W"}}
	if err := db.RecordState(context.Background(), state, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordState(context.Background(), state, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.SampleCount(context.Background()); n != 1 {
		t.Fatalf("sample count=%d", n)
	}
	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := db.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(backup); err != nil || st.Size() == 0 {
		t.Fatalf("backup stat=%v err=%v", st, err)
	}
}

func TestCorruptDatabaseIsPreservedAndRebuilt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hub.db")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("preserved corrupt files=%v", matches)
	}
}
