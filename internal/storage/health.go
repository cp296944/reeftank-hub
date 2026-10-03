package storage

import "context"

// CheckHealth verifies that the core schema is readable. It intentionally does
// not compete for SQLite's writer lock while HA history is being backfilled.
// External HA, cloud and radio availability are not prerequisites.
func (d *DB) CheckHealth(ctx context.Context) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, query := range []string{
		`SELECT id FROM water_records LIMIT 1`,
		`SELECT entity_id FROM entity_samples LIMIT 1`,
		`SELECT device_id FROM equipment_events LIMIT 1`,
		`SELECT device_id FROM jebao_samples LIMIT 1`,
	} {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}
