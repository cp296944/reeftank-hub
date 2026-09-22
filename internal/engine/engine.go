package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/cp296944/reeftank-hub/internal/k7tcp"
	"github.com/cp296944/reeftank-hub/internal/tally"
)

// Snapshot is the engine's authoritative input for one tick — the base
// schedule the user pushed plus all effect settings.
type Snapshot struct {
	Base     Schedule
	Manual   [Channels]int
	AutoMode bool
	Config   Config
}

// Provider hands the engine a fresh Snapshot each tick (implemented by the
// httpapi store).
type Provider interface {
	EngineSnapshot() Snapshot
}

// Lamp is the subset of *lamp.Lamp the engine drives (satisfied by *lamp.Lamp;
// a fake stands in for tests).
type Lamp interface {
	Hand(ch [k7tcp.Channels]uint8) error
	SyncTime() error
}

// Override is a timed full-output replacement (Feed / Maintenance). Nil = none.
type Override struct {
	Channels [Channels]int
	Source   string
	Until    time.Time
}

// OutputStatus is what /api/output/status returns (mirrors Effects.h).
type OutputStatus struct {
	Target      [Channels]int `json:"target"`
	Sent        [Channels]int `json:"sent"`
	TargetMs    int64         `json:"target_ms"`
	SentMs      int64         `json:"sent_ms"`
	LastWriteOK bool          `json:"last_write_ok"`
	Source      string        `json:"source"`
}

type Engine struct {
	prov Provider
	lamp Lamp
	tz   *time.Location

	mu          sync.Mutex
	status      OutputStatus
	lastSent    [Channels]int
	haveSent    bool
	override    *Override
	overrideWas bool          // was an override active on the previous tick?
	interval    time.Duration // live-tunable (smooth ramp)
	lastPush    time.Time

	// live is true only while smooth-ramp is on: the engine is then the live
	// driver, pushing interpolated output to the lamp every tick. When false
	// the engine is dormant — the lamp runs the 24-slot 0x1007 schedule that
	// /api/push last wrote — except that Feed/Maintenance overrides still work.
	live bool
	// repushFn re-arms the lamp's own schedule (re-sends 0x1007). Called when
	// the engine stops being the live driver or a timed override ends while
	// dormant, so the lamp resumes autonomous scheduling. Set by main.
	repushFn func() error

	// shared, restart-surviving lamp-write tally; the engine bumps the auto side.
	tally *tally.Counter

	// driftCheck, when set, is run after each maintenance-pass time sync: it
	// reads the lamp's stored schedule, compares it to what pi-bridge expects,
	// and re-pushes if they differ. Returns true if it re-pushed. Set by main.
	driftCheck func() bool

	tickNow  chan struct{}
	reticker chan struct{}
	maintNow chan struct{} // fire a time-sync (+ drift check) now
}

func New(prov Provider, l Lamp, tz *time.Location, interval time.Duration) *Engine {
	if tz == nil {
		tz = time.Local
	}
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Engine{
		prov: prov, lamp: l, tz: tz, interval: interval,
		tickNow:  make(chan struct{}, 1),
		reticker: make(chan struct{}, 1),
		maintNow: make(chan struct{}, 1),
	}
}

// Kick forces an immediate recompute+push (call after a user push / mode change).
func (e *Engine) Kick() {
	select {
	case e.tickNow <- struct{}{}:
	default:
	}
}

// SetInterval retunes the tick cadence at runtime (smooth ramp on/off).
func (e *Engine) SetInterval(d time.Duration) {
	if d < 15*time.Second {
		d = 15 * time.Second
	}
	e.mu.Lock()
	changed := e.interval != d
	e.interval = d
	e.mu.Unlock()
	if changed {
		slog.Info("engine interval changed", "interval", d)
		select {
		case e.reticker <- struct{}{}:
		default:
		}
	}
}

// Interval returns the current tick cadence.
func (e *Engine) Interval() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.interval
}

// LastPush is when the engine last successfully wrote to the lamp.
func (e *Engine) LastPush() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastPush
}

// SetLive switches the engine between live-driving (smooth ramp on: push
// interpolated output every tick) and dormant (smooth ramp off: the lamp runs
// its own pushed 0x1007 schedule; the engine only steps in for Feed/Maintenance
// overrides). Kicks a tick on change.
func (e *Engine) SetLive(on bool) {
	e.mu.Lock()
	changed := e.live != on
	e.live = on
	if changed {
		e.haveSent = false // force a fresh send when live-driving resumes
	}
	e.mu.Unlock()
	if changed {
		slog.Info("engine live-driving changed", "live", on)
		e.Kick()
	}
}

// Live reports whether the engine is currently the live driver.
func (e *Engine) Live() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.live
}

// SetRepushFn wires the callback that re-arms the lamp's own schedule.
func (e *Engine) SetRepushFn(fn func() error) {
	e.mu.Lock()
	e.repushFn = fn
	e.mu.Unlock()
}

// SetDriftCheck wires the post-time-sync "does the lamp still hold the schedule
// we expect" check.
func (e *Engine) SetDriftCheck(fn func() bool) {
	e.mu.Lock()
	e.driftCheck = fn
	e.mu.Unlock()
}

// MaintNow triggers a lamp time-sync (+ drift check) as soon as the tick loop
// can — used when the lamp link recovers, so a power-cycled lamp gets its clock
// back within seconds instead of at the next daily pass.
func (e *Engine) MaintNow() {
	select {
	case e.maintNow <- struct{}{}:
	default:
	}
}

// SetTally wires the shared restart-surviving lamp-write counter.
func (e *Engine) SetTally(t *tally.Counter) {
	e.mu.Lock()
	e.tally = t
	e.mu.Unlock()
}

