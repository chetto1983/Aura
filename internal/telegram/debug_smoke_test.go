package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/agent/tools/registry"
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

func TestDebugTextSmokeResultFromMessagesDetectsRetrievalCapsule(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "memory", []llm.Message{
		{
			Role:    "system",
			Content: "Base prompt\n\n## Retrieval Capsule\n\n### Relevant Pages\n- [[aura-operating-memory]]",
		},
	})

	if !result.RetrievalCapsulePresent {
		t.Fatal("RetrievalCapsulePresent = false, want true")
	}
}

func TestDebugTextSmokeResultIgnoresBasePromptRetrievalCapsuleMention(t *testing.T) {
	result := debugTextSmokeResultFromMessages("1148481707", "memory", []llm.Message{
		{
			Role:    "system",
			Content: "The system prompt may include a compact ## Retrieval Capsule with relevant pages.",
		},
	})

	if result.RetrievalCapsulePresent {
		t.Fatal("RetrievalCapsulePresent = true for base prompt mention, want false")
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
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "1", Name: "search_files", Arguments: map[string]any{"pattern": "name: subagent-driven-development", "globs": []any{".agents/skills/**/SKILL.md"}}}}},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "2", Name: "read_file", Arguments: map[string]any{"path": ".agents/skills/subagent-driven-development/SKILL.md"}}}},
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

func TestRuntimeSnapshotPreservesToolSignals(t *testing.T) {
	b := &Bot{cfg: &config.Config{TraceRetentionDays: config.DefaultTraceRetentionDays}}
	b.storeOrchestrationSnapshot("1148481707", turnStats{
		promptVersion:       "aura-agent-v1",
		promptHash:          "abc123",
		toolset:             "registered",
		toolsetSelectReason: "all registered tools exposed",
		toolsExposed:        []string{"run_aurabot_swarm"},
		toolsCalled:         []string{"execute_code"},
		readSkills:          []string{"subagent-driven-development"},
		loopSteps:           2,
		terminalTool:        "run_aurabot_swarm",
		duplicateToolCall:   true,
		tokensPrompt:        10,
		tokensCompletion:    5,
		tokensTotal:         15,
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

func TestExecuteToolCallsRunsRegistryToolRegardlessOfAdvertisedAllowlist(t *testing.T) {
	// Registry is the single source of truth. The toolsExposed slice is kept
	// for snapshot/logging only — execution never gates on it. A registered
	// tool always runs, even when the model picked something outside the
	// curated prompt-side allowlist (e.g. recovered from prior-turn memory).
	reg := tools.NewRegistry(nil)
	target := &countingTelegramTool{name: "execute_code", result: "ran"}
	reg.Register(target)
	b := &Bot{
		cfg:   &config.Config{},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "exec-1", Name: "execute_code"}},
		[]string{"search_memory"}, // narrower than registry
		nil,
	)

	if target.calls != 1 {
		t.Fatalf("execute_code called %d times, want 1", target.calls)
	}
	if summary.lastResult != "ran" {
		t.Fatalf("lastResult = %q, want %q", summary.lastResult, "ran")
	}
}

func TestExecuteToolCallsRunsDocumentToolWithoutSkillGate(t *testing.T) {
	reg := tools.NewRegistry(nil)
	doc := &countingTelegramTool{name: "create_pdf", result: "pdf created"}
	reg.Register(doc)
	b := &Bot{
		cfg:   &config.Config{},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "pdf-1", Name: "create_pdf"}},
		[]string{"create_pdf"},
		nil,
	)
	if doc.calls != 1 {
		t.Fatalf("protected tool calls = %d, want 1", doc.calls)
	}
	if summary.lastResult != "pdf created" {
		t.Fatalf("lastResult = %q", summary.lastResult)
	}
}

func TestExecuteToolCallsSameBatchSkillReadAndProtectedToolBothRun(t *testing.T) {
	reg := tools.NewRegistry(nil)
	read := &countingTelegramTool{name: "read_file", result: "skill body"}
	doc := &countingTelegramTool{name: "create_pdf", result: "pdf created"}
	reg.Register(read)
	reg.Register(doc)
	b := &Bot{
		cfg:   &config.Config{},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{
			{ID: "skill-1", Name: "read_file", Arguments: map[string]any{"path": ".agents/skills/document-pdf/SKILL.md"}},
			{ID: "pdf-1", Name: "create_pdf"},
		},
		[]string{"read_file", "create_pdf"},
		nil,
	)

	if len(summary.readSkillNames) != 1 || summary.readSkillNames[0] != "document-pdf" {
		t.Fatalf("readSkillNames = %+v, want document-pdf", summary.readSkillNames)
	}
	if read.calls != 1 {
		t.Fatalf("read_file calls = %d, want 1", read.calls)
	}
	if doc.calls != 1 {
		t.Fatalf("create_pdf calls = %d, want 1", doc.calls)
	}
}

func TestExecuteToolCallsTracksTerminalTools(t *testing.T) {
	reg := tools.NewRegistry(nil)
	exec := &countingTelegramTool{name: "execute_code", result: "5050"}
	reg.Register(exec)
	b := &Bot{
		cfg:   &config.Config{},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "exec-1", Name: "execute_code"}},
		[]string{"execute_code"},
		[]string{"systematic-debugging"},
	)

	if summary.terminalTool != "execute_code" {
		t.Fatalf("terminalTool = %q, want execute_code", summary.terminalTool)
	}
	if exec.calls != 1 {
		t.Fatalf("execute_code calls = %d, want 1", exec.calls)
	}
}

