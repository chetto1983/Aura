package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type preExecPersistProbeTool struct {
	conv   *fakeConvStore
	convID string
}

func (p preExecPersistProbeTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "pre_exec_probe",
		Summary:     "Probe pre-execution persistence.",
		Description: "Probe pre-execution persistence.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Mutating:    true,
	}
}

func (p preExecPersistProbeTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	p.conv.mu.Lock()
	seen := false
	for _, turn := range p.conv.turns[p.convID] {
		if turn.Role == llm.RoleAssistant && strings.Contains(string(turn.ToolCalls), "pre_exec_probe") {
			seen = true
			break
		}
	}
	p.conv.mu.Unlock()
	if !seen {
		return tools.NewResult(ctx, "assistant tool_calls missing before mutating execution")
	}
	return tools.NewResult(ctx, "assistant tool_calls persisted before mutating execution")
}

type identityScopeProbeTool struct {
	got string
}

func (p *identityScopeProbeTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "identity_scope_probe",
		Summary:     "Probe identity scope.",
		Description: "Probe identity scope.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (p *identityScopeProbeTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	p.got = identityctx.IdentityID(ctx)
	return tools.NewResult(ctx, p.got)
}

// newTestRunner builds a Runner over the in-memory fakes + a scripted FakeClient,
// with a registry carrying ask_user + text_response so the agent can pause and
// terminate. titleTimeout is short so the auto-title worker is bounded.
func newTestRunner(t *testing.T, client llm.Client) (*Runner, *fakeConvStore, *fakePauseStore) {
	return newTestRunnerCfg(t, client, llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768})
}

// newTestRunnerCfg is newTestRunner with a caller-supplied llm.Config so a test can
// exercise model/price-table-dependent persistence (e.g. the CostUSD table fallback).
func newTestRunnerCfg(t *testing.T, client llm.Client, cfg llm.Config) (*Runner, *fakeConvStore, *fakePauseStore) {
	t.Helper()
	conv := newFakeConvStore()
	pause := newFakePauseStore()
	id := newFakeIdentityStore()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	reg.Register(&tools.ReadToolOutput{})
	r := New(Deps{
		Conv:            conv,
		Pause:           pause,
		Identity:        id,
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          client,
		Registry:        reg,
		LLM:             cfg,
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
	})
	return r, conv, pause
}

func newConvID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

// drain runs a Turn iterator to completion, collecting Events and the first error.
func drain(seq func(yield func(*agent.Event, error) bool)) ([]*agent.Event, error) {
	var evs []*agent.Event
	var firstErr error
	for ev, err := range seq {
		if err != nil {
			firstErr = err
			break
		}
		evs = append(evs, ev)
	}
	return evs, firstErr
}

// askUserCall builds a finalized ask_user tool call.
func askUserCall(id, question, kind string) llm.ToolCall {
	return agenttest.MakeToolCall(id, "ask_user", `{"question":`+quote(question)+`,"kind":`+quote(kind)+`}`)
}

func textResponseCall(id, text string) llm.ToolCall {
	return agenttest.MakeToolCall(id, "text_response", `{"text":`+quote(text)+`}`)
}

func quote(s string) string { return `"` + s + `"` }

// pauseEventOf returns the AwaitingInput payload of the first pause Event, or nil.
func pauseEventOf(evs []*agent.Event) *agent.AwaitingInput {
	for _, ev := range evs {
		if ev != nil && ev.Actions.AwaitingInput != nil {
			return ev.Actions.AwaitingInput
		}
	}
	return nil
}

