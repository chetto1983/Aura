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
