package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/semindex"
)

// TestGuardedTiebreak_PromotesConfidentBM25Hit: when BM25 returns a SMALL set (≤5)
// and the hit is inside embedding's top-15, the guard promotes it above an embedding
// near-tie. Proves the tiebreak actually FIRES (not just no-ops).
func TestGuardedTiebreak_PromotesConfidentBM25Hit(t *testing.T) {
	t.Setenv("AURA_TOOLSEARCH_FUSION", "guarded")
	emb := []semindex.Scored{
		{Label: "alpha", Score: 0.80},
		{Label: "beta", Score: 0.79}, // near-tie with alpha
	}
	// BM25 confidently prefers beta (small result-set, beta in embedding top-15).
	out := guardedTiebreak(emb, []string{"beta"})
	if out[0].Label != "beta" {
		t.Fatalf("guarded tiebreak did not promote the confident BM25 hit: got %s first", out[0].Label)
	}
}

// TestGuardedTiebreak_NoOpWhenFlooded: a large BM25 result-set (>5 = noisy) is a
// strict no-op; the embedding order is returned unchanged (spike-056: a flooding BM25
// has no confident signal). Also covers the kill-switch and out-of-top-15 cases.
func TestGuardedTiebreak_NoOpWhenFlooded(t *testing.T) {
	t.Setenv("AURA_TOOLSEARCH_FUSION", "guarded")
	emb := []semindex.Scored{{Label: "alpha", Score: 0.80}, {Label: "beta", Score: 0.79}}

	flooded := []string{"beta", "c", "d", "e", "f", "g"} // 6 > 5 -> no-op
	if out := guardedTiebreak(emb, flooded); out[0].Label != "alpha" {
		t.Errorf("flooded BM25 must be a no-op, got %s first", out[0].Label)
	}
	// A confident BM25 hit OUTSIDE embedding's top-15 is ignored (intersection gate).
	wide := make([]semindex.Scored, 0, 20)
	for i := range 20 {
		wide = append(wide, semindex.Scored{Label: fmt.Sprintf("t%02d", i), Score: 1.0 - float64(i)*0.01})
	}
	if out := guardedTiebreak(wide, []string{"t19"}); out[0].Label != "t00" {
		t.Errorf("BM25 hit outside top-15 must be ignored, got %s first", out[0].Label)
	}
	// Kill-switch off => unchanged regardless.
	t.Setenv("AURA_TOOLSEARCH_FUSION", "off")
	if out := guardedTiebreak(emb, []string{"beta"}); out[0].Label != "alpha" {
		t.Errorf("fusion off must be a no-op, got %s first", out[0].Label)
	}
}

// TestToolSearch_IncrementalMountEmbedsOnlyDelta (Req-3 / D-03): the FIRST search
// embeds every deferred tool (N); after N more deferred tools are registered and
// InvalidateIndex() fires (the MCP-mount seam), the NEXT search embeds ONLY the N
// additions (exactly N more Embed-texts, not a full re-embed of 2N), and the new
// tools are immediately searchable. Asserts the embedded-text counter, the load-bearing
// "delta only" cost guarantee.
func TestToolSearch_IncrementalMountEmbedsOnlyDelta(t *testing.T) {
	reg := NewRegistry()
	const n = 4
	for i := range n {
		reg.Register(bm25Tool{name: fmt.Sprintf("alpha_%d", i), desc: fmt.Sprintf("alpha capability number %d", i)})
	}
	emb := &bagEmbedder{}
	ts := &ToolSearch{Registry: reg, Embed: emb}
	ctx := ctxWith(t, "sess-delta", "call-delta")

	// First search embeds all N deferred tools.
	if _, err := ts.Execute(ctx, []byte(`{"query":"alpha capability"}`)); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	emb.mu.Lock()
	afterFirst := emb.embedded
	emb.mu.Unlock()
	// N tools + 1 query embed.
	if afterFirst != n+1 {
		t.Fatalf("first search embedded %d texts, want %d (N tools + 1 query)", afterFirst, n+1)
	}

	// Mount N MORE deferred tools, then signal the delta via the zero-arg hook.
	for i := range n {
		reg.Register(bm25Tool{name: fmt.Sprintf("beta_%d", i), desc: fmt.Sprintf("beta gadget number %d", i)})
	}
	ts.InvalidateIndex()

	if _, err := ts.Execute(ctx, []byte(`{"query":"beta gadget number 0"}`)); err != nil {
		t.Fatalf("post-mount Execute: %v", err)
	}
	emb.mu.Lock()
	delta := emb.embedded - afterFirst
	emb.mu.Unlock()
	// EXACTLY the N new tools + 1 query embed — NOT a full re-embed of 2N.
	if delta != n+1 {
		t.Fatalf("post-mount search embedded %d new texts, want %d (N delta tools + 1 query, NOT a full re-embed)", delta, n+1)
	}

	// The newly-mounted tools are immediately searchable.
	res, err := ts.Execute(ctx, []byte(`{"query":"beta gadget number 2","max_results":1}`))
	if err != nil {
		t.Fatalf("search for new tool: %v", err)
	}
	if !strings.Contains(res.Preview, "## beta_2") {
		t.Fatalf("newly-mounted beta_2 was not searchable after the delta embed: %q", res.Preview)
	}
}

