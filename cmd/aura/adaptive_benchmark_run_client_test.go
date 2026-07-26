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
		stream: make(chan llm.Chunk),
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
	if stream != delegate.stream ||
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
		stream: make(chan llm.Chunk),
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
		stream: make(chan llm.Chunk),
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
		stream: make(chan llm.Chunk),
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	intercepted := make(chan llm.Chunk)
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
	if stream != intercepted ||
		interceptorCalls != 1 ||
		delegate.calls != 0 {
		t.Fatalf(
			"intercepted stream/calls/delegate = %v/%d/%d",
			stream == intercepted,
			interceptorCalls,
			delegate.calls,
		)
	}

	clear()
	stream, err = client.Stream(t.Context(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if stream != delegate.stream || delegate.calls != 1 {
		t.Fatalf(
			"delegate stream/calls after clear = %v/%d",
			stream == delegate.stream,
			delegate.calls,
		)
	}
}

func TestAdaptiveBenchmarkObservedClientInterceptorClearIsGenerationSafe(
	t *testing.T,
) {
	t.Parallel()
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: make(chan llm.Chunk),
	}
	client, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	firstStream := make(chan llm.Chunk)
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
	secondStream := make(chan llm.Chunk)
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
	if stream != secondStream || delegate.calls != 0 {
		t.Fatalf(
			"stale clear stream/delegate = %v/%d",
			stream == secondStream,
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
		stream: make(chan llm.Chunk),
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
			return make(chan llm.Chunk), nil
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

func (client *adaptiveBenchmarkLLMClientFake) Stream(
	_ context.Context,
	request llm.Request,
) (<-chan llm.Chunk, error) {
	client.calls++
	client.request = request
	client.order = append(client.order, "delegate")
	return client.stream, nil
}
