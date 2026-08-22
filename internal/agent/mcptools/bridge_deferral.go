// bridge_deferral.go implements D-27's always-loaded-slot arithmetic
// (requirement TOOL-14, PRD amendment #123, prd.md:154): a mounted MCP server
// whose model-facing tool count is <= maxAlwaysLoadedMCPTools earns an
// always-loaded manifest slot, capped globally at maxAlwaysLoadedMCPSlots
// simultaneously-loaded servers, granted in mount order (BuiltInCatalog's
// sorted order, internal/mcp/manager/catalog.go), overflow deferred.
//
// The tiering axis used to be schema size; the operator's decision
// (docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md §2) made it
// frequency + count instead: the calendar curated fork exposes 1 model-facing
// tool and WhatsApp's exposes 3 (1 curated multiplex tool plus the 2 view-bound
// raw reads its fork deliberately keeps outside the merge, because their live
// MCP Apps views break under CallReadOnlyTool's Mutating gate) — both qualify,
// WhatsApp with zero headroom to spare. Memory exposes 4 model-facing tools
// (10 advertised, 6 hidden by bridgePolicy.modelFacing) and therefore stays
// deferred exactly as it does today; that leaves memory's own tiering to
// Phase 48 rather than an implicit side effect of this arithmetic. N=1 was
// rejected as brittle: a fork that split one verb into two tools would fall off
// the cliff for no reason related to what the model actually carries. Both
// numbers are Go constants, not env vars — no declaration ceremony is needed at
// mount time, and the AURA_MCP_* env catalogue is already in measured debt.
package mcptools

import (
	"log/slog"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxAlwaysLoadedMCPTools is the per-server model-facing tool ceiling a mount
// must not exceed to earn an always-loaded slot.
const maxAlwaysLoadedMCPTools = 3

// maxAlwaysLoadedMCPSlots is the global cap on simultaneously always-loaded MCP
// servers, regardless of how many individually qualify.
const maxAlwaysLoadedMCPSlots = 2

// loadedSlotBudget is the process-lifetime always-loaded slot counter. It only
// grows: tools.Registry has no Unregister and no unmount path exists
// (bridge_supervisor.go's D-104 invariant), so a granted slot is never
// released. Mounts happen at boot in BuiltInCatalog's sorted order, which is
// what makes the grant deterministic across restarts.
var loadedSlotBudget struct {
	mu    sync.Mutex
	spent int
}

// countModelFacing counts advertised's tools AFTER policy.modelFacing
// filtering, comparing names as exact byte strings — no case folding, no
// Unicode normalization, no trimming, so "Send" and "send" count as two tools.
func countModelFacing(policy bridgePolicy, advertised []*sdkmcp.Tool) int {
	n := 0
	for _, t := range advertised {
		if policy.modelFacing(t.Name) {
			n++
		}
	}
	return n
}

// grantLoadedSlot decides whether namespace's mount earns one of the
// maxAlwaysLoadedMCPSlots global always-loaded slots for a mount exposing
// modelFacing model-facing tools. A count of 0 or above maxAlwaysLoadedMCPTools
// is refused WITHOUT consuming a slot — an empty or oversized server can never
// starve a real one out of the manifest. Otherwise one slot is consumed if any
// remain, so a third individually-qualifying server still fails closed once the
// global budget is spent.
func grantLoadedSlot(namespace string, modelFacing int) bool {
	if modelFacing == 0 || modelFacing > maxAlwaysLoadedMCPTools {
		return false
	}
	loadedSlotBudget.mu.Lock()
	defer loadedSlotBudget.mu.Unlock()
	if loadedSlotBudget.spent >= maxAlwaysLoadedMCPSlots {
		slog.Info("mcp always-loaded slot refused: budget exhausted",
			"namespace", namespace, "model_facing", modelFacing)
		return false
	}
	loadedSlotBudget.spent++
	slog.Info("mcp always-loaded slot granted",
		"namespace", namespace, "model_facing", modelFacing,
		"slots_remaining", maxAlwaysLoadedMCPSlots-loadedSlotBudget.spent)
	return true
}

// resetLoadedSlotBudgetForTest resets the package-level slot counter. Test-only:
// production has no unmount path to justify resetting it, and no caller outside
// a _test.go file should ever need to.
func resetLoadedSlotBudgetForTest() {
	loadedSlotBudget.mu.Lock()
	defer loadedSlotBudget.mu.Unlock()
	loadedSlotBudget.spent = 0
}

// warnIfDeferralWouldFlip reports — and never applies — a reconnect whose
// recomputed model-facing count would cross the always-loaded ceiling relative
// to the decision frozen on policy at mount (policy.alwaysLoaded,
// policy.modelFacingCount). refreshSpec always reads the frozen bit, so a
// mid-conversation reconnect can never add or remove a tool from the manifest
// out from under the KV-cache prefix the model is relying on; this only ever
// reports drift for an operator to act on.
//
// It deliberately does NOT call grantLoadedSlot: recomputing the real decision
// on every reconnect would spend (or refuse to spend) the global budget as a
// side effect of a health-check-shaped call, corrupting the very budget the
// frozen-at-mount design exists to keep stable. Eligibility is compared purely
// against the ceiling (nowQualifies), which is exactly the same fact that
// governs whether the FROZEN decision could ever differ from a hypothetical
// fresh mount today.
func warnIfDeferralWouldFlip(namespace string, policy bridgePolicy, advertised []*sdkmcp.Tool) {
	newCount := countModelFacing(policy, advertised)
	nowQualifies := newCount > 0 && newCount <= maxAlwaysLoadedMCPTools
	if nowQualifies == policy.alwaysLoaded {
		return
	}
	slog.Warn("mcp server deferral would change on reconnect",
		"namespace", namespace,
		"frozen_deferred", !policy.alwaysLoaded,
		"old_model_facing", policy.modelFacingCount,
		"new_model_facing", newCount,
	)
}
