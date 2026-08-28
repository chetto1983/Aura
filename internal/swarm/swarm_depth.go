package swarm

import (
	"fmt"
	"os"
	"strconv"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// defaultMaxDepth is the AURA_SWARM_MAX_DEPTH builtin (D-10). Flat v1 never reaches
// depth >= cap at runtime because workers run on a registry with swarm_spawn
// removed (Without, D-08); the guard is a forward-compat code defense, unit-tested
// with a synthetic depth.
const defaultMaxDepth = 2

// maxDepth reads AURA_SWARM_MAX_DEPTH, falling back to defaultMaxDepth when unset,
// empty, or unparseable (non-fatal, mirroring config.envIntDefault).
func maxDepth() int {
	v := os.Getenv("AURA_SWARM_MAX_DEPTH")
	if v == "" {
		return defaultMaxDepth
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultMaxDepth
	}
	return n
}

// MaxDepth exports maxDepth for the composition root (cmd/aura), which already
// imports internal/swarm and needs the live depth cap to inject into
// swarm_spawn's rendered schema (plan 51-03, SWARM-02). This is NOT an addition
// to the Tier A/B operator knob registry (internal/config/config_knobs_test.go
// bans AURA_SWARM_MAX_DEPTH there deliberately) — a model-facing tool schema is
// a different catalog with a different consumer, per 51-RESEARCH.md Open
// Question 1.
func MaxDepth() int {
	return maxDepth()
}

// checkDepth is the AURA_SWARM_MAX_DEPTH code guard (D-10). It returns the PRD
// error literal ("MAX_SPAWN_DEPTH=<max> exceeded") and false when depth >= max, so
// a spawn at or beyond the cap is rejected before any worker is constructed; it
// returns ("", true) when the spawn is allowed.
func checkDepth(depth, max int) (string, bool) {
	if depth >= max {
		return fmt.Sprintf("MAX_SPAWN_DEPTH=%d exceeded", max), false
	}
	return "", true
}

// canNest reports whether a worker built at rc.Depth (THIS Run call's own
// invocation depth) may itself hold swarm_spawn: only when its OWN spawn call,
// landing at depth+1, would still clear checkDepth against the live
// AURA_SWARM_MAX_DEPTH cap. Reuses checkDepth/maxDepth rather than re-deriving the
// comparison (D-10's guard stays the single source of the boundary).
//
// TODO(RED): stub for the RED test phase -- always closed, matching flat v1's
// unconditional strip. GREEN wires the real depth+1 comparison.
func canNest(depth int) bool {
	return false
}

// nestingClosedNotice is the model-readable degrade-to-leaf framing (mirrors
// hermes' delegate_tool.py role:leaf|orchestrator, which degrades orchestrator to
// leaf past max_spawn_depth) a worker reads in its own brief when workerRegistry
// withheld swarm_spawn for the depth cap.
//
// TODO(RED): placeholder text for the RED test phase.
const nestingClosedNotice = "nesting-closed-notice-red-stub"

// workerRegistry returns the registry granted to a worker built from this Run
// call (rc.Depth is THIS call's own depth), and whether nesting is closed for it
// because of the depth cap (the caller uses this to surface nestingClosedNotice).
//
// TODO(RED): stub for the RED test phase -- canNest always reports false, so
// every worker strips swarm_spawn (today's flat-v1 behavior) and the
// nesting-closed notice is never surfaced. GREEN replaces the branch bodies with
// the real depth-conditional grant.
func workerRegistry(rc RunConfig) (*tools.Registry, bool) {
	if !canNest(rc.Depth) {
		return tools.Without(rc.ParentRegistry, swarmSpawnTool), false
	}
	return tools.Without(rc.ParentRegistry, swarmSpawnTool), false
}
