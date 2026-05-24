package governance

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/aura/aura/internal/ctxmetrics"
	"github.com/aura/aura/internal/llm"
)

const (
	// DefaultPayloadThresholdTokens is the token estimate above which the
	// summarizer is triggered. Sits above typical single tool results but below
	// a wiki page full body. Configurable via AURA_PAYLOAD_THRESHOLD_TOKENS.
	DefaultPayloadThresholdTokens = 16384

	// DefaultPayloadMaxTokens is the upper bound above which the summarizer is
	// skipped: the payload is too large to summarize cheaply; spill/truncation
	// handles it instead. Configurable via AURA_PAYLOAD_MAX_TOKENS.
	DefaultPayloadMaxTokens = 65536

	// payloadBreakerMaxFails is the consecutive-failure count that trips the
	// circuit breaker for the rest of the session.
	payloadBreakerMaxFails = 3

	// payloadCharsPerToken is the rough chars-to-tokens denominator used to
	// estimate token count from byte length (model-agnostic, matches hermes).
	payloadCharsPerToken = 4
)

// SummarizedPayload is the result of a successful tool-result summarization.
type SummarizedPayload struct {
	Summary       string
	OriginalBytes int
	SummaryBytes  int
}

// PayloadSummarizer intercepts oversized tool results BEFORE they enter agent
// history. MaybeSummarize returns nil for pass-through (no LLM call is made in
// that case). Nil implementations are safe: callers should check for nil before
// calling — by convention a nil PayloadSummarizer means "disabled for this agent
// (e.g. the summarizer sub-agent itself, recursive dispatch prevention)".
type PayloadSummarizer interface {
	MaybeSummarize(ctx context.Context, toolName, parentTaskHint, raw string) *SummarizedPayload
}

// SubagentPayloadSummarizer is the production implementation of PayloadSummarizer.
// It calls the LLM directly (no tool calls) to produce a compact extraction
// summary when tool output exceeds ThresholdTokens.
//
// Circuit breaker: 3 consecutive failures disable the summarizer for the rest of
// the session (disabled flag never resets). Consecutive success resets the failure
// counter, so a transient failure doesn't permanently degrade service.
type SubagentPayloadSummarizer struct {
	client       llm.Client
	model        string
	systemPrompt string
	// ThresholdTokens is the minimum estimated token count to trigger the summarizer.
	ThresholdTokens int
	// MaxTokens is the upper bound above which the summarizer is skipped.
	MaxTokens int

	mu        sync.Mutex
	failCount uint8
	disabled  bool
}

// NewSubagentPayloadSummarizer constructs a ready-to-use SubagentPayloadSummarizer.
// systemPrompt is the extraction-contract prompt (SKILL.md content in production).
// thresholdTokens and maxTokens default to their package-level defaults when ≤ 0.
func NewSubagentPayloadSummarizer(client llm.Client, model, systemPrompt string, thresholdTokens, maxTokens int) *SubagentPayloadSummarizer {
	if thresholdTokens <= 0 {
		thresholdTokens = DefaultPayloadThresholdTokens
	}
	if maxTokens <= 0 {
		maxTokens = DefaultPayloadMaxTokens
	}
	return &SubagentPayloadSummarizer{
		client:          client,
		model:           model,
		systemPrompt:    systemPrompt,
		ThresholdTokens: thresholdTokens,
		MaxTokens:       maxTokens,
	}
}

// MaybeSummarize applies the pass-through guards in order and, on success,
// returns a *SummarizedPayload. Returns nil when any guard fires.
//
// Guard order (matches openhuman harness/payload_summarizer.rs:55-310):
//  1. tokens < threshold_tokens  → skip (no LLM call)
//  2. tokens > max_payload_tokens → skip (too expensive for summarizer)
//  3. circuit breaker tripped    → skip
//  4. LLM dispatch
//  5. empty summary              → reject + record_failure
//  6. summary.len() >= raw.len() → reject + record_failure
func (s *SubagentPayloadSummarizer) MaybeSummarize(ctx context.Context, toolName, parentTaskHint, raw string) *SummarizedPayload {
	tokenEst := len(raw) / payloadCharsPerToken

	// Guard 1: below threshold — no LLM call needed.
	if tokenEst < s.ThresholdTokens {
		return nil
	}

	// Guard 2: above max — too expensive; spill/truncation handles it.
	if tokenEst > s.MaxTokens {
		return nil
	}

	// Guard 3: circuit breaker tripped.
	s.mu.Lock()
	if s.disabled {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	// Guard 4: dispatch to LLM.
	summary, err := s.callLLM(ctx, toolName, parentTaskHint, raw)
	if err != nil {
		s.recordFailure(toolName, err)
		return nil
	}

	// Guard 5: empty summary.
	if strings.TrimSpace(summary) == "" {
		s.recordFailure(toolName, nil)
		slog.Warn("payload_summarizer: LLM returned empty summary", "tool", toolName)
		return nil
	}

	// Guard 6: summary must shrink the payload.
	if len(summary) >= len(raw) {
		s.recordFailure(toolName, nil)
		slog.Warn("payload_summarizer: summary not shorter than raw, rejecting",
			"tool", toolName, "summary_bytes", len(summary), "raw_bytes", len(raw))
		return nil
	}

	s.recordSuccess()
	return &SummarizedPayload{
		Summary:       summary,
		OriginalBytes: len(raw),
		SummaryBytes:  len(summary),
	}
}

// recordFailure increments the failure counter and trips the breaker at 3 consecutive failures.
func (s *SubagentPayloadSummarizer) recordFailure(toolName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCount++
	if err != nil {
		slog.Warn("payload_summarizer: dispatch failed", "tool", toolName, "err", err)
	}
	if s.failCount >= payloadBreakerMaxFails {
		s.disabled = true
		ctxmetrics.Global.PayloadBreakerTripsTotal.Add(1)
		slog.Warn("payload_summarizer: circuit breaker tripped",
			"consecutive_failures", s.failCount)
	}
}

// recordSuccess resets the consecutive-failure counter on a successful summarization.
// Does NOT untrip the circuit breaker once disabled.
func (s *SubagentPayloadSummarizer) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCount = 0
	ctxmetrics.Global.PayloadSummarizationsTotal.Add(1)
}

// IsDisabled reports whether the circuit breaker is tripped.
func (s *SubagentPayloadSummarizer) IsDisabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disabled
}

// callLLM performs the single-shot LLM summarization call.
func (s *SubagentPayloadSummarizer) callLLM(ctx context.Context, toolName, parentTaskHint, raw string) (string, error) {
	msgs := make([]llm.Message, 0, 2)
	if s.systemPrompt != "" {
		msgs = append(msgs, llm.Message{Role: "system", Content: s.systemPrompt})
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: buildPayloadPrompt(toolName, parentTaskHint, raw)})
	req := llm.Request{
		Messages:    msgs,
		Model:       s.model,
		Temperature: llm.Float64Ptr(0),
	}
	resp, err := s.client.Send(ctx, req)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// buildPayloadPrompt constructs the user turn for payload summarization.
func buildPayloadPrompt(toolName, parentTaskHint, raw string) string {
	var b strings.Builder
	b.WriteString("Extract the essential information from the following tool output.\n\n")
	if toolName != "" {
		b.WriteString("Tool: ")
		b.WriteString(toolName)
		b.WriteString("\n")
	}
	if parentTaskHint != "" {
		b.WriteString("Context: ")
		b.WriteString(parentTaskHint)
		b.WriteString("\n")
	}
	b.WriteString("\nTool output:\n")
	b.WriteString(raw)
	return b.String()
}
