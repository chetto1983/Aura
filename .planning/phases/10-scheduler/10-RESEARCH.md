# Phase 10: Scheduler - Research

**Researched:** 2026-06-04
**Domain:** Postgres job-queue HA (SKIP LOCKED + session advisory lock + heartbeat), cron parsing (gronx), tz/DST-safe scheduling, long-lived `aura serve` daemon, agent_job spawn seam
**Confidence:** HIGH (ground-truth code reads + go-proxy version verify + official pgx/gronx source); the two hard externals (gronx tz-via-ref, pgxpool session-lock) are VERIFIED against source.

## Summary

CONTEXT.md (29 locked decisions D-01..D-29) already settled WHAT to build: build-in-tree (not scheduler-mcp), the full ROADMAP HA stack (D-02), `at|every|cron` grammar triad with per-task IANA tz (D-06/D-07), `adhocore/gronx` parser-only (D-08), one `task` tool via ActionRouter (D-09), direct LlmAgent construction mirroring `swarm.runChild` (D-24/D-25), composite WhatsApp/mail MCP self-send delivery (D-19), and a doc-only Wave-0 PRD amendment first (D-29). This research is the IMPLEMENTATION-level knowledge the planner needs: exact external-library mechanics and exact current signatures/line-numbers of the shipped code the plan builds on.

Two externals are load-bearing and now VERIFIED against source: (1) **gronx** preserves `ref.Location()` through its `bump()`/`time.Date()` loop — so DST-safe recompute (D-07) is achieved by passing `NextTickAfter` a `ref` already in `Europe/Rome` and reading the returned in-zone time, then storing it as UTC. There is NO tz parameter on the parser functions; the caller carries tz via the ref's `*time.Location`. (2) **pgxpool session-level advisory locks** MUST be held on a single `pool.Acquire()`'d `*pgxpool.Conn` for the run's entire lifetime — exactly D-03. A lock taken on an arbitrary pooled conn and "released" later lands on a different conn and silently never unlocks; the correct mitigation is hold-the-conn (the lock auto-releases at session end / connection death, which is also the desired chaos-recovery semantics).

`internal/cron/` is confirmed greenfield (empty/absent). Migration floor is **0008** → scheduler is **0009**. The store/CI/goleak/injectable-clock patterns the plan copies are all shipped and were read line-by-line below.

