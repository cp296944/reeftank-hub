// Package equipment owns ReefTank Hub's logical equipment names. Home
// Assistant entity IDs are transport identifiers only; users can move plugs
// without renaming HA entities by reassigning them here.
package equipment

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Device struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name"`
	Slot         int    `json:"slot"`
	SwitchEntity string `json:"switch_entity"`
	Critical     bool   `json:"critical"`
}

type Snapshot struct {
	Schema    int       `json:"schema"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
	Devices   []Device  `json:"devices"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	data Snapshot
	now  func() time.Time
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.data = defaultSnapshot()
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("parse equipment map: %w", err)
	}
	if err := validate(s.data); err != nil {
		return nil, fmt.Errorf("validate equipment map: %w", err)
	}
	return s, nil
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.data)
}

// Assign changes the HA switch bound to one logical device. If the new entity
// is already assigned, the two devices exchange entities, which matches the
// common real-world operation of swapping plugs.
func (s *Store) Assign(id, switchEntity string) (Snapshot, error) {
	if !strings.HasPrefix(switchEntity, "switch.") || strings.ContainsAny(switchEntity, " \t\r\n") {
		return Snapshot{}, errors.New("switch_entity must be a Home Assistant switch entity ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target := -1
	for i := range s.data.Devices {
		if s.data.Devices[i].ID == id {
			target = i
			break
		}
	}
	if target < 0 {
		return Snapshot{}, fmt.Errorf("unknown equipment %q", id)
	}
	old := s.data.Devices[target].SwitchEntity
	if old == switchEntity {
		return cloneSnapshot(s.data), nil
	}
	for i := range s.data.Devices {
		if i != target && s.data.Devices[i].SwitchEntity == switchEntity {
			s.data.Devices[i].SwitchEntity = old
			break
		}
	}
	s.data.Devices[target].SwitchEntity = switchEntity
	s.data.Revision++
	s.data.UpdatedAt = s.now().UTC()
	if err := validate(s.data); err != nil {
		return Snapshot{}, err
	}
	if err := s.saveLocked(); err != nil {
		return Snapshot{}, err
	}
	return cloneSnapshot(s.data), nil
}

func (s *Store) Rename(id, displayName string) (Snapshot, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len([]rune(displayName)) > 60 {
		return Snapshot{}, errors.New("display_name must contain 1-60 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Devices {
		if s.data.Devices[i].ID != id {
			continue
		}
		if s.data.Devices[i].DisplayName == displayName {
			return cloneSnapshot(s.data), nil
		}
		s.data.Devices[i].DisplayName = displayName
		s.data.Revision++
		s.data.UpdatedAt = s.now().UTC()
		if err := s.saveLocked(); err != nil {
			return Snapshot{}, err
		}
		return cloneSnapshot(s.data), nil
	}
	return Snapshot{}, fmt.Errorf("unknown equipment %q", id)
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func validate(s Snapshot) error {
	if s.Schema != 1 || len(s.Devices) != 18 {
		return fmt.Errorf("expected schema 1 with 18 devices")
	}
	ids, entities, slots := map[string]bool{}, map[string]bool{}, map[int]bool{}
	for _, d := range s.Devices {
		if d.ID == "" || ids[d.ID] {
			return fmt.Errorf("duplicate or empty device id %q", d.ID)
		}
		if d.Slot < 1 || d.Slot > 18 || slots[d.Slot] {
			return fmt.Errorf("duplicate or invalid slot %d", d.Slot)
		}
		if !strings.HasPrefix(d.SwitchEntity, "switch.") || entities[d.SwitchEntity] {
			return fmt.Errorf("duplicate or invalid switch entity %q", d.SwitchEntity)
		}
		ids[d.ID], entities[d.SwitchEntity], slots[d.Slot] = true, true, true
	}
	return nil
}

func cloneSnapshot(in Snapshot) Snapshot {
	out := in
	out.Devices = append([]Device(nil), in.Devices...)
	sort.Slice(out.Devices, func(i, j int) bool { return out.Devices[i].Slot < out.Devices[j].Slot })
	return out
}

func defaultSnapshot() Snapshot {
	names := []string{"K7右", "K7中", "K7左-塑膠", "主馬", "冷水機馬達", "GMP30R", "空", "空", "珊瑚熊捲棉機", "珊瑚熊補水器", "JNS蛋白機", "冷水機", "GMP30L", "底缸DLW20", "滴定機", "空", "空", "空"}
	ids := []string{
		"switch.tp_link_power_strip_3a31_1_dmp_40zao_lang", "switch.tp_link_power_strip_3a31_2_leng_shui_ji_ma_da", "switch.tp_link_power_strip_3a31_3_k7_prodeng", "switch.tp_link_power_strip_3a31_4_zhu_ma", "switch.tp_link_power_strip_3a31_5_dan_bai_ji", "switch.tp_link_power_strip_3a31_6_ekoraljian_kong",
		"switch.tp_link_power_strip_3f2d_07_kong", "switch.tp_link_power_strip_3f2d_08_ekoraljian_kong", "switch.tp_link_power_strip_3f2d_09_shan_hu_xiong_juan_mian_ji", "switch.tp_link_power_strip_3f2d_10_kong", "switch.tp_link_power_strip_3f2d_11_hong_hai_dan_bai_ji", "switch.tp_link_power_strip_3f2d_12_dmp_40zao_lang",
		"switch.tp_link_power_strip_bfb9_13_dmp40l", "switch.tp_link_power_strip_bfb9_14_di_gang_dlw20", "switch.tp_link_power_strip_bfb9_15_di_ding_ji", "switch.tp_link_power_strip_bfb9_16_kong", "switch.tp_link_power_strip_bfb9_17_kong", "switch.tp_link_power_strip_bfb9_18_kong",
	}
	critical := map[int]bool{4: true, 5: true, 10: true, 11: true, 12: true, 15: true}
	devices := make([]Device, 18)
	for i := range devices {
		devices[i] = Device{ID: fmt.Sprintf("outlet_%02d", i+1), DisplayName: names[i], Slot: i + 1, SwitchEntity: ids[i], Critical: critical[i+1]}
	}
	return Snapshot{Schema: 1, Revision: 1, UpdatedAt: time.Now().UTC(), Devices: devices}
}
