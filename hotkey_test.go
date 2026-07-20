package main

import "testing"

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

func TestIsAnyKey(t *testing.T) {
	hk, _ := parseHotkey("ctrl+r")
	if !hk.isAnyKey(65508) { // ctrl R variant
		t.Error("ctrl R rawcode should belong to combo")
	}
	if hk.isAnyKey(65505) { // shift L
		t.Error("shift does not belong to combo")
	}
}
