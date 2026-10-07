package runner

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

type recordedDecision struct {
	convID   string
	seq      int
	decision conversations.TurnDecision
}

type recordingDecisionStore struct {
	mu      sync.Mutex
	records []recordedDecision
	err     error
}

func (s *recordingDecisionStore) RecordTurnDecision(_ context.Context, convID string, seq int, d conversations.TurnDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, recordedDecision{convID: convID, seq: seq, decision: d})
	return s.err
}

func (s *recordingDecisionStore) snapshot() []recordedDecision {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.records)
}

type recordingRecallStore struct {
	mu       sync.Mutex
	recall   arcadedb.TurnRecall
	requests []arcadedb.TurnRecallRequest
}

func (s *recordingRecallStore) RecallTurns(_ context.Context, request arcadedb.TurnRecallRequest) (arcadedb.TurnRecall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, request)
	return s.recall, nil
}

func (s *recordingRecallStore) snapshot() []arcadedb.TurnRecallRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

type deferredProbeTool struct{ name string }

func (p deferredProbeTool) Spec() tools.Spec {
	return tools.Spec{Name: p.name, Summary: "Deferred probe.", Description: "Deferred probe.",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true}
}

func (deferredProbeTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "ok")
}

// reasoningRoute is a route that takes an adaptive effort; the test runner's default
// config is not a reasoning target.
func reasoningRoute() llm.Config {
	return llm.Config{Model: "test-model", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
		ContextWindow: 1000000, MaxOutputTokens: 32768, AdaptiveReasoning: true}
}

func decisionRunner(t *testing.T, cfg llm.Config, turns ...agenttest.FakeTurn) (*Runner, *fakeConvStore, *agenttest.FakeClient, *recordingDecisionStore) {
	t.Helper()
	client := agenttest.NewFakeClient(turns...)
	r, conv, _ := newTestRunnerCfg(t, client, cfg)
	decisions := &recordingDecisionStore{}
	r.turnDecisions = decisions
	return r, conv, client, decisions
}

func userSeqs(conv *fakeConvStore, convID string) []int {
	conv.mu.Lock()
	defer conv.mu.Unlock()
	var seqs []int
	for _, turn := range conv.turns[convID] {
		if turn.Role == llm.RoleUser {
			seqs = append(seqs, turn.Seq)
		}
	}
	return seqs
}

