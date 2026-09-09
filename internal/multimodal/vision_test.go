package multimodal

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeVisionReq(t *testing.T, r *http.Request) visionChatRequest {
	t.Helper()
	raw, _ := io.ReadAll(r.Body)
	var req visionChatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode vision request: %v", err)
	}
	return req
}

func TestVisionLocalRouteNoAuth(t *testing.T) {
	var gotAuth, gotPath, gotModel, gotDataURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		req := decodeVisionReq(t, r)
		gotModel = req.Model
		if len(req.Messages) == 1 && len(req.Messages[0].Content) == 2 && req.Messages[0].Content[1].ImageURL != nil {
			gotDataURL = req.Messages[0].Content[1].ImageURL.URL
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"a cat"}}]}`)
	}))
	defer srv.Close()

	c := NewVisionClient(VisionConfig{
		LocalBaseURL: srv.URL,
		LocalModel:   "local-vl",
		HTTPClient:   srv.Client(),
	})
	out, err := c.Describe(t.Context(), []byte("img"), "", "Describe this image.")
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if out != "a cat" {
		t.Fatalf("description = %q, want %q", out, "a cat")
	}
	if gotAuth != "" {
		t.Errorf("local route sent Authorization %q, want none", gotAuth)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions (no /v1 doubling)", gotPath)
	}
	if gotModel != "local-vl" {
		t.Errorf("model = %q, want local-vl", gotModel)
	}
	if !strings.HasPrefix(gotDataURL, "data:application/octet-stream;base64,") {
		t.Errorf("empty MIME data URL = %q, want application/octet-stream fallback", gotDataURL)
	}
}

func TestVisionPrefersPrimaryModelWhenItAcceptsImages(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotModel = decodeVisionReq(t, r).Model
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	// The sidecar is configured too: the operator-selected model wins anyway, so a
	// model that reports the image modality is used without anyone setting a switch.
	c := NewVisionClient(VisionConfig{
		PrimaryAcceptsImages: true,
		Model:                "gemma4:31b-cloud",
		PrimaryBaseURL:       srv.URL,
		PrimaryAPIKey:        "shared-key",
		LocalBaseURL:         "http://aura-ocr-vl:8082/v1",
		LocalModel:           "glm-ocr",
		HTTPClient:           srv.Client(),
	})
	if _, err := c.Describe(t.Context(), []byte("img"), "image/png", "x"); err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if gotAuth != "Bearer shared-key" {
		t.Errorf("Authorization = %q, want Bearer shared-key", gotAuth)
	}
	if gotModel != "gemma4:31b-cloud" {
		t.Errorf("vision model = %q, want the operator-selected primary model", gotModel)
	}
	if got := c.VisionModel(); got != gotModel {
		t.Errorf("VisionModel() = %q, want wire model %q", got, gotModel)
	}
}

func TestVisionFallsBackToSidecarWhenPrimaryRejectsImages(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotModel = decodeVisionReq(t, r).Model
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	// z-ai/glm-5.3 advertises text only while its :flash sibling takes images, which is
	// exactly why the route reads the model's card instead of a hand-set switch.
	c := NewVisionClient(VisionConfig{
		PrimaryAcceptsImages: false,
		Model:                "z-ai/glm-5.3",
		PrimaryBaseURL:       "https://openrouter.ai/api/v1",
		PrimaryAPIKey:        "shared-key",
		LocalBaseURL:         srv.URL,
		LocalModel:           "glm-ocr",
		HTTPClient:           srv.Client(),
	})
	if _, err := c.Describe(t.Context(), []byte("img"), "image/png", "x"); err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("sidecar Authorization = %q, want none", gotAuth)
	}
	if gotModel != "glm-ocr" {
		t.Errorf("vision model = %q, want the local sidecar model", gotModel)
	}
}

func TestVisionReportsNoRouteWhenNeitherArmCanSeeImages(t *testing.T) {
	// Structural absence, not a transient outage: the caller degrades on this instead
	// of retrying, so it must be distinguishable from an unreachable endpoint.
	c := NewVisionClient(VisionConfig{
		PrimaryAcceptsImages: false,
		Model:                "z-ai/glm-5.3",
		PrimaryBaseURL:       "https://openrouter.ai/api/v1",
		PrimaryAPIKey:        "shared-key",
	})
	_, err := c.Describe(t.Context(), []byte("img"), "image/png", "x")
	if !errors.Is(err, ErrNoVisionRoute) {
		t.Fatalf("err = %v, want ErrNoVisionRoute", err)
	}
}

func TestVisionEmptyConfigHasNoRoute(t *testing.T) {
	c := NewVisionClient(VisionConfig{})
	if _, err := c.Describe(t.Context(), []byte("x"), "image/png", "p"); !errors.Is(err, ErrNoVisionRoute) {
		t.Fatalf("err = %v, want ErrNoVisionRoute", err)
	}
}

func TestVisionNon2xxStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := NewVisionClient(VisionConfig{LocalBaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.Describe(t.Context(), []byte("x"), "image/png", "p")
	var se *StatusError
	if err == nil || !errors.As(err, &se) || se.StatusCode != http.StatusBadGateway {
		t.Fatalf("err = %v, want *StatusError 502", err)
	}
}

func TestVisionEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()
	c := NewVisionClient(VisionConfig{LocalBaseURL: srv.URL, HTTPClient: srv.Client()})
	if _, err := c.Describe(t.Context(), []byte("x"), "image/png", "p"); err == nil ||
		!strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("err = %v, want empty choices", err)
	}
}
