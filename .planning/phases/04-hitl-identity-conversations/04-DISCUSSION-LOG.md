# Phase 4: HITL + Identity + Conversations - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-30
**Phase:** 04-hitl-identity-conversations
**Areas discussed:** Pause/resume loop architecture, Persistence layer shape (Store pattern), CLI surface structure, Commit decomposition + execution mode, + 4 additional locked forks

**Discussion character:** Research-heavy. The user repeatedly redirected from "pick an option" to "deep-search the industrial pattern" — first online (LangGraph/ADK/durable-exec), then the curated D:/tmp sources (adk-go-study, picobot), then Anthropic papers + MCP elicitation. Decisions were locked only after triangulation across ≥2 independent sources.

---

## Pause/resume loop architecture (Area 1)

| Option | Description | Selected |
|--------|-------------|----------|
| New orchestrator wraps LlmAgent | LlmAgent stays pure leaf; new type owns conversation_id + Store + pause-state | ✓ (after research) |
| Evolve LlmAgent into the persistent loop | LlmAgent gains a Store dependency + LoadHistory + pause interception | |

**User's choice:** Directed "deep search industrial pattern and discuss" → "look pattern on d:/tmp" → "search also in other repo in d:/tmp". Research (ADK-Go `runner.go`/`session.Service`/`request_confirmation_processor`, picobot `loop.go`/`session`, LangGraph interrupt+checkpointer, Anthropic, durable-exec) converged on **orchestrator + pluggable persistence + leaf agent**. Locked the Runner pattern with ADK naming.

**Notes:** Sub-decisions locked across several follow-ups: Runner distinct from existing `workflow.LoopAgent` (naming collision confirmed in ADK-Go too); pause surfaces as a new `Actions.AwaitingInput` Event (not the iter.Seq2 error slot — consistent with Phase 3 D-04/D-15); persist-by-observing-the-Event-stream; **dropped ADK's flow-processor indirection** (Aura's single loop ≠ ADK's processor pipeline) — pause detection in the agent dispatch, orchestration in the Runner; resume = fresh `Run` over rehydrated history (can't suspend a range-over-func); reworked verb surface (single driver `Turn`, pure-persistence `SubmitAnswer(s)`, `Stop`); intra-turn assistant-message rewrite for OpenAI wire-correctness; swarm forward-compat. Then "search antropic paper" → 4 hardened criteria SC-1..SC-4.

---

## Persistence layer shape — Store pattern (Area 2)

| Option | Description | Selected |
|--------|-------------|----------|
| Consumer-side interfaces (Runner defines them) | Idiomatic "accept interfaces, return structs"; fakes in unit tests | ✓ |
| Producer-side interfaces (each package exports) | More explicit but less idiomatic, fat-interface risk | |
| Shared db.WithTx helper, Stores wrap it | One reusable Begin/Commit/Rollback for atomic per-turn writes | ✓ |
| Each Store manages its own tx inline | Duplicated tx logic, SC-2 failure-mode risk | |

**User's choice:** Consumer-side interfaces + shared `db.WithTx` helper.
**Notes:** sqlc already locks ONE generated package (sqlc.yaml); Phase 1 `knowledge_migrations` established the surface. Per-domain Stores (SPEC names the 3 packages). Query files per table. L1/L2/L2.5 in conversations/context.go with cached tiktoken-go (captured as defaults).

---

## CLI surface structure (Area 3)

| Option | Description | Selected |
|--------|-------------|----------|
| Bare `aura chat` = New persisted conversation REPL | `new` implicit; `resume` (no id) = most-recent; Claude.ai ergonomics | ✓ |
| Bare `aura chat` = Resume most-recent, else new | Continuity-first; surprising for fresh threads | |
| Bare `aura chat` = print help / require subcommand | Most explicit, least ergonomic | |
| HITL render: Kind-specific prompts | text / [y/N] default No / numbered pick | ✓ (enriched) |
| HITL render: Uniform free-text | simpler, loses [y/N] safety + numbered pick | |

**User's choice:** Bare chat = new persisted REPL. For HITL rendering, directed "look antropic design" → researched MCP elicitation + Claude Code permission UX → adopted the **full MCP three-action model (accept/decline/cancel)** on top of kind-specific prompts + no-secrets guardrail.
**Notes:** REPL refactored to drive the Runner. The three-action model records a small SPEC enrichment (AM-02: resumed_answer gains an action).

---

## Commit decomposition + execution mode (Area 4)

| Option | Description | Selected |
|--------|-------------|----------|
| 1.7 identity FIRST to derisk the Store pattern | Simplest independent slice proves the pattern before 1.5/1.8 copy it | ✓ |
| Strict PRD dependency order (1.5→1.7→1.8→1.8.5) | Matches migration numbering literally | |
| GSD path: plan-phase → execute-phase (gsd-executor) | Full GSD gates end-to-end | ✓ |
| Hybrid: GSD plans, Codex parallel-session implements | Faster on big slice, mixes patterns | |
| Decide exec mode at plan time | Defer until PLAN.md surface concrete | |

**User's choice:** 1.7 identity first; full GSD path (plan-phase → execute-phase).
**Notes:** PRD-amendment commit first; atomic Gate-2-green commits; ≤600 LOC splits.

---

## Additional locked forks (researched)

| Fork | Decision | Selected |
|--------|-------------|----------|
| Auto-title goroutine | Lifecycle-bound `context.WithoutCancel`+timeout, WaitGroup-tracked, goleak sync point | ✓ |
| Boot orphan-scan | After db.Open, O_NOFOLLOW/Lstat symlink guard, audit-only size WARN | ✓ |
| FTS excerpt | SQL query layer = locked cross-slice contract; excerpt app-side windowed | ✓ |
| OTel spans | conversation.turn parents llm.request + persist-turn tx span; no per-query spans | ✓ |

**User's choice:** Directed "deep search industrial patterns on d:/tmp and online" → researched Go graceful-shutdown/`WithoutCancel` (picobot + web), pg_trgm docs (no ts_headline), golang-observability. Locked all four.

---

## Claude's Discretion

- FTS excerpt window size (~60 chars) + first-N fallback length.
- Auto-title `WithTimeout` value + `Runner.Stop` drain timeout.
- `identity.sql`/`capability_grants.sql` one file or two.
- Span names beyond the four pinned.
- `db.WithTx` path within `internal/db`.
- L1/L2/L2.5 placement (conversations/context.go) + tiktoken-go encoder caching — defaulted.

## Deferred Ideas

- L3 LLM-driven compaction (`chat_compact`) → future.
- Swarm `proxied_from_child_id` propagation logic → Phase 9 (columns created in 0003).
- Telegram `/search` + `/cancel` + `/cost` bindings → Phase 13 (FTS query layer built here).
- `capability_grants` audit table + glob patterns → multi-user milestone.
- LLM-facing identity/conversation tools → deferred (self-elevation risk).
- KV-cache stable-prefix evaluation of the system turn → Phase 6.
- URL-mode elicitation → not needed (form-mode only + no-secrets guardrail).
