package main

// The embed probe asks the sidecar what it IS, instead of reporting what we configured.
//
// It used to POST a real embedding and return len(vector). That number could never be
// anything but cfg.Embed.Dimensions: EmbeddingClient truncates every vector to that width
// (internal/embeddings/client.go -- TruncateMRL passes an exact match, errors on a narrower
// one and cuts a wider one), so the probe paid for a /v1/models round-trip plus an inference
// in order to print a configuration value back at the operator. The one case where the
// number WOULD have differed is returned as an error, never as a different dimension.
//
// /health and /props cost neither call and report facts that can contradict the config --
// which GGUF is really loaded, how much context it has, how many slots it serves. Measured
// on the live sidecar 2026-09-21: /health answers 200 {"status":"ok"} in 1.5ms without an
// API key, and on a restart it answers 503 {"error":{"message":"Loading model"}} for a
// second before flipping to 200. Both states are upstream-documented and both were observed.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

const doctorEmbedProbeTimeout = 10 * time.Second

// llamaProps is the slice of llama.cpp's /props this probe reports. The endpoint returns
// far more; naming only these keeps the detail line readable and the coupling small.
type llamaProps struct {
	ModelPath  string `json:"model_path"`
	TotalSlots int    `json:"total_slots"`
	Generation struct {
		NCtx int `json:"n_ctx"`
	} `json:"default_generation_settings"`
}

func defaultDoctorProbeEmbed(ctx context.Context, cfg *config.Config) (string, error) {
	base, _, model := cfg.EmbedRoute()
	if model != "" {
		// A hosted embedder's health belongs to its provider, and probing it would bill a
		// call on every `aura doctor`. What this stack can be wrong about is the route.
		return fmt.Sprintf("cloud model %s (not probed)", model), nil
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", fmt.Errorf("no embedding sidecar configured (AURA_EMBED_BASE_URL is empty)")
	}

	client := doctorHTTPClient
	if client == nil {
		client = &http.Client{Timeout: doctorEmbedProbeTimeout}
	}
	defer client.CloseIdleConnections()

	status, err := probeEmbedHealth(ctx, client, base)
	if err != nil {
		return "", err
	}
	if status == http.StatusServiceUnavailable {
		// Distinct from unreachable, and the operator's action is different: wait.
		return "", fmt.Errorf("embedding sidecar is still loading its model")
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("embedding sidecar health returned HTTP %d", status)
	}

	props, err := probeEmbedProps(ctx, client, base)
	if err != nil {
		return "", err
	}
	return describeEmbedProps(props), nil
}

func probeEmbedHealth(ctx context.Context, client *http.Client, base string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return 0, err
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode, nil
}

func probeEmbedProps(ctx context.Context, client *http.Client, base string) (llamaProps, error) {
	var props llamaProps
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/props", nil)
	if err != nil {
		return props, err
	}
	response, err := client.Do(request)
	if err != nil {
		return props, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return props, fmt.Errorf("embedding sidecar properties returned HTTP %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&props); err != nil {
		return props, fmt.Errorf("decode embedding sidecar properties: %w", err)
	}
	return props, nil
}

// describeEmbedProps names the loaded model rather than its path: the container path is the
// same on every host and carries no information the operator does not already have.
func describeEmbedProps(props llamaProps) string {
	parts := []string{"ready"}
	if name := path.Base(strings.TrimSpace(props.ModelPath)); name != "" && name != "." && name != "/" {
		parts = append(parts, name)
	}
	if props.Generation.NCtx > 0 {
		parts = append(parts, fmt.Sprintf("n_ctx %d", props.Generation.NCtx))
	}
	if props.TotalSlots > 0 {
		parts = append(parts, fmt.Sprintf("%d slot(s)", props.TotalSlots))
	}
	return strings.Join(parts, ", ")
}