func TestTurnRecordsTheDecisionOnTheDispatchedUserTurn(t *testing.T) {
	r, conv, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(textResponseCall("call-1", "fatto")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortHigh)

	if _, err := drain(r.Turn(ctx, convID, new("riscrivi questa funzione in Go con i test"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	records := decisions.snapshot()
	seqs := userSeqs(conv, convID)
	if len(records) != 1 || len(seqs) != 1 || records[0].convID != convID || records[0].seq != seqs[0] {
		t.Fatalf("decisions %+v, user seqs %v; want one decision on the dispatched user turn", records, seqs)
	}
	d := records[0].decision
	if d.EffortSource != agent.EffortSourceUser || d.EffortRequested != "high" || d.Effort != "high" ||
		d.ContextKey != agent.TurnContextKey(nil, "") || !strings.HasPrefix(d.RouteKey, "route1:") ||
		!strings.HasPrefix(d.PolicyVersion, "policy1:") || d.OriginRef != "" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestEachTurnRecordsItsDecisionOnItsOwnUserTurn(t *testing.T) {
	r, conv, _, decisions := decisionRunner(t, reasoningRoute(),
		agenttest.ToolCallTurn(textResponseCall("call-1", "una poesia")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "a poem")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	for _, text := range []string{"scrivi una poesia sul mare", "ora traducila in inglese"} {
		if _, err := drain(r.Turn(ctx, convID, new(text))); err != nil {
			t.Fatalf("turn %q: %v", text, err)
		}
	}
	records := decisions.snapshot()
	seqs := userSeqs(conv, convID)
	if len(records) != 2 || len(seqs) != 2 || records[0].seq != seqs[0] || records[1].seq != seqs[1] {
		t.Fatalf("decisions %+v, user seqs %v; want one per user turn, in order", records, seqs)
	}
	if records[0].decision.ContextKey == records[1].decision.ContextKey {
		t.Fatal("the second turn carries the first turn's context key")
	}
}

func TestBranchRerunRecordsNoDecision(t *testing.T) {
	r, _, _, decisions := decisionRunner(t, reasoningRoute(),
		agenttest.ToolCallTurn(textResponseCall("call-1", "prima risposta")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "risposta rifatta")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	if _, err := drain(r.Turn(ctx, convID, new("spiegami la differenza tra TCP e UDP"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if _, err := drain(r.TurnBranch(ctx, convID, 0)); err != nil {
		t.Fatalf("branch re-run: %v", err)
	}
	if records := decisions.snapshot(); len(records) != 1 {
		t.Fatalf("decisions = %+v, want only the dispatched turn's", records)
	}
}

func TestFailedTurnRecordsNoDecision(t *testing.T) {
	r, _, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.FakeTurn{Err: errFake})
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortHigh)
	if _, err := drain(r.Turn(ctx, convID, new("analizza questo log di errore"))); err == nil {
		t.Fatal("the turn was expected to fail")
	}
	if records := decisions.snapshot(); len(records) != 0 {
		t.Fatalf("a failed turn recorded %+v", records)
	}
}

func TestPausedTurnRecordsItsDecisionAtThePause(t *testing.T) {
	r, conv, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(askUserCall("call-1", "Procedo?", "approval")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	if _, err := drain(r.Turn(ctx, convID, new("cancella i file temporanei nella cartella download"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	records := decisions.snapshot()
	seqs := userSeqs(conv, convID)
	if len(records) != 1 || records[0].seq != seqs[0] || records[0].decision.EffortSource != agent.EffortSourceUser {
		t.Fatalf("decisions %+v, user seqs %v; want the decision written at the pause flush", records, seqs)
	}
}

func TestExplicitNoneIsRecordedAsNone(t *testing.T) {
	r, _, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(textResponseCall("call-1", "ok")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortNone)
	if _, err := drain(r.Turn(ctx, convID, new("elenca i file nella cartella"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if records := decisions.snapshot(); len(records) != 1 || records[0].decision.EffortRequested != "none" || records[0].decision.Effort != "none" {
		t.Fatalf("decisions = %+v, want an explicit none, not NULL", records)
	}
}

// A route without adaptive effort still writes the context key and the route, so a later
// read knows the turn existed under that route and context.
func TestTurnWithoutAnEffortStillRecordsItsContext(t *testing.T) {
	r, _, _, decisions := decisionRunner(t, llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		agenttest.ToolCallTurn(textResponseCall("call-1", "ok")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(context.Background(), convID, new("elenca i file nella cartella"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	records := decisions.snapshot()
	if len(records) != 1 {
		t.Fatalf("decisions = %+v, want one", records)
	}
	if d := records[0].decision; d.ContextKey == "" || d.RouteKey == "" || d.EffortSource != "" || d.Effort != "" || d.EffortRequested != "" {
		t.Fatalf("decision = %+v, want the context and route without an effort", d)
	}
}

func TestTurnSurvivesAFailedDecisionWrite(t *testing.T) {
	r, conv, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(textResponseCall("call-1", "fatto")))
	decisions.err = errFake
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	if _, err := drain(r.Turn(ctx, convID, new("scrivi un haiku"))); err != nil {
		t.Fatalf("a failed decision write failed the turn: %v", err)
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if !slices.ContainsFunc(conv.turns[convID], func(turn conversations.AppendTurnParams) bool {
		return turn.Role == llm.RoleAssistant && turn.Content == "fatto"
	}) {
		t.Fatalf("turns = %+v, want the answer persisted", conv.turns[convID])
	}
}

func TestTurnBindsTheIdentitysMemoryAndPreloadsItsTools(t *testing.T) {
	r, conv, client, _ := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(textResponseCall("call-1", "fatto")))
	r.registry.Register(deferredProbeTool{name: "recall_probe"})
	recall := &recordingRecallStore{recall: arcadedb.TurnRecall{ToolTurns: []arcadedb.RecalledTurn{{
		Distance: 0.02, SourceRef: "postgres://aura/conversations/past/turns/1", Tools: []string{"recall_probe"},
	}}}}
	r.turnRecall = recall
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	const text = "prenota il solito tavolo per venerdì"
	if _, err := drain(r.Turn(ctx, convID, new(text))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	owner, err := conv.Get(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	requests := recall.snapshot()
	if len(requests) != 1 {
		t.Fatalf("recall requests = %d, want 1", len(requests))
	}
	got := requests[0]
	seqs := userSeqs(conv, convID)
	if got.IdentityID != owner.IdentityID || got.Text != text || got.ContextKey != agent.TurnContextKey(nil, "") ||
		got.SourceRef != reasoningSourceRef(convID, seqs[0]) || got.IncludeLabels ||
		!slices.Contains(got.DeferredTools, "recall_probe") {
		t.Fatalf("recall request = %+v", got)
	}
	if !slices.ContainsFunc(client.Requests[0].Tools, func(def llm.ToolDef) bool { return def.Function.Name == "recall_probe" }) {
		t.Fatal("the first request does not carry the remembered recall_probe")
	}
}

func TestReadTurnContextKeysWhatTheModelReadsBeforeTheMessage(t *testing.T) {
	visible := "che tempo fa domani a Cuneo?"
	always := "<skills>meteo: usa weather_lookup</skills>"
	memory := "<memory_context>vive a Cuneo</memory_context>"
	cfg := conversations.ContextConfig{AlwaysBlock: always,
		TransientContext: &conversations.TransientContext{Content: memory, BeforeCurrentUser: true}}
	history := []llm.Message{
		{Role: llm.RoleUser, Content: always},
		{Role: llm.RoleUser, Content: memory},
		{Role: llm.RoleUser, Content: visible},
	}
	instructions := []llm.Message{{Role: llm.RoleUser, Content: always}}

	got := readTurnContext(history, turnInput{visibleUserMsg: &visible}, cfg, nil)
	if got.key != agent.TurnContextKey(instructions, "") || !got.standalone {
		t.Fatalf("turn context = %+v, want the always-block in the key and the turn standalone", got)
	}

	// The memory block is rebuilt every turn; it must not move the key.
	other := "<memory_context>ha un cane</memory_context>"
	moved := slices.Clone(history)
	moved[1].Content = other
	movedCfg := cfg
	movedCfg.TransientContext = &conversations.TransientContext{Content: other, BeforeCurrentUser: true}
	if readTurnContext(moved, turnInput{visibleUserMsg: &visible}, movedCfg, nil).key != got.key {
		t.Fatal("the per-turn memory block moved the context key")
	}

	model := "<knowledge_base>\nmanuale.pdf\n</knowledge_base>\n\nUser message:\n" + visible
	withBlocks := readTurnContext(history, turnInput{visibleUserMsg: &visible, modelUserMsg: &model}, cfg, nil)
	if withBlocks.key != agent.TurnContextKey(instructions, strings.TrimSuffix(model, visible)) || withBlocks.key == got.key {
		t.Fatalf("catalog blocks are not in the key: %+v", withBlocks)
	}

	if readTurnContext(history, turnInput{visibleUserMsg: &visible}, cfg, []string{"attachment-1"}).key != "" {
		t.Fatal("a turn with attachments got a context key")
	}
	rewritten := "in breve: " + visible + " (rispondi in una riga)"
	if readTurnContext(history, turnInput{visibleUserMsg: &visible, modelUserMsg: &rewritten}, cfg, nil).key != "" {
		t.Fatal("a model message that does not end with the typed text got a context key")
	}
	if readTurnContext(history, turnInput{}, cfg, nil) != (turnContext{}) {
		t.Fatal("a run without a dispatched message got a context")
	}
}

func TestReadTurnContextIsStandaloneOnlyWithoutConversation(t *testing.T) {
	visible := "ok"
	fresh := []llm.Message{{Role: llm.RoleSystem, Content: "persisted system turn"}, {Role: llm.RoleUser, Content: visible}}
	if got := readTurnContext(fresh, turnInput{visibleUserMsg: &visible}, conversations.ContextConfig{}, nil); !got.standalone || got.key != agent.TurnContextKey(nil, "") {
		t.Fatalf("fresh turn context = %+v, want standalone with the empty-history key", got)
	}

	afterCode := []llm.Message{
		{Role: llm.RoleUser, Content: "riscrivi questa funzione in Go con i test"},
		{Role: llm.RoleAssistant, Content: "Ecco il piano."},
		{Role: llm.RoleUser, Content: visible},
	}
	afterThanks := []llm.Message{
		{Role: llm.RoleUser, Content: "grazie dell'aiuto"},
		{Role: llm.RoleAssistant, Content: "Prego!"},
		{Role: llm.RoleUser, Content: visible},
	}
	code := readTurnContext(afterCode, turnInput{visibleUserMsg: &visible}, conversations.ContextConfig{}, nil)
	thanks := readTurnContext(afterThanks, turnInput{visibleUserMsg: &visible}, conversations.ContextConfig{}, nil)
	if code.standalone || thanks.standalone {
		t.Fatal("an ok after an exchange was read as standalone")
	}
	if code.key == thanks.key || code.key == agent.TurnContextKey(nil, "") {
		t.Fatal("the same ok after two different exchanges shares a context key")
	}
}
