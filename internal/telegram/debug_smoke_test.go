package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tools"
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

func TestRuntimeSnapshotPreservesToolAndHiddenToolSignals(t *testing.T) {
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
		hiddenToolRejected:  true,
		terminalTool:        "run_aurabot_swarm",
		duplicateToolCall:   true,
		tokensPrompt:        10,
		tokensCompletion:    5,
		tokensTotal:         15,
		retryNudgesSent:     1,
		spiralBreakerFired:  true,
		tieredBudgetTier:    "code_exec",
	})

	snap, ok := b.loadOrchestrationSnapshot("1148481707")
	if !ok {
		t.Fatal("snapshot missing")
	}
	if snap.ToolsetSelectReason == "" {
		t.Fatal("ToolsetSelectReason is empty")
	}
	if !snap.HiddenToolRejected {
		t.Fatal("HiddenToolRejected = false, want true")
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
	if snap.RetryNudgesSent != 1 || !snap.SpiralBreakerFired || snap.TieredBudgetTier != "code_exec" {
		t.Fatalf("guardrail snapshot fields = retry %d spiral %v tier %q", snap.RetryNudgesSent, snap.SpiralBreakerFired, snap.TieredBudgetTier)
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

func TestExecuteToolCallsRejectsHiddenToolBeforeRegistryExecution(t *testing.T) {
	reg := tools.NewRegistry(nil)
	dangerous := &countingTelegramTool{name: "execute_code", result: "should not run"}
	reg.Register(dangerous)
	b := &Bot{
		cfg:   &config.Config{},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "hidden-1", Name: "execute_code"}},
		[]string{"search_memory"},
		nil,
	)

	if !summary.hiddenRejected {
		t.Fatal("hiddenRejected = false, want true")
	}
	if dangerous.calls != 0 {
		t.Fatalf("hidden tool executed %d times, want 0", dangerous.calls)
	}
	if !strings.Contains(summary.lastResult, "not available in this runtime") {
		t.Fatalf("lastResult = %q, want hidden tool error", summary.lastResult)
	}
	if summary.fatalResult != "" {
		t.Fatalf("fatalResult = %q, want hidden tool to stay recoverable", summary.fatalResult)
	}
}

func TestUserFacingFatalToolResultHidesRawJSONForHiddenWriteFile(t *testing.T) {
	raw := tools.FormatFatalToolError(errors.New(`tool "write_file" is not exposed in the active toolset`))
	got := userFacingFatalToolResult(raw)
	if strings.Contains(got, `"ok":false`) || strings.Contains(got, `"retryable":false`) {
		t.Fatalf("user-facing result leaked raw tool JSON: %q", got)
	}
	if strings.Contains(got, "write_file") {
		t.Fatalf("user-facing result leaked tool name: %q", got)
	}
	if !strings.Contains(got, "non e' stato esposto") {
		t.Fatalf("user-facing result = %q, want capture explanation", got)
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

func TestRunToolCallingLoopExecutesSandboxWithoutRetry(t *testing.T) {
	reg := tools.NewRegistry(nil)
	exec := &countingTelegramTool{name: "execute_code", result: "5050"}
	reg.Register(exec)
	fake := &scriptedTelegramLLM{responses: []llm.Response{
		{
			ToolCalls: []llm.ToolCall{{ID: "exec-early", Name: "execute_code"}},
		},
		{Content: "5050"},
	}}
	b := &Bot{
		cfg:   &config.Config{},
		llm:   fake,
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})
	convCtx.AddUserMessage("Use execute_code to compute sum(range(1, 101)).")
	allowlist := []string{"execute_code"}

	response, stats := b.runToolCallingLoop(context.Background(), nil, convCtx, "1148481707", nil, allowlist, agentPromptPlan{
		Version: "test",
		Hash:    "hash",
	}, false)

	if response != "5050" {
		t.Fatalf("response = %q, want 5050", response)
	}
	if stats.terminalTool != "execute_code" {
		t.Fatalf("terminalTool = %q, want execute_code", stats.terminalTool)
	}
	if len(stats.readSkills) != 0 {
		t.Fatalf("readSkills = %+v, want none", stats.readSkills)
	}
	if exec.calls != 1 {
		t.Fatalf("execute_code calls = %d, want 1", exec.calls)
	}
	if len(fake.requests) != 1 {
		t.Fatalf("LLM requests = %d, want 1 without preflight retry", len(fake.requests))
	}
	if len(fake.requests[0].Tools) == 0 || fake.requests[0].Tools[0].Name != "execute_code" {
		t.Fatalf("first exposed tool = %+v, want execute_code first", fake.requests[0].Tools)
	}
}

func TestMaxToolLoopIterationsHonorsRuntimeConfigInsteadOfToolsetHardCap(t *testing.T) {
	b := &Bot{cfg: &config.Config{MaxToolIterations: 20, AgentLoopMaxSteps: 8}}

	if got := b.maxToolLoopIterations(); got != 8 {
		t.Fatalf("maxToolLoopIterations = %d, want runtime cap 8", got)
	}
}

func TestTerminalExecuteCodeResultMasksArtifactMetadata(t *testing.T) {
	raw := "exit_code: 0\nelapsed_ms: 42\n\nwrote files\n\nartifacts:\n- aura_sales_summary.csv (22 bytes, text/csv, delivered=true, persisted=true, source_id=src_0123456789abcdef)\n- aura_sales_plot.png (2048 bytes, image/png, delivered=true, persisted=true, source_id=src_fedcba9876543210)"

	got := formatTerminalExecuteCodeResult(raw)

	for _, leaked := range []string{"source_id", "delivered=true", "persisted=true", "text/csv", "image/png"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("formatTerminalExecuteCodeResult leaked %q in %q", leaked, got)
		}
	}
	for _, want := range []string{"wrote files", "aura_sales_summary.csv", "aura_sales_plot.png"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatTerminalExecuteCodeResult = %q, want %q", got, want)
		}
	}
}

