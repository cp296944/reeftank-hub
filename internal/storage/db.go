package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cp296944/reeftank-hub/internal/equipment"
	"github.com/cp296944/reeftank-hub/internal/homeassistant"
	_ "modernc.org/sqlite"
)

type DB struct {
	db   *sql.DB
	path string
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	d, err := open(path)
	if err == nil {
		return d, nil
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return nil, err
	}
	corrupt := path + ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
	if renameErr := os.Rename(path, corrupt); renameErr != nil {
		return nil, fmt.Errorf("database open failed (%v), preserve corrupt database: %w", err, renameErr)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, statErr := os.Stat(path + suffix); statErr == nil {
			_ = os.Rename(path+suffix, corrupt+suffix)
		}
	}
	return open(path)
}

func open(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	d := &DB{db: sqldb, path: path}
	if err := d.migrate(context.Background()); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.db.Close() }
func (d *DB) Path() string { return d.path }

func (d *DB) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS source_status(source TEXT PRIMARY KEY, last_success TEXT, last_error TEXT, stale INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS entity_samples(entity_id TEXT NOT NULL, state TEXT NOT NULL, value REAL, unit TEXT, source_time TEXT NOT NULL, received_time TEXT NOT NULL, source TEXT NOT NULL, PRIMARY KEY(entity_id, source_time))`,
		`CREATE INDEX IF NOT EXISTS idx_entity_samples_time ON entity_samples(entity_id, source_time)`,
		`CREATE TABLE IF NOT EXISTS equipment_mapping(device_id TEXT PRIMARY KEY, display_name TEXT NOT NULL, switch_entity TEXT NOT NULL, slot INTEGER NOT NULL, critical INTEGER NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS water_quality(id INTEGER PRIMARY KEY, metric TEXT NOT NULL, value REAL, unit TEXT, source_time TEXT NOT NULL, received_time TEXT NOT NULL, UNIQUE(metric, source_time))`,
		`CREATE TABLE IF NOT EXISTS temperature_samples(source TEXT NOT NULL, value REAL NOT NULL, unit TEXT NOT NULL, source_time TEXT NOT NULL, received_time TEXT NOT NULL, latency_ms INTEGER, PRIMARY KEY(source,source_time))`,
		`CREATE TABLE IF NOT EXISTS dosing_heads(id INTEGER PRIMARY KEY, name TEXT NOT NULL, liquid TEXT, calibration REAL, container_ml REAL, remaining_ml REAL, enabled INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS dosing_audit(id INTEGER PRIMARY KEY, head_id INTEGER, action TEXT NOT NULL, amount_ml REAL, status TEXT NOT NULL, detail TEXT, source_time TEXT NOT NULL, received_time TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS app_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(1,datetime('now'))`,
		`INSERT OR IGNORE INTO app_settings(key,value,updated_at) VALUES('retention.entity_samples_days','0',datetime('now'))`,
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	return tx.Commit()
}

func (d *DB) RecordTemperature(ctx context.Context, value float64, sourceTime, received time.Time, latency time.Duration) error {
	_, err := d.db.ExecContext(ctx, `INSERT OR REPLACE INTO temperature_samples(source,value,unit,source_time,received_time,latency_ms) VALUES(?,?,?,?,?,?)`, "xiaoyu", value, "°C", sourceTime.UTC().Format(time.RFC3339Nano), received.UTC().Format(time.RFC3339Nano), latency.Milliseconds())
	return err
}

func (d *DB) ReplaceWaterQuality(ctx context.Context, at time.Time, values map[string]float64) error {
	tx, e := d.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for metric, value := range values {
		if _, e = tx.ExecContext(ctx, `INSERT INTO water_quality(metric,value,unit,source_time,received_time) VALUES(?,?,?,?,?) ON CONFLICT(metric,source_time) DO UPDATE SET value=excluded.value,received_time=excluded.received_time`, metric, value, "", at.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (d *DB) WaterHistory(ctx context.Context) (map[string][]map[string]any, error) {
	rows, e := d.db.QueryContext(ctx, `SELECT metric,value,source_time FROM water_quality ORDER BY source_time`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string][]map[string]any{}
	for rows.Next() {
		var m, t string
		var v float64
		if e = rows.Scan(&m, &v, &t); e != nil {
			return nil, e
		}
		out[m] = append(out[m], map[string]any{"value": v, "time": t})
	}
	return out, rows.Err()
}

func (d *DB) TemperatureCount(ctx context.Context) (int64, error) {
	var n int64
	err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM temperature_samples`).Scan(&n)
	return n, err
}

type HistoryPoint struct {
	EntityID    string `json:"entity_id"`
	State       string `json:"state"`
	LastUpdated string `json:"last_updated"`
}

func (d *DB) History(ctx context.Context, entityIDs []string, since time.Time) ([][]HistoryPoint, error) {
	out := make([][]HistoryPoint, 0, len(entityIDs))
	for _, id := range entityIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		rows, err := d.db.QueryContext(ctx, `SELECT entity_id,state,source_time FROM entity_samples WHERE entity_id=? AND source_time>=? ORDER BY source_time`, id, since.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return nil, err
		}
		group := []HistoryPoint{}
		for rows.Next() {
			var p HistoryPoint
			if err := rows.Scan(&p.EntityID, &p.State, &p.LastUpdated); err != nil {
				rows.Close()
				return nil, err
			}
			group = append(group, p)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		out = append(out, group)
	}
	return out, nil
}

func (d *DB) RecordState(ctx context.Context, state homeassistant.State, received time.Time) error {
	if state.EntityID == "" || state.LastUpdated == "" && state.LastChanged == "" {
		return nil
	}
	sourceTime := state.LastUpdated
	if sourceTime == "" {
		sourceTime = state.LastChanged
	}
	var value any
	if n, err := strconv.ParseFloat(state.State, 64); err == nil {
		value = n
	}
	unit, _ := state.Attributes["unit_of_measurement"].(string)
	_, err := d.db.ExecContext(ctx, `INSERT INTO entity_samples(entity_id,state,value,unit,source_time,received_time,source) VALUES(?,?,?,?,?,?,?) ON CONFLICT(entity_id,source_time) DO UPDATE SET state=excluded.state,value=excluded.value,unit=excluded.unit,received_time=excluded.received_time`, state.EntityID, state.State, value, unit, sourceTime, received.UTC().Format(time.RFC3339Nano), "home_assistant")
	return err
}

func (d *DB) SetSourceStatus(ctx context.Context, source string, success bool, detail string, now time.Time) error {
	stale := 1
	lastSuccess := any(nil)
	lastError := detail
	if success {
		stale, lastSuccess, lastError = 0, now.UTC().Format(time.RFC3339Nano), ""
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO source_status(source,last_success,last_error,stale,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(source) DO UPDATE SET last_success=COALESCE(excluded.last_success,source_status.last_success),last_error=excluded.last_error,stale=excluded.stale,updated_at=excluded.updated_at`, source, lastSuccess, lastError, stale, now.UTC().Format(time.RFC3339Nano))
	return err
}

func (d *DB) Backup(dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	quoted := strings.ReplaceAll(filepath.ToSlash(dest), "'", "''")
	_, err := d.db.Exec("VACUUM INTO '" + quoted + "'")
	return err
}

func (d *DB) SampleCount(ctx context.Context) (int64, error) {
	var n int64
	err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM entity_samples`).Scan(&n)
	return n, err
}

func (d *DB) SyncEquipment(ctx context.Context, snap equipment.Snapshot) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, device := range snap.Devices {
		if _, err := tx.ExecContext(ctx, `INSERT INTO equipment_mapping(device_id,display_name,switch_entity,slot,critical,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(device_id) DO UPDATE SET display_name=excluded.display_name,switch_entity=excluded.switch_entity,slot=excluded.slot,critical=excluded.critical,updated_at=excluded.updated_at`, device.ID, device.DisplayName, device.SwitchEntity, device.Slot, device.Critical, snap.UpdatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
