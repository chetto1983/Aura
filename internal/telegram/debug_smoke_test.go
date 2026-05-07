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
	"github.com/aura/aura/internal/orchestration"
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
	b := &Bot{cfg: &config.Config{TraceRetentionDays: config.DefaultTraceRetentionDays}}
	b.storeOrchestrationSnapshot("1148481707", turnStats{
		promptVersion:       "aura-agent-v1",
		promptHash:          "abc123",
		toolProfile:         "swarm_research",
		profileSelectReason: "matched swarm_research broad synthesis cues",
		toolsExposed:        []string{"run_aurabot_swarm"},
		toolsCalled:         []string{"execute_code"},
		activeCapabilities:  []string{"swarm_research"},
		readSkills:          []string{"subagent-driven-development"},
		loopSteps:           2,
		hiddenToolRejected:  true,
		skillPreflightFail:  true,
		terminalTool:        "run_aurabot_swarm",
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
	if !snap.SkillPreflightFail {
		t.Fatal("SkillPreflightFail = false, want true")
	}
	if snap.LoopSteps != 2 || snap.TerminalTool != "run_aurabot_swarm" {
		t.Fatalf("loop/terminal snapshot = steps %d terminal %q", snap.LoopSteps, snap.TerminalTool)
	}
	if len(snap.ActiveCapabilities) != 1 || snap.ActiveCapabilities[0] != "swarm_research" {
		t.Fatalf("ActiveCapabilities = %+v", snap.ActiveCapabilities)
	}
	if len(snap.ReadSkills) != 1 || snap.ReadSkills[0] != "subagent-driven-development" {
		t.Fatalf("ReadSkills = %+v", snap.ReadSkills)
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

func TestPruneOrchestrationSnapshotsHonorsTraceRetentionDays(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	b := &Bot{cfg: &config.Config{TraceRetentionDays: 7}}
	b.orchMap.Store("old", orchestrationSnapshot{StoredAt: now.Add(-8 * 24 * time.Hour)})
	b.orchMap.Store("fresh", orchestrationSnapshot{StoredAt: now.Add(-6 * 24 * time.Hour)})

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
		cfg:   &config.Config{SkillPreflight: string(orchestration.SkillPreflightRequired)},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "hidden-1", Name: "execute_code"}},
		[]string{"search_memory"},
		orchestration.ProfileDefault,
		nil,
	)

	if !summary.hiddenRejected {
		t.Fatal("hiddenRejected = false, want true")
	}
	if dangerous.calls != 0 {
		t.Fatalf("hidden tool executed %d times, want 0", dangerous.calls)
	}
	if !strings.Contains(summary.lastResult, "not exposed in the active tool profile") {
		t.Fatalf("lastResult = %q, want hidden fatal", summary.lastResult)
	}
}

func TestUserFacingFatalToolResultHidesRawJSONForHiddenWriteWiki(t *testing.T) {
	raw := tools.FormatFatalToolError(errors.New(`tool "write_wiki" is not exposed in the active tool profile`))
	got := userFacingFatalToolResult(raw)
	if strings.Contains(got, `"ok":false`) || strings.Contains(got, `"retryable":false`) {
		t.Fatalf("user-facing result leaked raw tool JSON: %q", got)
	}
	if !strings.Contains(got, "write_wiki") || !strings.Contains(got, "cattura automatica") {
		t.Fatalf("user-facing result = %q, want write_wiki/capture explanation", got)
	}
}

func TestExecuteToolCallsRequiresApplicableSkillBeforeProtectedTool(t *testing.T) {
	reg := tools.NewRegistry(nil)
	doc := &countingTelegramTool{name: "create_pdf", result: "pdf created"}
	reg.Register(doc)
	b := &Bot{
		cfg:   &config.Config{SkillPreflight: string(orchestration.SkillPreflightRequired)},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	blocked := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "pdf-1", Name: "create_pdf"}},
		[]string{"create_pdf"},
		orchestration.ProfileDocument,
		nil,
	)
	if !blocked.skillPreflightRejected {
		t.Fatal("skillPreflightRejected = false, want true")
	}
	if doc.calls != 0 {
		t.Fatalf("protected tool executed %d times before skill read, want 0", doc.calls)
	}
	if !strings.Contains(blocked.lastResult, "requires reading an applicable skill") {
		t.Fatalf("blocked result = %q", blocked.lastResult)
	}
	if blocked.fatalResult != "" {
		t.Fatalf("fatalResult = %q, want retryable preflight error to stay in loop", blocked.fatalResult)
	}

	allowed := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "pdf-2", Name: "create_pdf"}},
		[]string{"create_pdf"},
		orchestration.ProfileDocument,
		[]string{"document-pdf"},
	)
	if allowed.skillPreflightRejected {
		t.Fatal("skillPreflightRejected = true after applicable skill read")
	}
	if doc.calls != 1 {
		t.Fatalf("protected tool calls = %d, want 1", doc.calls)
	}
	if allowed.lastResult != "pdf created" {
		t.Fatalf("allowed lastResult = %q", allowed.lastResult)
	}
}

