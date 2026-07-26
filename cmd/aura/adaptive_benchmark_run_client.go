package main

import (
	"context"
	"errors"
	"sync"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

type adaptiveBenchmarkModelStartObserver func(context.Context, string)

type adaptiveBenchmarkObservedClient struct {
	delegate llm.Client

	mu                 sync.Mutex
	observer           adaptiveBenchmarkModelStartObserver
	observerGeneration uint64
	transportFailure   error
}

func newAdaptiveBenchmarkObservedClient(
	delegate llm.Client,
) (*adaptiveBenchmarkObservedClient, error) {
	if delegate == nil {
		return nil, errors.New(
			"adaptive benchmark LLM client is unavailable",
		)
	}
	return &adaptiveBenchmarkObservedClient{delegate: delegate}, nil
}

func (client *adaptiveBenchmarkObservedClient) SetModelStartObserver(
	observer adaptiveBenchmarkModelStartObserver,
) func() {
	if client == nil {
		return func() {}
	}
	client.mu.Lock()
	client.observerGeneration++
	generation := client.observerGeneration
	client.observer = observer
	client.mu.Unlock()
	return func() {
		client.mu.Lock()
		defer client.mu.Unlock()
		if client.observerGeneration == generation {
			client.observer = nil
		}
	}
}

func (client *adaptiveBenchmarkObservedClient) FailNextTransport(
	failure error,
) error {
	if client == nil || failure == nil {
		return errors.New(
			"adaptive benchmark transport failure is invalid",
		)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.transportFailure != nil {
		return errors.New(
			"adaptive benchmark transport failure is already armed",
		)
	}
	client.transportFailure = failure
	return nil
}

func (client *adaptiveBenchmarkObservedClient) Stream(
	ctx context.Context,
	request llm.Request,
) (<-chan llm.Chunk, error) {
	if client == nil || client.delegate == nil {
		return nil, errors.New(
			"adaptive benchmark LLM client is unavailable",
		)
	}
	client.mu.Lock()
	observer := client.observer
	failure := client.transportFailure
	client.transportFailure = nil
	client.mu.Unlock()
	if observer != nil {
		observer(ctx, tools.RequestIDFromContext(ctx))
	}
	if failure != nil {
		return nil, failure
	}
	return client.delegate.Stream(ctx, request)
}
