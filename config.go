package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// HotkeyMode controls whether the hotkey is a toggle or must be held.
type HotkeyMode string

const (
	ModeToggle HotkeyMode = "toggle" // press to start, press again to stop
	ModeHold   HotkeyMode = "hold"   // hold to record, release to stop
)

// Config holds all runtime configuration for the program.
type Config struct {
	// ASR server
	ASRURL     string
	Language   string
	ASREngine  string
	ASRTimeout int
	ASRRetries int

	// Hotkey
	HotkeyStr  string
	Hotkey     *ParsedHotkey
	HotkeyMode HotkeyMode

	// Audio device
	TargetDevice   int // user-specified device ID (-1 = auto)
	SelectedDevice int // resolved device index (set by main after pickDevice)
	SetDevice      int // -set-audio-input flag value (-1 = not set)
	ListDevices    bool

	// Misc
	TempDir string
	Debug   bool
}

// loadConfig parses flags and environment variables, then validates the result.
func loadConfig() *Config {
	c := &Config{SelectedDevice: -1, SetDevice: -1, TargetDevice: -1}

	// ── Flag definitions ──────────────────────────────────────────────
	flag.StringVar(&c.ASRURL, "url", "", "ASR server base URL (env: ASR_URL; default: http://localhost:9000)")
	flag.StringVar(&c.Language, "language", "", "ISO-639-1 language code (env: ASR_LANGUAGE; default: auto-detect)")
	flag.StringVar(&c.ASREngine, "engine", "", "ASR engine name (env: ASR_ENGINE; default: faster_whisper)")
	flag.IntVar(&c.ASRTimeout, "timeout", 0, "ASR request timeout in seconds (env: ASR_TIMEOUT; default: 60)")
	flag.IntVar(&c.ASRRetries, "retries", 0, "ASR retry attempts (env: ASR_RETRIES; default: 3)")
	flag.StringVar(&c.HotkeyStr, "hotkey", "",
		"Hotkey combo, e.g. ctrl+shift+r (env: HOTKEY; default: ctrl+shift+r)")
	modeFlag := flag.String("mode", "",
		"Hotkey mode: toggle (press/press) or hold (hold/release) (env: HOTKEY_MODE; default: toggle)")
	flag.IntVar(&c.TargetDevice, "device", -1, "Audio device ID (-1 = auto-select; env: DEVICE_ID)")
	flag.IntVar(&c.SetDevice, "set-audio-input", -1, "Save audio input device by ID and exit")
	flag.BoolVar(&c.ListDevices, "list-audio-input", false, "List available audio input devices and exit")
	flag.StringVar(&c.TempDir, "tempdir", "", "Temp directory for WAV files (default: OS temp dir)")
	flag.BoolVar(&c.Debug, "debug", false, "Enable verbose debug logging (env: DEBUG=1)")
	flag.Parse()

	// ── Environment variable fallbacks ────────────────────────────────
	//   flag value wins over env; env wins over the hardcoded default.

	envStr := func(ptr *string, envKey, def string) {
		if *ptr != "" {
			return
		}
		if v := os.Getenv(envKey); v != "" {
			*ptr = v
			return
		}
		*ptr = def
	}
	envInt := func(ptr *int, envKey string, def int) {
		if *ptr != 0 {
			return
		}
		if v := os.Getenv(envKey); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*ptr = n
				return
			}
		}
		*ptr = def
	}

	envStr(&c.ASRURL, "ASR_URL", "http://localhost:9000")
	envStr(&c.Language, "ASR_LANGUAGE", "auto")
	envStr(&c.ASREngine, "ASR_ENGINE", "faster_whisper")
	envInt(&c.ASRTimeout, "ASR_TIMEOUT", 60)
	envInt(&c.ASRRetries, "ASR_RETRIES", 3)
	envStr(&c.HotkeyStr, "HOTKEY", "ctrl+shift+r")

	if !c.Debug {
		if v := os.Getenv("DEBUG"); v == "1" || strings.ToLower(v) == "true" {
			c.Debug = true
		}
	}

	if c.TempDir == "" {
		if v := os.Getenv("TEMP_DIR"); v != "" {
			c.TempDir = v
		} else {
			c.TempDir = os.TempDir()
		}
	}

	// Device from env (only when not provided via flag)
	if c.TargetDevice < 0 {
		if v := os.Getenv("DEVICE_ID"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				c.TargetDevice = n
			}
		}
	}

	// Hotkey mode: flag > env > default
	if *modeFlag == "" {
		*modeFlag = os.Getenv("HOTKEY_MODE")
	}
	switch strings.ToLower(*modeFlag) {
	case "hold":
		c.HotkeyMode = ModeHold
	default:
		c.HotkeyMode = ModeToggle
	}

	// Parse hotkey string
	hk, err := parseHotkey(c.HotkeyStr)
	if err != nil {
		log.Fatalf("[CONFIG] Invalid hotkey %q: %v\n\nValid key names: %s",
			c.HotkeyStr, err, validKeyNames())
	}
	c.Hotkey = hk

	return c
}

