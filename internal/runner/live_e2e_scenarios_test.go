//go:build live_e2e

// live_e2e_scenarios_test.go carries the four Phase-4 UAT scenarios driven against
// the live model + live Postgres. The harness + helpers live in live_e2e_test.go.
// Reuses the package's drain (runner_test.go) and assertWireValid / pauseEventOf
// (runner_multipause_test.go, runner_test.go) — those are unit-tier helpers that
// compile under any tag because the package's _test.go files build together.
package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// approvalInducer is a prompt explicit enough to reliably make DeepSeek-V4 Flash
// emit ONE ask_user(kind=approval) call. It names the tool's intent (ask for
// approval before an irreversible action) so a deliberate-by-design primitive
// (D-A3-04) actually fires.
const approvalInducer = "I want to permanently delete the folder ~/old-project and everything in it. " +
	"This is irreversible. Before you do anything, you MUST call the ask_user tool with kind=approval " +
	"to get my explicit yes/no approval. Do not proceed without asking."

// firstPause returns the first pending pause for a conversation or fails. The Runner
// is the sole writer; a missing pending means the live model did not pause — a real
// signal, surfaced with a clear message (never weakened to a mock).
func firstPause(t *testing.T, h *liveHarness, ctx context.Context, convID string) askuser.Pending {
	t.Helper()
	pending, err := h.r.PendingFor(ctx, convID)
	if err != nil {
		t.Fatalf("PendingFor: %v", err)
	}
	if len(pending) == 0 {
		t.Fatalf("SC#1: live model did not call ask_user — no paused_states row. "+
			"This is a real model-behavior signal, not a test bug. convID=%s", convID)
	}
	return pending[0]
}

// TestLiveE2E_PauseResume is SC#1: a live ask_user(approval) pause, a programmatic
// resume, and DB-ground-truth assertions — a paused_states row is created, its
// resumed_answer matches the injected answer, the rehydrated history is wire-valid
// with the tool answer keyed to the ORIGINAL tool_call_id, and the post-resume Turn
// drives a genuine fresh LLM reply.
func TestLiveE2E_PauseResume(t *testing.T) {
	h := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	convID := h.newLiveConversation(t, ctx)

	evs, err := drain(h.r.Turn(ctx, convID, userPtr(approvalInducer)))
	if err != nil {
		t.Fatalf("SC#1 pause turn: %v", err)
	}
	ai := pauseEventOf(evs)
	if ai == nil {
		t.Fatalf("SC#1: live run emitted no AwaitingInput pause Event — model did not call ask_user. " +
			"Real signal: the inducing prompt failed to trigger a pause against the live provider.")
	}
	if ai.Kind != tools.KindApproval {
		t.Logf("SC#1 note: model paused with kind=%q (inducer requested approval) — pause is still valid", ai.Kind)
	}

	pending := firstPause(t, h, ctx, convID)
	origToolCallID := pending.ToolCallID
	if origToolCallID == "" {
		t.Fatal("SC#1: paused_states row has empty tool_call_id — pause is not wire-anchorable")
	}
	t.Logf("SC#1 pause: token=%s tool_call_id=%s kind=%s question=%q", pending.Token, origToolCallID, ai.Kind, truncate(pending.Question, 80))

	// Resume programmatically with an accept answer.
	const answer = "yes"
	directive, err := h.r.SubmitAnswer(ctx, pending.Token, ResponseInput{Action: askuser.ActionAccept, Content: answer})
	if err != nil {
		t.Fatalf("SC#1 SubmitAnswer: %v", err)
	}
	if directive.Remaining != 0 {
		t.Fatalf("SC#1: want 0 remaining pending after the single resume, got %d", directive.Remaining)
	}

	// Ground truth #1: paused_states.resumed_answer == {accept, yes}, row resolved.
	gotAns, resolved := h.resumedAnswerOf(t, ctx, pending.Token)
	if !resolved {
		t.Fatal("SC#1: paused_states row not marked resumed after SubmitAnswer")
	}
	if gotAns.Action != askuser.ActionAccept || gotAns.Content != answer {
		t.Fatalf("SC#1: resumed_answer ground truth = {%q,%q}, want {accept,%q}", gotAns.Action, gotAns.Content, answer)
	}

	// Ground truth #2: the rehydrated history is wire-valid and the tool answer is
	// keyed to the ORIGINAL tool_call_id (assistant ask_user call -> tool answer).
	hist, err := h.conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("SC#1 LoadHistory: %v", err)
	}
	assertWireValid(t, hist)
	if !toolAnswerKeyedTo(hist, origToolCallID, answer) {
		t.Fatalf("SC#1: no RoleTool answer keyed to original tool_call_id %q with content %q in history", origToolCallID, answer)
	}

	// Ground truth #3: the post-resume Turn drives a GENUINE fresh LLM round — the
	// resumed answer is rehydrated into history and a real provider call is made. The
	// live model MAY converge on a final reply OR re-ask (emit another ask_user); BOTH
	// prove the pause/resume mechanism re-drove the model. Requiring convergence on a
	// final reply within N rounds tests the live model's mood, not Aura's resume — it
	// is inherently flaky — so we assert the MECHANISM: at least one genuine fresh round
	// (real prompt tokens on a reply, or a re-pause that grew the persisted history).
	turnsBefore, err := h.conv.CountTurns(ctx, convID)
	if err != nil {
		t.Fatalf("SC#1 CountTurns: %v", err)
	}
	reply, usage, redrove := h.resumePostResume(t, ctx, convID, 3)
	if !redrove {
		t.Fatal("SC#1: resume drove no genuine fresh LLM round (neither a real reply nor a history-growing re-ask)")
	}
	turnsAfter, err := h.conv.CountTurns(ctx, convID)
	if err != nil {
		t.Fatalf("SC#1 CountTurns after: %v", err)
	}
	if turnsAfter <= turnsBefore {
		t.Fatalf("SC#1: turn count did not grow after resume (%d -> %d) — no fresh round persisted", turnsBefore, turnsAfter)
	}
	if strings.TrimSpace(reply) != "" {
		if usage.PromptTokens == 0 {
			t.Fatal("SC#1: post-resume final reply reported zero prompt tokens — not a real LLM call")
		}
		t.Logf("SC#1 post-resume CONVERGED reply (%dB, prompt=%d cached=%d): %q", len(reply), usage.PromptTokens, usage.CachedTokens, truncate(reply, 120))
	} else {
		t.Logf("SC#1 post-resume: live model kept re-asking across all rounds — pause/resume mechanism verified via genuine fresh re-drives (history grew %d -> %d); convergence to a final reply is the model's prerogative, not required", turnsBefore, turnsAfter)
	}
}

