package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"log/slog"
)

// State is the app-wide pipeline state, mirrored to the tray icon and the UI.
type State int

const (
	StateIdle       State = iota // waiting for the hotkey
	StateRecording               // capturing audio
	StateProcessing              // ASR request in flight
	StatePasted                  // paste done: cosmetic, reverts to Idle after 2s
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "waiting"
	case StateRecording:
		return "recording"
	case StateProcessing:
		return "transcribing"
	case StatePasted:
		return "pasted"
	default:
		return fmt.Sprintf("state(%d)", int(s))
	}
}

// pastedDisplayTime is how long the "pasted" tray state lingers before
// reverting to waiting.
const pastedDisplayTime = 2 * time.Second

// Pipeline owns the record → transcribe → paste state machine.
type Pipeline struct {
	cfg     *configStore
	history *HistoryStore
	tray    *Tray

	mu        sync.Mutex
	state     State
	activeRec *Recorder
	devices   []*portaudio.DeviceInfo // cached at startup

	pastedGen atomic.Uint64 // invalidates stale Pasted→Idle timers

	ctx context.Context // wails context for EventsEmit; set in Start
}

func NewPipeline(cfg *configStore, history *HistoryStore, tray *Tray) *Pipeline {
	return &Pipeline{cfg: cfg, history: history, tray: tray}
}

// Start stores the wails context and the cached device list.
func (p *Pipeline) Start(ctx context.Context, devices []*portaudio.DeviceInfo) {
	p.mu.Lock()
	p.ctx = ctx
	p.devices = devices
	p.mu.Unlock()
}

func (p *Pipeline) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// setState updates the state and mirrors it to the tray and the frontend.
func (p *Pipeline) setState(s State) {
	p.mu.Lock()
	old := p.state
	p.state = s
	ctx := p.ctx
	p.mu.Unlock()

	if old != s {
		slog.Info("[STATE] Transition", "from", old.String(), "to", s.String())
	}
	p.tray.SetState(s)
	if ctx != nil {
		runtime.EventsEmit(ctx, "state:changed", s.String())
	}
}

// emitError surfaces a pipeline error to the frontend.
func (p *Pipeline) emitError(err error) {
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "pipeline:error", err.Error())
	}
}

// Toggle is the hotkey action: Idle starts recording, Recording stops it,
// anything else is ignored.
func (p *Pipeline) Toggle() {
	p.mu.Lock()
	state := p.state
	p.mu.Unlock()

	switch state {
	case StateIdle, StatePasted:
		p.startRecording()
	case StateRecording:
		p.stopRecording()
	case StateProcessing:
		slog.Info("[ACTION] Hotkey ignored: ASR processing in progress")
	}
}

func (p *Pipeline) startRecording() {
	cfg := p.cfg.Get()

	p.mu.Lock()
	if p.state != StateIdle && p.state != StatePasted {
		p.mu.Unlock()
		return
	}

	var device *portaudio.DeviceInfo
	idx := pickDevice(p.devices, cfg.DeviceID)
	if idx >= 0 && idx < len(p.devices) {
		device = p.devices[idx]
	} else {
		host, err := portaudio.DefaultHostApi()
		if err != nil || host == nil || host.DefaultInputDevice == nil {
			p.mu.Unlock()
			slog.Error("[REC] No usable audio input device: cannot record")
			p.emitError(fmt.Errorf("no usable audio input device"))
			return
		}
		device = host.DefaultInputDevice
	}

	// Invalidate any pending Pasted→Idle timer: a new recording owns the state now.
	p.pastedGen.Add(1)

	rec := NewRecorder(device)
	p.activeRec = rec
	p.state = StateRecording
	p.mu.Unlock()

	slog.Info("[REC] Recording started", "device", device.Name)
	p.setState(StateRecording) // re-emit for tray/UI (state already set under lock)
	rec.Start()
}

func (p *Pipeline) stopRecording() {
	p.mu.Lock()
	rec := p.activeRec
	p.activeRec = nil
	state := p.state
	if state == StateRecording {
		p.state = StateProcessing
	}
	p.mu.Unlock()

	if rec == nil {
		// Two rapid presses can both enter stopRecording: the first one took
		// the recorder and owns the Processing state. Only fall back to Idle
		// when the state machine really was Recording with no recorder.
		slog.Warn("[REC] stopRecording called with no active recorder", "state", state.String())
		if state == StateRecording {
			p.setState(StateIdle)
		}
		return
	}

	p.setState(StateProcessing)
	go p.processRecording(rec)
}

