package anthropic

import (
	"net/http"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

// newSDK is the one constructor of the official SDK client every call site uses.
//
// WithoutEnvironmentDefaults keeps the SDK from reading ANTHROPIC_API_KEY,
// ANTHROPIC_AUTH_TOKEN, ANTHROPIC_BASE_URL and a profile file from the host
// (option/requestoption.go, v2.15.0). The SDK also traces every call through the global
// OpenTelemetry provider and puts message content in the span when
// ANTHROPIC_OPEN_TELEMETRY_TRACES_CONTENT_MODE=content (option/tracing.go), and forwards
// trace headers to the API; both are off, since Aura records its own llm.request span.
// Retries stay with the agent loop.
func newSDK(key string, hc *http.Client, baseURL string) anthropic.Client {
	return anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithAPIKey(key),
		option.WithBaseURL(baseURL),
		option.WithMaxRetries(0),
		option.WithHTTPClient(hc),
		option.WithMiddleware(openai_compat.StreamIdleMiddleware),
		option.WithoutOpenTelemetry(),
		option.WithOpenTelemetryPropagation(false),
	)
}
