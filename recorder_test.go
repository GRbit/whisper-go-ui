package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestBuildWAV checks the RIFF/PCM-16 header byte-for-byte for a tiny sample set.
func TestBuildWAV(t *testing.T) {
	samples := []int16{0, 1000, -1000, 32767}
	data := buildWAV(samples, 16000)

	wantLen := 44 + len(samples)*2
	if len(data) != wantLen {
		t.Fatalf("len = %d, want %d", len(data), wantLen)
	}

	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		t.Error("missing RIFF/WAVE magic")
	}
	if got := binary.LittleEndian.Uint32(data[4:8]); got != uint32(36+len(samples)*2) {
		t.Errorf("chunk size = %d", got)
	}
	if string(data[12:16]) != "fmt " {
		t.Error("missing fmt chunk")
	}
	if got := binary.LittleEndian.Uint16(data[20:22]); got != 1 {
		t.Errorf("audio format = %d, want 1 (PCM)", got)
	}
	if got := binary.LittleEndian.Uint16(data[22:24]); got != 1 {
		t.Errorf("channels = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(data[24:28]); got != 16000 {
		t.Errorf("sample rate = %d", got)
	}
	if got := binary.LittleEndian.Uint32(data[28:32]); got != 32000 {
		t.Errorf("byte rate = %d", got)
	}
	if got := binary.LittleEndian.Uint16(data[34:36]); got != 16 {
		t.Errorf("bits per sample = %d", got)
	}
	if string(data[36:40]) != "data" {
		t.Error("missing data chunk")
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != uint32(len(samples)*2) {
		t.Errorf("data size = %d", got)
	}

	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, samples)
	if !bytes.Equal(data[44:], buf.Bytes()) {
		t.Error("PCM payload mismatch")
	}
}
