package mcp

import (
	"bytes"
	"io"
	"log/slog"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/redact"
)

// protocolLines passes a stdio server's stdout to the client one line at a time, and only the
// lines that can be JSON-RPC: a line whose first non-blank byte opens an object or an array.
//
// A self-installing server can print to stdout on its first run. Measured 2026-09-26:
// mcp-server-fetch's first `fetch` ran `npm install` for its readability helper, which wrote
// seven lines ("added 39 packages ...") to stdout. The go-sdk's IOTransport ends the session on
// the first line that is not JSON (transport.go, the decode loop), so the first call after a
// cold start failed. The TypeScript SDK LibreChat uses (1.30.1, client/stdio.js
// processReadBuffer) reports such a line and keeps reading; this does the same.
type protocolLines struct {
	dst    io.Writer
	name   string
	logger *slog.Logger
	buf    []byte
}

func (w *protocolLines) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i+1]
		w.buf = w.buf[i+1:]
		if err := w.pass(line); err != nil {
			return len(p), err
		}
	}
	// A line longer than the transport accepts is its to refuse, not this buffer's to grow.
	if len(w.buf) > sdkmcp.DefaultMaxLineLength {
		line := w.buf
		w.buf = nil
		if _, err := w.dst.Write(line); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (w *protocolLines) pass(line []byte) error {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		_, err := w.dst.Write(line)
		return err
	}
	if len(trimmed) > 0 {
		const shown = 200
		if len(trimmed) > shown {
			trimmed = trimmed[:shown]
		}
		w.logger.Warn("mcp stdio server wrote a non-protocol line to stdout; dropped",
			"server", redact.Line(w.name), "line", RedactSecrets(string(trimmed)))
	}
	return nil
}
