# Phase 4: HITL + Identity + Conversations — Specification

**Created:** 2026-05-30
**Ambiguity score:** 0.10 (gate: ≤ 0.20)
**Requirements:** 14 locked

## Goal

Add the agent-loop persistence cluster: `ask_user` pause/resume (FIFO multi-pause) backed by `aura.paused_states`, single-user `local` identity + `capability_grants` scaffolding, multi-thread Claude.ai-style conversation persistence with deterministic context management (L1+L2+L2.5) and per-conversation token+USD aggregation, and `pg_trgm` full-text search over conversation turns — moving the agent from in-memory-only to crash-recoverable persisted state.

## Background

After Phase 3 the agent is the real `LlmAgent` (`internal/agent/llm_agent.go`) that drives `llm.Client` through a budget-gated tool loop, but **all conversational state is in-memory**: `cmd/aura/chat.go` is a stateless REPL with no `list/resume/new/archive`, messages are appended only to `LlmAgent`'s in-memory slice, and there is no `ask_user`, no identity model, and no persistence. Migrations on disk stop at `0002_knowledge_migrations` — none of `paused_states`, `identities`, `capability_grants`, `conversations`, or `conversation_turns` exist. There is no `internal/identity/`, `internal/conversations/`, or `internal/askuser/` package, and `internal/agent/tools/` has no `ask_user.go`. This phase introduces four PRD sub-slices (1.5 / 1.7 / 1.8 / 1.8.5) as one cluster because they share the Postgres `sqlc` + `golang-migrate` substrate and have a hard dependency chain (1.8.5 FTS needs 1.8's `conversation_turns`; `paused_states.conversation_id` gains its FK only once `conversations` exists in 1.8).

## Requirements

1. **ask_user pause primitive**: A non-deferred `ask_user` tool pauses the loop and waits for structured user input.
   - Current: No `ask_user` tool exists; the loop cannot pause for input. Pre-rewrite primitive (164 LOC) preserved at tag `pre-rewrite-2026-05-27`.
   - Target: `internal/agent/tools/ask_user.go` exposes a non-deferred `ask_user` tool with args `{question:string, options?:[2-4 string|{label,value}], kind:clarification|approval|choice, priority?:int 0-100}`; `Execute` returns sentinel `ErrAwaitingUserInput{Question,Options,Kind,Priority,ToolCallID}` (NOT a `ToolResult`); the loop intercepts it and does NOT append a fake `RoleTool`.
   - Acceptance: Stub LLM returns one `ask_user(kind=approval)` call → loop enters paused state, one `PausedState` row written to `aura.paused_states`, no `RoleTool` appended; args validation rejects empty `question`, `options` count of exactly 1, non-distinct labels, and `priority` outside 0-100.

2. **Multi-pause FIFO + intra-turn exclusivity**: Multiple simultaneous `ask_user` calls coalesce as separate ordered pending states; `ask_user` wins over batched tool calls.
   - Current: No pause queue exists.
   - Target: N `ask_user` calls in one turn → N `PausedState` rows ordered `priority DESC, created_at ASC`; loop stays paused while ≥1 row has `resumed_at IS NULL`; `Resume(token,answer)` resolves one, `ResumeBatch(answers map)` resolves many; when an LLM turn batches `ask_user` with other tool calls, only `ask_user` dispatches and the others are dropped + re-emitted on resume.
   - Acceptance: Test with stub returning `2×ask_user + 1×read_tool_output` in one turn → exactly 2 `PausedState` rows, `read_tool_output` dropped, `len(pending)==2`; `ResumeBatch` of all answers injects each as `RoleTool{ToolCallID:<original>, Content:answer}` and the loop resumes a single LLM round with all accumulated tool results.

3. **paused_states persistence + crash recovery**: Pending pauses survive process restart.
   - Current: No `paused_states` table.
   - Target: Migration `0003_paused_states.up.sql` creates `aura.paused_states (token uuid pk, conversation_id text, kind text CHECK IN (clarification,approval,choice), question text, options jsonb, priority int 0-100, resume_context jsonb, tool_call_id text, proxied_from_child_id uuid null, proxied_tool_call_id text null, created_at, resumed_at null, resumed_answer null)` with partial index `(conversation_id, resumed_at) WHERE resumed_at IS NULL`; `conversation_id` is plain `text` here (no FK) for 1.5↔1.8 independence; sqlc queries `InsertPausedState/GetByToken/ListPending/MarkResumed/MarkResumedBatch/CleanupResumedOlderThan`.
   - Acceptance: Integration test (`db_integration`) inserts pending rows, restarts the store, and `ListPending(conv_id)` returns them in `priority DESC, created_at ASC` order; invalid token to `Resume` is rejected with a clear error.

