// Package telegram — this file is the shared 9c multimodal sidecar config + the
// thin OpenAI-compat HTTP-client ctor the four media clients (voice/tts/photo/
// documents) reuse. There is ZERO Go ML here: the sidecars own the models
// (faster-whisper STT, Kokoro TTS, GLM-OCR vision, markitdown); Aura is a plain
// HTTP POST + decode + error-wrap. The client mirrors internal/llm/openai_compat
// (a connect-timeout on the dialer + a request ctx that carries the total
// timeout, DisableKeepAlives so a kept-alive conn never outlives the request and
// trips goleak).
package telegram

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// MultimodalConfig is the telegram-package projection of the central
// internal/config multimodal knobs (AURA_VISION_CLOUD + the upstream-named
// MULTIMODAL_*/STT_*/TTS_* sidecar vars). It is populated by the composition root
// (plan 13-09) and passed to the media clients; keeping it local frees the
// telegram package from an internal/config import (the established config.go
// pattern). Zero values are sensible: an empty base URL means the corresponding
// modality is unconfigured (the handler degrades, never panics).
type MultimodalConfig struct {
	// VisionCloud routes image understanding: false (default) → the local
	// aura-ocr-vl sidecar; true → OpenRouter cloud vision. One env branch, zero
	// code dup (#60 / Pitfall 6).
	VisionCloud bool

	// Model is the primary LLM id (config.Model). photo.go reads SupportsVision on
	// it to decide whether the cloud branch attaches the image to the primary turn
	// or falls back to FallbackModel.
	Model string

	// MultimodalBaseURL/Model are the local vision sidecar (aura-ocr-vl) base +
	// model id. FallbackModel is the cloud vision model used when VisionCloud is
	// true and the primary Model lacks SupportsVision.
	MultimodalBaseURL string
	MultimodalModel   string
	FallbackModel     string

	// OpenRouterBaseURL/APIKey are the cloud vision endpoint (VisionCloud=true).
	// The key is set ONLY on the Authorization header at request-build time, never
	// logged or serialized (the openai_compat D-28 discipline).
	OpenRouterBaseURL string
	OpenRouterAPIKey  string

	// STTBaseURL/Model are the speech-to-text sidecar (aura-stt, faster-whisper).
	// STTLanguage pins the transcription language ("it"); empty = whisper
	// auto-detect, which mis-detects short clips (spike-027: probe used language=it).
	STTBaseURL  string
	STTModel    string
	STTLanguage string

	// TTSBaseURL/Voice/Format are the text-to-speech sidecar (aura-tts, Kokoro).
	// TTSCaption is the (optional) ASCII-safe caption put on the voice note.
	TTSBaseURL string
	TTSVoice   string
	TTSFormat  string
	TTSCaption string

	// DocumentsBaseURL is the markitdown /convert base.
	DocumentsBaseURL string

	// RetryBackoff is the per-retry sleep schedule in MILLISECONDS (voice.go: the
	// PRD 1s/2s default when nil). Exposed so tests pin a fast schedule.
	RetryBackoff []int

	// TimeoutSec bounds each sidecar request (T-13-08-SidecarDoS, 30s default when
	// zero). The timeout rides the request ctx, not http.Client.Timeout, so a
	// healthy slow body is not aborted mid-read.
	TimeoutSec int
}

// defaultSidecarTimeoutSec is the per-request ceiling (T-13-08-SidecarDoS). 30s
// matches the PRD sync ceiling and is generous for the CPU sidecars.
const defaultSidecarTimeoutSec = 30

// connectTimeoutSec bounds the TCP dial (the connect leg only — the total
// request budget is the ctx deadline).
const connectTimeoutSec = 10

// newSidecarHTTPClient builds the shared HTTP client for the media sidecars. It
// mirrors openai_compat.New: a connect timeout on the dialer, NO
// http.Client.Timeout (the deadline rides the request ctx), and DisableKeepAlives
// so a pooled conn never lingers past the request and trips the package goleak.
func newSidecarHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: connectTimeoutSec * time.Second,
			}).DialContext,
			DisableKeepAlives: true,
		},
	}
}

// requestTimeout resolves the per-request ctx ceiling from the config (30s
// default).
func (c MultimodalConfig) requestTimeout() time.Duration {
	sec := c.TimeoutSec
	if sec <= 0 {
		sec = defaultSidecarTimeoutSec
	}
	return time.Duration(sec) * time.Second
}

// withTimeout derives a request-scoped ctx carrying the sidecar timeout. The
// caller MUST call the returned cancel (defer) so the timer is released — an
// undrained timer goroutine would trip goleak.
func (c MultimodalConfig) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.requestTimeout())
}

// sidecarStatusError wraps a non-2xx sidecar response into a typed error so the
// retry loop (voice.go) and the handlers classify on a status code, never a
// string. It never carries the request body (no secret leak).
type sidecarStatusError struct {
	endpoint   string
	statusCode int
}

func (e *sidecarStatusError) Error() string {
	return fmt.Sprintf("telegram sidecar %s: HTTP %d", e.endpoint, e.statusCode)
}
