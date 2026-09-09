package multimodal

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

type fakeCaps struct {
	caps llm.ProviderContentCapabilities
	ok   bool
}

func (f fakeCaps) ContentCapabilities(context.Context) (llm.ProviderContentCapabilities, bool) {
	return f.caps, f.ok
}

func TestPrimaryAcceptsImagesReadsTheImageModality(t *testing.T) {
	source := fakeCaps{caps: llm.ProviderContentCapabilities{
		Modalities: map[string]bool{"text": true, "image": true, "video": true},
	}, ok: true}
	if !PrimaryAcceptsImages(t.Context(), source) {
		t.Error("PrimaryAcceptsImages = false, want true for a card advertising image")
	}
}

func TestPrimaryRejectsImagesWhenCardIsTextOnly(t *testing.T) {
	source := fakeCaps{caps: llm.ProviderContentCapabilities{
		Modalities: map[string]bool{"text": true},
	}, ok: true}
	if PrimaryAcceptsImages(t.Context(), source) {
		t.Error("PrimaryAcceptsImages = true, want false for a text-only card")
	}
}

func TestPrimaryRejectsImagesWhenCapabilityIsUnknown(t *testing.T) {
	// An unknown backend or a failed probe must not be read as permission: the
	// sidecar keeps the job rather than sending an image to a model that may refuse it.
	if PrimaryAcceptsImages(t.Context(), fakeCaps{ok: false}) {
		t.Error("PrimaryAcceptsImages = true, want false when the source cannot answer")
	}
	if PrimaryAcceptsImages(t.Context(), nil) {
		t.Error("PrimaryAcceptsImages(nil) = true, want false")
	}
}

func TestVisionConfigFromCarriesPrimaryRouteAndCapability(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Model = "gemma4:31b-cloud"
	cfg.LLM.BaseURL = "http://host.docker.internal:11434/v1"
	cfg.LLM.APIKey = "k"
	cfg.MultimodalBaseURL = "http://aura-ocr-vl:8082/v1"
	cfg.MultimodalModel = "glm-ocr"

	got := VisionConfigFrom(cfg, true)
	if !got.PrimaryAcceptsImages {
		t.Error("PrimaryAcceptsImages = false, want the resolved capability carried through")
	}
	if got.PrimaryBaseURL != cfg.LLM.BaseURL || got.PrimaryAPIKey != "k" || got.Model != "gemma4:31b-cloud" {
		t.Errorf("primary route = %q/%q/%q, want the operator-selected model's own endpoint",
			got.PrimaryBaseURL, got.PrimaryAPIKey, got.Model)
	}
	if got.LocalBaseURL != cfg.MultimodalBaseURL || got.LocalModel != "glm-ocr" {
		t.Errorf("sidecar route = %q/%q, want it preserved as the fallback", got.LocalBaseURL, got.LocalModel)
	}
}
