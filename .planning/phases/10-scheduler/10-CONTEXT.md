# Phase 10: Scheduler - Context

**Gathered:** 2026-06-04
**Status:** Ready for planning
**Method:** Research-grounded discussion (Phase 8/9 playbook) — 4 parallel researcher passes (grammar/tool-surface, PG-queue HA mechanics, host-process/delivery/spawn-seam, scheduler-mcp off-the-shelf evaluation) triangulating D:/tmp curated sources (nanobot, picobot, codex, system_prompts_leaks) against 2026 industrial patterns (ChatGPT Tasks, Claude Code routines, OpenClaw, River/Oban/solid_queue/GoodJob). 28 decisions across 5 interactive rounds. The product North Star is 4 canonical Italian queries — all four MUST be expressible and deliverable end-to-end:
1. "ogni mattina alle 9:30 fammi il riassunto delle mail da evadere" → `cron "30 9 * * *"` Europe/Rome, agent_job with mail MCP tools
2. "ogni sera dammi le ultime notizie di Cuneo e provincia" → `cron` daily evening, agent_job with web tools
3. "ricordami di chiamare Monica alle 17:30" → `at` one-shot reminder
4. "ogni lunedì fammi il riassunto della borsa italiana" → `cron "0 9 * * 1"`, weekly agent_job

<domain>
## Phase Boundary

Deliver CAP-06 — cron + persistent `agent_job` queue on Postgres. `internal/cron/` is **greenfield** (empty dir). Builds: migration **0009** (`scheduler_tasks` + `agent_job_runs`; floor is 0008), DIY 30s tick loop hosted by a **real `aura serve` daemon** (first long-lived process — replaces the `main.go` TODO), `Handler = agent.Agent` dispatch (Slice 0.9 amendment), `reminder` + `agent_job` + `backup_postgres`/`backup_neo4j` TaskKinds, ONE LLM-facing `task` tool (ActionRouter first consumer), risk-gated `pending_approval` flow consuming the **already-shipped** `internal/scoring` (Phase 8 D-11/D-12), composite notification delivery via the **already-mounted** WhatsApp/mail MCP self-send, and the full ROADMAP HA stack (SKIP LOCKED + advisory lock + heartbeat + chaos test — user-ratified, see D-02).

**Build-vs-buy settled:** `PhialsBasement/scheduler-mcp` (user-proposed) and the MCP-scheduler landscape (`jolks/mcp-cron`, `liao1fan/schedule-task-mcp`) were evaluated and REJECTED — an external MCP scheduler is a passive stdio subprocess that cannot call back into Aura's runtime to run a tool-bound budgeted LlmAgent, persists to SQLite (violates the "Postgres = application state" PRD lock), and only replaces the trivial ~180-LOC tick loop while adding +1 process/+1 DB/+1 API key. NOT a sandbox-agent-style pivot: the hard 90% of CAP-06 (agent-job dispatch, governance, audit, PG persistence) is intrinsically Aura-internal. One pattern stolen: scheduler-mcp's clean subprocess command-executor shape for the backup TaskKinds (~30 LOC, not a dependency).

