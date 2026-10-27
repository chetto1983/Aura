package main

import (
	"strings"
	"testing"
)

// A strict boot hashes the embedding model the sidecar serves (serve_embed_preflight.go), so
// the daemon mounts the sidecar's volume, read-only, where AURA_EMBED_MODEL_DIR points.
func TestAuraReadsTheServedEmbeddingModel(t *testing.T) {
	root := repoRootForTest(t)
	service := composeServiceBlock(t, readProjectFile(t, root, "compose.yaml"), "aura")
	for _, want := range []string{
		"AURA_EMBED_MODEL_DIR: /var/lib/aura-embed-model",
		"- aura-llama-embed:/var/lib/aura-embed-model:ro",
	} {
		if !strings.Contains(service, want) {
			t.Fatalf("aura missing %q:\n%s", want, service)
		}
	}
}

// The media indexer opens the daemon's ChatGPT credentials store, so aura-ingest mounts
// aura-home where the daemon does and derives the same store path from AURA_SKILLS_DIR. A
// volume subpath would need Docker Engine 26, and no install checks the engine.
func TestIngestOpensTheDaemonsChatGPTStore(t *testing.T) {
	root := repoRootForTest(t)
	service := composeServiceBlock(t, readProjectFile(t, root, "compose.yaml"), "aura-ingest")
	for _, want := range []string{"- aura-home:/var/lib/aura", "AURA_SKILLS_DIR: /var/lib/aura/skills"} {
		if !strings.Contains(service, want) {
			t.Fatalf("aura-ingest missing %q:\n%s", want, service)
		}
	}
	if strings.Contains(service, "subpath:") {
		t.Fatalf("aura-ingest mounts a volume subpath:\n%s", service)
	}
}
