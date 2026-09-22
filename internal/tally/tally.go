// Package tally is a small, day-bucketed counter of lamp writes that survives a
// restart or OTA update. The engine bumps the "auto" side, the HTTP API bumps
// "manual"; both share one *Counter so /api/output/status keeps a consistent
// "今日上傳次數" across process restarts within the same local day.
//
// It is deliberately cheap on I/O (the whole point of counting writes is flash
// wear): increments only touch memory; the file is flushed on a timer and on
// shutdown.
package tally

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type snapshot struct {
	Date   string `json:"date"`
	Auto   int    `json:"auto"`
	Manual int    `json:"manual"`
}

type Counter struct {
	mu    sync.Mutex
	path  string
	tz    *time.Location
	s     snapshot
	dirty bool
}

// Load reads an existing counter file; if its date isn't today (tz-local), it
// starts fresh. A missing/corrupt file also starts fresh.
func Load(path string, tz *time.Location) *Counter {
	if tz == nil {
		tz = time.Local
	}
	c := &Counter{path: path, tz: tz}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c.s)
	}
	c.roll(c.today())
	return c
}

func (c *Counter) today() string { return time.Now().In(c.tz).Format("2006-01-02") }

// roll must be called with the lock held (or during Load).
func (c *Counter) roll(today string) {
	if c.s.Date != today {
		c.s = snapshot{Date: today}
		c.dirty = true
	}
}

func (c *Counter) AddAuto() {
	c.mu.Lock()
	c.roll(c.today())
	c.s.Auto++
	c.dirty = true
	c.mu.Unlock()
}

func (c *Counter) AddManual() {
	c.mu.Lock()
	c.roll(c.today())
	c.s.Manual++
	c.dirty = true
	c.mu.Unlock()
}

// Today returns (auto, manual, date). It rolls to a new day if needed.
func (c *Counter) Today() (int, int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roll(c.today())
	return c.s.Auto, c.s.Manual, c.s.Date
}

// Save atomically writes the file if anything changed since the last Save.
func (c *Counter) Save() error {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return nil
	}
	b, _ := json.Marshal(c.s)
	c.dirty = false
	c.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Run flushes the counter every `every` and once more when ctx is cancelled.
func (c *Counter) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 5 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = c.Save()
			return
		case <-t.C:
			_ = c.Save()
		}
	}
}