// TestToolSearch_ConcurrentMountAndSearch (Req-3 / risk #5): InvalidateIndex (the
// write-append path) and Execute (the read path) racing concurrently must be
// -race clean — the semindex.Ranker RWMutex + the ToolSearch mutex cover the bank.
func TestToolSearch_ConcurrentMountAndSearch(t *testing.T) {
	reg := NewRegistry()
	for i := range 6 {
		reg.Register(bm25Tool{name: fmt.Sprintf("tool_%d", i), desc: fmt.Sprintf("capability number %d", i)})
	}
	ts := &ToolSearch{Registry: reg, Embed: &bagEmbedder{}}
	ctx := ctxWith(t, "sess-conc", "call-conc")

	var wg sync.WaitGroup
	// Searchers.
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ts.Execute(ctx, []byte(`{"query":"capability number 3"}`)); err != nil {
				t.Errorf("concurrent Execute: %v", err)
			}
		}()
	}
	// Refreshers (the mount seam) racing the searches.
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ts.InvalidateIndex()
		}()
	}
	wg.Wait()
}

// TestToolSearch_CacheInvariantAcrossNQueries (Req-7): per-query ranking happens
// INSIDE Execute only — across N DIFFERENT queries the tool_search Spec().Description
// is byte-identical and carries no timestamp-shaped substring. A per-query mutation
// of the Description (or messages[0]) would poison the OpenRouter implicit cache.
func TestToolSearch_CacheInvariantAcrossNQueries(t *testing.T) {
	ts, ctx := newSearch(t,
		bm25Tool{name: "web_fetch", desc: "retrieve a url and render markdown"},
		bm25Tool{name: "calculator", desc: "evaluate arithmetic expressions"},
		bm25Tool{name: "calendar", desc: "list meetings and appointments"},
		bm25Tool{name: "github__create_issue", desc: "open an issue"},
	)
	baseline := ts.Spec().Description
	queries := []string{
		"retrieve a web page",
		"evaluate arithmetic",
		"list my meetings",
		"open a github issue",
		"completely unrelated nonsense xyzzy",
	}
	for _, q := range queries {
		raw, _ := json.Marshal(map[string]string{"query": q})
		if _, err := ts.Execute(ctx, raw); err != nil {
			t.Fatalf("Execute(%q): %v", q, err)
		}
		got := ts.Spec().Description
		if got != baseline {
			t.Fatalf("Description mutated after query %q (cache poisoning):\nbase: %q\ngot:  %q", q, baseline, got)
		}
		if loc := descClockShape.FindString(got); loc != "" {
			t.Fatalf("Description contains a timestamp-shaped substring %q after query %q (cache poisoning)", loc, q)
		}
	}
}