// TestTurn_SingleAskUser_PausesAndWritesOneRow asserts a single ask_user pause
// writes exactly one paused_states row (sole writer) and stops the loop.
func TestTurn_SingleAskUser_PausesAndWritesOneRow(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "Proceed?", "approval")),
	)
	r, _, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	evs, err := drain(r.Turn(ctx, convID, new("hi")))
	if err != nil {
		t.Fatalf("turn error: %v", err)
	}
	if pause.inserts != 1 {
		t.Fatalf("want exactly 1 paused_states insert, got %d", pause.inserts)
	}
	if pause.unresolvedCount(convID) != 1 {
		t.Fatalf("want 1 unresolved pending, got %d", pause.unresolvedCount(convID))
	}
	if ai := pauseEventOf(evs); ai == nil {
		t.Fatal("expected a pause Event with AwaitingInput")
	} else if ai.Kind != "approval" {
		t.Fatalf("want kind approval, got %q", ai.Kind)
	}
}

func TestTurnScopesToolsToConversationIdentity(t *testing.T) {
	probe := &identityScopeProbeTool{}
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "identity_scope_probe", `{}`)),
	)
	r, conv, _ := newTestRunner(t, client)
	r.registry.Register(probe)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("hi"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	c, err := conv.Get(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	if probe.got != c.IdentityID {
		t.Fatalf("tool identity scope = %q, want conversation identity %q", probe.got, c.IdentityID)
	}
}

func TestTurnWithModelUserMessagePersistsVisibleTextAndSendsModelContext(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "done")),
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	visible := "Quali robot sono elencati?"
	model := "<knowledge_base trust=\"operator_pinned_context\">\nuse document_search\n</knowledge_base>\n\nUser message:\n" + visible
	if _, err := drain(r.TurnWithModelUserMessage(ctx, convID, visible, model)); err != nil {
		t.Fatalf("turn: %v", err)
	}

	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) == 0 || hist[0].Role != llm.RoleUser {
		t.Fatalf("history = %+v, want leading user turn", hist)
	}
	if hist[0].Content != visible {
		t.Fatalf("persisted user content = %q, want visible text %q", hist[0].Content, visible)
	}
	if strings.Contains(hist[0].Content, "knowledge_base") {
		t.Fatalf("persisted user content leaked model context: %q", hist[0].Content)
	}

	req := client.Requests[0]
	sawModelContext := false
	for _, msg := range req.Messages {
		if msg.Role == llm.RoleUser && msg.Content == model {
			sawModelContext = true
			break
		}
	}
	if !sawModelContext {
		t.Fatalf("LLM request did not include model-context user message: %+v", req.Messages)
	}
}

func TestTurnRejectsMismatchedContextIdentity(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "should not run")),
	)
	r, _, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	ctx := identityctx.WithIdentityID(
		context.Background(),
		"00000000-0000-0000-0000-000000000002",
	)
	_, err := drain(r.Turn(ctx, convID, new("hi")))
	if err == nil {
		t.Fatal("turn with mismatched authenticated identity succeeded")
	}
	if !strings.Contains(err.Error(), "conversation identity mismatch") {
		t.Fatalf("error = %v, want identity mismatch", err)
	}
	if client.CallCount() != 0 {
		t.Fatalf("LLM ran despite identity mismatch: calls=%d", client.CallCount())
	}
}

// TestSubmitAnswer_ReturnsRemaining asserts SubmitAnswer resolves one pending and
// reports the remaining count; the loop stays paused while >=1 row is unresolved.
func TestSubmitAnswer_ReturnsRemaining(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "First?", "clarification"),
			askUserCall("call-2", "Second?", "clarification"),
		),
	)
	r, _, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn error: %v", err)
	}
	if pause.inserts != 2 {
		t.Fatalf("want 2 inserts, got %d", pause.inserts)
	}

	pending, err := pause.ListPending(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	directive, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionAccept, Content: "a1"})
	if err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	if directive.Remaining != 1 {
		t.Fatalf("want remaining 1 after first answer, got %d", directive.Remaining)
	}
	directive, err = r.SubmitAnswer(ctx, pending[1].Token, ResponseInput{Action: askuser.ActionAccept, Content: "a2"})
	if err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	if directive.Remaining != 0 {
		t.Fatalf("want remaining 0 after both, got %d", directive.Remaining)
	}
}

