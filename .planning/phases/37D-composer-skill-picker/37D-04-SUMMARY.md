---
phase: 37D-composer-skill-picker
plan: 04
subsystem: web-composer
tags: [web, react, composer, skills, aria, combobox, run-envelope, WEBSKILL, vitest]
type: execute
wave: 3
autonomous: true
dependency_graph:
  requires:
    - phase: 37D-02
      provides: "GET /api/composer/skills + the aura.skill run-envelope decode + Mechanism-A server application"
    - phase: 37D-03
      provides: "the self-contained picker foundation (api.ts, skillPickerModel.ts, SkillPicker, SkillPill, chat.skillPicker i18n)"
  provides:
    - "web/src/chat/auraRunBody.ts — buildAuraRunBody folds {attachment_ids?, skill?} into ONE aura object (or none)"
    - "web/src/chat/composer/useComposerSkills.ts — one-shot skills fetch ([] on any throw — the D-09 degrade)"
    - "web/src/chat/composer/usePinnedSkill.ts — the lifted pinnedSkill seam (mirrors useAttachmentUploads)"
    - "web/src/chat/ExternalStoreChat.tsx — pinnedSkill lift + skills fetch + skill carried on send + cleared after"
    - "web/src/chat/Composer.tsx — the / trigger + ARIA combobox keys + SkillPill + add-files/new-chat/clear quick actions"
    - "web/src/chat/ExternalStoreChat_folds.ts — the pure message/asset-fold helpers (refactor-on-touch extraction)"
  affects:
    - "37D-05 (composer-skills e2e drives this wired UI end-to-end: open → filter → select → pill → send carries aura.skill)"
tech_stack:
  added: []
  patterns:
    - "aura run-envelope one-field extension: buildAuraRunBody folds attachment_ids + skill into ONE aura object, extracted OUT of sseAdapter.ts (at exactly 600 LOC) so the field lands without breaching the cap"
    - "lifted-seam hook: usePinnedSkill owns the pinnedSkill useState and returns { pinnedSkill, setPinnedSkill } so ExternalStoreChat destructures the seam (mirrors useAttachmentUploads) instead of an inline useState"
    - "APG combobox with derived open/active state: no setState-in-effect — the dismissed close is derived from the text Escape recorded, and the active-index resets to 0 via an adjust-during-render guard (React re-renders before commit, no cascading paint)"
    - "composeEventHandlers guard: assistant-ui composes the caller onKeyDown BEFORE its internal handleKeyPress and skips it on defaultPrevented, so preventDefault ONLY while menuOpen keeps Enter-send/paste/drop intact when closed (D-09)"
key_files:
  created:
    - "web/src/chat/auraRunBody.ts — buildAuraRunBody(id, opts) (20 LOC)"
    - "web/src/chat/composer/useComposerSkills.ts — one-shot degrade-safe skills fetch hook (23 LOC)"
    - "web/src/chat/composer/useComposerSkills.test.ts — success + reject→[] (40 LOC)"
    - "web/src/chat/composer/usePinnedSkill.ts — the lifted pinnedSkill seam (15 LOC)"
    - "web/src/chat/composer/usePinnedSkill.test.ts — initial null + pin/clear (31 LOC)"
    - "web/src/chat/ExternalStoreChat_folds.ts — the pure message/asset-fold helpers, extracted (125 LOC)"
    - "web/src/chat/__tests__/ExternalStoreChat.skill.test.tsx — skill carried on send + cleared, none→no aura (126 LOC)"
  modified:
    - "web/src/chat/sseAdapter.ts — StreamRunOptions.skill + body delegates to buildAuraRunBody (600 → 598 LOC)"
    - "web/src/chat/__tests__/sseAdapter.test.ts — the three envelope-fold cases"
    - "web/src/chat/Composer.tsx — the / picker integration (283 → 403 LOC)"
    - "web/src/chat/__tests__/Composer.test.tsx — 12 picker cases (open/filter/keys/a11y/degrade/quick-actions/pill)"
    - "web/src/chat/ExternalStoreChat.tsx — pinnedSkill lift + skills + skill-on-send + onNewChat (626 → 518 LOC after the folds extraction)"
    - "web/src/chat/ExternalStoreChat.rehydration.test.tsx — foldAgentOntoAssistant import repointed to the folds module"
    - "web/src/AppShell.tsx — onNewChat={startNewConversation} (590 → 591 LOC)"
