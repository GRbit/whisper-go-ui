package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-vgo/robotgo"
)

// Indirection over robotgo so tests can stub these: the real calls need an
// X server and would clobber the user's actual clipboard.
var (
	clipboardRead  = robotgo.ReadAll
	clipboardWrite = robotgo.WriteAll
	pasteKeyTap    = robotgo.KeyTap
)

// deliverText hands the recognized text to the user according to the paste
// settings: copy it to the clipboard, send a paste keystroke, or both.
//
// Auto-paste always goes through the clipboard (full string in one shot, so
// it works with any language/script and any length of text). When
// CopyToClipboard is off, the previous clipboard text is restored after the
// receiving app has had time to read the pasted content.
//
// Returns true if a paste keystroke was sent. A non-nil error means
// delivery failed and the text may not have reached the user.
func deliverText(text string, c *Config) (bool, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		dbg("[PASTE] Nothing to deliver — text is empty after TrimSpace")
		return false, nil
	}
	if !c.CopyToClipboard && !c.AutoPaste {
		dbg("[PASTE] Clipboard and auto-paste both disabled — text goes to history only")
		return false, nil
	}

	dbg("[PASTE] Text to deliver (%d chars): %q  copy=%v paste=%v combo=%s",
		len(text), clip(text, 120), c.CopyToClipboard, c.AutoPaste, c.PasteCombo)

	// Save the old clipboard text when we will need to put it back.
	var prev string
	var prevOK bool
	if c.AutoPaste && !c.CopyToClipboard {
		if p, err := clipboardRead(); err == nil {
			prev, prevOK = p, true
		} else {
			dbg("[PASTE] Could not read clipboard for restore: %v", err)
		}
	}

	// ── 1. Write to clipboard ──────────────────────────────────────────
	// On failure, abort before the keystroke: pasting now would insert
	// whatever the clipboard held before. The old content is untouched,
	// so there is nothing to restore either.
	if err := clipboardWrite(text); err != nil {
		return false, fmt.Errorf("clipboard write: %w", err)
	}

	if !c.AutoPaste {
		dbg("[PASTE] Copied to clipboard, auto-paste disabled")
		return false, nil
	}

	// Brief pause: give the clipboard manager time to propagate the new
	// content before we send the paste keystroke.
	// 150 ms is conservative but reliable across DE/clipboard manager combos.
	time.Sleep(150 * time.Millisecond)

	// ── 2. Send the paste keystroke to the focused window ─────────────
	// On failure, keep the text in the clipboard (skip the restore below)
	// so the user can still paste manually.
	var tapErr error
	if c.PasteCombo == PasteCtrlShiftV {
		tapErr = pasteKeyTap("v", "lctrl", "lshift")
	} else {
		tapErr = pasteKeyTap("v", "lctrl")
	}
	if tapErr != nil {
		return false, fmt.Errorf("paste keystroke: %w (text left in clipboard)", tapErr)
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
		if err := clipboardWrite(prev); err != nil {
			info("[PASTE] Restoring previous clipboard text failed: %v", err)
		} else {
			dbg("[PASTE] Previous clipboard text restored (%d chars)", len(prev))
		}
	}
	return true, nil
}
