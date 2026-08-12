package conversations

import (
	"context"
	"fmt"
	"log/slog"
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

// compactionHeader/Footer frame the LLM summary so the model reads it as condensed
// prior context, not as a fresh user instruction. It is Aura's own faithful summary
// of the real conversation, so it is trusted context (no distrust wording).
const compactionHeader = "<earlier_conversation_summary>\n" +
	"The earlier part of this conversation was condensed to fit the context window. " +
	"Treat the following as an accurate summary of what was already said.\n"
const compactionFooter = "\n</earlier_conversation_summary>"

// summaryMaxTokens bounds the summarizer's output; summaryRenderCap bounds the
// transcript sent to it (a compaction fires because history is large, so the
// summarizer input must itself be bounded — the recent tail is kept preferentially).
const (
	summaryMaxTokens   = 1024
	summaryPerTurnCap  = 4000
	summaryRenderCap   = 60000
	compactionTimeout  = 45 * time.Second
	summaryTemperature = 0.3
)

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

// tryCompact summarizes the historical rounds (everything between the protected head
// and the active user-led round) into one synthetic summary turn, keeping head and
// active verbatim. It returns (compacted, true) only when a non-empty summary was
// produced AND the result fits under cap; otherwise (nil, false) so the caller falls
// through to the deterministic L2.5 drop. It never mutates turns.
func tryCompact(ctx context.Context, sum Summarizer, enc *tiktoken.Tiktoken, turns []Turn, cap int) ([]Turn, bool) {
	if sum == nil {
		return nil, false
	}
	head, history, active := splitHeadHistoryActive(turns)
	if len(history) == 0 {
		return nil, false // nothing older than the active round to condense
	}
	summary, err := sum.Summarize(ctx, toMessages(history))
	if err != nil {
		slog.Warn("context compaction failed; falling back to deterministic drop", "err", err)
		return nil, false
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil, false
	}
	summaryTurn := Turn{
		Role:       llm.RoleUser,
		Content:    compactionHeader + summary + compactionFooter,
		ToolCallID: compactionMarker,
	}
	out := make([]Turn, 0, len(head)+1+len(active))
	out = append(out, head...)
	out = append(out, summaryTurn)
	out = append(out, active...)
	if totalTokens(enc, out) > cap {
		return nil, false // the summary itself (+ active + head) still overflows
	}
	return out, true
}

// llmSummarizer is the production Summarizer: it wraps an llm.Client and a model,
// reusing the same single-call Stream+drain shape as GenerateTitle.
type llmSummarizer struct {
	client llm.Client
	model  string
}

// NewLLMSummarizer builds a Summarizer over client+model. A nil client yields nil so
// callers can wire it unconditionally and let ContextConfig.Summarizer stay nil.
func NewLLMSummarizer(client llm.Client, model string) Summarizer {
	if client == nil {
		return nil
	}
	return &llmSummarizer{client: client, model: model}
}

const summaryPrompt = "You compress an earlier slice of a conversation into a dense factual summary " +
	"for another assistant to continue from. Preserve decisions, concrete facts, names, numbers, " +
	"file paths, user preferences, and still-open threads. Drop pleasantries and redundancy. " +
	"Write the summary ONLY: no preamble, no meta-commentary."

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
	ctx, cancel := context.WithTimeout(ctx, compactionTimeout)
	defer cancel()

	req := llm.Request{
		Model: s.model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: summaryPrompt},
			{Role: llm.RoleUser, Content: transcript},
		},
		Temperature: summaryTemperature,
		MaxTokens:   summaryMaxTokens,
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

// renderRoundsForSummary flattens the rounds into a labelled transcript, capping each
// turn and the total so a large history cannot blow the summarizer's own window. When
// over the total cap it keeps the MOST RECENT turns (the oldest are the least useful
// to a continuation) and prepends an elision note.
func renderRoundsForSummary(rounds []llm.Message) string {
	rendered := make([]string, 0, len(rounds))
	for _, m := range rounds {
		if m.Role != llm.RoleUser && m.Role != llm.RoleAssistant && m.Role != llm.RoleTool {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		if len(content) > summaryPerTurnCap {
			content = content[:summaryPerTurnCap] + " […]"
		}
		rendered = append(rendered, m.Role+": "+content)
	}
	if len(rendered) == 0 {
		return ""
	}
	elided := false
	total := 0
	kept := make([]string, 0, len(rendered))
	for i := len(rendered) - 1; i >= 0; i-- {
		if total+len(rendered[i]) > summaryRenderCap && len(kept) > 0 {
			elided = true
			break
		}
		total += len(rendered[i])
		kept = append(kept, rendered[i])
	}
	for l, r := 0, len(kept)-1; l < r; l, r = l+1, r-1 {
		kept[l], kept[r] = kept[r], kept[l]
	}
	out := strings.Join(kept, "\n")
	if elided {
		out = "[earlier turns omitted]\n" + out
	}
	return out
}
