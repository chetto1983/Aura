package mcptools

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
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
		// memoryHiddenFromModel (bridge_policy.go), so 4 model-facing.
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
		if got := countModelFacing(bridgePolicy{memorySurface: true}, advertised); got != 4 {
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

// captureWarnLogs swaps slog's default handler for a text handler over a
// buffer, restoring the original on cleanup. Shared by every test in this file
// that asserts on WARN output (task 2, D-27's reconnect-drift warning) so the
// swap-and-restore mechanics live in exactly one place, mirroring
// bridge_trust_test.go's TestBridgedToolRefreshSpecWarnsOnMutatingAndRequiredArgChanges,
// which already establishes this pattern for refreshSpec's other warn blocks.
func captureWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &logs
}

// TestDeferralWarnIfDeferralWouldFlip covers warnIfDeferralWouldFlip directly:
// it warns exactly once when the recomputed count crosses the ceiling relative
// to the frozen decision, and stays silent when the decision would not change.
// This is a pure function call, independent of the global slot budget (it never
// calls grantLoadedSlot), so no reset is needed here.
func TestDeferralWarnIfDeferralWouldFlip(t *testing.T) {
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

// findBridged returns the *bridgedTool in bridged whose raw (wire) name is raw,
// failing the test if none matches.
func findBridged(t *testing.T, bridged []tools.Tool, raw string) *bridgedTool {
	t.Helper()
	for _, tl := range bridged {
		if bt := tl.(*bridgedTool); bt.name == raw {
			return bt
		}
	}
	t.Fatalf("no bridged tool with raw name %q", raw)
	return nil
}

// TestRefreshSpecsLockedFreezeSurvivesReconnectBothDirections proves task 2's
// central claim: refreshSpecsLocked (the reconnect path's only entry point,
// bridge_supervisor.go) never recomputes the deferral decision, in BOTH
// directions across a listing that would flip the decision if it were
// recomputed. This already holds on today's code without any change here —
// bridgeToolsWithPolicy (task 1) froze policy.alwaysLoaded once per mount and
// stored an identical copy on every bridgedTool, and refreshSpec
// (bridge.go:82) reads only that frozen bit, never advertised's count. This
// test exercises the real wiring end to end (bridgeTools -> trackBridgedTools
// -> refreshSpecsLocked) rather than asserting the pure function in isolation,
// so a future change that accidentally threads advertised's count back into
// spec.Deferred would be caught here.
func TestRefreshSpecsLockedFreezeSurvivesReconnectBothDirections(t *testing.T) {
	t.Run("alwaysLoaded frozen true survives a reconnect listing that would no longer qualify", func(t *testing.T) {
		resetLoadedSlotBudgetForTest()
		twoTools := []*sdkmcp.Tool{mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil)}
		bridged := bridgeTools("qual", nil, twoTools, defaultMCPCallTimeout)
		for _, tool := range bridged {
			if tool.Spec().Deferred {
				t.Fatalf("precondition: %s must have earned a slot (Deferred:false) on a fresh budget", tool.Spec().Name)
			}
		}
		srv := NewMountedServer("qual", nil)
		srv.trackBridgedTools(bridged)

		fiveTools := []*sdkmcp.Tool{
			mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil), mustTool("c", "c", nil, nil),
			mustTool("d", "d", nil, nil), mustTool("e", "e", nil, nil),
		}
		srv.mu.Lock()
		srv.refreshSpecsLocked(fiveTools)
		srv.mu.Unlock()

		if findBridged(t, bridged, "a").Spec().Deferred || findBridged(t, bridged, "b").Spec().Deferred {
			t.Fatal("frozen alwaysLoaded:true must survive a reconnect listing that would no longer qualify: Deferred must stay false")
		}
	})

	t.Run("alwaysLoaded frozen false survives a reconnect listing that would now qualify", func(t *testing.T) {
		resetLoadedSlotBudgetForTest()
		fiveTools := []*sdkmcp.Tool{
			mustTool("t1", "t", nil, nil), mustTool("t2", "t", nil, nil), mustTool("t3", "t", nil, nil),
			mustTool("t4", "t", nil, nil), mustTool("t5", "t", nil, nil),
		}
		bridged := bridgeTools("over", nil, fiveTools, defaultMCPCallTimeout)
		for _, tool := range bridged {
			if !tool.Spec().Deferred {
				t.Fatalf("precondition: %s must exceed the ceiling and stay Deferred:true", tool.Spec().Name)
			}
		}
		srv := NewMountedServer("over", nil)
		srv.trackBridgedTools(bridged)

		twoTools := []*sdkmcp.Tool{mustTool("t1", "t", nil, nil), mustTool("t2", "t", nil, nil)}
		srv.mu.Lock()
		srv.refreshSpecsLocked(twoTools)
		srv.mu.Unlock()

		if !findBridged(t, bridged, "t1").Spec().Deferred || !findBridged(t, bridged, "t2").Spec().Deferred {
			t.Fatal("frozen alwaysLoaded:false must survive a reconnect listing that would now qualify: Deferred must stay true")
		}
	})
}

