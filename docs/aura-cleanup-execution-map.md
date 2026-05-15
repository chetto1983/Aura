# Aura Cleanup Execution Map — North-Star Before Any New Feature

**Date:** 2026-05-15
**Source of truth:** `D:/Aura/prd.md` +
`D:/Aura/.planning/deep-refactor/INDEX.md` + current handoff files
**Trigger to add new features (WhatsApp, etc.):** ALL of the items in this map at status `done`.

**Authority note:** this map is an orientation layer, not the active contract.
If it conflicts with `D:/Aura/prd.md`,
`D:/Aura/.planning/deep-refactor/INDEX.md`, `D:/Aura/CONTINUE-HERE.md`,
`D:/Aura/.planning/HANDOFF.json`, or a phase `progress.md`, the durable PRD and
handoff files win. Current resume state on 2026-05-15: Phase01A is implemented
and Go-verified, Phase01B1 and Phase01B2 are implemented and verified locally,
and the next bounded implementation slice is Phase01B3 dashboard bearer actor
context.

---

## The One-Sentence Goal (PRD §14)

> Aura becomes maintainable when every path enters through a channel or scheduled entrypoint, flows through `chat`, uses one `agent` runtime, accesses capabilities through `agent/tools`, learns from recoverable tool failures through `learning`, persists through `memory` and `storage`, and returns through an adapter without the core ever learning about the adapter's world.

This is the binary success metric: every code path traceable in that exact shape, no exceptions.

---

## Target Module Map (PRD §4)

```
internal/
  agent/          ✓ exists       core loop, runtime, governance, run state
  chat/           ✓ exists       normalized messages, runs, routing, events
  identity/       ✓ NEW (P01B1)  principals, channel accounts, actors, grants
  channels/       ✓ exists       telegram, web, silent, swarm adapters
  agent/tools/    ✓ exists       tool registry, schemas, execution, toolsets
  workflow/       ✗ NEW (P6)     durable tool execution, retries, idempotency
  learning/       ✗ NEW (P6)     tool experience, self-healing, promotion
  rag/            ✗ NEW (P7)     schema-aware retrieval, hybrid search, rerank
  cache/          ✗ NEW (later)  disposable acceleration, separate from storage
  memory/         ✗ NEW (P9)     wiki semantics, recall, memory policy
  storage/        ✓ exists       sqlite, qdrant, artifacts, sources, indexes
  cron/           ✓ exists       scheduled entrypoint
  api/            ✓ exists       HTTP, dashboard, auth middleware
  config/         ✓ exists       loading + runtime settings
```

**5 packages still missing**: `workflow`, `learning`, `rag`, `cache`, `memory`. Their absence is the structural debt that the 9-phase plan resolves.

---

## Phase Status Snapshot (2026-05-15)

| Phase | Status | Evidence |
|---|---|---|
| Phase 1 - Stabilize Map | in progress | Phase01 parent exists; continue through bounded sub-phases instead of broad map cleanup. |
| Phase 1A - Run/Event Foundation | local verified | migration v6 adds `runs`, `run_events`, `run_outbox`, idempotency, and audit tables; `internal/storage/runs` and `internal/chat` Hub metadata persistence are verified in `Phase01A_Run_Event_Foundation/benchmark.md`. |
| Phase 1B1 - Identity foundation | closed verified | migration v7 plus `internal/identity`; local gates and subagent verification passed. |
| Phase 1B2 - Allowlist backfill | local verified | migration v8 backfills persisted `allowed_users` into Telegram principals, channel accounts, session actors, and grants; auth bootstrap/approval creates matching identity rows. |
| Phase 1B3 - Dashboard bearer actor context | next slice | Resolve dashboard bearer tokens into actor context and prevent authenticated user override. |
| Phase 1C - Question Gate | scaffolded only | `chat_questions` table, question_requested/answered events, QuestionGate logic. |
| Phase 2 — Protect Telegram | ⏳ scaffolded | record-replay fixture for byte-comparable parity |
| Phase 3 — Move Channels Behind Chat | ⏳ scaffolded | route Telegram + web fully through Hub |
| Phase 4 — Collapse Agent Runtime | ⏳ scaffolded | merge agentloop+agentruntime, extract governance + prompt assembly |
| Phase 5 — Consolidate Tools | ⏳ scaffolded | tools as agent capabilities, visibility tiers, capability annotations |
| Phase 6 — Tool Experience Loop | ⏳ scaffolded | ToolObservation, internal/workflow + internal/learning (NEW pkgs) |
| Phase 7 — Rebuild RAG Typed Memory | ⏳ scaffolded | internal/rag (NEW pkg), typed layers, hybrid FTS+vector w/ RRF |
| Phase 8 — Cron + Swarm RunGraph | ⏳ scaffolded | durable schedule fires, parent/child run IDs, policy-driven graphs |
| Phase 9 — Memory & Source Discipline | ⏳ scaffolded | internal/memory (NEW pkg), wiki as projection, sources immutable |

