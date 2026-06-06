package toolinvocations

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestEventToParams_StartShape(t *testing.T) {
	started := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	p, err := (Event{
		ConversationID: "00000000-0000-0000-0000-000000000001",
		RequestID:      "00000000-0000-0000-0000-000000000002",
		ToolCallID:     "call-1",
		ToolName:       "shell_exec",
		Event:          EventStart,
		Seq:            1,
		StartedAt:      started,
		Arguments:      `{"command":"echo hi"}`,
		ArgsBytes:      len(`{"command":"echo hi"}`),
	}).toParams()
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if !p.StartedAt.Valid || !p.StartedAt.Time.Equal(started) {
		t.Fatalf("StartedAt = %+v, want %s", p.StartedAt, started)
	}
	if p.EndedAt.Valid || p.Status.Valid || p.DurationMs.Valid {
		t.Fatalf("start params must not carry end-only fields: ended=%+v status=%+v duration=%+v", p.EndedAt, p.Status, p.DurationMs)
	}
	if !p.ArgsRaw.Valid || p.ArgsRaw.String != `{"command":"echo hi"}` {
		t.Fatalf("ArgsRaw = %+v", p.ArgsRaw)
	}
}

// TestEventToParams_RedactsArgsSecret is the WR-02 store-boundary regression: a
// Bearer token placed in the tool Arguments must land [REDACTED] in the
// InsertToolInvocationParams.ArgsRaw column, never verbatim, while ArgsBytes keeps
// the pre-redaction forensic byte count of the original argument. result_preview is
// likewise redacted.
func TestEventToParams_RedactsArgsSecret(t *testing.T) {
	const secret = "sk-or-v1deadbeefcafebabe0123"
	args := `{"command":"curl -H \"Authorization: Bearer ` + secret + `\" https://x"}`
	preview := `error: bad token sk-leakedpreviewkey1234567`
	p, err := (Event{
		ConversationID: "00000000-0000-0000-0000-000000000001",
		RequestID:      "00000000-0000-0000-0000-000000000002",
		ToolCallID:     "call-1",
		ToolName:       "shell_exec",
		Event:          EventEnd,
		Seq:            2,
		EndedAt:        time.Date(2026, 6, 6, 10, 0, 1, 0, time.UTC),
		Arguments:      args,
		ArgsBytes:      len(args),
		ResultPreview:  preview,
		PreviewBytes:   len(preview),
		ResultBytes:    len(preview),
	}).toParams()
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if !p.ArgsRaw.Valid {
		t.Fatal("ArgsRaw must be valid")
	}
	if strings.Contains(p.ArgsRaw.String, secret) {
		t.Errorf("secret persisted verbatim in ArgsRaw: %q", p.ArgsRaw.String)
	}
	if !strings.Contains(p.ArgsRaw.String, redactedPlaceholder) {
		t.Errorf("ArgsRaw missing %q: %q", redactedPlaceholder, p.ArgsRaw.String)
	}
	// ArgsBytes records the ORIGINAL argument size, not the redacted footprint.
	if p.ArgsBytes.Int32 != int32(len(args)) {
		t.Errorf("ArgsBytes = %d, want pre-redaction byte count %d", p.ArgsBytes.Int32, len(args))
	}
	if strings.Contains(p.ResultPreview.String, "sk-leakedpreviewkey1234567") {
		t.Errorf("secret persisted verbatim in ResultPreview: %q", p.ResultPreview.String)
	}
}

func TestEventToParams_EndShapeWithMeta(t *testing.T) {
	exitCode := 7
	ended := time.Date(2026, 6, 6, 10, 0, 1, 0, time.UTC)
	p, err := (Event{
		ConversationID: "00000000-0000-0000-0000-000000000001",
		RequestID:      "00000000-0000-0000-0000-000000000002",
		ToolCallID:     "call-1",
		ToolName:       "shell_exec",
		Event:          EventEnd,
		Seq:            2,
		EndedAt:        ended,
		DurationMS:     25,
		Status:         "ok",
		ResultPreview:  "[exit code 7]",
		PreviewBytes:   len("[exit code 7]"),
		ResultBytes:    len("[exit code 7]"),
		ExitCode:       &exitCode,
		Meta:           map[string]any{"exit_code": 7, "timed_out": false},
	}).toParams()
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if !p.EndedAt.Valid || !p.DurationMs.Valid || p.DurationMs.Int64 != 25 || !p.Status.Valid {
		t.Fatalf("end params missing end fields: ended=%+v duration=%+v status=%+v", p.EndedAt, p.DurationMs, p.Status)
	}
	if !p.ExitCode.Valid || p.ExitCode.Int32 != 7 {
		t.Fatalf("ExitCode = %+v, want 7", p.ExitCode)
	}
	var meta map[string]any
	if err := json.Unmarshal(p.Meta, &meta); err != nil {
		t.Fatalf("meta json: %v", err)
	}
	if meta["exit_code"].(float64) != 7 || meta["timed_out"].(bool) {
		t.Fatalf("meta = %#v", meta)
	}
}

func TestEventToParams_BadUUID(t *testing.T) {
	_, err := (Event{ConversationID: "not-a-uuid", RequestID: "00000000-0000-0000-0000-000000000002"}).toParams()
	if err == nil {
		t.Fatal("toParams with bad conversation_id: want error")
	}
}

// TestEventToParams_StartDefaultsStartedAt is the IN-01 regression: a start event
// with a zero StartedAt defaults the column to the resolved ts so the event-shape
// CHECK (started_at NOT NULL for a start row) cannot fail on a forgetful emitter.
func TestEventToParams_StartDefaultsStartedAt(t *testing.T) {
	ts := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	p, err := (Event{
		ConversationID: "00000000-0000-0000-0000-000000000001",
		RequestID:      "00000000-0000-0000-0000-000000000002",
		ToolCallID:     "call-1",
		ToolName:       "shell_exec",
		Event:          EventStart,
		Seq:            1,
		Timestamp:      ts, // StartedAt deliberately left zero
	}).toParams()
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if !p.StartedAt.Valid || !p.StartedAt.Time.Equal(ts) {
		t.Fatalf("StartedAt = %+v, want it defaulted to the resolved ts %s", p.StartedAt, ts)
	}
}

// TestClampInt32 is the WR-03 regression: int byte-count/seq fields saturate at the
// int32 bounds instead of wrapping into a wrong/negative forensic count.
func TestClampInt32(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want int32
	}{
		{0, 0},
		{42, 42},
		{math.MaxInt32, math.MaxInt32},
		{math.MaxInt32 + 1, math.MaxInt32},
		{math.MaxInt64, math.MaxInt32},
		{-1, -1},
		{math.MinInt32, math.MinInt32},
		{math.MinInt32 - 1, math.MinInt32},
	} {
		if got := clampInt32(tc.in); got != tc.want {
			t.Errorf("clampInt32(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
