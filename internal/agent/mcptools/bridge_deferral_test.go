package mcptools

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// This file's budget-touching subtests are deliberately NOT t.Parallel(): the
// always-loaded slot counter (bridge_deferral.go's loadedSlotBudget) is a single
// package-level global, so two subtests spending it concurrently would race each
// other's counts. Every subtest that cares about the arithmetic instead calls
// resetLoadedSlotBudgetForTest() as its first line, which is what makes them
// order-independent against the rest of the package's tests without needing
// isolation via parallelism.

// TestDeferralCountModelFacing covers countModelFacing's four documented cases:
// a plain count, the real memory tool surface (10 advertised, 6 hidden -> 4),
// an empty listing, and exact-byte-string name comparison (no case folding).
func TestDeferralCountModelFacing(t *testing.T) {
	t.Run("plain count with nothing hidden", func(t *testing.T) {
		advertised := []*sdkmcp.Tool{
			mustTool("a", "a", nil, nil),
			mustTool("b", "b", nil, nil),
			mustTool("c", "c", nil, nil),
		}
		if got := countModelFacing(bridgePolicy{}, advertised); got != 3 {
			t.Fatalf("countModelFacing = %d, want 3", got)
		}
	})

	t.Run("memory policy hides six of the real ten tool names", func(t *testing.T) {
		// The real cmd/arcadedb-mcp surface: 10 advertised, 6 in
		// memoryHiddenFromModel (bridge_memory.go), so 4 model-facing.
		advertised := []*sdkmcp.Tool{
			mustTool("memory_entities", "d", nil, nil),
			mustTool("memory_digest", "d", nil, nil),
			mustTool("memory_merge_entities", "d", nil, nil),
			mustTool("memory_forget", "d", nil, nil),
			mustTool("graph_schema", "d", nil, nil),
			mustTool("memory_upsert_fact", "d", nil, nil),
			mustTool("memory_recall", "d", nil, nil),
			mustTool("memory_search", "d", nil, nil),
			mustTool("memory_facts_about", "d", nil, nil),
			mustTool("memory_reembed", "d", nil, nil),
		}
		if got := countModelFacing(bridgePolicy{memory: true}, advertised); got != 4 {
			t.Fatalf("countModelFacing (memory policy) = %d, want 4", got)
		}
	})

	t.Run("empty advertised list", func(t *testing.T) {
		if got := countModelFacing(bridgePolicy{}, nil); got != 0 {
			t.Fatalf("countModelFacing(nil) = %d, want 0", got)
		}
	})

	t.Run("exact byte comparison, no case folding", func(t *testing.T) {
		advertised := []*sdkmcp.Tool{
			mustTool("Send", "d", nil, nil),
			mustTool("send", "d", nil, nil),
		}
		if got := countModelFacing(bridgePolicy{}, advertised); got != 2 {
			t.Fatalf("countModelFacing(Send, send) = %d, want 2 (case-distinct)", got)
		}
	})
}

// TestDeferralGrantLoadedSlot covers grantLoadedSlot's ceiling boundary, the
// global 2-slot cap, and the "an over-ceiling or zero count never spends a slot"
// invariant.
func TestDeferralGrantLoadedSlot(t *testing.T) {
	t.Run("ceiling boundary: 2 loaded, 3 loaded, 4 deferred", func(t *testing.T) {
		resetLoadedSlotBudgetForTest()
		if !grantLoadedSlot("a", 2) {
			t.Fatal("count 2 must earn a slot on a fresh budget")
		}
		resetLoadedSlotBudgetForTest()
		if !grantLoadedSlot("b", 3) {
			t.Fatal("count 3 (the ceiling itself) must earn a slot on a fresh budget")
		}
		resetLoadedSlotBudgetForTest()
		if grantLoadedSlot("c", 4) {
			t.Fatal("count 4 (one past the ceiling) must never earn a slot")
		}
	})

	// VALIDATION.md flags this as the case most likely to be missed: the budget
	// is a GLOBAL 2-slot cap, not per-namespace, so a third individually
	// qualifying server must still be refused — fail closed.
	t.Run("global 2-slot cap fails closed on the third individually-qualifying grant", func(t *testing.T) {
		resetLoadedSlotBudgetForTest()
		if !grantLoadedSlot("first", 1) {
			t.Fatal("first grant on a fresh budget must succeed")
		}
		if !grantLoadedSlot("second", 1) {
			t.Fatal("second grant must succeed; the global cap is 2")
		}
		if grantLoadedSlot("third", 1) {
			t.Fatal("third grant must be refused; the global budget is exhausted")
		}
	})

	t.Run("an over-ceiling count is refused without spending the budget", func(t *testing.T) {
		resetLoadedSlotBudgetForTest()
		if grantLoadedSlot("big-1", 4) {
			t.Fatal("count 4 must be refused")
		}
		if grantLoadedSlot("big-2", 4) {
			t.Fatal("count 4 must be refused")
		}
		if !grantLoadedSlot("small", 1) {
			t.Fatal("two refused over-ceiling grants must not have spent the budget")
		}
	})

	t.Run("a zero count is refused without spending the budget", func(t *testing.T) {
		resetLoadedSlotBudgetForTest()
		if grantLoadedSlot("empty", 0) {
			t.Fatal("count 0 must never earn a slot")
		}
		if !grantLoadedSlot("real", 1) {
			t.Fatal("a zero-count refusal must not have spent the budget")
		}
	})
}

