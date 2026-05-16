package memoryindex

import (
	"testing"
)

func TestContentHash(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		h1 := ContentHash("user", "hello world", "model-a")
		h2 := ContentHash("user", "hello world", "model-a")
		if h1 != h2 {
			t.Fatalf("expected deterministic hash, got %q != %q", h1, h2)
		}
		if len(h1) != 64 {
			t.Fatalf("expected 64-char hex, got %d chars: %q", len(h1), h1)
		}
	})

	t.Run("role_sensitive", func(t *testing.T) {
		h1 := ContentHash("user", "hello", "model-a")
		h2 := ContentHash("assistant", "hello", "model-a")
		if h1 == h2 {
			t.Fatalf("expected different hashes for different roles, got same: %q", h1)
		}
	})

	t.Run("model_sensitive", func(t *testing.T) {
		h1 := ContentHash("user", "hello", "model-a")
		h2 := ContentHash("user", "hello", "model-b")
		if h1 == h2 {
			t.Fatalf("expected different hashes for different models, got same: %q", h1)
		}
	})

	t.Run("body_sensitive", func(t *testing.T) {
		h1 := ContentHash("user", "hello", "model-a")
		h2 := ContentHash("user", "world", "model-a")
		if h1 == h2 {
			t.Fatalf("expected different hashes for different bodies, got same: %q", h1)
		}
	})

	t.Run("empty_inputs", func(t *testing.T) {
		h := ContentHash("", "", "")
		if len(h) != 64 {
			t.Fatalf("expected 64-char hex for empty inputs, got %d chars", len(h))
		}
	})
}
