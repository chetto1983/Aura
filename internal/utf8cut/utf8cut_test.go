package utf8cut

import (
	"strings"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"
)

func TestPrefix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"shorter than max", "abc", 5, "abc"},
		{"exact length", "abc", 3, "abc"},
		{"ascii truncate", "hello", 3, "hel"},
		{"cut inside a rune backs off", "àbc", 1, ""},
		{"keeps a whole rune", "àbc", 2, "à"},
		{"zero max", "hello", 0, ""},
		{"negative max", "hello", -1, ""},
		{"empty", "", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Prefix(tc.s, tc.max); got != tc.want {
				t.Fatalf("Prefix(%q, %d) = %q, want %q", tc.s, tc.max, got, tc.want)
			}
		})
	}
}

func TestPrefixNeverSplitsARune(t *testing.T) {
	t.Parallel()
	s := "aàbèc€d"
	for max := 0; max <= len(s); max++ {
		got := Prefix(s, max)
		if len(got) > max || !utf8.ValidString(got) || !strings.HasPrefix(s, got) {
			t.Fatalf("Prefix(%q, %d) = %q: over budget, split rune, or not a prefix", s, max, got)
		}
	}
}

// The retired mcptools copy cut back to just before an invalid byte, wherever it sat, losing
// every valid byte after it. ValidPrefix keeps the text and stays valid UTF-8.
func TestValidPrefixKeepsTextAroundAnInvalidByte(t *testing.T) {
	t.Parallel()
	s := "ok\xffstill here"
	got := ValidPrefix(s, 64)
	if got != "ok�still here" {
		t.Fatalf("ValidPrefix(%q) = %q, want the invalid byte replaced and the rest kept", s, got)
	}
	if cut := ValidPrefix(s, 4); !utf8.ValidString(cut) || len(cut) > 4 {
		t.Fatalf("ValidPrefix(%q, 4) = %q, want valid UTF-8 within budget", s, cut)
	}
}

// The tool-result preview's Req#6 property, which moved here with the helper: for any
// string and any budget, the output is valid UTF-8, within budget, and a prefix of the input.
func TestPrefixProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		content := rapid.String().Draw(rt, "content")
		max := rapid.IntRange(0, len(content)+16).Draw(rt, "max")
		got := Prefix(content, max)
		if !utf8.ValidString(got) || len(got) > max || !strings.HasPrefix(content, got) {
			rt.Fatalf("Prefix(%q, %d) = %q: invalid, over budget, or not a prefix", content, max, got)
		}
	})
}
