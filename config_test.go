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
	if c.Theme != ThemeDark {
		t.Errorf("Theme should default to dark, got %q", c.Theme)
	}
	if !c.CopyToClipboard || !c.AutoPaste || c.PasteCombo != PasteCtrlV {
		t.Errorf("paste behaviour should default to copy+paste with ctrl+v, got copy=%v paste=%v combo=%q",
			c.CopyToClipboard, c.AutoPaste, c.PasteCombo)
	}
	if c.HotkeyDisabled {
		t.Error("HotkeyDisabled should default to false (hotkey active) for configs without the field")
	}
}

// TestConfigHotkeyDisabledRoundTrip guards the DE-delegation switch: a saved
// hotkeyDisabled=true must survive a save/load cycle, and parseArgs must
// recognize the CLI flag a DE shortcut would use instead of the hotkey.
func TestConfigHotkeyDisabledRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c := defaultConfig()
	c.HotkeyDisabled = true
	if err := saveConfig(c); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	loaded, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !loaded.HotkeyDisabled {
		t.Error("HotkeyDisabled = false after round trip, want true")
	}

	got := parseArgs([]string{"--toggle-recording"})
	if !got.toggleRecording {
		t.Error("parseArgs should recognize --toggle-recording")
	}
	if got.help {
		t.Error("parseArgs found help in --toggle-recording")
	}
	if !parseArgs([]string{"-h"}).help {
		t.Error("parseArgs should recognize -h")
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
		{"light theme ok", func(c *Config) { c.Theme = ThemeLight }, true},
		{"bad theme", func(c *Config) { c.Theme = "solarized" }, false},
		{"control+v forbidden", func(c *Config) { c.HotkeyStr = "control+v" }, false},
		{"ctrl+shift+v forbidden", func(c *Config) { c.HotkeyStr = "ctrl+shift+v" }, false},
		// The synthetic paste holds ctrl(+shift)+v, so any combo made purely
		// of those keys fires off the app's own paste (feedback loop).
		{"v+ctrl forbidden", func(c *Config) { c.HotkeyStr = "v+ctrl" }, false},
		{"bare v forbidden", func(c *Config) { c.HotkeyStr = "v" }, false},
		{"shift+v forbidden", func(c *Config) { c.HotkeyStr = "shift+v" }, false},
		{"v+shift+control forbidden", func(c *Config) { c.HotkeyStr = "v+shift+control" }, false},
		{"ctrl+alt+v ok", func(c *Config) { c.HotkeyStr = "ctrl+alt+v" }, true},
		{"paste combo ctrl+shift+v ok", func(c *Config) { c.PasteCombo = PasteCtrlShiftV }, true},
		{"bad paste combo", func(c *Config) { c.PasteCombo = "middle-click" }, false},
		{"delivery fully off ok", func(c *Config) { c.CopyToClipboard = false; c.AutoPaste = false }, true},
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
