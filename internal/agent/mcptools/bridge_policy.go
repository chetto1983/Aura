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

func (p bridgePolicy) defaultDeferred() bool {
	return !p.alwaysLoaded
}

// memoryHiddenFromModel is the memory surface the HOST keeps and the model does
// not see. Every entry stays live on cmd/arcadedb-mcp and reachable out of band:
// memory_digest and memory_search are called per turn by the runner's memory
// context (serve_memory_context.go), and the rest answer to `aura memory`
// (search/facts/entities/reembed/schema).
//
// memory_merge_entities joined them on 2026-08-31 for the D-27 slot arithmetic
// (bridge_deferral.go), not because it is host-only. At 4 model-facing tools
// memory was one over maxAlwaysLoadedMCPTools, so the ONE capability this
// deployment exists for -- writing down what it learns -- cost a tool_search
// round trip before the model could reach memory_upsert_fact, while calendar (1
// tool) and whatsapp (3) held both always-loaded slots. Merging two entities is
// hygiene an operator performs deliberately (`aura memory merge <duplicate>
// <survivor>`, cmd/aura/memory.go), not something the model needs mid-turn;
// spending the manifest slot on recall/upsert/forget instead buys the three
// verbs that ARE the product.
//
// The cost is explicit and belongs here rather than in a commit message: mounts
// run in BuiltInCatalog's alphabetical order (calendar, memory, whatsapp) and
// there are only maxAlwaysLoadedMCPSlots of them, so memory taking slot 2 means
// WHATSAPP now stays deferred. That is the trade -- remembering beats messaging
// for an assistant whose thesis is memory -- and it is what to revisit first if
// the ceiling or the slot count ever moves.
var memoryHiddenFromModel = map[string]struct{}{
	"graph_schema":          {},
	"memory_digest":         {},
	"memory_entities":       {},
	"memory_facts_about":    {},
	"memory_merge_entities": {},
	"memory_reembed":        {},
	"memory_search":         {},
}

func (p bridgePolicy) modelFacing(tool string) bool {
	if !p.memorySurface {
		return true
	}
	_, hidden := memoryHiddenFromModel[tool]
	return !hidden
}
