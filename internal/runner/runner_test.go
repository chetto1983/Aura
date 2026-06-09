package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

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

func userPtr(s string) *string { return &s }

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

	evs, err := drain(r.Turn(ctx, convID, userPtr("hi")))
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

	if _, err := drain(r.Turn(ctx, convID, userPtr("go"))); err != nil {
		t.Fatalf("turn error: %v", err)
	}
	if pause.inserts != 2 {
		t.Fatalf("want 2 inserts, got %d", pause.inserts)
	}

	pending, err := pause.ListPending(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionAccept, Content: "a1"})
	if err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("want remaining 1 after first answer, got %d", remaining)
	}
	remaining, err = r.SubmitAnswer(ctx, pending[1].Token, ResponseInput{Action: askuser.ActionAccept, Content: "a2"})
	if err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("want remaining 0 after both, got %d", remaining)
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
	r, _, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, userPtr("Where am I?"))); err != nil {
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
	remaining, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionAccept, Content: "Rome"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("want 0 remaining, got %d", remaining)
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

	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err != nil {
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

	evs, err := drain(r.Turn(ctx, convID, userPtr("ciao")))
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

	if _, err := drain(r.Turn(ctx, convID, userPtr("What's the weather in Rome?"))); err != nil {
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

	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err != nil {
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
