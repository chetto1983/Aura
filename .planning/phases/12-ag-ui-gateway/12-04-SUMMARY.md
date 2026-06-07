---
phase: 12-ag-ui-gateway
plan: 04
subsystem: testing
tags: [ag-ui, sse, smoke, ci, db-integration, coverage, mutation, gate-3, reasoning, amendment-57]

# Dependency graph
requires:
  - phase: 12-01
    provides: "the pinned AG-UI SDK + the pure translator state machine + golden fixtures the db_integration tier and smoke exercise"
  - phase: 12-03
    provides: "internal/agui/server.go (POST /agent/run SSE + GET /threads/<id>/messages) + cmd/aura/serve.go http.Server daemon mount the smoke drives live"
  - phase: 12-05
    provides: "the llm+agent reasoning data-plane (Chunk.Reasoning / LLMResponse.Reasoning) that surfaces on the live REASONING_* leg"
  - phase: 12-06
    provides: "the translator REASONING_* lifecycle + the live 💭 CLI reasoning render the operator confirms"
provides:
  - "scripts/agui_smoke.sh: live curl SSE round-trip (POST /agent/run) + GET MESSAGES_SNAPSHOT + 404 chokepoint against a real aura serve daemon; two legs (DEGRADED dummy-key CI / LIVE AGUI_SMOKE_LIVE=1 REASONING_* hard-assert)"
  - "ci.yml: ./internal/agui/... wired into the integration-test db_integration package list (SC1/SC3, -p 1, CI=true, no-skip-as-green) + an 'AG-UI gateway live smoke (degraded leg)' step"
  - "internal/agui server.go 404 fix (malformed/non-UUID thread id → 404 at both handlers, not a leaked 500) + helpers_test/server_test coverage to 86.8%"
  - "docs/aura-quality-snapshot.md Phase-12 rows (live SSE+REASONING_* PASS, agui 86.8%, owned-surface 86.2%, translator.go mutation 76.2%, operator Gate-3 11/11)"
  - "12-VALIDATION.md Per-Task Verification Map fully green + wave_0_complete: true + the operator Gate-3 sign-off recorded"