// resumePostResume drives the post-resume Turn(nil). The live model MAY converge on
// a final reply OR re-ask (another ask_user) — BOTH are genuine fresh LLM rounds that
// prove the resume mechanism re-drove the model, so neither is treated as failure.
// It returns the final reply + usage (when the model converges) and redrove=true once
// ANY genuine fresh round is observed: a real reply with prompt tokens, or a re-pause
// that grew the persisted history. Convergence on a final reply is NOT required —
// asserting that against a live model is flaky and would test the model's mood, not
// Aura's pause/resume. A round that yields neither a reply nor a re-pause is a true
// no-op and fails.
func (h *liveHarness) resumePostResume(t *testing.T, ctx context.Context, convID string, maxRounds int) (reply string, usage llm.Usage, redrove bool) {
	t.Helper()
	for round := 1; round <= maxRounds; round++ {
		before, err := h.conv.CountTurns(ctx, convID)
		if err != nil {
			t.Fatalf("SC#1 resume round %d CountTurns: %v", round, err)
		}
		evs, err := drain(h.r.Turn(ctx, convID, nil))
		if err != nil {
			t.Fatalf("SC#1 resume round %d: %v", round, err)
		}
		if r, u := finalReply(evs); strings.TrimSpace(r) != "" {
			return r, u, true
		}
		// No final reply — the model re-paused. Confirm it was a genuine round (history
		// grew), resolve the new pending, and re-drive.
		if pauseEventOf(evs) == nil {
			t.Fatalf("SC#1 resume round %d: no reply and no re-pause — resume drove no LLM round. events: %s", round, describeEvents(evs))
		}
		after, err := h.conv.CountTurns(ctx, convID)
		if err != nil {
			t.Fatalf("SC#1 resume round %d CountTurns after: %v", round, err)
		}
		if after > before {
			redrove = true // a genuine fresh round persisted the re-ask
		}
		pending, err := h.r.PendingFor(ctx, convID)
		if err != nil || len(pending) == 0 {
			t.Fatalf("SC#1 resume round %d: re-paused but no pending row (err=%v)", round, err)
		}
		t.Logf("SC#1 resume round %d: model re-asked (kind=%s) — genuine re-drive, accepting and re-driving", round, pending[0].Kind)
		if _, err := h.r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionAccept, Content: "yes, go ahead"}); err != nil {
			t.Fatalf("SC#1 resume round %d: SubmitAnswer: %v", round, err)
		}
	}
	// Exhausted rounds with only re-asks: the mechanism re-drove the model every round
	// (redrove reflects whether history grew). Convergence is not required.
	return "", usage, redrove
}

