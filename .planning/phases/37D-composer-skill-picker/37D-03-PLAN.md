---
phase: 37D-composer-skill-picker
plan: 03
type: execute
wave: 2
depends_on: ["37D-01"]
files_modified:
  - web/src/chat/composer/api.ts
  - web/src/chat/composer/api.test.ts
  - web/src/chat/composer/skillPickerModel.ts
  - web/src/chat/composer/skillPickerModel.test.ts
  - web/src/chat/composer/SkillPicker.tsx
  - web/src/chat/composer/SkillPill.tsx
  - web/src/chat/composer/__tests__/SkillPicker.test.tsx
  - web/src/i18n/resources.ts
autonomous: true
requirements: [WEBSKILL-01, WEBSKILL-03]
must_haves:
  truths:
    - "fetchComposerSkills() GETs /api/composer/skills via the same-origin getJSON helper and returns body.skills ?? [] — a throw (incl. 401) or an empty body yields [] (the degrade-to-no-op source, D-09)"
    - "skillPickerModel exposes PURE helpers: shouldOpen(text, skillsCount) is true only when text.startsWith('/') AND skillsCount>0 (D-05 trigger + D-09 degrade); filterPickerItems groups the Commands actions + the incrementally-filtered skills; pickerKeyAction maps ArrowUp/ArrowDown/Enter/Escape to up/down/select/close and everything else to none"
    - "SkillPicker renders a role=listbox above the input with grouped role=option rows (icon + name + optional subtitle, section headers per D-07); the option whose id === activeOptionId carries aria-selected=true and is JS-scrolled into view on change (aria-activedescendant is not browser-auto-scrolled, Pitfall 6)"
    - "SkillPill renders a removable pinned-skill pill mirroring AttachmentChip (bordered inline-flex + truncated name + ghost X button with a localized remove aria-label firing onRemove)"
    - "every new chat.skillPicker.* string exists in BOTH en and it (parity test green) — D-10 i18n en+it parity for all new strings (menu labels, group headers, pill, quick-command labels)"
  artifacts:
    - path: "web/src/chat/composer/api.ts"
      provides: "COMPOSER_SKILLS_PATH + ComposerSkillRow type + fetchComposerSkills(): Promise<readonly ComposerSkillRow[]>"
      contains: "fetchComposerSkills"
    - path: "web/src/chat/composer/skillPickerModel.ts"
      provides: "shouldOpen, filterPickerItems, flattenItems, nextActiveIndex, optionId, pickerKeyAction, PickerItem/PickerGroup/QuickCommand types"
      min_lines: 40
    - path: "web/src/chat/composer/SkillPicker.tsx"
      provides: "presentational ARIA listbox (grouped options, active aria-selected + scroll-into-view, onSelect)"
      min_lines: 40
    - path: "web/src/chat/composer/SkillPill.tsx"
      provides: "removable pinned-skill pill (AttachmentChip pattern)"
  key_links:
    - from: "web/src/chat/composer/api.ts"
      to: "/api/composer/skills"
      via: "getJSON same-origin fetch"
      pattern: "COMPOSER_SKILLS_PATH"
    - from: "web/src/chat/composer/SkillPicker.tsx"
      to: "web/src/chat/composer/skillPickerModel.ts"
      via: "consumes PickerGroup[] + optionId"
      pattern: "from './skillPickerModel'"
  prohibitions:
    - "MUST NOT render a skill description or name via dangerouslySetInnerHTML — rows are React text nodes only (auto-escaped; zero XSS surface for server-provided strings)"
    - "MUST NOT open the menu when the skills list is empty — shouldOpen returns false for skillsCount===0 so an empty/unreachable endpoint degrades to a no-op (D-09); the fetch client returns [] on any throw"
    - "MUST NOT move DOM focus into the list — the picker keeps focus on the composer input (APG combobox); the listbox/options are navigated via aria-activedescendant only (focus stays on the input, wired in 37D-04)"
    - "MUST NOT add chat.skillPicker.* keys to only one locale — the resources.parity.test fails unless en AND it both carry them"
    - "MUST NOT edit Composer.tsx / ExternalStoreChat.tsx / sseAdapter.ts in this plan — those are the 37D-04 integration surface; this plan ships the self-contained component + model + client + i18n only"
