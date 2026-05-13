package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// transcribeFile sends the WAV file at wavPath to the remote ASR /asr endpoint
// and returns the plain-text transcript.
//
// Compatible with:
//   - ahmetoner/whisper-asr-webservice  (faster_whisper / openai_whisper engines)
//   - any server implementing the same multipart POST /asr?task=transcribe&output=txt API
func transcribeFile(ctx context.Context, c *Config, wavPath string) (string, error) {
	endpoint := strings.TrimRight(c.ASRURL, "/") + "/asr"
	q := url.Values{}
	q.Set("task", "transcribe")
	q.Set("output", "txt")
	q.Set("encode", "true") // ask server to re-encode; extra safety for edge cases

	if c.Language != "" && c.Language != "auto" {
		q.Set("language", c.Language)
		dbg("[ASR] Language set to: %q", c.Language)
	} else {
		dbg("[ASR] Language: server-side auto-detect")
	}

	if strings.EqualFold(c.ASREngine, "faster_whisper") {
		q.Set("vad_filter", "true")
		dbg("[ASR] VAD filter: enabled (faster_whisper engine)")
	}

	fullURL := endpoint + "?" + q.Encode()
	dbg("[ASR] Full endpoint URL: %s", fullURL)

	// ── Read audio file ────────────────────────────────────────────────
	dbg("[ASR] Reading WAV file: %s", wavPath)
	fileBytes, err := os.ReadFile(wavPath)
	if err != nil {
		return "", fmt.Errorf("read WAV %s: %w", wavPath, err)
	}
	dbg("[ASR] WAV size: %d bytes (%.1f KiB)", len(fileBytes), float64(len(fileBytes))/1024)

	filename := filepath.Base(wavPath)
	client := &http.Client{
		Timeout: time.Duration(c.ASRTimeout) * time.Second,
	}

	var lastErr error
	for attempt := 1; attempt <= c.ASRRetries; attempt++ {
		if attempt > 1 {
			// Exponential back-off: 2s, 4s, 8s, …
			wait := time.Duration(1<<uint(attempt-1)) * time.Second
			info("[ASR] Retry %d/%d in %s...", attempt, c.ASRRetries, wait)
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("context cancelled while waiting for retry: %w", ctx.Err())
			case <-time.After(wait):
			}
		}

		dbg("[ASR] Attempt %d/%d — building multipart body for %q", attempt, c.ASRRetries, filename)
		body, contentType, err := buildMultipartBody(fileBytes, filename)
		if err != nil {
			return "", fmt.Errorf("build multipart body: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, body)
		if err != nil {
			return "", fmt.Errorf("build HTTP request: %w", err)
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Accept", "text/plain")

		dbg("[ASR] Sending POST to %s (attempt %d)...", endpoint, attempt)
		tStart := time.Now()

		resp, err := client.Do(req)
		roundTrip := time.Since(tStart)

		if err != nil {
			lastErr = fmt.Errorf("HTTP POST: %w", err)
			info("[ASR] Attempt %d failed (%.2fs): %v", attempt, roundTrip.Seconds(), lastErr)
			continue
		}

		dbg("[ASR] Response received in %.3fs: HTTP %d  Content-Type: %s",
			roundTrip.Seconds(), resp.StatusCode, resp.Header.Get("Content-Type"))

		body2, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if readErr != nil {
			lastErr = fmt.Errorf("read response body: %w", readErr)
			info("[ASR] Attempt %d: failed to read response body: %v", attempt, readErr)
			continue
		}

		dbg("[ASR] Response body (%d bytes): %q", len(body2), clip(string(body2), 500))

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, clip(string(body2), 200))
			info("[ASR] Attempt %d: server error: %v", attempt, lastErr)
			continue
		}

		transcript := strings.TrimSpace(string(body2))
		info("[ASR] Transcription received in %.2fs  (%d chars)", roundTrip.Seconds(), len(transcript))
		if debugMode && len(transcript) > 0 {
			dbg("[ASR] Full transcript: %q", clip(transcript, 400))
		}
		return transcript, nil
	}

	return "", fmt.Errorf("all %d ASR attempt(s) failed; last error: %w", c.ASRRetries, lastErr)
}

// buildMultipartBody wraps fileBytes in a multipart/form-data body.
// The field name "audio_file" matches what whisper-asr-webservice expects.
func buildMultipartBody(fileBytes []byte, filename string) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("audio_file", filename)
	if err != nil {
		return nil, "", fmt.Errorf("multipart CreateFormFile: %w", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		return nil, "", fmt.Errorf("multipart write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("multipart close: %w", err)
	}

	dbg("[ASR] Multipart body: %d bytes  content-type: %s", buf.Len(), w.FormDataContentType())
	return &buf, w.FormDataContentType(), nil
}

// clip returns s truncated to at most n bytes, appending "…" if shortened.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