func TestTerminalFileResultMasksSourceIDAndRawJSON(t *testing.T) {
	raw := `{"source_id":"src_secret","filename":"report.docx","size_bytes":1234,"delivered":true}`

	got := formatTerminalFileResult("create_docx", raw)

	for _, leaked := range []string{"src_secret", "source_id", "delivered"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("formatTerminalFileResult leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "report.docx") || !strings.Contains(got, "inviato") {
		t.Fatalf("formatTerminalFileResult = %q, want filename and delivery summary", got)
	}
}

func TestTerminalToolFallbackMasksInternalResultMetadata(t *testing.T) {
	raw := `{"source_id":"src_secret","metrics":{"tokens_total":123},"exit_code":0}`

	got := terminalToolFallbackResponse("execute_code", raw)

	for _, leaked := range []string{"src_secret", "source_id", "tokens_total", "exit_code"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("terminalToolFallbackResponse leaked %q in %q", leaked, got)
		}
	}
}

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

func TestModelToolNamesExposesCoreToolSearchSurface(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(&countingTelegramTool{name: "search_memory", result: "memory"})
	reg.Register(&countingTelegramTool{name: "schedule_task", result: "scheduled"})
	reg.Register(tools.NewToolSearchTool(reg))
	reg.Register(&countingTelegramTool{name: "execute_code", result: "code"})
	reg.Register(&categorizedCountingTelegramTool{
		countingTelegramTool: countingTelegramTool{name: "mcp_mail_imap_search_messages", result: "mail"},
		category:             tools.CategoryAutonomous,
	})

	b := &Bot{tools: reg}
	got := b.modelToolNames()

	for _, want := range []string{"search_memory", "schedule_task", "tool_search", "execute_code"} {
		if !stringSliceContains(got, want) {
			t.Fatalf("allowlist missing %q: %+v", want, got)
		}
	}
	if stringSliceContains(got, "mcp_mail_imap_search_messages") {
		t.Fatalf("raw MCP tool exposed before tool_search discovery: %+v", got)
	}
}

func TestRunToolCallingLoopAddsToolSearchDiscoveries(t *testing.T) {
	reg := tools.NewRegistry(nil)
	searchTool := tools.NewToolSearchTool(reg)
	reg.Register(searchTool)
	mailTool := &countingTelegramTool{name: "mcp_mail_imap_search_messages", result: "mail result"}
	reg.Register(mailTool)
	fake := &scriptedTelegramLLM{responses: []llm.Response{
		{
			ToolCalls: []llm.ToolCall{{ID: "search-1", Name: "tool_search", Arguments: map[string]any{
				"query": "mail search messages",
				"limit": float64(3),
			}}},
		},
		{
			ToolCalls: []llm.ToolCall{{ID: "mail-1", Name: "mcp_mail_imap_search_messages", Arguments: map[string]any{}}},
		},
		{Content: "mail done"},
	}}
	b := &Bot{
		cfg:   &config.Config{},
		llm:   fake,
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})
	convCtx.AddUserMessage("cerca email")

	response, stats := b.runToolCallingLoop(context.Background(), nil, convCtx, "1148481707", nil,
		[]string{"tool_search"}, agentPromptPlan{Version: "test", Hash: "hash"}, false)

	if response != "mail done" {
		t.Fatalf("response = %q, want mail done", response)
	}
	if mailTool.calls != 1 {
		t.Fatalf("mail tool calls = %d, want 1", mailTool.calls)
	}
	if len(fake.requests) != 3 {
		t.Fatalf("LLM requests = %d, want 3", len(fake.requests))
	}
	if hasLLMTool(fake.requests[0].Tools, "mcp_mail_imap_search_messages") {
		t.Fatalf("mail tool exposed before search: %+v", fake.requests[0].Tools)
	}
	if !hasLLMTool(fake.requests[1].Tools, "mcp_mail_imap_search_messages") {
		t.Fatalf("mail tool not exposed after search: %+v", fake.requests[1].Tools)
	}
	if !stringSliceContains(stats.toolsExposed, "mcp_mail_imap_search_messages") {
		t.Fatalf("stats toolsExposed = %+v, missing discovered mail tool", stats.toolsExposed)
	}
}

