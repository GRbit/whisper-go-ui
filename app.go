package main

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound application backend.
type App struct {
	ctx context.Context

	cfg      *configStore
	history  *HistoryStore
	tray     *Tray
	pipeline *Pipeline
	hotkey   *HotkeyListener

	devices []*portaudio.DeviceInfo

	// winVisible tracks window visibility for the tray left-click toggle
	// (wails v2 has no visibility query). Tray actions update it directly;
	// the frontend confirms via "window:visibility" page-visibility events,
	// which also covers the window close button and minimize.
	winVisible atomic.Bool
}

func NewApp() *App {
	a := &App{cfg: &configStore{}}
	a.winVisible.Store(true) // the window starts visible
	return a
}

// startup wires everything together once Wails hands us the context.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, err := loadConfig()
	if err != nil {
		info("[INIT] Config problem (using defaults where needed): %v", err)
	}
	a.cfg.Set(cfg)
	debugMode.Store(cfg.Debug)
	if cfg.Debug {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}

	if err := portaudio.Initialize(); err != nil {
		log.Printf("[FATAL] PortAudio init: %v", err)
	}
	a.devices, err = portaudio.Devices()
	if err != nil {
		log.Printf("[ERROR] portaudio.Devices(): %v", err)
	}
	info("[INIT] Found %d PortAudio device(s)", len(a.devices))

	a.history = NewHistoryStore(cfg.HistoryMode)
	a.tray = NewTray()
	a.pipeline = NewPipeline(a.cfg, a.history, a.tray)
	a.pipeline.Start(ctx, a.devices)

	combo, err := parseHotkey(cfg.HotkeyStr)
	if err != nil {
		// Config was validated on save, but guard against a hand-edited file.
		info("[INIT] Invalid hotkey %q (%v) — falling back to ctrl+shift+r", cfg.HotkeyStr, err)
		combo, _ = parseHotkey("ctrl+shift+r")
	}
	a.hotkey = NewHotkeyListener(combo, a.pipeline.Toggle)
	go a.hotkey.Run()

	runtime.EventsOn(ctx, "window:visibility", func(args ...interface{}) {
		if len(args) > 0 {
			if visible, ok := args[0].(bool); ok {
				a.winVisible.Store(visible)
			}
		}
	})

	a.tray.Start(
		a.toggleWindow,
		a.showWindow,
		func() { runtime.Quit(a.ctx) },
	)
}

// showWindow makes the window visible and records that.
func (a *App) showWindow() {
	runtime.WindowShow(a.ctx)
	a.winVisible.Store(true)
}

// toggleWindow is the tray left-click action.
func (a *App) toggleWindow() {
	if a.winVisible.Load() {
		runtime.WindowHide(a.ctx)
		a.winVisible.Store(false)
	} else {
		a.showWindow()
	}
}

// shutdown releases global resources.
func (a *App) shutdown(_ context.Context) {
	a.pipeline.StopActiveRecorder()
	a.hotkey.Stop()
	a.tray.Stop()
	portaudio.Terminate()
}

// ── bound methods (exposed to the frontend) ──────────────────────────────────

// GetConfig returns the current configuration for the Settings form.
func (a *App) GetConfig() Config {
	return a.cfg.Get()
}

// SaveConfig validates, persists and live-applies a new configuration.
func (a *App) SaveConfig(c Config) error {
	c.normalize()
	if err := c.validate(); err != nil {
		return err
	}
	if err := saveConfig(&c); err != nil {
		return err
	}

	old := a.cfg.Get()
	a.cfg.Set(&c)
	debugMode.Store(c.Debug)

	if c.HotkeyStr != old.HotkeyStr {
		combo, err := parseHotkey(c.HotkeyStr)
		if err != nil {
			return err // unreachable: validate() already parsed it
		}
		a.hotkey.SetCombo(combo)
	}
	if c.HistoryMode != old.HistoryMode {
		a.history.SetMode(c.HistoryMode)
		// A mode switch can change the visible list (disk entries merge
		// in); the History tab stays mounted and reloads on this event.
		runtime.EventsEmit(a.ctx, "history:added")
	}
	return nil
}

// ValidateHotkey returns "" when the combo parses, or an error message with
// the list of valid key names.
func (a *App) ValidateHotkey(s string) string {
	if _, err := parseHotkey(s); err != nil {
		return fmt.Sprintf("%v — valid keys: %s", err, validKeyNames())
	}
	return ""
}

// hotkeyCaptureTimeout bounds how long CaptureHotkey waits for a combo.
const hotkeyCaptureTimeout = 10 * time.Second

// CaptureHotkey blocks until the user presses a key combo and returns it as
// a hotkey string ("ctrl+shift+r"). Empty string means cancelled (Escape),
// timed out, or a capture already in progress.
func (a *App) CaptureHotkey() string {
	if a.hotkey == nil { // bound method callable before startup finishes
		return ""
	}
	return a.hotkey.Capture(hotkeyCaptureTimeout)
}

// ListInputDevices returns all PortAudio input devices for the Settings dropdown.
func (a *App) ListInputDevices() []AudioDevice {
	return listInputDevices(a.devices)
}

// GetHistory returns transcripts, newest first.
func (a *App) GetHistory() []HistoryEntry {
	return a.history.All()
}

// ClearHistory wipes RAM history and truncates the on-disk file.
func (a *App) ClearHistory() error {
	return a.history.Clear()
}

// GetState returns the current pipeline state string for the status bar.
func (a *App) GetState() string {
	return a.pipeline.State().String()
}

// ToggleRecording mirrors the global hotkey from the UI.
func (a *App) ToggleRecording() {
	a.pipeline.Toggle()
}
