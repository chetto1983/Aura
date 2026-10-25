package arcadedb

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func exactTermKeys(terms []ExactTerm) []string {
	keys := make([]string, 0, len(terms))
	for _, term := range terms {
		keys = append(keys, term.Key)
	}
	return keys
}

func TestQueryExactTerms(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"a quoted name inside a sentence", `Quale famiglia di font mostra "Ubuntu Mono" nel documento?`, []string{"ubuntu mono"}},
		{"a bare quoted name", `"Ubuntu Mono"`, []string{"ubuntu mono"}},
		{"an unquoted name is not exact", "Ubuntu Mono", nil},
		{"a capitalised sentence is not exact", "Mostra Ubuntu Mono?", nil},
		{"a lowercase word is not exact", "fattura", nil},
		{"typographic quotes, guillemets and a file name", "cerca “Gross Sales” e «Units Sold» in Financial_Sample.xlsx",
			[]string{"gross sales", "units sold", "financial_sample xlsx", "financial_sample"}},
		{"an identifier", "che difetto riguardava il reclamo RC-2026-040", []string{"rc-2026-040"}},
		{"a lowercase identifier", "vitamina b12", []string{"b12"}},
		{"an unclosed quote", `un "virgolettato aperto`, nil},
		{"empty quotes", `cerca "" qui`, nil},
		{"file names need a lettered extension", "esempio.md, v1.2 e e.g. 3.14", []string{"esempio md", "v1"}},
		{"duplicates collapse", `"Aura" e ancora "aura"`, []string{"aura"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := exactTermKeys(QueryExactTerms(tc.query))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("QueryExactTerms(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func TestExactTermKeepsTheCallersWording(t *testing.T) {
	terms := QueryExactTerms(`mostra "Ubuntu Mono" in calibre_demo.docx`)
	raw := make([]string, 0, len(terms))
	for _, term := range terms {
		raw = append(raw, term.Raw)
	}
	if want := []string{"Ubuntu Mono", "calibre_demo.docx", "calibre_demo"}; !slices.Equal(raw, want) {
		t.Fatalf("raw terms = %q, want %q", raw, want)
	}
}

func TestExactTermCarriedBy(t *testing.T) {
	term := QueryExactTerms(`quale documento mostra "Ubuntu Mono"?`)[0]
	cases := []struct {
		texts []string
		want  bool
	}{
		{[]string{"here is some text in the Ubuntu Mono typeface"}, true},
		{[]string{"title only", "THE UBUNTU MONO FONT"}, true},
		{[]string{"the Ubuntu typeface, and Mono elsewhere"}, false},
		{[]string{"Ubuntu", "Mono"}, false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := term.CarriedBy(tc.texts...); got != tc.want {
			t.Fatalf("CarriedBy(%q) = %v, want %v", tc.texts, got, tc.want)
		}
	}
	identifier := QueryExactTerms("reclamo RC-2026-040")[0]
	if !identifier.CarriedBy("Il reclamo RC-2026-040 riguarda un difetto.") {
		t.Fatal("an identifier must be carried by the text that names it")
	}
	if identifier.CarriedBy("RC-2026-0400") {
		t.Fatal("an identifier must be carried whole")
	}
	file := QueryExactTerms("apri Financial_Sample.xlsx")[0]
	if !file.CarriedBy("Financial_Sample.xlsx") || file.CarriedBy("Financial_Sample.csv") {
		t.Fatal("a file name must be carried by that name and no other")
	}
}

// A record carries the question's exact terms only when it carries every one, each in any of its
// texts; a question with none has no carrier.
func TestCarriesEvery(t *testing.T) {
	terms := QueryExactTerms(`il reclamo RC-2026-040 cita "Ubuntu Mono"?`)
	if !CarriesEvery(terms, "RC-2026-040", "stampato in Ubuntu Mono") {
		t.Fatal("two texts carrying one term each carry both")
	}
	if CarriesEvery(terms, "il font Ubuntu Mono") {
		t.Fatal("a text missing the identifier carries the terms")
	}
	if CarriesEvery(nil, "anything") {
		t.Fatal("a question with no exact term has a carrier")
	}
}

func TestExactTermLucenePhrase(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"Ubuntu Mono", `"Ubuntu Mono"`},
		{`back\slash`, `"back\\slash"`},
		{`say "x"`, `"say \"x\""`},
	}
	for _, tc := range cases {
		if got := (ExactTerm{Raw: tc.raw}).lucenePhrase(); got != tc.want {
			t.Fatalf("lucenePhrase(%q) = %s, want %s", tc.raw, got, tc.want)
		}
	}
	if got := lucenePhrases(QueryExactTerms(`"Ubuntu Mono" e RC-2026-040`)); got != `"Ubuntu Mono" OR "rc-2026-040"` {
		t.Fatalf("lucenePhrases = %s, want every phrase OR-ed", got)
	}
}

func FuzzQueryExactTerms(f *testing.F) {
	for _, seed := range []string{`"Ubuntu Mono"`, `"a" «b» “c”`, "RC-2026-040.", "x.docx, y.md!", `"`, ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, query string) {
		if !utf8.ValidString(query) {
			return
		}
		seen := make(map[string]struct{})
		for _, term := range QueryExactTerms(query) {
			if term.Key == "" || strings.TrimSpace(term.Raw) == "" {
				t.Fatalf("empty term from %q: %+v", query, term)
			}
			if _, duplicate := seen[term.Key]; duplicate {
				t.Fatalf("duplicate term %q from %q", term.Key, query)
			}
			seen[term.Key] = struct{}{}
			// Extraction and carrying tokenise apart: a term the query names must be one the
			// query itself carries, or no document could ever carry it either.
			if !term.CarriedBy(query) {
				t.Fatalf("term %q is not carried by the query %q that named it", term.Key, query)
			}
		}
	})
}
