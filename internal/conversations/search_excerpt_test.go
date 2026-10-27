package conversations

import (
	"strings"
	"testing"
)

func TestClampExcerptRuneSafeEllipses(t *testing.T) {
	t.Parallel()
	s := "abècd€fg"
	if got := clampExcerpt(s, 3, 7); got != "…cd…" {
		t.Fatalf("clampExcerpt should move byte indexes to rune starts with ellipses, got %q", got)
	}
	if got := clampExcerpt(s, -10, len(s)+10); got != s {
		t.Fatalf("full-range clampExcerpt = %q, want %q", got, s)
	}
}

func TestExcerptWindowsAroundTheMatch(t *testing.T) {
	t.Parallel()
	content := strings.Repeat("prima ", 30) + "METEO   di oggi\n" + strings.Repeat("dopo ", 30)
	got := Excerpt(content, "meteo")
	if !strings.Contains(got, "METEO di oggi") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("Excerpt = %q, want a whitespace-folded window around the case-insensitive match", got)
	}
	if fuzzy := Excerpt(content, "assente"); !strings.HasPrefix(fuzzy, "prima") || len(fuzzy) > 120+len("…") {
		t.Fatalf("Excerpt without a literal match = %q, want the first 120 bytes", fuzzy)
	}
}
