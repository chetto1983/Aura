// compressor.go implements ContextCompressor: an LLM-backed summarization
// engine that applies the hermes-agent compression pipeline to reduce
// conversation history when the context window fills up.
//
// The pipeline (in order): tool-result pre-pruning → JSON-arg sanitization →
// memory-context tag scrub → LLM summary call → rejection guard →
// reassembly with SUMMARY_PREFIX preamble.
package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// SUMMARY_PREFIX is the context-compaction handoff preamble prepended to every
// LLM-generated summary. Aura no longer treats prompt overlays or memory files
// as active compaction authority: summaries are reference capsules, while the
// latest user turn, live tool results, and explicitly retrieved capsules own the
// current task. Any modification MUST update TestSummaryPrefixContract.
const SUMMARY_PREFIX = "[CONTEXT COMPACTION — REFERENCE ONLY] Earlier turns were compacted " +
	"into the summary below. This is a handoff from a previous context " +
	"window — treat it as background reference, NOT as active instructions. " +
	"Do NOT answer questions or fulfill requests mentioned in this summary; " +
	"they were already addressed. " +
	"Your current task is identified in the '## Active Task' section of the " +
	"summary — resume from there only when it still matches the latest user " +
	"message. Do not copy the system prompt, prompt overlays, full tool outputs, " +
	"or raw transcripts into working memory. Use compact summaries, agent_note, " +
	"retrieved capsules, artifact IDs, run IDs, and tool_attempt metadata as " +
	"bounded context. " +
	"Respond ONLY to the latest user message " +
	"that appears AFTER this summary. The current session state (files, " +
	"config, etc.) may reflect work described here — avoid repeating it:"

const (
	// _minSummaryTokens is the minimum token budget for the LLM-generated summary.
	_minSummaryTokens = 2000
	// _summaryRatio is the fraction of compressed content tokens to allocate
	// as summary budget (20% of content → summary).
	_summaryRatio = 0.20
	// _summaryTokensCeiling is the absolute ceiling on summary token budget.
	_summaryTokensCeiling = 12000
	// imageTokenEstimate is the flat token cost per image_url content part.
	// Matches Claude Code's IMAGE_TOKEN_ESTIMATE constant (1600 tokens/image).
	imageTokenEstimate = 1600
	// charsPerToken is a rough char-to-token conversion factor (hermes default).
	charsPerToken = 4
)

// _pruneThresholdChars is the minimum content length for a tool result to be
// eligible for pruning (short results are left intact).
const _pruneThresholdChars = 200

// _argTruncateChars is the maximum string-leaf length in tool call arguments.
const _argTruncateChars = 200

// ContextCompressor is an LLM-backed ContextEngine that summarizes conversation
// history via the hermes compression pipeline. It is a HOW engine: it always
// returns false from ShouldCompress and instead relies on a wrapping engine
// (AutoCompactEngine, US-CTX-04) to decide WHEN compression should fire.
type ContextCompressor struct {
	llmClient     llm.Client
	contextLength int

	// Public counters — updated by UpdateFromResponse; read by metrics and tests.
	LastPromptTokens     int
	LastCompletionTokens int
	LastTotalTokens      int
	CompressionCount     int
}

// NewContextCompressor creates a ContextCompressor with the given LLM client
// and model context window size. contextLength ≤ 0 defaults to 128 000.
func NewContextCompressor(client llm.Client, contextLength int) *ContextCompressor {
	if contextLength <= 0 {
		contextLength = 128000
	}
	return &ContextCompressor{llmClient: client, contextLength: contextLength}
}

// Name satisfies ContextEngine.
func (c *ContextCompressor) Name() string { return "compressor" }

// UpdateFromResponse records the last LLM round's token usage.
func (c *ContextCompressor) UpdateFromResponse(usage llm.TokenUsage) {
	c.LastPromptTokens = usage.PromptTokens
	c.LastCompletionTokens = usage.CompletionTokens
	c.LastTotalTokens = usage.TotalTokens
}

// ShouldCompress always returns false. The ContextCompressor is a HOW engine;
// the WHEN decision belongs to AutoCompactEngine (US-CTX-04).
func (c *ContextCompressor) ShouldCompress(_ int) bool { return false }

