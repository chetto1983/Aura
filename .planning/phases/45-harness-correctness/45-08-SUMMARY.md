---
phase: 45-harness-correctness
plan: 08
subsystem: agent-harness
tags: [e2e, live-verification, gate-matrix, mutation, coverage, govulncheck, memory, idempotency]

# Dependency graph
requires: ["45-02", "45-03", "45-04", "45-05", "45-06", "45-07"]
provides:
  - "45-VALIDATION.md: the full gate matrix, the four live-run preconditions quoted before the first turn, and per-step live results with their SQL"
  - "docker/aura/Dockerfile + compose.yaml: the commit SHA baked into aura:local, so `aura version` makes image provenance checkable instead of assumed"
  - "compose.yaml: AURA_MEMORY_OPERATOR_DISPLAY_NAME wired to the arcadedb-mcp service (the knob Phase 45 read but never plumbed)"
  - "The measured evidence that opened 45-09"
affects: [45-09]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Score a live turn on the TRANSPORT, not by reading it: count deliberation markers per SSE channel and test every identifier in the reply against that turn's tool-result payloads. A human skim would have passed the turn that failed."
    - "Preconditions are recorded and committed BEFORE the first message, so they cannot be retrofitted to match the outcome"
    - "Assertions scoped to the run's own conversation_id, so concurrent scheduler activity can neither satisfy nor pollute a claim"

key-files:
  created:
    - .planning/phases/45-harness-correctness/deferred-items.md
  modified:
    - .planning/phases/45-harness-correctness/45-VALIDATION.md
    - docker/aura/Dockerfile
    - compose.yaml
    - internal/gateway/gateway_integration_test.go
    - internal/gateway/gateway_adversarial_triad_integration_test.go
    - internal/gateway/gateway_round_ordinal_integration_test.go
    - docs/aura-quality-snapshot.md

key-decisions:
  - "The gate matrix caught a real regression the phase's own verification had missed: five db_integration gateway tests were broken by 45-03's replayedMarker, invisible because 45-03 verified under WSL without the db_integration tag. Fixed in 34a3cb810, 78/78 subtests green. This is the CI-coverage-gate tag-set trap exactly as documented."
  - "govulncheck went RED on 7 stdlib CVEs. The human chose bump-first over accept-and-defer, so the toolchain moved 1.26.5 -> 1.26.6 (81b55b961) and the gate went green (0 called, down from 7). CI needed no edit: every setup-go step already resolves go-version-file: go.mod."
  - "MEM-04's live failure was a DEPLOYMENT gap, not a code defect. compose.yaml never declared AURA_MEMORY_OPERATOR_DISPLAY_NAME on arcadedb-mcp, so canonicalSubject had no canonical form to collapse onto and behaved exactly as documented. Fixed in f104d2dc2; MEM-04 then passed."
  - "SC#3 was NOT proven live and is not dressed up as proven. The only non-destructive induction (duplicate Idempotency-Key) is refused at the HTTP layer before the agent loop runs; reaching the replay layer needs a scheduler reclaim or a container killed mid-execution, which is destructive against the operator's live deployment and was not done unasked."
  - "SC#4's target fact does not exist. Recall abstained twice with no_qualified_candidates and Aura reported the absence instead of confabulating — itself a good signal, but the step as written is unrunnable."
  - "Three executor dispatches stalled on the 600s watchdog. Tasks 2 and 3 were completed inline by the orchestrator after the third, rather than risking a fourth."

requirements-completed: [HARN-01, HARN-02, MEM-04, MEM-05]
requirements-open: [HARN-03, ACC-01, ACC-02]

# Metrics
duration: ~4h across 3 stalled dispatches + inline completion
completed: 2026-08-15
---

# Phase 45 Plan 08: Prove the phase, and close it honestly Summary

**The automated matrix went green and the live scenario did not: driving the real agent through the real stack proved SC#1, SC#2, MEM-04 and MEM-05 and the D-12 invariant, and caught two defects every automated tier had passed — a reply that leaked drafting notes carrying invented identifiers, and a memory knob that was never plumbed to its sidecar.**

## Performance

