package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	hook "github.com/robotn/gohook"
)

// ── hotkey parsing (tables reused from the CLI implementation) ────────────────

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

	// Letters — X11 keysyms match ASCII, but gohook reports the keysym
	// computed with the live modifier state: uppercase when shift is held,
	// lowercase otherwise (and inverted under caps lock). List both so
	// combos work with and without shift.
	"a": {65, 97}, "b": {66, 98}, "c": {67, 99}, "d": {68, 100}, "e": {69, 101},
	"f": {70, 102}, "g": {71, 103}, "h": {72, 104}, "i": {73, 105}, "j": {74, 106},
	"k": {75, 107}, "l": {76, 108}, "m": {77, 109}, "n": {78, 110}, "o": {79, 111},
	"p": {80, 112}, "q": {81, 113}, "r": {82, 114}, "s": {83, 115}, "t": {84, 116},
	"u": {85, 117}, "v": {86, 118}, "w": {87, 119}, "x": {88, 120}, "y": {89, 121},
	"z": {90, 122},

	// Digits
	"0": {48}, "1": {49}, "2": {50}, "3": {51}, "4": {52},
	"5": {53}, "6": {54}, "7": {55}, "8": {56}, "9": {57},

	// Function keys  (XK_F1 = 0xFFBE = 65470, …)
	"f1": {65470}, "f2": {65471}, "f3": {65472}, "f4": {65473},
	"f5": {65474}, "f6": {65475}, "f7": {65476}, "f8": {65477},
	"f9": {65478}, "f10": {65479}, "f11": {65480}, "f12": {65481},
}

// ── combo capture (reverse mapping: observed rawcodes -> combo string) ────────

// modifierOrder fixes the canonical modifier names and their order in
// captured combo strings.
var modifierOrder = []struct {
	name  string
	codes []uint16
}{
	{"ctrl", []uint16{65507, 65508}},
	{"shift", []uint16{65505, 65506}},
	{"alt", []uint16{65513, 65514}},
	{"win", []uint16{65515, 65516}},
}

// modifierCodes is the set of all modifier rawcodes.
var modifierCodes = map[uint16]bool{}

// rawcodeKeyName maps non-modifier rawcodes to their canonical key name.
var rawcodeKeyName = map[uint16]string{}

const escapeRawcode = 65307

func init() {
	for _, m := range modifierOrder {
		for _, c := range m.codes {
			modifierCodes[c] = true
		}
	}
	// Aliases share rawcodes with a canonical name; skip them so the
	// reverse map is deterministic. Modifiers are handled by modifierOrder.
	skip := map[string]bool{
		"ctrl": true, "control": true, "shift": true, "alt": true,
		"super": true, "win": true, "meta": true,
		"return": true, "esc": true, "del": true,
	}
	for name, codes := range keyRawcodes {
		if skip[name] {
			continue
		}
		for _, c := range codes {
			rawcodeKeyName[c] = name
		}
	}
}