// threeSimultaneousInducer asks the model to emit exactly three ask_user calls in
// ONE turn at three DISTINCT priorities, so SC#2 can assert FIFO priority DESC,
// per-row resolution, and the CR-02 single-assistant-turn wire shape.
const threeSimultaneousInducer = "Before you help me deploy, you need three decisions from me up front. " +
	"In a SINGLE response, call the ask_user tool THREE times — do not answer in between:\n" +
	"1. kind=approval, priority=90, question: 'Approve deploying to PRODUCTION now?'\n" +
	"2. kind=clarification, priority=50, question: 'Which region should I deploy to?'\n" +
	"3. kind=approval, priority=10, question: 'Should I also email the team afterward?'\n" +
	"Emit all three ask_user calls together in this one turn."

// TestLiveE2E_ThreeSimultaneous is SC#2: the live model emits 3 ask_user calls in
// one turn; the Runner writes 3 paused_states rows; PendingFor returns them in FIFO
// priority DESC; SubmitAnswers resolves all 3 with the correct {action,content};
// and the rehydrated history carries ONE assistant turn with all 3 tool_calls
// immediately followed by 3 tool answers keyed to their tool_call_ids (CR-02).
func TestLiveE2E_ThreeSimultaneous(t *testing.T) {
	h := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	convID := h.newLiveConversation(t, ctx)

	if _, err := drain(h.r.Turn(ctx, convID, userPtr(threeSimultaneousInducer))); err != nil {
		t.Fatalf("SC#2 pause turn: %v", err)
	}

	pending, err := h.r.PendingFor(ctx, convID)
	if err != nil {
		t.Fatalf("SC#2 PendingFor: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("SC#2: want exactly 3 paused_states rows from one turn, got %d. "+
			"Real signal: the live model did not emit 3 simultaneous ask_user calls. priorities=%v",
			len(pending), prioritiesOf(pending))
	}

	// Ground truth #1: FIFO order is priority DESC (90, 50, 10).
	prios := prioritiesOf(pending)
	for i := 1; i < len(prios); i++ {
		if prios[i] > prios[i-1] {
			t.Fatalf("SC#2: PendingFor not priority DESC: %v", prios)
		}
	}
	t.Logf("SC#2 FIFO priority DESC order: %v", prios)

	// Ground truth #2: resolve all 3 with distinct {action,content}; assert per-row.
	answers := map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionAccept, Content: "yes-deploy"},
		pending[1].Token: {Action: askuser.ActionAccept, Content: "eu-west-1"},
		pending[2].Token: {Action: askuser.ActionDecline, Content: ""},
	}
	remaining, err := h.r.SubmitAnswers(ctx, answers)
	if err != nil {
		t.Fatalf("SC#2 SubmitAnswers: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("SC#2: want 0 remaining after resolving all 3, got %d", remaining)
	}
	for tok, want := range answers {
		got, resolved := h.resumedAnswerOf(t, ctx, tok)
		if !resolved {
			t.Errorf("SC#2: token %s not resolved", tok)
			continue
		}
		// decline persists the "user declined" marker (toResumeAnswer), accept persists content.
		wantContent := want.Content
		if want.Action == askuser.ActionDecline {
			wantContent = declinedContent
		}
		if got.Action != want.Action || got.Content != wantContent {
			t.Errorf("SC#2: token %s resumed_answer = {%q,%q}, want {%q,%q}", tok, got.Action, got.Content, want.Action, wantContent)
		}
	}

	// Ground truth #3: CR-02 — ONE assistant turn carrying all 3 tool_calls,
	// immediately followed by 3 tool answers each keyed to its tool_call_id.
	hist, err := h.conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("SC#2 LoadHistory: %v", err)
	}
	assertWireValid(t, hist)
	turns, maxCalls := countAssistantPauseTurns(hist)
	if turns != 1 {
		t.Fatalf("SC#2 CR-02: want exactly 1 assistant pause turn carrying all calls, got %d", turns)
	}
	if maxCalls != 3 {
		t.Fatalf("SC#2 CR-02: the single assistant pause turn must carry all 3 ask_user tool_calls, got %d", maxCalls)
	}
	t.Logf("SC#2 CR-02 wire-valid: 1 assistant turn with %d tool_calls + 3 keyed tool answers", maxCalls)
}

