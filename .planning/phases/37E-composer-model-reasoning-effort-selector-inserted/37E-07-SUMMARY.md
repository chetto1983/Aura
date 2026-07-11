---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 07
subsystem: web-composer
tags: [reasoning-effort, composer, selector, capability-aware, aria, i18n, hydrate-on-reopen, playwright, terminal-plan]

# Dependency graph
requires:
  - phase: 37E-06
    provides: "GET /api/composer/reasoning-capabilities -> {levels,default,backend,detected} (render ONLY levels; safe floor {auto,off} when detected=false) AND the aura.effort run-body field (server two-stage governed)"
  - phase: 37E-03
    provides: "Conversation.ReasoningEffort read projection (the DTO field the selector hydrates from; restored on reopen)"
provides:
  - "fetchReasoningCapabilities + ReasoningCapabilities + REASONING_CAPABILITIES_FLOOR (composer/api.ts) — capability fetch, degrade-to-{auto,off} on any throw"
  - "useReasoningCapabilities hook (mount-fetch, mirrors useComposerSkills)"
  - "useReasoningEffort hook (per-conversation hydrate on threadId, clamp unsupported->auto, NO clear on send)"
  - "buildAuraRunBody effort fold (aura.effort for the six fixed levels, OMITTED for auto) + StreamRunOptions.effort"
  - "Composer capability-driven ARIA reasoning-effort <select> (renders ONLY advertised levels, D-13) + ExternalStoreChat wiring (hydrate from useConversation ReasoningEffort; effort rides every send)"
  - "chat.composer.effort.* i18n (7 labels + ariaLabel) en+it parity"
  - "Conversation.ReasoningEffort frontend DTO field"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Capability-driven dynamic UI (D-13): the selector renders ONLY the symbols the endpoint advertises — never a hard-coded 7 — degrading to the {auto,off} floor when detection fails"
    - "Per-conversation preference hook distinct from the per-turn pin: useReasoningEffort hydrates once per threadId from the conversation DTO and is NEVER cleared on send (contrast usePinnedSkill's clear-after-send)"
    - "Adjust-state-during-render (not setState-in-effect): both the hydrate and the clamp run during render guarded by a state comparison — the sanctioned React pattern the eslint react-hooks rule enforces, mirroring Composer's activeIndexFilter"
    - "Native <select> for an accessible dynamic control: keyboard + screen-reader + touch correct out of the box, separate from the message textbox so it never reclassifies the input or intercepts Enter-send/paste/drop (37D precedent)"

key-files:
  created:
    - web/src/chat/composer/useReasoningCapabilities.ts
    - web/src/chat/composer/useReasoningEffort.ts
    - web/src/chat/composer/__tests__/reasoningEffort.test.ts
    - web/e2e/composer-effort.spec.ts
  modified:
    - web/src/chat/composer/api.ts
    - web/src/chat/auraRunBody.ts
    - web/src/chat/sseAdapter.ts
    - web/src/chat/Composer.tsx
    - web/src/chat/ExternalStoreChat.tsx
    - web/src/conversations/useConversations.ts
    - web/src/i18n/resources.ts
    - web/src/i18n/resources.composer.ts
    - web/src/chat/__tests__/Composer.test.tsx

key-decisions:
  - "Native <select> over a hand-rolled radiogroup: it is keyboard/screen-reader/touch-correct out of the box and impossible to get roving-tabindex wrong on a terminal plan; styled cohesively with the composer (rounded-full, surface-2, accent focus ring, ChevronDown, small-caps) so it is distinctive, not generic AI-slop"
  - "Effort hydrates from the RAW Go conversation struct field ReasoningEffort (PascalCase, no json tag — the single-conversation GET writes the struct verbatim), NOT a snake_case DTO; the frontend Conversation interface gained a ReasoningEffort field"
  - "i18n landed as chat.composer.effort.* via resources.composer.ts (the 37D skillPicker split precedent) because resources.ts sits at the 600-LOC cap — the labels could not be inlined there without breaching it"
  - "The clamp-unsupported->auto is defense-in-depth (T-37E-07-ADVISORY): the server two-stage-validates authoritatively; the UI clamp only keeps the selector on a renderable value"

patterns-established:
  - "Capability-aware composer control: fetch the advertised set, render it dynamically, degrade to a safe floor — reusable for any future per-model UI affordance"

