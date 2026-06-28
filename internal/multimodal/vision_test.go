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
	var gotAuth, gotPath, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotModel = decodeVisionReq(t, r).Model
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"a cat"}}]}`)
	}))
	defer srv.Close()

	c := NewVisionClient(VisionConfig{
		LocalBaseURL: srv.URL,
		LocalModel:   "local-vl",
		HTTPClient:   srv.Client(),
	})
	out, err := c.Describe(t.Context(), []byte("img"), "image/png", "Describe this image.")
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
}

func TestVisionCloudRouteBearerAndModel(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotModel = decodeVisionReq(t, r).Model
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	c := NewVisionClient(VisionConfig{
		VisionCloud:       true,
		Model:             "deepseek/deepseek-v4-flash", // non-vision → fallback
		FallbackModel:     "minimax/minimax-m3",
		OpenRouterBaseURL: srv.URL,
		OpenRouterAPIKey:  "shared-key",
		HTTPClient:        srv.Client(),
	})
	if _, err := c.Describe(t.Context(), []byte("img"), "image/png", "x"); err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if gotAuth != "Bearer shared-key" {
		t.Errorf("cloud Authorization = %q, want Bearer shared-key", gotAuth)
	}
	// The model on the wire must equal what the route resolves (internal
	// consistency — avoids coupling the test to the SupportsVision table).
	if gotModel != c.VisionModel() {
		t.Errorf("wire model %q != VisionModel() %q", gotModel, c.VisionModel())
	}
	if gotModel == "" {
		t.Error("cloud vision sent an empty model id")
	}
}

func TestVisionEmptyBaseURL(t *testing.T) {
	c := NewVisionClient(VisionConfig{})
	if _, err := c.Describe(t.Context(), []byte("x"), "image/png", "p"); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v, want not-configured", err)
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
