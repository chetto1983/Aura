// Package multimodal is the SINGLE shared client for Aura's OpenAI-compatible
// media sidecars — vision (image → text), STT (audio → text), and TTS (text →
// audio). Both user-facing surfaces consume it: the Telegram channel
// (internal/channels/telegram) and the web/upload asset pipeline
// (internal/assets). Each modality is a thin stdlib net/http POST + decode with
// ONE config-only local↔cloud swap (D-28): unset cloud model → the local sidecar
// (no auth); set a cloud model → the shared OpenRouter endpoint authenticated
// with the SINGLE OPENROUTER_API_KEY every cloud backend reuses. The Bearer key
// is set ONLY on the Authorization header at request-build time, never logged or
// serialized.
//
// This package owns the wire format and the credential boundary; callers own
// their own I/O (telebot file download + retry + sendVoice; objectstore Get +
// image downscale + Result mapping). There is ZERO Go ML here — the sidecars own
// the models (faster-whisper STT, Kokoro TTS, GLM-OCR vision) or OpenRouter does.
package multimodal

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// defaultTimeoutSec is the per-request ceiling when a config TimeoutSec is unset
// (T-13-08-SidecarDoS). It rides the request ctx, not http.Client.Timeout, so a
// healthy slow body is not aborted mid-read.
const defaultTimeoutSec = 30

// connectTimeoutSec bounds the TCP dial only; the total request budget is the
// ctx deadline.
const connectTimeoutSec = 10

// HTTPClient builds the shared HTTP client for the media sidecars. It mirrors
// internal/llm/openai_compat.New: a connect timeout on the dialer, NO
// http.Client.Timeout (the deadline rides the request ctx), and DisableKeepAlives
// so a pooled conn never lingers past the request and trips the package goleak.
func HTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: connectTimeoutSec * time.Second,
			}).DialContext,
			DisableKeepAlives: true,
		},
	}
}

// TimeoutContext derives a request-scoped ctx carrying the sidecar timeout (sec,
// or defaultTimeoutSec when sec <= 0). The caller MUST call the returned cancel
// (defer) so the timer is released — an undrained timer goroutine trips goleak.
func TimeoutContext(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		sec = defaultTimeoutSec
	}
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}

// StatusError wraps a non-2xx sidecar response into a typed error so callers
// classify on a status code, never a string. It never carries the request body
// (no secret leak).
type StatusError struct {
	Endpoint   string
	StatusCode int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("multimodal sidecar %s: HTTP %d", e.Endpoint, e.StatusCode)
}

// resolveClient returns the caller-supplied client or a fresh shared one.
func resolveClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return HTTPClient()
}

// setBearer sets the Authorization header ONLY when apiKey is non-empty (the
// cloud route). The local sidecar route sends no auth. Centralizing this keeps
// the credential boundary in one place (D-28).
func setBearer(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}
