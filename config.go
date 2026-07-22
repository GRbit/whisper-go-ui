package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"log/slog"
)

// History storage modes.
const (
	HistoryRAM  = "ram"  // keep transcripts in memory only
	HistoryDisk = "disk" // additionally persist to a JSONL file under XDG data dir
)

// UI themes.
const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// Paste keystroke combos.
const (
	PasteCtrlV      = "ctrl+v"
	PasteCtrlShiftV = "ctrl+shift+v" // terminals
)

// Config is the persisted application configuration.
// JSON tags match the legacy ~/.config/whisper-go-ui/config.json schema so an
// existing file from the previous app version loads cleanly; new fields simply
// get defaults when absent.
type Config struct {
	ASRURL     string `json:"asrUrl"`
	Language   string `json:"language"`
	ASREngine  string `json:"asrEngine"`
	ASRTimeout int    `json:"asrTimeout"`
	ASRRetries int    `json:"asrRetries"`
	HotkeyStr  string `json:"hotkey"`
	HotkeyMode string `json:"hotkeyMode"` // legacy; the app is toggle-only now
	// HotkeyDisabled switches the in-app global hotkey off so a desktop
	// environment shortcut bound to `whisper-go-ui --toggle-recording` can
	// own the combo instead (both active would toggle twice per press).
	// Absent in older configs -> false -> hotkey stays active.
	HotkeyDisabled bool `json:"hotkeyDisabled"`
	DeviceID   int    `json:"deviceId"`   // requested PortAudio device (-1 = auto)
	Debug      bool   `json:"debug"`

	AuthHeaderName  string `json:"authHeaderName"`
	AuthHeaderValue string `json:"authHeaderValue"`
	HistoryMode     string `json:"historyMode"` // "ram" | "disk"
	// HistoryLimit caps how many entries the on-disk history file keeps
	// (cleanup happens on write). 0 means no limit. Legacy configs without
	// the field get the default because loadConfig unmarshals over defaults.
	HistoryLimit int    `json:"historyLimit"`
	Theme        string `json:"theme"` // "dark" | "light"

	// What to do with the recognized text. Auto-paste works through the
	// clipboard; with CopyToClipboard off the previous clipboard text is
	// restored after the paste keystroke.
	CopyToClipboard bool   `json:"copyToClipboard"`
	AutoPaste       bool   `json:"autoPaste"`
	PasteCombo      string `json:"pasteCombo"` // "ctrl+v" | "ctrl+shift+v"
}

func defaultConfig() *Config {
	return &Config{
		ASRURL:      "http://localhost:9000",
		Language:    "auto",
		ASREngine:   "faster_whisper",
		ASRTimeout:  60,
		ASRRetries:  3,
		HotkeyStr:   "ctrl+shift+r",
		HotkeyMode:  "toggle",
		DeviceID:     -1,
		HistoryMode:  HistoryRAM,
		HistoryLimit: 1000,
		Theme:        ThemeDark,

		CopyToClipboard: true,
		AutoPaste:       true,
		PasteCombo:      PasteCtrlV,
	}
}

// configDir returns the XDG config directory for the app, creating it if needed.
func configDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "whisper-go-ui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir %s: %w", dir, err)
	}
	return dir, nil
}

// dataDir returns the XDG data directory for the app, creating it if needed.
func dataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "whisper-go-ui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create data dir %s: %w", dir, err)
	}
	return dir, nil
}

func configPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// loadConfig reads config.json over a fully-defaulted struct, so missing or
// unknown fields degrade gracefully. A missing file is not an error.
func loadConfig() (*Config, error) {
	c := defaultConfig()
	path, err := configPath()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("[CFG] No config file: using defaults", "path", path)
			return c, nil
		}
		return c, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(data, c); err != nil {
		return defaultConfig(), fmt.Errorf("parse config %s: %w", path, err)
	}
	c.normalize()
	slog.Debug("[CFG] Loaded config", "path", path)
	return c, nil
}