**Primary recommendation:** Build per D-29 doc-amendment Wave 0, then `internal/cron` in two sub-slice commits (6a infra: migration 0009 + sqlc stores + gronx parsing + tz-aware NextRunAt + tick loop + advisory-lock held-conn claim; 6b agent_job: ActionRouter `task` tool + LlmAgent spawn seam mirroring `runChild` + ask_user auto-reject + backup handlers + composite Notifier). Pass `ref` in target tz to gronx; hold one `*pgxpool.Conn` per running job for the session advisory lock; use an injectable `Now func() time.Time` clock (the shipped `budget.go` W8 pattern) for goleak-clean tick/heartbeat tests.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Cron expression parse / next-fire compute | App (`internal/cron`, gronx) | — | Pure CPU; gronx is parser-only, DIY tick retained (D-08) |
| Due-task claim + concurrency control | Database (Postgres FOR UPDATE SKIP LOCKED + advisory lock) | App (held-conn lifecycle) | DB owns atomic claim + idempotency; app owns conn lifetime (D-03) |
| Heartbeat / orphan recovery | App (30s ticker) + Database (`last_heartbeat_at`) | — | Liveness write is a DB UPDATE; boot scan is a DB query (D-02) |
| Tick loop hosting / lifecycle | App (`aura serve` daemon) | OS (systemd `Restart=on-failure`, D-16) | First long-lived process; replaces `main.go:70` TODO (D-15) |
| agent_job execution | App (LlmAgent, mirrors `swarm.runChild`) | — | Intrinsically Aura-internal; budget inherited from row (D-24) |
| Risk gating | App (`internal/scoring`, shipped) | — | Pure module; P10 is its first runtime consumer (D-12 lifts) |
| Notification delivery | App (Notifier) → MCP tier (WhatsApp/mail self-send) | App (stdout fallback) | Egress via already-mounted MCP (D-19); Telegram slots P13 |
| Backup dump | OS (`docker exec` pg_dump/neo4j-admin) | App (fixed-argv exec, D-26) | Phase-8 dockerCLI precedent; never mounts socket |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/adhocore/gronx` | v1.20.0 | Parser-only cron: `NextTickAfter`/`IsDue`/`IsValid` | [VERIFIED: go module proxy `go list -m -versions`] Zero transitive deps (D-08); preserves `ref.Location()` for DST-safe recompute |
| `github.com/jackc/pgx/v5` + `/pgxpool` | v5.9.2 (already in go.mod) | Pool, `Acquire()` held-conn for session advisory lock, `FOR UPDATE SKIP LOCKED` | [VERIFIED: go.mod] Already the project's driver; sqlc-compatible DBTX |
| sqlc (generated) | (in-tree toolchain) | `scheduler_tasks` + `agent_job_runs` typed queries | [VERIFIED: codebase] Canonical store pattern (identity 04-02) |
| `internal/agent` (LlmAgent/Budget) | in-tree | agent_job child construction + step_budget inheritance | [VERIFIED: codebase] `swarm.runChild` is the proven template |
| `internal/scoring` | in-tree | `ComputeTaskTier`/`GateRecommended`/`RequiresImmediateAlert` | [VERIFIED: codebase] Shipped Phase 8, P10 first consumer |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `log/slog` | stdlib | structured tick/skip/recovery logs | every tick + skip-log (D-04) + recovery summary (D-21) |
| `go.uber.org/goleak` | (in-tree) | tick-loop + heartbeat-ticker leak gate | TestMain in `internal/cron` (precedent below) |
| `os/exec` (fixed-argv) | stdlib | backup `docker exec` (D-26) | Phase-8 dockerCLI LookPath-gated shape |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `adhocore/gronx` | `robfig/cron/v3` SpecSchedule | robfig couples a scheduler+runner (more than parser-only); has transitive surface; gronx wins on D-08 zero-dep + parser-only intent. Keep robfig as fallback ONLY if a gronx gap appears (none found). |
| session `pg_try_advisory_lock` | `pg_advisory_xact_lock` (tx-scoped) | xact-lock auto-releases at tx end — incompatible with a long-running job held across many statements; D-02/D-03 require session-scoped held-conn. |
| held-conn advisory lock | River `RescueStuckJobsAfter` minimal model | Researcher's rec; user OVERRODE (D-02). Do not re-litigate. |

**Installation:**
```bash
go get github.com/adhocore/gronx@v1.20.0
```

**Version verification:** `go list -m -versions github.com/adhocore/gronx` returns through `v1.20.0` (latest, 2026-05-21) on the Go module proxy [VERIFIED: go list]. pgx v5.9.2 already in go.mod [VERIFIED: go.mod].

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `github.com/adhocore/gronx` | Go module proxy | >5 yrs (v0.1.0 → v1.20.0) | widely used Go cron lib | github.com/adhocore/gronx | n/a (slopcheck=npm/PyPI only) | Approved — go-proxy verified, zero-dep, source read |
| `github.com/jackc/pgx/v5` | Go module proxy | mature | de-facto Go pg driver | github.com/jackc/pgx | n/a | Approved — already in go.mod |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

*slopcheck 0.6.1 is installed but only verifies npm/PyPI; for the Go ecosystem the authoritative check is `go list -m -versions` against the proxy, which confirmed gronx exists with the claimed version lineage and the official `github.com/adhocore/gronx` source repo. gronx is the user-locked dep (D-08) — treat as a confirmed decision, not a fresh recommendation.*

## Architecture Patterns

### System Architecture Diagram

```
                          aura serve (D-15 — first long-lived daemon, replaces main.go:70 TODO)
                                          │ boot
            ┌─────────────────────────────┼──────────────────────────────────┐
            ▼                             ▼                                    ▼
   bootServe (refactored          scheduler.Start(ctx)                  signal block
   bootChat composition root,     │                                     (SIGINT/SIGTERM)
   error-returning, D-15)         │                                          │ ctx cancel
   = pool + MCP mounts +          ▼                                          ▼
     registry + Runner      ┌──────────────────┐                    graceful shutdown:
                            │ boot orphan scan │                    finish in-flight tick →
                            │ (heartbeat>90s → │                    join workers → reverse-
                            │  unknown_recovery│                    close MCP closers (goleak)
                            │  + missed catch- │
                            │  up once, D-18)  │
                            └────────┬─────────┘
                                     ▼
                          ┌──────────────────────┐  every 30s tick (injectable Now clock)
                          │  TICK LOOP           │
                          │  DueTasks query:     │  SELECT ... WHERE next_run_at<=now()
                          │  FOR UPDATE          │    AND status='active'
                          │  SKIP LOCKED         │  ORDER BY next_run_at  (btree index)
                          └──────────┬───────────┘
                                     │ per due task
                       ┌─────────────▼──────────────┐
                       │ pool.Acquire() → dedicated  │  conn := pool.Acquire(ctx)
                       │ *pgxpool.Conn (HELD)        │  ok := pg_try_advisory_lock(task_hash)
                       │ pg_try_advisory_lock(hash)  │  ok==false → D-04 skip+log+reschedule
                       └─────────────┬───────────────┘  (singleton-per-task)
                                     │ ok==true → owned
                       ┌─────────────▼──────────────────────────────────────┐
                       │ insert agent_job_runs row (step_budget, wall budget) │
                       │ heartbeat ticker (30s → last_heartbeat_at) on conn   │
                       └─────────────┬──────────────────────────────────────┘
                                     │ dispatch by TaskKind (Handler = agent.Agent)
        ┌────────────────┬───────────┼────────────────┬──────────────────────┐
        ▼                ▼           ▼                ▼                      ▼
   reminder         agent_job    backup_postgres  backup_neo4j        (future kinds)
   (verbatim text)  (LlmAgent,   (docker exec      (docker exec        = 1 file +
                    mirror        pg_dump)          neo4j-admin)         HandlerMeta
                    runChild,
                    budget from
                    row, ask_user
                    auto-reject)
        │                │           │                │
        └────────────────┴─────┬─────┴────────────────┘
                               ▼ result → agent_job_runs.summary (forensics)
                  ┌────────────────────────────────┐
                  │ Notifier (composite, D-19)     │  per-task notify route:
                  │ whatsapp|email (MCP self-send) │  send_message / send_email
                  │ → fallback stdout + retry (D-22)│  (already-mounted, main.go:150-158)
                  └────────────────────────────────┘
                               ▼ on completion
                  release advisory lock + Release() conn + recompute next_run_at in-zone (D-07)
