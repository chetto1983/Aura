package swarm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

// transcript_api_test.go pins SWARM-10: a mid-run read returns partial content
// without blocking, hostile childID values are rejected before any filesystem call,
// offsets are monotonic across sequential reads, and an empty/missing directory is
// not an error. Every test writes through dumpTranscript itself (report.go) so the
// read path is proven against the REAL writer, per 51-PATTERNS.md's Wave-0 guidance
// — never a hand-rolled JSONL fixture.

func newEvent(author string) agent.Event {
	return agent.Event{
		RequestID: uuid.Must(uuid.NewV7()),
		Author:    author,
		Timestamp: time.Now(),
	}
}

// TestListChildTranscriptsEmptyDirectory asserts ListChildTranscripts never errors
// for a conversation with no swarm directory yet (D-behavior: "empty slice when
// none, never an error for a missing directory").
func TestListChildTranscriptsEmptyDirectory(t *testing.T) {
	tmp := t.TempDir()
	ids, err := ListChildTranscripts(context.Background(), tmp, "conv-empty")
	if err != nil {
		t.Fatalf("ListChildTranscripts on a missing directory returned an error: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("want empty slice, got %v", ids)
	}
}

// TestListChildTranscriptsDiscoversWrittenChildren proves the discovery half: once
// dumpTranscript has written for w1 and w2, both ids are reported (order-independent).
func TestListChildTranscriptsDiscoversWrittenChildren(t *testing.T) {
	tmp := t.TempDir()
	if err := dumpTranscript(tmp, "conv1", "w1", newEvent("w1")); err != nil {
		t.Fatalf("dumpTranscript w1: %v", err)
	}
	if err := dumpTranscript(tmp, "conv1", "w2", newEvent("w2")); err != nil {
		t.Fatalf("dumpTranscript w2: %v", err)
	}
	ids, err := ListChildTranscripts(context.Background(), tmp, "conv1")
	if err != nil {
		t.Fatalf("ListChildTranscripts: %v", err)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["w1"] || !got["w2"] || len(ids) != 2 {
		t.Fatalf("want exactly [w1 w2], got %v", ids)
	}
}

// TestReadTranscriptMidRunReturnsPartialWithoutBlocking asserts a read while the
// worker is still (hypothetically) writing returns exactly the complete lines
// written so far — no wait for EOF, no block — and a subsequent partial (no
// trailing newline) write is NOT surfaced until it is completed by a newline.
func TestReadTranscriptMidRunReturnsPartialWithoutBlocking(t *testing.T) {
	tmp := t.TempDir()
	if err := dumpTranscript(tmp, "conv1", "w1", newEvent("w1")); err != nil {
		t.Fatalf("dumpTranscript: %v", err)
	}

	body, offset, err := ReadTranscript(context.Background(), tmp, "conv1", "w1", 0)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 complete line, got %d: %q", len(lines), body)
	}
	if offset != int64(len(body)) {
		t.Fatalf("offset = %d, want len(body) = %d", offset, len(body))
	}

	// Simulate an in-progress partial line (no trailing '\n' yet): appended raw,
	// bypassing dumpTranscript's one-line-per-write shape, to model exactly the
	// moment a writer is mid-flush.
	path := filepath.Join(tmp, "conv1", "swarm", "w1.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open for partial append: %v", err)
	}
	if _, err := f.WriteString(`{"partial":true`); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	body2, offset2, err := ReadTranscript(context.Background(), tmp, "conv1", "w1", offset)
	if err != nil {
		t.Fatalf("ReadTranscript (partial window): %v", err)
	}
	if len(body2) != 0 {
		t.Fatalf("an incomplete trailing line must not be surfaced, got %q", body2)
	}
	if offset2 != offset {
		t.Fatalf("offset must not advance past an incomplete line: got %d, want %d", offset2, offset)
	}
}

