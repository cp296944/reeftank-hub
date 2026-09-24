package threadborder

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Status struct {
	Configured       bool              `json:"configured"`
	RCPDevice        string            `json:"rcp_device,omitempty"`
	RCPPresent       bool              `json:"rcp_present"`
	OTBR             bool              `json:"otbr_connected"`
	Role             string            `json:"role,omitempty"`
	Network          string            `json:"network_name,omitempty"`
	Error            string            `json:"error,omitempty"`
	MaintenanceReady bool              `json:"maintenance_ready"`
	FirmwareVersion  string            `json:"firmware_version,omitempty"`
	Jobs             map[string]string `json:"jobs"`
}

type Monitor struct {
	EnvPath string
	RESTURL string
	Client  *http.Client
}

func New() *Monitor {
	return &Monitor{EnvPath: "/opt/reeftank-hub/thread/.env", RESTURL: "http://127.0.0.1:8081/api/node", Client: &http.Client{Timeout: 2 * time.Second}}
}

func (m *Monitor) Snapshot() Status {
	s := Status{Jobs: map[string]string{}}
	if _, err := os.Stat("/etc/systemd/system/reeftank-thread-flash.service"); err == nil {
		s.MaintenanceReady = true
	}
	if b, err := os.ReadFile("/opt/reeftank-hub/thread/firmware/current"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "tag=") {
				s.FirmwareVersion = strings.TrimPrefix(line, "tag=")
			}
		}
	}
	for action, unit := range actionUnits {
		out, err := exec.Command("systemctl", "is-active", unit).Output()
		state := strings.TrimSpace(string(out))
		if err != nil && state == "" {
			state = "inactive"
		}
		s.Jobs[action] = state
	}
	b, err := os.ReadFile(m.EnvPath)
	if err == nil {
		s.Configured = true
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "RCP_DEVICE=") {
				s.RCPDevice = strings.TrimSpace(strings.TrimPrefix(line, "RCP_DEVICE="))
			}
		}
	}
	if s.RCPDevice == "" {
		if paths, _ := filepath.Glob("/dev/serial/by-id/*"); len(paths) == 1 {
			s.RCPDevice = paths[0]
		}
	}
	if s.RCPDevice != "" {
		_, err = os.Stat(s.RCPDevice)
		s.RCPPresent = err == nil
	}
	resp, err := m.Client.Get(m.RESTURL)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		s.Error = resp.Status
		return s
	}
	s.OTBR = true
	var node map[string]any
	if json.NewDecoder(resp.Body).Decode(&node) == nil {
		attributes := node
		if data, ok := node["data"].(map[string]any); ok {
			if a, ok := data["attributes"].(map[string]any); ok {
				attributes = a
			}
		}
		s.Role, _ = attributes["role"].(string)
		s.Network, _ = attributes["networkName"].(string)
	}
	return s
}

func (m *Monitor) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hub/thread/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(m.Snapshot())
	})
	mux.HandleFunc("POST /api/hub/thread/action", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Action  string `json:"action"`
			Confirm bool   `json:"confirm"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil || !in.Confirm {
			http.Error(w, `{"error":"explicit confirmation is required"}`, http.StatusBadRequest)
			return
		}
		unit, ok := actionUnits[in.Action]
		if !ok {
			http.Error(w, `{"error":"unsupported action"}`, http.StatusBadRequest)
			return
		}
		if !m.Snapshot().MaintenanceReady {
			http.Error(w, `{"error":"Thread maintenance is not installed"}`, http.StatusConflict)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "systemctl", "start", "--no-block", unit)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Run(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": strings.TrimSpace(output.String())})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"started": in.Action, "unit": unit})
	})
}

var actionUnits = map[string]string{
	"flash":    "reeftank-thread-flash.service",
	"install":  "reeftank-thread-install.service",
	"restart":  "reeftank-thread-restart.service",
	"rollback": "reeftank-thread-rollback.service",
}
