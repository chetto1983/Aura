# Phase 4: HITL + Identity + Conversations - Context

**Gathered:** 2026-05-30
**Status:** Ready for planning
**Method:** Research-grounded discussion across 4 selected areas (loop architecture, Store pattern, CLI/HITL UX, sequencing) + 4 additional locked forks. Industrial patterns triangulated from curated D:/tmp sources (ADK-Go, picobot) + web (LangGraph, Anthropic context-engineering / long-running-harness, MCP elicitation, durable-exec HITL, pg_trgm, Go graceful-shutdown). SPEC already locked all 14 requirements (ambiguity 0.10); this CONTEXT captures only the HOW the planner/researcher would otherwise guess.

<domain>
## Phase Boundary

Move the agent from in-memory-only to crash-recoverable persisted state. Four PRD sub-slices land as one cluster (shared `sqlc`+`golang-migrate` substrate, hard dependency chain): **1.5** `ask_user` pause/resume (FIFO multi-pause + intra-turn exclusivity) on `aura.paused_states`; **1.7** single-user `local` identity + `capability_grants` scaffolding; **1.8** multi-thread Claude.ai-style conversation persistence with deterministic context management (L1 microcompact + L2 budget + L2.5 picobot hard buffer) + per-conversation token+USD aggregation + auto-title; **1.8.5** `pg_trgm` FTS over conversation turns. Migrations `0003`→`0006`.

This is the phase where Aura's agent loop gains a **persistence + orchestration layer** above the Phase-3 `LlmAgent` leaf, and the first real HITL pause primitive.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**14 requirements are locked.** See `04-SPEC.md` for full requirements, boundaries, and acceptance criteria (ambiguity 0.10, gate ≤0.20).

Downstream agents MUST read `04-SPEC.md` before planning or implementing. Requirements are not duplicated here — but **this CONTEXT amends the SPEC** in three places (see `### PRD/SPEC Amendments Required`). Where this CONTEXT and SPEC conflict, the amendments here win.

**In scope (from SPEC.md):**
- `ask_user` tool (non-deferred) + `ErrAwaitingUserInput` sentinel + FIFO multi-pause + intra-turn exclusivity
- `aura.paused_states` persistence with crash recovery + `Resume`/`ResumeBatch`/`Loop.Stop` auto-resolve
- `aura.identities` + `aura.capability_grants` + seeded `local`/`*` + `HasCapability` wildcard + `aura identity` CLI
- `aura.conversations` + `aura.conversation_turns` multi-thread persistence + byte-identical resume + atomic per-turn writes
- Auto-title (best-effort LLM) + per-conversation token + USD aggregation
- Deterministic context management L1 (microcompact) + L2 (dynamic budget) + L2.5 (picobot hard rolling buffer + `context_rot_events`)
- `$AURA_RUN_DIR` cleanup cascade + boot orphan scan + tmp TTL + size WARN
- Conversation FTS via `pg_trgm` GIN + `aura chat search` CLI + the shared query layer for future Telegram `/search`
- `aura chat {list|new|resume|archive|unarchive|delete|rename|search}` + `aura paused-states {list|purge}` CLI
- Migrations `0003`–`0006`

**Out of scope (from SPEC.md):**
- L3 full LLM-driven compaction (`chat_compact`) — deferred
- Internal pause timeout / `timed_out` state — caller owns wall-clock timeouts
- Real multi-user auth / RBAC / login / OAuth — 1.7 is scaffolding only
- `capability_grants` audit table + glob patterns — premature for single-user wildcard
- LLM-facing tools for identity/conversation — infra-only here
- Telegram `/search` binding + `/cancel`/`/cost` — Phase 13 (only the FTS query layer is built here)
- Swarm `proxied_from_child_id` propagation logic — columns exist in 0003; resolution in Phase 9
- KV-cache stable-prefix evaluation of the `system` turn — Phase 6

</spec_lock>

<decisions>
## Implementation Decisions

> All decisions are HOW (implementation), not WHAT (SPEC owns WHAT). Each is the planner's default unless research surfaces a concrete reason to deviate. Research grounding is cited where a decision was triangulated against external sources.

### Loop architecture — Runner pattern (Area 1, deeply researched)

