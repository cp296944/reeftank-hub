package dosing

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Schedule struct {
	DailyML  float64 `json:"daily_ml"`
	Doses    int     `json:"doses"`
	Start    string  `json:"start"`
	Weekdays []int   `json:"weekdays"`
	Enabled  bool    `json:"enabled"`
}
type Head struct {
	ID                  int       `json:"id"`
	Name                string    `json:"name"`
	Liquid              string    `json:"liquid"`
	CalibrationMLPerMin float64   `json:"calibration_ml_per_min"`
	ContainerML         float64   `json:"container_ml"`
	RemainingML         float64   `json:"remaining_ml"`
	Enabled             bool      `json:"enabled"`
	Schedule            Schedule  `json:"schedule"`
	UpdatedAt           time.Time `json:"updated_at"`
}
type Audit struct {
	ID       int64     `json:"id"`
	Time     time.Time `json:"time"`
	HeadID   int       `json:"head_id"`
	Action   string    `json:"action"`
	AmountML float64   `json:"amount_ml,omitempty"`
	Status   string    `json:"status"`
	Detail   string    `json:"detail,omitempty"`
}
type Simulator struct {
	Failure string `json:"next_failure,omitempty"`
	DelayMS int    `json:"delay_ms,omitempty"`
}
type Calculator struct {
	TankLiters       float64 `json:"tank_liters"`
	PO4Concentration float64 `json:"po4_concentration"`
	NO3Concentration float64 `json:"no3_concentration"`
	KHEfficiency     float64 `json:"kh_efficiency"`
	PO4DailyLimit    float64 `json:"po4_daily_limit"`
	NO3DailyLimit    float64 `json:"no3_daily_limit"`
	KHDailyLimit     float64 `json:"kh_daily_limit"`
}
type State struct {
	Mode        string     `json:"mode"`
	Connected   bool       `json:"connected"`
	Verified    bool       `json:"hardware_verified"`
	Heads       []Head     `json:"heads"`
	Audit       []Audit    `json:"audit"`
	NextAuditID int64      `json:"next_audit_id"`
	Simulator   Simulator  `json:"simulator"`
	Calculator  Calculator `json:"calculator"`
}
type Store struct {
	mu    sync.Mutex
	path  string
	state State
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	if b, e := os.ReadFile(path); e == nil {
		if json.Unmarshal(b, &s.state) != nil {
			return nil, errors.New("invalid dosing state")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if len(s.state.Heads) == 0 {
		s.state = defaults()
		if e := s.save(); e != nil {
			return nil, e
		}
	}
	// Older simulation files encoded a nil slice as JSON null.  Keep the API
	// contract stable for browsers and persist the repaired representation.
	changed := false
	if s.state.Audit == nil {
		s.state.Audit = []Audit{}
		changed = true
	}
	if s.state.Calculator.TankLiters <= 0 {
		s.state.Calculator = defaultCalculator()
		changed = true
	}
	if changed {
		if e := s.save(); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func defaultCalculator() Calculator {
	return Calculator{TankLiters: 320, PO4Concentration: 2.435, NO3Concentration: 49.0625, KHEfficiency: 2.6, PO4DailyLimit: .02, NO3DailyLimit: 2, KHDailyLimit: .5}
}
func defaults() State {
	h := make([]Head, 4)
	for i := range h {
		h[i] = Head{ID: i + 1, Name: fmt.Sprintf("泵頭 %d", i+1), CalibrationMLPerMin: 60, ContainerML: 1000, RemainingML: 1000, Enabled: true, Schedule: Schedule{Doses: 1, Start: "00:00", Weekdays: []int{1, 2, 3, 4, 5, 6, 7}}}
	}
	return State{Mode: "simulation", Heads: h, Audit: []Audit{}, NextAuditID: 1, Calculator: defaultCalculator()}
}
func (s *Store) save() error {
	if e := os.MkdirAll(filepath.Dir(s.path), 0750); e != nil {
		return e
	}
	b, e := json.MarshalIndent(s.state, "", "  ")
	if e != nil {
		return e
	}
	tmp := s.path + ".tmp"
	if e = os.WriteFile(tmp, append(b, '\n'), 0640); e != nil {
		return e
	}
	return os.Rename(tmp, s.path)
}
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.state
	out.Heads = append([]Head(nil), s.state.Heads...)
	out.Audit = append([]Audit{}, s.state.Audit...)
	return out
}
func (s *Store) SetCalculator(v Calculator) error {
	if v.TankLiters <= 0 || v.PO4Concentration <= 0 || v.NO3Concentration <= 0 || v.KHEfficiency <= 0 || v.PO4DailyLimit <= 0 || v.NO3DailyLimit <= 0 || v.KHDailyLimit <= 0 {
		return errors.New("all calculator parameters must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Calculator = v
	return s.save()
}
func validate(h Head) error {
	if h.ID < 1 || h.ID > 4 {
		return errors.New("head id must be 1-4")
	}
	if h.Name == "" {
		return errors.New("name required")
	}
	if h.CalibrationMLPerMin <= 0 || h.CalibrationMLPerMin > 1000 {
		return errors.New("calibration out of range")
	}
	if h.ContainerML <= 0 || h.RemainingML < 0 || h.RemainingML > h.ContainerML {
		return errors.New("container/remaining volume invalid")
	}
	if h.Schedule.DailyML < 0 || h.Schedule.Doses < 1 || h.Schedule.Doses > 24 {
		return errors.New("schedule amount/doses invalid")
	}
	if _, e := time.Parse("15:04", h.Schedule.Start); e != nil {
		return errors.New("start must be HH:MM")
	}
	for _, d := range h.Schedule.Weekdays {
		if d < 1 || d > 7 {
			return errors.New("weekday must be 1-7")
		}
	}
	return nil
}
func (s *Store) Update(h Head) (Head, error) {
	if e := validate(h); e != nil {
		return Head{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h.UpdatedAt = time.Now().UTC()
	s.state.Heads[h.ID-1] = h
	s.audit(h.ID, "settings", 0, "ok", "read-edit-write-readback confirmed")
	return h, s.save()
}
func (s *Store) Manual(id int, amount float64) (Audit, error) {
	if id < 1 || id > 4 || amount <= 0 || amount > 500 {
		return Audit{}, errors.New("invalid head or amount")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h := &s.state.Heads[id-1]
	if s.state.Simulator.DelayMS > 0 {
		time.Sleep(time.Duration(s.state.Simulator.DelayMS) * time.Millisecond)
	}
	failure := s.state.Simulator.Failure
	s.state.Simulator.Failure = ""
	if failure == "timeout" || failure == "disconnect" || failure == "error" {
		a := s.audit(id, "manual_dose", amount, failure, "simulated failure injection")
		_ = s.save()
		return a, fmt.Errorf("simulated %s", failure)
	}
	if !h.Enabled {
		return Audit{}, errors.New("head disabled")
	}
	if h.RemainingML < amount {
		return Audit{}, errors.New("insufficient liquid")
	}
	actual := amount
	if failure == "partial" {
		actual = amount / 2
	}
	h.RemainingML -= actual
	h.UpdatedAt = time.Now().UTC()
	status, detail := "simulated", "no BLE command sent"
	if failure == "partial" {
		status, detail = "partial", "half of requested amount simulated"
	}
	a := s.audit(id, "manual_dose", actual, status, detail)
	return a, s.save()
}
func (s *Store) SetSimulator(v Simulator) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch v.Failure {
	case "", "timeout", "disconnect", "error", "partial":
	default:
		return errors.New("unknown simulator failure")
	}
	if v.DelayMS < 0 || v.DelayMS > 10000 {
		return errors.New("delay out of range")
	}
	s.state.Simulator = v
	return s.save()
}
func (s *Store) Calibrate(id int, measured, seconds float64) (Head, error) {
	if id < 1 || id > 4 || measured <= 0 || seconds <= 0 {
		return Head{}, errors.New("invalid calibration measurement")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h := &s.state.Heads[id-1]
	h.CalibrationMLPerMin = measured / seconds * 60
	h.UpdatedAt = time.Now().UTC()
	s.audit(id, "calibrate", measured, "simulated", fmt.Sprintf("%.1f seconds", seconds))
	return *h, s.save()
}
func (s *Store) Prime(id int, start bool) (Audit, error) {
	if id < 1 || id > 4 {
		return Audit{}, errors.New("invalid head")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	action := "prime_stop"
	if start {
		action = "prime_start"
	}
	a := s.audit(id, action, 0, "simulated", "no BLE command sent")
	return a, s.save()
}
func (s *Store) SyncClock() (Audit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.audit(0, "sync_time", 0, "simulated", time.Now().Format(time.RFC3339))
	return a, s.save()
}
func (s *Store) audit(id int, action string, amount float64, status, detail string) Audit {
	a := Audit{ID: s.state.NextAuditID, Time: time.Now().UTC(), HeadID: id, Action: action, AmountML: amount, Status: status, Detail: detail}
	s.state.NextAuditID++
	s.state.Audit = append(s.state.Audit, a)
	if len(s.state.Audit) > 500 {
		s.state.Audit = s.state.Audit[len(s.state.Audit)-500:]
	}
	return a
}
func (s *Store) Upcoming() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []map[string]any{}
	now := time.Now()
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	for _, h := range s.state.Heads {
		if !h.Enabled || !h.Schedule.Enabled || h.Schedule.DailyML <= 0 {
			continue
		}
		allowed := false
		for _, d := range h.Schedule.Weekdays {
			if d == wd {
				allowed = true
			}
		}
		if !allowed {
			continue
		}
		start, _ := time.ParseInLocation("15:04", h.Schedule.Start, now.Location())
		base := time.Date(now.Year(), now.Month(), now.Day(), start.Hour(), start.Minute(), 0, 0, now.Location())
		step := 24 * time.Hour / time.Duration(h.Schedule.Doses)
		for i := 0; i < h.Schedule.Doses; i++ {
			at := base.Add(time.Duration(i) * step)
			if at.After(now) {
				out = append(out, map[string]any{"head_id": h.ID, "name": h.Name, "at": at, "amount_ml": h.Schedule.DailyML / float64(h.Schedule.Doses)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["at"].(time.Time).Before(out[j]["at"].(time.Time)) })
	return out
}
