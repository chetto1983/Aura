package conversations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// compactionMarker tags the synthetic summary turn in its ToolCallID field, exactly
// as alwaysBlockMarker tags the always-block: a field a real persisted user turn
// never populates. isCompaction keys off it so toMessages renders a clean user-role
// message (a user message must not carry a tool_call_id).
const compactionMarker = "__aura_compaction__"

// compactionHeader/Footer frame the summary. Both live in compaction_template.go with the
// reasoning for each clause: the framing is what stops a model from reading condensed
// history as a fresh instruction and resuming work the person already called off.
const compactionHeader = compactionFraming
const compactionFooter = compactionFramingEnd

// The summary's length is asked for in the prompt and never capped on the wire; the
// transcript it summarizes is bounded in compaction_transcript.go, where hermes-agent's
// numbers and its reasons are recorded. The CALL has no private timeout: production passes
// the same measured total-call budget as every other LLM request, while the transport owns
// the independent stream-idle watchdog (amendment #153).
const summaryTemperature = 0.3

// Summarizer condenses a slice of earlier conversation rounds into a single dense
// summary. It is the LLM seam the context ladder uses to compact history instead of
// hard-dropping it (L2.4, before the deterministic L2.5 fail-safe). A nil Summarizer
// on ContextConfig disables compaction — the ladder then behaves exactly as before.
type Summarizer interface {
	Summarize(ctx context.Context, rounds []llm.Message) (string, error)
}

// isCompaction reports whether t is the injected synthetic summary turn.
func isCompaction(t Turn) bool {
	return t.ToolCallID == compactionMarker && t.Role == llm.RoleUser
}

// compactionTurn builds the synthetic summary turn, framed and marked.
//
// It carries the WATERMARK as its Seq — the seq of the newest turn it speaks for — because
// a later pass over the same list has to be able to tell what it already stands for:
// turnsAfter must not hand it back to the summarizer as fresh material (its text arrives
// there as the carried summary instead), and protectRecentTail must read it as the round
// boundary it genuinely is. Seq 0 made it look like a turn from before the beginning.
func compactionTurn(summary string, coversThroughSeq int) Turn {
	return Turn{
		Seq:        coversThroughSeq,
		Role:       llm.RoleUser,
		Content:    compactionHeader + summary + compactionFooter,
		ToolCallID: compactionMarker,
	}
}

// tryCompact summarizes the historical rounds (everything between the protected head
// and the active user-led round) into one synthetic summary turn, keeping head and
// active verbatim. Its outcome distinguishes a skipped attempt (disabled/no history)
// from a failed one (error/empty/oversized), so a later L2.5 drop can report degradation
// without mislabelling ordinary deterministic reduction. It never mutates turns.
type compactionAttemptStatus uint8

const (
	compactionSkipped compactionAttemptStatus = iota
	compactionSucceeded
	compactionFailed
)

func tryCompact(
	ctx context.Context,
	sum Summarizer,
	cache compactionCache,
	conversationID, branchID string,
	enc *tiktoken.Tiktoken,
	turns []Turn,
	cap int,
) ([]Turn, compactionAttemptStatus) {
	if sum == nil {
		return nil, compactionSkipped
	}
	head, history, active := splitHeadHistoryActive(turns)
	history, active = protectRecentTail(enc, history, active, cap)
	if len(history) == 0 {
		return nil, compactionSkipped // nothing older than the protected tail to condense
	}
	summary, ok := compactionSummary(ctx, sum, cache, conversationID, branchID, history)
	if !ok {
		return nil, compactionFailed
	}
	// The ladder keeps whatever it produced. It only got here because the history no longer
	// fits, so a summary beats the L2.5 hard drop waiting below even when it saves little.
	// `/compact` is in the opposite situation — nothing is over budget — and makes the
	// opposite call; see Store.Compact.
	if summary.Fresh {
		storeCompaction(ctx, cache, conversationID, branchID, summary)
	}
	out := make([]Turn, 0, len(head)+1+len(active))
	out = append(out, head...)
	out = append(out, compactionTurn(summary.Summary, history[len(history)-1].Seq))
	out = append(out, active...)
	if totalTokens(enc, out) > cap {
		return nil, compactionFailed // the summary itself (+ active + head) still overflows
	}
	return out, compactionSucceeded
}

// llmSummarizer is the production Summarizer: it wraps an llm.Client and a model,
// reusing the same single-call Stream+drain shape as GenerateTitle.
type llmSummarizer struct {
	client        llm.Client
	model         string
	contextWindow int
	totalTimeout  time.Duration
}

// NewLLMSummarizer builds a Summarizer over client+model. A nil client yields nil so
// callers can wire it unconditionally and let ContextConfig.Summarizer stay nil.
//
// contextWindow is the deployment's window: it ceilings the length the prompt asks for, so
// a 32K model is not asked for the summary a 1M model can afford. Zero means unknown and
// falls back to the fixed ceiling. totalTimeout is the deployment's shared LLM total-call
// budget; the transport independently applies its configured stream-idle watchdog.
func NewLLMSummarizer(client llm.Client, model string, contextWindow int, totalTimeout time.Duration) Summarizer {
	if client == nil {
		return nil
	}
	return &llmSummarizer{
		client: client, model: model, contextWindow: contextWindow, totalTimeout: totalTimeout,
	}
}

// Summarize renders the rounds to a bounded transcript and asks the model for a
// summary. It drains the stream fully (interface contract) and requires a clean stop.
func (s *llmSummarizer) Summarize(ctx context.Context, rounds []llm.Message) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("summarize: nil client")
	}
	transcript := renderRoundsForSummary(rounds)
	if transcript == "" {
		return "", fmt.Errorf("summarize: empty transcript")
	}
	if s.totalTimeout <= 0 {
		return "", fmt.Errorf("summarize: total timeout must be positive, got %s", s.totalTimeout)
	}
	ctx, cancel := context.WithTimeout(ctx, s.totalTimeout)
	defer cancel()

	enc, err := encoder()
	if err != nil {
		return "", fmt.Errorf("summarize: tiktoken encoder: %w", err)
	}
	instruction := summaryTemplate(summaryBudget(enc, transcript, s.contextWindow))
	if carriesPreviousSummary(rounds) {
		instruction = iterativeUpdateInstruction + "\n" + instruction
	}
	req := llm.Request{
		Model: s.model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: summarySystemPrompt},
			{Role: llm.RoleUser, Content: transcript + "\n\n---\n" + instruction},
		},
		Temperature: summaryTemperature,
		Reasoning:   llm.ReasoningConfig{Effort: llm.ReasoningEffortNone},
		ToolChoice:  "none",
	}
	ch, err := s.client.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarize: stream: %w", err)
	}
	var b strings.Builder
	var finishReason string
	for chunk := range ch { // drain fully (interface contract)
		if chunk.Err != nil {
			return "", fmt.Errorf("summarize: stream: %w", chunk.Err)
		}
		b.WriteString(chunk.Text)
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
	}
	if finishReason != "stop" {
		return "", fmt.Errorf("summarize: incomplete stream (finish_reason=%q)", finishReason)
	}
	return strings.TrimSpace(b.String()), nil
}
