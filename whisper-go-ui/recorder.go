package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gordonklaus/portaudio"
)

// Audio constants shared with the rest of the package.
const (
	whisperSampleRate = 16000 // Hz — Whisper's native rate
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
	dbg("[REC] Start() — launching capture goroutine")
	go func() {
		path, dur, err := r.capture()
		dbg("[REC] Capture goroutine finished: path=%q dur=%.3fs err=%v", path, dur.Seconds(), err)
		r.resCh <- recResult{path, dur, err}
	}()
}

// Stop signals the capture goroutine to stop. Non-blocking; idempotent.
func (r *Recorder) Stop() {
	select {
	case <-r.stopCh:
		dbg("[REC] Stop() called but stopCh already closed (idempotent)")
	default:
		dbg("[REC] Stop() closing stopCh — capture loop will exit after current Read()")
		close(r.stopCh)
	}
}

// Wait blocks until the capture goroutine has finished writing the WAV file,
// then returns the WAV path and audio duration.
func (r *Recorder) Wait() (string, time.Duration, error) {
	dbg("[REC] Wait() blocking on resCh...")
	res := <-r.resCh
	dbg("[REC] Wait() unblocked: path=%q dur=%.3fs err=%v", res.wavPath, res.duration.Seconds(), res.err)
	return res.wavPath, res.duration, res.err
}

// ── internal capture ──────────────────────────────────────────────────────────

func (r *Recorder) capture() (string, time.Duration, error) {
	dev := r.device
	actualSR := dev.DefaultSampleRate
	ratio := actualSR / float64(whisperSampleRate)

	dbg("[REC] capture(): device=%q  nativeSR=%.0fHz  targetSR=%dHz  ratio=%.4f",
		dev.Name, actualSR, whisperSampleRate, ratio)

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

	dbg("[REC] Opening PortAudio stream...")
	stream, err := portaudio.OpenStream(params, inputBuf)
	if err != nil {
		return "", 0, fmt.Errorf("portaudio.OpenStream: %w", err)
	}
	defer func() {
		dbg("[REC] Stopping + closing PortAudio stream")
		if sErr := stream.Stop(); sErr != nil {
			dbg("[REC] stream.Stop() warning: %v", sErr)
		}
		if cErr := stream.Close(); cErr != nil {
			dbg("[REC] stream.Close() warning: %v", cErr)
		}
	}()

	dbg("[REC] stream.Start()")
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

	// Periodic progress ticker — fires every 2 s
	progressTicker := time.NewTicker(2 * time.Second)
	defer progressTicker.Stop()

	dbg("[REC] Entering capture loop (stop signal on stopCh)...")

captureLoop:
	for {
		// ── Check stop signal (non-blocking) ───────────────────────
		select {
		case <-r.stopCh:
			elapsed := time.Since(startWall)
			dbg("[REC] Stop signal received at frame %d  elapsed=%.3fs  samples=%d",
				frameCount, elapsed.Seconds(), len(samples))
			break captureLoop
		default:
		}

		// ── Periodic progress log ──────────────────────────────────
		select {
		case <-progressTicker.C:
			info("[REC] ... %.1fs recorded  (%d frames, %d samples, %.1f KiB)",
				time.Since(startWall).Seconds(),
				frameCount,
				len(samples),
				float64(len(samples)*2)/1024,
			)
		default:
		}

		// ── Blocking read — returns after framesPerBuffer frames ───
		if err := stream.Read(); err != nil {
			log.Printf("[REC] stream.Read() error: %v — stopping capture", err)
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

	dbg("[REC] Capture done: %d frames  %d samples  wall=%.3fs  audio=%.3fs",
		frameCount, len(samples), wallDur.Seconds(), audioDur.Seconds())

	// Reject recordings that are too short to produce a meaningful transcript.
	const minSamples = whisperSampleRate / 10 // 100 ms
	if len(samples) < minSamples {
		return "", 0, fmt.Errorf(
			"recording too short: %d samples = %.0fms (need ≥ 100ms)",
			len(samples), float64(len(samples))*1000/float64(whisperSampleRate),
		)
	}

	// ── Build WAV and write to temp file ──────────────────────────────
	dbg("[REC] Building WAV (PCM-16 mono %dHz, %d samples)...", whisperSampleRate, len(samples))
	wavData := buildWAV(samples, whisperSampleRate)

	f, err := os.CreateTemp("", "wpaste-*.wav")
	if err != nil {
		return "", 0, fmt.Errorf("os.CreateTemp: %w", err)
	}
	wavPath := f.Name()
	dbg("[REC] Writing WAV to temp file: %s", wavPath)

	if _, werr := f.Write(wavData); werr != nil {
		f.Close()
		os.Remove(wavPath)
		return "", 0, fmt.Errorf("write WAV to %s: %w", wavPath, werr)
	}
	if cerr := f.Close(); cerr != nil {
		os.Remove(wavPath)
		return "", 0, fmt.Errorf("close WAV temp file: %w", cerr)
	}

	dbg("[REC] WAV successfully written: %s  (%.1f KiB, %.3fs audio)",
		wavPath, float64(len(wavData))/1024, audioDur.Seconds())

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
