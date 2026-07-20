package main

import (
	"strings"
	"time"

	"github.com/go-vgo/robotgo"
)

// deliverText hands the recognized text to the user according to the paste
// settings: copy it to the clipboard, send a paste keystroke, or both.
//
// Auto-paste always goes through the clipboard (full string in one shot, so
// it works with any language/script and any length of text). When
// CopyToClipboard is off, the previous clipboard text is restored after the
// receiving app has had time to read the pasted content.
//
// Returns true if a paste keystroke was sent.
func deliverText(text string, c *Config) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		dbg("[PASTE] Nothing to deliver — text is empty after TrimSpace")
		return false
	}
	if !c.CopyToClipboard && !c.AutoPaste {
		dbg("[PASTE] Clipboard and auto-paste both disabled — text goes to history only")
		return false
	}

	dbg("[PASTE] Text to deliver (%d chars): %q  copy=%v paste=%v combo=%s",
		len(text), clip(text, 120), c.CopyToClipboard, c.AutoPaste, c.PasteCombo)

	// Save the old clipboard text when we will need to put it back.
	var prev string
	var prevOK bool
	if c.AutoPaste && !c.CopyToClipboard {
		if p, err := robotgo.ReadAll(); err == nil {
			prev, prevOK = p, true
		} else {
			dbg("[PASTE] Could not read clipboard for restore: %v", err)
		}
	}

	// ── 1. Write to clipboard ──────────────────────────────────────────
	robotgo.WriteAll(text)

	if !c.AutoPaste {
		dbg("[PASTE] Copied to clipboard, auto-paste disabled")
		return false
	}

	// Brief pause: give the clipboard manager time to propagate the new
	// content before we send the paste keystroke.
	// 150 ms is conservative but reliable across DE/clipboard manager combos.
	time.Sleep(150 * time.Millisecond)

	// ── 2. Send the paste keystroke to the focused window ─────────────
	if c.PasteCombo == PasteCtrlShiftV {
		robotgo.KeyTap("v", "lctrl", "lshift")
	} else {
		robotgo.KeyTap("v", "lctrl")
	}

	// Small post-paste delay — lets the receiving app process the event
	// before we potentially do anything else.
	time.Sleep(50 * time.Millisecond)
	dbg("[PASTE] %s sent — paste complete", c.PasteCombo)

	// ── 3. Optionally restore the previous clipboard text ─────────────
	if !c.CopyToClipboard && prevOK {
		// The receiving app reads the clipboard while handling the paste
		// keystroke; wait long enough for slow apps before overwriting.
		// Only text is restored — non-text clipboard content is lost.
		time.Sleep(300 * time.Millisecond)
		robotgo.WriteAll(prev)
		dbg("[PASTE] Previous clipboard text restored (%d chars)", len(prev))
	}
	return true
}