decisions:
  - "Tasks executed 1 → 3 → 2 (sseAdapter → Composer → ExternalStoreChat/AppShell): ExternalStoreChat's <Composer> passes props Composer must first DECLARE, and the plan allocates Composer.tsx exclusively to Task 3, so the Composer integration was committed before the ExternalStoreChat wiring — keeping every commit tsc/lint/test-clean and honoring the per-task file allocation"
  - "The three buildAuraRunBody branch cases live in sseAdapter.test.ts as direct pure-function tests (not a fetch-stub streamRun harness): streamRun delegates body: JSON.stringify(buildAuraRunBody(id, opts)) so the existing network tests (byte-identical no-aura + attachments branch) already prove the delegation, and the pure test needs zero harness duplication"
  - "Picker open/active state is DERIVED, not effect-driven: an initial setState-in-effect re-arm tripped react-hooks/set-state-in-effect, so dismissed is derived from the text Escape recorded and the active-index resets via an adjust-during-render guard"
  - "onNewChat typed () => void | Promise<void> across the chain so AppShell can pass the async startNewConversation directly (grep-exact) without tripping no-misused-promises under strictTypeChecked"
metrics:
  tasks_completed: 3
  duration: "~40 min (3 tasks, incl. the refactor-on-touch + full-suite coverage run)"
  completed: "2026-07-10"
  files_changed: 14
  commits: ["a1ca9b75", "fcdc9697", "b53d3dfb"]
---

# Phase 37D Plan 04: Composer Skill-Picker Integration Summary

