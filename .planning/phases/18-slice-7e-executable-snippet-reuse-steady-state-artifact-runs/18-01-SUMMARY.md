---
phase: 18-slice-7e-executable-snippet-reuse-steady-state-artifact-runs
plan: 01
subsystem: database
tags: [postgres, sqlc, pgx, ledger, prd-amendment, tool-invocations, deepseek]

# Dependency graph
requires:
  - phase: 11 (Slice 7 skills loop)
    provides: skill tool + host shell_exec surface the probe exercises
provides:
  - PRD amendment #55 (host-primary snippet posture, D-01) + ~2190 follow-up RESOLVED
  - CAP-08.1 requirement row + CAP-08 reconciliation (D-04)
  - aura.tool_invocations append-only ledger (migration 0011 + store + sqlc + runner wiring + cmd/aura/shell.go)
  - db_integration ledger round-trip + append-only trigger tests
  - docs/phase-18-xlsx-call-breakdown.md — D-03 grounded call budget + gate-metric correction
affects: [18-02, 18-03, 18-04, slice-7e]

# Tech tracking
tech-stack:
  added: []
  patterns: [append-only ledger with BEFORE UPDATE/DELETE/TRUNCATE reject triggers, gap-derived LLM-roundtrip estimation from ledger timestamps]

key-files:
  created:
    - internal/toolinvocations/store.go
    - internal/toolinvocations/store_integration_test.go
    - internal/db/migrations/0011_tool_invocations.up.sql
    - internal/db/sqlc/tool_invocations.sql.go
    - cmd/aura/shell.go
    - docs/phase-18-xlsx-call-breakdown.md
  modified:
    - prd.md
    - .planning/REQUIREMENTS.md
    - internal/runner/runner_persist.go

key-decisions:
  - "Amendment number is #55, not the plan's #54 — #54 was already taken by D-43 (completion critic gate)"
  - "D-03 finding: request_id is per-user-turn (count=1 for a whole run), NOT an LLM-roundtrip proxy — the 18-04 gate must count event_kind='end' rows (<=6) + wall-clock (<40s) instead of distinct request_ids"
  - "Probe ran through the production aura chat surface (operator directive), not the eval harness — stronger production-parity grounding"

patterns-established:
  - "Ledger gate metric: tool budget = count(end events) in window; roundtrips = gap-derived (started_at - lag(ended_at) > 0.5s); wall = max(ended_at)-min(started_at)"

requirements-completed: [CAP-08.1]

# Metrics
duration: ~75min (incl. parallel-session wait + paid probe)
completed: 2026-06-06
---

# Phase 18 Plan 01: Wave-0 gate Summary

**PRD flipped to host-primary snippet posture (#55/D-01), CAP-08.1 registered, the append-only tool_invocations ledger committed + integration-proven, and the ~5-call/<40s target grounded by one paid production-surface run — which exposed that request_id cannot be the 18-04 gate metric.**

## Performance

- **Duration:** ~75 min wall (probe itself 142.8s)
- **Completed:** 2026-06-06
- **Tasks:** 3/3 (Task 2's commit half landed externally via Codex)
- **Files modified:** 49 across 4 commits (+1 external)

## Accomplishments
- PRD amendment #55: snippet execution posture HOST-PRIMARY (host shell_exec by-path), sandbox_exec demoted to named escalation; line ~2190 "INVARIATA per ora" follow-up marked RESOLVED; [SUPERATO #55] markers through §Slice 7e-core
- CAP-08.1 added + CAP-08 reconciled in REQUIREMENTS.md; coverage 29 v1 requirements / 10 CAP
- aura.tool_invocations append-only ledger live: migration 0011 (reject triggers verified by test), store, sqlc, runner persistence, cmd/aura/shell.go — build/vet/unit/db_integration/race all green
- D-03 characterization: 1 paid run via `aura chat` — 21 tool dispatches, ~19 LLM roundtrips, 142.8s authoring run; steady-state projected 4-5 dispatches/25-40s; full per-call table in docs/phase-18-xlsx-call-breakdown.md

## Task Commits

1. **Task 1: PRD amendment + CAP-08.1** - `930cb963` (docs)
2. **Task 2: ledger substrate** - `a81133fa` (feat — landed externally via parallel Codex session, swept in unrelated agent/shell work; accepted per project mega-commit velocity pattern)
3. **Task 2: db_integration round-trip** - `8c3ed97e` (test — TestLedgerRoundTrip + TestLedgerAppendOnly, RUN not skipped, 0.10s/0.07s vs live PG)
4. **Task 3: D-03 probe doc** - `2cce24fe` (docs)

## Files Created/Modified
- `internal/toolinvocations/` — append-only ledger store (Insert, ListByConversation) + unit/integration/goleak tests
- `internal/db/migrations/0011_tool_invocations.{up,down}.sql` — table + reject-mutation triggers + aura_app SELECT/INSERT grant
- `prd.md` / `.planning/REQUIREMENTS.md` — amendment #55 / CAP-08.1
- `docs/phase-18-xlsx-call-breakdown.md` — D-03 grounding + gate-metric correction

## Decisions Made
- Amendment #55 (not #54 — collision with D-43)
- 18-04 gate metric replaced: ledger end-event count ≤6 + wall <40s (request_id is per-user-turn — asserting `distinct request_id <= 5` would pass trivially and gate nothing)

## Deviations from Plan

### 1. Amendment number drift
- **Found during:** Task 1 — plan said #54; prd.md already had #54 (D-43, commit eed15b4d)
- **Fix:** used #55 everywhere, cross-references consistent

### 2. Task 2 commit half landed externally
- **Found during:** orchestration — parallel Codex session committed the substrate as `a81133fa` (46 files) mid-phase
- **Impact:** plan's "isolated substrate commit" became part of a broader feat commit; verification half (build/vet/tests/round-trip) executed per plan afterward, zero re-authoring needed

### 3. D-03 probe via production chat surface
- **Found during:** Task 3 checkpoint — operator directed "do it in autonomy using aura chat" instead of `go test -tags cot_eval TestSkillsE2E`
- **Impact:** stronger grounding (production registry); the probe exercised skill-reuse shape (xlsx + yahoo-finance skills pre-existing in export root), not self-install

### 4. Gate-metric correction (the D-03 payoff)
- **Found during:** ledger dump — `distinct request_ids = 1` for a 21-call run
- **Fix:** documented replacement metric in docs/phase-18-xlsx-call-breakdown.md; 18-04 must consume it

**Impact on plan:** deviations 1-3 procedural; deviation 4 is a material correction the 18-04 gate depends on. No scope creep.

## Issues Encountered
- prd.md still references `0011_snippet_runs` (~2540/~2644) as an optional forensics table while shipped slot 0011 = tool_invocations (deliberately skipped per D-19/A4). Wording drift only — flagged for a future doc pass, out of Task-1 scope.

## User Setup Required
None — OPENROUTER_API_KEY was already configured; probe consumed one paid run with explicit operator authorization.

## Next Phase Readiness
- 18-02 (host-primary action=use) unblocked: posture amendment committed, ledger live
- 18-04 MUST use the corrected gate metric (end-event count + wall-clock, NOT distinct request_ids)
