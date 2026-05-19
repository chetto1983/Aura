package install

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// Pinned upstream coordinates for the Whisper GGML small model. Bump when the
// upstream re-uploads (HF LFS URLs are typically stable).
//
// The SHA-256 below was verified 2026-05-19 against:
//   - HF LFS metadata sha256 field for the ggml-small.bin resolve URL
// Rotation procedure:
//  1. curl -sL --max-time 600 <URL> -o /tmp/ggml-small.bin
//  2. sha256sum /tmp/ggml-small.bin
//  3. paste lowercase hex into WhisperModelSHA256 + bump the date comment
const (
	WhisperModelFilename = "ggml-small.bin"
	WhisperModelURL      = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin"
	WhisperModelSHA256   = "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b"
	WhisperModelSize     = 487_601_967
)

// Env vars allow operators to point at a private mirror without recompiling.
const (
	envWhisperModelURL    = "AURA_WHISPER_MODEL_URL"
	envWhisperModelSHA256 = "AURA_WHISPER_MODEL_SHA256"
)

// EnsureWhisperModel guarantees the Whisper GGML model exists at the canonical
// path inside dataDir with a verified SHA-256. Fast cache-hit path: a single
// hash pass over the existing file. Mirrors the EnsureEmbeddingModel pattern.
func EnsureWhisperModel(ctx context.Context, dataDir string, progress ProgressFn) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("install: dataDir required")
	}
	target := filepath.Join(dataDir, WhisperModelFilename)
	if err := EnsureParentDir(target); err != nil {
		return err
	}
	url := envOrDefault(envWhisperModelURL, WhisperModelURL)
	sha := envOrDefault(envWhisperModelSHA256, WhisperModelSHA256)
	return Download(ctx, DownloadOpts{
		URL:      url,
		Dest:     target,
		SHA256:   sha,
		Progress: progress,
	})
}
