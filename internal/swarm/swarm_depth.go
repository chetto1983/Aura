package swarm

import (
	"fmt"
	"os"
	"strconv"
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
