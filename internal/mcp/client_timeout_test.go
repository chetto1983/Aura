package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStdioCallToolContextTimeoutUnblocksClose(t *testing.T) {
	cliStdinR, cliStdinW := io.Pipe()
	srvStdoutR, srvStdoutW := io.Pipe()
	c := newClientForTest("hung", cliStdinW, srvStdoutR)

	serverDone := make(chan struct{})
	releaseServer := make(chan struct{})
	go func() {
		defer close(serverDone)
		dec := json.NewDecoder(cliStdinR)
		for {
			var req map[string]any
			if err := dec.Decode(&req); err != nil {
				return
			}
			if method, _ := req["method"].(string); method == "tools/call" {
				<-releaseServer
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := c.CallTool(ctx, "hang", nil)
	if err == nil {
		t.Fatal("CallTool returned nil error for a hung server")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CallTool error = %v, want context deadline", err)
	}
	if !IsTransportError(err) {
		t.Fatalf("CallTool error must poison the transport, got %v", err)
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- c.Close() }()
	select {
	case <-closeDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close blocked after a timed-out MCP read")
	}

	close(releaseServer)
	_ = cliStdinR.Close()
	_ = srvStdoutW.Close()
	select {
	case <-serverDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hung fake server did not exit after client close")
	}
}

func TestStdioConfiguredCallTimeoutBoundsBackgroundCaller(t *testing.T) {
	cliStdinR, cliStdinW := io.Pipe()
	srvStdoutR, srvStdoutW := io.Pipe()
	c := newClientForTest("configured-timeout", cliStdinW, srvStdoutR)
	c.callTimeout = 25 * time.Millisecond

	serverDone := make(chan struct{})
	releaseServer := make(chan struct{})
	go func() {
		defer close(serverDone)
		dec := json.NewDecoder(cliStdinR)
		var req map[string]any
		if dec.Decode(&req) == nil {
			<-releaseServer
		}
	}()

	start := time.Now()
	_, err := c.CallTool(context.Background(), "hang", nil)
	if !errors.Is(err, context.DeadlineExceeded) || !IsTransportError(err) {
		t.Fatalf("configured timeout error = %v, want transport deadline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("configured call timeout returned after %v", elapsed)
	}

	close(releaseServer)
	_ = c.Close()
	_ = cliStdinR.Close()
	_ = srvStdoutW.Close()
	<-serverDone
}

func TestStdioCallToolTimeoutDoesNotAffectIndependentClient(t *testing.T) {
	cliStdinR, cliStdinW := io.Pipe()
	srvStdoutR, srvStdoutW := io.Pipe()
	hung := newClientForTest("hung", cliStdinW, srvStdoutR)
	releaseHung := make(chan struct{})
	hungDone := make(chan struct{})
	go func() {
		defer close(hungDone)
		dec := json.NewDecoder(cliStdinR)
		for {
			var req map[string]any
			if err := dec.Decode(&req); err != nil {
				return
			}
			if method, _ := req["method"].(string); method == "tools/call" {
				<-releaseHung
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	_, err := hung.CallTool(ctx, "hang", nil)
	cancel()
	if err == nil || !IsTransportError(err) {
		t.Fatalf("hung CallTool should return a transport error, got %v", err)
	}

	healthy, cleanup := newTestPair(t)
	defer cleanup()
	if err := healthy.initialize(); err != nil {
		t.Fatalf("healthy initialize: %v", err)
	}
	out, err := healthy.CallTool(context.Background(), "sandbox_exec", map[string]any{"container_id": "x"})
	if err != nil {
		t.Fatalf("healthy CallTool after independent timeout: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("healthy output = %q, want hello world", out)
	}

	close(releaseHung)
	_ = hung.Close()
	_ = cliStdinR.Close()
	_ = srvStdoutW.Close()
	select {
	case <-hungDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hung fake server did not exit after cleanup")
	}
}

func TestStdioReadResponseContextTimeoutIncludesRecv(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	c := newClientForTest("hung-read", nopWriteCloser{}, stdoutR)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	_, err := c.readResponseContext(ctx, 1)
	if err == nil || !strings.Contains(err.Error(), "recv") {
		t.Fatalf("want recv timeout error, got %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !IsTransportError(err) {
		t.Fatalf("timeout should wrap both context and transport errors, got %v", err)
	}
	_ = stdoutW.Close()
}
