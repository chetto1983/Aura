---
phase: 37D-composer-skill-picker
plan: 03
subsystem: web-composer
tags: [web, react, composer, skills, aria, combobox, listbox, i18n, WEBSKILL, vitest]
type: execute
wave: 2
autonomous: true
dependency_graph:
  requires:
    - phase: 37D-01
      provides: "prd.md Amendment #81 — the RequireAuth-only GET /api/composer/skills contract + the aura.skill envelope (the PRD-first gate)"
    - phase: 37D-02
      provides: "GET /api/composer/skills backend (the {name,description,type} rows the fetch client consumes)"
  provides:
    - "web/src/chat/composer/api.ts — fetchComposerSkills() same-origin client, the degrade-to-no-op source ([] on any throw/empty)"
    - "web/src/chat/composer/skillPickerModel.ts — the PURE combobox model (shouldOpen/filterPickerItems/flattenItems/nextActiveIndex/optionId/pickerKeyAction) 37D-04 drives"
    - "web/src/chat/composer/SkillPicker.tsx — the presentational APG ARIA listbox (grouped options, active aria-selected + JS-scroll, onSelect)"
    - "web/src/chat/composer/SkillPill.tsx — the removable pinned-skill pill (AttachmentChip shape)"
    - "chat.skillPicker.* en+it i18n keys (parity-gated) + resources.composer.ts split module"
  affects:
    - "37D-04 (Composer mounts SkillPicker on the / trigger, renders SkillPill, drives the model's active-index/key mapping, carries aura.skill on send)"
    - "37D-05 (composer-skills e2e drives the picker UI end-to-end)"
tech_stack:
  added: []
  patterns:
    - "PURE decision model (skillPickerModel.ts, React/DOM-free) as the coverage/mutation target — the presentational SkillPicker.tsx is thin and prop-driven; protects the ≥85% web floor"
    - "W3C WAI-ARIA APG combobox/listbox: <button role=option> rows (native-interactive → jsx-a11y-clean), active aria-selected + JS scrollIntoView (aria-activedescendant is not browser-auto-scrolled, Pitfall 6), focus never leaves the input (mousedown preventDefault)"
    - "Degrade-to-no-op fetch client (getJSON throw/empty → []) mirroring governanceApi.ts's same-origin pattern but fail-soft"
    - "Nested-i18n concern split: resources.composer.ts exports the inner skillPicker object referenced as chat.skillPicker (vs the top-level ...spread bundles) to keep resources.ts ≤600 LOC"
key_files:
  created:
    - "web/src/chat/composer/api.ts — COMPOSER_SKILLS_PATH + ComposerSkillRow + fetchComposerSkills() (32 LOC)"
    - "web/src/chat/composer/api.test.ts — 200 rows / missing-skills / 401 / 503 degrade (54 LOC)"
    - "web/src/chat/composer/skillPickerModel.ts — the pure combobox helpers (161 LOC)"
    - "web/src/chat/composer/skillPickerModel.test.ts — every <behavior> case (147 LOC)"
    - "web/src/chat/composer/SkillPicker.tsx — presentational APG ARIA listbox (167 LOC)"
    - "web/src/chat/composer/SkillPill.tsx — removable pinned-skill pill (34 LOC)"
    - "web/src/chat/composer/__tests__/SkillPicker.test.tsx — listbox/options/active/scroll/onSelect/empty + pill (158 LOC)"
    - "web/src/i18n/resources.composer.ts — chat.skillPicker.* en+it copy, split out of resources.ts (32 LOC)"
  modified:
    - "web/src/i18n/resources.ts — references composerSkillPickerEn/It as chat.skillPicker (612 → 591 LOC)"
decisions:
  - "fetchComposerSkills CATCHES any getJSON throw (401/500/503) and returns [] (not a propagated throw) — the plan's prohibition 'the fetch client returns [] on any throw' + behavior 'resolves [] when getJSON throws' + the must-have 'a throw yields []' are authoritative over the looser action-text 'propagates to the caller'; the client is the guaranteed degrade-to-no-op source (D-09)"
  - "Skills grouped into a single flat 'Skills' group + a 'Commands' group (RESEARCH A5 safe default), using exactly the two defined i18n headers (commandsHeader/skillsHeader) — 'group by type' is Claude's Discretion and was NOT taken (no per-type header keys exist); still satisfies 'one-or-more skill groups'"
  - "Commands filter against their `command` id (add-files/new-chat/clear) as the pure-model corpus — the id mirrors the localized label's intent so the React/DOM-free model filters commands without resolving i18n (filterPickerItems has no resolver param)"
  - "Options render as <button type=button role=option tabIndex=-1> (not <li role=option> + onClick) — native buttons are jsx-a11y-exempt from click-events-have-key-events and `option` is an interactive role, so the APG listbox is lint-clean without eslint-disable; the button role override + tabIndex=-1 keep it out of the tab order with focus on the input"
  - "Global option index computed via a pure prefix-sum (indexGroups reduce) not a mutable render-time counter — react-hooks/immutability forbids reassign-after-render"
  - "The new chat.skillPicker.* copy lives in a NEW resources.composer.ts (referenced, not top-level-spread, because the keys nest under chat) — refactor-on-touch to keep resources.ts ≤600 after the additions pushed it to 612"
