package engine

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cp296944/reeftank-hub/internal/k7tcp"
	"github.com/cp296944/reeftank-hub/internal/tally"
)

type fakeProv struct{ s Snapshot }

func (f fakeProv) EngineSnapshot() Snapshot { return f.s }

// fakeLamp records writes so tests can assert whether the engine drove the lamp.
type fakeLamp struct {
	mu     sync.Mutex
	hands  int
	last   [k7tcp.Channels]uint8
	failed bool
}

func (l *fakeLamp) Hand(ch [k7tcp.Channels]uint8) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hands++
	l.last = ch
	if l.failed {
		return errors.New("boom")
	}
	return nil
}
func (l *fakeLamp) SyncTime() error { return nil }
func (l *fakeLamp) count() int      { l.mu.Lock(); defer l.mu.Unlock(); return l.hands }

func constSnap(v int) Snapshot {
	var s Snapshot
	s.AutoMode = true
	s.Config.MasterBrightness = 100
	for h := 0; h < Slots; h++ {
		s.Base[h] = [8]int{h, 0, v, v, v, v, v, v}
	}
	return s
}

func TestSetInterval(t *testing.T) {
	e := New(fakeProv{}, &fakeLamp{}, time.UTC, time.Minute)
	if e.Interval() != time.Minute {
		t.Fatalf("initial interval = %v", e.Interval())
	}
	e.SetInterval(5 * time.Second) // clamped to 15s
	if e.Interval() != 15*time.Second {
		t.Errorf("clamp: got %v want 15s", e.Interval())
	}
	e.SetInterval(3 * time.Minute)
	if e.Interval() != 3*time.Minute {
		t.Errorf("set: got %v", e.Interval())
	}
}

func TestOverride(t *testing.T) {
	e := New(fakeProv{}, &fakeLamp{}, time.UTC, time.Minute)
	if a, _, _ := e.OverrideActive(); a {
		t.Fatal("no override expected initially")
	}
	e.SetOverride(&Override{Channels: [Channels]int{9, 9, 9, 9, 9, 9}, Source: "feed", Until: time.Now().Add(time.Hour)})
	a, src, _ := e.OverrideActive()
	if !a || src != "feed" {
		t.Errorf("override = %v %q", a, src)
	}
	// expired override reads inactive
	e.SetOverride(&Override{Source: "feed", Until: time.Now().Add(-time.Minute)})
	if a, _, _ := e.OverrideActive(); a {
		t.Error("expired override should read inactive")
	}
	e.SetOverride(nil)
	if a, _, _ := e.OverrideActive(); a {
		t.Error("nil override")
	}
}

func TestStepAppliesOverride(t *testing.T) {
	// engine.step with an active override should target + drive the override
	// channels even when the schedule says otherwise, and even when dormant.
	var s Snapshot
	s.AutoMode = true
	lp := &fakeLamp{}
	e := New(fakeProv{s: s}, lp, time.UTC, time.Minute)
	e.SetOverride(&Override{Channels: [Channels]int{5, 4, 3, 2, 1, 0}, Source: "maintenance", Until: time.Now().Add(time.Hour)})
	e.step()
	st := e.Status()
	if st.Source != "maintenance" || st.Target != [Channels]int{5, 4, 3, 2, 1, 0} {
		t.Errorf("status after override step = %+v", st)
	}
	if lp.count() == 0 {
		t.Error("override step must write to the lamp even when not live")
	}
}

func TestStepDormantWhenNotLive(t *testing.T) {
	lp := &fakeLamp{}
	tal := tally.Load(filepath.Join(t.TempDir(), "w.json"), time.UTC)
	e := New(fakeProv{s: constSnap(40)}, lp, time.UTC, time.Minute)
	e.SetTally(tal)
	e.step() // not live, no override
	st := e.Status()
	want := [Channels]int{40, 40, 40, 40, 40, 40}
	if st.Target != want {
		t.Fatalf("target = %v want %v", st.Target, want)
	}
	if lp.count() != 0 {
		t.Errorf("dormant engine must not write to the lamp, wrote %d times", lp.count())
	}
	if st.Sent != want {
		t.Errorf("dormant status should mirror Sent=Target (lamp runs its own schedule), got Sent=%v", st.Sent)
	}
	if a, _, _ := tal.Today(); a != 0 {
		t.Errorf("dormant step counted %d writes, want 0", a)
	}
}

func TestStepDrivesWhenLive(t *testing.T) {
	lp := &fakeLamp{}
	tal := tally.Load(filepath.Join(t.TempDir(), "w.json"), time.UTC)
	e := New(fakeProv{s: constSnap(40)}, lp, time.UTC, time.Minute)
	e.SetTally(tal)
	e.SetLive(true)
	e.step()
	if lp.count() == 0 {
		t.Fatal("live engine must write to the lamp")
	}
	if a, _, _ := tal.Today(); a != 1 {
		t.Errorf("live step counted %d writes, want 1", a)
	}
	e.step() // unchanged output -> no second write
	if lp.count() != 1 {
		t.Errorf("live engine wrote again with no change, count=%d", lp.count())
	}
}

func TestOverrideEndReArmsScheduleWhenDormant(t *testing.T) {
	lp := &fakeLamp{}
	e := New(fakeProv{s: constSnap(30)}, lp, time.UTC, time.Minute)
	var reArmed int
	e.SetRepushFn(func() error { reArmed++; return nil })

	e.SetOverride(&Override{Channels: [Channels]int{9, 9, 9, 9, 9, 9}, Source: "feed", Until: time.Now().Add(time.Hour)})
	e.step() // override active
	if reArmed != 0 {
		t.Fatal("re-arm fired while override still active")
	}
	e.SetOverride(&Override{Source: "feed", Until: time.Now().Add(-time.Minute)}) // expired
	e.step()                                                                      // override just ended
	if reArmed != 1 {
		t.Errorf("expected schedule re-arm once after override ended, got %d", reArmed)
	}
	e.step() // steady dormant
	if reArmed != 1 {
		t.Errorf("re-arm should fire only on the transition, got %d", reArmed)
	}
}

func TestUntilNextDaily(t *testing.T) {
	d := untilNextDaily(4, time.UTC)
	if d <= 0 || d > 24*time.Hour {
		t.Errorf("untilNextDaily(4) = %v, want (0, 24h]", d)
	}
	// the target is exactly 04:00 UTC
	target := time.Now().In(time.UTC).Add(d)
	if target.Hour() != 4 || target.Minute() != 0 {
		t.Errorf("next daily lands at %02d:%02d, want 04:00", target.Hour(), target.Minute())
	}
}
