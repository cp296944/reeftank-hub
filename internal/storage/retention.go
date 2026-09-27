package storage

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

const retentionSettingKey = "retention.entity_samples_days"

type RetentionCleanup struct {
	EntitySamples      int64 `json:"entity_samples"`
	TemperatureSamples int64 `json:"temperature_samples"`
	EquipmentEvents    int64 `json:"equipment_events"`
}

func ValidRetentionDays(days int) bool {
	return days == 0 || days == 30 || days == 90 || days == 180 || days == 365
}

func (d *DB) RetentionDays(ctx context.Context) int {
	days, err := strconv.Atoi(d.Setting(ctx, retentionSettingKey, "0"))
	if err != nil || !ValidRetentionDays(days) {
		return 0
	}
	return days
}

func (d *DB) SetRetentionDays(ctx context.Context, days int) (RetentionCleanup, error) {
	if !ValidRetentionDays(days) {
		return RetentionCleanup{}, fmt.Errorf("retention_days must be one of 0, 30, 90, 180, 365")
	}
	if err := d.SetSetting(ctx, retentionSettingKey, strconv.Itoa(days)); err != nil {
		return RetentionCleanup{}, err
	}
	return d.CleanupRetention(ctx)
}

// CleanupRetention removes only automatically collected telemetry. Manually
// entered water-quality and water-change records remain permanent.
func (d *DB) CleanupRetention(ctx context.Context) (RetentionCleanup, error) {
	days := d.RetentionDays(ctx)
	if days == 0 {
		return RetentionCleanup{}, nil
	}
	cutoff := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339Nano)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return RetentionCleanup{}, err
	}
	defer tx.Rollback()
	var out RetentionCleanup
	for _, q := range []struct {
		sql string
		n   *int64
	}{
		{`DELETE FROM entity_samples WHERE source_time < ?`, &out.EntitySamples},
		{`DELETE FROM temperature_samples WHERE source_time < ?`, &out.TemperatureSamples},
		{`DELETE FROM equipment_events WHERE started_at < ?`, &out.EquipmentEvents},
	} {
		result, execErr := tx.ExecContext(ctx, q.sql, cutoff)
		if execErr != nil {
			return RetentionCleanup{}, execErr
		}
		*q.n, _ = result.RowsAffected()
	}
	if err := tx.Commit(); err != nil {
		return RetentionCleanup{}, err
	}
	return out, nil
}
