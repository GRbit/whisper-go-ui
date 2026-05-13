package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gordonklaus/portaudio"
	hook "github.com/robotn/gohook"
)

type State int

const (
	StateIdle       State = iota
	StateRecording  State = iota
	StateProcessing State = iota
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "IDLE"
	case StateRecording:
		return "RECORDING"
	case StateProcessing:
		return "PROCESSING"
	default:
		return fmt.Sprintf("STATE(%d)", int(s))
	}
}

var (
	mu            sync.Mutex
	appState      = StateIdle
	activeRec     *Recorder
	cachedDevices []*portaudio.DeviceInfo

	cfg       *Config
	debugMode bool // mirrors cfg.Debug; kept as bare bool so inner funcs can read cheaply
)

// dbg prints a debug line when debugMode is on.
// Caller is responsible for a meaningful [TAG] prefix.
func dbg(format string, args ...interface{}) {
	if debugMode {
		log.Printf("[DBG] "+format, args...)
	}
}

// info always prints — normal operational messages.
func info(format string, args ...interface{}) {
	log.Printf(format, args...)
}

func getState() State {
	mu.Lock()
	defer mu.Unlock()
	return appState
}

func setState(s State) {
	mu.Lock()
	old := appState
	appState = s
	mu.Unlock()
	if old != s {
		info("[STATE] %s → %s", old, s)
	}
}

func main() {
	cfg = loadConfig()
	debugMode = cfg.Debug

	if debugMode {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	} else {
		log.SetFlags(log.LstdFlags)
	}

	dbg("[INIT] Debug mode enabled — verbose logging active")
	dbg("[INIT] Config: ASR_URL=%s engine=%s timeout=%ds retries=%d",
		cfg.ASRURL, cfg.ASREngine, cfg.ASRTimeout, cfg.ASRRetries)
	dbg("[INIT] Hotkey: %q  mode: %s", cfg.HotkeyStr, cfg.HotkeyMode)
	dbg("[INIT] Language: %q  TempDir: %s", cfg.Language, cfg.TempDir)

	// Initialise PortAudio
	dbg("[PA] Calling portaudio.Initialize()")
	if err := portaudio.Initialize(); err != nil {
		log.Fatalf("[FATAL] PortAudio init: %v", err)
	}
	defer func() {
		dbg("[PA] portaudio.Terminate()")
		portaudio.Terminate()
	}()

	var err error
	cachedDevices, err = portaudio.Devices()
	if err != nil {
		log.Fatalf("[FATAL] portaudio.Devices(): %v", err)
	}
	dbg("[PA] Found %d PortAudio device(s)", len(cachedDevices))

	// Special modes: list / set device
	if cfg.ListDevices {
		printDevices(cachedDevices)
		return
	}
	if cfg.SetDevice >= 0 {
		if cfg.SetDevice >= len(cachedDevices) {
			log.Fatalf("[FATAL] Device %d out of range (0..%d)", cfg.SetDevice, len(cachedDevices)-1)
		}
		saveDevice(cfg.SetDevice)
		info("Saved device [%d] %s to ~/.config/go-desktop-transcriber/device",
			cfg.SetDevice, cachedDevices[cfg.SetDevice].Name)
		return
	}

	// Pick the recording device
	cfg.SelectedDevice = pickDevice(cachedDevices, cfg)

	devName := "(PortAudio default)"
	if cfg.SelectedDevice >= 0 && cfg.SelectedDevice < len(cachedDevices) {
		devName = fmt.Sprintf("[%d] %s", cfg.SelectedDevice, cachedDevices[cfg.SelectedDevice].Name)
	}

	// Banner
	fmt.Printf("╔═══════════════════════════════════════╗\n")
	fmt.Printf("║         whisper-paste  v1.0           ║\n")
	fmt.Printf("╚═══════════════════════════════════════╝\n")
	fmt.Printf("  Device  : %s\n", devName)
	fmt.Printf("  ASR     : %s  (engine=%s  timeout=%ds)\n",
		cfg.ASRURL, cfg.ASREngine, cfg.ASRTimeout)
	fmt.Printf("  Hotkey  : %s  (%s mode)\n", cfg.HotkeyStr, cfg.HotkeyMode)
	fmt.Printf("  Language: %s\n", cfg.Language)
	fmt.Printf("  Debug   : %v\n", debugMode)
	fmt.Println()
	fmt.Println("  Ready. Press Ctrl+C to exit.")
	fmt.Println()

	// OS signal handler — clean shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		info("[SIGNAL] Received %v — shutting down...", sig)
		mu.Lock()
		rec := activeRec
		mu.Unlock()
		if rec != nil {
			info("[SIGNAL] Stopping active recording before exit")
			rec.Stop()
		}
		hook.End()
		portaudio.Terminate()
		os.Exit(0)
	}()

	runHotkeyLoop()
}

