// Package gateway is Aura's single in-process policy-enforcement point (PEP) over
// the agent tool-dispatch loop (GATE-01). This file is its pure classification
// substrate (D-02): classify maps a tool descriptor plus the UNTRUSTED,
// model-supplied raw arguments to a scoring.RiskTier with no DB, IO, or env read.
//
// It is a monotone saturate-upward de-escalator: the default is always the mutating
// floor, ONLY explicitly-enumerated read actions lower to scoring.Safe, and anything
// unknown, empty, or unparseable saturates to scoring.Risky (never Safe). The risk
// vocabulary is reused verbatim from internal/scoring — that package is NEVER
// modified here. The one landmine it must avoid (D-02c): scoring.ComputeSkillTier is
// mutation-only and returns Risky for a read, so reads are allow-listed in the tables
// below and never routed through it.
package gateway

import (
	"encoding/json"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/scoring"
)

// multiplexedClassifiers maps each action-multiplexed tool NAME to its per-action
// classifier. classify dispatches through it, and the boot-guard (guard.go) asserts
// that every registered Mutating+Multiplexed tool appears here — a newly-added
// multiplexed tool that forgets its entry fails the boot-guard rather than silently
// under-gating its actions (RESEARCH Pitfall 2 / D-02d).
var multiplexedClassifiers = map[string]func(json.RawMessage) scoring.RiskTier{
	"skill":       classifySkill,
	"task":        classifyTask,
	"swarm_spawn": classifySwarmSpawn,
}

// classify is the monotone saturate-upward de-escalator (D-02). An action-multiplexed
// tool is routed to its per-action classifier; any other tool falls back to its
// Mutating bit (shell_exec/fs_write and MCP tools with !ReadOnlyHint are already
// Mutating). It never lowers an unrecognised input below the mutating floor.
//
// The generic mutating floor is Normal, NOT Risky: "this tool writes something" is what
// nearly every tool does, and the vocabulary's own Normal tier exists precisely for it.
//
// Classification is not enforcement. What a tier does is decided in decide.go, where only
// Destructive stops the turn: skill delete, an agent_job with a destructive payload, and
// shell_exec's own destructive-pattern approval in internal/agent/tools. Everything else —
// skill create/update -> Risky, task run_now -> Risky, an unparseable or unknown
// multiplexed action -> Risky (fail-safe), swarm_spawn -> Normal — is reserved, recorded
// and auditable, and the operator is not interrupted for it. That matches what the agent
// is told (internal/agent/prompt.go: "Destructive or irreversible actions ... require
// operator approval first"), which the old Risky-gates-everything rule contradicted.
func classify(spec tools.Spec, rawArgs json.RawMessage) scoring.RiskTier {
	if fn, ok := multiplexedClassifiers[spec.Name]; ok {
		return fn(rawArgs)
	}
	if spec.Mutating {
		return scoring.Normal
	}
	return scoring.Safe
}

// skillFixedTiers pins the skill actions whose tier does not depend on a body: the reads
// (list/info/use) to Safe, the snippet-lifecycle writes (restore/archive/save_snippet) to
// Normal, and install to Risky. It deliberately OMITS create/update/delete — those are the
// only actions routed to scoring.ComputeSkillTier (skillScoredActions); routing a read
// there would gate it (the D-02c landmine).
//
// install is Risky, and pinned rather than left to the unknown-action fallback (which is
// also Risky): a tier that holds only because nothing claimed the action is a tier that
// changes the day someone touches the fallback. It fetches third-party code and lands it
// active — persistent self-modification from a supply-chain input, which is the tier's own
// definition. It is not Destructive: an install adds a skill and takes nothing away.
var skillFixedTiers = map[string]scoring.RiskTier{
	"list":         scoring.Safe,
	"info":         scoring.Safe,
	"use":          scoring.Safe,
	"restore":      scoring.Normal,
	"archive":      scoring.Normal,
	"save_snippet": scoring.Normal,
	"install":      scoring.Risky,
}

