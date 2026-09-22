package homeassistant

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cp296944/reeftank-hub/internal/equipment"
)

type API struct {
	Client    *Client
	Equipment *equipment.Store
	Sync      *Syncer
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/ha/power", a.power)
	mux.HandleFunc("GET /api/hub/ha/history", a.history)
	mux.HandleFunc("POST /api/hub/ha/switch", a.setSwitch)
}

func (a *API) power(w http.ResponseWriter, r *http.Request) {
	if a.Client == nil || !a.Client.Configured() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"configured": false, "error": "Home Assistant credentials are not configured"})
		return
	}
	states, connected, last := a.Sync.Snapshot()
	if len(states) == 0 {
		var err error
		states, err = a.Client.States(r.Context())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"configured": true, "connected": false, "error": err.Error()})
			return
		}
		last = time.Now().UTC()
	}
	snap := a.Equipment.Snapshot()
	strips := make([]map[string]any, 0, len(snap.PowerStrips))
	for _, strip := range snap.PowerStrips {
		outlets := make([]map[string]any, 0, 6)
		for _, device := range snap.Devices {
			if device.Slot < strip.SlotStart || device.Slot > strip.SlotEnd {
				continue
			}
			base := strings.TrimPrefix(device.SwitchEntity, "switch.")
			outlets = append(outlets, map[string]any{
				"id": device.ID, "slot": device.Slot, "name": device.DisplayName, "critical": device.Critical,
				"switch_entity": device.SwitchEntity, "switch": states[device.SwitchEntity],
				"voltage": states["sensor."+base+"_voltage"], "current": states["sensor."+base+"_current"],
				"power": states["sensor."+base+"_current_consumption"], "today": states["sensor."+base+"_today_s_consumption"],
				"month": states["sensor."+base+"_this_month_s_consumption"],
			})
		}
		strips = append(strips, map[string]any{
			"id": strip.ID, "name": strip.DisplayName, "led_entity": strip.LEDEntity, "led": states[strip.LEDEntity],
			"current": states[strip.CurrentEntity], "power": states[strip.PowerEntity], "today": states[strip.TodayEnergyEntity],
			"month": states[strip.MonthEnergyEntity], "on_since": states[strip.OnSinceEntity], "outlets": outlets,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "connected": connected, "stale": !connected || time.Since(last) > 2*time.Minute, "updated_at": last, "strips": strips, "temperature": states["sensor.hai_shui_gang_wen_du"]})
}

func TrackedEntities(snap equipment.Snapshot) []string {
	seen := map[string]bool{"sensor.hai_shui_gang_wen_du": true}
	for _, strip := range snap.PowerStrips {
		for _, id := range []string{strip.LEDEntity, strip.CurrentEntity, strip.PowerEntity, strip.TodayEnergyEntity, strip.MonthEnergyEntity, strip.OnSinceEntity} {
			seen[id] = true
		}
	}
	for _, device := range snap.Devices {
		base := strings.TrimPrefix(device.SwitchEntity, "switch.")
		for _, id := range []string{device.SwitchEntity, "sensor." + base + "_voltage", "sensor." + base + "_current", "sensor." + base + "_current_consumption", "sensor." + base + "_today_s_consumption", "sensor." + base + "_this_month_s_consumption"} {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}

func (a *API) setSwitch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		EntityID string `json:"entity_id"`
		On       *bool  `json:"on"`
		Confirm  bool   `json:"confirm"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.On == nil || !a.allowedSwitch(in.EntityID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a mapped switch entity and boolean on are required"})
		return
	}
	if !*in.On && a.isCritical(in.EntityID) && !in.Confirm {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "turning off a critical device requires confirm=true"})
		return
	}
	if err := a.Client.SetSwitch(r.Context(), in.EntityID, *in.On); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entity_id": in.EntityID, "on": *in.On})
}

func (a *API) isCritical(entityID string) bool {
	for _, device := range a.Equipment.Snapshot().Devices {
		if device.SwitchEntity == entityID {
			return device.Critical
		}
	}
	return false
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*31 {
		hours = 24 * 7
	}
	snap := a.Equipment.Snapshot()
	ids := []string{"sensor.hai_shui_gang_wen_du"}
	for _, strip := range snap.PowerStrips {
		ids = append(ids, strip.PowerEntity, strip.CurrentEntity)
	}
	end := time.Now()
	raw, err := a.Client.History(r.Context(), end.Add(-time.Duration(hours)*time.Hour), end, ids)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(raw)
}

func (a *API) allowedSwitch(entityID string) bool {
	snap := a.Equipment.Snapshot()
	for _, device := range snap.Devices {
		if device.SwitchEntity == entityID {
			return true
		}
	}
	for _, strip := range snap.PowerStrips {
		if strip.LEDEntity == entityID {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
