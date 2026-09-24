package highfreq

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/cp296944/reeftank-hub/internal/equipment"
	"github.com/cp296944/reeftank-hub/internal/storage"
)

type DeviceStatus struct {
	ID         string                        `json:"id"`
	Name       string                        `json:"name"`
	Enabled    bool                          `json:"enabled"`
	Active     bool                          `json:"active"`
	Voltage    float64                       `json:"voltage"`
	Current    float64                       `json:"current"`
	Power      float64                       `json:"power"`
	UpdatedAt  time.Time                     `json:"updated_at"`
	Error      string                        `json:"error,omitempty"`
	TodayCount int                           `json:"today_count"`
	History    []storage.EquipmentDailyCount `json:"history,omitempty"`
}
type detector struct {
	high, low int
	active    bool
	eventID   int64
	started   time.Time
}
type Monitor struct {
	equipment *equipment.Store
	db        *storage.DB
	loc       *time.Location
	client    hs300Client
	mu        sync.RWMutex
	statuses  map[string]DeviceStatus
	detectors map[string]*detector
	childIDs  map[string][]string
}

func New(eq *equipment.Store, db *storage.DB, loc *time.Location) *Monitor {
	return &Monitor{equipment: eq, db: db, loc: loc, client: hs300Client{timeout: 3 * time.Second}, statuses: map[string]DeviceStatus{}, detectors: map[string]*detector{}, childIDs: map[string][]string{}}
}

func (m *Monitor) Run(ctx context.Context) {
	for _, d := range m.equipment.Snapshot().Devices {
		if d.HighFrequency {
			if n, err := m.db.BackfillEquipmentEvents(ctx, d.ID, time.Now().Add(-24*time.Hour)); err != nil {
				slog.Warn("backfill equipment activity", "device", d.ID, "err", err)
			} else if n > 0 {
				slog.Info("backfilled equipment activity", "device", d.ID, "events", n)
			}
		}
	}
	var wg sync.WaitGroup
	for _, strip := range m.equipment.Snapshot().PowerStrips {
		s := strip
		wg.Add(1)
		go func() { defer wg.Done(); m.runStrip(ctx, s.ID) }()
	}
	<-ctx.Done()
	wg.Wait()
}
func (m *Monitor) runStrip(ctx context.Context, stripID string) {
	next := time.Now()
	backoff := time.Duration(0)
	for {
		if wait := time.Until(next); wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		snap := m.equipment.Snapshot()
		var strip equipment.PowerStrip
		found := false
		for _, s := range snap.PowerStrips {
			if s.ID == stripID {
				strip = s
				found = true
				break
			}
		}
		if !found {
			return
		}
		enabled := []equipment.Device{}
		for _, d := range snap.Devices {
			if d.HighFrequency && d.Slot >= strip.SlotStart && d.Slot <= strip.SlotEnd {
				enabled = append(enabled, d)
			}
		}
		interval := time.Duration(strip.PollIntervalSeconds) * time.Second
		if interval < time.Second {
			interval = time.Second
		}
		if len(enabled) == 0 {
			next = time.Now().Add(time.Second)
			continue
		}
		err := m.pollStrip(ctx, strip, enabled)
		if err != nil {
			if backoff == 0 {
				backoff = 5 * time.Second
			} else if backoff < time.Minute {
				backoff *= 3
				if backoff > time.Minute {
					backoff = time.Minute
				}
			}
			slog.Warn("HS300 high-frequency poll", "strip", strip.ID, "err", err)
		} else {
			backoff = 0
		}
		if backoff > interval {
			next = time.Now().Add(backoff)
		} else {
			next = time.Now().Add(interval)
		}
	}
}
func (m *Monitor) pollStrip(ctx context.Context, strip equipment.PowerStrip, devices []equipment.Device) error {
	m.mu.RLock()
	ids := append([]string(nil), m.childIDs[strip.ID]...)
	m.mu.RUnlock()
	var err error
	if len(ids) != 6 {
		ids, err = m.client.childIDs(ctx, strip.Host)
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.childIDs[strip.ID] = ids
		m.mu.Unlock()
	}
	type result struct {
		d equipment.Device
		r outletReading
		e error
	}
	ch := make(chan result, len(devices))
	for _, d := range devices {
		d := d
		go func() {
			idx := d.Slot - strip.SlotStart
			r, e := m.client.energy(ctx, strip.Host, ids[idx])
			ch <- result{d, r, e}
		}()
	}
	var first error
	for range devices {
		x := <-ch
		if x.e != nil {
			m.setError(x.d, x.e.Error())
			if first == nil {
				first = x.e
			}
			continue
		}
		m.accept(ctx, x.d, x.r, time.Now())
	}
	return first
}
func (m *Monitor) setError(d equipment.Device, msg string) {
	m.mu.Lock()
	s := m.statuses[d.ID]
	s.ID = d.ID
	s.Name = d.DisplayName
	s.Enabled = true
	s.Error = msg
	m.statuses[d.ID] = s
	m.mu.Unlock()
}
func (m *Monitor) accept(ctx context.Context, d equipment.Device, r outletReading, at time.Time) {
	_ = m.db.RecordOutletSample(ctx, d.ID, at, r.Voltage, r.Current, r.Power)
	m.mu.Lock()
	det := m.detectors[d.ID]
	if det == nil {
		det = &detector{}
		m.detectors[d.ID] = det
	}
	powered := r.Voltage >= 80 && r.Voltage <= 140
	running := powered && (r.Current >= 0.005 || r.Power >= 0.5)
	if running {
		det.high++
		det.low = 0
	} else {
		det.low++
		det.high = 0
	}
	if !det.active && det.high >= 1 {
		det.active = true
		det.started = at
		id, e := m.db.StartEquipmentEvent(ctx, d.ID, at, r.Current, r.Power)
		if e == nil {
			det.eventID = id
		}
	} else if det.active && running && det.eventID != 0 {
		_ = m.db.UpdateEquipmentEvent(ctx, det.eventID, det.started, at, r.Current, r.Power, false)
	} else if det.active && det.low >= 2 {
		if det.eventID != 0 {
			_ = m.db.UpdateEquipmentEvent(ctx, det.eventID, det.started, at, r.Current, r.Power, true)
		}
		det.active = false
		det.eventID = 0
	}
	active := det.active
	m.mu.Unlock()
	today, _, _ := m.db.EquipmentActivity(ctx, d.ID, 30, m.loc)
	m.mu.Lock()
	m.statuses[d.ID] = DeviceStatus{ID: d.ID, Name: d.DisplayName, Enabled: true, Active: active, Voltage: r.Voltage, Current: r.Current, Power: r.Power, UpdatedAt: at, TodayCount: today}
	m.mu.Unlock()
}

func (m *Monitor) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/high-frequency", func(w http.ResponseWriter, r *http.Request) {
		snap := m.equipment.Snapshot()
		m.mu.RLock()
		statuses := map[string]DeviceStatus{}
		for k, v := range m.statuses {
			statuses[k] = v
		}
		m.mu.RUnlock()
		for _, d := range snap.Devices {
			if !d.HighFrequency {
				continue
			}
			s := statuses[d.ID]
			s.ID = d.ID
			s.Name = d.DisplayName
			s.Enabled = true
			s.TodayCount, s.History, _ = m.db.EquipmentActivity(r.Context(), d.ID, 30, m.loc)
			statuses[d.ID] = s
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": statuses, "updated_at": time.Now()})
	})
}
