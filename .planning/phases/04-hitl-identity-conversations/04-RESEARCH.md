# Phase 4: HITL + Identity + Conversations — Research

**Researched:** 2026-05-30
**Domain:** Go agent-loop persistence (pause/resume HITL, identity scaffolding, multi-thread conversation persistence + deterministic context management + pg_trgm FTS) over the existing Postgres/sqlc/pgx/golang-migrate substrate
**Confidence:** HIGH (every recommendation grounded in real code at file:line; the SPEC ambiguity score is 0.10 and CONTEXT pre-locked all HOW decisions)

## Summary

This phase is **not greenfield**. The agent runtime (`internal/agent`), the LLM client + `Usage`/`CostUSD` accounting (`internal/llm`), the ToolResult + sidecar-spillover pattern (`internal/agent/tools`), the Postgres/sqlc/pgx/golang-migrate stack (`internal/db`), and the deferred-tool framework all exist and are tested at ≥85% coverage. Phase 4 adds a **persistence + orchestration layer** *above* the Phase-3 `LlmAgent` leaf: a new `internal/runner` orchestrator, three domain Stores (`internal/identity`, `internal/conversations`, `internal/askuser`), a non-deferred `ask_user` tool + `ErrAwaitingUserInput` sentinel, four migrations (`0003`–`0006`), and `aura chat`/`identity`/`paused-states` CLI groups.

The SPEC (14 locked requirements) and CONTEXT (D-A1..D-A5 decisions, AM-01..AM-03 amendments, SC-1..SC-4 hardened criteria) are exhaustive and pre-closed the design space. This research's job is to **ground each decision in the actual code seam** and surface the gaps between what CONTEXT assumes and what the codebase currently provides. Three such gaps are material and listed as OPEN QUESTIONS: (1) the CLI is a **hand-rolled `switch` dispatcher, not cobra** — go.mod has no `spf13/cobra`; (2) `llm.Config` has **no `ContextWindow` / `MaxOutputTokens` field** — the L2 budget formula's inputs don't exist yet; (3) **`tiktoken-go` is not a dependency** — it must be added (verified available, `v0.1.8`, real GitHub origin).

