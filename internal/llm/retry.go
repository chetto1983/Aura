package llm

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// RetryClient wraps a Client with classify-then-retry behavior.
//   - TRANSIENT errors retry up to MaxRetries with jittered exponential backoff
//     at the caller's Request.Temperature (preserved, D-08).
//   - CONTENT errors retry up to MaxContentRetries with the staircase
//     ContentTemperatures (override applied to Request.Temperature, D-08), no
//     backoff sleep, and a validation nudge appended to Request.Messages.
//   - PERMANENT errors return immediately.
type RetryClient struct {
	inner               Client
	maxRetries          int
	baseDelay           time.Duration
	maxDelay            time.Duration
	maxContentRetries   int
	contentTemperatures []float64
	jitterRatio         float64
}

// RetryConfig holds retry configuration.
type RetryConfig struct {
	MaxRetries          int
	BaseDelay           time.Duration
	MaxDelay            time.Duration
	MaxContentRetries   int
	ContentTemperatures []float64
	JitterRatio         float64
}

// DefaultRetryConfig returns sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:          5,
		BaseDelay:           time.Second,
		MaxDelay:            30 * time.Second,
		MaxContentRetries:   3,
		ContentTemperatures: []float64{0.0, 0.3, 0.7},
		JitterRatio:         0.5,
	}
}

// NewRetryClient wraps a Client with classify-then-retry logic.
// Signature is preserved for backward compatibility (D-10).
func NewRetryClient(inner Client, cfg RetryConfig) *RetryClient {
	if cfg.MaxContentRetries == 0 {
		cfg.MaxContentRetries = 3
	}
	if len(cfg.ContentTemperatures) == 0 {
		cfg.ContentTemperatures = []float64{0.0, 0.3, 0.7}
	}
	if cfg.JitterRatio == 0 {
		cfg.JitterRatio = 0.5
	}
	return &RetryClient{
		inner:               inner,
		maxRetries:          cfg.MaxRetries,
		baseDelay:           cfg.BaseDelay,
		maxDelay:            cfg.MaxDelay,
		maxContentRetries:   cfg.MaxContentRetries,
		contentTemperatures: cfg.ContentTemperatures,
		jitterRatio:         cfg.JitterRatio,
	}
}

// Send classifies each error and dispatches per bucket.
func (r *RetryClient) Send(ctx context.Context, req Request) (Response, error) {
	callerTemp := req.Temperature // D-08 — captured once, restored on every TRANSIENT retry
	contentAttempt, transientAttempt := 0, 0
	for {
		resp, err := r.inner.Send(ctx, req)
		if err == nil {
			return resp, nil
		}
		bucket, retryable, cleaned := Classify(err)
		switch bucket {
		case BucketPermanent:
			return Response{}, err
		case BucketTransient:
			if !retryable || transientAttempt >= r.maxRetries {
				return Response{}, err
			}
			req.Temperature = callerTemp // D-08 preservation
			d := jitteredBackoff(transientAttempt, r.baseDelay, r.maxDelay, r.jitterRatio)
			transientAttempt++
			select {
			case <-ctx.Done():
				return Response{}, ctx.Err()
			case <-time.After(d):
			}
		case BucketContent:
			if contentAttempt >= r.maxContentRetries || contentAttempt >= len(r.contentTemperatures) {
				return Response{}, err
			}
			t := r.contentTemperatures[contentAttempt]
			req.Temperature = &t // D-08 override on CONTENT only
			req.Messages = appendValidationNudge(req.Messages, cleaned)
			contentAttempt++
			// No sleep on CONTENT retry.
		}
	}
}

// Stream wraps the inner Stream call with the same classify-dispatch shape as
// Send. The streamed chunk element type is Token (defined in client.go:69-75)
// — there is no StreamChunk type in the codebase. On inner-stream error path
// (the error returned BEFORE any chunk is emitted) the same classify dispatch
// applies; mid-stream errors propagate to the consumer via Token.Err on the
// returned channel.
func (r *RetryClient) Stream(ctx context.Context, req Request) (<-chan Token, error) {
	callerTemp := req.Temperature // D-08
	contentAttempt, transientAttempt := 0, 0
	for {
		ch, err := r.inner.Stream(ctx, req)
		if err == nil {
			return ch, nil
		}
		bucket, retryable, cleaned := Classify(err)
		switch bucket {
		case BucketPermanent:
			return nil, err
		case BucketTransient:
			if !retryable || transientAttempt >= r.maxRetries {
				return nil, err
			}
			req.Temperature = callerTemp // D-08 preservation
			d := jitteredBackoff(transientAttempt, r.baseDelay, r.maxDelay, r.jitterRatio)
			transientAttempt++
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
		case BucketContent:
			if contentAttempt >= r.maxContentRetries || contentAttempt >= len(r.contentTemperatures) {
				return nil, err
			}
			t := r.contentTemperatures[contentAttempt]
			req.Temperature = &t
			req.Messages = appendValidationNudge(req.Messages, cleaned)
			contentAttempt++
			// No sleep on CONTENT retry.
		}
	}
}

// jitteredBackoff computes base*2^attempt * uniform(1-r, 1+r), capped at maxDelay.
func jitteredBackoff(attempt int, base, max time.Duration, ratio float64) time.Duration {
	exp := math.Pow(2, float64(attempt))
	d := time.Duration(float64(base) * exp)
	if d > max {
		d = max
	}
	// Symmetric jitter: [1-ratio, 1+ratio]
	jitter := 1 + (rand.Float64()*2-1)*ratio
	scaled := time.Duration(float64(d) * jitter)
	if scaled < 0 {
		return 0
	}
	if scaled > max {
		return max
	}
	return scaled
}

// appendValidationNudge appends a system message instructing the LLM to retry
// with corrected output. The cleaned string from Classify() is already redacted.
func appendValidationNudge(msgs []Message, cleaned string) []Message {
	nudge := Message{
		Role:    "system",
		Content: "Previous output failed validation: " + cleaned + ". Retry with corrected output.",
	}
	out := make([]Message, 0, len(msgs)+1)
	out = append(out, msgs...)
	out = append(out, nudge)
	return out
}