// captureCombo builds a combo string from the currently held keys and the
// rawcode of the key that was just pressed. Returns ok=false when rc is a
// modifier (combo not complete yet) or an unknown key.
func captureCombo(pressed map[uint16]bool, rc uint16) (string, bool) {
	if modifierCodes[rc] {
		return "", false
	}
	name, known := rawcodeKeyName[rc]
	if !known {
		return "", false
	}
	var parts []string
	for _, m := range modifierOrder {
		for _, c := range m.codes {
			if pressed[c] {
				parts = append(parts, m.name)
				break
			}
		}
	}
	parts = append(parts, name)
	return strings.Join(parts, "+"), true
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
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ── global listener ───────────────────────────────────────────────────────────

// HotkeyListener runs one gohook event loop for the process lifetime.
// gohook observes keys passively via XRecord — nothing is grabbed, so the
// combo can be hot-swapped without restarting the loop and registration can
// never fail or conflict with other applications.
type HotkeyListener struct {
	mu      sync.RWMutex
	combo   *ParsedHotkey
	fire    func()      // called once per combo press (toggle semantics)
	capture chan string // non-nil while a combo capture is in progress
}

func NewHotkeyListener(combo *ParsedHotkey, fire func()) *HotkeyListener {
	return &HotkeyListener{combo: combo, fire: fire}
}

// SetCombo swaps the active hotkey combo; takes effect on the next key event.
func (h *HotkeyListener) SetCombo(c *ParsedHotkey) {
	h.mu.Lock()
	h.combo = c
	h.mu.Unlock()
	info("[HOTKEY] Active combo changed to %s", c)
}

func (h *HotkeyListener) getCombo() *ParsedHotkey {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.combo
}

// Capture blocks until the user presses a complete combo (modifiers + one
// regular key), presses Escape (returns ""), or the timeout expires
// (returns ""). Only one capture can be active at a time; a second call
// while one is running returns "" immediately.
func (h *HotkeyListener) Capture(timeout time.Duration) string {
	ch := make(chan string, 1)
	h.mu.Lock()
	if h.capture != nil {
		h.mu.Unlock()
		return ""
	}
	h.capture = ch
	h.mu.Unlock()
	info("[HOTKEY] Combo capture started (Escape cancels, %.0fs timeout)", timeout.Seconds())

	select {
	case combo := <-ch:
		return combo
	case <-time.After(timeout):
		h.mu.Lock()
		if h.capture == ch {
			h.capture = nil
		}
		h.mu.Unlock()
		// The event loop may have delivered right before we cleared.
		select {
		case combo := <-ch:
			return combo
		default:
		}
		info("[HOTKEY] Combo capture timed out")
		return ""
	}
}

func (h *HotkeyListener) getCapture() chan string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.capture
}

// finishCapture ends the capture owning ch and delivers the result.
func (h *HotkeyListener) finishCapture(ch chan string, combo string) {
	h.mu.Lock()
	if h.capture == ch {
		h.capture = nil
	}
	h.mu.Unlock()
	ch <- combo // buffered; never blocks
}

// Run blocks on the gohook event stream until hook.End() is called.
// Toggle-only: the combo fires once per press; hold-repeats are debounced.
func (h *HotkeyListener) Run() {
	evChan := hook.Start()

	// pressed tracks every rawcode that is currently held down.
	pressed := make(map[uint16]bool)
	// comboFired prevents retriggering while keys are still held.
	comboFired := false

	info("[HOTKEY] Listening for %s (toggle mode)", h.getCombo())

	for ev := range evChan {
		rc := ev.Rawcode
		hk := h.getCombo()

		switch {
		// ── Key DOWN (kind=3) or HOLD repeat (kind=4) ──────────────────
		case ev.Kind == 3 || ev.Kind == 4:
			isRepeat := pressed[rc]
			pressed[rc] = true

			// Capture mode: swallow events until a full combo (or Escape)
			// arrives; the active hotkey must not fire off captured keys.
			if ch := h.getCapture(); ch != nil {
				if isRepeat {
					continue
				}
				if rc == escapeRawcode {
					info("[HOTKEY] Combo capture cancelled with Escape")
					h.finishCapture(ch, "")
				} else if combo, ok := captureCombo(pressed, rc); ok {
					info("[HOTKEY] Captured combo: %s", combo)
					h.finishCapture(ch, combo)
				}
				continue
			}

			if !isRepeat && hk.isTriggered(pressed) && !comboFired {
				comboFired = true
				info("[HOTKEY] Combo activated: %s", hk)
				h.fire()
			}

		// ── Key UP (kind=5) ────────────────────────────────────────────
		case ev.Kind == 5:
			delete(pressed, rc)
			if comboFired && hk.isAnyKey(rc) {
				comboFired = false
			}
		}
	}
	info("[HOTKEY] Event loop terminated")
}

// Stop terminates the gohook event loop (unblocks Run).
func (h *HotkeyListener) Stop() {
	hook.End()
}
