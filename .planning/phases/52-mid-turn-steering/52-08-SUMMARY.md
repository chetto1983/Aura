---
phase: 52-mid-turn-steering
plan: 08
subsystem: validation
tags: [gate-3, live-e2e, playwright, resume-01, steer, coverage, mutation, quality-snapshot]

# Dependency graph
requires:
  - phase: 52-mid-turn-steering (plan 03)
    provides: "RESUME-01's three folded defects (empty-accept refusal, per-pause decision policy, pending-approval TTL sweep)"
  - phase: 52-mid-turn-steering (plan 04)
    provides: "the cockpit steer route, the aura.steer echo frame, drain-time persistence"
  - phase: 52-mid-turn-steering (plan 05)
    provides: "auto_delivery_next_turn and the 410-on-terminal-run refusal"
  - phase: 52-mid-turn-steering (plan 06)
    provides: "Telegram steer/queue wiring (unit-proven, not live-corroborated by this plan)"
  - phase: 52-mid-turn-steering (plan 07)
    provides: "the cockpit composer's dedicated 'Redirect the current turn' control and its i18n strings"
provides:
  - ".planning/phases/52-mid-turn-steering/52-VALIDATION.md — the phase's live evidence record, scored 9.0/10"
  - "A real bug fix: internal/agui/approvals_api.go now maps askuser.ErrInvalidAnswer to 400 on POST /api/approvals/{token}/resolve (was a generic 500)"
  - "prd.md Amendment #146 — the composer contract is a dedicated control, correcting 52-07's own truth #1 wording"
  - "internal/steer/inbox.go mutation score closed from 52% to 100% via two new literal-constant boundary tests"
  - "docs/aura-quality-snapshot.md's AG-UI gateway row re-attested with today's live-measured figures"
affects: []

actuals:
  tokens: 210000
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Throwaway Playwright spec (web/e2e/_tmp-52-08-steer-live.spec.ts, written, run, then deleted — never committed) as the sanctioned way to get genuine browser-level live evidence for a validation-only plan without adding a permanent test the codebase doesn't otherwise want."
    - "Throwaway git worktree (per CLAUDE.md's own go-mutesting rule) reused across all three required mutation targets, removed after each session."
    - "A hardcoded LITERAL constant in a boundary test, independent of the production symbol it's asserting against, is required to kill an off-by-one mutant on a package-level default constant — reading the same mutated symbol the production code uses moves both in lock-step and the mutant survives."
    - "RLS-scoped psql queries (SET app.current_identity = '<uuid>'; ...) as the direct-evidence path into aura.conversation_turns / aura.paused_states, since aura_app enforces owner-isolation via RLS even for a superficially simple SELECT."

key-files:
  created:
    - .planning/phases/52-mid-turn-steering/52-VALIDATION.md
  modified:
    - internal/agui/approvals_api.go
    - internal/agui/approvals_api_unit_test.go
    - internal/steer/inbox_test.go
    - prd.md
    - docs/aura-quality-snapshot.md
    - .planning/REQUIREMENTS.md
    - .planning/ROADMAP.md

