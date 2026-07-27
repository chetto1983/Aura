package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// TestGatewayApprovalRoundTrip is the closed-blocker proof: it drives a FULL persist ->
// LoadManagedHistory -> re-Turn cycle over a real Runner (memConvStore is proven faithful
// for tool_calls args) and asserts that after the operator accepts the relayed
// gateway_approval ask_user pause (via the production newGatewayResumeHook), the resumed
// model re-emits the EXACT skill/delete call from rehydrated history, its fingerprint
// MATCHES the recorded ledger key (routeApprove.Consume hits), and the spy tool executes
// EXACTLY ONCE — NOT a gateway-internal ledger call, but the end-to-end round-trip.
//
// The scripted "model" (roundTripClient) copies the question + resume_context VERBATIM
// from the round-1 approval-required tool result it observes in the rehydrated history —
// it never hand-fabricates a resume_context, so the fidelity of the persist->resume path
// is exercised, not bypassed.
func TestGatewayApprovalRoundTrip(t *testing.T) {
	gw := gateway.New(config.ProfileSingleUserHardened, acquireStore{})
	spy := &spyDeleteTool{}
	reg := tools.NewRegistry()
	reg.Register(spy)
	reg.Register(tools.AskUser{})
	reg.Register(tools.TextResponse{})

	conv := newMemConvStore()
	client := &roundTripClient{}
	run := runner.New(runner.Deps{
		Conv:            conv,
		Pause:           newRTPauseStore(),
		Identity:        memIdentityStore{},
		CacheMetrics:    memCacheMetricStore{},
		ToolInvocations: memToolInvocationStore{},
		Client:          client,
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768, TotalTimeoutSec: 120},
		TitleTimeout:    time.Second,
		StopTimeout:     time.Second,
		Gateway:         gw,
		// The SAME gateway instance the runner's PEP reads — the hook writes its ledger.
		ResumeHook: chainResumeHooks(newGatewayResumeHook(gw)),
	})
	ctx := context.Background()
	convID, err := run.NewConversation(ctx)
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}

	// Round 1: the model emits skill/delete -> gets the approval-required result ->
	// relays it via ask_user -> the turn PAUSES. The mutating action is WITHHELD.
	paused, err := drainForPause(run.Turn(ctx, convID, strPtr("launch a swarm to build the thing")))
	if err != nil {
		t.Fatalf("round 1 turn: %v", err)
	}
	if !paused {
		t.Fatal("round 1 must PAUSE on the relayed gateway_approval ask_user")
	}
	if spy.Count() != 0 {
		t.Fatalf("spy Execute count after round 1 = %d, want 0 (mutating action withheld)", spy.Count())
	}

	// The relayed pending must carry a non-blank question (the checker WARNING fix) and the
	// gateway_approval resume_context the hook decodes.
	pending, err := run.PendingFor(ctx, convID)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("want exactly 1 pending, got %d", len(pending))
	}
	if pending[0].Question == "" {
		t.Fatal("relayed gateway_approval pending must render a non-blank question")
	}
	if len(pending[0].ResumeContext) == 0 {
		t.Fatal("relayed pending must carry the gateway_approval resume_context")
	}

	// Operator accepts via the production SubmitAnswers -> applyResumeHook ->
	// newGatewayResumeHook bridge (the model never writes the approval).
	if _, err := run.SubmitAnswers(ctx, map[string]runner.ResponseInput{
		pending[0].Token: {Action: askuser.ActionAccept, Content: "approved"},
	}); err != nil {
		t.Fatalf("submit answers (accept): %v", err)
	}

	// Round 2 (resume): Turn(convID, nil) rehydrates the verbatim skill/delete call, the
	// model re-emits it, routeApprove Consumes the recorded approval (fingerprint match),
	// the reservation acquires, and the spy tool executes EXACTLY ONCE — no re-pause.
	paused2, err := drainForPause(run.Turn(ctx, convID, nil))
	if err != nil {
		t.Fatalf("round 2 resume turn: %v", err)
	}
	if paused2 {
		t.Fatal("round 2 must NOT re-pause — the recorded approval must let the re-emit through")
	}
	if spy.Count() != 1 {
		t.Fatalf("spy Execute count after resume = %d, want exactly 1 (fingerprint matched, executed once)", spy.Count())
	}
}