func hasLLMTool(defs []llm.ToolDefinition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}

type categorizedCountingTelegramTool struct {
	countingTelegramTool
	category string
}

func (t *categorizedCountingTelegramTool) Category() string { return t.category }

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

type scriptedTelegramLLM struct {
	responses []llm.Response
	requests  []llm.Request
}

func (f *scriptedTelegramLLM) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.requests = append(f.requests, req)
	if len(f.requests) <= len(f.responses) {
		return f.responses[len(f.requests)-1], nil
	}
	return llm.Response{}, nil
}

func (f *scriptedTelegramLLM) Stream(_ context.Context, req llm.Request) (<-chan llm.Token, error) {
	f.requests = append(f.requests, req)
	resp := llm.Response{}
	if len(f.requests) <= len(f.responses) {
		resp = f.responses[len(f.requests)-1]
	}
	ch := make(chan llm.Token, 1)
	ch <- llm.Token{
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
		Usage:     resp.Usage,
		Done:      true,
	}
	close(ch)
	return ch, nil
}

func TestFormatTerminalExecuteCodeResultKeepsStdoutAndArtifacts(t *testing.T) {
	raw := "exit_code: 0\nelapsed_ms: 42\n\n5050\n\nartifacts:\n- aura_sum.csv (36 bytes, text/csv, delivered=true, persisted=true, source_id=src_123)"

	got := formatTerminalExecuteCodeResult(raw)
	if !strings.Contains(got, "5050") || !strings.Contains(got, "File generati:") || !strings.Contains(got, "aura_sum.csv") {
		t.Fatalf("formatTerminalExecuteCodeResult() = %q", got)
	}
	for _, leaked := range []string{"exit_code", "elapsed_ms", "source_id", "delivered=true", "persisted=true"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("formatTerminalExecuteCodeResult leaked %q in %q", leaked, got)
		}
	}
}

func TestFormatTerminalFileResultUsesMetadataWithoutExtraLLM(t *testing.T) {
	got := formatTerminalFileResult("create_docx", `{"source_id":"src_123","filename":"report.docx","size_bytes":42,"delivered":true}`)
	for _, want := range []string{"DOCX", "report.docx", "inviato"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatTerminalFileResult() = %q, missing %q", got, want)
		}
	}
	for _, leaked := range []string{"src_123", "source_id", "delivered"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("formatTerminalFileResult() leaked %q in %q", leaked, got)
		}
	}
}

func TestTerminalToolFinalizationMessagesBlockToolMarkup(t *testing.T) {
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "file-1", Name: "write_file"}}},
		{Role: "tool", ToolCallID: "file-1", Content: `{"ok":true}`},
	}

	got := terminalToolFinalizationMessages(messages, "write_file")
	if len(got) != len(messages)+1 {
		t.Fatalf("messages len = %d, want %d", len(got), len(messages)+1)
	}
	last := got[len(got)-1].Content
	if !strings.Contains(last, "Do not call tools") || !strings.Contains(last, "Do not emit JSON, XML, DSML, or tool-call markup") {
		t.Fatalf("terminal finalization instruction = %q", last)
	}
}

func TestTerminalToolFallbackRejectsToolMarkup(t *testing.T) {
	raw := `<｜｜DSML｜｜tool_calls><｜｜DSML｜｜invoke name="write_file">`
	if !looksLikeToolCallMarkup(raw) {
		t.Fatal("looksLikeToolCallMarkup = false, want true")
	}
	got := terminalToolFallbackResponse("write_file", raw)
	if strings.Contains(got, "DSML") || strings.Contains(got, "tool_calls") {
		t.Fatalf("fallback leaked tool markup: %q", got)
	}
	if !strings.Contains(got, "file") {
		t.Fatalf("fallback = %q, want file completion", got)
	}
}
