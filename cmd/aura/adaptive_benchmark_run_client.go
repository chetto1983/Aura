package main

import (
	"context"
	"errors"
	"sync"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

type adaptiveBenchmarkModelStartObserver func(context.Context, string)

type adaptiveBenchmarkStreamInterceptor func(
	context.Context,
	llm.Request,
) (<-chan llm.Chunk, error)

type adaptiveBenchmarkObservedClient struct {
	delegate llm.Client

	mu                          sync.Mutex
	observer                    adaptiveBenchmarkModelStartObserver
	observerGeneration          uint64
	streamInterceptor           adaptiveBenchmarkStreamInterceptor
	streamInterceptorGeneration uint64
	transportFailure            error
	transportFailurePersistent  bool
	transportFailureGeneration  uint64
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

func (client *adaptiveBenchmarkObservedClient) SetStreamInterceptor(
	interceptor adaptiveBenchmarkStreamInterceptor,
) (func(), error) {
	if client == nil || interceptor == nil {
		return nil, errors.New(
			"adaptive benchmark stream interceptor is invalid",
		)
	}
	client.mu.Lock()
	client.streamInterceptorGeneration++
	generation := client.streamInterceptorGeneration
	client.streamInterceptor = interceptor
	client.mu.Unlock()
	return func() {
		client.mu.Lock()
		defer client.mu.Unlock()
		if client.streamInterceptorGeneration == generation {
			client.streamInterceptor = nil
		}
	}, nil
}

func (client *adaptiveBenchmarkObservedClient) FailNextTransport(
	failure error,
) error {
	_, err := client.armTransportFailure(failure, false)
	return err
}

func (client *adaptiveBenchmarkObservedClient) FailTransportUntilCleared(
	failure error,
) (func(), error) {
	generation, err := client.armTransportFailure(failure, true)
	if err != nil {
		return nil, err
	}
	return func() {
		client.mu.Lock()
		defer client.mu.Unlock()
		if client.transportFailureGeneration == generation {
			client.transportFailure = nil
			client.transportFailurePersistent = false
		}
	}, nil
}

func (client *adaptiveBenchmarkObservedClient) armTransportFailure(
	failure error,
	persistent bool,
) (uint64, error) {
	if client == nil || failure == nil {
		return 0, errors.New(
			"adaptive benchmark transport failure is invalid",
		)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.transportFailure != nil {
		return 0, errors.New(
			"adaptive benchmark transport failure is already armed",
		)
	}
	client.transportFailureGeneration++
	client.transportFailure = failure
	client.transportFailurePersistent = persistent
	return client.transportFailureGeneration, nil
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
	interceptor := client.streamInterceptor
	failure := client.transportFailure
	if !client.transportFailurePersistent {
		client.transportFailure = nil
	}
	client.mu.Unlock()
	if observer != nil {
		observer(ctx, tools.RequestIDFromContext(ctx))
	}
	if failure != nil {
		return nil, failure
	}
	if interceptor != nil {
		return interceptor(ctx, request)
	}
	return client.delegate.Stream(ctx, request)
}