// saveConfig atomically writes config.json with 0600 perms (the auth header
// value is a secret).
func saveConfig(c *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	slog.Debug("[CFG] Saved config", "path", path)
	return nil
}

// normalize clamps out-of-range values back to defaults.
func (c *Config) normalize() {
	d := defaultConfig()
	if strings.TrimSpace(c.ASRURL) == "" {
		c.ASRURL = d.ASRURL
	}
	if c.Language == "" {
		c.Language = d.Language
	}
	if c.ASREngine == "" {
		c.ASREngine = d.ASREngine
	}
	if c.ASRTimeout <= 0 {
		c.ASRTimeout = d.ASRTimeout
	}
	if c.ASRRetries <= 0 {
		c.ASRRetries = d.ASRRetries
	}
	if strings.TrimSpace(c.HotkeyStr) == "" {
		c.HotkeyStr = d.HotkeyStr
	}
	if c.HistoryMode != HistoryDisk {
		c.HistoryMode = HistoryRAM
	}
	if c.HistoryLimit < 0 {
		c.HistoryLimit = d.HistoryLimit
	}
	if c.Theme != ThemeLight {
		c.Theme = ThemeDark
	}
	if c.PasteCombo != PasteCtrlShiftV {
		c.PasteCombo = PasteCtrlV
	}
	if c.DeviceID < -1 {
		c.DeviceID = -1
	}
}

// validate rejects configs that must not be persisted.
func (c *Config) validate() error {
	if strings.TrimSpace(c.ASRURL) == "" {
		return fmt.Errorf("API URL must not be empty")
	}
	if !strings.HasPrefix(c.ASRURL, "http://") && !strings.HasPrefix(c.ASRURL, "https://") {
		return fmt.Errorf("API URL must start with http:// or https://")
	}
	hk, err := parseHotkey(c.HotkeyStr)
	if err != nil {
		return fmt.Errorf("invalid hotkey %q: %w", c.HotkeyStr, err)
	}
	// The paste step sends a synthetic Ctrl+V or Ctrl+Shift+V; a hotkey made
	// only of those keys would re-trigger the pipeline from its own paste.
	if hk.conflictsWithPaste() {
		return fmt.Errorf("%s cannot be used as the hotkey (it is a paste keystroke)", hk)
	}
	if c.AuthHeaderName == "" && c.AuthHeaderValue != "" {
		return fmt.Errorf("auth header value set but header name is empty")
	}
	if c.ASRTimeout <= 0 || c.ASRTimeout > 600 {
		return fmt.Errorf("timeout must be between 1 and 600 seconds")
	}
	if c.ASRRetries <= 0 || c.ASRRetries > 10 {
		return fmt.Errorf("retries must be between 1 and 10")
	}
	if c.HistoryMode != HistoryRAM && c.HistoryMode != HistoryDisk {
		return fmt.Errorf("history mode must be %q or %q", HistoryRAM, HistoryDisk)
	}
	if c.HistoryLimit < 0 {
		return fmt.Errorf("history limit must be 0 (no limit) or a positive number")
	}
	if c.Theme != ThemeDark && c.Theme != ThemeLight {
		return fmt.Errorf("theme must be %q or %q", ThemeDark, ThemeLight)
	}
	if c.PasteCombo != PasteCtrlV && c.PasteCombo != PasteCtrlShiftV {
		return fmt.Errorf("paste combo must be %q or %q", PasteCtrlV, PasteCtrlShiftV)
	}
	return nil
}

// configStore guards concurrent access to the live config (hotkey goroutine,
// pipeline goroutine and bound UI methods all read it).
type configStore struct {
	mu sync.RWMutex
	c  *Config
}

func (s *configStore) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s.c
}

func (s *configStore) Set(c *Config) {
	s.mu.Lock()
	s.c = c
	s.mu.Unlock()
}
