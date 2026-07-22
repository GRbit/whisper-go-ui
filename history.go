package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"log/slog"
)

// maxHistoryEntries caps the in-memory history (and how much of the JSONL
// file is loaded back on startup).
const maxHistoryEntries = 200

// HistoryEntry is one completed transcription.
type HistoryEntry struct {
	Time        time.Time `json:"time"`
	Text        string    `json:"text"`
	DurationSec float64   `json:"durationSec"`
}

// HistoryStore keeps recent transcripts in RAM and, in disk mode, mirrors
// them to a JSONL file under the XDG data dir.
type HistoryStore struct {
	mu      sync.Mutex
	entries []HistoryEntry
	disk    bool
	path    string // JSONL file path; empty if data dir is unavailable
	limit   int    // max entries kept in the JSONL file; 0 = no limit
}

func NewHistoryStore(mode string, limit int) *HistoryStore {
	s := &HistoryStore{limit: limit}
	dir, err := dataDir()
	if err != nil {
		slog.Warn("[HIST] Data dir unavailable, disk mode disabled", "error", err)
	} else {
		s.path = filepath.Join(dir, "history.jsonl")
	}
	s.SetMode(mode)
	return s
}

// SetLimit changes the on-disk entry cap (0 = no limit) and compacts the
// file right away so a lowered limit takes effect without waiting for the
// next transcription.
func (s *HistoryStore) SetLimit(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit == s.limit {
		return
	}
	s.limit = limit
	slog.Info("[HIST] History file limit changed", "limit", limit)
	s.compactFileLocked()
}

// SetMode switches between RAM and disk persistence. Switching to disk
// leaves the file untouched: existing RAM entries stay visible in the view
// but are never written; only transcriptions made after the switch are
// appended to the file.
func (s *HistoryStore) SetMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wantDisk := mode == HistoryDisk && s.path != ""
	if wantDisk == s.disk {
		return
	}
	s.disk = wantDisk

	if s.disk {
		// View = file content + RAM entries the file does not have yet
		// (entries loaded during an earlier disk period would duplicate).
		loaded := s.loadFileLocked()
		merged := loaded
		for _, e := range s.entries {
			if !containsEntry(loaded, e) {
				merged = append(merged, e)
			}
		}
		if len(merged) > maxHistoryEntries {
			merged = merged[len(merged)-maxHistoryEntries:]
		}
		s.entries = merged
		slog.Info("[HIST] Disk mode on: new entries append to the file", "shown", len(s.entries), "path", s.path)
	} else {
		slog.Info("[HIST] RAM mode on: file kept as-is, no further appends")
	}
}

// containsEntry reports whether list already holds e (same time and text).
func containsEntry(list []HistoryEntry, e HistoryEntry) bool {
	for _, x := range list {
		if x.Text == e.Text && x.Time.Equal(e.Time) {
			return true
		}
	}
	return false
}

// Add records a new transcript.
func (s *HistoryStore) Add(e HistoryEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, e)
	if len(s.entries) > maxHistoryEntries {
		s.entries = s.entries[len(s.entries)-maxHistoryEntries:]
	}

	if s.disk {
		if err := s.appendFileLocked(e); err != nil {
			slog.Error("[HIST] Append failed", "path", s.path, "error", err)
		}
		s.compactFileLocked()
	}
}

// All returns entries newest-first.
func (s *HistoryStore) All() []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HistoryEntry, len(s.entries))
	for i, e := range s.entries {
		out[len(s.entries)-1-i] = e
	}
	return out
}

// Clear wipes RAM entries and truncates the JSONL file.
func (s *HistoryStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = nil
	if s.path != "" {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", s.path, err)
		}
	}
	return nil
}

// ── file helpers (caller holds s.mu) ─────────────────────────────────────────

func (s *HistoryStore) loadFileLocked() []HistoryEntry {
	f, err := os.Open(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("[HIST] Open failed", "path", s.path, "error", err)
		}
		return nil
	}
	defer f.Close()

	var entries []HistoryEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e HistoryEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			slog.Debug("[HIST] Skipping malformed line", "error", err)
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		slog.Warn("[HIST] Scan failed", "path", s.path, "error", err)
	}
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}
	return entries
}

// compactFileLocked truncates the JSONL file to its newest s.limit lines.
// No-op with limit 0 (unlimited), no file, or a file within the limit.
// The rewrite goes through a tmp file + rename so a crash mid-compaction
// cannot lose the history.
func (s *HistoryStore) compactFileLocked() {
	if s.limit <= 0 || s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("[HIST] Read for compaction failed", "path", s.path, "error", err)
		}
		return
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= s.limit || lines[0] == "" {
		return
	}
	keep := lines[len(lines)-s.limit:]

	tmp := s.path + ".tmp"
	content := strings.Join(keep, "\n") + "\n"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		slog.Warn("[HIST] Write for compaction failed", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		slog.Warn("[HIST] Rename for compaction failed", "path", s.path, "error", err)
		return
	}
	slog.Debug("[HIST] History file compacted", "dropped", len(lines)-s.limit, "kept", len(keep))
}

func (s *HistoryStore) appendFileLocked(e HistoryEntry) error {
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}
