package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// localModelList is the slice of llama.cpp's /v1/models the attestation reads. Measured on
// the lab VM 2026-09-23 (llama.cpp b10964): data[0].id is the GGUF path, and meta carries
// n_embd 768, n_params 307581696, size 327060480 and ftype "Q8_0". n_ctx is left out on
// purpose: it is the -c the server was started with, not a property of the model.
type localModel struct {
	ID   string `json:"id"`
	Meta struct {
		NEmbd   int    `json:"n_embd"`
		NParams int64  `json:"n_params"`
		Size    int64  `json:"size"`
		FType   string `json:"ftype"`
	} `json:"meta"`
}

type localModelList struct {
	Data []localModel `json:"data"`
}

// AttestLocal names the model the local sidecar actually serves, from the sidecar itself, for
// the embedding space's key. The served file's bytes are a separate check: the strict boot
// hashes it against AURA_EMBED_FINGERPRINT (ServedModelFile, cmd/aura's embed preflight).
func AttestLocal(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	model, name, err := localServedModel(ctx, client, baseURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s|size=%d|params=%d|embd=%d|ftype=%s",
		name, model.Meta.Size, model.Meta.NParams, model.Meta.NEmbd, strings.TrimSpace(model.Meta.FType)), nil
}

// ServedModelFile is the file name of the GGUF the local sidecar serves.
func ServedModelFile(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	_, name, err := localServedModel(ctx, client, baseURL)
	return name, err
}

func localServedModel(ctx context.Context, client *http.Client, baseURL string) (localModel, string, error) {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverRoot(baseURL)+"/v1/models", nil)
	if err != nil {
		return localModel{}, "", fmt.Errorf("attest local embedder: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return localModel{}, "", fmt.Errorf("attest local embedder: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return localModel{}, "", fmt.Errorf("attest local embedder: /v1/models returned HTTP %d", response.StatusCode)
	}
	var list localModelList
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&list); err != nil {
		return localModel{}, "", fmt.Errorf("attest local embedder: decode /v1/models: %w", err)
	}
	if len(list.Data) != 1 {
		return localModel{}, "", fmt.Errorf("attest local embedder: the sidecar serves %d models, want exactly 1", len(list.Data))
	}
	model := list.Data[0]
	name := path.Base(strings.TrimSpace(model.ID))
	if name == "." || name == "/" || model.Meta.NEmbd <= 0 || model.Meta.NParams <= 0 ||
		model.Meta.Size <= 0 || strings.TrimSpace(model.Meta.FType) == "" {
		return localModel{}, "", fmt.Errorf("attest local embedder: /v1/models does not describe its model (id %q)", model.ID)
	}
	return model, name, nil
}
