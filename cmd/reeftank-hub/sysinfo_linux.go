//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// diskKB returns the total and available space (KiB) of the filesystem holding
// `path`.
func diskKB(path string) (total, free int) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bs := int64(st.Bsize)
	return int(int64(st.Blocks) * bs / 1024), int(int64(st.Bavail) * bs / 1024)
}

func cpuUsagePercent() float64 {
	read := func() (idle, total uint64) {
		b, err := os.ReadFile("/proc/stat")
		if err != nil {
			return
		}
		fields := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
		for i, field := range fields[1:] {
			value, _ := strconv.ParseUint(field, 10, 64)
			total += value
			if i == 3 || i == 4 {
				idle += value
			}
		}
		return
	}
	idle1, total1 := read()
	time.Sleep(120 * time.Millisecond)
	idle2, total2 := read()
	if total2 <= total1 {
		return 0
	}
	return 100 * (1 - float64(idle2-idle1)/float64(total2-total1))
}

func throttleFlags() uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "vcgencmd", "get_throttled").Output()
	if err != nil {
		return 0
	}
	value := strings.TrimSpace(strings.TrimPrefix(string(b), "throttled="))
	flags, _ := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 64)
	return flags
}