requirements-completed: [WEBMODEL-01, WEBMODEL-02, WEBMODEL-03]

# Metrics
duration: ~55min
completed: 2026-07-11
---

# Phase 37E Plan 07: Capability-Aware Composer Reasoning-Effort Selector Summary

**The user-facing surface that makes the whole phase real: a compact ARIA `<select>` near the Send affordance renders ONLY the reasoning-effort levels the active model advertises (fetched from `GET /api/composer/reasoning-capabilities`, dynamic — never a hard-coded 7, D-13), holds a per-conversation value hydrated from the conversation DTO's `ReasoningEffort` (37E-03) and restored on reopen, folds the chosen fixed level into `aura.effort` (omitting `auto`), and degrades to `{auto,off}` on detection failure — without disrupting the `/`-skill picker, Enter-send, paste, or drop. New state/fetch logic is extracted to two hooks so `ExternalStoreChat.tsx` (537) and `sseAdapter.ts` (599) stay under the 600-LOC cap.**

## Performance
- **Tasks:** 3/3 committed
- **Files:** 4 created, 9 modified
- **Completed:** 2026-07-11

## Accomplishments
- **Capability fetch + degrade (D-09):** `fetchReasoningCapabilities()` (composer/api.ts) GETs the endpoint via the same-origin `getJSON`; ANY throw (401/500/503) or a body missing/emptying `levels` degrades to the exported `REASONING_CAPABILITIES_FLOOR` `{levels:['auto','off'],default:'auto',detected:false}`. `useReasoningCapabilities` mount-fetches it (mirrors `useComposerSkills`, mounted-guard).
- **Per-conversation effort hook:** `useReasoningEffort(threadId, hydratedEffort, levels)` hydrates once per thread from the conversation DTO's persisted `ReasoningEffort` (default `auto` when `""`), exposes `effort`/`setEffort`, is **never cleared on send**, and clamps a stored/selected value not in the advertised `levels` back to `auto` (D-13 defense-in-depth). Both transitions adjust state **during render** (guarded by a state comparison) — no setState-in-effect (lint-forbidden; mirrors Composer's `activeIndexFilter`).
- **Run-body fold:** `buildAuraRunBody` folds `aura.effort` for the six fixed levels and **OMITS it for `auto`** (`opts.effort && opts.effort !== 'auto'`); a plain turn is byte-identical to before. `StreamRunOptions.effort?: string` added (sseAdapter one-line, 599 LOC).
- **The selector:** a compact accessible native `<select>` (aria-label `Reasoning effort`) near Send, rendering `capabilities.levels` **dynamically** (auto-first) — exactly the advertised subset, never the full 7. It is a separate control, so it does not reclassify the message textbox or intercept Enter-send/paste/drop (37D discipline preserved). `ComposerProps` gained `effort`/`effortLevels`/`onEffortChange`; the selector is absent when no levels are wired.
- **Wiring + hydration:** `ExternalStoreChat` destructures `useReasoningCapabilities()` + `useReasoningEffort(threadId, useConversation(threadId).data?.ReasoningEffort, caps.levels)`, passes `effort`/`effortLevels`/`onEffortChange` to `<Composer>`, and includes `effort` in every `streamRun({...})` (auto omitted by the fold) — **not** cleared after send. The frontend `Conversation` interface gained `ReasoningEffort?: string`.
- **i18n parity:** `chat.composer.effort.*` (7 labels + `ariaLabel`) added to BOTH locales via `resources.composer.ts` (en: Auto/Off/Low/Medium/High/Extra/Max; it: Auto/Off/Basso/Medio/Alto/Extra/Massimo) — parity CI green.
- **Tests:** vitest `reasoningEffort.test.ts` (15) — fold omit-auto/include-fixed/coexist-with-skill, capability degrade (throw + non-ok + missing-levels), hydrate + clamp + re-hydrate + no-clear; Composer selector tests (5) — exact advertised set, hydrated value, `onEffortChange`, absent-without-levels, idle-textbox-preserved. Playwright `composer-effort.spec.ts` (2 tests × chromium+mobile-chrome) — dynamic subset, auto-omits/high-wires `aura.effort`, per-conversation restore on reopen.

