package multimodal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
)

// ErrNoVisionRoute reports that nothing in this configuration can read an image:
// the operator-selected model does not accept the image modality and no local
// sidecar is configured. It is deliberately distinct from an unreachable endpoint,
// because the two want opposite handling — a caller degrades on this and retries on
// that, and degrading on a transient outage would cache a blank answer.
var ErrNoVisionRoute = errors.New("vision: no route accepts images")

// ImageReader reads one image with the operator-selected model through that model's own
// client, for a route the HTTP primary arm cannot speak: the ChatGPT plan, whose token only
// the Responses API accepts. A wrapped llm.ErrMandatoryModalityUnavailable says the image
// cannot be read there; any other error is a failure.
type ImageReader interface {
	Describe(ctx context.Context, image []byte, mimeType, prompt string) (string, error)
}

// VisionConfig selects the image-analysis route from what the models can actually
// do, not from a switch. PrimaryReader, when set, is the operator-selected Model reading
// the image itself, and the HTTP primary arm is not used. Otherwise PrimaryAcceptsImages is
// the resolved answer of the model's own card or capability probe
// (llm.ContentCapabilitySource): when it is true the operator-selected Model reads the image
// at PrimaryBaseURL with PrimaryKey's answer. The local aura-ocr-vl sidecar
// (LocalBaseURL/LocalModel, no auth) takes what the primary cannot read.
//
// It is resolved upstream, not here, so route() stays a pure config branch that
// issues no I/O (Pitfall 6 / #60). Both HTTP arms speak OpenAI /chat/completions, so the
// base already carries any version segment and only "/chat/completions" is appended
// (no /v1 doubling).
type VisionConfig struct {
	PrimaryReader        ImageReader
	PrimaryAcceptsImages bool
	Model                string
	LocalBaseURL         string
	LocalModel           string
	PrimaryBaseURL       string
	PrimaryKey           KeyFunc
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
// asset pipeline downscales first) with mimeType and prompt to the vision route, and
// returns the description and the model that wrote it. No arm able to read the image is
// ErrNoVisionRoute; a non-2xx response is a *StatusError.
func (c *VisionClient) Describe(ctx context.Context, imageBytes []byte, mimeType, prompt string) (text, model string, err error) {
	reqCtx, cancel := TimeoutContext(ctx, c.cfg.TimeoutSec)
	defer cancel()
	if c.cfg.PrimaryReader != nil {
		text, err = c.cfg.PrimaryReader.Describe(reqCtx, imageBytes, mimeType, prompt)
		if err == nil {
			return text, c.cfg.Model, nil
		}
		// A transient failure of the chosen model retries; it never hands the document
		// to another model.
		if !errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
			return "", "", err
		}
		if c.cfg.LocalBaseURL == "" {
			return "", "", fmt.Errorf("%w: %w", ErrNoVisionRoute, err)
		}
		return c.post(reqCtx, c.cfg.LocalBaseURL, nil, c.cfg.LocalModel, imageBytes, mimeType, prompt)
	}
	baseURL, keyFunc, model, ok := c.route()
	if !ok {
		return "", "", ErrNoVisionRoute
	}
	return c.post(reqCtx, baseURL, keyFunc, model, imageBytes, mimeType, prompt)
}

// post is one OpenAI /chat/completions vision request, the shape both HTTP arms speak.
func (c *VisionClient) post(ctx context.Context, baseURL string, keyFunc KeyFunc, model string, imageBytes []byte, mimeType, prompt string) (string, string, error) {
	apiKey, err := readKey(ctx, keyFunc)
	if err != nil {
		return "", "", fmt.Errorf("vision: %w", err)
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
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return "", "", &StatusError{Endpoint: "vision", StatusCode: resp.StatusCode}
	}
	var decoded visionChatResponse
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", "", fmt.Errorf("vision: decode: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", "", fmt.Errorf("vision: empty choices")
	}
	return decoded.Choices[0].Message.Content, model, nil
}

// route is the SINGLE config-only vision branch (Pitfall 6 / #60). ok is false when
// neither arm can see an image, which is ErrNoVisionRoute rather than a failed call. It
// names the key's reader, not the key: reading it is I/O, done with the request.
func (c *VisionClient) route() (baseURL string, key KeyFunc, model string, ok bool) {
	if c.cfg.PrimaryAcceptsImages && c.cfg.PrimaryBaseURL != "" {
		return c.cfg.PrimaryBaseURL, c.cfg.PrimaryKey, c.cfg.Model, true
	}
	if c.cfg.LocalBaseURL != "" {
		return c.cfg.LocalBaseURL, nil, c.cfg.LocalModel, true
	}
	return "", nil, "", false
}
