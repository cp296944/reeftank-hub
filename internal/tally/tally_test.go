package tally

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSurvivesReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "writes.json")
	c := Load(p, time.UTC)
	c.AddAuto()
	c.AddAuto()
	c.AddManual()
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	c2 := Load(p, time.UTC)
	a, m, _ := c2.Today()
	if a != 2 || m != 1 {
		t.Fatalf("after reload: auto=%d manual=%d, want 2/1", a, m)
	}
}

func TestRollsOnNewDay(t *testing.T) {
	p := filepath.Join(t.TempDir(), "writes.json")
	// write a file dated yesterday
	yest := time.Now().In(time.UTC).AddDate(0, 0, -1).Format("2006-01-02")
	b, _ := json.Marshal(snapshot{Date: yest, Auto: 99, Manual: 99})
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}

	c := Load(p, time.UTC)
	a, m, day := c.Today()
	if a != 0 || m != 0 {
		t.Errorf("stale day should reset: auto=%d manual=%d", a, m)
	}
	if day != time.Now().In(time.UTC).Format("2006-01-02") {
		t.Errorf("date = %q, want today", day)
	}
}

func TestSaveIsNoOpWhenClean(t *testing.T) {
	p := filepath.Join(t.TempDir(), "writes.json")
	c := Load(p, time.UTC)
	c.AddAuto()
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	fi1, _ := os.Stat(p)
	time.Sleep(10 * time.Millisecond)
	if err := c.Save(); err != nil { // nothing changed
		t.Fatal(err)
	}
	fi2, _ := os.Stat(p)
	if fi1.ModTime() != fi2.ModTime() {
		t.Error("Save rewrote the file with no changes")
	}
}
