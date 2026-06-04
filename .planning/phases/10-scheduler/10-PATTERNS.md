# Phase 10: Scheduler - Pattern Map

**Mapped:** 2026-06-04
**Files analyzed:** 18 new/modified targets
**Analogs found:** 16 / 18 (2 greenfield with no in-repo analog → RESEARCH.md patterns)

All file targets are derived from CONTEXT.md (D-01..D-29) and RESEARCH.md §"Recommended Project Structure" (lines 134-159). Two sub-slice commits: **6a infra** (migration + stores + parsing + tick + claim) and **6b agent_job** (ActionRouter + `task` tool + spawn seam + handlers + notify).

> **Cross-cutting hard constraint surfaced during mapping:** `internal/cron` MUST NOT import `internal/swarm` (D-24). The proven child-spawn recipe (`swarm.runChild`) and the `Without` registry helper currently live in `internal/swarm`. The planner MUST promote `Without` (currently `internal/swarm/registry.go:10`) to `internal/agent/tools` (e.g. `Registry.Without` or a `tools.Without` free function) so BOTH swarm and cron consume it without a cycle. See Shared Pattern §Registry-Without below. This was flagged as OQ2 in `.planning/DECISIONS.md:179`.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/db/migrations/0009_scheduler.up.sql` / `.down.sql` | migration | CRUD/DDL | `internal/db/migrations/0007_cache_metrics.up.sql` | exact |
| `internal/db/queries/scheduler_tasks.sql` | config (sqlc) | CRUD | `internal/db/queries/identity.sql` | exact |
| `internal/db/queries/agent_job_runs.sql` | config (sqlc) | CRUD | `internal/db/queries/identity.sql` | exact |
| `internal/cron/store.go` | store | CRUD | `internal/identity/store.go` | exact (D-A4-01 canonical) |
| `internal/cron/schedule.go` | utility | transform | (none in-repo) → gronx Pattern 1 | no-analog (greenfield) |
| `internal/cron/claim.go` | service | event-driven (DB claim) | (none) → RESEARCH Pattern 2 | no-analog (greenfield) |
| `internal/cron/heartbeat.go` | service | event-driven (ticker) | `internal/agent/budget.go` W8 clock | partial (clock pattern only) |
| `internal/cron/recover.go` | service | batch | `cmd/aura/chat.go` ScanOrphans boot scan (99-131) | role-match |
| `internal/cron/scheduler.go` | service | event-driven (tick loop) | `internal/agent/budget.go` W8 injectable clock | partial (clock/goleak) |
| `internal/cron/notify.go` | service | request-response (egress) | `cmd/aura/main.go` mcpAllowlist (150-158) | partial (MCP self-send seam) |
| `internal/cron/handlers/reminder.go` | handler | transform | `internal/swarm/swarm.go` runChild (132-192) | role-match |
| `internal/cron/handlers/agentjob.go` | handler | request-response (LLM) | `internal/swarm/swarm.go` runChild (132-192) | **exact** (THE template) |
| `internal/cron/handlers/backup.go` | handler | file-I/O (subprocess) | `internal/mcp/client.go` exec.CommandContext (68-71) | role-match |
| `internal/agent/tools/action.go` | utility | request-response | `internal/agent/tools/spec.go` (Tool/Spec/Registry) | role-match |
| `internal/agent/tools/task.go` | tool | request-response | `cmd/aura/main.go` buildBaseRegistry registration (87-113) + spec.go Spec | role-match |
| `cmd/aura/serve.go` | route (daemon) | event-driven | `cmd/aura/chat.go` bootChat (95-160) | exact (refactor target) |
| `cmd/aura/task.go` | route (CLI) | request-response | `cmd/aura/web.go` runWeb/runWebDoctor (21-48) + main.go switch (41-76) | exact |
| `scripts/scheduler_chaos.sh` | test | event-driven | (none) → web-tools E2E cross-compile precedent | no-analog (greenfield) |

## Pattern Assignments

### `internal/cron/store.go` (store, CRUD)

**Analog:** `internal/identity/store.go` — the canonical Store the whole DB lineage (identity → askuser → conversations) copies verbatim (D-A4-01 / CONTEXT D-A4-01).

**Struct + constructor pattern** (`internal/identity/store.go:45-57`):
```go
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}
```
Rule the cron store copies: non-tx reads use `s.q`; atomic multi-statement writes wrap `db.WithTx` (the claim insert + next_run_at recompute is the first place cron actually needs `WithTx`, unlike identity which is single-statement only).

**Domain projection at the package boundary** (`internal/identity/store.go:59-70`) — plain Go types, NOT the sqlc `pgtype` wrappers; a `fromRow` converter per table:
```go
type Identity struct { ID, Name, Kind string }
func fromRow(r sqlc.AuraIdentities) Identity {
	return Identity{ID: uuid.UUID(r.ID.Bytes).String(), Name: r.Name, Kind: r.Kind}
}
```
The cron store gets `Task`/`Run` domain structs + `taskFromRow`/`runFromRow`; the `tz`, `cron_expr`, `payload`, `step_budget`, `next_run_at` columns project to plain Go types.

**Missing-row → sentinel, never raw pgx.ErrNoRows** (`internal/identity/store.go:87-96`):
```go
if errors.Is(err, pgx.ErrNoRows) {
	return Identity{}, fmt.Errorf("get identity %q: %w", name, ErrIdentityNotFound)
}
```
Cron declares `ErrTaskNotFound`, `ErrAlreadyRunning` (D-04) as package sentinels.

**SQLSTATE classification — errors.As + pgErr.Code, NEVER string-match** (`internal/identity/store.go:180-185`):
```go
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
```
Reuse the idempotent-insert idiom for `agent_job_runs.completed_with_hash` (the SC#2 idempotency column — a duplicate completion swallows 23505).

---

### `internal/db/migrations/0009_scheduler.up.sql` (migration, DDL)

**Analog:** `internal/db/migrations/0007_cache_metrics.up.sql` — the migration floor is **0008** (`0008_proxied_child_id_text`, verified), so scheduler is **0009**.

**Role-separation GRANT block** (`0007_cache_metrics.up.sql:29-30`) — DDL reserved for `aura_migrate`, `aura_app` gets only the DML it needs:
```sql
GRANT SELECT, INSERT ON aura.cache_metrics TO aura_app;
GRANT ALL            ON aura.cache_metrics TO aura_migrate;
```
For 0009: `scheduler_tasks` needs `SELECT, INSERT, UPDATE, DELETE` (cancel + next_run_at recompute); `agent_job_runs` needs `SELECT, INSERT, UPDATE` (heartbeat + status transitions, append-then-update — no DELETE, audit-forever per PRD OQ4). `GRANT ALL` to `aura_migrate` on both.

**Index build inside the implicit migration tx** (`0007_cache_metrics.up.sql:21-27`) — plain (non-CONCURRENT) index on a fresh empty table; golang-migrate forbids the concurrent variant in a tx block. The RESEARCH DueTasks query (lines 297-308) wants:
```sql
CREATE INDEX scheduler_tasks_due_idx ON aura.scheduler_tasks (next_run_at)
    WHERE status = 'active';   -- partial: only active rows polled
