# Phase 10: Scheduler - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-04
**Phase:** 10-scheduler
**Areas discussed:** Build vs buy (scheduler-mcp), HA posture, Schedule grammar + parser, Tool surface, Host process, Delivery, Spawn seam, Auto-reject, Chaos host/topology, Tier param, Backups, Deferred flag, Catch-up, Notify route, Approval UX, Job toolsets, Ops posture, Failure alert, Origin link, CLI parity, Overlap, Lock conn, Notify fail, Identity FK, Health, Quiet hours, Wave-0 process

**Method:** User selected all 4 proposed gray areas + mandated deep research (online + D:/tmp) anchored to 4 canonical Italian product queries. 4 parallel gsd-advisor-researcher agents ran (grammar/tools, PG-queue HA, host/delivery/spawn, scheduler-mcp evaluation — the last triggered by a user-dropped GitHub link mid-discussion). 5 interactive AskUserQuestion rounds followed.

---

## Build vs buy (user-dropped link: PhialsBasement/scheduler-mcp)

| Option | Description | Selected |
|--------|-------------|----------|
| Build internal/cron (Recommended) | PRD shape; steal scheduler-mcp's command-executor pattern for backups | ✓ |
| Pivot to scheduler-mcp | SQLite, no callback into Aura runtime, tool-less own-OpenAI ai_task | |
| Pivot to jolks/mcp-cron | Go+SQLite, same disqualifier | |

