package asciifold

import "testing"

func TestCaptionFoldsCommonLatinFamilies(t *testing.T) {
	t.Parallel()
	input := "\u00e0\u0113\u00ef\u00f6\u016b\u0107\u00f1\u00ff " +
		"\u00c4\u00c9\u00ce\u00d8\u00dc\u010c\u00d1 \u03a9"
	if got, want := Caption(input), "aeioucny AEIOUCN"; got != want {
		t.Fatalf("Caption() = %q, want %q", got, want)
	}
}

func TestCaptionKeepsOnlyASCII(t *testing.T) {
	t.Parallel()
	got := Caption("  Fattura n\u00b0 12 \u2014 \u20ac30 \U0001F4C4  ")
	for i := 0; i < len(got); i++ {
		if got[i] > 0x7F {
			t.Fatalf("Caption() = %q keeps byte %#x above 0x7F", got, got[i])
		}
	}
	if want := "Fattura n 12  30"; got != want {
		t.Fatalf("Caption() = %q, want %q (unfoldable runes dropped, edges trimmed)", got, want)
	}
}