```

### Recommended Project Structure
```
internal/cron/
├── scheduler.go        # tick loop + Start(ctx)/graceful shutdown; injectable Now (≤600 LOC)
├── claim.go            # pool.Acquire held-conn + pg_try_advisory_lock + task_hash (D-03/D-04)
├── heartbeat.go        # 30s heartbeat ticker on the held conn; goleak-clean
├── recover.go          # boot orphan scan (stale>90s → unknown_recovery) + missed catch-up (D-18)
├── schedule.go         # at|every|cron parse (gronx) + tz-aware NextRunAt recompute (D-06/D-07)
├── store.go            # sqlc adapter over scheduler_tasks + agent_job_runs (copy identity 04-02)
├── notify.go           # Notifier iface + stdout + MCP-send impls + fallback chain (D-19/D-22)
├── handlers/
│   ├── reminder.go     # HandlerMeta{reminder} — verbatim text delivery
│   ├── agentjob.go     # LlmAgent spawn mirroring swarm.runChild; ask_user auto-reject (D-24/D-25)
│   └── backup.go       # docker exec pg_dump / neo4j-admin (D-26, Phase-8 dockerCLI shape)
internal/agent/tools/
├── action.go           # ActionRouter helper (~90 LOC, D-09, first consumer)
└── task.go             # ONE non-deferred `task` tool, action enum (D-09/D-10/D-11)
internal/db/migrations/
├── 0009_scheduler.up.sql / .down.sql   # scheduler_tasks + agent_job_runs (D-28 fwd-compat cols)
internal/db/queries/
├── scheduler_tasks.sql / agent_job_runs.sql   # sqlc
cmd/aura/
├── serve.go            # daemon entrypoint (replaces main.go:70 TODO, D-15)
└── task.go             # aura task {schedule|list|cancel|run_now|approve|runs|doctor} (D-14/D-17)
scripts/scheduler_chaos.sh   # 3 workers, 60s partition, SC#2 (D-02/D-05)
```

### Pattern 1: gronx tz-via-ref DST-safe next-run (D-07)
**What:** gronx has NO tz parameter; it preserves `ref.Location()` through its internal `bump()`/`time.Date()` loop [VERIFIED: gronx next.go source — `loc := ref.Location()`].
**When to use:** every recurring (`every`/`cron`) recompute after a fire.
```go
// Source: gronx next.go (loc := ref.Location() preserved through bump())
// Store (expr, tz) per task; DB column is timestamptz (UTC). Compute IN-ZONE:
loc, err := time.LoadLocation(task.TZ)            // e.g. "Europe/Rome"
refInZone := lastFire.In(loc)                     // carry tz via the ref
next, err := gronx.NextTickAfter(task.CronExpr, refInZone, false) // false = strictly after
task.NextRunAt = next.UTC()                       // store UTC, never a fixed offset (D-07)
```
**Anti-pattern killed:** storing `next_run_at` as a fixed UTC offset → silent ±1h drift across the DST boundary. Always recompute in-zone.

### Pattern 2: pgxpool session advisory lock — hold the conn (D-03)
**What:** session-level `pg_try_advisory_lock` is bound to the connection that took it; it auto-releases at session end / connection death [VERIFIED: pgx discussions #2097 + PostgreSQL docs — `pg_advisory_unlock_all` is implicitly invoked at session end even on ungraceful disconnect].
**When to use:** continuous ownership of a running job.
```go
// Source: pgx discussions #2097 + Qube Cinema advisory-lock pitfall writeup
conn, err := pool.Acquire(ctx)         // dedicated conn — DO NOT release until job done
// (NO defer conn.Release() before the lock work — must outlive the run)
var locked bool
err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, taskHash).Scan(&locked)
if !locked {
    conn.Release()                     // someone else owns it → D-04 skip+log+reschedule
    return errAlreadyRunning
}
// ... run the job; heartbeat ticker UPDATEs last_heartbeat_at on THIS conn ...
_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, taskHash) // explicit unlock
conn.Release()                          // return to pool (DISCARD ALL not relied on)
```
**Chaos semantics (SC#2):** if a worker is network-partitioned, its conn dies → Postgres auto-releases the advisory lock at session end → a survivor's `pg_try_advisory_lock` succeeds and picks up the task. Idempotency via `agent_job_runs.completed_with_hash` prevents duplicate side-effects.
**Footgun:** do NOT take the lock on a pooled conn you then return; the unlock would land on a different conn and silently no-op (the lock leaks until that original conn is destroyed). `pgxpool.Config.AfterRelease` is a belt-and-suspenders cleanup but the held-conn pattern is the primary mitigation.

### Pattern 3: agent_job child spawn — mirror swarm.runChild (D-24)
**What:** construct a fresh `agent.NewLlmAgent` directly, NOT through `runner.Turn` (ephemeral session, amendment #23). The shipped template is `internal/swarm/swarm.go:132-192`.
```go
// Source: internal/swarm/swarm.go:136-175 (runChild), VERIFIED current
worker := agent.NewLlmAgent(agent.LlmAgentConfig{
    Client:     deps.Client,
    LLM:        deps.LLM,
    Registry:   tools.Without(parentReg, "swarm_spawn"), // D-13: full registry minus swarm_spawn
    PreviewCap: cfg.ToolPreviewCap,
    RunDir:     cfg.RunDir,
    SessionID:  fmt.Sprintf("agent_job:%s", runID),       // FLAT, ephemeral (amendment #23)
    UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: goal}},
})
budget, _ := agent.NewBudget(agent.BudgetOptions{MaxSteps: &stepBudget}) // from agent_job_runs row (D-24)
ic := agent.InvocationContext{Ctx: ctx, RequestID: uuid.Must(uuid.NewV7()), Budget: budget}
for ev, err := range worker.Run(ic) {
    if err != nil { /* failed */ break }
    if ai := ev.Actions.AwaitingInput; ai != nil { /* D-25 auto-reject — see Pattern 4 */ }
    if ev.LLMResponse != nil && ev.LLMResponse.Content != "" { summary = ev.LLMResponse.Content }
}
```
**Verified signatures:** `agent.BudgetOptions{MaxSteps *int}` (budget.go:83-94); `agent.NewBudget(opts)` (budget.go:110); `ev.Actions.AwaitingInput *AwaitingInput` with `.Question/.Options/.ToolCallID` (event.go:81-90).

### Pattern 4: ask_user auto-reject — inject-and-continue (D-25)
**What:** on `Actions.AwaitingInput`, re-Run a fresh LlmAgent with prior turns + a synthesized `RoleTool` answer (the runner's resume seam, minus the DB). The repl/runner precedent: `RoleTool` message keyed to the original `tool_call_id` (runner_more_test.go:68 `TestSubmitAnswer_Decline` injects a "user declined" RoleTool marker).
```go
// synthesized answer keyed to ai.ToolCallID:
answer := llm.Message{Role: llm.RoleTool, ToolCallID: ai.ToolCallID,
    Content: "<auto-rejected: scheduled job has no human responder>"}
