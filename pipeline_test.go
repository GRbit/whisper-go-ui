package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeNotifier records every pipeline notification for assertions.
type fakeNotifier struct {
	mu     sync.Mutex
	states []State
	errors []error
	added  []HistoryEntry
}

func (f *fakeNotifier) StateChanged(s State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, s)
}

func (f *fakeNotifier) PipelineError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors = append(f.errors, err)
}

func (f *fakeNotifier) HistoryAdded(e HistoryEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, e)
}

func (f *fakeNotifier) statesSeen() []State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]State(nil), f.states...)
}

// newTestPipeline builds a Pipeline against a fakeNotifier, so tests can
// assert exactly which states and errors were announced.
func newTestPipeline(t *testing.T) (*Pipeline, *fakeNotifier) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := &configStore{}
	cfg.Set(defaultConfig())
	fake := &fakeNotifier{}
	return NewPipeline(cfg, NewHistoryStore(HistoryRAM, 0), fake), fake
}

// TestStopRecordingKeepsProcessingState guards the double-press race: two
// rapid hotkey presses while recording both read StateRecording in Toggle;
// the first stopRecording hands the recorder to the ASR goroutine and moves
// to Processing, the second finds activeRec == nil. It must not force the
// state back to Idle while transcription is still running.
func TestStopRecordingKeepsProcessingState(t *testing.T) {
	p, fake := newTestPipeline(t)

	// State after the first press won the race: Processing, recorder taken.
	p.mu.Lock()
	p.state = StateProcessing
	p.activeRec = nil
	p.mu.Unlock()

	p.stopRecording() // the second press

	if got := p.State(); got != StateProcessing {
		t.Errorf("state after duplicate stopRecording = %v, want %v", got, StateProcessing)
	}
	// The tray/UI must not even see a flicker to Idle.
	for _, s := range fake.statesSeen() {
		if s == StateIdle {
			t.Errorf("duplicate stopRecording announced Idle to the notifier")
		}
	}
}

// syncWriter is a goroutine-safe log sink: the processRecording goroutine
// logs concurrently with the test reading the captured output.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// TestStopRecordingLogsTransition guards the transition log: stopRecording
// used to mutate p.state under the lock and then call setState with the
// same value, so the Recording -> Processing transition was compared
// against itself and "[STATE] Transition" never logged.
//
// Test mechanics: a fake capture goroutine answers the Stop signal with an
// error result, so processRecording settles back to Idle without touching
// PortAudio or the network; the test polls for that settle before reading
// the captured log.
func TestStopRecordingLogsTransition(t *testing.T) {
	w := &syncWriter{}
	prev := slog.Default()
	slog.SetDefault(newLogger(w))
	defer slog.SetDefault(prev)

	p, _ := newTestPipeline(t)
	rec := NewRecorder(nil)
	go func() {
		<-rec.stopCh
		rec.resCh <- recResult{err: fmt.Errorf("fake capture aborted")}
	}()

	p.mu.Lock()
	p.state = StateRecording
	p.activeRec = rec
	p.mu.Unlock()

	p.stopRecording()

	deadline := time.Now().Add(2 * time.Second)
	for p.State() != StateIdle && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	logs := w.String()
	want := "from=recording to=transcribing"
	if !strings.Contains(logs, want) {
		t.Errorf("transition log missing %q in:\n%s", want, logs)
	}
}

// TestExpirePasted covers the Pasted display timer callback directly:
// a current-generation timer reverts Pasted to Idle; a stale-generation
// timer (a recording took over in the meantime) must not touch the state.
func TestExpirePasted(t *testing.T) {
	p, _ := newTestPipeline(t)

	p.mu.Lock()
	p.state = StatePasted
	p.mu.Unlock()
	gen := p.pastedGen.Add(1)

	p.expirePasted(gen)
	if got := p.State(); got != StateIdle {
		t.Errorf("state after current-gen expiry = %v, want %v", got, StateIdle)
	}

	// A recording takes over: generation bumped, state Recording.
	p.mu.Lock()
	p.state = StateRecording
	p.mu.Unlock()
	stale := gen
	p.pastedGen.Add(1)

	p.expirePasted(stale)
	if got := p.State(); got != StateRecording {
		t.Errorf("state after stale-gen expiry = %v, want %v", got, StateRecording)
	}
}

