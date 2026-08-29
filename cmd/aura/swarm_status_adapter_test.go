package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/documents"
)

func TestSwarmStatusProjectRowUsesCompleteTailAndTerminalTime(t *testing.T) {
	const conversationID = "conv-1"
	const childID = "w1"
	start := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	events := []agent.Event{
		{Timestamp: start.Add(time.Second), LLMResponse: &agent.LLMResponse{Content: "working"}},
		{Timestamp: start.Add(4 * time.Second), Actions: agent.Actions{StateDelta: map[string]any{"swarm_child_status": "ok"}}},
	}
	runDir := writeSwarmStatusTranscript(t, conversationID, childID, events, `{"timestamp":"partial`)
	adapter := swarmStatusAdapter{runDir: runDir}

	status, err := adapter.projectRow(t.Context(), conversationID, documents.DelegationJobRow{
		ChildID: childID, Goal: "goal", Status: "succeeded", AttemptCount: 1, MaxAttempts: 3,
		CreatedAt: start, CompletedAt: start.Add(4900 * time.Millisecond),
	}, 1)
	if err != nil {
		t.Fatalf("projectRow: %v", err)
	}
	if status.ElapsedSec != 4 {
		t.Fatalf("elapsed_sec = %d, want 4", status.ElapsedSec)
	}
	if len(status.Tail) != 1 || status.Tail[0].Kind != "event" || status.Tail[0].Detail != "worker finished: ok" {
		t.Fatalf("tail = %#v", status.Tail)
	}
	wantLastEventAt := events[1].Timestamp.Format(time.RFC3339Nano)
	if status.LastEventAt != wantLastEventAt {
		t.Fatalf("last_event_at = %q, want %q", status.LastEventAt, wantLastEventAt)
	}
}

func TestSwarmStatusProjectRowRejectsCorruptCompleteEvent(t *testing.T) {
	const conversationID = "conv-1"
	const childID = "w1"
	runDir := t.TempDir()
	dir := filepath.Join(runDir, conversationID, "swarm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, childID+".jsonl"), []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := (swarmStatusAdapter{runDir: runDir}).projectRow(t.Context(), conversationID, documents.DelegationJobRow{
		ChildID: childID, CreatedAt: time.Now(), Status: "running",
	}, 20)
	if err == nil || !strings.Contains(err.Error(), "decode transcript event") {
		t.Fatalf("projectRow error = %v, want transcript decode error", err)
	}
}

func TestSwarmStatusEventKindDetail(t *testing.T) {
	cases := []struct {
		name       string
		event      agent.Event
		wantKind   string
		wantDetail string
	}{
		{name: "pause", event: agent.Event{Actions: agent.Actions{AwaitingInput: &agent.AwaitingInput{Question: "choose"}}}, wantKind: "pause", wantDetail: "choose"},
		{name: "tool call", event: agent.Event{Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{Event: agent.ToolInvocationStart, ToolName: "read_file"}}}, wantKind: "tool_call", wantDetail: "read_file"},
		{name: "tool error", event: agent.Event{Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{Event: agent.ToolInvocationEnd, ToolName: "shell_exec", Status: "error", Error: "denied"}}}, wantKind: "error", wantDetail: "denied"},
		{name: "tool result", event: agent.Event{Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{Event: agent.ToolInvocationEnd, ToolName: "read_file", ResultPreview: "contents"}}}, wantKind: "tool_result", wantDetail: "contents"},
		{name: "assistant", event: agent.Event{LLMResponse: &agent.LLMResponse{Content: "done"}}, wantKind: "assistant", wantDetail: "done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, detail := swarmStatusEventKindDetail(tc.event)
			if kind != tc.wantKind || detail != tc.wantDetail {
				t.Fatalf("kind/detail = %q/%q, want %q/%q", kind, detail, tc.wantKind, tc.wantDetail)
			}
		})
	}
}

func writeSwarmStatusTranscript(t *testing.T, conversationID, childID string, events []agent.Event, partial string) string {
	t.Helper()
	runDir := t.TempDir()
	dir := filepath.Join(runDir, conversationID, "swarm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	var body bytes.Buffer
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("Marshal event: %v", err)
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	body.WriteString(partial)
	if err := os.WriteFile(filepath.Join(dir, childID+".jsonl"), body.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return runDir
}
