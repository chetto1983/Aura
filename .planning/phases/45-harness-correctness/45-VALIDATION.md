---
phase: 45
slug: harness-correctness
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-13
---

# Phase 45 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded by `/gsd-plan-phase 45` from `45-RESEARCH.md` §Validation Architecture.
> The planner fills the Per-Task Verification Map once PLAN.md task IDs exist.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (Go 1.26 toolchain; testify + goleak per project convention) |
| **Config file** | none — repo-root `Makefile` + `scripts/coverage_gate.sh` drive the tiers |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/agent/... ./internal/agent/tools/... ./internal/arcadedb/...` |
| **Full suite command** | `make quality-full` (WSL, stack up: `make db-migrate memory-up`) |
| **Estimated runtime** | quick ~90–180 s · full ~15–25 min |

**Tier map for the packages this phase touches** (from RESEARCH.md §6 — confirm each against
the file before relying on it):

| Tier | Build tag | Covers | Feeds coverage floor? |
|------|-----------|--------|-----------------------|
| unit | *(none)* | `internal/agent`, `internal/agent/tools`, `internal/arcadedb` pure logic | yes |
| db integration | `db_integration` | `aura.tool_invocations` assertions via Postgres | yes (`scripts/coverage_gate.sh`) |
| arcadedb integration | `arcadedb_integration` | memory supersede / entity resolution (SC#4) | **no** — runs in CI via `make agent-memory-eval` (`.github/workflows/ci.yml:713,811`) but is excluded from the `db_integration`-only floor |
| live E2E | — | full scenario on the running stack, driven by the real agent | no |

> **Fix-on-touch item this phase owns.** RESEARCH.md §6 measured that CLAUDE.md's claim
> "`arcadedb_integration` runs NOWHERE" is **false** — it runs with `-race` and a coverage
> profile via `make agent-memory-eval`. It simply does not feed the coverage floor. The
> corrected statement must land in CLAUDE.md/STATE.md in this phase, and the planner must not
> plan around a blocker that does not exist.

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test -race ./internal/<touched-package>/`
- **After every plan wave:** quick run command above (all three packages, `-race`)
- **Before `/gsd-verify-work`:** `make quality-full` green **and** the live E2E scenario driven
  through the running stack (project DoD — a green unit suite closes nothing).
- **Max feedback latency:** 180 s at task granularity.

---

## Per-Task Verification Map

*Populated by the planner from PLAN.md. One row per task; every task must have an automated
command or an explicit Wave 0 dependency.*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| *(pending planner output)* | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Success-Criterion → Evidence Map

Each ROADMAP success criterion, the tier that proves it, and the observation that counts as
proof. A criterion proved only at a lower tier than listed here is **not** proved.

| SC | Statement (abbreviated) | Proving tier | Observation that counts as proof |
|----|-------------------------|--------------|----------------------------------|
| 1 | Same mutating command re-issued in a later round executes twice | `db_integration` + live E2E | Two distinct rows in `aura.tool_invocations` for the two rounds — asserted by SQL, not by log reading |
| 2 | A retried dispatch still executes exactly once | `db_integration` | Exactly one execution row for the reclaimed operation after a simulated dispatch replay |
| 3 | A legitimate replay carries a visible replay marker | unit + live E2E | `replayedMarker` present in the tool result surfaced to the model **and** the OTel span attribute set alongside it (mirroring the `resultExpiredMarker` seam) |
| 4 | Correcting one fact leaves siblings valid | `arcadedb_integration` + live E2E | Post-correction graph read shows exactly one closed validity window among facts sharing subject+predicate |
| 5 | Operator-language replies, no leaked deliberation, no unkept stated intention | live E2E only | Real scenario transcript, scored — cannot be sampled at any automated tier |

---

## Wave 0 Requirements

*Populated by the planner. Expected shape:*

- [ ] Test stubs for the round-ordinal key discrimination (SC#1/#2) before the key shape changes
- [ ] Test stubs for the `replayedMarker` seam (SC#3), mirroring the existing `resultExpiredMarker` tests
- [ ] Test stubs for the exact-fact-identifier supersede path (SC#4) under `arcadedb_integration`
- [ ] Pre-flight grep (RESEARCH.md open question 2): does any existing test pin `FingerprintTyped`'s
      exact field set or the `"tool-child-v1"` literal? Answer before the key-shape task runs.

*Framework install: not required — `go test` and all tiers already exist.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| SC#5 — operator-language replies, no raw deliberation in user-facing text, every stated intention either ran or was plainly disclaimed | ACC-01, ACC-02 | Judgement over a real conversation; no assertion samples it | Drive the real agent on the running stack through the SC#1–#4 scenario; read the transcript; score >9.8 per project DoD |
| Mutation spot-check on the changed key-composition + supersede files | ACC-01 | `go-mutesting` is a WSL-only campaign, not a CI step | WSL: `PATH=~/go/bin:$PATH go-mutesting` over the touched files; record killed/total in this file; floor ≥70% |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] SC#1–#4 each proved at the tier named in the evidence map (not a lower one)
- [ ] SC#5 scored >9.8 on a real live scenario
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
