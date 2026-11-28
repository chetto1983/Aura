package openai_compat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const testSecret = "sk-test-credential-0123456789abcdef"

func TestCredentialHTTPClientRefusesRedirectsAndKeepAlives(t *testing.T) {
	t.Parallel()
	hc := NewCredentialHTTPClient(5)
	if err := hc.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect policy %v", err)
	}
	if !hc.Transport.(*http.Transport).DisableKeepAlives {
		t.Fatal("keep-alives on")
	}
}

func TestScrubLeavesAnEmptySecretAlone(t *testing.T) {
	t.Parallel()
	if got := Scrub("plain body", ""); got != "plain body" {
		t.Fatalf("Scrub with no secret = %q", got)
	}
	if got := Scrub("a "+testSecret+" b", testSecret); got != "a "+Redacted+" b" {
		t.Fatalf("Scrub = %q", got)
	}
}

func TestScrubBodyRedactsASecretCutByTheCap(t *testing.T) {
	t.Parallel()
	head := strings.Repeat("y", MaxErrorBodyBytes-10)
	cases := []struct {
		name, body string
		want       func(string) bool
	}{
		{"a body read at the cap ending in the secret's first bytes", head + testSecret[:10], func(got string) bool {
			return got == head+Redacted
		}},
		{"a body shorter than the cap keeps a tail that only looks like the secret", "y" + testSecret[:10], func(got string) bool {
			return got == "y"+testSecret[:10]
		}},
		{"a whole secret over the cap is scrubbed before the cut", head + testSecret + strings.Repeat("z", 100), func(got string) bool {
			return len(got) == MaxErrorBodyBytes && !strings.Contains(got, testSecret[:6])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ScrubBody(tc.body, testSecret); !tc.want(got) || len(got) > MaxErrorBodyBytes {
				t.Fatalf("ScrubBody tail = %q", got[max(0, len(got)-40):])
			}
		})
	}
	if got := ScrubBody(head+"abc", ""); got != head+"abc" {
		t.Fatal("an empty secret rewrote the body")
	}
}

func TestScrubErrorKeepsACleanErrorItself(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("stream: %w", context.Canceled)
	if got := ScrubError(wrapped, testSecret); got != wrapped {
		t.Fatalf("ScrubError rewrote a clean error: %v", got)
	}
	got := ScrubError(errors.New("dial tcp: "+testSecret), testSecret)
	if got.Error() != "dial tcp: "+Redacted {
		t.Fatalf("ScrubError = %v", got)
	}
}
