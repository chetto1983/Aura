package summarizer

import (
	"strings"
	"testing"
)

func TestTriageCandidate(t *testing.T) {
	tests := []struct {
		category string
		want     TargetLayer
	}{
		{"person", TargetUserMemory},
		{"preference", TargetUserMemory},
		{"fact", TargetUserMemory},
		{"todo", TargetUserMemory},
		{"project", TargetWiki},
		{"", TargetWiki},
	}
	for _, tt := range tests {
		got := TriageCandidate(Candidate{Category: tt.category})
		if got != tt.want {
			t.Errorf("TriageCandidate(%q) = %q, want %q", tt.category, got, tt.want)
		}
	}
}

func TestUserFactHandle_IdenticalModuloFormatProduceSameHandle(t *testing.T) {
	// Leading/trailing spaces + internal multi-space + case variation must produce same handle.
	c1 := Candidate{Category: "preference", Fact: "  Likes   Coffee  "}
	c2 := Candidate{Category: "preference", Fact: "likes coffee"}
	h1 := UserFactHandle(c1)
	h2 := UserFactHandle(c2)
	if h1 != h2 {
		t.Errorf("expected same handle for normalized-equal facts: %q vs %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "user_memory:") {
		t.Errorf("handle must start with 'user_memory:', got %q", h1)
	}
	suffix := strings.TrimPrefix(h1, "user_memory:")
	if len(suffix) != 16 {
		t.Errorf("sha suffix must be 16 hex chars, got %d: %q", len(suffix), suffix)
	}
}
