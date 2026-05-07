package telegram

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestDebugTextSmokeResultFromMessagesDetectsExecuteCodeAnd5050(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "compute", []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Name: "execute_code",
				Arguments: map[string]any{
					"code": "print(sum(range(1, 101)))",
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Content:    "exit_code: 0\nelapsed_ms: 42\n\n5050",
		},
		{
			Role:    "assistant",
			Content: "The result is 5050.",
		},
	})

	if !result.CalledExecuteCode {
		t.Fatal("CalledExecuteCode = false, want true")
	}
	if !result.Contains5050 {
		t.Fatal("Contains5050 = false, want true")
	}
	if result.FinalText != "The result is 5050." {
		t.Fatalf("FinalText = %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0] != "execute_code" {
		t.Fatalf("ToolCalls = %v", result.ToolCalls)
	}
}

func TestDebugTextSmokeResultFromMessagesDetectsArtifactMetadata(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "make artifact", []llm.Message{
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Name: "execute_code",
				Arguments: map[string]any{
					"code": "open('/tmp/aura_out/aura_artifact.txt','w').write('hello')",
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Content:    "exit_code: 0\nelapsed_ms: 42\n\nwrote files\n\nartifacts:\n- aura_sales_summary.csv (22 bytes, text/csv, delivered=true, persisted=true, source_id=src_0123456789abcdef)\n- aura_sales_plot.png (2048 bytes, image/png, delivered=true, persisted=true, source_id=src_fedcba9876543210)",
		},
	})

	if !result.CalledExecuteCode {
		t.Fatal("CalledExecuteCode = false, want true")
	}
	if !result.ContainsArtifactMetadata {
		t.Fatal("ContainsArtifactMetadata = false, want true")
	}
	if len(result.ArtifactFilenames) != 2 || result.ArtifactFilenames[0] != "aura_sales_summary.csv" || result.ArtifactFilenames[1] != "aura_sales_plot.png" {
		t.Fatalf("ArtifactFilenames = %v", result.ArtifactFilenames)
	}
	if len(result.ArtifactSourceIDs) != 2 || result.ArtifactSourceIDs[0] != "src_0123456789abcdef" || result.ArtifactSourceIDs[1] != "src_fedcba9876543210" {
		t.Fatalf("ArtifactSourceIDs = %v", result.ArtifactSourceIDs)
	}
}

func TestDebugDocumentSendsAfterReturnsOnlyNewSends(t *testing.T) {
	b := &Bot{}
	b.recordDebugDocumentSend("old.txt", []byte("old"), "old")
	after := b.debugDocSeq.Load()
	b.recordDebugDocumentSend("aura_artifact.txt", []byte("hello"), "caption")

	sends := b.debugDocumentSendsAfter(after)
	if len(sends) != 1 {
		t.Fatalf("sends = %d, want 1: %+v", len(sends), sends)
	}
	if sends[0].Filename != "aura_artifact.txt" || sends[0].SizeBytes != 5 || sends[0].Caption != "caption" {
		t.Fatalf("send = %+v", sends[0])
	}
}

func TestDebugTextSmokeResultFromMessagesReportsMissingTool(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "compute", []llm.Message{
		{Role: "assistant", Content: "5050"},
	})

	if result.CalledExecuteCode {
		t.Fatal("CalledExecuteCode = true, want false")
	}
	if !result.Contains5050 {
		t.Fatal("Contains5050 = false, want true from final text")
	}
}

func TestDebugTextSmokeResultDefaultsTokenFieldsToMissingUsage(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "compute", []llm.Message{
		{Role: "assistant", Content: "done"},
	})

	if result.TokenUsageReported {
		t.Fatal("TokenUsageReported = true, want false before RunDebugTextSmoke fills budget deltas")
	}
	if result.TokensPrompt != 0 || result.TokensCompletion != 0 || result.TokensTotal != 0 {
		t.Fatalf("token fields = prompt %d completion %d total %d, want zero defaults", result.TokensPrompt, result.TokensCompletion, result.TokensTotal)
	}
}

func TestDebugTextSmokeResultDetectsOrchestrationToolUsage(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "summary", []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "1", Name: "list_skills"}}},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "2", Name: "read_skill"}}},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "3", Name: "run_aurabot_swarm"}}},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "4", Name: "execute_code"}}},
	})

	if !result.SkillsRead {
		t.Fatal("SkillsRead = false, want true")
	}
	if !result.SwarmUsed {
		t.Fatal("SwarmUsed = false, want true")
	}
	if !result.SandboxUsed {
		t.Fatal("SandboxUsed = false, want true")
	}
}

