// Package openai_compat adapts the official openai-go SDK to Aura's llm.Client.
package openai_compat

import (
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

// Error renders the status and (on 429) the retry-after hint. It deliberately
// omits the body's full contents from the headline so a long provider body does
// not flood logs; callers inspect HTTPError.Body directly when they need it.
func (e *HTTPError) Error() string {
	if e.RetryAfterSec > 0 {
		return fmt.Sprintf("llm: provider returned HTTP %d (retry after %ds)", e.StatusCode, e.RetryAfterSec)
	}
	return fmt.Sprintf("llm: provider returned HTTP %d", e.StatusCode)
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
