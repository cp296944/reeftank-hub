package storage

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"
)

func (d *DB) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/storage/status", func(w http.ResponseWriter, r *http.Request) {
		count, err := d.SampleCount(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"database": filepath.Base(d.path), "samples": count, "retention_days": 0})
	})
	mux.HandleFunc("POST /api/hub/storage/backup", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Confirm bool `json:"confirm"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil || !in.Confirm {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
			return
		}
		dest := filepath.Join(filepath.Dir(d.path), "backups", "hub-"+time.Now().UTC().Format("20060102T150405Z")+".db")
		if err := d.Backup(dest); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"backup": dest})
	})
	mux.HandleFunc("GET /api/hub/storage/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="reeftank-hub.db"`)
		w.Header().Set("Content-Type", "application/vnd.sqlite3")
		http.ServeFile(w, r, d.path)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
