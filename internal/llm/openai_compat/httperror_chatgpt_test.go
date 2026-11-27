package openai_compat

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDirectPlanAdmissionErrorRetainsDetailAndRequestID(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{},
		Body: io.NopCloser(strings.NewReader(`{"detail":"Enable plan usage in ChatGPT Settings"}`))}
	response.Header.Set("x-request-id", "req-plan")
	err := newHTTPError(response)
	if !strings.Contains(err.Error(), "Enable plan usage") || err.RequestID != "req-plan" {
		t.Fatalf("direct admission reason or support reference lost: %v", err)
	}
	response.Header.Set("x-request-id", strings.Repeat("a", 200)+"\n injected")
	if got := responseRequestID(response); len(got) > 128 || strings.Contains(got, "\n") {
		t.Fatal("request identifier is not bounded to one line")
	}
}

// Anthropic names the support reference request-id, not x-request-id.
func TestRequestIDFallsBackToTheAnthropicHeader(t *testing.T) {
	response := &http.Response{Header: http.Header{}}
	response.Header.Set("request-id", "req_011")
	if got := responseRequestID(response); got != "req_011" {
		t.Fatalf("request-id not read: %q", got)
	}
	response.Header.Set("x-request-id", "req-openai")
	if got := responseRequestID(response); got != "req-openai" {
		t.Fatalf("x-request-id must win when both are sent: %q", got)
	}
}
