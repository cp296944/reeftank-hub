//go:build !linux

package main

func diskKB(path string) (total, free int) { return 0, 0 }
func cpuUsagePercent() float64             { return 0 }
func throttleFlags() uint64                { return 0 }
