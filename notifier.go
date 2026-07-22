package main

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Notifier is how the pipeline reports what happened: state transitions,
// errors and finished transcripts. The pipeline knows nothing about the
// tray or the Wails runtime, which keeps the state machine testable with
// a fake implementation.
type Notifier interface {
	StateChanged(s State)
	PipelineError(err error)
	HistoryAdded(e HistoryEntry)
}

// appNotifier fans pipeline notifications out to the tray and the frontend.
// Frontend events need the Wails context, which exists only after startup:
// notifications before Bind still reach the tray (it buffers pre-ready
// states itself); the frontend is not loaded yet, so dropping its events
// is correct.
type appNotifier struct {
	tray *Tray

	mu  sync.RWMutex
	ctx context.Context
}

func newAppNotifier(tray *Tray) *appNotifier {
	return &appNotifier{tray: tray}
}

// Bind attaches the Wails context once startup provides it.
func (n *appNotifier) Bind(ctx context.Context) {
	n.mu.Lock()
	n.ctx = ctx
	n.mu.Unlock()
}

func (n *appNotifier) context() context.Context {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.ctx
}

func (n *appNotifier) StateChanged(s State) {
	n.tray.SetState(s)
	if ctx := n.context(); ctx != nil {
		runtime.EventsEmit(ctx, "state:changed", s.String())
	}
}

func (n *appNotifier) PipelineError(err error) {
	if ctx := n.context(); ctx != nil {
		runtime.EventsEmit(ctx, "pipeline:error", err.Error())
	}
}

func (n *appNotifier) HistoryAdded(e HistoryEntry) {
	if ctx := n.context(); ctx != nil {
		runtime.EventsEmit(ctx, "history:added", e)
	}
}
