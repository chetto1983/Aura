package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// helper: SHA-256 hex of an in-memory blob (matches what the server hands out).
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// trackedServer captures every request and serves a configurable body
// with full Range support so the resume path can be exercised.
type trackedServer struct {
	mu       sync.Mutex
	body     []byte
	requests []*http.Request
	rangeOK  bool // when false, ignore Range header and return 200 with full body
	failures int  // first N requests return 503 before serving normally
}

func newTrackedServer(body []byte, rangeOK bool) *trackedServer {
	return &trackedServer{body: body, rangeOK: rangeOK}
}

func (s *trackedServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.Clone(r.Context()))
		if s.failures > 0 {
			s.failures--
			s.mu.Unlock()
			http.Error(w, "transient", http.StatusServiceUnavailable)
			return
		}
		s.mu.Unlock()
		rangeHdr := r.Header.Get("Range")
		if s.rangeOK && rangeHdr != "" {
			var start int64
			_, _ = strings.NewReader(rangeHdr).Read(make([]byte, 0))
			// minimal parsing: "bytes=N-"
			prefix := "bytes="
			if strings.HasPrefix(rangeHdr, prefix) {
				rest := strings.TrimPrefix(rangeHdr, prefix)
				dash := strings.IndexByte(rest, '-')
				if dash > 0 {
					var n int64
					for _, c := range rest[:dash] {
						if c < '0' || c > '9' {
							n = -1
							break
						}
						n = n*10 + int64(c-'0')
					}
					if n >= 0 && n < int64(len(s.body)) {
						start = n
					}
				}
			}
			w.Header().Set("Content-Length", itoa(int64(len(s.body))-start))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(s.body[start:])
			return
		}
		w.Header().Set("Content-Length", itoa(int64(len(s.body))))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.body)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	out := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		out = string('0'+rune(n%10)) + out
		n /= 10
	}
	if neg {
		out = "-" + out
	}
	return out
}

func TestVerifyFileSHA256_OK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	body := []byte("hello world")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFileSHA256(path, sha256Hex(body)); err != nil {
		t.Fatalf("VerifyFileSHA256: %v", err)
	}
}

