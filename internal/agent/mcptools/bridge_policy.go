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

var memoryHiddenFromModel = map[string]struct{}{
	"graph_schema":       {},
	"memory_digest":      {},
	"memory_entities":    {},
	"memory_facts_about": {},
	"memory_reembed":     {},
	"memory_search":      {},
}

func (p bridgePolicy) modelFacing(tool string) bool {
	if !p.memorySurface {
		return true
	}
	_, hidden := memoryHiddenFromModel[tool]
	return !hidden
}
