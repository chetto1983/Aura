// Package openai_compat adapts the official openai-go SDK to Aura's llm.Client.
package openai_compat

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	openai "github.com/openai/openai-go/v3"
)

// maxErrorBodyBytes caps how much of a non-2xx provider response body we read
// into HTTPError.Body. The provider's error body is bounded in practice; the cap
// is defence against a pathological large body (T-03-04).
const maxErrorBodyBytes = 64 << 10

// HTTPError is a non-2xx response from the provider. The wire layer does ZERO
// retries (Req#4) — it surfaces this signal and lets the caller (Plan 04) decide.
// Body is the provider's response body, never the request: it cannot contain the
// API key (D-28). The Error string likewise never contains the key.
type HTTPError struct {
	StatusCode    int
	RetryAfterSec int
	Body          string
}

func adaptSDKError(err error) error {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	body := apiErr.RawJSON()
	if len(body) > maxErrorBodyBytes {
		body = body[:maxErrorBodyBytes]
	}
	result := &HTTPError{StatusCode: apiErr.StatusCode, Body: body}
	if apiErr.StatusCode == http.StatusTooManyRequests && apiErr.Response != nil {
		if value := strings.TrimSpace(apiErr.Response.Header.Get("Retry-After")); value != "" {
			if seconds, parseErr := strconv.Atoi(value); parseErr == nil {
				result.RetryAfterSec = seconds
			}
		}
	}
	return result
}

// Error renders the status, the provider's own explanation, and (on 429) the
// retry-after hint.
//
// The explanation used to be left out entirely -- the body was captured and the doc said
// "callers inspect HTTPError.Body directly when they need it", but the caller that
// matters logs err.Error(), so nothing ever did. A bare "provider returned HTTP 400" is
// the least useful true sentence available: the provider had already said exactly what
// was wrong. Measured cost on 2026-09-03: every easy turn on a reasoning-mandatory model
// failed for weeks behind that string, while the discarded body read "Reasoning is
// mandatory for this endpoint and cannot be disabled."
//
// Only the message is lifted, and only a bounded slice of it, so a long body still cannot
// flood a log line; Body keeps the full text for anyone who wants it.
func (e *HTTPError) Error() string {
	head := fmt.Sprintf("llm: provider returned HTTP %d", e.StatusCode)
	if e.RetryAfterSec > 0 {
		head = fmt.Sprintf("%s (retry after %ds)", head, e.RetryAfterSec)
	}
	if detail := e.providerMessage(); detail != "" {
		return head + ": " + detail
	}
	return head
}

// maxErrorMessageRunes bounds the explanation lifted into the Error string. Long enough
// for a real provider sentence, short enough that a log line stays one line.
const maxErrorMessageRunes = 240

// providerMessage extracts the provider's human-readable explanation from Body.
// OpenAI-compatible backends nest it at error.message; anything else (a plain-text 502
// from a proxy, an HTML error page) falls back to the body's own leading text, which is
// still far more than the status code alone.
func (e *HTTPError) providerMessage() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return ""
	}
	var wire struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	message := body
	if err := json.Unmarshal([]byte(body), &wire); err == nil {
		switch {
		case strings.TrimSpace(wire.Error.Message) != "":
			message = wire.Error.Message
		case strings.TrimSpace(wire.Message) != "":
			message = wire.Message
		default:
			// Valid JSON that names no message: the raw object says less than nothing
			// to a reader, so leave the headline clean rather than pasting braces.
			return ""
		}
	}
	message = strings.Join(strings.Fields(message), " ")
	if runes := []rune(message); len(runes) > maxErrorMessageRunes {
		message = string(runes[:maxErrorMessageRunes]) + "..."
	}
	return message
}

// newHTTPError builds an HTTPError from a non-2xx response. It reads (a bounded
// slice of) the body and parses Retry-After on 429. The caller has already
// confirmed StatusCode/100 != 2.
func newHTTPError(resp *http.Response) *HTTPError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	e := &HTTPError{StatusCode: resp.StatusCode, Body: string(body)}
	if resp.StatusCode == http.StatusTooManyRequests {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if n, err := strconv.Atoi(ra); err == nil {
				e.RetryAfterSec = n
			}
		}
	}
	return e
}
