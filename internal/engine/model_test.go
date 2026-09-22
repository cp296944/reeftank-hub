package engine

import (
	"testing"
	"time"
)

// ramp schedule: channel 0 goes 0 at 00:00 -> 100 at 12:00 -> 0 at 24:00
func rampSchedule() Schedule {
	var s Schedule
	for h := 0; h < Slots; h++ {
		s[h][0] = h
		v := 0
		if h <= 12 {
			v = h * 100 / 12
		} else {
			v = (24 - h) * 100 / 12
		}
		s[h][2] = v
	}
	return s
}

func TestInterpolate(t *testing.T) {
	s := rampSchedule()
	// at 06:30 the ch0 value is between s[6]=50 and s[7]=58 -> 50 + .5*(58-50)=54
	got := interpolate(s, 6, 30)
	if got[0] != 54 {
		t.Errorf("interpolate 06:30 ch0 = %d, want 54", got[0])
	}
	// exactly noon = peak
	if v := interpolate(s, 12, 0)[0]; v != 100 {
		t.Errorf("noon ch0 = %d want 100", v)
	}
	// wrap: 23:30 between s[23]=8 and s[0]=0 -> 4
	if v := interpolate(s, 23, 30)[0]; v != 4 {
		t.Errorf("23:30 ch0 = %d want 4", v)
	}
}

func TestMaster(t *testing.T) {
	c := Config{MasterBrightness: 50}
	ch := [Channels]int{100, 80, 40, 0, 0, 0}
	c.applyMaster(&ch)
	if ch != [Channels]int{50, 40, 20, 0, 0, 0} {
		t.Errorf("applyMaster 50%% = %v", ch)
	}
	// 100 is a no-op, >100 caps at 100
	c2 := Config{MasterBrightness: 200}
	ch2 := [Channels]int{60, 10, 0, 0, 0, 0}
	c2.applyMaster(&ch2)
	if ch2 != [Channels]int{100, 20, 0, 0, 0, 0} {
		t.Errorf("applyMaster 200%% = %v", ch2)
	}
}

func TestAcclimationPercent(t *testing.T) {
	c := Config{}
	c.Acclimation.Enabled = true
	c.Acclimation.StartPercent = 70
	c.Acclimation.DurationDays = 20
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Acclimation.StartEpoch = start.Unix()

	if p := c.AcclimationPercent(start); p != 70 {
		t.Errorf("day 0 = %d want 70", p)
	}
	// day 10 of 20 -> 70 + 30*0.5 = 85
	if p := c.AcclimationPercent(start.Add(10 * 24 * time.Hour)); p != 85 {
		t.Errorf("day 10 = %d want 85", p)
	}
	// past the end -> 100
	if p := c.AcclimationPercent(start.Add(40 * 24 * time.Hour)); p != 100 {
		t.Errorf("day 40 = %d want 100", p)
	}
	// disabled -> 100
	c.Acclimation.Enabled = false
	if p := c.AcclimationPercent(start); p != 100 {
		t.Errorf("disabled = %d want 100", p)
	}
}

func TestSeasonalShift(t *testing.T) {
	c := Config{}
	c.Seasonal.Enabled = true
	c.Seasonal.MaxShiftMinutes = 60
	// day 172 (~June 21) is the cos() anchor: cos(0)=1 -> +maxShift
	jun21 := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	if s := c.SeasonalShiftMinutes(jun21); s < 58 {
		t.Errorf("day-172 shift = %d, want ~+60", s)
	}
	// ~half a year later -> near -60
	dec := time.Date(2026, 12, 21, 12, 0, 0, 0, time.UTC)
	if s := c.SeasonalShiftMinutes(dec); s > -58 {
		t.Errorf("mid-winter shift = %d, want ~-60", s)
	}
}

func TestSiestaActive(t *testing.T) {
	c := Config{}
	c.Siesta.Enabled = true
	c.Siesta.Start = "13:00"
	c.Siesta.DurationMins = 90
	at := func(h, m int) time.Time { return time.Date(2026, 3, 1, h, m, 0, 0, time.UTC) }
	if !c.siestaActive(at(13, 30)) {
		t.Error("13:30 should be in siesta")
	}
	if c.siestaActive(at(15, 0)) {
		t.Error("15:00 should be past siesta (13:00+90=14:30)")
	}
	if c.siestaActive(at(12, 0)) {
		t.Error("12:00 before siesta")
	}
}

func TestMoon(t *testing.T) {
	// 2000-01-06 18:14 UTC is the anchored new moon
	newMoon := time.Unix(refNewMoon, 0).UTC()
	if p := MoonPhase(newMoon); p > 0.01 && p < 0.99 {
		t.Errorf("anchor phase = %.3f, want ~0", p)
	}
	if i := MoonIllumination(newMoon); i > 0.02 {
		t.Errorf("anchor illumination = %.3f want ~0", i)
	}
	var synHalf float64 = synodicDays / 2 // runtime var → allows truncation
	full := newMoon.Add(time.Duration(int64(synHalf*86400)) * time.Second)
	if i := MoonIllumination(full); i < 0.98 {
		t.Errorf("half-synodic illumination = %.3f want ~1", i)
	}
}

func TestComputeManualVsAuto(t *testing.T) {
	c := Config{Device: "k7pro", MasterBrightness: 100}
	s := rampSchedule()
	manual := [Channels]int{10, 20, 30, 40, 50, 60}

	m := c.Compute(s, manual, false, time.Now())
	if m.Source != "manual" || m.Channels != manual {
		t.Errorf("manual compute = %+v", m)
	}

	noon := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	a := c.Compute(s, manual, true, noon)
	if a.Source != "auto" || a.Channels[0] != 100 {
		t.Errorf("auto compute at noon = %+v, want ch0=100", a)
	}
}

func TestComputeLunarOverlay(t *testing.T) {
	c := Config{Device: "k7mini"}
	c.MasterBrightness = 100
	c.Lunar.Enabled = true
	c.Lunar.Start = "18:00"
	c.Lunar.End = "06:00"
	c.Lunar.MaxIntensity = 20
	c.Lunar.DayThreshold = 2

	var dark Schedule // all zero -> night, lunar allowed
	for h := 0; h < Slots; h++ {
		dark[h][0] = h
	}
	// pick a time that is night AND a moon time with decent illumination
	night := time.Date(2026, 1, 30, 23, 0, 0, 0, time.UTC) // ~ full-ish moon
	out := c.Compute(dark, [Channels]int{}, true, night)
	if out.Source != "lunar" {
		t.Fatalf("expected lunar source, got %q (illum=%.2f)", out.Source, MoonIllumination(night))
	}
	if out.Channels[1] == 0 {
		t.Errorf("royal blue should be lifted by moonlight, got %v", out.Channels)
	}
}