func TestExecuteToolCallsDoesNotLetSameBatchSkillReadSatisfyProtectedTool(t *testing.T) {
	reg := tools.NewRegistry(nil)
	read := &countingTelegramTool{name: "read_skill", result: "skill body"}
	doc := &countingTelegramTool{name: "create_pdf", result: "pdf created"}
	reg.Register(read)
	reg.Register(doc)
	b := &Bot{
		cfg:   &config.Config{SkillPreflight: string(orchestration.SkillPreflightRequired)},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{
			{ID: "skill-1", Name: "read_skill", Arguments: map[string]any{"name": "document-pdf"}},
			{ID: "pdf-1", Name: "create_pdf"},
		},
		[]string{"read_skill", "create_pdf"},
		orchestration.ProfileDocument,
		nil,
	)

	if !summary.skillPreflightRejected {
		t.Fatal("skillPreflightRejected = false, want same-batch protected call rejected")
	}
	if len(summary.readSkillNames) != 1 || summary.readSkillNames[0] != "document-pdf" {
		t.Fatalf("readSkillNames = %+v, want document-pdf", summary.readSkillNames)
	}
	if read.calls != 1 {
		t.Fatalf("read_skill calls = %d, want 1", read.calls)
	}
	if doc.calls != 0 {
		t.Fatalf("create_pdf calls = %d, want 0", doc.calls)
	}
}

func TestExecuteToolCallsTracksTerminalTools(t *testing.T) {
	reg := tools.NewRegistry(nil)
	exec := &countingTelegramTool{name: "execute_code", result: "5050"}
	reg.Register(exec)
	b := &Bot{
		cfg:   &config.Config{SkillPreflight: string(orchestration.SkillPreflightRequired)},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "exec-1", Name: "execute_code"}},
		[]string{"execute_code"},
		orchestration.ProfileSandboxCompute,
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
			SkillPreflight:     string(orchestration.SkillPreflightRequired),
			TerminalToolPolicy: "off",
		},
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "exec-1", Name: "execute_code"}},
		[]string{"execute_code"},
		orchestration.ProfileSandboxCompute,
		[]string{"systematic-debugging"},
	)

	if summary.terminalTool != "" {
		t.Fatalf("terminalTool = %q, want disabled terminal policy", summary.terminalTool)
	}
	if exec.calls != 1 {
		t.Fatalf("execute_code calls = %d, want 1", exec.calls)
	}
}

