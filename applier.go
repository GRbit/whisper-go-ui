package main

import "sync"

// configApplier live-applies a saved configuration. Components that react
// to config changes (logging, hotkey, history) subscribe once with a
// function comparing old vs new; SaveConfig only persists the config and
// calls Apply, so App stops hand-routing every field to every component.
type configApplier struct {
	mu   sync.Mutex
	subs []func(old, cur Config)
}

// Subscribe registers a live-apply reaction. Safe to call concurrently
// with Apply; the new subscriber is picked up by the next Apply.
func (ca *configApplier) Subscribe(f func(old, cur Config)) {
	ca.mu.Lock()
	ca.subs = append(ca.subs, f)
	ca.mu.Unlock()
}

// Apply runs every subscriber with the previous and the current config,
// in subscription order.
func (ca *configApplier) Apply(old, cur Config) {
	ca.mu.Lock()
	subs := make([]func(old, cur Config), len(ca.subs))
	copy(subs, ca.subs)
	ca.mu.Unlock()
	for _, f := range subs {
		f(old, cur)
	}
}
