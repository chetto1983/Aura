---
phase: 06-kv-cache-builder
plan: 01
subsystem: infra
tags: [prd-amendment, kv-cache, prompt-builder, postgres, cache-metrics, doc-gate]

# Dependency graph
requires:
  - phase: 04-conversation-persistence
    provides: "migrations 0001-0006 shipped (0006 conversation_turns_fts last); llm.Usage{PromptTokens,CompletionTokens,CachedTokens,Cost} populated by openai_compat client"
provides:
  - "prd.md §Slice 4 amended: OQ2 in-memory→Postgres aura.cache_metrics persistence (D-02), migration 0007 reserved"
  - "prd.md §Slice 4 file-targets relocated internal/llm/prompt.go → internal/agent/prompt/ (D-01a, import-cycle resolution)"
  - "prd.md §Slice 4 cache_deepseek.go file-target dropped (D-02a, parsing already shipped in openai_compat/sse.go + usage.go)"
  - ".planning/PROJECT.md Capabilities block aligned to authoritative REQUIREMENTS.md scheme (KV cache = CAP-04)"
affects: [06-02, 06-03, 06-04, 06-05, kv-cache-builder, prompt-builder, cache-metrics]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PRD-first doc gate: a single prd: amendment commit (zero .go files) lands BEFORE any phase code commit — mirrors Phase-0 and 05-01 doc-only gate pattern"

key-files:
  created: []
  modified:
    - "prd.md — §Slice 4 OQ2 override + file-target table relocation + cache_deepseek drop + Goal-line relocation note"
    - ".planning/PROJECT.md — Capabilities block CAP-03→CAP-07 renumber (KV cache = CAP-04)"

key-decisions:
  - "D-02: cache stats persist per-turn to Postgres aura.cache_metrics (migration 0007), overriding PRD OQ2 'in-memory only'"
  - "D-01a: PromptBuilder relocates to internal/agent/prompt/ (builder.go + hash.go + cache_anthropic.go) — internal/llm cannot import internal/agent/tools (import cycle via manifest.go)"
  - "D-02a: internal/llm/cache_deepseek.go dropped — usage parsing already shipped in internal/llm/openai_compat/sse.go + usage.go"
  - "PROJECT.md CAP drift fixed by aligning the whole Capabilities block to REQUIREMENTS.md authoritative scheme (avoids a duplicate CAP-04)"

patterns-established:
  - "PRD-first amendment gate: deviations from prd.md require a prd: commit before the code commit; OQ3 (already 'Proposto: SÌ') left untouched as it needs no amendment"

requirements-completed: [CAP-04]

# Metrics
duration: 9min
completed: 2026-06-02
---

# Phase 6 Plan 01: Slice 4 PRD-amendment gate Summary

**PRD-first doc gate for the KV cache builder — amends Slice 4 to persist cache metrics to Postgres aura.cache_metrics (D-02), relocate PromptBuilder to internal/agent/prompt/ around a confirmed import cycle (D-01a), drop the already-shipped cache_deepseek.go target (D-02a), and fix the CAP-03→CAP-04 KV-cache label drift — committed as a single prd: commit with zero .go files.**

## Performance

- **Duration:** ~9 min
- **Started:** 2026-06-02
- **Completed:** 2026-06-02
- **Tasks:** 2
- **Files modified:** 2 (prd.md, .planning/PROJECT.md)