// processRecording is the async pipeline: stop → WAV → ASR → paste.
func (p *Pipeline) processRecording(rec *Recorder) {
	cfg := p.cfg.Get()

	finish := func(err error) {
		if err != nil {
			slog.Error("[PROC] Pipeline error", "error", err)
			p.emitError(err)
		}
		p.setState(StateIdle)
	}

	rec.Stop()
	wavPath, audioDur, err := rec.Wait()
	if err != nil {
		finish(fmt.Errorf("recorder: %w", err))
		return
	}
	slog.Info("[PROC] Audio captured", "duration", audioDur, "path", wavPath)
	defer os.Remove(wavPath)

	if audioDur < 250*time.Millisecond {
		slog.Info("[PROC] Recording too short: skipping ASR", "duration", audioDur, "minimum", 250*time.Millisecond)
		finish(nil)
		return
	}

	slog.Info("[PROC] Sending audio to ASR", "duration", audioDur, "url", cfg.ASRURL)
	asrStart := time.Now()
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.ASRTimeout)*time.Second,
	)
	defer cancel()

	transcript, err := transcribeFile(ctx, &cfg, wavPath)
	asrElapsed := time.Since(asrStart)

	if err != nil {
		finish(fmt.Errorf("ASR failed after %.2fs: %w", asrElapsed.Seconds(), err))
		return
	}

	if strings.TrimSpace(transcript) == "" {
		slog.Info("[PROC] ASR returned empty transcript (no speech detected)", "elapsed", asrElapsed)
		finish(nil)
		return
	}

	slog.Info("[PROC] ASR completed", "elapsed", asrElapsed, "chars", len(transcript))

	pasted, pasteErr := deliverText(transcript, &cfg)
	switch {
	case pasteErr != nil:
		// Surface it, but keep going: the transcript still goes to history.
		slog.Error("[PROC] Delivery failed", "error", pasteErr)
		p.emitError(fmt.Errorf("delivery failed: %w", pasteErr))
	case pasted:
		slog.Info("[PROC] Pasted", "chars", len(transcript))
	case cfg.CopyToClipboard:
		slog.Info("[PROC] Copied to clipboard (auto-paste off)", "chars", len(transcript))
	default:
		slog.Info("[PROC] Recognized (delivery disabled, history only)", "chars", len(transcript))
	}

	entry := HistoryEntry{Time: time.Now(), Text: transcript, DurationSec: audioDur.Seconds()}
	p.history.Add(entry)

	p.mu.Lock()
	ctx2 := p.ctx
	p.mu.Unlock()
	if ctx2 != nil {
		runtime.EventsEmit(ctx2, "history:added", entry)
	}

	// No "pasted" state to announce when delivery failed or is disabled.
	if pasteErr != nil || (!pasted && !cfg.CopyToClipboard) {
		p.setState(StateIdle)
		return
	}

	// Show the "pasted" state for 2s, unless a new recording takes over.
	p.setState(StatePasted)
	gen := p.pastedGen.Add(1)
	time.AfterFunc(pastedDisplayTime, func() {
		if p.pastedGen.Load() == gen && p.State() == StatePasted {
			p.setState(StateIdle)
		}
	})
}

// StopActiveRecorder aborts a recording in progress (used at shutdown).
// It waits for the capture goroutine to finish: the caller tears down
// PortAudio right after, and Terminate during an active stream.Read is a
// cgo use-after-teardown. The abandoned WAV file, if any, is removed.
func (p *Pipeline) StopActiveRecorder() {
	p.mu.Lock()
	rec := p.activeRec
	p.activeRec = nil
	p.mu.Unlock()
	if rec == nil {
		return
	}
	rec.Stop()
	wavPath, _, err := rec.Wait()
	if err != nil {
		slog.Debug("[REC] Recorder finished with error during shutdown stop", "error", err)
	}
	if wavPath != "" {
		if rmErr := os.Remove(wavPath); rmErr != nil {
			slog.Debug("[REC] Removing abandoned WAV failed", "path", wavPath, "error", rmErr)
		}
	}
}
