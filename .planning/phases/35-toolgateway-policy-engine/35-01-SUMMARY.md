---
phase: 35-toolgateway-policy-engine
plan: 01
subsystem: api
tags: [policy-engine, gateway, risk-tier, classification, fail-closed, property-testing, rapid]

# Dependency graph
requires:
  - phase: 02-agent-core
    provides: internal/scoring RiskTier vocabulary (ComputeSkillTier/ComputeTaskTier/GateRecommended)
  - phase: 11-skills
    provides: skill/task/swarm_spawn multiplexed tool descriptors + action enums
provides:
  - "Mutating:true fail-closed floor on the 3 action-multiplexed tools (skill/task/swarm_spawn)"
  - "optional tools.Spec.Multiplexed bool descriptor hint (runtime-only, never LLM-visible)"
  - "internal/gateway.classify(spec, rawArgs) → scoring.RiskTier — pure monotone saturate-upward de-escalator"
  - "internal/gateway.ValidateClassifiable(reg) boot-time fail-loud wiring guard"
affects: [35-03-gateway-decide-pep, 35-04-durable-reservation, 35-toolgateway-policy-engine]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "monotone saturate-upward classifier: default mutating floor; ONLY allow-listed reads lower to Safe; unknown/empty/parse-fail saturates to Risky"
    - "schema-driven exhaustiveness test: covered-action set derived off the live tool action enum so a new action without a mapping fails the test"
    - "fail-loud boot-time wiring guard mirroring the tools.Registry.Register duplicate-name panic idiom"

key-files:
  created:
    - internal/gateway/guard.go
    - internal/gateway/classify_test.go
    - internal/gateway/classify_property_test.go
    - internal/gateway/guard_test.go
  modified:
    - internal/gateway/classify.go
    - internal/agent/tools/skill.go
    - internal/agent/tools/task.go
    - internal/agent/tools/swarm_spawn.go
    - internal/agent/tools/spec.go

key-decisions:
  - "Reads are allow-listed in the classifier table, NEVER routed through scoring.ComputeSkillTier (whose default branch returns Risky for list) — the D-02c landmine, pinned by TestClassifyReadsNotScored"
  - "The AG-016 agent_job force-bump expresses 'below Risky' via scoring.GateRecommended so internal/scoring stays byte-unchanged"
  - "Boot-guard fires only for Mutating+Multiplexed tools (a non-mutating multiplexed tool falls back to Safe correctly, so it is not a wiring bug)"
  - "REQUIREMENTS.md GATE-01/GATE-03 left UNMARKED — both are shared with 35-03 (Decide PEP) and are not fully delivered by the classification substrate alone"

patterns-established:
  - "Saturate-upward invariant proven by pgregory.net/rapid property tests (non-enumerated + parse-fail never Safe)"
  - "Schema-driven exhaustiveness guards against Pitfall 2 (a silently under-gated new multiplexed action)"

requirements-completed: []  # GATE-01/GATE-03 are shared with 35-03 and only partially delivered here — not marked complete

coverage:
  - id: D1
    description: "skill/task/swarm_spawn report Spec().Mutating == true AND Multiplexed == true (fail-closed floor, GATE-03)"
    requirement: "GATE-03"
    verification:
      - kind: unit
        ref: "internal/agent/tools/ (go test -race, Spec assertions)"
        status: pass
    human_judgment: false
  - id: D2
    description: "classify returns Safe ONLY for enumerated reads; unknown/empty/parse-fail → Risky; swarm_spawn → Risky; agent_job schedule forced ≥ Risky (GATE-01 classification)"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/classify_test.go#TestClassifyTable"
        status: pass
      - kind: unit
        ref: "internal/gateway/classify_test.go#TestClassifyExhaustive"
        status: pass
    human_judgment: false
  - id: D3
    description: "Monotone saturate-upward invariant: non-enumerated action + unparseable args never return Safe"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/classify_property_test.go#TestClassifyNonEnumeratedNeverSafe"
        status: pass
      - kind: unit
        ref: "internal/gateway/classify_property_test.go#TestClassifyParseFailNeverSafe"
        status: pass
    human_judgment: false
  - id: D4
    description: "Anti-landmine: a skill read reaches Safe only via the allow-list, never via ComputeSkillTier"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/classify_test.go#TestClassifyReadsNotScored"
        status: pass
    human_judgment: false
  - id: D5
    description: "Boot-guard panics on a Mutating+Multiplexed tool with no per-action classifier"
    requirement: "GATE-01"
    verification:
      - kind: unit
        ref: "internal/gateway/guard_test.go#TestValidateClassifiablePanicsOnUnwiredMultiplexed"
        status: pass
    human_judgment: false

# Metrics
duration: ~25min
completed: 2026-07-03
status: complete
---

# Phase 35 Plan 01: ToolGateway Classification Substrate Summary

**A DB-free monotone saturate-upward `classify(spec, rawArgs) → scoring.RiskTier` de-escalator with a `Mutating:true` fail-closed floor on the three action-multiplexed tools and a boot-time fail-loud wiring guard.**

## Performance

- **Duration:** ~25 min (task 3 + bookkeeping; tasks 1-2 salvaged from a crash-interrupted run)
- **Completed:** 2026-07-03
- **Tasks:** 3
- **Files modified:** 9 (5 created, 4 modified across tasks 1-3)