## Task Commits
1. **Task 1: capability+effort hooks, fetch client, run-body effort fold** — `671c37c3` (feat)
2. **Task 2: capability-driven Composer effort selector + ExternalStoreChat wiring + en/it i18n** — `db155d0c` (feat)
3. **Task 3: Playwright e2e — dynamic levels, aura.effort wire, per-conversation restore** — `c4e36efa` (test)

**Plan metadata:** (this docs commit — SUMMARY + STATE + ROADMAP + REQUIREMENTS)

## Files Created/Modified
- `web/src/chat/composer/useReasoningCapabilities.ts` (created, 31) — mount-fetch + degrade.
- `web/src/chat/composer/useReasoningEffort.ts` (created, 45) — per-conversation hydrate/clamp/no-clear (adjust-during-render).
- `web/src/chat/composer/__tests__/reasoningEffort.test.ts` (created, 15 tests) — the fold + fetch-degrade + hook gate.
- `web/e2e/composer-effort.spec.ts` (created, 228) — the terminal acceptance e2e (golden-replay).
- `web/src/chat/composer/api.ts` (modified, 67) — `fetchReasoningCapabilities`, `ReasoningCapabilities`, `REASONING_CAPABILITIES_FLOOR/PATH`.
- `web/src/chat/auraRunBody.ts` (modified) — the effort fold.
- `web/src/chat/sseAdapter.ts` (modified, 599) — `StreamRunOptions.effort`.
- `web/src/chat/Composer.tsx` (modified, 442) — the selector control + props.
- `web/src/chat/ExternalStoreChat.tsx` (modified, 537) — hook wiring + send fold + Composer props.
- `web/src/conversations/useConversations.ts` (modified) — `Conversation.ReasoningEffort`.
- `web/src/i18n/resources.ts` (modified, 598) — nest `chat.composer.effort` en+it.
- `web/src/i18n/resources.composer.ts` (modified) — `composerEffortEn`/`composerEffortIt`.
- `web/src/chat/__tests__/Composer.test.tsx` (modified) — 5 selector tests.

## Decisions Made
- **Native `<select>` over a radiogroup** — correctness-first on a terminal plan (keyboard/SR/touch correct out of the box), styled cohesively with the composer so it is distinctive without risking roving-tabindex bugs.
- **Hydrate from the raw Go struct field `ReasoningEffort`** — the single-conversation GET (`handleGetConversation`) writes the `conversations.Conversation` struct verbatim (no json tags → PascalCase), so the frontend reads `conversation.ReasoningEffort`, not a snake_case DTO.
- **i18n via `resources.composer.ts`** — `resources.ts` sits at the 600-LOC cap; the labels split out exactly as the 37D skillPicker bundle did.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] i18n lives in TypeScript resource files, not `en.json`/`it.json`**
- **Found during:** Task 2. The plan's `files_modified` lists `web/src/i18n/en.json` + `it.json`, but the project has NO JSON i18n — it uses typed `resources.ts` + split `resources.<ns>.ts` bundles.
- **Fix:** Added `composerEffortEn`/`composerEffortIt` to `resources.composer.ts` and nested them as `chat.composer.effort` in `resources.ts` (both locales), the exact 37D skillPicker pattern. Parity CI (`resources.parity.test.ts`) green.
- **Files:** `web/src/i18n/resources.composer.ts`, `web/src/i18n/resources.ts`.

**2. [Rule 3 - Blocking] e2e path convention is `web/e2e/`, not `web/tests/e2e/`**
- **Found during:** Task 3. The plan path `web/tests/e2e/composer-effort.spec.ts` does not match the repo — `playwright.config.ts` `testDir: './e2e'` and every existing spec lives in `web/e2e/`.
- **Fix:** Created the spec at `web/e2e/composer-effort.spec.ts` so Playwright discovers it; `pnpm test:e2e -g composer-effort` (`playwright test composer-effort`) selects it. Placing it at the plan's path would have been undiscovered by the runner.
- **Files:** `web/e2e/composer-effort.spec.ts`.

**3. [Rule 3 - Blocking] `Conversation.ReasoningEffort` was not yet on the frontend DTO type**
- **Found during:** Task 2. 37E-03 added the Go projection field, but the frontend `Conversation` interface (useConversations.ts) did not carry it, so hydration would not type-check.
- **Fix:** Added `readonly ReasoningEffort?: string` to the frontend `Conversation` interface.
- **Files:** `web/src/conversations/useConversations.ts`.