patterns_established:
  - "Pattern: a pure client-side decision model + a thin presentational component, unit-tested to ~100% as the coverage/mutation target (the ≥85% web-floor discipline for greenfield UI)"
  - "Pattern: APG combobox listbox with <button role=option> rows + JS-scroll active into view + focus-preserving mousedown (the reusable a11y skeleton for future cockpit menus)"
  - "Pattern: nested-i18n concern extraction (resources.composer.ts referenced as chat.skillPicker) when a top-level ...spread bundle would clobber the parent namespace"
requirements_touched: [WEBSKILL-01, WEBSKILL-03]
requirements_completed: []
metrics:
  tasks_completed: 2
  duration: "~40 min (2 TDD tasks, incl. full web suite + coverage + refactor-on-touch)"
  completed: "2026-07-10"
  files_changed: 9
  commits: ["2769cdff", "2f98eb94"]
---

# Phase 37D Plan 03: Composer Skill-Picker Frontend Foundation Summary

**The self-contained, high-coverage picker surface 37D-04 mounts into the live Composer: a same-origin skills client that degrades to `[]` on any failure (the D-09 no-op source), a PURE React/DOM-free combobox model (trigger predicate + incremental filter/group + wrap-around active-index + key→action mapping — the coverage/mutation target), a presentational W3C WAI-ARIA APG listbox (`SkillPicker`, grouped options with active `aria-selected` + JS-scroll-into-view), a removable pinned-skill pill (`SkillPill`), and `chat.skillPicker.*` copy in en+it — with zero HTML-injection surface for server strings and no wiring into `Composer.tsx`/`ExternalStoreChat.tsx` (that is 37D-04).**

## Performance

- **Duration:** ~40 min (2 TDD tasks)
- **Completed:** 2026-07-10
- **Tasks:** 2
- **Files created/modified:** 9 (8 created, 1 modified)

## Accomplishments

- **Skills client (degrade-to-no-op source, D-09):** `fetchComposerSkills()` GETs `/api/composer/skills` via the same-origin `getJSON` and returns `body.skills ?? []`; ANY throw (401/500/503) or a body missing `skills` is caught → `[]`, so the `/` picker never opens on an empty/unreachable list and never leaks an error surface (T-37D-06). 100% covered.
- **Pure combobox model (the coverage/mutation target):** `shouldOpen` (D-05 trigger + D-09 degrade), `filterPickerItems` (Commands + incrementally-filtered Skills groups, empty groups dropped), `flattenItems`, `nextActiveIndex` (wrap-around, `-1` on empty), `optionId`, `pickerKeyAction` — React/DOM-free, 100% covered.
- **APG ARIA listbox (`SkillPicker`):** a `<ul role="listbox">` of grouped `<button role="option">` rows (icon + name + optional subtitle, section headers per D-07); the active option carries `aria-selected` and is JS-`scrollIntoView`'d on change (Pitfall 6); `mousedown` `preventDefault` keeps DOM focus on the input; empty groups render `null`; option indices via a pure prefix-sum (react-hooks/immutability-clean).
- **`SkillPill`:** a removable pinned-skill pill mirroring `AttachmentChip` (accent `Sparkles` + truncated name + ghost `X` with a localized remove `aria-label` firing `onRemove`).
- **i18n en+it parity:** `chat.skillPicker.*` (filterPlaceholder, commandsHeader, skillsHeader, pinnedRemove, cmd{AddFiles,NewChat,Clear}(+Subtitle)) in BOTH locales, parity gate green.
- **Coverage:** the new composer surface (api/model/SkillPicker/SkillPill) is **100% statements/branches/functions/lines**; the FULL web suite (1239 tests) passes with the global coverage gate at 92.55% / 86.66% / 92.71% / 94.28% (≥85% floor).

## Task Commits

Each task was committed atomically (test + implementation together, per the one-task-one-commit discipline + Co-Authored-By trailer):

