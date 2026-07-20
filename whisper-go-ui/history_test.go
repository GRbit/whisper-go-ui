package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func entry(text string) HistoryEntry {
	return HistoryEntry{Time: time.Now().Truncate(time.Second), Text: text, DurationSec: 1.5}
}

func TestHistoryRAMModeAddAndOrder(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := NewHistoryStore(HistoryRAM)

	s.Add(entry("first"))
	s.Add(entry("second"))

	all := s.All()
	if len(all) != 2 {
		t.Fatalf("len = %d", len(all))
	}
	if all[0].Text != "second" || all[1].Text != "first" {
		t.Errorf("All() must be newest-first, got %q, %q", all[0].Text, all[1].Text)
	}

	// RAM mode must not create the JSONL file.
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_DATA_HOME"), "whisper-go-ui", "history.jsonl")); !os.IsNotExist(err) {
		t.Error("RAM mode must not write history.jsonl")
	}
}

func TestHistoryCap(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := NewHistoryStore(HistoryRAM)
	for i := 0; i < maxHistoryEntries+50; i++ {
		s.Add(entry(strings.Repeat("x", 3)))
	}
	if n := len(s.All()); n != maxHistoryEntries {
		t.Errorf("len = %d, want cap %d", n, maxHistoryEntries)
	}
}

func TestHistoryDiskModePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s := NewHistoryStore(HistoryDisk)
	s.Add(entry("persisted"))

	// Simulate app restart.
	s2 := NewHistoryStore(HistoryDisk)
	all := s2.All()
	if len(all) != 1 || all[0].Text != "persisted" {
		t.Fatalf("restart lost history: %+v", all)
	}

	fi, err := os.Stat(filepath.Join(dir, "whisper-go-ui", "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("history perms = %o, want 600", perm)
	}
}

func TestHistoryRAMToDiskFlush(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s := NewHistoryStore(HistoryRAM)
	s.Add(entry("before switch"))
	s.SetMode(HistoryDisk)
	s.Add(entry("after switch"))

	s2 := NewHistoryStore(HistoryDisk)
	all := s2.All()
	if len(all) != 2 {
		t.Fatalf("expected both entries on disk after mode switch, got %+v", all)
	}
	if all[0].Text != "after switch" || all[1].Text != "before switch" {
		t.Errorf("order wrong: %q, %q", all[0].Text, all[1].Text)
	}
}

func TestHistoryClearTruncatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s := NewHistoryStore(HistoryDisk)
	s.Add(entry("bye"))
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(s.All()) != 0 {
		t.Error("RAM entries survive Clear")
	}

	s2 := NewHistoryStore(HistoryDisk)
	if n := len(s2.All()); n != 0 {
		t.Errorf("file entries survive Clear: %d", n)
	}
}

func TestHistorySkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	appDir := filepath.Join(dir, "whisper-go-ui")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"time":"2026-07-20T12:00:00Z","text":"good","durationSec":2}
this is not json
{"time":"2026-07-20T12:01:00Z","text":"also good","durationSec":3}
`
	if err := os.WriteFile(filepath.Join(appDir, "history.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewHistoryStore(HistoryDisk)
	if n := len(s.All()); n != 2 {
		t.Errorf("want 2 valid entries, got %d", n)
	}
}