func TestExecuteToolCallsHonorsTerminalToolPolicyOff(t *testing.T) {
	reg := tools.NewRegistry(nil)
	exec := &countingTelegramTool{name: "execute_code", result: "5050"}
	reg.Register(exec)
	b := &Bot{
		cfg: &config.Config{
			TerminalToolPolicy: "off",
		},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "exec-1", Name: "execute_code"}},
		[]string{"execute_code"},
		[]string{"systematic-debugging"},
	)

	if summary.terminalTool != "" {
		t.Fatalf("terminalTool = %q, want disabled terminal policy", summary.terminalTool)
	}
	if exec.calls != 1 {
		t.Fatalf("execute_code calls = %d, want 1", exec.calls)
	}
}


func TestMaxToolLoopIterationsHonorsRuntimeConfigInsteadOfToolsetHardCap(t *testing.T) {
	b := &Bot{cfg: &config.Config{AgentLoopMaxSteps: 8}}

	if got := b.maxToolLoopIterations(); got != 8 {
		t.Fatalf("maxToolLoopIterations = %d, want runtime cap 8", got)
	}
}

// Note: prior tests asserted that hardcoded canned formatters
// (formatTerminalExecuteCodeResult, formatTerminalFileResult,
// terminalToolFallbackResponse) scrubbed internal metadata. Those helpers
// were removed entirely — the agent now sounds like a copilot, not a
// templated robot, by routing every terminal-tool turn through the LLM
// synthesizer with a one-shot retry. The metadata-leak guarantee moved up
// the stack into the synthesis prompt itself ("no internal markers like
// exit_code or source_id — plain prose only").

func TestSearchMemoryArgumentsForceCallerChatID(t *testing.T) {
	reg := tools.NewRegistry(nil)
	search := &countingTelegramTool{name: "search_memory", result: "memory"}
	reg.Register(search)
	b := &Bot{cfg: &config.Config{}, tools: reg}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "search-1", Name: "search_memory", Arguments: map[string]any{"query": "documents", "limit": float64(9)}}},
		[]string{"search_memory"},
		nil,
	)

	if !strings.Contains(summary.lastResult, "memory") {
		t.Fatalf("lastResult = %q", summary.lastResult)
	}
	args := toolArgumentsForTool(
		"search_memory",
		map[string]any{"query": "private notes", "chat_id": float64(999), "limit": float64(9)},
		42,
	)
	if got := args["chat_id"]; got != float64(42) {
		t.Fatalf("chat_id = %#v, want 42", got)
	}
	if got := args["limit"]; got != float64(9) {
		t.Fatalf("limit = %#v, want model-provided limit preserved", got)
	}
}

func TestFailedTerminalToolDoesNotStopTurn(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(&errorTelegramTool{name: "execute_shell"})
	b := &Bot{cfg: &config.Config{TerminalToolPolicy: "on"}, tools: reg, logger: slog.Default()}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "shell-1", Name: "execute_shell", Arguments: map[string]any{"command": "find /home/user"}}},
		[]string{"execute_shell"},
		nil,
	)

	if summary.terminalTool != "" {
		t.Fatalf("terminalTool = %q, want empty for failed terminal tool", summary.terminalTool)
	}
	if got := convCtx.Messages()[len(convCtx.Messages())-1].Content; !strings.HasPrefix(got, "Error: ") {
		t.Fatalf("tool result = %q, want plain Error: prefix for model recovery", got)
	}
}

type countingTelegramTool struct {
	name     string
	result   string
	calls    int
	lastArgs map[string]any
}

func (t *countingTelegramTool) Name() string { return t.name }

func (t *countingTelegramTool) Description() string { return "counting fake tool" }

func (t *countingTelegramTool) Parameters() map[string]any { return map[string]any{"type": "object"} }

func (t *countingTelegramTool) Execute(_ context.Context, args map[string]any) (string, error) {
	t.calls++
	t.lastArgs = args
	return t.result, nil
}

type errorTelegramTool struct {
	name string
}

func (t *errorTelegramTool) Name() string { return t.name }

func (t *errorTelegramTool) Description() string { return "error fake tool" }

func (t *errorTelegramTool) Parameters() map[string]any { return map[string]any{"type": "object"} }

func (t *errorTelegramTool) Execute(context.Context, map[string]any) (string, error) {
	return "", fmt.Errorf("shell command failed (exit=1): find: '/home/user': No such file or directory")
}


func TestTerminalToolFinalizationMessagesAppendsLLMPrompt(t *testing.T) {
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "file-1", Name: "write_file"}}},
		{Role: "tool", ToolCallID: "file-1", Content: `{"ok":true}`},
	}

	got := terminalToolFinalizationMessages(messages, "write_file")
	if len(got) != len(messages)+1 {
		t.Fatalf("messages len = %d, want %d", len(got), len(messages)+1)
	}
	last := got[len(got)-1].Content
	for _, want := range []string{"Do not call tools", "natural prose"} {
		if !strings.Contains(last, want) {
			t.Fatalf("terminal finalization instruction = %q, missing %q", last, want)
		}
	}
}

func TestLooksLikeToolCallMarkupRecognisesDSML(t *testing.T) {
	raw := `<｜｜DSML｜｜tool_calls><｜｜DSML｜｜invoke name="write_file">`
	if !looksLikeToolCallMarkup(raw) {
		t.Fatal("looksLikeToolCallMarkup = false, want true")
	}
}