**Working item right now:** Phase01B3 dashboard bearer actor context. Ralph
queue files may exist, but they are not the active deep-refactor route unless a
current turn explicitly selects the Ralph queue.

---

## Critical Path (Execution Order)

The order matters because of dependencies. Each phase has prerequisites that gate the next.

```
[Now] Phase01B3 - dashboard bearer actor context
   ↓
[1] Phase01B4-B7 - tool capability, Telegram/API actor wiring, cron/swarm delegation, denial events
   ↓
[2] Phase01C - Question Gate (3-4 slices, real product work)
   ↓
[3] Phase 2 — Protect Telegram (record-replay fixture, ~3 slices)
   ↓
[4] Phase 3 — Move Channels Behind Chat (~4 slices)
   ↓
[5] Phase 4 — Collapse Agent Runtime (~5 slices)
   |    │
   |    ├─► improve-codebase-architecture skill — invoke ONCE before locking interfaces
   |    └─► critical decision: agentloop+agentruntime+agent unification shape
   ↓
[6] Phase 5 — Consolidate Tools (~5 slices)
   |    │
   |    ├─► improve-codebase-architecture skill — invoke for tool registry/index/sets/swarmtools merge
   |    └─► first time we introduce visibility tiers + capability annotations
   ↓
[7] Phase 6 — Tool Experience Loop (~6 slices)
   |    │
   |    ├─► CREATE internal/workflow/ — durable tool execution
   |    ├─► CREATE internal/learning/ — tool experience, lessons, promotion
   |    └─► ToolObservation as single result/error contract
   ↓
[8] Phase 7 — Rebuild RAG (~7 slices)
   |    │
   |    ├─► CREATE internal/rag/ — schema-aware retrieval
   |    ├─► improve-codebase-architecture skill — invoke for RAG interface shape
   |    └─► GraphRAG patterns from PRD §5.9 (NO Neo4j)
   ↓
[9] Phase 8 — Cron + Swarm RunGraph (~5 slices)
   ↓
[10] Phase 9 — Memory & Source Discipline (~5 slices)
   |    │
   |    ├─► CREATE internal/memory/ — wiki semantics, recall, policy
   |    └─► CREATE internal/cache/ — separate from storage
   ↓
[FINISH] All §9 dependency rules holding under automated lint
   ↓
[NEW FEATURES] WhatsApp + production rollout + 2026 patterns
```

Estimated total work: ~45 slices × ~4-15 min each (Ralph speed) = 4-12 hours of focused execution time spread over multiple sessions.

---

## Per-Phase Quick Reference

### Phase01A - Run/Event Foundation
- **Status:** local verified.
- **Scope already covered:** `runs`, `run_events`, `run_outbox`,
  idempotency, audit tables, `internal/storage/runs`, and `chat.Hub`
  metadata persistence.
