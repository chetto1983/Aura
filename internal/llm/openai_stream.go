package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// readSSEStream reads Server-Sent Events from the response body.
//
// Tool-call streaming protocol (OpenAI-compat): the model emits a series
// of delta chunks where the first chunk for each tool call slot carries
// id/type/function.name and possibly a leading function.arguments
// fragment, and subsequent chunks for the same `index` carry only
// further function.arguments fragments. We accumulate per-index state
// here so the caller never has to reassemble partial argument JSON.
// On terminal chunk (FinishReason set or [DONE]) we materialize the
// accumulated state as fully-parsed []ToolCall and emit it on the final
// Done=true token.
func (c *OpenAIClient) readSSEStream(body io.ReadCloser, ch chan<- Token) {
	defer close(ch)
	defer body.Close()

	type accum struct {
		id      string
		name    string
		argsBuf strings.Builder
	}
	toolBuf := map[int]*accum{}
	var indices []int // preserve insertion order so emitted ToolCalls are stable
	var usage TokenUsage

	finish := func() {
		if len(toolBuf) == 0 {
			ch <- Token{Done: true, Usage: usage}
			return
		}
		calls := make([]ToolCall, 0, len(toolBuf))
		for _, idx := range indices {
			a := toolBuf[idx]
			args, err := parseToolCallArguments(a.name, a.argsBuf.String())
			if err != nil {
				ch <- Token{Err: err, Done: true}
				return
			}
			calls = append(calls, ToolCall{ID: a.id, Name: a.name, Arguments: args})
		}
		ch <- Token{ToolCalls: calls, Usage: usage, Done: true}
	}

	scanner := bufio.NewScanner(body)
	// Default scanner buffer (64KB) is too small for chunked streams that
	// occasionally land on a single oversized line.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			finish()
			return
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		// End-of-stream usage chunk: empty Choices, populated Usage.
		if chunk.Usage != nil {
			usage = TokenUsage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
			if chunk.Usage.CompletionTokensDetail != nil {
				usage.ReasoningTokens = chunk.Usage.CompletionTokensDetail.ReasoningTokens
			}
			if chunk.Usage.PromptTokensDetails != nil {
				usage.CacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			if chunk.Usage.CacheReadInputTokens > 0 {
				usage.CacheReadTokens = chunk.Usage.CacheReadInputTokens
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		// Surface reasoning deltas as they arrive so UIs can show the
		// model's chain-of-thought live instead of stalling on a placeholder
		// while the provider thinks. Empty everywhere except reasoning-
		// capable providers (DeepSeek V4 Flash, OpenAI o-series / gpt-5*).
		if choice.Delta.Reasoning != "" {
			ch <- Token{Reasoning: choice.Delta.Reasoning}
		}
		if choice.Delta.Content != "" {
			ch <- Token{Content: choice.Delta.Content}
		}
		for _, tc := range choice.Delta.ToolCalls {
			a, ok := toolBuf[tc.Index]
			if !ok {
				a = &accum{}
				toolBuf[tc.Index] = a
				indices = append(indices, tc.Index)
			}
			if tc.ID != "" {
				a.id = tc.ID
			}
			if tc.Function.Name != "" {
				a.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				a.argsBuf.WriteString(tc.Function.Arguments)
			}
		}
		// Do not finish on finish_reason alone. OpenAI-compatible streams can
		// send the usage summary in a later empty-choices chunk, followed by
		// [DONE]. Returning here would drop token/cost accounting for providers
		// that honor stream_options.include_usage.
	}

	if err := scanner.Err(); err != nil {
		ch <- Token{Err: fmt.Errorf("stream read: %w", err), Done: true}
		return
	}
	finish()
}
