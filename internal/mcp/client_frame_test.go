package mcp

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// frameTestMaxContent mirrors the D-08 default 1 MiB stdio-frame cap
// (AURA_MCP_STDIO_MAX_FRAME's documented default). It is a test-local literal
// rather than a reference to the production constant so this boundary contract
// is pinned independently of whatever internal name/shape client.go picks.
const frameTestMaxContent = 1 << 20

// buildFrameLine returns a syntactically valid JSON-RPC response line of EXACTLY
// contentLen bytes (excluding the trailing '\n'), padding a string field so
// boundary tests can assert both "accepted, id matches" and the precise byte
// count in one shot.
func buildFrameLine(t *testing.T, id int64, contentLen int) []byte {
	t.Helper()
	head := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"pad":"`, id)
	const tail = `"}}`
	padLen := contentLen - len(head) - len(tail)
	if padLen < 0 {
		t.Fatalf("buildFrameLine: contentLen %d too small for envelope (need >= %d)", contentLen, len(head)+len(tail))
	}
	line := make([]byte, 0, contentLen+1)
	line = append(line, head...)
	line = append(line, strings.Repeat("a", padLen)...)
	line = append(line, tail...)
	if len(line) != contentLen {
		t.Fatalf("buildFrameLine: built %d bytes, want %d", len(line), contentLen)
	}
	return append(line, '\n')
}

// TestReadResponseBlockingAcceptsFrameJustUnderCap is a regression guard: a frame
// one byte under the cap must always be accepted (D-08's "1048575 accepted" leg).
func TestReadResponseBlockingAcceptsFrameJustUnderCap(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	c := newClientForTest("frame", nopWriteCloser{}, stdoutR)
	defer func() { _ = stdoutW.Close() }()

	go func() { _, _ = stdoutW.Write(buildFrameLine(t, 1, frameTestMaxContent-1)) }()

	if _, err := c.readResponseBlocking(1); err != nil {
		t.Fatalf("readResponseBlocking at cap-1: %v", err)
	}
}

// TestReadResponseBlockingAcceptsFrameAtCap pins the exact-cap contract: a frame
// of exactly the configured max (1048576 bytes) must be accepted, not rejected
// (the off-by-one this D-08 fix must get right).
func TestReadResponseBlockingAcceptsFrameAtCap(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	c := newClientForTest("frame", nopWriteCloser{}, stdoutR)
	defer func() { _ = stdoutW.Close() }()

	go func() { _, _ = stdoutW.Write(buildFrameLine(t, 1, frameTestMaxContent)) }()

	if _, err := c.readResponseBlocking(1); err != nil {
		t.Fatalf("readResponseBlocking at exact cap: %v", err)
	}
}

// TestReadResponseBlockingAbortsOversizedFrame is the core D-08/D-09 behavior: one
// byte over cap must abort the transport with an IsTransportError, never a decode
// error from a truncated/garbage parse.
func TestReadResponseBlockingAbortsOversizedFrame(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	c := newClientForTest("frame", nopWriteCloser{}, stdoutR)
	defer func() { _ = stdoutW.Close() }()

	go func() { _, _ = stdoutW.Write(buildFrameLine(t, 1, frameTestMaxContent+1)) }()

	_, err := c.readResponseBlocking(1)
	if err == nil {
		t.Fatal("readResponseBlocking on an over-cap frame returned nil error, want IsTransportError")
	}
	if !IsTransportError(err) {
		t.Fatalf("readResponseBlocking on an over-cap frame = %v, want IsTransportError", err)
	}
}

// TestReadResponseBlockingNoResyncAfterOversizedFrame is D-09's "never resync"
// prohibition: even with a well-formed frame queued right behind the oversized
// one, a subsequent read on the same client must still error, not silently pick
// up the next frame as if nothing happened.
func TestReadResponseBlockingNoResyncAfterOversizedFrame(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	c := newClientForTest("frame", nopWriteCloser{}, stdoutR)
	defer func() { _ = stdoutW.Close() }()

	go func() {
		_, _ = stdoutW.Write(buildFrameLine(t, 1, frameTestMaxContent+1))
		_, _ = stdoutW.Write(buildFrameLine(t, 2, 60))
	}()

	if _, err := c.readResponseBlocking(1); err == nil || !IsTransportError(err) {
		t.Fatalf("first (over-cap) read = %v, want IsTransportError", err)
	}
	if _, err := c.readResponseBlocking(2); err == nil {
		t.Fatal("read after an over-cap frame succeeded — transport was resynced instead of aborted")
	}
}

// TestReadResponseBlockingClosedStdoutIsTransportError covers Pitfall #4: a
// scanner-based reader's clean EOF (Scan()==false, Err()==nil) must never be
// mistaken for "no more responses" — a closed peer always means the transport is
// gone, wrapping io.ErrUnexpectedEOF, never a silent (nil, nil).
func TestReadResponseBlockingClosedStdoutIsTransportError(t *testing.T) {
	stdoutR, stdoutW := io.Pipe()
	c := newClientForTest("closed", nopWriteCloser{}, stdoutR)
	_ = stdoutW.Close() // peer gone before any data is ever sent

	res, err := c.readResponseBlocking(1)
	if err == nil {
		t.Fatalf("readResponseBlocking on a closed, empty stdout = (%v, nil), want an error", res)
	}
	if !IsTransportError(err) {
		t.Fatalf("readResponseBlocking on a closed, empty stdout = %v, want IsTransportError", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("readResponseBlocking on a closed, empty stdout = %v, want io.ErrUnexpectedEOF in chain", err)
	}
}