1. **Task 1: Skills API client + pure combobox model** — `2769cdff` (feat) — `COMPOSER_MODEL_OK`
2. **Task 2: SkillPicker ARIA listbox + SkillPill + en/it i18n** — `2f98eb94` (feat, amended to fold in the readability-token fix) — `SKILLPICKER_OK`

**Plan metadata:** this SUMMARY + STATE.md + ROADMAP.md (docs commit).

_TDD note: both tasks are `tdd="true"`; the behavior cases were authored as the test suite and each task landed as a single atomic `feat(...)` commit (impl+test) per CLAUDE.md "one slice = one commit" — matching the 37D-02 / 37C / 37B sequential-executor precedent._

## Files Created/Modified

**Created:**
- `web/src/chat/composer/api.ts` — `COMPOSER_SKILLS_PATH`, `ComposerSkillRow {name,description,type}`, `fetchComposerSkills(): Promise<readonly ComposerSkillRow[]>` (try/catch → `[]`).
- `web/src/chat/composer/api.test.ts` — 200 rows (+ same-origin/Accept assertions), missing-skills → `[]`, 401 → `[]`, 503 → `[]`.
- `web/src/chat/composer/skillPickerModel.ts` — `shouldOpen`, `pickerFilter`, `filterPickerItems`, `flattenItems`, `nextActiveIndex`, `optionId`, `pickerKeyAction` + `PickerItem`/`PickerGroup`/`QuickCommand` types + `COMMANDS_HEADER_KEY`/`SKILLS_HEADER_KEY`.
- `web/src/chat/composer/skillPickerModel.test.ts` — every `<behavior>` case (trigger, filter/group, drop-empty, command-id match, wrap-around, optionId, key mapping).
- `web/src/chat/composer/SkillPicker.tsx` — presentational `SkillPicker({ groups, activeOptionId, listboxId, labelledById, onSelect })`.
- `web/src/chat/composer/SkillPill.tsx` — `SkillPill({ name, onRemove })`.
- `web/src/chat/composer/__tests__/SkillPicker.test.tsx` — listbox/options render, active `aria-selected`, `scrollIntoView` spy, focus-preserving `mousedown` + `onSelect`, empty→null, unknown-type/no-description fallback, `SkillPill` remove.
- `web/src/i18n/resources.composer.ts` — `composerSkillPickerEn`/`composerSkillPickerIt` (the inner `chat.skillPicker` copy).

**Modified:**
- `web/src/i18n/resources.ts` — imports + references `skillPicker: composerSkillPickerEn/It` in each locale's `chat` group; 612 → **591 LOC**.

## Verification

- `npx tsc --noEmit` — clean across the full web tree.
- `npx eslint --max-warnings=0` — clean on every new/edited file (incl. jsx-a11y for the APG listbox; the `<button role=option>` pattern needed no eslint-disable).
- `npx prettier --write` — all new/edited files formatted.
- `npx vitest run` — FULL web suite green (**1239 tests**); coverage gate **92.55% stmts / 86.66% branch / 92.71% funcs / 94.28% lines** (≥85% floor). Scoped composer surface: **100%** stmts/branch/funcs/lines.
- i18n en↔it parity (`resources.parity.test.ts`) green with the new `chat.skillPicker.*` keys.
- Acceptance guards: `COMPOSER_MODEL_OK`, `SKILLPICKER_OK`; `grep dangerouslySetInnerHTML src/chat/composer/` → 0 (fail-on-hit guard passes); `role="listbox"`/`role="option"`/`scrollIntoView`/`aria-selected` all present in `SkillPicker.tsx`.
- All touched files ≤600 LOC (max `SkillPicker.tsx` 167).
- Pre-commit hooks (jscpd dup + go vet + whole-tree file-size) green on both commits — no `--no-verify`.
- `web/package.json` + lockfile unchanged; NO new deps, migrations, or env vars.

## Threat Model Coverage

- **T-37D-05 (XSS via skill metadata):** mitigated — every skill name/description/subtitle renders as an auto-escaped React text node; NO `dangerouslySetInnerHTML` anywhere in `composer/` (the Task-2 fail-on-hit verify guard actually fails on a match, and it passes clean).
- **T-37D-06 (error surface / broken-open on failed fetch):** mitigated — `fetchComposerSkills` returns `[]` on any throw (no error surface) and `shouldOpen(_, 0) === false`, so an empty/unreachable list degrades the picker to a no-op (D-09), never a leaked error or a broken open state.
- **T-37D-SC (package installs):** N/A — 37D-03 installs NO external packages; `web/package.json` + lockfile byte-unchanged.