key-decisions:
  - "Scored the phase 9.0/10 against the five ROADMAP Success Criteria (2.0 points each: SC#1-#4 full marks, SC#5 half marks for the cockpit leg only) rather than rounding up or averaging away the missing Telegram leg. Per the plan's own explicit rule, a score below 9.8 means the phase does not close, and this document says so in its own Sign-Off section rather than leaving it to be inferred."
  - "Added a throwaway, uncommitted Playwright spec to get genuine browser-level (not just curl/backend) evidence for SC#1/#2's cockpit leg, reusing the gotoAuthenticated/sameOriginFetch/agent-run-probe patterns already established in web/e2e/chat-real-agent.spec.ts. This goes beyond what the plan's own action text required (curl-driven A/B was sufficient per the acceptance criteria) but directly strengthens T-52-50's mitigation (a phase sealed on a green suite with the live behaviour never observed) by proving the actual composer button, not just the HTTP contract behind it. Deleted after use; not committed."
  - "Recorded a genuine container-freshness gap honestly rather than silently working around it: the earliest backend A/B/leftover/terminal-refusal tests ran against a container incarnation whose exact build commit was not verified before those tests began, a process miss against the plan's own precondition. Documented in 52-VALIDATION.md with the reasoning for why it does not invalidate those specific observations (the RESUME-01 500 found in that exact window is itself evidence the pre-fix code was genuinely running), rather than quietly re-running everything to erase the gap from the record."
  - "Fixed the RESUME-01 500-vs-400 bug found in internal/agui/approvals_api.go on touch (CLAUDE.md 'NOT MY WORK... fix on touch, never skip'), since it was discovered by doing exactly what Task 1 asked (exercising the empty-answer refusal live) rather than by unrelated exploration — squarely a Rule 1 bug, not a scope violation."
  - "Kept STEER-05's REQUIREMENTS.md checkbox checked ([x]) rather than un-checking a prior plan's completion, but downgraded its traceability-table status to 'Partial' with the live-vs-unit-only distinction spelled out — following the RESUME-01/STEER-01..06 convention already established in this file (the checkbox tracks 'behavior is implemented correctly', the traceability table tracks 'live Gate-3 closure')."

patterns-established:
  - "A live-E2E validation plan's SUMMARY inherits the coverage table shape from feature-plan SUMMARYs (id/description/requirement/verification/human_judgment), but each row's verification kind is 'other' pointing at a database query, an SSE frame, or an HTTP status observed live — never 'unit' or 'integration' — since a validation plan's whole point is that a green suite is not the evidence."

requirements-completed: [STEER-02, STEER-04, STEER-06, RESUME-01]

