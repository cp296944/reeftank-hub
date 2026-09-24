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
		`CREATE TABLE IF NOT EXISTS water_records(id INTEGER PRIMARY KEY AUTOINCREMENT, measured_at TEXT NOT NULL, no3 REAL, po4 REAL, ph REAL, sg REAL, kh REAL, ca REAL, mg REAL, water_change INTEGER NOT NULL DEFAULT 0, change_liters REAL, note TEXT, source TEXT NOT NULL DEFAULT 'hub', source_ref TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(source,source_ref))`,
		`CREATE INDEX IF NOT EXISTS idx_water_records_time ON water_records(measured_at)`,
		`CREATE TABLE IF NOT EXISTS temperature_samples(source TEXT NOT NULL, value REAL NOT NULL, unit TEXT NOT NULL, source_time TEXT NOT NULL, received_time TEXT NOT NULL, latency_ms INTEGER, PRIMARY KEY(source,source_time))`,
		`CREATE TABLE IF NOT EXISTS dosing_heads(id INTEGER PRIMARY KEY, name TEXT NOT NULL, liquid TEXT, calibration REAL, container_ml REAL, remaining_ml REAL, enabled INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS dosing_audit(id INTEGER PRIMARY KEY, head_id INTEGER, action TEXT NOT NULL, amount_ml REAL, status TEXT NOT NULL, detail TEXT, source_time TEXT NOT NULL, received_time TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS app_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS outlet_samples(device_id TEXT NOT NULL, sampled_at TEXT NOT NULL, voltage REAL NOT NULL, current REAL NOT NULL, power REAL NOT NULL, PRIMARY KEY(device_id,sampled_at))`,
		`CREATE INDEX IF NOT EXISTS idx_outlet_samples_time ON outlet_samples(sampled_at)`,
		`CREATE TABLE IF NOT EXISTS equipment_events(id INTEGER PRIMARY KEY AUTOINCREMENT, device_id TEXT NOT NULL, started_at TEXT NOT NULL, ended_at TEXT, duration_seconds REAL, peak_current REAL NOT NULL DEFAULT 0, peak_power REAL NOT NULL DEFAULT 0, samples INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS idx_equipment_events_device_time ON equipment_events(device_id,started_at)`,
		`INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(1,datetime('now'))`,
		`INSERT OR IGNORE INTO app_settings(key,value,updated_at) VALUES('retention.entity_samples_days','0',datetime('now'))`,
		`INSERT OR IGNORE INTO app_settings(key,value,updated_at) VALUES('temperature.source','direct',datetime('now'))`,
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

type EquipmentDailyCount struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

type EquipmentEvent struct {
	ID              int64    `json:"id"`
	DeviceID        string   `json:"device_id"`
	StartedAt       string   `json:"started_at"`
	EndedAt         *string  `json:"ended_at,omitempty"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
	PeakCurrent     float64  `json:"peak_current"`
	PeakPower       float64  `json:"peak_power"`
	Samples         int      `json:"samples"`
}

func (d *DB) EquipmentEvents(ctx context.Context, deviceID string, days, limit int, loc *time.Location) ([]EquipmentEvent, error) {
	if days <= 0 || days > 36500 {
		days = 36500
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	since := time.Now().In(loc).AddDate(0, 0, -days)
	rows, err := d.db.QueryContext(ctx, `SELECT id,device_id,started_at,ended_at,duration_seconds,peak_current,peak_power,samples FROM equipment_events WHERE device_id=? AND started_at>=? ORDER BY started_at DESC LIMIT ?`, deviceID, since.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EquipmentEvent{}
	for rows.Next() {
		var e EquipmentEvent
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.StartedAt, &e.EndedAt, &e.DurationSeconds, &e.PeakCurrent, &e.PeakPower, &e.Samples); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) RecordOutletSample(ctx context.Context, deviceID string, at time.Time, voltage, current, power float64) error {
	if _, err := d.db.ExecContext(ctx, `INSERT OR REPLACE INTO outlet_samples(device_id,sampled_at,voltage,current,power) VALUES(?,?,?,?,?)`, deviceID, at.UTC().Format(time.RFC3339Nano), voltage, current, power); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `DELETE FROM outlet_samples WHERE sampled_at < ?`, at.Add(-24*time.Hour).UTC().Format(time.RFC3339Nano))
	return err
}

func (d *DB) StartEquipmentEvent(ctx context.Context, deviceID string, at time.Time, current, power float64) (int64, error) {
	r, err := d.db.ExecContext(ctx, `INSERT INTO equipment_events(device_id,started_at,peak_current,peak_power,samples) VALUES(?,?,?,?,1)`, deviceID, at.UTC().Format(time.RFC3339Nano), current, power)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (d *DB) UpdateEquipmentEvent(ctx context.Context, id int64, started, at time.Time, current, power float64, finished bool) error {
	if finished {
		_, err := d.db.ExecContext(ctx, `UPDATE equipment_events SET ended_at=?,duration_seconds=?,peak_current=max(peak_current,?),peak_power=max(peak_power,?),samples=samples+1 WHERE id=?`, at.UTC().Format(time.RFC3339Nano), at.Sub(started).Seconds(), current, power, id)
		return err
	}
	_, err := d.db.ExecContext(ctx, `UPDATE equipment_events SET peak_current=max(peak_current,?),peak_power=max(peak_power,?),samples=samples+1 WHERE id=?`, current, power, id)
	return err
}

// BackfillEquipmentEvents rebuilds missing activity starts from the retained
// raw outlet samples.  It is deliberately idempotent: an event with the exact
// device/start timestamp is never inserted twice.  A single sample above the
// threshold is enough because short top-off and roller runs can fit between
// two polls; voltage must still be in the normal mains range to reject noise.
func (d *DB) BackfillEquipmentEvents(ctx context.Context, deviceID string, since time.Time) (int, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT sampled_at,voltage,current,power FROM outlet_samples WHERE device_id=? AND sampled_at>=? ORDER BY sampled_at`, deviceID, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type event struct {
		start, end  time.Time
		peakCurrent float64
		peakPower   float64
		samples     int
	}
	var active *event
	events := []event{}
	for rows.Next() {
		var raw string
		var voltage, current, power float64
		if err = rows.Scan(&raw, &voltage, &current, &power); err != nil {
			return 0, err
		}
		at, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			continue
		}
		running := voltage >= 80 && voltage <= 140 && (current >= .005 || power >= .5)
		if running {
			if active == nil {
				active = &event{start: at, end: at}
			}
			active.end = at
			active.samples++
			if current > active.peakCurrent {
				active.peakCurrent = current
			}
			if power > active.peakPower {
				active.peakPower = power
			}
		} else if active != nil {
			active.end = at
			events = append(events, *active)
			active = nil
		}
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if active != nil {
		events = append(events, *active)
	}
	inserted := 0
	for _, e := range events {
		result, execErr := d.db.ExecContext(ctx, `INSERT INTO equipment_events(device_id,started_at,ended_at,duration_seconds,peak_current,peak_power,samples) SELECT ?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM equipment_events WHERE device_id=? AND started_at=?)`, deviceID, e.start.UTC().Format(time.RFC3339Nano), e.end.UTC().Format(time.RFC3339Nano), e.end.Sub(e.start).Seconds(), e.peakCurrent, e.peakPower, e.samples, deviceID, e.start.UTC().Format(time.RFC3339Nano))
		if execErr != nil {
			return inserted, execErr
		}
		if n, _ := result.RowsAffected(); n > 0 {
			inserted++
		}
	}
	return inserted, nil
}