## Accomplishments
- Overrode PRD Slice 4 OQ2 ("in-memory only / stats are debug") with per-turn Postgres persistence to a new `aura.cache_metrics` table (conversation_id, seq, ts, prompt_tokens, cached_tokens, cost_usd), reserving migration **0007** (0006 confirmed last shipped both in prd.md and on disk under `internal/db/migrations/`).
- Relocated the `PromptBuilder` file-target from `internal/llm/prompt.go` to the new `internal/agent/prompt/` subpackage (builder.go + hash.go + cache_anthropic.go + builder_test.go), citing the confirmed import cycle: `internal/agent/tools/manifest.go` imports `internal/llm`, so `internal/llm` cannot import `internal/agent/tools` for `RenderToolDefs()`.
- Dropped the `internal/llm/cache_deepseek.go` file-target with an "already shipped / dropped" note pointing at `internal/llm/openai_compat/sse.go` + `usage.go` (D-02a).
- Fixed the CAP label drift in PROJECT.md so the KV-cache requirement reads **CAP-04** (matching authoritative REQUIREMENTS.md), aligning the whole Capabilities block (CAP-03 Swarm, CAP-04 KV, CAP-05 Web, CAP-06 Scheduler, CAP-07 Skills).
- Landed everything as one `prd:` amendment commit containing only prd.md + PROJECT.md — zero `.go` files — satisfying PRD-first before any Phase-6 code.

## Task Commits

Per the plan, both tasks land in ONE combined `prd:` amendment commit (Task 1 edits prd.md; Task 2 edits PROJECT.md + commits both — one slice/sub-slice = one commit):

1. **Task 1: Amend prd.md §Slice 4 (OQ2 persistence + file-target relocation + cache_deepseek drop)** — committed in `7cf8acf2`
2. **Task 2: Fix CAP-03→CAP-04 label drift in PROJECT.md + single amendment commit** — `7cf8acf2` (prd)

## Files Created/Modified
- `prd.md` — §Slice 4: OQ2 rewritten to Postgres `aura.cache_metrics` persistence + migration 0007; file-target table relocated to `internal/agent/prompt/` with the import-cycle rationale inline; `cache_deepseek.go` dropped with a shipped-already note; Goal line annotated with the D-01a relocation. OQ3 (`ToolsCacheControl`, D-03) left unchanged.
- `.planning/PROJECT.md` — Capabilities block renumbered to the authoritative REQUIREMENTS.md scheme so the KV-cache requirement is CAP-04.

## Decisions Made
- **Aligned the whole PROJECT.md Capabilities block, not just the KV line.** The plan asked to relabel KV-cache CAP-03→CAP-04, but line 37 already carried CAP-04 on "Web tools". A bare single-line edit would have produced a duplicate CAP-04. The authoritative source (REQUIREMENTS.md) maps CAP-03→Swarm, CAP-04→KV, CAP-05→Web tools; aligning the contiguous block (CAP-02→CAP-03 Swarm, …, CAP-06→CAP-07 Skills) is the tightest fix that lands the required KV=CAP-04 without introducing a broken duplicate. Phase-9 Swarm correctly ends up labelled CAP-03 as the plan required ("do not relabel Phase-9 Swarm's CAP-03" → its end-state is CAP-03). REQUIREMENTS.md was NOT touched (authoritative, already correct).

## Deviations from Plan

None affecting scope — plan executed as written. The one judgment call (aligning the full Capabilities block rather than a single line) is documented under Decisions Made: it is the minimal correct way to satisfy the plan's "PROJECT.md references CAP-04 for the KV-cache requirement" acceptance without leaving a duplicate CAP-04 label.

## Issues Encountered
None. The migration-numbering check (0006 last shipped → 0007 next) was confirmed against both `prd.md` and the on-disk `internal/db/migrations/` listing before writing the amendment.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- PRD-first gate satisfied: all subsequent Phase-6 code plans (06-02..06-05) may now commit `.go` files / the migration 0007 / scripts without violating PRD-first.
- Reserved targets for downstream plans: `internal/agent/prompt/` subpackage, `aura.cache_metrics` migration 0007, `aura cache-stats --since=<window>` SQL query, `scripts/cache_invariant_audit.sh` + `aura cache-audit` hidden subcommand (per 06-CONTEXT D-04/D-06).
- No blockers.

## Self-Check: PASSED

- `06-01-SUMMARY.md` — FOUND
- amendment commit `7cf8acf2` (prd.md + PROJECT.md, 0 .go files) — FOUND
- summary commit `c1467bed` — FOUND
- working tree clean

---
*Phase: 06-kv-cache-builder*
*Completed: 2026-06-02*