// SetOverride installs or clears a timed full-output override.
func (e *Engine) SetOverride(o *Override) {
	e.mu.Lock()
	e.override = o
	e.mu.Unlock()
	e.Kick()
}

func (e *Engine) OverrideActive() (bool, string, time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.override == nil || time.Now().After(e.override.Until) {
		return false, "", time.Time{}
	}
	return true, e.override.Source, e.override.Until
}

func (e *Engine) Status() OutputStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// ClockSane reports whether the host clock looks set (post-2023).
func ClockSane() bool { return time.Now().Unix() > 1700000000 }

func (e *Engine) Run(ctx context.Context) {
	slog.Info("engine started", "interval", e.interval, "tz", e.tz.String())
	// prime the lamp clock, then tick
	e.maintenance()
	e.step()

	t := time.NewTicker(e.Interval())
	defer t.Stop()
	// One clock re-sync (+ drift check) a day, at ~04:00 local — the tank is
	// dark and nobody's watching. /api/push already bundles the time, and
	// MaintNow() handles a lamp reconnect, so this is just a slow drift guard.
	mt := time.NewTimer(untilNextDaily(4, e.tz))
	defer mt.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.step()
		case <-e.tickNow:
			e.step()
		case <-e.reticker:
			t.Reset(e.Interval())
		case <-mt.C:
			e.maintenance()
			mt.Reset(untilNextDaily(4, e.tz))
		case <-e.maintNow:
			e.maintenance()
		}
	}
}

// maintenance re-syncs the lamp clock, then (if wired) checks the lamp still
// holds the schedule pi-bridge expects and re-pushes if it drifted.
func (e *Engine) maintenance() {
	if err := e.lamp.SyncTime(); err != nil {
		slog.Warn("engine: lamp time sync failed", "err", err)
		return
	}
	slog.Debug("engine: lamp clock synced")
	e.mu.Lock()
	dc := e.driftCheck
	e.mu.Unlock()
	if dc != nil && dc() {
		slog.Info("engine: lamp schedule had drifted from expected — re-pushed")
	}
}

// untilNextDaily returns the duration to the next occurrence of `hour`:00 local.
func untilNextDaily(hour int, tz *time.Location) time.Duration {
	now := time.Now().In(tz)
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, tz)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return time.Until(next)
}

func (e *Engine) step() {
	now := time.Now().In(e.tz)
	if !ClockSane() {
		slog.Warn("engine: host clock not set, skipping tick")
		return
	}

	e.mu.Lock()
	ov := e.override
	live := e.live
	wasOverride := e.overrideWas
	repush := e.repushFn
	e.mu.Unlock()

	overrideActive := ov != nil && now.Before(ov.Until)

	var out Output
	if overrideActive {
		out = Output{Channels: ov.Channels, Source: ov.Source}
	} else {
		snap := e.prov.EngineSnapshot()
		out = snap.Config.Compute(snap.Base, snap.Manual, snap.AutoMode, now)
	}

	overrideJustEnded := wasOverride && !overrideActive
	e.mu.Lock()
	e.overrideWas = overrideActive
	e.mu.Unlock()

	e.mu.Lock()
	e.status.Target = out.Channels
	e.status.TargetMs = now.UnixMilli()
	e.status.Source = out.Source

	// A timed override (Feed/Maintenance) just ended while the engine is not the
	// live driver: re-arm the lamp's own 0x1007 schedule so it resumes its
	// autonomous curve instead of holding the last override value, then stay
	// dormant.
	if overrideJustEnded && !live {
		e.status.Sent = out.Channels
		e.status.SentMs = now.UnixMilli()
		e.status.LastWriteOK = true
		e.haveSent = false
		e.mu.Unlock()
		if repush != nil {
			if err := repush(); err != nil {
				slog.Warn("engine: re-arm lamp schedule after override failed", "err", err)
			} else {
				slog.Info("engine: override ended, re-armed lamp schedule")
			}
		}
		return
	}

	// Decide whether to actively write this tick:
	//   override active           -> yes (Feed/Maintenance must reach the lamp)
	//   live driver (smooth ramp)  -> yes, on change
	//   otherwise                  -> no; the lamp runs its own pushed schedule
	drive := overrideActive || live
	if !drive {
		// dormant: the lamp runs its own pushed 0x1007 schedule, so the
		// computed target is (as far as we know) what it is showing.
		e.status.Sent = out.Channels
		e.status.SentMs = now.UnixMilli()
		e.status.LastWriteOK = true
		e.haveSent = false
		e.mu.Unlock()
		return
	}
	changed := overrideJustEnded || !e.haveSent || out.Channels != e.lastSent
	e.mu.Unlock()
	if !changed {
		return
	}

	err := e.lamp.Hand(toU8(out.Channels))
	e.mu.Lock()
	e.status.LastWriteOK = err == nil
	tal := e.tally
	if err == nil {
		e.status.Sent = out.Channels
		e.status.SentMs = time.Now().UnixMilli()
		e.lastSent = out.Channels
		e.haveSent = true
		e.lastPush = time.Now()
	}
	e.mu.Unlock()
	if err != nil {
		slog.Warn("engine: lamp write failed", "err", err)
	} else {
		if tal != nil {
			tal.AddAuto()
		}
		slog.Debug("engine: pushed", "src", out.Source, "ch", out.Channels)
	}
}

func toU8(in [Channels]int) [k7tcp.Channels]uint8 {
	var out [k7tcp.Channels]uint8
	for i := 0; i < Channels && i < k7tcp.Channels; i++ {
		v := in[i]
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		out[i] = uint8(v)
	}
	return out
}
