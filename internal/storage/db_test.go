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

func TestBuiltinWaterSeedIsCompleteAndIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seed := BuiltinWaterSeed()
	if len(seed) != 73 {
		t.Fatalf("seed rows=%d", len(seed))
	}
	if err := db.SeedWaterRecords(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedWaterRecords(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	dash, err := db.WaterDashboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dash["count"] != 73 {
		t.Fatalf("dashboard count=%v", dash["count"])
	}
	latest := dash["latest"].(map[string]any)
	if latest["ca"].(map[string]any)["value"] != 430.0 {
		t.Fatalf("latest ca=%v", latest["ca"])
	}
}

func TestManualWaterRecordsAllowMultipleEmptySourceRefs(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, measuredAt := range []string{"2026-09-20T12:34", "2026-09-21T12:34"} {
		if _, err := db.InsertWaterRecord(context.Background(), WaterRecord{
			MeasuredAt:  measuredAt,
			WaterChange: true,
		}); err != nil {
			t.Fatalf("insert manual record at %s: %v", measuredAt, err)
		}
	}
}

func TestBackfillEquipmentEventsCountsShortSingleSampleRun(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	base := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, time.UTC)
	for i, watts := range []float64{0.05, 3.3, 0.04, 0.03} {
		if err := db.RecordOutletSample(ctx, "outlet_09", base.Add(time.Duration(i)*2*time.Second), 117, watts/117, watts); err != nil {
			t.Fatal(err)
		}
	}
	for run := 0; run < 2; run++ {
		n, err := db.BackfillEquipmentEvents(ctx, "outlet_09", base.Add(-time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if (run == 0 && n != 1) || (run == 1 && n != 0) {
			t.Fatalf("run %d inserted %d events", run, n)
		}
	}
	today, history, err := db.EquipmentActivity(ctx, "outlet_09", 1, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history length=%d", len(history))
	}
	if today != 1 {
		t.Fatalf("today count=%d", today)
	}
}
