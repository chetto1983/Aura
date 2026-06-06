---
phase: 18-slice-7e-executable-snippet-reuse-steady-state-artifact-runs
plan: 04
subsystem: testing
tags: [eval, cot_eval, snippet-reuse, steady-state, tool-invocations, ledger, xlsx, yfinance, d-03, cap-08.1]

# Dependency graph
requires:
  - phase: 18 (Wave 1 / 18-01)
    provides: tool_invocations ledger (migration 0011) + the D-03 ≤6-dispatch/<40s grounded gate metric (request_id-proxy correction)
  - phase: 18 (Wave 2 / 18-02)
    provides: host-primary snippet action=use frame (skill action=use → host shell_exec by-path) — the mechanic the steady-state gate measures
  - phase: 18 (Wave 3 / 18-03)
    provides: in-loop save_snippet/restore/archive + Writer.Restore — the lifecycle the reuse scenario can author against
  - phase: 11 (Slice 7e-core)
    provides: snippet store + skill action-enum tool + host shell_exec surface
provides:
  - A REAL, intent-honoring pre-seeded snippet fixture (live yfinance market builder) so the steady-state reuse gate is satisfiable via the snippet's OWN output (no recovery churn)
  - The live steady-state numbers (endEvents=5 / wallClock=11.057s) stamped into docs/aura-quality-snapshot.md's Phase-18 cells
  - The eval↔production registry parity + ledger-asserted reuse gate closed end-to-end (TestSnippetReuseE2E green, paid)
affects: [slice-7e, Phase 18 close-out]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fixture-to-intent repair (NOT a budget/assertion weakening): when a pre-seeded test fixture contradicts the scenario's DECLARED intent and the failed run's ledger proves the product behaved correctly, repair the fixture to honor the intent — budgets/assertions stay strict, justified in the commit body per NEVER-MODIFY-TESTS"
    - "Steady-state reuse measurement: pre-seed an ACTIVE snippet that honors its own description → measure ONE reuse run from the durable aura.tool_invocations ledger (event_kind='end' count + wall-clock), never r.Reply"

key-files:
  created: []
  modified:
    - internal/eval/skills_snippet_reuse_cot_eval_test.go
    - docs/aura-quality-snapshot.md

key-decisions:
  - "reuseSnippetCode() rewritten from a 4-ticker hardcoded STUB to a live yfinance market builder honoring the SaveSnippet description; the date cell ('Aggiornato al <ISO>') satisfies the strict today cell-value floor through the snippet's OWN output"
  - "Kept the strict today cell-value check (NOT softened to title/filename) — a proper snippet writes a date cell, so the strict check holds with no weakening"
  - "Ran the paid gate ONCE (operator-authorized) after the fixture fix; recorded BOTH runs (the fixture-defect 13/71.8s diagnostic + the final 5/11.057s steady state)"

patterns-established:
  - "Fixture-defect triage: a red paid gate whose ledger proves the product path WORKED (action=use → host by-path exec) is a fixture defect, not a product gap — repair the seed, re-run once, do not weaken the gate"

requirements-completed: [CAP-08.1]

# Metrics
duration: ~35min
completed: 2026-06-06
---

# Phase 18 Plan 04: Steady-state snippet-reuse gate — fixture repair + paid close-out Summary

**Repaired the pre-seeded snippet fixture from a 4-ticker hardcoded stub to a real live-yfinance market builder that honors its own SaveSnippet description, then re-ran the paid steady-state gate ONCE: `TestSnippetReuseE2E` went GREEN at endEvents=5 / wallClock=11.057s / today=true — a ≈4× dispatch and ≈13× wall-clock collapse from the D-03 authoring baseline (21 / 142.8s), proving the Phase-18 snippet-reuse steady state holds end-to-end via the production runner + the durable tool_invocations ledger.**

## Performance

- **Duration:** ~35 min wall
- **Completed:** 2026-06-06
- **Tasks:** fixture repair + paid checkpoint close-out (Tasks 1+2 of 18-04 landed in prior commits `6f0aca06` / `77cbc0c3`)
- **Files modified:** 2

## Accomplishments

