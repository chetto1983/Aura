package llm

import (
	"context"
	"errors"
	"testing"
)

// fakeTimeoutError implements net.Error with configurable Timeout().
type fakeTimeoutError struct{ timeout bool }

func (e *fakeTimeoutError) Error() string   { return "fake network error" }
func (e *fakeTimeoutError) Timeout() bool   { return e.timeout }
func (e *fakeTimeoutError) Temporary() bool { return e.timeout }

func TestUserMessageFor_RateLimited_429(t *testing.T) {
	err := &APIError{StatusCode: 429, Body: "rate limit exceeded"}
	got := UserMessageFor(err)
	if got != msgRateLimited {
		t.Errorf("got %q, want %q", got, msgRateLimited)
	}
}

func TestUserMessageFor_RateLimited_MessagePattern(t *testing.T) {
	err := errors.New("rate limit exceeded by provider")
	got := UserMessageFor(err)
	if got != msgRateLimited {
		t.Errorf("got %q, want %q", got, msgRateLimited)
	}
}

func TestUserMessageFor_NetworkTimeout(t *testing.T) {
	err := &fakeTimeoutError{timeout: true}
	got := UserMessageFor(err)
	if got != msgNetworkIssue {
		t.Errorf("got %q, want %q", got, msgNetworkIssue)
	}
}

func TestUserMessageFor_NetworkDeadline(t *testing.T) {
	got := UserMessageFor(context.DeadlineExceeded)
	if got != msgNetworkIssue {
		t.Errorf("got %q, want %q", got, msgNetworkIssue)
	}
}

func TestUserMessageFor_AuthFailed_401(t *testing.T) {
	err := &APIError{StatusCode: 401, Body: "unauthorized"}
	got := UserMessageFor(err)
	if got != msgAuthFailed {
		t.Errorf("status 401: got %q, want %q", got, msgAuthFailed)
	}
}

func TestUserMessageFor_AuthFailed_403(t *testing.T) {
	err := &APIError{StatusCode: 403, Body: "forbidden"}
	got := UserMessageFor(err)
	if got != msgAuthFailed {
		t.Errorf("status 403: got %q, want %q", got, msgAuthFailed)
	}
}

func TestUserMessageFor_ModelUnavailable_404(t *testing.T) {
	err := &APIError{StatusCode: 404, Body: "model not found"}
	got := UserMessageFor(err)
	if got != msgModelUnavailable {
		t.Errorf("got %q, want %q", got, msgModelUnavailable)
	}
}

func TestUserMessageFor_ModelUnavailable_MessagePattern(t *testing.T) {
	err := errors.New("model not found in registry")
	got := UserMessageFor(err)
	if got != msgModelUnavailable {
		t.Errorf("got %q, want %q", got, msgModelUnavailable)
	}
}

func TestUserMessageFor_Unknown(t *testing.T) {
	err := &APIError{StatusCode: 400, Body: "bad request"}
	got := UserMessageFor(err)
	if got != msgUnknown {
		t.Errorf("got %q, want %q", got, msgUnknown)
	}
}

func TestUserMessageFor_NoLeakOfErrContent(t *testing.T) {
	sensitiveErr := errors.New("sk-or-v1-secretapikey and https://secret.host/path")
	got := UserMessageFor(sensitiveErr)
	allowed := []string{msgRateLimited, msgNetworkIssue, msgAuthFailed, msgModelUnavailable, msgUnknown}
	for _, a := range allowed {
		if got == a {
			return
		}
	}
	t.Errorf("output %q is not one of the fixed template strings (possible leak)", got)
}

func TestUserMessageFor_Nil(t *testing.T) {
	got := UserMessageFor(nil)
	if got != "" {
		t.Errorf("got %q, want empty string for nil error", got)
	}
}
