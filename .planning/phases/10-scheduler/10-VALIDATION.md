---
phase: 10
slug: scheduler
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-04
---

# Phase 10 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + table-driven + `goleak.VerifyTestMain` (`internal/cron/main_test.go`) + rapid property tests where indicated |
| **Config file** | none (Go convention); build tag `db_integration` for DB tier (CI job `Integration tests (db_integration tag)`, ci.yml:114) |
| **Quick run command** | `go test -race ./internal/cron/...` (unit, injectable clock — sub-second, no DB) |
| **Full suite command** | `go test -tags db_integration -race -count=1 ./internal/cron/... ./internal/db/...` + `scripts/scheduler_chaos.sh` (operator) |
| **Estimated runtime** | ~5s unit / ~60s db_integration / ~5min chaos |

---

## Sampling Rate

- **After every task commit:** Run `go vet ./... && go build ./... && go test -race ./internal/cron/...`
- **After every plan wave:** Run `go test -tags db_integration -race -count=1 ./internal/cron/... ./internal/db/...`
- **Before `/gsd-verify-work`:** Full tag matrix green + chaos script operator-run recorded in Manual-Only table + coverage ≥85% owned-surface + live North-Star smoke (Q3 + one cron agent_job)
- **Max feedback latency:** 60 seconds (db_integration tier)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | — | — | CAP-06 / SC#1 | — | cron fires once per window, no double-exec | db_integration | `go test -tags db_integration -race -run TestClaimSkipLocked ./internal/cron/` | ❌ W0 | ⬜ pending |
| TBD | — | — | CAP-06 / SC#1 | — | `at\|every\|cron` parse + tz NextRunAt + DST | unit | `go test -race -run TestNextRunAt ./internal/cron/` | ❌ W0 | ⬜ pending |
| TBD | — | — | CAP-06 / SC#2 | — | survivor picks up partitioned worker's task, no dup side-effects | chaos (Manual-Only + CI-advisory) | `scripts/scheduler_chaos.sh` | ❌ W0 | ⬜ pending |
| TBD | — | — | CAP-06 / SC#2 | — | idempotency via `completed_with_hash` | db_integration | `go test -tags db_integration -run TestIdempotentCompletion ./internal/cron/` | ❌ W0 | ⬜ pending |
| TBD | — | — | CAP-06 / SC#3 | — | nightly backups produce dumps; 24h-miss alert | Manual-Only + unit | operator-run + `go test -race -run TestMissedBackupAlert ./internal/cron/` | ❌ W0 | ⬜ pending |
| TBD | — | — | CAP-06 / SC#4 | — | agent_job terminates at inherited step budget | unit + db_integration | `go test -race -run TestAgentJobBudgetInherit ./internal/cron/` | ❌ W0 | ⬜ pending |
| TBD | — | — | CAP-06 / SC#4 | — | ask_user auto-reject never blocks, <30s, audit marker | unit + live smoke | `go test -race -run TestAskUserAutoReject ./internal/cron/` | ❌ W0 | ⬜ pending |
| TBD | — | — | North-Star Q3 | — | natural-prompt reminder → `at` task via `task` tool | live smoke E2E (env-gated) | `OPENROUTER_API_KEY`-gated, NOT CI | ❌ W0 | ⬜ pending |
| TBD | — | — | North-Star Q1/Q2 | — | natural-prompt cron agent_job + MCP delivery | live smoke E2E (env-gated) | MCP-mounted, NOT CI | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*
*Task IDs filled by planner — map rows to plan tasks when PLAN.md files land.*

---

## Wave 0 Requirements

- [ ] `internal/cron/main_test.go` — `goleak.VerifyTestMain` (tick + heartbeat ticker leak gate)
- [ ] `internal/cron/schedule_test.go` — at/every/cron parse + tz NextRunAt + DST-boundary frozen-clock test
- [ ] `internal/cron/claim_test.go` (db_integration) — FOR UPDATE SKIP LOCKED + advisory-lock singleton (SC#1/SC#2)
- [ ] `internal/cron/recover_test.go` (db_integration) — orphan scan + missed catch-up-once (D-18)
- [ ] `internal/cron/handlers/agentjob_test.go` — budget inheritance (SC#4) + ask_user auto-reject (D-25)
- [ ] `internal/agent/tools/task_test.go` — schema-shape assertion (D-10, `required=["action"]` only, no root oneOf/anyOf/enum)
- [ ] `scripts/scheduler_chaos.sh` — 3 workers, 60s partition, idempotency check (SC#2; topology = planner's choice D-05)
- [ ] CI: scheduler db_integration rides the existing `integration-test` job env (AURA_DB_URL/AURA_DB_MIGRATE_URL already exported, ci.yml:132); chaos as CI-advisory non-blocking

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Chaos partition recovery | CAP-06 / SC#2 | Needs 3 live workers + `docker network disconnect`; CI run is advisory, operator run is gating (D-05) | `scripts/scheduler_chaos.sh` with stack up; record result here |
| Backup dump artifacts | CAP-06 / SC#3 | `docker exec` into aura-postgres/aura-neo4j; artifact inspection in `$AURA_BACKUP_DIR` | schedule nightly tasks, verify dump files exist + restore runbook smoke |
| North-Star live smoke (Q3 + Q1/Q2) | CAP-06 | Live LLM (OPENROUTER_API_KEY) + mounted MCP — not CI | natural Italian prompt, no "cron" word; verify task row + delivery |
| Mutation spot-check ≥70% | CLAUDE.md gate | go-mutesting on WSL only | critical files (claim/heartbeat/schedule); record score here |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
