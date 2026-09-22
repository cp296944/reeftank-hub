package dosing

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (s *Store) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/dosing", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"state": s.Snapshot(), "upcoming": s.Upcoming(), "capabilities": Capabilities()})
	})
	mux.HandleFunc("PUT /api/hub/dosing/heads/", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/hub/dosing/heads/"))
		var h Head
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&h) != nil {
			write(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		h.ID = id
		got, e := s.Update(h)
		if e != nil {
			write(w, 400, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, got)
	})
	mux.HandleFunc("POST /api/hub/dosing/manual", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			HeadID  int     `json:"head_id"`
			Amount  float64 `json:"amount_ml"`
			Confirm bool    `json:"confirm"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || !in.Confirm {
			write(w, 400, map[string]string{"error": "head_id, amount_ml and confirm=true required"})
			return
		}
		a, e := s.Manual(in.HeadID, in.Amount)
		if e != nil {
			write(w, 400, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, a)
	})
	mux.HandleFunc("POST /api/hub/dosing/calibrate", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			HeadID   int     `json:"head_id"`
			Measured float64 `json:"measured_ml"`
			Seconds  float64 `json:"seconds"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			write(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		h, e := s.Calibrate(in.HeadID, in.Measured, in.Seconds)
		if e != nil {
			write(w, 400, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, h)
	})
	mux.HandleFunc("PUT /api/hub/dosing/simulator", func(w http.ResponseWriter, r *http.Request) {
		var v Simulator
		if json.NewDecoder(r.Body).Decode(&v) != nil {
			write(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		if e := s.SetSimulator(v); e != nil {
			write(w, 400, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, v)
	})
	mux.HandleFunc("POST /api/hub/dosing/prime", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			HeadID  int  `json:"head_id"`
			Start   bool `json:"start"`
			Confirm bool `json:"confirm"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || !in.Confirm {
			write(w, 400, map[string]string{"error": "confirm=true required"})
			return
		}
		a, e := s.Prime(in.HeadID, in.Start)
		if e != nil {
			write(w, 400, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, a)
	})
	mux.HandleFunc("POST /api/hub/dosing/sync-time", func(w http.ResponseWriter, r *http.Request) {
		a, e := s.SyncClock()
		if e != nil {
			write(w, 500, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, a)
	})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