// TestRefreshSpecsLockedWarnsWhenReconnectWouldFlipDeferral proves
// refreshSpecsLocked (bridge_supervisor.go) is wired to warnIfDeferralWouldFlip:
// a reconnect listing that crosses the ceiling relative to the frozen decision
// emits exactly the WARN warnIfDeferralWouldFlip itself produces, and an
// unchanged listing stays silent. Unlike TestDeferralWarnIfDeferralWouldFlip
// (which calls the pure function directly), this drives it through the real
// reconnect entry point so the wiring itself — not just the function — is
// under test.
func TestRefreshSpecsLockedWarnsWhenReconnectWouldFlipDeferral(t *testing.T) {
	t.Run("warns when the reconnect listing crosses the ceiling", func(t *testing.T) {
		resetLoadedSlotBudgetForTest()
		logs := captureWarnLogs(t)
		twoTools := []*sdkmcp.Tool{mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil)}
		bridged := bridgeTools("qual", nil, twoTools, defaultMCPCallTimeout)
		srv := NewMountedServer("qual", nil)
		srv.trackBridgedTools(bridged)

		fiveTools := []*sdkmcp.Tool{
			mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil), mustTool("c", "c", nil, nil),
			mustTool("d", "d", nil, nil), mustTool("e", "e", nil, nil),
		}
		srv.mu.Lock()
		srv.refreshSpecsLocked(fiveTools)
		srv.mu.Unlock()

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
	})

	t.Run("stays silent when the reconnect listing leaves the decision unchanged", func(t *testing.T) {
		resetLoadedSlotBudgetForTest()
		logs := captureWarnLogs(t)
		twoTools := []*sdkmcp.Tool{mustTool("a", "a", nil, nil), mustTool("b", "b", nil, nil)}
		bridged := bridgeTools("qual", nil, twoTools, defaultMCPCallTimeout)
		srv := NewMountedServer("qual", nil)
		srv.trackBridgedTools(bridged)

		srv.mu.Lock()
		srv.refreshSpecsLocked(twoTools)
		srv.mu.Unlock()

		if logs.Len() != 0 {
			t.Fatalf("unchanged decision must not warn, got: %s", logs.String())
		}
	})
}

func bridgeTools(namespace string, srv *MountedServer, advertised []*sdkmcp.Tool, callTimeout time.Duration) []tools.Tool {
	return bridgeToolsWithPolicy(namespace, srv, advertised, callTimeout, defaultBridgePolicy(namespace))
}

func specFromToolDef(namespace string, t *sdkmcp.Tool) tools.Spec {
	return specFromToolDefWithPolicy(namespace, t, defaultBridgePolicy(namespace))
}

// resetLoadedSlotBudgetForTest resets the package-level slot counter. Test-only:
// production has no unmount path to justify resetting it, and no caller outside
// a _test.go file should ever need to.
func resetLoadedSlotBudgetForTest() {
	loadedSlotBudget.mu.Lock()
	defer loadedSlotBudget.mu.Unlock()
	loadedSlotBudget.spent = 0
}
