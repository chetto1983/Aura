package arcadedb

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func captureWarn(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return &buf, func() { slog.SetDefault(previous) }
}

// A recall that loses half its evidence to a failing engine must not look like a memory
// that simply holds less.
func TestRecallDropsReportsWhatItLost(t *testing.T) {
	buf, restore := captureWarn(t)
	defer restore()

	var drops recallDrops
	drops.record(errors.New("first window"))
	drops.record(errors.New("second window"))
	drops.report("semantic")

	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &record); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, buf.String())
	}
	if record["mode"] != "semantic" {
		t.Fatalf("mode = %v, want the path named", record["mode"])
	}
	if record["dropped"] != float64(2) {
		t.Fatalf("dropped = %v, want 2", record["dropped"])
	}
	// The LAST cause, not the first: an operator reading one line wants the state the
	// pass ended in.
	if got, _ := record["err"].(string); !strings.Contains(got, "second window") {
		t.Fatalf("err = %q, want the last cause", got)
	}
}

// A clean pass says nothing. This is what keeps the line worth reading.
func TestRecallDropsStaysQuietWhenNothingWasLost(t *testing.T) {
	buf, restore := captureWarn(t)
	defer restore()

	var drops recallDrops
	drops.report("recent")

	if buf.Len() != 0 {
		t.Fatalf("log = %q, want silence when no conversation was skipped", buf.String())
	}
}
