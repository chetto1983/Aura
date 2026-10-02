package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	openai "github.com/openai/openai-go/v3"
)

func basicRequest() llm.Request {
	return llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}
}

func TestSDKHTTPErrorMetadata(t *testing.T) {
	const token = "oauth-test-token"
	for _, retryAfter := range []string{"7", "invalid", "-1"} {
		t.Run(retryAfter, func(t *testing.T) {
			apiErr := &openai.Error{StatusCode: http.StatusTooManyRequests, Response: &http.Response{Header: http.Header{
				"X-Request-Id": {"  request-" + token + " " + strings.Repeat("x", 180)},
				"Retry-After":  {retryAfter},
			}}}
			if err := apiErr.UnmarshalJSON([]byte(`{"error":{"message":"` + token + strings.Repeat("x", 70<<10) + `"}}`)); err != nil {
				t.Fatal(err)
			}
			result, ok := errors.AsType[*openai_compat.HTTPError](sanitizeError(fmt.Errorf("wrapped: %w", apiErr), token))
			if !ok || result.StatusCode != 429 || len(result.RequestID) != 128 || len(result.Body) > 64<<10 || strings.Contains(result.RequestID, token) || strings.Contains(result.Body, token) {
				t.Fatalf("SDK error metadata: %#v", result)
			}
			wantRetry := 0
			if retryAfter == "7" {
				wantRetry = 7
			}
			if result.RetryAfterSec != wantRetry {
				t.Fatalf("retry seconds: %d", result.RetryAfterSec)
			}
		})
	}
	wrapped := &openai_compat.HTTPError{StatusCode: 401, RequestID: "request-" + token, Body: token}
	result, ok := errors.AsType[*openai_compat.HTTPError](sanitizeError(wrapped, token))
	if !ok || strings.Contains(result.Error(), token) || strings.Contains(result.RequestID, token) || wrapped.RequestID != "request-"+token {
		t.Fatal("shared HTTP error redaction lost or mutated original")
	}
}

func TestArgumentDeltasAreUsedWithoutDone(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		event(w, "response.output_item.added", `{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call1","name":"aura.lookup","arguments":""}}`)
		event(w, "response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{"}`)
		event(w, "response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","output_index":2,"delta":"}"}`)
		event(w, "response.completed", completed)
	})
	chunks := drain(t, client, basicRequest())
	if len(chunks) < 2 || chunks[0].ToolCall == nil || chunks[0].ToolCall.Function.Name != "lookup" || chunks[0].ToolCall.Function.Arguments != "{}" {
		t.Fatalf("argument deltas: %#v", chunks)
	}
}

func TestStreamErrorsRedactCredentials(t *testing.T) {
	token := "oauth-test-token"
	for _, name := range []string{"response.failed", "error"} {
		t.Run(name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if name == "response.failed" {
					event(w, name, `{"type":"response.failed","response":{"error":{"message":"oauth-test-token"}}}`)
				} else {
					event(w, name, `{"error":{"message":"oauth-test-token"}}`)
				}
			})
			chunks := drain(t, client, basicRequest())
			if err := chunks[len(chunks)-1].Err; err == nil || strings.Contains(err.Error(), token) {
				t.Fatalf("credential leaked: %v", err)
			}
		})
	}
	err := safeResponseError("", strings.Repeat("long ", 200)+token, token)
	if err.Code != "response.failed" || len(err.Message) > 512 || strings.Contains(err.Error(), token) {
		t.Fatal(err)
	}
	if got := sanitizeError(fmt.Errorf("request denied: %s", token), token); strings.Contains(got.Error(), token) {
		t.Fatal(got)
	}
	if got := sanitizeError(context.Canceled, token); !errors.Is(got, context.Canceled) {
		t.Fatal("cancellation identity lost")
	}
}