- **Gate evidence:** `Phase01A_Run_Event_Foundation/benchmark.md`.

### Phase01B2 - Allowlist Backfill
- **Status:** local verified.
- **Scope already covered:** persisted Telegram `allowed_users` rows are
  backfilled into principals, channel accounts, session actors, and owner/user
  grants while preserving `allowed_users`.
- **Out-of-scope still open:** dashboard actor session, tool capability checks,
  cron delegation, swarm delegation, and denial-event persistence.
- **Next slice:** Phase01B3 dashboard bearer actor context.

### Phase01B3 - Dashboard Bearer Actor Context
- **Scope:** resolve dashboard bearer tokens into actor context and prevent
  authenticated user override.
- **Canonical folder:**
  `D:/Aura/.planning/deep-refactor/Phase01/subphases/Phase01B_Identity_Capability_Grants`.
- **Do not bundle:** tool capability checks, cron delegation, swarm delegation,
  or denial-event wiring.

### Phase01C — Question Gate
- **Scope:** durable `chat_questions` table; `question_requested`/`answered`/`approval_*` events; `QuestionGate` before risky tool execution + durable memory writes
- **Key decision:** `request_input` is a NARROW structured action, NOT a broad always-loaded tool (PRD §5.2)
- **Anti-pattern to avoid:** `ask_user` as always-loaded escape hatch
- **Gate:** clear instruction does NOT produce needless question; missing required slots produce ONE scoped question; risky tool calls produce approval (not generic clarification)

### Phase 2 — Protect Telegram
- **Scope:** record-and-replay fixture for Telegram BEFORE moving any outbound behavior
- **Why first:** Telegram has subtle product behavior (progressive edits, CoT markers, entity rendering) that's hard to test post-move without a fixture
- **Gate:** fixture exists, covers simple reply + CoT + tool/entity table; later adapter output byte-comparable

### Phase 3 — Move Channels Behind Chat
- **Scope:** port Telegram outbound into `channels/telegram` (most of it already done); route web chat through `chat` behind conservative flag; later Telegram through Hub behind flag
- **Gate:** fixture diff zero; `/api/chat` JSON shape unchanged

### Phase 4 — Collapse Agent Runtime ⚠️ CRITICAL
- **Scope:** extract governance; extract prompt/context assembly into versioned bundle; merge `agentloop` + `agentruntime` into `agent`; remove compat wrappers when production refs are gone
- **Where Ousterhout matters most:** decide the agent runtime INTERFACE before moving code
- **Skill invocation:** `improve-codebase-architecture` on `internal/agent` + `internal/agentloop` + `internal/agentruntime` to surface design alternatives (minimalist, flexible, caller-optimized, ports&adapters)
- **Gate:** no duplicate loop body; prompt bundle snapshots deterministic; prompt evals cover context utilization + tool triggering + question behavior + output contract

### Phase 5 — Consolidate Tools
- **Scope:** move registry/toolindex/toolsets/swarmtools under `agent/tools`; assign visibility tiers (always_loaded / deferred_discoverable / programmatic_callable / blocked_from_programmatic / runtime_gated); assign required capability + retry/idempotency/risk class
- **Open opportunity:** apply `mcpdesc.Compose()` pattern from cli-printing-press to uniform tool descriptions (see memory `cli-printing-press-eval-2026-05-15`)
- **Skill invocation:** `improve-codebase-architecture` for the merge shape
- **Gate:** deterministic tool list; deterministic pool order; discovery top-k evals pass; parameter accuracy evals for example-backed tools; programmatic internal calls bounded/redacted/audited

### Phase 6 — Tool Experience Loop ⚠️ NEW PACKAGES
- **Scope:** `ToolObservation` contract (ok / recoverable / blocked / fatal / cancelled); Tool Supervisor (retry, redaction, idempotency, budgets); route risky long-running stateful tools through DURABLE WORKFLOW; learning events for tool attempts; lesson retrieval; promotion only after validation
- **NEW packages:** `internal/workflow/` (durable execution) + `internal/learning/` (tool experience, lessons)
- **Gate:** recoverable tool error correctable in same run; workflow-backed tool steps survive process restart and don't double-apply; `side_effect_unknown` never blindly retried; secrets redacted from learning records