// TestDeferralDefaultDeferredZeroValue proves the zero-value bridgePolicy keeps
// every existing caller's unconditional "deferred" behaviour, and that
// alwaysLoaded:true is the only thing that flips it.
func TestDeferralDefaultDeferredZeroValue(t *testing.T) {
	if !(bridgePolicy{}).defaultDeferred() {
		t.Fatal("zero-value bridgePolicy.defaultDeferred() must be true")
	}
	if (bridgePolicy{alwaysLoaded: true}).defaultDeferred() {
		t.Fatal("bridgePolicy{alwaysLoaded: true}.defaultDeferred() must be false")
	}
}

// TestDeferralBridgeToolsAppliesTheArithmetic proves the arithmetic is actually
// wired into bridgeToolsWithPolicy: a qualifying server's bridged specs come out
// Deferred:false, and an over-ceiling server's specs stay Deferred:true.
func TestDeferralBridgeToolsAppliesTheArithmetic(t *testing.T) {
	resetLoadedSlotBudgetForTest()
	twoTools := []*sdkmcp.Tool{mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil)}
	for _, tool := range bridgeTools("qual", nil, twoTools, defaultMCPCallTimeout) {
		if tool.Spec().Deferred {
			t.Fatalf("%s: a 2-tool server on a fresh budget must earn an always-loaded slot (Deferred:false)", tool.Spec().Name)
		}
	}

	fiveTools := []*sdkmcp.Tool{
		mustTool("t1", "t", nil, nil), mustTool("t2", "t", nil, nil), mustTool("t3", "t", nil, nil),
		mustTool("t4", "t", nil, nil), mustTool("t5", "t", nil, nil),
	}
	for _, tool := range bridgeTools("over", nil, fiveTools, defaultMCPCallTimeout) {
		if !tool.Spec().Deferred {
			t.Fatalf("%s: a 5-tool server exceeds the ceiling and must stay Deferred:true", tool.Spec().Name)
		}
	}
}

// TestDeferralBridgeToolsEmptyAdvertisedIsSafe proves an empty tools/list
// bridges to zero tools without panicking and without spending the budget.
func TestDeferralBridgeToolsEmptyAdvertisedIsSafe(t *testing.T) {
	resetLoadedSlotBudgetForTest()
	bridged := bridgeTools("empty", nil, nil, defaultMCPCallTimeout)
	if len(bridged) != 0 {
		t.Fatalf("empty advertised list must bridge zero tools, got %d", len(bridged))
	}
	if !grantLoadedSlot("after-empty", 1) {
		t.Fatal("bridging an empty listing must not have spent the global slot budget")
	}
}

// TestDeferralWarnIfDeferralWouldFlip covers warnIfDeferralWouldFlip directly:
// it warns exactly once when the recomputed count crosses the ceiling relative
// to the frozen decision, and stays silent when the decision would not change.
// This is a pure function call, independent of the global slot budget (it never
// calls grantLoadedSlot), so no reset is needed here.
func TestDeferralWarnIfDeferralWouldFlip(t *testing.T) {
	captureWarnLogs := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		var logs bytes.Buffer
		old := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
		t.Cleanup(func() { slog.SetDefault(old) })
		return &logs
	}

	t.Run("warns when a would-no-longer-qualify reconnect crosses the ceiling", func(t *testing.T) {
		logs := captureWarnLogs(t)
		policy := bridgePolicy{alwaysLoaded: true, modelFacingCount: 2}
		fiveTools := []*sdkmcp.Tool{
			mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil), mustTool("c", "c", nil, nil),
			mustTool("d", "d", nil, nil), mustTool("e", "e", nil, nil),
		}
		warnIfDeferralWouldFlip("qual", policy, fiveTools)

		out := logs.String()
		for _, want := range []string{
			"mcp server deferral would change on reconnect",
			"namespace=qual",
			"frozen_deferred=false",
			"old_model_facing=2",
			"new_model_facing=5",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("WARN missing %q, got: %s", want, out)
			}
		}
		if strings.Count(out, "\n") != 1 {
			t.Fatalf("want exactly one WARN record, got: %s", out)
		}
	})

	t.Run("warns when a would-now-qualify reconnect crosses the ceiling", func(t *testing.T) {
		logs := captureWarnLogs(t)
		policy := bridgePolicy{alwaysLoaded: false, modelFacingCount: 5}
		twoTools := []*sdkmcp.Tool{mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil)}
		warnIfDeferralWouldFlip("qual", policy, twoTools)

		out := logs.String()
		for _, want := range []string{
			"mcp server deferral would change on reconnect",
			"namespace=qual",
			"frozen_deferred=true",
			"old_model_facing=5",
			"new_model_facing=2",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("WARN missing %q, got: %s", want, out)
			}
		}
	})

	t.Run("stays silent when the recomputed count leaves the decision unchanged", func(t *testing.T) {
		logs := captureWarnLogs(t)
		policy := bridgePolicy{alwaysLoaded: true, modelFacingCount: 2}
		twoTools := []*sdkmcp.Tool{mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil)}
		warnIfDeferralWouldFlip("qual", policy, twoTools)
		if logs.Len() != 0 {
			t.Fatalf("decision unchanged must not warn, got: %s", logs.String())
		}
	})
}
