//go:build measure

// Measurement harness (NOT a gate): does the Okapi BM25 ranker generalize, and would
// a length-normalization variant (BM25L / BM25+) do better?
//
//	go test -tags measure ./internal/agent/tools -run TestMeasureBM25Variants -v
//
// The shipped gate corpus (search_gate_test.go) scores 100%/100%, but it is a
// REGRESSION suite: amendment #194 added the six phrasings that had just failed in
// production, so the ranker has been tuned against it. heldOutCases below were written
// against the tool inventory and never used to tune anything.
//
// English only, deliberately. The model knows the tool surface is English and writes its
// discovery queries in English regardless of the operator's language, which is the
// distribution search_gate_test.go already records.
package tools

import (
	"math"
	"slices"
	"testing"
)

// heldOutCases probe the two axes that matter for this corpus.
//
// PARAPHRASE: the query avoids the tool's own vocabulary. The indexed document is only
// name + Summary + argument names (searchDocument), so a lexical ranker has very little
// surface to match against and vocabulary mismatch is its known failure mode.
//
// NEAR-DUPLICATE: 50 of the 85 corpus tools are Linear's get_*/list_*/save_* families,
// whose summaries differ by a verb and a noun. Exact term matching should be STRONG here,
// and it is the case a semantic ranker is most likely to blur — so a fair test has to
// carry it, not only the cases lexical matching loses.
var heldOutCases = []gateCase{
	// --- paraphrase, built-ins ---
	{"what does the clock say right now", []string{"current_time"}},
	{"stop the job I started earlier", []string{"shell_kill"}},
	{"has my long build finished yet", []string{"shell_poll"}},
	{"look this up online", []string{"web_search"}},
	{"grab the text of that page", []string{"web_fetch"}},
	{"split this across several helpers working at once", []string{"swarm_spawn"}},
	{"note down the steps still to do", []string{"todo_write"}},
	{"teach yourself a new capability", []string{"skill_manage"}},
	{"do this again every morning", []string{"task"}},
	{"what do you already know about me", []string{"memory__memory_recall"}},
	{"store that my dog is called Argo", []string{"memory__memory_upsert_fact"}},
	{"book a meeting for next Tuesday", []string{"calendar__calendar"}},
	{"drop Marco a line on WhatsApp", []string{"whatsapp__send_message"}},
	{"what did we say to each other last week", []string{"whatsapp__list_messages"}},
	// --- near-duplicate discrimination, Linear ---
	{"details of one specific project", []string{"linear__get_project"}},
	{"which projects exist in the workspace", []string{"linear__list_projects"}},
	{"open a new ticket", []string{"linear__save_issue"}},
	{"which teams are there", []string{"linear__list_teams"}},
	{"details for a single person", []string{"linear__get_user"}},
	{"everyone in the workspace", []string{"linear__list_users"}},
	{"what statuses can an issue be in", []string{"linear__list_issue_statuses"}},
	{"write a reply under an issue", []string{"linear__save_comment"}},
	{"remove an attached file", []string{"linear__delete_attachment"}},
	{"which cycles does this team run", []string{"linear__list_cycles"}},
}

// scorer is one ranking formula over the shared index.
type scorer func(idx *bm25Index, doc int, qterms []string) float64

// okapiScore is the SHIPPED formula, restated here so all variants are scored through one
// harness rather than compared against a different code path.
func okapiScore(idx *bm25Index, doc int, qterms []string) float64 {
	tf, docLen := idx.tf[doc], float64(idx.docLn[doc])
	var score float64
	for _, t := range qterms {
		f := float64(tf[t])
		if f == 0 {
			continue
		}
		score += idx.idf[t] * (f * (bm25K1 + 1)) / (f + bm25K1*(1-bm25B+bm25B*docLen/idx.avgdl))
	}
	return score
}