coverage:
  - id: SC1
    description: "Typing a redirect while Aura is mid-task changes what she does next, live, with no tool killed mid-execution"
    requirement: STEER-01
    verification:
      - kind: other
        ref: "aura.conversation_turns for 01a03b44-4ac6-...-9abc0ac26 (curl) and 01a03bca-06ca-...-4ea980faad43 (live Playwright browser session): shell_exec completed (exit_code=0, timed_out=false) before either steer landed; the redirected web_search query and final answer both reflect Bitcoin, not the original weather request"
        status: pass
      - kind: other
        ref: "live browser session: STEER_HTTP_STATUS 202, NOTICE_TEXT 'Message sent — the turn was redirected.', RELOAD_SHOWS_STEER_TEXT: true"
        status: pass
    human_judgment: true
    rationale: "Live evidence read from the running stack and a real browser session, not a test name; a human should independently confirm by repeating the composer interaction described in 52-VALIDATION.md."
  - id: SC2
    description: "The steer appears in the persisted conversation at the point it actually landed, not appended at the end"
    requirement: STEER-03
    verification:
      - kind: other
        ref: "aura.conversation_turns seq positions (steer at seq 6 of 9 and seq 4 of 9 in two independent conversations, never the last row); resume_before.sse / resume_after.sse Last-Event-ID proof"
        status: pass
    human_judgment: false
  - id: SC3
    description: "Steering a run that has just finished is delivered automatically as the next user turn, preceded by a visible line, never silently swallowed"
    requirement: STEER-04
    verification:
      - kind: other
        ref: "SELECT count(*) FROM aura.conversation_turns WHERE conversation_id = '01a03b44-4c68-...-823c35feaa88' AND content LIKE 'The previous turn ended before this message could be delivered%' → 1"
        status: pass
    human_judgment: false
  - id: SC4
    description: "A steered turn consumes no more steps or wallclock than an unsteered one (D-13 A/B, judged on ceiling+deadline, consumption as corroboration)"
    requirement: STEER-02
    verification:
      - kind: other
        ref: "AURA_LOOP_MAX_STEPS=25 / AURA_LOOP_MAX_WALLCLOCK_SEC=300 unmoved (process-wide constants, never mutated by llm_agent_steer.go); baseline 4 rounds/3 tool calls/66s vs steered 4 rounds/3 tool calls/59s"
        status: pass
    human_judgment: false
  - id: SC5
    description: "The same steer works from a channel, not only the cockpit"
    requirement: STEER-05
    verification:
      - kind: other
        ref: "cockpit leg: see SC1. Telegram leg: NOT exercised — no scriptable Telegram session in this environment (long-polling bot, no local-bot-api sidecar, no Telethon/API_ID/API_HASH)"
        status: partial
    human_judgment: true
    rationale: "Half-credited (1.0/2.0) in 52-VALIDATION.md's score. Closing this requires a human sending a real Telegram message during a live run."
  - id: RESUME01-400
    description: "An accept carrying no answer against the real cockpit resolve route returns 400, not 500"
    requirement: RESUME-01
    verification:
      - kind: other
        ref: "POST /api/approvals/{token}/resolve {\"action\":\"accept\",\"content\":\"\"} → 500 before commit 99c07b5ba, 400 after (same token re-attempted, rebuilt image)"
        status: pass
    human_judgment: false
  - id: RESUME01-403
    description: "A decision the pause's policy does not permit returns 403"
    requirement: RESUME-01
    verification:
      - kind: other
        ref: "seeded paused_states row 53e7339b-... with resume_context={\"allowed_decisions\":[\"accept\"]}; POST .../resolve {\"action\":\"decline\"} → 403 'approval decision not allowed'"
        status: pass
    human_judgment: false
  - id: RESUME01-TTL
    description: "A pending approval left past the TTL resolves as an expiry, never as a yes"
    requirement: RESUME-01
    verification:
      - kind: other
        ref: "seeded paused_states row e1f12f8b-... (created_at = now() - 3 days); the real 60s sweep set resumed_answer={\"action\":\"expired\",...} without any config override or restart; a subsequent resolve attempt returned 410"
        status: pass
    human_judgment: false
  - id: STEER06
    description: "The PRD amendment ratifying mid-turn steering is committed before any of its code"
    requirement: STEER-06
    verification:
      - kind: other
        ref: "git log: amendment commit 9b783bd54 (2026-08-25 08:05:15+02:00) predates the first steering code commit 43c9cb5cf (2026-08-25 15:41:27+02:00) by over 7 hours"
        status: pass
    human_judgment: false

duration: ~2h 46min measured from the first live E2E test (conversation created 2026-08-26T01:31:59+02:00) to the final commit (fad03d7bc 2026-08-26T04:17:34+02:00)
completed: 2026-08-26
status: complete
---

# Phase 52 Plan 08: Gate 3 — Mid-turn steering live E2E Summary

**A real multi-step scenario driven three times against the running stack (backend curl, live Playwright browser, and seeded-but-real database rows) proves SC#1-#4 and RESUME-01 with database-level and browser-level evidence — including a real RESUME-01 bug found and fixed — while honestly scoring the phase 9.0/10 because SC#5's Telegram leg could not be live-corroborated in this environment.**

## Performance

- **Duration:** ~2h 46min (see frontmatter `duration`)
- **Started:** 2026-08-26T01:31:59+02:00 (first live conversation created)
- **Completed:** 2026-08-26T04:17:34+02:00 (final commit)
- **Tasks:** 2/2
- **Files modified:** 7 (1 created, 6 modified); 0 stray untracked files left behind

## Accomplishments

- Drove ONE real multi-step scenario (`shell_exec` → `web_search` → one-sentence answer) three
  times: unsteered baseline, cockpit-steered via curl, and cockpit-steered via a genuine Playwright
  browser session against the running stack (typed into the real composer, clicked the real
  "Redirect the current turn" button) — comparing all three on `aura.conversation_turns` and
  `aura.tool_invocations` per the plan's own instruction, never on test output.