// TestLiveE2E_AutoTitleAndCost is SC#3: a multi-turn conversation gets a non-empty
// auto-generated title and a cumulative total_cost_usd > 0 aggregated from per-turn
// usage. Asserts the conversations Store/DB layer the CLI `aura chat list` reads.
func TestLiveE2E_AutoTitleAndCost(t *testing.T) {
	h := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	convID := h.newLiveConversation(t, ctx)

	// Three short user turns — enough to cross the seq>=3 auto-title threshold.
	prompts := []string{
		"What is the capital of France? One word.",
		"And the capital of Japan? One word.",
		"Thanks. In one short sentence, what do those two cities have in common?",
	}
	// A transient provider hiccup on an assistant round must not fail this test — the
	// per-turn usage is persisted by the Runner regardless of whether the round itself
	// errored mid-stream (a logged, non-fatal condition, like SC#4).
	for i, p := range prompts {
		if _, err := drain(h.r.Turn(ctx, convID, userPtr(p))); err != nil {
			t.Logf("SC#3 turn %d: live model round errored (%v) — continuing", i+1, err)
		}
	}

	// The auto-title worker fires post-round on a WaitGroup; Stop joins it (bounded).
	// The worker is best-effort (D-A5-01): a transient live-provider hiccup on the
	// title call leaves the column NULL. To keep this autonomous test deterministic
	// without weakening the assertion, give the REAL worker a bounded number of
	// chances: each extra trivial Turn re-fires maybeAutoTitle (it only fires while
	// the title is still NULL and seq>=3), and Stop joins it. A title that never
	// lands across all attempts is a real signal (surfaced below), not flakiness we
	// hide.
	conv := h.awaitAutoTitle(t, ctx, convID, 3)

	// Ground truth #1: cumulative TOKEN usage aggregated from per-turn usage > 0. This
	// is the always-reliable cumulative-usage proof; OpenRouter reports prompt/
	// completion tokens on every turn (cost is reported only sometimes — see #3).
	if conv.TotalInputTokens <= 0 {
		t.Fatalf("SC#3: total_input_tokens = %d, want > 0 (cumulative usage must aggregate per turn)", conv.TotalInputTokens)
	}
	t.Logf("SC#3 cumulative: cost_usd=%.6f input_tok=%d output_tok=%d cached_tok=%d",
		conv.TotalCostUSD, conv.TotalInputTokens, conv.TotalOutputTokens, conv.TotalCachedTokens)

	// Ground truth #2: a non-empty auto-generated title (LLM-generated, set in DB).
	if !conv.TitleSet || strings.TrimSpace(conv.Title) == "" {
		t.Fatalf("SC#3: conversation has no auto-generated title (TitleSet=%v, Title=%q) — auto-title worker did not set it",
			conv.TitleSet, conv.Title)
	}
	t.Logf("SC#3 auto-title: %q", conv.Title)

	// Ground truth #3: the cumulative cost EQUALS the sum of the per-turn cache_metrics
	// rows the Runner wrote — the real aggregation-correctness invariant, regardless of
	// the absolute value. Cost is provider-reported on the wire (usage.Cost): OpenRouter
	// includes it most turns but not always; when it omits it for all turns the
	// aggregate is legitimately 0 (an Aura-faithful pass-through, NOT a defect). So we
	// hard-gate the aggregate==metrics-sum equality and the rows-written invariant, and
	// assert cost>0 only when the provider actually reported it.
	var metricCost float64
	var metricRows int
	if err := h.pool.QueryRow(ctx,
		"SELECT COALESCE(SUM(cost_usd),0), COUNT(*) FROM aura.cache_metrics WHERE conversation_id = $1",
		convID,
	).Scan(&metricCost, &metricRows); err != nil {
		t.Fatalf("SC#3 sum cache_metrics: %v", err)
	}
	if metricRows == 0 {
		t.Fatal("SC#3: no cache_metrics rows written — the per-turn metric path did not run")
	}
	if abs64(metricCost-conv.TotalCostUSD) > 1e-9 {
		t.Fatalf("SC#3: conversation aggregate cost_usd=%.6f != SUM(cache_metrics.cost_usd)=%.6f — aggregation is wrong",
			conv.TotalCostUSD, metricCost)
	}
	if conv.TotalCostUSD > 0 {
		t.Logf("SC#3 cumulative USD: $%.6f (> 0, provider reported cost) — matches cache_metrics sum over %d rows", conv.TotalCostUSD, metricRows)
	} else {
		t.Logf("SC#3 NOTE: cumulative cost_usd=0 — OpenRouter omitted per-turn cost on the wire this run (usage.Cost nil); "+
			"aggregate correctly equals the cache_metrics sum (%d rows). Token aggregation (input=%d) proves cumulative usage.",
			metricRows, conv.TotalInputTokens)
	}
}