**Primary recommendation:** Follow CONTEXT's locked decisions verbatim — they are research-grounded and internally consistent. Build in CONTEXT's sequenced order (1.7 identity first to derisk the Store pattern → 1.5 → 1.8 → 1.8.5). Resolve the three OPEN QUESTIONS at plan time (cobra-vs-switch is the biggest: the codebase pattern is `switch`, CLAUDE.md says "FOLLOW EXISTING PATTERNS / never invent new approaches when codebase patterns exist" — this directly contradicts CONTEXT's "cobra command group" assumption). Treat `CREATE INDEX CONCURRENTLY` + `CREATE EXTENSION` in one migration file as a hard landmine (golang-migrate transaction-wrapping).

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (HOW — implementation, planner default unless research surfaces a concrete reason to deviate)

**Loop architecture — Runner pattern (D-A1):**
- **D-A1-01** New `runner.Runner` orchestrator in `internal/runner/`. NOT an `Agent`; distinct from `internal/agent/workflow` `LoopAgent`. Owns `conversation_id`, drives the agent turn-by-turn, persists by observing the Event stream. SPEC's `Loop.Turn`/`Loop.Stop` → `Runner.Turn`/`Runner.Stop`.
- **D-A1-02** Pluggable per-domain `Store`s (Postgres prod, in-memory fakes in unit tests): `identity.Store`, `conversations.Store` (owns `LoadHistory`), `askuser.Store`.
- **D-A1-03** `LlmAgent` stays DB-free. Pause **detection** in a new `internal/agent/llm_agent_pause.go`: catch `tools.ErrAwaitingUserInput`, suppress the `RoleTool`, emit a pause Event via a **new `Actions.AwaitingInput`** field. Pause **persistence + resume orchestration** in the Runner. NO ADK flow-processor indirection.
- **D-A1-04** `ErrAwaitingUserInput` sentinel lives in `internal/agent/tools/ask_user.go` (pure types, no DB). The Event is the pause-payload carrier; `askuser.Store` never imports `tools`.
- **D-A1-05** Resume = a *fresh* `agent.Run` over rehydrated history, NOT a suspended goroutine (a `range`-over-func `iter.Seq2` can't be suspended). Durability lives entirely in the Store.
- **D-A1-06** Runner verb surface: `Turn(ctx, convID, userMsg *string) iter.Seq2[*agent.Event, error]` (sole loop-driver; `userMsg=nil` = continue-after-resume); `SubmitAnswer(ctx, token, response) (remaining int, err error)` + `SubmitAnswers(ctx, map)` (pure persistence wrappers over `askuser.Store.MarkResumed`/`MarkResumedBatch`, return pending-count); `Stop(ctx, convID) error` (lifecycle terminate → auto-resolve orphans).
- **D-A1-07** Intra-turn exclusivity: at pause the persisted assistant message is rewritten to contain ONLY the `ask_user` tool_call(s) so the OpenAI wire stays valid; dropped siblings re-emitted by the model next round.
- **D-A1-08** Swarm forward-compat: pause Event carries `tool_call_id` + originating-agent id; `paused_states.proxied_*` left NULL for direct calls; Phase 9 populates. Root Runner is the single writer of `paused_states`.

**HITL resolution — MCP elicitation (D-A3-01..04):**
- Three-action model: accept / decline / cancel. `resumed_answer` stores `{action, content}`. accept → inject answer as `RoleTool`; decline → inject "user declined" `RoleTool`; cancel → abort the turn (reuses Ctrl+C → `Stop` auto-resolve).
- Kind-specific REPL rendering: `clarification` → free-text; `approval` → `[y/N]` default **No**; `choice` → numbered pick over 2-4 options. Multi-pause prompts in `priority DESC, created_at ASC` order.
- No-secrets guardrail: `ask_user` MUST NOT collect passwords/keys/tokens (documented in tool description + system prompt).
- `ask_user` is a deliberate primitive, never auto-fired.

**Persistence — canonical Store pattern (D-A2-01..06):**
- Per-domain `Store{pool *pgxpool.Pool, q *sqlc.Queries}` via `sqlc.New(pool)`; returns a struct.
- Consumer-side interfaces (Runner-defined): `ConversationStore`, `PauseStore`, `IdentityStore` — narrow, only methods used. Concrete Stores satisfy implicitly. Unit fakes, real Postgres under `db_integration`.
- Shared `db.WithTx(ctx, pool, fn func(*sqlc.Queries) error) error` helper in `internal/db`. `conversations.Store.AppendTurn` wraps it for the atomic INSERT-turn + UPDATE-aggregates write.
- Query files per table in `internal/db/queries/`: `paused_states.sql`, `identity.sql`, `capability_grants.sql`, `conversations.sql`, `conversation_turns.sql`, `context_rot_events.sql`.
- Composition root = the `aura chat` boot path: `db.Open` → 3 Stores → Runner.
- L1/L2/L2.5 in `internal/conversations/context.go`; token estimation via a cached `tiktoken-go` cl100k_base encoder (init once at boot, goleak-safe).

**CLI (D-A3-05/06):**
- `aura chat` becomes a command group `{list|new|resume|archive|unarchive|delete|rename|search}`; bare `aura chat` = start a NEW persisted conversation REPL; `aura chat resume` (no id) = most-recent active. `aura identity {list|get|grant|revoke}` + `aura paused-states {list|purge}` as own groups.
- The REPL drives `runner.Runner` (with `conversation_id`), preserving Phase-3 streaming + cost footer + dim tool-activity + two-stage Ctrl+C. A pause Event → render `ask_user` prompt inline → `SubmitAnswer` → `Turn(convID, nil)` to continue.

**Sequencing (D-A4-01..03):**
- Slice order: **1.7 identity FIRST** (derisk Store pattern), then 1.5 → 1.8 → 1.8.5.
- PRD-amendment commit FIRST, then N atomic sub-commits in dependency order, Gate-2 green between each, ≤600 LOC splits (split `llm_agent.go` only for pause-detection → `llm_agent_pause.go`).
- Full GSD path: `/gsd-plan-phase 4` → `/gsd-execute-phase` (gsd-executor, wave-based).

**Additional locked forks (D-A5-01..04):**
- Auto-title = lifecycle-bound worker via `context.WithoutCancel(turnCtx)` + bounded `WithTimeout`, tracked by a Runner-owned `sync.WaitGroup`; `Runner.Stop` does a bounded `wg.Wait()`. Idempotent `UPDATE … WHERE title IS NULL`; errors never block chat. Fires after `seq >= 3`.
- Boot orphan-scan = `ScanOrphans(ctx, pool, runDir)` after `db.Open`, before serving; `O_NOFOLLOW`/`Lstat` symlink-escape guard; tmp/* >24h sweep; `du` size WARN is audit-only never auto-purge.
- FTS: SQL query layer is the locked cross-slice contract (`content % $1 ORDER BY similarity(content,$1) DESC LIMIT $2`); excerpt is app-side (pg_trgm has no `ts_headline`).
- OTel: span the turn (`conversation.turn` parents `llm.request`), one span around the persist-turn tx (`conversation.persist_turn`), low-cardinality `conversation.pause` span. No per-query spans.

**Hardened acceptance (SC-1..SC-4):** L1-first ordering (no `context_rot_events` row when L1 alone suffices); crash atomicity (no partial turn after INSERT/UPDATE failure); resume never inherits a broken state (pending auto-resolved + orphan dir gone + byte-identical LoadHistory); pause = no silent LLM re-run.

### PRD/SPEC Amendments Required (one PRD-amendment commit at phase head)
- **AM-01** Only `llm_agent_pause.go` (pause-detection) lives in the agent; `LoadHistory` is `conversations.Store.LoadHistory`; the Runner seeds the agent via the existing `LlmAgentConfig.UserTurns`. The agent stays DB-free.
- **AM-02** `paused_states.resumed_answer` stores `{action: accept|decline|cancel, content}` (MCP three-action), not plain text.
- **AM-03** SPEC's `Loop.Turn`/`Loop.Stop` → `Runner.Turn`/`Runner.Stop` (avoid collision with `internal/agent/workflow` `LoopAgent`).

### Claude's Discretion (defaulted, planner-overridable)
- FTS excerpt window size (~60 chars) + first-N fallback length.
- Auto-title `WithTimeout` value + `Runner.Stop` `wg.Wait()` drain timeout.
- `identity.sql`/`capability_grants.sql` one file or two.
- Span names beyond the four pinned.
- `db.WithTx` path within `internal/db` (root or `internal/db/tx.go`).

### Deferred Ideas (OUT OF SCOPE — ignore completely)
- L3 LLM-driven compaction (`chat_compact`).
- Swarm `proxied_from_child_id` *propagation logic* (columns created in 0003; resolution Phase 9).
- Telegram `/search` + `/cancel` + `/cost` bindings (Phase 13; only the FTS query layer is built here).
- `capability_grants` audit table + glob patterns (multi-user milestone).
- LLM-facing identity/conversation tools (`identity_grant`, `conversation_search`).
- KV-cache stable-prefix evaluation of the system turn (Phase 6).
- URL-mode elicitation.
- Internal pause timeout / `timed_out` state.
- Real multi-user auth / RBAC / login / OAuth.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Slice | Research Support |
|----|-------------|-------|------------------|
| **CORE-02** | `ask_user` tool pause/resume FIFO multi-pause, persistent `paused_states`, `ErrAwaitingUserInput` sentinel; swarm `proxied_from_child_id` mapping (columns only here) | 1.5 | Seam: `internal/agent/llm_agent.go:191-230` (`dispatch`) is where `ErrAwaitingUserInput` is caught; sentinel lives in new `tools/ask_user.go` (D-A1-04); new `Actions.AwaitingInput` on `internal/agent/event.go:62-66`; `aura.paused_states` migration `0003`; `internal/askuser/` Store. Maps to SPEC Req#1-4, 11. |
| **CORE-03** | Identity minimal + `capability_grants` scaffolding; single-user `local` + wildcard `'*'`; `HasCapability()` | 1.7 | Migrations `0004`; `internal/identity/` Store; seed via migration `INSERT … ON CONFLICT DO NOTHING` with fixed UUID `00000000-0000-0000-0000-000000000001`; `HasCapability(ctx, identityID, cap)` wildcard-or-exact. Maps to SPEC Req#5-6. |
| **CORE-04** | Conversation persistence multi-thread + microcompact L1 + budget L2 (+ L2.5) + auto-title + token/USD aggregation | 1.8 | Migrations `0005`; `internal/conversations/` Store (`LoadHistory`, `AppendTurn` via `db.WithTx`); context mgmt in `conversations/context.go`; token/USD source = `llm.Usage` + `llm.CostUSD` (`internal/llm/client.go:61`, `internal/llm/prices.go:32`); sidecar layout reuses `tools/result.go` path scheme. Maps to SPEC Req#7-12. |
| **CORE-05** | Conversation FTS via `pg_trgm` GIN + `aura chat search` CLI (+ Telegram `/search` query-layer contract) | 1.8.5 | Migration `0006`; sqlc `SearchConversationTurns`; query layer reusable (not in CLI); excerpt app-side. Maps to SPEC Req#13. |

All four requirement IDs are fully covered by the 14 SPEC requirements; SPEC Req#14 (migration sequence `0003`–`0006`) is the cross-cutting substrate constraint.
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Pause detection (`ErrAwaitingUserInput` interception) | Agent (`internal/agent`) | — | The sentinel surfaces inside tool dispatch; the agent is the only place that sees `Execute` return. Stays DB-free (AM-01). |
| Pause persistence + resume orchestration | Runner (`internal/runner`) | `askuser.Store` | The agent must not own storage (D-A1-03); the Runner observes the pause Event and writes `paused_states`. |
| Conversation persistence + LoadHistory + context mgmt | `internal/conversations` Store | Runner (calls it) | Storage is a domain Store; the Runner consumes a narrow interface (D-A2-02). |
| Token/USD accounting | `internal/llm` (`Usage`, `CostUSD`) — exists | `conversations.Store` aggregates | Per-turn `Usage` already flows on the final chunk; the Store sums it into `conversations.total_*`. |
| Capability lookup | `internal/identity` Store | — | Pure DB read; no Runner coupling (derisk-first slice). |
| FTS query | `internal/conversations` Store (sqlc query) | CLI / future Telegram (presentation) | Query layer is the locked cross-slice contract; excerpt is per-channel presentation (D-A5-03). |
| CLI command dispatch + REPL | `cmd/aura` (package `main`) | Runner (REPL drives it) | The composition root + user surface; **currently a hand-rolled `switch`, not cobra** — see OPEN QUESTION 1. |
| Sidecar filesystem lifecycle | `cmd/aura` boot (`ScanOrphans`) + `conversations.Store` (delete cascade) | `tools/result.go` (writes) | Boot GC + delete-time `RemoveAll`; the write path already exists in `tools/result.go`. |

## Standard Stack

### Core (all already present unless noted)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/jackc/pgx/v5` | v5.9.2 (present) | Postgres pool + tx; sqlc `sql_package: pgx/v5` | Locked in `sqlc.yaml`; `internal/db/db.go` `Open` returns `*pgxpool.Pool` [VERIFIED: go.mod + sqlc.yaml] |
| sqlc | v1.31.1 (generator; generated code present) | Typed query layer in `internal/db/sqlc` | One generated package; Phase-1 `knowledge_migrations` established the surface (`internal/db/sqlc/db.go`, `New(DBTX)`, `(*Queries).WithTx(pgx.Tx)`) [VERIFIED: generated header] |
| `github.com/golang-migrate/migrate/v4` | v4.19.1 (present) | Migration runner, iofs `//go:embed migrations/*.sql` | `internal/db/migrate.go`; runs as `aura_migrate` only [VERIFIED: go.mod + migrate.go] |
| `github.com/google/uuid` | v1.6.0 (present) | UUIDv7 thread IDs / paused-state tokens | `cmd/aura/chat.go:77` mints `uuid.NewV7()` for `sessionID` [VERIFIED: go.mod] |
| `go.uber.org/goleak` | v1.3.0 (present) | Goroutine-leak gate (auto-title worker, multi-pause) | `goleak.VerifyTestMain(m)` in every package's `main_test.go` [VERIFIED: go.mod + db_test.go:25] |
| `github.com/pkoukk/tiktoken-go` | **v0.1.8 (NOT present — must add)** | cl100k_base token estimation for L2 budget gating | CONTEXT D-A2-06 / SPEC Constraints mandate it; ~5-10% approximation, gating only [VERIFIED: go list -m -versions confirms v0.1.8, origin github.com/pkoukk/tiktoken-go; ASSUMED as the right choice — confirm at plan time it is acceptable to add] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `golang.org/x/sync` | v0.20.0 (present, direct) | errgroup (already used by ParallelAgent) | Not strictly needed here; auto-title uses stdlib `sync.WaitGroup` per D-A5-01 |
| `pgregory.net/rapid` | v1.3.0 (present, test-only) | Property-based tests (FIFO ordering determinism, byte-identical LoadHistory) | SC-2/SC-4 hardening if planner wants property tests |
| `github.com/jackc/pgx/v5/pgtype` | (via pgx) | `pgtype.Numeric` (cost `numeric(10,4)`), `pgtype.UUID`, `pgtype.Timestamptz`, `pgtype.Text` (nullable cols) | sqlc emits these for nullable / numeric columns — see Pitfall 5 |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `tiktoken-go` | Char/4 heuristic | CONTEXT explicitly locked tiktoken-go; the heuristic is cheaper-no-dep but less accurate. SPEC says estimation is "gating only, not billed accuracy" — a heuristic *would* technically satisfy that, but deviating contradicts a locked decision. Keep tiktoken-go unless plan-time review objects to the new dep. |
| spf13/cobra | Hand-rolled `switch` (current pattern) | **Material conflict** — see OPEN QUESTION 1. go.mod has no cobra; `cmd/aura/main.go` + `db.go` use nested `switch` dispatchers. CLAUDE.md mandates following existing patterns. |
| `CREATE INDEX CONCURRENTLY` | Plain `CREATE INDEX` | SPEC locks CONCURRENTLY (non-blocking). But it cannot run in a transaction — see Pitfall 6 (golang-migrate landmine). |

**Installation (only the genuinely-new dependency):**
```bash
go get github.com/pkoukk/tiktoken-go@v0.1.8   # verify slopcheck/registry at plan time before adding
```

**Version verification performed:** `go list -m -versions github.com/pkoukk/tiktoken-go` → highest `v0.1.8`; `go list -m -json …@v0.1.8` → Origin URL `https://github.com/pkoukk/tiktoken-go` (real repo, not slopsquat). All other deps are already in go.mod at the versions above.

## Package Legitimacy Audit

> Only one external package is *new* in this phase. All others are already vendored and CI-passing.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `github.com/pkoukk/tiktoken-go` | Go module proxy | mature (v0.1.x since 2023, 8 tagged releases through v0.1.8) | widely used Go tiktoken port | github.com/pkoukk/tiktoken-go (confirmed via `go list -m -json` Origin URL) | not run (slopcheck unavailable in this session) | **Flagged — planner adds `checkpoint:human-verify` before `go get`** |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none by tooling; `tiktoken-go` is tagged `[ASSUMED]` because slopcheck could not be run this session. Per protocol, the planner must gate the `go get` behind a `checkpoint:human-verify` task. Mitigating evidence: real GitHub origin verified, 8 release tags, embeds the BPE vocab the cl100k_base tokenizer needs. Note: at first use it may attempt to **download the cl100k_base `.tiktoken` vocab file over the network** unless an offline encoder/`tiktoken_loader` is configured — verify offline behavior (relevant to `feedback_minipc_cpu_budget` and to CI determinism).

## Architecture Patterns

### System Architecture Diagram

```
                          aura chat / identity / paused-states  (cmd/aura, package main)
                                          │  (composition root: config.Load → db.Open → 3 Stores → Runner)
                                          ▼
   user stdin ──► REPL loop ──► runner.Runner.Turn(ctx, convID, userMsg)
                    ▲                     │
                    │                     ├─► conversations.Store.LoadHistory(convID)
                    │                     │        └─► context.go: L1 microcompact → L2 budget gate → L2.5 pair-drop
                    │                     │                                                │
                    │                     │                                       context_rot_events row (L2.5 only)
                    │                     ▼
                    │            agent.LlmAgent.Run(ic)  ──(existing Phase-3 loop, DB-free)──►  llm.Client.Stream
                    │                     │   dispatch() ──► tool.Execute()
                    │                     │                      │
                    │                     │          ask_user.Execute returns ErrAwaitingUserInput  (sentinel, NOT ToolResult)
                    │                     │                      ▼
                    │            llm_agent_pause.go: catch sentinel, suppress RoleTool,
                    │                     │          rewrite assistant msg to ask_user-only tool_calls (D-A1-07),
                    │                     │          emit Event{Actions.AwaitingInput=...}
                    │                     ▼
                    │            Runner observes pause Event ──► askuser.Store.InsertPausedState (N rows, FIFO)
                    │                     │
                    │            Runner also persists each turn ──► conversations.Store.AppendTurn
                    │                     │                              └─► db.WithTx: INSERT turn + UPDATE conv aggregates (atomic)
                    │                     │                              └─► seq>=3: WithoutCancel auto-title worker (WaitGroup-tracked)
                    │                     ▼
                    └──── pause Event renders inline [y/N]/numbered/free-text prompt
                              user answers ──► Runner.SubmitAnswer(token, {action,content})
                                                  └─► askuser.Store.MarkResumed
                              remaining==0 ──► Runner.Turn(convID, nil)  (continue: inject RoleTool answers, fresh LLM round)

   boot:  db.Open ──► ScanOrphans(runDir): RemoveAll conversations/* dirs with no DB row (O_NOFOLLOW guard); tmp/* >24h sweep; du WARN
   FTS:   aura chat search "q" ──► conversations.Store.SearchConversationTurns(q, limit)  [content % q ORDER BY similarity DESC]
                                       └─► app-side excerpt window ──► print conv_id|seq|similarity|excerpt
```

### Recommended Project Structure (new files in **bold**)
```
internal/
├── runner/                       # NEW — orchestrator (NOT an Agent)
│   ├── runner.go                 # Turn / SubmitAnswer(s) / Stop; consumer-side interfaces
│   └── *_test.go                 # unit (fakes) + db_integration
├── identity/                     # NEW — Store{pool,q}; HasCapability; grant/revoke
├── conversations/                # NEW — Store; LoadHistory; AppendTurn(db.WithTx); context.go (L1/L2/L2.5); search; orphan scan helper
├── askuser/                      # NEW — Store over paused_states; Insert/Get/ListPending/MarkResumed(Batch)/Cleanup
├── agent/
│   ├── llm_agent.go              # MODIFY (split): pause-detection extracted
│   ├── llm_agent_pause.go        # NEW — catch ErrAwaitingUserInput, rewrite assistant msg, emit Actions.AwaitingInput
│   ├── event.go                  # MODIFY — add Actions.AwaitingInput
│   └── tools/
│       └── ask_user.go           # NEW — non-deferred tool + ErrAwaitingUserInput sentinel (pure types)
└── db/
    ├── tx.go (or db.go)          # NEW — db.WithTx helper
    ├── migrations/0003..0006     # NEW — paused_states, identity, conversations(+FK alter+context_rot_events), FTS
    ├── queries/*.sql             # NEW — per-table query files (6)
    └── sqlc/                     # REGENERATED after queries land
cmd/aura/
├── chat.go / chat_render.go      # MODIFY — drive Runner, add subcommand group
├── identity.go                   # NEW — aura identity {list|get|grant|revoke}
├── paused_states.go              # NEW — aura paused-states {list|purge}
└── main.go                       # MODIFY — wire chat group + identity + paused-states cases
```

### Pattern 1: Per-domain Store (canonical, copy from Phase-1 sqlc usage)
**What:** `Store{pool *pgxpool.Pool, q *sqlc.Queries}` built via `sqlc.New(pool)`. Non-tx reads use `s.q`; atomic writes wrap `db.WithTx`.
**When to use:** Every domain package (`identity`, `conversations`, `askuser`). 1.7 proves it first.
**Example (grounded in the existing generated surface):**
```go
// internal/db/sqlc/db.go (EXISTING): New(db DBTX) *Queries ; (*Queries).WithTx(tx pgx.Tx) *Queries
// Store wrapping (NEW), pattern to follow:
type Store struct {
    pool *pgxpool.Pool
    q    *sqlc.Queries
}
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: sqlc.New(pool)} }
```

### Pattern 2: Shared `db.WithTx` for atomic per-turn write (SC-2)
**What:** One reusable Begin/Commit/Rollback-on-error/panic helper.
**When to use:** `conversations.Store.AppendTurn` (INSERT turn + UPDATE aggregates in one tx).
**Example:**
```go
// internal/db/tx.go (NEW) — DRY (CLAUDE.md reusable-code)
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(*sqlc.Queries) error) (err error) {
    tx, err := pool.Begin(ctx)
    if err != nil { return err }
    defer func() {
        if p := recover(); p != nil { _ = tx.Rollback(ctx); panic(p) }
        if err != nil { _ = tx.Rollback(ctx); return }
        err = tx.Commit(ctx)
    }()
    return fn(sqlc.New(tx)) // sqlc.New accepts a pgx.Tx (it is a DBTX)
}
```
*Note: `sqlc.New` takes `DBTX`; `pgx.Tx` satisfies `DBTX` (Exec/Query/QueryRow). Confirmed against `internal/db/sqlc/db.go`.*

### Pattern 3: Pause-detection seam in the agent (D-A1-03/AM-01)
**What:** The existing `dispatch` at `internal/agent/llm_agent.go:191-230` calls `a.runTool` → `tool.Execute`. Today an `Execute` error becomes a RoleTool error string (`runTool`, line 243-245). The pause sentinel must be caught *before* that fallback, suppress the RoleTool, rewrite the assistant message to ask_user-only tool_calls, and emit a pause Event.
**Where exactly:** `runTool` (`llm_agent.go:236-247`) returns `(string)` today; the pause path needs a distinct return channel. The cleanest seam: have `dispatch` call `tool.Execute` (or a thin wrapper) and check `errors.As(err, &tools.ErrAwaitingUserInput{})` before the generic `err != nil` branch. The pause-detection logic moves to `llm_agent_pause.go`.
**Anti-pattern avoided:** Do NOT return the sentinel through the `iter.Seq2` error slot — that violates Phase-2 D-04/D-15 (error slot = real infra failure only). The pause is an **Event** (`Actions.AwaitingInput`), consistent with how budget exhaustion / escalate already work (`event.go:62-66`, `errors.go`).

### Pattern 4: Resume = fresh Run over rehydrated history (D-A1-05)
**What:** A `range`-over-func iterator cannot be suspended. On resume, the Runner calls `conversations.Store.LoadHistory(convID)` → seeds `LlmAgentConfig.UserTurns` → constructs a fresh `LlmAgent` (exactly as `cmd/aura/chat.go:160-168` does today) → drives one round. The injected answers are `RoleTool{ToolCallID:<original>, Content:answer}` rows already in the loaded history (SC-4: no silent LLM re-run).

### Pattern 5: Auto-title worker outliving the turn (D-A5-01)
**What:** `context.WithoutCancel(turnCtx)` + bounded `WithTimeout`, tracked by a Runner-owned `sync.WaitGroup`; `Runner.Stop` does a bounded `wg.Wait()` so goleak sees no leak.
```go
// fires after seq>=3, errors never block chat
r.wg.Add(1)
go func() {
    defer r.wg.Done()
    ctx := context.WithoutCancel(turnCtx)          // turnCtx dies when Turn returns
    ctx, cancel := context.WithTimeout(ctx, r.titleTimeout)
    defer cancel()
    title, err := generateTitle(ctx, ...)          // best-effort
    if err != nil { return }                        // NULL title renders "(untitled <created_at>)"
    _ = r.conv.SetTitleIfNull(ctx, convID, title)   // idempotent UPDATE … WHERE title IS NULL
}()
```

### Anti-Patterns to Avoid
- **Pause via error slot:** breaks D-04/D-15; use `Actions.AwaitingInput` Event.
- **Storage in the agent:** breaks D-A1-03/AM-01; agent stays DB-free.
- **`Loop` naming:** collides with `internal/agent/workflow` `LoopAgent` (confirmed `workflow/loop.go`); use `Runner` (AM-03).
- **Mutating `messages[0]`:** the system prompt is byte-stable and never mutated (`llm_agent.go:35,69,133`); L1 microcompact must only touch `role='tool'` turns, never seq=1 — KV-cache-poisoning constraint (see Pitfall 1).
- **Fire-and-forget goroutine for auto-title:** leaks under goleak; use the WaitGroup pattern.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Token estimation | Custom BPE | `tiktoken-go` cl100k_base (cached encoder) | CONTEXT-locked; BPE is non-trivial; gating-only accuracy is fine |
| Transaction boilerplate | Inline Begin/Commit per Store | `db.WithTx` (D-A2-03) | DRY; uniform rollback-on-panic; SC-2 atomicity |
| Fuzzy text search | LIKE scans / custom ranking | `pg_trgm` GIN + `similarity()` | SPEC-locked; native, indexed, cross-slice contract |
| Typed query layer | Hand-written Scan boilerplate | sqlc-generated `Queries` | Already the project standard; `emit_interface: true` gives the Querier |
| Sidecar spillover for large turn content | New file-write code | Reuse `tools/result.go` path scheme (`<run_dir>/conversations/<id>/<key>.…`) + `validateID` | Path-traversal guard already implemented (T-03-07); turn content uses `<seq>.content` per PRD/SPEC |
| UUIDv7 generation | Custom | `uuid.NewV7()` (already used `chat.go:77`) | Standard, monotonic, sortable |

**Key insight:** ~70% of this phase is *wiring existing primitives* (Store-over-sqlc, sidecar spillover, Usage/CostUSD, UUIDv7, goleak discipline) into a new orchestration layer. The genuinely new mechanisms are: the pause sentinel + Event, the FIFO multi-pause Store, the context-management ladder, and the FTS query.

## Runtime State Inventory

> This is primarily a greenfield-feature phase, but it *introduces* runtime state that future operations must track. Inventory of state this phase creates (relevant to Req#12 boot orphan scan + SC-3):

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | NEW: `aura.paused_states`, `aura.identities`, `aura.capability_grants`, `aura.conversations`, `aura.conversation_turns`, `aura.context_rot_events` — all created this phase | Migrations 0003-0006; seed identity `local`/`*` in 0004 |
| Live service config | None — no external service config embeds phase state. (Telegram `/search` binding is Phase 13.) | None — verified by scope boundaries in SPEC |
| OS-registered state | None — no OS task/service registration in this phase | None — verified (no scheduler until Phase 12) |
| Secrets/env vars | New env vars (code-read only): `AURA_CONVERSATION_TURN_CAP_BYTES`, `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS`, `AURA_HISTORY_HARD_CAP_TURNS`, `AURA_RUN_DIR_WARN_THRESHOLD_BYTES`, `AURA_MODEL_CONTEXT_WINDOW`, `AURA_MODEL_MAX_OUTPUT_TOKENS`. No secrets. | Add to `internal/config/config.go` (see OPEN QUESTION 2 re ContextWindow) |
| Build artifacts | sqlc regenerates `internal/db/sqlc/*` after queries land (NOT hand-edited). `tiktoken-go` may fetch a vocab file at first use | Run `sqlc generate` after adding queries; verify tiktoken offline behavior |

**The canonical cleanup question (Req#12):** after `aura chat delete <id>`, runtime state in three places must be purged: (1) `conversation_turns` + `paused_states` via `ON DELETE CASCADE` FK; (2) `$AURA_RUN_DIR/conversations/<id>/` via `os.RemoveAll` after the DB commit; (3) boot orphan scan reconciles any dir whose `conversations` row no longer exists. The existing sidecar write path (`tools/result.go`) writes into `<run_dir>/conversations/<session_id>/<tool_call_id>.result` — **note the boot scan must treat `session_id == conversation_id`** (Phase-3 D-26: `sessionID = Event.ThreadID = conversation_id`), so the orphan-scan key is the conversation UUID.

## Common Pitfalls

### Pitfall 1: KV-cache poisoning via L1 microcompact mutating cached prefix
**What goes wrong:** L1 rewrites old `role='tool'` turn content to a sidecar pointer. If it touches `messages[0]` (system prompt) or any message inside the stable cached prefix, every subsequent request invalidates the provider prompt cache (DeepSeek-V4 has 80% cache discount per `reference_openrouter_provider_capabilities`).
**Why it happens:** Naive "rewrite old turns" loops include seq=1.
**How to avoid:** L1 only rewrites `role='tool'` turns with `seq < (max_seq - AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS)`. NEVER seq=1 (system). The system prompt is byte-stable (`llm_agent.go:69,133`; `prompt.go`). The cache-poisoning sites map (`reference_aura_cache_poisoning_sites_2026-05-27`) is the prior-art warning. KV stable-prefix evaluation of the system turn is explicitly Phase 6 (deferred), so here just persist system as seq=1 and never rewrite it.
**Warning signs:** A test that asserts `LoadHistory` byte-identity across two calls (SC-4 / SPEC Req#8) failing on the first message; cache-ratio regression in the CoT eval harness.

### Pitfall 2: pgx lazy error at `rows.Err`, classify by SQLSTATE not message
**What goes wrong:** `pool.Query` returns a `pgx.Rows` whose error surfaces only at `rows.Err()` / `Scan`, not at the `Query` call. Code that checks the `Query` error and assumes success mis-handles "table does not exist" (`42P01`) and unique-violation (`23505`) cases.
**Why it happens:** pgx defers query execution. Documented prior bug in this repo (`project_reset_fixed_status_pgx_bug_open`: Reset role-drop + Status `42P01`→empty).
**How to avoid:** sqlc-generated code already checks `rows.Err()` correctly (see `knowledge_migrations.sql.go`). For hand-written classification (e.g. idempotent grant → ignore `23505`, system-managed `'*'` rejection), use `var pgErr *pgconn.PgError; errors.As(err, &pgErr)` and switch on `pgErr.Code` (`23505`, `23503` FK violation). Never string-match the message.
**Warning signs:** A grant/revoke idempotency test (SPEC Req#6) that passes locally but flakes; integration test asserting cascade FK that checks the wrong error.

### Pitfall 3: Goroutine leak from auto-title worker
**What goes wrong:** A fire-and-forget `go generateTitle()` outlives the test; `goleak.VerifyTestMain` fails the package.
**Why it happens:** The title call uses `WithoutCancel(turnCtx)` (correct — it must outlive the turn) but without a join point the test ends before it returns.
**How to avoid:** Runner-owned `sync.WaitGroup`; `Runner.Stop` does a bounded `wg.Wait()`; tests call `Stop` (the sync point). This is D-A5-01, modeled on the picobot background-worker shape.
**Warning signs:** `goleak` reports a goroutine in `generateTitle`/`http`/`tiktoken` at test teardown.

### Pitfall 4: FIFO ordering non-determinism in multi-pause
**What goes wrong:** Three `ask_user` calls in one turn → three rows; if `created_at` collides (same `now()` within a tx), `ORDER BY priority DESC, created_at ASC` is non-deterministic, breaking SPEC Req#2/Acceptance #2.
**Why it happens:** `created_at timestamptz DEFAULT now()` is the *transaction* timestamp — identical for rows inserted in one tx.
**How to avoid:** Add a tiebreaker. Options: insert in a deterministic order and add a secondary sort key (e.g. a per-batch sequence, or `token` as a final tiebreaker for total order). Recommend `ORDER BY priority DESC, created_at ASC, token ASC` so the order is total even when `created_at` ties. Confirm the test inserts with distinct priorities OR relies on the token tiebreaker. (This is a concrete schema/query decision for the planner.)
**Warning signs:** The 3-pause FIFO test flaking under `-count=10`.

### Pitfall 5: sqlc emits `pgtype.*` for nullable/numeric columns, not plain Go types
**What goes wrong:** `total_cost_usd numeric(10,4)` → sqlc emits `pgtype.Numeric` (not `float64`); nullable `title text` → `pgtype.Text`; `resumed_at timestamptz NULL` → `pgtype.Timestamptz`. Code that expects plain types won't compile or mishandles NULL.
**Why it happens:** sqlc maps Postgres nullability/precision to pgx's `pgtype` wrappers (`emit_json_tags`, no `emit_pointers_for_null_types` in `sqlc.yaml`).
**How to avoid:** Plan the Store methods to convert `pgtype.Numeric`/`pgtype.Text` at the boundary (e.g. `.Float64Value()`, `.Valid` checks). Consider whether `total_cost_usd` should be `numeric` (exact, pgtype.Numeric) vs a simpler representation — SPEC locks `numeric(10,4)`, so handle `pgtype.Numeric`. Check the generated `models.go` after `sqlc generate` before writing Store code.
**Warning signs:** Compile errors on Store methods; `(untitled)` rendering logic comparing a string to NULL incorrectly.

### Pitfall 6: `CREATE INDEX CONCURRENTLY` + `CREATE EXTENSION` in one migration file (golang-migrate landmine)
**What goes wrong:** golang-migrate's postgres driver wraps each migration in a transaction. `CREATE INDEX CONCURRENTLY` **cannot run inside a transaction block** (Postgres hard error). golang-migrate auto-detects and skips the tx wrap *only when the CONCURRENTLY statement is the sole statement in the file*. SPEC Req#13 wants `CREATE EXTENSION IF NOT EXISTS pg_trgm` + `CREATE INDEX CONCURRENTLY …` in the same `0006` file — that's two statements, so the auto-detect fails and the migration errors.
**Why it happens:** The single-statement heuristic in golang-migrate.
**How to avoid:** Two viable options for the planner: (a) **split** `CREATE EXTENSION` into a prior statement/migration and keep `CREATE INDEX CONCURRENTLY` as the *only* statement in its own migration file (cleanest with golang-migrate's heuristic); or (b) put `x-migrations-table`/multi-statement config aside and use a plain (non-concurrent) `CREATE INDEX` in the wrapped tx — but SPEC locks CONCURRENTLY. Recommend: `CREATE EXTENSION IF NOT EXISTS pg_trgm` can live in `0006` as a separate concern, but the CONCURRENTLY index must be isolated. The planner should verify the exact golang-migrate v4.19.1 behavior against the embedded iofs source and structure `0006` accordingly. The down migration (`DROP INDEX` + optionally `DROP EXTENSION`) has the same constraint if using `DROP INDEX CONCURRENTLY`.
**Warning signs:** `aura db migrate` fails on `0006` with "CREATE INDEX CONCURRENTLY cannot run inside a transaction block"; SPEC Acceptance #14 (`0003`→`0006` clean apply) fails.

### Pitfall 7: Default privileges already grant `aura_app` — don't re-grant DDL
**What goes wrong:** New tables need `aura_app` SELECT/INSERT/UPDATE/DELETE but NOT DDL. The `0001_init` `ALTER DEFAULT PRIVILEGES` already grants DML on future `aura_migrate`-created tables to `aura_app`.
**Why it happens:** Forgetting that `0001` set default privileges (`0001_init.up.sql` step 3).
**How to avoid:** New migrations get DML grants automatically. Adding explicit `GRANT SELECT, INSERT, UPDATE, DELETE` is belt-and-suspenders (Phase-1 `0002` did this for forensic clarity) but never grant TRUNCATE/DROP/CREATE to `aura_app` (T-1.05-02). Migrations run as `aura_migrate` (Req#14).
**Warning signs:** `aura db migrate` succeeds but runtime `aura_app` queries get permission-denied (missing grant) — or the inverse, role-separation test `TestRoleSeparation_AppDenied` fails because `aura_app` got too much.

## Code Examples

### Existing pause-detection seam (the integration point to modify)
```go
// internal/agent/llm_agent.go:236-247 (EXISTING runTool — the fallback that must NOT swallow the pause sentinel)
func (a *LlmAgent) runTool(ctx context.Context, call llm.ToolCall) string {
    tool, ok := a.registry.Get(call.Function.Name)
    if !ok { return fmt.Sprintf("error: unknown tool %q", call.Function.Name) }
    toolCtx := tools.WithToolCallContext(ctx, a.sessionID, call.ID, a.runDir, a.previewCap)
    res, err := tool.Execute(toolCtx, json.RawMessage(call.Function.Arguments))
    if err != nil {
        return fmt.Sprintf("error: %s", err.Error())  // ← pause sentinel must be intercepted BEFORE this line
    }
    return res.Preview
}
```

### Token/USD accounting source (already exists — Store aggregates these)
```go
// internal/llm/client.go:61 — Usage flows on the trailing chunk; llm_agent.go:146 captures it in `usage`
type Usage struct { PromptTokens, CompletionTokens, CachedTokens int; Cost *float64 }
// internal/llm/prices.go:32 — never reports $0 for unknown model (returns ok=false → "n/a")
func CostUSD(prices map[string]Price, model string, promptTokens, completionTokens int, providerCost *float64) (display string, ok bool)
```
*The Runner reads `usage` after each turn (as `runOneTurn` does at `chat.go:187`) and the conversations Store sums `PromptTokens`/`CompletionTokens`/`CachedTokens` into `total_*_tokens` and the USD figure into `total_cost_usd` inside the same `AppendTurn` tx (SPEC Req#8).*

### FTS query (locked cross-slice contract, D-A5-03 / SPEC Req#13)
```sql
-- internal/db/queries/conversation_turns.sql (NEW)
-- name: SearchConversationTurns :many
SELECT conversation_id, seq, content, similarity(content, $1) AS sim
FROM aura.conversation_turns
WHERE content % $1
ORDER BY similarity(content, $1) DESC
LIMIT $2;
-- Telegram /search (Phase 13) reuses this EXACT query; only excerpt rendering differs.
```

### Migration role + grant pattern (follow 0002 precedent)
```sql
-- 0004_identity.up.sql sketch (follows 0002 grant style; seed is idempotent)
CREATE TABLE aura.identities ( id uuid PRIMARY KEY, name text UNIQUE NOT NULL,
  kind text NOT NULL CHECK (kind IN ('system','user','channel','service')),
  created_at timestamptz NOT NULL DEFAULT now() );
CREATE TABLE aura.capability_grants ( identity_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
  capability text NOT NULL, granted_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (identity_id, capability) );
INSERT INTO aura.identities (id, name, kind)
  VALUES ('00000000-0000-0000-0000-000000000001','local','system') ON CONFLICT DO NOTHING;
INSERT INTO aura.capability_grants (identity_id, capability)
  VALUES ('00000000-0000-0000-0000-000000000001','*') ON CONFLICT DO NOTHING;
-- (DEFAULT PRIVILEGES from 0001 already grant aura_app DML; explicit GRANTs optional/forensic)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| In-memory single-session REPL (`chat.go`) | Persisted multi-thread conversations + crash recovery | This phase | The agent becomes durable; resume = fresh Run over rehydrated history |
| Suspend-the-loop HITL | Stateless loop + durable Store (re-run from session events) | This phase | Matches ADK/LangGraph; a `range`-over-func cannot be suspended (D-A1-05) |
| `ts_headline` for search excerpts | App-side excerpt windowing | pg_trgm has none | Excerpt is per-channel presentation; SQL query is the locked contract |
| `Loop.Turn`/`Loop.Stop` (SPEC draft) | `Runner.Turn`/`Runner.Stop` | AM-03 | Avoids collision with `workflow.LoopAgent` |

**Deprecated/outdated:**
- The pre-rewrite 164-LOC `ask_user` primitive (at tag `pre-rewrite-2026-05-27`) is reference-only; the new tool is non-deferred + sentinel-based.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `tiktoken-go v0.1.8` is the correct, acceptable new dependency for token estimation | Standard Stack | Adding an external dep; planner should gate behind `checkpoint:human-verify` (slopcheck not run this session). Mitigated: real GitHub origin verified |
| A2 | The CLI should remain the hand-rolled `switch` dispatcher, NOT adopt cobra | OPEN QUESTION 1 | If CONTEXT's "cobra group" is taken literally, a new dep + pattern is introduced, contradicting CLAUDE.md "follow existing patterns". Needs explicit plan-time resolution |
| A3 | `ContextWindow`/`MaxOutputTokens` for the L2 budget formula must be ADDED to config (they don't exist in `llm.Config`) | OPEN QUESTION 2 | The L2 `hard_cap = ContextWindow - max(MaxOutputTokens,20000) - 13000` has no input source today; planner must add `AURA_MODEL_CONTEXT_WINDOW`/`AURA_MODEL_MAX_OUTPUT_TOKENS` |
| A4 | FIFO determinism needs a `token`/sequence tiebreaker beyond `priority, created_at` | Pitfall 4 | `now()` ties within a tx → flaky FIFO test; planner must add a total-order tiebreaker |
| A5 | `0006` must isolate `CREATE INDEX CONCURRENTLY` from `CREATE EXTENSION` for golang-migrate | Pitfall 6 | Migration fails on apply if both share a tx-wrapped file; planner must structure the file(s) accordingly |
| A6 | tiktoken-go may fetch the cl100k_base vocab over the network at first use | Package Audit | CI non-determinism + mini-PC network dependency; verify offline encoder config |

## Open Questions

1. **CLI: cobra vs hand-rolled switch.**
   - What we know: CONTEXT (D-A3-05) says "`aura chat` becomes a cobra command group". The ACTUAL codebase has **no `spf13/cobra` in go.mod** and uses nested hand-rolled `switch` dispatchers (`cmd/aura/main.go:29-49`, `cmd/aura/db.go:runDB`). CONTEXT even mis-describes the precedent: it says cobra "follows Phase 3's `config` precedent on the hand-rolled `main.go` switch dispatcher" — i.e. the precedent is a *switch*, not cobra.
   - What's unclear: Whether to introduce cobra (new dep + pattern, contradicts CLAUDE.md "FOLLOW EXISTING PATTERNS / never invent new approaches when codebase patterns exist") or implement the subcommand groups with the existing nested-`switch` pattern.
   - Recommendation: **Use the existing nested-`switch` pattern** (`runChat(args)` → `switch args[0]` over `list|new|resume|…`), mirroring `runDB`. This satisfies the CLI surface in CONTEXT without a new dependency and honors CLAUDE.md. Flag this deviation from CONTEXT's wording explicitly in the PLAN; it is a HOW detail, not a SPEC requirement (SPEC never says "cobra").

2. **L2 budget inputs missing from config.**
   - What we know: L2 formula is `hard_cap = ContextWindow - max(MaxOutputTokens,20000) - 13000`. `llm.Config` (`internal/llm/config.go:54-65`) has `MaxTokens` (the request max, default 4096) but **no `ContextWindow`**. SPEC Constraints list `AURA_MODEL_CONTEXT_WINDOW` / `AURA_MODEL_MAX_OUTPUT_TOKENS` overrides.
   - What's unclear: Where these live (a new `internal/conversations` config, or extend `llm.Config`/`internal/config`) and their defaults (DeepSeek-V4 is ~1M window per memory `reference_openrouter_provider_capabilities`).
   - Recommendation: Add `AURA_MODEL_CONTEXT_WINDOW` (default reflecting the 1M DeepSeek window) + `AURA_MODEL_MAX_OUTPUT_TOKENS` to `internal/config` (or a context-mgmt config), read by `conversations/context.go`. Confirm defaults at plan time.

3. **`conversation_spillover` table — does it exist?**
   - What we know: REQUIREMENTS CORE-04 mentions `aura.conversation_spillover`, but SPEC Req#7 models content spillover as a **sidecar file** (`content_sidecar_path` column + `$AURA_RUN_DIR/conversations/<id>/<seq>.content`), NOT a table. SPEC's migration list (`0003`–`0006`) has no `conversation_spillover` table.
   - What's unclear: Whether CORE-04's mention of a table is superseded by SPEC's sidecar-file approach.
   - Recommendation: Follow SPEC (sidecar file + `content_sidecar_path` column). SPEC is the locked truth-source and post-dates REQUIREMENTS' wording; the sidecar reuses the existing `tools/result.go` pattern. No `conversation_spillover` table.

4. **sqlc `pgtype.Numeric` ergonomics for `total_cost_usd`.**
   - What we know: `numeric(10,4)` → `pgtype.Numeric` in generated models (Pitfall 5). CostUSD produces a `$%.6f` display string + ok flag; the aggregate is a running sum.
   - What's unclear: Whether to aggregate USD in Go (parse CostUSD → float, sum, store) or in SQL (`UPDATE … total_cost_usd = total_cost_usd + $delta`). The atomic-tx approach (SPEC Req#8) favors a SQL `+=`.
   - Recommendation: Aggregate in SQL inside the `AppendTurn` tx with a `numeric` delta; convert at the read boundary for display. Verify the generated type after `sqlc generate`.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Postgres (running) | All migrations + `db_integration` tests | ✓ (compose stack) | 17+ | — (blocking for integration tier) |
| `aura_app` / `aura_migrate` roles | Role-separation migrations | ✓ (EnsureRoles bootstrap) | — | — |
| `pg_trgm` extension | FTS (0006) | Available in standard Postgres contrib | bundled | — (CREATE EXTENSION in migration) |
| sqlc generator | Regenerate after queries land | Available (v1.31.1 generated the existing code) | v1.31.1 | — |
| `tiktoken-go` | L2 token estimation | ✗ (not in go.mod) | add v0.1.8 | char/4 heuristic (deviates from CONTEXT) |
| WSL toolchain (race/coverage/mutation) | Gate-2/Gate-3 | ✓ (CLAUDE.md: WSL primary) | — | CI Linux |

**Missing dependencies with no fallback:** none blocking.
**Missing dependencies with fallback:** `tiktoken-go` (fallback = heuristic, but deviates from a locked decision — prefer adding the dep behind a human-verify checkpoint).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go.uber.org/goleak` v1.3.0 + `pgregory.net/rapid` v1.3.0 (property tests) |
| Config file | none (Go convention); `sqlc.yaml` for query gen |
| Quick run command | `go test ./internal/{runner,identity,conversations,askuser,agent}/...` (unit, fakes) |
| Full suite command | `go test -tags db_integration -race ./internal/... -count=1` (WSL, stack up; derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD` per `reference_db_knowledge_integration_test_invocation`) |

### Phase Requirements → Test Map
> Every assertion must verify the **artifact** (DB row / filesystem / CLI output), never just the LLM reply (memory `probe_must_verify_artifact_not_reply`).

| Req / SPEC AC | Behavior | Test Type | Automated Command | Ground-truth assertion | Tier / File |
|--------|----------|-----------|-------------------|------------------------|-------------|
| Req#1 / AC1 | `ask_user` pauses, writes 1 `paused_states` row, no fake RoleTool | unit + db_integration | `go test ./internal/runner/...` ; `-tags db_integration` | `SELECT count(*) FROM aura.paused_states WHERE resumed_at IS NULL` == 1; assistant msg has no `role='tool'` for the ask_user call | ❌ Wave 0 |
| Req#2 / AC2 | 3 simultaneous `ask_user` → 3 rows, FIFO `priority DESC, created_at ASC[, token]` | db_integration + rapid | `-tags db_integration` | `ListPending` returns 3 in deterministic order; `ResumeBatch` injects 3 `RoleTool` | ❌ Wave 0 |
| Req#2 / AC3 | intra-turn exclusivity: only `ask_user` dispatched, siblings dropped | unit | `go test ./internal/agent/...` | persisted assistant msg contains ONLY ask_user tool_calls; `len(pending)==2` for 2×ask_user+1×other | ❌ Wave 0 |
| Req#3 | crash recovery: restart store, `ListPending` returns rows in order; invalid token rejected | db_integration | `-tags db_integration` | rows survive new `Store` instance; `Resume(badToken)` returns a clear error | ❌ Wave 0 |
| Req#4 | no internal timeout / no `timed_out` status | grep/smoke | `grep -r timed_out internal/ ` (must be empty) | no `timed_out` in schema or loop | ❌ Wave 0 |
| Req#5 / AC7 | fresh boot seeds `local`/`*`; `HasCapability("local","any_tool")`==true | db_integration | `-tags db_integration` | `SELECT … FROM aura.capability_grants` == 1 row `(0…001,'*')`; `HasCapability` true via wildcard | ❌ Wave 0 |
| Req#6 / AC8 | grant/revoke idempotent; `'*'` grant/revoke rejected; FK cascade | db_integration | `-tags db_integration` | repeat grant = no error / 1 row; `grant local '*'` → non-zero exit + system-managed msg; delete identity cascades grants | ❌ Wave 0 |
| Req#7 / AC5 | persist 3 turns, restart, resume reconstructs history; >cap spills to sidecar | db_integration | `-tags db_integration` | `LoadHistory` returns 3 turns post-restart; `content=NULL` + `content_sidecar_path` set + file on disk for >65536B | ❌ Wave 0 |
| Req#8 / SC-2 | `LoadHistory` byte-identical ×2; atomic per-turn tx; failure → no partial turn | db_integration + rapid | `-tags db_integration` | two `LoadHistory` calls byte-equal; injected failure between INSERT/UPDATE → rollback, no orphan turn | ❌ Wave 0 |
| Req#9 / AC4 | auto-title after seq>=3; LLM fail leaves NULL no crash; `chat list` shows non-zero USD | unit (fake client) + db_integration | both | `title` set after 3 turns; fake-error → title NULL, chat continues; `total_cost_usd` > 0 aggregated | ❌ Wave 0 |
| Req#10 / AC9,AC10 / SC-1 | L1 evicts tool result after N turns (sidecar still fetchable); L2.5 drops oldest pair + writes `context_rot_events`, `len even`; L1-first (no rot row when L1 suffices) | smoke + unit | `scripts/microcompact_smoke.sh` + `go test` | tool turn content→pointer after N; `read_tool_output` still works; `SELECT … context_rot_events` row on hard-drop; `len(history)%2==0`; SC-1: zero rot rows when L1 alone fits | ❌ Wave 0 + script |
| Req#11 / AC11 | `Runner.Stop` auto-resolves orphan pendings | db_integration | `-tags db_integration` | zero `resumed_at IS NULL` rows for conv after Stop; `paused-states list` shows auto-terminated answer | ❌ Wave 0 |
| Req#12 / AC12 / SC-3 | delete cascade removes turns+paused_states+`$AURA_RUN_DIR/conversations/<id>/`; boot orphan scan; resume on broken state recovers | db_integration | `-tags db_integration` | dir gone after delete; stray dir removed at boot; SC-3: pending auto-resolved + orphan gone + byte-identical LoadHistory | ❌ Wave 0 |
| Req#13 / AC6 | `aura chat search "phrase"` returns excerpts by similarity; same query → identical set (cross-slice) | db_integration + CLI smoke | `-tags db_integration` | rows ordered by `similarity` DESC from GIN index; query layer identical to future Telegram | ❌ Wave 0 |
| Req#14 / AC13,AC14 | `0003`→`0006` apply clean; re-run no-op; denied as `aura_app`, ok as `aura_migrate` | db_integration | `-tags db_integration` | migrate count 4 on fresh DB, 0 on re-run; DDL as `aura_app` → permission denied | ❌ Wave 0 |
| SC-4 | resume injects RoleTool, no duplicate ask_user tool_call / no silent LLM re-run | unit (fake client) | `go test ./internal/runner/...` | next request messages carry original question→answer pair, no second ask_user call | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/<package>/ && go test -race ./internal/<package>/` (Gate-2, CLAUDE.md post-edit validation)
- **Per wave merge:** `go test -tags db_integration -race ./internal/... -count=1` (WSL, stack up)
- **Phase gate:** full tag matrix green + `golangci-lint run ./...` == 0 + coverage ≥85% (owned surface) + mutation ≥70% on critical files (`context.go`, `askuser` Store, pause-detection) before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/runner/*_test.go` — unit (fakes) for Turn/SubmitAnswer/Stop + SC-4 (no silent re-run)
- [ ] `internal/identity/*_test.go` — HasCapability wildcard, grant/revoke idempotency, `'*'` rejection, FK cascade (db_integration)
- [ ] `internal/conversations/*_test.go` — LoadHistory byte-identity, AppendTurn atomicity (SC-2), context L1/L2/L2.5 (SC-1), search, orphan scan
- [ ] `internal/askuser/*_test.go` — Insert/ListPending FIFO order, MarkResumed(Batch), crash recovery, cleanup
- [ ] `internal/agent/llm_agent_pause_test.go` — sentinel interception, intra-turn rewrite, Actions.AwaitingInput emission
- [ ] `scripts/microcompact_smoke.sh` — L1 eviction + sidecar fetch + L2.5 pair-drop with `context_rot_events` row (mirrors `scripts/loop_budget_smoke.sh` precedent)
- [ ] Shared fakes: in-memory `PauseStore`/`ConversationStore`/`IdentityStore` for unit tests (no DB → supports 85% floor); reuse `agenttest.FakeClient` for LLM
- [ ] CI: ensure `0003`-`0006` migration job runs under `db_integration` with composed DSNs (no-skip-as-green)
- [ ] Framework install: `go get github.com/pkoukk/tiktoken-go@v0.1.8` (behind human-verify checkpoint)

## Security Domain

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Identity is *scaffolding* only; no auth/login this phase (SPEC out-of-scope) |
| V3 Session Management | partial | `conversation_id` = UUIDv7 (unguessable); no auth session yet |
| V4 Access Control | partial | `HasCapability` wildcard is the *seam* for future enforcement; not enforced on tools yet (infra-only) |
| V5 Input Validation | yes | `ask_user` args validation (empty question, options count, distinct labels, priority 0-100); capability name regex `^[a-z][a-z0-9._-]{0,63}$`; FTS query is parameterized (`$1`) |
| V6 Cryptography | no | No new crypto; UUIDv7 via `google/uuid` |
| V12 File Resources | yes | Sidecar path-traversal guard (`tools/result.go` `validateID`); boot orphan scan `O_NOFOLLOW`/`Lstat` symlink-escape guard (D-A5-02) |

### Known Threat Patterns for Go + Postgres + agent loop
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SQL injection in FTS / queries | Tampering | sqlc parameterized queries (`$1`); never string-concat user input |
| Path traversal via conversation_id / tool_call_id in sidecar | Tampering | `validateID` rejects `..` + separators before `filepath.Join` (existing, T-03-07); reuse for `<seq>.content` |
| Symlink escape in boot orphan `RemoveAll` | Tampering/EoP | `O_NOFOLLOW`/`Lstat` guard on the walk (D-A5-02) — a malicious symlink must not redirect RemoveAll outside run dir |
| Secrets captured via `ask_user` | Info disclosure | No-secrets guardrail in tool description + system prompt (D-A3-03 / MCP MUST NOT) |
| Self-elevation via LLM-facing identity tool | EoP | Identity/capability tools are infra-only (CLI), NOT LLM-facing (deferred) |
| Password leak in DSN logs | Info disclosure | Existing `redactDSN` discipline (`db.go:72`); migrations never log credentials |
| `aura_app` performing DDL | EoP | Role separation: migrations as `aura_migrate` only; `aura_app` denied DDL/TRUNCATE (Req#14, T-1.05-02) |

## Sources

### Primary (HIGH confidence)
- Codebase (file:line cited inline): `internal/agent/llm_agent.go`, `event.go`, `agent.go`, `errors.go`, `tools/{spec,result,read_tool_output,manifest}.go`, `internal/llm/{client,config,prices}.go`, `internal/db/{db,migrate}.go`, `internal/db/sqlc/*`, `internal/db/migrations/000{1,2}*`, `internal/db/queries/knowledge_migrations.sql`, `internal/config/config.go`, `cmd/aura/{main,chat,db}.go`, `sqlc.yaml`, `go.mod` — all read this session
- `.planning/phases/04-hitl-identity-conversations/04-SPEC.md` (14 locked requirements, acceptance criteria)
- `.planning/phases/04-hitl-identity-conversations/04-CONTEXT.md` (D-A1..D-A5, AM-01..03, SC-1..4)
- `prd.md` §Slice 1.5/1.7/1.8/1.8.5 (schemas, env catalog, file targets)
- `.planning/REQUIREMENTS.md` (CORE-02..05)
- `go list -m -versions/-json github.com/pkoukk/tiktoken-go` (registry + origin verification)

### Secondary (MEDIUM confidence)
- [PostgreSQL pg_trgm docs](https://www.postgresql.org/docs/current/pgtrgm.html) — `gin_trgm_ops`, `%` operator, `similarity()`, GIN vs GiST
- [golang-migrate issue #284 / #137](https://github.com/golang-migrate/migrate/issues/284) — CREATE INDEX CONCURRENTLY transaction-wrap constraint + single-statement auto-detect
- [postgres driver pkg.go.dev](https://pkg.go.dev/github.com/golang-migrate/migrate/v4/database/postgres) — multi-statement / tx behavior

### Tertiary (LOW confidence — flagged)
- tiktoken-go offline-vocab behavior (A6) — not verified this session; planner must confirm

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all deps verified in go.mod / registry; one new dep (tiktoken-go) origin-verified
- Architecture: HIGH — every seam grounded at file:line; CONTEXT pre-locked the design
- Pitfalls: HIGH (1-3,5,7 grounded in code + repo memory) / MEDIUM (4 FIFO determinism, 6 golang-migrate — verified via official issues but exact v4.19.1 behavior needs plan-time confirmation)
- Open questions: 3 material gaps between CONTEXT assumptions and actual code (cobra, ContextWindow config, spillover table)

**Research date:** 2026-05-30
**Valid until:** 2026-06-29 (stable substrate; re-verify tiktoken-go + golang-migrate behavior if the phase slips)
