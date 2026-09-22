package sheets

import (
	"testing"
	"time"
)

func TestNumbers(t *testing.T) {
	got := numbers(map[string]any{"no3": "2.5", "po4": 0.04, "junk": 9})
	if got["no3"] != 2.5 || got["po4"] != .04 || len(got) != 2 {
		t.Fatal(got)
	}
}
func TestParseTime(t *testing.T) {
	if parseTime("2026-09-22").Year() != 2026 {
		t.Fatal("date parse")
	}
	_ = time.Second
}