// drainForPause ranges a Runner.Turn event stream, reporting whether any pause Event was
// emitted. A stream error aborts.
func drainForPause(seq iter.Seq2[*agent.Event, error]) (bool, error) {
	paused := false
	for ev, err := range seq {
		if err != nil {
			return paused, err
		}
		if ev != nil && ev.Actions.AwaitingInput != nil {
			paused = true
		}
	}
	return paused, nil
}

func strPtr(s string) *string { return &s }

// roundTripClient is a stateful llm.Client that drives the 4-call round-trip off the
// rehydrated history alone (never a static script): skill/delete -> observe the
// approval-required result -> relay it via ask_user (copying question + resume_context
// VERBATIM) -> [operator accepts] -> re-emit the exact skill/delete -> text_response.
type roundTripClient struct {
	mu    sync.Mutex
	calls int
}

func (c *roundTripClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.mu.Unlock()

	turn := scriptRoundTripTurn(req.Messages, n)
	ch := make(chan llm.Chunk, len(turn.Chunks)+1)
	for _, ck := range turn.Chunks {
		ch <- ck
	}
	close(ch)
	return ch, nil
}

func scriptRoundTripTurn(msgs []llm.Message, n int) agenttest.FakeTurn {
	// The spy already executed (its "executed" result is in history) → finish the turn.
	if anyToolResultContains(msgs, "executed") {
		return agenttest.ToolCallTurn(agenttest.MakeToolCall("call-final", "text_response", `{"text":"swarm launched"}`))
	}
	// The last tool result is the gateway approval-required message → relay it via ask_user,
	// copying the question + resume_context VERBATIM (fidelity, not a fabricated context).
	if last, ok := lastToolResult(msgs); ok {
		if q, rc, ok := extractApprovalRelay(last); ok {
			askArgs, _ := json.Marshal(map[string]any{"question": q, "kind": "approval", "resume_context": rc})
			return agenttest.ToolCallTurn(agenttest.MakeToolCall("call-ask", "ask_user", string(askArgs)))
		}
	}
	// Fresh user turn OR resumed after accept → emit skill/delete with a FRESH tool_call_id
	// (a re-emit changes the id — the ledger key deliberately excludes it, keying on args).
	return agenttest.ToolCallTurn(agenttest.MakeToolCall(
		fmt.Sprintf("call-skill-%d", n), "skill", `{"action":"delete","name":"obsolete-skill"}`))
}

// extractApprovalRelay pulls the gateway approval-required JSON out of the (untrusted
// <tool_output> envelope-wrapped) RoleTool content and returns the question +
// resume_context the model relays via ask_user. It locates the balanced JSON object
// inside the envelope, so it is robust to the trust-wrapper the agent adds around tool
// output.
func extractApprovalRelay(content string) (question string, resumeContext json.RawMessage, ok bool) {
	// The untrusted <tool_output> envelope HTML-escapes the body (" → &#34;); unescape it
	// the way a real model reads through the entities before parsing the JSON.
	obj, found := firstJSONObject(html.UnescapeString(content))
	if !found {
		return "", nil, false
	}
	var p struct {
		Error         string          `json:"error"`
		Question      string          `json:"question"`
		ResumeContext json.RawMessage `json:"resume_context"`
	}
	if err := json.Unmarshal([]byte(obj), &p); err != nil || p.Error != "gateway_approval_required" {
		return "", nil, false
	}
	return p.Question, p.ResumeContext, true
}

// firstJSONObject returns the first balanced {...} JSON object substring, honoring string
// literals + escapes so nested braces (the resume_context object) do not confuse it.
func firstJSONObject(s string) (string, bool) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

