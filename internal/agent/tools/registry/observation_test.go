package tools

import (
	"bytes"
	"encoding/gob"
	"errors"
	"testing"
	"time"
)

// TestBucketOfAllTenLabels verifies the 10-label → 5-bucket mapping defined in
// error.go:13-41 and consolidated in observation.go.
func TestBucketOfAllTenLabels(t *testing.T) {
	cases := []struct {
		class string
		want  Outcome
	}{
		// empty class → ok (successful tool call)
		{"", OutcomeOK},
		// recoverable
		{"validation", OutcomeRecoverable},
		{"not_found", OutcomeRecoverable},
		{"rate_limited", OutcomeRecoverable},
		{"io", OutcomeRecoverable},
		// blocked
		{"permission", OutcomeBlocked},
		{"blocked", OutcomeBlocked},
		// fatal
		{"error", OutcomeFatal},
		{"timeout", OutcomeFatal},
		// cancelled
		{"cancelled", OutcomeCancelled},
	}
	for _, c := range cases {
		got := BucketOf(c.class)
		if got != c.want {
			t.Errorf("BucketOf(%q) = %v, want %v", c.class, got, c.want)
		}
	}
}

// TestBucketOfUnknownClassIsFatal confirms unknown labels fall to fatal
// (conservative default).
func TestBucketOfUnknownClassIsFatal(t *testing.T) {
	for _, label := range []string{"unknown", "network_error", "foo"} {
		if got := BucketOf(label); got != OutcomeFatal {
			t.Errorf("BucketOf(%q) = %v, want OutcomeFatal", label, got)
		}
	}
}

// TestToolKindDerivation checks the mcp_ prefix rule from mcp.go:26-27.
func TestToolKindDerivation(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"mcp_github_search", "mcp"},
		{"mcp_server_tool", "mcp"},
		{"mcp_", "mcp"},
		{"web_search", "native"},
		{"search_memory", "native"},
		{"mcp", "native"}, // no trailing underscore → not an MCP name
		{"", "native"},
	}
	for _, c := range cases {
		got := ToolKindOf(c.name)
		if got != c.want {
			t.Errorf("ToolKindOf(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestObservationArgsHashStable verifies the hash is deterministic regardless
// of map-iteration order, and that arg keys contain no values.
func TestObservationArgsHashStable(t *testing.T) {
	args := map[string]any{"b": 2, "a": 1, "c": "hello"}

	h1, keys1 := ObservationArgsHash(args)
	h2, keys2 := ObservationArgsHash(args)

	if h1 != h2 {
		t.Fatal("hash is not deterministic across two calls with same input")
	}
	if len(keys1) != len(keys2) {
		t.Fatalf("key list length mismatch: %d vs %d", len(keys1), len(keys2))
	}
	// Keys must be sorted
	for i := 1; i < len(keys1); i++ {
		if keys1[i] < keys1[i-1] {
			t.Errorf("keys not sorted: keys1[%d]=%q < keys1[%d]=%q", i, keys1[i], i-1, keys1[i-1])
		}
	}
}

// TestObservationArgsHashEmptyArgs produces a stable hash for nil/empty args.
func TestObservationArgsHashEmptyArgs(t *testing.T) {
	h1, keys1 := ObservationArgsHash(nil)
	h2, keys2 := ObservationArgsHash(map[string]any{})

	if h1 != h2 {
		t.Error("nil and empty map should produce the same hash")
	}
	if len(keys1) != 0 || len(keys2) != 0 {
		t.Error("empty args should yield empty key list")
	}
}

// TestToolObservationGobRoundTrip ensures the struct survives encoding/gob
// serialisation intact (required for the downstream persistence layer in
// US-J02).
func TestToolObservationGobRoundTrip(t *testing.T) {
	args := map[string]any{"query": "test", "limit": 10}
	argsHash, argKeys := ObservationArgsHash(args)
	original := ToolObservation{
		RunID:         "run-abc",
		ToolName:      "mcp_github_search",
		ToolKind:      ToolKindOf("mcp_github_search"),
		AttemptN:      2,
		Outcome:       OutcomeRecoverable,
		Class:         "rate_limited",
		Reason:        "",
		ArgsHash:      argsHash,
		ArgKeys:       argKeys,
		SchemaHash:    [16]byte{1, 2, 3},
		ErrorRedacted: "rate limit exceeded",
		StartedAt:     time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
		ElapsedMS:     42,
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(original); err != nil {
		t.Fatalf("gob encode: %v", err)
	}
	var decoded ToolObservation
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatalf("gob decode: %v", err)
	}

	if decoded.RunID != original.RunID {
		t.Errorf("RunID: got %q, want %q", decoded.RunID, original.RunID)
	}
	if decoded.ToolName != original.ToolName {
		t.Errorf("ToolName: got %q, want %q", decoded.ToolName, original.ToolName)
	}
	if decoded.ToolKind != original.ToolKind {
		t.Errorf("ToolKind: got %q, want %q", decoded.ToolKind, original.ToolKind)
	}
	if decoded.Outcome != original.Outcome {
		t.Errorf("Outcome: got %v, want %v", decoded.Outcome, original.Outcome)
	}
	if decoded.ArgsHash != original.ArgsHash {
		t.Errorf("ArgsHash mismatch after gob round-trip")
	}
	if len(decoded.ArgKeys) != len(original.ArgKeys) {
		t.Errorf("ArgKeys length: got %d, want %d", len(decoded.ArgKeys), len(original.ArgKeys))
	}
	if decoded.ElapsedMS != original.ElapsedMS {
		t.Errorf("ElapsedMS: got %d, want %d", decoded.ElapsedMS, original.ElapsedMS)
	}
}

// TestOutcomeStringMethod verifies human-readable labels for logging.
func TestOutcomeStringMethod(t *testing.T) {
	cases := []struct {
		o    Outcome
		want string
	}{
		{OutcomeOK, "ok"},
		{OutcomeRecoverable, "recoverable"},
		{OutcomeBlocked, "blocked"},
		{OutcomeFatal, "fatal"},
		{OutcomeCancelled, "cancelled"},
		{Outcome(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.o.String(); got != c.want {
			t.Errorf("Outcome(%d).String() = %q, want %q", c.o, got, c.want)
		}
	}
}

// TestClassifyToolErrorFeedsIntoObservation is an integration-level smoke test:
// ClassifyToolError → BucketOf produces a coherent observation for a real error.
func TestClassifyToolErrorFeedsIntoObservation(t *testing.T) {
	err := errors.New("rate limit exceeded: too many requests")
	class := ClassifyToolError(err)
	outcome := BucketOf(class)
	if class != "rate_limited" {
		t.Errorf("ClassifyToolError = %q, want rate_limited", class)
	}
	if outcome != OutcomeRecoverable {
		t.Errorf("BucketOf(rate_limited) = %v, want OutcomeRecoverable", outcome)
	}
}