**The 37D-03 picker foundation is now wired into the live composer and the run path end-to-end: typing `/` at an empty composer opens the APG combobox (keys handled ONLY while open, focus never leaving the input), selecting pins a removable `SkillPill` and clears the `/`-filter, and on send `streamRun` folds `aura.skill` into the SAME envelope as `attachment_ids` (37D-02 applies it as the turn's first action) — plus the add-files/new-chat/clear quick actions run as pure client actions, and Enter-send/paste/drop stay intact when the menu is closed (D-09), the whole surface degrading to a no-op when the skills list is empty/unreachable.**

## Performance

- **Duration:** ~40 min (3 tasks)
- **Completed:** 2026-07-10
- **Tasks:** 3
- **Files changed:** 14 (7 created, 7 modified)

## Accomplishments

- **`aura.skill` on the run envelope (WEBSKILL-02):** `StreamRunOptions.skill?` + a NEW `buildAuraRunBody(id, opts)` that folds `{attachment_ids?, skill?}` into ONE `aura` object (emitted only when either is set; byte-identical to today when neither is). Extracted OUT of `sseAdapter.ts` (which sat at EXACTLY 600 LOC) so the field lands without breaching the cap; `streamRun` now `body: JSON.stringify(buildAuraRunBody(id, opts))`.
- **Lifted `pinnedSkill` seam + degrade-safe skills fetch (WEBSKILL-01/02):** `usePinnedSkill()` owns the `pinnedSkill` state and returns `{ pinnedSkill, setPinnedSkill }` (mirrors `useAttachmentUploads`); `useComposerSkills()` fetches once on mount via `fetchComposerSkills` and swallows any rejection to `[]` (the D-09 no-op source). `ExternalStoreChat` consumes both, carries `skill: pinnedSkill.name` into `streamRun` (conditional spread — exactOptionalPropertyTypes), and clears the pill after a successful send (mirrors `uploads.clearReady`).
- **Composer `/` integration (WEBSKILL-01/03):** the composer text is read reactively (`useAuiState((s) => s.composer.text)`); `menuOpen = shouldOpen(text, skills.length) && !dismissed && options.length>0`; the input carries the APG combobox ARIA (`role=combobox` / `aria-expanded` / `aria-controls` / `aria-activedescendant` / `aria-haspopup` / `aria-autocomplete`); the composed `onKeyDown` maps `pickerKeyAction` (↑/↓ move + `preventDefault`, Enter selects + `preventDefault`, Escape closes + `preventDefault`) ONLY while open — closed, it returns early so the library's Enter-send/paste/drop run untouched. Selecting a skill pins the `SkillPill` + clears the `/`-filter; add-files clicks the Paperclip input, new-chat calls `onNewChat`, clear resets text + pill + pending attachments — all pure client, no agent round-trip.
- **`onNewChat` threaded from AppShell:** `<ExternalStoreChat onNewChat={startNewConversation} />` (typed `() => void | Promise<void>` across the chain so the async source passes strict `no-misused-promises`).
- **Coverage:** full web suite **155 files / 1260 tests** green; global gate **92.6% stmts / 86.68% branch / 92.77% funcs / 94.34% lines** (≥85% floor). No new deps, migrations, or env; i18n parity untouched (37D-03-owned).

## Task Commits

Each task landed as one atomic `feat(37D-04)` commit (impl + tests together, per the sequential-executor + 37D-02/03 precedent). Execution/commit order was **1 → 3 → 2** (see Deviations):

1. **Task 1 — sseAdapter `aura.skill` envelope** — `a1ca9b75` (`SSEADAPTER_SKILL_OK`)
2. **Task 3 — Composer `/` picker integration** — `fcdc9697` (`COMPOSER_INTEGRATION_OK`)
3. **Task 2 — ExternalStoreChat pinnedSkill lift + hooks + AppShell** — `b53d3dfb` (`EXTSTORE_SKILL_OK`)

**Plan metadata:** this SUMMARY + STATE.md + ROADMAP.md (docs commit).

## Deviations from Plan

Rules 1–3 auto-applied (no user permission needed); tracked here.

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Executed tasks 1 → 3 → 2 (Composer before ExternalStoreChat)**
- **Found during:** Task 2 planning.
- **Issue:** Task 2 wires `<Composer skills=… pinnedSkill=… onPinSkill=… onNewChat=… />`, which does not tsc-compile until `Composer` DECLARES those props — and the plan allocates `Composer.tsx` exclusively to Task 3. Doing Task 2 first would either fail tsc or force a prop declaration in the wrong task's file.
- **Fix:** Committed the Composer integration (Task 3) before the ExternalStoreChat wiring (Task 2). Every commit stays tsc/lint/test-clean and each keeps its plan-allocated files; only the commit order changed.
- **Files:** none extra — ordering only.
- **Commits:** `fcdc9697` (Task 3) then `b53d3dfb` (Task 2).

**2. [Rule 3 - Blocking] Refactor-on-touch: extracted `ExternalStoreChat_folds.ts` to stay ≤600 LOC**
- **Found during:** Task 2 (the pinnedSkill/skills wiring + 6-prop `<Composer>` + expanded `onNew` deps).
- **Issue:** The wiring pushed `ExternalStoreChat.tsx` from 599 → **626 LOC**, over the CLAUDE.md 600-LOC cap (the file-size hook would block the commit). The plan explicitly anticipated this ("deep-refactor-on-touch in the SAME commit … extract to a sibling").
- **Fix:** Moved the pure message/asset-fold helpers (`appendMessageText`, `userMessage`, `foldAssetsPositionally`, `attachAssetsToUserMessages`, `foldAgentOntoAssistant`, `replaceAssetInMessages`, `removeAssetFromMessages`, `assistantErrorMessage`, `isAbortSignalAborted`) into a NEW `ExternalStoreChat_folds.ts` and imported them back. `ExternalStoreChat.tsx` → **518 LOC**; the folds module → 125 LOC.
- **Files:** `web/src/chat/ExternalStoreChat.tsx`, `web/src/chat/ExternalStoreChat_folds.ts`, `web/src/chat/ExternalStoreChat.rehydration.test.tsx` (the one external consumer of `foldAgentOntoAssistant` — its import repointed to the folds module).
- **Commit:** `b53d3dfb`.

**3. [Rule 1 - Bug] Replaced the re-arm `useEffect` with derived state (react-hooks/set-state-in-effect)**
- **Found during:** Task 3 (eslint).
- **Issue:** A `useEffect(() => { setPickerDismissed(false); setActiveIndex(0); }, [composerText])` tripped `react-hooks/set-state-in-effect` (cascading renders).
- **Fix:** `dismissed` is now derived — Escape records the text it closed at (`setDismissedAt(text)`) and `menuOpen` checks `dismissedAt !== composerText`, so the next keystroke re-arms with no effect; the active-index resets to 0 via an adjust-during-render guard (`if (activeIndexFilter !== pickerFilter) { … }`).
- **Files:** `web/src/chat/Composer.tsx`.
- **Commit:** `fcdc9697`.

**Interpretation note (not a deviation):** the plan's `<action>` describes the three `buildAuraRunBody` cases as "parse the captured POST body". They are implemented as direct pure-function tests of `buildAuraRunBody` in `sseAdapter.test.ts` (the file the verify command names) — `streamRun` delegates `body: JSON.stringify(buildAuraRunBody(id, opts))`, so the existing `sseAdapter_network.test.ts` streamRun cases (byte-identical no-aura + the attachments branch) already prove the POST delegation, and testing the extracted pure function directly exercises every branch with zero fetch-harness duplication (the 37D-03 pure-model-as-coverage-target discipline).

## Threat Model Coverage

- **T-37D-07 (arbitrary skill name injected on send):** mitigated — `pinnedSkill` is set ONLY from a `SkillPicker` selection over the fetched list (`onPinSkill(row)`); `ExternalStoreChat` carries only `pinnedSkill.name`, and the server (37D-02) resolves it against the loader's validated key set and no-ops an unknown name. No free-form text ever reaches `aura.skill`.
- **T-37D-08 (self-DoS — broken send):** mitigated — keys are `preventDefault`'d ONLY while `menuOpen`; closed, the library's Enter-send/paste/drop run untouched (proven by the closed-menu Enter-send test asserting `defaultPrevented === false`, the literal-mid-text-slash test, and the REAL-runtime `ExternalStoreChat.skill`/`.attachments` send tests). The trigger is `shouldOpen` (never a mid-text slash).
- **T-37D-SC (package installs):** N/A — 37D-04 installs NO external packages; `web/package.json` + lockfile byte-unchanged.

## Known Stubs

None. Every seam is wired and exercised: `buildAuraRunBody` (3 branch tests + the live `streamRun` delegation), `useComposerSkills`/`usePinnedSkill` (renderHook tests), the `ExternalStoreChat` skill-send + clear-after-send (real-runtime POST-body test), and the Composer `/` trigger + keys + a11y + quick-actions + pill (12 RTL cases). `skills={[]}` is the deliberate degrade-to-no-op path (D-09), not a stub.

## Requirements

`WEBSKILL-01/02/03` remain `[ ]` and `requirements mark-complete` was intentionally NOT run — matching the 37D-01/02/03 phase-spanning precedent: 37D-04 delivers the working in-app integration, but WEBSKILL-03 explicitly requires the Playwright e2e + the terminal coverage gate, which is **37D-05** (the terminal plan marks all three after proving the full flow live).

## Verification

- `npx tsc --noEmit` — clean across the full web tree (after every task).
- `npx eslint --max-warnings=0` + `npx prettier` — clean on every new/edited file (incl. jsx-a11y for the combobox input; no eslint-disable added).
- `npx vitest run` — FULL suite **155 files / 1260 tests** green; coverage **92.6% / 86.68% / 92.77% / 94.34%** (≥85% floor); the pre-existing Composer + ExternalStoreChat (attachments/rehydration/voice) suites stayed green (non-regression on Enter-send/paste/drop/attachments + the folds extraction).
- Targeted acceptance: `SSEADAPTER_SKILL_OK`, `COMPOSER_INTEGRATION_OK`, `EXTSTORE_SKILL_OK`.
- All touched files ≤600 LOC (sseAdapter 598, Composer 403, ExternalStoreChat 518, folds 125, AppShell 591).
- Prohibitions honored: NO edits to any 37D-03-owned file (`composer/{api,skillPickerModel,SkillPicker,SkillPill}`, `i18n/resources*`) — `git diff` over the three commits confirms empty; ONE `aura` object (never two); keys intercepted only while open; no editable `/name` token; only a picker-selected name carried.
- Pre-commit hooks (jscpd dup + go vet + whole-tree file-size) green on all three task commits — no `--no-verify`.
- `web/package.json` + lockfile byte-unchanged; NO new deps, migrations, or env.

## Self-Check: PASSED

- FOUND: `web/src/chat/auraRunBody.ts`, `composer/useComposerSkills.ts`, `composer/usePinnedSkill.ts`, `ExternalStoreChat_folds.ts`, `__tests__/ExternalStoreChat.skill.test.tsx` (+ the two hook tests).
- FOUND: commits `a1ca9b75` (Task 1), `fcdc9697` (Task 3), `b53d3dfb` (Task 2).
- All acceptance greps pass; no 37D-03-owned file touched; all touched files ≤600 LOC; full suite + coverage green.

---
*Phase: 37D-composer-skill-picker*
*Completed: 2026-07-10*
