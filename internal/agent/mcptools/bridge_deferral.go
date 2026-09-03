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
// WhatsApp with zero headroom to spare.
//
// Memory used to hide most of its surface from the model to fit under this
// ceiling. That set is GONE as of 2026-09-03, on the operator's decision, and the
// reason it had to go is that hiding was never deferral: bridgeToolsWithPolicy
// SKIPPED a hidden tool, so it was absent from the model's world entirely and
// tool_search could not reach it either. The memory-aura skill and the per-turn
// memory pointer both instructed calls to tools that did not exist -- the skill
// requires reading memory_entities before any write, and the pointer routed to
// memory_facts_about and memory_search; all three were absent. Read back from the
// live database on 2026-09-03, the agent had answered every memory question with
// memory_recall because recall, upsert and batch were the only three it held.
//
// All eleven memory tools are bridged now. Memory is therefore over this ceiling
// and does NOT hold an always-loaded slot: its tools are deferred, exactly like
// web_search, and one tool_search reaches any of them. That is the trade the
// operator chose -- a tool the model can find beats a tool the model cannot see --
// and the slot memory gives up goes to whatsapp, which had been the one deferred
// under the previous arrangement.
//
// The ceiling moved 3 -> 4 on 2026-09-03, and the reason is a measurement rather
// than a preference. Hiding a tool here does not defer it: bridgeToolsWithPolicy
// SKIPS it, so it is absent from the model's world and tool_search cannot reach
// it either. At 3, the memory surface the model actually held was recall, upsert
// and batch -- and recall subsumes facts_about and search, while batch subsumes
// forget and merge, so that set is nearly complete. `memory_entities` was the one
// exception: the vocabulary listing has NO model-facing substitute, and it is the
// read the memory-aura skill makes mandatory before any write, because reusing an
// existing name is the only thing that makes two facts meet. Measured on a real
// memory: 108 facts had produced 211 entities, 207 of them used exactly once.
// Keeping the ceiling at 3 was therefore not saving context, it was buying a
// disconnected graph -- for 1327 bytes of schema, a fifth of what recall costs.
//
// The slot outcome is unchanged by the move: calendar (1), memory (4) and
// whatsapp (3) all qualify at 4, there are still only maxAlwaysLoadedMCPSlots of
// them, and alphabetical order still hands them to calendar and memory.
// Because mounts are granted in alphabetical order against only 2 slots,
// calendar and memory now hold them and WHATSAPP is the one that stays
// deferred. That reordering is the deliberate outcome, not a side effect: read
// the trade in memoryHiddenFromModel's comment before changing either
// constant. N=1 was
// rejected as brittle: a fork that split one verb into two tools would fall off
// the cliff for no reason related to what the model actually carries. Both
// numbers are Go constants, not env vars — no declaration ceremony is needed at
// mount time, and the AURA_MCP_* env catalogue is already in measured debt.
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

// countModelFacing counts advertised's tools AFTER policy.modelFacing
// filtering, comparing names as exact byte strings — no case folding, no
// Unicode normalization, no trimming, so "Send" and "send" count as two tools.

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
