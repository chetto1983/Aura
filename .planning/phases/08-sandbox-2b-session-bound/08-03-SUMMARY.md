---
phase: 08-sandbox-2b-session-bound
plan: 03
subsystem: scoring
tags: [risk-tier, pure-module, governance, sandbox-advisory, tdd]
requires:
  - "08-01 (PRD-amendment gate: D-11/D-12 scope codified)"
provides:
  - "internal/scoring: RiskTier enum + ComputeSandboxTier/ComputeTaskTier/ComputeSkillTier + GateRecommended + RequiresImmediateAlert + UP-only modifier table"
  - "sandbox advisory seam (ComputeSandboxTier) for the execute tool in 08-08"
affects:
  - "08-08 (execute tool consumes ComputeSandboxTier for {risk_tier, gate_recommended})"
  - "P10 Scheduler (consumes ComputeTaskTier/RequiresImmediateAlert — unwired here)"
  - "P11 Skills (consumes ComputeSkillTier — unwired here)"
tech-stack:
  added: []
  patterns:
    - "pure module idiom (analog: internal/llm/prices.go) — no DB/IO/env"
    - "caller-supplied threshold arg (mirrors dnspin TTL constructor arg), config owns the env read"
    - "single tierOrder slice as the sole ordering source for rank + monotone bump + threshold compare"
    - "rapid property-based test for modifier monotonicity"
key-files:
  created:
    - "internal/scoring/scoring.go"
    - "internal/scoring/scoring_test.go"
  modified: []
decisions:
  - "Unknown RiskTier sorts at Risky (rank fallback) so unclassified input gates conservatively, never slips past as Safe."
  - "onlyPyPI matches ONLY pypi.org + files.pythonhosted.org (case-insensitive); any other host — including a lookalike suffix pypi.org.evil.com — makes the whole allowlist non-PyPI (T-08-03-INFO-TIER mitigation)."
  - "Unrecognised task Kind defaults to Normal (not Safe) — conservative."
  - "ComputeSkillTier body arg reserved for future content-based escalation; today action alone decides the tier."
metrics:
  duration: "~12 min"
  completed: 2026-06-03
  coverage: "100.0% of statements (unit; no integration/race-only paths — pure module)"
  loc: "scoring.go 185 / scoring_test.go 186"
---

# Phase 8 Plan 03: internal/scoring Pure Risk-Tier Module Summary

Shipped the FULL shared `internal/scoring/` module (D-11): the `RiskTier` enum (Safe<Normal<Risky<Destructive), all three `Compute*Tier` functions, `GateRecommended`, `RequiresImmediateAlert`, and the UP-only modifier table — a pure transform with no DB, no IO, and no env read (the alert threshold is a caller-supplied argument, mirroring how `dnspin` takes its TTL as a constructor arg). Built strictly TDD: a `test(...)` RED commit (exhaustive tables + rapid monotonicity property, failing at runtime) precedes the `feat(...)` GREEN commit.

## What Was Built

- **`RiskTier` enum + ordering** — a single `tierOrder` slice drives `rank()`, the monotone `bumpTier()` modifier, and threshold comparison, so all three are total and mutually consistent. An unknown tier sorts at Risky (conservative).
- **`ComputeSandboxTier`** (D-12, the ONLY Phase-8-wired path) — empty allowlist → Safe; PyPI-only → Safe; any arbitrary host → Risky. `onlyPyPI` matches only `pypi.org` + `files.pythonhosted.org` (case-insensitive), so a lookalike suffix cannot downgrade arbitrary egress to Safe.
- **`ComputeTaskTier`** — base kind map (reminder/backup* → Safe, agent_job/unknown → Normal, agent_job with destructive-keyword payload → Destructive) plus the UP-only modifier bumps (every_minute/every_hour, silent, reasoning agent tier), saturating at Destructive. **Built + tested but UNWIRED in Phase 8** (D-12 scope guard — P10 wires it).
- **`ComputeSkillTier`** — create/update/install → Risky, delete → Destructive, unknown action → Risky (conservative). **Built + tested but UNWIRED** (P11 wires it).
- **`GateRecommended(t)`** — true for Risky/Destructive (advisory gate). Wired by 08-08's execute tool.
- **`RequiresImmediateAlert(tier, threshold)`** — rank comparison against the caller-supplied threshold; unknown threshold falls back to Risky. **Caller passes `cfg.RiskAlertThreshold`; scoring never reads env** (the plan's central purity invariant).

## Tests

Exhaustive `t.Parallel()` table (`TestScoring`) mirroring `internal/web/ssrf_test.go` `TestBlocked_Classification`: one block per `Compute*Tier` plus `GateRecommended` and `RequiresImmediateAlert` (every tier×threshold boundary). `TestModifierMonotone` is a `pgregory.net/rapid` property test asserting any number of modifier bumps on any base tier yields a tier `>=` the base (never lowers), saturates at Destructive, and raises a non-saturated tier.

## Verification

| Check | Result |
|-------|--------|
| `go vet ./internal/scoring/` | clean |
| `go build ./...` | clean |
| `go test -cover ./internal/scoring/` | ok — **100.0% of statements** |
| `go test -race ./internal/scoring/` | ok (race-clean) |
| `golangci-lint run ./internal/scoring/` | 0 issues |
| purity grep (`database/sql\|pgx\|os.Getenv\|net/http`) | PURE — no DB/IO/env import |
| file-size hook | both files ≤600 LOC (185 / 186) |

## Threat Model Compliance

- **T-08-03-INFO-TIER (risk underclassification, mitigate):** `onlyPyPI` matches only the two canonical PyPI hosts; any other host (incl. lookalike suffix) → Risky → `GateRecommended` true. Modifier table is monotone (property-tested) so a tier can never be silently lowered.
- **T-08-03-SCOPE (scope creep into P10/P11, mitigate):** Task/Skill tiers + `RequiresImmediateAlert` are built + unit-tested but have ZERO runtime consumers in this plan — only the sandbox advisory path is consumed (in 08-08). No scheduler/skills pipeline code added.
- **T-08-03-SC (module installs, accept):** stdlib (`regexp`, `strings`) + the existing `pgregory.net/rapid` test dep only; no new module.

## TDD Gate Compliance

- RED commit `0390796a` (`test(08-03): ...`) — tables + property compile and vet-clean (scoring.go stub returns wrong tiers), all behavioural assertions fail at runtime.
- GREEN commit `ce894ee9` (`feat(08-03): ...`) — real logic; all tests pass, 100% coverage.
- REFACTOR: none needed (module ~185 LOC, single-responsibility, lint-clean).

Note: the lefthook `vet` pre-commit hook vets the whole package, so a compile-failing RED commit is impossible; the RED commit therefore carries a vet-clean stub `scoring.go` whose runtime assertions all fail — a genuine RED (tests run and fail), satisfying the gate.

## Deviations from Plan

None — plan executed as written. One in-plan-allowed lint fix: added a comment to the `SkillAction` const block to satisfy `revive` (exported-const doc rule); no behaviour change.

## Self-Check: PASSED
- FOUND: internal/scoring/scoring.go
- FOUND: internal/scoring/scoring_test.go
- FOUND commit: 0390796a (RED, test)
- FOUND commit: ce894ee9 (GREEN, feat)
