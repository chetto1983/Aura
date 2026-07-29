package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
)

type completionLeakTool struct{}

func (completionLeakTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "fake_write",
		Summary:     "Fake mutating tool.",
		Description: "Fake mutating tool.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"v":{"type":"string"}}}`),
		Mutating:    true,
	}
}

func (completionLeakTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "wrote:"+string(raw))
}

type completionLeakClient struct {
	veto    string
	calls   int
	request []llm.Request
}

func (c *completionLeakClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	snap := req
	snap.Messages = append([]llm.Message(nil), req.Messages...)
	c.request = append(c.request, snap)
	c.calls++

	switch c.calls {
	case 1:
		return chunksFor(agentToolCallChunk("c1", "fake_write", `{"v":"x"}`), llm.Chunk{FinishReason: "tool_calls"}), nil
	case 2:
		return chunksFor(llm.Chunk{Text: c.veto}, llm.Chunk{FinishReason: "stop"}), nil
	case 3:
		return chunksFor(llm.Chunk{Text: "NOT_DONE: the output was never produced"}, llm.Chunk{FinishReason: "stop"}), nil
	default:
		if requestContains(req, c.veto) {
			return chunksFor(llm.Chunk{Text: "LEAK: " + c.veto}, llm.Chunk{FinishReason: "stop"}), nil
		}
		return chunksFor(llm.Chunk{Text: "clean finalize"}, llm.Chunk{FinishReason: "stop"}), nil
	}
}

func TestContentStopVetoDoesNotPersistIntoFinalize(t *testing.T) {
	const vetoed = "I wrote the script; you run it yourself"
	client := &completionLeakClient{veto: vetoed}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(completionLeakTool{})
	a := NewLlmAgent(LlmAgentConfig{
		Client:     client,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30, CompletionGate: true},
		Registry:   reg,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "make the file"}},
	})
	a.recoveryAttempts = 1
	maxSteps := 2
	budget, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}

	evs, err := collectInternal(a.Run(InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: budget}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	final := finalInternal(t, evs)
	if strings.Contains(final, vetoed) {
		t.Fatalf("finalize output leaked the vetoed answer: %q", final)
	}
	for _, msg := range a.history {
		if msg.Role == llm.RoleAssistant && strings.Contains(msg.Content, vetoed) {
			t.Fatalf("vetoed content-stop answer persisted as assistant history: %#v", a.history)
		}
	}
	if client.calls != 4 {
		t.Fatalf("client calls = %d, want mutating turn + content stop + critic + finalize", client.calls)
	}
	finalizeReq := client.request[3]
	if requestContains(finalizeReq, vetoed) {
		t.Fatalf("finalize request copied the vetoed answer forward: %#v", finalizeReq.Messages)
	}
	assertMessagesToolCallsAnswered(t, finalizeReq.Messages)
}

type retryClient struct {
	errs   []error
	chunks []llm.Chunk
	calls  int
}

func (r *retryClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	r.calls++
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		return nil, err
	}
	return chunksFor(r.chunks...), nil
}

func TestSynthesizeRetriesTransientOpen(t *testing.T) {
	client := &retryClient{
		errs:   []error{errors.New("wsarecv: connection reset by peer")},
		chunks: []llm.Chunk{{Text: "sintesi"}, {Usage: &llm.Usage{PromptTokens: 1, CompletionTokens: 2}}},
	}
	a := NewLlmAgent(LlmAgentConfig{
		Client:   client,
		LLM:      llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry: tools.NewRegistry(),
	})

	answer, usage, err := a.synthesize(InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7())})
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	if answer != "sintesi" {
		t.Fatalf("answer = %q, want retry success content", answer)
	}
	if usage.PromptTokens != 1 || usage.CompletionTokens != 2 {
		t.Fatalf("usage = %+v, want streamed usage", usage)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want two open attempts", client.calls)
	}
}

func TestSynthesizeRetryExhaustedReturnsFinalError(t *testing.T) {
	client := &retryClient{
		errs: []error{
			errors.New("wsarecv: reset one"),
			errors.New("wsarecv: reset two"),
		},
	}
	a := NewLlmAgent(LlmAgentConfig{
		Client:   client,
		LLM:      llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry: tools.NewRegistry(),
	})

	_, _, err := a.synthesize(InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7())})
	if err == nil || !strings.Contains(err.Error(), "reset two") {
		t.Fatalf("synthesize err = %v, want final retry error", err)
	}
	if client.calls != 2 {
		t.Fatalf("calls = %d, want retry budget spent", client.calls)
	}
}

func agentToolCallChunk(id, name, args string) llm.Chunk {
	tc := llm.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return llm.Chunk{ToolCall: &tc}
}

