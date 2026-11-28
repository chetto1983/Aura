package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

// ErrStreamIncomplete refuses an interrupted answer even when text already arrived. It
// wraps io.ErrUnexpectedEOF because a cut stream is a transport failure the agent retries.
var ErrStreamIncomplete = fmt.Errorf("anthropic: stream ended before message_stop: %w", io.ErrUnexpectedEOF)

// consume forwards text and thinking deltas as they arrive and everything else once
// message_stop proves the response complete: the tool calls, the provider state, the
// finish reason, then the usage. The SDK's Accumulate assembles the message.
func (c *Client) consume(ctx context.Context, stream *messageStream, firedIdle func() bool, out chan<- llm.Chunk, key string, built builtRequest) {
	emit := func(chunk llm.Chunk) bool {
		select {
		case out <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}
	var message anthropic.BetaMessage
	fail := func(err error) {
		c.logFailure(built, &message, err)
		emit(llm.Chunk{Err: err})
	}
	for stream.Next() {
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			fail(fmt.Errorf("anthropic: %w", err))
			return
		}
		switch {
		case event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "":
			if !emit(llm.Chunk{Text: event.Delta.Text}) {
				return
			}
		case event.Type == "content_block_delta" && event.Delta.Type == "thinking_delta" && event.Delta.Thinking != "":
			if !emit(llm.Chunk{Reasoning: event.Delta.Thinking}) {
				return
			}
		case event.Type == "message_stop":
			if err := c.finish(&message, emit, built); err != nil {
				fail(err)
			}
			return
		}
	}
	fail(streamEndError(stream, firedIdle, key))
}

// finish emits a complete response. It returns the error that refuses one, and nil when the
// response went out or the caller stopped reading.
func (c *Client) finish(message *anthropic.BetaMessage, emit func(llm.Chunk) bool, built builtRequest) error {
	finish, err := finishReason(message.StopReason)
	if err != nil {
		return err
	}
	var calls []llm.ToolCall
	state := make([]anthropic.BetaContentBlockParamUnion, 0, len(message.Content))
	for _, block := range message.Content {
		state = append(state, block.ToParam())
		if block.Type != "tool_use" {
			continue
		}
		call := llm.ToolCall{ID: block.ID, Type: "function"}
		call.Function.Name, call.Function.Arguments = block.Name, string(objectInput(string(block.Input)))
		calls = append(calls, call)
	}
	var encoded json.RawMessage
	if len(calls) > 0 {
		if encoded, err = json.Marshal(state); err != nil {
			return fmt.Errorf("anthropic: encode provider state: %w", err)
		}
	}
	for i := range calls {
		if !emit(llm.Chunk{ToolCall: &calls[i]}) {
			return nil
		}
	}
	if encoded != nil && !emit(llm.Chunk{ProviderState: encoded}) {
		return nil
	}
	if !emit(llm.Chunk{FinishReason: finish}) {
		return nil
	}
	usage := message.Usage
	used := tokens{input: int(usage.InputTokens), read: int(usage.CacheReadInputTokens), write: int(usage.CacheCreationInputTokens), output: int(usage.OutputTokens)}
	cost := callCost(c.cfg.Prices, built.params.Model, used)
	c.logRequest(built, message, used, cost)
	emit(llm.Chunk{Usage: &llm.Usage{PromptTokens: used.prompt(), CompletionTokens: used.output, CachedTokens: used.read, Cost: cost}})
	return nil
}

// finishReason maps stop_reason onto Aura's finish reasons. pause_turn belongs to server
// tools, which Aura never sends, and compaction to server-side compaction, which it never
// enables: either would leave the turn without an answer to resume.
func finishReason(reason anthropic.BetaStopReason) (string, error) {
	switch reason {
	case anthropic.BetaStopReasonEndTurn, anthropic.BetaStopReasonStopSequence:
		return "stop", nil
	case anthropic.BetaStopReasonToolUse:
		return "tool_calls", nil
	case anthropic.BetaStopReasonMaxTokens, anthropic.BetaStopReasonModelContextWindowExceeded:
		return "length", nil
	case anthropic.BetaStopReasonRefusal:
		return llm.FinishContentFilter, nil
	default:
		return "", fmt.Errorf("anthropic: unsupported stop_reason %q", reason)
	}
}

// streamEndError names why a stream stopped before message_stop.
func streamEndError(stream *messageStream, firedIdle func() bool, key string) error {
	if firedIdle() {
		return openai_compat.ErrStreamIdleTimeout
	}
	if err := stream.Err(); err != nil {
		return providerError(err, key)
	}
	return ErrStreamIncomplete
}
