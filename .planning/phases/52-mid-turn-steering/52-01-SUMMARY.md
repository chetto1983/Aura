---
phase: 52-mid-turn-steering
plan: 01
subsystem: config
tags: [prd-amendment, config, steering, resume, dark-code, envutil]

requires: []
provides:
  - "PRD amendment #142: five corrections to amendment #132 (auto-delivery, two drain points, nonce marker + placement, AURA_AGUI_RUN_STEER default true + in-phase composer contract, D-05's missing media queue), each with hermes/live-tree citations and a 'what this does NOT prove' paragraph"
  - "REQUIREMENTS.md/ROADMAP.md realigned to the corrected contract (auto-delivery wording, Depends-on fix, RESUME-01 minted and marked Complete)"
  - "Amendment #133 re-pointed from the deleted Phase 47 to Phase 52/RESUME-01"
  - "internal/config/config_agui_steer.go: AGUISteerConfig{Enabled,Max,MaxBytes} + loadAGUISteerConfig(), wired into Config.AGUISteer"
  - "config_knobs.go: three new AURA_AGUI_RUN_STEER* catalogue rows, D-11/D-12 comments naming the decisions"
  - "TestEveryNewKnobIsCatalogued: a catalogue-vs-loader consistency tripwire covering the four new knobs plus AURA_AGUI_RUN_DETACH"
affects: [52-02, 52-04, 52-05, 52-06, 52-07, 52-08]

