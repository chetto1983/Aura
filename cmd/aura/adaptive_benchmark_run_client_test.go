package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

func TestAdaptiveBenchmarkObservedClientDelegatesWithRequestObserver(
	t *testing.T,
) {
	t.Parallel()
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: adaptiveBenchmarkClosedChunkStream(),
		order:  []string{},
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatalf("newAdaptiveBenchmarkObservedClient: %v", err)
	}
	var observedRequestID string
	clearObserver := client.SetModelStartObserver(func(
		_ context.Context,
		requestID string,
	) {
		delegate.order = append(delegate.order, "observe")
		observedRequestID = requestID
	})
	request := llm.Request{Model: "benchmark-model", MaxTokens: 17}
	ctx := tools.WithRequestID(t.Context(), "request-7")

	stream, err := client.Stream(ctx, request)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if stream == nil ||
		observedRequestID != "request-7" ||
		!reflect.DeepEqual(delegate.request, request) ||
		!reflect.DeepEqual(delegate.order, []string{"observe", "delegate"}) {
		t.Fatalf(
			"stream=%v request_id=%q request=%#v order=%v",
			stream,
			observedRequestID,
			delegate.request,
			delegate.order,
		)
	}
	clearObserver()
	if _, err := client.Stream(ctx, request); err != nil {
		t.Fatalf("Stream after clear: %v", err)
	}
	if !reflect.DeepEqual(
		delegate.order,
		[]string{"observe", "delegate", "delegate"},
	) {
		t.Fatalf("observer survived cleanup: %v", delegate.order)
	}
}

func TestAdaptiveBenchmarkObservedClientFailsExactlyOneTransport(
	t *testing.T,
) {
	t.Parallel()
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: adaptiveBenchmarkClosedChunkStream(),
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	transportErr := errors.New("injected transport failure")
	if err := client.FailNextTransport(transportErr); err != nil {
		t.Fatalf("FailNextTransport: %v", err)
	}
	if _, err := client.Stream(t.Context(), llm.Request{}); !errors.Is(
		err,
		transportErr,
	) {
		t.Fatalf("first Stream error=%v", err)
	}
	if _, err := client.Stream(t.Context(), llm.Request{}); err != nil {
		t.Fatalf("second Stream: %v", err)
	}
	if delegate.calls != 1 {
		t.Fatalf("delegate calls=%d, want 1", delegate.calls)
	}
}

func TestAdaptiveBenchmarkObservedClientFailsTransportUntilScopeClears(
	t *testing.T,
) {
	t.Parallel()
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: adaptiveBenchmarkClosedChunkStream(),
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	transportErr := errors.New("persistent injected transport failure")
	clearFailure, err := client.FailTransportUntilCleared(transportErr)
	if err != nil {
		t.Fatalf("FailTransportUntilCleared: %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := client.Stream(
			t.Context(),
			llm.Request{},
		); !errors.Is(err, transportErr) {
			t.Fatalf("Stream attempt %d error=%v", attempt, err)
		}
	}
	if delegate.calls != 0 {
		t.Fatalf("delegate calls while armed=%d, want 0", delegate.calls)
	}
	clearFailure()
	if _, err := client.Stream(t.Context(), llm.Request{}); err != nil {
		t.Fatalf("Stream after clear: %v", err)
	}
	if delegate.calls != 1 {
		t.Fatalf("delegate calls after clear=%d, want 1", delegate.calls)
	}
}

