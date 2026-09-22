// Package bootstrap reports whether the one-time ReefTank Hub system
// integration has been installed. The actual privileged installer is kept
// separate from the web server so an HTTP request can never acquire root.
package bootstrap

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const MarkerName = "HUB_BOOTSTRAPPED.json"

type Marker struct {
	Schema      int               `json:"schema"`
	InstalledAt time.Time         `json:"installed_at"`
	Version     string            `json:"version"`
	Components  map[string]string `json:"components,omitempty"`
}

type Status struct {
	Required       bool    `json:"required"`
	Ready          bool    `json:"ready"`
	Supported      bool    `json:"supported"`
	Platform       string  `json:"platform"`
	InstallCommand string  `json:"install_command,omitempty"`
	Marker         *Marker `json:"marker,omitempty"`
	Error          string  `json:"error,omitempty"`
}

// Inspect is read-only. A missing marker is the normal pre-bootstrap state.
func Inspect(installRoot string) Status {
	s := Status{
		Required:       true,
		Supported:      runtime.GOOS == "linux" && runtime.GOARCH == "arm64",
		Platform:       runtime.GOOS + "/" + runtime.GOARCH,
		InstallCommand: "sudo /opt/reeftank-hub/current/reeftank-hub bootstrap install",
	}
	b, err := os.ReadFile(filepath.Join(installRoot, "state", MarkerName))
	if errors.Is(err, os.ErrNotExist) {
		return s
	}
	if err != nil {
		s.Error = err.Error()
		return s
	}
	var m Marker
	if err := json.Unmarshal(b, &m); err != nil {
		s.Error = "bootstrap marker is invalid: " + err.Error()
		return s
	}
	if m.Schema < 1 || m.InstalledAt.IsZero() {
		s.Error = "bootstrap marker is incomplete"
		return s
	}
	s.Ready = true
	s.Required = false
	s.Marker = &m
	s.InstallCommand = ""
	return s
}
