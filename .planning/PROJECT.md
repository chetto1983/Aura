# Aura

## What This Is

Aura is a self-hosted AI agent platform in Go for a single operator and, increasingly, small
teams. She holds a real conversation across channels (web cockpit, Telegram, WhatsApp), runs
tools against a sandboxed workspace and the host, keeps bitemporal long-term memory in a
per-identity graph, indexes and searches the operator's documents, and extends herself by
writing her own skills and MCP servers.

The truth-source for architecture and constraints is [`prd.md`](../prd.md); this file is the
planning projection of it.

## Core Value

**When Aura says she did something, she did it — and she can find what she knew.** Every other
capability is negotiable; a harness that reports work it did not perform, or forgets what it
was told, makes the rest worthless.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- ✓ Postgres control plane (`aura.*`), sqlc + golang-migrate — v0.0.0
- ✓ Agent runtime: `Agent` interface, workflow agents, budget + loop guard — v0.0.0
- ✓ Real LLM client with ToolResult preview/persist + sidecar spillover — v0.0.0
- ✓ `ask_user` loop pause/resume primitive — v0.0.0
- ✓ Multi-thread conversation persistence with token/USD aggregation — v0.0.0
- ✓ Sandbox runner (Docker sidecar, seccomp, ulimit) — v0.0.0
- ✓ Swarm coordinator (ParallelAgent, depth cap) — v0.0.0
- ✓ KV-cache stable-prefix builder — v0.0.0
- ✓ Deferred-tool pattern + `tool_search` (BM25, 96% top-1 @17µs) — v0.0.0
- ✓ Skills self-extension (`skill` tool: create/update/install/snippets, `always:true`) — v0.0.0
- ✓ Deep Search Cockpit: Vite + React over AG-UI/SSE — v1.0.0
- ✓ Telegram channel with approval relay — v1.0.0
- ✓ Document pipeline: ingest, chunk, embed, `document_search`/`document_open` — v1.0.0
- ✓ ArcadeDB long-term memory, one database per identity, server-enforced tenancy — v2.0.0
- ✓ ToolGateway with risk tiers and a destructive-action approval gate — v2.0.0
- ✓ Per-user sandbox + Authula multi-user auth — v2.0.0
- ✓ Scheduler (`task`): reminders, agent jobs, Postgres backup — v2.0.0
- ✓ MCP sidecars: `arcadedb-mcp`, `aura-pim-mcp` (calendar/mail), WhatsApp bridge — v2.0.0
- ✓ Deterministic L1/L2/L2.5 context ladder with rot-event audit — v2.0.0

### Active

<!-- Milestone v2.1.0 HERMES-CLAUDE_PARITY. See REQUIREMENTS.md for REQ-IDs. -->

- [ ] The harness never returns a result the tool did not produce this call
- [ ] A memory correction closes exactly the fact it names
- [ ] The model-facing tool surface fits in working memory (~26 tools, ceremony removed)
- [ ] MCP servers are trusted; their descriptions are not wrapped as hostile data
- [ ] Context compresses under pressure instead of forgetting
- [ ] The agent can see what it already has — loaded tools, injected memory, pruned skills

### Out of Scope

<!-- Explicit boundaries with reasoning, to prevent re-adding. -->

- OpenClaw Node sidecar plugin host — in-process Go seams + MCP cover it; the sidecar is only
  justified if running OpenClaw binaries unmodified becomes a hard requirement
- Migrating the graph off ArcadeDB (PuppyGraph / TuringDB / Apache AGE evaluated) — none is a
  drop-in replacement
- Telegram Mini App for approvals — rejected; shared resolve + native Telegram UI instead
- `make_document` routing tool — the friction it was meant to fix was the F-1 replay bug, not a
  missing router; fixing the harness removes the need
- `remind_me` / `remember` as new wrapper tools — delivered instead by flattening `task` and
  `memory_recall` in place, so there is one obvious way to do it
- Onboarding interview — removed 2026-07-26; profile facts come from use

## Context

- **Scale**: ~98k LOC non-test across 68 packages (~7k sqlc-generated), ~143k LOC of tests.
- **History**: v0.0.0 phases 0-21, v1.0.0 phases 22-30, v2.0.0 phases 31-44. v2.0.0 was never
  tagged; `.planning/` was purged at its close and partially regenerated (`codebase/`, `intel/`,
  `handoffs/`). This milestone continues phase numbering at **45**.