actuals:
  tokens: 8221
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Config sub-struct + non-fatal envutil fallback + malformed-falls-back test triple (config_agui_run.go's shape, cloned for config_agui_steer.go)"
    - "Catalogue-vs-loader consistency test as a standing tripwire against a second D-11-shaped drift"

key-files:
  created:
    - internal/config/config_agui_steer.go
    - internal/config/config_agui_steer_test.go
  modified:
    - prd.md
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md
    - internal/config/config.go
    - internal/config/config_knobs.go

key-decisions:
  - "Next PRD amendment number is #142, not #141 — #141 was claimed mid-session by an unrelated concurrent commit (42711b508, a different Codex-authored session correcting the document-pipeline gate) that landed between the plan's own reading pass and the first edit. Recomputed live per <no_stale_inputs>, not trusted from any prior reading."
  - "AURA_ASKUSER_PAUSE_TTL_SEC / RESUME-01's TTL half was already shipped by plan 52-03's out-of-order execution (commits 0d5f48dfd/6d1ec3859, landed before this plan ran despite 52-01 being Wave 1). Not re-implemented; RESUME-01 is minted here and marked Complete to reflect the accurate combined state, since all three folded defects (empty-answer refusal, per-tool decision policy, pending-approval TTL) are independently closed as of 2026-08-25."
  - "AURA_AGUI_RUN_DETACH's catalogue default was also already corrected to true by 52-03's commit 0d5f48dfd (an in-scope Rule-1 fix that executor made while touching config_knobs.go for the TTL row). This plan adds only the missing D-11-naming comment and the TestEveryNewKnobIsCatalogued tripwire the acceptance criteria required."
  - "The five corrections live as a new sibling amendment (#142) referencing #132 by number, PLUS in-place strikethrough+replacement edits inside #132's own text at the three sentences that asserted the superseded behaviour (item B's terminal-event-only delivery, the singular drain point, and item 10/12's default-false + deferred-composer clauses) — matching the must_haves truths requirement literally, not just the automated grep checks."

patterns-established:
  - "A catalogued knob's Default string is round-tripped against the loader's actual applied default in a dedicated test (TestEveryNewKnobIsCatalogued), not just asserted once at knob-creation time — the pattern any future D-11-shaped drift should be caught by."

requirements-completed: [STEER-06, RESUME-01]

coverage:
  - id: D1
    description: "PRD amendment #142 lands the five hermes/live-tree corrections to #132, with citations and a 'what this does NOT prove' paragraph, committed before any steering implementation code exists in this phase"
    requirement: "STEER-06"
    verification:
      - kind: other
        ref: "git log --oneline order (044cbf8f3 precedes 43c9cb5cf; no internal/steer/*, llm_agent_steer.go, server_run_steer.go, runner_steer.go, bot_dispatch_steer.go or steerRun.ts commits exist yet)"
        status: pass
      - kind: other
        ref: "plan Task 1 <verify><automated> bash block (amendment-number formula, phrase greps, awk-scoped #133/RESUME-01 checks, ROADMAP Depends-on/Requirements-line checks)"
        status: pass
    human_judgment: false
  - id: D2
    description: "REQUIREMENTS.md and ROADMAP.md no longer assert the superseded 'operator re-sends' wording; RESUME-01 is minted with a bullet and a traceability row"
    requirement: "RESUME-01"
    verification:
      - kind: other
        ref: "plan Task 1 <verify><automated> bash block (grep -c RESUME-01 >= 2, Requirements-line contains RESUME-01, no re-send phrase left)"
        status: pass
    human_judgment: false
  - id: D3
    description: "internal/config/config_agui_steer.go ships AGUISteerConfig with Enabled default true, Max=8, MaxBytes=16384, all non-fatal envutil fallbacks; wired into Config.AGUISteer"
    verification:
      - kind: unit
        ref: "internal/config/config_agui_steer_test.go#TestAGUISteerConfigDefaultsAndOverrides"
        status: pass
      - kind: unit
        ref: "internal/config/config_agui_steer_test.go#TestEveryNewKnobIsCatalogued"
        status: pass
    human_judgment: false
  - id: D4
    description: "No env default is stated two different ways anywhere in the tree, and a test now enforces that for AURA_AGUI_RUN_STEER*, AURA_ASKUSER_PAUSE_TTL_SEC and AURA_AGUI_RUN_DETACH"
    verification:
      - kind: unit
        ref: "internal/config/config_agui_steer_test.go#TestEveryNewKnobIsCatalogued"
        status: pass
      - kind: other
        ref: "for k in AURA_AGUI_RUN_STEER AURA_AGUI_RUN_STEER_MAX AURA_AGUI_RUN_STEER_MAX_BYTES AURA_ASKUSER_PAUSE_TTL_SEC; do grep -q \"$k\" internal/config/config_knobs.go && grep -q \"$k\" prd.md; done"
        status: pass
    human_judgment: false

duration: 30min
completed: 2026-08-25
status: complete
---

# Phase 52 Plan 01: Amendment gate + steer/pause-TTL knob surface Summary

**PRD amendment #142 lands five hermes-and-live-tree corrections to amendment #132 (auto-delivery, two drain points, a nonce-based steer marker with two placement clauses, `AURA_AGUI_RUN_STEER` default `true` with the composer contract in-phase, and D-05's missing Telegram media queue) before any steering code exists, and ships the `AGUISteerConfig` knob bundle plus a catalogue-vs-loader consistency tripwire.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-08-25 (session start, reading pass)
- **Completed:** 2026-08-25T15:41:27+02:00 (last commit)
- **Tasks:** 2
- **Files modified:** 5 modified, 2 created

## Accomplishments

- Amendment #142 (new sibling section) carries all five corrections found by re-reading hermes and the live tree during `/gsd-discuss-phase 52`, each with file:line evidence and an explicit "what this does NOT prove" paragraph, matching #132/#133's own convention.
- Amendment #132's own text is edited in place at the three sentences that asserted the superseded behaviour — struck through, replacement beside it, pointing at #142 — not just superseded by a separate section a reader might miss.
- Amendment #133 is re-pointed from the deleted Phase 47 to Phase 52/`RESUME-01` with a dated line, without rewriting its findings.
- REQUIREMENTS.md STEER-04 and ROADMAP Phase 52 SC#3 both now read "delivered automatically as the next user turn, preceded by a visible line saying that happened" instead of "returned/returns to the operator to re-send."
- ROADMAP Phase 52's `Depends on:` line no longer claims Phase 51; it states the actual execution order and why the original reasoning inverted.
- `RESUME-01` is minted in REQUIREMENTS.md (bullet + traceability row) and added to ROADMAP Phase 52's `**Requirements**:` line, marked Complete — all three folded amendment-#133 defects are independently closed as of 2026-08-25.
- `internal/config/config_agui_steer.go` ships `AGUISteerConfig{Enabled, Max, MaxBytes}` and `loadAGUISteerConfig()`, wired into `Config.AGUISteer` via `loadBase()`. `Enabled` defaults `true` (D-12).
- `config_knobs.go` gains the three `AURA_AGUI_RUN_STEER*` rows plus D-11/D-12-naming comments; prd.md's env catalogue gains the matching three rows.
- `TestEveryNewKnobIsCatalogued` is a standing tripwire: it fails if any cataloged knob's `Default` string diverges from what the loader actually applies, covering the four new knobs and `AURA_AGUI_RUN_DETACH`.

## Task Commits

Each task was committed atomically:

1. **Task 1: Land the five corrections to amendment #132, move the three documents, mint RESUME-01** — `044cbf8f3` (docs)
2. **Task 2: The knob surface — steer knobs + D-11 reconcile** — `43c9cb5cf` (feat)

_No plan-metadata commit is separate from this SUMMARY's own commit (below)._

## Files Created/Modified

- `prd.md` — Amendment #142 (new section) + in-place strikethrough corrections inside §132 + a re-pointing line inside §133 + three new env-catalogue rows
- `.planning/REQUIREMENTS.md` — STEER-04 auto-delivery wording, RESUME-01 bullet + traceability row
- `.planning/ROADMAP.md` — Phase 52 SC#3 auto-delivery wording, `Depends on:` correction, `RESUME-01` added to the Requirements line
- `internal/config/config.go` — `Config.AGUISteer AGUISteerConfig` field + `loadBase()` wiring
- `internal/config/config_knobs.go` — three new `AURA_AGUI_RUN_STEER*` rows + D-11/D-12 comments
- `internal/config/config_agui_steer.go` (new) — `AGUISteerConfig`, `loadAGUISteerConfig()`
- `internal/config/config_agui_steer_test.go` (new) — `TestAGUISteerConfigDefaultsAndOverrides`, `TestEveryNewKnobIsCatalogued`

## Decisions Made

- **Amendment number resolved live as #142, not #141.** `<no_stale_inputs>` warned every number must be measured at execution time. A first read found max amendment #140 (so #141), but before the first edit landed, an unrelated concurrent commit (`42711b508`, a different session correcting the document-pipeline production gate) claimed `#141` for an unrelated topic. Recomputed immediately before writing: `#142` is the true next-free number, verified with the exact `<no_stale_inputs>` formula against the post-collision tree.
- **`AURA_ASKUSER_PAUSE_TTL_SEC` default is `172800` seconds (48 hours)** — chosen by plan 52-03 (which executed out of the plan's nominal wave order, landing before this plan ran) and unchanged here: long enough that an operator stepping away for a full working day does not find their approval silently expired, deliberately independent of `AURA_MCP_ELICITATION_TIMEOUT_SEC` (which bounds an in-flight protocol wait, not an async cross-turn ledger entry). `internal/config/config_askuser.go` and its test already existed and were not duplicated by this plan.
- **The auto-delivery wording landed** (for plans 52-05/52-08 to assert against): the stable, load-bearing phrase across all three documents is **"delivered automatically as the next user turn"**. prd.md's full sentence: *"a steer still queued when the run ends is delivered automatically as the next user turn, preceded by a visible line saying that is what happened."* REQUIREMENTS.md/ROADMAP.md use the same phrase with a trailing "...saying that happened" (no "is what"). Assert on the stable substring, not the full sentence, if matching across documents.
- **`RESUME-01` text as landed** (REQUIREMENTS.md bullet): *"The approval resume path refuses an accept carrying no answer, refuses a decision the pause's policy does not permit, and expires a pending approval as an expiry rather than as a yes — without weakening the `WHERE resumed_at IS NULL` conditional update that IS the idempotency key. Folded from PRD amendment #133 (Phase 47, deleted 2026-08-25); all three defects closed 2026-08-25 (empty-answer refusal, per-tool decision policy, pending-approval TTL — see `52-03-SUMMARY.md`)."* Marked `[x]` / Complete.
- **`AURA_AGUI_RUN_STEER_MAX`/`_MAX_BYTES` caps (8 / 16384) are read from config** (`AGUISteerConfig.Max`/`.MaxBytes`), never a literal in the not-yet-built `internal/steer` package — plan 52-02 must consume these fields, not hardcode the numbers. This plan does not implement or test the queue-push/refuse behavior itself (no `internal/steer` package exists yet); that is 52-02's `<behavior>` obligation.

## Deviations from Plan

### Informational (not Rule 1-4 auto-fixes — accurately reflecting a tree that moved since the plan was authored)

**1. Plan 52-03 executed out of order, ahead of this plan.** `<no_stale_inputs>`-class discovery: reading the tree before starting showed `internal/config/config_askuser.go`, `config_askuser_test.go`, the `AURA_ASKUSER_PAUSE_TTL_SEC` catalogue row (prd.md + config_knobs.go), and the `AURA_AGUI_RUN_DETACH` default correction (config_knobs.go) already committed via `0d5f48dfd`/`6d1ec3859` — plan 52-03's work — despite 52-03 being Wave 2 and this plan (52-01, Wave 1) supposed to mint `RESUME-01` first. Amendment #140 (pending-approval TTL) was likewise already in prd.md. Not re-implemented; this plan instead retroactively mints `RESUME-01` and marks it Complete, and adds only the D-11-naming comment plus `TestEveryNewKnobIsCatalogued` that acceptance criteria still required and that 52-03 had not added.
- **Found during:** Pre-Task-1 reading pass (required_reading + `<no_stale_inputs>` measurements)
- **Files affected:** none modified beyond what Task 1/Task 2 already covered
- **Verification:** `git log -S` confirmed `0d5f48dfd` is the commit that flipped `AURA_AGUI_RUN_DETACH`'s catalogue default; `internal/config/config_askuser.go` pre-existed and its test passed unmodified

**2. Amendment number collision from a concurrent session.** See "Decisions Made" above — resolved live per `<no_stale_inputs>`, no plan content weakened.
- **Found during:** Task 1, immediately before the final append edit
- **Files affected:** none — the correct number (#142) was used from the first write, no rework needed
- **Verification:** `grep -o 'Amendment #[0-9]*' prd.md | ... | tail -1` recomputed after the collision landed, confirmed #142 is unused before writing

---

**Total deviations:** 0 auto-fixed (Rules 1-4); 2 informational findings, both resolved by accurate measurement per `<no_stale_inputs>`, no plan content weakened or reworked.
**Impact on plan:** None on scope or correctness. Task 2's file list (`config_askuser.go`/`config_askuser_test.go`) is unmodified because that work was already correct and complete.

## Issues Encountered

None beyond the two informational findings above.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- The amendment gate is closed: `internal/steer`, `llm_agent_steer.go`, the AG-UI steer route, the Telegram steer branch and the web steer client may now be built against a contract that matches hermes and the live tree, with no known contradiction left unaddressed.
- `Config.AGUISteer` is ready for plan 52-02 to consume (`Enabled`/`Max`/`MaxBytes`), and `Config.AskUser.PauseTTLSec` is already live from 52-03.
- Plans 52-05 and 52-08 should assert against the "delivered automatically as the next user turn" substring landed in prd.md/REQUIREMENTS.md/ROADMAP.md, not a full-sentence match (the three documents phrase the trailing clause slightly differently).
- No blockers for Wave 2 (52-02, 52-03 already shipped).

## Self-Check: PASSED

- `internal/config/config_agui_steer.go` — FOUND
- `internal/config/config_agui_steer_test.go` — FOUND
- Commit `044cbf8f3` — FOUND in `git log --oneline --all`
- Commit `43c9cb5cf` — FOUND in `git log --oneline --all`
- `go vet ./...`, `go build ./...` — PASS
- `go test -race ./internal/config/...` (WSL) — PASS
- All Task 1 and Task 2 `<verify><automated>` bash blocks — PASS (re-run verbatim, see Decisions/Coverage above)

---
*Phase: 52-mid-turn-steering*
*Completed: 2026-08-25*
