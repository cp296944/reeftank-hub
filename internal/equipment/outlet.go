package equipment

import "fmt"

// PhysicalOutlet resolves the saved HA binding against the known physical
// outlet catalog, never against the logical device's original Slot. Unknown
// entities fail closed instead of silently reading a different socket.
func PhysicalOutlet(snap Snapshot, device Device) (PowerStrip, int, error) {
	for _, physical := range defaultSnapshot().Devices {
		if physical.SwitchEntity != device.SwitchEntity {
			continue
		}
		for _, strip := range snap.PowerStrips {
			if physical.Slot >= strip.SlotStart && physical.Slot <= strip.SlotEnd {
				return strip, physical.Slot - strip.SlotStart, nil
			}
		}
	}
	return PowerStrip{}, 0, fmt.Errorf("entity %s has no verified physical outlet mapping; high-frequency polling stopped", device.SwitchEntity)
}