func TestAdaptiveBenchmarkObservedClientInterceptsScopedStream(
	t *testing.T,
) {
	t.Parallel()
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: adaptiveBenchmarkClosedChunkStream(),
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	intercepted := adaptiveBenchmarkClosedChunkStream()
	interceptorCalls := 0
	clear, err := client.SetStreamInterceptor(
		func(
			context.Context,
			llm.Request,
		) (<-chan llm.Chunk, error) {
			interceptorCalls++
			return intercepted, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := client.Stream(t.Context(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if stream == nil ||
		interceptorCalls != 1 ||
		delegate.calls != 0 {
		t.Fatalf(
			"intercepted stream_non_nil/calls/delegate = %v/%d/%d",
			stream != nil,
			interceptorCalls,
			delegate.calls,
		)
	}

	clear()
	stream, err = client.Stream(t.Context(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if stream == nil || delegate.calls != 1 {
		t.Fatalf(
			"delegate stream_non_nil/calls after clear = %v/%d",
			stream != nil,
			delegate.calls,
		)
	}
}

func TestAdaptiveBenchmarkObservedClientInterceptorClearIsGenerationSafe(
	t *testing.T,
) {
	t.Parallel()
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: adaptiveBenchmarkClosedChunkStream(),
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	firstStream := adaptiveBenchmarkClosedChunkStream()
	clearFirst, err := client.SetStreamInterceptor(
		func(
			context.Context,
			llm.Request,
		) (<-chan llm.Chunk, error) {
			return firstStream, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondStream := adaptiveBenchmarkClosedChunkStream()
	clearSecond, err := client.SetStreamInterceptor(
		func(
			context.Context,
			llm.Request,
		) (<-chan llm.Chunk, error) {
			return secondStream, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	clearFirst()
	stream, err := client.Stream(t.Context(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if stream == nil || delegate.calls != 0 {
		t.Fatalf(
			"stale clear stream_non_nil/delegate = %v/%d",
			stream != nil,
			delegate.calls,
		)
	}
	clearSecond()
	if _, err := client.Stream(t.Context(), llm.Request{}); err != nil {
		t.Fatal(err)
	}
	if delegate.calls != 1 {
		t.Fatalf("delegate calls after active clear = %d", delegate.calls)
	}
}

func TestAdaptiveBenchmarkObservedClientTransportFailurePrecedesInterceptor(
	t *testing.T,
) {
	t.Parallel()
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: adaptiveBenchmarkClosedChunkStream(),
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	interceptorCalls := 0
	if _, err := client.SetStreamInterceptor(
		func(
			context.Context,
			llm.Request,
		) (<-chan llm.Chunk, error) {
			interceptorCalls++
			return adaptiveBenchmarkClosedChunkStream(), nil
		},
	); err != nil {
		t.Fatal(err)
	}
	transportErr := errors.New("transport wins")
	if err := client.FailNextTransport(transportErr); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Stream(
		t.Context(),
		llm.Request{},
	); !errors.Is(err, transportErr) {
		t.Fatalf("Stream error = %v", err)
	}
	if interceptorCalls != 0 || delegate.calls != 0 {
		t.Fatalf(
			"interceptor/delegate calls = %d/%d",
			interceptorCalls,
			delegate.calls,
		)
	}
}

func TestAdaptiveBenchmarkObservedClientRejectsUndeclaredToolBeforeDispatch(
	t *testing.T,
) {
	t.Parallel()
	source := make(chan llm.Chunk, 2)
	call := llm.ToolCall{ID: "call-1", Type: "function"}
	call.Function.Name = "calendar_create_event"
	call.Function.Arguments = `{"when":"next Tuesday"}`
	source <- llm.Chunk{ToolCall: &call}
	source <- llm.Chunk{FinishReason: "tool_calls"}
	close(source)
	delegate := &adaptiveBenchmarkLLMClientFake{stream: source}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	var allowed llm.ToolDef
	allowed.Type = "function"
	allowed.Function.Name = "tool_search"

	stream, err := client.Stream(
		t.Context(),
		llm.Request{Tools: []llm.ToolDef{allowed}},
	)
	if err != nil {
		t.Fatal(err)
	}
	chunk, ok := <-stream
	if !ok || !errors.Is(
		chunk.Err,
		errAdaptiveBenchmarkModelToolCallRejected,
	) {
		t.Fatalf("rejected chunk = %#v", chunk)
	}
	if _, ok := <-stream; ok {
		t.Fatal("guard emitted content after rejected tool call")
	}

	emptySource := make(chan llm.Chunk, 1)
	emptySource <- llm.Chunk{ToolCall: &call}
	close(emptySource)
	emptyClient, err := newAdaptiveBenchmarkObservedClient(
		&adaptiveBenchmarkLLMClientFake{stream: emptySource},
	)
	if err != nil {
		t.Fatal(err)
	}
	emptyStream, err := emptyClient.Stream(t.Context(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	emptyChunk := <-emptyStream
	if !errors.Is(
		emptyChunk.Err,
		errAdaptiveBenchmarkModelToolCallRejected,
	) {
		t.Fatalf("empty-manifest chunk = %#v", emptyChunk)
	}
}

func TestAdaptiveBenchmarkObservedClientRejectsInvalidFailureState(
	t *testing.T,
) {
	t.Parallel()
	if client, err := newAdaptiveBenchmarkObservedClient(nil); err == nil ||
		client != nil {
		t.Fatalf("client=%#v error=%v", client, err)
	}
	client, err := newAdaptiveBenchmarkObservedClient(
		&adaptiveBenchmarkLLMClientFake{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.FailNextTransport(nil); err == nil {
		t.Fatal("accepted nil transport failure")
	}
	first := errors.New("first")
	if err := client.FailNextTransport(first); err != nil {
		t.Fatal(err)
	}
	if err := client.FailNextTransport(errors.New("second")); err == nil {
		t.Fatal("overwrote armed transport failure")
	}
}

type adaptiveBenchmarkLLMClientFake struct {
	stream  chan llm.Chunk
	request llm.Request
	order   []string
	calls   int
}

func adaptiveBenchmarkClosedChunkStream() chan llm.Chunk {
	stream := make(chan llm.Chunk)
	close(stream)
	return stream
}

func (client *adaptiveBenchmarkLLMClientFake) Stream(
	_ context.Context,
	request llm.Request,
) (<-chan llm.Chunk, error) {
	client.calls++
	client.request = request
	client.order = append(client.order, "delegate")
	return client.stream, nil
}