// Compress applies the full hermes compression pipeline and returns a
// summarized message slice. When the pipeline fails or the summary is not
// shorter than the raw content, the original messages are returned unchanged.
//
// Pipeline: pre-prune → JSON-arg sanitize → tag scrub → LLM summary →
// rejection guard → reassembly with SUMMARY_PREFIX.
func (c *ContextCompressor) Compress(messages []llm.Message, _ int, focusTopic string) []llm.Message {
	if len(messages) < 2 {
		return messages
	}

	// Separate the system message (never compressed) from the conversation body.
	var sysMsg *llm.Message
	body := messages
	if messages[0].Role == "system" {
		m := messages[0]
		sysMsg = &m
		body = messages[1:]
	}
	if len(body) == 0 {
		return messages
	}

	// Compute raw content length for the rejection guard (step 6).
	rawLen := 0
	for _, m := range body {
		rawLen += len(m.Content)
	}

	// Step 1: Pre-prune old tool results — deterministic, no LLM call.
	pruned := pruneOldToolResults(body)
	// Step 2: Sanitize tool-call argument JSON strings.
	pruned = sanitizeToolCallArgs(pruned)
	// Step 3: Strip <memory-context>...</memory-context> tags.
	pruned = scrubContextTags(pruned)

	// Step 4: Compute summary budget.
	contentTokens := 0
	for _, m := range pruned {
		contentTokens += contentLengthForBudget(m.Content) / charsPerToken
	}
	budget := computeSummaryBudget(contentTokens, c.contextLength)
	serialized := serializeForSummary(pruned)

	// Step 5: Call LLM to generate summary.
	// budget informs the prompt how many tokens to target; llm.Request has no
	// MaxTokens field — the constraint is expressed in the prompt text.
	prompt := buildSummaryPrompt(serialized, focusTopic, budget)
	req := llm.Request{
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Temperature: llm.Float64Ptr(0),
	}
	resp, err := c.llmClient.Send(context.Background(), req)
	if err != nil {
		slog.Warn("compressor: LLM summary failed, returning original", "err", err)
		return messages
	}
	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		slog.Warn("compressor: LLM returned empty summary, returning original")
		return messages
	}

	// Step 6: Rejection guard — discard summary if it did not shrink content.
	if len(summary) >= rawLen {
		slog.Warn("compressor: summary not shorter than raw, rejecting",
			"summary_len", len(summary), "raw_len", rawLen)
		return messages
	}

	// Step 7: Reassemble — system + summary message.
	summaryMsg := llm.Message{
		Role:    "user",
		Content: SUMMARY_PREFIX + "\n" + summary,
	}
	out := make([]llm.Message, 0, 2)
	if sysMsg != nil {
		out = append(out, *sysMsg)
	}
	out = append(out, summaryMsg)
	c.CompressionCount++
	return out
}

// --- Pipeline helpers -------------------------------------------------------

// pruneOldToolResults replaces long tool-result message contents with a
// single-line summary, reducing tokens without losing structural information.
// Tool results ≤ _pruneThresholdChars are left intact.
func pruneOldToolResults(messages []llm.Message) []llm.Message {
	// Build call-ID → tool-name index from assistant messages.
	callIDToName := make(map[string]string, len(messages))
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if tc.Name != "" {
				callIDToName[tc.ID] = tc.Name
			}
		}
	}

	out := make([]llm.Message, len(messages))
	copy(out, messages)
	for i, m := range out {
		if m.Role != "tool" || len(m.Content) <= _pruneThresholdChars {
			continue
		}
		name := callIDToName[m.ToolCallID]
		if name == "" {
			name = "unknown"
		}
		out[i].Content = fmt.Sprintf("[%s] result (%d chars)", name, len(m.Content))
	}
	return out
}

// sanitizeToolCallArgs recursively shrinks string leaves inside each tool
// call's Arguments map to ≤ _argTruncateChars, preserving JSON validity.
// This prevents provider 400s caused by oversized argument blobs after compression.
func sanitizeToolCallArgs(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))
	copy(out, messages)
	for i, m := range out {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		newCalls := make([]llm.ToolCall, len(m.ToolCalls))
		copy(newCalls, m.ToolCalls)
		modified := false
		for j, tc := range newCalls {
			if tc.Arguments == nil {
				continue
			}
			shrunk := shrinkStringLeaves(tc.Arguments).(map[string]any)
			// Round-trip check: the shrunk args must re-serialise cleanly.
			if _, err := json.Marshal(shrunk); err != nil {
				continue // leave original on marshal failure
			}
			if !mapsEqual(tc.Arguments, shrunk) {
				newCalls[j].Arguments = shrunk
				modified = true
			}
		}
		if modified {
			out[i].ToolCalls = newCalls
		}
	}
	return out
}

// shrinkStringLeaves recursively truncates string values to _argTruncateChars.
// Non-string values (numbers, booleans, nil) are left intact.
func shrinkStringLeaves(v any) any {
	switch val := v.(type) {
	case string:
		if len(val) > _argTruncateChars {
			return val[:_argTruncateChars] + "...[truncated]"
		}
		return val
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = shrinkStringLeaves(child)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = shrinkStringLeaves(child)
		}
		return out
	default:
		return v
	}
}