affects: [12-verify, 13-telegram]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-leg live smoke: a DEGRADED leg (dummy key, CI step) proves the SSE pump/translator/daemon-mount wire key-free; a LIVE leg (AGUI_SMOKE_LIVE=1 + real OpenRouter key) hard-asserts the REASONING_* lifecycle interleaved before TEXT (amendment #57). FRAME ground truth asserted (event types), never prose or secrets (T-12-15)."
    - "No-skip-as-green smoke: under $CI the script exits non-zero when the DB env is unset — a skipped tier fails the gate (T-12-14); the agui db_integration tier round-trips 0.04–0.05s each (not a sub-ms skip tell)."
    - "Operator-delegated Gate-3 via an autonomous E2E loop scoring against frame/DB/snapshot ground truth (artifact-not-reply), not the model's prose."

key-files:
  created:
    - "scripts/agui_smoke.sh — live SSE round-trip + GET snapshot + 404 smoke; DEGRADED (CI) and LIVE (REASONING_*) legs; seeds a conversation key-free via docker exec psql; polls the port (no fixed sleep); prints ≥1 SSE frame body for visual inspection"
  modified:
    - ".github/workflows/ci.yml — ./internal/agui/... added to the integration-test db_integration package list + an AG-UI gateway live smoke (degraded leg) step"
    - "internal/agui/server.go — [Rule 1] uuid.Parse guard maps a malformed/non-UUID thread id to 404 at both handlers (was leaking the store parse error as a 500)"
    - "internal/agui/server_test.go + internal/agui/helpers_test.go — lock the 404 fix + cover IDGenerator/payloadString/resumeAnswers/lastUserMessage/bufferCap/SubmitAnswers/LoadHistory sanitized-error paths → internal/agui 86.8%"
    - "docs/aura-quality-snapshot.md — Phase-12 rows (coverage/mutation/smoke + operator Gate-3 11/11)"
    - ".planning/phases/12-ag-ui-gateway/12-VALIDATION.md — Status cells flipped green (incl. the final Operator-live-Gate-3 row), wave_0_complete: true, operator sign-off recorded"

key-decisions:
  - "[Rule 1] A malformed/non-UUID thread id now maps to 404 at BOTH handlers via a uuid.Parse guard before the store round-trip, instead of leaking the store's parse error as a 500 — caught live by the smoke's does-not-exist chokepoint (T-12-11)."
  - "The autonomous CI gate uses a FakeClient/degraded leg (fast db_integration tier); the live OpenRouter curl + REASONING_* lifecycle is the operator Gate-3 leg (key operator-gated per the prior-phase pattern)."
  - "translator.go mutation 76.2% (48/63 killed) advisory-accepted: the 15 survivors are near-equivalent (a sort.Strings removal on already-deterministic output + enum-build mutants in the ask_user schema helper pinned by the golden-shape tests) — project precedent (db.go 82.8% / budget.go 89.4%)."
  - "Operator delegated the live Gate-3 sign-off to an autonomous E2E loop ('do all E2E test in autonomy and loop until score is >95%'); the loop scored 11/11 (100%) in 3 iterations (2 driver-harness fixes, zero product defects) — sign-off accepted on frame/DB/snapshot ground truth."

patterns-established:
  - "Pattern: a two-leg live smoke (DEGRADED key-free CI leg + LIVE REASONING_* leg) is the AG-UI Gate-3 reference command; the fast db_integration tier is the autonomous CI gate."
  - "Pattern: Gate-3 sign-off may be operator-delegated to an autonomous E2E loop that asserts artifact ground truth (SSE frames, persisted DB rows, MESSAGES_SNAPSHOT), never the model's reply."

requirements-completed: [UX-01]

# Metrics
duration: ~2h (incl. checkpoint wait)
completed: 2026-06-07
---

# Phase 12 Plan 04: AG-UI Gateway Gate-3 Closure Summary

**Live AG-UI SSE smoke (POST /agent/run round-trip + GET MESSAGES_SNAPSHOT + 404 + REASONING_* live leg) wired into CI as a no-skip-as-green db_integration tier, with internal/agui at 86.8% coverage, translator.go mutation 76.2%, and an operator-delegated autonomous E2E Gate-3 sign-off scoring 11/11.**

## Performance

- **Duration:** ~2h (Task 1 execution + human-verify checkpoint wait + Task 2 close-out)
- **Started:** 2026-06-07 (Task 1 commit 1867c0c2 at 00:32 CEST)
- **Completed:** 2026-06-07
- **Tasks:** 2 (1 auto + 1 checkpoint:human-verify)
- **Files modified:** 7 (Task 1) + 3 docs (Task 2 close-out)

## Accomplishments

- `scripts/agui_smoke.sh` proves the live AG-UI SSE round-trip against a real `aura serve` daemon: builds aura, seeds a conversation key-free (docker exec psql), polls `127.0.0.1:9080`, POSTs a RunAgentInput, and asserts FRAME ground truth (`event: RUN_STARTED` … `event: RUN_FINISHED`, plus `REASONING_START` … `REASONING_END` before the first `TEXT_MESSAGE_START` on the live leg — amendment #57); then GETs the MESSAGES_SNAPSHOT and asserts a 404 on an unknown id.
- The `internal/agui` db_integration tier now RUNS in CI (`./internal/agui/...` added to the `go test -tags db_integration -race -p 1` package list, CI=true armed, 0.04–0.05s round-trips — not a skip tell), plus a degraded-leg smoke CI step.
- A Rule-1 bug surfaced live and was fixed: a malformed/non-UUID thread id was leaking the store's parse error as a 500; a `uuid.Parse` guard now maps it to 404 at both handlers (T-12-11). Coverage to 86.8%.
- Quality gates recorded: `internal/agui` coverage 86.8% (owned-surface 86.2%, ≥85%), `translator.go` mutation 76.2% (≥70%, 15 near-equivalent survivors advisory-accepted).
- The 12-VALIDATION.md Per-Task Verification Map is fully green and `wave_0_complete: true`.
- **Operator live Gate-3 signed off.** The operator delegated the live sign-off to an autonomous E2E loop, which scored **11/11 (100%)** in 3 iterations (2 driver-harness fixes, zero product defects).

## Task Commits

Each task was committed atomically:

1. **Task 1: agui_smoke.sh + CI db_integration tier + coverage/mutation + VALIDATION flip** — `1867c0c2` (feat) — includes the Rule-1 404 fix, the smoke script, the CI wiring, the quality-snapshot rows, and the initial VALIDATION status flip.
2. **Task 2: Operator live Gate-3 sign-off** — checkpoint:human-verify, no code change. Operator delegated to an autonomous E2E loop (11/11). Close-out (VALIDATION final row + Approval narration + quality-snapshot operator row) committed with the plan metadata.

**Plan metadata:** `docs(12-04): complete AG-UI gateway Gate-3 plan` (this SUMMARY + VALIDATION + quality-snapshot + STATE + ROADMAP).

## Files Created/Modified

- `scripts/agui_smoke.sh` — live SSE round-trip + GET snapshot + 404 smoke (DEGRADED/LIVE legs, REASONING_* on the live leg)
- `.github/workflows/ci.yml` — `./internal/agui/...` in the db_integration package list + the degraded-leg smoke step
- `internal/agui/server.go` — Rule-1 404 fix (uuid.Parse guard before the store round-trip)
- `internal/agui/server_test.go`, `internal/agui/helpers_test.go` — lock the 404 fix + helper/error-path coverage → 86.8%
- `docs/aura-quality-snapshot.md` — Phase-12 rows + operator Gate-3 11/11
- `.planning/phases/12-ag-ui-gateway/12-VALIDATION.md` — full green map, wave_0_complete: true, operator sign-off recorded

## Decisions Made

- The Rule-1 404 fix (malformed/non-UUID id → 404, not a leaked 500) was the only product change in this plan; it was caught by the smoke's does-not-exist chokepoint (T-12-11).
- The autonomous CI gate uses the degraded/FakeClient leg (fast db_integration tier); the live OpenRouter REASONING_* round-trip is the operator Gate-3 leg, per the prior-phase pattern.
- translator.go mutation 76.2% advisory-accepted (near-equivalent survivors, project precedent).
- Gate-3 sign-off was operator-delegated to an autonomous E2E loop asserting frame/DB/snapshot ground truth.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Malformed/non-UUID thread id leaked a 500 instead of 404**
- **Found during:** Task 1 (the live smoke's does-not-exist chokepoint, T-12-11)
- **Issue:** `GET /threads/<bad-id>/messages` and the POST path passed a non-UUID thread id straight to the store, which returned a parse error surfaced as HTTP 500 — leaking an internal error shape on an attacker-controlled input.
- **Fix:** A `uuid.Parse` guard at both handlers maps a malformed/non-UUID id to 404 before the store round-trip.
- **Files modified:** `internal/agui/server.go`, `internal/agui/server_test.go`, `internal/agui/helpers_test.go`
- **Verification:** `bash scripts/agui_smoke.sh` 404 chokepoint passes live (C8 in the E2E loop: `GET /threads/does-not-exist/messages` → HTTP 404); unit tests lock the path.
- **Committed in:** `1867c0c2` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 Rule-1 bug). **Impact on plan:** the fix is a correctness/info-disclosure hardening on a boundary input, in scope for the live-smoke Gate-3. No scope creep.

## Issues Encountered

- The autonomous E2E loop required 2 driver-harness fixes across 3 iterations (the early `score.txt`/`answer.txt` artifacts on disk show a C6 FAIL with `answer=''` — a driver-side answer-reconstruction bug, NOT a product defect). The final run scored 11/11. Ground truth corroborates zero product defects: the SSE `TEXT_MESSAGE_CONTENT` deltas reconstruct exactly to `Ciao! 2 + 2 = **4** 🎉`, the MESSAGES_SNAPSHOT and the `aura.conversation_turns` assistant row (len=21) both match, and no CoT is persisted.

## Operator Live Gate-3 (Task 2) — Sign-Off Evidence

The operator delegated the live sign-off to an autonomous E2E loop ("do all E2E test in autonomy and loop until score is >95%"). Final score **11/11 (100%)**, 3 iterations (2 driver-harness fixes, zero product defects). Artifacts persisted in `D:/tmp/agui-e2e/` (`sse.txt`, `snap.json`, `serve.log`, `db_turns.txt`, `answer.txt`, `chat_leg.out`). Linux build, WSL, live OpenRouter (2 paid calls). All 11 checks PASS:

- **C1 build:** `go build ./cmd/aura` clean.
- **C2 serve boot:** `aura serve: agui http server listening addr=127.0.0.1:9080` + port accepts.
- **C3:** SSE opens `event: RUN_STARTED`.
- **C4 REASONING lifecycle** complete and ordered: `REASONING_START → REASONING_MESSAGE_START → REASONING_MESSAGE_CONTENT×N → REASONING_MESSAGE_END → REASONING_END`.
- **C5:** first `REASONING_END` precedes the first `TEXT_MESSAGE_START` (#57 interleave) — verified in `sse.txt`.
- **C6:** `RUN_FINISHED` outcome success; answer reconstructs from `TEXT_MESSAGE_CONTENT` deltas = `Ciao! 2 + 2 = **4** 🎉`; STATE_DELTA carried usage (6528 cache_hit / 6881 prompt tokens, $0.000193).
- **C7:** `GET /threads/<tid>/messages` → MESSAGES_SNAPSHOT with the seeded user turn; CoT snippet NOT present.
- **C8:** `GET /threads/does-not-exist/messages` → HTTP 404 (the Rule-1 fix).
- **C9 DB ground truth (artifact-not-reply):** `aura.conversation_turns` assistant row content len=21 (`Ciao! 2 + 2 = **4** 🎉`), CoT absent from all rows.
- **C10:** SIGTERM → process exits, `serve.log` shows `graceful shutdown complete`, no panic / goroutine-leak.
- **C11 CLI render:** `printf 'ciao dimmi 2+2\n/exit\n' | aura chat new` → dim 💭 + reasoning deltas (per-delta ANSI reset) stream BEFORE the answer; `· shell_exec` tool trace interleaved; answer `**4**` plain; usage `· 6864 tok · $0.000182`; exit 0; no mojibake.

Reasoning streamed live (173 chars CoT, "The user is asking for 2+2 calculation…"), never persisted.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Phase 12 AG-UI Gateway is complete (6/6 plans). UX-01 success criteria 1/3 proven live (curl SSE round-trip + GET snapshot), SC2 (boundary) and SC4 (pin) gated in CI; the reasoning data-plane (amendment #57) is operator-confirmed end-to-end.
- Ready for `/gsd-verify-work 12` then `/gsd-plan-phase 13` (Channels + Telegram + Multimodal), which depends on Phase 12 (the fanout client-subscriber seam from 12-02 + the SSE wire).

## Self-Check: PASSED

- Files claimed created/modified exist on disk: `scripts/agui_smoke.sh`, `internal/agui/server.go`, `internal/agui/server_test.go`, `internal/agui/helpers_test.go`, `.github/workflows/ci.yml`, `docs/aura-quality-snapshot.md`, `.planning/phases/12-ag-ui-gateway/12-VALIDATION.md`, `.planning/phases/12-ag-ui-gateway/12-04-SUMMARY.md` — all FOUND.
- Task 1 commit `1867c0c2` present on `tabula-rasa` (verified via `git log`).
- E2E ground truth corroborated: `sse.txt` event ordering (RUN_STARTED → REASONING lifecycle → TEXT → RUN_FINISHED success; REASONING_END before first TEXT_MESSAGE_START), `snap.json` MESSAGES_SNAPSHOT (no CoT), `db_turns.txt` assistant len=21, `serve.log` graceful shutdown.

---
*Phase: 12-ag-ui-gateway*
*Completed: 2026-06-07*
