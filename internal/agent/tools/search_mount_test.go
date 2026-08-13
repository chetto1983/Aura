package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestToolSearch_MountMakesNewToolsSearchable: after an MCP mount registers more
// deferred tools, InvalidateIndex (the zero-arg bridge seam) drops the index and the
// next search ranks over the enlarged set. This used to assert an incremental
// re-embed budget; the index is now rebuilt outright, because rebuilding it is CPU
// only and the delta bookkeeping existed solely to avoid re-embedding over a network.
func TestToolSearch_MountMakesNewToolsSearchable(t *testing.T) {
	reg := NewRegistry()
	const n = 4
	for i := range n {
		reg.Register(bm25Tool{name: fmt.Sprintf("alpha_%d", i), summary: fmt.Sprintf("alpha capability number %d", i)})
	}
	ts := &ToolSearch{Registry: reg}
	ctx := ctxWith(t, "sess-delta", "call-delta")

	if _, err := ts.Execute(ctx, []byte(`{"query":"alpha capability"}`)); err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	for i := range n {
		reg.Register(bm25Tool{name: fmt.Sprintf("beta_%d", i), summary: fmt.Sprintf("beta gadget number %d", i)})
	}
	ts.InvalidateIndex()

	res, err := ts.Execute(ctx, []byte(`{"query":"beta gadget number 2","max_results":1}`))
	if err != nil {
		t.Fatalf("search for new tool: %v", err)
	}
	if !strings.Contains(res.Preview, "## beta_2") {
		t.Fatalf("newly-mounted beta_2 was not searchable after the mount: %q", res.Preview)
	}
}

// TestToolSearch_StaleIndexIsNotServedAfterMount: registering tools WITHOUT the
// invalidate signal must not surface them — and the signal must make them surface.
// This is the failure the old delta bookkeeping could hide: an index that answers
// from a set the registry no longer has.
func TestToolSearch_StaleIndexIsNotServedAfterMount(t *testing.T) {
	reg := NewRegistry()
	reg.Register(bm25Tool{name: "alpha_tool", summary: "alpha capability"})
	ts := &ToolSearch{Registry: reg}
	ctx := ctxWith(t, "sess-stale", "call-stale")

	if _, err := ts.Execute(ctx, []byte(`{"query":"alpha capability"}`)); err != nil {
		t.Fatalf("warm the index: %v", err)
	}
	reg.Register(bm25Tool{name: "gamma_tool", summary: "gamma widget"})

	res, err := ts.Execute(ctx, []byte(`{"query":"gamma widget"}`))
	if err != nil {
		t.Fatalf("pre-invalidate Execute: %v", err)
	}
	if strings.Contains(res.Preview, "## gamma_tool") {
		t.Fatal("a tool registered without InvalidateIndex was served from a stale index")
	}

	ts.InvalidateIndex()
	res, err = ts.Execute(ctx, []byte(`{"query":"gamma widget"}`))
	if err != nil {
		t.Fatalf("post-invalidate Execute: %v", err)
	}
	if !strings.Contains(res.Preview, "## gamma_tool") {
		t.Fatalf("InvalidateIndex did not pick up the new tool: %q", res.Preview)
	}
}

// TestToolSearch_ConcurrentMountAndSearch (Req-3 / risk #5): InvalidateIndex (the
// write-append path) and Execute (the read path) racing concurrently must be
// -race clean: one mutex covers the index pointer and the slices aligned to it.
func TestToolSearch_ConcurrentMountAndSearch(t *testing.T) {
	reg := NewRegistry()
	for i := range 6 {
		reg.Register(bm25Tool{name: fmt.Sprintf("tool_%d", i), summary: fmt.Sprintf("capability number %d", i)})
	}
	ts := &ToolSearch{Registry: reg}
	ctx := ctxWith(t, "sess-conc", "call-conc")

	var wg sync.WaitGroup
	// Searchers.
	for range 12 {
		wg.Go(func() {
			if _, err := ts.Execute(ctx, []byte(`{"query":"capability number 3"}`)); err != nil {
				t.Errorf("concurrent Execute: %v", err)
			}
		})
	}
	// Refreshers (the mount seam) racing the searches.
	for range 6 {
		wg.Go(func() {
			ts.InvalidateIndex()
		})
	}
	wg.Wait()
}

// TestToolSearch_CacheInvariantAcrossNQueries (Req-7): per-query ranking happens
// INSIDE Execute only — across N DIFFERENT queries the tool_search Spec().Description
// is byte-identical and carries no timestamp-shaped substring. A per-query mutation
// of the Description (or messages[0]) would poison the OpenRouter implicit cache.
func TestToolSearch_CacheInvariantAcrossNQueries(t *testing.T) {
	ts, ctx := newSearch(t,
		bm25Tool{name: "web_fetch", summary: "retrieve a url and render markdown"},
		bm25Tool{name: "calculator", summary: "evaluate arithmetic expressions"},
		bm25Tool{name: "calendar", summary: "list meetings and appointments"},
		bm25Tool{name: "github__create_issue", summary: "open an issue"},
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
