package anthropic

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

const (
	maxErrorBodyBytes = 64 << 10
	maxRequestIDBytes = 128
	redacted          = "[redacted]"
)

// errorStatus classifies an API error by its type rather than its status: an `error`
// event inside a stream arrives with the stream's own 200.
var errorStatus = map[anthropic.ErrorType]int{
	anthropic.ErrorTypeOverloadedError:     529,
	anthropic.ErrorTypeRateLimitError:      http.StatusTooManyRequests,
	anthropic.ErrorTypeAPIError:            http.StatusInternalServerError,
	anthropic.ErrorTypeTimeoutError:        http.StatusInternalServerError,
	anthropic.ErrorTypeAuthenticationError: http.StatusUnauthorized,
	anthropic.ErrorTypePermissionError:     http.StatusForbidden,
	anthropic.ErrorTypeBillingError:        http.StatusPaymentRequired,
	anthropic.ErrorTypeInvalidRequestError: http.StatusBadRequest,
}

// providerError turns what the SDK returned into the *openai_compat.HTTPError the agent
// loop already classifies (429 and 5xx retry, the rest do not), with the house key
// scrubbed from every string, and names Settings when the key itself was refused.
func providerError(err error, key string) error {
	httpErr, ok := errors.AsType[*openai_compat.HTTPError](err)
	if !ok {
		apiErr, isAPI := errors.AsType[*anthropic.Error](err)
		if !isAPI {
			return scrubbed(err, key)
		}
		httpErr = fromAPIError(apiErr)
	}
	result := *httpErr
	result.Body = scrub(result.Body, key)
	result.RequestID = scrub(result.RequestID, key)
	switch result.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("anthropic: Claude refused the API key; check ANTHROPIC_API_KEY in Settings: %w", &result)
	case http.StatusPaymentRequired:
		return fmt.Errorf("anthropic: the Claude account has no credit; top up the balance in the Claude Console or change ANTHROPIC_API_KEY in Settings: %w", &result)
	}
	return &result
}

// fromAPIError maps the one *anthropic.Error that reaches this client: an `error` event
// inside a 200 stream, since the idle middleware turns every non-2xx response into an
// HTTPError first. A type this SDK does not name is a server failure.
func fromAPIError(apiErr *anthropic.Error) *openai_compat.HTTPError {
	status, known := errorStatus[apiErr.Type()]
	if !known {
		status = http.StatusInternalServerError
	}
	body := apiErr.RawJSON()
	if len(body) > maxErrorBodyBytes {
		body = body[:maxErrorBodyBytes]
	}
	id := strings.Join(strings.Fields(apiErr.RequestID), " ")
	if len(id) > maxRequestIDBytes {
		id = id[:maxRequestIDBytes]
	}
	return &openai_compat.HTTPError{StatusCode: status, Body: body, RequestID: id}
}

func scrub(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, redacted)
}

func scrubbed(err error, key string) error {
	if message := scrub(err.Error(), key); message != err.Error() {
		return errors.New(message)
	}
	return err
}