---

<objective>
Build the self-contained, high-coverage picker foundation the Composer integration (37D-04) mounts: the same-origin skills API client (the degrade-to-no-op source), the PURE combobox model (trigger predicate, incremental filter+group, active-index math, key→action mapping — the coverage/mutation target), the presentational ARIA listbox (`SkillPicker`), the removable pinned-skill pill (`SkillPill`), and the en+it i18n keys. This is the one genuinely greenfield surface of 37D (no in-repo combobox analog — built to the W3C WAI-ARIA APG combobox/listbox pattern, D-07/D-08); keeping the decision logic in a pure model protects the ≥85% web coverage floor.

Purpose: Deliver a deterministic, unit-tested picker component + model with a clean prop/function contract 37D-04 wires into the live composer, with no dependency on the composer text plumbing (which lives in 37D-04).
Output: `composer/api.ts`, `composer/skillPickerModel.ts` (+ tests, high coverage), `composer/SkillPicker.tsx`, `composer/SkillPill.tsx` (+ SkillPicker.test.tsx), and `chat.skillPicker.*` copy in en+it.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37D-composer-skill-picker/37D-RESEARCH.md
@.planning/phases/37D-composer-skill-picker/37D-PATTERNS.md
@web/src/governance/governanceApi.ts
@web/src/chat/attachments/AttachmentChip.tsx
</context>

