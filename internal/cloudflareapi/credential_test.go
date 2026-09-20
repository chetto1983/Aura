package cloudflareapi

import (
	"errors"
	"net/http"
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
