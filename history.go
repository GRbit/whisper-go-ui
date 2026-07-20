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
// flushes the current RAM entries so nothing already transcribed is lost.
func (s *HistoryStore) SetMode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	wantDisk := mode == HistoryDisk && s.path != ""
	if wantDisk == s.disk {
		return
	}
	s.disk = wantDisk

	if s.disk {
		loaded := s.loadFileLocked()
		if len(s.entries) > 0 {
			// RAM→disk: rewrite the file as loaded entries + RAM entries.
			merged := append(loaded, s.entries...)
			if len(merged) > maxHistoryEntries {
				merged = merged[len(merged)-maxHistoryEntries:]
			}
			s.entries = merged
			if err := s.rewriteFileLocked(); err != nil {
				info("[HIST] Flush to disk failed: %v", err)
			}
		} else {
			s.entries = loaded
		}
		info("[HIST] Disk mode on — %d entries at %s", len(s.entries), s.path)
	} else {
		info("[HIST] RAM mode on — file kept as-is, no further appends")
	}
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

func (s *HistoryStore) rewriteFileLocked() error {
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, e := range s.entries {
		data, err := json.Marshal(e)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		w.Write(data)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.path)
}