| Metric | Value |
|---|---|
| Tasks | 2 of 3 complete (Task 3's snapshot done; sign-off deliberately not ticked) |
| Live steps driven | 6 of 8 (a, b, f, g, h1, h2); c not reachable, d target absent |
| Defects found by the live run | 2, both invisible to every automated tier |
| Executor stalls | 3 |

## Task 1 — the gate matrix

| Gate | Result |
|---|---|
| `make quality` (vet, lint, deadcode, file-size, test-race) | green, 0 lint issues |
| `govulncheck` | RED (7 stdlib CVEs) → **green** after the 1.26.6 bump |
| `coverage_docker.sh` | **85.8%**, above the 85% floor |
| `make agent-memory-eval` | PASS, **MRS 100.00** |
| Mutation spot-check | `idempotency_operation.go` 100% (9/9), `llm_agent_call_dedup.go` 100% (32/32), `memory_supersede.go` 77.8% (21/27) |
| Image provenance | `aura version` prints the build SHA; verified equal to HEAD |

Task 1 also found and fixed a real regression: 45-03's `replayedMarker` broke five
`db_integration` gateway tests, unseen because 45-03 verified under WSL without that tag.

## Task 2 — the live scenario

Full evidence, with SQL and verbatim tool output, in `45-VALIDATION.md`.

| Step | Requirement | Verdict |
|---|---|---|
| a | SC#1 | **PASS** — 2 end rows / 2 distinct ids / 2 distinct previews; both executed |
| b | SC#2 | **PASS** — duplicate-Idempotency-Key retry executed once (1 start, 1 end) |
| c | SC#3 | **NOT PROVEN LIVE** — replay layer unreachable non-destructively; stays unit-proven |
| d | SC#4 | **TARGET ABSENT** — the D-23 fact does not exist; recall correctly abstained |
| e | SC#5 | **FAILED, then fixed in 45-09** and re-driven clean on its own trigger |
| f | MEM-04 | **PASS** after `f104d2dc2` — one entity, `distinct subjects: {'Davide'}` |
| g | MEM-05 | **PASS** both halves — rejected, and recovered unaided |
| h1 | HARN-09 | **NOT REPRODUCED** — the model batched; same-message half stays unit-proven |
| h2 | D-12 | **PASS** both directions — 0 orphan calls, 0 executions without a call |

## Issues Encountered

**Three executor stalls** (600s watchdog, no recovery), at the `newServer` signature edit,
after Task 3's RED, and mid-Task-2. No committed work was lost and `go build ./...` stayed
clean throughout; the breakages were test-only. Tasks 2 and 3 were finished inline.

**A concurrent session shared the working tree and the branch**, committing between this
plan's commits and merging the phase into `master` mid-run. Every commit here was
path-checked (`git diff --cached --name-only`) before landing and no foreign path was ever
included; conversely no 45-xx commit touched `internal/db/`, `internal/documents/`,
`internal/assets/` or `cmd/aura/`.

**Direct graph reads were unavailable.** Reading `.env` for the ArcadeDB credential is
denied by the operator's permission policy and neither image ships `curl`/`wget`, so
ArcadeDB ground truth was read through the agent and its MCP tool results. That is the
surface 45-07 changed, and it is what the model saw — but it is not an independent read
and nothing here is claimed as one.

## Not Claimed

SC#3 and SC#4 are open. HARN-09's same-message half and HARN-08's repair are unit-proven
only — zero repairs fired live, named as HARN-08's own concession. The sign-off block is
deliberately unticked and `nyquist_compliant` is unset: the phase does not meet its own
>9.8 bar on this evidence, and marking it otherwise would be the exact dishonesty the
phase exists to remove.

## Self-Check: PASSED

- [x] Four preconditions recorded and COMMITTED before the first message
- [x] Every live claim carries its command output or SQL
- [x] Assertions scoped to the run's own conversation_id
- [x] Two defects found, both fixed (`f104d2dc2`, and 45-09) and re-verified live
- [x] Unreached steps named as unreached; no tier upgraded to cover a gap
- [x] Quality snapshot re-attested; gate prints `ok: … checked 1 row(s)`
- [x] No foreign path in any commit
