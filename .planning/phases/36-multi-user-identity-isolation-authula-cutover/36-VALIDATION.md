---
phase: 36
slug: multi-user-identity-isolation-authula-cutover
status: approved
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-05
---

# Phase 36 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `36-RESEARCH.md` §Validation Architecture (Nyquist enabled). Per-task
> IDs are assigned by the planner; the Nyquist auditor refines this map post-planning.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (stdlib) + build tags; `pgregory.net/rapid` v1.3.0 (property); `go.uber.org/goleak` v1.3.0 |
| **Config file** | none (Go convention); build tags gate the live tiers |
| **Quick run command** | `go test ./internal/<pkg>/` (unit) |
| **Full suite command** | `go test -tags 'db_integration neo4j_integration' -race ./...` + new `garage_integration authula_integration musr_e2e` tags |
| **Estimated runtime** | unit ~sub-minute/pkg; full live matrix minutes (stack up) |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test ./internal/<pkg>/` + `-race` on touched packages (CLAUDE.md post-edit gate).
- **After every plan wave:** `go test -tags 'db_integration neo4j_integration' -race ./...` on the live stack.
- **Before `/gsd-verify-work`:** full matrix incl. `garage_integration authula_integration musr_e2e` green under `$CI` (no-skip-as-green); owned-surface coverage ≥ 85%.
- **Max feedback latency:** unit sub-minute; live tiers gated per wave.

---

## Per-Task Verification Map

> Task IDs (`36-NN-MM`) are filled in by the planner. Rows below are the requirement-level
> observable behaviors from RESEARCH §Validation Architecture that every plan must map to.

| Req | Behavior (observable) | Threat Ref | Test Type | Automated Command | File Exists | Status |
|-----|-----------------------|------------|-----------|-------------------|-------------|--------|
| MUSR-01 | Two-identity cross-deny: B gets **404** on GET/list/search of A's conversation/approval/document/asset; **403** on delete/archive/resolve of a known A id | T-cross-deny | live E2E | `go test -tags 'db_integration neo4j_integration garage_integration authula_integration musr_e2e' -run TestTwoIdentityCrossDeny ./...` | ❌ W0 | ⬜ pending |
| MUSR-01 | **RLS backstop:** a query WITHOUT the `*ForIdentity` filter still returns 0 foreign rows (RLS + `SET LOCAL`) | T-rls-backstop | integration | `go test -tags db_integration -run TestRLSBackstop ./internal/db` | ❌ W0 | ⬜ pending |
| MUSR-01 | **Neo4j fail-closed:** empty `user_identifier` → 0 hits; B scoped search misses A's doc; A finds own | T-docs-failclosed | integration | `go test -tags neo4j_integration -run TestDocumentsFailClosed ./internal/documents` | ❌ W0 | ⬜ pending |
| MUSR-02 | B-created Web conversation owned by B and runs; owner = `identityctx.IdentityID(ctx)` | T-cross-deny | live E2E | part of `TestTwoIdentityCrossDeny` | ❌ W0 | ⬜ pending |
| MUSR-03 | Session B cannot poll/kill session A's job; IDs unguessable (`crypto/rand`) | T-job-owner | unit + integration | `go test -run TestBackgroundJobOwnerDeny ./internal/agent/tools` | ❌ W0 | ⬜ pending |
| MUSR-04 | TTL expiry records status + terminates the process group; age metric present | T-job-ttl | unit | `go test -run TestBackgroundJobTTLExpiry ./internal/agent/tools` | ❌ W0 | ⬜ pending |
| MUSR-05 | Conversation delete (all 3 surfaces) cancels active work, expires pauses, evicts session tools, handles bg jobs, THEN deletes | T-delete-lifecycle | unit + integration | `go test -run TestConversationDeleteLifecycle ./internal/runner` | partial | ⬜ pending |
| MUSR-06 | Authula default; provisioning→login→isolated-run; break-glass CLI mints a working reset; **no session token in any URL/query string** | T-token-url | live E2E + audit | `TestProvisionLoginIsolatedRun` + static grep gate for query-string tokens | ❌ W0 | ⬜ pending |
| D-14/D-27 | **Saga idempotency/resumability:** re-run mid-failure converges to one consistent identity; de-provision symmetric | T-saga-resume | integration | `go test -tags 'db_integration garage_integration' -run TestProvisioningSagaResumable ./internal/agui` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/db/rls_integration_test.go` — RLS backstop (`db_integration`), MUSR-01
- [ ] `internal/documents/fail_closed_integration_test.go` — empty-identity + cross-deny (`neo4j_integration`); port the spike-085 harness
- [ ] `internal/agent/tools/shell_bg_owner_test.go` + `shell_bg_ttl_test.go` — MUSR-03/04
- [ ] `internal/runner/conversation_delete_lifecycle_test.go` — MUSR-05 (extend `runner_evict_test.go`)
- [ ] `internal/agui/provisioning_saga_resumable_test.go` — D-14/D-27 (`db_integration garage_integration`)
- [ ] `cmd/aura/.../two_identity_e2e_test.go` — the `musr_e2e` cross-deny gate (all four live tags)
- [ ] `internal/objectstore/garageadmin/*` — Admin v2 client + integration test (`garage_integration`)
- [ ] CI: add Garage (admin enabled) + Authula services to the live-stack workflow; export composed DSNs + `AURA_GARAGE_ADMIN_*`
- [ ] Static grep gate: no session token appears in a URL/query string (MUSR-06)

---

## Property-Based Candidates (`pgregory.net/rapid`)

- **RLS:** for a random set of `(identity, rows)`, a read under `identity_i` returns exactly `identity_i`'s rows (never a foreign row).
- **Job IDs:** minted IDs are collision-free and unguessable across N concurrent starts.
- **Saga:** for a random failure-injection point in the leg sequence, re-run converges to the same terminal state (idempotency).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Local-RAM-limited full live matrix | MUSR-01/06 | 32GB RAM upgrade pending; full Garage+Authula+PG+Neo4j stack | Runs live + gates on Linux CI (no-skip-as-green); locally may `t.Skip` |

*Mutation spot-check (≥70% killed) on the RLS carrier + saga journal + documents fail-closed filter, per CLAUDE.md Gate 3.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [x] Feedback latency acceptable (unit sub-minute)
- [x] `nyquist_compliant: true` set in frontmatter (plan-level; `wave_0_complete` flips during execution once the Wave-0 test files exist)

**Approval:** approved 2026-07-05 (plan-checker VERIFICATION PASSED, revision iteration 2)
