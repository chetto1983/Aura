---
phase: 00-prd-amendments
plan: 04
status: complete
type: execute
gap_closure: false
requirements: [PRD-01]
files_modified:
  - prd.md
key-files:
  created: []
  modified:
    - prd.md
---

# Plan 00-04 Summary — Cross-cutting pitfalls (Amendments #15-19)

Applied PRD amendments #15 through #19. Five cross-cutting hardening items: goleak mandate extension, KV cache invariant CI gate, audit role separation, embedding dim env contract, agent loop budget contract. New top-level §Cross-cutting section added between Slice 4 and §Test discipline.

## Amendments Applied

| # | Amendment | Slice/Section | Sites |
|---|---|---|---|
| 15 | goleak mandate extended to ALL goroutine-spawning packages | §Test discipline | 1 bullet rewrite (line 1514 area) — explicit list of every Slice that spawns goroutines |
| 16 | New §Cross-cutting KV cache invariant CI section + `cache_invariant_audit.sh` script reference | new §Cross-cutting top-level section | Section inserted between Slice 4 closing `---` and `## §Test discipline` (line 1452 area) |
| 17 | Postgres audit role separation (`aura_app` vs `aura_migrate`) + `BEFORE TRUNCATE` trigger | 0.5 + 7c + env table | Slice 0.5 acceptance bullet (line 50 area), Slice 7c migration row trigger extension (line 2031 area), Slice 7c acceptance bullet (line 1996 area), Slice 7c commit body, env table row `AURA_DB_MIGRATE_URL` (line 4364 area), §Cross-cutting Pitfall #3 summary cross-ref |
| 18 | Embedding dim env contract + boot assertion + runbook | 0.7 + 11a + 11b + env table | Slice 0.7 acceptance (2 new bullets), Slice 11a acceptance bullet, Slice 11b acceptance bullet, env table row `AURA_EMBED_DIMENSIONS` |
| 19 | Agent loop budget contract (3 env vars + child budget inheritance) | 0.9 + 1 + 3 + 6 + struct + env table | Slice 0.9 InvocationContext struct fields (`RemainingSteps`, `RemainingWallclockDeadline`), Slice 0.9 acceptance (2 new bullets), Slice 1 acceptance cross-ref, Slice 3 acceptance cross-ref, Slice 6 acceptance cross-ref, 3 env table rows (`AURA_LOOP_MAX_STEPS`, `AURA_LOOP_MAX_WALLCLOCK_SEC`, `AURA_LOOP_DEDUP_WINDOW`) |

## New §Cross-cutting Section Anatomy

- Header: `## §Cross-cutting — KV cache invariant CI (amendment #16, Pitfall #3 P0)`
- Subsections: Architectural rule (TWO system messages), CI gate `scripts/cache_invariant_audit.sh`, Cross-slice acceptance bullets (per-slice enumeration), Pitfall #3 mitigation summary
- Position verified: appears AFTER `## Slice 4 — KV cache` and BEFORE `## §Test discipline`

## Verification (greps at final state)

```
grep -c "cache_invariant_audit\.sh" prd.md   → 5   (≥3 required) ✓
grep -c "amendment #15" prd.md                → 1   (≥1 required) ✓
grep -c "amendment #16" prd.md                → 3   (≥2 required) ✓
grep -c "^## §Cross-cutting" prd.md           → 1   (≥1 required) ✓
grep "messages\[0\] mutated" prd.md           → present           ✓
grep -c "aura_app" prd.md                     → 4   (≥4 required) ✓
grep -c "aura_migrate" prd.md                 → 4   (≥4 required) ✓
grep -c "AURA_DB_MIGRATE_URL" prd.md          → 3   (≥3 required) ✓
grep -cE "^\| `AURA_DB_MIGRATE_URL`" prd.md   → 1   (exactly 1)   ✓
grep -c "BEFORE TRUNCATE" prd.md              → 3   (≥2 required) ✓
grep -c "raise_audit_immutable_truncate" prd.md → 3 (≥2 required) ✓
grep -c "amendment #17" prd.md                → 6   (≥5 required) ✓
grep -c "Pitfall #6" prd.md                   → 5   (≥3 required) ✓
grep -c "AURA_EMBED_DIMENSIONS" prd.md        → 4   (≥4 required) ✓
grep -c "amendment #18" prd.md                → 5   (≥5 required) ✓
grep -c "AURA_LOOP_MAX_STEPS" prd.md          → 3   (≥3 required) ✓
grep -c "AURA_LOOP_MAX_WALLCLOCK_SEC" prd.md  → 2   (≥2 required) ✓
grep -c "AURA_LOOP_DEDUP_WINDOW" prd.md       → 2   (≥2 required) ✓
grep -c "RemainingSteps" prd.md               → 5   (≥3 required) ✓
grep -c "amendment #19" prd.md                → 10  (≥7 required) ✓
grep -cE "^\| `AURA_EMBED_DIMENSIONS`" prd.md → 1   (exactly 1)   ✓
grep -cE "^\| `AURA_LOOP_MAX_STEPS`" prd.md   → 1   (exactly 1)   ✓
grep -c "Pitfall #7" prd.md                   → 3   (≥3 required) ✓
grep -c "Pitfall #9" prd.md                   → 6   (≥4 required) ✓
```

## Deviations

**Plan task 1 case-sensitivity (Amendment vs amendment #15).** Initial pass wrote "Amendment #15 (2026-05-29):" with capital A, leaving the `amendment #15` lowercase grep at zero matches. Changed to "Per amendment #15 ..." — same change pattern applied earlier in Plans 00-01/00-02 (the strict greps in this PRD use lowercase consistently).

**Plan task 3 `Pitfall #7` reach.** Initial pass had 2× `Pitfall #7` mentions (Slice 0.7 acceptance bullet + env table row); the plan required ≥3. Added `Pitfall #7 P0` qualifier to the Slice 11b ingest-batch acceptance bullet (already cited amendment #18; this just makes the Pitfall reference explicit for cross-section audit). No content change — just labelling.

**Plan task 3 Edit 6 line-number drift.** As anticipated by the plan ("DO NOT use absolute line numbers"), the InvocationContext struct fence had already shifted due to plan 00-02 inserting `RequestID`. Located the insertion point dynamically by anchoring on the existing `RequestID ... amendment #9` line (which plan 00-02 added) and inserting `RemainingSteps` + `RemainingWallclockDeadline` immediately after it. Both fields now precede the `// ... extension points` comment, satisfying the aggregator's struct-ordering expectation.

## No commit created

Per plan frontmatter `commit_per_plan: false`. All edits remain uncommitted in the working tree, ready for aggregation by plan 00-06.

## No code/sql/yaml/sh files created

Only `prd.md` modified. The `scripts/cache_invariant_audit.sh` script is *referenced* in the new §Cross-cutting section but explicitly deferred to Phase 6 (Slice 4 implementation) for creation. Likewise the new `aura.agent_job_runs.step_budget` column referenced in Slice 6 acceptance is deferred to Phase 9 (Slice 6 implementation).
