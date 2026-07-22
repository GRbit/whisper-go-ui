package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"log/slog"
)

// App is the Wails-bound application backend.
type App struct {
	ctx context.Context

	cfg      *configStore
	history  *HistoryStore
	tray     *Tray
	notifier *appNotifier
	pipeline *Pipeline
	hotkey   *HotkeyListener
	applier  configApplier

	// winVisible tracks window visibility for the tray left-click toggle
	// (wails v2 has no visibility query). Tray actions update it directly;
	// the frontend confirms via "window:visibility" page-visibility events,
	// which also covers the window close button and minimize.
	winVisible atomic.Bool

	// toggleOnLaunch is set when --toggle-recording was passed on the command
	// line of a fresh start; startup begins recording once the pipeline is up.
	toggleOnLaunch bool
}

// NewApp constructs the app with every component the frontend's bound
// methods touch. Wails binds methods before startup() runs and the frontend
// calls GetConfig/GetState/GetHistory as soon as it loads, so anything that
// does not need the Wails context must exist here, not in startup().
func NewApp() *App {
	a := &App{cfg: &configStore{}}
	a.cfg.Set(defaultConfig())
	a.history = NewHistoryStore(defaultConfig().HistoryMode, defaultConfig().HistoryLimit)
	a.tray = NewTray()
	a.notifier = newAppNotifier(a.tray)
	a.pipeline = NewPipeline(a.cfg, a.history, a.notifier)
	a.winVisible.Store(true) // the window starts visible

	// Live-apply subscribers: how a saved (or startup-loaded) config change
	// reaches each component. The hotkey subscriber joins in startup(),
	// once the listener exists.
	a.applier.Subscribe(func(_, cur Config) {
		if cur.Debug {
			logLevel.Set(slog.LevelDebug)
		} else {
			logLevel.Set(slog.LevelInfo)
		}
	})
	a.applier.Subscribe(func(old, cur Config) {
		if cur.HistoryLimit != old.HistoryLimit {
			a.history.SetLimit(cur.HistoryLimit)
		}
		if cur.HistoryMode != old.HistoryMode {
			a.history.SetMode(cur.HistoryMode)
			// A mode switch can change the visible list (disk entries merge
			// in); the mounted History tab reloads on this event.
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "history:added")
			}
		}
	})
	return a
}

// startup wires the ctx-dependent parts together once Wails hands us the context.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, err := loadConfig()
	if err != nil {
		slog.Warn("[INIT] Config problem (using defaults where needed)", "error", err)
	}
	prev := a.cfg.Get() // the NewApp defaults
	a.cfg.Set(cfg)
	a.applier.Apply(prev, *cfg)

	if err := portaudio.Initialize(); err != nil {
		slog.Error("[INIT] PortAudio init failed", "error", err)
	}
	devices, err := portaudio.Devices()
	if err != nil {
		slog.Error("[INIT] Listing PortAudio devices failed", "error", err)
	}
	slog.Info("[INIT] PortAudio devices found", "count", len(devices))

	a.notifier.Bind(ctx)
	a.pipeline.SetDevices(devices)

	combo, err := parseHotkey(cfg.HotkeyStr)
	if err != nil {
		// Config was validated on save, but guard against a hand-edited file.
		slog.Warn("[INIT] Invalid hotkey: falling back to ctrl+shift+r", "hotkey", cfg.HotkeyStr, "error", err)
		combo, _ = parseHotkey("ctrl+shift+r")
	}
	a.hotkey = NewHotkeyListener(combo, a.pipeline.Toggle)
	a.hotkey.SetDisabled(cfg.HotkeyDisabled)
	go a.hotkey.Run()
	a.applier.Subscribe(func(old, cur Config) {
		if cur.HotkeyStr != old.HotkeyStr {
			// validate() already parsed the combo; a parse error here means
			// a programming bug, so it is only logged.
			if combo, err := parseHotkey(cur.HotkeyStr); err == nil {
				a.hotkey.SetCombo(combo)
			} else {
				slog.Error("[CFG] Saved hotkey failed to parse on apply", "hotkey", cur.HotkeyStr, "error", err)
			}
		}
		if cur.HotkeyDisabled != old.HotkeyDisabled {
			a.hotkey.SetDisabled(cur.HotkeyDisabled)
		}
	})

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

	if a.toggleOnLaunch {
		slog.Info("[INIT] --toggle-recording on launch: starting recording")
		a.pipeline.Toggle()
	}
}

// onSecondInstance handles a repeated `whisper-go-ui` launch
// (SingleInstanceLock). With --toggle-recording it toggles the pipeline
// exactly like the global hotkey, without raising the window, so a DE
// keyboard shortcut bound to that command behaves like the hotkey; a plain
// launch brings the existing window to the front.
func (a *App) onSecondInstance(data options.SecondInstanceData) {
	opts := parseArgs(data.Args)
	slog.Info("[APP] Second instance launch", "args", data.Args)
	if opts.toggleRecording {
		a.pipeline.Toggle()
		return
	}
	a.showWindow()
}

// appMenu builds the window menu bar: a single Help menu with usage
// instructions, credits, and Quit at the bottom. The help and credits items
// open frontend modals via events.
func (a *App) appMenu() *menu.Menu {
	root := menu.NewMenu()
	helpMenu := root.AddSubmenu("Help")
	helpMenu.AddText("How to run", nil, func(_ *menu.CallbackData) {
		slog.Debug("[APP] Menu: help opened")
		runtime.EventsEmit(a.ctx, "menu:help")
	})
	helpMenu.AddText("Credits", nil, func(_ *menu.CallbackData) {
		slog.Debug("[APP] Menu: credits opened")
		runtime.EventsEmit(a.ctx, "menu:credits")
	})
	helpMenu.AddSeparator()
	helpMenu.AddText("Quit", nil, func(_ *menu.CallbackData) {
		slog.Info("[APP] Quit requested from window menu")
		runtime.Quit(a.ctx)
	})
	return root
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
	a.applier.Apply(old, c)
	return nil
}

// ValidateHotkey returns "" when the combo parses, or an error message with
// the list of valid key names.
func (a *App) ValidateHotkey(s string) string {
	if _, err := parseHotkey(s); err != nil {
		return fmt.Sprintf("%v - valid keys: %s", err, validKeyNames())
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
	return listInputDevices(a.pipeline.Devices())
}

// RefreshInputDevices rescans the hardware (PortAudio only sees hotplugged
// devices after a Terminate+Initialize cycle) and returns the new list.
// Refused while a recording or transcription is in flight.
func (a *App) RefreshInputDevices() ([]AudioDevice, error) {
	devices, err := a.pipeline.RescanDevices()
	if err != nil {
		return nil, err
	}
	return listInputDevices(devices), nil
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
