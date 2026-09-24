package threadborder

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
	mu      sync.Mutex
	nonce   string
	expires time.Time
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
	for action, unit := range jobUnits {
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
	req, err := http.NewRequest(http.MethodGet, m.RESTURL, nil)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	req.Header.Set("Accept", "application/vnd.api+json")
	resp, err := m.Client.Do(req)
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
	mux.HandleFunc("GET /api/hub/thread/dataset-token", func(w http.ResponseWriter, r *http.Request) {
		if !privateRequest(r) {
			writeError(w, http.StatusForbidden, "Thread credentials are accepted only from the local network")
			return
		}
		buf := make([]byte, 24)
		if _, err := rand.Read(buf); err != nil {
			writeError(w, http.StatusInternalServerError, "unable to create one-time token")
			return
		}
		m.mu.Lock()
		m.nonce, m.expires = hex.EncodeToString(buf), time.Now().Add(5*time.Minute)
		nonce := m.nonce
		m.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"nonce": nonce, "expires_in": 300})
	})
	mux.HandleFunc("POST /api/hub/thread/dataset", func(w http.ResponseWriter, r *http.Request) {
		if !privateRequest(r) {
			writeError(w, http.StatusForbidden, "Thread credentials are accepted only from the local network")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		var in struct {
			TLV      string `json:"tlv"`
			Nonce    string `json:"nonce"`
			Confirm  bool   `json:"confirm"`
			Validate bool   `json:"validate_only"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		summary, normalized, err := parseDatasetTLV(in.TLV)
		in.TLV = ""
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if in.Validate {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"valid": true, "dataset": summary})
			return
		}
		if !in.Confirm || !m.consumeNonce(in.Nonce) {
			writeError(w, http.StatusBadRequest, "valid one-time confirmation token is required")
			return
		}
		pending := "/opt/reeftank-hub/thread/pending/dataset.tlv"
		if err := os.MkdirAll(filepath.Dir(pending), 0o700); err != nil {
			writeError(w, http.StatusInternalServerError, "credential staging is unavailable")
			return
		}
		file, err := os.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err == nil {
			err = file.Chmod(0o600)
		}
		if err == nil {
			_, err = file.WriteString(normalized + "\n")
		}
		if file != nil {
			_ = file.Close()
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "unable to stage Thread credentials")
			return
		}
		normalized = ""
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "systemctl", "start", "--no-block", jobUnits["dataset"]).CombinedOutput(); err != nil {
			_ = os.Remove(pending)
			writeError(w, http.StatusInternalServerError, strings.TrimSpace(string(output)))
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"started": true, "dataset": summary})
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

func (m *Monitor) consumeNonce(got string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ok := m.nonce != "" && time.Now().Before(m.expires) && subtle.ConstantTimeCompare([]byte(got), []byte(m.nonce)) == 1
	m.nonce, m.expires = "", time.Time{}
	return ok
}

func privateRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback())
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func parseDatasetTLV(raw string) (map[string]any, string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	b, err := hex.DecodeString(normalized)
	if err != nil || len(b) < 20 || len(b) > 1024 {
		return nil, "", fmt.Errorf("TLV must be an even-length hexadecimal operational dataset")
	}
	summary := map[string]any{}
	seenKey := false
	for pos := 0; pos < len(b); {
		if pos+2 > len(b) {
			return nil, "", fmt.Errorf("truncated TLV header")
		}
		typ, size := b[pos], int(b[pos+1])
		pos += 2
		if pos+size > len(b) {
			return nil, "", fmt.Errorf("truncated TLV value")
		}
		value := b[pos : pos+size]
		pos += size
		switch typ {
		case 0:
			if size == 3 {
				summary["channel"] = int(value[1])<<8 | int(value[2])
			}
		case 1:
			if size == 2 {
				summary["pan_id"] = fmt.Sprintf("0x%02x%02x", value[0], value[1])
			}
		case 2:
			if size == 8 {
				summary["extended_pan_id"] = hex.EncodeToString(value)
			}
		case 3:
			if size > 0 && size <= 16 {
				summary["network_name"] = string(value)
			}
		case 5:
			seenKey = size == 16
		}
	}
	if summary["network_name"] == nil || summary["channel"] == nil || summary["pan_id"] == nil || summary["extended_pan_id"] == nil || !seenKey {
		return nil, "", fmt.Errorf("TLV is missing required Thread dataset fields")
	}
	return summary, normalized, nil
}

var actionUnits = map[string]string{
	"flash":    "reeftank-thread-flash.service",
	"install":  "reeftank-thread-install.service",
	"restart":  "reeftank-thread-restart.service",
	"rollback": "reeftank-thread-rollback.service",
}

var jobUnits = map[string]string{
	"flash":    "reeftank-thread-flash.service",
	"install":  "reeftank-thread-install.service",
	"restart":  "reeftank-thread-restart.service",
	"rollback": "reeftank-thread-rollback.service",
	"dataset":  "reeftank-thread-dataset.service",
}