// TestExpirePastedTakeoverRace guards the clobber race: the old timer read
// the generation and the state in two separate lock acquisitions, so a
// recording that started exactly between check and set had its Recording
// state overwritten to Idle. A deterministic red test is impossible (the
// window sits between two lock acquisitions inside one function, with no
// injection point), so this is a stress probe: it interleaves the timer
// callback with a takeover that bumps the generation and sets Recording
// under one lock, exactly like startRecording does. Whichever order the
// two run in, the state must end up Recording; with the old two-step
// check this failed whenever the takeover hit the window.
func TestExpirePastedTakeoverRace(t *testing.T) {
	p, _ := newTestPipeline(t)

	for i := 0; i < 5000; i++ {
		p.mu.Lock()
		p.state = StatePasted
		p.mu.Unlock()
		gen := p.pastedGen.Add(1)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			p.expirePasted(gen)
		}()
		go func() {
			defer wg.Done()
			p.mu.Lock()
			p.pastedGen.Add(1)
			p.state = StateRecording
			p.mu.Unlock()
		}()
		wg.Wait()

		if got := p.State(); got != StateRecording {
			t.Fatalf("iteration %d: state = %v after takeover, want %v", i, got, StateRecording)
		}
	}
}

// TestRescanDevicesRefusedWhileBusy guards the rescan safety property:
// Terminate during an open capture stream is a cgo use-after-teardown, so
// a rescan must be refused unless the pipeline is Idle or Pasted.
func TestRescanDevicesRefusedWhileBusy(t *testing.T) {
	p, _ := newTestPipeline(t)
	for _, s := range []State{StateRecording, StateProcessing} {
		p.mu.Lock()
		p.state = s
		p.mu.Unlock()
		if _, err := p.RescanDevices(); err == nil {
			t.Errorf("RescanDevices in state %v: want error, got nil", s)
		}
	}
}

// TestRescanDevicesIdle exercises the allowed path against the real
// PortAudio library (no stream is opened; just a Terminate+Init+list
// cycle). It asserts the pipeline snapshot is updated to the same list.
func TestRescanDevicesIdle(t *testing.T) {
	p, _ := newTestPipeline(t)
	devices, err := p.RescanDevices()
	if err != nil {
		t.Fatalf("RescanDevices while idle: %v", err)
	}
	if got := len(p.Devices()); got != len(devices) {
		t.Errorf("pipeline snapshot has %d devices, rescan returned %d", got, len(devices))
	}
}

// TestStopActiveRecorderWaitsForCapture guards the shutdown teardown order:
// shutdown() calls StopActiveRecorder and then portaudio.Terminate, so
// StopActiveRecorder must not return while the capture goroutine may still
// be inside stream.Read (cgo use-after-teardown otherwise).
//
// Test mechanics: a stand-in capture goroutine reacts to the Stop signal
// with a deliberate 50ms delay before flipping `finished` and delivering
// the result. If StopActiveRecorder only signals Stop without waiting for
// the result, it returns during that delay and sees finished == false.
func TestStopActiveRecorderWaitsForCapture(t *testing.T) {
	p, _ := newTestPipeline(t)

	rec := NewRecorder(nil) // capture never started; we fake its goroutine
	var finished atomic.Bool
	go func() {
		<-rec.stopCh
		time.Sleep(50 * time.Millisecond)
		finished.Store(true)
		rec.resCh <- recResult{}
	}()

	p.mu.Lock()
	p.state = StateRecording
	p.activeRec = rec
	p.mu.Unlock()

	p.StopActiveRecorder()

	if !finished.Load() {
		t.Error("StopActiveRecorder returned before the capture goroutine finished")
	}
}

// TestStopRecordingWithoutRecorderResetsFromRecording covers the legitimate
// half of the rec == nil branch: if the state really is Recording but the
// recorder is gone, the pipeline must fall back to Idle, not get stuck.
func TestStopRecordingWithoutRecorderResetsFromRecording(t *testing.T) {
	p, _ := newTestPipeline(t)

	p.mu.Lock()
	p.state = StateRecording
	p.activeRec = nil
	p.mu.Unlock()

	p.stopRecording()

	if got := p.State(); got != StateIdle {
		t.Errorf("state after stopRecording with lost recorder = %v, want %v", got, StateIdle)
	}
}