```

**Forward-compat columns (D-28):** `identity_id` FK → `aura.identities` (default `local`, CORE-03 parity) + `origin_conversation_id uuid NULL` (forensics parity with `paused_states`) + `agent_job_runs.paused_state_token` FK → `aura.paused_states(token)` (the task.approve-after-ask_user path; cron itself never writes paused_states per D-25). `COMMENT ON TABLE` per the 0007 convention (line 32-33).

---

### `internal/db/queries/scheduler_tasks.sql` + `agent_job_runs.sql` (sqlc, CRUD)

**Analog:** `internal/db/queries/identity.sql` — the sqlc `-- name: X :one|:many|:exec` annotation shape.

```sql
-- name: CreateIdentity :one
INSERT INTO aura.identities (id, name, kind)
VALUES ($1, $2, $3)
RETURNING id, name, kind, created_at;

-- name: ListIdentities :many
SELECT id, name, kind, created_at FROM aura.identities
ORDER BY created_at ASC, name ASC;
```
Cron's DueTasks claim query is NOT a plain sqlc SELECT — it needs `FOR UPDATE SKIP LOCKED` (RESEARCH lines 297-308); sqlc handles it as a `:many` with the locking clause inline. The advisory-lock `SELECT pg_try_advisory_lock($1)` and `pg_advisory_unlock($1)` run via raw `conn.QueryRow`/`conn.Exec` on the HELD conn (Shared Pattern §Held-conn advisory lock), NOT through sqlc, because they must bind to a specific `*pgxpool.Conn`.

---

### `internal/cron/handlers/agentjob.go` (handler, request-response/LLM)

**Analog:** `internal/swarm/swarm.go` `runChild` (lines 132-192) — THE proven template (D-24). Construct a fresh `agent.NewLlmAgent` directly; NEVER `runner.Turn` (amendment #23 ephemeral session).

**Child construction** (`internal/swarm/swarm.go:136-144`):
```go
worker := agent.NewLlmAgent(agent.LlmAgentConfig{
	Client:     rc.Client,
	LLM:        rc.LLM,
	Registry:   Without(rc.ParentRegistry, swarmSpawnTool), // → tools.Without after promotion
	PreviewCap: rc.Cfg.ToolPreviewCap,
	RunDir:     rc.Cfg.RunDir,
	SessionID:  fmt.Sprintf("%s-swarm-%s", rc.ConvID, childID), // FLAT — no slash
	UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: structuredBrief(goal)}},
})
```
For agent_job: `SessionID: fmt.Sprintf("agent_job:%s", runID)` (flat, ephemeral, amendment #23); `UserTurns` = the task goal as the first user message; `Registry` = full registry minus `swarm_spawn` (D-13, no toolsets scoping).

**Budget from the DB row** (D-24, step_budget column → `internal/agent/budget.go:110` + `:83-94`):
```go
budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &stepBudget}) // stepBudget from agent_job_runs row
ic := agent.InvocationContext{Ctx: ctx, RequestID: uuid.Must(uuid.NewV7()), Budget: budget}
```
`BudgetOptions.MaxSteps *int` (nil = fall through to env/default; non-nil overrides) — a one-line wire from the `agent_job_runs.step_budget` row.

**Event-stream drain + AwaitingInput interception** (`internal/swarm/swarm.go:155-175`):
```go
for ev, err := range worker.Run(ic) {
	if err != nil { /* failed */ break }
	if ev == nil { continue }
	if ai := ev.Actions.AwaitingInput; ai != nil { /* D-25 auto-reject — see below */ continue }
	if ev.LLMResponse != nil && ev.LLMResponse.Content != "" { summary = ev.LLMResponse.Content }
}
```
`ev.Actions.AwaitingInput` is `*agent.AwaitingInput` carrying `.Question`, `.Options []PauseOption`, `.ToolCallID` (`internal/agent/event.go:81-90`). The handler returns the final `summary` → `agent_job_runs.summary` (forensics, D-19).

**ask_user auto-reject — inject-and-continue (D-25):** on `AwaitingInput`, re-Run a fresh LlmAgent with prior turns + a synthesized RoleTool answer keyed to `ai.ToolCallID` (the `chat_repl.go` SubmitAnswer resume seam, minus the DB):
```go
answer := llm.Message{Role: llm.RoleTool, ToolCallID: ai.ToolCallID,
	Content: "<auto-rejected: scheduled job has no human responder>"}
