// Package telegram — this file is the image-understanding sidecar client (UX-04).
// It is ONE function with ONE `if cfg.VisionCloud` branch (Pitfall 6 / #60): the
// switch is config-only, zero code dup. The UNCHANGED default
// (AURA_VISION_CLOUD=false) routes to the local aura-ocr-vl sidecar (GLM-OCR);
// AURA_VISION_CLOUD=true routes to OpenRouter cloud vision, using the primary
// model when it SupportsVision and falling back to MULTIMODAL_FALLBACK_MODEL
// (minimax-m3) otherwise so an image is never sent to a non-vision model
// (T-13-08-VisionMisroute). Both arms speak the same OpenAI /chat/completions
// image_url shape and return the description/OCR text, which becomes a text user
// message fed to runner.Turn (handler, 13-09).
package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
)

// photoClient POSTs an image to either the local vision sidecar or OpenRouter and
// returns the description. Thin OpenAI-compat client (zero Go ML); the only
// runtime decision is the single VisionCloud branch.
type photoClient struct {
	cfg        MultimodalConfig
	httpClient *http.Client
}

// newPhotoClient builds a vision client over the multimodal config.
func newPhotoClient(cfg MultimodalConfig) *photoClient {
	return &photoClient{cfg: cfg, httpClient: newSidecarHTTPClient()}
}

// chatRequest is the minimal OpenAI /chat/completions body carrying a single user
// message with a text part + a base64 image_url part — the shape both the local
// GLM-OCR sidecar and OpenRouter cloud vision accept.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// chatResponse is the minimal /chat/completions response — the first choice's
// message content is the description/OCR text.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Describe sends the image (+ the user's caption as the prompt) to the configured
// vision route and returns the description/OCR text. The route is decided by the
// SINGLE VisionCloud branch: false → local aura-ocr-vl; true → OpenRouter with the
// SupportsVision-gated model selection. The caption may be empty (a bare image).
func (p *photoClient) Describe(ctx context.Context, image []byte, mimeType, caption string) (string, error) {
	baseURL, apiKey, model := p.route()

	// Downscale oversized photos so the CPU vision sidecar stays in spike-range
	// latency (a full-res phone photo otherwise takes ~55s and blows the timeout).
	imgBytes := image
	if ds, dsMime := downscaleForVision(image); dsMime != "" {
		imgBytes, mimeType = ds, dsMime
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imgBytes)
	prompt := caption
	if prompt == "" {
		prompt = "Describe this image."
	}
	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{{
			Role: "user",
			Content: []contentPart{
				{Type: "text", Text: prompt},
				{Type: "image_url", ImageURL: &imageURL{URL: dataURL}},
			},
		}},
	})
	if err != nil {
		return "", err
	}

	reqCtx, cancel := p.cfg.withTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey) // cloud route only; set ONLY here (D-28)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return "", &sidecarStatusError{endpoint: "vision", statusCode: resp.StatusCode}
	}

	var decoded chatResponse
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("telegram vision: decode: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("telegram vision: empty choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

// route is the SINGLE config-only branch (Pitfall 6 / #60). It returns the
// (baseURL, apiKey, model) for the chosen vision path:
//
//   - VisionCloud=false (unchanged default): the local aura-ocr-vl sidecar, no key,
//     the local MULTIMODAL_MODEL.
//   - VisionCloud=true: OpenRouter — the PRIMARY model when it SupportsVision, else
//     MULTIMODAL_FALLBACK_MODEL (minimax-m3), so an image never reaches a
//     non-vision model (T-13-08-VisionMisroute).
func (p *photoClient) route() (baseURL, apiKey, model string) {
	if p.cfg.VisionCloud {
		model := p.cfg.Model
		if !llm.SupportsVision(model) {
			model = p.cfg.FallbackModel
		}
		return p.cfg.OpenRouterBaseURL, p.cfg.OpenRouterAPIKey, model
	}
	return p.cfg.MultimodalBaseURL, "", p.cfg.MultimodalModel
}