4. **No internal pause timeout (locked boundary)**: A paused loop has no internal wall-clock timeout.
   - Current: No pause machine exists.
   - Target: The loop state machine has exactly `running → waiting_for_user → running`; there is NO `timed_out` terminal state and no internal sweeper that expires `PausedState` rows by age. Any wall-clock timeout is the caller's (CLI/Telegram) responsibility.
   - Acceptance: No code path transitions a `PausedState` to a timed-out state on a timer; the only resolution paths are `Resume`, `ResumeBatch`, or `Loop.Stop` auto-resolve (Req 11). Grep confirms no `timed_out` status value in the schema or loop code.

5. **Identity minimal + capability_grants scaffolding**: Single-user `local` identity seeded with a wildcard grant.
   - Current: No identity model; `skill_audit.actor_id` (future) would be opaque `text`.
   - Target: Migration `0004_identity.up.sql` creates `aura.identities (id uuid pk, name text unique, kind text CHECK IN (system,user,channel,service), created_at)` + `aura.capability_grants (identity_id uuid FK ON DELETE CASCADE, capability text, granted_at, pk(identity_id,capability))`; seeds identity `local` with fixed UUID `00000000-0000-0000-0000-000000000001` (kind `system`) and a `'*'` grant, both `ON CONFLICT DO NOTHING`.
   - Acceptance: After `aura db migrate` on a fresh DB, `aura.identities` has exactly one `local`/`system` row with the fixed UUID and `aura.capability_grants` has one `(0...001, '*')` row.

6. **HasCapability wildcard semantics + identity CLI**: Capability lookup with match-all wildcard; CLI manages grants.
   - Current: No capability check exists.
   - Target: `HasCapability(ctx, identityID, cap) (bool, error)` returns true when the identity has either `'*'` or the exact `cap`; capability names validated `^[a-z][a-z0-9._-]{0,63}$`; CLI `aura identity {list|get <name>|grant <name> <cap>|revoke <name> <cap>}` with idempotent grant/revoke, and grant/revoke of `'*'` rejected with a clear "wildcard is system-managed" error.
   - Acceptance: `HasCapability("local","any_tool")` returns `true` on a fresh boot (wildcard); `aura identity grant local foo` then `revoke local foo` are idempotent; `aura identity grant local '*'` exits non-zero with the system-managed message; integration test covers grant/revoke idempotency, wildcard rejection, and FK cascade on identity delete.

7. **Conversation persistence multi-thread**: Conversations + per-message turns persist and survive restart.
   - Current: `cmd/aura/chat.go` is an in-memory single-session REPL; no `conversations`/`conversation_turns` tables.
   - Target: Migration `0005_conversations.up.sql` creates `aura.conversations` (id, title, identity_id FK, created_at, last_active_at, status CHECK IN active/archived/deleted, model, total_input/output/cached_tokens, total_cost_usd numeric(10,4), metadata) + `aura.conversation_turns` (conversation_id FK ON DELETE CASCADE, seq, role CHECK IN system/user/assistant/tool, content, content_sidecar_path, tool_call_id, tool_calls jsonb, created_at, *_tokens, pk(conversation_id,seq)); same migration ALTERs `paused_states.conversation_id` to `uuid` + FK `conversations(id) ON DELETE CASCADE` and adds `resumed_answer`; CLI `aura chat {list[--archived]|new|resume [<id>]|archive|unarchive|delete <id> --confirm|rename}`.
   - Acceptance: Integration test creates a chat, writes 3 turns, restarts the process, `aura chat resume <id>` reconstructs the loop history and the assistant sees the full prior turns; content `> AURA_CONVERSATION_TURN_CAP_BYTES` (default 65536) spills to `$AURA_RUN_DIR/conversations/<id>/<seq>.content` with `content=NULL` + `content_sidecar_path` set.

