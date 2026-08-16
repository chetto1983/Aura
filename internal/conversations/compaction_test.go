// Unit tier (no build tag): the L2.4 LLM-compaction step and its render/summarizer
// helpers, driven by local fakes (no DB, no network). The ladder falls back to the
// deterministic L2.5 drop on any summarizer failure — proven here.
package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
)

type fakeSummarizer struct {
	summary   string
	err       error
	calls     int
	gotRounds []llm.Message
}

func (f *fakeSummarizer) Summarize(_ context.Context, rounds []llm.Message) (string, error) {
	f.calls++
	f.gotRounds = rounds
	return f.summary, f.err
}

// overBudgetTurns is the shared fixture: a first round bloated past any small cap
// (user/assistant text L1 cannot touch) plus a small recent active round.
func overBudgetTurns() []Turn {
	big := strings.Repeat("word ", 3000)
	return []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 2, Role: llm.RoleUser, Content: big},
		{Seq: 3, Role: llm.RoleAssistant, Content: big},
		{Seq: 4, Role: llm.RoleUser, Content: "recent q"},
		{Seq: 5, Role: llm.RoleAssistant, Content: "recent a"},
	}
}

// hardCap = 37000 - 20000 - 13000 = 4000: the two bloated turns blow it, the compacted
// (summary + active) fits.
func overBudgetConfig() ContextConfig {
	return ContextConfig{ContextWindow: 20000 + 13000 + 4000, MaxOutputTokens: 1, ToolEvictAfterTurns: 100}
}

func joinContent(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// TestLadder_Compaction_SummarizesHistory_KeepsActive: over-budget history is
// condensed into a synthetic summary turn; the active round survives verbatim; no
// L2.5 rot event is written (compaction pre-empted the hard-drop).
func TestLadder_Compaction_SummarizesHistory_KeepsActive(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}
	sum := &fakeSummarizer{summary: "USER ASKED A THING; ASSISTANT DID IT"}

	cfg := overBudgetConfig()
	cfg.Summarizer = sum

	msgs, err := applyContextLadder(context.Background(), "conv", overBudgetTurns(), cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if sum.calls != 1 {
		t.Fatalf("summarizer must be called exactly once, got %d", sum.calls)
	}
	if len(emit.calls) != 0 {
		t.Fatalf("compaction must pre-empt L2.5 → no rot event, got %d", len(emit.calls))
	}
	joined := joinContent(msgs)
	if !strings.Contains(joined, "USER ASKED A THING") {
		t.Errorf("compacted history must carry the summary, got %.200q", joined)
	}
	if !strings.Contains(joined, "earlier_conversation_summary") {
		t.Errorf("summary must be framed as condensed prior context, got %.200q", joined)
	}
	if !strings.Contains(joined, "recent q") || !strings.Contains(joined, "recent a") {
		t.Errorf("active round must survive verbatim, got %.200q", joined)
	}
	if strings.Contains(joined, "word word word") {
		t.Errorf("bloated history must be condensed away, not kept verbatim")
	}
	if msgs[0].Role != llm.RoleSystem {
		t.Errorf("system L0 must be preserved, got role %q", msgs[0].Role)
	}
	// The summary turn must be a clean user message (no tool_call_id leaked).
	for _, m := range msgs {
		if strings.Contains(m.Content, compactionMarker) {
			t.Errorf("compaction marker leaked into a wire message")
		}
	}
}

// TestLadder_Compaction_FallsBackToDrop_OnSummarizerError: a summarizer error must NOT
// fail the load — the ladder falls through to the deterministic L2.5 drop.
func TestLadder_Compaction_FallsBackToDrop_OnSummarizerError(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}
	sum := &fakeSummarizer{err: errors.New("summarizer boom")}

	cfg := overBudgetConfig()
	cfg.Summarizer = sum

	msgs, err := applyContextLadder(context.Background(), "conv", overBudgetTurns(), cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder must not surface a summarizer error: %v", err)
	}
	if sum.calls != 1 {
		t.Fatalf("summarizer must be attempted once, got %d", sum.calls)
	}
	if len(emit.calls) != 1 {
		t.Fatalf("fallback to L2.5 must write exactly one rot event, got %d", len(emit.calls))
	}
	if strings.Contains(joinContent(msgs), "earlier_conversation_summary") {
		t.Errorf("no summary must be injected when the summarizer errors")
	}
}

