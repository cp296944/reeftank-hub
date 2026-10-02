package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Persisted with each sample so a restart, including after the first low
// reading, does not change the two-low-samples debounce rule.
type activityState struct {
	Last    time.Time
	Started time.Time
	EventID int64
	Low     int
	Binding string
}

func activityRunning(voltage, current, power float64) bool {
	return voltage >= 80 && voltage <= 140 && (current >= .005 || power >= .5)
}

func loadActivityState(ctx context.Context, tx *sql.Tx, id string) (activityState, error) {
	var s activityState
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, "activity.v2."+id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &s)
		return s, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return s, err
	}
	// Adopt old history without deleting/reclassifying it. Resume the last
	// open event; closed history forms a watermark that replay cannot cross.
	var start string
	var end sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,started_at,ended_at FROM equipment_events WHERE device_id=? ORDER BY started_at DESC LIMIT 1`, id).Scan(&s.EventID, &start, &end)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Started, err = time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return s, err
	}
	s.Last = s.Started
	if end.Valid {
		s.Last, err = time.Parse(time.RFC3339Nano, end.String)
		s.EventID = 0
		return s, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT sampled_at,voltage,current,power FROM outlet_samples WHERE device_id=? ORDER BY sampled_at DESC LIMIT 2`, id)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	first, stillLow := true, true
	for rows.Next() {
		var at string
		var v, a, w float64
		if err = rows.Scan(&at, &v, &a, &w); err != nil {
			return s, err
		}
		if first {
			s.Last, err = time.Parse(time.RFC3339Nano, at)
			if err != nil {
				return s, err
			}
			first = false
		}
		if stillLow && !activityRunning(v, a, w) {
			s.Low++
		} else {
			stillLow = false
		}
	}
	return s, rows.Err()
}

// RecordEquipmentReading is the sole live/replay event transition path. Raw
// sample, event and checkpoint commit together or not at all.
func (d *DB) RecordEquipmentReading(ctx context.Context, id, binding string, at time.Time, voltage, current, power float64) (bool, bool, error) {
	d.activityMu.Lock()
	defer d.activityMu.Unlock()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()
	s, err := loadActivityState(ctx, tx, id)
	if err != nil {
		return false, false, err
	}
	if !at.After(s.Last) {
		return s.EventID != 0, false, nil
	}
	closeEvent := func(end time.Time) error {
		_, e := tx.ExecContext(ctx, `UPDATE equipment_events SET ended_at=?,duration_seconds=? WHERE id=? AND ended_at IS NULL`, end.UTC().Format(time.RFC3339Nano), end.Sub(s.Started).Seconds(), s.EventID)
		return e
	}
	// A legacy process may have stopped after the second low raw sample was
	// saved but before it closed its event. Do not merge the next run into it.
	if s.EventID != 0 && s.Low >= 2 {
		if err = closeEvent(s.Last); err != nil {
			return false, false, err
		}
		s.EventID = 0
		s.Low = 0
	}
	if s.EventID != 0 && binding != "" && s.Binding != "" && binding != s.Binding {
		if err = closeEvent(s.Last); err != nil {
			return false, false, err
		}
		s.EventID = 0
		s.Low = 0
	}
	if binding != "" {
		s.Binding = binding
	}
	running, created := activityRunning(voltage, current, power), false
	if running {
		s.Low = 0
		if s.EventID == 0 {
			r, e := tx.ExecContext(ctx, `INSERT INTO equipment_events(device_id,started_at,peak_current,peak_power,samples) VALUES(?,?,?,?,1)`, id, at.UTC().Format(time.RFC3339Nano), current, power)
			if e != nil {
				return false, false, e
			}
			s.EventID, err = r.LastInsertId()
			if err != nil {
				return false, false, err
			}
			s.Started = at
			created = true
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE equipment_events SET peak_current=max(peak_current,?),peak_power=max(peak_power,?),samples=samples+1 WHERE id=?`, current, power, s.EventID)
			if err != nil {
				return false, false, err
			}
		}
	} else if s.EventID != 0 {
		s.Low++
		if s.Low >= 2 {
			if err = closeEvent(at); err != nil {
				return false, false, err
			}
			s.EventID = 0
			s.Low = 0
		}
	}
	s.Last = at
	b, err := json.Marshal(s)
	if err != nil {
		return false, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, "activity.v2."+id, string(b), at.UTC().Format(time.RFC3339Nano)); err != nil {
		return false, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO outlet_samples(device_id,sampled_at,voltage,current,power) VALUES(?,?,?,?,?)`, id, at.UTC().Format(time.RFC3339Nano), voltage, current, power); err != nil {
		return false, false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM outlet_samples WHERE sampled_at < ?`, at.Add(-24*time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		return false, false, err
	}
	return s.EventID != 0, created, tx.Commit()
}
