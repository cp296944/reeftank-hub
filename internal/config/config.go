// Package config loads pi-bridge settings from (in ascending precedence):
// built-in defaults, an optional JSON file, environment variables, CLI flags.
//
// JSON keeps the binary dependency-free. The file lives next to the data dir so
// OTA updates (which only swap the binary) never disturb it.
package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Lamp link (wlan0 side).
	LampHost string `json:"lamp_host"`
	LampPort int    `json:"lamp_port"`

	// LAN services (eth0 side).
	Listen string `json:"listen"` // web UI + REST, e.g. ":80"
	Proxy  string `json:"proxy"`  // raw 8266 passthrough, e.g. ":8266" ("" disables)

	// Location for sun/moon math.
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`

	// Scheduler behaviour.
	SmoothRamp bool `json:"smooth_ramp"` // default off — flash write-wear

	// OTA.
	UpdateRepo     string `json:"update_repo"`     // "owner/name"
	UpdateChannel  string `json:"update_channel"`  // "stable" | "prerelease"
	UpdateInterval string `json:"update_interval"` // how often to CHECK, Go duration ("" disables checks)
	AutoUpdate     bool   `json:"auto_update"`     // if true, apply a found update automatically; else only surface it for the UI button

	// Paths.
	InstallRoot string `json:"install_root"` // OTA layout root (releases/, current, state/)
	DataDir     string `json:"data_dir"`     // writable app state; default ${InstallRoot}/data

	// Home Assistant. The token is environment-only and is never serialized.
	HAURL   string `json:"ha_url,omitempty"`
	HAToken string `json:"-"`

	// Xiaoyu Weilai temperature source. The URL contains the device serial and
	// therefore stays environment-only, just like the HA token.
	XiaoyuURL string `json:"-"`
	SheetsURL string `json:"-"`

	// Ops.
	BackupTarget string `json:"backup_target"` // rsync/scp dest, "" = off
	LogLevel     string `json:"log_level"`     // debug|info|warn|error
}

func Defaults() Config {
	return Config{
		LampHost:       "192.168.4.1",
		LampPort:       8266,
		Listen:         ":80",
		Proxy:          ":8266",
		Latitude:       22.63,
		Longitude:      120.30,
		Timezone:       "Asia/Taipei",
		SmoothRamp:     false,
		UpdateRepo:     "cp296944/reeftank-hub",
		UpdateChannel:  "stable",
		UpdateInterval: "1h",
		AutoUpdate:     false, // manual by default — the UI's "檢查更新 / 立即更新" button
		InstallRoot:    "/opt/reeftank-hub",
		DataDir:        "", // filled by normalize() to ${InstallRoot}/data
		HAURL:          "",
		BackupTarget:   "",
		LogLevel:       "info",
	}
}

func (c *Config) normalize() {
	if strings.TrimSpace(c.DataDir) == "" {
		c.DataDir = c.InstallRoot + "/data"
	}
}

// Load resolves configuration. args is normally os.Args[1:].
func Load(args []string) (Config, error) {
	cfg := Defaults()

	fs := flag.NewFlagSet("reeftank-hub", flag.ContinueOnError)
	file := fs.String("config", envOr("REEFTANK_CONFIG", ""), "path to JSON config file (optional)")
	fs.StringVar(&cfg.LampHost, "lamp-host", cfg.LampHost, "K7 lamp IP")
	fs.IntVar(&cfg.LampPort, "lamp-port", cfg.LampPort, "K7 lamp TCP port")
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "web UI + REST listen address")
	fs.StringVar(&cfg.Proxy, "proxy", cfg.Proxy, "raw 8266 passthrough listen address (empty disables)")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "writable state directory")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "debug|info|warn|error")
	printVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if *printVersion {
		return cfg, ErrVersionRequested
	}

	// JSON file (between defaults and env/flags is hard with flag pkg; we apply
	// file first, then re-apply any explicitly-set flags/env on top).
	if *file != "" {
		if err := applyFile(&cfg, *file); err != nil {
			return cfg, err
		}
		// re-parse so explicit flags win over the file
		_ = fs.Parse(args)
	}

	applyEnv(&cfg)
	cfg.normalize()

	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// ErrVersionRequested is returned by Load when -version was passed.
var ErrVersionRequested = fmt.Errorf("version requested")

func applyFile(cfg *Config, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	return nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("K7_LAMP_HOST"); v != "" {
		cfg.LampHost = v
	}
	if v := os.Getenv("K7_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("K7_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("K7_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("K7_LAMP_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.LampPort = n
		}
	}
	if v := os.Getenv("HA_URL"); v != "" {
		cfg.HAURL = strings.TrimRight(strings.TrimSpace(v), "/")
	}
	if v := os.Getenv("HA_TOKEN"); v != "" {
		cfg.HAToken = strings.TrimSpace(v)
	}
	if v := os.Getenv("XIAOYU_URL"); v != "" {
		cfg.XiaoyuURL = strings.TrimSpace(v)
	}
	if v := os.Getenv("GOOGLE_SHEETS_URL"); v != "" {
		cfg.SheetsURL = strings.TrimSpace(v)
	}
}

func (c Config) validate() error {
	if strings.TrimSpace(c.LampHost) == "" {
		return fmt.Errorf("lamp_host is empty")
	}
	if c.LampPort <= 0 || c.LampPort > 65535 {
		return fmt.Errorf("lamp_port %d out of range", c.LampPort)
	}
	if strings.TrimSpace(c.Listen) == "" {
		return fmt.Errorf("listen is empty")
	}
	if c.HAURL != "" {
		u, err := url.Parse(c.HAURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("ha_url %q invalid", c.HAURL)
		}
	}
	if c.XiaoyuURL != "" {
		u, err := url.Parse(c.XiaoyuURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("xiaoyu_url invalid (HTTPS URL required)")
		}
	}
	if c.SheetsURL != "" {
		u, err := url.Parse(c.SheetsURL)
		if err != nil || u.Scheme != "https" || u.Host != "script.google.com" {
			return fmt.Errorf("google_sheets_url invalid")
		}
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level %q invalid", c.LogLevel)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Save writes the config as pretty JSON (used by the settings page later).
func (c Config) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
