package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeAssistantEnvironmentAndSecretSerialization(t *testing.T) {
	t.Setenv("HA_URL", "http://ha.example.test:8123/")
	t.Setenv("HA_TOKEN", "super-secret-token")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HAURL != "http://ha.example.test:8123" || cfg.HAToken != "super-secret-token" {
		t.Fatalf("HA config not loaded: url=%q token-set=%v", cfg.HAURL, cfg.HAToken != "")
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "super-secret-token") || strings.Contains(string(b), "ha_token") {
		t.Fatal("HA token was serialized")
	}
}

func TestRejectsInvalidHomeAssistantURL(t *testing.T) {
	t.Setenv("HA_URL", "file:///etc/passwd")
	if _, err := Load(nil); err == nil {
		t.Fatal("unsafe HA URL accepted")
	}
}
