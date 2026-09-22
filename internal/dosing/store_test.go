package dosing

import (
	"path/filepath"
	"testing"
)

func TestSimulation(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "dosing.json"))
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.Manual(1, 12.5)
	if e != nil || a.Status != "simulated" {
		t.Fatalf("%+v %v", a, e)
	}
	if s.Snapshot().Heads[0].RemainingML != 987.5 {
		t.Fatal("remaining volume not updated")
	}
}
func TestRejectsUnsafe(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "dosing.json"))
	if _, e := s.Manual(1, 0); e == nil {
		t.Fatal("zero dose accepted")
	}
	h := s.Snapshot().Heads[0]
	h.Schedule.Doses = 0
	if _, e := s.Update(h); e == nil {
		t.Fatal("invalid schedule accepted")
	}
}
func TestFailureInjection(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "dosing.json"))
	_ = s.SetSimulator(Simulator{Failure: "partial"})
	a, e := s.Manual(1, 10)
	if e != nil || a.Status != "partial" || a.AmountML != 5 {
		t.Fatalf("%+v %v", a, e)
	}
	_ = s.SetSimulator(Simulator{Failure: "timeout"})
	if _, e = s.Manual(1, 1); e == nil {
		t.Fatal("timeout not injected")
	}
}