// TestResume_NoSilentReRun_SC4 asserts that on resume the next request carries the
// original ask_user question→answer pair as messages, with exactly ONE ask_user
// tool_call total (no duplicate) and no replay LLM call to re-emit the question.
func TestResume_NoSilentReRun_SC4(t *testing.T) {
	client := agenttest.NewFakeClient(
		// Turn 1: the model emits an ask_user pause.
		agenttest.ToolCallTurn(askUserCall("call-1", "What city?", "clarification")),
		// Turn 2 (after resume): the model answers with a terminal text_response.
		agenttest.ToolCallTurn(textResponseCall("call-2", "Rome it is.")),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	// This test measures the resume round only. Mark the fake conversation titled so
	// the best-effort auto-title worker cannot add an unrelated LLM request.
	if err := conv.Rename(ctx, convID, "Resume invariant"); err != nil {
		t.Fatalf("mark titled: %v", err)
	}

	if _, err := drain(r.Turn(ctx, convID, new("Where am I?"))); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	callsAfterTurn1 := client.CallCount()
	if callsAfterTurn1 != 1 {
		t.Fatalf("turn 1 should be exactly 1 LLM call, got %d", callsAfterTurn1)
	}

	pending, err := pause.ListPending(ctx, convID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d (err=%v)", len(pending), err)
	}
	directive, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionAccept, Content: "Rome"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if directive.Remaining != 0 {
		t.Fatalf("want 0 remaining, got %d", directive.Remaining)
	}

	// Continue-after-resume: userMsg=nil.
	if _, err := drain(r.Turn(ctx, convID, nil)); err != nil {
		t.Fatalf("turn 2 (resume): %v", err)
	}
	// Exactly ONE additional LLM call on resume — no silent re-run of turn 1.
	if got := client.CallCount(); got != 2 {
		t.Fatalf("SC-4: resume must be exactly 1 fresh LLM call (total 2), got %d", got)
	}

	// The 2nd request's messages must carry the original ask_user tool_call AND its
	// RoleTool answer, with no DUPLICATE ask_user tool_call.
	req := client.LastRequest()
	askUserCalls, toolAnswers := 0, 0
	sawAnswer := false
	for _, m := range req.Messages {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "ask_user" {
				askUserCalls++
			}
		}
		if m.Role == llm.RoleTool {
			toolAnswers++
			if strings.Contains(m.Content, "Rome") {
				sawAnswer = true
			}
		}
	}
	if askUserCalls != 1 {
		t.Fatalf("SC-4: want exactly 1 ask_user tool_call in the resume request, got %d", askUserCalls)
	}
	if toolAnswers != 1 || !sawAnswer {
		t.Fatalf("SC-4: want the injected RoleTool answer carrying %q (toolAnswers=%d, sawAnswer=%v)", "Rome", toolAnswers, sawAnswer)
	}
}

func TestTurnPersistsAssistantToolCallsBeforeMutatingToolExecutes(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			agenttest.MakeToolCall("call-probe", "pre_exec_probe", `{}`),
		),
		agenttest.ToolCallTurn(textResponseCall("call-final", "done")),
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	r.registry.Register(preExecPersistProbeTool{conv: conv, convID: convID})
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("probe"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	for _, m := range hist {
		if m.Role == llm.RoleTool && m.ToolCallID == "call-probe" {
			if strings.Contains(m.Content, "missing before mutating execution") {
				t.Fatalf("tool executed before assistant tool_calls were persisted: %q", m.Content)
			}
			return
		}
	}
	t.Fatalf("missing RoleTool result for pre_exec_probe: %+v", hist)
}

