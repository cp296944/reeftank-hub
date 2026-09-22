package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallIntoSandboxAndRerun(t *testing.T) {
	systemRoot := t.TempDir()
	installRoot := filepath.Join(systemRoot, "opt", "reeftank-hub")
	exe := filepath.Join(systemRoot, "source-binary")
	if err := os.WriteFile(exe, []byte("test-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 4, 0, 0, 0, time.UTC)
	opts := InstallOptions{
		InstallRoot: installRoot, SystemRoot: systemRoot, Executable: exe,
		Version: "hub-vtest", SkipSystemCommands: true, Now: func() time.Time { return now },
	}
	first, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed {
		t.Fatal("first install reported no changes")
	}
	for _, name := range []string{
		"etc/systemd/system/reeftank-hub.service",
		"etc/reeftank-hub",
		"etc/systemd/system/reeftank-hub-maintenance.service",
		"etc/polkit-1/rules.d/50-reeftank-hub-maintenance.rules",
		"usr/local/libexec/reeftank-hub-maintainer",
	} {
		if _, err := os.Stat(filepath.Join(systemRoot, filepath.FromSlash(name))); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := Inspect(installRoot); !got.Ready || got.Marker.Version != "hub-vtest" {
		t.Fatalf("status after install = %+v", got)
	}

	second, err := Install(opts)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatal("identical rerun should be idempotent")
	}
}

func TestInstallBacksUpManagedFile(t *testing.T) {
	systemRoot := t.TempDir()
	installRoot := filepath.Join(systemRoot, "opt", "reeftank-hub")
	exe := filepath.Join(systemRoot, "source-binary")
	if err := os.WriteFile(exe, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(systemRoot, "usr", "local", "libexec", "reeftank-hub-maintainer")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 22, 5, 0, 0, 0, time.UTC)
	result, err := Install(InstallOptions{
		InstallRoot: installRoot, SystemRoot: systemRoot, Executable: exe,
		Version: "hub-vtest", SkipSystemCommands: true, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	_ = filepath.Walk(result.BackupDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); string(b) == "old-binary" {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Fatalf("old managed file not found below %s", result.BackupDir)
	}
}
