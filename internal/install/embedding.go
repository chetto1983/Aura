package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Pinned upstream coordinates for the embedding model. Bump when the upstream
// re-uploads (rare; HF mirrors are typically stable).
//
// The SHA-256 below was verified 2026-05-13 against:
//   - HF's `X-Linked-ETag` HEAD response header for the resolve URL
//   - a local re-hash of the downloaded 277,852,192-byte body
// Both matched. Document the rotation procedure here before bumping:
//  1. curl -sL --max-time 600 <URL> -o /tmp/file.gguf
//  2. sha256sum /tmp/file.gguf
//  3. paste lowercase hex into EmbeddingModelSHA256 + bump the date comment
const (
	EmbeddingModelFilename = "embeddinggemma-300m-Q4_0.gguf"
	EmbeddingModelURL      = "https://huggingface.co/unsloth/embeddinggemma-300m-GGUF/" +
		"resolve/main/embeddinggemma-300m-Q4_0.gguf"
	EmbeddingModelSHA256 = "edc6015cb15694c27be7d1d33f1bc015db9a358ff51ed524628c027504907ba9"
	EmbeddingModelSize   = 277_852_192
)

// Env vars allow operators to point at a private mirror without recompiling.
// Empty env value → use the constant. Useful when:
//   - HF is down and the user has a backup HTTPS mirror
//   - The user wants to swap to a different quantization (Q5/Q8) at their
//     own risk; pair this with AURA_EMBEDDING_MODEL_SHA256.
const (
	envEmbeddingModelURL    = "AURA_EMBEDDING_MODEL_URL"
	envEmbeddingModelSHA256 = "AURA_EMBEDDING_MODEL_SHA256"
)

// EnsureEmbeddingModel guarantees the embedding GGUF exists at the canonical
// path inside dataDir with a verified SHA-256. Fast cache-hit path: a single
// hash pass over the existing file (~1 s on SSD for 265 MB).
//
// progress may be nil. Caller owns the lifecycle (e.g. the `cmd/aura-init-models`
// binary calls this then exits).
func EnsureEmbeddingModel(ctx context.Context, dataDir string, progress ProgressFn) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("install: dataDir required")
	}
	target := filepath.Join(dataDir, EmbeddingModelFilename)
	if err := EnsureParentDir(target); err != nil {
		return err
	}
	url := envOrDefault(envEmbeddingModelURL, EmbeddingModelURL)
	sha := envOrDefault(envEmbeddingModelSHA256, EmbeddingModelSHA256)
	return Download(ctx, DownloadOpts{
		URL:      url,
		Dest:     target,
		SHA256:   sha,
		Progress: progress,
	})
}

func envOrDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}
