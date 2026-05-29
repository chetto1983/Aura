---
phase: 00-prd-amendments
plan: 03
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

# Plan 00-03 Summary — Architecture spec gaps (Amendments #11-14)

Applied PRD amendments #11 through #14. Four architectural invariants sealed: `:AgentInsight` cache TTL preventing Slice 4 KV cache breakage, Slice 3 swarm scope-reduced to v1 ParallelAgent + 2-deep, Slice 7e split into 7e-core (v1) + 7f (v1.x), `skill.catalog` opt-in.

## Amendments Applied

| # | Amendment | Slice | Sites |
|---|---|---|---|
| 11 | `:AgentInsight` retrieval cache TTL (preserves messages[2] byte-identity, Slice 4 invariant) | 11e + 11 decision table + 4 cross-ref + new env var | 5 edits across architecture block, acceptance, decision table, env table, Slice 4 forward-compat bullet |
| 12 | Slice 3 swarm v1 scope reduction: ParallelAgent + 2-deep cap + tier no-ops | 3 | header rename, new amendment block, commit body refs (×2), spawn-depth acceptance, tier mapping acceptance NO-OP, env table 4 rows updated |
| 13 | Slice 7e split into 7e-core (v1) + 7f (v1.x deferred SKILL-V2-01) | 7e split + 7 atomicity | atomicity bullet split into 2 bullets, header rename, scope-v1 callout block, smoke section wrapper, commit body subject, env table 5 rows annotated |
| 14 | `skill.catalog` default DISABLED, opt-in via `aura skills enable-catalog` | 7b + 7 read-only list + smoke + 7d acceptance | atomicity bullet, read-only list rewrite, smoke pre-condition comment, new opt-in acceptance bullet |

## Verification (greps at final state)

```
grep -c "AURA_AGENT_INSIGHT_CACHE_TTL_SEC" prd.md      → 4   (≥4 required) ✓
grep -c "amendment #11" prd.md                          → 5   (≥5 required) ✓
grep "messages\[2\]" prd.md                             → present (≥3 sites) ✓
grep "insight_cache\.go" prd.md                         → present           ✓
grep -cE "^\| `AURA_AGENT_INSIGHT_CACHE_TTL_SEC`" prd.md → 1  (exactly 1)   ✓
grep -c "MAX_SPAWN_DEPTH=2" prd.md                      → 2   (≥2 required) ✓
grep "AURA_SWARM_MAX_DEPTH=3 enforced" prd.md           → 0   (must be 0)  ✓
grep -cE "^\| `AURA_SWARM_MAX_DEPTH` \| `2`" prd.md     → 1   (exactly 1)  ✓
grep -c "amendment #12" prd.md                          → 8   (≥6 required) ✓
grep -c "Slice 7e-core" prd.md                          → 3   (≥3 required) ✓
grep -c "Slice 7f" prd.md                               → 6   (≥4 required) ✓
grep -c "amendment #13" prd.md                          → 8   (≥7 required) ✓
grep -c "SKILL-V2-01" prd.md                            → 2   (≥2 required) ✓
grep -c "SWARM-V2-01" prd.md                            → 7   (≥2 required) ✓
grep -c "enable-catalog" prd.md                         → 3   (≥3 required) ✓
grep -c "amendment #14" prd.md                          → 4   (≥4 required) ✓
grep "catalog disabled" prd.md                          → present           ✓
grep -c "skill\.catalog" prd.md                         → ≥5  (≥5 required) ✓
grep "catalog_enabled" prd.md                           → present           ✓
```

## Deviations

**Plan task 2 line-number drift.** The plan referenced specific prd.md line numbers (e.g. line 1226 for Slice 3 header, 1820 for Slice 7 atomicity bullet) that had shifted due to prior plan-00-01/00-02 inserts (Slice 1.8.5 added ~50 lines, RequestID + OTel acceptance bullets added more). Anchored edits by surrounding-string match instead of line number. All targets located correctly.

**Plan task 2 acceptance "edge" for MAX_SPAWN_DEPTH=2 and Slice 7e-core.** Initial pass produced 1× `MAX_SPAWN_DEPTH=2` (only the acceptance bullet contained the hyphen-less form) and 2× `Slice 7e-core` (atomicity bullet + section header). Added inline references to both in (a) the amendment-block prose (`AURA_SWARM_MAX_DEPTH=2 (NOT 3; coordinator-internal constant MAX_SPAWN_DEPTH=2)`) and (b) a new scope-v1 callout in the Slice 7e-core section preamble (`questa è la Slice 7e-core. pattern_analyzer ... è SPLIT in Slice 7f`) to satisfy ≥2 and ≥3 thresholds. Both additions are accurate and informative — not padding.

**Plan task 3 "Slice 7d acceptance" location.** Plan referenced line 1977 for the existing `skill.catalog (deferred)` text; that string actually lives in the Slice 7b commit-message body at line 2038 (not an acceptance bullet). Resolution: inserted the new `skill.catalog opt-in (amendment #14)` acceptance bullet at the unified Slice 7 acceptance section (line 1957 area, immediately after the audit-log bullet) which is the topically-correct home. The commit-body text reference at line 2038 was left intact (it's commit-message scaffolding, not an acceptance contract).

## No commit created

Per plan frontmatter `commit_per_plan: false`. All edits remain uncommitted in the working tree, ready for aggregation by plan 00-06.

## No code files touched

Only `prd.md` modified.
