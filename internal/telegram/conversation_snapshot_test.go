package telegram

import (
	"testing"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/config"
)

func TestRuntimeSnapshotPreservesToolSignals(t *testing.T) {
	b := &Bot{cfg: &config.Config{TraceRetentionDays: config.DefaultTraceRetentionDays}}
	b.storeOrchestrationSnapshot("1148481707", agent.TurnStats{
		PromptVersion:       "aura-agent-v1",
		PromptHash:          "abc123",
		Toolset:             "registered",
		ToolsetSelectReason: "all registered tools exposed",
		ToolsExposed:        []string{"run_aurabot_swarm"},
		ToolsCalled:         []string{"execute_code"},
		ReadSkills:          []string{"subagent-driven-development"},
		LoopSteps:           2,
		TerminalTool:        "run_aurabot_swarm",
		DuplicateToolCall:   true,
		TokensPrompt:        10,
		TokensCompletion:    5,
		TokensTotal:         15,
	})

	snap, ok := b.loadOrchestrationSnapshot("1148481707")
	if !ok {
		t.Fatal("snapshot missing")
	}
	if snap.ToolsetSelectReason == "" {
		t.Fatal("ToolsetSelectReason is empty")
	}
	if snap.LoopSteps != 2 || snap.TerminalTool != "run_aurabot_swarm" {
		t.Fatalf("loop/terminal snapshot = steps %d terminal %q", snap.LoopSteps, snap.TerminalTool)
	}
	if len(snap.ReadSkills) != 1 || snap.ReadSkills[0] != "subagent-driven-development" {
		t.Fatalf("ReadSkills = %+v", snap.ReadSkills)
	}
	if snap.TokensTotal != 15 {
		t.Fatalf("TokensTotal = %d, want 15", snap.TokensTotal)
	}
	if !snap.DuplicateToolCall {
		t.Fatal("DuplicateToolCall = false, want true")
	}
}

func TestPruneOrchestrationSnapshotsHonorsTraceRetentionDays(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	b := &Bot{cfg: &config.Config{TraceRetentionDays: 7}}
	b.sessionStore().StoreSnapshot("old", orchestrationSnapshot{StoredAt: now.Add(-8 * 24 * time.Hour)})
	b.sessionStore().StoreSnapshot("fresh", orchestrationSnapshot{StoredAt: now.Add(-6 * 24 * time.Hour)})

	b.pruneOrchestrationSnapshots(now)

	if _, ok := b.loadOrchestrationSnapshot("old"); ok {
		t.Fatal("old snapshot survived retention pruning")
	}
	if _, ok := b.loadOrchestrationSnapshot("fresh"); !ok {
		t.Fatal("fresh snapshot was pruned")
	}
}

func TestDebugDocumentSendsAfterReturnsOnlyNewSends(t *testing.T) {
	b := &Bot{}
	b.recordDebugDocumentSend("old.txt", []byte("old"), "old")
	after := b.dbg.seq.Load()
	b.recordDebugDocumentSend("aura_artifact.txt", []byte("hello"), "caption")

	sends := b.debugDocumentSendsAfter(after)
	if len(sends) != 1 {
		t.Fatalf("sends = %d, want 1: %+v", len(sends), sends)
	}
	if sends[0].Filename != "aura_artifact.txt" || sends[0].SizeBytes != 5 || sends[0].Caption != "caption" {
		t.Fatalf("send = %+v", sends[0])
	}
}
