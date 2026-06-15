---
phase: 00-prd-amendments
plan: 05
status: complete
type: execute
gap_closure: false
requirements: [PRD-01]
files_modified:
  - prd.md
  - docs/aura-quality-snapshot.md
key-files:
  created:
    - docs/aura-quality-snapshot.md
  modified:
    - prd.md
---

# Plan 00-05 Summary — Quality gate cluster (Amendment #20)

Applied PRD amendment #20. Created `docs/aura-quality-snapshot.md` as living-doc with seed schema (6 metric rows × 5 columns including CI gate path globs) + wired prd.md to reference it from Slice 11d acceptance, Slice 11a schema acceptance, §Slice Q&A discipline Gate 3 DoD, §Cross-cutting Pitfall #3 summary cross-ref. HNSW `M=32` (NOT default 16) injected into all 4 CREATE VECTOR INDEX statements with `ef_construction=200`.

## Amendments Applied

| # | Amendment | Files | Sites |
|---|---|---|---|
| 20 | Establish `docs/aura-quality-snapshot.md` living doc + CI gate + HNSW M=32 baseline | NEW docs/aura-quality-snapshot.md + prd.md edits | 1 new file (~96 lines), 4 Cypher CREATE VECTOR INDEX updates (Slice 11a), Slice 11a HNSW verification acceptance bullet, Slice 11d HNSW M=32 baseline + CI gate acceptance bullets, §Slice Q&A discipline Gate 3 DoD bullet, §Cross-cutting Pitfall #3 summary cross-ref |

## New Files

**`docs/aura-quality-snapshot.md`** (96 lines, seed)

Sections present in order:
1. Title `# Aura Quality Snapshot (living doc)` + dates + Owner
2. Purpose (4-sentence rationale citing amendment #20 + `feedback_aura_as_product`)
3. Quality matrix table — 6 rows × 6 columns: `Metric | Target | Last measured | Last value | Owner phase | CI gate path`
   - Sandbox escape rate (Phase 5)
   - KV cache hit rate (Phase 6)
   - GraphRAG retrieval recall@5 @ 100K corpus (Phase 15)
   - Vector search p95 latency @ 100K corpus (Phase 15)
   - Telegram MarkdownV2 escape fuzz (Phase 13)
   - Skill snippet exec success rate (Phase 11)
4. HNSW configuration baseline (amendment #20 cross-ref) — documents `M=32` + `ef_construction=200`
5. CI gate contract — `scripts/quality_snapshot_gate.sh` algorithm + exit codes
6. How to update (operator runbook, 4 steps)
7. Cross-phase dependency note — quotes `feedback_aura_as_product` + per-phase staging dependencies
8. References — prd.md anchor sites + research SUMMARY.md + ROADMAP.md success criteria

## Verification (greps at final state)

```
Snapshot file (docs/aura-quality-snapshot.md):
  wc -l                                          → 96   (≥60 required)  ✓
  HNSW M=32                                      → present              ✓
  SandboxEscapeBench                             → present              ✓
  recall@5                                       → present              ✓
  p95                                            → present              ✓
  cache hit rate (case-insensitive)              → present              ✓
  amendment #20                                  → 5    (≥2 required)   ✓
  Phase 5 / Phase 6 / Phase 15                   → all present          ✓
  quality_snapshot_gate.sh                       → present              ✓

prd.md:
  grep -c "docs/aura-quality-snapshot.md"        → 3    (≥3 required)   ✓
  grep -c "amendment #20"                        → 9    (≥5 required)   ✓
  grep -cF "vector.hnsw.m\`: 32" (literal)       → 4    (≥4 required)   ✓
  grep -c "HNSW M=32"                            → 6    (≥2 required)   ✓
  grep -c "quality_snapshot_gate.sh"             → 3    (≥2 required)   ✓
  grep "feedback_aura_as_product"                → present              ✓
```

## Deviations

**Plan task 2 acceptance grep pattern (`vector\.hnsw\.m..: 32`) was malformed.** The plan's regex represents the backtick + colon as `..` (any 2 chars), but the actual text in prd.md has only 1 char between `m` and `:` (the closing backtick `` ` ``). The pattern `vector\.hnsw\.m..: 32` therefore returns 0 matches against valid content. Verified the criterion satisfied via two alternative greps documented in the plan as acceptable options:

- `grep -c "HNSW M=32" prd.md` → 6 (plan threshold ≥ 2) ✓
- `grep -cF "vector.hnsw.m\`: 32" prd.md` (fixed-string variant) → 4 (plan threshold ≥ 4) ✓

Both alternative forms exceed plan thresholds. The functional intent (4 CREATE VECTOR INDEX statements with explicit `M=32`) is met.

## No commit created

Per plan frontmatter `commit_per_plan: false`. All edits remain uncommitted in the working tree, ready for aggregation by plan 00-06.

## No code/sql/yaml/sh files created

`docs/` parent directory was created. Only the new markdown living-doc + the existing `prd.md` modified. The referenced `scripts/quality_snapshot_gate.sh` and `scripts/memory_recall_bench.sh` are explicitly deferred to Phase 15 (per amendment #20 and Slice 11d acceptance) — neither file authored here.
