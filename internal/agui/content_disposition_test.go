package agui

import (
	"net/url"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// TestContentDisposition_Property fuzzes contentDisposition over arbitrary unicode filenames and
// asserts the RFC-6266 safety invariants hold for EVERY input: the value starts with the
// attachment prefix, carries exactly one filename= and one filename* param, never contains
// a raw CR/LF (header-injection guard, T-HdrInj), and the extended param percent-decodes back to
// the original filename (unicode round-trip / preservation, D-11).
//
// Inputs that literally contain the param-name substrings ("filename=" / "filename*=") are filtered
// out so the exactly-one-of-each assertions are rigorous: those are the only strings whose ASCII
// fallback could echo a second param name into the value (url.PathEscape escapes '*', so the
// extended param can never echo "filename*=").
func TestContentDisposition_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.String().Filter(func(s string) bool {
			return !strings.Contains(s, "filename=") && !strings.Contains(s, "filename*=")
		}).Draw(rt, "name")

		out := contentDisposition(name)

		if !strings.HasPrefix(out, `attachment; filename="`) {
			rt.Fatalf("output %q missing the attachment; filename=\" prefix", out)
		}
		if strings.ContainsRune(out, '\r') || strings.ContainsRune(out, '\n') {
			rt.Fatalf("output %q contains a raw CR/LF (header injection)", out)
		}
		if n := strings.Count(out, "filename="); n != 1 {
			rt.Fatalf("output %q has %d filename= params, want exactly 1", out, n)
		}
		if n := strings.Count(out, "filename*=UTF-8''"); n != 1 {
			rt.Fatalf("output %q has %d filename*= params, want exactly 1", out, n)
		}
		_, extended, found := strings.Cut(out, "filename*=UTF-8''")
		if !found {
			rt.Fatalf("output %q missing the filename* extended param", out)
		}
		decoded, err := url.PathUnescape(extended)
		if err != nil {
			rt.Fatalf("filename* value %q is not valid percent-encoding: %v", extended, err)
		}
		if decoded != name {
			rt.Fatalf("filename* round-trip = %q, want %q", decoded, name)
		}
	})
}

// TestContentDisposition_Table pins the concrete fold + encode behaviour on curated inputs: unicode
// folds in the fallback but is preserved in filename*, the quoted-string delimiters and CR/LF are
// neutralised, a semicolon stays inside the quotes (never a param separator), and an empty name
// collapses to "download".
func TestContentDisposition_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		in           string
		wantFallback string
		wantDecoded  string
	}{
		{"unicode folds in fallback, preserved in ext", "relazióne.pdf", "relazione.pdf", "relazióne.pdf"},
		{"embedded quote neutralised", `a"b.pdf`, "a_b.pdf", `a"b.pdf`},
		{"embedded CRLF never raw", "a\r\nb.pdf", "a__b.pdf", "a\r\nb.pdf"},
		{"backslash neutralised", `a\b.pdf`, "a_b.pdf", `a\b.pdf`},
		{"semicolon stays quoted", "a;b.pdf", "a;b.pdf", "a;b.pdf"},
		{"empty collapses to download", "", "download", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := contentDisposition(tc.in)
			if strings.ContainsRune(out, '\r') || strings.ContainsRune(out, '\n') {
				t.Fatalf("output %q contains a raw CR/LF", out)
			}
			if c := strings.Count(out, "filename="); c != 1 {
				t.Fatalf("output %q has %d filename= params, want 1", out, c)
			}
			if c := strings.Count(out, "filename*=UTF-8''"); c != 1 {
				t.Fatalf("output %q has %d filename*= params, want 1", out, c)
			}
			wantPrefix := `attachment; filename="` + tc.wantFallback + `"; filename*=UTF-8''`
			if !strings.HasPrefix(out, wantPrefix) {
				t.Fatalf("output %q, want prefix %q", out, wantPrefix)
			}
			_, ext, _ := strings.Cut(out, "filename*=UTF-8''")
			decoded, err := url.PathUnescape(ext)
			if err != nil {
				t.Fatalf("filename* %q is not valid percent-encoding: %v", ext, err)
			}
			if decoded != tc.wantDecoded {
				t.Fatalf("filename* round-trip = %q, want %q", decoded, tc.wantDecoded)
			}
		})
	}
}