// abs64 is the float64 absolute value (avoids a math import for one call).
func abs64(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// awaitAutoTitle joins the auto-title worker (Stop) and, while the title is still
// NULL, re-fires the REAL worker by driving an additional trivial Turn — up to
// maxAttempts times. It returns the conversation once titled, or fails after the
// budget is spent (a title that never lands across attempts is a real signal). This
// exercises the production worker path end-to-end; it never sets the title directly.
func (h *liveHarness) awaitAutoTitle(t *testing.T, ctx context.Context, convID string, maxAttempts int) conversations.Conversation {
	t.Helper()
	var conv conversations.Conversation
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := h.r.Stop(ctx, convID); err != nil {
			t.Fatalf("SC#3 Stop (join title worker, attempt %d): %v", attempt, err)
		}
		c, err := h.conv.Get(ctx, convID)
		if err != nil {
			t.Fatalf("SC#3 Get (attempt %d): %v", attempt, err)
		}
		conv = c
		if conv.TitleSet && strings.TrimSpace(conv.Title) != "" {
			return conv
		}
		if attempt < maxAttempts {
			t.Logf("SC#3: title still NULL after attempt %d — re-firing the auto-title worker", attempt)
			if _, err := drain(h.r.Turn(ctx, convID, userPtr("Briefly, anything else notable about those two cities?"))); err != nil {
				t.Fatalf("SC#3 retry turn (attempt %d): %v", attempt, err)
			}
		}
	}
	t.Fatalf("SC#3: conversation has no auto-generated title after %d worker attempts (TitleSet=%v, Title=%q) — "+
		"the live auto-title worker never produced a title; real signal, not test flake",
		maxAttempts, conv.TitleSet, conv.Title)
	return conv
}

// TestLiveE2E_SearchSimilarity is SC#4: persist turns over a live multi-turn
// conversation, then run the conversations pg_trgm search for a present term and
// assert results are ordered by similarity DESC and contain the matching turns.
func TestLiveE2E_SearchSimilarity(t *testing.T) {
	h := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	convID := h.newLiveConversation(t, ctx)

	// Drive term-dominant user turns so the search term is a large fraction of each
	// turn's content and clears the pg_trgm % threshold (a short term against
	// sentence-length content scores below it — correct pg_trgm behavior, not a
	// defect; the UAT spec records this). The user turns are persisted verbatim, so
	// the term is guaranteed present regardless of what the model replies. The third
	// turn is the most term-dominant, so it should rank highest by similarity.
	const term = "photosynthesis"
	prompts := []string{
		"Define photosynthesis briefly.",
		"More on photosynthesis?",
		"photosynthesis photosynthesis",
	}
	// The search target is the PERSISTED USER turn content, which the Runner writes
	// (appendUserTurn) BEFORE it calls the model. A transient provider hiccup on the
	// assistant round therefore does not affect what SC#4 asserts — the user turn is
	// already durable. So a per-turn LLM error is logged, not fatal: it must not turn
	// a search-ordering test into a network-flake failure.
	for i, p := range prompts {
		if _, err := drain(h.r.Turn(ctx, convID, userPtr(p))); err != nil {
			t.Logf("SC#4 turn %d: live model round errored (%v) — the user turn is already persisted, continuing", i+1, err)
		}
	}

	results, err := h.conv.SearchConversationTurns(ctx, term, 50)
	if err != nil {
		t.Fatalf("SC#4 SearchConversationTurns: %v", err)
	}

	// Ground truth #1: results are ordered by similarity DESC (the locked FTS query).
	for i := 1; i < len(results); i++ {
		if results[i].Similarity > results[i-1].Similarity {
			t.Fatalf("SC#4: results not similarity DESC at %d: %.4f > %.4f", i, results[i].Similarity, results[i-1].Similarity)
		}
	}

	// Ground truth #2: this conversation's term-dominant user turns are matched, and
	// every hit for this conversation actually contains the term (no false hit).
	var mine []SC4Hit
	for _, r := range results {
		if r.ConversationID != convID {
			continue
		}
		if !strings.Contains(strings.ToLower(r.Content), term) {
			t.Fatalf("SC#4: hit seq %d does not contain %q: %q", r.Seq, term, truncate(r.Content, 80))
		}
		mine = append(mine, SC4Hit{Seq: r.Seq, Sim: r.Similarity, Content: r.Content})
	}
	if len(mine) == 0 {
		t.Fatalf("SC#4: no matching turns for %q in this conversation — pg_trgm %% returned nothing for present term", term)
	}

	// Ground truth #3: the most term-dominant turn (the 'photosynthesis photosynthesis'
	// user turn) is present and ranks first among this conversation's hits (DESC).
	top := mine[0]
	for _, m := range mine[1:] {
		if m.Sim > top.Sim {
			t.Fatalf("SC#4: this conversation's hits not similarity DESC: seq %d (%.4f) > top seq %d (%.4f)", m.Seq, m.Sim, top.Seq, top.Sim)
		}
	}
	if !strings.Contains(strings.ToLower(top.Content), term) {
		t.Fatalf("SC#4: top hit does not contain %q: %q", term, truncate(top.Content, 80))
	}
	t.Logf("SC#4 search %q: %d hits in conv, similarity DESC verified, top seq=%d sim=%.4f content=%q",
		term, len(mine), top.Seq, top.Sim, truncate(top.Content, 60))
}

