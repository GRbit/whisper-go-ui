package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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
}

func NewHistoryStore(mode string) *HistoryStore {
	s := &HistoryStore{}
	dir, err := dataDir()
	if err != nil {
		info("[HIST] Data dir unavailable, disk mode disabled: %v", err)
	} else {
		s.path = filepath.Join(dir, "history.jsonl")
	}
	s.SetMode(mode)
	return s
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
		info("[HIST] Disk mode on — %d entries shown, new ones append to %s", len(s.entries), s.path)
	} else {
		info("[HIST] RAM mode on — file kept as-is, no further appends")
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
			info("[HIST] Append to %s failed: %v", s.path, err)
		}
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
			info("[HIST] Open %s: %v", s.path, err)
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
			dbg("[HIST] Skipping malformed line: %v", err)
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		info("[HIST] Scan %s: %v", s.path, err)
	}
	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}
	return entries
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
