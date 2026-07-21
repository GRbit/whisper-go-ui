package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestPipeline builds a Pipeline with no Wails context (setState skips
// EventsEmit when ctx is nil) and a not-ready tray (SetState buffers).
func newTestPipeline(t *testing.T) *Pipeline {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	cfg := &configStore{}
	cfg.Set(defaultConfig())
	return NewPipeline(cfg, NewHistoryStore(HistoryRAM), NewTray())
}

// TestStopRecordingKeepsProcessingState guards the double-press race: two
// rapid hotkey presses while recording both read StateRecording in Toggle;
// the first stopRecording hands the recorder to the ASR goroutine and moves
// to Processing, the second finds activeRec == nil. It must not force the
// state back to Idle while transcription is still running.
func TestStopRecordingKeepsProcessingState(t *testing.T) {
	p := newTestPipeline(t)

	// State after the first press won the race: Processing, recorder taken.
	p.mu.Lock()
	p.state = StateProcessing
	p.activeRec = nil
	p.mu.Unlock()

	p.stopRecording() // the second press

	if got := p.State(); got != StateProcessing {
		t.Errorf("state after duplicate stopRecording = %v, want %v", got, StateProcessing)
	}
}

// TestExpirePasted covers the Pasted display timer callback directly:
// a current-generation timer reverts Pasted to Idle; a stale-generation
// timer (a recording took over in the meantime) must not touch the state.
func TestExpirePasted(t *testing.T) {
	p := newTestPipeline(t)

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
	p := newTestPipeline(t)

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
	p := newTestPipeline(t)

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
	p := newTestPipeline(t)

	p.mu.Lock()
	p.state = StateRecording
	p.activeRec = nil
	p.mu.Unlock()

	p.stopRecording()

	if got := p.State(); got != StateIdle {
		t.Errorf("state after stopRecording with lost recorder = %v, want %v", got, StateIdle)
	}
}