```
`ask_user` STAYS in the child registry (the PRD wants the model to SEE the rejection, not a missing tool). Acceptance: cron job invoking ask_user never blocks, completes <30s, audit shows the marker.

**HandlerMeta pattern** (Slice 0.9 / CONTEXT "Established Patterns"): each TaskKind = 1 file with an `agent.Agent` impl + `HandlerMeta{Kind, MaxDuration, ReschedulesOnRecovery}`. No dispatch switch — `Handler = agent.Agent`.

---

### `internal/cron/handlers/backup.go` (handler, file-I/O subprocess)

**Analog:** `internal/mcp/client.go` (lines 68-71) — fixed-argv `exec.CommandContext` (the Phase-8 dockerCLI precedent, D-26). Steal scheduler-mcp's command-executor shape (subprocess + captured exit status).

```go
// G204: Command/Args come from operator-controlled config, not model output.
cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...) //nolint:gosec
cmd.Env = append(os.Environ(), cfg.Env...)
```
For backups: `exec.CommandContext(ctx, "docker", "exec", "aura-postgres", "pg_dump", ...)` and `"docker", "exec", "aura-neo4j", "neo4j-admin", "database", "dump", ...`. LookPath-gate `docker` first; NEVER mount the socket. Destination `AURA_BACKUP_DIR` (default `~/.aura/backups/`), 14d/7d retention. Capture exit status → `agent_job_runs.summary`; SC#3 fires a 24h-missed alert. Backup handler tests are Manual-Only / operator-gated (RESEARCH Open Q2 — container-name stability), `t.Fatal`-under-`$CI` discipline.

---

### `internal/agent/tools/action.go` + `task.go` (utility + tool, request-response)

**Analog:** `internal/agent/tools/spec.go` (Tool/Spec/Registry mechanics) + `cmd/aura/main.go:87-113` (registration).

**Tool interface the ActionRouter+task tool implements** (`internal/agent/tools/spec.go:30-58`):
```go
type Spec struct {
	Name        string
	Summary     string          // one line, always shown in the manifest
	Description string
	Parameters  json.RawMessage // JSON-schema for the tool arguments
	Deferred    bool            // task = false (D-11, core verb)
}
type Tool interface {
	Spec() Spec
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}
```
`task` is **non-deferred** (D-11): `Deferred: false`, tight 1-line `Summary`. ActionRouter (`action.go`, ~90 LOC, D-09 first consumer) dispatches the `action` enum (`schedule|list|cancel|run_now|approve`) inside one `Execute`.

**Schema discipline (D-10, nanobot regression #3113):** top-level `required = ["action"]` ONLY; per-action requirements in field descriptions; NO root-level `oneOf`/`anyOf`/`enum` (breaks OpenAI-wire — DeepSeek is OpenAI-compat). A unit test asserts the schema shape (`task_test.go`).

**Registration (non-deferred built-in)** (`cmd/aura/main.go:87-104`) — register in `buildBaseRegistry` so both `buildRegistry` and `buildRegistryWithMCP` get it; manifest auto-sorts alphabetically (cache-load-bearing, NEVER hand-order):
```go
reg.Register(tools.AskUser{}) // ... existing non-deferred built-ins ...
reg.Register(&tools.WebFetch{Engine: webEngine}) // manifest auto-sorts; never hand-order
```
`reg.Validate()` (spec.go:94-102) stays green because `task` is non-deferred.

---

### `cmd/aura/serve.go` (daemon route, event-driven)

**Analog:** `cmd/aura/chat.go` `bootChat` (lines 95-160) — the composition root D-15 refactors into an **error-returning** boot (no `os.Exit`) shared by chat + serve.

**Current composition root** (`cmd/aura/chat.go:99-159`) — note every error path `os.Exit`s (RESEARCH Pitfall 6: this kills graceful shutdown if reused as-is):
```go
func bootChat(ctx context.Context) *chatEnv {
	cfg, err := config.Load() // ... os.Exit on err ...
	pool, err := db.Open(ctx, &cfg.DB) // ... os.Exit ...
	reg, mcpClosers, err := buildRegistryWithMCP(ctx, cfg) // ... os.Exit ...
	client := openai_compat.New(cfg.LLM)
	run := runner.New(runner.Deps{ Conv, Pause, Identity, CacheMetrics, Client, Registry: reg, LLM, RunDir, PreviewCap, EvictAfter })
	return &chatEnv{cfg, pool, conv, pause, identity, run, client, mcpClosers}
}
```
Refactor target: extract `bootServe(ctx) (*serveEnv, error)` returning errors; serve owns `scheduler.Start(ctx)` + signal block (SIGINT/SIGTERM) + graceful shutdown: ctx cancel → finish in-flight tick → join workers → `closeMCPServers` reverse-close (already exists, `main.go:161-169`) → goleak-clean.

**Boot orphan scan precedent** (`cmd/aura/chat.go:124-131`) — the recover.go boot scan (D-18 orphan + missed-catch-up) mirrors `conversations.ScanOrphans` (a scan failure is a WARN degradation, not a boot-blocker):
```go
if err := conversations.ScanOrphans(ctx, pool, ...); err != nil {
	fmt.Fprintln(os.Stderr, "warn: boot orphan scan:", err)
}
```

**Switch-case wiring** (`cmd/aura/main.go:70-71`) — replace the TODO:
```go
case "shell", "serve":
	fmt.Println("TODO: implemented by the agent-loop and CLI slices") // → runServe(os.Args[2:])