// append the prior assistant tool_calls turn + this answer, re-Run within remaining budget
```
**Acceptance (D-25):** a cron job invoking ask_user never blocks, completes <30s, audit shows the auto-rejected marker. `ask_user` STAYS in the child registry (the PRD wants the model to see the rejection, not a missing tool).

### Anti-Patterns to Avoid
- **God-class store** (pre-rewrite `internal/cron/store.go` was 594 LOC) → sqlc + the identity-04-02 thin store pattern; ≤600 LOC per file (CLAUDE.md).
- **5 separate `task_*` tool files** (pre-rewrite `scheduler.go` tool was 587 LOC) → ONE `task` tool + ActionRouter (D-09).
- **Root-level `oneOf`/`anyOf`/`enum` on the tool schema** → breaks OpenAI-wire (DeepSeek is OpenAI-compat); `required=["action"]` ONLY, per-action reqs in field descriptions (D-10, nanobot regression #3113). A test asserts schema shape.
- **`tier` param / `swarm.TierConfig` reference** → dead machinery (Phase 9 grep-confirmed); CUT (D-12).
- **`runner.Turn` for agent_job** → persists history; amendment #23 forbids it. Direct LlmAgent only (D-24).
- **Storing fixed UTC offset for recurring tasks** → DST drift (D-07).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Cron next-fire compute | hand-rolled field matcher | `gronx.NextTickAfter` | DST/step/range edge cases; zero-dep (D-08) |
| Atomic due-task claim under concurrency | `SELECT` + app-side mutex | `FOR UPDATE SKIP LOCKED` | DB-level skip-locked is the canonical River/GoodJob pattern |
| Continuous single-owner | app heartbeat-only | session `pg_try_advisory_lock` held-conn | auto-release on conn death = free failover (D-02/D-03) |
| Multi-statement atomic writes | manual Begin/Commit | `db.WithTx` (shipped tx.go) | panic-safe rollback, the one DRY seam |
| Migration SQL | AI-hand-rolled | golang-migrate 0009 + sqlc | role separation + idempotency (golang-database skill #12) |
| Injectable clock for ticker tests | real time.Sleep | `Now func() time.Time` (budget.go W8) | goleak-clean, deterministic (no synctest goroutines) |
| Tool multi-action dispatch | per-action tool files | ActionRouter (D-09) | one manifest entry, cache-stable |

**Key insight:** the HA mechanics (SKIP LOCKED + session advisory lock auto-release) are a 40-year-old Postgres pattern; the ONLY novel-to-this-repo wiring is the held-conn lifecycle (D-03) — get that right and the chaos test passes by construction.

## Runtime State Inventory

> Phase 10 is greenfield (`internal/cron` empty) — NOT a rename/refactor. This section is normally omitted, but two state surfaces matter because the daemon is the first long-lived process.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | New `aura.scheduler_tasks` + `aura.agent_job_runs` (migration 0009). No existing rows to migrate. | code edit only (fresh tables) |
| Live service config | systemd unit (D-16, `Restart=on-failure`) — operator-registered, NOT in git as runnable state | documented unit file in repo + operator installs |
| OS-registered state | None — no Windows Task Scheduler / launchd; daemon is the scheduler. Verified: scope is the in-process tick loop. | none |
| Secrets/env vars | New `AURA_SCHEDULER_TZ`, `AURA_SCHEDULER_NOTIFY_DEFAULT` + recipient, `AURA_SCHEDULER_QUIET_HOURS`, tick/cap/retry knobs, `AURA_BACKUP_DIR` — catalog in PRD amendment (D-29). No existing secret renamed. | code + .env.example + PRD env catalog |
| Build artifacts | None stale — greenfield package. | none |

## Common Pitfalls

### Pitfall 1: Advisory lock on a pooled (not held) connection
**What goes wrong:** lock taken on conn A returned to pool; unlock runs on conn B → no-op; lock leaks until conn A is destroyed.
**Why it happens:** session-scoped locks bind to the connection, not the pool.
**How to avoid:** `pool.Acquire()` a dedicated conn and hold it for the run's whole lifetime (D-03); explicit `pg_advisory_unlock` + `Release()` at completion.
**Warning signs:** chaos test shows a task never re-acquired by a survivor; `pg_locks` shows orphaned advisory locks.

### Pitfall 2: Pool starvation from held conns
**What goes wrong:** N concurrent jobs each hold a conn → pool exhausted → tick query / heartbeat blocks.
**Why it happens:** max-concurrent-runs cap not sized vs pool size.
**How to avoid:** size `AURA_SCHEDULER_MAX_CONCURRENT_RUNS` strictly below pool `MaxConns`, reserving headroom for the tick DueTasks query + each run's heartbeat UPDATE (which can run on the held conn, so heartbeat does NOT need a second conn). Planner sizes this (D-03, Claude's discretion).
**Warning signs:** tick latency spikes; `pgxpool` Acquire timeouts.

### Pitfall 3: DST ±1h drift on recurring tasks
**What goes wrong:** "9:30 every morning" fires at 8:30 or 10:30 after a clock change.
**Why it happens:** storing a fixed UTC offset instead of recomputing in-zone.
**How to avoid:** store `(expr, tz)`; recompute via `NextTickAfter(expr, lastFire.In(loc), false)` then `.UTC()` (D-07, Pattern 1).
**Warning signs:** off-by-one-hour fires the week of a DST transition.

### Pitfall 4: goleak failure from the heartbeat ticker
**What goes wrong:** `time.NewTicker` goroutine outlives the run → `goleak.VerifyTestMain` red (CLAUDE.md no-leak gate).
**Why it happens:** ticker not stopped on ctx cancel / run completion.
**How to avoid:** injectable `Now` clock for tests (budget.go:64-68 W8 — a plain func field, NOT Go 1.26 synctest which spawns goroutines that trip goleak); `defer ticker.Stop()`; ctx-cancel-driven shutdown.
**Warning signs:** sub-second "integration" runtime that actually skipped (no-skip-as-green), or a leaked ticker goroutine.

### Pitfall 5: Schema root-level enum breaks DeepSeek
**What goes wrong:** `task` tool with root `oneOf`/`enum` → OpenAI-wire provider 400.
**How to avoid:** `required=["action"]` only; per-action constraints in field descriptions; assert schema shape in a unit test (D-10).

### Pitfall 6: `aura serve` os.Exit kills graceful shutdown
**What goes wrong:** reusing `bootChat` (which `os.Exit`es on every error) inside the long-lived daemon → no clean MCP closer reverse-close, leaked subprocesses.
**How to avoid:** refactor `bootChat` (chat.go:99-160) into an error-RETURNING composition root reused by both chat and serve (D-15); serve owns the signal block + reverse-close.

## Code Examples

### DueTasks claim query (FOR UPDATE SKIP LOCKED)
```sql
-- Source: River/GoodJob canonical pattern + golang-database skill #8
-- index: CREATE INDEX scheduler_tasks_due_idx ON aura.scheduler_tasks (next_run_at)
--        WHERE status = 'active';   (partial index — only active rows polled)
SELECT id, kind, cron_expr, tz, payload, step_budget, ...
FROM aura.scheduler_tasks
WHERE status = 'active' AND next_run_at <= now()
ORDER BY next_run_at
LIMIT $1                       -- batch = max-concurrent headroom
FOR UPDATE SKIP LOCKED;        -- concurrent workers never collide on the same row
```

### Boot orphan scan (D-02/D-18)
```sql
-- stale heartbeat → unknown_recovery; ALSO keep the PRD MaxDuration boot query (D-02)
UPDATE aura.agent_job_runs
SET status = 'unknown_recovery'
WHERE status = 'running'
  AND last_heartbeat_at < now() - interval '90 seconds'
