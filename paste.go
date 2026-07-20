package main

import (
	"strings"
	"time"

	"github.com/go-vgo/robotgo"
)

// pasteText copies text to the clipboard and sends Ctrl+V to the currently
// focused window.
//
// The function does NOT type the text character by character — it writes the
// full string to the clipboard in one shot and then emits a single paste
// keystroke, so it works correctly with any language/script and any length
// of text.
func pasteText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		dbg("[PASTE] Nothing to paste — text is empty after TrimSpace")
		return
	}

	dbg("[PASTE] Text to paste (%d chars): %q", len(text), clip(text, 120))

	// ── 1. Write to clipboard ──────────────────────────────────────────
	robotgo.WriteAll(text)

	// Brief pause: give the clipboard manager time to propagate the new
	// content before we send the paste keystroke.
	// 150 ms is conservative but reliable across DE/clipboard manager combos.
	time.Sleep(150 * time.Millisecond)

	// ── 2. Send Ctrl+V to the focused window ──────────────────────────
	robotgo.KeyTap("v", "lctrl")

	// Small post-paste delay — lets the receiving app process the event
	// before we potentially do anything else.
	time.Sleep(50 * time.Millisecond)
	dbg("[PASTE] Ctrl+V sent — paste complete")
}
