package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"time"

	"github.com/gordonklaus/portaudio"
	"log/slog"
)

// Audio constants shared with the rest of the package.
const (
	whisperSampleRate = 16000 // Hz, Whisper's native rate
	channels          = 1     // mono
	framesPerBuffer   = 2048  // ~46ms at 44100 Hz; ~128ms at 16000 Hz
)

// Recorder captures microphone audio into memory and writes a WAV file on demand.
type Recorder struct {
	device *portaudio.DeviceInfo
	stopCh chan struct{} // closed by Stop() to break the capture loop
	resCh  chan recResult
}

type recResult struct {
	wavPath  string
	duration time.Duration
	err      error
}

// NewRecorder creates a Recorder but does not start capturing yet.
func NewRecorder(device *portaudio.DeviceInfo) *Recorder {
	return &Recorder{
		device: device,
		stopCh: make(chan struct{}),
		resCh:  make(chan recResult, 1),
	}
}

// Start launches the capture goroutine.
// Call Stop() to signal it to finish, then Wait() to get the WAV path.
func (r *Recorder) Start() {
	slog.Debug("[REC] Start(): launching capture goroutine")
	go func() {
		path, dur, err := r.capture()
		slog.Debug("[REC] Capture goroutine finished", "path", path, "duration", dur, "error", err)
		r.resCh <- recResult{path, dur, err}
	}()
}

// Stop signals the capture goroutine to stop. Non-blocking; idempotent.
func (r *Recorder) Stop() {
	select {
	case <-r.stopCh:
		slog.Debug("[REC] Stop() called but stopCh already closed (idempotent)")
	default:
		slog.Debug("[REC] Stop() closing stopCh: capture loop will exit after current Read()")
		close(r.stopCh)
	}
}

// Wait blocks until the capture goroutine has finished writing the WAV file,
// then returns the WAV path and audio duration.
func (r *Recorder) Wait() (string, time.Duration, error) {
	slog.Debug("[REC] Wait() blocking on resCh...")
	res := <-r.resCh
	slog.Debug("[REC] Wait() unblocked", "path", res.wavPath, "duration", res.duration, "error", res.err)
	return res.wavPath, res.duration, res.err
}

// ── internal capture ──────────────────────────────────────────────────────────

func (r *Recorder) capture() (string, time.Duration, error) {
	dev := r.device
	actualSR := dev.DefaultSampleRate
	ratio := actualSR / float64(whisperSampleRate)

	slog.Debug("[REC] capture() starting", "device", dev.Name,
		"nativeSR", actualSR, "targetSR", whisperSampleRate, "ratio", ratio)

	// Allocate raw input buffer (native sample rate)
	inputBuf := make([]int16, framesPerBuffer)

	params := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   dev,
			Channels: channels,
			Latency:  dev.DefaultLowInputLatency,
		},
		SampleRate:      actualSR,
		FramesPerBuffer: framesPerBuffer,
	}

	slog.Debug("[REC] Opening PortAudio stream...")
	stream, err := portaudio.OpenStream(params, inputBuf)
	if err != nil {
		return "", 0, fmt.Errorf("portaudio.OpenStream: %w", err)
	}
	defer func() {
		slog.Debug("[REC] Stopping + closing PortAudio stream")
		if sErr := stream.Stop(); sErr != nil {
			slog.Debug("[REC] stream.Stop() warning", "error", sErr)
		}
		if cErr := stream.Close(); cErr != nil {
			slog.Debug("[REC] stream.Close() warning", "error", cErr)
		}
	}()

	slog.Debug("[REC] stream.Start()")
	if err := stream.Start(); err != nil {
		return "", 0, fmt.Errorf("portaudio stream.Start: %w", err)
	}

	// ── capture loop ───────────────────────────────────────────────────
	var (
		samples    []int16 // downsampled, 16-kHz
		cursor     float64
		frameCount int
	)
	startWall := time.Now()

	// Periodic progress ticker: fires every 2 s
	progressTicker := time.NewTicker(2 * time.Second)
	defer progressTicker.Stop()

	slog.Debug("[REC] Entering capture loop (stop signal on stopCh)...")

