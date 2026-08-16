package conversations

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
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

// summaryMaxTokens bounds the summarizer's output; summaryRenderCap bounds the
// transcript sent to it (a compaction fires because history is large, so the
// summarizer input must itself be bounded — the recent tail is kept preferentially).
const (
	summaryMaxTokens  = 1024
	summaryPerTurnCap = 4000
	// summaryToolOutputCap is the per-tool-turn slice the summarizer is shown. Deliberately
	// far below summaryPerTurnCap: see the pruning comment in renderRoundsForSummary.
	summaryToolOutputCap = 400
	summaryRenderCap     = 60000
	compactionTimeout    = 45 * time.Second
	summaryTemperature   = 0.3
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
func tryCompact(
	ctx context.Context,
	sum Summarizer,
	cache compactionCache,
	conversationID, branchID string,
	enc *tiktoken.Tiktoken,
	turns []Turn,
	cap int,
) ([]Turn, bool) {
	if sum == nil {
		return nil, false
	}
	head, history, active := splitHeadHistoryActive(turns)
	history, active = protectRecentTail(enc, history, active, cap)
	if len(history) == 0 {
		return nil, false // nothing older than the protected tail to condense
	}
	summary, ok := compactionSummary(ctx, sum, cache, conversationID, branchID, history)
	if !ok {
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

	instruction := summaryTemplate(summaryMaxTokens)
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
		// Tool output is pruned HARD before the summarizer sees it, and this is where the
		// tokens actually are: measured on three real conversations (2026-08-16), tool
		// content was 211,074 of 345,000 characters -- 61% -- against 18,790 of assistant
		// prose. It is also the least summarizable material in the transcript, because it
		// describes HOW something was done rather than WHAT was decided, and a page of
		// shell transcript reliably drowns the one sentence that mattered.
		//
		// The head is kept rather than the tail: a tool result announces its outcome first
		// (exit code, error class, first rows) and pads afterwards. hermes-agent's
		// compressor does the same pre-pass for the same reason.
		cap := summaryPerTurnCap
		if m.Role == llm.RoleTool {
			cap = summaryToolOutputCap
		}
		if len(content) > cap {
			// Walk back to a rune boundary: a naive byte slice can cut a multi-byte rune
			// in half and hand the summarizer invalid UTF-8 (continuation bytes are
			// 0b10xxxxxx).
			cut := cap
			for cut > 0 && !utf8.RuneStart(content[cut]) {
				cut--
			}
			content = content[:cut] + " […]"
		}
		// Cap first, then redact: an over-cap secret tail is already truncated away and
		// the patterns scan a bounded input (the ledger's ordering, same reason).
		//
		// The transcript carries verbatim user text and tool output, so it can carry a
		// credential the model put on a command line. It leaves the process for an
		// external summarizer, and the summary it produces is about to become DURABLE
		// (slice 1b persists it), which turns a transient exposure into a stored one.
		rendered = append(rendered, m.Role+": "+redact.String(content))
	}
	if len(rendered) == 0 {
		return ""
	}
	elided := false
	total := 0
	kept := make([]string, 0, len(rendered))
	// Walk backward so the budget is spent on the MOST RECENT turns, then restore
	// chronological order — a summarizer reading the tail out of sequence would invent
	// a causality the conversation never had.
	for _, line := range slices.Backward(rendered) {
		if total+len(line) > summaryRenderCap && len(kept) > 0 {
			elided = true
			break
		}
		total += len(line)
		kept = append(kept, line)
	}
	slices.Reverse(kept)
	out := strings.Join(kept, "\n")
	if elided {
		out = "[earlier turns omitted]\n" + out
	}
	return out
}