**User's choice:** Build internal/cron
**Notes:** Researcher verdict "false friend — not a sandbox-agent pivot": the hard 90% of CAP-06 (agent-job dispatch into Aura's runtime, governance, audit, PG persistence) cannot cross an MCP stdio boundary.

---

## HA posture (ROADMAP vs PRD)

| Option | Description | Selected |
|--------|-------------|----------|
| PRD-minimal, re-spec SC#2 (Recommended) | SKIP LOCKED + MaxDuration boot recovery; industry convergence (River/Oban-v2.6/solid_queue); kill-9 crash test | |
| Middle: keep heartbeat | + heartbeat-while-running; industry deprecated it | |
| ROADMAP-full | Advisory lock + heartbeat + 90s scan + completed_with_hash + 3-worker partition chaos test | ✓ |

**User's choice:** ROADMAP-full — deliberate override of the researcher recommendation; SC#2 stays as written.
**Notes:** Researcher had flagged the pgxpool session-lock footgun (solid_queue rejected GoodJob's advisory model for it; Oban removed its heartbeat table in v2.6). Mitigation captured as D-03 (dedicated held conn per run) in a later round.

---

## Schedule grammar

| Option | Description | Selected |
|--------|-------------|----------|
| at\|every\|cron + tz (Recommended) | Convergent nanobot/OpenClaw/ChatGPT-Tasks triad; covers all 4 queries; DST-safe | ✓ |
| Cron-only | Cannot express the Monica one-shot | |
| PRD structured kinds | Weekly-Monday inexpressible | |

**User's choice:** at|every|cron + per-task IANA tz. PRD amendment required.

## Cron parser dependency

| Option | Description | Selected |
|--------|-------------|----------|
| adhocore/gronx (Recommended) | Zero transitive deps, parser-only | ✓ |
| robfig/cron/v3 parser-only | Battle-tested, heavier | |
| You decide in plan-phase | | |

**User's choice:** gronx.

---

## Host process

| Option | Description | Selected |
|--------|-------------|----------|
| aura serve daemon (Recommended) | bootChat refactored to return errors; serve = boot + scheduler.Start + signal block | ✓ |
| Separate worker subcommand | Two processes, double MCP mounts | |

## Delivery channel

| Option | Description | Selected |
|--------|-------------|----------|
| WhatsApp/mail MCP + audit + stdout (Recommended) | Composite; no amendment-#23 conflict | ✓ |
| stdout Notifier only | Invisible on a daemon | |
| Thread-per-run + notify | ChatGPT Tasks model; needs #23 carve-out | |

## Spawn seam

| Option | Description | Selected |
|--------|-------------|----------|
| Direct LlmAgent, mirror runChild (Recommended) | No internal/swarm import in cron | ✓ |
| Reuse swarm.Run, goals=[1] | Drags swarm env semantics into cron | |

## Auto-reject mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Inject-and-continue (PRD-faithful) | Model sees `<auto-rejected>` RoleTool and decides | ✓ |
| Terminate-with-status (simplest) | swarm-style stop at pause | |

---

## Residual round 2

| Question | Options | Selected |
|----------|---------|----------|
| Chaos test host | Operator-run (Rec) / CI DinD / Both | **Both** (CI advisory + operator gating) |
| Tier param | Cut from v1 (Rec) / Keep inert | **Cut from v1** |
| Backup mechanics | docker exec (Rec) / Host binaries / You decide | **docker exec** |
| task tool deferred? | Non-deferred (Rec) / Deferred | **Non-deferred** |

## Residual round 3

| Question | Options | Selected |
|----------|---------|----------|
| Missed-run catch-up | Catch up once flagged (Rec) / Skip / Per-kind | **Catch up once, flagged** |
| Per-task delivery override | Per-task notify field + default (Rec) / Global only | **Per-task notify field** |
| Approval UX pre-Telegram | Notifier alert + CLI approve (Rec) / ask_user-only | **Notifier + CLI approve** |
| agent_job tool scoping | Full registry no scoping (Rec) / toolsets allowlist | **Full registry, no scoping** |

## Residual round 4

| Question | Options | Selected |
|----------|---------|----------|
| Ops posture | systemd unit (Rec) / Compose service / Manual | **systemd unit** |
| Failure alert | Notify on failure (Rec) / Audit-only | **Notify on failure** |
| Origin link | Nullable origin column (Rec) / No | **origin_conversation_id column** |
| CLI parity | Full triad (Rec) / --cron only | **Full triad** |

## Residual round 5

| Question | Options | Selected |
|----------|---------|----------|
| Overlap policy | Skip+log+reschedule (Rec) / Queue one / Concurrent | **Skip, log, reschedule** |
| Advisory-lock conn | Dedicated held conn (Rec) / Xact-scoped / You decide | **Dedicated held conn per run** |
| Chaos topology | Containerized+disconnect (Rec) / Host+proxy-kill / You decide | **You decide in plan-phase** |
| Notify delivery failure | Fallback chain + retry (Rec) / No retry / Fail run | **Fallback chain + bounded retry** |

## Residual round 6

| Question | Options | Selected |
|----------|---------|----------|
| Identity FK | identity_id column (Rec) / No | **identity_id column** |
| Daemon health | task doctor verb (Rec) / healthz / Both | **aura task doctor** |
| Quiet hours | None v1 (Rec) / Quiet window env | **Quiet window env** (AURA_SCHEDULER_QUIET_HOURS) |
| Wave-0 amendment plan | Yes (Rec) / Fold into code plan | **Yes, Wave-0 doc-only plan** |

---

## Claude's Discretion

- Env-var names/defaults for new knobs (tick interval, max-concurrent-runs, retry attempts, recipient)
- Chaos-test topology + task_hash derivation
- sqlc query split, store adapter, heartbeat ticker implementation
- `aura task doctor` / `task list` output formats
- MissedSince placement + skip-log wording
- Notification text shape per channel

## Deferred Ideas

- Thread-per-run output conversations → Telegram/AG-UI era (#23 carve-out)
- Tier-mapped models → SWARM-V2-01
- Per-task toolsets allowlist → if real need appears
- Telegram Notifier impl → Phase 13
- Compose-izing the aura binary → channel/gateway era
- `aura task runs purge` escape hatch → only if audit-forever ever bothers