- **What triggered this milestone**: two live debugging sessions on 2026-08-04 (234 + 10 turns,
  2.76M input tokens) were exported from `aura.conversations` and audited. Three documents in
  [`docs/audit/live-conversations-2026-08-04/`](../docs/audit/live-conversations-2026-08-04/)
  carry the findings with file:line evidence — `FINDINGS.md` (F-1..F-10),
  `TOOL-SIMPLIFICATION.md`, `CONTEXT-MANAGEMENT.md` (C-1..C-8).
- **The two headline defects**: an identical mutating tool call inside one user request is
  *replayed* from the operation registry instead of re-executed, so the model received a stale
  `shell_exec` traceback and a stale `memory_forget` count — and wrote the resulting
  misdiagnosis into long-term memory as fact. Separately, `supersedes:true` on the multi-valued
  `learned_lesson` predicate closed **8** facts instead of 1, and the repair lost the original
  statements and provenance.
- **The quieter lesson**: two of Aura's own three top-priority requests turned out to be already
  implemented — memory *is* injected every turn, and a deferred tool dropped from the manifest
  *is* still callable. She could not tell, and built tooling to solve problems she did not have.
  A material share of this milestone is making existing behaviour legible.
- **Reference implementations on disk**: `D:/tmp/hermes-agent` (80 model-facing tools, an ~11k
  LOC pluggable context engine with LLM summarization) and
  `D:/tmp/system-prompts-and-models-of-ai-tools` (30+ vendor agent prompts).

## Constraints

- **Tech stack**: Go 1.26, Postgres 18 + sqlc + golang-migrate, ArcadeDB ≥ 26.4.2, Docker
  Compose — established across 68 packages; no rewrites.
- **Quality gates**: coverage floor 85% on the owned surface, mutation ≥70% on each phase's
  critical files, `golangci-lint` clean, `govulncheck` clean. Enforced in CI, runnable via
  `make quality-full`.
- **File size**: no non-test Go file over 600 LOC — refactor on touch.
- **KV cache**: `messages[0]` must stay byte-identical across turns; the always-block sits at
  `messages[1]`. Any context change must preserve this.
- **Hardware**: single mini-PC, 16 shared cores, RTX A2000 4GB. Embedding and rerank both run
  locally; the LLM is remote (OpenRouter) by default.
- **Migrations**: the next number is `ls internal/db/migrations/ | tail -1` + 1 at landing time,
  never deduced from a slice number.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Deferred-tool pattern for the manifest | Keeps the per-turn manifest small; scales past 50 tools without cache bloat | ⚠️ Revisit — v2.1.0 un-defers the frequent set; the pattern stays for the long tail |
| Trust MCP sidecar descriptions | Every mounted server is ours or operator-installed; the "untrusted data" wrapper costs tokens and reads as noise | — Pending (v2.1.0) |
| At-most-once reservation on mutating tools | Prevents a retried dispatch from repeating a side effect | ⚠️ Revisit — the key omits `tool_call_id`, so a deliberate re-run is misread as a retry (F-1) |
| Deterministic context ladder, no LLM | Pure, testable offline, cache-stable | ⚠️ Revisit — v2.1.0 adds an LLM summarization rung; the deterministic rungs stay |
| ArcadeDB as long-term memory, one DB per identity | Server-enforced tenancy beats a WHERE clause someone forgets | ✓ Good |
| Bitemporal facts with `supersedes` | Keeps "what did I believe last month" answerable | ⚠️ Revisit — blunt on multi-valued predicates (F-2) |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

## Current Milestone: v2.1.0 HERMES-CLAUDE_PARITY

**Goal:** Bring the agent harness to parity with hermes-agent and Claude Code — a tool surface
the model can hold in its head, a context ladder that compresses instead of forgetting, and a
harness that never reports a result it did not produce.

**Target features:**
- Harness correctness: no stale replay of a re-issued call; a memory correction closes exactly
  the fact it names
- Tool surface: un-defer the frequent set, strip ceremony parameters, merge overlapping tools,
  flatten `task`/`memory_recall`/`skill`/`web_search` — ~56 model-facing tools down to ~26
- MCP trust: drop the untrusted-server description wrapper
- Context: evict superseded `tool_search` results, budget on real `prompt_tokens`, ghost-skill
  markers, per-category breakdown, and an LLM summarization rung with hermes' anti-thrash,
  cooldown and fallback machinery
- Third-party facade: `calendar__*` + `whatsapp__*` (28 tools) behind a curated surface

---
*Last updated: 2026-08-05 at the start of milestone v2.1.0 HERMES-CLAUDE_PARITY (bootstrapped
from prd.md, .planning/codebase/ and git history — the prior PROJECT.md was purged at v2.0.0
close).*
