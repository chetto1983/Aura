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
// AURA_SWARM_MAX_DEPTH cap. Delegates to checkDepth/maxDepth rather than
// re-deriving the comparison (D-10's guard stays the single source of the
// boundary; the depth machinery is not re-implemented here, only consumed).
func canNest(depth int) bool {
	_, ok := checkDepth(depth+1, maxDepth())
	return ok
}

// nestingClosedNotice is the model-readable degrade-to-leaf framing (mirrors
// hermes' delegate_tool.py role:leaf|orchestrator, which degrades orchestrator to
// leaf past max_spawn_depth) a worker reads in its own brief when workerRegistry
// withheld swarm_spawn for the depth cap -- so the model learns its own limit by
// reading it, never by calling an absent tool and getting a dispatch error.
const nestingClosedNotice = "You are at the maximum nested-delegation depth for this run: you cannot " +
	"spawn your own sub-workers. Complete this goal yourself with the tools you have."

// appendNestingClosedNotice appends the notice to a worker's context section,
// preserving structuredBrief's own empty-context contract (no section rendered)
// when there is nothing else to add.
func appendNestingClosedNotice(context string) string {
	if context == "" {
		return nestingClosedNotice
	}
	return context + "\n" + nestingClosedNotice
}

// workerRegistry returns the registry granted to a worker built from this Run
// call (rc.Depth is THIS call's own depth), and whether nesting is closed for it
// because of the depth cap (the caller uses this to surface
// appendNestingClosedNotice in the worker's own brief).
//
// swarm_spawn is rebound to a FRESH RunnerAdapter at rc.Depth+1, never the
// shared static adapter reused unchanged -- reusing it would pin every nested
// call's own depth at the SAME value forever (rc.Depth never advances) and
// defeat AURA_SWARM_MAX_DEPTH: an operator-raised cap would become unbounded
// recursion instead of the extra nesting level intended (T-51-18). Enqueuer is
// deliberately left nil on the fresh adapter: SWARM-04 forbids a nested
// delegation from ever taking the background branch, and Run's own
// rc.Depth<=1 gate already makes this redundant (a granted worker's own spawn
// always lands at depth>=2), but nil removes the possibility structurally
// rather than relying on that gate alone.
//
// The grant additionally requires that the PARENT registry already carried
// swarm_spawn (T-51-19): a child must never gain a tool its parent lacked, so a
// deployment that never wired swarm_spawn at all never grants it to a worker
// either, regardless of the configured depth cap.
func workerRegistry(rc RunConfig) (*tools.Registry, bool) {
	out := tools.Without(rc.ParentRegistry, swarmSpawnTool)
	if _, hadSpawn := rc.ParentRegistry.Get(swarmSpawnTool); !hadSpawn {
		return out, false
	}
	if !canNest(rc.Depth) {
		return out, true
	}
	out.Register(&tools.SwarmSpawn{
		Runner: &RunnerAdapter{Cfg: rc.Cfg, Depth: rc.Depth + 1},
		Caps: tools.SwarmCaps{
			MaxGoals:      rc.Cfg.MaxSwarmGoals,
			MaxConcurrent: rc.Cfg.MaxSwarmConcurrent,
			ChildIdleSec:  rc.Cfg.SwarmChildIdleSec,
			MaxDepth:      maxDepth(),
		},
	})
	return out, false
}
