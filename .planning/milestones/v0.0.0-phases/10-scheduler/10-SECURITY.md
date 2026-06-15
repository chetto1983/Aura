---
phase: 10
slug: scheduler
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-04
---

# Phase 10 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| operator/model → DB | task payloads (cron_expr, every_minutes, payload jsonb) reach the DB via the store | task definitions, schedules, jsonb payloads |
| aura_app role → schema | the running daemon uses aura_app; DDL reserved to aura_migrate | DML only (no DDL); agent_job_runs has no DELETE |
| worker → DB conn | each running job holds one pooled conn for its lifetime (advisory-lock binding) | advisory locks, run rows |
| concurrent workers → same task row | SKIP LOCKED + advisory lock guarantee single-execution | claim ownership |
| LLM → task tool | model-supplied action + payload reach the scheduler via the tool schema | untrusted model output (cron exprs, payloads) |
| operator CLI → store | operator flags reach the store; lower trust than DDL, higher than the model | task CRUD, approvals |
| cron → docker | backup handler shells out to docker exec (high-privilege if misused) | fixed argv, dump artifacts |
| cron → MCP egress | Notifier self-sends job output to WhatsApp/mail (exfiltration surface) | run summaries to configured recipient only |
| cron → LlmAgent | agent_job runs a tool-bound model with no human responder | budget-capped, ask_user auto-reject |
| daemon → OS | aura serve is the first long-lived host process (systemd-managed) | process lifecycle, graceful shutdown |
| chaos test → live stack | the chaos run partitions live workers + the DB network | test-only traffic, operator-gated |
| live smoke → real LLM + MCP egress | a real DeepSeek agent_job delivers to real WhatsApp/mail | self-send to AURA_EVAL_SELF_PHONE only |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-10-DOC-01 | Tampering | PRD env catalog | accept | Doc-only plan; env vars become load-bearing in 10-02..06 where their threat rows land | closed |
| T-10-DOC-02 | Repudiation | amendment provenance | mitigate | prd.md:1939 Amendment #46 cites D-06..D-29 (10-CONTEXT.md); decision IDs auditable | closed |
| T-10-SC-01 | Tampering | package installs (10-01) | mitigate | 10-RESEARCH.md:62-72 Package Legitimacy Audit — gronx Approved (go-proxy verified); no installs in 10-01 | closed |
| T-10-01 | Tampering | claim/store queries | mitigate | internal/db/queries/scheduler_tasks.sql all $1..$N binds; claim.go:62,76,91 advisory-lock raw queries use $1, never concat | closed |
| T-10-02 | Elevation | aura_app DDL | mitigate | 0009_scheduler.up.sql:60-63 aura_app DML-only; DDL reserved to aura_migrate | closed |
| T-10-03 | Tampering | aura_app DELETE on audit table | mitigate | 0009_scheduler.up.sql:62 agent_job_runs grant = SELECT/INSERT/UPDATE only, NO DELETE | closed |
| T-10-04 | DoS | held-conn pool starvation | accept | Sized in 10-03 (cap 4 < pool 10); cross-checked with T-10-07 | closed |
| T-10-05 | Tampering | invalid cron expr persisted | mitigate | schedule.go:84 gronx.IsValid gate in ParseSchedule before persist | closed |
| T-10-SC-02 | Tampering | go get gronx | mitigate | go.mod:8 gronx v1.20.0 (only new direct dep); 10-RESEARCH Approved | closed |
| T-10-06 | Tampering | double-execution under concurrency | mitigate | scheduler_tasks.sql:34 FOR UPDATE SKIP LOCKED + claim.go:62 pg_try_advisory_lock($1); TestClaimSkipLocked_Singleton, TestSchedulerTickSkipsInFlightAndReschedules | closed |
| T-10-07 | DoS | advisory-lock leak / pool starvation | mitigate | scheduler.go:30-35 cap 4 < pool 10 + semaphore (157-167); scheduler_test.go:26-28 asserts invariant; TestSchedulerTickBoundedByMaxConcurrent | closed |
| T-10-08 | Tampering | duplicate side-effects on recovery | mitigate | 0009_scheduler.up.sql:40 completed_with_hash UNIQUE; store.go:261-264 23505→ErrAlreadyRunning; claim_test.go:166-167 | closed |
| T-10-09 | Repudiation | silent stuck/orphaned run | mitigate | recover.go:26-37 stale>90s→MarkUnknownRecovery; scheduler.go:33-34 heartbeat 30s/stale 90s; TestRecoverOrphans_MarksStaleLeavesFresh | closed |
| T-10-10 | DoS | leaked heartbeat ticker goroutine | mitigate | heartbeat.go:29 defer ticker.Stop() + ctx-cancel + joinable stop; main_test.go:13 goleak gate; TestHeartbeatStopsOnCtxCancel | closed |
| T-10-SC-03 | Tampering | advisory-lock key collision | accept | claim.go:17-27 FNV-1a 64 over per-task UUID; collision = benign singleton-skip + reschedule (D-04), never a correctness break | closed |
| T-10-11 | Tampering | OpenAI-wire schema break | mitigate | task.go:101-117 required=["action"] only, no root oneOf/anyOf/enum; task_test.go:77-86 (TestTaskSchema) | closed |
| T-10-12 | Elevation/Destruction | destructive scheduled task | mitigate | task.go:187-195 ComputeTaskTier→GateRecommended⇒pending_approval; atomic INSERT serve_adapters.go:118-148/store.go:91; TestTaskScheduleDestructivePayloadGated | closed |
| T-10-13 | Spoofing | approval bypass of pending_approval | mitigate | serve_adapters.go:193 run_now AND status='active'; :209 approve only pending→active; task.go:232,254 CLI same + exit 64 (exit_codes.go:8) | closed |
| T-10-14 | Tampering | unvalidated cron expr from model | mitigate | task.go:265 resolveSchedule calls cron.ParseSchedule before persist; task_test.go:131-142 asserts nothing persisted on bad expr | closed |
| T-10-SC-04 | Tampering | package installs (10-04) | mitigate | go.mod unchanged; no new packages | closed |
| T-10-15 | Elevation | docker socket exposure via backup exec | mitigate | backup.go:137,150-155 fixed-argv LookPath-gated; no docker.sock/volume anywhere in internal/cron; TestBackupDumpArgv*Fixed | closed |
| T-10-16 | Tampering | argv injection into backup command | mitigate | backup.go:150-155 dumpArgv takes only app-computed dest, payload never interpolated; backup_test.go:40-52 asserts no shell metachars; -U aura_migrate is a DB role, not OS privilege — no regression | closed |
| T-10-17 | Information Disclosure | notification exfiltration of run output | mitigate | notify.go:142-160 buildSend delivers only to per-task / AURA_SCHEDULER_NOTIFY_RECIPIENT via allowlisted send_message/send_email | closed |
| T-10-18 | Destruction | agent_job destructive tool payload | mitigate | dispatch.go:153 RequiresImmediateAlert at dispatch; handler.go:87 tools.Without(parent,"swarm_spawn"); TestDispatchDestructiveRidesImmediateAlert, TestChildRegistryDropsSwarmKeepsAskUser | closed |
| T-10-19 | DoS | unbounded agent_job (ask_user loops) | mitigate | agentjob.go:138-145 budget-from-row cap + :29,73,79 maxAutoRejects=8 + wall-timeout; TestAgentJobBudgetInherit, TestAskUserAutoReject | closed |
| T-10-20 | Repudiation | silent-failing cron | mitigate | dispatch.go:125,149-150 notify-on-failure + audit summary always written; backup.go:227 24h missed alert; TestDispatchFailureCompletesFailedAndNotifies, TestMissedBackupAlertFiresOnlyPast24h | closed |
| T-10-SC-05 | Tampering | package installs (10-05) | mitigate | No new packages | closed |
| T-10-21 | DoS | os.Exit in shared boot kills graceful shutdown | mitigate | chat.go:103 bootChat→bootChatEnv (error-returning); serve.go:64,69-73 reverse-close + graceful shutdown; main_test.go goleak gate | closed |
| T-10-22 | Repudiation | chaos failover skip-as-green | mitigate | scheduler_chaos.sh:34,144-155 set -euo + exit 1 on missed/dup; ci.yml:132-137,162 CI arms env; store_test.go:36-37 envOrSkip t.Fatal under $CI | closed |
| T-10-23 | Information Disclosure | live smoke leaks output to wrong recipient | mitigate | e2e_test.go self-send recipient (AURA_EVAL_SELF_PHONE) via allowlisted MCP; recipient-scoping notify.go:142; 10-VALIDATION.md:49-50,110 Gate-3 operator sign-off 2026-06-04 | closed |
| T-10-SC-06 | Tampering | package installs (10-06) | mitigate | No new packages; blocking-human Gate-3 checkpoint signed off (10-VALIDATION.md:110) | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-10-01 | T-10-DOC-01 | 10-01 is a doc-only plan; the env-var defaults it catalogs carry no executable surface. Validation lands with the consuming code in 10-02..10-06, each under its own threat row. | gsd-secure-phase audit (plan disposition) | 2026-06-04 |
| AR-10-02 | T-10-04 | Held-conn pool starvation is sized where the held-conn lifecycle lands: 10-03 enforces max-concurrent cap (4) < pool MaxConns (10), asserted by test. Residual risk bounded by T-10-07's verified mitigation. | gsd-secure-phase audit (plan disposition) | 2026-06-04 |
| AR-10-03 | T-10-SC-03 | FNV-1a 64 advisory-lock key collision over per-task UUIDs: a collision causes only a benign singleton-skip + reschedule (D-04), never double-execution. Documented in claim.go:17-27. | gsd-secure-phase audit (plan disposition) | 2026-06-04 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-04 | 26 | 26 | 0 | gsd-security-auditor (opus) — retro verification from 10-01..10-06 PLAN threat models + SUMMARY threat flags; every claimed test confirmed to exist AND assert the claimed property |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-04