// skillScoredActions are the ONLY skill actions scored by scoring.ComputeSkillTier
// (create/update → Risky, delete → Destructive).
var skillScoredActions = map[string]bool{"create": true, "update": true, "delete": true}

// classifySkill de-escalates the enumerated skill reads/snippet-lifecycle actions and
// scores create/update/delete. A parse failure or an empty/unknown action saturates to
// Risky — a malformed arg must NEVER slip through as Safe.
func classifySkill(raw json.RawMessage) scoring.RiskTier {
	var a struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return scoring.Risky
	}
	if tier, ok := skillFixedTiers[a.Action]; ok {
		return tier
	}
	if skillScoredActions[a.Action] {
		return scoring.ComputeSkillTier(scoring.SkillAction(a.Action), "")
	}
	return scoring.Risky
}

// taskFixedTiers allow-lists the task actions with a FIXED tier: list (read) to Safe,
// cancel to Normal, run_now to Risky (it fires a task immediately).
var taskFixedTiers = map[string]scoring.RiskTier{
	"list":    scoring.Safe,
	"cancel":  scoring.Normal,
	"run_now": scoring.Risky,
}

// taskScoredActions are the task actions scored by scoring.ComputeTaskTier — only
// schedule. Kept as a set (not inlined) so the exhaustiveness test derives the live
// action coverage from the same source classifyTask uses.
var taskScoredActions = map[string]bool{"schedule": true}

// classifyTask de-escalates the fixed-tier task actions and scores schedule via
// scoring.ComputeTaskTier. An agent_job schedule is force-bumped to at least Risky
// (AG-016): a fresh full-capability agent turn at fire time is never below Risky even
// though ComputeTaskTier alone returns Normal for a plain agent_job. "below Risky" is
// expressed via scoring.GateRecommended (true only for Risky/Destructive) so
// internal/scoring stays untouched. Parse failure / empty / unknown → Risky.
func classifyTask(raw json.RawMessage) scoring.RiskTier {
	var a struct {
		Action  string          `json:"action"`
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return scoring.Risky
	}
	if tier, ok := taskFixedTiers[a.Action]; ok {
		return tier
	}
	if taskScoredActions[a.Action] {
		tier := scoring.ComputeTaskTier(scoring.TaskArgs{Kind: a.Kind, Payload: a.Payload})
		if a.Kind == "agent_job" && !scoring.GateRecommended(tier) {
			return scoring.Risky
		}
		return tier
	}
	return scoring.Risky
}

// classifySwarmSpawn ignores its goals-only args and returns Normal: reserved, recorded and
// audited like any other write, but not an interruption.
//
// It was Risky on the reasoning that "a swarm worker turn wields the full tool set". That is
// true and it is not a reason: the worker's tools are a SUBSET of the parent's — runChild
// derives Without(parentRegistry, "swarm_spawn") (D-08/D-10) — and the parent already uses
// every one of them without asking. Spawning grants no authority that was not already in the
// caller's hands; it fans out the same authority. Nothing escalates, and nothing recurses,
// because the spawn verb is structurally absent from a worker's registry rather than merely
// discouraged.
//
// The fan-out is bounded on every axis that could run away: AURA_SWARM_MAX_GOALS (8),
// AURA_SWARM_MAX_CONCURRENT (4), AURA_SWARM_CHILD_TIMEOUT_SEC (120) and a budget preflight,
// plus the AURA_SWARM_MAX_DEPTH guard behind the structural removal.
//
// The AG-016 parity it was modelled on does not transfer. A scheduled agent_job saturates
// upward because it runs LATER and UNATTENDED — the operator is not there to see it. A swarm
// runs now, inside a turn the operator just asked for, and they are watching it.
//
// What remains is real but is not what an approval prompt fixes: four workers touching one
// workspace concurrently can interleave in ways a serial parent cannot. That is a concurrency
// property to bound in the swarm, not a permission to request.
func classifySwarmSpawn(json.RawMessage) scoring.RiskTier { return scoring.Normal }