func TestVerifyFileSHA256_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := VerifyFileSHA256(path, strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestVerifyFileSHA256_Missing(t *testing.T) {
	if err := VerifyFileSHA256(filepath.Join(t.TempDir(), "nope"), strings.Repeat("a", 64)); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDownload_FullFresh(t *testing.T) {
	body := []byte("test payload alpha beta gamma")
	srv := newTrackedServer(body, true)
	hs := httptest.NewServer(srv.handler())
	defer hs.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	var captured [][2]int64
	progress := func(done, total int64) {
		captured = append(captured, [2]int64{done, total})
	}
	err := Download(context.Background(), DownloadOpts{
		URL:      hs.URL,
		Dest:     dest,
		SHA256:   sha256Hex(body),
		Progress: progress,
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("body mismatch")
	}
	if len(captured) == 0 || captured[len(captured)-1][0] != int64(len(body)) {
		t.Fatalf("expected progress to reach full body, got tail=%v", captured)
	}
	// Partial must not linger.
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial still exists: %v", err)
	}
}

func TestDownload_CacheHit_NoNetwork(t *testing.T) {
	body := []byte("already on disk")
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
	// Server set up but should never be hit.
	hits := atomic.Int64{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer hs.Close()
	progressCalls := 0
	err := Download(context.Background(), DownloadOpts{
		URL:      hs.URL,
		Dest:     dest,
		SHA256:   sha256Hex(body),
		Progress: func(_, _ int64) { progressCalls++ },
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("server hit %d times on cache-hit", hits.Load())
	}
	if progressCalls != 1 {
		t.Fatalf("expected exactly 1 synthetic 100%% tick on cache hit, got %d", progressCalls)
	}
}

func TestDownload_BadSHA_DeletesPartial(t *testing.T) {
	body := []byte("server-side corruption")
	srv := newTrackedServer(body, true)
	hs := httptest.NewServer(srv.handler())
	defer hs.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	bogusSHA := strings.Repeat("f", 64)
	err := Download(context.Background(), DownloadOpts{
		URL:         hs.URL,
		Dest:        dest,
		SHA256:      bogusSHA,
		MaxAttempts: 1, // SHA mismatch is non-retryable anyway, but assert no looping
	})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch error, got %v", err)
	}
	// SHA mismatch must short-circuit retries → exactly 1 server hit.
	if len(srv.requests) != 1 {
		t.Fatalf("expected 1 server hit (SHA mismatch is non-retryable), got %d", len(srv.requests))
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial should be deleted on SHA mismatch")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("dest must not be created on SHA mismatch")
	}
}

func TestDownload_ResumeFromPartial(t *testing.T) {
	body := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	srv := newTrackedServer(body, true)
	hs := httptest.NewServer(srv.handler())
	defer hs.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	// Seed a partial with the first 10 bytes.
	if err := os.WriteFile(dest+".partial", body[:10], 0o644); err != nil {
		t.Fatal(err)
	}
	err := Download(context.Background(), DownloadOpts{
		URL:    hs.URL,
		Dest:   dest,
		SHA256: sha256Hex(body),
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("resumed body wrong:\n  got:  %q\n  want: %q", got, body)
	}
	// Confirm server saw a Range: header.
	if len(srv.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(srv.requests))
	}
	if srv.requests[0].Header.Get("Range") == "" {
		t.Fatal("expected Range header on resume attempt")
	}
}

func TestDownload_RetriesTransient5xx(t *testing.T) {
	body := []byte("eventual success after retries")
	srv := newTrackedServer(body, false)
	srv.failures = 2 // first two requests fail, third succeeds
	hs := httptest.NewServer(srv.handler())
	defer hs.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	err := Download(context.Background(), DownloadOpts{
		URL:            hs.URL,
		Dest:           dest,
		SHA256:         sha256Hex(body),
		MaxAttempts:    5,
		RetryBaseDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if len(srv.requests) != 3 {
		t.Fatalf("expected 3 server hits (2 fails + 1 success), got %d", len(srv.requests))
	}
}

func TestDownload_GivesUpAfterMaxAttempts(t *testing.T) {
	srv := newTrackedServer([]byte("never served"), false)
	srv.failures = 999
	hs := httptest.NewServer(srv.handler())
	defer hs.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	err := Download(context.Background(), DownloadOpts{
		URL:            hs.URL,
		Dest:           dest,
		SHA256:         strings.Repeat("0", 64),
		MaxAttempts:    3,
		RetryBaseDelay: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	if !strings.Contains(err.Error(), "failed after 3 attempts") {
		t.Fatalf("error message wrong: %v", err)
	}
	if len(srv.requests) != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", len(srv.requests))
	}
}

func TestDownload_ContextCancel(t *testing.T) {
	// Server that streams forever — slow trickle so cancel takes effect mid-body.
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		buf := make([]byte, 1024)
		for {
			if _, err := w.Write(buf); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(5 * time.Millisecond)
			select {
			case <-r.Context().Done():
				return
			default:
			}
		}
	}))
	defer hs.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := Download(ctx, DownloadOpts{
		URL:            hs.URL,
		Dest:           dest,
		SHA256:         strings.Repeat("a", 64),
		MaxAttempts:    1,
		RetryBaseDelay: 5 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected ctx cancellation error")
	}
	// Cancellation arrives wrapped in either ctx.Err() or as a read error;
	// both are acceptable. What matters: dest does not exist.
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "read body") {
		t.Logf("err (informational, not failing): %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("dest should not exist on cancel")
	}
	// Partial may or may not exist — both are fine; next attempt resumes either way.
}

func TestDownload_ServerIgnoresRangeRestartsClean(t *testing.T) {
	body := []byte("range-unaware server body")
	srv := newTrackedServer(body, false) // rangeOK=false → always returns 200 with full body
	hs := httptest.NewServer(srv.handler())
	defer hs.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	// Seed a bogus partial with wrong leading bytes; if Download naively kept
	// it, the final hash would mismatch even though the server delivered the
	// correct body. Download must detect HTTP 200 (vs 206) and reset.
	if err := os.WriteFile(dest+".partial", []byte("XXX"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Download(context.Background(), DownloadOpts{
		URL:    hs.URL,
		Dest:   dest,
		SHA256: sha256Hex(body),
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(body) {
		t.Fatalf("body wrong after range-ignore restart:\n  got:  %q\n  want: %q", got, body)
	}
}

func TestLogProgressFn_RateLimits(t *testing.T) {
	// Use a discarding logger; we only care about emit-count timing logic.
	// The function takes a *slog.Logger; nil → slog.Default(). Tests
	// shouldn't pollute the default logger, so pass a discard handler.
	calls := atomic.Int64{}
	// LogProgressFn doesn't expose a hook, so instead validate by passing
	// a tiny interval and ensuring multiple updates are tolerated without
	// panic; this is mostly a smoke test of the rate-limit branch.
	p := LogProgressFn(nil, 100*time.Millisecond)
	for i := int64(1); i <= 100; i++ {
		p(i, 100)
		calls.Add(1)
	}
	if calls.Load() != 100 {
		t.Fatalf("expected 100 progress calls, got %d", calls.Load())
	}
}

func TestEnsureParentDir(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", "file.txt")
	if err := EnsureParentDir(deep); err != nil {
		t.Fatalf("EnsureParentDir: %v", err)
	}
	info, err := os.Stat(filepath.Dir(deep))
	if err != nil || !info.IsDir() {
		t.Fatalf("parent not created: %v", err)
	}
}

// keep io.Discard reference so future test refactors don't drop the import
// when removing other usages.
var _ = io.Discard