func TestRunToolCallingLoopRetriesAfterSandboxSkillPreflightError(t *testing.T) {
	reg := tools.NewRegistry(nil)
	list := &countingTelegramTool{name: "list_skills", result: "systematic-debugging"}
	read := &countingTelegramTool{name: "read_skill", result: "skill body"}
	exec := &countingTelegramTool{name: "execute_code", result: "5050"}
	reg.Register(list)
	reg.Register(read)
	reg.Register(exec)
	fake := &scriptedTelegramLLM{responses: []llm.Response{
		{
			ToolCalls: []llm.ToolCall{{ID: "exec-early", Name: "execute_code"}},
		},
		{
			ToolCalls: []llm.ToolCall{
				{ID: "list-1", Name: "list_skills"},
				{ID: "skill-1", Name: "read_skill", Arguments: map[string]any{"name": "systematic-debugging"}},
			},
		},
		{
			ToolCalls: []llm.ToolCall{{ID: "exec-final", Name: "execute_code"}},
		},
		{Content: "5050"},
	}}
	b := &Bot{
		cfg: &config.Config{
			SkillPreflight: string(orchestration.SkillPreflightRequired),
		},
		llm:   fake,
		tools: reg,
	}
	convCtx := conversation.NewContext(conversation.Config{})
	convCtx.AddUserMessage("Use execute_code to compute sum(range(1, 101)).")
	allowlist, err := orchestration.ToolsForProfile(orchestration.ProfileSandboxCompute, orchestration.Availability{Sandbox: true})
	if err != nil {
		t.Fatalf("ToolsForProfile: %v", err)
	}

	response, stats := b.runToolCallingLoop(context.Background(), nil, convCtx, "1148481707", nil, allowlist, orchestration.PromptPlan{
		Version: "test",
		Hash:    "hash",
	}, orchestration.ProfileDecision{Profile: orchestration.ProfileSandboxCompute, Reason: "test"})

	if response != "5050" {
		t.Fatalf("response = %q, want 5050", response)
	}
	if !stats.skillPreflightFail {
		t.Fatal("skillPreflightFail = false, want true for first rejected execute_code")
	}
	if stats.terminalTool != "execute_code" {
		t.Fatalf("terminalTool = %q, want execute_code", stats.terminalTool)
	}
	if !stringSliceContains(stats.readSkills, "systematic-debugging") {
		t.Fatalf("readSkills = %+v, want systematic-debugging", stats.readSkills)
	}
	if exec.calls != 1 {
		t.Fatalf("execute_code calls = %d, want only the post-preflight call to execute", exec.calls)
	}
	if list.calls != 1 || read.calls != 1 {
		t.Fatalf("preflight calls = list %d read %d, want 1/1", list.calls, read.calls)
	}
	if len(fake.requests) != 3 {
		t.Fatalf("LLM requests = %d, want 3 without no-tool terminal finalization", len(fake.requests))
	}
	if len(fake.requests[0].Tools) == 0 || fake.requests[0].Tools[0].Name != "list_skills" {
		t.Fatalf("first exposed tool = %+v, want list_skills first", fake.requests[0].Tools)
	}
}

func TestMaxToolLoopIterationsHonorsRuntimeCapAndProfilePolicy(t *testing.T) {
	b := &Bot{cfg: &config.Config{MaxToolIterations: 20, AgentLoopMaxSteps: 4}}

	if got := b.maxToolLoopIterations(orchestration.ProfileDocument); got != 4 {
		t.Fatalf("document maxToolLoopIterations = %d, want runtime cap 4", got)
	}
	if got := b.maxToolLoopIterations(orchestration.ProfileSwarmResearch); got != 2 {
		t.Fatalf("swarm maxToolLoopIterations = %d, want profile cap 2", got)
	}
}

func TestExecuteToolCallsHonorsCustomHookVeto(t *testing.T) {
	reg := tools.NewRegistry(nil)
	safe := &countingTelegramTool{name: "search_memory", result: "should not run"}
	reg.Register(safe)
	hookErr := errors.New("blocked by custom hook")
	b := &Bot{
		cfg:       &config.Config{SkillPreflight: string(orchestration.SkillPreflightOff)},
		tools:     reg,
		orchHooks: &capturingOrchestrationHooks{beforeErr: hookErr},
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "hook-1", Name: "search_memory"}},
		[]string{"search_memory"},
		orchestration.ProfileDefault,
		nil,
	)

	if summary.fatalResult == "" || !strings.Contains(summary.fatalResult, "blocked by custom hook") {
		t.Fatalf("fatalResult = %q, want custom hook error", summary.fatalResult)
	}
	if safe.calls != 0 {
		t.Fatalf("tool executed %d times despite hook veto, want 0", safe.calls)
	}
}

