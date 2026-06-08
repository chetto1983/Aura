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

	res, err := dc.Convert(context.Background(), []byte("small doc"), "doc.pdf")
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
	dc.OnAsyncResult = func(_ string, md string, _ error) { done <- md }

	payload := make([]byte, asyncTierMinBytes+1) // > 5MB → async tier
	res, err := dc.Convert(context.Background(), payload, "big.pdf")
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
	case <-time.After(3 * time.Second):
		t.Fatal("async conversion did not complete")
	}

	// Stop must drain the async goroutine (goleak-clean — the package TestMain
	// catches a leaked convert goroutine).
	dc.Stop(context.Background())
	if hits.Load() != 1 {
		t.Errorf("convert hits = %d, want 1", hits.Load())
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

	res, err := dc.Convert(context.Background(), make([]byte, refuseTierMinBytes+1), "huge.pdf")
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

	if _, err := dc.Convert(context.Background(), []byte("small"), "doc.pdf"); err == nil {
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
