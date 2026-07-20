package main

import (
	"sync/atomic"

	"fyne.io/systray"
	"log/slog"

	"whisper-go-ui/icons"
)

// Tray drives the system tray icon. The fyne.io/systray Linux backend is a
// pure-Go D-Bus StatusNotifierItem, so it runs on its own goroutine next to
// the Wails GTK main loop without conflict.
type Tray struct {
	ready   atomic.Bool
	pending atomic.Int64 // state to apply once the tray is ready (-1 = none)
}

func NewTray() *Tray {
	t := &Tray{}
	t.pending.Store(-1)
	return t
}

// Start launches the tray loop. onToggle runs on left-click (SNI Activate);
// onShow/onQuit are called from a tray-owned goroutine when the corresponding
// menu item is clicked. The right-click menu is rendered by the tray host
// from the exported dbusmenu, independent of these callbacks.
func (t *Tray) Start(onToggle, onShow, onQuit func()) {
	// Must be set before Run: the SNI ItemIsMenu property is derived from it
	// at registration time, and ItemIsMenu=false is what makes the host send
	// left clicks to Activate instead of opening the menu.
	systray.SetOnTapped(onToggle)
	go systray.Run(func() {
		systray.SetIcon(icons.Waiting)
		systray.SetTooltip("Whisper Transcriber — waiting")

		mShow := systray.AddMenuItem("Show window", "Open settings and history")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Exit the app")

		go func() {
			for {
				select {
				case <-mShow.ClickedCh:
					onShow()
				case <-mQuit.ClickedCh:
					onQuit()
				}
			}
		}()

		t.ready.Store(true)
		if p := t.pending.Swap(-1); p >= 0 {
			t.SetState(State(p))
		}
		slog.Info("[TRAY] System tray ready")
	}, nil)
}

// Stop tears down the tray.
func (t *Tray) Stop() {
	if t.ready.Load() {
		systray.Quit()
	}
}

// SetState swaps the tray icon to match the pipeline state. Calls made
// before the tray is ready are buffered (last one wins).
func (t *Tray) SetState(s State) {
	if !t.ready.Load() {
		t.pending.Store(int64(s))
		return
	}
	systray.SetIcon(iconFor(s))
	systray.SetTooltip("Whisper Transcriber — " + s.String())
}

func iconFor(s State) []byte {
	switch s {
	case StateRecording:
		return icons.Recording
	case StateProcessing:
		return icons.Transcribing
	case StatePasted:
		return icons.Pasted
	default:
		return icons.Waiting
	}
}
