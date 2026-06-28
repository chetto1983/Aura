// Package telegram — this file is the image-understanding sidecar client (UX-04).
// It is a thin wrapper over the shared multimodal.VisionClient (ONE config-only
// `VisionCloud` branch lives there — Pitfall 6 / #60): the UNCHANGED default
// (AURA_VISION_CLOUD=false) routes to the local aura-ocr-vl sidecar (GLM-OCR);
// AURA_VISION_CLOUD=true routes to OpenRouter cloud vision (the primary model when
// it SupportsVision, else MULTIMODAL_FALLBACK_MODEL, so an image never reaches a
// non-vision model — T-13-08-VisionMisroute). photo.go owns only the telegram glue:
// the per-photo downscale (a full-res phone photo otherwise blows the CPU sidecar
// timeout) and the empty-caption default. The description/OCR text becomes a text
// user message fed to runner.Turn (handler, 13-09).
package telegram

import (
	"context"

	"github.com/chetto1983/aura/internal/multimodal"
)

// photoClient wraps the shared vision client with the telegram-specific
// downscale + caption-default glue. Zero Go ML; the only runtime decision (the
// VisionCloud branch) lives inside multimodal.VisionClient.
type photoClient struct {
	vision *multimodal.VisionClient
}

// newPhotoClient builds a vision client over the multimodal config.
func newPhotoClient(cfg MultimodalConfig) *photoClient {
	return &photoClient{vision: multimodal.NewVisionClient(multimodal.VisionConfig{
		VisionCloud:       cfg.VisionCloud,
		Model:             cfg.Model,
		LocalBaseURL:      cfg.MultimodalBaseURL,
		LocalModel:        cfg.MultimodalModel,
		FallbackModel:     cfg.FallbackModel,
		OpenRouterBaseURL: cfg.OpenRouterBaseURL,
		OpenRouterAPIKey:  cfg.OpenRouterAPIKey,
		TimeoutSec:        cfg.TimeoutSec,
	})}
}

// Describe downscales an oversized photo, defaults an empty caption, and delegates
// to the shared vision client. The route (local aura-ocr-vl vs OpenRouter) is the
// SINGLE VisionCloud branch inside multimodal.VisionClient. The caption may be
// empty (a bare image).
func (p *photoClient) Describe(ctx context.Context, image []byte, mimeType, caption string) (string, error) {
	// Downscale oversized photos so the CPU vision sidecar stays in spike-range
	// latency (a full-res phone photo otherwise takes ~55s and blows the timeout).
	imgBytes := image
	if ds, dsMime := downscaleForVision(image); dsMime != "" {
		imgBytes, mimeType = ds, dsMime
	}
	prompt := caption
	if prompt == "" {
		prompt = "Describe this image."
	}
	return p.vision.Describe(ctx, imgBytes, mimeType, prompt)
}
