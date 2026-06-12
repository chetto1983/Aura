---
phase: 15-memory-subsystem
plan: 01
subsystem: docs/prd
tags: [prd-amendment, requirements, roadmap, memory, agent-memory-mcp, re-scope]
requires: []
provides:
  - "PRD amendment #62 (UX-06..09 re-scope to the adopted agent-memory MCP surface)"
  - "AURA_AGENT_MEMORY_MCP_PORT/IMAGE/URL env catalog entries"
  - "Re-stated REQUIREMENTS.md UX-06..09"
  - "Re-derived ROADMAP Phase 15 Success Criteria"
affects:
  - "all downstream Phase 15 wiring plans (15-02/15-03/15-04/15-05) plan against this re-scoped contract"
tech-stack:
  added: []
  patterns: ["PRD-first Wave-0 doc amendment for superseding phases (P8 #44 / P9 D-23 precedent)"]
key-files:
  created:
    - .planning/phases/15-memory-subsystem/15-01-SUMMARY.md
  modified:
    - prd.md
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md
decisions:
  - "D-12: UX-06 deferred, UX-07 package-owned + Leiden-deferred (#27), UX-08 advisory snapshot, UX-09 on-demand reasoning/insight (no messages[2], no journal cron)"
  - "D-11: 384d is already the live state — no 768d→384d migration drop; #61 node (2) framing superseded"
metrics:
  duration: ~6min
  completed: 2026-06-12
  tasks: 2
  files: 3
  commits: 2
---

# Phase 15 Plan 01: PRD Amendment #62 — UX-06..09 Re-scope to Adopted agent-memory MCP Summary

Doc-only Wave-0 PRD-first amendment that re-scopes requirements UX-06..09 from the
superseded bespoke 11a/11b/11d/11e build to the adopted `neo4j-labs/agent-memory` MCP
surface, landed before any Go code so the rest of Phase 15 plans against the real contract.

## What Was Built

Two atomic doc commits, both landing before any Go commit in Phase 15 (the `git log`
ordering gate, D-12):

1. **`prd.md` Amendment #62** (commit `05e680e4`): a new block-quote under §Slice 11,
   immediately below Amendment #61 (same `▶ Amendment #NN` format). Records the four
   re-scopes verbatim per D-12 with `[SUPERSEDED #61]` tags where bespoke items overlap #61:
   - **UX-06** (document → chunk → embed → entity doc-RAG pipeline) → `[DEFERRED]` to a
     future phase; the package is conversation/entity memory, not a chunked doc-RAG engine.
   - **UX-07** (Leiden community detection) → already deferred (#27, unchanged); entity
     resolution now owned by the package (POLE+O + provenance-safe-dedup, spike 034).
   - **UX-08** (recall@5 ≥ 0.8 / p95 ≤ 30ms) → ADVISORY snapshot vs the package, appended to
     `docs/aura-quality-snapshot.md`; amendment #20 snapshot gate still applies, but it is
     NOT an Aura-owned WRRF/p95 pass-fail gate.
   - **UX-09** (agent journal + cached `messages[2]` injection) → agent-written
     reasoning/insight recalled on demand via the package's reasoning-trace tools; NO cached
     `messages[2]` injection (D-04), NO background journal cron.
   - Env catalog: `AURA_AGENT_MEMORY_MCP_PORT` (8091), `AURA_AGENT_MEMORY_MCP_IMAGE`
     (`aura-agent-memory-mcp:local`), test-tier `AURA_AGENT_MEMORY_MCP_URL` added to the
     §Caps & Limits env-var index.
   - 384d-already-live note (D-11): no 768d→384d migration drop is needed; `0001_init.cypher`
     is 384d and `DefaultEmbedDimensions=384`; the "768d legacy migrations become dead"
     framing of #61 node (2) is stale and #62 supersedes it.
   - Cites D-11 and D-12.

2. **`REQUIREMENTS.md` + `ROADMAP.md`** (commit `2eda202d`):
   - REQUIREMENTS.md UX-06..09 lines re-stated to the adopted scope, each referencing
     amendment #62; the four `UX-0N:` IDs and `[ ]` checkbox markers preserved (re-scoped,
     not yet delivered — kept unchecked). Bespoke wording retained struck-through as design
     reference.
   - ROADMAP Phase 15 Success Criteria re-derived to this phase's actual deliverables:
     (1) default-on managed mount of the 16 `memory__*` tools (no `aura mcp install`);
     (2) `aura memory <verb>` operator CLI round-trip; (3) agent recall path
     `tool_search → memory__memory_search → text_response`; (4) `cache_invariant_audit.sh`
     passes unchanged (no `messages[2]` stream, D-04); (5) reproducible
     `docker compose build aura-agent-memory-mcp` + advisory recall@5/p95 snapshot. The
     bespoke 5 criteria retained as a superseded design-reference block-quote, not deleted.

## Key Decisions

- **D-12 (re-scope)** copied verbatim from 15-CONTEXT.md into the amendment and the
  requirements — the re-scope text is the contract downstream executors/verifiers plan
  against, so it had to land before any wiring code (PRD-first Wave-0 gate).
- **D-11 (384d already live)** recorded explicitly to head off a phantom 768d→384d migration
  drop: the live compose service, `0001_init.cypher`, and `DefaultEmbedDimensions` are all
  already 384d. Amendment #62 supersedes the stale 768d framing in Amendment #61 node (2).
- **Bespoke text retained, not deleted** (REQUIREMENTS struck-through, ROADMAP as a
  superseded block-quote) — mirrors how #61 retained the bespoke Goal/Slices as design
  reference.

## Deviations from Plan

None — plan executed exactly as written. Both tasks' automated verify greps passed on the
first attempt; no Rule 1–4 deviations, no auth gates, no checkpoints.

## Verification

- `grep -c "Amendment #62" prd.md` → 1 (block present under §Slice 11).
- `grep "AURA_AGENT_MEMORY_MCP_PORT" prd.md` → present (env index + amendment prose; 4 total
  `AURA_AGENT_MEMORY_MCP_` hits = 3 env rows + 1 catalog note).
- `grep -cE "UX-0[6789]" prd.md` → 8; `… .planning/REQUIREMENTS.md` → 8 (all four re-scoped in
  both files).
- `grep -cE "^- \[ \] \*\*UX-0[6789]\*\*" .planning/REQUIREMENTS.md` → 4 (IDs + unchecked
  boxes preserved).
- `git log --oneline` shows `05e680e4` (amendment #62) as the first Phase-15 commit; both
  plan commits touch **0** `.go` files (PRD-first ordering gate holds, D-12).
- ROADMAP keyword gate (`default-on|advisory|reasoning/insight|DEFERRED`) → present.

## Known Stubs

None — doc-only plan, no code, no data sources, no placeholders.
