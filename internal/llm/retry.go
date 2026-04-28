package llm

import (
	"context"
	"math"
	"time"
)

// RetryClient wraps a Client with exponential backoff retry logic.
type RetryClient struct {
	inner      Client
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

// RetryConfig holds retry configuration.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig returns sensible defaults (5 retries, 1s base, 30s max).
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 5,
		BaseDelay:  time.Second,
		MaxDelay:   30 * time.Second,
	}
}

// NewRetryClient wraps a Client with retry logic.
func NewRetryClient(inner Client, cfg RetryConfig) *RetryClient {
	return &RetryClient{
		inner:      inner,
		maxRetries: cfg.MaxRetries,
		baseDelay:  cfg.BaseDelay,
		maxDelay:   cfg.MaxDelay,
	}
}

// Send retries the request with exponential backoff on failure.
func (r *RetryClient) Send(ctx context.Context, req Request) (Response, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		resp, err := r.inner.Send(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt < r.maxRetries {
			delay := r.backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return Response{}, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return Response{}, lastErr
}

// Stream retries the streaming request with exponential backoff on failure.
func (r *RetryClient) Stream(ctx context.Context, req Request) (<-chan Token, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		ch, err := r.inner.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
		lastErr = err
		if attempt < r.maxRetries {
			delay := r.backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return nil, lastErr
}

func (r *RetryClient) backoffDelay(attempt int) time.Duration {
	delay := float64(r.baseDelay) * math.Pow(2, float64(attempt))
	if delay > float64(r.maxDelay) {
		return r.maxDelay
	}
	return time.Duration(delay)
}
