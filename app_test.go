package main

import "testing"

// TestBoundMethodsSafeBeforeStartup guards the startup race: Wails binds
// App's methods before startup() runs, and the frontend calls them as soon
// as it loads. Every bound method the frontend touches on load must work
// on a freshly constructed App, before startup() has run.
func TestBoundMethodsSafeBeforeStartup(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	a := NewApp()

	cfg := a.GetConfig() // panicked before the fix: configStore held a nil *Config
	if cfg.ASRURL == "" {
		t.Errorf("GetConfig before startup returned zero config: %+v", cfg)
	}
	if got := a.GetState(); got != StateIdle.String() {
		t.Errorf("GetState before startup = %q, want %q", got, StateIdle.String())
	}
	if got := a.GetHistory(); got == nil {
		t.Errorf("GetHistory before startup returned nil, want empty slice")
	}
	if got := a.CaptureHotkey(); got != "" {
		t.Errorf("CaptureHotkey before startup = %q, want empty", got)
	}
	if got := a.ListInputDevices(); len(got) != 0 {
		t.Errorf("ListInputDevices before startup returned %d devices, want 0", len(got))
	}
}
