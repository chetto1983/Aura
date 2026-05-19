package agent

import (
	"testing"
	"time"
)

func TestNewSnapshotFromTurnStatsAllFieldsPropagate(t *testing.T) {
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	stats := TurnStats{
		PromptVersion:           "aura-agent-v1",
		PromptModules:           []string{"mod-a", "mod-b"},
		PromptHash:              "deadbeef",
		Toolset:                 "registered",
		ToolsetSelectReason:     "all tools exposed",
		ToolsExposed:            []string{"tool-x", "tool-y"},
		ToolsCalled:             []string{"tool-x"},
		ReadSkills:              []string{"skill-1"},
		LoopSteps:               3,
		LLMCalls:                4,
		ToolCalls:               2,
		SkillsRead:              true,
		SwarmUsed:               true,
		SandboxUsed:             true,
		TerminalTool:            "tool-x",
		DuplicateToolCall:       true,
		TokensPrompt:            100,
		TokensCompletion:        50,
		TokensTotal:             150,
		CostUSD:                 0.003,
	}

	snap := NewSnapshotFromTurnStats(stats, now)

	if !snap.StoredAt.Equal(now) {
		t.Fatalf("StoredAt = %v, want %v", snap.StoredAt, now)
	}
	if snap.PromptVersion != "aura-agent-v1" {
		t.Fatalf("PromptVersion = %q", snap.PromptVersion)
	}
	if snap.PromptHash != "deadbeef" {
		t.Fatalf("PromptHash = %q", snap.PromptHash)
	}
	if snap.Toolset != "registered" {
		t.Fatalf("Toolset = %q", snap.Toolset)
	}
	if snap.ToolsetSelectReason != "all tools exposed" {
		t.Fatalf("ToolsetSelectReason = %q", snap.ToolsetSelectReason)
	}
	if snap.LoopSteps != 3 || snap.LLMCalls != 4 || snap.ToolCalls != 2 {
		t.Fatalf("counters = steps %d llm %d tool %d", snap.LoopSteps, snap.LLMCalls, snap.ToolCalls)
	}
	if !snap.SkillsRead || !snap.SwarmUsed || !snap.SandboxUsed {
		t.Fatal("SkillsRead/SwarmUsed/SandboxUsed not set")
	}
	if snap.TerminalTool != "tool-x" {
		t.Fatalf("TerminalTool = %q", snap.TerminalTool)
	}
	if !snap.DuplicateToolCall {
		t.Fatal("DuplicateToolCall = false, want true")
	}
	if snap.TokensPrompt != 100 || snap.TokensCompletion != 50 || snap.TokensTotal != 150 {
		t.Fatalf("tokens = prompt %d completion %d total %d", snap.TokensPrompt, snap.TokensCompletion, snap.TokensTotal)
	}
	if snap.CostUSD != 0.003 {
		t.Fatalf("CostUSD = %v, want 0.003", snap.CostUSD)
	}

	// Slice-clone behavior: mutating original must not affect snapshot.
	stats.PromptModules[0] = "mutated"
	if snap.PromptModules[0] == "mutated" {
		t.Fatal("PromptModules not deep-copied")
	}
	stats.ToolsExposed[0] = "mutated"
	if snap.ToolsExposed[0] == "mutated" {
		t.Fatal("ToolsExposed not deep-copied")
	}
	stats.ToolsCalled[0] = "mutated"
	if snap.ToolsCalled[0] == "mutated" {
		t.Fatal("ToolsCalled not deep-copied")
	}
	stats.ReadSkills[0] = "mutated"
	if snap.ReadSkills[0] == "mutated" {
		t.Fatal("ReadSkills not deep-copied")
	}
}
