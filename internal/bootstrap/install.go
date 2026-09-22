package bootstrap

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

//go:embed assets/*
var installAssets embed.FS

type InstallOptions struct {
	InstallRoot        string
	SystemRoot         string // "/" in production; temp dir in tests
	Version            string
	Executable         string
	SkipSystemCommands bool
	Now                func() time.Time
	Run                func(name string, args ...string) error
}

type InstallResult struct {
	Changed    bool     `json:"changed"`
	BackupDir  string   `json:"backup_dir,omitempty"`
	Components []string `json:"components"`
}

// Install performs the one-time, idempotent system integration. It is never
// reachable from HTTP; a human must invoke the binary with sudo.
func Install(o InstallOptions) (InstallResult, error) {
	if o.InstallRoot == "" {
		o.InstallRoot = "/opt/reeftank-hub"
	}
	if o.SystemRoot == "" {
		o.SystemRoot = string(filepath.Separator)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Run == nil {
		o.Run = runCommand
	}
	if o.Executable == "" {
		var err error
		o.Executable, err = os.Executable()
		if err != nil {
			return InstallResult{}, fmt.Errorf("find current executable: %w", err)
		}
	}
	production := filepath.Clean(o.SystemRoot) == string(filepath.Separator)
	if production {
		if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
			return InstallResult{}, fmt.Errorf("bootstrap requires linux/arm64, running %s/%s", runtime.GOOS, runtime.GOARCH)
		}
		if !runningAsRoot() {
			return InstallResult{}, errors.New("bootstrap install must be run with sudo")
		}
	}

	stamp := o.Now().UTC().Format("20060102T150405Z")
	backupDir := filepath.Join(o.InstallRoot, "backups", "bootstrap-"+stamp)
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return InstallResult{}, fmt.Errorf("create backup directory: %w", err)
	}
	credentialDir := rooted(o.SystemRoot, "/etc/reeftank-hub")
	if err := os.MkdirAll(credentialDir, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("create credential directory: %w", err)
	}
	if err := os.Chmod(credentialDir, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("protect credential directory: %w", err)
	}

	files := []struct {
		asset string
		dest  string
		mode  fs.FileMode
	}{
		{"assets/reeftank-hub.service", rooted(o.SystemRoot, "/etc/systemd/system/reeftank-hub.service"), 0o644},
		{"assets/reeftank-hub-maintenance.service", rooted(o.SystemRoot, "/etc/systemd/system/reeftank-hub-maintenance.service"), 0o644},
		{"assets/50-reeftank-hub-maintenance.rules", rooted(o.SystemRoot, "/etc/polkit-1/rules.d/50-reeftank-hub-maintenance.rules"), 0o644},
	}
	changed := false
	for _, f := range files {
		body, err := installAssets.ReadFile(f.asset)
		if err != nil {
			return InstallResult{}, err
		}
		c, err := installManagedFile(f.dest, body, f.mode, backupDir)
		if err != nil {
			return InstallResult{}, err
		}
		changed = changed || c
	}

	helper := rooted(o.SystemRoot, "/usr/local/libexec/reeftank-hub-maintainer")
	bin, err := os.ReadFile(o.Executable)
	if err != nil {
		return InstallResult{}, fmt.Errorf("read current executable: %w", err)
	}
	c, err := installManagedFile(helper, bin, 0o755, backupDir)
	if err != nil {
		return InstallResult{}, err
	}
	changed = changed || c

	if !o.SkipSystemCommands {
		if _, err := exec.LookPath("bluetoothctl"); err != nil {
			if _, aptErr := exec.LookPath("apt-get"); aptErr != nil {
				return InstallResult{}, errors.New("BlueZ is missing and apt-get is unavailable")
			}
			if err := o.Run("apt-get", "update"); err != nil {
				return InstallResult{}, fmt.Errorf("apt-get update: %w", err)
			}
			if err := o.Run("apt-get", "install", "-y", "bluez", "dbus"); err != nil {
				return InstallResult{}, fmt.Errorf("install BlueZ: %w", err)
			}
		}
		if err := o.Run("systemctl", "daemon-reload"); err != nil {
			return InstallResult{}, fmt.Errorf("systemd daemon-reload: %w", err)
		}
		// BlueZ may be socket/dbus activated on some Pi OS releases. A failed
		// enable must not erase the successfully installed integration.
		_ = o.Run("systemctl", "enable", "--now", "bluetooth.service")
	}

	marker := Marker{
		Schema: 1, InstalledAt: o.Now().UTC(), Version: o.Version,
		Components: map[string]string{
			"main_service":       "reeftank-hub.service",
			"credential_dir":     credentialDir,
			"maintenance_helper": helper,
			"systemd_unit":       "reeftank-hub-maintenance.service",
			"polkit_rule":        "50-reeftank-hub-maintenance.rules",
			"bluez":              "required",
		},
	}
	b, _ := json.MarshalIndent(marker, "", "  ")
	markerPath := filepath.Join(o.InstallRoot, "state", MarkerName)
	if err := writeAtomic(markerPath, append(b, '\n'), 0o644); err != nil {
		return InstallResult{}, fmt.Errorf("write bootstrap marker: %w", err)
	}
	return InstallResult{Changed: changed, BackupDir: backupDir, Components: []string{"main_service", "credential_dir", "maintenance_helper", "systemd_unit", "polkit_rule", "bluez"}}, nil
}

func rooted(root, absolute string) string {
	clean := strings.TrimPrefix(filepath.Clean(absolute), string(filepath.Separator))
	return filepath.Join(root, clean)
}

func installManagedFile(dest string, body []byte, mode fs.FileMode, backupDir string) (bool, error) {
	if old, err := os.ReadFile(dest); err == nil {
		if string(old) == string(body) {
			return false, nil
		}
		rel := strings.TrimLeft(filepath.ToSlash(filepath.Clean(dest)), "/\\")
		// A sandbox test may use a Windows drive-qualified path. Backups are
		// always relative trees, so strip the volume separator as well.
		rel = strings.ReplaceAll(rel, ":", "")
		backup := filepath.Join(backupDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(backup), 0o750); err != nil {
			return false, err
		}
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return false, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := writeAtomic(dest, body, mode); err != nil {
		return false, fmt.Errorf("install %s: %w", dest, err)
	}
	return true, nil
}

func writeAtomic(dest string, body []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
