package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// asyncConvertWaitTimeout bounds how long an async-tier test waits for its
// callback. It is a generous safety net, NOT a latency assertion: the heavy
// 50MiB round-trip runs a full HTTP upload under the -race detector, and on a
// contended CI runner a tight 3s deadline intermittently loses the scheduler
// race (flake). The value only fails the test if the callback never fires.
const asyncConvertWaitTimeout = 30 * time.Second

// convertHandler asserts the markitdown /convert contract and returns the canned
// markdown.
func convertHandler(t *testing.T, hits *atomic.Int32, markdown string, status int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("convert method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/convert") {
			t.Errorf("convert path = %s, want .../convert", r.URL.Path)
		}
		// Drain the request body before responding: the async-tier payload is >5MB
		// and a server that replies mid-upload resets the connection (a real
		// markitdown sidecar reads the multipart file fully first).
		_, _ = io.Copy(io.Discard, r.Body)
		if status >= 400 {
			http.Error(w, "boom", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"markdown":"`+markdown+`"}`)
	}
}

// TestDocumentsSyncTier proves a ≤5MB document is converted synchronously and the
// markdown is returned.
func TestDocumentsSyncTier(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "# Titolo\\nparagrafo", http.StatusOK))
	defer srv.Close()

	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: srv.URL})
	defer dc.Stop(context.Background())

	res, err := dc.Convert(context.Background(), []byte("small doc"), "doc.pdf", nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if res.Status != ConvertSync {
		t.Errorf("status = %v, want ConvertSync", res.Status)
	}
	if !strings.Contains(res.Markdown, "Titolo") {
		t.Errorf("markdown = %q, want it to contain the converted text", res.Markdown)
	}
	if hits.Load() != 1 {
		t.Errorf("convert hits = %d, want 1", hits.Load())
	}
}

// TestDocumentsAsyncTier proves a 5-50MB document is accepted for async conversion
// (the call returns ConvertAsync immediately) and the goroutine completes,
// delivering the markdown over the result callback — drained goleak-clean by Stop.
func TestDocumentsAsyncTier(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "# big doc markdown", http.StatusOK))
	defer srv.Close()

	done := make(chan string, 1)
	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: srv.URL})

	payload := make([]byte, asyncTierMinBytes+1) // > 5MB → async tier
	res, err := dc.Convert(context.Background(), payload, "big.pdf",
		func(_ string, md string, _ error) { done <- md })
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if res.Status != ConvertAsync {
		t.Fatalf("status = %v, want ConvertAsync", res.Status)
	}

	select {
	case md := <-done:
		if !strings.Contains(md, "big doc markdown") {
			t.Errorf("async markdown = %q, want the converted text", md)
		}
	case <-time.After(asyncConvertWaitTimeout):
		t.Fatal("async conversion did not complete")
	}

	// Stop must drain the async goroutine (goleak-clean — the package TestMain
	// catches a leaked convert goroutine).
	dc.Stop(context.Background())
	if hits.Load() != 1 {
		t.Errorf("convert hits = %d, want 1", hits.Load())
	}
}

func TestDocumentTierAllowsExactlyFiveMiBSync(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "# exact five", http.StatusOK))
	defer srv.Close()

	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: srv.URL})
	defer dc.Stop(context.Background())

	res, err := dc.Convert(context.Background(), make([]byte, asyncTierMinBytes), "exact-5.pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != ConvertSync {
		t.Fatalf("status = %v, want ConvertSync", res.Status)
	}
}

func TestDocumentTierAllowsExactlyFiftyMiBAsync(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "# exact fifty", http.StatusOK))
	defer srv.Close()

	done := make(chan struct{}, 1)
	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: srv.URL})
	defer dc.Stop(context.Background())

	res, err := dc.Convert(context.Background(), make([]byte, refuseTierMinBytes), "exact-50.pdf",
		func(string, string, error) { done <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != ConvertAsync {
		t.Fatalf("status = %v, want ConvertAsync", res.Status)
	}
	select {
	case <-done:
	case <-time.After(asyncConvertWaitTimeout):
		t.Fatal("async exact 50MiB conversion did not finish")
	}
}

func TestDocumentTierRefusesOverFiftyMiB(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "unused", http.StatusOK))
	defer srv.Close()

	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: srv.URL})
	defer dc.Stop(context.Background())

	res, err := dc.Convert(context.Background(), make([]byte, refuseTierMinBytes+1), "over-50.pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != ConvertRefused {
		t.Fatalf("status = %v, want ConvertRefused", res.Status)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d, want 0", hits.Load())
	}
}

// TestDocumentsAsyncTierUsesPerRequestCallbacks proves async conversion results
// are delivered to the callback passed with that Convert call, not through mutable
// client-wide state that a later document can overwrite.
func TestDocumentsAsyncTierUsesPerRequestCallbacks(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"markdown":"converted markdown"}`)
	}))
	defer srv.Close()

	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: srv.URL})
	defer dc.Stop(context.Background())

	first := make(chan string, 1)
	second := make(chan string, 1)
	payload := make([]byte, asyncTierMinBytes+1)
	if _, err := dc.Convert(context.Background(), payload, "one.pdf",
		func(fileName, md string, err error) {
			if err != nil {
				first <- "error: " + err.Error()
				return
			}
			first <- fileName + ":" + md
		}); err != nil {
		t.Fatalf("first Convert: %v", err)
	}
	if _, err := dc.Convert(context.Background(), payload, "two.pdf",
		func(fileName, md string, err error) {
			if err != nil {
				second <- "error: " + err.Error()
				return
			}
			second <- fileName + ":" + md
		}); err != nil {
		t.Fatalf("second Convert: %v", err)
	}

	var gotFirst, gotSecond string
	select {
	case gotFirst = <-first:
	case <-time.After(asyncConvertWaitTimeout):
		t.Fatal("first async conversion did not call its callback")
	}
	select {
	case gotSecond = <-second:
	case <-time.After(asyncConvertWaitTimeout):
		t.Fatal("second async conversion did not call its callback")
	}
	if !strings.HasPrefix(gotFirst, "one.pdf:") {
		t.Fatalf("first callback got %q, want one.pdf result", gotFirst)
	}
	if !strings.HasPrefix(gotSecond, "two.pdf:") {
		t.Fatalf("second callback got %q, want two.pdf result", gotSecond)
	}
}

