package main

import (
	"os"
	"strconv"
	"strings"
)

// Pi/OS resource reads for /api/diag and the in-UI system monitor. All are
// best-effort: on a non-Linux dev box the /proc and /sys files are absent and
// these return zeros.

func meminfoKB() (total, avail int) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.Atoi(f[1])
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	return
}

func loadAvg() (l1, l5, l15 float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	f := strings.Fields(string(b))
	if len(f) >= 3 {
		l1, _ = strconv.ParseFloat(f[0], 64)
		l5, _ = strconv.ParseFloat(f[1], 64)
		l15, _ = strconv.ParseFloat(f[2], 64)
	}
	return
}

// socTempC reads the SoC temperature in °C (0 if unavailable).
func socTempC() float64 {
	for _, p := range []string{
		"/sys/class/thermal/thermal_zone0/temp",
		"/sys/devices/virtual/thermal/thermal_zone0/temp",
	} {
		if b, err := os.ReadFile(p); err == nil {
			if m, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil && m > 0 {
				return float64(m) / 1000.0
			}
		}
	}
	return 0
}
