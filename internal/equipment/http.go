package equipment

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Store) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/equipment", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.Snapshot())
	})
	mux.HandleFunc("PUT /api/hub/equipment/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			SwitchEntity *string `json:"switch_entity"`
			DisplayName  *string `json:"display_name"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil || (in.SwitchEntity == nil && in.DisplayName == nil) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "switch_entity or display_name is required"})
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		var snap Snapshot
		var err error
		if in.SwitchEntity != nil {
			snap, err = s.Assign(id, strings.TrimSpace(*in.SwitchEntity))
		}
		if err == nil && in.DisplayName != nil {
			snap, err = s.Rename(id, *in.DisplayName)
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, snap)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
