//go:build !web_integration

package web

import (
	"context"
	"errors"
	"net"
	"testing"
)

// timeoutErr is a net.Error whose Timeout() reports true — the only branch
// isNetTimeout cares about.
type timeoutErr struct{ to bool }

func (e timeoutErr) Error() string   { return "timeout" }
func (e timeoutErr) Timeout() bool   { return e.to }
func (e timeoutErr) Temporary() bool { return false }

func TestClassifyTransportErr(t *testing.T) {
	ie := &internalError{code: CodeBlockedURL, reason: ReasonPrivateOrMetadata, host: "secret.internal"}
	if got := classifyTransportErr(ie); !errors.Is(got, ie) {
		t.Errorf("an internalError (SSRF block) must pass through non-retryable, got %v", got)
	}
	if got := classifyTransportErr(errors.New("connection reset")); !errors.Is(got, errRetryable) {
		t.Errorf("a generic transport error must become errRetryable, got %v", got)
	}
}

func TestIsNetTimeout(t *testing.T) {
	if !isNetTimeout(timeoutErr{to: true}) {
		t.Error("a net.Error with Timeout()==true must be a net timeout")
	}
	if isNetTimeout(timeoutErr{to: false}) {
		t.Error("a net.Error with Timeout()==false must NOT be a net timeout")
	}
	if isNetTimeout(errors.New("plain")) {
		t.Error("a non-net error must NOT be a net timeout")
	}
}

func TestClassifyFetchErr(t *testing.T) {
	c := &Client{}

	t.Run("web_error_passthrough", func(t *testing.T) {
		we := &WebError{Code: CodeResponseTooLarge, Message: "too big"}
		got, ok := AsWebError(c.classifyFetchErr(we))
		if !ok || got.Code != CodeResponseTooLarge {
			t.Errorf("a *WebError must pass through unchanged, got %+v ok=%v", got, ok)
		}
	})

	t.Run("internal_error_sanitized", func(t *testing.T) {
		ie := &internalError{code: CodeBlockedURL, reason: ReasonPrivateOrMetadata, host: "secret.internal"}
		got, ok := AsWebError(c.classifyFetchErr(ie))
		if !ok || got.Code != CodeBlockedURL || got.Reason != ReasonPrivateOrMetadata {
			t.Errorf("an *internalError must sanitize to its code/reason, got %+v ok=%v", got, ok)
		}
	})

	t.Run("deadline_becomes_timeout", func(t *testing.T) {
		got, ok := AsWebError(c.classifyFetchErr(context.DeadlineExceeded))
		if !ok || got.Code != CodeTimeout {
			t.Errorf("a deadline must map to timeout, got %+v ok=%v", got, ok)
		}
	})

	t.Run("net_timeout_becomes_timeout", func(t *testing.T) {
		got, ok := AsWebError(c.classifyFetchErr(net.Error(timeoutErr{to: true})))
		if !ok || got.Code != CodeTimeout {
			t.Errorf("a net timeout must map to timeout, got %+v ok=%v", got, ok)
		}
	})

	t.Run("leftover_retryable_becomes_http_error", func(t *testing.T) {
		got, ok := AsWebError(c.classifyFetchErr(errRetryable))
		if !ok || got.Code != CodeHTTPError {
			t.Errorf("a leftover errRetryable must map to http_error, got %+v ok=%v", got, ok)
		}
	})
}
