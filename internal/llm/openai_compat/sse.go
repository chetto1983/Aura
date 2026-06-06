package openai_compat

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// donePayload is the literal end-of-stream sentinel. It is NOT valid JSON and is
// recognized before the json.Unmarshal path (D-17) — unmarshaling it would error.
const donePayload = "[DONE]"

// wireChunk mirrors the OpenAI streaming-chunk envelope. Only consumed fields are
// declared; unknown fields are ignored by encoding/json. usage is present only on
// the final message (RESEARCH Pitfall 4). Reasoning is accept-both (#33): vLLM/DGX
// local emits `reasoning`, DeepSeek-V4 remote emits `reasoning_content`; either is
// emitted as a Chunk{Reasoning} the instant it decodes (amendment #57).
type wireChunk struct {
	Choices []struct {
		Delta struct {
			Content          string          `json:"content"`
			Reasoning        string          `json:"reasoning"`
			ReasoningContent string          `json:"reasoning_content"`
			ToolCalls        []toolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usageWire `json:"usage"`
}

// parseResult is what a fully consumed (or cleanly cancelled) stream yields: the
// captured usage (nil if the provider sent none) and the final finish_reason.
// Chunks are delivered incrementally via the emit callback during the parse.
type parseResult struct {
	usage        Usage
	hasUsage     bool
	finishReason string
}

// parseSSE drives the defensive SSE read loop over r. It:
//   - reads line-by-line with bufio.Reader (NEVER bufio.Scanner — Pitfall #1: a
//     >64KiB tool-arg line would trip the Scanner token cap);
//   - skips blank lines and `:` comment/keep-alive lines (`: OPENROUTER PROCESSING`)
//     before any json.Unmarshal (D-17 / T-03-06);
//   - returns cleanly on the `data: [DONE]` sentinel without unmarshaling it;
//   - emits each text delta as a Chunk{Text} immediately (Req#1, token-by-token);
//   - emits each reasoning delta as a Chunk{Reasoning} immediately, accept-both on
//     `reasoning` (vLLM/DGX) or `reasoning_content` (DeepSeek-V4) — stream-only, never
//     accumulated into content (amendment #57 / #33);
//   - accumulates tool_call deltas by index, emitting one Chunk{ToolCall} per call
//     when the stream finalizes (Req#2);
//   - captures the final usage object (Req#12 wire half);
//   - emits a trailing Chunk{FinishReason} carrying the terminal finish_reason.
//
// emit returning false means the consumer stopped draining; parseSSE returns
// immediately so the caller can tear down (the channel-based Stream relies on
// drain-to-close, but a stale emit must not block forever).
//
// An io.EOF (clean end) returns nil; a ctx-cancel surfaces here as the wrapped
// reader returning context.Canceled, which is returned to the caller.
func parseSSE(r io.Reader, emit func(llm.Chunk) bool) (parseResult, error) {
	br := bufio.NewReader(r)
	acc := newAccumulator()
	var res parseResult

	finalize := func() bool {
		for _, tc := range acc.finalize() {
			tc := tc
			if !emit(llm.Chunk{ToolCall: &tc}) {
				return false
			}
		}
		if res.finishReason != "" {
			return emit(llm.Chunk{FinishReason: res.finishReason})
		}
		return true
	}

	for {
		line, readErr := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			switch {
			case line == "" || strings.HasPrefix(line, ":"):
				// blank or SSE comment/keep-alive — skip before the data: check.
			case strings.HasPrefix(line, "data: "):
				payload := strings.TrimPrefix(line, "data: ")
				if payload == donePayload {
					if !finalize() {
						return res, nil
					}
					return res, nil
				}
				var wc wireChunk
				if jErr := json.Unmarshal([]byte(payload), &wc); jErr != nil {
					return res, fmt.Errorf("openai_compat: malformed SSE chunk: %w", jErr)
				}
				if !handleChunk(&wc, acc, &res, emit) {
					return res, nil
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				if !finalize() {
					return res, nil
				}
				return res, nil
			}
			return res, readErr
		}
	}
}

// handleChunk folds one decoded wireChunk into the running parse: it emits any
// text delta immediately, emits any reasoning delta immediately (accept-both #33),
// records the finish_reason, captures usage, and feeds tool-call deltas into the
// accumulator. It returns false if emit signalled the consumer stopped draining.
// Reasoning is emitted with the same immediacy as Content and NEVER enters the
// accumulator — it is stream-only (amendment #57).
func handleChunk(wc *wireChunk, acc *accumulator, res *parseResult, emit func(llm.Chunk) bool) bool {
	for i := range wc.Choices {
		c := &wc.Choices[i]
		if c.Delta.Content != "" {
			if !emit(llm.Chunk{Text: c.Delta.Content}) {
				return false
			}
		}
		if r := reasoningDelta(c.Delta.Reasoning, c.Delta.ReasoningContent); r != "" {
			if !emit(llm.Chunk{Reasoning: r}) {
				return false
			}
		}
		for _, d := range c.Delta.ToolCalls {
			acc.add(d)
		}
		if c.FinishReason != "" {
			res.finishReason = c.FinishReason
		}
	}
	if wc.Usage != nil {
		res.usage = wc.Usage.toUsage()
		res.hasUsage = true
	}
	return true
}

// reasoningDelta resolves the accept-both reasoning field (#33): vLLM/DGX local
// uses `reasoning`, DeepSeek-V4 remote uses `reasoning_content`. It prefers
// `reasoning` when both are non-empty (no real provider sends both).
func reasoningDelta(reasoning, reasoningContent string) string {
	if reasoning != "" {
		return reasoning
	}
	return reasoningContent
}
