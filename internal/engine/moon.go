package engine

import (
	"math"
	"time"
)

// Port of arduino/src/Moon.cpp — a simple synodic-month model anchored to a
// known new moon. Good enough for aquarium moonlight; not an ephemeris.

const (
	synodicDays = 29.530588853
	refNewMoon  = 947182440 // 2000-01-06 18:14 UTC
)

// MoonPhase returns 0.0 (new) .. 0.5 (full) .. 1.0 (new again) at t.
func MoonPhase(t time.Time) float64 {
	days := float64(t.Unix()-refNewMoon) / 86400.0
	cycle := math.Mod(days, synodicDays)
	if cycle < 0 {
		cycle += synodicDays
	}
	return cycle / synodicDays
}

// MoonIllumination returns the lit fraction 0..1 at t.
func MoonIllumination(t time.Time) float64 {
	return (1.0 - math.Cos(MoonPhase(t)*2.0*math.Pi)) / 2.0
}

func MoonPhaseName(t time.Time) string {
	p := MoonPhase(t)
	switch {
	case p < 0.0625 || p >= 0.9375:
		return "New Moon"
	case p < 0.1875:
		return "Waxing Crescent"
	case p < 0.3125:
		return "First Quarter"
	case p < 0.4375:
		return "Waxing Gibbous"
	case p < 0.5625:
		return "Full Moon"
	case p < 0.6875:
		return "Waning Gibbous"
	case p < 0.8125:
		return "Last Quarter"
	default:
		return "Waning Crescent"
	}
}
