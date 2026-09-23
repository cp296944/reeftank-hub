package threadborder

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Status struct {
	Configured bool   `json:"configured"`
	RCPDevice  string `json:"rcp_device,omitempty"`
	RCPPresent bool   `json:"rcp_present"`
	OTBR       bool   `json:"otbr_connected"`
	Role       string `json:"role,omitempty"`
	Network    string `json:"network_name,omitempty"`
	Error      string `json:"error,omitempty"`
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
	s := Status{}
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
}