## Accomplishments

- Set the `Mutating:true` fail-closed floor + a new optional `Multiplexed bool` descriptor hint on `skill`/`task`/`swarm_spawn` (tasks 1-2, salvaged).
- Delivered the pure `internal/gateway/classify.go` de-escalator: default is the mutating floor, ONLY explicitly-enumerated reads lower to `scoring.Safe`, everything unknown/empty/unparseable saturates to `scoring.Risky` (tasks 1-2, salvaged).
- Added `internal/gateway/guard.go` `ValidateClassifiable(reg)` — a boot-time panic on any `Mutating+Multiplexed` tool the classifier cannot tier (fail-loud wiring guard, RESEARCH Pitfall 2).
- Proved the substrate: behavior table over all 9 skill + 4 task actions, schema-driven exhaustiveness, the `ReadsNotScored` anti-landmine, and two `rapid` property invariants (non-enumerated + parse-fail never `Safe`).

## Task Commits

1. **Task 1: Mutating floor + Multiplexed hint on the 3 multiplexed tools** — `63442cfa` (feat) — _salvaged from crash-interrupted run_
2. **Task 2: internal/gateway/classify.go monotone saturate-upward de-escalator** — `d9da5fac` (feat) — _salvaged from crash-interrupted run_
3. **Task 3: boot-guard + classifier table/exhaustiveness/anti-landmine/property tests** — `348fab6b` (feat)

_Note: this plan resumed after a PC crash — tasks 1-2 were already committed on master and verified present (`git log`), not re-executed._

## Files Created/Modified

- `internal/gateway/classify.go` — monotone de-escalator (`classify`, `classifySkill`, `classifyTask`, `classifySwarmSpawn`, the fixed-tier + scored-action tables) [task 2]
- `internal/gateway/guard.go` — `ValidateClassifiable` boot-time fail-loud wiring guard [task 3]
- `internal/gateway/classify_test.go` — behavior table + schema-driven exhaustiveness + `ReadsNotScored` anti-landmine [task 3]
- `internal/gateway/classify_property_test.go` — `rapid` saturate-upward + parse-fail invariants [task 3]
- `internal/gateway/guard_test.go` — panic-on-unwired-multiplexed + accepts-wired + ignores-non-mutating [task 3]
- `internal/agent/tools/spec.go` — new `Multiplexed bool` field [task 1]
- `internal/agent/tools/{skill,task,swarm_spawn}.go` — `Mutating:true` + `Multiplexed:true` on Specs [task 1]

## Decisions Made

- **Reads are allow-listed, never scored.** `scoring.ComputeSkillTier`'s default branch returns `Risky` for a read like `list`, so routing a read through it would gate it (the D-02c landmine). Reads live in `skillFixedTiers`/`taskFixedTiers`; only `create/update/delete` (skill) and `schedule` (task) route to `scoring.Compute*Tier`. `TestClassifyReadsNotScored` pins this by asserting BOTH the `ComputeSkillTier("list")==Risky` landmine value AND `classify(skill list)==Safe`.
- **`internal/scoring` stays byte-unchanged.** The AG-016 `agent_job` force-bump ("below Risky → Risky") is expressed via `scoring.GateRecommended` (true only for Risky/Destructive), not by editing scoring. `git diff --stat internal/scoring/` is empty.
- **Boot-guard scope = Mutating AND Multiplexed.** A non-mutating multiplexed tool correctly falls back to `Safe` via `classify`'s Mutating-bit branch, so it is not a wiring bug; the guard ignores it (`TestValidateClassifiableIgnoresNonMutatingMultiplexed`).
- **GATE-01/GATE-03 NOT marked complete in REQUIREMENTS.md.** Both requirements are also claimed by 35-03 (the Decide PEP); the classification substrate delivers only part of each, so marking them done now would be inaccurate.

## Deviations from Plan

None — plan executed exactly as written. Tasks 1-2 were pre-committed (crash recovery), verified present, and not re-executed; task 3 followed the plan's file list and behavior spec.

## Issues Encountered

- **`go test -race` requires cgo on Windows.** Ran the race tier natively in WSL (`CGO_ENABLED=1`), per CLAUDE.md — `internal/gateway` and `internal/agent/tools` both green.
- **`golangci-lint` not runnable in this environment.** The binary is absent from the WSL install (default user is root with an empty `~/go/bin`). Lint is not in this plan's verify block; `go vet` (the enforced Gate 2 static check) passed clean. Out of scope for this plan.

## Next Phase Readiness

- `classify` + `ValidateClassifiable` are ready for 35-03 to wire into the Gateway `Decide` PEP and the serve boot.
- The `Mutating` floor unblocks the reservation (D-01a) and crash-orphan reconciler (D-01d) in waves 3-4.

## Self-Check: PASSED

- `internal/gateway/guard.go` — FOUND
- `internal/gateway/classify_test.go` — FOUND
- `internal/gateway/classify_property_test.go` — FOUND
- `internal/gateway/guard_test.go` — FOUND
- Commit `63442cfa` (task 1) — FOUND
- Commit `d9da5fac` (task 2) — FOUND
- Commit `348fab6b` (task 3) — FOUND

---
*Phase: 35-toolgateway-policy-engine*
*Completed: 2026-07-03*
