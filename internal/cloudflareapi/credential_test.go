package cloudflareapi

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMissingCredentialNeverReachesTransport(t *testing.T) {
	for _, token := range []string{"", " \t"} {
		calls := 0
		c := New("", token, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected transport") })})
		_, err := c.VerifyToken(t.Context())
		if err == nil || Retryable(err) || calls != 0 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
		if err = c.DeleteTunnel(t.Context(), "account", "tunnel"); err == nil || calls != 0 {
			t.Fatalf("delete err=%v calls=%d", err, calls)
		}
	}
}

func TestVerifyTokenNamesAnInactiveToken(t *testing.T) {
	for _, status := range []string{"expired", "disabled"} {
		c := New("https://cloudflare.test", "token", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"success":true,"result":{"status":"` + status + `"}}`))}, nil
		})})
		_, err := c.VerifyToken(t.Context())
		if !errors.Is(err, ErrTokenInactive) || Retryable(err) {
			t.Fatalf("status %s: err=%v", status, err)
		}
	}
}
