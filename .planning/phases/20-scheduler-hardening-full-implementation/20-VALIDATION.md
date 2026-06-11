---
phase: 20
slug: scheduler-hardening-full-implementation
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-11
---

# Phase 20 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `20-RESEARCH.md` §Validation Architecture (Nyquist Dimension 8). Truth-source for tiers: `20-SPEC.md` R1–R7 + Acceptance Criteria.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + table-driven; `goleak` TestMain in `channels`/`telegram`/`cron`; `-race` mandatory |
| **Config file** | none (go test); tiers gated by build tag `db_integration`; the two LIVE gates are manual + CDP |
| **Quick run command** | `go test -race ./internal/channels/... ./internal/cron/... ./internal/agent/tools/...` |
| **Full suite command** | `go test -race -tags db_integration ./internal/...` (+ the two manual LIVE gates) |
| **Integration invocation** | `go test -tags db_integration -race -run TestDispatch ./internal/cron -count=1` (derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`, per MEMORY) |
| **Estimated runtime** | unit ~10–20s; +db_integration ~30–60s; LIVE gates ~2–3 min each (manual) |

---

## Sampling Rate

- **After every task commit:** `go test -race ./internal/<touched-package>/` (touched package's unit tier)
- **After every plan wave:** `go test -race -tags db_integration ./internal/cron/... ./internal/channels/...` + `golangci-lint run ./...` + coverage gate
- **Before `/gsd-verify-work`:** full tag matrix green + BOTH live gates (R5 Step 1, R6 Step 2) signed off + owned-surface ≥85%
- **Max feedback latency:** < 60 seconds (unit tier per commit)

---

## Phase Requirement → Test Map

| Req | Behavior | Tier | Automated Command / Gate | Analog test file to mirror |
|-----|----------|------|--------------------------|----------------------------|
| R1 | origin+identity capture: convC/identityI persisted; bare ctx → NULL + `'local'`; deleted-conv leaves identity | unit (tool) + **db_integration** (round-trip) | `go test ./internal/agent/tools/ -run TestActionSchedule...`; `go test -tags db_integration ./internal/cron -run TestCreateTask...` | tools `result_test.go` (`WithToolCallContext`); cron `store_test.go` + `dispatch_integration_test.go` (`migratedPool`) |
| R2 | registry fan-out: first-delivers-wins (deterministic order), not-my-user fall-through, owns-but-fails stops, not-started never asked | unit | `go test ./internal/channels/ -run TestRegistryDeliver...` | `registry_test.go` (`fakeChannel` → add `fakeDeliverer`) |
| R3 | Telegram Deliver: found→send recorded; `ErrNoRows`→(false,nil); send err→(false,err); `'local'`→not-my-user | unit (Offline bot + fake Store + recording botSender) | `go test ./internal/channels/telegram/ -run TestDeliver...` | `artifact_test.go` / `renderer_test.go` (recording botSender + `tele.ChatID`) |
| R4 | deliverToOrigin precedence + kill-switch (channel delivers⇒Notifier NOT called; explicit route⇒channel skipped; no owner⇒route; owns-fails⇒failed pending + no Notifier; gate off⇒route; nil deps⇒legacy) | unit | `go test ./internal/cron/ -run TestDeliverToOrigin...` | `dispatch_test.go` (`captureNotifier`, `fakeNotificationStore`, `fakeCompleter`; add `fakeChannelDeliverer`) |
| R5 | Step 1 immediate path (reminder set in Telegram → same chat) | **LIVE (hard gate, D-04)** | manual: schedule "remind me in 1 min" in a Telegram DM → text arrives in the SAME chat after ~70s; assert `scheduler_tasks.origin_conversation_id` AND `identity_id` set | CDP Telegram harness (MEMORY `reference_cdp_telegram_live_test_harness`); spike §Live recipe |
| R6 | Step 2 deferred/failed sweep route-back | **db_integration** (regression) + **LIVE (hard gate, D-04)** | integration: insert pending row w/ identity → sweep → fake Deliverer receives it. LIVE: force `AURA_SCHEDULER_QUIET_HOURS` to cover now, schedule agent_job, sweep after window → arrives in origin Telegram chat | cron `dispatch_test.go` `TestDispatchSweepNotifications...` + `dispatch_integration_test.go`; live recipe ex `19-11-PLAN.md` quiet-hours forcing |
| R6 | migration 0014 up/down clean | **db_integration** | `migratedPool(t)` applies 0014; down test reverts | `scheduler_integration_test.go` migration harness |
| R7 | route precedence (explicit notify=whatsapp on TG-origin → whatsapp; CLI reminder → route; nil deps byte-identical) | unit | covered by the R4 dispatch precedence table cases | `dispatch_test.go` |

---

## Per-Task Verification Map

> Task IDs are assigned by the planner (`/gsd-plan-phase`). Each task inherits its requirement's tier + command from the map above. The planner fills this table with concrete `{N}-{plan}-{task}` IDs.

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 20-XX-XX | XX | N | R{n} | unit / db_integration / live | `{per map above}` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/channels/registry_test.go` — add `fakeDeliverer` (Channel+Deliverer) + `TestRegistryDeliverToIdentity*` (4 cases)
- [ ] `internal/channels/telegram/deliver_test.go` — Offline bot + fake Store + recording botSender (3 cases + `'local'` not-my-user)
- [ ] `internal/cron/dispatch_test.go` (or new `deliver_test.go`) — add `fakeChannelDeliverer` + `TestDeliverToOrigin*` (6 cases incl. kill-switch + nil deps)
- [ ] `internal/agent/tools/task_test.go` — `actionSchedule` ctx-sessionID capture + bare-ctx `""` (reuse `ctxWith`/`WithToolCallContext` from `result_test.go`)
- [ ] `internal/cron/` migration-0014 + identity round-trip `db_integration` test (mirror `dispatch_integration_test.go` `migratedPool`)
- [ ] Live gate recipe for Step 1 + Step 2 (reuse the CDP harness + the 19-11 quiet-hours forcing pattern)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Step 1 immediate path — reminder returns to origin Telegram chat | R5 | crosses schedule-time adapter → DB → dispatch → real bot boundary that no fake spans; assert the DESTINATION not the agent reply | `aura serve` (telegram on, account onboarded). In the DM: "remind me in 1 minute to drink water". After ~70s the text appears in the SAME chat (CDP-observed). DB: `SELECT origin_conversation_id, identity_id FROM aura.scheduler_tasks WHERE kind='reminder' ORDER BY created_at DESC LIMIT 1` → both non-NULL, identity_id = chat identity. NEVER assert on `r.Reply`. |
| Step 2 deferred/failed sweep route-back | R6 | only the real-bot + quiet-hours sweep crossing is observable end-to-end | Force `AURA_SCHEDULER_QUIET_HOURS` to cover "now", schedule an agent_job, let the sweep run after the window → notification lands in the origin Telegram chat (CDP-observed). DB ground truth: the `pending_notifications` row's `identity_id` is set and `status` → `delivered`. |

> Both manual gates are **HARD phase-close gates (D-04 / Fork 3)**, not advisory. No-skip-as-green (CLAUDE.md).

---

## Coverage Target

Owned-surface **≥85% hard floor** (CLAUDE.md, overrides PRD 75/60). New files carrying it: `internal/channels/deliver.go` (interface — trivial), `internal/channels/registry.go` (`DeliverToIdentity`), `internal/channels/telegram/deliver.go` + `store.go` (`GetAccountByIdentity`), `internal/cron/deliver.go` (or `dispatch.go` additions). `cmd/aura` glue (`serve_adapters.go` conv resolution) is excluded from the floor but the adapter logic should still have a unit test where feasible.

---

## Nyquist Sampling Rationale

The new behavior decomposes into four orthogonal seams, each fully observed by one focused tier:

1. **Fan-out ordering + tri-state** — observable ONLY at `Registry.DeliverToIdentity` unit level (4 cases). No higher tier adds ordering information.
2. **Telegram identity→chat resolution + send** — observable at `telegram.Deliver` unit level with the Offline bot (the only place `ErrNoRows`/send-error/`'local'` branches are distinguishable). The live gate does NOT re-test these.
3. **Dispatch precedence + kill-switch + pending-on-fail** — observable at `deliverToOrigin` unit level (6 cases); R4+R7 fully collapse here. The live gate adds nothing.
4. **End-to-end identity snapshot + route-back** — observable ONLY live (R5/R6 hard gates) because it crosses the schedule-time adapter → DB → dispatch → real bot boundary no fake spans. db_integration covers the DB round-trip + migration up/down.

**Minimum non-redundant set:** 17 unit cases (4+4+6+3) + 2 db_integration tests (round-trip + migration up/down) + 2 live gates. Adding e2e coverage of the unit-observable branches (1–3) is redundant; omitting either live gate leaves the cross-boundary snapshot/route-back unobserved — the exact Nyquist under-sampling class that produced the Phase-19 headline bug.

---

## Validation Sign-Off

- [ ] All tasks have an `<automated>` verify or a Wave 0 dependency
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s (unit tier)
- [ ] Both LIVE gates (R5 Step 1, R6 Step 2) signed off
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