**Out of scope:** Telegram Notifier (Phase 13 slots into the Notifier interface), thread-per-run output conversations (needs amendment-#23 carve-out — deferred to Telegram/AG-UI era), tier-mapped models (SWARM-V2-01), per-task toolsets scoping, graph_maintenance TaskKind (already removed by PRD audit round 1), compose-izing the aura binary.

</domain>

<decisions>
## Implementation Decisions

### Build vs buy (Area 0 — user-dropped link evaluated)
- **D-01 Build `internal/cron` per PRD** (6a ~700 LOC + 6b ~600 LOC sub-slice commits). scheduler-mcp rejected as false friend (see Phase Boundary). Steal only its command-executor pattern (subprocess + captured exit status) for backups.

### HA posture (Area 1) — ROADMAP-FULL, user-ratified against researcher rec
- **D-02 Keep the full ROADMAP HA stack — deliberate user override.** The researcher recommended PRD-minimal (industry convergence: River `RescueStuckJobsAfter`, Oban v2.6 removed its heartbeat table, solid_queue rejected advisory locks); the user explicitly chose ROADMAP-full. Planner MUST implement: `FOR UPDATE SKIP LOCKED` DueTasks + `pg_try_advisory_lock(task_hash)` continuous ownership + 30s heartbeat UPDATE to `agent_job_runs.last_heartbeat_at` + boot-time orphan scan (stale heartbeat > 90s → `unknown_recovery`) + `completed_with_hash` idempotency column + `scripts/scheduler_chaos.sh` (3 workers, 60s network partition, SC#2 as written). The PRD's MaxDuration-based boot recovery query ALSO stays (per-handler-correct; the heartbeat scan complements it). Do NOT re-litigate this in planning.
- **D-03 Advisory-lock connection strategy: dedicated held conn per running job.** `pool.Acquire()` at claim, take the lock on that conn, hold the conn for the run's lifetime, release lock + conn at completion (session-scoped locks die with the session — the pgxpool footgun the researcher flagged). Bounded by a max-concurrent-runs cap (planner sizes it vs pool size + heartbeat ticker).
- **D-04 Overlap policy: skip, log, reschedule (singleton-per-task).** Tick skips a task whose run is in-flight — the advisory lock makes detection free; log `skipped: previous run in progress`; recompute `next_run_at`. Also covers `run_now`-while-running (structured "already running" error).
- **D-05 Chaos test runs BOTH ways:** CI job non-blocking/advisory + operator-run as the gating record (VALIDATION.md Manual-Only table). Topology (containerized-aura ×3 + `docker network disconnect` — the web-tools E2E cross-compile precedent — vs host processes + DB-proxy kill) is **planner's choice** per CI runner capabilities.

### Schedule grammar + parsing (Area 4)
- **D-06 Grammar = industrial triad `at | every | cron`** (the convergent nanobot/OpenClaw/ChatGPT-Tasks shape; picobot's duration-only grammar is the cautionary gap — cannot express Q1/Q2/Q4):
  - `at` — one-shot, stored as resolved UTC `timestamptz` (covers "Monica 17:30");
  - `every` — interval, floor `MinScheduleEveryMinutes` (default 5, configurable — PRD fix);
  - `cron` — standard 5-field expression + **per-task IANA `tz` column** (default from config, `Europe/Rome`).
  PRD-amendment required: the PRD's `daily HH:MM`/`in=10m` grammar cannot express weekly-Monday and has no tz axis.
- **D-07 DST-safe evaluation:** recurring tasks store `(expr, tz)` and recompute `next_run_at` IN-ZONE after each fire — NEVER store a fixed UTC offset (silent ±1h drift across DST). DB stays UTC; only the computation is tz-aware.
- **D-08 Parser dep = `adhocore/gronx`** (zero transitive deps, parser-only `NextTick`/`IsDue`). The DIY 30s tick loop stays (PRD intent preserved); the lib computes next-fire-time only. PRD amendment: "nessuna libreria cron esterna" → "parser-only cron lib, DIY tick retained".

### LLM tool surface (Area 4)
- **D-09 ONE `task` tool with `action` enum** (`schedule|list|cancel|run_now|approve`) via the ActionRouter helper (~90 LOC, `internal/agent/tools/action.go`, first multi-action consumer — Slice 7 reuses). What all 4 surveyed peers ship. Supersedes the PRD file-target table's 5 separate task_* tool files.
- **D-10 Schema discipline (load-bearing, nanobot regression #3113):** top-level `required = ["action"]` ONLY; per-action requirements expressed inside field descriptions; NO root-level `oneOf`/`anyOf`/`enum` (breaks OpenAI-wire providers — DeepSeek is OpenAI-compat). A test asserts the schema shape.
- **D-11 `task` is NON-deferred.** Scheduling/reminders are a core personal-agent verb (the 4 North-Star queries); one manifest entry, tight 1-line summary.
- **D-12 Tier param CUT from v1.** PRD's `tier ∈ {worker,chat,reasoning}` validated against `swarm.TierConfig.Available()` references machinery cut in Phase 9 (grep-confirmed dead). Children use the single configured model; scoring's `+0.2` reasoning modifier becomes a flat default. PRD amendment marks the references stale.
- **D-13 No per-task toolsets scoping in v1.** Children get the full registry minus `swarm_spawn`; the PRD smoke's `toolsets:[wiki,web]` payload field is dropped (amendment).
- **D-14 CLI parity: full triad on `aura task schedule`** (`--cron`/`--at`/`--every` + `--max-steps` per amendment #19) so every grammar path is operator-testable without an LLM (SC#1 + chaos/smoke scripts need this).

### Host process + lifecycle (Area 2)
- **D-15 Real `aura serve` daemon hosts the tick loop** (replaces the `main.go:70` TODO). Refactor `bootChat` (`cmd/aura/chat.go:99-160`) into an error-returning composition root (no `os.Exit`) reused by serve: boot = pool + MCP mounts + registry + Runner + `scheduler.Start(ctx)` + signal block. Graceful shutdown: ctx cancel → finish in-flight tick → join workers → close MCP closers in reverse — goleak-clean. Every surveyed peer hosts cron in the gateway daemon; REPL-piggyback rejected (jobs fire when no chat is open).
- **D-16 Ops posture: documented systemd unit on the host** (`Restart=on-failure`), native binary (needs Docker sidecars, MCP subprocesses, `~/.aura`). Compose-izing aura deferred.
- **D-17 Daemon observability: `aura task doctor` CLI verb** (matches the `aura mcp doctor`/`web doctor` pattern) — checks last-tick freshness, due tasks, in-flight runs, heartbeat staleness against PG. No HTTP surface before Phase 12.
- **D-18 Missed-run catch-up: catch up ONCE, flagged.** Each overdue task fires once at boot with `MissedSince` in its context (notification can say "late summary, scheduled 9:30"); multiple missed windows collapse to one run; applies to reminders, agent_jobs AND backups (SC#3 24h alert fires only if still missed after catch-up).

### Delivery + notifications (Area 2)
- **D-19 Composite delivery (PRD OQ2 amended):** scheduled-job output reaches the user via the **already-mounted WhatsApp/mail MCP self-send** (`cmd/aura/main.go:150-158` allowlists already include `send_message`/`send_email`); `agent_job_runs.summary` keeps forensics; stdout Notifier is the fallback sink + SC#3 24h missed-backup alert. NO conflict with amendment #23: it forbids *history persistence*, delivery is *output egress* — both stay. stdout-only rejected (nobody tails a daemon).
- **D-20 Per-task notify route:** optional `notify: whatsapp|email|stdout` payload field the LLM sets from the user's phrasing; `AURA_SCHEDULER_NOTIFY_DEFAULT` + recipient env as global fallback (nanobot per-job channel+chat_id pattern). Telegram Notifier impl slots in at Phase 13.
- **D-21 Notify on failure too:** agent_job failures ride the same per-task route ("borsa summary failed: <LastError>") + the boot-recovery summary line. Audit row always written. Silent-failing cron is the classic ops footgun.
- **D-22 Delivery failure = fallback chain + bounded retry:** per-task route → on failure fall back to stdout AND mark the run notification-undelivered → bounded retry on a later tick (~3 attempts). Job result never lost (audit summary). Fail-soft, matching the Phase 9 MCP boot posture.
- **D-23 Quiet hours window:** `AURA_SCHEDULER_QUIET_HOURS` (e.g. `23:00-07:30`): non-DESTRUCTIVE-tier notifications defer to window end; reminders the user explicitly scheduled inside the window still fire at their time.

### agent_job spawn seam (Area 3) — PRD is STALE here, amendments required
- **D-24 Direct LlmAgent construction, mirroring `swarm.runChild`** (`internal/swarm/swarm.go:132-192` is the proven template): registry `Without(reg, "swarm_spawn")`, goal as first user message, `agent.NewBudget(BudgetOptions{MaxSteps: &step_budget})` from the `agent_job_runs.step_budget` row (amendment #19 — maps cleanly onto the shipped `budget.go:110`), ephemeral session `agent_job:<run_id>` (amendment #23 honored — NEVER through the persisting `runner.Turn`), drain events, collect final text. **NO `internal/swarm` import in `internal/cron`.** PRD's `Coordinator.Spawn`/`RejectingResponder`/`TierConfig` references (4 sites: prd.md:1973/2008/2044/2075) are dead — amendment replaces them.
- **D-25 ask_user auto-reject = inject-and-continue (PRD-faithful).** On `Actions.AwaitingInput`: re-Run a fresh LlmAgent with prior turns + a synthesized RoleTool answer `"<auto-rejected: scheduled job has no human responder>"` (the Runner's resume seam minus the DB — `chat_repl.go` SubmitAnswer pattern). The model sees the rejection in-band and decides how to proceed, bounded by remaining budget. `ask_user` STAYS in the child registry (the PRD explicitly wants the model to receive the rejection, not a missing tool). Acceptance: cron job invoking ask_user never blocks, completes <30s with the auto-rejected marker in audit.

### Backups + governance + schema details
- **D-26 Backups via `docker exec`** into `aura-postgres` (`pg_dump`) and `aura-neo4j` (`neo4j-admin database dump`) — Phase-8 fixed-argv dockerCLI precedent (LookPath-gated, NEVER mounts the socket). Destination `AURA_BACKUP_DIR` env, default `~/.aura/backups/` (reconciles ROADMAP's `$AURA_BACKUP_DIR` with the PRD path). Retention 14d/7d rolling per PRD. Executor shape stolen from scheduler-mcp's command task.
- **D-27 Approval UX pre-Telegram:** RISKY/DESTRUCTIVE IMMEDIATE alert rides the composite Notifier route; approval via `aura task approve <id>` CLI or the agent re-emitting `ask_user(kind=approval)` — both PRD-specced, no new surface.
- **D-28 Schema forward-compat columns:** `scheduler_tasks.identity_id` FK → `aura.identities` (default `local`, no v1 behavior — CORE-03 parity) + `scheduler_tasks.origin_conversation_id uuid NULL` (NULL for CLI-created; forensics parity with `paused_states`; future channels reply-in-thread).
- **D-29 Wave-0 doc-only PRD-amendment plan (10-01) BEFORE any code** (precedent 05-01/08-01/09-01). Covers: grammar triad + tz column + gronx dep ("no cron lib" wording), stale Coordinator/RejectingResponder/TierConfig refs (D-12/D-24/D-25), delivery model OQ2 → composite (D-19), toolsets payload cut (D-13), migration number 0009 (PRD says "numero al landing"), `AURA_BACKUP_DIR` naming, env catalog additions (`AURA_SCHEDULER_TZ`, `AURA_SCHEDULER_NOTIFY_DEFAULT` + recipient, `AURA_SCHEDULER_QUIET_HOURS`, tick/cap knobs), tool-surface consolidation (D-09 supersedes the 5-file task_* table), ROADMAP SC#1 wording note (CLI also accepts `--at`/`--every` so the one-shot North-Star query is verifiable).

### Claude's Discretion
- Exact env-var names/defaults for the new knobs (tick interval, max-concurrent-runs cap, retry attempts, notify recipient) — follow `AURA_<DOMAIN>_<UNIT>` convention, catalog them in the PRD amendment.
- Chaos-test topology (D-05) and the `task_hash` derivation for advisory locks (collision posture in the 64-bit namespace).
- sqlc query split, store adapter shape (copy the canonical 04-02 store pattern), heartbeat ticker implementation (must be goleak-clean).
- `aura task doctor` output format; `task list` rendering of `[awaiting approval]`/next_run_at.
- Where `MissedSince` rides (task context vs notification prefix) and the exact skip-log wording.
- Notification text shape for reminders (verbatim payload text per PRD ReminderAgent) vs agent_job summaries (per-channel length cap — WhatsApp-friendly).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope (in-repo)
- `prd.md` §"Slice 6 — Scheduler" (lines ~1935-2077) — truth-source for WHAT: acceptance list, file targets, migration column spec (line 2004), commit templates 6a/6b, OQ1-4 (all closed). NOTE: Coordinator/RejectingResponder/tier/toolsets/grammar items are superseded by the D-29 Wave-0 amendment.
- `prd.md` §Backup strategy (lines ~113-115) — backup TaskKind destinations, retention, restore runbooks.
- `prd.md` §Risk-Based Governance (lines ~4459+) — the pending_approval pipeline this phase wires (scoring module itself SHIPPED in Phase 8).
- `prd.md` line ~1994 — amendment #23 ephemeral agent_job sessions (honored by D-24; delivery is egress, not history — D-19).
- `prd.md` line ~1993 — amendment #19 step_budget contract (D-24, SC#4).
- `.planning/ROADMAP.md` §"Phase 10: Scheduler" (lines ~300-313) — goal + 4 success criteria. SC#2 chaos test stays AS WRITTEN (D-02 user override); SC#1 gets the CLI-triad wording note (D-29).
- `.planning/REQUIREMENTS.md` CAP-06 (line 37).

### Shipped code this phase builds on (ground-truth read during scout)
- `internal/swarm/swarm.go` `runChild` (lines 132-192) — THE template for D-24/D-25 (worker construction, AwaitingInput detection, registry Without, budget child).
- `internal/agent/budget.go` `NewBudget`/`BudgetOptions` (line ~110) — step_budget override seam.
- `internal/agent/llm_agent_pause.go` + `internal/agent/event.go` — `Actions.AwaitingInput` seam D-25 intercepts; `llm_agent.go:210-214` pause-terminates the Run.
- `internal/scoring/scoring.go` — `ComputeTaskTier`/`GateRecommended`/`RequiresImmediateAlert` SHIPPED (Phase 8 D-11); Phase 10 is its first runtime consumer (Phase 8 D-12 scope guard lifts here).
- `cmd/aura/chat.go` `bootChat` (lines 99-160) — the composition root D-15 refactors to error-returning; `cmd/aura/main.go:104` `buildRegistryWithMCP` + lines 150-158 MCP allowlists (send_email/send_message — the D-19 delivery channel).
- `cmd/aura/chat_repl.go` `SubmitAnswer` resume seam — the D-25 inject-and-continue pattern (minus DB).
- `internal/identity/store.go` — the canonical store pattern (D-A4-01) the cron store copies; identities FK for D-28.
- `internal/db/migrations/0008_proxied_child_id_text.up.sql` — migration floor; scheduler = 0009.
- `internal/agent/tools/spec.go`/`manifest.go` — Registry/Deferred mechanics for the D-09/D-11 task tool; alphabetical manifest ordering is cache-load-bearing.
- Pre-rewrite references (PRD-cited, read at tag `pre-rewrite-2026-05-27`): `internal/cron/scheduler.go` (255-LOC tick loop, OK shape), `internal/cron/store.go` (594-LOC god class — the anti-pattern sqlc kills), `internal/agent/tools/registry/scheduler.go` (587-LOC tool — the anti-pattern ActionRouter kills).

### Industrial references (research evidence)
- `D:/tmp/nanobot/nanobot/cron/types.py` + `cron/service.py` + `agent/tools/cron.py` + `cli/commands.py:811-880` — the at|every|cron triad source; per-job channel delivery; schema `required=["action"]` lesson (`tests/cron/test_cron_tool_schema_contract.py:95-100`); never-auto-rerun recovery pattern.
- `D:/tmp/picobot/internal/agent/tools/cron.go` + `internal/cron/scheduler.go` — cautionary: duration-only grammar cannot express wall-clock daily/weekly (fails 3 of the 4 North-Star queries).
- `D:/tmp/system_prompts_leaks/OpenAI/prompt-automation-context.md` — ChatGPT Tasks automations contract (VEVENT + separate Timezone field).
- `D:/tmp/system_prompts_leaks/Anthropic/claude-code.md` (~9-37) — CronCreate/CronDelete/CronList cron-expression contract.
- <https://docs.openclaw.ai/automation/cron-jobs> — at/every/cron triad in production OpenClaw.
- <https://help.openai.com/en/articles/10291617-scheduled-tasks-in-chatgpt> — thread-per-run + notification model (deferred idea) + notify-on-failure precedent.
- <https://github.com/adhocore/gronx> — chosen parser (D-08); fallback <https://pkg.go.dev/github.com/robfig/cron/v3>.
- <https://github.com/PhialsBasement/scheduler-mcp> — evaluated + rejected (D-01); command-executor pattern stolen for D-26.
- <https://riverqueue.com/docs/maintenance-services>, <https://hexdocs.pm/oban/v2-6.html>, <https://github.com/rails/solid_queue>, <https://github.com/bensheldon/good_job> — the HA-mechanics survey behind D-02's researcher rec (user overrode; cited for the record + the pgxpool session-lock footgun D-03 mitigates).
- Auto-memory `feedback_no_atomic_bombs_minimal_industrial_shape` — governing lens; D-02 is the documented, deliberate exception.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `swarm.runChild` — the proven child-LlmAgent recipe D-24 mirrors (construction, drain, AwaitingInput handling, Without registry).
- `internal/scoring` — shipped + exhaustively unit-tested; Phase 10 wires `ComputeTaskTier` → `pending_approval` + alert threshold.
- `bootChat` composition root — serve reuses it post-refactor (D-15); MCP mail/WhatsApp mounts + allowlists already production (Phase 9).
- Canonical store pattern (identity 04-02 → askuser → conversations lineage) — the cron store copies it verbatim; `db.WithTx` for atomic writes; SQLSTATE classification.
- Phase-8 fixed-argv `dockerCLI` (LookPath-gated) — D-26 backup exec reuses the shape.
- `agent.NewBudget(BudgetOptions{MaxSteps})` — step_budget inheritance is a one-line wire.
- `goleak.VerifyNone` TestMain discipline + injectable-clock pattern (Phase 2 W8) — tick loop + heartbeat ticker + reaper tests.

### Established Patterns
- Handler = `agent.Agent` (Slice 0.9): one runtime, no dispatch switch; new TaskKind = 1 file with an Agent impl + `HandlerMeta{Kind, MaxDuration, ReschedulesOnRecovery}`.
- Doc-only Wave-0 PRD-amendment plan (05-01/08-01/09-01 precedent) — D-29.
- Deferred-tool/manifest cache discipline — `task` is non-deferred (D-11) but its Description/schema must stay turn-stable; manifest stays alphabetical.
- No-skip-as-green: db_integration tier under CI with exact env; chaos script = CI-advisory + operator-gating (D-05).
- Fail-soft sidecar posture (Phase 9 MCP boot) — D-22 notification chain mirrors it.
- 2 sub-slice commits (6a infra, 6b agent_job+ActionRouter) per PRD atomicity note.

### Integration Points
- `cmd/aura/main.go` — `serve` case replaces TODO (D-15); `aura task {schedule|list|cancel|approve|run_now|runs|doctor}` subcommand (hand-rolled switch, runDB/runIdentity precedent).
- `cmd/aura/chat.go` `bootChat` → error-returning boot shared by chat/serve.
- Registry wiring — `task` tool registered non-deferred in `buildBaseRegistry`; ActionRouter lands in `internal/agent/tools/action.go`.
- `internal/db/migrations/0009_scheduler.{up,down}.sql` + `internal/db/queries/{scheduler_tasks,agent_job_runs}.sql` (sqlc).
- Notifier interface in `internal/cron` (or sibling) with stdout + MCP-send impls; Telegram impl slots in Phase 13.
- `agent_job_runs.paused_state_token` FK → `aura.paused_states(token)` (PRD forensics parity — note D-25 auto-reject means cron itself never writes paused_states; the FK serves the task.approve-after-ask_user path).

</code_context>

<specifics>
## Specific Ideas

- The 4 Italian North-Star queries (header) are the acceptance lens — the planner should make at least the reminder (Q3) and one cron agent_job (Q1 or Q2) real smoke/E2E scenarios, natural-prompt style (no "cron" in the prompt; the model picks the `task` tool — PRD §Test discipline + Phase 9 precedent).
- User explicitly chose the FULL ROADMAP HA stack over the researcher's minimal-shape recommendation — this is a deliberate, informed exception to "no atomic bombs". Plan it properly (D-03 conn strategy is the hard part); do not quietly downscope it.
- User dropped the scheduler-mcp link mid-discussion expecting a sandbox-agent-style pivot check — the evaluation ran and the verdict (build) was ratified. Future similar links deserve the same treatment: evaluate honestly before building.
- Delivery must reach the user where they are TODAY (WhatsApp/mail), not where they'll be in Phase 13.

</specifics>

<deferred>
## Deferred Ideas

- **Thread-per-run output conversations** (ChatGPT Tasks model: each run writes a readable conversation + notification linking to it) → Telegram/AG-UI era; needs an amendment-#23 carve-out.
- **Tier-mapped models for agent_job** → SWARM-V2-01 (re-add `tier` payload when TierConfig exists).
- **Per-task toolsets allowlist** → revisit if a real need appears (registry filter seams exist).
- **Telegram Notifier impl** → Phase 13 (slots into the Notifier interface).
- **Compose-izing the aura binary** → channel/gateway era.
- **`aura task runs purge --before <date> --confirm`** → PRD OQ4 escape hatch; ship only if the audit-forever table ever bothers anyone (PRD: MB-size for years).

### Reviewed Todos (not folded)
None — `.planning/todos/pending/` is empty (todo.match-phase returned 0).

</deferred>

---

*Phase: 10-scheduler*
*Context gathered: 2026-06-04*
