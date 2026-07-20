package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func writeTestWAV(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.wav")
	if err := os.WriteFile(path, buildWAV([]int16{1, 2, 3, 4}, 16000), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func asrTestConfig(url string) *Config {
	c := defaultConfig()
	c.ASRURL = url
	c.ASRRetries = 1
	c.ASRTimeout = 5
	return c
}

// TestTranscribeRequestShape asserts the multipart field name, query params
// and auth header of the outgoing request, and the plain-text response path.
func TestTranscribeRequestShape(t *testing.T) {
	var gotReq *http.Request
	var gotField string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r.Clone(context.Background())
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		if r.MultipartForm != nil {
			for field := range r.MultipartForm.File {
				gotField = field
			}
		}
		w.Write([]byte("  hello world \n"))
	}))
	defer srv.Close()

	c := asrTestConfig(srv.URL)
	c.Language = "en"
	c.AuthHeaderName = "X-Api-Key"
	c.AuthHeaderValue = "sekrit"

	got, err := transcribeFile(context.Background(), c, writeTestWAV(t))
	if err != nil {
		t.Fatalf("transcribeFile: %v", err)
	}
	if got != "hello world" {
		t.Errorf("transcript = %q (must be trimmed)", got)
	}

	if gotReq.URL.Path != "/asr" {
		t.Errorf("path = %q", gotReq.URL.Path)
	}
	q := gotReq.URL.Query()
	if q.Get("task") != "transcribe" || q.Get("output") != "txt" || q.Get("encode") != "true" {
		t.Errorf("base query params wrong: %v", q)
	}
	if q.Get("language") != "en" {
		t.Errorf("language = %q", q.Get("language"))
	}
	if q.Get("vad_filter") != "true" {
		t.Errorf("vad_filter must be set for faster_whisper, query: %v", q)
	}
	if gotField != "audio_file" {
		t.Errorf("multipart field = %q, want audio_file", gotField)
	}
	if gotReq.Header.Get("X-Api-Key") != "sekrit" {
		t.Errorf("auth header missing, got %q", gotReq.Header.Get("X-Api-Key"))
	}
}

// TestTranscribeNoAuthHeader asserts nothing extra is sent when auth is unset,
// and auto language / non-faster_whisper engine omit their params.
func TestTranscribeNoAuthHeader(t *testing.T) {
	var q map[string][]string
	var apiKey []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		apiKey = r.Header.Values("X-Api-Key")
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := asrTestConfig(srv.URL)
	c.ASREngine = "openai_whisper"
	c.Language = "auto"

	if _, err := transcribeFile(context.Background(), c, writeTestWAV(t)); err != nil {
		t.Fatal(err)
	}
	if len(apiKey) != 0 {
		t.Errorf("no auth header expected, got %v", apiKey)
	}
	if _, ok := q["language"]; ok {
		t.Error("language=auto must not be sent")
	}
	if _, ok := q["vad_filter"]; ok {
		t.Error("vad_filter must not be sent for openai_whisper")
	}
}

// TestTranscribeRetriesOn500 asserts a 500 is retried and the second attempt's
// transcript is returned.
func TestTranscribeRetriesOn500(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("second try"))
	}))
	defer srv.Close()

	c := asrTestConfig(srv.URL)
	c.ASRRetries = 2

	got, err := transcribeFile(context.Background(), c, writeTestWAV(t))
	if err != nil {
		t.Fatalf("transcribeFile: %v", err)
	}
	if got != "second try" {
		t.Errorf("transcript = %q", got)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("server calls = %d, want 2", n)
	}
}

// TestTranscribeAllAttemptsFail asserts the terminal error wraps the last
// server error and mentions the attempt count.
func TestTranscribeAllAttemptsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	c := asrTestConfig(srv.URL)

	_, err := transcribeFile(context.Background(), c, writeTestWAV(t))
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "HTTP 403") {
		t.Errorf("error should carry last status: %v", err)
	}
}
