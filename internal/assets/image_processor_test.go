package assets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
)

func TestImageProcessorReadsObjectAndCallsVisionSidecar(t *testing.T) {
	objects := objectstore.NewFake()
	ref := objectstore.ObjectRef{Bucket: "b", Key: "image/original"}
	imageBytes := []byte("\x89PNGfake-image")
	if _, err := objects.Put(context.Background(), ref, strings.NewReader(string(imageBytes)), objectstore.PutOptions{MIMEType: "image/png", Size: int64(len(imageBytes))}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var gotModel, gotPrompt, gotDataURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("vision method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("vision path = %s, want .../chat/completions", r.URL.Path)
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("vision decode: %v", err)
		}
		gotModel = body.Model
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" {
			t.Fatalf("vision messages = %+v, want one user message", body.Messages)
		}
		for _, part := range body.Messages[0].Content {
			switch part.Type {
			case "text":
				gotPrompt = part.Text
			case "image_url":
				gotDataURL = part.ImageURL.URL
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"control panel with gauges"}}]}`)
	}))
	defer srv.Close()

	result, err := NewImageProcessor(objects, multimodal.VisionConfig{
		LocalBaseURL: srv.URL,
		LocalModel:   "glm-ocr",
	}).ProcessAsset(context.Background(), Asset{
		ID:           "asset-1",
		Modality:     ModalityImage,
		ObjectBucket: ref.Bucket,
		ObjectKey:    ref.Key,
		FileName:     "panel.png",
		MIMEType:     "image/png",
	})
	if err != nil {
		t.Fatalf("ProcessAsset: %v", err)
	}

	if result.Status != StatusComplete || result.Summary != "control panel with gauges" {
		t.Fatalf("result = %+v, want complete vision summary", result)
	}
	if gotModel != "glm-ocr" {
		t.Fatalf("vision model = %q, want glm-ocr", gotModel)
	}
	if gotPrompt != "Describe this image." {
		t.Fatalf("vision prompt = %q, want default image prompt", gotPrompt)
	}
	wantDataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)
	if gotDataURL != wantDataURL {
		t.Fatalf("vision image_url = %q, want object bytes data URL", gotDataURL)
	}
}
