package embeddings

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// The id is a hash of one canonical form, so the form itself is the contract.
func TestSpaceIDIsTheHashOfTheCanonicalForm(t *testing.T) {
	got := SpaceFor(config.EmbedOpenRouter, "qwen/qwen3-embedding-8b", "", 768, "")
	canonical := fmt.Sprintf(`{"v":1,"recipe":%d,"dims":768,"route":"openrouter","model":"qwen/qwen3-embedding-8b"}`, RecipeVersion)
	sum := sha256.Sum256([]byte(canonical))
	if want := "es1-" + hex.EncodeToString(sum[:8]); got.ID != want {
		t.Fatalf("ID = %q, want %q (hash of %s)", got.ID, want, canonical)
	}
}

func TestSpaceSeparatesWhatChangesTheVectors(t *testing.T) {
	const artifact = "embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0"
	base := SpaceFor(config.EmbedLocal, "", "", 768, artifact)
	for name, other := range map[string]Space{
		"another local artifact": SpaceFor(config.EmbedLocal, "", "", 768, strings.Replace(artifact, "Q8_0", "Q4_K_M", 2)),
		"another width":          SpaceFor(config.EmbedLocal, "", "", 512, artifact),
		"a cloud model":          SpaceFor(config.EmbedOpenRouter, "google/gemini-embedding-2", "", 768, ""),
	} {
		if other.ID == base.ID {
			t.Errorf("%s shares the id %s with the local space", name, base.ID)
		}
	}
}

// Colon-separated strings were ambiguous: model ids carry ":free" and endpoints carry ports.
func TestSpaceIDsDoNotCollideOnColons(t *testing.T) {
	a := SpaceFor(config.EmbedEndpoint, "b", "https://a:8080", 768, "")
	b := SpaceFor(config.EmbedEndpoint, "8080:b", "https://a", 768, "")
	if a.ID == b.ID {
		t.Fatalf("two different endpoint routes share the id %s", a.ID)
	}
	free := SpaceFor(config.EmbedOpenRouter, "nvidia/nemotron-3-embed-1b:free", "", 768, "")
	paid := SpaceFor(config.EmbedOpenRouter, "nvidia/nemotron-3-embed-1b", "", 768, "")
	if free.ID == paid.ID {
		t.Fatalf(":free and the base model share the id %s", free.ID)
	}
}

// OpenRouter's host is not part of the space: a proxy for the same model must not demand a
// rebuild. A manual endpoint's base IS the model selector, so it is.
func TestSpaceIncludesTheBaseOnlyForAManualEndpoint(t *testing.T) {
	if SpaceFor(config.EmbedOpenRouter, "m", "https://proxy.example/api", 768, "").ID !=
		SpaceFor(config.EmbedOpenRouter, "m", "", 768, "").ID {
		t.Error("the OpenRouter space changed with the host")
	}
	if SpaceFor(config.EmbedEndpoint, "m", "https://one.example", 768, "").ID ==
		SpaceFor(config.EmbedEndpoint, "m", "https://two.example", 768, "").ID {
		t.Error("two manual endpoints share one space")
	}
	if SpaceFor(config.EmbedEndpoint, "m", "https://one.example/", 768, "").ID !=
		SpaceFor(config.EmbedEndpoint, "m", "https://one.example", 768, "").ID {
		t.Error("a trailing slash changed the endpoint space")
	}
}

func TestSpaceLabelIsReadable(t *testing.T) {
	const artifact = "embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0"
	for _, tc := range []struct {
		space Space
		want  string
	}{
		{SpaceFor(config.EmbedLocal, "", "", 768, artifact), "local embeddinggemma-300M-Q8_0.gguf, 768d, recipe 1"},
		{SpaceFor(config.EmbedOpenRouter, "qwen/qwen3-embedding-8b", "", 768, ""), "openrouter qwen/qwen3-embedding-8b, 768d, recipe 1"},
		{SpaceFor(config.EmbedEndpoint, "vendor/embed-1", "https://embed.example", 768, ""), "endpoint https://embed.example vendor/embed-1, 768d, recipe 1"},
	} {
		if tc.space.Label != tc.want {
			t.Errorf("Label = %q, want %q", tc.space.Label, tc.want)
		}
	}
}
