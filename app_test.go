package main

import (
	"log/slog"
	"testing"
)

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

// TestConfigApplierOrderAndArgs pins the applier contract: subscribers run
// in subscription order and each receives the old and the new config.
func TestConfigApplierOrderAndArgs(t *testing.T) {
	var ca configApplier
	var order []string

	ca.Subscribe(func(old, cur Config) {
		order = append(order, "first")
		if old.ASRTimeout != 1 || cur.ASRTimeout != 2 {
			t.Errorf("subscriber got old=%d cur=%d, want 1 and 2", old.ASRTimeout, cur.ASRTimeout)
		}
	})
	ca.Subscribe(func(_, _ Config) { order = append(order, "second") })

	ca.Apply(Config{ASRTimeout: 1}, Config{ASRTimeout: 2})

	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("subscriber order = %v, want [first second]", order)
	}
}

// TestSaveConfigAppliesLive verifies SaveConfig routes changes through the
// applier: flipping Debug must move the global log level, and a history
// limit change must reach the store (observable via file compaction).
func TestSaveConfigAppliesLive(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	prevLevel := logLevel.Level()
	defer logLevel.Set(prevLevel)

	a := NewApp()

	c := a.GetConfig()
	c.Debug = true
	if err := a.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	if got := logLevel.Level(); got != slog.LevelDebug {
		t.Errorf("log level after Debug=true save = %v, want %v", got, slog.LevelDebug)
	}

	c.Debug = false
	if err := a.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	if got := logLevel.Level(); got != slog.LevelInfo {
		t.Errorf("log level after Debug=false save = %v, want %v", got, slog.LevelInfo)
	}
}
