package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
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

// Pipeline owns the record -> transcribe -> paste state machine. It reports
// everything observable (state, errors, transcripts) through the Notifier
// and knows nothing about the tray or the Wails runtime.
type Pipeline struct {
	cfg     *configStore
	history *HistoryStore
	notify  Notifier

	mu        sync.Mutex
	state     State
	activeRec *Recorder
	devices   []*portaudio.DeviceInfo // snapshot; refreshed by RescanDevices

	pastedGen atomic.Uint64 // invalidates stale Pasted->Idle timers
}

func NewPipeline(cfg *configStore, history *HistoryStore, notify Notifier) *Pipeline {
	return &Pipeline{cfg: cfg, history: history, notify: notify}
}

// SetDevices stores the device list scanned at startup.
func (p *Pipeline) SetDevices(devices []*portaudio.DeviceInfo) {
	p.mu.Lock()
	p.devices = devices
	p.mu.Unlock()
}

func (p *Pipeline) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Devices returns the current device list snapshot.
func (p *Pipeline) Devices() []*portaudio.DeviceInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.devices
}

// RescanDevices re-enumerates PortAudio devices. PortAudio only notices
// hotplugged hardware after a full Terminate+Initialize cycle, and Terminate
// must never run while a capture stream is open. Holding p.mu with the state
// at Idle or Pasted guarantees that: the capture goroutine has exited before
// the state leaves Processing, and startRecording needs the lock to start a
// new one.
func (p *Pipeline) RescanDevices() ([]*portaudio.DeviceInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != StateIdle && p.state != StatePasted {
		return nil, fmt.Errorf("cannot rescan audio devices while %s", p.state)
	}

	slog.Info("[DEV] Rescanning PortAudio devices")
	if err := portaudio.Terminate(); err != nil {
		// Not fatal: happens when PortAudio was never initialized.
		slog.Warn("[DEV] PortAudio terminate before rescan failed", "error", err)
	}
	if err := portaudio.Initialize(); err != nil {
		return nil, fmt.Errorf("portaudio initialize: %w", err)
	}
	devices, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("portaudio devices: %w", err)
	}
	p.devices = devices
	slog.Info("[DEV] Device rescan complete", "count", len(devices))
	return devices, nil
}

// setState updates the state and announces it through the notifier.
func (p *Pipeline) setState(s State) {
	p.mu.Lock()
	old := p.state
	p.state = s
	p.mu.Unlock()

	p.announceState(old, s)
}

// announceState logs a transition and reports it to the notifier.
// Called outside the lock: notifier implementations must not run under p.mu.
func (p *Pipeline) announceState(old, s State) {
	if old != s {
		slog.Info("[STATE] Transition", "from", old.String(), "to", s.String())
	}
	p.notify.StateChanged(s)
}

// expirePasted reverts Pasted to Idle when the 2s display timer fires,
// unless a newer recording took the state over. The generation check and
// the transition happen under one lock acquisition: startRecording bumps
// pastedGen and sets Recording under the same lock, so a recording started
// between check and set can no longer be clobbered back to Idle.
func (p *Pipeline) expirePasted(gen uint64) {
	p.mu.Lock()
	if p.pastedGen.Load() != gen || p.state != StatePasted {
		p.mu.Unlock()
		slog.Debug("[STATE] Pasted display timer expired stale: ignoring")
		return
	}
	p.state = StateIdle
	p.mu.Unlock()

	p.announceState(StatePasted, StateIdle)
}

// emitError surfaces a pipeline error through the notifier.
func (p *Pipeline) emitError(err error) {
	p.notify.PipelineError(err)
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
	old := p.state
	p.state = StateRecording
	p.mu.Unlock()

	slog.Info("[REC] Recording started", "device", device.Name)
	p.announceState(old, StateRecording)
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

	p.announceState(state, StateProcessing)
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
	transcript, err := transcribe(&cfg, wavPath)
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
	p.notify.HistoryAdded(entry)

	// No "pasted" state to announce when delivery failed or is disabled.
	if pasteErr != nil || (!pasted && !cfg.CopyToClipboard) {
		p.setState(StateIdle)
		return
	}

	// Show the "pasted" state for 2s, unless a new recording takes over.
	p.setState(StatePasted)
	gen := p.pastedGen.Add(1)
	time.AfterFunc(pastedDisplayTime, func() { p.expirePasted(gen) })
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
