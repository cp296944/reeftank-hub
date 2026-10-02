package highfreq

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
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
type outletClient interface {
	childIDs(context.Context, string) ([]string, error)
	energy(context.Context, string, string) (outletReading, error)
}
type Monitor struct {
	equipment *equipment.Store
	db        *storage.DB
	loc       *time.Location
	client    outletClient
	mu        sync.RWMutex
	statuses  map[string]DeviceStatus
	childIDs  map[string][]string
}

func New(eq *equipment.Store, db *storage.DB, loc *time.Location) *Monitor {
	return &Monitor{equipment: eq, db: db, loc: loc, client: hs300Client{timeout: 3 * time.Second}, statuses: map[string]DeviceStatus{}, childIDs: map[string][]string{}}
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
			if !d.HighFrequency {
				continue
			}
			physical, _, err := equipment.PhysicalOutlet(snap, d)
			if err != nil {
				m.setError(d, err.Error())
				continue
			}
			if physical.ID == strip.ID {
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
			physical, idx, e := equipment.PhysicalOutlet(m.equipment.Snapshot(), d)
			if e != nil || physical.ID != strip.ID || idx < 0 || idx >= len(ids) {
				ch <- result{d: d, e: fmt.Errorf("cannot resolve physical outlet for %s", d.SwitchEntity)}
				return
			}
			r, e := m.client.energy(ctx, strip.Host, ids[idx])
			ch <- result{d, r, e}
		}()
	}
	var first error
	for range devices {
		x := <-ch
		// A save can happen while a network request is in flight. Discard the
		// old binding's result instead of attaching it to the new assignment.
		current := false
		for _, latest := range m.equipment.Snapshot().Devices {
			if latest.ID == x.d.ID && latest.SwitchEntity == x.d.SwitchEntity && latest.HighFrequency {
				current = true
				break
			}
		}
		if !current {
			continue
		}
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
	active, _, err := m.db.RecordEquipmentReading(ctx, d.ID, d.SwitchEntity, at, r.Voltage, r.Current, r.Power)
	if err != nil {
		m.setError(d, "activity storage: "+err.Error())
		return
	}
	today, _, err := m.db.EquipmentActivity(ctx, d.ID, 30, m.loc)
	if err != nil {
		m.setError(d, "activity history: "+err.Error())
		return
	}
	m.mu.Lock()
	m.statuses[d.ID] = DeviceStatus{ID: d.ID, Name: d.DisplayName, Enabled: true, Active: active, Voltage: r.Voltage, Current: r.Current, Power: r.Power, UpdatedAt: at, TodayCount: today}
	m.mu.Unlock()
}

func (m *Monitor) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/high-frequency", func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if days < 0 || days > 36500 {
			days = 30
		}
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
			s.TodayCount, s.History, _ = m.db.EquipmentActivity(r.Context(), d.ID, days, m.loc)
			statuses[d.ID] = s
		}
		events := map[string][]storage.EquipmentEvent{}
		for _, id := range []string{"outlet_09", "outlet_10"} {
			events[id], _ = m.db.EquipmentEvents(r.Context(), id, days, 1000, m.loc)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"devices": statuses, "events": events, "days": days, "updated_at": time.Now()})
	})
}
