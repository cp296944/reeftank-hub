// Package engine is the Go port of arduino/src/Effects.cpp — the always-on
// lighting engine. model.go holds the pure computation (no I/O): given a base
// schedule, the wall clock, and the effect configs, it produces the 6 channel
// values that should be on the lamp right now.
//
// It mirrors Effects.cpp exactly so golden-vector tests can pin it against the
// firmware. Channel values are percentages 0..100 (the K7 wire protocol is
// 0..255 but the firmware and UI work in 0..100 and the lamp scales).
package engine

import (
	"fmt"
	"math"
	"time"
)

const (
	Channels = 6
	Slots    = 24
)

// Schedule is 24 hourly rows. Each row is [hour, minute, c0..c5] like the wire
// format; only columns 2..7 (the 6 channels) carry light values 0..100.
type Schedule [Slots][8]int

// Config is the full set of effect settings (mirrors the *Config structs in
// Effects.h). Times are "HH:MM".
type Config struct {
	Device string // "k7mini" | "k7pro"

	MasterBrightness     int // 0..200
	ScheduleShiftMinutes int // whole-schedule UI shift

	Lunar struct {
		Enabled       bool
		Start, End    string
		ClampStart    string
		ClampEnd      string
		MaxIntensity  int
		DayThreshold  int
		TrackMoonrise bool
	}
	Siesta struct {
		Enabled      bool
		Start        string
		DurationMins int
		Intensity    int // percent reduction
	}
	Acclimation struct {
		Enabled      bool
		StartPercent int
		DurationDays int
		StartEpoch   int64
	}
	Seasonal struct {
		Enabled         bool
		MaxShiftMinutes int
	}
}

// ---- interpolation --------------------------------------------------------

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func roundi(f float64) int { return int(math.Round(f)) }

// interpolate linearly blends channel values between hourly points, exactly
// like Effects.cpp interpolateChannels(sched, h, m).
func interpolate(s Schedule, h, m int) [Channels]int {
	lo := s[h%Slots]
	hi := s[(h+1)%Slots]
	frac := float64(m) / 60.0
	var out [Channels]int
	for i := 0; i < Channels; i++ {
		v := float64(lo[2+i]) + frac*float64(hi[2+i]-lo[2+i])
		out[i] = clamp(roundi(v), 0, 100)
	}
	return out
}

func wrapMinutes(mins int) int {
	mins %= 1440
	if mins < 0 {
		mins += 1440
	}
	return mins
}

func parseHHMM(s string) int {
	var h, m int
	if n, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || n != 2 {
		return -1
	}
	return h*60 + m
}

func windowLen(start, end int) int {
	l := end - start
	if l <= 0 {
		l += 1440
	}
	return l
}

func inTimeWindow(nowMins, start, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return nowMins >= start && nowMins < end
	}
	return nowMins >= start || nowMins < end
}

func dayOfYear(t time.Time) int { return t.YearDay() }

// ---- acclimation / seasonal --------------------------------------------------

// AcclimationPercent mirrors Effects.cpp acclimationPercentNow.
func (c Config) AcclimationPercent(now time.Time) int {
	a := c.Acclimation
	if !a.Enabled || a.StartPercent >= 100 || a.DurationDays <= 0 {
		return 100
	}
	if a.StartEpoch == 0 {
		return a.StartPercent
	}
	elapsedDays := int((now.Unix() - a.StartEpoch) / 86400)
	if elapsedDays <= 0 {
		return a.StartPercent
	}
	if elapsedDays >= a.DurationDays {
		return 100
	}
	frac := float64(elapsedDays) / float64(a.DurationDays)
	return clamp(roundi(float64(a.StartPercent)+float64(100-a.StartPercent)*frac), 1, 100)
}

// SeasonalShiftMinutes mirrors Effects.cpp seasonalShiftMinutesNow.
func (c Config) SeasonalShiftMinutes(now time.Time) int {
	s := c.Seasonal
	if !s.Enabled {
		return 0
	}
	maxShift := clamp(s.MaxShiftMinutes, 0, 180)
	if maxShift == 0 {
		return 0
	}
	angle := 2.0 * math.Pi * float64(dayOfYear(now)-172) / 365.2422
	return roundi(math.Cos(angle) * float64(maxShift))
}

// EffectiveSchedule mirrors Effects.cpp rebuildEffectiveSchedule: applies the
// seasonal + UI time shift (by resampling) and the acclimation brightness scale.
func (c Config) EffectiveSchedule(base Schedule, now time.Time) Schedule {
	shift := c.SeasonalShiftMinutes(now) + c.ScheduleShiftMinutes
	acPct := c.AcclimationPercent(now)
	var eff Schedule
	for h := 0; h < Slots; h++ {
		sample := wrapMinutes(h*60 - shift)
		ch := interpolate(base, sample/60, sample%60)
		eff[h][0] = base[h][0]
		eff[h][1] = base[h][1]
		for c := 0; c < Channels; c++ {
			eff[h][2+c] = clamp(roundi(float64(ch[c])*float64(acPct)/100.0), 0, 100)
		}
		for c := 2 + Channels; c < 8; c++ {
			eff[h][c] = base[h][c]
		}
	}
	return eff
}

// ---- overlays ------------------------------------------------------------

func (c Config) applyMaster(ch *[Channels]int) {
	if c.MasterBrightness == 100 {
		return
	}
	for i := 0; i < Channels; i++ {
		v := roundi(float64(ch[i]) * float64(c.MasterBrightness) / 100.0)
		if v > 100 {
			v = 100
		}
		ch[i] = v
	}
}