```

---

### `cmd/aura/task.go` (CLI route, request-response)

**Analog:** `cmd/aura/web.go` `runWeb`/`runWebDoctor` (lines 21-48) + the `main.go:41-76` hand-rolled switch (runDB/runIdentity precedent, no cobra).

**Subcommand dispatch shape** (`cmd/aura/web.go:21-35`):
```go
func runWeb(args []string) {
	if len(args) == 0 { fmt.Fprintln(os.Stderr, "usage: ..."); os.Exit(exitUsage) }
	switch args[0] {
	case "doctor": runWebDoctor()
	case "tool":   runWebTool(args[1:])
	default: fmt.Fprintf(os.Stderr, "aura web: unknown subcommand %q ...\n", args[0]); os.Exit(exitUsage)
	}
}
```
`runTask` switches `schedule|list|cancel|run_now|approve|runs|doctor` (D-14/D-17). `aura task schedule` carries the full triad `--cron`/`--at`/`--every` + `--max-steps` (amendment #19) so every grammar path is operator-testable without an LLM (SC#1 + chaos/smoke scripts).

**`task doctor` shape** (`cmd/aura/web.go:42-48` runWebDoctor) — config.LoadDB() (no OPENROUTER_API_KEY needed), human-readable output, sysexits codes; checks last-tick freshness, due tasks, in-flight runs, heartbeat staleness against PG (D-17). Matches `aura mcp doctor`/`web doctor`. Add the `case "task": runTask(os.Args[2:])` to `main.go`'s switch and to `usage()`.

## Shared Patterns

### Held-conn advisory lock (D-03) — the load-bearing novel wiring
**Source:** RESEARCH.md Pattern 2 (lines 174-192) — no in-repo analog; pgx-source-verified.
**Apply to:** `internal/cron/claim.go` (claim) + `internal/cron/heartbeat.go` (UPDATE on the SAME held conn).
```go
conn, err := pool.Acquire(ctx)        // dedicated conn — DO NOT Release until job done
var locked bool
err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, taskHash).Scan(&locked)
if !locked { conn.Release(); return errAlreadyRunning } // D-04 skip+log+reschedule
// ... run job; heartbeat ticker UPDATEs last_heartbeat_at on THIS conn ...
_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, taskHash)
conn.Release()
```
**Footgun (Pitfall 1):** a session lock taken on a pooled conn you then return unlocks on a DIFFERENT conn → silent no-op leak. Hold the conn. Size `AURA_SCHEDULER_MAX_CONCURRENT_RUNS` strictly below pool `MaxConns` (Pitfall 2). `task_hash` = FNV-1a 64 over the task UUID (RESEARCH lines 325-337, Claude's discretion).

### Multi-statement atomic write — db.WithTx
**Source:** `internal/db/tx.go` (the one DRY seam; identity is single-statement so doesn't use it, but conversations/askuser do).
**Apply to:** the claim insert (`agent_job_runs` row) + `next_run_at` recompute, and any completion-status + idempotency-hash write. Panic-safe rollback. Do NOT hand-roll Begin/Commit (RESEARCH "Don't Hand-Roll").

### Registry-Without — MUST be promoted out of internal/swarm
**Source:** `internal/swarm/registry.go:10` `func Without(parent *tools.Registry, names ...string) *tools.Registry`.
**Apply to:** `internal/cron/handlers/agentjob.go` needs `Without(reg, "swarm_spawn")` (D-13) but CANNOT import `internal/swarm` (D-24). **Planner action:** move `Without` to `internal/agent/tools` (as `Registry.Without` method or `tools.Without` free func), update `internal/swarm` to call the promoted version. This is the OQ2 carve-out (`.planning/DECISIONS.md:179`). Deep-refactor-on-touch (CLAUDE.md) covers the swarm callsite update in the same commit.

### Injectable clock + goleak (W8) — tick loop + heartbeat ticker tests
**Source:** `internal/agent/budget.go:64-94` (W8 RATIONALE: plain `Now func() time.Time` field, NOT Go 1.26 synctest which spawns goroutines that trip goleak).
**Apply to:** `internal/cron/scheduler.go` (`Now func() time.Time` default `time.Now`) + `heartbeat.go` (`defer ticker.Stop()`, ctx-cancel shutdown). Test gate: `internal/cron/main_test.go` with `goleak.VerifyTestMain(m)` (exact copy of `internal/identity/main_test.go:13-15`, build-tagged for the db_integration tier).
```go
// internal/identity/main_test.go (copy verbatim, retarget package)
func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }
```

### MCP self-send delivery (D-19) — already-mounted egress
**Source:** `cmd/aura/main.go:150-158` `mcpAllowlist` — the allowlists already include `send_email` (mail) and `send_message` (whatsapp).
**Apply to:** `internal/cron/notify.go` — the Notifier resolves the per-task `notify: whatsapp|email|stdout` route to an MCP tool call on the already-mounted registry; stdout fallback + bounded retry (D-22, mirrors the Phase 9 fail-soft MCP boot posture). No new MCP wiring — the channel is production since Phase 9.

### Risk gating — scoring is shipped, P10 is first consumer
**Source:** `internal/scoring/scoring.go:120` `ComputeTaskTier(TaskArgs) RiskTier`, `:177` `GateRecommended(t) bool` (Risky||Destructive), `:183` `RequiresImmediateAlert(tier, threshold)`.
**Apply to:** the `task schedule` path + agent_job dispatch → `ComputeTaskTier` → `GateRecommended` ⇒ `pending_approval` status; `RequiresImmediateAlert` ⇒ ride the composite Notifier (D-27). Threshold is an argument (config owns `AURA_RISK_ALERT_THRESHOLD`), never an env read inside scoring. Phase 8 D-12 scope guard lifts here.

### gronx tz-via-ref DST-safe NextRunAt (D-07)
**Source:** RESEARCH.md Pattern 1 (lines 161-172) — gronx preserves `ref.Location()`; NO tz parameter.
**Apply to:** `internal/cron/schedule.go`.
```go
loc, _ := time.LoadLocation(task.TZ)              // "Europe/Rome"
refInZone := lastFire.In(loc)                     // carry tz via the ref
next, _ := gronx.NextTickAfter(task.CronExpr, refInZone, false) // false = strictly after
task.NextRunAt = next.UTC()                       // store UTC, NEVER a fixed offset
```
Add a frozen-`Now` DST-boundary unit test (Open Q1: non-existent local wall-clock at spring-forward). `gronx.IsValid` validates the expr before persist (V5 input validation).

## No Analog Found

Files with no close in-repo match (planner uses RESEARCH.md patterns):

| File | Role | Data Flow | Reason / Source |
|------|------|-----------|-----------------|
| `internal/cron/schedule.go` | utility | transform | First cron-parsing code in the repo (pre-rewrite version deprecated). Use RESEARCH Pattern 1 (gronx tz-via-ref) + gronx README. |
| `internal/cron/claim.go` | service | event-driven | First `FOR UPDATE SKIP LOCKED` + session advisory lock in the repo. Use RESEARCH Pattern 2 + Code Examples (lines 297-337). |
| `scripts/scheduler_chaos.sh` | test | event-driven | First chaos script. Topology = planner's choice (D-05); web-tools E2E cross-compile + `docker network disconnect` is the cited precedent (not an in-repo analog). |

Partial-only (clock/boot-scan idiom borrowed, but the surface is greenfield): `internal/cron/scheduler.go`, `heartbeat.go`, `recover.go`, `notify.go`.

## Metadata

**Analog search scope:** `internal/identity`, `internal/swarm`, `internal/agent` (budget/tools/event/scoring), `internal/db` (migrations/queries), `internal/mcp`, `cmd/aura` (chat/main/web/chat_repl).
**Files scanned (read):** 12 source files + 2 query/migration files + 1 test (TestMain).
**Pattern extraction date:** 2026-06-04
**Anti-patterns to avoid (RESEARCH lines 227-233):** pre-rewrite 594-LOC `store.go` god-class (→ sqlc + thin store), 587-LOC `scheduler.go` tool (→ ActionRouter), root-level schema enum (→ `required=["action"]`), `tier`/`TierConfig` dead refs (D-12 cut), `runner.Turn` for agent_job (amendment #23 — direct LlmAgent), fixed-UTC-offset recurring tasks (DST drift). ≤600 LOC per file (CLAUDE.md): the structure splits `scheduler.go`/`claim.go`/`heartbeat.go`/`recover.go`/`schedule.go`/`store.go`/`notify.go` precisely to stay under the floor.
