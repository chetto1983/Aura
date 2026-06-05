package skills

import (
	"strings"
	"testing"
)

func TestRenderManifestByteStableAndSorted(t *testing.T) {
	t.Parallel()
	// Deliberately UNsorted input — the render must sort it, so map-iteration order
	// from the loader can never leak into the manifest (cache-stability, T-11-02-I1).
	in := []Skill{
		{Name: "zebra", Description: "Last alphabetically."},
		{Name: "alpha", Description: "First alphabetically."},
		{Name: "mango", Description: "Middle."},
	}
	first := RenderManifest(in, 8192)
	second := RenderManifest(in, 8192)
	if first != second {
		t.Fatalf("RenderManifest not byte-stable:\n%q\nvs\n%q", first, second)
	}

	ai := strings.Index(first, "alpha")
	mi := strings.Index(first, "mango")
	zi := strings.Index(first, "zebra")
	if !(ai < mi && mi < zi) {
		t.Fatalf("manifest not alphabetical: alpha@%d mango@%d zebra@%d\n%s", ai, mi, zi, first)
	}

	// Reordering the input must not change the output (sort is the stabilizer).
	reordered := []Skill{in[1], in[2], in[0]}
	if RenderManifest(reordered, 8192) != first {
		t.Fatalf("manifest changed when input was reordered — not order-independent")
	}
}

func TestRenderManifestOverflowTail(t *testing.T) {
	t.Parallel()
	in := []Skill{
		{Name: "aaa", Description: strings.Repeat("x", 40)},
		{Name: "bbb", Description: strings.Repeat("y", 40)},
		{Name: "ccc", Description: strings.Repeat("z", 40)},
	}
	// A tiny cap forces overflow after the first entry.
	out := RenderManifest(in, 50)
	if !strings.Contains(out, "more — search with skill action=list {query}") {
		t.Fatalf("expected overflow tail, got:\n%s", out)
	}
	// At least one entry must still render before the tail (we never drop everything).
	if !strings.Contains(out, "aaa") {
		t.Fatalf("first entry should render before overflow tail:\n%s", out)
	}
	if strings.Contains(out, "ccc") {
		t.Fatalf("third entry should have overflowed under a 50-byte cap:\n%s", out)
	}
}

func TestRenderManifestEmpty(t *testing.T) {
	t.Parallel()
	if got := RenderManifest(nil, 8192); !strings.Contains(got, "No skills") {
		t.Fatalf("empty manifest = %q, want a 'No skills' notice", got)
	}
}

func TestBM25CorpusSorted(t *testing.T) {
	t.Parallel()
	in := []Skill{
		{Name: "zebra", Description: "z desc"},
		{Name: "alpha", Description: "a desc"},
	}
	corpus := BM25Corpus(in)
	if len(corpus) != 2 {
		t.Fatalf("corpus len = %d, want 2", len(corpus))
	}
	if !strings.HasPrefix(corpus[0], "alpha ") || !strings.HasPrefix(corpus[1], "zebra ") {
		t.Fatalf("corpus not name-sorted: %v", corpus)
	}
}