// ── hotkey parsing ────────────────────────────────────────────────────────────

// keyRawcodes maps human-readable key names to their X11 keysym rawcodes.
// Modifier keys list both L and R variants; the hotkey check accepts either.
var keyRawcodes = map[string][]uint16{
	// Modifier keys (L + R variants)
	"ctrl":    {65507, 65508},
	"control": {65507, 65508},
	"shift":   {65505, 65506},
	"alt":     {65513, 65514},
	"super":   {65515, 65516},
	"win":     {65515, 65516},
	"meta":    {65515, 65516},

	// Whitespace / navigation
	"space":     {32},
	"tab":       {65289},
	"enter":     {65293},
	"return":    {65293},
	"backspace": {65288},
	"escape":    {65307},
	"esc":       {65307},
	"delete":    {65535},
	"del":       {65535},
	"insert":    {65379},
	"home":      {65360},
	"end":       {65367},
	"pageup":    {65365},
	"pagedown":  {65366},
	"up":        {65362},
	"down":      {65364},
	"left":      {65361},
	"right":     {65363},

	// Letters — uppercase X11 keysym == ASCII uppercase
	"a": {65}, "b": {66}, "c": {67}, "d": {68}, "e": {69},
	"f": {70}, "g": {71}, "h": {72}, "i": {73}, "j": {74},
	"k": {75}, "l": {76}, "m": {77}, "n": {78}, "o": {79},
	"p": {80}, "q": {81}, "r": {82}, "s": {83}, "t": {84},
	"u": {85}, "v": {86}, "w": {87}, "x": {88}, "y": {89},
	"z": {90},

	// Digits
	"0": {48}, "1": {49}, "2": {50}, "3": {51}, "4": {52},
	"5": {53}, "6": {54}, "7": {55}, "8": {56}, "9": {57},

	// Function keys  (XK_F1 = 0xFFBE = 65470, …)
	"f1": {65470}, "f2": {65471}, "f3": {65472}, "f4": {65473},
	"f5": {65474}, "f6": {65475}, "f7": {65476}, "f8": {65477},
	"f9": {65478}, "f10": {65479}, "f11": {65480}, "f12": {65481},
}

// ParsedHotkey is an ordered set of keys that must all be pressed simultaneously.
type ParsedHotkey struct {
	keys []hotkeyKey
}

type hotkeyKey struct {
	name     string
	rawcodes []uint16
}

// parseHotkey parses a "ctrl+shift+r" style string into a ParsedHotkey.
func parseHotkey(s string) (*ParsedHotkey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty hotkey string")
	}
	parts := strings.Split(strings.ToLower(s), "+")
	hk := &ParsedHotkey{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		codes, ok := keyRawcodes[p]
		if !ok {
			return nil, fmt.Errorf("unknown key %q", p)
		}
		hk.keys = append(hk.keys, hotkeyKey{name: p, rawcodes: codes})
	}
	if len(hk.keys) == 0 {
		return nil, fmt.Errorf("no valid keys parsed from %q", s)
	}
	return hk, nil
}

// isTriggered returns true when every key in the combo has at least one rawcode pressed.
func (hk *ParsedHotkey) isTriggered(pressed map[uint16]bool) bool {
	for _, k := range hk.keys {
		found := false
		for _, rc := range k.rawcodes {
			if pressed[rc] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return len(hk.keys) > 0
}

// isAnyKey returns true if rawcode belongs to any key in the combo.
func (hk *ParsedHotkey) isAnyKey(rc uint16) bool {
	for _, k := range hk.keys {
		for _, code := range k.rawcodes {
			if code == rc {
				return true
			}
		}
	}
	return false
}

// String returns a human-readable combo string (e.g. "ctrl+shift+r").
func (hk *ParsedHotkey) String() string {
	names := make([]string, len(hk.keys))
	for i, k := range hk.keys {
		names[i] = k.name
	}
	return strings.Join(names, "+")
}

// validKeyNames returns a sorted, comma-joined list of valid key names for error messages.
func validKeyNames() string {
	names := make([]string, 0, len(keyRawcodes))
	for k := range keyRawcodes {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}
