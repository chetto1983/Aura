package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
)

// ErrStreamIncomplete refuses an interrupted answer even when text already arrived.
var ErrStreamIncomplete = errors.New("chatgpt: stream ended without response.completed")

// ResponseError preserves terminal provider codes for plan-limit recovery.
type ResponseError struct {
	Code    string
	Message string
}

func (e *ResponseError) Error() string { return fmt.Sprintf("chatgpt: %s: %s", e.Code, e.Message) }

func consumeStream(ctx context.Context, stream *ssestream.Stream[responses.ResponseStreamEventUnion], firedIdle func() bool, out chan<- llm.Chunk, token string) {
	emit := func(chunk llm.Chunk) bool {
		select {
		case out <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}
	calls := map[int64]responses.ResponseFunctionToolCall{}
	order := []int64{}
	remember := func(index int64, call responses.ResponseFunctionToolCall) {
		if _, ok := calls[index]; !ok {
			order = append(order, index)
		}
		calls[index] = call
	}
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta", "response.refusal.delta":
			if !emit(llm.Chunk{Text: event.Delta}) {
				return
			}
		case "response.reasoning_summary_text.delta":
			if !emit(llm.Chunk{Reasoning: event.Delta}) {
				return
			}
		case "response.output_item.added", "response.output_item.done":
			if event.Item.Type == "function_call" {
				remember(event.OutputIndex, event.Item.AsFunctionCall())
			}
		case "response.function_call_arguments.delta":
			call, ok := calls[event.OutputIndex]
			if !ok {
				emit(llm.Chunk{Err: errors.New("chatgpt: tool arguments arrived before the function call")})
				return
			}
			call.Arguments += event.Delta
			calls[event.OutputIndex] = call
		case "response.function_call_arguments.done":
			call, ok := calls[event.OutputIndex]
			if !ok {
				emit(llm.Chunk{Err: errors.New("chatgpt: tool arguments arrived before the function call")})
				return
			}
			call.Arguments = event.Arguments
			calls[event.OutputIndex] = call
		case "response.failed", "response.incomplete", "error":
			code, message := event.Code, event.Message
			if event.Type == "response.failed" {
				code, message = string(event.Response.Error.Code), event.Response.Error.Message
			}
			if event.Type == "response.incomplete" {
				code, message = "response.incomplete", event.Response.IncompleteDetails.Reason
			}
			emit(llm.Chunk{Err: safeResponseError(code, message, token)})
			return
		case "response.completed":
			for i, item := range event.Response.Output {
				if item.Type == "function_call" {
					remember(int64(i), item.AsFunctionCall())
				}
			}
			for _, index := range order {
				call := calls[index]
				if call.CallID == "" || call.Name == "" || (call.Namespace != "" && call.Namespace != toolNamespace) {
					emit(llm.Chunk{Err: errors.New("chatgpt: invalid tool call or namespace")})
					return
				}
				translated := llm.ToolCall{ID: call.CallID, Type: "function"}
				translated.Function.Name = strings.TrimPrefix(call.Name, toolNamespace+".")
				translated.Function.Arguments = call.Arguments
				if !emit(llm.Chunk{ToolCall: &translated}) {
					return
				}
			}
			finish := "stop"
			if len(order) > 0 {
				finish = "tool_calls"
			}
			if !emit(llm.Chunk{FinishReason: finish}) {
				return
			}
			if event.Response.JSON.Usage.Valid() {
				usage := event.Response.Usage
				emit(llm.Chunk{Usage: &llm.Usage{PromptTokens: int(usage.InputTokens), CompletionTokens: int(usage.OutputTokens), CachedTokens: int(usage.InputTokensDetails.CachedTokens)}})
			}
			return
		}
	}
	if firedIdle() {
		emit(llm.Chunk{Err: openai_compat.ErrStreamIdleTimeout})
		return
	}
	if err := stream.Err(); err != nil {
		emit(llm.Chunk{Err: sanitizeError(err, token)})
		return
	}
	emit(llm.Chunk{Err: ErrStreamIncomplete})
}

func safeResponseError(code, message, token string) *ResponseError {
	if code == "" {
		code = "response.failed"
	}
	message = strings.Join(strings.Fields(strings.ReplaceAll(message, token, "[redacted]")), " ")
	if len(message) > 512 {
		message = message[:512]
	}
	return &ResponseError{Code: strings.ReplaceAll(code, token, "[redacted]"), Message: message}
}

func sanitizeError(err error, token string) error {
	if httpErr, ok := errors.AsType[*openai_compat.HTTPError](err); ok {
		copy := *httpErr
		copy.Body = strings.ReplaceAll(copy.Body, token, "[redacted]")
		copy.RequestID = strings.ReplaceAll(copy.RequestID, token, "[redacted]")
		return &copy
	}
	if apiErr, ok := errors.AsType[*openai.Error](err); ok {
		body := strings.ReplaceAll(apiErr.RawJSON(), token, "[redacted]")
		if len(body) > 64<<10 {
			body = body[:64<<10]
		}
		result := &openai_compat.HTTPError{StatusCode: apiErr.StatusCode, Body: body}
		if apiErr.Response != nil {
			id := strings.ReplaceAll(apiErr.Response.Header.Get("x-request-id"), token, "[redacted]")
			id = strings.Join(strings.Fields(id), " ")
			if len(id) > 128 {
				id = id[:128]
			}
			result.RequestID = id
			if apiErr.StatusCode == http.StatusTooManyRequests {
				if seconds, parseErr := strconv.Atoi(strings.TrimSpace(apiErr.Response.Header.Get("Retry-After"))); parseErr == nil {
					result.RetryAfterSec = max(seconds, 0)
				}
			}
		}
		return result
	}
	if token != "" && strings.Contains(err.Error(), token) {
		return errors.New(strings.ReplaceAll(err.Error(), token, "[redacted]"))
	}
	return err
}