func chunksFor(chunks ...llm.Chunk) <-chan llm.Chunk {
	ch := make(chan llm.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

func requestContains(req llm.Request, needle string) bool {
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}

func collectInternal(seq func(yield func(*Event, error) bool)) ([]*Event, error) {
	var evs []*Event
	for ev, err := range seq {
		if err != nil {
			return evs, err
		}
		if ev != nil {
			evs = append(evs, ev)
		}
	}
	return evs, nil
}

func finalInternal(t *testing.T, evs []*Event) string {
	t.Helper()
	for _, v := range slices.Backward(evs) {
		if v.LLMResponse != nil && v.LLMResponse.FinishReason != "" {
			return v.LLMResponse.Content
		}
	}
	t.Fatal("no final event")
	return ""
}

func assertMessagesToolCallsAnswered(t *testing.T, hist []llm.Message) {
	t.Helper()
	for i, msg := range hist {
		if msg.Role != llm.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		pending := map[string]string{}
		for _, call := range msg.ToolCalls {
			pending[call.ID] = call.Function.Name
		}
		for j := i + 1; j < len(hist) && hist[j].Role == llm.RoleTool; j++ {
			delete(pending, hist[j].ToolCallID)
		}
		if len(pending) > 0 {
			t.Fatalf("assistant history[%d] has unanswered tool calls before the next non-tool message: pending=%v history=%#v",
				i, pending, hist)
		}
	}
}

func TestRetryableNetworkTextDocumentsPlanMarkers(t *testing.T) {
	for _, marker := range []string{"wsarecv", "connection reset", "unexpected EOF", "EOF"} {
		if !retryableStreamOpenError(fmt.Errorf("%s", marker)) {
			t.Fatalf("marker %q should remain retryable", marker)
		}
	}
	if retryableStreamOpenError(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded must not be retried")
	}
	if !retryableStreamOpenError(&openai_compat.HTTPError{StatusCode: 429, RetryAfterSec: 1}) {
		t.Fatal("HTTP 429 should be retryable")
	}
	if !retryableStreamOpenError(&openai_compat.HTTPError{StatusCode: 502}) {
		t.Fatal("HTTP 5xx should be retryable")
	}
	if retryableStreamOpenError(&openai_compat.HTTPError{StatusCode: 400}) {
		t.Fatal("HTTP 400 should not be retried")
	}
	if got := streamOpenRetryDelayFor(&openai_compat.HTTPError{StatusCode: 429, RetryAfterSec: 99}); got != streamOpenMaxRetryAfterWait {
		t.Fatalf("Retry-After delay should be bounded at %s, got %s", streamOpenMaxRetryAfterWait, got)
	}
}

// TestRetryableStreamOpenError_TypedSentinels proves the retry classifier uses
// errors.Is on typed network sentinels, not only substring matching (B-13). A
// sentinel wrapped in an error whose message carries NO substring marker is still
// retryable — the substring table alone would miss it (e.g. a platform that renders
// syscall.ECONNRESET as "forcibly closed", not "connection reset").
func TestRetryableStreamOpenError_TypedSentinels(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF},
		{"io.EOF", io.EOF},
		{"syscall.ECONNRESET", syscall.ECONNRESET},
		{"syscall.ECONNREFUSED", syscall.ECONNREFUSED},
		{"syscall.ETIMEDOUT", syscall.ETIMEDOUT},
	} {
		wrapped := opaqueWrapErr{msg: "upstream stream interrupted", inner: tc.err}
		if !retryableStreamOpenError(wrapped) {
			t.Errorf("%s wrapped with a marker-free message must be retryable via errors.Is", tc.name)
		}
	}
	if retryableStreamOpenError(errors.New("invalid api key configuration")) {
		t.Error("opaque non-network error must NOT be retried")
	}
}

// TestRetryableStreamOpenError_IdleTimeout proves B-08's classifier half: a stream
// idle-timeout stall is a RETRYABLE transport error, so the loop's mid-stream retry
// path fires once on it (mirroring ECONNRESET). It is matched via errors.Is, so a
// wrapped, marker-free message is still retryable.
func TestRetryableStreamOpenError_IdleTimeout(t *testing.T) {
	if !retryableStreamOpenError(openai_compat.ErrStreamIdleTimeout) {
		t.Fatal("ErrStreamIdleTimeout must be retryable (B-08)")
	}
	wrapped := opaqueWrapErr{msg: "upstream stream stalled", inner: openai_compat.ErrStreamIdleTimeout}
	if !retryableStreamOpenError(wrapped) {
		t.Fatal("a wrapped ErrStreamIdleTimeout must be retryable via errors.Is")
	}
}

// opaqueWrapErr wraps a sentinel via Unwrap while presenting a message that
// contains none of the substring markers — so only errors.Is can classify it.
type opaqueWrapErr struct {
	msg   string
	inner error
}

func (e opaqueWrapErr) Error() string { return e.msg }
func (e opaqueWrapErr) Unwrap() error { return e.inner }

func TestStreamOpenRetryDoesNotSleepPastContext(t *testing.T) {
	client := &retryClient{errs: []error{errors.New("wsarecv: slow reset")}}
	a := NewLlmAgent(LlmAgentConfig{Client: client, LLM: llm.Config{Model: "m", Provider: "p"}, Registry: tools.NewRegistry()})
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, err := a.streamWithOpenRetry(ctx, llm.Request{}, "test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("streamWithOpenRetry err = %v, want deadline exceeded", err)
	}
	if client.calls != 1 {
		t.Fatalf("calls = %d, want no second attempt after context deadline", client.calls)
	}
}
