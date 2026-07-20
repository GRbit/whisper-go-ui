package main

import (
	"errors"
	"testing"
)

// stubPaste replaces the robotgo indirection vars for one test and restores
// them afterwards. Returned pointers observe what deliverText did.
type pasteSpy struct {
	writes  []string // successful and failed clipboardWrite payloads
	taps    []string // "ctrl+v" or "ctrl+shift+v"
	prev    string   // what clipboardRead returns
	readErr error
	// writeErr fails the write whose payload equals failOn ("" = never)
	writeErr error
	failOn   string
}

func stubPaste(t *testing.T, spy *pasteSpy) {
	t.Helper()
	origRead, origWrite, origTap := clipboardRead, clipboardWrite, pasteKeyTap
	t.Cleanup(func() {
		clipboardRead, clipboardWrite, pasteKeyTap = origRead, origWrite, origTap
	})
	clipboardRead = func() (string, error) { return spy.prev, spy.readErr }
	clipboardWrite = func(s string) error {
		spy.writes = append(spy.writes, s)
		if spy.writeErr != nil && s == spy.failOn {
			return spy.writeErr
		}
		return nil
	}
	pasteKeyTap = func(key string, args ...interface{}) error {
		combo := "ctrl+" + key
		if len(args) > 1 {
			combo = "ctrl+shift+" + key
		}
		spy.taps = append(spy.taps, combo)
		return nil
	}
}

// A failed clipboard write must abort delivery: sending the paste keystroke
// anyway would paste whatever was in the clipboard before.
func TestDeliverTextWriteFailureAbortsPaste(t *testing.T) {
	spy := &pasteSpy{prev: "previous", writeErr: errors.New("no clipboard"), failOn: "hello"}
	stubPaste(t, spy)

	cfg := &Config{AutoPaste: true, CopyToClipboard: false, PasteCombo: PasteCtrlV}
	pasted, err := deliverText("hello", cfg)

	if err == nil {
		t.Error("want an error when the clipboard write fails")
	}
	if pasted {
		t.Error("must not report pasted")
	}
	if len(spy.taps) != 0 {
		t.Errorf("paste keystroke sent after failed clipboard write: %v", spy.taps)
	}
	if len(spy.writes) != 1 {
		t.Errorf("clipboard restore must not run after failed write, writes: %q", spy.writes)
	}
}

// Happy path with copy off: write text, send the combo, restore the
// previous clipboard content afterwards.
func TestDeliverTextAutoPasteRestoresClipboard(t *testing.T) {
	spy := &pasteSpy{prev: "previous"}
	stubPaste(t, spy)

	cfg := &Config{AutoPaste: true, CopyToClipboard: false, PasteCombo: PasteCtrlShiftV}
	pasted, err := deliverText("hello", cfg)

	if err != nil || !pasted {
		t.Fatalf("pasted=%v err=%v", pasted, err)
	}
	if len(spy.taps) != 1 || spy.taps[0] != "ctrl+shift+v" {
		t.Errorf("taps = %v, want [ctrl+shift+v]", spy.taps)
	}
	wantWrites := []string{"hello", "previous"}
	if len(spy.writes) != 2 || spy.writes[0] != wantWrites[0] || spy.writes[1] != wantWrites[1] {
		t.Errorf("writes = %q, want %q", spy.writes, wantWrites)
	}
}

// Copy-to-clipboard on: the transcript must stay in the clipboard, so no
// restore happens even with auto-paste enabled.
func TestDeliverTextCopyModeKeepsTextInClipboard(t *testing.T) {
	spy := &pasteSpy{prev: "previous"}
	stubPaste(t, spy)

	cfg := &Config{AutoPaste: true, CopyToClipboard: true, PasteCombo: PasteCtrlV}
	pasted, err := deliverText("hello", cfg)

	if err != nil || !pasted {
		t.Fatalf("pasted=%v err=%v", pasted, err)
	}
	if len(spy.writes) != 1 || spy.writes[0] != "hello" {
		t.Errorf("writes = %q, want just the transcript", spy.writes)
	}
}

// A failed paste keystroke must leave the transcript in the clipboard for a
// manual paste: no restore, and the error names what happened.
func TestDeliverTextTapFailureSkipsRestore(t *testing.T) {
	spy := &pasteSpy{prev: "previous"}
	stubPaste(t, spy)
	tapErr := errors.New("xtest unavailable")
	pasteKeyTap = func(key string, args ...interface{}) error { return tapErr }

	cfg := &Config{AutoPaste: true, CopyToClipboard: false, PasteCombo: PasteCtrlV}
	pasted, err := deliverText("hello", cfg)

	if !errors.Is(err, tapErr) {
		t.Errorf("err = %v, want wrapped tap error", err)
	}
	if pasted {
		t.Error("must not report pasted when the keystroke failed")
	}
	if len(spy.writes) != 1 {
		t.Errorf("restore must not overwrite the transcript, writes: %q", spy.writes)
	}
}
