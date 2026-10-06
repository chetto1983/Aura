//go:build measure

// Measurement harness (NOT a gate): what retrievalKeywords buys, scored with and
// without it over the same corpus.
//
//	go test -tags measure ./internal/agent/tools -run TestMeasureRetrievalKeywords -v
//
// testdata/blind_eval_2026-10-06.json was written by a separate agent that saw only the
// tool names and summaries in deferred_manifest.json — not the ranker, not the tests, not
// the keywords, which were fixed before the set was opened. It is blind ONCE: anyone who
// tunes the keywords against its misses spends that, and must write a new set to measure
// the next change. Negatives are the queries of a family scored on a corpus without that
// family, plus requests no tool serves; for them the right answer is an empty ranking.
package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

type blindEvalSet struct {
	Cases []struct {
		Query string   `json:"query"`
		Lang  string   `json:"lang"`
		Gold  []string `json:"gold"`
	} `json:"cases"`
	OutOfScope []struct {
		Query string `json:"query"`
	} `json:"out_of_scope"`
}

var unservedRequests = []string{
	"order a pizza for delivery", "book a flight to Rome", "transfer money to a bank account",
	"turn on the living room lights", "translate a video into Spanish", "play a song on spotify",
	"check my car's tire pressure", "post a tweet", "open a pull request on github",
	"query the postgres database", "compress a folder into a zip archive", "convert a pdf to word",
}

func rankedNames(specs []Spec, query string) []string {
	idx := newBM25Index(specs)
	var out []string
	for _, r := range idx.rank(query) {
		out = append(out, specs[r.doc].Name)
	}
	return out
}

func withoutFamily(specs []Spec, family string) []Spec {
	return slices.DeleteFunc(slices.Clone(specs), func(s Spec) bool {
		return strings.HasPrefix(s.Name, family+nsDelimiterStr)
	})
}

func scoreRetrieval(specs []Spec, cases []gateCase) (top1, recall5 int, misses []string) {
	for _, c := range cases {
		got := rankedNames(specs, c.query)
		at := slices.IndexFunc(got, func(n string) bool { return isGold(n, c.gold) })
		if at == 0 {
			top1++
		}
		if at >= 0 && at < 5 {
			recall5++
			continue
		}
		first := "(nothing)"
		if len(got) > 0 {
			first = got[0]
		}
		misses = append(misses, fmt.Sprintf("%s -> %s (want %s)", c.query, first, c.gold[0]))
	}
	return top1, recall5, misses
}

func TestMeasureRetrievalKeywords(t *testing.T) {
	corpus := loadManifestFixture(t)
	raw, err := os.ReadFile("testdata/blind_eval_2026-10-06.json")
	if err != nil {
		t.Fatalf("read blind eval: %v", err)
	}
	var blind blindEvalSet
	if err := json.Unmarshal(raw, &blind); err != nil {
		t.Fatalf("decode blind eval: %v", err)
	}
	byLang := map[string][]gateCase{}
	for _, c := range blind.Cases {
		byLang[c.Lang] = append(byLang[c.Lang], gateCase{c.Query, c.Gold})
	}
	positives := slices.Concat(gateCases, heldOutCases, byLang["en"], byLang["it"])

	type negative struct{ family, query string }
	var negatives []negative
	for _, fam := range []string{"memory", "calendar", "whatsapp"} {
		for _, c := range positives {
			if strings.HasPrefix(c.gold[0], fam+nsDelimiterStr) {
				negatives = append(negatives, negative{fam, c.query})
			}
		}
	}
	for _, q := range unservedRequests {
		negatives = append(negatives, negative{query: q})
	}
	for _, c := range blind.OutOfScope {
		negatives = append(negatives, negative{query: c.Query})
	}

	keywords := retrievalKeywords
	defer func() { retrievalKeywords = keywords }()
	for _, mode := range []string{"without", "with"} {
		retrievalKeywords = nil
		if mode == "with" {
			retrievalKeywords = keywords
		}
		for _, set := range []struct {
			name  string
			cases []gateCase
		}{{"gate", gateCases}, {"heldout", heldOutCases}, {"blind-en", byLang["en"]}, {"blind-it", byLang["it"]}} {
			top1, recall5, misses := scoreRetrieval(corpus, set.cases)
			t.Logf("%-7s keywords  %-8s top-1 %2d/%d  recall@5 %2d/%d", mode, set.name, top1, len(set.cases), recall5, len(set.cases))
			if mode == "with" {
				for _, m := range misses {
					t.Logf("        miss: %s", m)
				}
			}
		}
		empty := 0
		for _, n := range negatives {
			scored := corpus
			if n.family != "" {
				scored = withoutFamily(corpus, n.family)
			}
			if len(rankedNames(scored, n.query)) == 0 {
				empty++
			}
		}
		t.Logf("%-7s keywords  negatives empty ranking %d/%d", mode, empty, len(negatives))
	}
}
