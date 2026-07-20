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

	"log/slog"
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
		slog.Debug("[ASR] Language set", "language", c.Language)
	} else {
		slog.Debug("[ASR] Language: server-side auto-detect")
	}

	if strings.EqualFold(c.ASREngine, "faster_whisper") {
		q.Set("vad_filter", "true")
		slog.Debug("[ASR] VAD filter: enabled (faster_whisper engine)")
	}

	fullURL := endpoint + "?" + q.Encode()
	slog.Debug("[ASR] Endpoint ready", "url", fullURL)

	// ── Read audio file ────────────────────────────────────────────────
	slog.Debug("[ASR] Reading WAV file", "path", wavPath)
	fileBytes, err := os.ReadFile(wavPath)
	if err != nil {
		return "", fmt.Errorf("read WAV %s: %w", wavPath, err)
	}
	slog.Debug("[ASR] WAV file read", "bytes", len(fileBytes))

	filename := filepath.Base(wavPath)
	client := &http.Client{
		Timeout: time.Duration(c.ASRTimeout) * time.Second,
	}

	var lastErr error
	for attempt := 1; attempt <= c.ASRRetries; attempt++ {
		if attempt > 1 {
			// Exponential back-off: 2s, 4s, 8s, …
			wait := time.Duration(1<<uint(attempt-1)) * time.Second
			slog.Info("[ASR] Retrying", "attempt", attempt, "retries", c.ASRRetries, "wait", wait)
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("context cancelled while waiting for retry: %w", ctx.Err())
			case <-time.After(wait):
			}
		}

		slog.Debug("[ASR] Building multipart body", "attempt", attempt, "retries", c.ASRRetries, "filename", filename)
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
		if c.AuthHeaderName != "" {
			req.Header.Set(c.AuthHeaderName, c.AuthHeaderValue)
			slog.Debug("[ASR] Auth header attached", "header", c.AuthHeaderName)
		}

		slog.Debug("[ASR] Sending POST", "endpoint", endpoint, "attempt", attempt)
		tStart := time.Now()

		resp, err := client.Do(req)
		roundTrip := time.Since(tStart)

		if err != nil {
			lastErr = fmt.Errorf("HTTP POST: %w", err)
			slog.Warn("[ASR] Attempt failed", "attempt", attempt, "elapsed", roundTrip, "error", lastErr)
			continue
		}

		slog.Debug("[ASR] Response received", "elapsed", roundTrip,
			"status", resp.StatusCode, "contentType", resp.Header.Get("Content-Type"))

		body2, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if readErr != nil {
			lastErr = fmt.Errorf("read response body: %w", readErr)
			slog.Warn("[ASR] Reading response body failed", "attempt", attempt, "error", readErr)
			continue
		}

		slog.Debug("[ASR] Response body", "bytes", len(body2), "body", clip(string(body2), 500))

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, clip(string(body2), 200))
			slog.Warn("[ASR] Server error", "attempt", attempt, "error", lastErr)
			continue
		}

		transcript := strings.TrimSpace(string(body2))
		slog.Info("[ASR] Transcription received", "elapsed", roundTrip, "chars", len(transcript))
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

	return &buf, w.FormDataContentType(), nil
}

// clip returns s truncated to at most n bytes, appending "…" if shortened.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
