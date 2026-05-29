---
phase: 00-prd-amendments
plan: 01
status: complete
type: execute
gap_closure: false
requirements: [PRD-01]
files_modified:
  - prd.md
  - CLAUDE.md
key-files:
  created: []
  modified:
    - prd.md
    - CLAUDE.md
---

# Plan 00-01 Summary — Stack drift cluster (Amendments #1-6)

Applied PRD amendments #1 through #6 from `.planning/research/SUMMARY.md` "PRD Amendments Required" table. All edits are pure documentation — zero code files touched, zero commits created (deferred to phase aggregator plan 00-06).

## Amendments Applied

| # | Amendment | Files | Sites |
|---|---|---|---|
| 1 | Go 1.25 minimum (AG-UI requires 1.24.4+) | prd.md, CLAUDE.md | prd.md lines 241, 247, 465, 489, 518 + CLAUDE.md line 123 |
| 2 | Neo4j 5.26-community LTS pin | prd.md | prd.md lines 87, 142, 160, 196, 210, 216 |
| 3 | codeberg.org/readeck/go-readability/v2 migration | prd.md | prd.md lines 1594-1595, 1629, 1665 (OQ closure) |
| 4 | Custom MarkdownV2 escaper (~80 LOC) replaces eekstunt dep | prd.md | prd.md lines 2472 (go.mod removed), 2489, 2687, 2758 |
| 5 | telebot.v4 SHA pin post-2026-05-08 | prd.md | prd.md line 2471 |
| 6 | AG-UI Go SDK SHA pin post-2026-05-14 + new Slice 8 acceptance bullet | prd.md | prd.md lines 2320, 2374 (new), 2430 |

## Verification (greps at final state)

```
grep -c "Go 1\.25" prd.md          → 5   (≥5 required) ✓
grep -c "Go 1\.25" CLAUDE.md       → 1   (≥1 required) ✓
grep -c "5\.26-community" prd.md   → 4   (≥4 required) ✓
grep "Neo4j 5\.x Community" prd.md → 0   (must be 0)   ✓
grep -c "codeberg.org/readeck/go-readability/v2" prd.md → 3 (≥3 required) ✓
grep "go-shiori/go-readability" prd.md → 0 (must be 0) ✓
grep -c "amendment #3" prd.md      → 2   (≥2 required) ✓
grep -c "amendment #4" prd.md      → 3   (≥3 required) ✓
grep -c "amendment #5" prd.md      → 1   (≥1 required) ✓
grep -c "amendment #6" prd.md      → 3   (≥3 required) ✓
grep "SHA-post-2026-05-08" prd.md  → present           ✓
grep "SHA-post-2026-05-14" prd.md  → present           ✓
grep "telebot\.v4 latest" prd.md   → 0   (must be 0)   ✓
grep "require github.com/eekstunt/telegramify-markdown-go" prd.md → 0 ✓
grep "ag-ui/sdks/community/go v\.\.\." prd.md → 0      ✓
```

## Deviations

**Plan acceptance vs action contradiction (Task 3, Amendment #4):**
The plan's automated verify required `grep -E "eekstunt/telegramify-markdown-go" prd.md` to return ZERO matches, but the plan's `<action>` block explicitly instructed adding rationale text that mentions `eekstunt/telegramify-markdown-go` in two places (line 2489 decision-table rationale + line 2687 acceptance-bullet rationale "...eliminates `eekstunt/...` dep"). The action text is the authoritative WHAT-to-do; the strict grep test is internally inconsistent.

Resolution: followed the action verbatim. Two informational mentions remain (lines 2489 and 2687), both in explanatory amendment rationale, not as functional dependencies. The functional intent — `eekstunt/telegramify-markdown-go` removed from go.mod — is fully satisfied: the `require github.com/eekstunt/...` line is deleted.

This contradiction does NOT warrant a PRD-amendment commit (per CLAUDE.md Q&A revision protocol) because the architectural decision is unchanged — only the test phrasing was over-strict.

## No commit created

Per plan frontmatter `commit_per_plan: false` and `phase_commit_aggregator: false`, this plan creates NO git commit. All edits remain uncommitted in the working tree, ready for aggregation by plan 00-06.

## No code files touched

`git status` confirms only `prd.md` and `CLAUDE.md` modified. No `.go`, `.sql`, `.yaml`, `.json`, or other code/config files.