// TestLadder_Compaction_FallsBackToDrop_WhenSummaryOverflows: a summary that is itself
// too large to fit must not be used — the ladder falls through to L2.5.
func TestLadder_Compaction_FallsBackToDrop_WhenSummaryOverflows(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}
	sum := &fakeSummarizer{summary: strings.Repeat("big ", 5000)} // ~5000 tokens > 4000 cap

	cfg := overBudgetConfig()
	cfg.Summarizer = sum

	_, err := applyContextLadder(context.Background(), "conv", overBudgetTurns(), cfg, enc, emit)
	if err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if sum.calls != 1 {
		t.Fatalf("summarizer must be attempted once, got %d", sum.calls)
	}
	if len(emit.calls) != 1 {
		t.Fatalf("an oversized summary must fall through to L2.5, got %d rot events", len(emit.calls))
	}
}

// TestLadder_NilSummarizer_UsesL25: with no summarizer the ladder behaves exactly as
// before (deterministic L2.5 drop), the backward-compatible default.
func TestLadder_NilSummarizer_UsesL25(t *testing.T) {
	enc := mustEncoderRaw(t)
	emit := &fakeRotEmitter{}

	cfg := overBudgetConfig() // Summarizer left nil

	if _, err := applyContextLadder(context.Background(), "conv", overBudgetTurns(), cfg, enc, emit); err != nil {
		t.Fatalf("ladder: %v", err)
	}
	if len(emit.calls) != 1 {
		t.Fatalf("nil summarizer must use L2.5 (one rot event), got %d", len(emit.calls))
	}
}

// TestTryCompact_NoHistory_ReturnsFalse: when only head + active exist, there is
// nothing older to condense, so the summarizer is never called.
func TestTryCompact_NoHistory_ReturnsFalse(t *testing.T) {
	enc := mustEncoderRaw(t)
	sum := &fakeSummarizer{summary: "should not be used"}
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{Seq: 2, Role: llm.RoleUser, Content: "only round"},
	}
	if _, ok := tryCompact(context.Background(), sum, nil, "", "", enc, turns, 4000); ok {
		t.Fatal("tryCompact must return false when there is no history to summarize")
	}
	if sum.calls != 0 {
		t.Fatalf("summarizer must not be called with no history, got %d", sum.calls)
	}
}

func TestRenderRoundsForSummary(t *testing.T) {
	rounds := []llm.Message{
		{Role: llm.RoleSystem, Content: "system noise"}, // dropped
		{Role: llm.RoleUser, Content: "  a question  "},
		{Role: llm.RoleAssistant, Content: ""}, // dropped (empty)
		{Role: llm.RoleTool, Content: "tool result"},
	}
	got := renderRoundsForSummary(rounds)
	if strings.Contains(got, "system noise") {
		t.Errorf("system turns must be dropped: %q", got)
	}
	if !strings.Contains(got, "user: a question") {
		t.Errorf("user turn must be labelled and trimmed: %q", got)
	}
	if !strings.Contains(got, "tool: tool result") {
		t.Errorf("tool turn must be included: %q", got)
	}
}

// TestRenderRoundsForSummary_RedactsCredentials guards the transcript boundary. What
// goes in is verbatim user text and tool output, so it can carry a credential the model
// put on a command line; what comes out leaves the process for an external summarizer
// and, once slice 1b lands, is persisted. A leak here is durable, not transient.
func TestRenderRoundsForSummary_RedactsCredentials(t *testing.T) {
	rounds := []llm.Message{
		{Role: llm.RoleTool, Content: `curl -H "Authorization: Bearer sk-or-v1abcdef1234567890ghij" https://x`},
		{Role: llm.RoleUser, Content: "connect with postgresql://aura:hunter2@db.internal/aura"},
		{Role: llm.RoleAssistant, Content: `{"api_key":"live-key-abcdef123456"}`},
	}
	got := renderRoundsForSummary(rounds)
	for _, leak := range []string{
		"sk-or-v1abcdef1234567890ghij",
		"hunter2",
		"live-key-abcdef123456",
	} {
		if strings.Contains(got, leak) {
			t.Errorf("credential reached the summarizer transcript: %q survives in %q", leak, got)
		}
	}
	if !strings.Contains(got, redact.Placeholder) {
		t.Errorf("expected redaction marker in %q", got)
	}
}

