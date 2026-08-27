package openai_compat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

func consumeSDKStream(ctx context.Context, stream *ssestream.Stream[openai.ChatCompletionChunk], idle *idleRequestControl, out chan<- llm.Chunk) {
	emit := func(chunk llm.Chunk) bool {
		select {
		case out <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}
	var accumulator openai.ChatCompletionAccumulator
	var usage *llm.Usage
	finishReason := ""
	for stream.Next() {
		chunk := stream.Current()
		if !accumulator.AddChunk(chunk) {
			emit(llm.Chunk{Err: errors.New("openai_compat: rejected invalid stream chunk")})
			return
		}
		for choiceIndex := range chunk.Choices {
			choice := &chunk.Choices[choiceIndex]
			if choice.Delta.Content != "" && !emit(llm.Chunk{Text: choice.Delta.Content}) {
				return
			}
			if reasoning := sdkReasoningDelta(choice.Delta); reasoning != "" && !emit(llm.Chunk{Reasoning: reasoning}) {
				return
			}
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
		if chunk.JSON.Usage.Valid() {
			value := sdkUsage(chunk.Usage)
			usage = &value
		}
	}
	if idle.firedIdle() {
		emit(llm.Chunk{Err: ErrStreamIdleTimeout})
		return
	}
	if err := stream.Err(); err != nil {
		emit(llm.Chunk{Err: adaptStreamError(err)})
		return
	}
	if len(accumulator.Choices) > 0 {
		for _, call := range accumulator.Choices[0].Message.ToolCalls {
			var translated llm.ToolCall
			translated.ID = call.ID
			translated.Type = call.Type
			translated.Function.Name = call.Function.Name
			translated.Function.Arguments = call.Function.Arguments
			if translated.Type == "" {
				translated.Type = "function"
			}
			if !emit(llm.Chunk{ToolCall: &translated}) {
				return
			}
		}
	}
	if finishReason != "" {
		if !emit(llm.Chunk{FinishReason: finishReason}) {
			return
		}
	}
	if usage != nil {
		if !emit(llm.Chunk{Usage: usage}) {
			return
		}
	}
	reasoningtrace.Record("openai_compat_stream_done", map[string]any{
		"finish_reason": finishReason,
		"has_usage":     usage != nil,
	})
	if finishReason == "" {
		emit(llm.Chunk{Err: errStreamMissingFinishReason})
	}
}

func sdkReasoningDelta(delta openai.ChatCompletionChunkChoiceDelta) string {
	for _, key := range []string{"reasoning", "reasoning_content"} {
		field, ok := delta.JSON.ExtraFields[key]
		if !ok || field.Raw() == "" || field.Raw() == "null" {
			continue
		}
		var value string
		if json.Unmarshal([]byte(field.Raw()), &value) == nil && value != "" {
			return value
		}
	}
	return ""
}

func sdkUsage(usage openai.CompletionUsage) llm.Usage {
	result := llm.Usage{
		PromptTokens:     int(usage.PromptTokens),
		CompletionTokens: int(usage.CompletionTokens),
		CachedTokens:     int(usage.PromptTokensDetails.CachedTokens),
	}
	if field, ok := usage.JSON.ExtraFields["cost"]; ok && field.Raw() != "" && field.Raw() != "null" {
		var cost float64
		if json.Unmarshal([]byte(field.Raw()), &cost) == nil {
			result.Cost = &cost
		}
	}
	return result
}

func adaptStreamError(err error) error {
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typeErr) {
		return fmt.Errorf("openai_compat: malformed SSE chunk: %w", err)
	}
	return err
}