func runHotkeyLoop() {
	hk := cfg.Hotkey
	evChan := hook.Start()
	defer hook.End()

	// pressed tracks every rawcode that is currently held down.
	pressed := make(map[uint16]bool)
	// comboFired prevents retriggering while keys are still held (debounce key-hold repeats).
	comboFired := false

	info("[HOTKEY] Listening for %s (%s mode) — Ctrl+C to quit", cfg.HotkeyStr, cfg.HotkeyMode)

	for ev := range evChan {
		rc := ev.Rawcode

		switch {
		// ── Key DOWN (kind=3) or HOLD repeat (kind=4) ──────────────────
		case ev.Kind == 3 || ev.Kind == 4:
			isRepeat := pressed[rc] // true if this is a hold-repeat event
			pressed[rc] = true

			if debugMode && !isRepeat {
				dbg("[KEY] Down  rawcode=%-6d char=%q  state=%s  combo=%v",
					rc, string([]rune{ev.Keychar}), getState(), comboFired)
			}

			// Fire the combo exactly once per press (skip hold-repeat events)
			if !isRepeat && hk.isTriggered(pressed) && !comboFired {
				comboFired = true
				info("[HOTKEY] Combo activated: %s", cfg.HotkeyStr)
				onHotkeyDown()
			}

		// ── Key UP (kind=5) ────────────────────────────────────────────
		case ev.Kind == 5:
			dbg("[KEY] Up    rawcode=%-6d char=%q  state=%s  combo=%v",
				rc, string([]rune{ev.Keychar}), getState(), comboFired)
			delete(pressed, rc)

			// Any key that belongs to our hotkey combo being released:
			if hk.isAnyKey(rc) {
				if comboFired {
					comboFired = false
					dbg("[KEY] Hotkey key released (rawcode=%d)", rc)
					onHotkeyUp()
				}
			}
		}
	}
}

// onHotkeyDown is called once per hotkey press (not on hold-repeat).
func onHotkeyDown() {
	mu.Lock()
	state := appState
	mu.Unlock()

	dbg("[ACTION] onHotkeyDown — current state: %s", state)

	switch state {
	case StateIdle:
		// Both modes start recording on first press.
		info("[ACTION] Starting recording...")
		startRecordingLocked()

	case StateRecording:
		if cfg.HotkeyMode == ModeToggle {
			info("[ACTION] Toggle stop — stopping recording...")
			doStop()
		} else {
			dbg("[ACTION] Hold mode: ignoring keydown while recording")
		}

	case StateProcessing:
		info("[ACTION] Hotkey ignored — ASR processing in progress")
	}
}

// onHotkeyUp is called when any key in the hotkey combo is released.
func onHotkeyUp() {
	dbg("[ACTION] onHotkeyUp — current state: %s  mode: %s", getState(), cfg.HotkeyMode)

	if cfg.HotkeyMode != ModeHold {
		return
	}
	mu.Lock()
	state := appState
	mu.Unlock()

	if state == StateRecording {
		info("[ACTION] Hold released — stopping recording...")
		doStop()
	}
}