func TestOrchestrationSnapshotPreservesRouteAndHiddenToolSignals(t *testing.T) {
	b := &Bot{}
	b.storeOrchestrationSnapshot("1148481707", turnStats{
		promptVersion:       "aura-agent-v1",
		promptHash:          "abc123",
		toolProfile:         "swarm_research",
		profileSelectReason: "matched swarm_research broad synthesis cues",
		toolsExposed:        []string{"run_aurabot_swarm"},
		toolsCalled:         []string{"execute_code"},
		hiddenToolRejected:  true,
		terminalSwarm:       true,
		swarmFinalization:   "aggregate",
		duplicateSwarm:      true,
		workerCount:         2,
		workerFailures:      0,
		tokensPrompt:        10,
		tokensCompletion:    5,
		tokensTotal:         15,
	})

	snap, ok := b.loadOrchestrationSnapshot("1148481707")
	if !ok {
		t.Fatal("snapshot missing")
	}
	if snap.ProfileSelectReason == "" {
		t.Fatal("ProfileSelectReason is empty")
	}
	if !snap.HiddenToolRejected {
		t.Fatal("HiddenToolRejected = false, want true")
	}
	if !snap.TerminalSwarm || snap.SwarmFinalization != "aggregate" {
		t.Fatalf("terminal swarm snapshot = %v %q", snap.TerminalSwarm, snap.SwarmFinalization)
	}
	if snap.TokensTotal != 15 {
		t.Fatalf("TokensTotal = %d, want 15", snap.TokensTotal)
	}
	if !snap.DuplicateSwarm || snap.WorkerCount != 2 || snap.WorkerFailures != 0 {
		t.Fatalf("swarm metrics snapshot = duplicate %v workers %d failures %d", snap.DuplicateSwarm, snap.WorkerCount, snap.WorkerFailures)
	}
}

func TestFormatTerminalSwarmResultUsesSynthesisAndMetrics(t *testing.T) {
	got := formatTerminalSwarmResult(`{"ok":true,"status":"completed","summary":"Pipeline is healthy.","metrics":{"total_tasks":3,"completed_tasks":3,"failed_tasks":0,"llm_calls":4,"tool_calls":7,"tokens_total":123,"wall_ms":456}}`)

	for _, want := range []string{"Pipeline is healthy.", "tasks=3/3 completed", "tokens=123", "wall_ms=456"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatTerminalSwarmResult() = %q, missing %q", got, want)
		}
	}
}

func TestTurnStatsApplySwarmMetricsRollsUpUsage(t *testing.T) {
	stats := turnStats{llmCalls: 1, toolCalls: 1, tokensPrompt: 2, tokensCompletion: 3, tokensTotal: 5}

	stats.applySwarmMetrics(`{"metrics":{"llm_calls":2,"tool_calls":4,"tokens_prompt":10,"tokens_completion":20,"tokens_total":30}}`, 1.5, 2.0)

	if stats.llmCalls != 3 || stats.toolCalls != 5 {
		t.Fatalf("calls = llm %d tools %d", stats.llmCalls, stats.toolCalls)
	}
	if stats.tokensPrompt != 12 || stats.tokensCompletion != 23 || stats.tokensTotal != 35 {
		t.Fatalf("tokens = prompt %d completion %d total %d", stats.tokensPrompt, stats.tokensCompletion, stats.tokensTotal)
	}
	if stats.workerCount != 0 || stats.workerFailures != 0 {
		t.Fatalf("worker metrics = count %d failures %d, want zero without task metrics", stats.workerCount, stats.workerFailures)
	}
	if stats.costUSD <= 0 {
		t.Fatalf("costUSD = %f, want positive", stats.costUSD)
	}
}

func TestTurnStatsApplySwarmMetricsRollsUpWorkerCounts(t *testing.T) {
	stats := turnStats{}

	stats.applySwarmMetrics(`{"metrics":{"total_tasks":3,"failed_tasks":1,"running_tasks":1,"pending_tasks":0}}`, 0, 0)

	if stats.workerCount != 3 || stats.workerFailures != 2 {
		t.Fatalf("worker metrics = count %d failures %d", stats.workerCount, stats.workerFailures)
	}
}

func TestCapDuplicateSwarmCallsKeepsOnlyFirstRun(t *testing.T) {
	calls := []llm.ToolCall{
		{ID: "1", Name: "run_aurabot_swarm"},
		{ID: "2", Name: "read_swarm_result"},
		{ID: "3", Name: "run_aurabot_swarm"},
	}

	got, duplicates := capDuplicateSwarmCalls("swarm_research", calls)

	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "2" {
		t.Fatalf("kept calls = %+v", got)
	}
	if len(duplicates) != 1 || duplicates[0].ID != "3" {
		t.Fatalf("duplicates = %+v", duplicates)
	}
}

func TestSwarmFinalizationMessagesDoesNotDuplicateRawResult(t *testing.T) {
	raw := `{"ok":true,"summary":"large aggregate"}`
	messages := []llm.Message{
		{Role: "user", Content: "audit everything"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "swarm-1", Name: "run_aurabot_swarm"}}},
		{Role: "tool", ToolCallID: "swarm-1", Content: raw},
	}

	got := swarmFinalizationMessages(messages)
	if len(got) != len(messages)+1 {
		t.Fatalf("messages len = %d, want %d", len(got), len(messages)+1)
	}
	if got[len(got)-1].Role != "user" {
		t.Fatalf("last role = %q, want user", got[len(got)-1].Role)
	}
	if strings.Contains(got[len(got)-1].Content, raw) {
		t.Fatalf("finalization instruction duplicates raw swarm result: %q", got[len(got)-1].Content)
	}

	var rawOccurrences int
	for _, msg := range got {
		if strings.Contains(msg.Content, raw) {
			rawOccurrences++
		}
	}
	if rawOccurrences != 1 {
		t.Fatalf("raw swarm result occurrences = %d, want 1", rawOccurrences)
	}
}
