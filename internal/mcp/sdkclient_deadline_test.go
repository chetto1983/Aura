package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestOpenSDKSessionHTTPHonoursCallerDeadline pins D-06/MCPH-04 on the SDK path: a
// hung MCP server costs the caller its own timeout and no more.
//
// The SDK does not provide this. Measured 2026-08-16 against go-sdk v1.7.0: a 1s
// deadline against an unresponsive endpoint returned after 7.0s, having issued the
// three requests of the dual-era fallback ladder and let each run to completion.
// The assertion below is therefore on ELAPSED TIME, not merely on the returned
// error — the error was always right, it just arrived seven times too late.
func TestOpenSDKSessionHTTPHonoursCallerDeadline(t *testing.T) {
	var hits atomic.Int64
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))

	const deadline = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	start := time.Now()
	session, err := OpenSDKSession(ctx, "hung", ManagedServer{
		Type: ServerTypeStreamableHTTP,
		URL:  srv.URL,
	}, EgressPolicy{}, SessionOptions{})
	elapsed := time.Since(start)

	// Release the handlers before any Fatal, so a failing run still tears down.
	close(release)
	WaitForAbandonedCalls()
	srv.Close()

	if session != nil {
		_ = session.Close()
		t.Fatalf("OpenSDKSession(hung) returned a session, want none")
	}
	if err == nil {
		t.Fatal("OpenSDKSession(hung): err = nil, want a transport error")
	}
	if !IsTransportError(err) {
		t.Errorf("OpenSDKSession(hung): err = %v, want it to classify as a transport error so MountWithRetry still gates on it", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("OpenSDKSession(hung): err = %v, want it to wrap context.DeadlineExceeded", err)
	}
	// Generous ceiling: the point is that the bound EXISTS, not that it is tight.
	// Before the fix this took 7s against a 1s deadline.
	if ceiling := 10 * deadline; elapsed > ceiling {
		t.Fatalf("OpenSDKSession(hung) took %v with a %v deadline, want it bounded under %v (D-06/MCPH-04) — the SDK observes the deadline only BETWEEN requests of its dual-era fallback ladder, so the host must bound the connect itself", elapsed, deadline, ceiling)
	}
	if got := hits.Load(); got == 0 {
		t.Errorf("hung server saw %d requests, want at least 1 — the test proved nothing if the endpoint was never dialed", got)
	}
}

// TestOpenSDKSessionHTTPSucceedsWithinDeadline guards the other side of the bound:
// the goroutine hand-off must not break the ordinary path, and must return the
// session rather than dropping it.
func TestOpenSDKSessionHTTPSucceedsWithinDeadline(t *testing.T) {
	srv, _ := startSDKHTTPServer(t, 1, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := OpenSDKSession(ctx, "live", ManagedServer{
		Type: ServerTypeStreamableHTTP,
		URL:  srv.URL,
	}, EgressPolicy{}, SessionOptions{})
	if err != nil {
		t.Fatalf("OpenSDKSession(live): %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	if session.ID() == "" && session.InitializeResult() == nil {
		t.Error("OpenSDKSession(live) returned a session with neither an id nor an initialize result")
	}
}