func (c Config) siestaActive(now time.Time) bool {
	if !c.Siesta.Enabled {
		return false
	}
	s := parseHHMM(c.Siesta.Start)
	if s < 0 {
		s = 13 * 60
	}
	s = wrapMinutes(s + c.SeasonalShiftMinutes(now) + c.ScheduleShiftMinutes)
	dur := c.Siesta.DurationMins
	if dur < 1 {
		dur = 1
	}
	nowMins := now.Hour()*60 + now.Minute()
	return inTimeWindow(nowMins, s, wrapMinutes(s+dur))
}

func (c Config) applySiesta(ch *[Channels]int, now time.Time) {
	if !c.siestaActive(now) {
		return
	}
	depth := clamp(c.Siesta.Intensity, 0, 100)
	if depth == 0 {
		return
	}
	factor := 100 - depth
	for i := 0; i < Channels; i++ {
		ch[i] = clamp(roundi(float64(ch[i])*float64(factor)/100.0), 0, 100)
	}
}

// LunarWindow mirrors Effects.cpp lunarWindowNow (start,end minutes).
func (c Config) LunarWindow(now time.Time) (start, end int) {
	rs := parseHHMM(c.Lunar.Start)
	re := parseHHMM(c.Lunar.End)
	if rs < 0 {
		rs = 18*60 + 30
	}
	if re < 0 {
		re = 6*60 + 30
	}
	wl := windowLen(rs, re)
	shift := 0
	if c.Lunar.TrackMoonrise {
		delta := MoonPhase(now) - 0.5
		if delta < -0.5 {
			delta += 1.0
		}
		if delta >= 0.5 {
			delta -= 1.0
		}
		shift = roundi(delta * 1440.0)
	}
	start = wrapMinutes(rs + shift)
	end = wrapMinutes(start + wl)
	if c.Lunar.TrackMoonrise {
		cs := parseHHMM(c.Lunar.ClampStart)
		ce := parseHHMM(c.Lunar.ClampEnd)
		if cs < 0 {
			cs = 18 * 60
		}
		if ce < 0 {
			ce = 8 * 60
		}
		if ns, ne, ok := clampWindowToNight(start, end, cs, ce); ok {
			start, end = ns, ne
		} else {
			end = start
		}
	}
	return start, end
}

func clampWindowToNight(start, end, clampStart, clampEnd int) (int, int, bool) {
	rawLen := windowLen(start, end)
	clampLen := windowLen(clampStart, clampEnd)
	bestOverlap, bestStart, bestEnd := -1, 0, 0
	for rawShift := -1440; rawShift <= 1440; rawShift += 1440 {
		ss := start + rawShift
		se := ss + rawLen
		for cShift := 0; cShift <= 1440; cShift += 1440 {
			cs := clampStart + cShift
			ce := cs + clampLen
			os := maxi(ss, cs)
			oe := mini(se, ce)
			if oe-os > bestOverlap {
				bestOverlap, bestStart, bestEnd = oe-os, os, oe
			}
		}
	}
	if bestOverlap <= 0 {
		return 0, 0, false
	}
	return wrapMinutes(bestStart), wrapMinutes(bestEnd), true
}

func (c Config) lunarWindowActive(now time.Time) bool {
	s, e := c.LunarWindow(now)
	return inTimeWindow(now.Hour()*60+now.Minute(), s, e)
}

// lunarScheduleAllows: the daytime schedule must be near-dark right now.
func (c Config) lunarScheduleAllows(eff Schedule, now time.Time) bool {
	ch := interpolate(eff, now.Hour(), now.Minute())
	th := clamp(c.Lunar.DayThreshold, 0, 100)
	for i := 0; i < Channels; i++ {
		if ch[i] > th {
			return false
		}
	}
	return true
}

func (c Config) applyLunar(ch *[Channels]int, now time.Time) {
	pct := roundi(float64(c.Lunar.MaxIntensity) * MoonIllumination(now))
	if pct <= 0 {
		return
	}
	if pct > 100 {
		pct = 100
	}
	if pct > ch[1] {
		ch[1] = pct
	}
	if c.Device == "k7pro" {
		b := roundi(float64(pct) * 0.7)
		if b > 100 {
			b = 100
		}
		if b > ch[4] {
			ch[4] = b
		}
	}
}

// ---- top-level ----------------------------------------------------------

// Output is the computed lamp state at an instant.
type Output struct {
	Channels [Channels]int
	Source   string // "auto" | "manual" | "lunar" | "feed" | "maintenance"
}

// Compute is the pure heart of the engine: mirrors Effects.cpp
// restoreScheduledOutputNow for the auto path, plus manual.
//
//   - autoMode=false → manual channels + master
//   - autoMode=true  → effective schedule, interpolated to now, then siesta,
//     master, and (if the lunar window is active and the schedule is dark) the
//     lunar overlay.
func (c Config) Compute(base Schedule, manual [Channels]int, autoMode bool, now time.Time) Output {
	if !autoMode {
		ch := manual
		c.applyMaster(&ch)
		return Output{Channels: ch, Source: "manual"}
	}
	eff := c.EffectiveSchedule(base, now)
	ch := interpolate(eff, now.Hour(), now.Minute())
	c.applySiesta(&ch, now)
	c.applyMaster(&ch)
	src := "auto"
	if c.Lunar.Enabled && c.lunarWindowActive(now) && c.lunarScheduleAllows(eff, now) {
		c.applyLunar(&ch, now)
		src = "lunar"
	}
	return Output{Channels: ch, Source: src}
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}
