package mcptools

type bridgePolicy struct {
	// identityScoped means the HTTP session carries one identity's OAuth bearer
	// and therefore must be selected from the per-identity session pool.
	identityScoped bool
	memorySurface  bool
	recipeSource   string
	// views is the MCP Apps gate: may this mount put its own HTML in front of the
	// operator (bridge_views.go)? It is set from the mount's TRUST CLASS, so a
	// policy built without one — defaultBridgePolicy, every test double — renders
	// nothing. That default is deliberate and fail-closed: a plain `mcpServers`
	// entry declares no trust class at all, and "no class" must not read as "the
	// most trusted one".
	views bool
	// alwaysLoaded is frozen once, at mount, by bridgeToolsWithPolicy's call to
	// grantLoadedSlot (bridge_deferral.go, D-27 / TOOL-14 amendment #123): whether
	// this mount's model-facing tool count won one of the two global
	// always-loaded slots. Every bridgedTool built from one mount carries the
	// identical value and never recomputes it — that freeze is what lets
	// refreshSpec survive a reconnect without flipping a tool's manifest
	// presence out from under the model's KV-cache prefix.
	alwaysLoaded bool
	// modelFacingCount is the model-facing tool count grantLoadedSlot was scored
	// against at mount, frozen alongside alwaysLoaded so a reconnect can report
	// what changed (warnIfDeferralWouldFlip) without ever recomputing the
	// decision itself.
	modelFacingCount int
}

func defaultBridgePolicy(namespace string) bridgePolicy {
	return bridgePolicy{memorySurface: namespace == "memory"}
}

// memoryManifestCore is the memory surface that rides in EVERY turn's manifest. The
// other seven memory tools are still BRIDGED -- present, and reached with one
// tool_search exactly like web_search -- because hiding is not deferral: a hidden tool
// was skipped by bridgeToolsWithPolicy and could not be reached at all, which is how
// the memory-aura skill came to instruct calls to tools that did not exist.
//
// These four are the ones with no substitute among the rest. `memory_recall` already
// answers what memory_facts_about and memory_search answer (its `entity` parameter
// takes the graph path, its `query` the hybrid one), `memory_batch` subsumes forget and
// merge, and the runner injects the digest per turn. Nothing covers `memory_entities`:
// listing the names already in use is what the skill requires before any write, because
// reusing an existing name is the only thing that makes two facts meet, and a memory
// measured at 108 facts had produced 211 entities, 207 of them used exactly once.
var memoryManifestCore = map[string]struct{}{
	"memory_recall":      {},
	"memory_upsert_fact": {},
	"memory_batch":       {},
	"memory_entities":    {},
}

// deferredTool decides whether one tool stays out of the always-loaded manifest. A
// mount that earned no slot defers everything; the memory mount defers everything
// outside its core.
func (p bridgePolicy) deferredTool(tool string) bool {
	if !p.alwaysLoaded {
		return true
	}
	if !p.memorySurface {
		return false
	}
	_, core := memoryManifestCore[tool]
	return !core
}

// manifestCount is what the slot arithmetic weighs: the tools that would ride in every
// turn, not everything the server advertises. Counting all eleven would put memory over
// the ceiling and defer the whole surface, which is the outcome this split exists to
// avoid.
func (p bridgePolicy) manifestCount(advertised int) int {
	if p.memorySurface {
		return len(memoryManifestCore)
	}
	return advertised
}
