package openai_compat

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

// Redacted replaces a credential in every string a provider error carries.
const Redacted = "[redacted]"

// NewCredentialHTTPClient is the transport of a route that sends a credential Aura holds
// for the account (the Claude house key, the ChatGPT access token): it bounds the connect
// and refuses redirects, so the credential is only ever sent to the endpoint it was meant for.
func NewCredentialHTTPClient(connectTimeoutSec int) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:       (&net.Dialer{Timeout: time.Duration(connectTimeoutSec) * time.Second}).DialContext,
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Scrub replaces every occurrence of secret in s. An empty secret leaves s alone, where a
// bare strings.ReplaceAll would insert the marker between every byte.
func Scrub(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, Redacted)
}

// ScrubError returns err itself when its text holds no secret, so errors.Is still finds a
// cancellation, and a plain error carrying the scrubbed text otherwise.
func ScrubError(err error, secret string) error {
	if message := Scrub(err.Error(), secret); message != err.Error() {
		return errors.New(message)
	}
	return err
}

// ScrubBody scrubs the secret first and bounds the body after, so a cut never splits a
// secret out of reach of the match. A body this package already cut at the cap (newHTTPError
// reads at most MaxErrorBodyBytes) can still end in the first bytes of the secret; that tail
// is redacted too.
func ScrubBody(body, secret string) string {
	cutUpstream := len(body) == MaxErrorBodyBytes
	body = Scrub(body, secret)
	if cutUpstream && secret != "" {
		for n := min(len(secret)-1, len(body)); n > 0; n-- {
			if strings.HasSuffix(body, secret[:n]) {
				body = body[:len(body)-n] + Redacted
				break
			}
		}
	}
	if len(body) > MaxErrorBodyBytes {
		body = body[:MaxErrorBodyBytes]
	}
	return body
}
