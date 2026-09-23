package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type WaterRecord struct {
	ID           int64    `json:"id,omitempty"`
	MeasuredAt   string   `json:"measured_at"`
	NO3          *float64 `json:"no3,omitempty"`
	PO4          *float64 `json:"po4,omitempty"`
	PH           *float64 `json:"ph,omitempty"`
	SG           *float64 `json:"sg,omitempty"`
	KH           *float64 `json:"kh,omitempty"`
	CA           *float64 `json:"ca,omitempty"`
	MG           *float64 `json:"mg,omitempty"`
	WaterChange  bool     `json:"water_change"`
	ChangeLiters *float64 `json:"change_liters,omitempty"`
	Note         string   `json:"note,omitempty"`
	Source       string   `json:"source,omitempty"`
	SourceRef    string   `json:"source_ref,omitempty"`
	CreatedAt    string   `json:"created_at,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

func parseLocalTime(v string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(v), time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("measured_at must be an ISO date/time")
}

func (d *DB) InsertWaterRecord(ctx context.Context, v WaterRecord) (int64, error) {
	t, err := parseLocalTime(v.MeasuredAt)
	if err != nil {
		return 0, err
	}
	if v.Source == "" {
		v.Source = "hub"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var sourceRef any
	if ref := strings.TrimSpace(v.SourceRef); ref != "" {
		sourceRef = ref
	}
	res, err := d.db.ExecContext(ctx, `INSERT INTO water_records(measured_at,no3,po4,ph,sg,kh,ca,mg,water_change,change_liters,note,source,source_ref,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, t.UTC().Format(time.RFC3339Nano), v.NO3, v.PO4, v.PH, v.SG, v.KH, v.CA, v.MG, v.WaterChange, v.ChangeLiters, strings.TrimSpace(v.Note), v.Source, sourceRef, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateWaterRecord(ctx context.Context, id int64, v WaterRecord) error {
	t, err := parseLocalTime(v.MeasuredAt)
	if err != nil {
		return err
	}
	res, err := d.db.ExecContext(ctx, `UPDATE water_records SET measured_at=?,no3=?,po4=?,ph=?,sg=?,kh=?,ca=?,mg=?,water_change=?,change_liters=?,note=?,updated_at=? WHERE id=?`, t.UTC().Format(time.RFC3339Nano), v.NO3, v.PO4, v.PH, v.SG, v.KH, v.CA, v.MG, v.WaterChange, v.ChangeLiters, strings.TrimSpace(v.Note), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *DB) DeleteWaterRecord(ctx context.Context, id int64) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM water_records WHERE id=?`, id)
	return err
}

func scanWater(rows *sql.Rows) (WaterRecord, error) {
	var v WaterRecord
	var wc int
	err := rows.Scan(&v.ID, &v.MeasuredAt, &v.NO3, &v.PO4, &v.PH, &v.SG, &v.KH, &v.CA, &v.MG, &wc, &v.ChangeLiters, &v.Note, &v.Source, &v.SourceRef, &v.CreatedAt, &v.UpdatedAt)
	v.WaterChange = wc != 0
	return v, err
}
func (d *DB) WaterRecords(ctx context.Context, limit int) ([]WaterRecord, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id,measured_at,no3,po4,ph,sg,kh,ca,mg,water_change,change_liters,COALESCE(note,''),source,COALESCE(source_ref,''),created_at,updated_at FROM water_records ORDER BY measured_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WaterRecord{}
	for rows.Next() {
		v, e := scanWater(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) WaterDashboard(ctx context.Context) (map[string]any, error) {
	recs, err := d.WaterRecords(ctx, 5000)
	if err != nil {
		return nil, err
	}
	latest := map[string]any{}
	history := map[string][]map[string]any{}
	metrics := []string{"no3", "po4", "ph", "sg", "kh", "ca", "mg"}
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		vals := map[string]*float64{"no3": r.NO3, "po4": r.PO4, "ph": r.PH, "sg": r.SG, "kh": r.KH, "ca": r.CA, "mg": r.MG}
		for _, m := range metrics {
			if vals[m] != nil {
				p := map[string]any{"value": *vals[m], "time": r.MeasuredAt}
				history[m] = append(history[m], p)
				latest[m] = p
			}
		}
	}
	return map[string]any{"configured": true, "latest": latest, "history": history, "records": recs, "count": len(recs)}, nil
}

func (d *DB) TemperatureHistory(ctx context.Context, hours int) ([]map[string]any, error) {
	if hours <= 0 || hours > 24*3660 {
		hours = 24 * 30
	}
	rows, err := d.db.QueryContext(ctx, `SELECT value,source_time,latency_ms FROM temperature_samples WHERE source_time>=? ORDER BY source_time`, time.Now().Add(-time.Duration(hours)*time.Hour).UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var v float64
		var t string
		var ms sql.NullInt64
		if err := rows.Scan(&v, &t, &ms); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"value": v, "time": t, "latency_ms": ms.Int64})
	}
	return out, rows.Err()
}

func (d *DB) Setting(ctx context.Context, key, def string) string {
	var v string
	if d.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, key).Scan(&v) != nil {
		return def
	}
	return v
}
func (d *DB) SetSetting(ctx context.Context, key, value string) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, key, value, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (d *DB) TemperatureSource(ctx context.Context) string {
	return d.Setting(ctx, "temperature.source", "direct")
}
func (d *DB) LatestHATemperature(ctx context.Context) (float64, time.Time, bool) {
	var v float64
	var raw string
	err := d.db.QueryRowContext(ctx, `SELECT value,source_time FROM entity_samples WHERE entity_id='sensor.hai_shui_gang_wen_du' AND value IS NOT NULL ORDER BY source_time DESC LIMIT 1`).Scan(&v, &raw)
	if err != nil {
		return 0, time.Time{}, false
	}
	t, e := time.Parse(time.RFC3339Nano, raw)
	return v, t, e == nil
}

func (d *DB) registerWater(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/water", func(w http.ResponseWriter, r *http.Request) {
		v, e := d.WaterDashboard(r.Context())
		if e != nil {
			writeJSON(w, 500, map[string]string{"error": e.Error()})
			return
		}
		h, _ := d.TemperatureHistory(r.Context(), 24*3660)
		v["temperature_history"] = h
		v["temperature_source"] = d.Setting(r.Context(), "temperature.source", "direct")
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("POST /api/hub/water/records", func(w http.ResponseWriter, r *http.Request) {
		var v WaterRecord
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&v) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		id, e := d.InsertWaterRecord(r.Context(), v)
		if e != nil {
			writeJSON(w, 400, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 201, map[string]any{"id": id})
	})
	mux.HandleFunc("PUT /api/hub/water/records/", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/hub/water/records/"), 10, 64)
		var v WaterRecord
		if id <= 0 || json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&v) != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid record"})
			return
		}
		if e := d.UpdateWaterRecord(r.Context(), id, v); e != nil {
			writeJSON(w, 400, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"id": id})
	})
	mux.HandleFunc("DELETE /api/hub/water/records/", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/hub/water/records/"), 10, 64)
		if id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "invalid id"})
			return
		}
		if e := d.DeleteWaterRecord(r.Context(), id); e != nil {
			writeJSON(w, 500, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": id})
	})
	mux.HandleFunc("GET /api/hub/temperature/history", func(w http.ResponseWriter, r *http.Request) {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		v, e := d.TemperatureHistory(r.Context(), hours)
		if e != nil {
			writeJSON(w, 500, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 200, v)
	})
	mux.HandleFunc("GET /api/hub/settings/temperature", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"source": d.Setting(r.Context(), "temperature.source", "direct")})
	})
	mux.HandleFunc("PUT /api/hub/settings/temperature", func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Source string `json:"source"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil || (v.Source != "direct" && v.Source != "ha") {
			writeJSON(w, 400, map[string]string{"error": "source must be direct or ha"})
			return
		}
		if e := d.SetSetting(r.Context(), "temperature.source", v.Source); e != nil {
			writeJSON(w, 500, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"source": v.Source})
	})
}

func (d *DB) SeedWaterRecords(ctx context.Context, records []WaterRecord) error {
	for _, v := range records {
		if v.Source == "" {
			v.Source = "excel_import"
		}
		if _, err := d.InsertWaterRecord(ctx, v); err != nil && !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("seed %s: %w", v.SourceRef, err)
		}
	}
	return nil
}
