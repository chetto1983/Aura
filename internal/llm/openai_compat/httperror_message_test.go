package openai_compat

import (
	"strings"
	"testing"
)

// The exact body OpenRouter returned for z-ai/glm-5.3-flash on 2026-09-03 when Aura's
// adaptive tier sent effort "none". The whole point is that this sentence reaches the
// log: without it the failure reads "provider returned HTTP 400" and says nothing.
func TestHTTPErrorCarriesTheProviderExplanation(t *testing.T) {
	err := &HTTPError{StatusCode: 400, Body: `{"error":{"message":` +
		`"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400}}`}
	got := err.Error()
	if !strings.Contains(got, "HTTP 400") || !strings.Contains(got, "Reasoning is mandatory") {
		t.Fatalf("Error() = %q, want the status AND the provider's sentence", got)
	}
}

func TestHTTPErrorMessageDegradesHonestly(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantContains string
		wantExact                bool
	}{
		{name: "no body at all", body: "", wantContains: "llm: provider returned HTTP 400", wantExact: true},
		{name: "json naming no message", body: `{"detail":{"x":1}}`,
			wantContains: "llm: provider returned HTTP 400", wantExact: true},
		{name: "plain text from a proxy", body: "upstream connect error", wantContains: "upstream connect error"},
		{name: "bare message field", body: `{"message":"bad request"}`, wantContains: "bad request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (&HTTPError{StatusCode: 400, Body: tc.body}).Error()
			if tc.wantExact && got != tc.wantContains {
				t.Fatalf("Error() = %q, want exactly %q", got, tc.wantContains)
			}
			if !strings.Contains(got, tc.wantContains) {
				t.Fatalf("Error() = %q, want it to contain %q", got, tc.wantContains)
			}
		})
	}
}

// A pathological body must not turn one log line into a page.
func TestHTTPErrorMessageIsBounded(t *testing.T) {
	err := &HTTPError{StatusCode: 500, Body: `{"error":{"message":"` + strings.Repeat("x", 5000) + `"}}`}
	if got := err.Error(); len([]rune(got)) > maxErrorMessageRunes+80 {
		t.Fatalf("Error() is %d runes, want it bounded", len([]rune(got)))
	}
}

// Retry-After still leads: it is the actionable half on a 429.
func TestHTTPErrorKeepsTheRetryHintAlongsideTheMessage(t *testing.T) {
	err := &HTTPError{StatusCode: 429, RetryAfterSec: 12, Body: `{"error":{"message":"slow down"}}`}
	got := err.Error()
	if !strings.Contains(got, "retry after 12s") || !strings.Contains(got, "slow down") {
		t.Fatalf("Error() = %q, want both the retry hint and the message", got)
	}
}