## Deviations from Plan

Rules 1-3 auto-applied (no user permission needed); tracked here.

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Refactor-on-touch: extracted `resources.composer.ts` to stay ≤600 LOC**
- **Found during:** Task 2 (commit pre-commit file-size hook).
- **Issue:** The new `chat.skillPicker.*` keys (11 lines × 2 locales) pushed `web/src/i18n/resources.ts` to 612 LOC, over the CLAUDE.md 600-LOC cap; the file-size hook blocked the commit.
- **Fix:** Extracted the `skillPicker` copy into a new `web/src/i18n/resources.composer.ts` (`composerSkillPickerEn`/`It`) and referenced it as `skillPicker: composerSkillPicker*` inside each locale's `chat` group (a nested reference — the existing sub-modules top-level-`...spread`, which would clobber the parent `chat` object here). `resources.ts` back to 591 LOC.
- **Files:** `web/src/i18n/resources.ts`, `web/src/i18n/resources.composer.ts`.
- **Commit:** `2f98eb94`.

**2. [Rule 1 - Bug] Readability-token guard violations (accent-as-text + sub-11px label)**
- **Found during:** Task 2 (full-suite run — `src/__tests__/readabilityTokens.test.ts`).
- **Issue:** The repo-wide readability guard flagged `text-accent` (a fill-only token) used as an icon color in `SkillPicker.tsx`/`SkillPill.tsx`, and `text-[0.6875rem]` (10.66px at the 15.5px operator base, below the 11px floor) on the section header.
- **Fix:** Swapped `text-accent` → `text-accent-text` (the readable accent token used across the cockpit) and `text-[0.6875rem]` → `text-xs`.
- **Files:** `web/src/chat/composer/SkillPicker.tsx`, `web/src/chat/composer/SkillPill.tsx`.
- **Commit:** `2f98eb94` (amended into Task 2).

**Interpretation note (not a deviation):** the plan's `<action>` text describes `fetchComposerSkills` as letting "a throw propagate to the caller", but its own must-have truth ("a throw … yields []"), `<behavior>` ("resolves [] when getJSON throws"), and prohibition ("the fetch client returns [] on any throw") all require the client to catch and return `[]`. Implemented as catch→`[]` (the authoritative, strictly-more-robust reading).

## Requirements

`WEBSKILL-01` and `WEBSKILL-03` are **phase-spanning** and remain `[ ]`: 37D-03 ships the self-contained picker foundation (client + model + component + pill + i18n), but WEBSKILL-01's `/`-triggered menu in the Composer is delivered by the integration (37D-04), and WEBSKILL-03's a11y-in-context + Playwright e2e + the coverage terminal gate is 37D-05. `requirements mark-complete` was intentionally NOT run here — matching the 37D-01 / 37D-02 gate-plan precedent (the terminal plan marks them).

## Known Stubs

None. Every export is wired and exercised: the model is consumed by `SkillPicker` + its tests, the client is unit-tested against a mocked `fetch`, and both components render real data in tests. The prop/function contract (`SkillPicker` props, `skillPickerModel` helpers, `fetchComposerSkills`) is the deliberate seam 37D-04 consumes — not a stub.

## Next Phase Readiness

- **37D-04** mounts `SkillPicker` on the `/` trigger (`shouldOpen`/`pickerFilter`), drives the active index (`nextActiveIndex`/`optionId`) + key mapping (`pickerKeyAction`) from the composer input's `aria-activedescendant`, renders `SkillPill`, wires the three quick commands (add-files/new-chat/clear), and carries `aura.skill` on send. The contract is frozen and typed.
- **37D-05** e2e drives the whole flow (open → filter → select → pill → send) against the live stack.
- No blockers; no external service configuration required.

## Self-Check: PASSED

- FOUND: `web/src/chat/composer/api.ts`, `api.test.ts`, `skillPickerModel.ts`, `skillPickerModel.test.ts`, `SkillPicker.tsx`, `SkillPill.tsx`, `__tests__/SkillPicker.test.tsx`, `web/src/i18n/resources.composer.ts`
- FOUND: commit `2769cdff` (Task 1, feat), commit `2f98eb94` (Task 2, feat)
- All touched files ≤600 LOC (max `SkillPicker.tsx` 167); composer surface 100% coverage; full web suite (1239 tests) + parity green.
- No unintended file deletions (the resources.ts edit replaces the inline block with a 1-line reference; no files removed).

---
*Phase: 37D-composer-skill-picker*
*Completed: 2026-07-10*