- Proved the D-13 A/B with the reading rule stated before the numbers: `AURA_LOOP_MAX_STEPS=25` /
  `AURA_LOOP_MAX_WALLCLOCK_SEC=300` are process-wide constants unmoved by the steer drain code, and
  consumption (4 rounds / 3 tool calls in both runs) was identical — STEER-02 met on the gate, not
  argued from the corroboration.
- Proved the leftover auto-delivery case with a `COUNT(*)` query returning exactly `1` (the T-52-58
  double-write guard), and the exact `steerAutoDeliveryNotice` byte-string in the persisted row.
- Exercised RESUME-01's three folded defects live and **found a real bug**: `POST
  /api/approvals/{token}/resolve` returned 500 instead of 400 for an empty-content accept, because
  `handleResolveApproval` never mapped `askuser.ErrInvalidAnswer` the way the sibling AG-UI-native
  resume path already did. Fixed, tested, rebuilt, re-verified live (500 → 400).
- Closed `internal/steer/inbox.go`'s mutation gap from 52% to 100% (25/25 killed) by adding two
  hardcoded-literal boundary tests that don't move in lock-step with the mutated production default
  constants — `internal/agent/llm_agent_steer.go` was already 100% (34/34); `bot_dispatch_queue.go`
  landed at 74.07% (20/27), above the 70% floor, accepted as-is.
- Re-measured the full gate matrix on this tree: Go race suite 73 packages/0 failures, `govulncheck`
  clean, owned-surface coverage 86.3% (27069/31384), frontend vitest coverage 91.16%/85.19%/90.41%/
  92.96% (stmt/branch/fn/line), Stryker mutation 74.64% (pre-existing baseline), and a fresh
  byte-for-byte `internal/webui/dist` freshness diff (zero differences) via the Docker webbuild
  stage.
- Re-attested `docs/aura-quality-snapshot.md`'s AG-UI gateway row (the only row whose CI-gate-path
  glob matched this phase's changed files) with today's date and the fresh 85.5%/86.3% figures.
- Corrected 52-07's own truth #1 wording via PRD Amendment #146: the composer contract is satisfied
  by a dedicated "Redirect the current turn" control, not by an un-disabled Send — documented rather
  than left as an apparent unmet must-have.
- Wrote `.planning/phases/52-mid-turn-steering/52-VALIDATION.md`, the phase's first and only
  validation artifact, scoring it 9.0/10 against the five ROADMAP Success Criteria and stating
  plainly that the phase does not close this session, with the exact residual gap named (SC#5's
  Telegram leg) and what would close it (one human-in-the-loop Telegram check).

## Task Commits

1. **Task 1 (live E2E) — RESUME-01 bug fix**: `99c07b5ba` (fix) — found while exercising the
   empty-answer refusal live
2. **Task 1 (live E2E) — PRD amendment**: `f22a22207` (docs) — carried-forward composer-contract
   correction from 52-07
3. **Task 2 (gate re-measurement) — mutation gap closure**: `abe5db745` (test)
4. **Task 2 (gate re-measurement) — quality snapshot re-attestation**: `bbbcea04d` (docs)
5. **Task 1+2 (evidence record + requirement/roadmap closure)**: `fad03d7bc` (docs)

_Note: this SUMMARY's own metadata commit follows per the executor's final_commit step._

## Files Created/Modified

- `.planning/phases/52-mid-turn-steering/52-VALIDATION.md` (new) — the phase's live evidence record
- `internal/agui/approvals_api.go` (328 LOC) — the RESUME-01 500→400 fix (`ErrInvalidAnswer` branch)
- `internal/agui/approvals_api_unit_test.go` (365 LOC) — `TestApprovalsAPI_ResolveInvalidAnswerReturns400`
- `internal/steer/inbox_test.go` (354 LOC) — `TestNewResolvesNonPositiveCapsToPackageDefaults` (7 subtests, closes the mutation gap)
- `prd.md` — Amendment #146 (composer contract is a dedicated control)
- `docs/aura-quality-snapshot.md` — AG-UI gateway row re-attested
- `.planning/REQUIREMENTS.md` — STEER-02/04/05/06 and RESUME-01 checkboxes and traceability rows updated to match live evidence
- `.planning/ROADMAP.md` — Phase 52 plan count (8/8), Wave 5 checkbox, and phase-line score (9.0/10, does not close)
- `web/e2e/_tmp-52-08-steer-live.spec.ts` (created, run, then deleted — not committed) — throwaway browser-level evidence gathering

## Decisions Made

See `key-decisions` in the frontmatter for: the 9.0/10 scoring methodology; the throwaway
Playwright browser session added beyond the plan's minimum bar; the honestly-recorded
container-freshness gap for the earliest backend tests; the RESUME-01 bug fix on touch; and the
STEER-05 checkbox/traceability-table convention.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `POST /api/approvals/{token}/resolve` returned 500 instead of 400 for an empty-content accept**
- **Found during:** Task 1, exercising RESUME-01's empty-answer refusal live
- **Issue:** `handleResolveApproval` in `internal/agui/approvals_api.go` mapped `ErrPauseExpired`/`ErrPauseNotFound`/`ErrResumeDecisionNotAllowed` explicitly but fell through to a generic 500 for `askuser.ErrInvalidAnswer`, unlike the sibling AG-UI-native `/agent/run` resume path which already returned 400 for the same sentinel.
- **Fix:** Added the missing `errors.Is(err, askuser.ErrInvalidAnswer)` branch mapping to 400.
- **Files modified:** `internal/agui/approvals_api.go`, `internal/agui/approvals_api_unit_test.go`
- **Verification:** `go test ./internal/agui/ -run TestApprovalsAPI -v` green; re-verified live against the rebuilt image (500 before, 400 after, same seeded token).
- **Commit:** `99c07b5ba`

**2. [Rule 2 - Missing Critical] `internal/steer/inbox.go`'s zero/negative-cap fallback branch had no test coverage**
- **Found during:** Task 2, the mandatory Go mutation spot-check
- **Issue:** All 12 initially-surviving mutants (52% killed) were in `New()`'s cap-resolution branch and the `defaultMax=32`/`defaultMaxBytes=32768` constants; every existing test passed explicit non-zero `Config` values, so the fallback path and its exact literal defaults were never asserted.
- **Fix:** Added `TestNewResolvesNonPositiveCapsToPackageDefaults` (zero-Config, Max=1/MaxBytes=1 boundaries, negative caps) plus two subtests asserting hardcoded LITERAL values (32, 32768) independent of the production symbol — reading the same mutated symbol the production code uses moves both in lock-step and can't distinguish an off-by-one.
- **Files modified:** `internal/steer/inbox_test.go`
- **Verification:** go-mutesting score 52% → 84% → 100% (25/25 killed), run in a throwaway worktree.
- **Commit:** `abe5db745`

### Notable Additions (beyond the plan's minimum bar)

**3. [Not a deviation from a defect — a strengthening of the evidence] A throwaway Playwright browser session for SC#1/#2's cockpit leg**
- The plan's acceptance criteria only required the backend-level curl proof (HTTP status, `aura.steer` frame, DB position). A genuine browser session was added to prove the actual composer UI (textbox accepting the redirect while a tool ran, the dedicated "Redirect the current turn" button, the rendered notice, and a page reload) — closer to T-52-50's own stated threat (a phase sealed on a green suite with the live behaviour never observed).
- **Files:** `web/e2e/_tmp-52-08-steer-live.spec.ts` (deleted after use, never committed — not a permanent test, per this plan's evidence-gathering purpose only).

**4. [PRD amendment carried forward from 52-07]** — Amendment #146: the composer contract is satisfied by a dedicated "Redirect the current turn" control, not by an un-disabled `Send`, correcting 52-07-PLAN.md's own truth #1 wording (measured-false: `@assistant-ui/react`'s native `Send` stays disabled under Aura's legacy `useExternalStoreRuntime`, which doesn't implement `capabilities.queue`). Committed as `f22a22207`, separately from the code commit per PRD-amendment-first discipline.

**Total deviations:** 2 auto-fixed (1 bug, 1 missing-critical test coverage), 1 evidence-strengthening addition beyond the plan's minimum, 1 carried-forward PRD amendment.
**Impact on plan:** The bug fix and mutation-gap closure were both discovered by doing exactly what Task 1/Task 2 asked, not by unrelated exploration. No scope creep; no architectural change.

## Issues Encountered

- **SC#5's Telegram leg is not live-proven this session.** The bot uses long-polling `getUpdates`
  against the real Telegram API; there is no local-bot-api sidecar, no Telethon/Pyrogram session,
  and no `API_ID`/`API_HASH` configured, so there is no way to script an inbound message as a real
  Telegram user from this environment. This is the single reason the phase scores 9.0/10 instead of
  10/10, and it is a structural/environmental gap, not a code defect — the wiring itself is
  unit-proven by 52-06 with a discriminating test (`TestOneInboxServesBothSurfaces`).
- **The `user_message_fallback` steer-delivery branch was not exercised live.** Every attempt to
  land a steer while the conversation tail was not a tool result raced against drain-point A and
  lost (landing as `auto_delivery_next_turn` instead), because drain-A fires essentially
  synchronously at round start. Recorded as a finding in `52-VALIDATION.md`, matching 52-04's own
  prior observation that this branch has only ever been proven against a fake LLM client.
- **A pre-existing flaky test was found, not fixed.** A full-suite vitest run showed one failure in
  `AppShell.shell.test.tsx`; isolated re-run passed 17/17, and a fresh full-suite re-run for this
  plan's own coverage measurement passed cleanly (exit 0). Unrelated to any file this plan touched;
  per the deviation rules' scope boundary, not auto-fixed.
- **A genuine container-freshness process gap, honestly recorded rather than silently corrected.**
  The earliest backend tests in this session ran against a container incarnation whose exact build
  commit was not verified before those tests began. Documented in full in `52-VALIDATION.md`'s
  "Container freshness" section, along with why it does not invalidate those specific observations.

## User Setup Required

None — no external service configuration required. Closing SC#5 in full requires a human operator
to send one real Telegram message (and ideally a photo, and a `/cancel`) to the bot during a live
turn; no setup beyond having the bot's Telegram chat already configured (it already is, in
production use).

## Next Phase Readiness

- Phase 52 does NOT close per CLAUDE.md's `>9.8` Definition-of-Done bar. `.planning/ROADMAP.md`'s
  Phase 52 line and `52-VALIDATION.md`'s own Sign-Off section both say so explicitly, with the exact
  residual gap named (SC#5's Telegram leg) and what would close it.
- Recommended next action, when a human is available: repeat the live scenario from a real Telegram
  client (plain-text redirect, a photo during a live turn, and a `/cancel`), then update only
  `52-VALIDATION.md`'s Success-Criteria section and score — the gate/coverage numbers in this
  document do not need to be re-measured for that follow-up.
- `internal/agui/approvals_api.go` is now at 328/600 LOC — ample headroom for the next touch.
- The re-attested `docs/aura-quality-snapshot.md` AG-UI gateway row carries today's figures; the
  next phase touching `internal/agui/**` should re-measure rather than trust this row's date
  indefinitely.

## Self-Check: PASSED

- FOUND: .planning/phases/52-mid-turn-steering/52-VALIDATION.md
- FOUND: internal/agui/approvals_api.go
- FOUND: internal/agui/approvals_api_unit_test.go
- FOUND: internal/steer/inbox_test.go
- FOUND: prd.md
- FOUND: docs/aura-quality-snapshot.md
- FOUND: commit 99c07b5ba
- FOUND: commit f22a22207
- FOUND: commit abe5db745
- FOUND: commit bbbcea04d
- FOUND: commit fad03d7bc

---
*Phase: 52-mid-turn-steering*
*Completed: 2026-08-26*
