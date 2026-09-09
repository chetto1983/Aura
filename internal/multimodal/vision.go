package multimodal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrNoVisionRoute reports that nothing in this configuration can read an image:
// the operator-selected model does not accept the image modality and no local
// sidecar is configured. It is deliberately distinct from an unreachable endpoint,
// because the two want opposite handling — a caller degrades on this and retries on
// that, and degrading on a transient outage would cache a blank answer.
var ErrNoVisionRoute = errors.New("vision: no route accepts images")

// VisionConfig selects the image-analysis route from what the models can actually
// do, not from a switch. PrimaryAcceptsImages is the resolved answer of the model's
// own card or capability probe (llm.ContentCapabilitySource): when it is true the
// operator-selected Model reads the image at PrimaryBaseURL, otherwise the local
// aura-ocr-vl sidecar (LocalBaseURL/LocalModel, no auth) does.
//
// It is resolved upstream, not here, so route() stays a pure config branch that
// issues no I/O (Pitfall 6 / #60). Both arms speak OpenAI /chat/completions, so the
// base already carries any version segment and only "/chat/completions" is appended
// (no /v1 doubling).
type VisionConfig struct {
	PrimaryAcceptsImages bool
	Model                string
	LocalBaseURL         string
	LocalModel           string
	PrimaryBaseURL       string
	PrimaryAPIKey        string
	TimeoutSec           int
	HTTPClient           *http.Client // optional; nil → a fresh shared client
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
	baseURL, apiKey, model, ok := c.route()
	if !ok {
		return "", ErrNoVisionRoute
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
	_, _, model, _ := c.route()
	return model
}

// route is the SINGLE config-only vision branch (Pitfall 6 / #60). ok is false when
// neither arm can see an image, which is ErrNoVisionRoute rather than a failed call.
func (c *VisionClient) route() (baseURL, apiKey, model string, ok bool) {
	if c.cfg.PrimaryAcceptsImages && c.cfg.PrimaryBaseURL != "" {
		return c.cfg.PrimaryBaseURL, c.cfg.PrimaryAPIKey, c.cfg.Model, true
	}
	if c.cfg.LocalBaseURL != "" {
		return c.cfg.LocalBaseURL, "", c.cfg.LocalModel, true
	}
	return "", "", "", false
}
