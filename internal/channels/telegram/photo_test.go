package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// visionHandler builds an httptest handler that asserts the /chat/completions
// image_url contract, records that it was hit, and returns the canned description.
func visionHandler(t *testing.T, hits *atomic.Int32, wantModel, description string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("vision path = %s, want .../chat/completions", r.URL.Path)
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content []struct {
					Type     string `json:"type"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("vision decode body: %v", err)
		}
		if wantModel != "" && body.Model != wantModel {
			t.Errorf("vision model = %q, want %q", body.Model, wantModel)
		}
		var sawImage bool
		for _, m := range body.Messages {
			for _, c := range m.Content {
				if c.Type == "image_url" && strings.HasPrefix(c.ImageURL.URL, "data:image/") {
					sawImage = true
				}
			}
		}
		if !sawImage {
			t.Error("vision request must carry a base64 data: image_url")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`+description+`"}}]}`)
	}
}

// TestPhotoLocalBranch proves AURA_VISION_CLOUD=false routes to the local
// aura-ocr-vl sidecar (sidecar hit, cloud NOT) — the unchanged default (#60).
func TestPhotoLocalBranch(t *testing.T) {
	t.Parallel()
	var localHits, cloudHits atomic.Int32
	local := httptest.NewServer(visionHandler(t, &localHits, "glm-ocr", "testo locale"))
	defer local.Close()
	cloud := httptest.NewServer(visionHandler(t, &cloudHits, "", "cloud"))
	defer cloud.Close()

	cfg := MultimodalConfig{
		VisionCloud:       false,
		Model:             "deepseek/deepseek-v4-flash",
		MultimodalBaseURL: local.URL,
		MultimodalModel:   "glm-ocr",
		OpenRouterBaseURL: cloud.URL,
		FallbackModel:     "minimax/minimax-m3",
	}
	pc := newPhotoClient(cfg)

	got, err := pc.Describe(context.Background(), []byte("\x89PNGfake"), "image/png", "che c'è qui?")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got != "testo locale" {
		t.Errorf("description = %q, want %q", got, "testo locale")
	}
	if localHits.Load() != 1 {
		t.Errorf("local sidecar hits = %d, want 1", localHits.Load())
	}
	if cloudHits.Load() != 0 {
		t.Errorf("cloud must NOT be hit on the local branch, got %d", cloudHits.Load())
	}
}

// TestPhotoCloudBranchVisionModel proves AURA_VISION_CLOUD=true with a
// vision-capable primary model routes to OpenRouter using the PRIMARY model
// (SupportsVision gates the cloud route) — sidecar NOT hit.
func TestPhotoCloudBranchVisionModel(t *testing.T) {
	t.Parallel()
	var localHits, cloudHits atomic.Int32
	local := httptest.NewServer(visionHandler(t, &localHits, "", "local"))
	defer local.Close()
	cloud := httptest.NewServer(visionHandler(t, &cloudHits, "minimax/minimax-m3", "cloud desc"))
	defer cloud.Close()

	cfg := MultimodalConfig{
		VisionCloud:       true,
		Model:             "minimax/minimax-m3", // SupportsVision == true
		MultimodalBaseURL: local.URL,
		OpenRouterBaseURL: cloud.URL,
		FallbackModel:     "minimax/minimax-m3",
	}
	pc := newPhotoClient(cfg)

	got, err := pc.Describe(context.Background(), []byte("\x89PNGfake"), "image/png", "descrivi")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got != "cloud desc" {
		t.Errorf("description = %q, want %q", got, "cloud desc")
	}
	if cloudHits.Load() != 1 {
		t.Errorf("cloud hits = %d, want 1", cloudHits.Load())
	}
	if localHits.Load() != 0 {
		t.Errorf("local sidecar must NOT be hit on the cloud branch, got %d", localHits.Load())
	}
}

// TestPhotoCloudBranchFallbackModel proves AURA_VISION_CLOUD=true with a
// NON-vision primary model falls back to MULTIMODAL_FALLBACK_MODEL rather than
// sending an image to a non-vision model (T-13-08-VisionMisroute).
func TestPhotoCloudBranchFallbackModel(t *testing.T) {
	t.Parallel()
	var cloudHits atomic.Int32
	cloud := httptest.NewServer(visionHandler(t, &cloudHits, "minimax/minimax-m3", "fallback desc"))
	defer cloud.Close()

	cfg := MultimodalConfig{
		VisionCloud:       true,
		Model:             "deepseek/deepseek-v4-flash", // SupportsVision == false → fallback
		OpenRouterBaseURL: cloud.URL,
		FallbackModel:     "minimax/minimax-m3",
	}
	pc := newPhotoClient(cfg)

	got, err := pc.Describe(context.Background(), []byte("\x89PNGfake"), "image/png", "descrivi")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got != "fallback desc" {
		t.Errorf("description = %q, want %q", got, "fallback desc")
	}
	if cloudHits.Load() != 1 {
		t.Errorf("cloud (fallback model) hits = %d, want 1", cloudHits.Load())
	}
}

// TestPhotoSidecarError proves a 5xx from the chosen route surfaces an error.
func TestPhotoSidecarError(t *testing.T) {
	t.Parallel()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer local.Close()

	cfg := MultimodalConfig{VisionCloud: false, Model: "deepseek/deepseek-v4-flash", MultimodalBaseURL: local.URL, MultimodalModel: "glm-ocr"}
	pc := newPhotoClient(cfg)

	if _, err := pc.Describe(context.Background(), []byte("img"), "image/png", "x"); err == nil {
		t.Fatal("a sidecar 5xx must surface an error")
	}
}