func TestExecuteToolCallsReportsAvailableToolUsageToHooks(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(&countingTelegramTool{name: "run_aurabot_swarm", result: `{"metrics":{"tokens_prompt":1000,"tokens_completion":500,"tokens_total":1500}}`})
	hooks := &capturingOrchestrationHooks{}
	b := &Bot{
		cfg: &config.Config{
			SkillPreflight:       string(orchestration.SkillPreflightRequired),
			CostInputPerMTokens:  1,
			CostOutputPerMTokens: 2,
		},
		tools:     reg,
		orchHooks: hooks,
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "swarm-1", Name: "run_aurabot_swarm"}},
		[]string{"run_aurabot_swarm"},
		orchestration.ProfileSwarmResearch,
		nil,
	)

	if summary.lastResult == "" {
		t.Fatal("lastResult is empty")
	}
	if len(hooks.after) != 1 {
		t.Fatalf("after hook calls = %d, want 1", len(hooks.after))
	}
	event := hooks.after[0]
	if event.ResultSizeBytes == 0 || event.DurationMS < 0 {
		t.Fatalf("event size/duration = %d/%d", event.ResultSizeBytes, event.DurationMS)
	}
	if event.TokensPrompt != 1000 || event.TokensCompletion != 500 || event.TokensTotal != 1500 {
		t.Fatalf("event tokens = %d/%d/%d", event.TokensPrompt, event.TokensCompletion, event.TokensTotal)
	}
	if event.CostUSD <= 0 {
		t.Fatalf("event CostUSD = %f, want positive", event.CostUSD)
	}
}

type capturingOrchestrationHooks struct {
	beforeErr error
	after     []orchestration.TraceEvent
}

func (h *capturingOrchestrationHooks) BeforeProfileSelect(orchestration.TraceEvent) {}
func (h *capturingOrchestrationHooks) AfterProfileSelect(orchestration.TraceEvent)  {}
func (h *capturingOrchestrationHooks) BeforePromptCompose(orchestration.TraceEvent) {}
func (h *capturingOrchestrationHooks) AfterPromptCompose(orchestration.TraceEvent)  {}
func (h *capturingOrchestrationHooks) BeforeExposeTools(orchestration.TraceEvent)   {}
func (h *capturingOrchestrationHooks) AfterTurn(orchestration.TraceEvent)           {}

func (h *capturingOrchestrationHooks) BeforeToolCall(orchestration.TraceEvent) error {
	return h.beforeErr
}

func (h *capturingOrchestrationHooks) AfterToolCall(event orchestration.TraceEvent) {
	h.after = append(h.after, event)
}

type countingTelegramTool struct {
	name   string
	result string
	calls  int
}

func (t *countingTelegramTool) Name() string { return t.name }

func (t *countingTelegramTool) Description() string { return "counting fake tool" }

func (t *countingTelegramTool) Parameters() map[string]any { return map[string]any{"type": "object"} }

func (t *countingTelegramTool) Execute(context.Context, map[string]any) (string, error) {
	t.calls++
	return t.result, nil
}

type scriptedTelegramLLM struct {
	responses []llm.Response
	requests  []llm.Request
}

func (f *scriptedTelegramLLM) Send(context.Context, llm.Request) (llm.Response, error) {
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

func TestFormatTerminalSwarmResultUsesSynthesisAndMetrics(t *testing.T) {
	got := formatTerminalSwarmResult(`{"ok":true,"status":"completed","summary":"Pipeline is healthy.","metrics":{"total_tasks":3,"completed_tasks":3,"failed_tasks":0,"llm_calls":4,"tool_calls":7,"tokens_total":123,"wall_ms":456}}`)

	for _, want := range []string{"Pipeline is healthy.", "tasks=3/3 completed", "tokens=123", "wall_ms=456"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatTerminalSwarmResult() = %q, missing %q", got, want)
		}
	}
}

func TestFormatTerminalExecuteCodeResultKeepsStdoutAndArtifacts(t *testing.T) {
	raw := "exit_code: 0\nelapsed_ms: 42\n\n5050\n\nartifacts:\n- aura_sum.csv (36 bytes, text/csv, delivered=true, persisted=true, source_id=src_123)"

	got := formatTerminalExecuteCodeResult(raw)
	if !strings.Contains(got, "5050") || !strings.Contains(got, "Artifacts:") || !strings.Contains(got, "aura_sum.csv") {
		t.Fatalf("formatTerminalExecuteCodeResult() = %q", got)
	}
	if strings.Contains(got, "exit_code") || strings.Contains(got, "elapsed_ms") {
		t.Fatalf("formatTerminalExecuteCodeResult leaked raw execution header: %q", got)
	}
}

func TestFormatTerminalFileResultUsesMetadataWithoutExtraLLM(t *testing.T) {
	got := formatTerminalFileResult("create_docx", `{"source_id":"src_123","filename":"report.docx","size_bytes":42,"delivered":true}`)
	for _, want := range []string{"DOCX", "report.docx", "src_123", "delivered"} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatTerminalFileResult() = %q, missing %q", got, want)
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