> **Grounding:** ADK-Go (`D:/tmp/adk-go-study`: `runner/runner.go`, `session/service.go`, `internal/llminternal/request_confirmation_processor.go`, `examples/toolconfirmation/main.go`, `agent/workflowagents/loopagent/agent.go`) + picobot (`D:/tmp/picobot/internal/agent/loop.go` + `internal/session`) + LangGraph interrupt/checkpointer (web) + Anthropic context-engineering & long-running-harness (web) + durable-exec HITL (web). All sources converge on: **orchestrator + pluggable persistence + leaf agent**, agent never owns storage.

- **D-A1-01 — New `runner.Runner` orchestrator in `internal/runner/`.** NOT an `Agent`; distinct from the existing `internal/agent/workflow/loop.go` `LoopAgent` (which is a control-flow Agent — naming collision confirmed real in ADK-Go too, which is why ADK names the orchestrator `Runner`). The Runner owns `conversation_id`, drives the agent turn-by-turn, and **persists by observing the Event stream** (ADK `sessionService.AppendEvent`-per-Event style — runner.go:257). SPEC's `Loop.Turn`/`Loop.Stop` verbs → `Runner.Turn`/`Runner.Stop`.
- **D-A1-02 — Pluggable per-domain `Store`s = ADK `session.Service` / LangGraph `Checkpointer` role.** Postgres impl in prod, in-memory fakes in unit tests. Three domain Stores: `identity.Store`, `conversations.Store` (owns `LoadHistory`), `askuser.Store`. (Shape detailed in Area 2.)
- **D-A1-03 — `LlmAgent` stays a DB-free leaf.** Pause **detection** lives in the agent's existing dispatch loop (new file `internal/agent/llm_agent_pause.go`): catch `tools.ErrAwaitingUserInput`, suppress the `RoleTool`, emit a pause Event via a **new `Actions.AwaitingInput`** field (sibling to `Actions.Escalate`). Pause **persistence + resume orchestration** lives in the Runner. **NO ADK-style flow-processor indirection** — Aura's single hand-rolled loop ≠ ADK's processor pipeline; importing it would violate scope/pattern rules.
- **D-A1-04 — `ErrAwaitingUserInput` sentinel lives in `internal/agent/tools/ask_user.go`** (pure types, no DB), so both the tool (returns it, SPEC Req#1) and the agent dispatch (catches it) import it without dragging Postgres into the agent package. The **Event is the pause-payload carrier**; `askuser.Store` never imports `tools`.
- **D-A1-05 — Resume = a *fresh* `agent.Run` over rehydrated history, NOT a suspended goroutine.** Once a `range`-over-func `iter.Seq2` `yield` returns, the iterator is done — Go can't suspend/resume it. So pause = the agent's `Run` emits the pause Event and returns; durability is entirely in the Store. Matches ADK (re-run from session events) + LangGraph (re-invoke from checkpoint).
- **D-A1-06 — Runner verb surface (reworked):**
  - `Turn(ctx, convID string, userMsg *string) iter.Seq2[*agent.Event, error]` — the **sole** loop-driver. `userMsg=nil` = "continue after resume": inject all resolved-but-not-continued answers as `RoleTool` and drive one fresh round.
  - `SubmitAnswer(ctx, token, response) (remaining int, err error)` + `SubmitAnswers(ctx, map) (remaining int, err error)` — **pure persistence** wrappers over `askuser.Store.MarkResumed`/`MarkResumedBatch`; return pending-count so the caller knows when to call `Turn(convID, nil)`. The loop stays paused while ≥1 pending row exists (SPEC Req#2).
  - `Stop(ctx, convID) error` — lifecycle terminate → auto-resolve orphan pendings (Req#11).
  - SPEC's `Resume`/`ResumeBatch` map to `askuser.Store.MarkResumed`/`MarkResumedBatch` (already in Req#3's sqlc list).
- **D-A1-07 — Intra-turn exclusivity wire-correctness.** OpenAI requires every assistant `tool_call` to have a matching `tool` response. When `ask_user` is batched with sibling tool calls, at pause the **persisted assistant message is rewritten to contain only the `ask_user` tool_call(s)** so the wire stays valid (N ask_user → N injected RoleTool answers); dropped siblings are re-emitted by the model next round (SPEC Req#2 "dropped + re-emitted on resume").
- **D-A1-08 — Swarm forward-compat (zero Phase-4 rework).** The pause Event carries `tool_call_id` + originating-agent id. `paused_states.proxied_from_child_id`/`proxied_tool_call_id` (created in 0003) are left NULL for direct calls; Phase 9 populates them as the Event crosses the child→parent boundary (Phase-2 Event bubbling already supports this). The root Runner is the single writer of `paused_states`.

### HITL resolution model — full MCP elicitation (Area 3, researched)

> **Grounding:** MCP elicitation spec (web) + Claude Code permission UX (web) + Anthropic HITL ("pause before irreversible actions") + durable-exec ("pin the approved payload").

- **D-A3-01 — Adopt the MCP three-action model: accept / decline / cancel.** `SubmitAnswer` carries `{action: accept|decline|cancel, content}`; `paused_states.resumed_answer` stores action+content. **accept** → inject the answer as `RoleTool`; **decline** → inject a "user declined" `RoleTool` so the model adapts and continues; **cancel** → abort the turn (reuses the two-stage Ctrl+C → `Loop.Stop` auto-resolve path, Req#11). This is a small SPEC enrichment (see Amendments).
- **D-A3-02 — Kind-specific REPL rendering.** `clarification` → free-text prompt; `approval` → `[y/N]` default **No** (Claude-Code-cautious default, safe for irreversible actions); `choice` → numbered pick (1..N) over the 2-4 `ask_user` options. Multi-pause prompts each pending in `priority DESC, created_at ASC` order.
- **D-A3-03 — No-secrets guardrail (MCP MUST NOT).** `ask_user` MUST NOT be used to collect passwords/API keys/tokens/payment credentials — documented in the tool description + system-prompt awareness. Mirrors MCP elicitation's hard prohibition.
- **D-A3-04 — `ask_user` is a deliberate primitive, never auto-fired** (Claude Code rubber-stamp-fatigue warning). The model calls it intentionally; the system prompt makes it tool-aware without encouraging overuse.

### Persistence layer — canonical Store pattern (Area 2)

> Sets the pattern every future DB slice (Scheduler P10, Skills P11, Memory P15) copies. sqlc already generates ONE `internal/db/sqlc` package (locked in `sqlc.yaml`); the Phase-1 `knowledge_migrations` queries already established the generated surface.

- **D-A2-01 — Per-domain `Store{pool *pgxpool.Pool, q *sqlc.Queries}`** built via `sqlc.New(pool)`; returns a struct. Non-tx methods use `s.q`; atomic multi-statement writes use the tx helper (D-A2-03). SPEC names the three packages (`internal/identity`, `internal/conversations`, `internal/askuser`) — per-domain Stores, NOT one shared store.
- **D-A2-02 — Consumer-side interfaces (Runner-defined) for testability.** Idiomatic Go "accept interfaces, return structs": the `runner` package defines the narrow interfaces it consumes (`ConversationStore`, `PauseStore`, `IdentityStore`) with only the methods it calls; concrete `*conversations.Store`/`*askuser.Store`/`*identity.Store` satisfy them implicitly. Unit tests pass hand-written fakes (no DB → supports the 85% floor); `db_integration` tests use real Postgres. No interface ceremony in the domain packages.
- **D-A2-03 — Shared `db.WithTx(ctx, pool, fn func(*sqlc.Queries) error) error` helper** in `internal/db` (DRY, CLAUDE.md reusable-code). Handles Begin/Commit/Rollback-on-error/panic uniformly. `conversations.Store.AppendTurn` wraps it for the atomic `INSERT turn + UPDATE conv aggregates` (Req#8 / SC-2). Every future multi-statement write reuses it.
- **D-A2-04 — Query files per table** in `internal/db/queries/`: `paused_states.sql`, `identity.sql`, `capability_grants.sql`, `conversations.sql`, `conversation_turns.sql`, `context_rot_events.sql`.
- **D-A2-05 — Composition root = the `aura chat` boot path** (mirrors Phase 3's `chat.go` wiring): `db.Open` → construct the 3 Stores → construct the `Runner`.
- **D-A2-06 — Context management L1/L2/L2.5 lives in `internal/conversations/context.go`**, applied in/around `LoadHistory` per the SC-1 ordering (L1 tool-clearing → L2 budget → L2.5 pair-drop). Token estimation via a **cached `tiktoken-go` cl100k_base encoder** (init once at boot, goleak-safe, ~5-10% approximation for gating only — not billed accuracy, per SPEC Constraints).

### CLI surface (Area 3)

- **D-A3-05 — `aura chat` becomes a cobra command group** (follows Phase 3's `config` precedent on the hand-rolled `main.go` switch dispatcher). Subcommands `{list|new|resume|archive|unarchive|delete|rename|search}`; default `RunE` (bare `aura chat`) = **start a NEW persisted conversation REPL**. `aura chat resume` (no id) = resume most-recent active conversation (SPEC's `resume [<id>]` optional-id form); `aura chat resume <id>` = specific. `aura identity {list|get|grant|revoke}` + `aura paused-states {list|purge}` as their own groups.
- **D-A3-06 — The REPL drives `runner.Runner`** (with a `conversation_id`), NOT a bare `LlmAgent`. Phase 3's streaming + per-turn cost footer (D-11) + dim tool-activity (D-12) + two-stage Ctrl+C (D-10) UX is preserved, now sourced from the Runner's Event stream. A pause Event → the REPL renders the `ask_user` prompt inline (D-A3-02), collects the response, calls `SubmitAnswer`, then `Turn(convID, nil)` to continue.

### Sequencing & execution (Area 4)

- **D-A4-01 — Slice order: 1.7 identity FIRST (derisk the Store pattern), then 1.5 → 1.8 → 1.8.5.** 1.7 is the simplest, fully-independent slice (2 tables, no Runner coupling, no context mgmt, no pause logic) — it establishes + proves the canonical Store pattern (D-A2-01..06) on low-risk surface before 1.5/1.8 copy it. Migration numbers don't force code order (0003 and 0004 are independent until 0005's FK alter).
- **D-A4-02 — PRD-amendment commit FIRST**, then N atomic sub-commits in dependency order with Gate-2 (`go vet + build + test + race`) green between each, ≤600 LOC splits (split `llm_agent.go` only for the pause-detection half → `llm_agent_pause.go`). Mirrors Phase 3 D-01/D-02.
- **D-A4-03 — Full GSD path: `/gsd-plan-phase 4` → `/gsd-execute-phase` (gsd-executor, wave-based).** Per the "follow full GSD procedure for GSD invocations" rule; stays inside the GSD gates end-to-end, no external Codex session.

### Additional locked forks (researched)

> **Grounding:** Go graceful-shutdown consensus + `golang-context` `WithoutCancel` (web + picobot) for auto-title; pg_trgm docs (no `ts_headline`) for FTS; `golang-observability` + Phase 3 OTel precedent for spans.

- **D-A5-01 — Auto-title = lifecycle-bound worker (goleak-safe), NOT fire-and-forget.** Fire via `context.WithoutCancel(turnCtx)` + a bounded `WithTimeout` (outlives the turn whose ctx cancels on `Turn` return, still bounded + cancellable on shutdown — the documented pattern for background work outliving a request). **Track with a `sync.WaitGroup`** owned by the Runner; `Runner.Stop` does a bounded `wg.Wait()`; tests hit that sync point so **goleak sees no leak**. Idempotent `UPDATE … WHERE title IS NULL`; errors never block chat (SPEC Req#9). Fires after `seq >= 3`.
- **D-A5-02 — Boot orphan-scan = reconciliation GC with symlink guard.** `ScanOrphans(ctx, pool, runDir)` runs at boot **after `db.Open`, before serving**; walks `$AURA_RUN_DIR/conversations/*`, `RemoveAll` dirs with no `conversations` row. **`O_NOFOLLOW`/`Lstat` symlink-escape guard** on the walk (a malicious symlink must not redirect `RemoveAll` outside the run dir). tmp/* >24h sweep; **`du` size WARN is audit-only, NEVER auto-purge** (`AURA_RUN_DIR_WARN_THRESHOLD_BYTES` default 1 GiB). rm-failure → WARN + Notifier, recovered at next boot scan (Req#12).
- **D-A5-03 — FTS: SQL query layer is the locked cross-slice contract; excerpt is app-side.** pg_trgm has no `ts_headline`, so excerpting is necessarily application-level. **Locked:** `content % $1 ORDER BY similarity(content,$1) DESC LIMIT $2` (SPEC Req#13) — Telegram `/search` (Phase 13) reuses this exact query. **Excerpt = presentation helper** (per channel): locate the query substring (case-insensitive `strings.Index`), window ±~60 chars, fall back to first-N chars on fuzzy match. CLI prints `<conv_id>|<seq>|<similarity>|<excerpt>`.
- **D-A5-04 — OTel: span the turn, not every query.** `conversation.turn` span wraps each `Runner.Turn` → parents the `llm.request` spans within it (Phase 3 D-03/D-05). One span around the atomic per-turn write tx (`conversation.persist_turn`). A low-cardinality `conversation.pause` span on the pause path. **No per-query spans** (noise/overhead). slog stays thin/secondary (Phase 3 D-30).

### Hardened acceptance criteria (Anthropic + durable-exec grounded)

> These strengthen the SPEC's 14 with industrial-grade, source-grounded tests. They are **verification hardening of scoped requirements — NOT new scope.** They feed the planner's test plan + Gate-3 DoD. Grounding: Anthropic context-engineering ("tool-result clearing is the lightest-touch compaction — do it first"; "clean resumable state, no half-implemented carry-forward"; "detect broken states before resuming") + durable-exec ("pin the approved payload; idempotency is a prerequisite for safe checkpointing").

- **SC-1 — L1-first ordering.** A history bloated by large *tool outputs* must be brought under budget by **L1 alone, zero pairs dropped** (no `context_rot_events` row written) — proves tool-clearing is exhausted before the heavier L2.5 pair-drop.
- **SC-2 — Crash atomicity.** A failure injected *between* the turn `INSERT` and the aggregates `UPDATE` leaves **no partial turn** — after restart, `LoadHistory` shows the conversation as it was *before* the failed turn (transaction rollback), never a half-written turn.
- **SC-3 — Resume never inherits a broken state.** A conversation with an open pending **and** an orphan sidecar dir, after restart+resume: pending auto-resolved (Req#11) **and** orphan dir gone (Req#12), with byte-identical `LoadHistory` on top.
- **SC-4 — Pause = no silent LLM re-run.** On resume the answer is injected as `RoleTool{ToolCallID:<original>}` and the loop does **not** replay the prior turn to re-emit the `ask_user` question via a fresh LLM call — the next request's messages carry the original question→answer pair, no duplicate `ask_user` tool_call.

### PRD/SPEC Amendments Required

> Combined into one PRD-amendment commit at the head of the phase (D-A4-02), before any implementation. Same-slice deviations; mirrors Phase 3's grouped amendments.

- **AM-01 — `_history.go` is NOT in the agent.** SPEC Constraints hint at splitting `llm_agent.go` into `llm_agent.go + _pause.go + _history.go`. Amendment: only `_pause.go` (the pause-**detection** half) lives in the agent (`llm_agent_pause.go`). `LoadHistory` is `conversations.Store.LoadHistory`; the Runner seeds the agent via the *existing* `LlmAgentConfig.UserTurns`. The agent stays DB-free. (Industrial separation: ADK/LangGraph/picobot all keep storage out of the agent.)
- **AM-02 — `paused_states.resumed_answer` gains an action.** SPEC models `resumed_answer` as plain text (accept-only). Amendment: store `{action: accept|decline|cancel, content}` to support the MCP elicitation three-action model (D-A3-01). `decline` and `cancel` are inherent to any HITL pause.
- **AM-03 — SPEC "Loop" → "Runner".** SPEC's `Loop.Turn`/`Loop.Stop` are renamed to `Runner.Turn`/`Runner.Stop` to avoid the naming collision with the existing `internal/agent/workflow` `LoopAgent` (a control-flow Agent). ADK-Go independently names this orchestrator `Runner`.

### Claude's Discretion (defaulted, planner-overridable)
- Exact excerpt window size (~60 chars) and first-N fallback length for FTS.
- Bounded `WithTimeout` value for the auto-title call and the `Runner.Stop` `wg.Wait()` drain timeout.
- Whether `identity.sql` and `capability_grants.sql` are one file or two.
- Exact span names beyond the four pinned (`conversation.turn`, `conversation.persist_turn`, `conversation.pause`).
- Whether `db.WithTx` lives in `internal/db` root or a `internal/db/tx.go` — DRY intent is the constraint, not the path.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Locked requirements (read first)
- `.planning/phases/04-hitl-identity-conversations/04-SPEC.md` — 14 locked requirements, boundaries, constraints, acceptance criteria (ambiguity 0.10). **MUST read** — apply AM-01/AM-02/AM-03 amendments from this CONTEXT where they extend it.

### PRD (source of truth)
- `prd.md` §Slice 1.5 (`ask_user` pause/resume + multi-pause FIFO) — goal, smoke, acceptance, file targets, env catalog, OQ#3 (caller owns timeout).
- `prd.md` §Slice 1.7 (Identity minimal + capability_grants) — wildcard semantics, scaffolding boundary, OQ#1/#3 (audit table + globs deferred).
- `prd.md` §Slice 1.8 / 1.8b (Conversation persistence + microcompact) — L1/L2 context mgmt, auto-title, token+USD aggregation, OQ#3 (L3 deferred).
- `prd.md` §Slice 1.8.5 (FTS) + Amendment #22 (L2.5 picobot hard rolling buffer + `context_rot_events`).
- `prd.md` §Naming convention — `AURA_<DOMAIN>_<UNIT>` env discipline.
- `prd.md` §Slice Q&A discipline — 3 gates (DoR / Implementation / DoD).

### Prior phases (this repo)
- `.planning/phases/03-llm-client-toolresult/03-CONTEXT.md` — Phase-3 decisions Phase 4 consumes: D-26 (`sessionID = Event.ThreadID` UUIDv7 → durable `conversation_id`, same shape no migration), D-25 (shared spillover helper + sidecar layout `$AURA_RUN_DIR/conversations/<id>/`), D-13/D-14 (`text_response` terminal + sequential multi-tool dispatch), D-04/D-15 (error slot = real infra failure only; everything else terminal Event), D-03/D-05/D-30 (OTel `llm.request` spans + real exporter + thin slog), D-22 (`LLMConfig` shape).
- `.planning/phases/03-llm-client-toolresult/03-SPEC.md` — ToolResult signature, `read_tool_output` (the L1 sidecar pointer target).
- `.planning/phases/02-agent-cornerstone/02-CONTEXT.md` — Event/Actions/InvocationContext shape; `Actions.Escalate` (sibling to the new `Actions.AwaitingInput`); `Event.WithContext`/`WithSubAgent` bubbling (swarm proxy forward-compat, D-A1-08); Budget tree; `iter.Seq2` discipline.
- `.planning/phases/01-infra-db-knowledge/01-CONTEXT.md` — D-07 config composition; `db.Open` pool + role separation (`aura_app`/`aura_migrate`); golang-migrate runner; `redactDSN` discipline.
- `.planning/ROADMAP.md` Phase 4 entry — goal + Success Criteria (Slices 1.5/1.7/1.8/1.8.5).
- `.planning/REQUIREMENTS.md` CORE-02 / CORE-03 / CORE-04 — slice-mapped acceptance.
- `.planning/PROJECT.md` — substrate identity, Postgres-primary, single-user `local` default.

### Existing code (drop-in / integration points)
- `internal/agent/llm_agent.go` — the Phase-3 `LlmAgent.Run` loop (`iter.Seq2`, owns in-memory history, dispatch at L191-230). Pause detection added in a new `llm_agent_pause.go`; the agent stays DB-free.
- `internal/agent/event.go` — `Event`/`LLMResponse`/`Actions`; add `Actions.AwaitingInput` (D-A1-03); `ThreadID` (= `conversation_id`).
- `internal/agent/tools/spec.go` + `manifest.go` — `Tool`/`Registry`; `ask_user.go` is a NEW non-deferred tool + the `ErrAwaitingUserInput` sentinel (D-A1-04).
- `internal/agent/workflow/loop.go` — the existing `LoopAgent` (control-flow Agent) the Runner must NOT collide with (AM-03).
- `internal/db/db.go` — `db.Open(ctx,cfg) → *pgxpool.Pool` (composition root input); add `db.WithTx` helper (D-A2-03).
- `internal/db/sqlc/` — generated package (Phase 1 `knowledge_migrations` established the surface; `sqlc.New(DBTX)` + `(*Queries).WithTx(pgx.Tx)`); regenerated after adding queries.
- `internal/db/migrations/` — stops at `0002`; add `0003`–`0006`.
- `internal/db/queries/` — empty; add per-table `.sql` files (D-A2-04).
- `cmd/aura/chat.go` + `chat_render.go` — Phase-3 in-memory REPL; refactor to drive `runner.Runner` (D-A3-06); add cobra subcommand group.
- `cmd/aura/main.go` — hand-rolled switch dispatcher; `chat` becomes a cobra group; add `identity` + `paused-states` cases.

### Codebase maps
- `.planning/codebase/STACK.md`, `STRUCTURE.md`, `CONVENTIONS.md`, `TESTING.md`, `INTEGRATIONS.md`.

### Project discipline
- `CLAUDE.md` §Behavioral rules (≤600 LOC, refactor-on-touch, reusable code, 3-strike, NEVER SUPPOSE), §Tool design (deferred-tool pattern; `ask_user` is **non-deferred**), §Post-edit validation (Gate 2), §Coverage floor 85%, §No-skip-as-green, §Env vars, §Quality tooling (WSL primary, mutation ≥70%, goleak, race).

### Memory priors that constrain decisions
- `feedback_agent_must_know_tools_exist` — tool-aware system prompt (drives D-A3-04 + ask_user awareness).
- `feedback_one_module_per_slice` + `feedback_codex_over_ralph` — atomic sub-commits, stop+verify (D-A4-02).
- `feedback_follow_full_gsd_procedure` — full GSD gates (D-A4-03).
- `feedback_master_direct_workflow` — commit on master, no PRs/branches unless asked.
- `feedback_aura_as_product` + `feedback_lint_coverage_ci_mandatory_phase_end` — quality matrix, SC-1..SC-4 hardening, golangci-lint=0 + ≥85% coverage + green CI at phase end.
- `reference_db_knowledge_integration_test_invocation` — `db_integration` tag invocation + derive `AURA_DB_URL`/`MIGRATE_URL` from `POSTGRES_PASSWORD`.
- `feedback_minipc_cpu_budget` — `tiktoken-go` encoder init once, no busy-loop; bounded background workers.

### External validation (research, 2026-05-30)
- **Curated D:/tmp:** `adk-go-study` (`runner/runner.go`, `session/service.go`, `request_confirmation_processor.go`, `examples/toolconfirmation`, `loopagent`) — the Runner+SessionService+confirmation pattern; `picobot/internal/agent/loop.go` + `internal/session` + `internal/heartbeat` — load/save session separation + ctx-cancel background worker.
- **Web:** LangGraph interrupt + checkpointer; Anthropic "Effective context engineering for AI agents" (tool-result clearing, compaction, structured note-taking) + "Effective harnesses for long-running agents" (clean resumable state, detect broken states); MCP elicitation spec (accept/decline/cancel + no-secrets MUST NOT); Claude Code permission UX (deny→ask→allow, cautious default, rubber-stamp fatigue); durable-exec HITL (pin approved payload, idempotency); pg_trgm docs (no `ts_headline` → app-side excerpt); Go graceful-shutdown + `context.WithoutCancel` for background work outliving a request.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/db/db.go` `Open` → `*pgxpool.Pool` + `redactDSN` — the composition-root input; `sqlc.New(pool)` builds each Store's `*Queries`.
- `internal/db/sqlc` generated surface (`DBTX`, `New`, `WithTx`) — Phase 1 established it; Phase 4 adds queries + regenerates.
- `internal/agent/{event,budget,budget_dedup,tracing}.go` — Event/Actions (+ new `Actions.AwaitingInput`), budget tree, OTel span helpers (Phase 3) reused by the Runner.
- `internal/agent/tools/{spec,manifest,result,read_tool_output}.go` — Registry + ToolResult + the `read_tool_output` builtin that L1 microcompact evicts tool content to (sidecar pointer).
- `cmd/aura/chat.go` REPL scaffold (streaming, cost footer, two-stage Ctrl+C) — refactored to drive the Runner.
- Phase 3 `config` cobra subcommand — the precedent for the `chat`/`identity`/`paused-states` cobra groups.

### Established Patterns
- Module `github.com/chetto1983/aura`; Go 1.26.x; `iter.Seq2` GA; `github.com/google/uuid`, `go.uber.org/goleak`, `pgx/v5` present.
- Phase-1/2/3 test discipline: `goleak.VerifyTestMain`, build-tag integration tiers (`db_integration`), `-race`, owned-surface coverage ≥85%, deterministic CI tier + manual real smoke (no-skip-as-green).
- Role separation: migrations as `aura_migrate`, runtime as `aura_app`; `redactDSN` so `POSTGRES_PASSWORD` never logs.
- Deferred-tool pattern: `ask_user` is **non-deferred** (small, always-visible, like `read_tool_output`/`current_time`).
- picobot background-worker shape (`go func()` + `<-ctx.Done()`) generalizes to the auto-title `WithoutCancel`+WaitGroup worker.

### Integration Points
- `internal/runner/` (new) — Runner orchestrator; imports `agent` + the 3 Stores (via consumer-side interfaces).
- `internal/identity/`, `internal/conversations/`, `internal/askuser/` (new) — domain Stores over `internal/db/sqlc`.
- `internal/agent/llm_agent_pause.go` (new) — pause detection in dispatch.
- `internal/db/migrations/0003`–`0006` (new) + `internal/db/queries/*.sql` (new) + `db.WithTx` helper.
- `$AURA_RUN_DIR/conversations/<id>/` — sidecar layout (Phase 3 D-26) + boot orphan scan (D-A5-02).
- `cmd/aura` — cobra groups `chat`/`identity`/`paused-states` + the Runner-driven REPL + boot wiring (`ScanOrphans`, encoder init, Store + Runner construction).

</code_context>

<specifics>
## Specific Ideas

- **Resume-as-fresh-Run (D-A1-05):** the mental model is "the loop is stateless across pauses; the Store is the only durable thing" — identical to ADK re-running from session events and LangGraph re-invoking from a checkpoint.
- **Auto-title worker (D-A5-01):** `wg.Add(1); go func(){ defer wg.Done(); ctx := context.WithoutCancel(turnCtx); ctx, cancel := context.WithTimeout(ctx, …); … }()` — the `WithoutCancel` is the load-bearing detail (the turn's ctx dies when `Turn` returns).
- **Intra-turn rewrite (D-A1-07):** at pause, the persisted assistant message is rewritten to ask_user-only tool_calls — the concrete fix for the OpenAI "every tool_call needs a tool response" wire rule.
- **FTS cross-slice contract (D-A5-03):** the SQL is the locked artifact (Telegram `/search` Phase 13 reuses it byte-for-byte); only the excerpt rendering differs per channel.
- **1.7-first derisking (D-A4-01):** identity is the "hello world" of the Store pattern — prove `Store{pool,q}` + consumer interfaces + `db.WithTx` + `db_integration` tests there before the Runner-coupled slices.

</specifics>

<deferred>
## Deferred Ideas

- **L3 LLM-driven compaction (`chat_compact`)** → future; 1M DeepSeek window + DB/Neo4j state make the L2 hard cap rare (SPEC out-of-scope, PRD §1.8 OQ#3). L1+L2+L2.5 cover Phase 4.
- **Swarm `proxied_from_child_id` propagation logic** → Phase 9; columns created in 0003, the root Runner is the single writer, Phase-2 Event bubbling carries the child id (D-A1-08).
- **Telegram `/search` + `/cancel` + `/cost` bindings** → Phase 13; only the FTS query layer + the Runner cancel/cost primitives are built here.
- **`capability_grants` audit table + glob patterns (`memory.*`)** → multi-user milestone; single-user wildcard `'*'` is sufficient now.
- **LLM-facing identity/conversation tools** (`identity_grant`, `conversation_search`) → deferred (self-elevation risk); infra-only here.
- **KV-cache stable-prefix evaluation of the system turn** → Phase 6; the system message is just persisted as `seq=1` here.
- **URL-mode elicitation** (MCP's sensitive-interaction mode) → not needed; `ask_user` is form-mode only, with the no-secrets guardrail (D-A3-03).

None of these are acted on in Phase 4.

</deferred>

---

*Phase: 4-hitl-identity-conversations*
*Context gathered: 2026-05-30*
