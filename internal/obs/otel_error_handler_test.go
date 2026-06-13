package obs

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// captureLogger returns a slog.Logger writing JSON to buf, mirroring the daemon's
// production handler shape (so the handler under test logs consistently).
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// linesOf splits the captured JSON-lines buffer into non-empty records.
func linesOf(buf *bytes.Buffer) []string {
	var out []string
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestRateLimitedOTelHandler_FirstErrorLogged(t *testing.T) {
	var buf bytes.Buffer
	now := time.Unix(0, 0)
	h := newRateLimitedOTelHandler(captureLogger(&buf), time.Minute, func() time.Time { return now })

	h.Handle(errors.New("export timeout: connection refused"))

	lines := linesOf(&buf)
	if len(lines) != 1 {
		t.Fatalf("first error: want 1 log line, got %d: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "export timeout: connection refused") {
		t.Fatalf("first log missing error detail: %s", lines[0])
	}
}

func TestRateLimitedOTelHandler_BurstWithinWindowSuppressed(t *testing.T) {
	var buf bytes.Buffer
	now := time.Unix(0, 0)
	h := newRateLimitedOTelHandler(captureLogger(&buf), time.Minute, func() time.Time { return now })

	for i := 0; i < 50; i++ {
		now = now.Add(time.Second) // still inside the 1-minute window
		h.Handle(errors.New("queue full"))
	}

	if got := len(linesOf(&buf)); got != 1 {
		t.Fatalf("burst within window: want 1 log line (rest suppressed), got %d", got)
	}
}

func TestRateLimitedOTelHandler_NextWindowReportsSuppressedCount(t *testing.T) {
	var buf bytes.Buffer
	now := time.Unix(0, 0)
	h := newRateLimitedOTelHandler(captureLogger(&buf), time.Minute, func() time.Time { return now })

	h.Handle(errors.New("first")) // logs (line 1)
	for i := 0; i < 9; i++ {      // 9 suppressed inside the window
		h.Handle(errors.New("burst"))
	}
	now = now.Add(2 * time.Minute) // advance past the window
	h.Handle(errors.New("second")) // logs (line 2), must report the 9 suppressed

	lines := linesOf(&buf)
	if len(lines) != 2 {
		t.Fatalf("want 2 log lines (one per window), got %d: %q", len(lines), buf.String())
	}

	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &rec); err != nil {
		t.Fatalf("second log not JSON: %v: %s", err, lines[1])
	}
	suppressed, ok := rec["suppressed"]
	if !ok {
		t.Fatalf("second log missing suppressed count: %s", lines[1])
	}
	if n, _ := suppressed.(float64); n != 9 {
		t.Fatalf("suppressed count = %v, want 9: %s", suppressed, lines[1])
	}
	if !strings.Contains(lines[1], "second") {
		t.Fatalf("second log missing the triggering error detail: %s", lines[1])
	}
}

func TestRateLimitedOTelHandler_FirstLogOmitsSuppressedWhenZero(t *testing.T) {
	var buf bytes.Buffer
	now := time.Unix(0, 0)
	h := newRateLimitedOTelHandler(captureLogger(&buf), time.Minute, func() time.Time { return now })

	h.Handle(errors.New("solo"))

	var rec map[string]any
	if err := json.Unmarshal([]byte(linesOf(&buf)[0]), &rec); err != nil {
		t.Fatalf("log not JSON: %v", err)
	}
	if _, ok := rec["suppressed"]; ok {
		t.Fatalf("first log should omit suppressed when zero: %s", buf.String())
	}
}

func TestRateLimitedOTelHandler_NilLoggerDefaultsToSlogDefault(t *testing.T) {
	// A nil logger must not panic — it falls back to slog.Default().
	h := newRateLimitedOTelHandler(nil, time.Minute, nil)
	h.Handle(errors.New("must not panic"))
}
