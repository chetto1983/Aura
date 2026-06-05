---
spike: 003
name: skills-sh-search-api
type: standard
validates: "Given queries (xlsx, find skills, golang), when GET skills.sh/api/search?q= is fetched from Go, then stable JSON parses and both North-Star targets (anthropics/skills→xlsx, vercel-labs/skills→find-skills) are discoverable"
verdict: VALIDATED
related: [004a, 004b, 006]
tags: [skills, catalog, api, phase-11]
---

# Spike 003: skills-sh-search-api

## What This Validates

Phase-11 7b's catalog transport: the skills.sh JSON search endpoint consumed from Go with the exact client shape Aura will ship (D-11/D-15 from `11-CONTEXT.md`). The endpoint is the CLI's internal one (vercel-labs/skills#426 — no public API yet), so drift-detection matters as much as happy-path.

## Research

Prior: the discuss-phase researcher probed `GET /api/search?q=` once via curl (clean JSON `{query, searchType, skills:[{id,skillId,name,installs,source}]}`). This spike re-proves from Go with a strict-decoder tripwire (`DisallowUnknownFields`) to expose schema drift deliberately.

## How to Run

```bash
go run ./.planning/spikes/003-skills-sh-search-api
```

## What to Expect

4 PASS lines (schema across 4 query variants, North-Star targets, edge cases, 5-burst), `[SUMMARY] VALIDATED`, exit 0.

## Investigation Trail

1. Strict decode FAILED immediately — the schema is **richer than the curl probe showed**: results carry `isDuplicate` (per-skill) and some responses a top-level `count`. Fields VARY per response. Lax decode (Go default, ignore unknown) handles everything.
2. `searchType` is adaptive server-side: single-word → `fuzzy`, multi-word → `semantic` ("excel spreadsheet" semantically matched `openai/skills/spreadsheet` as top). Free relevance for natural-language catalog queries from the model.
3. North-Star targets: `anthropics/skills/xlsx` = TOP result for "xlsx" (102,055 installs); `vercel-labs/skills/find-skills` = top for "find skills" (1,861,217 installs). Both stable IDs.
4. Edges: empty query → **HTTP 400** (client must guard); garbage query → valid JSON, 0 results; unicode/Italian query works (6 results for "fogli di calcolo è").
5. Burst of 5 rapid calls: no 429, worst latency 1.46s (first call — cold), then 250-700ms.

## Results

**VALIDATED** — `/api/search` is Go-consumable as 7b's primary transport.

**Client contract for the planner:**
- Lax JSON decode (never `DisallowUnknownFields` in prod — fields drift); only `id`/`skillId`/`name`/`installs`/`source` are load-bearing.
- Guard empty/whitespace queries client-side (400 otherwise).
- Timeout ~15s, treat ≥400 as catalog-unavailable (fail-soft tool error, not crash); rank by `installs`; `id` = `owner/repo/skill` is the stable install handle for 004.
- Multi-word queries are MORE effective (semantic) — the `action=catalog` description should encourage natural phrases, not keywords.
- No rate-limit observed at 5-burst; still cache results per-conversation (the endpoint is internal/undocumented — #426).
