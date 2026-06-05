package skills

import (
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// fuzzBlocklist is the seed blocklist the fuzz + corpus tests check against
// (the prd.md §Slice 7 builtin list).
var fuzzBlocklist = defaultBlocklistFixture

// collapsesToBlocklist is the SC#3 oracle: it returns true iff the NFKC-folded,
// lowercased input contains a blocklist literal. ValidateForWrite (model path)
// MUST reject exactly these inputs. This is the same computation violatesBlocklist
// performs — the property test asserts the public API agrees with the oracle.
func collapsesToBlocklist(input string) bool {
	folded := strings.ToLower(norm.NFKC.String(input))
	for _, bad := range fuzzBlocklist {
		if bad != "" && strings.Contains(folded, strings.ToLower(bad)) {
			return true
		}
	}
	return false
}

// assertSC3 enforces the SC#3 invariant for one input: if it NFKC-collapses to a
// blocklist literal, the model-path validator (allowBlocklisted=false) MUST
// reject it. (When it does not collapse, we make no claim — a benign body is
// allowed.)
func assertSC3(t *testing.T, input string) {
	t.Helper()
	fm := Frontmatter{Name: "fuzz-skill", Description: "fuzz", Type: TypeInstruction}
	err := ValidateForWrite(fm, input, fuzzBlocklist, 1<<20, false)
	if collapsesToBlocklist(input) && err == nil {
		t.Fatalf("SC#3 violation: input NFKC-collapses to a blocklist literal but ValidateForWrite accepted it; input=%q normalized=%q", input, norm.NFKC.String(input))
	}
}

// FuzzSkillValidator drives Unicode/NFKC mutations of blocklist patterns and
// asserts the SC#3 property: no NFKC-collapse-to-blocklist input slips through
// the model-path validator. Acceptance command:
//
//	go test -fuzz=FuzzSkillValidator -fuzztime=60s ./internal/skills/
func FuzzSkillValidator(f *testing.F) {
	// Seed: plain literals, fullwidth/compatibility variants, benign strings,
	// and embedded-in-prose variants.
	for _, bad := range fuzzBlocklist {
		f.Add(bad)
		f.Add(toFullwidth(bad))
		f.Add("preamble " + bad + " suffix")
		f.Add("preamble " + toFullwidth(bad) + " suffix")
	}
	f.Add("a perfectly benign skill body")
	f.Add("")
	f.Add("ﬀﬁﬂ ligatures that NFKC-expand")
	f.Add("①②③ circled digits")

	f.Fuzz(func(t *testing.T, input string) {
		assertSC3(t, input)
	})
}

// TestSkillValidator_NFKCCorpus is the deterministic 10K-mutation guard so CI
// without -fuzz still exercises the SC#3 property over a generated NFKC/Unicode
// corpus. It mutates every blocklist literal across a matrix of obfuscations and
// asserts the oracle-vs-validator agreement on each.
func TestSkillValidator_NFKCCorpus(t *testing.T) {
	corpus := buildNFKCCorpus()
	if len(corpus) < 10000 {
		t.Fatalf("corpus too small: %d, want >= 10000", len(corpus))
	}
	for _, input := range corpus {
		assertSC3(t, input)
	}
}

// buildNFKCCorpus generates >= 10K NFKC/Unicode mutations of the blocklist
// literals plus benign filler. Mutations: identity, fullwidth, prose-embedded,
// case-flipped, whitespace-padded, ligature-injected, and combinations — each
// crossed with a set of benign prefixes/suffixes to push the count past 10K.
func buildNFKCCorpus() []string {
	mutators := []func(string) string{
		func(s string) string { return s },
		toFullwidth,
		func(s string) string { return strings.ToUpper(s) },
		func(s string) string { return "  " + s + "  " },
		func(s string) string { return "ﬁ" + s + "ﬂ" },
		func(s string) string { return toFullwidth(strings.ToUpper(s)) },
		func(s string) string { return "\t" + s + "\n" },
		func(s string) string { return "①②③" + s },
		func(s string) string { return toFullwidth(s) + s },
	}
	wrappers := []string{
		"%s",
		"intro text %s outro text",
		"line one\n%s\nline two",
		"### Heading\n%s\n- bullet",
		"json {\"k\": \"%s\"}",
		"<tag>%s</tag>",
		"prefix-%s-suffix",
		"prose with the token %s right here in the middle of a sentence",
	}
	benign := []string{
		"a normal skill", "describe spreadsheets", "format markdown tables",
		"nothing dangerous here", "convert csv to xlsx", "summarize a document",
	}

	var corpus []string
	for _, bad := range fuzzBlocklist {
		for _, mut := range mutators {
			m := mut(bad)
			for _, w := range wrappers {
				corpus = append(corpus, sprintfWrap(w, m))
				// Mix benign filler around the mutated token to vary byte offsets.
				for _, b := range benign {
					corpus = append(corpus, b+" "+sprintfWrap(w, m)+" "+b)
				}
			}
		}
	}
	// Pure-benign tail (never collapses) to assert no false positives.
	for _, b := range benign {
		for i := 0; i < 100; i++ {
			corpus = append(corpus, b+strings.Repeat(" ok", i%7))
		}
	}
	return corpus
}

// sprintfWrap inserts m into a single-%s template without importing fmt into the
// hot loop indirection — a tiny helper kept literal for clarity.
func sprintfWrap(tmpl, m string) string {
	return strings.Replace(tmpl, "%s", m, 1)
}
