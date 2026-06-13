package openai_compat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// TestStream_IdleTimeoutAbortsStall proves B-08: when the provider opens the stream,
// sends one delta, then STALLS (no further bytes), the per-chunk idle watchdog aborts
// the read and emits a terminal ErrStreamIdleTimeout — well before the whole-call
// timeout. Without the watchdog the stall is bounded only by the total timeout (the
// 1s ctx here), so the test distinguishes the two by asserting the abort lands inside
// the idle window.
func TestStream_IdleTimeoutAbortsStall(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"start\"},\"finish_reason\":null}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Stall: hold the connection open, sending nothing, until the client aborts
		// (its request ctx fires) or the test releases us.
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	defer srv.Close()
	defer close(released)

	c := New(testConfig(srv.URL))
	c.streamIdleTimeout = 80 * time.Millisecond // short idle window for the test

	// A 1s total-call ctx is the only OTHER way the stall could end; the watchdog
	// must win, proving it is the idle watchdog and not the whole-call timeout.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	ch, err := c.Stream(ctx, llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := drain(ch)
	elapsed := time.Since(start)

	if len(chunks) == 0 {
		t.Fatal("stream emitted no chunks")
	}
	if chunks[0].Text != "start" {
		t.Fatalf("first chunk text=%q, want the pre-stall delta", chunks[0].Text)
	}
	last := chunks[len(chunks)-1]
	if !errors.Is(last.Err, ErrStreamIdleTimeout) {
		t.Fatalf("a stalled stream must terminate with ErrStreamIdleTimeout, got %#v", last)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("idle abort took %v — too slow to be the watchdog (the whole-call timeout fired instead)", elapsed)
	}
}

// TestStream_IdleTimeoutDisabledWhenZero proves the watchdog is opt-in: with
// streamIdleTimeout == 0 a healthy stream completes normally (no spurious idle abort).
func TestStream_IdleTimeoutDisabledWhenZero(t *testing.T) {
	srv := httptest.NewServer(fixtureHandler(t, "text_stop.sse"))
	defer srv.Close()

	c := New(testConfig(srv.URL))
	c.streamIdleTimeout = 0 // disabled

	ch, err := c.Stream(context.Background(), llm.Request{Model: "m"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for _, ck := range drain(ch) {
		if errors.Is(ck.Err, ErrStreamIdleTimeout) {
			t.Fatal("a healthy stream with the watchdog disabled must not emit ErrStreamIdleTimeout")
		}
	}
}