8. **Resume contract byte-identical**: Rehydrating a conversation reproduces loop history deterministically.
   - Current: No persistence to rehydrate from.
   - Target: `LoadHistory(conv_id)` reconstructs `[]Message` from `conversation_turns ORDER BY seq` such that two consecutive calls return byte-identical slices (modulo `tool_calls` jsonb deserialization); per-turn write is a single atomic transaction (`BEGIN; INSERT turn; UPDATE conversations SET last_active_at, token aggregates, total_cost_usd; COMMIT`).
   - Acceptance: Test asserts `LoadHistory(id)` byte-equal across two calls; integration test confirms token + USD aggregates on `conversations` match the sum of turn-level token columns after a multi-turn run.

9. **Auto-title + per-conversation cost aggregation**: Conversations get an LLM-generated title and cumulative token/USD totals.
   - Current: No titles, no aggregation.
   - Target: After `seq >= 3`, a best-effort background LLM call generates a 4-6 word title via `UPDATE conversations SET title=:t WHERE id=:id AND title IS NULL` (idempotent, errors never block chat, NULL title renders as `(untitled <created_at>)`); `aura chat list` shows `id|title|created_at|last_active_at|turns|total_cost_usd`.
   - Acceptance: Test with a stub LLM client → 3-turn conversation → title set; LLM failure leaves title NULL without crashing the chat; `aura chat list` displays non-zero `total_cost_usd` aggregated from turns.

