package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigLegacySchema proves a config.json written by the previous app
// version (9 fields, no auth/history keys) loads with defaults for new fields.
func TestLoadConfigLegacySchema(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	legacy := `{
  "asrUrl": "http://192.168.2.25:20172",
  "language": "auto",
  "asrEngine": "faster_whisper",
  "asrTimeout": 60,
  "asrRetries": 3,
  "hotkey": "ctrl+shift+r",
  "hotkeyMode": "toggle",
  "deviceId": -1,
  "debug": true
}`
	appDir := filepath.Join(dir, "whisper-go-ui")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.ASRURL != "http://192.168.2.25:20172" {
		t.Errorf("ASRURL = %q", c.ASRURL)
	}
	if !c.Debug {
		t.Error("Debug should be true")
	}
	if c.AuthHeaderName != "" || c.AuthHeaderValue != "" {
		t.Errorf("auth header should default empty, got %q/%q", c.AuthHeaderName, c.AuthHeaderValue)
	}
	if c.HistoryMode != HistoryRAM {
		t.Errorf("HistoryMode should default to ram, got %q", c.HistoryMode)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := loadConfig()
	if err != nil {
		t.Fatalf("missing file must not error, got %v", err)
	}
	d := defaultConfig()
	if c.ASRURL != d.ASRURL || c.HotkeyStr != d.HotkeyStr {
		t.Errorf("expected defaults, got %+v", c)
	}
}

func TestSaveConfigRoundTripAndPerms(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := defaultConfig()
	c.AuthHeaderName = "X-Api-Key"
	c.AuthHeaderValue = "secret"
	c.HistoryMode = HistoryDisk
	if err := saveConfig(c); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}

	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config perms = %o, want 600 (holds auth secret)", perm)
	}

	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if *got != *c {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", got, c)
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		wantOK bool
	}{
		{"defaults", func(c *Config) {}, true},
		{"empty url", func(c *Config) { c.ASRURL = " " }, false},
		{"no protocol", func(c *Config) { c.ASRURL = "localhost:9000" }, false},
		{"https ok", func(c *Config) { c.ASRURL = "https://asr.example.com" }, true},
		{"bad hotkey", func(c *Config) { c.HotkeyStr = "ctrl+banana" }, false},
		{"ctrl+v forbidden", func(c *Config) { c.HotkeyStr = "ctrl+v" }, false},
		{"auth value without name", func(c *Config) { c.AuthHeaderValue = "x" }, false},
		{"auth pair ok", func(c *Config) { c.AuthHeaderName = "X-Key"; c.AuthHeaderValue = "x" }, true},
		{"timeout too big", func(c *Config) { c.ASRTimeout = 601 }, false},
		{"retries zero", func(c *Config) { c.ASRRetries = 0 }, false},
		{"bad history mode", func(c *Config) { c.HistoryMode = "cloud" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := defaultConfig()
			tc.mutate(c)
			err := c.validate()
			if tc.wantOK && err != nil {
				t.Errorf("want valid, got %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Error("want error, got nil")
			}
		})
	}
}