// finalReply extracts the final assistant prose + usage from a drained Turn's
// Events (the final Event carries FinishReason + the StateDelta usage the Runner
// stamps). usageFromStateDelta is the runner package's own helper (runner_persist.go).
func finalReply(evs []*agent.Event) (string, llm.Usage) {
	for _, ev := range evs {
		if ev == nil || ev.LLMResponse == nil {
			continue
		}
		if ev.LLMResponse.FinishReason != "" {
			return ev.LLMResponse.Content, usageFromStateDelta(ev.Actions.StateDelta)
		}
	}
	return "", llm.Usage{}
}

// SC4Hit is one per-conversation search hit projected for the SC#4 ordering +
// content assertions.
type SC4Hit struct {
	Seq     int
	Sim     float32
	Content string
}

// describeEvents summarizes a drained Turn's Events for a failure message: the
// ordered kinds + any tool names + finish reason, so a non-final terminal (another
// pause, a budget trip, a non-existent-tool loop) is visible in the failure.
func describeEvents(evs []*agent.Event) string {
	var b strings.Builder
	for _, ev := range evs {
		if ev == nil {
			continue
		}
		switch {
		case ev.Actions.AwaitingInput != nil:
			b.WriteString("[pause] ")
		case ev.Actions.Escalate:
			if r, ok := ev.Actions.StateDelta["limit_hit"].(string); ok {
				b.WriteString("[terminal:" + r + "] ")
			} else {
				b.WriteString("[terminal] ")
			}
		case ev.LLMResponse != nil && len(ev.LLMResponse.ToolCalls) > 0:
			for _, tc := range ev.LLMResponse.ToolCalls {
				b.WriteString("[tool_call:" + tc.Function.Name + "] ")
			}
		case ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "":
			b.WriteString("[final:" + ev.LLMResponse.FinishReason + ":" + truncate(ev.LLMResponse.Content, 60) + "] ")
		}
	}
	if b.Len() == 0 {
		return "(no events)"
	}
	return b.String()
}

// toolAnswerKeyedTo reports whether the history contains a RoleTool message keyed to
// the given tool_call_id carrying the expected content (SC#1 wire-correctness).
func toolAnswerKeyedTo(msgs []llm.Message, toolCallID, content string) bool {
	for _, m := range msgs {
		if m.Role == llm.RoleTool && m.ToolCallID == toolCallID && m.Content == content {
			return true
		}
	}
	return false
}

// prioritiesOf projects the pending priorities in their returned (FIFO) order.
func prioritiesOf(pending []askuser.Pending) []int {
	out := make([]int, len(pending))
	for i, p := range pending {
		out[i] = p.Priority
	}
	return out
}

// truncate shortens a string for log lines without pulling in a dependency.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