### Phase 7 — Rebuild RAG Typed Memory ⚠️ NEW PACKAGE
- **Scope:** memory layer IDs + citation handles; collection metadata registry (wiki / sources / user_memory / archive / operational); split recall by task intent (`recall_user` / `recall_knowledge` / `recall_operational`); hybrid FTS/vector with RRF; projection freshness registry; per-slug wiki upsert + full rebuild separately; GraphIndex with typed weighted edges; LOCAL GraphRAG (no graph DB)
- **NEW package:** `internal/rag/`
- **Skill invocation:** `improve-codebase-architecture` for retrieval interface shape
- **Gate:** user facts NOT in wiki unless promoted; source hits cite source/page/span; wiki hits cite [[slug]]; retrieval fixtures prove hybrid > vector-only > keyword-only on golden set; stale/degraded projections visible to agent and dashboard

### Phase 8 — Cron + Swarm RunGraph
- **Scope:** cron emits durable schedule-fires with idempotency keys; swarm uses parent/child run IDs; policy-driven RunGraph; team_collaboration; mailbox/task board persisted/replayable
- **Out of scope as PERMANENT limit:** read-only-only workers, max_spawn_depth=1, fixed roles — these may be initial slices but NOT architectural ceilings
- **Gate:** parent run ID propagation; child actor grants ⊆ parent authority; task claim race-safe under concurrent attempts; teammate-to-teammate messages addressed/scoped/audited; missed/coalesced/skipped/retried cron fires observable

### Phase 9 — Memory & Source Discipline ⚠️ NEW PACKAGES
- **Scope:** clarify `memory` vs `storage`; wiki as readable projection; sources immutable; indexes rebuildable; SQLite WAL/busy-timeout/retry hardened
- **NEW packages:** `internal/memory/` (wiki semantics + recall + policy) + `internal/cache/` (acceleration, separate from storage)
- **Gate:** wiki output inspected; source conversion fixtures use must-include/must-not-include; SQLite concurrency tested under pressure

---

## Cross-Cutting Concerns (Apply EVERY Phase)

