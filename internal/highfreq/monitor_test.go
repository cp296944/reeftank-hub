package highfreq

import (
	"context"
	"github.com/cp296944/reeftank-hub/internal/equipment"
	"github.com/cp296944/reeftank-hub/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

type fakeOutletClient struct{ host, child string }

func (f *fakeOutletClient) childIDs(context.Context, string) ([]string, error) {
	return []string{"0", "1", "2", "3", "4", "5"}, nil
}
func (f *fakeOutletClient) energy(_ context.Context, host, child string) (outletReading, error) {
	f.host, f.child = host, child
	return outletReading{Voltage: 117, Power: 3}, nil
}

func TestPollingFollowsRemappedOutlet(t *testing.T) {
	eq, err := equipment.Open(filepath.Join(t.TempDir(), "equipment.json"))
	if err != nil {
		t.Fatal(err)
	}
	snap := eq.Snapshot()
	target := snap.Devices[17].SwitchEntity
	if _, err = eq.Assign("outlet_09", target); err != nil {
		t.Fatal(err)
	}
	snap = eq.Snapshot()
	d := snap.Devices[8]
	strip, idx, err := equipment.PhysicalOutlet(snap, d)
	if err != nil || strip.ID != "bfb9" || idx != 5 {
		t.Fatalf("physical mapping %s %d %v", strip.ID, idx, err)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m := New(eq, db, time.UTC)
	fake := &fakeOutletClient{}
	m.client = fake
	if err = m.pollStrip(context.Background(), strip, []equipment.Device{d}); err != nil {
		t.Fatal(err)
	}
	if fake.host != strip.Host || fake.child != "5" {
		t.Fatalf("read wrong outlet: %+v", fake)
	}
	d.SwitchEntity = "switch.unknown"
	if _, _, err = equipment.PhysicalOutlet(snap, d); err == nil {
		t.Fatal("unknown binding silently accepted")
	}
}
