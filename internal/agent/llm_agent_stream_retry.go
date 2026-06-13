package agent

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

const (
	streamOpenMaxAttempts       = 2
	streamOpenRetryDelay        = 750 * time.Millisecond
	streamOpenMaxRetryAfterWait = 5 * time.Second
)

func (a *LlmAgent) streamWithOpenRetry(ctx context.Context, req llm.Request, requestID string) (<-chan llm.Chunk, error) {
	var lastErr error
	for attempt := 1; attempt <= streamOpenMaxAttempts; attempt++ {
		if a.breaker != nil {
			if err := a.breaker.Allow(); err != nil {
				return nil, err
			}
		}
		recordLLMStreamOpen()
		ch, err := a.client.Stream(ctx, req)
		if err == nil {
			if a.breaker != nil {
				a.breaker.Success()
			}
			if attempt > 1 {
				reasoningtrace.Record("agent_stream_open_retry_success", map[string]any{
					"request_id": requestID,
					"thread_id":  a.sessionID,
					"attempt":    attempt,
				})
			}
			return ch, nil
		}
		lastErr = err
		reasoningtrace.Record("agent_stream_open_error", map[string]any{
			"request_id": requestID,
			"thread_id":  a.sessionID,
			"attempt":    attempt,
			"error":      err.Error(),
		})
		if !retryableStreamOpenError(err) {
			return nil, err
		}
		if a.breaker != nil {
			a.breaker.Failure(err)
		}
		if attempt == streamOpenMaxAttempts {
			return nil, err
		}
		recordLLMStreamRetry()
		timer := time.NewTimer(streamOpenRetryDelayFor(err))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

func retryableStreamOpenError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var httpErr *openai_compat.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 429 || httpErr.StatusCode >= 500
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return true
		}
		if urlErr.Err != nil && retryableNetworkText(urlErr.Err.Error()) {
			return true
		}
	}
	// A stream idle-timeout stall (B-08) is a retryable transport error: the stream
	// opened cleanly then went silent, so a fresh attempt may succeed.
	if errors.Is(err, openai_compat.ErrStreamIdleTimeout) {
		return true
	}
	// Typed network sentinels are the primary classifier (B-13): errors.Is sees a
	// wrapped sentinel even when its rendered message carries no substring marker
	// (e.g. a platform that renders ECONNRESET as "forcibly closed"). The substring
	// table below is the last-resort fallback for platform strings without a sentinel.
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	return retryableNetworkText(err.Error())
}

func retryableNetworkText(s string) bool {
	s = strings.ToLower(s)
	for _, marker := range []string{
		"wsarecv",
		"connection attempt failed",
		"connection reset",
		"connection refused",
		"connection timed out",
		"server closed idle connection",
		"unexpected eof",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return s == "eof"
}

func streamOpenRetryDelayFor(err error) time.Duration {
	var httpErr *openai_compat.HTTPError
	if errors.As(err, &httpErr) && httpErr.RetryAfterSec > 0 {
		d := time.Duration(httpErr.RetryAfterSec) * time.Second
		if d > streamOpenMaxRetryAfterWait {
			return streamOpenMaxRetryAfterWait
		}
		return d
	}
	return streamOpenRetryDelay
}
