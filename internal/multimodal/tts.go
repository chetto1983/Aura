package multimodal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// TTSConfig selects the text-to-speech route. With CloudModel unset (default) the
// text is POSTed to the local aura-tts (Kokoro) sidecar at
// LocalBaseURL+"/audio/speech" (no model id — Kokoro is single-model and selects
// the voice via the Voice field, no auth). Set CloudModel to swap to OpenRouter's
// /audio/speech with the shared Bearer key + the cloud model id. Both arms speak
// OpenAI /audio/speech, so the base already carries any version segment and only
// "/audio/speech" is appended (no /v1 doubling).
type TTSConfig struct {
	LocalBaseURL      string
	Voice             string
	Format            string // response_format (e.g. "opus"); defaults to "opus"
	CloudModel        string
	OpenRouterBaseURL string
	OpenRouterAPIKey  string
	TimeoutSec        int
	HTTPClient        *http.Client // optional; nil → a fresh shared client
}

// TTSClient synthesizes speech audio from text.
type TTSClient struct {
	cfg        TTSConfig
	httpClient *http.Client
}

// NewTTSClient builds a TTS client over the config.
func NewTTSClient(cfg TTSConfig) *TTSClient {
	return &TTSClient{cfg: cfg, httpClient: resolveClient(cfg.HTTPClient)}
}

// AudioFormat returns the configured response_format (the container of the bytes
// Synthesize returns), defaulting to "opus" so callers tag the voice note MIME.
func (c *TTSClient) AudioFormat() string {
	if c.cfg.Format == "" {
		return "opus"
	}
	return c.cfg.Format
}

// ttsRequest is the OpenAI /audio/speech body. Model is omitempty so the local
// (no-model) route sends none.
type ttsRequest struct {
	Model          string `json:"model,omitempty"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

// Synthesize POSTs text to the chosen /audio/speech endpoint and returns the raw
// audio bytes (the AudioFormat container). A non-2xx response is a *StatusError.
func (c *TTSClient) Synthesize(ctx context.Context, text string) ([]byte, error) {
	reqCtx, cancel := TimeoutContext(ctx, c.cfg.TimeoutSec)
	defer cancel()

	baseURL, apiKey, model := c.route()
	body, err := json.Marshal(ttsRequest{
		Model:          model,
		Input:          text,
		Voice:          c.cfg.Voice,
		ResponseFormat: c.AudioFormat(),
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		baseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, &StatusError{Endpoint: "tts", StatusCode: resp.StatusCode}
	}
	return io.ReadAll(resp.Body)
}

// route is the SINGLE config-only TTS branch. Cloud (CloudModel set) → OpenRouter
// with the shared key + model; local → the aura-tts sidecar, no key, no model.
func (c *TTSClient) route() (baseURL, apiKey, model string) {
	if c.cfg.CloudModel != "" {
		return c.cfg.OpenRouterBaseURL, c.cfg.OpenRouterAPIKey, c.cfg.CloudModel
	}
	return c.cfg.LocalBaseURL, "", ""
}
