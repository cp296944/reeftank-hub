package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cp296944/reeftank-hub/internal/homeassistant"
	"github.com/cp296944/reeftank-hub/internal/storage"
	"github.com/cp296944/reeftank-hub/internal/temperature"
)

type panelAPI struct {
	db   *storage.DB
	temp *temperature.Poller
	ha   *homeassistant.API
}

func (p *panelAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/panel/status", p.status)
	mux.HandleFunc("POST /api/panel/water-change", p.waterChange)
}

func (p *panelAPI) status(w http.ResponseWriter, r *http.Request) {
	d, err := p.db.WaterDashboard(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	latest, _ := d["latest"].(map[string]any)
	records, _ := d["records"].([]storage.WaterRecord)
	out := map[string]any{"generated_at": time.Now(), "hub": "ok", "version": "1"}
	metric := func(key, format string, low, high float64) {
		point, _ := latest[key].(map[string]any)
		v, ok := point["value"].(float64)
		if !ok {
			out[key] = "off:無資料"
			return
		}
		level := "ok"
		if v < low || v > high {
			level = "warn"
		}
		out[key] = level + ":" + fmt.Sprintf(format, v)
	}
	metric("no3", "%.1f", 2, 10)
	metric("po4", "%.2f", .02, .10)
	metric("sg", "%.3f", 1.024, 1.027)
	metric("kh", "%.1f", 7, 9)
	metric("ca", "%.0f", 400, 450)
	metric("mg", "%.0f", 1250, 1400)

	age := func(keys ...string) string {
		var oldest time.Time
		for _, key := range keys {
			point, _ := latest[key].(map[string]any)
			raw, _ := point["time"].(string)
			t, e := time.Parse(time.RFC3339Nano, raw)
			if e != nil {
				continue
			}
			if oldest.IsZero() || t.Before(oldest) {
				oldest = t
			}
		}
		if oldest.IsZero() {
			return "off:無資料"
		}
		days := time.Since(oldest).Hours() / 24
		level := "ok"
		if days > 14 {
			level = "bad"
		} else if days > 7 {
			level = "warn"
		}
		return fmt.Sprintf("%s:%.0f 天前", level, days)
	}
	out["np_age"] = age("no3", "po4")
	out["elements_age"] = age("kh", "ca", "mg")
	lastChange := time.Time{}
	for _, rec := range records {
		if rec.WaterChange {
			lastChange, _ = time.Parse(time.RFC3339Nano, rec.MeasuredAt)
			break
		}
	}
	if lastChange.IsZero() {
		out["water_change"] = "off:無資料"
	} else {
		days := time.Since(lastChange).Hours() / 24
		level := "ok"
		if days > 14 {
			level = "bad"
		} else if days > 7 {
			level = "warn"
		}
		out["water_change"] = fmt.Sprintf("%s:%.0f 天前", level, days)
	}

	t := p.temp.Snapshot()
	if !t.Configured || t.Stale {
		out["temperature"] = "off:無資料"
	} else {
		level := "ok"
		if t.Value > 27 {
			level = "bad"
		} else if t.Value < 24 {
			level = "warn"
		}
		out["temperature"] = fmt.Sprintf("%s:%.1f°C", level, t.Value)
	}
	p.addPower(out)
	writeJSON(w, http.StatusOK, out)
}

func (p *panelAPI) addPower(out map[string]any) {
	states, connected, _ := p.ha.Sync.Snapshot()
	if !connected {
		out["equipment"] = "off:無資料"
		out["watts"] = "off:--"
		out["kwh"] = "off:--"
		out["led"] = "red"
		return
	}
	var mainW, chillerW float64
	var month float64
	equip := "正常"
	for _, strip := range p.ha.Equipment.Snapshot().PowerStrips {
		if v := stateFloat(states[strip.MonthEnergyEntity]); v >= 0 {
			month += v
		}
	}
	for _, dev := range p.ha.Equipment.Snapshot().Devices {
		base := strings.TrimPrefix(dev.SwitchEntity, "switch.")
		watts := stateFloat(states["sensor."+base+"_current_consumption"])
		name := dev.DisplayName
		switchState := states[dev.SwitchEntity].State
		switch {
		case strings.Contains(name, "主馬"):
			mainW = watts
			if watts >= 0 && watts < 20 {
				equip = "主馬停止"
			}
		case strings.Contains(name, "冷水機馬達"):
			if equip == "正常" && watts >= 0 && watts < 5 {
				equip = "冷水泵停"
			}
		case strings.Contains(name, "蛋白機"):
			if equip == "正常" && watts >= 0 && watts < 5 {
				equip = "蛋白機停"
			}
		case strings.Contains(name, "冷水機"):
			chillerW = watts
		}
		if equip == "正常" && (strings.Contains(name, "捲棉") || strings.Contains(name, "補水") || strings.Contains(name, "滴定")) && switchState == "off" {
			equip = "插座關閉"
		}
	}
	level := "ok"
	if equip != "正常" {
		level = "bad"
	}
	out["equipment"] = level + ":" + equip
	out["watts"] = fmt.Sprintf("ok:主馬%.0fW/冷水機%.0fW", mainW, chillerW)
	out["kwh"] = fmt.Sprintf("ok:本%.0f/上-kWh", month)
	out["led"] = "off"
	tempState, _ := out["temperature"].(string)
	if equip != "正常" || strings.HasPrefix(tempState, "bad:") {
		out["led"] = "red"
	} else if wc, ok := out["water_change"].(string); ok && (strings.HasPrefix(wc, "warn:") || strings.HasPrefix(wc, "bad:")) {
		out["led"] = "orange"
	}
}

func stateFloat(s homeassistant.State) float64 {
	v, err := strconv.ParseFloat(s.State, 64)
	if err != nil {
		return -1
	}
	return v
}

func (p *panelAPI) waterChange(w http.ResponseWriter, r *http.Request) {
	id, err := p.db.InsertWaterRecord(context.Background(), storage.WaterRecord{MeasuredAt: time.Now().Format(time.RFC3339), WaterChange: true, Source: "cyd", Note: "CYD 記錄換水"})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "recorded": true})
}