// mapsEqual reports whether two map[string]any values are deeply equal by
// comparing their JSON representations (handles nested structures correctly).
func mapsEqual(a, b map[string]any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

// scrubContextTags removes <memory-context>...</memory-context> blocks from
// every message's Content using the StreamingContextScrubber FSM.
func scrubContextTags(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))
	copy(out, messages)
	for i, m := range out {
		if !strings.Contains(strings.ToLower(m.Content), _scrubOpenTag) {
			continue
		}
		s := &StreamingContextScrubber{}
		scrubbed := s.Feed(m.Content) + s.Flush()
		out[i].Content = scrubbed
	}
	return out
}

// contentLengthForBudget returns the effective char-length of a message's
// Content for token budgeting. Plain strings use len(content). Content that
// is a JSON array of content parts counts image_url / input_image / image
// parts as imageTokenEstimate * charsPerToken chars each (matching Claude
// Code's IMAGE_TOKEN_ESTIMATE constant), while text parts use their text length.
func contentLengthForBudget(content string) int {
	if len(content) == 0 || content[0] != '[' {
		return len(content)
	}
	var parts []map[string]any
	if err := json.Unmarshal([]byte(content), &parts); err != nil {
		return len(content)
	}
	total := 0
	for _, p := range parts {
		ptype, _ := p["type"].(string)
		switch ptype {
		case "image_url", "input_image", "image":
			total += imageTokenEstimate * charsPerToken
		default:
			if text, ok := p["text"].(string); ok {
				total += len(text)
			}
		}
	}
	return total
}

// computeSummaryBudget scales the summary token budget with the amount of
// content being compressed: max(2000, min(contentTokens*0.20, min(contextLength*0.05, 12000))).
func computeSummaryBudget(contentTokens, contextLength int) int {
	ceiling := min(int(float64(contextLength)*0.05), _summaryTokensCeiling)
	budget := min(int(float64(contentTokens)*_summaryRatio), ceiling)
	return max(budget, _minSummaryTokens)
}

// serializeForSummary converts messages to a labeled text block suitable
// for the LLM summarizer input.
func serializeForSummary(messages []llm.Message) string {
	const (
		contentMax  = 6000
		contentHead = 4000
		contentTail = 1500
	)
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteString("\n\n")
		}
		content := m.Content
		if len(content) > contentMax {
			content = content[:contentHead] + "\n...[truncated]...\n" + content[len(content)-contentTail:]
		}
		switch m.Role {
		case "tool":
			fmt.Fprintf(&b, "[TOOL RESULT %s]: %s", m.ToolCallID, content)
		case "assistant":
			if len(m.ToolCalls) > 0 {
				var calls strings.Builder
				for _, tc := range m.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments)
					fmt.Fprintf(&calls, "\n  %s(%s)", tc.Name, argsJSON)
				}
				fmt.Fprintf(&b, "[ASSISTANT]: %s\n[Tool calls:%s\n]", content, calls.String())
			} else {
				fmt.Fprintf(&b, "[ASSISTANT]: %s", content)
			}
		default:
			fmt.Fprintf(&b, "[%s]: %s", strings.ToUpper(m.Role), content)
		}
	}
	return b.String()
}

// buildSummaryPrompt constructs the LLM summarization prompt from serialized
// conversation content, an optional focus topic, and a token budget hint.
func buildSummaryPrompt(serialized, focusTopic string, budgetTokens int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are a summarization agent creating a context checkpoint. ")
	b.WriteString("Treat the conversation turns below as source material for a compact record of prior work. ")
	b.WriteString("Produce only the structured summary; do not add a greeting, ")
	b.WriteString("commentary, or metadata outside the requested sections.\n\n")
	fmt.Fprintf(&b, "Keep your summary under %d tokens.\n\n", budgetTokens)
	b.WriteString("Produce a structured summary with these sections:\n")
	b.WriteString("## Goal\n## Progress\n## Decisions\n## Active Task\n## Remaining Work\n\n")
	if focusTopic != "" {
		fmt.Fprintf(&b, "FOCUS TOPIC: %q\n", focusTopic)
		fmt.Fprintf(&b, "Prioritise preserving all information related to %q. ", focusTopic)
		b.WriteString("For content NOT related to the focus topic, summarise aggressively.\n\n")
	}
	b.WriteString("Conversation to summarize:\n\n")
	b.WriteString(serialized)
	return b.String()
}