func (d *DB) EquipmentActivity(ctx context.Context, deviceID string, days int, loc *time.Location) (int, []EquipmentDailyCount, error) {
	now := time.Now().In(loc)
	var start time.Time
	if days <= 0 {
		var raw sql.NullString
		if err := d.db.QueryRowContext(ctx, `SELECT min(started_at) FROM equipment_events WHERE device_id=?`, deviceID).Scan(&raw); err != nil {
			return 0, nil, err
		}
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		if raw.Valid {
			if first, err := time.Parse(time.RFC3339Nano, raw.String); err == nil {
				local := first.In(loc)
				start = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
			}
		}
		days = int(now.Sub(start).Hours()/24) + 1
	} else {
		if days > 36500 {
			days = 36500
		}
		since := now.AddDate(0, 0, -days+1)
		start = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, loc)
	}
	rows, err := d.db.QueryContext(ctx, `SELECT started_at FROM equipment_events WHERE device_id=? AND started_at>=? ORDER BY started_at`, deviceID, start.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	today := now.Format("2006-01-02")
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return 0, nil, err
		}
		t, e := time.Parse(time.RFC3339Nano, raw)
		if e == nil {
			counts[t.In(loc).Format("2006-01-02")]++
		}
	}
	out := make([]EquipmentDailyCount, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, EquipmentDailyCount{Day: day, Count: counts[day]})
	}
	return counts[today], out, rows.Err()
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
	EntityID    string  `json:"entity_id"`
	State       string  `json:"state"`
	LastUpdated string  `json:"last_updated"`
	Min         float64 `json:"min,omitempty"`
	Max         float64 `json:"max,omitempty"`
	Average     float64 `json:"average,omitempty"`
	Samples     int64   `json:"samples,omitempty"`
}

func (d *DB) History(ctx context.Context, entityIDs []string, since time.Time) ([][]HistoryPoint, error) {
	hours := int(time.Since(since).Hours())
	bucket := 60
	switch {
	case hours > 24*31:
		bucket = 86400
	case hours > 24*7:
		bucket = 3600
	case hours > 24:
		bucket = 900
	case hours > 6:
		bucket = 300
	}
	out := make([][]HistoryPoint, 0, len(entityIDs))
	for _, id := range entityIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		rows, err := d.db.QueryContext(ctx, `SELECT entity_id,printf('%.6f',avg(value)),max(source_time),min(value),max(value),avg(value),count(*) FROM entity_samples WHERE entity_id=? AND source_time>=? AND value IS NOT NULL GROUP BY CAST(strftime('%s',source_time)/? AS INTEGER) ORDER BY max(source_time)`, id, since.UTC().Format(time.RFC3339Nano), bucket)
		if err != nil {
			return nil, err
		}
		group := []HistoryPoint{}
		for rows.Next() {
			var p HistoryPoint
			if err := rows.Scan(&p.EntityID, &p.State, &p.LastUpdated, &p.Min, &p.Max, &p.Average, &p.Samples); err != nil {
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