// TestDocumentsRefuseTier proves a >50MB document is refused with a user-facing
// message and NO sidecar call (T-13-08-SidecarDoS).
func TestDocumentsRefuseTier(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "should not be called", http.StatusOK))
	defer srv.Close()

	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: srv.URL})
	defer dc.Stop(context.Background())

	res, err := dc.Convert(context.Background(), make([]byte, refuseTierMinBytes+1), "huge.pdf", nil)
	if err != nil {
		t.Fatalf("Convert (refuse) must not error, it returns a refuse status: %v", err)
	}
	if res.Status != ConvertRefused {
		t.Fatalf("status = %v, want ConvertRefused", res.Status)
	}
	if res.Message == "" {
		t.Error("a refused document must carry a user-facing message")
	}
	if hits.Load() != 0 {
		t.Errorf("a refused document must NOT hit the sidecar, got %d", hits.Load())
	}
}

// TestDocumentsSyncSidecarError proves a sync-tier sidecar 5xx surfaces an error.
func TestDocumentsSyncSidecarError(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(convertHandler(t, &hits, "", http.StatusServiceUnavailable))
	defer srv.Close()

	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: srv.URL})
	defer dc.Stop(context.Background())

	if _, err := dc.Convert(context.Background(), []byte("small"), "doc.pdf", nil); err == nil {
		t.Fatal("a sync sidecar 5xx must surface an error")
	}
}

// TestDocumentsStopIdempotent proves Stop on a client with no in-flight async work
// is a clean no-op (idempotent, goleak-clean).
func TestDocumentsStopIdempotent(t *testing.T) {
	t.Parallel()
	dc := newDocumentsClient(MultimodalConfig{DocumentsBaseURL: "http://unused"})
	dc.Stop(context.Background())
	dc.Stop(context.Background()) // second call must not panic
}