// TestStop_AutoResolvesAndJoinsWaitGroup asserts Stop auto-resolves orphan pendings
// (zero unresolved after) and joins the auto-title worker (goleak via TestMain).
func TestStop_AutoResolvesAndJoinsWaitGroup(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "Proceed?", "approval")),
	)
	r, _, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("hi"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if pause.unresolvedCount(convID) != 1 {
		t.Fatalf("want 1 unresolved before Stop, got %d", pause.unresolvedCount(convID))
	}
	if err := r.Stop(ctx, convID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if pause.unresolvedCount(convID) != 0 {
		t.Fatalf("want 0 unresolved after Stop, got %d", pause.unresolvedCount(convID))
	}
}

func TestTurn_FastGreetingSkipsLLM(t *testing.T) {
	client := agenttest.NewFakeClient()
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	evs, err := drain(r.Turn(ctx, convID, new("ciao")))
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if client.CallCount() != 0 {
		t.Fatalf("fast greeting must not call the LLM, got %d calls", client.CallCount())
	}
	if len(evs) != 2 || evs[0].LLMResponse == nil || evs[0].LLMResponse.FinishReason != "" ||
		evs[1].LLMResponse == nil || evs[1].LLMResponse.FinishReason != "fast_path" {
		t.Fatalf("fast greeting events = %+v, want streamed chunk then fast_path final event", evs)
	}
	if evs[0].LLMResponse.Content == "" || evs[1].LLMResponse.Content != evs[0].LLMResponse.Content {
		t.Fatal("fast greeting returned empty content")
	}
	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 2 || hist[0].Role != llm.RoleUser || hist[0].Content != "ciao" ||
		hist[1].Role != llm.RoleAssistant || hist[1].Content != evs[1].LLMResponse.Content {
		t.Fatalf("history after fast greeting = %+v", hist)
	}
}

func TestFastReplyFor_NormalizesItalianGreeting(t *testing.T) {
	cases := []string{"ciao", "Ciao!", "ciao Aura", "\ufeffciao"}
	for _, in := range cases {
		answer, ok := fastReplyFor(in)
		if !ok || answer == "" {
			t.Fatalf("fastReplyFor(%q) = (%q, %v), want non-empty fast reply", in, answer, ok)
		}
	}
}

// TestAutoTitle_FiresAfterSeq3 asserts the auto-title worker sets the title after
// the history reaches seq>=3, using the fake client to produce the title.
func TestAutoTitle_FiresAfterSeq3(t *testing.T) {
	client := agenttest.NewFakeClient(
		// The chat turn: a terminal text_response (user + assistant = 2 turns; with
		// the seeded system turn the conversation reaches seq>=3).
		agenttest.ToolCallTurn(textResponseCall("call-1", "Sure, here you go.")),
		// The auto-title call: streamed title text.
		agenttest.TextChunks("stop", "Weather In Rome Question"),
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	// Pre-seed a system turn so the chat turn brings the count to >=3.
	seedSystemTurn(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("What's the weather in Rome?"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	// Join the auto-title worker.
	if err := r.Stop(ctx, convID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	c, err := conv.Get(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.TitleSet || c.Title == "" {
		t.Fatalf("expected auto-title to be set, got TitleSet=%v Title=%q", c.TitleSet, c.Title)
	}
}

// TestAutoTitle_ErrorLeavesTitleNull asserts a title-generation error never blocks
// chat and leaves the title NULL.
func TestAutoTitle_ErrorLeavesTitleNull(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "Done.")),
		// The auto-title call fails (real infra failure path).
		agenttest.FakeTurn{Err: errFake},
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	seedSystemTurn(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("hi"))); err != nil {
		t.Fatalf("turn must not fail on a title error: %v", err)
	}
	if err := r.Stop(ctx, convID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	c, err := conv.Get(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	if c.TitleSet {
		t.Fatalf("title must remain NULL on a generation error, got %q", c.Title)
	}
}

func mustCreate(t *testing.T, r *Runner, convID string) {
	t.Helper()
	if _, err := r.NewConversationWithID(context.Background(), convID); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
}

// seedSystemTurn appends a system turn (seq 1) so the next chat turn reaches the
// seq>=3 auto-title threshold (system + user + assistant).
func seedSystemTurn(t *testing.T, r *Runner, convID string) {
	t.Helper()
	p := conversations.AppendTurnParams{
		ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "system prompt",
	}
	if err := r.Conv.AppendTurn(context.Background(), p); err != nil {
		t.Fatalf("seed system turn: %v", err)
	}
}
