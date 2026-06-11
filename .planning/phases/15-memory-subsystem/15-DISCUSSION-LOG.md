# Phase 15: Memory Subsystem - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-11
**Phase:** 15-memory-subsystem
**Areas discussed:** Capture trigger, Recall behaviour, Doc ingestion scope, Tools & dedup, Mount/governance, Requirements re-scope

---

## Capture trigger — what Aura remembers, when

| Option | Description | Selected |
|--------|-------------|----------|
| Agent-decides | Aura calls memory write-tools deliberately when she judges something worth remembering (Claude-Code parity); no passive every-turn extraction, no confirmation prompts | ✓ |
| Passive auto-extract | Background post-turn LLM extraction of facts/entities/prefs from every conversation (mem0-style); richer but extra LLM call + write churn per turn | |
| Explicit cue only | Stores only on 'remember X'; predictable but forgets unflagged items | |

**User's choice:** Agent-decides
**Notes:** Aligns with the standing operator directive (full terminal like Claude / no ceremony). Compose `--no-auto-preferences` stays consistent.

---

## Recall behaviour — how memory reaches the model

| Option | Description | Selected |
|--------|-------------|----------|
| Pull-on-demand | Agent calls `memory_search` when it decides it needs context (spike-035 path); keeps cached KV prefix untouched | ✓ |
| Proactive injection | Surface top-K relevant memories at conversation start in a cached `messages[2]` block; needs TTL cache to hold KV invariant | |
| Hybrid | Proactively inject only a small cached agent-journal insight block; pull everything else | |

**User's choice:** Pull-on-demand
**Notes:** KV-cache invariant (#11/#29) holds trivially → no `messages[2]` injection machinery / TTL cache built this phase.

---

## Doc ingestion scope — what feeds memory

| Option | Description | Selected |
|--------|-------------|----------|
| Conversational/agent memory only | Ship the package's native surface; defer file/URL document-RAG; re-scopes UX-06 (needs PRD amendment) | ✓ |
| Include document ingestion | Build `aura memory ingest <file>` (markitdown → chunk → embed → store) now; most owned-surface LOC | |
| Light: text + URL only | Ingest plain text + web_fetch'd URLs; defer binary doc parsing | |

**User's choice:** Conversational/agent memory only
**Notes:** "No atomic bombs / minimal industrial shape." Document-RAG becomes its own future phase.

---

## Tools & dedup — which memory tools the agent gets

| Option | Description | Selected |
|--------|-------------|----------|
| Read + core writes | Recall + the 4 core writes (message/entity/fact/preference); block raw `graph_query` | |
| Full 16-tool surface | Everything incl. reasoning-trace memory + read-only `graph_query`; max capability | ✓ |
| Read-only | Recall only, all writes blocked (spike-032 posture) | |

**User's choice:** Full 16-tool surface
**Notes:** Max capability incl. the reasoning-memory differentiator. `graph_query` is read-only (spike-032), so no write-via-Cypher escape. Tools mount Deferred + namespaced `memory__*`.

---

## Mount / governance — how the sidecar is mounted

| Option | Description | Selected |
|--------|-------------|----------|
| Managed recipe, default-on | Trusted Phase-16 managed recipe (doctor/status/logs/policy/profiles), mounts by default, fail-soft | ✓ |
| Bespoke always-on mount | Wire directly like mail/whatsapp, outside the recipe system; fewer moving parts, no manager surface | |
| Managed recipe, opt-in | Recipe the operator enables per profile, off by default | |

**User's choice:** Managed recipe, default-on
**Notes:** Memory is core → on out of the box; reuses the shipped Phase-16 control plane.

---

## Requirements re-scope — PRD amendment consequence

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, amend & proceed | Defer UX-06; UX-08 recall@5/p95 become advisory; UX-09 → agent-written + on-demand (no `messages[2]` injection / no journal cron); PRD amendment #62 records it | ✓ |
| Keep doc ingestion (UX-06) | Reverse the ingestion answer — build `aura memory ingest <file>` now | |
| Keep proactive injection (UX-09) | Reverse the recall answer — keep cached `messages[2]` AgentInsight block + TTL-cache work | |

**User's choice:** Yes, amend & proceed
**Notes:** PRD amendment #62 is the planner's first plan (PRD-first principle; Wave-0 doc-only amendment, the established pattern for superseding phases).

---

## Claude's Discretion

- KV-cache invariant confirmation (expected trivial under pull-on-demand) — researcher/planner verifies via `cache_invariant_audit.sh`.
- Reproducible compose `build:` for the memory image (replace hand-built `:spike-fixed`).
- Upstreaming the `provenance-safe-dedup` fork as a PR (optional, post-merge).
- Exact `aura memory` CLI verb → `memory__*` tool mapping.
- How reasoning-trace memory is exercised/asserted in tests.
- Embedding dimension: defaulted to 384d granite (user did not object).

## Deferred Ideas

- Document-RAG ingestion (file/URL → chunk → embed → entity pipeline) — own future phase.
- Proactive cached-insight injection (`messages[2]`) + background agent-journal cron — revisit if pull-on-demand proves insufficient.
- Leiden community detection + summaries (UX-07 / 11c) — already deferred (amendment #27).
- 11f Task Canvas — sequencing-independent, not this phase.
- Multi-user memory isolation — future scope refactor.
