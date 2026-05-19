package install

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestEnsureWhisperModel_CacheHit(t *testing.T) {
	body := []byte("fake-whisper-cached-payload")
	fakeSHA := sha256Hex(body)
	dir := t.TempDir()
	dest := filepath.Join(dir, WhisperModelFilename)
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}

	hits := atomic.Int64{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "should not be called on cache-hit", http.StatusInternalServerError)
	}))
	defer hs.Close()

	t.Setenv(envWhisperModelURL, hs.URL)
	t.Setenv(envWhisperModelSHA256, fakeSHA)

	if err := EnsureWhisperModel(context.Background(), dir, nil); err != nil {
		t.Fatalf("EnsureWhisperModel: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("server hit %d times on cache-hit path; expected 0", hits.Load())
	}
}

func TestEnsureWhisperModel_CorruptRedownload(t *testing.T) {
	correctBody := []byte("fake-whisper-correct-content")
	correctSHA := sha256Hex(correctBody)
	dir := t.TempDir()
	dest := filepath.Join(dir, WhisperModelFilename)
	// Seed a corrupt file — SHA will not match, triggering re-download.
	if err := os.WriteFile(dest, []byte("corrupt-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(correctBody)
	}))
	defer hs.Close()

	t.Setenv(envWhisperModelURL, hs.URL)
	t.Setenv(envWhisperModelSHA256, correctSHA)

	if err := EnsureWhisperModel(context.Background(), dir, nil); err != nil {
		t.Fatalf("EnsureWhisperModel: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(correctBody) {
		t.Fatalf("file not re-downloaded: got %q, want %q", got, correctBody)
	}
}

func TestEnsureWhisperModel_EnvOverride(t *testing.T) {
	body := []byte("fake-whisper-from-env-url")
	fakeSHA := sha256Hex(body)
	dir := t.TempDir()

	hits := atomic.Int64{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer hs.Close()

	// Override both URL and SHA via env; default constants must NOT be used.
	t.Setenv(envWhisperModelURL, hs.URL)
	t.Setenv(envWhisperModelSHA256, fakeSHA)

	if err := EnsureWhisperModel(context.Background(), dir, nil); err != nil {
		t.Fatalf("EnsureWhisperModel: %v", err)
	}
	if hits.Load() == 0 {
		t.Fatal("env URL override not honoured: expected ≥1 hit on custom server")
	}
}
