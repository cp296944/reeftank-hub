package storage

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeletedSeedDoesNotReturnAfterReopen(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "legacy"}[legacy], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hub.db")
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err = db.SeedWaterRecords(ctx, BuiltinWaterSeed()); err != nil {
				t.Fatal(err)
			}
			// Include the case where every imported record was intentionally deleted.
			if _, err = db.db.Exec(`DELETE FROM water_records`); err != nil {
				t.Fatal(err)
			}
			if legacy {
				if _, err = db.db.Exec(`DELETE FROM app_settings WHERE key='water.builtin_seed_v1'`); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			db, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = db.SeedWaterRecords(ctx, BuiltinWaterSeed()); err != nil {
				t.Fatal(err)
			}
			var n int
			if err = db.db.QueryRow(`SELECT count(*) FROM water_records`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("deleted records returned: %d %v", n, err)
			}
		})
	}
}

func TestSeedFailureIsAtomic(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.SeedWaterRecords(context.Background(), []WaterRecord{{MeasuredAt: "2026-09-01"}, {MeasuredAt: "invalid"}})
	if err == nil {
		t.Fatal("invalid seed accepted")
	}
	var n int
	db.db.QueryRow(`SELECT count(*) FROM water_records`).Scan(&n)
	if n != 0 {
		t.Fatal("partial seed committed")
	}
}

func TestExportIncludesUncheckpointedWAL(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.db.Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.InsertWaterRecord(context.Background(), WaterRecord{MeasuredAt: "2026-10-01", Note: "latest WAL data"}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	db.Register(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/hub/storage/export", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	exported := filepath.Join(t.TempDir(), "export.db")
	if err = os.WriteFile(exported, rr.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	copy, err := sql.Open("sqlite", exported)
	if err != nil {
		t.Fatal(err)
	}
	defer copy.Close()
	var note, integrity string
	if err = copy.QueryRow(`SELECT note FROM water_records`).Scan(&note); err != nil || note != "latest WAL data" {
		t.Fatalf("export lost data: %q %v", note, err)
	}
	if err = copy.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("invalid export: %s %v", integrity, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, ".export-*")); len(matches) != 0 {
		t.Fatal("temporary export leaked")
	}
}

func TestHealthChecksSchemaAndLeavesNoProbe(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.CheckHealth(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	db.db.QueryRow(`SELECT count(*) FROM app_settings WHERE key='health.probe'`).Scan(&n)
	if n != 0 {
		t.Fatal("probe persisted")
	}
	db.db.Exec(`DROP TABLE equipment_events`)
	if db.CheckHealth(context.Background()) == nil {
		t.Fatal("missing core table considered healthy")
	}
}

func TestActivityLiveReplayAndRestartAgree(t *testing.T) {
	for _, restartAt := range []int{-1, 2, 3} {
		t.Run(time.Duration(restartAt).String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hub.db")
			db, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { db.Close() }()
			ctx := context.Background()
			base := time.Now().UTC().Add(-time.Minute)
			for i, w := range []float64{3, 0, 3, 0, 0} {
				if i == restartAt {
					db.Close()
					db, err = Open(path)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = db.BackfillEquipmentEvents(ctx, "roller", base.Add(-time.Second)); err != nil {
						t.Fatal(err)
					}
				}
				if _, _, err = db.RecordEquipmentReading(ctx, "roller", "entity9", base.Add(time.Duration(i)*time.Second), 117, w/117, w); err != nil {
					t.Fatal(err)
				}
			}
			if n, err := db.BackfillEquipmentEvents(ctx, "roller", base.Add(-time.Second)); err != nil || n != 0 {
				t.Fatalf("replay inserted %d: %v", n, err)
			}
			events, err := db.EquipmentEvents(ctx, "roller", 1, 100, time.UTC)
			if err != nil || len(events) != 1 || events[0].EndedAt == nil {
				t.Fatalf("events=%+v err=%v", events, err)
			}
		})
	}
}

func TestActivityReplayUsesTwoLowReadings(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	base := time.Now().Add(-time.Minute)
	for i, w := range []float64{3, 0, 3, 0, 0} {
		if err = db.RecordOutletSample(ctx, "roller", base.Add(time.Duration(i)*time.Second), 117, w/117, w); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := db.BackfillEquipmentEvents(ctx, "roller", base); err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
}

func TestMigrationErrorDoesNotReplaceDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`CREATE TABLE entity_samples(original_value TEXT); INSERT INTO entity_samples VALUES('preserve me')`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if db, err := Open(path); err == nil {
		db.Close()
		t.Fatal("expected incompatible schema failure")
	}
	raw, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var value string
	if err = raw.QueryRow(`SELECT original_value FROM entity_samples`).Scan(&value); err != nil || value != "preserve me" {
		t.Fatalf("original data lost: %q %v", value, err)
	}
}

func TestActivityBindingChangeEndsOldEvent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	base := time.Now().Add(-time.Minute)
	for i, binding := range []string{"socket9", "socket18"} {
		if _, _, err = db.RecordEquipmentReading(ctx, "roller", binding, base.Add(time.Duration(i)*time.Second), 117, .03, 3); err != nil {
			t.Fatal(err)
		}
	}
	events, err := db.EquipmentEvents(ctx, "roller", 1, 100, time.UTC)
	if err != nil || len(events) != 2 || events[1].EndedAt == nil {
		t.Fatalf("binding change merged events: %+v %v", events, err)
	}
}

func TestLegacyUnclosedEventWithTwoLowSamples(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	base := time.Now().Add(-time.Minute)
	if _, err = db.StartEquipmentEvent(ctx, "roller", base, .03, 3); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err = db.RecordOutletSample(ctx, "roller", base.Add(time.Duration(i)*time.Second), 117, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = db.RecordEquipmentReading(ctx, "roller", "socket9", base.Add(3*time.Second), 117, .03, 3); err != nil {
		t.Fatal(err)
	}
	events, err := db.EquipmentEvents(ctx, "roller", 1, 100, time.UTC)
	if err != nil || len(events) != 2 || events[1].EndedAt == nil {
		t.Fatalf("legacy stopped event merged: %+v %v", events, err)
	}
}
