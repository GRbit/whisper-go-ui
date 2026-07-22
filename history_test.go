package main

import (
	"fmt"
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
	s := NewHistoryStore(HistoryRAM, 0)

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
	s := NewHistoryStore(HistoryRAM, 0)
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

	s := NewHistoryStore(HistoryDisk, 0)
	s.Add(entry("persisted"))

	// Simulate app restart.
	s2 := NewHistoryStore(HistoryDisk, 0)
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

	s := NewHistoryStore(HistoryRAM, 0)
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
	s2 := NewHistoryStore(HistoryDisk, 0)
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

	s := NewHistoryStore(HistoryDisk, 0)
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
	s2 := NewHistoryStore(HistoryDisk, 0)
	persisted := s2.All()
	if len(persisted) != 2 {
		t.Fatalf("file must hold pre-existing + new entry only, got %+v", persisted)
	}
	if persisted[0].Text != "new in disk mode" || persisted[1].Text != "on disk" {
		t.Errorf("file content wrong: %q, %q", persisted[0].Text, persisted[1].Text)
	}
}

// countFileLines returns the number of non-empty lines in the history file.
func countFileLines(t *testing.T, dir string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "whisper-go-ui", "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// TestHistoryDiskLimitCleanupOnWrite: with a limit set, appending past it
// must truncate the file to the newest `limit` entries.
func TestHistoryDiskLimitCleanupOnWrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s := NewHistoryStore(HistoryDisk, 5)
	for i := 0; i < 8; i++ {
		s.Add(entry(fmt.Sprintf("entry %d", i)))
	}

	if n := countFileLines(t, dir); n != 5 {
		t.Errorf("file has %d lines, want 5 (limit)", n)
	}

	// The survivors must be the newest entries.
	s2 := NewHistoryStore(HistoryDisk, 5)
	all := s2.All()
	if len(all) != 5 || all[0].Text != "entry 7" || all[4].Text != "entry 3" {
		t.Errorf("wrong survivors after cleanup: %+v", all)
	}
}

// TestHistoryDiskLimitZeroUnlimited: limit 0 must never truncate.
func TestHistoryDiskLimitZeroUnlimited(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s := NewHistoryStore(HistoryDisk, 0)
	for i := 0; i < 8; i++ {
		s.Add(entry(fmt.Sprintf("entry %d", i)))
	}
	if n := countFileLines(t, dir); n != 8 {
		t.Errorf("file has %d lines, want all 8 (no limit)", n)
	}
}

// TestHistorySetLimitCompactsFile: lowering the limit compacts the file
// right away, not only on the next append.
func TestHistorySetLimitCompactsFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s := NewHistoryStore(HistoryDisk, 0)
	for i := 0; i < 8; i++ {
		s.Add(entry(fmt.Sprintf("entry %d", i)))
	}

	s.SetLimit(3)

	if n := countFileLines(t, dir); n != 3 {
		t.Errorf("file has %d lines after SetLimit(3), want 3", n)
	}
}

func TestHistoryClearTruncatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	s := NewHistoryStore(HistoryDisk, 0)
	s.Add(entry("bye"))
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(s.All()) != 0 {
		t.Error("RAM entries survive Clear")
	}

	s2 := NewHistoryStore(HistoryDisk, 0)
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

	s := NewHistoryStore(HistoryDisk, 0)
	if n := len(s.All()); n != 2 {
		t.Errorf("want 2 valid entries, got %d", n)
	}
}