RETURNING id, task_id;
```

### Heartbeat UPDATE (on the held conn, every 30s)
```sql
UPDATE aura.agent_job_runs SET last_heartbeat_at = now() WHERE id = $1;
```

### task_hash → 64-bit advisory key (Claude's discretion, D-A)
```go
// Source: Go stdlib hash/fnv — recommended choice for the 64-bit single-int form
// pg_try_advisory_lock(bigint) takes one int64; FNV-1a 64 over the task identity
// (e.g. the task UUID string) gives a uniform 64-bit key. Collision posture: the
// 64-bit namespace + per-task UUID input makes accidental collision negligible;
// a collision only causes a spurious singleton-skip (D-04 logs + reschedules), never
// a correctness break. Prefer the single-int64 form over the two-int32 form (simpler;
// one namespace). xxhash is an alternative but adds a dep — FNV is stdlib, zero-dep.
h := fnv.New64a()
h.Write([]byte(task.ID.String()))
key := int64(h.Sum64())   // pg_try_advisory_lock($1::bigint)
```

### Injectable clock for the tick loop (goleak-clean)
```go
// Source: internal/agent/budget.go:64-68 W8 — plain func field, NOT synctest
type Scheduler struct {
    Now func() time.Time   // defaults to time.Now; tests inject a controllable clock
    // ...
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Heartbeat table for liveness | River `RescueStuckJobsAfter`; Oban v2.6 REMOVED its heartbeat table; solid_queue rejected advisory locks | 2024-2025 | Industry trends minimal; user OVERRODE to ROADMAP-full (D-02) — implement the full stack anyway, it is the documented deliberate exception |
| 5 separate scheduler tools | one `action`-enum tool (nanobot/ChatGPT-Tasks/OpenClaw convergence) | 2025-2026 | D-09 ActionRouter |
| `daily HH:MM` / `in=10m` grammar (PRD original) | `at \| every \| cron` triad + per-task IANA tz | this phase (D-06) | PRD amendment; only the triad expresses all 4 North-Star queries |

**Deprecated/outdated:**
- PRD `Coordinator.Spawn`/`RejectingResponder`/`TierConfig` references (prd.md:1973/2008/2044/2075): dead since Phase 9; D-24 replaces with direct LlmAgent construction.
- PRD `tier ∈ {worker,chat,reasoning}` validated vs `swarm.TierConfig.Available()`: dead machinery, CUT (D-12).
- PRD smoke `toolsets:[wiki,web]` payload field: dropped (D-13).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | FNV-1a 64 single-int64 advisory key (vs xxhash/sha-trunc) | Code Examples task_hash | LOW — collision only causes a benign singleton-skip+reschedule (D-04); explicitly Claude's discretion (D-A) |
| A2 | Heartbeat UPDATE can run on the held conn (no 2nd conn needed) | Pitfall 2 | LOW — if pool sizing assumed otherwise, planner adjusts max-concurrent cap; verifiable in db_integration tier |
| A3 | gronx DST in-zone correctness for the Europe/Rome spring-forward gap (e.g. 02:30 non-existent local time) | Pattern 1 | MEDIUM — gronx preserves loc via time.Date but the non-existent-local-time normalization is Go-stdlib behavior; add a db_integration/unit test asserting the post-transition fire time (planner: dedicated DST test fixture) |

**All other claims are VERIFIED (codebase reads / go-proxy / pgx+gronx source) or CITED.**

## Open Questions

1. **gronx behavior at a non-existent local wall-clock time (DST spring-forward gap).**
   - What we know: gronx preserves `ref.Location()` and uses `time.Date()` (which normalizes non-existent local times forward by the gap).
   - What's unclear: whether a cron `30 2 * * *` on the spring-forward night fires once-shifted or is skipped.
   - Recommendation: add a dedicated unit test with a frozen `Now` at the Europe/Rome 2026 DST boundary asserting the chosen behavior; document it. Not a blocker — `time.Date` normalization is deterministic.

2. **Backup `docker exec` reachability in CI vs operator host.**
   - What we know: D-26 reuses the Phase-8 LookPath-gated fixed-argv dockerCLI; CI has the compose stack.
   - What's unclear: whether `aura-postgres`/`aura-neo4j` container names are stable across the operator's Docker Desktop + WSL and CI.
   - Recommendation: backup handler tests are Manual-Only / operator-gated (assert artifact in `$AURA_BACKUP_DIR`); CI verifies the exec wiring with a fake docker shim or skips under no-container with `t.Fatal`-under-`$CI` discipline.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Postgres 17 (`aura-postgres`) | claim/heartbeat/orphan queries, migration 0009 | ✓ (compose stack) | 17 | — (blocking; db_integration tier) |
| `github.com/adhocore/gronx` | cron parsing (D-08) | ✓ (go proxy) | v1.20.0 | robfig/cron/v3 (only if gronx gap) |
| pgx/pgxpool v5 | held-conn advisory lock | ✓ (go.mod) | v5.9.2 | — |
| `docker` CLI | backup handlers (D-26) | ✓ (Phase-8 precedent) | — | backup tests Manual-Only if absent |
| WhatsApp/mail MCP | composite delivery (D-19) | ✓ (mounted, main.go:150-158) | — | stdout Notifier fallback (D-22) |
| `Europe/Rome` tzdata | DST-safe NextRunAt | ✓ (Go embeds tzdata or host /usr/share/zoneinfo) | — | bundle `time/tzdata` import if host lacks zoneinfo |

**Missing dependencies with no fallback:** none identified — all blocking deps present.
**Missing dependencies with fallback:** docker (backup tests degrade to Manual-Only); host zoneinfo (import `time/tzdata` to embed — recommend doing so for a self-contained binary).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + table-driven + `goleak.VerifyTestMain` (TestMain in `internal/cron/main_test.go`) + property tests (rapid) where indicated |
| Config file | none (Go convention); build tags `db_integration` for DB tier (CI job `Integration tests (db_integration tag)`, ci.yml:114) |
| Quick run command | `go test ./internal/cron/...` (unit, injectable clock — sub-second, no DB) |
| Full suite command | `go test -tags db_integration -race -count=1 ./internal/cron/... ./internal/db/...` + chaos script (operator) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| CAP-06 / SC#1 | cron fires once per window, no double-exec | db_integration (claim + advisory lock concurrency) | `go test -tags db_integration -race -run TestClaimSkipLocked ./internal/cron/` | ❌ Wave 0 |
| CAP-06 / SC#1 | `at\|every\|cron` parse + tz NextRunAt + DST | unit (injectable clock, frozen Now) | `go test -run TestNextRunAt ./internal/cron/` | ❌ Wave 0 |
| CAP-06 / SC#2 | partitioned worker's task picked up by survivor, no dup side-effects | chaos script (Manual-Only gating + CI-advisory) | `scripts/scheduler_chaos.sh` | ❌ Wave 0 |
| CAP-06 / SC#2 | idempotency via `completed_with_hash` | db_integration | `go test -tags db_integration -run TestIdempotentCompletion ./internal/cron/` | ❌ Wave 0 |
| CAP-06 / SC#3 | nightly backups produce dumps in `$AURA_BACKUP_DIR`; 24h-miss alert | Manual-Only (docker exec artifact) + unit (alert logic) | operator-run + `go test -run TestMissedBackupAlert ./internal/cron/` | ❌ Wave 0 |
| CAP-06 / SC#4 | scheduler agent_job terminates at inherited 10-step budget | unit (mock agent) + db_integration (budget from row) | `go test -run TestAgentJobBudgetInherit ./internal/cron/` | ❌ Wave 0 |
| CAP-06 / SC#4 | ask_user auto-reject never blocks, <30s, audit marker | unit + live smoke (natural prompt) | `go test -run TestAskUserAutoReject ./internal/cron/` | ❌ Wave 0 |
| North-Star Q3 | "ricordami di chiamare Monica alle 17:30" → `at` reminder | live smoke E2E (natural prompt, no "cron" word) | env-gated `cot_eval`-style (OPENROUTER_API_KEY) | ❌ Wave 0 |
| North-Star Q1/Q2 | morning mail summary / Cuneo news → cron agent_job + MCP delivery | live smoke E2E (mail/WhatsApp read-back, Phase 9 precedent) | env-gated, MCP-mounted | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test -race ./internal/cron/` (unit, injectable clock)
- **Per wave merge:** `go test -tags db_integration -race -count=1 ./internal/cron/... ./internal/db/...`
- **Phase gate:** full tag matrix green (unit+db_integration) + chaos script operator-run recorded in VALIDATION.md Manual-Only table + coverage ≥85% owned-surface (CLAUDE.md floor) + live North-Star smoke (Q3 + one cron agent_job) before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/cron/main_test.go` — `goleak.VerifyTestMain` (tick + heartbeat ticker leak gate)
- [ ] `internal/cron/schedule_test.go` — at/every/cron parse + tz NextRunAt + DST-boundary frozen-clock test (Open Q1)
- [ ] `internal/cron/claim_test.go` (db_integration) — FOR UPDATE SKIP LOCKED + advisory-lock singleton (SC#1/SC#2)
- [ ] `internal/cron/recover_test.go` (db_integration) — orphan scan + missed catch-up-once (D-18)
- [ ] `internal/cron/handlers/agentjob_test.go` — budget inheritance (SC#4) + ask_user auto-reject (D-25)
- [ ] `internal/agent/tools/task_test.go` — schema-shape assertion (D-10, required=["action"] only)
- [ ] `scripts/scheduler_chaos.sh` — 3 workers, 60s partition, idempotency (SC#2, topology = planner's choice D-05)
- [ ] CI: add scheduler db_integration to the existing `integration-test` job env (AURA_DB_URL/AURA_DB_MIGRATE_URL already exported, ci.yml:132); chaos as CI-advisory non-blocking (D-05)
- [ ] Live North-Star smoke (Q3 reminder + Q1/Q2 cron agent_job) — env-gated, NOT CI (OPENROUTER_API_KEY)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | single-user `local` identity scaffolding (D-28 FK only, no v1 behavior) |
| V3 Session Management | no | n/a (no web session in this phase; daemon is loopback) |
| V4 Access Control | yes | `aura_app` vs `aura_migrate` role separation (migration 0009 GRANTs mirror 0005/0007); risk-gated `pending_approval` (scoring, D-27) |
| V5 Input Validation | yes | cron expr via `gronx.IsValid` before persist; tool schema `required=["action"]` (D-10); payload scanned by `scoring.ComputeTaskTier` destructive-keyword regex |
| V6 Cryptography | no | FNV hash for advisory key is non-security (collision-tolerant by design, A1) |

### Known Threat Patterns for {Go daemon + Postgres + docker exec backups}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SQL injection in claim/heartbeat queries | Tampering | sqlc parameterized queries only (golang-database skill #2); never string-concat |
| docker socket exposure via backup exec | Elevation | D-26 fixed-argv `docker exec` (LookPath-gated), NEVER mounts `/var/run/docker.sock` (Phase-8 precedent) |
| Destructive scheduled task (e.g. agent_job with `drop`/`rm` payload) | Tampering/Destruction | `scoring.ComputeTaskTier` → Destructive → `RequiresImmediateAlert` + `GateRecommended` → `pending_approval` flow (D-27) |
| Privilege escalation via migration as `aura_app` | Elevation | DDL reserved for `aura_migrate` (AURA_DB_MIGRATE_URL, amendment #17); `aura_app` gets SELECT/INSERT/UPDATE only on the new tables |
| Silent-failing cron (no alert) | Repudiation | notify-on-failure (D-21) + audit row always written + SC#3 24h missed-backup alert |
| Advisory lock leak / pool starvation DoS | DoS | held-conn discipline (D-03) + max-concurrent cap < pool MaxConns (Pitfall 2) |

## Project Constraints (from CLAUDE.md)

- **Coverage floor ≥85%** owned-surface across full tag matrix (overrides PRD 75/60) — report combined figure.
- **No-skip-as-green:** db_integration tier `t.Fatal`s under `$CI` when env unset; chaos script CI-advisory + operator-gating.
- **≤600 LOC per file:** split `scheduler.go`/`claim.go`/`heartbeat.go`/`recover.go` etc.; the pre-rewrite 594-LOC store and 587-LOC tool are the anti-patterns to avoid.
- **Deferred-tool pattern:** `task` is NON-deferred (D-11, core verb) — but Description/schema must stay turn-stable; manifest stays alphabetical (cache-load-bearing).
- **Migration numbering:** floor is 0008 → scheduler is **0009** (verified: last migration is `0008_proxied_child_id_text`).
- **Post-edit validation (Gate 2):** `go vet ./... && go build ./... && go test -race ./internal/<pkg>/` after every Go file edit.
- **One slice = one commit** (or N per sub-slice): 6a infra + 6b agent_job (PRD atomicity note, D-01).
- **PRD-amendment commit BEFORE code** (D-29 Wave-0 plan 10-01).
- **Output user-facing text in Italian** via directive; all prompts in English.
- **golangci-lint=0 + govulncheck + dupl** before phase close (`make quality`).

## Sources

### Primary (HIGH confidence)
- Codebase ground-truth reads (current line numbers VERIFIED): `internal/swarm/swarm.go:120-205` (runChild), `internal/agent/budget.go:60-138` (BudgetOptions/NewBudget/W8 clock), `internal/agent/event.go` (Actions.AwaitingInput:67-90), `cmd/aura/chat.go:95-160` (bootChat composition root), `cmd/aura/main.go:60-169` (serve TODO:70, buildRegistryWithMCP:115, mcpAllowlist:150-158), `cmd/aura/chat_repl.go:116-140` (driveTurn/resume), `internal/identity/store.go:1-57` (canonical store), `internal/runner/runner.go:139-179` (Turn/resume-as-fresh-Run), `internal/db/tx.go` (WithTx), `internal/db/migrations/0007-0008` (migration shape + GRANTs + floor), `internal/scoring/scoring.go` (ComputeTaskTier/GateRecommended/RequiresImmediateAlert), `internal/agent/tools/spec.go:31-101` + `manifest.go` (Deferred/Render), `.github/workflows/ci.yml:114-287` (db_integration tier env)
- `go list -m -versions github.com/adhocore/gronx` → v1.20.0 latest [go module proxy]
- gronx `next.go` source — `loc := ref.Location()` preserved through `bump()`/`time.Date()` [github.com/adhocore/gronx/blob/main/next.go]
- pgx discussions #2097 + PostgreSQL docs — session advisory locks auto-release at session end (even ungraceful disconnect)

### Secondary (MEDIUM confidence)
- gronx README / pkg.go.dev — API signatures (NextTickAfter/IsDue/IsValid), 5/6/7-field support, zero-dep, tasker tz
- Qube Cinema "Unlocking Advisory Locks" + jackc/pgx discussion #2097 — pgxpool held-conn pattern, AfterRelease hook
- River / Oban v2.6 / solid_queue / GoodJob — HA-mechanics survey (D-02 record)
- golang-database skill (samber v1.2.1) — FOR UPDATE FOR-UPDATE, parameterized queries, migration tooling rules

### Tertiary (LOW confidence)
- CronBase DST guide — general cron/tz pitfall framing (corroborates Pattern 1; gronx-specific DST gap behavior flagged Open Q1)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — gronx version go-proxy-verified, pgx in go.mod, all in-tree deps read
- Architecture: HIGH — every shipped seam read at current line numbers; spawn/lock/clock patterns are direct copies of verified code
- Pitfalls: HIGH — advisory-lock + DST pitfalls verified against pgx/gronx source; goleak/clock pitfall is the shipped W8 precedent
- DST non-existent-local-time edge: MEDIUM — flagged Open Q1, needs a frozen-clock unit test (not a blocker)

**Research date:** 2026-06-04
**Valid until:** 2026-07-04 (stable — gronx/pgx are mature; recheck gronx version at plan time with `go list -m -versions`)
