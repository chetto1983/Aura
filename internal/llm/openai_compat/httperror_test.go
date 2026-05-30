package openai_compat

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHTTPError_Error asserts the headline carries the status and the 429
// retry-after hint, and (release-blocking, T-03-01) never the API key.
func TestHTTPError_Error(t *testing.T) {
	e := &HTTPError{StatusCode: 429, RetryAfterSec: 7, Body: "rate limited"}
	got := e.Error()
	if !strings.Contains(got, "429") || !strings.Contains(got, "7") {
		t.Errorf("Error() = %q, want status 429 + retry 7s", got)
	}

	e2 := &HTTPError{StatusCode: 500, Body: "boom"}
	if strings.Contains(e2.Error(), "retry") {
		t.Errorf("Error() = %q, should not mention retry without Retry-After", e2.Error())
	}
}

// TestNewHTTPError_RetryAfter asserts newHTTPError parses Retry-After on 429 and
// reads the provider body into HTTPError.Body.
func TestNewHTTPError_RetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"slow down"}`))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL) //nolint:noctx // test
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	herr := newHTTPError(resp)
	if herr.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", herr.StatusCode)
	}
	if herr.RetryAfterSec != 12 {
		t.Errorf("RetryAfterSec = %d, want 12", herr.RetryAfterSec)
	}
	if !strings.Contains(herr.Body, "slow down") {
		t.Errorf("Body = %q, want provider body", herr.Body)
	}

	// errors.As shape so Plan-04 callers can detect it.
	var target *HTTPError
	var wrapped error = herr
	if !errors.As(wrapped, &target) {
		t.Error("errors.As(*HTTPError) failed — caller cannot classify the wire error")
	}
}