### Dependency Rules (PRD §9) — the lint
**Allowed (frecce architettoniche):**
- channels/* → chat
- chat → agent
- agent → agent/tools
- agent → {learning, rag, memory} via interface
- rag → {memory, learning, storage} via interface
- {learning, memory} → storage
- api → chat
- cron → chat or agent

**Forbidden (lint deve fallire):**
- agent → channels/* / api / telegram
- agent → concrete qdrant/sqlite/source parser details
- memory → channels/*
- storage → agent
- rag → channels/*
- rag writing memory directly
- tools → chat (unless explicitly chat-facing + reviewed)
- learning → channels/*
- learning auto-editing prompts/code/skills without validation
- cron/swarm owning a separate loop

**Implementation:** dedicated slice during or after Phase 5 — Go-based importgraph check OR `go-arch-lint` adoption. Configurable via YAML mapping the allowed/forbidden edges.

### CI Health (parallel work)
- Frontend `npm --prefix web ci` syntax fix
- Delete obsolete `test-tool-search-removal.ps1` guard (contradicts current PRD §5.5)
- See memory `ci-broken-2026-05-15` for full root cause

### Memory Policy (apply during Phase 7-9)
- Wiki = readable curated; NOT chat logs / tool failures / scratchpad
- User memory writes require explicit intent OR `memory.user.write` capability
- Operational memory = Aura's lessons, NOT user wiki pollution
- Source corpus = immutable until curated

---

## Slice Quality Bar (Definition of Done) — PRD §11

A slice is DONE only when:
- module responsibility clearer than before
- dependencies move toward allowed direction
- no god package created
- old behavior protected by tests or fixtures
- build/vet/test green
- public behavior unchanged unless PRD explicitly says otherwise
- sources, examples, rejected alternatives recorded for research-driven decisions
- prd.json + progress notes updated only AFTER verification
- commit atomic + named for the architectural change

A slice is NOT done when:
- files merely moved
- tests only prove compilation
- behavior "probably" preserved
- package got better name but still owns too many concerns
- temporary compat wrapper becomes new permanent architecture

---

## Skills to Invoke Per Phase (Recommendation)

| Phase | Skill | Why |
|---|---|---|
| Phase 4 (Collapse Runtime) | `improve-codebase-architecture` | The merge shape decision is irreversible; surface 3-4 design alternatives BEFORE moving code |
| Phase 5 (Consolidate Tools) | `improve-codebase-architecture` | Tool registry/index/sets/swarmtools merge shape; visibility tier interface design |
| Phase 5 closing | mcpdesc.Compose() pattern port | From cli-printing-press eval; uniform tool descriptions ~250 LOC |
| Phase 7 (Rebuild RAG) | `improve-codebase-architecture` | RAG interface shape (recall_user / recall_knowledge / recall_operational); GraphRAG seams |
| Whenever stuck | `zoom-out` | Re-establish system view when phase details consume focus |
| End of each Phase | `to-issues` | Materialize next-phase slices into prd.json (alternative to hand-authoring) |

**Always-on (load into AGENTS.md):**
- `agent-rules-books` Clean Architecture **mini** (~45 LOC of rules)
- `agent-rules-books` Refactoring **nano** (~30 LOC of rules)

These compress Robert C. Martin + Martin Fowler discipline into the agent's session context. Combined with PRD §9 dependency rules, they form the "architectural conscience" that every Ralph iter inherits.

---

## Anti-Patterns to Avoid (lessons from prior arcs)

1. **Bundling sub-phases into one mega-slice** — Phase 1B1 + 1B2 + 1C would have been a disaster; the deliberate split is the discipline
2. **Files merely moved without responsibility clarification** — see PRD §11 "not done" list
3. **Adding compat wrappers as permanent architecture** — strangler is for the move, not the destination
4. **Renaming without teaching** — every rename must answer the "what kind of code is this?" test (PRD §6)
5. **Skipping the fixture before risky moves** — Phase 2 exists because Telegram has subtle behavior that's irrecoverable post-move without a baseline
6. **Trusting CI as a green/red signal while it's broken** — fix CI before relying on it (see memory `ci-broken-2026-05-15`)

---

## "WhatsApp" — Where It Slots

After ALL phases above are status `done`:
- New channel package: `internal/channels/whatsapp/`
- Implements `chat.InboundAdapter` (already a stable contract by then)
- Decisions to make: WhatsApp Business API vs whatsapp-web.js-like wrapper, auth/account model
- Roughly 5-7 slices for a working channel
- This is intentionally THE LAST thing because it tests whether the cleanup actually delivered the "add a new chat app = one channel package, no agent changes" promise

If WhatsApp requires ANY edit to `internal/agent/`, `internal/chat/`, `internal/agent/tools/`, OR `internal/memory/` — the cleanup did not succeed and the architecture is still leaky.

---

## Tracking This Map

This is a planning artifact, NOT a contract. The real contracts are:
- `D:/Aura/prd.md` (north-star PRD)
- `D:/Aura/.planning/aura-deep-refactor-decisions.json` (ADR route)
- `D:/Aura/.planning/deep-refactor/INDEX.md` (phase folder map)
- `D:/Aura/CONTINUE-HERE.md` and `D:/Aura/.planning/HANDOFF.json` (resume
  pointer)
- `D:/Aura/scripts/ralph/prd.json` only when the current turn explicitly
  selects the Ralph queue

Update this map only when the underlying contracts change. Otherwise, use it for orientation, not authority.