**4. [Rule 3 - Blocking] `useReasoningEffort` initially used setState-in-effect (eslint-forbidden)**
- **Found during:** Task 1. The clamp/hydrate effects tripped `react-hooks/set-state-in-effect`.
- **Fix:** Rewrote both transitions as adjust-state-during-render guarded by a state comparison (the sanctioned pattern already in `Composer.tsx` for `activeIndexFilter`). Tests unchanged and green.

No architectural changes (Rule 4), no auth gates, no new dependencies.

## Threat Register Outcome
- **T-37E-07-ADVISORY** (EoP/Tampering, client-chosen effort) — the selector is advisory only; the server two-stage-validates every fixed level (37E-06 Stage-1 enum + Stage-2 capability → 400). The UI clamps unsupported stored values to `auto` as defense-in-depth (tested: hydrate `high` with `{auto,off}` → clamps to `auto`).
- **T-37E-07-DEGRADE** (Availability) — `fetchReasoningCapabilities` degrades to `{auto,off}` on any throw/non-ok (tested: reject + 503 + missing-levels); a failed fetch never breaks the Composer or blocks sending. The e2e routes the endpoint.
- **T-37E-07-INPUT** (Tampering, `/`-picker / Enter-send) — the selector is a separate native `<select>`, does not reclassify the input; the idle-textbox regression guard (Composer.test) + the full 1280-test suite (incl. the 37D skill-picker/Enter-send/paste/drop tests) pass unchanged.
- **T-37E-07-XSS** — the effort renders as a controlled `<select>` value from a fixed symbol set; labels come from i18n resources, never user input / raw HTML.

No new threat surface (no new endpoint, auth route, or dependency).

## Known Stubs
None. The selector is wired to the live capability endpoint and the persisted conversation field end-to-end; the fold, hydration, and restore are real. Graduated-effort FIDELITY on a real backend (D-09) is manual-only (deferred to /gsd-verify-work) — CI asserts only the wire + dynamic set + restore, never `low<mid<high` on DeepSeek, exactly as the plan's Manual-Only note requires.

## Verification Evidence
- `npx vitest run reasoningEffort` → 15 passed; `npx vitest run Composer resources.parity` → 77 passed.
- Full suite `npx vitest run` → **1280 passed / 156 files** (zero regressions — the 37D composer interactions intact).
- `npx vitest run --coverage` → exit 0 (all-files 92.62% stmts / 86.71% branch / 92.75% func / 94.35% lines — clears the ≥85% frontend floor).
- `npx tsc --noEmit` clean; `npx eslint` clean on every touched file (`--max-warnings=0`).
- `npx playwright test composer-effort --list` → compiles + lists 4 tests (2 × chromium+mobile-chrome). The e2e EXECUTES in the CI web-e2e job (needs a live `aura serve` + Authula stack + the built cockpit bundle, absent in this environment) — not fabricated as passed here.
- LOC caps: `sseAdapter.ts` 599, `ExternalStoreChat.tsx` 537, `Composer.tsx` 442, `resources.ts` 598 — all ≤600 (pre-commit file-size hook green on all three code commits).

## Next Phase Readiness
This is the TERMINAL 37E plan — the vertical is now user-observable, so WEBMODEL-01/02/03 are marked complete. The queued follow-on (memory: Phase 37A web artifact delivery) is unaffected. Phase-close quality-snapshot re-attestation + the phase-close `git push` are the remaining owner (orchestrator/user) steps.

## Self-Check: PASSED

Files (created) verified present on disk:
- web/src/chat/composer/useReasoningCapabilities.ts — FOUND
- web/src/chat/composer/useReasoningEffort.ts — FOUND
- web/src/chat/composer/__tests__/reasoningEffort.test.ts — FOUND
- web/e2e/composer-effort.spec.ts — FOUND

Commits verified in git log:
- 671c37c3 (Task 1) — FOUND
- db155d0c (Task 2) — FOUND
- c4e36efa (Task 3) — FOUND

---
*Phase: 37E-composer-model-reasoning-effort-selector-inserted*
*Completed: 2026-07-11*
