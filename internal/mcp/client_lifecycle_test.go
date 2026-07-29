package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const lifecyclePromptBound = 150 * time.Millisecond

func TestStdioWaiterHonorsContextWhileSessionBusy(t *testing.T) {
	cliStdinR, cliStdinW := io.Pipe()
	srvStdoutR, srvStdoutW := io.Pipe()
	c := newClientForTest("busy", cliStdinW, srvStdoutR)

	started := make(chan struct{})
	release := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		var req rpcReq
		if json.NewDecoder(cliStdinR).Decode(&req) != nil {
			return
		}
		close(started)
		<-release
		enc, _ := json.Marshal(rpcResp{
			JSONRPC: "2.0",
			ID:      &req.ID,
			Result:  json.RawMessage(`{}`),
		})
		_, _ = srvStdoutW.Write(append(enc, '\n'))
	}()

	ownerDone := make(chan error, 1)
	go func() { ownerDone <- c.Ping(context.Background()) }()
	<-started
	time.AfterFunc(250*time.Millisecond, func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Ping(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting Ping error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed >= lifecyclePromptBound {
		t.Fatalf("waiting Ping ignored its context for %v", elapsed)
	}

	if err := <-ownerDone; err != nil {
		t.Fatalf("owner Ping: %v", err)
	}
	_ = c.Close()
	_ = cliStdinR.Close()
	_ = srvStdoutW.Close()
	<-serverDone
}

func TestStdioCloseCancelsActiveCallBeforeWaiting(t *testing.T) {
	cliStdinR, cliStdinW := io.Pipe()
	srvStdoutR, srvStdoutW := io.Pipe()
	c := newClientForTest("closing", cliStdinW, srvStdoutR)
	c.stdoutCloser = srvStdoutR

	started := make(chan struct{})
	release := make(chan struct{})
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		var req rpcReq
		if json.NewDecoder(cliStdinR).Decode(&req) != nil {
			return
		}
		close(started)
		<-release
	}()

	callDone := make(chan error, 1)
	go func() { callDone <- c.Ping(context.Background()) }()
	<-started
	time.AfterFunc(300*time.Millisecond, func() { close(release) })

	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= lifecyclePromptBound {
		t.Fatalf("Close waited behind the active call for %v", elapsed)
	}
	if err := <-callDone; !IsTransportError(err) {
		t.Fatalf("active Ping error = %v, want transport cancellation", err)
	}
	<-serverDone
	_ = srvStdoutW.Close()
	if err := c.Ping(context.Background()); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("Ping after Close = %v, want ErrClientClosed", err)
	}
}

func TestHTTPWaiterHonorsContextWhileSessionBusy(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		close(started)
		<-release
		writeHTTPRPC(t, w, req.ID, map[string]any{})
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})

	ownerDone := make(chan error, 1)
	go func() { ownerDone <- c.Ping(context.Background()) }()
	<-started
	time.AfterFunc(250*time.Millisecond, func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Ping(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting Ping error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed >= lifecyclePromptBound {
		t.Fatalf("waiting Ping ignored its context for %v", elapsed)
	}
	if err := <-ownerDone; err != nil {
		t.Fatalf("owner Ping: %v", err)
	}
	_ = c.Close()
}

func TestHTTPCloseCancelsActiveCallBeforeWaiting(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(httpInitHandler(t, "closing", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
			writeHTTPRPC(t, w, 3, map[string]any{})
		}
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})

	callDone := make(chan error, 1)
	go func() { callDone <- c.Ping(context.Background()) }()
	<-started
	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= lifecyclePromptBound {
		t.Fatalf("Close waited behind the active call for %v", elapsed)
	}
	if err := <-callDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("active Ping error = %v, want cancellation", err)
	}
	if err := c.Ping(context.Background()); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("Ping after Close = %v, want ErrClientClosed", err)
	}
}