// TestRenderRoundsForSummary_CapDoesNotSplitARune asserts the per-turn byte cap lands on
// a rune boundary. A naive content[:cap] can slice a multi-byte rune in half, and the
// transcript then carries a replacement char into the summary — and into the DB with 1b.
func TestRenderRoundsForSummary_CapDoesNotSplitARune(t *testing.T) {
	// 'à' is 2 bytes, so a cap that lands mid-rune is guaranteed by an odd-length prefix.
	content := strings.Repeat("a", summaryPerTurnCap-1) + strings.Repeat("à", 40)
	got := renderRoundsForSummary([]llm.Message{{Role: llm.RoleUser, Content: content}})
	if !utf8.ValidString(got) {
		t.Errorf("per-turn cap split a rune: transcript is not valid UTF-8")
	}
	if strings.ContainsRune(got, '�') {
		t.Errorf("per-turn cap produced a replacement char: %.80q", got)
	}
}

func TestRenderRoundsForSummary_CapsAndElides(t *testing.T) {
	// One turn over the per-turn cap must be truncated with an ellipsis.
	long := []llm.Message{{Role: llm.RoleUser, Content: strings.Repeat("x", summaryPerTurnCap+500)}}
	got := renderRoundsForSummary(long)
	if !strings.Contains(got, "[…]") {
		t.Errorf("an over-cap turn must be truncated with an ellipsis: %.60q", got)
	}
	if len(got) > summaryPerTurnCap+64 {
		t.Errorf("per-turn cap not enforced, len=%d", len(got))
	}

	// Enough turns to blow the total cap: the oldest must be elided, newest kept.
	many := make([]llm.Message, 0, 40)
	for range 40 {
		many = append(many, llm.Message{Role: llm.RoleUser, Content: strings.Repeat("y", 3000)})
	}
	many = append(many, llm.Message{Role: llm.RoleAssistant, Content: "NEWEST-MARKER"})
	rendered := renderRoundsForSummary(many)
	if !strings.Contains(rendered, "earlier turns omitted") {
		t.Errorf("over-total render must carry the elision note: %.80q", rendered)
	}
	if !strings.Contains(rendered, "NEWEST-MARKER") {
		t.Errorf("the most recent turn must always be kept")
	}
}

func TestLLMSummarizer_Success(t *testing.T) {
	client := titleTextClient("stop", "condensed ", "summary")
	s := NewLLMSummarizer(client, "test-model")
	out, err := s.Summarize(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "hello"}})
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if out != "condensed summary" {
		t.Errorf("summary = %q, want %q", out, "condensed summary")
	}
	req := client.LastRequest()
	if req.Model != "test-model" || req.MaxTokens != summaryMaxTokens || req.ToolChoice != "none" {
		t.Errorf("request shape wrong: model=%q maxtokens=%d toolchoice=%q", req.Model, req.MaxTokens, req.ToolChoice)
	}
}

func TestLLMSummarizer_IncompleteStreamErrors(t *testing.T) {
	client := titleTextClient("length", "half a ") // finish_reason != stop
	s := NewLLMSummarizer(client, "m")
	if _, err := s.Summarize(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "x"}}); err == nil {
		t.Fatal("an incomplete stream must return an error")
	}
}

func TestLLMSummarizer_EmptyTranscriptErrors(t *testing.T) {
	client := titleTextClient("stop", "unused")
	s := NewLLMSummarizer(client, "m")
	// Only a system message → renderRoundsForSummary yields "" → error before any call.
	if _, err := s.Summarize(context.Background(), []llm.Message{{Role: llm.RoleSystem, Content: "sys"}}); err == nil {
		t.Fatal("an empty transcript must return an error")
	}
}

func TestNewLLMSummarizer_NilClient(t *testing.T) {
	if s := NewLLMSummarizer(nil, "m"); s != nil {
		t.Fatal("a nil client must yield a nil Summarizer so ContextConfig.Summarizer stays nil")
	}
}