10. **Deterministic context management L1 + L2 + L2.5**: Three no-LLM-call context layers bound history growth.
    - Current: No context management; history grows unbounded in memory.
    - Target: **L1** microcompact — on `LoadHistory`, `role='tool'` turns older than `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` (default 10) have `content` replaced with a `read_tool_output(<id>)` sidecar pointer (no LLM call). **L2** dynamic budget — `hard_cap = ContextWindow - max(MaxOutputTokens,20000) - 13000`, `warn_cap = 0.75×hard_cap`; over warn → log WARN, over hard → explicit `Loop.Turn` error suggesting `aura chat new`. **L2.5** picobot hard rolling buffer (amendment #22) — after L1+L2, if still over 100% window, deterministically drop the oldest user/assistant **pair** (preserve system L0 + keep `len(history) % 2 == 0`) until it fits, writing an audit row to `aura.context_rot_events {ts, conv_id, action:'hard_drop_pairs', pairs_dropped, tokens_before, tokens_after}`. L3 LLM-driven compaction is explicitly NOT implemented.
    - Acceptance: `scripts/microcompact_smoke.sh` asserts a tool result is evicted after `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` turns with the sidecar still fetchable (L1); test drives history past the hard cap and asserts oldest-pair drop with a `context_rot_events` row written and `len(history)` even after the drop (L2.5).

11. **Loop.Stop auto-resolve pendings**: Ending a conversation cleans up orphan pauses.
    - Current: No lifecycle hook; pendings would orphan.
    - Target: When a loop terminates (`completed`/`errored`/`interrupted_by_user`), `Loop.Stop` runs `UPDATE aura.paused_states SET resumed_at=now(), resumed_answer='<auto-terminated: conversation ended>' WHERE conversation_id=:id AND resumed_at IS NULL`; CLI escape hatch `aura paused-states {list|purge --before <ISO> --confirm}` exists.
    - Acceptance: Integration test inserts a conversation + pending rows, calls `Loop.Stop`, and asserts zero rows remain with `resumed_at IS NULL` for that conversation; `aura paused-states list` shows the auto-resolved rows with the auto-terminated answer.

12. **$AURA_RUN_DIR cleanup cascade + boot orphan scan**: Filesystem sidecars track DB lifecycle.
    - Current: No run-dir lifecycle management for conversations.
    - Target: `aura chat delete <id>` runs `os.RemoveAll($AURA_RUN_DIR/conversations/<id>/)` after the DB DELETE commits (rm failure → WARN + Notifier, recovered at next boot scan); boot orphan scan removes `$AURA_RUN_DIR/conversations/*` dirs with no matching `conversations` row; boot sweeps `$AURA_RUN_DIR/tmp/*` older than 24h; boot WARNs (audit-only, no purge) if `du -sb $AURA_RUN_DIR > AURA_RUN_DIR_WARN_THRESHOLD_BYTES` (default 1 GiB).
    - Acceptance: Integration test creates a conversation with a sidecar dir, deletes the conversation, and asserts the dir is gone; a stray `conversations/<orphan>/` dir with no DB row is removed by the boot scan; cascade delete also purges `conversation_turns` + `paused_states` for that conversation.

13. **Conversation FTS via pg_trgm**: Full-text search across turn content with a matching CLI.
    - Current: No search index, no `aura chat search`.
    - Target: Migration `0006_conversation_turns_fts.up.sql` runs `CREATE EXTENSION IF NOT EXISTS pg_trgm` + `CREATE INDEX CONCURRENTLY conversation_turns_content_trgm ON aura.conversation_turns USING GIN (content gin_trgm_ops)` (down drops both, idempotent); sqlc `SearchConversationTurns(query,limit)` uses `content % $1 ORDER BY similarity(content,$1) DESC LIMIT $2`; CLI `aura chat search "<query>" [--conversation <id>] [--limit N]` prints `<conv_id>|<turn_seq>|<similarity>|<excerpt>`.
    - Acceptance: `aura chat search "specific phrase"` returns matching turn excerpts ordered by similarity; the same query string produces an identical result set when later wired to Telegram `/search` (cross-slice invariant, binding lands in Phase 13 but the query layer is locked here).

14. **Migration sequence locked**: Phase 4 migrations occupy `0003`–`0006` without collision.
    - Current: On-disk migrations stop at `0002_knowledge_migrations`.
    - Target: `0003_paused_states` (1.5), `0004_identity` (1.7), `0005_conversations` (1.8, includes the `paused_states` FK alter + `resumed_answer` + `context_rot_events` table), `0006_conversation_turns_fts` (1.8.5). The PRD's draft `0005` collision for FTS is resolved by renumbering FTS to `0006`.
    - Acceptance: `aura db migrate` applies `0003`→`0006` cleanly on a fresh DB; re-running is a no-op ("no pending migrations"); each `*.down.sql` reverses its `*.up.sql`; migration only succeeds as `aura_migrate`, denied as `aura_app`.

## Boundaries

**In scope:**
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

**Out of scope:**
- L3 full LLM-driven compaction (`chat_compact`) — deferred; 1M DeepSeek window + DB/Neo4j state make the cap rare (PRD §1.8 OQ#3)
- Internal pause timeout / `timed_out` state — caller owns wall-clock timeouts (locked boundary, PRD §1.5 OQ#3)
- Real multi-user auth / RBAC / login / OAuth — Slice 1.7 is identity *scaffolding* only (REQUIREMENTS Out of Scope)
- `capability_grants` audit table + glob patterns (`memory.*`) — premature for single-user wildcard; lands with multi-user (PRD §1.7 OQ#1/#3)
- LLM-facing tools for identity/conversation (`identity_grant`, `conversation_search`/`summarize`) — self-elevation risk; infra-only here (PRD §1.7/1.8 deferred-tool partitions)
- Telegram `/search` binding + Telegram `/cancel`/`/cost` — Phase 13 (only the FTS query layer is built here)
- Swarm `proxied_from_child_id` *propagation logic* — the columns exist in `0003` but nested-child proxy resolution is exercised in Phase 9 (Swarm)
- KV-cache stable-prefix evaluation of the `system` turn — Phase 6 (the system message is just persisted as seq=1 here)

## Constraints

- **Coverage floor ≥ 85%** across the full tag matrix (unit + `db_integration`), overriding the PRD's 75/60 (CLAUDE.md). Mutation ≥ 70% killed on critical files; `goleak` clean; `-race` clean; `golangci-lint` 0 issues; every file ≤ 600 LOC (split `llm_agent.go` into `llm_agent.go` + `_pause.go` + `_history.go` if it crosses 600).
- **No-skip-as-green**: `db_integration` tests must actually run in CI (env-gated skip-helpers `t.Fatal` under `$CI`); a sub-second integration runtime is a skip tell.
- `paused_states.conversation_id` is plain `text` in `0003` (no FK) and becomes `uuid` + FK only in `0005` — required for 1.5↔1.8 migration independence.
- All env vars follow `AURA_<DOMAIN>_<UNIT>`: `AURA_CONVERSATION_TURN_CAP_BYTES=65536`, `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS=10`, `AURA_HISTORY_HARD_CAP_TURNS=50`, `AURA_RUN_DIR_WARN_THRESHOLD_BYTES=1073741824`, plus `AURA_MODEL_CONTEXT_WINDOW`/`AURA_MODEL_MAX_OUTPUT_TOKENS` overrides.
- Token estimation uses `tiktoken-go` (cl100k_base) as a fast ~5-10% approximation for gating only — not billed accuracy.
- FTS index uses `CREATE INDEX CONCURRENTLY` (non-blocking) and must be idempotent on re-run.
- Postgres role separation enforced: migrations run only as `aura_migrate`; `aura_app` denied DDL/TRUNCATE.

## Acceptance Criteria

- [ ] Operator triggers `ask_user` during a tool call → loop pauses with a `PausedState` row in `aura.paused_states`; answering via CLI resumes the loop with the answer injected as `RoleTool`
- [ ] 3 simultaneous `ask_user` calls → 3 `PausedState` rows; resolving all 3 in FIFO order (`priority DESC, created_at ASC`) resumes the loop with all 3 answers
- [ ] Intra-turn exclusivity: `ask_user` batched with other tool calls → only `ask_user` dispatched, others dropped, loop paused
- [ ] `aura chat list` shows multiple persisted conversations with auto-generated titles + per-conversation cumulative token + USD totals
- [ ] `aura chat resume <id>` after process restart reconstructs full history; `LoadHistory(id)` byte-identical across two calls
- [ ] `aura chat search "specific phrase"` returns matching turn excerpts from the `pg_trgm` GIN index
- [ ] Fresh boot: `aura.capability_grants` has one `(local, '*')` row and `HasCapability("local","any_tool")` returns `true`
- [ ] `aura identity grant local '*'` and `revoke local '*'` both rejected (system-managed wildcard)
- [ ] L1: tool result evicted to sidecar pointer after `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` turns, still fetchable via `read_tool_output`
- [ ] L2.5: history past hard cap drops oldest user/assistant pair, writes a `context_rot_events` row, leaves `len(history)` even
- [ ] `Loop.Stop` auto-resolves all open pendings for the conversation (zero `resumed_at IS NULL` rows afterward)
- [ ] `aura chat delete <id> --confirm` cascade-deletes turns + paused_states + removes `$AURA_RUN_DIR/conversations/<id>/`
- [ ] No internal timer transitions any `PausedState` to a timed-out state (grep confirms no `timed_out` status)
- [ ] `aura db migrate` applies `0003`→`0006` cleanly; re-run is a no-op; denied as `aura_app`, succeeds as `aura_migrate`

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.92  | 0.75 | ✓      | 4 sub-slices, concrete deliverable each, set confirmed       |
| Boundary Clarity   | 0.95  | 0.70 | ✓      | Scope / context-depth / pause-timeout all locked in round 1  |
| Constraint Clarity | 0.82  | 0.65 | ✓      | L2.5 + context_rot_events in; migrations 0003-0006 locked    |
| Acceptance Criteria| 0.90  | 0.70 | ✓      | 14 pass/fail criteria from per-slice PRD checklists          |
| **Ambiguity**      | 0.10  | ≤0.20| ✓      | PRD truth-source pre-closed all sub-slice open questions      |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective     | Question summary                          | Decision locked                                                      |
|-------|-----------------|-------------------------------------------|---------------------------------------------------------------------|
| 0     | Researcher      | What exists today vs phase target?        | In-memory-only agent; migrations stop at 0002; no ask_user/identity/conversations packages |
| 1     | Boundary Keeper | All four sub-slices in one phase?         | All four (1.5+1.7+1.8+1.8.5) land together as ROADMAP                |
| 1     | Boundary Keeper | Context management depth (L1/L2/L2.5)?     | L1 + L2 + L2.5 (picobot hard buffer + context_rot_events); L3 deferred |
| 1     | Boundary Keeper | Internal pause timeout?                    | No internal timeout — caller owns wall-clock; no timed_out state     |

---

*Phase: 04-hitl-identity-conversations*
*Spec created: 2026-05-30*
*Next step: /gsd:discuss-phase 4 — implementation decisions (how to build what's specified above)*
