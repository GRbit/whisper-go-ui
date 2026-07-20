package main

import (
	"testing"
	"time"
)

func TestParseHotkey(t *testing.T) {
	hk, err := parseHotkey("Ctrl+Shift+R")
	if err != nil {
		t.Fatalf("parseHotkey: %v", err)
	}
	if got := hk.String(); got != "ctrl+shift+r" {
		t.Errorf("String() = %q", got)
	}

	if _, err := parseHotkey(""); err == nil {
		t.Error("empty string must fail")
	}
	if _, err := parseHotkey("ctrl+banana"); err == nil {
		t.Error("unknown key must fail")
	}
	if _, err := parseHotkey("++"); err == nil {
		t.Error("no keys must fail")
	}
}

func TestIsTriggered(t *testing.T) {
	hk, _ := parseHotkey("ctrl+shift+r")

	pressed := map[uint16]bool{}
	if hk.isTriggered(pressed) {
		t.Error("nothing pressed must not trigger")
	}

	// Left ctrl + right shift + r — either L/R modifier variant counts.
	pressed[65507] = true // ctrl L
	pressed[65506] = true // shift R
	if hk.isTriggered(pressed) {
		t.Error("missing r must not trigger")
	}
	pressed[82] = true // r
	if !hk.isTriggered(pressed) {
		t.Error("full combo must trigger")
	}
}

// TestIsTriggeredShiftlessLetterCombo reproduces the win+r bug: gohook
// reports the keysym computed with the live modifier state, so a letter
// pressed without shift arrives as the lowercase keysym (r = 114, not
// R = 82). A combo without shift in it must still match.
func TestIsTriggeredShiftlessLetterCombo(t *testing.T) {
	hk, err := parseHotkey("win+r")
	if err != nil {
		t.Fatalf("parseHotkey: %v", err)
	}

	pressed := map[uint16]bool{
		65515: true, // Super_L (win)
		114:   true, // XK_r — lowercase, no shift held
	}
	if !hk.isTriggered(pressed) {
		t.Error("win + lowercase r keysym must trigger")
	}

	// Caps Lock inversion: shift combo while caps is on yields the
	// lowercase keysym; uppercase must also keep working for ctrl+shift+r.
	hk2, _ := parseHotkey("ctrl+shift+r")
	capsPressed := map[uint16]bool{
		65507: true, // ctrl L
		65505: true, // shift L
		114:   true, // caps lock flips R back to lowercase
	}
	if !hk2.isTriggered(capsPressed) {
		t.Error("ctrl+shift + lowercase r keysym (caps lock on) must trigger")
	}
}

func TestIsAnyKey(t *testing.T) {
	hk, _ := parseHotkey("ctrl+r")
	if !hk.isAnyKey(65508) { // ctrl R variant
		t.Error("ctrl R rawcode should belong to combo")
	}
	if hk.isAnyKey(65505) { // shift L
		t.Error("shift does not belong to combo")
	}
}

func TestCaptureCombo(t *testing.T) {
	cases := []struct {
		name    string
		pressed map[uint16]bool
		rc      uint16
		want    string
		wantOK  bool
	}{
		{
			"ctrl+shift+r (lowercase keysym under modifiers)",
			map[uint16]bool{65507: true, 65505: true, 114: true}, 114,
			"ctrl+shift+r", true,
		},
		{
			"win+r via right Super",
			map[uint16]bool{65516: true, 114: true}, 114,
			"win+r", true,
		},
		{
			"uppercase keysym maps to same letter",
			map[uint16]bool{65507: true, 65505: true, 82: true}, 82,
			"ctrl+shift+r", true,
		},
		{
			"bare function key, no modifiers",
			map[uint16]bool{65474: true}, 65474,
			"f5", true,
		},
		{
			"canonical alias: return keysym becomes enter",
			map[uint16]bool{65513: true, 65293: true}, 65293,
			"alt+enter", true,
		},
		{
			"modifier-only press is not complete",
			map[uint16]bool{65507: true}, 65507,
			"", false,
		},
		{
			"unknown rawcode ignored",
			map[uint16]bool{65507: true, 12345: true}, 12345,
			"", false,
		},
		{
			"modifier order is canonical regardless of press order",
			map[uint16]bool{65516: true, 65505: true, 65507: true, 116: true}, 116,
			"ctrl+shift+win+t", true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := captureCombo(tc.pressed, tc.rc)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("captureCombo = %q, %v — want %q, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestCaptureComboParsesBack proves every combo the capture can emit is
// accepted by parseHotkey (the field is saved through the same validation).
func TestCaptureComboParsesBack(t *testing.T) {
	combo, ok := captureCombo(map[uint16]bool{65507: true, 65515: true, 112: true}, 112)
	if !ok {
		t.Fatal("capture should complete")
	}
	if _, err := parseHotkey(combo); err != nil {
		t.Errorf("captured combo %q does not parse back: %v", combo, err)
	}
}

// TestCaptureLifecycle checks the Capture/finishCapture handshake: delivery
// unblocks the waiter, a second concurrent capture is rejected, and a
// timeout returns "" and clears the capture slot.
func TestCaptureLifecycle(t *testing.T) {
	h := NewHotkeyListener(nil, nil)

	got := make(chan string, 1)
	go func() { got <- h.Capture(2 * time.Second) }()

	// Wait until the capture slot is armed.
	deadline := time.After(2 * time.Second)
	for h.getCapture() == nil {
		select {
		case <-deadline:
			t.Fatal("capture never armed")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if r := h.Capture(10 * time.Millisecond); r != "" {
		t.Errorf("concurrent capture must return empty, got %q", r)
	}

	h.finishCapture(h.getCapture(), "ctrl+x")
	select {
	case r := <-got:
		if r != "ctrl+x" {
			t.Errorf("Capture returned %q, want ctrl+x", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Capture did not return after finishCapture")
	}
	if h.getCapture() != nil {
		t.Error("capture slot must be cleared after delivery")
	}

	if r := h.Capture(20 * time.Millisecond); r != "" {
		t.Errorf("timed-out capture must return empty, got %q", r)
	}
	if h.getCapture() != nil {
		t.Error("capture slot must be cleared after timeout")
	}
}
