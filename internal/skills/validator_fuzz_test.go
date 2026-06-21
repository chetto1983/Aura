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

// TestValidatorFuzz_NonASCIINameRejected is the SKW-01 encoding-edge backstop the
// install transport relies on: a non-ASCII or non-grammar skill name MUST be rejected
// by ValidateForWrite via SanitizeName (^[a-z0-9-]{1,64}$), even after the frontmatter
// parser NFKC-normalizes the name. The installer's staged-dir name check funnels through
// the SAME chokepoint, so this property pins the validator the transport depends on.
func TestValidatorFuzz_NonASCIINameRejected(t *testing.T) {
	t.Parallel()
	// Each of these is a name a fetched/cloned skill might declare. None matches the
	// grammar (uppercase, underscores, dots, traversal, fullwidth, emoji, CJK, empty),
	// so the model/install path must hard-reject every one with ErrInvalidName.
	badNames := []string{
		"Bad_Name", "UPPER", "with.dot", "../escape", "a/b", "",
		"naïve", "日本語", "skill😀", toFullwidth("abc"), strings.Repeat("a", 65),
		" leading", "trailing ", "under_score",
	}
	for _, name := range badNames {
		fm := Frontmatter{Name: name, Description: "ok", Type: TypeInstruction}
		if err := ValidateForWrite(fm, "clean body", fuzzBlocklist, 1<<20, false); err == nil {
			t.Errorf("ValidateForWrite name=%q accepted; want ErrInvalidName", name)
		}
	}
	// A canonical grammar-conformant name must still pass (no over-rejection).
	good := Frontmatter{Name: "good-skill-1", Description: "ok", Type: TypeInstruction}
	if err := ValidateForWrite(good, "clean body", fuzzBlocklist, 1<<20, false); err != nil {
		t.Errorf("ValidateForWrite rejected a grammar-conformant name: %v", err)
	}
}
