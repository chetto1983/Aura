---
phase: 20
slug: scheduler-hardening-full-implementation
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-11
validated: 2026-06-11
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

> Filled during `/gsd-validate-phase` (2026-06-11). Each automated command below was **re-run live this session** (not trusted from the SUMMARY/VERIFICATION reports): unit tier under `-race`, db_integration tier under `-race` against live Postgres on `127.0.0.1:5432`. The two LIVE gates were signed off operator-equivalent via the CDP harness (see `20-VERIFICATION.md` §Probe Execution).

| Task ID | Plan | Wave | Req | Test Type | Automated Command / Gate | Test File | Status |
|---------|------|------|-----|-----------|--------------------------|-----------|--------|
| 20-01-01 | 01 | 1 | R2 | unit | `go test -race ./internal/channels/ -run TestRegistryDeliverToIdentity` (5 cases) | `registry_test.go` | ✅ green |
| 20-01-02 | 01 | 1 | R3 | unit | `go test -race ./internal/channels/telegram/ -run TestDeliver` (5 cases + `TestGetAccountByIdentityLocalMapsToNotFound`) | `telegram/deliver_test.go` | ✅ green |
| 20-02-01 | 02 | 1 | R1 | unit | `go test -race ./internal/agent/tools/ -run TestActionScheduleCapturesOrigin` (with-ctx + bare-ctx) | `tools/task_test.go` | ✅ green |
| 20-02-02 | 02 | 1 | R1 | db_integration + LIVE | adapter conv→identity snapshot — no unit (composition-root glue, excluded from floor); observed via R6 round-trip + LIVE Step 1 | `dispatch_integration_test.go` / manual | ✅ green |
| 20-03-01 | 03 | 2 | R4, R7 | unit | `go test -race ./internal/cron/ -run TestDeliverToOrigin` (7 cases incl. R7 stdout-defers-to-origin) | `cron/deliver_test.go` | ✅ green |
| 20-03-02 | 03 | 2 | R5 (wiring) | build / compile-assert | `go build ./cmd/aura/` + `var _ cron.ChannelDeliverer = (*channels.Registry)(nil)` (serve.go:241) | `cmd/aura/serve.go` | ✅ green |
| 20-03-03 | 03 | 2 | R5 | **LIVE (hard gate D-04)** | manual CDP Step 1: reminder → same TG chat; `scheduler_tasks.origin_conversation_id` + `identity_id` both set | manual | ✅ signed off |
| 20-04-01 | 04 | 3 | R6 | db_integration | `go test -tags db_integration -race ./internal/cron/ -run TestDispatchPendingNotificationIdentityRoundTrip` (+ migration 0014 up/down) | `dispatch_integration_test.go` | ✅ green |
| 20-04-02 | 04 | 3 | R6 | db_integration + LIVE | swept-row route-back (same round-trip test) + LIVE Step 2 | `dispatch_integration_test.go` / manual | ✅ green |
| 20-04-03 | 04 | 3 | R6 | **LIVE (hard gate D-04)** | manual CDP Step 2: swept notification → origin TG chat; row `status` pending→delivered | manual | ✅ signed off |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] `internal/channels/registry_test.go` — `fakeDeliverer` (Channel+Deliverer) + `TestRegistryDeliverToIdentity` (**5 cases**, exceeds the planned 4) ✅
- [x] `internal/channels/telegram/deliver_test.go` — `sendRecorder` + `stubResolver` + `TestDeliver` (5 cases incl. `'local'` not-my-user) + `TestGetAccountByIdentityLocalMapsToNotFound` boundary ✅
- [x] `internal/cron/deliver_test.go` — `fakeChannelDeliverer` + `TestDeliverToOrigin` (**7 cases** incl. kill-switch, nil deps, and the R7-amended stdout-defers-to-origin) ✅
- [x] `internal/agent/tools/task_test.go` — `TestActionScheduleCapturesOrigin` ctx-sessionID capture + bare-ctx `""` (no panic) ✅
- [x] `internal/cron/dispatch_integration_test.go` — `TestDispatchPendingNotificationIdentityRoundTrip`: migration-0014 up/down + identity round-trip (`migratedPool`), **re-run live this session, 0.15s** ✅
- [x] Live gate recipe for Step 1 + Step 2 (CDP harness; Step 2 proven via controlled due-row insert exercising the exact `sweepNotifications`→`deliverSweptRow` path) — both signed off ✅

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

- [x] All tasks have an `<automated>` verify or a Wave 0 dependency
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (no MISSING — every requirement COVERED)
- [x] No watch-mode flags
- [x] Feedback latency < 60s (unit tier ~1s per package)
- [x] Both LIVE gates (R5 Step 1, R6 Step 2) signed off
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated 2026-06-11 (`/gsd-validate-phase 20`)

---

## Validation Audit 2026-06-11

State A audit of the plan-time VALIDATION.md (which was left as a `draft` with a placeholder Per-Task row). Re-ran every automated tier live rather than trusting `20-SUMMARY`/`20-VERIFICATION`.

| Metric | Count |
|--------|-------|
| Requirements (R1–R7) | 7 |
| COVERED | 7 |
| PARTIAL | 0 |
| MISSING | 0 |
| Gaps found | 0 |
| Resolved (auditor) | 0 |
| Escalated to manual-only | 0 (R5/R6 LIVE gates were manual-by-design from plan time, not escalations) |

**Live re-run evidence (this session):**

- Symbol existence confirmed on disk for all 5 named tests (no stale-`-run` `[no tests to run]` false-green): `TestRegistryDeliverToIdentity`, `TestDeliver`/`TestGetAccountByIdentityLocalMapsToNotFound`, `TestActionScheduleCapturesOrigin`, `TestDeliverToOrigin`, `TestDispatchPendingNotificationIdentityRoundTrip`.
- **Unit tier** (`-race`, `-v`, WSL native): 20 subtests fired and passed — Registry 5, Telegram Deliver 5 + boundary 1, origin-capture 2, deliverToOrigin 7 (incl. the R7-amended `stdout route defers to origin`).
- **db_integration tier** (`-race`, `-tags db_integration`, live Postgres `127.0.0.1:5432` reached from WSL): `TestDispatchPendingNotificationIdentityRoundTrip` PASS 0.15s — insert→sweep identity round-trip + migration 0014 down/up reversibility both exercised against the real DB.
- **LIVE gates** R5 / R6: operator-equivalent CDP sign-off recorded in `20-VERIFICATION.md` §Probe Execution (accepted by davide, 2026-06-11); R7 amendment override recorded in the verification frontmatter.
- Working tree clean (no `.tmp`/`testdata/rapid` parallel-session byproducts).

**Verdict:** Phase 20 is **Nyquist-compliant**. The minimum non-redundant set (20 unit cases + 1 db_integration round-trip/migration test + 2 LIVE gates) fully samples the four orthogonal seams; no gap to fill, no auditor spawn required.