func lastToolResult(msgs []llm.Message) (string, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleTool {
			return msgs[i].Content, true
		}
	}
	return "", false
}

// anyToolResultContains reports whether any RoleTool message body contains marker (the
// content is envelope-wrapped, so an exact match would miss it).
func anyToolResultContains(msgs []llm.Message, marker string) bool {
	for _, m := range msgs {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, marker) {
			return true
		}
	}
	return false
}

// spyDeleteTool is a mutating GateRecommended skill/delete tool that counts Execute calls.
type spyDeleteTool struct {
	mu    sync.Mutex
	count int
}

func (s *spyDeleteTool) Spec() tools.Spec {
	return tools.Spec{Name: "skill", Summary: "skill lifecycle (test spy)", Mutating: true}
}

func (s *spyDeleteTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
	return tools.ToolResult{Preview: "executed"}, nil
}

func (s *spyDeleteTool) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// acquireStore is a gateway reservationStore that always acquires (rows==1 → Allow →
// Execute), so the round-trip exercises the resume→re-enter→execute path without a live PG
// (the single-reservation/idempotency guarantees are the db_integration tier's job).
type acquireStore struct{}

func (acquireStore) Insert(context.Context, toolinvocations.Event) error { return nil }

func (acquireStore) Reserve(context.Context, toolinvocations.Event) (bool, *toolinvocations.Event, error) {
	return true, nil, nil
}

func (acquireStore) GetEnd(context.Context, string, string, string) (*toolinvocations.Event, error) {
	return nil, nil
}

// rtPauseStore is an in-memory PauseStore that PRESERVES ResumeContext + ToolCallID (the
// cache-audit memPauseStore drops them), so the relayed gateway_approval context survives
// the persist→resume round-trip for the resume hook to decode.
type rtPauseStore struct {
	mu      sync.Mutex
	byToken map[string]*askuser.Pending
	answers map[string]askuser.ResumeAnswer
}

func newRTPauseStore() *rtPauseStore {
	return &rtPauseStore{byToken: map[string]*askuser.Pending{}, answers: map[string]askuser.ResumeAnswer{}}
}

func (m *rtPauseStore) Insert(_ context.Context, p askuser.InsertParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byToken[p.Token] = &askuser.Pending{
		Token: p.Token, ConversationID: p.ConversationID, Kind: p.Kind, Question: p.Question,
		Options: p.Options, Priority: p.Priority, ToolCallID: p.ToolCallID, ResumeContext: p.ResumeContext,
	}
	return nil
}

func (m *rtPauseStore) GetByToken(_ context.Context, token string) (askuser.Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byToken[token]
	if !ok {
		return askuser.Pending{}, askuser.ErrPauseNotFound
	}
	return *p, nil
}

func (m *rtPauseStore) ListPending(_ context.Context, conversationID string) ([]askuser.Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []askuser.Pending
	for tok, p := range m.byToken {
		if p.ConversationID != conversationID {
			continue
		}
		if _, resolved := m.answers[tok]; resolved {
			continue
		}
		out = append(out, *p)
	}
	return out, nil
}

func (m *rtPauseStore) MarkResumed(_ context.Context, token string, ans askuser.ResumeAnswer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byToken[token]; !ok {
		return askuser.ErrPauseNotFound
	}
	m.answers[token] = ans
	return nil
}

func (m *rtPauseStore) MarkResumedBatch(_ context.Context, answers map[string]askuser.ResumeAnswer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for token := range answers {
		if _, ok := m.byToken[token]; !ok {
			return askuser.ErrPauseNotFound
		}
	}
	for token, ans := range answers {
		m.answers[token] = ans
	}
	return nil
}

func (m *rtPauseStore) AutoResolveForConversation(_ context.Context, conversationID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for tok, p := range m.byToken {
		if p.ConversationID == conversationID {
			if _, done := m.answers[tok]; !done {
				m.answers[tok] = askuser.ResumeAnswer{Action: askuser.ActionCancel, Content: "<auto>"}
			}
		}
	}
	return nil
}
