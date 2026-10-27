package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/filehash"
)

// embedSidecar answers /v1/models the way llama.cpp does (lab VM, 2026-09-23), serving the
// file name.
func embedSidecar(t *testing.T, file string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"/root/.cache/llama.cpp/` + file +
			`","meta":{"n_embd":768,"n_params":307581696,"size":327060480,"ftype":"Q8_0"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func servedGGUF(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("GGUF fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func strictLocalEmbed(base, dir, fingerprint string) *config.Config {
	return &config.Config{
		Profile: config.ProfileSingleUserHardened,
		Embed:   config.EmbedConfig{BaseURL: base, ModelDir: dir, Fingerprint: fingerprint},
	}
}

func TestEmbedArtifactPreflightHashesTheServedFile(t *testing.T) {
	sidecar := embedSidecar(t, "embeddinggemma-300M-Q8_0.gguf")
	dir := servedGGUF(t, "embeddinggemma-300M-Q8_0.gguf")
	sum, err := filehash.SHA256(filepath.Join(dir, "embeddinggemma-300M-Q8_0.gguf"))
	if err != nil {
		t.Fatal(err)
	}

	if err := embedArtifactPreflight(context.Background(), strictLocalEmbed(sidecar.URL, dir, sum)); err != nil {
		t.Fatalf("the served file hashing to the fingerprint = %v, want boot", err)
	}
	err = embedArtifactPreflight(context.Background(), strictLocalEmbed(sidecar.URL, dir, strings.Repeat("0", 64)))
	if err == nil || !strings.Contains(err.Error(), sum) || !strings.Contains(err.Error(), "AURA_EMBED_FINGERPRINT") {
		t.Fatalf("a mismatch = %v, want a refusal naming the served hash and the knob", err)
	}
}

func TestEmbedArtifactPreflightRefusesWhatItCannotHash(t *testing.T) {
	sidecar := embedSidecar(t, "embeddinggemma-300M-Q8_0.gguf")
	anyPin := strings.Repeat("a", 64)
	for name, cfg := range map[string]*config.Config{
		"no model dir":        strictLocalEmbed(sidecar.URL, "", anyPin),
		"file not in the dir": strictLocalEmbed(sidecar.URL, t.TempDir(), anyPin),
		"sidecar unreachable": strictLocalEmbed("http://127.0.0.1:1", t.TempDir(), anyPin),
	} {
		if err := embedArtifactPreflight(context.Background(), cfg); err == nil {
			t.Errorf("%s: booted, want a refusal", name)
		}
	}
}

func TestEmbedArtifactPreflightOnlyChecksAStrictLocalRoute(t *testing.T) {
	unreachable := "http://127.0.0.1:1"
	lenient := strictLocalEmbed(unreachable, "", "")
	lenient.Profile = config.ProfileDev
	cloud := strictLocalEmbed(unreachable, "", "")
	cloud.Embed.CloudModel = "openai/text-embedding-3-small"
	off := strictLocalEmbed("", "", "")
	for name, cfg := range map[string]*config.Config{"dev": lenient, "cloud route": cloud, "dense off": off, "nil": nil} {
		if err := embedArtifactPreflight(context.Background(), cfg); err != nil {
			t.Errorf("%s = %v, want no check", name, err)
		}
	}
}
