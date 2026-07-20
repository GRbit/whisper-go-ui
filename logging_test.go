package main

import (
	"bytes"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

// captureLog points the default slog logger at a buffer for one test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	prevLevel := logLevel.Level()
	t.Cleanup(func() {
		slog.SetDefault(prev)
		logLevel.Set(prevLevel)
	})
	slog.SetDefault(newLogger(&buf))
	return &buf
}

// Enabling debug at runtime (via SaveConfig) must produce debug lines with
// the same microsecond timestamp precision as enabling it at startup;
// previously only startup set the microseconds log flag.
func TestRuntimeDebugEnableMicrosecondTimestamps(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	buf := captureLog(t)
	logLevel.Set(slog.LevelInfo) // the app started with debug off

	a := NewApp()
	cfg := defaultConfig()
	a.cfg.Set(cfg)

	c := *cfg
	c.Debug = true
	if err := a.SaveConfig(c); err != nil {
		t.Fatal(err)
	}

	slog.Debug("[TEST] probe")
	line := buf.String()
	if line == "" {
		t.Fatal("debug line not printed after enabling debug at runtime")
	}
	if !regexp.MustCompile(`\d{2}:\d{2}:\d{2}\.\d{6}`).MatchString(line) {
		t.Errorf("debug line lacks microsecond timestamp: %q", line)
	}
}

// The level var must gate debug output both ways at runtime; info always
// prints.
func TestLogLevelTogglesDebugOutput(t *testing.T) {
	buf := captureLog(t)

	logLevel.Set(slog.LevelInfo)
	slog.Debug("[TEST] hidden")
	slog.Info("[TEST] visible")
	logLevel.Set(slog.LevelDebug)
	slog.Debug("[TEST] shown")
	logLevel.Set(slog.LevelInfo)
	slog.Debug("[TEST] hidden again")

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Errorf("debug line printed while level is Info:\n%s", out)
	}
	for _, want := range []string{"visible", "shown"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}
