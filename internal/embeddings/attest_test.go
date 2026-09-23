package embeddings

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// The body llama.cpp b10964 returned on the lab VM, 2026-09-23 (the Ollama-compatible
// "models" half trimmed; the attestation reads only "data").
const measuredModels = `{"object":"list","data":[{"id":"/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf","aliases":["/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf"],"tags":[],"object":"model","created":1790170262,"owned_by":"llamacpp","meta":{"vocab_type":true,"n_vocab":262144,"n_ctx":2048,"n_ctx_train":2048,"n_embd":768,"n_params":307581696,"size":327060480,"ftype":"Q8_0"}}]}`

func modelsServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("request = %s %s, want GET /v1/models", r.Method, r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAttestLocalNamesTheServedModel(t *testing.T) {
	server := modelsServer(t, measuredModels, http.StatusOK)
	got, err := AttestLocal(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("AttestLocal: %v", err)
	}
	const want = "embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0"
	if got != want {
		t.Fatalf("artifact = %q, want %q", got, want)
	}
}

// The context size is the server's -c, not the model: restarting with another -c must not
// look like a model change.
func TestAttestLocalIgnoresTheContextSize(t *testing.T) {
	server := modelsServer(t, strings.ReplaceAll(measuredModels, `"n_ctx":2048`, `"n_ctx":8192`), http.StatusOK)
	got, err := AttestLocal(context.Background(), server.Client(), server.URL+"/v1")
	if err != nil {
		t.Fatalf("AttestLocal: %v", err)
	}
	if strings.Contains(got, "8192") {
		t.Fatalf("artifact %q carries the context size", got)
	}
}

func TestAttestLocalRefusesWhatItCannotName(t *testing.T) {
	for name, body := range map[string]string{
		"no meta":    `{"data":[{"id":"/m/a.gguf"}]}`,
		"no ftype":   strings.Replace(measuredModels, `,"ftype":"Q8_0"`, ``, 1),
		"two models": `{"data":[{"id":"/m/a.gguf","meta":{"n_embd":768,"n_params":1,"size":1,"ftype":"Q8_0"}},{"id":"/m/b.gguf","meta":{"n_embd":768,"n_params":1,"size":1,"ftype":"Q8_0"}}]}`,
		"empty list": `{"data":[]}`,
		"not json":   `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			server := modelsServer(t, body, http.StatusOK)
			if got, err := AttestLocal(context.Background(), server.Client(), server.URL); err == nil {
				t.Fatalf("AttestLocal = %q, want an error", got)
			}
		})
	}
	t.Run("loading", func(t *testing.T) {
		server := modelsServer(t, `{"error":{"code":503,"message":"Loading model"}}`, http.StatusServiceUnavailable)
		if _, err := AttestLocal(context.Background(), server.Client(), server.URL); err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("err = %v, want the HTTP status named", err)
		}
	})
}

func TestRouteSpaceAttestsOnlyTheLocalRoute(t *testing.T) {
	server := modelsServer(t, measuredModels, http.StatusOK)
	local, err := RouteSpace(context.Background(), server.Client(), config.EmbedConfig{BaseURL: server.URL}, 768)
	if err != nil {
		t.Fatalf("RouteSpace(local): %v", err)
	}
	if !strings.Contains(local.Label, "embeddinggemma-300M-Q8_0.gguf") {
		t.Fatalf("local label = %q", local.Label)
	}

	cloud, err := RouteSpace(context.Background(), nil, config.EmbedConfig{
		BaseURL: "http://unreachable.invalid", CloudModel: "qwen/qwen3-embedding-8b",
	}, 768)
	if err != nil {
		t.Fatalf("RouteSpace(cloud) touched the network or failed: %v", err)
	}
	if cloud.ID != SpaceFor(config.EmbedOpenRouter, "qwen/qwen3-embedding-8b", "", 768, "").ID {
		t.Fatalf("cloud space = %+v", cloud)
	}

	if _, err := RouteSpace(context.Background(), nil, config.EmbedConfig{}, 768); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("empty local base: err = %v, want ErrNoRoute", err)
	}
}
