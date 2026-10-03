package jebao

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Device struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Model       string            `json:"model"`
	MAC         string            `json:"mac"`
	Identity    Identity          `json:"identity"`
	Attributes  map[string]any    `json:"attributes"`
	Labels      map[string]string `json:"labels"`
	Connected   bool              `json:"connected"`
	Stale       bool              `json:"stale"`
	Verified    bool              `json:"verified"`
	LastSuccess *time.Time        `json:"last_success"`
	LastAttempt *time.Time        `json:"last_attempt"`
	Error       string            `json:"error,omitempty"`
}
type settings struct {
	Hosts map[string]string `json:"hosts"`
}
type Snapshot struct {
	Devices        []Device `json:"devices"`
	ReadOnly       bool     `json:"read_only"`
	PollSeconds    int      `json:"poll_seconds"`
	Busy           bool     `json:"busy"`
	DiscoveryError string   `json:"discovery_error,omitempty"`
}
type HistoryPoint struct {
	Time      string   `json:"time"`
	Connected bool     `json:"connected"`
	Stale     bool     `json:"stale"`
	Speed     *float64 `json:"speed,omitempty"`
	Frequency *float64 `json:"frequency,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	SwitchOn  *bool    `json:"switch_on,omitempty"`
	Fault     bool     `json:"fault"`
}
type HistoryStore interface {
	RecordJebao(context.Context, Device) error
	JebaoHistory(context.Context, string, int) ([]HistoryPoint, error)
}
type Monitor struct {
	mu             sync.Mutex
	path           string
	devices        []Device
	settings       settings
	busy           bool
	discoveryError string
	discover       func(context.Context, string) ([]Identity, error)
	read           func(context.Context, Identity) (map[string]any, error)
	writeSpeed     func(context.Context, Identity, int) (map[string]any, error)
	writeFeeding   func(context.Context, Identity, bool) (map[string]any, error)
	writeFlow      func(context.Context, Identity, int) (map[string]any, error)
	history        HistoryStore
}

func (m *Monitor) SetHistoryStore(store HistoryStore) { m.history = store }

func Open(path string) (*Monitor, error) {
	m := &Monitor{path: path, settings: settings{Hosts: map[string]string{}}, discover: Discover, read: ReadStatus, writeSpeed: SetReturnSpeed, writeFeeding: SetReturnFeeding, writeFlow: SetWaveFlow, devices: []Device{
		{ID: "return-pump-1", Name: "主馬", Model: "MDP-10000", MAC: "e8:db:84:f4:cb:4c"},
		{ID: "wavemaker-1", Name: "06-GMP30R", Model: "GMP-30", MAC: "1c:db:d4:12:c7:88"},
		{ID: "wavemaker-2", Name: "13-GMP30L", Model: "GMP-30", MAC: "88:56:a6:fc:e1:00"},
		{ID: "wavemaker-3", Name: "造浪 3", Model: "DLW-20", MAC: "58:8c:81:64:c4:64"},
	}}
	var b []byte
	err := os.ErrNotExist
	if path != "" {
		b, err = os.ReadFile(path)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err = json.Unmarshal(b, &m.settings); err != nil {
			return nil, err
		}
	}
	if m.settings.Hosts == nil {
		m.settings.Hosts = map[string]string{}
	}
	for id, host := range m.settings.Hosts {
		if !m.known(id) || !privateIP(host) {
			return nil, errors.New("invalid JEBAO host settings")
		}
	}
	return m, nil
}
func privateIP(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() != nil && ip.IsPrivate()
}
func (m *Monitor) known(id string) bool {
	for _, d := range m.devices {
		if d.ID == id {
			return true
		}
	}
	return false
}
func (m *Monitor) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{ReadOnly: false, PollSeconds: 30, Busy: m.busy, DiscoveryError: m.discoveryError, Devices: make([]Device, len(m.devices))}
	for i, d := range m.devices {
		d.Stale = !d.Connected || d.LastSuccess == nil || time.Since(*d.LastSuccess) > 90*time.Second
		d.Attributes = cloneMap(d.Attributes)
		d.Labels = cloneLabels(d.Labels)
		s.Devices[i] = d
	}
	return s
}
func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneLabels(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (m *Monitor) Run(ctx context.Context) {
	m.Refresh(ctx, true)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	count := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count++
			m.Refresh(ctx, count%10 == 0)
		}
	}
}
func (m *Monitor) Refresh(ctx context.Context, discover bool) bool {
	m.mu.Lock()
	if m.busy {
		m.mu.Unlock()
		return false
	}
	m.busy = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.busy = false; m.mu.Unlock() }()
	if discover {
		ids, err := m.discover(ctx, "")
		m.mu.Lock()
		m.discoveryError = ""
		if err != nil {
			m.discoveryError = err.Error()
		}
		m.mu.Unlock()
		m.accept(ids)
	}
	m.mu.Lock()
	hosts := map[string]string{}
	for k, v := range m.settings.Hosts {
		hosts[k] = v
	}
	devices := append([]Device(nil), m.devices...)
	m.mu.Unlock()
	var wg sync.WaitGroup
	for i, d := range devices {
		wg.Add(1)
		go func(i int, d Device) {
			defer wg.Done()
			host := hosts[d.ID]
			if host == "" {
				host = d.Identity.IP
			}
			if host == "" {
				m.failed(i, "尚未探索到設備；請確認同一 LAN 或設定 IP")
				return
			}
			ids, err := m.discover(ctx, host)
			if err != nil {
				m.failed(i, err.Error())
				return
			}
			var id Identity
			for _, candidate := range ids {
				if strings.EqualFold(candidate.MAC, d.MAC) {
					id = candidate
					break
				}
			}
			if id.IP == "" {
				m.failed(i, "IP 未回應預期 MAC；未建立 TCP 連線")
				return
			}
			m.accept([]Identity{id})
			attrs, err := m.read(ctx, id)
			if err != nil {
				m.failed(i, err.Error())
				return
			}
			now := time.Now().UTC()
			labels := map[string]string{}
			model, _ := loadModel(id.ProductKey)
			for _, a := range model.Attrs {
				if n, ok := attrs[a.Name].(uint64); ok && int(n) < len(a.Enum) {
					labels[a.Name] = a.Enum[n]
				}
			}
			if id.ProductKey == "50dbc92221fd4d33ae69a1fedd43b555" {
				modes := []string{"脈衝造浪", "正弦造浪", "恆流", "隨機造浪", "潮汐", "營養運輸", "循環", "餵食", "自訂造浪"}
				for _, key := range []string{"Mode", "AutoMode"} {
					if n, ok := attrs[key].(uint64); ok && n < uint64(len(modes)) {
						labels[key] = modes[n]
					}
				}
			}
			m.mu.Lock()
			v := &m.devices[i]
			v.Attributes = attrs
			v.Labels = labels
			v.Connected = true
			v.Error = ""
			v.LastSuccess = &now
			v.LastAttempt = &now
			m.mu.Unlock()
		}(i, d)
	}
	wg.Wait()
	if m.history != nil {
		recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, device := range m.Snapshot().Devices {
			_ = m.history.RecordJebao(recordCtx, device)
		}
	}
	return true
}
func (m *Monitor) accept(ids []Identity) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		for i := range m.devices {
			d := &m.devices[i]
			if strings.EqualFold(id.MAC, d.MAC) {
				if d.Identity.ProductKey != "" && d.Identity.ProductKey != id.ProductKey {
					d.Attributes = nil
					d.Labels = nil
					d.LastSuccess = nil
				}
				d.Identity = id
			}
		}
	}
}
func (m *Monitor) failed(i int, message string) {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[i].Connected = false
	m.devices[i].Error = message
	m.devices[i].LastAttempt = &now
}
func (m *Monitor) Register(mux *http.ServeMux) {
	m.registerFeeding(mux)
	m.registerFlow(mux)
	mux.HandleFunc("POST /api/hub/jebao/return-pump/speed", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Speed *int `json:"speed"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.Speed == nil || *in.Speed < 1 || *in.Speed > 100 {
			reply(w, 400, map[string]string{"error": "速度須為 1–100 的整數"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		attrs, err := m.setSpeed(ctx, *in.Speed)
		if err != nil {
			reply(w, 409, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, map[string]any{"speed": attrs["Motor_Speed"], "verified_local": true, "cloud_verified": false})
	})
	mux.HandleFunc("GET /api/hub/jebao", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, m.Snapshot()) })
	mux.HandleFunc("GET /api/hub/jebao/history", func(w http.ResponseWriter, r *http.Request) {
		if m.history == nil {
			reply(w, 503, map[string]string{"error": "JEBAO history unavailable"})
			return
		}
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		if hours <= 0 || hours > 8760 {
			hours = 24
		}
		deviceID := r.URL.Query().Get("device_id")
		if deviceID != "" && !m.known(deviceID) {
			reply(w, 400, map[string]string{"error": "unknown device_id"})
			return
		}
		out := map[string][]HistoryPoint{}
		for _, device := range m.Snapshot().Devices {
			if deviceID != "" && device.ID != deviceID {
				continue
			}
			points, err := m.history.JebaoHistory(r.Context(), device.ID, hours)
			if err != nil {
				reply(w, 500, map[string]string{"error": "cannot read JEBAO history"})
				return
			}
			out[device.ID] = points
		}
		reply(w, 200, map[string]any{"hours": hours, "devices": out})
	})
	mux.HandleFunc("POST /api/hub/jebao/refresh", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if !m.Refresh(ctx, true) {
			reply(w, 409, map[string]string{"error": "JEBAO read is already running"})
			return
		}
		reply(w, 200, m.Snapshot())
	})
	mux.HandleFunc("GET /api/hub/jebao/settings", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		reply(w, 200, m.settings)
	})
	mux.HandleFunc("PUT /api/hub/jebao/settings", func(w http.ResponseWriter, r *http.Request) {
		var in settings
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.Hosts == nil {
			reply(w, 400, map[string]string{"error": "hosts are required"})
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.busy {
			reply(w, 409, map[string]string{"error": "wait for current read to finish"})
			return
		}
		for id, host := range in.Hosts {
			if !m.known(id) || !privateIP(host) {
				reply(w, 400, map[string]string{"error": "known device IDs and private IPv4 addresses only"})
				return
			}
		}
		b, _ := json.MarshalIndent(in, "", "  ")
		err := os.WriteFile(m.path+".tmp", b, 0600)
		if err == nil {
			err = os.Rename(m.path+".tmp", m.path)
		}
		if err != nil {
			reply(w, 500, map[string]string{"error": "cannot save JEBAO settings"})
			return
		}
		m.settings = in
		for i := range m.devices {
			m.devices[i].Connected = false
		}
		reply(w, 200, in)
	})
}
func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// DefaultPath keeps settings in the existing Hub data directory and backups.
func DefaultPath(dataDir string) string { return filepath.Join(dataDir, "jebao.json") }
