// serve_embed_preflight.go — the boot-time check that the local embedding sidecar serves the
// artifact AURA_EMBED_FINGERPRINT names. Until 2026-10-27 the strict gate checked only that the
// pair was present and hex (config.gateDocumentRetrieval), and llama.cpp's /v1/models answers
// an empty `digest`, so nothing in the running system ever compared a hash. This reads the file
// the sidecar names from the volume compose mounts read-only at AURA_EMBED_MODEL_DIR, and
// hashes it: 333,590,944 bytes took 0.69 s on 2026-10-27. Like the replication preflight it
// stays out of Config.ValidateProfile, which does no I/O.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/filehash"
)

const embedArtifactBootTimeout = 2 * time.Minute

// embedArtifactPreflight refuses a strict boot whose local embedding sidecar serves a file
// that does not hash to AURA_EMBED_FINGERPRINT, or whose file it cannot read. A lenient
// profile, the cloud route and a switched-off dense route have no local artifact to check.
func embedArtifactPreflight(ctx context.Context, cfg *config.Config) error {
	if cfg == nil || !cfg.Profile.Strict() || config.EmbedRouteKind(cfg.Embed) != config.EmbedLocal {
		return nil
	}
	base, _, _ := config.ResolveEmbedRoute(cfg.Embed, "")
	if strings.TrimSpace(base) == "" {
		return nil
	}
	dir := strings.TrimSpace(cfg.Embed.ModelDir)
	if dir == "" {
		return fmt.Errorf("config: AURA_EMBED_MODEL_DIR is unset: a strict boot hashes the GGUF the " +
			"embedding sidecar serves against AURA_EMBED_FINGERPRINT, and needs the directory that holds it " +
			"(compose mounts the sidecar's volume read-only and sets it)")
	}
	cctx, cancel := context.WithTimeout(ctx, embedArtifactBootTimeout)
	defer cancel()
	file, err := embeddings.ServedModelFile(cctx, nil, base)
	if err != nil {
		return fmt.Errorf("config: a strict boot could not name the model the embedding sidecar serves: %w", err)
	}
	path := filepath.Join(dir, file)
	sum, err := filehash.SHA256(path)
	if err != nil {
		return fmt.Errorf("config: a strict boot could not hash the served embedding model %s: %w", path, err)
	}
	if want := strings.ToLower(strings.TrimSpace(cfg.Embed.Fingerprint)); sum != want {
		return fmt.Errorf("config: the embedding sidecar serves %s, whose sha256 is %s, but "+
			"AURA_EMBED_FINGERPRINT is %s. Re-run the installer, which fetches AURA_EMBED_REVISION and "+
			"checks its hash, or set the pair to the artifact this host serves", file, sum, want)
	}
	slog.Info("aura serve: embedding model verified against AURA_EMBED_FINGERPRINT", "file", file, "sha256", sum)
	return nil
}