// bm25LScore (delta) normalizes tf by length FIRST and then shifts it, so a long document
// is not pushed toward zero the way Okapi's denominator pushes it. This corpus has very
// uneven lengths — Linear's verbose summaries against current_time's single line — which
// is exactly the condition the variant was designed for.
func bm25LScore(delta float64) scorer {
	return func(idx *bm25Index, doc int, qterms []string) float64 {
		tf, docLen := idx.tf[doc], float64(idx.docLn[doc])
		var score float64
		for _, t := range qterms {
			f := float64(tf[t])
			if f == 0 {
				continue
			}
			c := f / (1 - bm25B + bm25B*docLen/idx.avgdl)
			score += idx.idf[t] * ((bm25K1 + 1) * (c + delta)) / (bm25K1 + c + delta)
		}
		return score
	}
}

// bm25PlusScore adds a flat floor to every matched term, guaranteeing that a long document
// containing the term still outranks one that does not contain it at all.
func bm25PlusScore(delta float64) scorer {
	return func(idx *bm25Index, doc int, qterms []string) float64 {
		tf, docLen := idx.tf[doc], float64(idx.docLn[doc])
		var score float64
		for _, t := range qterms {
			f := float64(tf[t])
			if f == 0 {
				continue
			}
			norm := (f * (bm25K1 + 1)) / (f + bm25K1*(1-bm25B+bm25B*docLen/idx.avgdl))
			score += idx.idf[t] * (norm + delta)
		}
		return score
	}
}

func rankWith(idx *bm25Index, specs []Spec, query string, score scorer) []string {
	qterms := tokenize(query)
	type sd struct {
		doc int
		s   float64
	}
	var out []sd
	for i := range idx.tf {
		if s := score(idx, i, qterms); s > 0 {
			out = append(out, sd{i, s})
		}
	}
	slices.SortStableFunc(out, func(a, b sd) int {
		if a.s != b.s {
			if a.s > b.s {
				return -1
			}
			return 1
		}
		return a.doc - b.doc
	})
	names := make([]string, 0, len(out))
	for _, r := range out {
		names = append(names, specs[r.doc].Name)
	}
	return names
}

func evaluate(idx *bm25Index, specs []Spec, cases []gateCase, score scorer) (top1, recall3 float64, misses []string) {
	var t1, r3 int
	for _, c := range cases {
		got := rankWith(idx, specs, c.query, score)
		if len(got) > 0 && isGold(got[0], c.gold) {
			t1++
		}
		hit := false
		for _, n := range got[:min(3, len(got))] {
			if isGold(n, c.gold) {
				hit = true
			}
		}
		if hit {
			r3++
		} else {
			first := "(nothing)"
			if len(got) > 0 {
				first = got[0]
			}
			misses = append(misses, c.query+" -> "+first+" (want "+c.gold[0]+")")
		}
	}
	n := float64(len(cases))
	return float64(t1) / n, float64(r3) / n, misses
}

func TestMeasureBM25Variants(t *testing.T) {
	specs := loadManifestFixture(t)
	idx := newBM25Index(specs)

	lengths := append([]int(nil), idx.docLn...)
	slices.Sort(lengths)
	t.Logf("corpus: %d tools, doc length min=%d median=%d max=%d avg=%.1f",
		len(specs), lengths[0], lengths[len(lengths)/2], lengths[len(lengths)-1], idx.avgdl)
	t.Logf("")

	for _, v := range []struct {
		name  string
		score scorer
	}{
		{"Okapi BM25 (shipped)", okapiScore},
		{"BM25L delta=0.5", bm25LScore(0.5)},
		{"BM25L delta=1.0", bm25LScore(1.0)},
		{"BM25+ delta=1.0", bm25PlusScore(1.0)},
	} {
		gt1, gr3, _ := evaluate(idx, specs, gateCases, v.score)
		ht1, hr3, misses := evaluate(idx, specs, heldOutCases, v.score)
		t.Logf("%-22s  GATE top-1 %3.0f%% recall@3 %3.0f%%   |   HELD-OUT top-1 %3.0f%% recall@3 %3.0f%%",
			v.name, gt1*100, gr3*100, ht1*100, hr3*100)
		if math.Abs(ht1-0) < 1e9 && len(misses) > 0 && v.name == "Okapi BM25 (shipped)" {
			for _, m := range misses {
				t.Logf("      held-out recall@3 MISS: %s", m)
			}
		}
	}
}
