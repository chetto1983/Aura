package assets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/objectstore"
	xdraw "golang.org/x/image/draw"
)

type ImageProcessor struct {
	Objects    objectstore.Store
	Config     VisionConfig
	HTTPClient *http.Client
}

func NewImageProcessor(objects objectstore.Store, cfg VisionConfig) *ImageProcessor {
	return &ImageProcessor{Objects: objects, Config: cfg, HTTPClient: sidecarHTTPClient()}
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

func (p *ImageProcessor) ProcessAsset(ctx context.Context, asset Asset) (Result, error) {
	if p == nil || p.Objects == nil {
		return Result{}, fmt.Errorf("image processor is not configured")
	}
	baseURL, apiKey, model := p.route()
	if baseURL == "" {
		return Result{}, fmt.Errorf("image processor base URL is not configured")
	}
	ref := objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey}
	rc, attrs, err := p.Objects.Get(ctx, ref)
	if err != nil {
		return Result{}, err
	}
	defer rc.Close()
	imageBytes, err := io.ReadAll(rc)
	if err != nil {
		return Result{}, err
	}
	mimeType := asset.MIMEType
	if mimeType == "" {
		mimeType = attrs.MIMEType
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if ds, dsMime := downscaleAssetForVision(imageBytes); dsMime != "" {
		imageBytes, mimeType = ds, dsMime
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageBytes)
	body, err := json.Marshal(visionChatRequest{
		Model: model,
		Messages: []visionChatMessage{{
			Role: "user",
			Content: []visionContentPart{
				{Type: "text", Text: "Describe this image."},
				{Type: "image_url", ImageURL: &visionImageURL{URL: dataURL}},
			},
		}},
	})
	if err != nil {
		return Result{}, err
	}

	reqCtx, cancel := timeoutContext(ctx, p.Config.TimeoutSec)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return Result{}, &sidecarStatusError{endpoint: "vision", statusCode: resp.StatusCode}
	}
	var decoded visionChatResponse
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Result{}, fmt.Errorf("asset vision: decode: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return Result{}, fmt.Errorf("asset vision: empty choices")
	}
	summary := decoded.Choices[0].Message.Content
	return Result{
		Status:  StatusComplete,
		Summary: summary,
		Metadata: map[string]any{
			"vision_model": model,
		},
	}, nil
}

func (p *ImageProcessor) route() (baseURL, apiKey, model string) {
	if p.Config.VisionCloud {
		model := p.Config.Model
		if !llm.SupportsVision(model) {
			model = p.Config.FallbackModel
		}
		return p.Config.OpenRouterBaseURL, p.Config.OpenRouterAPIKey, model
	}
	return p.Config.MultimodalBaseURL, "", p.Config.MultimodalModel
}

func (p *ImageProcessor) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return sidecarHTTPClient()
}

const (
	assetVisionMaxEdge     = 1024
	assetVisionJPEGQuality = 85
)

func downscaleAssetForVision(raw []byte) (out []byte, mime string) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw, ""
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= assetVisionMaxEdge && h <= assetVisionMaxEdge {
		return raw, ""
	}
	nw, nh := assetVisionMaxEdge, assetVisionMaxEdge
	if w >= h {
		nh = h * assetVisionMaxEdge / w
	} else {
		nw = w * assetVisionMaxEdge / h
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: assetVisionJPEGQuality}); err != nil {
		return raw, ""
	}
	return buf.Bytes(), "image/jpeg"
}
