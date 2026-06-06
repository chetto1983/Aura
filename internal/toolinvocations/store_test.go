package toolinvocations

import (
	"encoding/json"
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