- **Fixture-to-intent repair** (`internal/eval/skills_snippet_reuse_cot_eval_test.go`): `reuseSnippetCode()` rewritten from a STUB (4 hardcoded tickers, columns Ticker/Nome/Data, no prices, no live fetch) to a REAL script honoring the declared description — fetches live yfinance quotes for the US indices + mega-caps (`^GSPC ^DJI ^IXIC` + AAPL MSFT GOOGL AMZN META TSLA NVDA), columns Ticker/Nome/Prezzo/Var %/Volume, an `Aggiornato al <YYYY-MM-DD>` date cell, saves `Mercato_Yahoo_<date>.xlsx` into cwd, prints the filename, exits non-zero with a clear message on fetch failure. Runs in ~2-5s (one batched yfinance download), inside the steady-state budget.
- **Live steady-state gate GREEN** (paid, operator-authorized, ONE run): `endEvents=5` (≤6), `wallClock=11.057s` (<40s), `roundtrips(diag)=5`, fresh+opens+today all true. Tool sequence: `current_time → shell_exec → skill action=use → shell_exec by-path → shell_exec verify` — exactly the projected steady-state shape.
- **Artifact visually verified** (`Mercato_Yahoo_2026-06-06.xlsx`, 10 tickers, real today's prices, `Aggiornato al 2026-06-06` cell; openpyxl row-dump, no mojibake, sensible structure).
- **Pitfall-7 host-prerequisite updated** (test header + quality-snapshot): host needs python3 + openpyxl + **yfinance**.
- **Quality snapshot stamped**: Phase-18 steady-state cells (dispatches=5 / wall=11.057s / diagnostic=5 / fresh+opens+today) filled with live numbers; both runs recorded (fixture-defect diagnostic + final steady state) + the network-hang backlog finding.

## Task Commits

1. **Fixture repair: real yfinance market builder + Pitfall-7 note** - `6a3c9d84` (test)
2. **Plan metadata: SUMMARY + snapshot live cells** - (this commit) (docs)

_Tasks 1+2 of the plan (eval registry parity + the ledger-asserted gate) landed earlier in `6f0aca06` (test) and `77cbc0c3` (test); this plan-04 close-out repaired the fixture the paid checkpoint exposed and ran the gate green._

## Files Created/Modified

- `internal/eval/skills_snippet_reuse_cot_eval_test.go` — `reuseSnippetCode()` rewritten to a live yfinance builder; SaveSnippet description updated to match; header Pitfall-7 note adds yfinance.
- `docs/aura-quality-snapshot.md` — Phase-18 matrix row + detail cells stamped with the live steady-state numbers; a two-run live log + the network-hang backlog finding added; Pitfall-7 operator command adds yfinance.

## Decisions Made

- **Strict today cell-value check KEPT, not softened.** The triage flagged that the today-floor greps cell VALUES for the ISO date and the fixture-defect run's recovered artifact carried the date only in title/filename (today=false). With a proper snippet that writes an `Aggiornato al <ISO>` cell this is moot — the snippet's own output satisfies the strict check, so the strict floor stays.
- **Repaired the fixture, did NOT weaken the gate.** Budgets stay ≤6 dispatches / <40s; the today check stays strict cell-value. This is a fixture-to-intent repair justified in the commit body per the NEVER-MODIFY-TESTS rule.
- **One paid run only.** The operator authorized a single re-run; it passed, so no further paid spend.

## Deviations from Plan

### 1. [Rule 1 - Bug] Pre-seeded snippet fixture was a stub contradicting its own description

- **Found during:** the paid checkpoint (the gate the plan's Task-3 checkpoint calls for).
- **Issue:** `reuseSnippetCode()` shipped 4 hardcoded tickers with columns Ticker/Nome/Data and NO prices / NO live fetch, while its `SaveSnippet` description promised "Build an .xlsx of today's market from a fetched table". The gate was UNSATISFIABLE via the snippet as seeded: the failed paid run (conversation `019e9d32-2b8e-7c94-a4b8-eea944b9f7b6`, `endEvents=13 / wallClock=71.8s / today=false`) is ledger-proven to be the model + product behaving CORRECTLY — `seq5 skill action=use market-xlsx-builder → seq7 host by-path exec` (the Phase-18 mechanic worked), then the model inspected the stub's output, found it inadequate, and recovered (`seq13-15` wrote+ran its own yfinance script → real today's data matching the morning D-03 probe). The 7 extra dispatches + ~30s were recovery from the stub, not a product gap.
- **Fix:** rewrote `reuseSnippetCode()` to a live yfinance builder honoring the description (prices + Var % + volume + an `Aggiornato al <ISO>` date cell); updated the SaveSnippet description to match (yfinance / Mercato_Yahoo_<date>.xlsx) and the Pitfall-7 host-prerequisite notes.
- **Files modified:** `internal/eval/skills_snippet_reuse_cot_eval_test.go`, `docs/aura-quality-snapshot.md`.
- **Verification:** `go vet -tags 'cot_eval db_integration' ./internal/eval/` exit 0; the key-free structural tier (`TestSnippetReuseE2E` SKIPs, `TestRegistry_SeamFree` + `TestRegistrySnippetReuse_HasSkillTool` PASS) green with OPENROUTER unset; the standalone snippet ran in 2.4s producing real prices; the paid re-run went GREEN (5 / 11.057s / today=true) with the artifact visually row-dumped.
- **Committed in:** `6a3c9d84`.

---

**Total deviations:** 1 (Rule 1 — fixture bug). **Impact on plan:** the repair UNBLOCKED the plan's Task-3 checkpoint; no scope creep, no gate weakening. The metric-substitution (D-03: ledger end-event count + wall-clock, NOT distinct request_id) was already baked into the gate from 18-01 and is honored unchanged.

## Issues Encountered

- **186s network hang on a first aborted attempt** (before the two completed runs): the live data fetch path hung ~186s before timing out. It did NOT recur on either completed run. **Backlog candidate:** a tighter per-request HTTP timeout + 1 retry in the live data path. Not a blocker, not in scope for a measurement-only fixture repair.
- **Two-run paid cost note:** the gate cost two paid runs total this session — Run 1 surfaced the fixture defect (today=false, the model's recovery), Run 2 (after the fix) is the steady-state row of record. The operator pre-authorized the single re-run; the fixture-defect run was the diagnostic that justified it.

## Quality + time scores (operator ask)

| Dimension | Steady state (Run 2) | D-03 authoring baseline | Authoring 143s baseline note |
|---|---|---|---|
| Tool dispatches (`event_kind='end'`) | **5** (budget ≤6) | 21 | ≈4.2× collapse |
| Wall-clock | **11.057s** (budget <40s) | 142.8s | ≈12.9× collapse |
| LLM roundtrips (diagnostic) | 5 (4 dispatching + 1 reply) | ≈19 | ≈3.8× collapse |
| Artifact (fresh/opens/today) | true/true/true | true/true/true | parity, via the snippet's own output |

The ~5-call / <40s steady-state intuition (D-03) is confirmed empirically: 5 dispatches / 11.057s, comfortably inside both grounded budgets.

## Coverage + mutation note

The quality-snapshot coverage (`scripts/coverage_gate.sh`) + new-handler mutation (`go-mutesting` on `Writer.Restore`, `actionRestore/actionArchive/actionSaveSnippet`, `SnippetHostPath`) cells remain `pending-operator-run` — those are a separate WSL `make quality-full` / `go-mutesting` stack operation, outside the scope of this fixture-repair + paid-gate close-out. The new handlers are unit/db-covered in 18-02/18-03; this entry records only the live steady-state gate the fixture repair unblocked.

## Known Stubs

None — the fixture stub was the defect this plan-04 close-out REPAIRED. The pre-seeded snippet now fetches live data and writes a real dated workbook; no placeholder/empty-value flows remain in the reuse path.

## Threat Surface

No new security-relevant surface. The snippet's blocklist scan runs on the new code at save time (benign by construction); T-18-11-I (no OPENROUTER key in the report) holds — the snapshot records only dispatch counts + durations, never args/secrets. yfinance is a HOST operator/fat-image prerequisite (T-18-SC accept: no Go-module install), not a new package dependency.

## Self-Check: PASSED

- Modified files verified present: `internal/eval/skills_snippet_reuse_cot_eval_test.go`, `docs/aura-quality-snapshot.md`, `18-04-SUMMARY.md`.
- Fixture-fix commit verified in git log: `6a3c9d84`.
- Paid gate verified GREEN: `TestSnippetReuseE2E` PASS (endEvents=5 / wallClock=11.057s / today=true); artifact `Mercato_Yahoo_2026-06-06.xlsx` row-dumped (10 tickers, real prices, date cell).
