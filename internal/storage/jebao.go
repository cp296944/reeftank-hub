package storage

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/cp296944/reeftank-hub/internal/jebao"
)

func numericAttribute(attributes map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		value, ok := attributes[key]
		if !ok {
			continue
		}
		var number float64
		switch v := value.(type) {
		case uint64:
			number = float64(v)
		case uint32:
			number = float64(v)
		case int:
			number = float64(v)
		case float64:
			number = v
		case json.Number:
			n, err := v.Float64()
			if err != nil {
				continue
			}
			number = n
		case string:
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				continue
			}
			number = n
		default:
			continue
		}
		return &number
	}
	return nil
}

func boolAttribute(attributes map[string]any, keys ...string) *bool {
	for _, key := range keys {
		value, ok := attributes[key]
		if !ok {
			continue
		}
		if v, ok := value.(bool); ok {
			return &v
		}
	}
	return nil
}

func (d *DB) RecordJebao(ctx context.Context, device jebao.Device) error {
	sampledAt := time.Now().UTC()
	if device.LastAttempt != nil {
		sampledAt = device.LastAttempt.UTC()
	}
	mode := device.Labels["Mode"]
	if mode == "" {
		mode = device.Labels["AutoMode"]
	}
	fault := false
	for key, value := range device.Attributes {
		if strings.HasPrefix(key, "Fault_") {
			if active, ok := value.(bool); ok && active {
				fault = true
				break
			}
		}
	}
	attributes, _ := json.Marshal(device.Attributes)
	_, err := d.db.ExecContext(ctx, `INSERT INTO jebao_samples(device_id,sampled_at,connected,stale,speed,frequency,mode,switch_on,fault,attributes_json)
		VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(device_id,sampled_at) DO UPDATE SET connected=excluded.connected,stale=excluded.stale,speed=excluded.speed,frequency=excluded.frequency,mode=excluded.mode,switch_on=excluded.switch_on,fault=excluded.fault,attributes_json=excluded.attributes_json`,
		device.ID, sampledAt.Format(time.RFC3339Nano), device.Connected, device.Stale,
		numericAttribute(device.Attributes, "Motor_Speed", "Flow", "flow"), numericAttribute(device.Attributes, "Frequency", "frequency"),
		mode, boolAttribute(device.Attributes, "SwitchON", "Switch", "switch"), fault, string(attributes))
	return err
}

func (d *DB) JebaoHistory(ctx context.Context, deviceID string, hours int) ([]jebao.HistoryPoint, error) {
	if hours <= 0 || hours > 8760 {
		hours = 24
	}
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour).UTC().Format(time.RFC3339Nano)
	rows, err := d.db.QueryContext(ctx, `SELECT sampled_at,connected,stale,speed,frequency,mode,switch_on,fault FROM jebao_samples WHERE device_id=? AND sampled_at>=? ORDER BY sampled_at`, deviceID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []jebao.HistoryPoint{}
	for rows.Next() {
		var point jebao.HistoryPoint
		var speed, frequency *float64
		var mode *string
		var switchOn *bool
		if err := rows.Scan(&point.Time, &point.Connected, &point.Stale, &speed, &frequency, &mode, &switchOn, &point.Fault); err != nil {
			return nil, err
		}
		point.Speed = speed
		point.Frequency = frequency
		point.SwitchOn = switchOn
		if mode != nil {
			point.Mode = *mode
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

func (d *DB) JebaoSampleCount(ctx context.Context) (int64, error) {
	var count int64
	err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM jebao_samples`).Scan(&count)
	return count, err
}