<artifacts_produced>
This plan produces:
- `web/src/chat/composer/api.ts` → `COMPOSER_SKILLS_PATH = '/api/composer/skills'`; `interface ComposerSkillRow { readonly name: string; readonly description: string; readonly type: string }`; `fetchComposerSkills(): Promise<readonly ComposerSkillRow[]>` (via `getJSON`, `body.skills ?? []`).
- `web/src/chat/composer/skillPickerModel.ts` → `type QuickCommand = 'add-files' | 'new-chat' | 'clear'`; `type PickerItem` (skill | command); `type PickerGroup = { headerKey: string; items: PickerItem[] }`; `shouldOpen(text, skillsCount): boolean`; `filterPickerItems(skills, filter): PickerGroup[]`; `flattenItems(groups): PickerItem[]`; `nextActiveIndex(current, delta, len): number` (wrap-around); `optionId(baseId, index): string`; `pickerKeyAction(key): 'up'|'down'|'select'|'close'|'none'`.
- `web/src/chat/composer/SkillPicker.tsx` → presentational `<SkillPicker groups activeOptionId listboxId labelledById onSelect />` rendering `role="listbox"` above the input with grouped `role="option"` rows (icon + name + subtitle), `aria-selected` on the active option, scroll-active-into-view effect.
- `web/src/chat/composer/SkillPill.tsx` → `<SkillPill name onRemove />` (AttachmentChip-shaped removable pill).
- i18n keys under `chat.skillPicker.*` (en + it): `filterPlaceholder`, `commandsHeader`, `skillsHeader`, `pinnedRemove` (aria, with `{name}`), `cmdAddFiles`(+`Subtitle`), `cmdNewChat`(+`Subtitle`), `cmdClear`(+`Subtitle`).
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Skills API client + pure combobox model (trigger, filter/group, key mapping) — the coverage target</name>
  <files>web/src/chat/composer/api.ts, web/src/chat/composer/api.test.ts, web/src/chat/composer/skillPickerModel.ts, web/src/chat/composer/skillPickerModel.test.ts</files>
  <behavior>
    - fetchComposerSkills resolves body.skills for a 200 {skills:[{name,description,type}]}; resolves [] when getJSON throws (mock a 401/500 throw) or the body has no skills field (degrade source, D-09).
    - shouldOpen('/', 3) === true; shouldOpen('/foo', 3) === true; shouldOpen('', 3) === false; shouldOpen('a/b', 3) === false (literal mid-text slash); shouldOpen('/', 0) === false (empty list ⇒ never opens, D-09).
    - filterPickerItems(skills, '') returns a Commands group (add-files/new-chat/clear) + one-or-more skill groups covering all skills; filterPickerItems(skills, 'cre') keeps only skills whose name/description matches 'cre' case-insensitively (and drops empty groups); a command matches when its localized label/key matches the filter token.
    - nextActiveIndex(0, -1, 5) === 4 (wrap up); nextActiveIndex(4, 1, 5) === 0 (wrap down); nextActiveIndex(-1, 1, 5) === 0.
    - optionId('skpick', 2) is stable/unique per index; pickerKeyAction('ArrowDown')==='down'; ('ArrowUp')==='up'; ('Enter')==='select'; ('Escape')==='close'; ('a')==='none'.
  </behavior>
  <read_first>
    - web/src/governance/governanceApi.ts:15,55-63,209-214 — getJSON import + the `{skills: [...]}` envelope + `body.skills ?? []` fallback + the SkillRow shape to mirror for ComposerSkillRow (same-origin, throws on non-200 incl 401 → the picker degrades to []).
    - web/src/api/json.ts — the getJSON signature (credentials:'same-origin', non-200 throws Error) so the test can mock a throw for the degrade case.
    - .planning/phases/37D-composer-skill-picker/37D-RESEARCH.md § Code Examples ("New skills API client", "Reading the composer text reactively") + § D-04 (grouping uses Type/Language — no richer category field exists) — the client shape + the trigger/degrade predicate + the grouping taxonomy (group by Type, with a flat "Skills" fallback; a separate "Commands" group).
    - web/src/chat/composer/ (new dir) — confirm no existing file; this establishes the composer/ subtree.
  </read_first>
  <action>
    Create web/src/chat/composer/api.ts mirroring governanceApi.ts: export `COMPOSER_SKILLS_PATH = '/api/composer/skills'`, `ComposerSkillRow {name, description, type}`, and `async fetchComposerSkills(): Promise<readonly ComposerSkillRow[]>` doing `const body = await getJSON<{skills?: readonly ComposerSkillRow[]}>(COMPOSER_SKILLS_PATH); return body.skills ?? []` — a throw propagates to the caller which treats it as [] (the 37D-04 fetch effect swallows it to []; document that the throw path is the degrade source). Create web/src/chat/composer/skillPickerModel.ts with the pure helpers: `shouldOpen(text, skillsCount)` = `text.startsWith('/') && skillsCount > 0`; `filterPickerItems(skills, filter)` = build a Commands group (add-files/new-chat/clear as PickerItem command entries) + skill groups (group by `type`, or a single "Skills" group when types are sparse — Claude's Discretion per D-04/A5) with an incremental case-insensitive filter over name+description (and the command labels), dropping empty groups; `flattenItems`, `nextActiveIndex` (wrap-around), `optionId(baseId, index)`, `pickerKeyAction(key)` mapping the four navigation keys. Keep skillPickerModel.ts pure (no React, no DOM). Write api.test.ts (mock getJSON: 200 body, throw, missing-skills) and skillPickerModel.test.ts covering every `<behavior>` case to high coverage. Refactor-on-touch not applicable (new files); keep each ≤600 LOC.
  </action>
  <acceptance_criteria>
    - `web/src/chat/composer/api.ts` exports `fetchComposerSkills`, `COMPOSER_SKILLS_PATH`, `ComposerSkillRow`; hits `/api/composer/skills` via getJSON.
    - `web/src/chat/composer/skillPickerModel.ts` exports `shouldOpen`, `filterPickerItems`, `flattenItems`, `nextActiveIndex`, `optionId`, `pickerKeyAction` and is React/DOM-free.
    - `cd D:/Repo/Aura/web && npx vitest run src/chat/composer/api.test.ts src/chat/composer/skillPickerModel.test.ts` exits 0 with both source files at high coverage (model ≥95%).
    - `cd D:/Repo/Aura/web && npx tsc --noEmit` clean.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/composer/api.test.ts src/chat/composer/skillPickerModel.test.ts && npx tsc --noEmit && echo COMPOSER_MODEL_OK</automated>
  </verify>
  <done>The skills client returns [] on any throw (degrade source) and the pure combobox model (trigger/filter/group/index/key) is unit-tested to high coverage, React/DOM-free.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: SkillPicker ARIA listbox + SkillPill + chat.skillPicker.* i18n (en+it)</name>
  <files>web/src/chat/composer/SkillPicker.tsx, web/src/chat/composer/SkillPill.tsx, web/src/chat/composer/__tests__/SkillPicker.test.tsx, web/src/i18n/resources.ts</files>
  <behavior>
    - SkillPicker renders role="listbox" (id === the listboxId prop) containing grouped role="option" rows for the given groups; each option has a stable id (optionId) and renders an icon + the name + an optional subtitle (description); section headers per group (D-07).
    - The option whose id === activeOptionId has aria-selected="true"; all others aria-selected="false"; on activeOptionId change, that option's scrollIntoView is invoked (spy asserts the JS-scroll, Pitfall 6).
    - Clicking (or pointer-selecting) an option calls onSelect with that PickerItem.
    - groups === [] (or all empty) renders nothing / an empty fragment (degrade-to-no-op, D-09) — no throw.
    - SkillPill renders the pinned skill name (truncated) + a ghost X button with aria-label from t('chat.skillPicker.pinnedRemove', {name}); clicking it calls onRemove.
    - resources.parity: every chat.skillPicker.* key exists in en AND it.
  </behavior>
  <read_first>
    - web/src/chat/attachments/AttachmentChip.tsx:12-35 — the EXACT removable-chip shape to copy for SkillPill (bordered inline-flex span + truncated label + ghost icon Button with aria-label firing onRemove + the X lucide icon).
    - web/src/conversations/ConversationSidebar.tsx:226-288,363-385 — the menu open/close + Escape/outside-click + above-anchor positioning mechanics to borrow for the SkillPicker popover shell (role/positioning only; the trigger differs — it is the composer `/`, wired in 37D-04).
    - web/src/chat/displays/CitationBubble.tsx:2,27-37 — the `Partial<Record<..., LucideIcon>>` + `File` fallback icon-map precedent for per-row skill/command icons (lucide-react).
    - .planning/phases/37D-composer-skill-picker/37D-PATTERNS.md § "NEW SkillPicker.tsx (PARTIAL analog)" — the three-concern decomposition (pill exact, open/close partial, combobox core greenfield) + the explicit W3C APG combobox target.
    - .planning/phases/37D-composer-skill-picker/37D-CONTEXT.md D-07/D-08 — the menu layout (above input, grouped, icon+name+subtitle) + the APG listbox a11y (aria-activedescendant target, focus stays on the input, JS-scroll active into view).
    - web/src/i18n/resources.ts — the en+it `chat.composer` / `chat.attachments` key groups (add a sibling `chat.skillPicker` group in BOTH locales) + web/src/i18n/__tests__/resources.parity.test.ts (the CI parity gate).
  </read_first>
  <action>
    Create web/src/chat/composer/SkillPicker.tsx as a PRESENTATIONAL component `SkillPicker({ groups, activeOptionId, listboxId, labelledById, onSelect })`: render a positioned-above popover containing `<ul role="listbox" id={listboxId}>` with, per group, a section header (localized via chat.skillPicker.commandsHeader / skillsHeader or the group headerKey) and `<li role="option" id={optionId(...)} aria-selected={id===activeOptionId}>` rows carrying a lucide icon + the name + an optional description subtitle (D-07). Add a `useEffect` keyed on activeOptionId that calls `document.getElementById(activeOptionId)?.scrollIntoView({block:'nearest'})` (JS-scroll the active option, Pitfall 6). Wire `onMouseDown`/`onClick` on each option (preventDefault to keep focus on the input) to `onSelect(item)`. When groups is empty render null (degrade, D-09). Do NOT own the open/active state or the input ARIA — that is the Composer's job (37D-04); this component is driven by props. Create web/src/chat/composer/SkillPill.tsx copying the AttachmentChip shape: a bordered inline-flex span with the truncated skill name + a ghost `Button variant="ghost" size="icon"` carrying `aria-label={t('chat.skillPicker.pinnedRemove',{name})}` and an `X` icon, firing `onRemove`. Add the `chat.skillPicker.*` key group to BOTH en and it in resources.ts (filterPlaceholder, commandsHeader, skillsHeader, pinnedRemove, cmdAddFiles(+Subtitle), cmdNewChat(+Subtitle), cmdClear(+Subtitle)). Write __tests__/SkillPicker.test.tsx (Vitest + @testing-library/react) covering the `<behavior>` cases (listbox+options render, aria-selected on active, scrollIntoView spy on active change, onSelect fires, empty→null, SkillPill remove). Refactor-on-touch: keep files ≤600 LOC; render server strings as text nodes only (no dangerouslySetInnerHTML).
  </action>
  <acceptance_criteria>
    - `grep -q 'role="listbox"' web/src/chat/composer/SkillPicker.tsx` AND `grep -q 'role="option"' web/src/chat/composer/SkillPicker.tsx` AND `grep -q "scrollIntoView" web/src/chat/composer/SkillPicker.tsx`.
    - `grep -q "aria-selected" web/src/chat/composer/SkillPicker.tsx` (active option marked).
    - `if grep -rq "dangerouslySetInnerHTML" web/src/chat/composer/; then exit 1; fi` passes (fail-on-hit guard — server strings render as text nodes; the check ACTUALLY fails the build on any `dangerouslySetInnerHTML` sink, unlike the prior always-exit-0 chain).
    - `cd D:/Repo/Aura/web && npx vitest run src/chat/composer/__tests__/SkillPicker.test.tsx` exits 0 (listbox/options, active aria-selected + scroll spy, onSelect, empty→null, pill remove).
    - `cd D:/Repo/Aura/web && npx vitest run src/i18n/__tests__/resources.parity.test.ts` exits 0 (en/it symmetry holds with the new chat.skillPicker.* keys).
    - `cd D:/Repo/Aura/web && npx tsc --noEmit` clean.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/composer/__tests__/SkillPicker.test.tsx src/i18n/__tests__/resources.parity.test.ts && if grep -rq "dangerouslySetInnerHTML" src/chat/composer/; then echo XSS_FOUND; exit 1; fi && npx tsc --noEmit && echo SKILLPICKER_OK</automated>
  </verify>
  <done>SkillPicker renders the grouped ARIA listbox (active aria-selected + JS-scroll), SkillPill is a removable AttachmentChip-shaped pill, and chat.skillPicker.* copy exists in en+it (parity green); server strings render as text nodes only.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| server skills payload → SkillPicker rendering | Server-provided skill name/description strings are rendered in the menu; they must not execute as markup |
| endpoint availability → menu open state | An empty/unreachable list must not open a broken menu or leak an error surface |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37D-05 | Tampering / Elevation (XSS via skill metadata) | SkillPicker option rows | mitigate | Names/descriptions render as React text nodes (auto-escaped); NO dangerouslySetInnerHTML anywhere in composer/ — enforced by Task 2's fail-on-hit verify guard `if grep -rq "dangerouslySetInnerHTML" src/chat/composer/; then echo XSS_FOUND; exit 1; fi` which actually FAILS the check on a match (the prior grep-and-echo-or chain exited 0 whether or not the sink was present, enforcing nothing) |
| T-37D-06 | Information Disclosure / DoS (error surface on failed fetch) | fetchComposerSkills + shouldOpen | mitigate | getJSON throw ⇒ [] (no error surface); shouldOpen(_, 0) === false ⇒ an empty/unreachable list degrades the menu to a no-op (D-09), never a leaked error or a broken open state |
| T-37D-SC | Tampering | npm/pip/cargo installs | accept | 37D installs NO external packages (RESEARCH § Package Legitimacy Audit: N/A) |
</threat_model>

<verification>
- `cd web && npx vitest run src/chat/composer/` green with api.ts + skillPickerModel.ts at high coverage and SkillPicker/SkillPill tests passing.
- i18n parity test green with the new chat.skillPicker.* keys in en+it.
- `npx tsc --noEmit` clean; no dangerouslySetInnerHTML in composer/.
</verification>

<success_criteria>
- A same-origin skills client that degrades to [] on any failure, a pure combobox model (trigger/filter/group/index/key) unit-tested to high coverage, a presentational ARIA listbox (grouped options, active aria-selected + JS-scroll-into-view), and a removable pinned-skill pill — all self-contained with a clean contract for 37D-04.
- chat.skillPicker.* copy exists in en+it (parity green); zero HTML-injection surface for server strings.
</success_criteria>

<output>
Create `.planning/phases/37D-composer-skill-picker/37D-03-SUMMARY.md` when done.
</output>