captureLoop:
	for {
		// ── Check stop signal (non-blocking) ───────────────────────
		select {
		case <-r.stopCh:
			elapsed := time.Since(startWall)
			slog.Debug("[REC] Stop signal received",
				"frame", frameCount, "elapsed", elapsed, "samples", len(samples))
			break captureLoop
		default:
		}

		// ── Periodic progress log ──────────────────────────────────
		select {
		case <-progressTicker.C:
			slog.Info("[REC] Recording in progress",
				"elapsed", time.Since(startWall),
				"frames", frameCount,
				"samples", len(samples),
			)
		default:
		}

		// ── Blocking read: returns after framesPerBuffer frames ───
		if err := stream.Read(); err != nil {
			slog.Error("[REC] stream.Read() failed: stopping capture", "error", err)
			break captureLoop
		}
		frameCount++

		// ── Downsample native SR → 16 kHz (nearest-neighbour) ─────
		// This is a simple decimation; no anti-alias filter, but perfectly
		// fine for voice/speech content.
		for cursor < float64(len(inputBuf)) {
			samples = append(samples, inputBuf[int(cursor)])
			cursor += ratio
		}
		cursor -= float64(len(inputBuf))
	}

	wallDur := time.Since(startWall)
	audioDur := time.Duration(float64(len(samples)) / float64(whisperSampleRate) * float64(time.Second))

	slog.Debug("[REC] Capture done", "frames", frameCount,
		"samples", len(samples), "wall", wallDur, "audio", audioDur)

	// Reject recordings that are too short to produce a meaningful transcript.
	const minSamples = whisperSampleRate / 10 // 100 ms
	if len(samples) < minSamples {
		return "", 0, fmt.Errorf(
			"recording too short: %d samples = %.0fms (need ≥ 100ms)",
			len(samples), float64(len(samples))*1000/float64(whisperSampleRate),
		)
	}

	// ── Build WAV and write to temp file ──────────────────────────────
	slog.Debug("[REC] Building WAV (PCM-16 mono)", "sampleRate", whisperSampleRate, "samples", len(samples))
	wavData := buildWAV(samples, whisperSampleRate)

	f, err := os.CreateTemp("", "wpaste-*.wav")
	if err != nil {
		return "", 0, fmt.Errorf("os.CreateTemp: %w", err)
	}
	wavPath := f.Name()
	slog.Debug("[REC] Writing WAV to temp file", "path", wavPath)

	if _, werr := f.Write(wavData); werr != nil {
		f.Close()
		os.Remove(wavPath)
		return "", 0, fmt.Errorf("write WAV to %s: %w", wavPath, werr)
	}
	if cerr := f.Close(); cerr != nil {
		os.Remove(wavPath)
		return "", 0, fmt.Errorf("close WAV temp file: %w", cerr)
	}

	slog.Debug("[REC] WAV written", "path", wavPath,
		"bytes", len(wavData), "audio", audioDur)

	return wavPath, audioDur, nil
}

// buildWAV encodes 16-bit signed PCM mono samples as a standard RIFF/WAV file.
func buildWAV(samples []int16, sampleRate int) []byte {
	dataBytes := uint32(len(samples) * 2) // 16-bit = 2 bytes/sample
	var buf bytes.Buffer

	// RIFF header
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataBytes)) // total file size - 8
	buf.WriteString("WAVE")

	// fmt sub-chunk (16 bytes for PCM)
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))           // sub-chunk size
	binary.Write(&buf, binary.LittleEndian, uint16(1))            // PCM = 1
	binary.Write(&buf, binary.LittleEndian, uint16(1))            // mono
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))   // sample rate
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2)) // byte rate (SR × channels × bps/8)
	binary.Write(&buf, binary.LittleEndian, uint16(2))            // block align (channels × bps/8)
	binary.Write(&buf, binary.LittleEndian, uint16(16))           // bits per sample

	// data sub-chunk
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, dataBytes)
	binary.Write(&buf, binary.LittleEndian, samples)

	return buf.Bytes()
}