// TestReadTranscriptOffsetMonotonicity proves two sequential reads never duplicate
// or skip a line: read after the first event, then again after a second event is
// appended, and assert the union is exactly the two lines with no overlap.
func TestReadTranscriptOffsetMonotonicity(t *testing.T) {
	tmp := t.TempDir()
	if err := dumpTranscript(tmp, "conv1", "w1", newEvent("w1")); err != nil {
		t.Fatalf("dumpTranscript ev1: %v", err)
	}
	body1, offset1, err := ReadTranscript(context.Background(), tmp, "conv1", "w1", 0)
	if err != nil {
		t.Fatalf("ReadTranscript 1: %v", err)
	}

	if err := dumpTranscript(tmp, "conv1", "w1", newEvent("w1")); err != nil {
		t.Fatalf("dumpTranscript ev2: %v", err)
	}
	body2, offset2, err := ReadTranscript(context.Background(), tmp, "conv1", "w1", offset1)
	if err != nil {
		t.Fatalf("ReadTranscript 2: %v", err)
	}

	if strings.TrimSpace(string(body2)) == "" {
		t.Fatalf("second read returned nothing new after a second event was appended")
	}
	// No duplication: body2 must not repeat body1's content.
	if strings.Contains(string(body2), strings.TrimRight(string(body1), "\n")) {
		t.Fatalf("second read duplicated the first read's line: body1=%q body2=%q", body1, body2)
	}
	if offset2 <= offset1 {
		t.Fatalf("offset did not advance: offset1=%d offset2=%d", offset1, offset2)
	}

	// A third read at offset2 with nothing new appended returns empty, same offset.
	body3, offset3, err := ReadTranscript(context.Background(), tmp, "conv1", "w1", offset2)
	if err != nil {
		t.Fatalf("ReadTranscript 3: %v", err)
	}
	if len(body3) != 0 || offset3 != offset2 {
		t.Fatalf("read with nothing new must return empty at the same offset, got body=%q offset=%d", body3, offset3)
	}
}

// TestReadTranscriptRejectsHostileChildID is the V5 threat-model test table
// (T-51-28): a childID carrying a traversal segment, a path separator (either
// direction), or built from one must be rejected BEFORE any filesystem call — the
// runDir the hostile call targets stays completely empty (zero files created, zero
// directories created beyond what the setup itself made).
func TestReadTranscriptRejectsHostileChildID(t *testing.T) {
	hostile := []string{"..", "../..", "a/b", `a\b`}
	for _, childID := range hostile {
		t.Run(childID, func(t *testing.T) {
			tmp := t.TempDir()
			_, _, err := ReadTranscript(context.Background(), tmp, "conv1", childID, 0)
			if err == nil {
				t.Fatalf("ReadTranscript(childID=%q) did not error", childID)
			}
			entries, rerr := os.ReadDir(tmp)
			if rerr != nil {
				t.Fatalf("ReadDir(tmp): %v", rerr)
			}
			if len(entries) != 0 {
				t.Fatalf("hostile childID %q caused filesystem access: %v", childID, entries)
			}
		})
	}
}

// TestReadTranscriptRejectsHostileConv is the conv-side twin of the childID table —
// the SAME hostile values must be rejected when they arrive as the conversation id.
func TestReadTranscriptRejectsHostileConv(t *testing.T) {
	hostile := []string{"..", "../..", "a/b", `a\b`}
	for _, conv := range hostile {
		t.Run(conv, func(t *testing.T) {
			tmp := t.TempDir()
			_, _, err := ReadTranscript(context.Background(), tmp, conv, "w1", 0)
			if err == nil {
				t.Fatalf("ReadTranscript(conv=%q) did not error", conv)
			}
			entries, rerr := os.ReadDir(tmp)
			if rerr != nil {
				t.Fatalf("ReadDir(tmp): %v", rerr)
			}
			if len(entries) != 0 {
				t.Fatalf("hostile conv %q caused filesystem access: %v", conv, entries)
			}
		})
	}
}

// TestListChildTranscriptsRejectsHostileConv mirrors the read-side rejection for the
// discovery function.
func TestListChildTranscriptsRejectsHostileConv(t *testing.T) {
	for _, conv := range []string{"..", "../..", "a/b", `a\b`} {
		t.Run(conv, func(t *testing.T) {
			tmp := t.TempDir()
			if _, err := ListChildTranscripts(context.Background(), tmp, conv); err == nil {
				t.Fatalf("ListChildTranscripts(conv=%q) did not error", conv)
			}
		})
	}
}

// TestReadTranscriptMissingChildIsEmptyNotError models a caller polling before the
// worker's first flush: an absent transcript file degrades to an empty read, not an
// error, mirroring dumpTranscript's own best-effort posture.
func TestReadTranscriptMissingChildIsEmptyNotError(t *testing.T) {
	tmp := t.TempDir()
	body, offset, err := ReadTranscript(context.Background(), tmp, "conv1", "w9", 0)
	if err != nil {
		t.Fatalf("ReadTranscript for a not-yet-written child returned an error: %v", err)
	}
	if len(body) != 0 || offset != 0 {
		t.Fatalf("want empty body at offset 0, got body=%q offset=%d", body, offset)
	}
}
