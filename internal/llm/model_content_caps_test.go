package llm

import (
	"context"
	"net/http"
	"testing"
)

// TestLlamaCppPropsVideoKey pins what spike 106 measured on llama.cpp b10951 with
// ggml-org/Qwen3-VL-2B-Instruct-GGUF loaded: GET /props answers this exact object, so
// the key that declares video input is literally "video" and llamaCppModalities needs
// no rename beside vision→image. It is the capability half of the video path — the
// wire half is openai_compat.videoContentPart — and the negative row is the control
// the spike itself noted was missing: an image-only build must leave video unsupported,
// which is what keeps a clip from being sent to a model that cannot decode it.
//
// It lives beside llamaCppModalities rather than in model_reasoning_caps_test.go
// (already 592 LOC) to stay under the 600-LOC cap.
func TestLlamaCppPropsVideoKey(t *testing.T) {
	for _, tc := range []struct {
		name      string
		props     string
		wantVideo bool
	}{
		{"video model", `{"modalities":{"vision":true,"video":true,"audio":false}}`, true},
		{"image-only model", `{"modalities":{"vision":true,"audio":false}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newLlamaCppContentCaps(Config{Provider: "llamacpp", BaseURL: "http://localhost:8080/v1"})
			src.httpClient = &http.Client{Transport: &fakeRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/props" {
					t.Errorf("path = %s, want /props", r.URL.Path)
				}
				return jsonResponse(http.StatusOK, []byte(tc.props)), nil
			}}}

			caps, detected := src.ContentCapabilities(context.Background())
			if !detected {
				t.Fatal("llama.cpp /props modalities were not detected")
			}
			if !caps.Modalities["image"] {
				t.Fatalf("vision must normalize to image, got %v", caps.Modalities)
			}
			if caps.Modalities["video"] != tc.wantVideo {
				t.Fatalf("video modality = %v, want %v (%v)", caps.Modalities["video"], tc.wantVideo, caps.Modalities)
			}
			if got := caps.SupportsMIME("video/mp4"); got != tc.wantVideo {
				t.Fatalf("SupportsMIME(video/mp4) = %v, want %v", got, tc.wantVideo)
			}
			if !caps.SupportsMIME("image/png") {
				t.Fatal("an image is native media on every llama.cpp vision build")
			}
		})
	}
}
