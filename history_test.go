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

// TestHistoryDiskModeSavesOnlyNewEntries: switching RAM->disk must not
// write the pre-existing RAM entries to the file; only transcriptions made
// after the switch are persisted. The view still shows everything.
func TestHistoryDiskModeSavesOnlyNewEntries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	file := filepath.Join(dir, "whisper-go-ui", "history.jsonl")

	s := NewHistoryStore(HistoryRAM)
	s.Add(entry("before switch"))
	s.SetMode(HistoryDisk)

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("switching to disk must not create/write the file")
	}

	s.Add(entry("after switch"))

	// The view keeps both; only the new entry reaches the file.
	all := s.All()
	if len(all) != 2 {
		t.Fatalf("view must keep both entries, got %+v", all)
	}
	s2 := NewHistoryStore(HistoryDisk)
	persisted := s2.All()
	if len(persisted) != 1 || persisted[0].Text != "after switch" {
		t.Errorf("file must hold only the post-switch entry, got %+v", persisted)
	}
}

// TestHistoryDiskRAMDiskRoundTrip: entries loaded from the file must not be
// duplicated in the view when switching disk->RAM->disk, and the file must
// still gain nothing but genuinely new entries.
func TestHistoryDiskRAMDiskRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s := NewHistoryStore(HistoryDisk)
	s.Add(entry("on disk"))
	s.SetMode(HistoryRAM)
	s.Add(entry("ram only"))
	s.SetMode(HistoryDisk)

	all := s.All()
	if len(all) != 2 {
		t.Fatalf("view must hold 2 entries without duplicates, got %+v", all)
	}
	if all[0].Text != "ram only" || all[1].Text != "on disk" {
		t.Errorf("order wrong: %q, %q", all[0].Text, all[1].Text)
	}

	s.Add(entry("new in disk mode"))
	s2 := NewHistoryStore(HistoryDisk)
	persisted := s2.All()
	if len(persisted) != 2 {
		t.Fatalf("file must hold pre-existing + new entry only, got %+v", persisted)
	}
	if persisted[0].Text != "new in disk mode" || persisted[1].Text != "on disk" {
		t.Errorf("file content wrong: %q, %q", persisted[0].Text, persisted[1].Text)
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
