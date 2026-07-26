package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

var errAdaptiveBenchmarkModelToolCallRejected = errors.New(
	"adaptive benchmark model tool call rejected",
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
	var stream <-chan llm.Chunk
	var err error
	if interceptor != nil {
		stream, err = interceptor(ctx, request)
	} else {
		stream, err = client.delegate.Stream(ctx, request)
	}
	if err != nil || len(request.Tools) == 0 {
		return stream, err
	}
	return adaptiveBenchmarkGuardModelToolCalls(ctx, request, stream), nil
}

func adaptiveBenchmarkGuardModelToolCalls(
	ctx context.Context,
	request llm.Request,
	source <-chan llm.Chunk,
) <-chan llm.Chunk {
	guarded := make(chan llm.Chunk, 1)
	go func() {
		defer close(guarded)
		for chunk := range source {
			if err := adaptiveBenchmarkValidateModelToolCall(
				request,
				chunk.ToolCall,
			); err != nil {
				select {
				case guarded <- llm.Chunk{Err: err}:
				case <-ctx.Done():
				}
				for range source {
				}
				return
			}
			select {
			case guarded <- chunk:
			case <-ctx.Done():
				for range source {
				}
				return
			}
		}
	}()
	return guarded
}

func adaptiveBenchmarkValidateModelToolCall(
	request llm.Request,
	call *llm.ToolCall,
) error {
	if call == nil {
		return nil
	}
	declared := false
	for _, tool := range request.Tools {
		if tool.Function.Name == call.Function.Name {
			declared = true
			break
		}
	}
	event := &agent.Event{
		Actions: agent.Actions{
			ToolInvocation: &agent.ToolInvocation{
				Event:     agent.ToolInvocationStart,
				ToolName:  call.Function.Name,
				Arguments: call.Function.Arguments,
			},
		},
	}
	if !declared || validateAdaptiveBenchmarkDispatch(event) != nil {
		return fmt.Errorf(
			"%w: %q",
			errAdaptiveBenchmarkModelToolCallRejected,
			call.Function.Name,
		)
	}
	return nil
}
