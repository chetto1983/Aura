---
phase: 00-prd-amendments
plan: 02
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

# Plan 00-02 Summary — Feature gaps cluster (Amendments #7-10)

Applied PRD amendments #7 through #10. Four small additive edits expanding existing slices — no new top-level slices, one decimal sub-slice (1.8.5). Pure documentation; zero code/commits.

## Amendments Applied

| # | Amendment | Slice | Sites |
|---|---|---|---|
| 7 | New sub-slice 1.8.5 conversation FTS (pg_trgm GIN + `aura chat search`) | new 1.8.5 | prd.md lines 1051-1100 (new section between 1.8 commit body and 2 header) |
| 8 | `/cost` + `/search` Telegram commands + CLI subcommand | 9b | prd.md lines 2550 (cmd count 8→10), 2587 (commands.go LOC +40 + 2 inline command rows), 2741-2743 (acceptance bullets) |
| 9 | `RequestID` UUIDv7 in `InvocationContext` + OTel span per LLM call | 0.9 + 1 | prd.md line 250 (Pre-requisiti `google/uuid` v1.6.0+), line 297 (struct field), line 464 (Slice 0.9 acceptance bullet), line 574 (Slice 1 OTel span acceptance bullet) |
| 10 | `AURA_SETUP_TOKEN` one-time setup wizard token | 9a | prd.md line 2534 (decision table), line 2731 (Slice 9a acceptance bullet), line 4336 (env table row inserted after AURA_SETUP_BIND) |

## New Slice 1.8.5 Anatomy

- Header: `## Slice 1.8.5 — Conversation full-text search (pg_trgm GIN + aura chat search)`
- Sections present: Goal, Atomicity note, Pre-requisiti, Smoke, Acceptance (5 machine-checkable bullets), File targets (5-row table, ~80 LOC src + ~30 test), Open questions (none), Migration numbering note, Commit message template
- Position verified: appears AFTER Slice 1.8 commit body, BEFORE `## Slice 2 — Sandbox runner`
- Requirement traceability: REQ CORE-05 (REQUIREMENTS.md line 28)

## Verification (greps at final state)

```
grep -c "^## Slice 1\.8\.5" prd.md       → 1   (exactly 1)    ✓
grep -c "pg_trgm" prd.md                 → 8   (≥3 required)  ✓
grep -c "aura chat search" prd.md        → 8   (≥2 required)  ✓
grep -c "amendment #7" prd.md            → 1   (≥1 required)  ✓
grep -c "0005_conversation_turns_fts" prd.md → 4 (≥2 req'd)   ✓
grep -c "/cost" prd.md                   → 6   (≥3 required)  ✓
grep -c "AURA_SETUP_TOKEN" prd.md        → 3   (≥3 required)  ✓
grep -c "amendment #8" prd.md            → 4   (≥3 required)  ✓
grep -c "amendment #10" prd.md           → 3   (≥3 required)  ✓
grep -cE "^\| `AURA_SETUP_TOKEN`" prd.md → 1   (exactly 1)    ✓
grep -c "RequestID" prd.md               → 3   (≥3 required)  ✓
grep -c "UUIDv7" prd.md                  → 3   (≥3 required)  ✓
grep -c "amendment #9" prd.md            → 4   (≥4 required)  ✓
grep "google/uuid" prd.md                → present            ✓
grep "llm\.request" prd.md               → present            ✓
grep "aura\.request_id" prd.md           → present            ✓
```

## Deviations

None. All grep checks pass at first attempt; case-sensitivity issue with "Amendment #7"/"Amendment #10" capital-A resolved by switching to lowercase "Per amendment #..." in the prose introduction phrasing (consistent with other amendment-citation conventions in the PRD).

## No commit created

Per plan frontmatter `commit_per_plan: false`. All edits remain uncommitted in the working tree, ready for aggregation by plan 00-06.

## No code files touched

Only `prd.md` modified.
