package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cp296944/reeftank-hub/internal/jebao"
)

func TestJebaoHistoryRecordsEveryDeviceState(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	device := jebao.Device{ID: "wavemaker-1", Connected: true, Attributes: map[string]any{"Flow": uint64(62), "Frequency": uint64(10), "SwitchON": true, "Fault_OverTemp": false}, Labels: map[string]string{"Mode": "隨機造浪"}, LastAttempt: &now}
	if err := db.RecordJebao(context.Background(), device); err != nil {
		t.Fatal(err)
	}
	points, err := db.JebaoHistory(context.Background(), device.ID, 24)
	if err != nil || len(points) != 1 {
		t.Fatalf("points=%+v err=%v", points, err)
	}
	if points[0].Speed == nil || *points[0].Speed != 62 || points[0].Frequency == nil || *points[0].Frequency != 10 || points[0].Mode != "隨機造浪" || points[0].SwitchOn == nil || !*points[0].SwitchOn || points[0].Fault {
		t.Fatalf("point=%+v", points[0])
	}
}