// startRecordingLocked starts a new Recorder goroutine.
// Caller should NOT hold mu when this is called — it acquires mu internally.
func startRecordingLocked() {
	mu.Lock()
	defer mu.Unlock()

	if appState != StateIdle {
		dbg("[REC] startRecordingLocked: unexpected state %s, aborting", appState)
		return
	}

	var device *portaudio.DeviceInfo
	if cfg.SelectedDevice >= 0 && cfg.SelectedDevice < len(cachedDevices) {
		device = cachedDevices[cfg.SelectedDevice]
		dbg("[REC] Using device [%d] %s  (SR=%.0fHz  latency=%.3fs)",
			cfg.SelectedDevice, device.Name,
			device.DefaultSampleRate, device.DefaultLowInputLatency.Seconds())
	} else {
		host, err := portaudio.DefaultHostApi()
		if err != nil || host == nil || host.DefaultInputDevice == nil {
			info("[ERROR] No usable audio input device — cannot record")
			return
		}
		device = host.DefaultInputDevice
		dbg("[REC] Falling back to PortAudio default device: %s", device.Name)
	}

	rec := NewRecorder(device)
	activeRec = rec
	appState = StateRecording
	info("[REC] Recording started on: %s  (press %s again to stop)", device.Name, cfg.HotkeyStr)
	rec.Start()
}

// doStop stops the active recorder and spawns the async processing goroutine.
func doStop() {
	mu.Lock()
	rec := activeRec
	activeRec = nil
	if appState == StateRecording {
		appState = StateProcessing
	}
	mu.Unlock()

	if rec == nil {
		info("[WARN] doStop called with no active recorder")
		setState(StateIdle)
		return
	}

	go processRecording(rec)
}

// processRecording is the async pipeline: stop → WAV → ASR → paste.
// Runs entirely outside the hotkey goroutine.
func processRecording(rec *Recorder) {
	defer func() {
		setState(StateIdle)
		info("[PROC] Pipeline complete — waiting for %s...", cfg.HotkeyStr)
	}()

	info("[PROC] Signalling recorder to stop...")
	rec.Stop()

	dbg("[PROC] Waiting for recorder goroutine to finish writing WAV...")
	wavPath, audioDur, err := rec.Wait()
	if err != nil {
		info("[ERROR] Recorder error: %v", err)
		return
	}
	info("[PROC] Audio captured: %.2fs  →  %s", audioDur.Seconds(), wavPath)
	defer func() {
		dbg("[PROC] Removing temp file: %s", wavPath)
		os.Remove(wavPath)
	}()

	if audioDur < 250*time.Millisecond {
		info("[PROC] Recording too short (%.0fms < 250ms) — skipping ASR", float64(audioDur.Milliseconds()))
		return
	}

	info("[PROC] Sending %.2fs of audio to ASR: %s", audioDur.Seconds(), cfg.ASRURL)
	dbg("[PROC] ASR params: engine=%q language=%q timeout=%ds retries=%d",
		cfg.ASREngine, cfg.Language, cfg.ASRTimeout, cfg.ASRRetries)

	asrStart := time.Now()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.ASRTimeout)*time.Second,
	)
	defer cancel()

	transcript, err := transcribeFile(ctx, cfg, wavPath)
	asrElapsed := time.Since(asrStart)

	if err != nil {
		info("[ERROR] ASR failed after %.2fs: %v", asrElapsed.Seconds(), err)
		return
	}

	if strings.TrimSpace(transcript) == "" {
		info("[PROC] ASR returned empty transcript (no speech detected) — %.2fs", asrElapsed.Seconds())
		return
	}

	info("[PROC] ASR completed in %.2fs — %d chars", asrElapsed.Seconds(), len(transcript))
	dbg("[PROC] Transcript: %q", clip(transcript, 200))

	info("[PROC] Pasting %d chars...", len(transcript))
	pasteText(transcript)
	info("[PROC] ✓ Pasted successfully")
}
