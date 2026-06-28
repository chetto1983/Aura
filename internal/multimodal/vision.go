package multimodal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
)

// VisionConfig selects the image-analysis route. VisionCloud=false (default) uses
// the local aura-ocr-vl sidecar (LocalBaseURL/LocalModel, no auth);
// VisionCloud=true routes to OpenRouter — the PRIMARY Model when it
// SupportsVision, else FallbackModel, so an image never reaches a non-vision
// model (T-13-08-VisionMisroute). Both arms speak OpenAI /chat/completions, so
// the base already carries any version segment and only "/chat/completions" is
// appended (no /v1 doubling).
type VisionConfig struct {
	VisionCloud       bool
	Model             string // primary LLM id, checked against SupportsVision
	LocalBaseURL      string
	LocalModel        string
	FallbackModel     string
	OpenRouterBaseURL string
	OpenRouterAPIKey  string
	TimeoutSec        int
	HTTPClient        *http.Client // optional; nil → a fresh shared client
}

// VisionClient POSTs an image + prompt to the chosen vision endpoint and returns
// the model's text description.
type VisionClient struct {
	cfg        VisionConfig
	httpClient *http.Client
}

// NewVisionClient builds a vision client over the config.
func NewVisionClient(cfg VisionConfig) *VisionClient {
	return &VisionClient{cfg: cfg, httpClient: resolveClient(cfg.HTTPClient)}
}

type visionChatRequest struct {
	Model    string              `json:"model"`
	Messages []visionChatMessage `json:"messages"`
}

type visionChatMessage struct {
	Role    string              `json:"role"`
	Content []visionContentPart `json:"content"`
}

type visionContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *visionImageURL `json:"image_url,omitempty"`
}

type visionImageURL struct {
	URL string `json:"url"`
}

type visionChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Describe sends imageBytes (already at whatever resolution the caller wants — the
// asset pipeline downscales first; telegram passes the photo as-is) with mimeType
// and prompt to the vision route, returning the description text. An empty base
// URL is a configuration error; a non-2xx response is a *StatusError.
func (c *VisionClient) Describe(ctx context.Context, imageBytes []byte, mimeType, prompt string) (string, error) {
	baseURL, apiKey, model := c.route()
	if baseURL == "" {
		return "", fmt.Errorf("vision base URL is not configured")
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageBytes)
	body, err := json.Marshal(visionChatRequest{
		Model: model,
		Messages: []visionChatMessage{{
			Role: "user",
			Content: []visionContentPart{
				{Type: "text", Text: prompt},
				{Type: "image_url", ImageURL: &visionImageURL{URL: dataURL}},
			},
		}},
	})
	if err != nil {
		return "", err
	}

	reqCtx, cancel := TimeoutContext(ctx, c.cfg.TimeoutSec)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return "", &StatusError{Endpoint: "vision", StatusCode: resp.StatusCode}
	}
	var decoded visionChatResponse
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("vision: decode: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("vision: empty choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

// VisionModel reports the model id the route would use, so callers can record it
// in result metadata without issuing a request.
func (c *VisionClient) VisionModel() string {
	_, _, model := c.route()
	return model
}

// route is the SINGLE config-only vision branch (Pitfall 6 / #60).
func (c *VisionClient) route() (baseURL, apiKey, model string) {
	if c.cfg.VisionCloud {
		m := c.cfg.Model
		if !llm.SupportsVision(m) {
			m = c.cfg.FallbackModel
		}
		return c.cfg.OpenRouterBaseURL, c.cfg.OpenRouterAPIKey, m
	}
	return c.cfg.LocalBaseURL, "", c.cfg.LocalModel
}
