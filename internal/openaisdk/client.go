// Package openaisdk builds the one openai-go client shape Aura uses against a third-party base URL.
package openaisdk

import (
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Client builds an SDK client from explicit options only. openai.NewClient would first load
// OPENAI_API_KEY, OPENAI_ADMIN_KEY and custom headers from the environment (openai-go v3 client.go,
// DefaultClientOptions) and send them to the operator's base URL whenever no identity key overrides
// them; the SDK's opt-out, requestconfig.EnvironmentDefaultsDisabled, is in an internal package. Only
// Options is populated, so callers use the generic Get/Post/Execute methods, or build a typed service
// from client.Options. Retries are off: a paid POST must never be sent twice.
func Client(httpClient *http.Client, baseURL string, opts ...option.RequestOption) *openai.Client {
	return &openai.Client{Options: append([]option.RequestOption{
		option.WithBaseURL(strings.TrimRight(baseURL, "/") + "/"),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	}, opts...)}
}
