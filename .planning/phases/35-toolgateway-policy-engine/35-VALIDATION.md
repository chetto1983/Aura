---
phase: 35
slug: toolgateway-policy-engine
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-03
---

# Phase 35 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `35-RESEARCH.md` §Validation Architecture (Requirement → Test Map). Task IDs are
> finalized at plan time; this map keys by requirement + target test file until then.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` — table-driven, `-race`, `go.uber.org/goleak` (package-wide `TestMain`, mirror `toolinvocations/main_test.go`) |
| **Config file** | none — existing harness (`sqlc`, live-PG `db_integration` stack, `toolinvocations.Store`, `internal/scoring` all present); Wave 0 creates `internal/gateway` + its test files |
| **Quick run command** | `go test -race ./internal/gateway/... ./internal/agent/... ./internal/toolinvocations/...` |
| **Full suite command** | `go test -tags db_integration -race ./internal/gateway/... ./internal/toolinvocations/... ./internal/runner/...` (live PG; composed `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`) |
| **Estimated runtime** | unit sub-second/pkg; full `db_integration` matrix ~tens of seconds against the live stack |

---

## Sampling Rate

- **After every task commit:** Run `go test -race ./internal/gateway/... ./internal/agent/... ./internal/toolinvocations/...` (unit tier, leak-checked, sub-second)
- **After every plan wave:** full unit matrix + `go test -tags db_integration -race ./internal/gateway/... ./internal/toolinvocations/... ./internal/runner/...` against the live stack
- **Before `/gsd-verify-work`:** `make quality-full` green — owned-surface coverage ≥85%, race-clean across the tag matrix, lint=0, vuln clean
- **Phase gate:** mutation spot-check **≥70% killed** on the critical files: `internal/gateway/classify.go` (monotone de-escalation), `internal/gateway/reserve.go` (rows==1/0 gate + replay), `internal/gateway/decide.go` (profile branch), `internal/gateway/reconcile.go` (append-only orphan) — WSL `go-mutesting` with `GOFLAGS=-tags=db_integration` for the reserve/replay branches
- **Max feedback latency:** unit < 5s; integration bounded by live-PG round-trips
- **No-skip-as-green:** the `db_integration` tier `t.Fatal`s under `$CI` when its env is unset — a skipped tier fails the gate, never passes it. Re-seed the `local` identity before the tier (FK 23503 if a parallel/coverage run wiped `...001`)

---

## Per-Task Verification Map

> Task IDs (`35-NN-MM`) assigned once PLAN.md files exist; the Nyquist audit backfills them.
> Tier: **U** = unit (`-race`), **I** = integration (`-tags db_integration`, live PG).

| Req / SC | Behavior to prove | Threat Ref | Test Type | Target test file (new/extend) | Status |
|----------|-------------------|------------|-----------|-------------------------------|--------|
| GATE-01 / SC-1 | Every non-`ask_user` dispatch produces a recorded decision; no tool executes without one | T-35-D (F-020) | U + I | new `internal/gateway/decide_test.go`, `gateway_integration_test.go` | ⬜ pending |
| GATE-01 | Read actions de-escalate to `Safe`; unknown/empty/parse-fail → `Risky`; `swarm_spawn` → `Risky` | T-35-B | U table | new `internal/gateway/classify_test.go` | ⬜ pending |
| GATE-01 | **Property:** for arbitrary garbage args, `classify` never returns `Safe` for a non-enumerated action (saturate-upward invariant) | T-35-B | U fuzz/property | new `internal/gateway/classify_property_test.go` | ⬜ pending |
| GATE-01 | **Exhaustiveness:** every live `skill`(9)/`task`(4) action maps to an explicit tier (no unexpected default) | T-35-B | U table (drive off tool schemas) | `classify_test.go` | ⬜ pending |
| GATE-01 | **Anti-landmine:** reads do NOT flow through `scoring.ComputeSkillTier` (test FAILS if they did — `list`→`Risky`) | T-35-B | U | `classify_test.go` | ⬜ pending |
| GATE-02 / SC-2 | Timeout / crash(non-zero) / non-zero-no-decision all DENY (turn aborts); tool never executes | T-35 (F-006) | U | **extend** `hooks_command_hardening_test.go`, `hooks_command_policy_internal_test.go` | ⬜ pending |
| GATE-02 | Explicit `fail_open` allows (contained) — proves the knob, not silent-allow | T-35 (F-006) | U | **extend** `hooks_policy_test.go` | ⬜ pending |
| GATE-03 / SC-3 | Under hardened/production, a mutating tool with a **failed** reservation is BLOCKED (Execute never called), returns deny | T-35-D | I (spy tool counting `Execute`) | new `gateway_integration_test.go` | ⬜ pending |
| GATE-03 | Reservation `start` is committed in PG **before** `Execute` runs (order proof) | T-35-D | I | ″ | ⬜ pending |
| GATE-04 | Duplicate key: 1st `rows==1`→Execute; 2nd `rows==0`→replay recorded `end`, Execute called **exactly once** | T-35-C | I | ″ | ⬜ pending |
| GATE-04 | Replay tolerates a missing/GC'd sidecar (preview + `result expired`, no error) | — | I | ″ | ⬜ pending |
| D-01d | `start`∧¬`end` older than grace → reconciler appends `end{status=error, indeterminate}` (APPEND, not UPDATE); mutating orphan NOT re-invoked; in-grace orphan untouched | T-35-F | I + goleak Start/Stop | new `reconcile_integration_test.go` | ⬜ pending |
| SC-4 | Under `dev`/`local_trusted`, mutating tool runs host-direct; gateway writes **no** reservation row (no-op) | — | U | `decide_test.go` | ⬜ pending |
| D-03 | Interactive → `ErrAwaitingUserInput{Kind:approval}` + `gateway_approval` ResumeContext; resume re-enters the gateway (retakes reservation) | T-35-C | U + I | new `approve_test.go` | ⬜ pending |
| D-03b | `approve` under `server_production` degrades to deny-with-guidance (no interactive pause) | T-35-G | U | `approve_test.go` | ⬜ pending |
| D-03c | Model cannot self-approve — no model-facing `approve` verdict on the tool schema | T-35-A | U | `approve_test.go` | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/gateway/` package created — `decide.go`, `classify.go`, `reserve.go`, `reconcile.go` (each ≤600 LOC) + a `main_test.go` with `goleak.VerifyTestMain` (mirror `toolinvocations/main_test.go`)
- [ ] `internal/gateway/decide_test.go`, `classify_test.go`, `classify_property_test.go`, `approve_test.go` (unit)
- [ ] `internal/gateway/gateway_integration_test.go`, `reconcile_integration_test.go` under `//go:build db_integration`
- [ ] Shared realistic fixtures: a `Store`-backed reservation over the live PG stack + a spy `tools.Tool` counting `Execute` calls (idempotency / fail-closed proofs) + a live-conversation UUID seed (re-seed `local` identity — FK 23503 if wiped)
- [ ] **Extend** GATE-02 tests (`hooks_command_hardening_test.go`, `hooks_command_policy_internal_test.go`, `hooks_policy_test.go`) with the timeout/crash/non-zero → deny + `fail_open` → allow matrix
- [ ] sqlc regen check: assert `InsertToolInvocation` returns rows (`:execrows`) and `GetToolInvocationEnd :one` exists
- **Framework install:** none — `goleak`, `sqlc`, live-PG `db_integration` harness, `internal/scoring` all already present

*Free-riding existing infra: `toolinvocations` `goleak.VerifyTestMain` + `store_integration_test.go` harness; `internal/scoring` pure tiers; the live `db_integration` stack.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `Decide` call adds negligible overhead (Bifrost +11µs / Agent-OS <0.1ms p99 as the bar) | GATE-01 | Micro-benchmark, environment-sensitive — assert order-of-magnitude, not a hard CI threshold | `go test -bench BenchmarkDecide ./internal/gateway/` on the dev host; confirm the allow/no-op path is sub-millisecond |
| Boot-guard panics on an unclassifiable mutating tool (if the optional `Multiplexed bool` + `Registry.Validate` guard is adopted) | GATE-01 | Panic-on-boot is a wiring assertion — exercise via a deliberately-misregistered fixture tool | Register a mutating multiplexed tool the classifier can't tier → expect `Register`-style panic at boot |

*All other phase behaviors have automated verification (unit + `db_integration`).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 5s (unit) / bounded by live PG (integration)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
