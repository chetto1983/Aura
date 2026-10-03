// bridge_deferral.go implements D-27's always-loaded-slot arithmetic
// (requirement TOOL-14, PRD amendment #123, prd.md:154): a mounted MCP server
// whose model-facing tool count is <= maxAlwaysLoadedMCPTools earns an
// always-loaded manifest slot, capped globally at maxAlwaysLoadedMCPSlots
// simultaneously-loaded servers, granted in mount order (BuiltInCatalog's
// sorted order, internal/mcp/manager/catalog.go), overflow deferred.
//
// The tiering axis is frequency + count, not schema size
// (docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md §2).
//
// Memory bridges every tool it advertises. Hiding one was never deferral:
// bridgeToolsWithPolicy SKIPS a hidden tool, so tool_search cannot reach it
// either. Read back from the live database on 2026-09-03, the agent had answered
// every memory question with memory_recall because recall, upsert and batch were
// the only memory tools it held. Its manifest count is memoryManifestCore (4),
// and the ceiling is 4 because of that core: memory_entities has no model-facing
// substitute and the memory-aura skill requires it before any write. Measured on
// a real memory, 108 facts had produced 211 entities, 207 of them used once.
//
// Who holds a slot, measured on a live turn on 2026-10-03 (prd.md §13):
//   - memory: yes, its core of 4;
//   - calendar: never (bridgePolicy.neverLoaded). Its one tool qualified by
//     count but was 13,136 of the 43,403 manifest characters a real turn sent;
//   - whatsapp: no. The curated merge meant to bring it to 3 tools has not
//     landed: the image CI pins (a463da5) and whatsapp-mcp:latest (1ec0233)
//     both advertise the same 15 raw tools.
//
// The second slot is therefore unused by the built-in set.
//
// N=1 was rejected as brittle: a fork that split one verb into two tools would
// fall off the cliff for no reason related to what the model actually carries.
// Both numbers are Go constants, not env vars: no declaration ceremony is needed
// at mount time, and the AURA_MCP_* env catalogue is already in measured debt.
package mcptools

import (
	"log/slog"
	"sync"

	"github.com/chetto1983/aura/internal/redact"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxAlwaysLoadedMCPTools is the per-server model-facing tool ceiling a mount
// must not exceed to earn an always-loaded slot.
const maxAlwaysLoadedMCPTools = 4

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
			"namespace", redact.Line(namespace), "model_facing", modelFacing)
		return false
	}
	loadedSlotBudget.spent++
	slog.Info("mcp always-loaded slot granted",
		"namespace", redact.Line(namespace), "model_facing", modelFacing,
		"slots_remaining", maxAlwaysLoadedMCPSlots-loadedSlotBudget.spent)
	return true
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
	newCount := policy.manifestCount(len(advertised))
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
