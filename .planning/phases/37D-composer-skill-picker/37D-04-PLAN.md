---
phase: 37D-composer-skill-picker
plan: 04
type: execute
wave: 3
depends_on: ["37D-02", "37D-03"]
files_modified:
  - web/src/chat/sseAdapter.ts
  - web/src/chat/__tests__/sseAdapter.test.ts
  - web/src/chat/auraRunBody.ts
  - web/src/chat/composer/useComposerSkills.ts
  - web/src/chat/composer/useComposerSkills.test.ts
  - web/src/chat/composer/usePinnedSkill.ts
  - web/src/chat/composer/usePinnedSkill.test.ts
  - web/src/chat/ExternalStoreChat.tsx
  - web/src/chat/__tests__/ExternalStoreChat.skill.test.tsx
  - web/src/AppShell.tsx
  - web/src/chat/Composer.tsx
  - web/src/chat/__tests__/Composer.test.tsx
autonomous: true
requirements: [WEBSKILL-01, WEBSKILL-02, WEBSKILL-03]
must_haves:
  truths:
    - "streamRun folds an optional skill into the SAME aura envelope as attachment_ids: when opts.skill is set the POST body carries aura:{attachment_ids?, skill}; when unset the body is byte-identical to today (no empty aura key)"
    - "ExternalStoreChat lifts a pinnedSkill state (mirroring the uploads seam), fetches the skills list once (degrading to [] on any throw — D-09), passes both to Composer, reads pinnedSkill?.name into streamRun in onNew, and clears the pinned skill after a send (like uploads.clearReady)"
    - "typing / as the first char of an empty composer opens the SkillPicker (shouldOpen); typing filters incrementally; ArrowUp/Down move the active option (aria-activedescendant on the input); Enter selects; Escape closes; when the menu is CLOSED, Enter sends and paste/drop still work (D-09 — keys intercepted ONLY while open)"
    - "selecting a skill sets the pinned pill (removable) and clears the /filter text; selecting add-files opens the file picker (existing Paperclip handler); new-chat calls the threaded startNewConversation; clear resets the composer text + pinned pill + pending attachments — all quick actions are pure client (no agent round-trip)"
    - "the composer input carries the APG combobox a11y: role/aria-expanded reflecting the menu, aria-controls to the listbox id, aria-activedescendant to the active option; DOM focus stays on the input (never moves into the list)"
  artifacts:
    - path: "web/src/chat/composer/useComposerSkills.ts"
      provides: "one-shot skills fetch hook (fetchComposerSkills → [] on throw, the D-09 degrade source)"
    - path: "web/src/chat/Composer.tsx"
      provides: "SkillPicker mount + / trigger + composed onChange/onKeyDown + input combobox ARIA + SkillPill row + add-files/new-chat/clear quick actions"
      contains: "SkillPicker"
    - path: "web/src/chat/ExternalStoreChat.tsx"
      provides: "pinnedSkill lift + skills fetch + skill carried into streamRun + cleared after send + onNewChat thread-through"
      contains: "pinnedSkill"
  key_links:
    - from: "web/src/chat/ExternalStoreChat.tsx"
      to: "web/src/chat/sseAdapter.ts"
      via: "streamRun({ ..., skill: pinnedSkill?.name })"
      pattern: "skill:"
    - from: "web/src/chat/Composer.tsx"
      to: "web/src/chat/composer/skillPickerModel.ts"
      via: "shouldOpen/filterPickerItems/pickerKeyAction/optionId"
      pattern: "shouldOpen"
    - from: "web/src/AppShell.tsx"
      to: "web/src/chat/ExternalStoreChat.tsx"
      via: "onNewChat={startNewConversation}"
      pattern: "onNewChat"
  prohibitions:
    - "MUST NOT intercept Enter / preventDefault any key when the menu is CLOSED — Enter-to-send, paste, and drop must remain intact (D-09); intercept keys ONLY when menuOpen"
    - "MUST NOT open the menu on a mid-text slash — the trigger is shouldOpen(text, skills.length) (text.startsWith('/') && skills.length>0); a literal slash typed mid-message never opens it (D-05)"
    - "MUST NOT insert an editable /name token into the composer or parse skill text on send — the pinned skill rides the aura.skill field only (D-01/D-06); clear the /filter text on select"
    - "MUST NOT send free-form text as aura.skill — pinnedSkill is set ONLY from a SkillPicker selection over the fetched list; only pinnedSkill?.name is carried"
    - "MUST NOT emit two aura keys — fold skill and attachment_ids into ONE aura object; when neither is set, emit no aura key (byte-identical to today)"
    - "MUST NOT touch web/src/chat/composer/{api,skillPickerModel,SkillPicker,SkillPill}.ts(x) or web/src/i18n/resources.ts — those are 37D-03-owned; this plan consumes them"
---

<objective>
Wire the 37D-03 picker into the live composer and the run path: extend the `aura` run envelope with the pinned `skill` field (37D-02 decodes it), lift a `pinnedSkill` state + a one-shot skills fetch into `ExternalStoreChat` (mirroring the `uploads` seam), and integrate the `/`-trigger + ARIA combobox key handling + pinned pill + quick-command actions into `Composer.tsx` — WITHOUT breaking the existing Enter-to-send / paste / drop behavior (keys intercepted only while the menu is open, D-09).

Purpose: Turn the standalone component + backend contract into the working end-to-end feature: type `/` → filter → select → pill → send carries `aura.skill` → the server applies the skill first; plus the add-files/new-chat/clear quick actions.
Output: `sseAdapter.ts` (+test) skill field, `useComposerSkills.ts` (+test), `ExternalStoreChat.tsx` (+test) pinnedSkill lift + send, `AppShell.tsx` onNewChat prop, `Composer.tsx` (+test) picker integration.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37D-composer-skill-picker/37D-RESEARCH.md
@.planning/phases/37D-composer-skill-picker/37D-PATTERNS.md
@web/src/chat/Composer.tsx
@web/src/chat/ExternalStoreChat.tsx
@web/src/chat/sseAdapter.ts
</context>

<artifacts_produced>
This plan produces:
- `web/src/chat/sseAdapter.ts` → `StreamRunOptions.skill?: string` + the `aura` body folds `{attachment_ids?, skill?}` into ONE object (emitted only when either is set).
- `web/src/chat/auraRunBody.ts` → `buildAuraRunBody(id, opts)` (NEW sibling): assembles the POST body `{ threadId, messages, aura? }`, folding `{attachment_ids?, skill?}` into ONE `aura` (or none); extracted OUT of `sseAdapter.ts` so that file stays ≤600 LOC (it is currently EXACTLY 600) while the `skill` fold lands here.
- `web/src/chat/composer/useComposerSkills.ts` → `useComposerSkills(): readonly ComposerSkillRow[]` (fetch once on mount via `fetchComposerSkills`, catch → [] — the D-09 degrade source).
- `web/src/chat/composer/usePinnedSkill.ts` → `usePinnedSkill()` (NEW hook mirroring the `useAttachmentUploads` lifted-seam pattern): owns the `pinnedSkill` state + `setPinnedSkill` internally and returns `{ pinnedSkill, setPinnedSkill }`, so `ExternalStoreChat` destructures the seam instead of an inline `useState` — keeps `ExternalStoreChat.tsx` ≤600 LOC (currently 599).
- `web/src/chat/ExternalStoreChat.tsx` → the `pinnedSkill`/`setPinnedSkill` seam (from the new `usePinnedSkill` hook) + `useComposerSkills()`, both passed to `<Composer>`; `skill: pinnedSkill?.name` carried into `streamRun` in `onNew`; `setPinnedSkill(null)` after a send; an `onNewChat` prop threaded down to Composer.
- `web/src/AppShell.tsx` → `onNewChat={startNewConversation}` passed into `<ExternalStoreChat>`.
- `web/src/chat/Composer.tsx` → extended `ComposerProps` (`skills`, `pinnedSkill`, `onPinSkill`, `onNewChat`); the `/`-trigger via `useAuiState((s)=>s.composer.text)` + `shouldOpen`; composed `onChange`/`onKeyDown` on `ComposerPrimitive.Input` (intercept ↑/↓/Enter/Esc ONLY when open); the input's combobox ARIA (`aria-expanded`/`aria-controls`/`aria-activedescendant`); the `<SkillPill>` row beside the attachment chips; the `<SkillPicker>` mount above the input; the add-files/new-chat/clear quick-action handlers.
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: sseAdapter — carry the pinned skill on the aura run envelope (WEBSKILL-02)</name>
  <files>web/src/chat/sseAdapter.ts, web/src/chat/auraRunBody.ts, web/src/chat/__tests__/sseAdapter.test.ts</files>
  <behavior>
    - streamRun({..., attachmentIds:['a1'], skill:'skill-creator'}) POSTs a body whose parsed aura === { attachment_ids: ['a1'], skill: 'skill-creator' } (ONE aura object).
    - streamRun({..., skill:'skill-creator'}) with no attachmentIds POSTs aura === { skill: 'skill-creator' }.
    - streamRun({...}) with neither set POSTs NO aura key (byte-identical to today).
  </behavior>
  <read_first>
    - web/src/chat/sseAdapter.ts:465-479 — the StreamRunOptions interface (add `readonly skill?: string;` beside attachmentIds).
    - web/src/chat/sseAdapter.ts:574-590 — the inline /agent/run POST body (`body: JSON.stringify({ threadId, messages, ...(attachmentIds ? { aura: { attachment_ids } } : {}) })`); EXTRACT this whole body assembly into a NEW sibling `web/src/chat/auraRunBody.ts` as `buildAuraRunBody(id, opts)` and fold `skill` into the SAME aura object there (emit aura only when attachmentIds?.length OR skill is set; keep it ONE object). sseAdapter.ts is EXACTLY 600 LOC — the extraction is what keeps it ≤600 once the field is added.
    - web/src/chat/__tests__/sseAdapter.test.ts (and sseAdapter_network.test.ts) — the existing body-shape/fetch assertions to extend (find how the POST body is captured + parsed).
    - .planning/phases/37D-composer-skill-picker/37D-PATTERNS.md § "MOD sseAdapter.ts" — the exact one-field extension shape (do NOT emit two aura keys).
  </read_first>
  <action>
    Create a NEW sibling file `web/src/chat/auraRunBody.ts` exporting `buildAuraRunBody(id: string, opts: StreamRunOptions)` that returns the full POST body object: `{ threadId: opts.threadId, messages: [{ id, role: "user", content: opts.userText }], ...(Object.keys(aura).length > 0 ? { aura } : {}) }` where `const aura = { ...(opts.attachmentIds?.length ? { attachment_ids: opts.attachmentIds } : {}), ...(opts.skill ? { skill: opts.skill } : {}) }` — so a set skill and/or attachments produce ONE aura object and neither produces no aura key. Add `readonly skill?: string;` to `StreamRunOptions` in sseAdapter.ts (~L469), and REPLACE the inline `body: JSON.stringify({ ... })` at sseAdapter.ts:580-586 with `body: JSON.stringify(buildAuraRunBody(id, opts))`. This removes ~7 inline lines from sseAdapter.ts (currently EXACTLY 600 LOC) so it stays ≤600 after the field is added, and lands the `skill` fold in `auraRunBody.ts`. Extend sseAdapter.test.ts with the three `<behavior>` cases (parse the captured POST body and assert the aura object) — these three cases exercise every branch of `buildAuraRunBody`, so it is covered without a separate test file. Refactor-on-touch: keep sseAdapter.ts and auraRunBody.ts each ≤600 LOC; no dead code.
  </action>
  <acceptance_criteria>
    - `grep -q "skill" web/src/chat/sseAdapter.ts` (the `StreamRunOptions.skill?` field) AND `grep -q "buildAuraRunBody" web/src/chat/auraRunBody.ts` (the extracted body assembler that carries the `skill` fold) AND `grep -q "buildAuraRunBody(id, opts)" web/src/chat/sseAdapter.ts` (sseAdapter delegates to it).
    - `wc -l < web/src/chat/sseAdapter.ts` ≤ 600 (the pre-commit check-file-size hook must not block the commit).
    - The body emits ONE `aura` object (no duplicate aura key) — asserted by parsing the POST body in the test.
    - `cd D:/Repo/Aura/web && npx vitest run src/chat/__tests__/sseAdapter.test.ts` exits 0 with the three aura-shape cases.
    - `cd D:/Repo/Aura/web && npx tsc --noEmit` clean.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/__tests__/sseAdapter.test.ts && npx tsc --noEmit && echo SSEADAPTER_SKILL_OK</automated>
  </verify>
  <done>streamRun carries an optional skill folded into the single aura envelope (with attachment_ids when both present, no aura key when neither) via the extracted `buildAuraRunBody` helper, proven by the parsed-body tests; sseAdapter.ts stays ≤600 LOC.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: ExternalStoreChat pinnedSkill lift + skills fetch + skill send + clear-after-send + AppShell onNewChat (WEBSKILL-01/02)</name>
  <files>web/src/chat/composer/useComposerSkills.ts, web/src/chat/composer/useComposerSkills.test.ts, web/src/chat/composer/usePinnedSkill.ts, web/src/chat/composer/usePinnedSkill.test.ts, web/src/chat/ExternalStoreChat.tsx, web/src/chat/__tests__/ExternalStoreChat.skill.test.tsx, web/src/AppShell.tsx</files>
  <behavior>
    - useComposerSkills fetches once on mount and returns the rows; when fetchComposerSkills throws (mock a rejection) it returns [] (the D-09 degrade), never throwing to the caller.
    - onNew carries skill: pinnedSkill?.name into streamRun — with a pinned skill the streamRun mock receives skill==='<name>'; with none, skill is undefined.
    - after a successful send, pinnedSkill is reset to null (the pill does not persist to the next turn), mirroring uploads.clearReady.
    - AppShell passes onNewChat={startNewConversation} to ExternalStoreChat, which threads it to Composer.
  </behavior>
  <read_first>
    - web/src/chat/ExternalStoreChat.tsx:155-176 — the ExternalStoreChatProps (add `onNewChat?: () => void`) + :178-194 the state block (consume `const { pinnedSkill, setPinnedSkill } = usePinnedSkill()` + `const skills = useComposerSkills()` beside `const uploads = useAttachmentUploads(threadId)` — do NOT inline a `useState` here; the state lives in the new usePinnedSkill hook to keep this file ≤600 LOC, currently 599).
    - web/src/chat/attachments/useAttachmentUploads.ts:1-17 — the lifted-seam hook shape to MIRROR for usePinnedSkill: a hook that owns `useState` internally and returns an object (`items/clearReady/...`); usePinnedSkill returns `{ pinnedSkill, setPinnedSkill }` (ComposerSkillRow | null) the same way useAttachmentUploads returns its seam.
    - web/src/chat/ExternalStoreChat.tsx:205-267 — onNew: read pinnedSkill?.name, add `skill: pinnedSkill?.name` into the streamRun call (beside attachmentIds:251), and after the await add `setPinnedSkill(null)` next to `if (readyAttachmentIds.length > 0) uploads.clearReady()` (:267).
    - web/src/chat/ExternalStoreChat.tsx:594 — the `<Composer uploads={uploads} draftPrompt={draftPrompt} />` render; add `skills`, `pinnedSkill`, `onPinSkill={setPinnedSkill}`, `onNewChat`.
    - web/src/AppShell.tsx:251-263 (startNewConversation) + :428-435 (the <ExternalStoreChat> instantiation) — add `onNewChat={startNewConversation}`.
    - web/src/chat/composer/api.ts (from 37D-03) — fetchComposerSkills + ComposerSkillRow to consume.
    - web/src/chat/__tests__/ExternalStoreChat.attachments.test.tsx — the harness (streamRun mock + onNew drive) to mirror for the skill-send + clear-after-send test.
    - .planning/phases/37D-composer-skill-picker/37D-RESEARCH.md § Pattern 3 ("mirror the uploads seam") — the four touch points (create state, pass to Composer, read in onNew, clear after send).
  </read_first>
  <action>
    Create web/src/chat/composer/useComposerSkills.ts: `useComposerSkills(): readonly ComposerSkillRow[]` — a useState<readonly ComposerSkillRow[]>([]) + a useEffect that calls fetchComposerSkills() once, sets the rows on success and swallows any rejection to [] (the D-09 degrade), with an abort/mounted guard. Create web/src/chat/composer/usePinnedSkill.ts MIRRORING the useAttachmentUploads lifted-seam pattern: `usePinnedSkill()` owns `const [pinnedSkill, setPinnedSkill] = useState<ComposerSkillRow | null>(null)` internally and returns `{ pinnedSkill, setPinnedSkill }` — extracting the state OUT of ExternalStoreChat.tsx (currently 599 LOC) so it stays ≤600 after the wiring below. In ExternalStoreChat.tsx: add `const skills = useComposerSkills()` and `const { pinnedSkill, setPinnedSkill } = usePinnedSkill()` beside the uploads seam (NO inline useState here); add `onNewChat?: () => void` to ExternalStoreChatProps and destructure it; in onNew add `skill: pinnedSkill?.name` to the streamRun options (beside attachmentIds) and `setPinnedSkill(null)` after the await beside the uploads.clearReady() call; pass `skills`, `pinnedSkill`, `onPinSkill={setPinnedSkill}`, `onNewChat` to `<Composer>`. In AppShell.tsx add `onNewChat={startNewConversation}` to the <ExternalStoreChat> instantiation. Create useComposerSkills.test.ts (renderHook: success returns rows; rejection returns []) and usePinnedSkill.test.ts (renderHook: initial null; setPinnedSkill pins a row; setPinnedSkill(null) clears). Create ExternalStoreChat.skill.test.tsx mirroring the attachments test: assert skill is carried into the streamRun mock and pinnedSkill is cleared after send. Refactor-on-touch: keep ExternalStoreChat.tsx ≤600 LOC (verify with wc -l); no dead code.
  </action>
  <acceptance_criteria>
    - `grep -q "useComposerSkills" web/src/chat/ExternalStoreChat.tsx` AND `grep -q "usePinnedSkill" web/src/chat/ExternalStoreChat.tsx` (state consumed from the extracted hook, not an inline useState) AND `grep -q "skill: pinnedSkill" web/src/chat/ExternalStoreChat.tsx`.
    - `grep -q "export function usePinnedSkill" web/src/chat/composer/usePinnedSkill.ts` (the NEW lifted-seam hook exists).
    - `grep -q "onNewChat={startNewConversation}" web/src/AppShell.tsx`.
    - useComposerSkills returns [] on a fetch rejection (degrade) — asserted in useComposerSkills.test.ts.
    - `cd D:/Repo/Aura/web && npx vitest run src/chat/composer/useComposerSkills.test.ts src/chat/composer/usePinnedSkill.test.ts src/chat/__tests__/ExternalStoreChat.skill.test.tsx` exits 0 (skill carried + cleared-after-send + degrade + pin/clear).
    - `wc -l < web/src/chat/ExternalStoreChat.tsx` ≤ 600 (the pre-commit check-file-size hook must not block the commit).
    - `cd D:/Repo/Aura/web && npx tsc --noEmit` clean.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/composer/useComposerSkills.test.ts src/chat/composer/usePinnedSkill.test.ts src/chat/__tests__/ExternalStoreChat.skill.test.tsx && npx tsc --noEmit && echo EXTSTORE_SKILL_OK</automated>
  </verify>
  <done>ExternalStoreChat consumes pinnedSkill from the extracted usePinnedSkill hook + a degrade-safe skills fetch, carries pinnedSkill?.name into streamRun, clears it after send, and threads onNewChat from AppShell's startNewConversation; ExternalStoreChat.tsx stays ≤600 LOC.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Composer integration — / trigger + ARIA combobox keys + SkillPill + quick actions (WEBSKILL-01/03)</name>
  <files>web/src/chat/Composer.tsx, web/src/chat/__tests__/Composer.test.tsx</files>
  <behavior>
    - Typing '/' as the first char of an empty composer (skills.length>0) opens the SkillPicker; the input gains aria-expanded="true", aria-controls=<listboxId>, aria-activedescendant=<active option id>; DOM focus stays on the textarea (document.activeElement === the input).
    - Typing '/cre' filters the options; ArrowDown/ArrowUp move the active option (aria-activedescendant updates); Enter selects the active option (preventDefault — does NOT send); Escape closes the menu (preventDefault — does NOT cancel a run).
    - When the menu is CLOSED: Enter triggers the normal send (not intercepted); a literal '/' typed mid-text (e.g. 'a/b') never opens the menu; paste/drop handlers on ComposerPrimitive.Root are untouched.
    - Selecting a skill sets the pinned pill (onPinSkill(row)) and clears the /filter text; the SkillPill renders and its X calls onPinSkill(null).
    - Selecting add-files calls fileInputRef.current?.click(); new-chat calls onNewChat?.(); clear resets the composer text + pinned pill + pending attachments — none of the three fire an agent run.
    - skills empty / undefined ⇒ '/' never opens the menu (degrade-to-no-op, D-09).
  </behavior>
  <read_first>
    - web/src/chat/Composer.tsx:31-34 (ComposerProps — extend with skills/pinnedSkill/onPinSkill/onNewChat, mirroring the optional uploads? prop) + :43-47 (useAui/useAuiState selectors; s.composer.text is the analog of s.composer.dictation at :47 — read the live composer text reactively; fallback: an onChange handler) + :80 (aui.composer().getState().text) + :89-96 (addFiles/handleFileChange) + :98-111 (handlePaste/handleDrop/handleDragOver — do NOT touch) + :193-205 (ComposerPrimitive.Root + the uploads.items.map AttachmentChip pill row — add the SkillPill beside it) + :219-263 (the Paperclip fileInputRef.click handler = add-files, and the ComposerPrimitive.Input where onChange/onKeyDown + the combobox ARIA attach).
    - web/src/chat/composer/skillPickerModel.ts + SkillPicker.tsx + SkillPill.tsx (from 37D-03) — shouldOpen/filterPickerItems/flattenItems/nextActiveIndex/optionId/pickerKeyAction + the SkillPicker/SkillPill props to consume.
    - .planning/phases/37D-composer-skill-picker/37D-RESEARCH.md § Pattern 5 (composeEventHandlers over onChange/onKeyDown — intercept keys ONLY while open, pass through when closed) + § Code Examples ("Reading the composer text reactively") + the assistant-ui integration note (read node_modules/@assistant-ui/react/dist/primitives/composer/ComposerInput.d.ts if the onChange/onKeyDown compose shape is unclear).
    - web/src/chat/__tests__/Composer.test.tsx — the existing Composer test harness (RTL + the aui runtime provider) to extend for the trigger/keys/a11y/quick-action/degrade cases; keep the existing paste/drop/Enter-send assertions green (non-regression).
  </read_first>
  <action>
    Extend ComposerProps (Composer.tsx:31-34) with `readonly skills?: readonly ComposerSkillRow[]`, `readonly pinnedSkill?: ComposerSkillRow | null`, `readonly onPinSkill?: (row: ComposerSkillRow | null) => void`, `readonly onNewChat?: () => void`. Read the live composer text via `const text = useAuiState((s) => s.composer.text)` (fallback to an onChange handler if the selector shape differs). Derive `const skillList = skills ?? []`, `const filter = text.startsWith('/') ? text.slice(1) : ''`, `const groups = useMemo(() => filterPickerItems(skillList, filter), [skillList, filter])`, and `menuOpen = shouldOpen(text, skillList.length) && !dismissed` (a `dismissed` state set true on Escape and reset when text changes). Hold `activeIndex` state (reset to 0 on filter change) and compute `activeOptionId = menuOpen ? optionId(baseId, activeIndex) : undefined`. On the ComposerPrimitive.Input add: `role="combobox"` semantics via `aria-expanded={menuOpen}`, `aria-controls={listboxId}`, `aria-activedescendant={activeOptionId}`; an `onKeyDown` that, WHEN menuOpen, switches on `pickerKeyAction(e.key)` — up/down → `setActiveIndex(nextActiveIndex(...))` + preventDefault; select → pick the flattened active item + preventDefault; close → `setDismissed(true)` + preventDefault; none → do nothing (passthrough); WHEN closed → do nothing (Enter-send/paste/drop untouched, D-09). Add the `onChange` passthrough if used for the text read. Render `<SkillPicker groups={groups} activeOptionId={activeOptionId} listboxId={listboxId} onSelect={handlePickItem} />` above the input (only when menuOpen). Render `<SkillPill name={pinnedSkill.name} onRemove={() => onPinSkill?.(null)} />` in the pill row (beside the attachment chips) when pinnedSkill != null. Implement `handlePickItem(item)`: for a skill → `onPinSkill?.(item.row)` + clear the /filter text (`aui.composer().setText('')`) + close; for a command → add-files: `fileInputRef.current?.click()`; new-chat: `onNewChat?.()`; clear: reset composer text + `onPinSkill?.(null)` + clear pending attachments via the uploads removal API — then clear the /filter text + close. Do NOT touch the ComposerPrimitive.Root onPaste/onDrop/onDragOver. Extend Composer.test.tsx with the `<behavior>` cases (open/filter/keys/a11y focus-stays-on-input, Enter-send-when-closed non-regression, literal mid-text slash, pill set/remove, the three quick actions fire the right handler with no run, empty-skills degrade). Refactor-on-touch: keep Composer.tsx ≤600 LOC (extract a small `useSkillMenu` hook within the file/dir if it nears the cap — but do NOT edit the 37D-03-owned files).
  </action>
  <acceptance_criteria>
    - `grep -q "SkillPicker" web/src/chat/Composer.tsx` AND `grep -q "SkillPill" web/src/chat/Composer.tsx` AND `grep -q "aria-activedescendant" web/src/chat/Composer.tsx` AND `grep -q "shouldOpen" web/src/chat/Composer.tsx`.
    - The onKeyDown preventDefault is guarded by menuOpen (keys intercepted only while open) — verified by the closed-menu Enter-send test staying green.
    - `cd D:/Repo/Aura/web && npx vitest run src/chat/__tests__/Composer.test.tsx` exits 0 (open/filter/↑↓/Enter-select/Esc, a11y attrs + focus-stays-on-input, Enter-send-when-closed, literal mid-text slash, pill set/remove, add-files/new-chat/clear quick actions, empty-skills degrade).
    - `cd D:/Repo/Aura/web && npx tsc --noEmit` clean.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && npx vitest run src/chat/__tests__/Composer.test.tsx && npx tsc --noEmit && echo COMPOSER_INTEGRATION_OK</automated>
  </verify>
  <done>Typing / opens the ARIA combobox picker (keys handled only while open, focus stays on the input), selecting pins a removable pill + carries the skill on send, and add-files/new-chat/clear run as pure client actions; Enter-send/paste/drop are preserved when the menu is closed.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| composer input → aura.skill on POST /agent/run | The client must carry only a skill NAME chosen from the server-fetched list, never free-form text; the server (37D-02) re-validates via the loader |
| `/` key handling → existing Enter-send/paste/drop | The picker must not degrade the composer's core send/attach behavior |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37D-07 | Tampering (arbitrary skill name injected on send) | pinnedSkill → streamRun skill | mitigate | pinnedSkill is set ONLY from a SkillPicker selection over the fetched list; only pinnedSkill?.name is carried; the server resolves it via the loader's validated key set (37D-02 T-37D-02) and no-ops an unknown name |
| T-37D-08 | Denial of Service (self — broken send) | ComposerPrimitive.Input onKeyDown | mitigate | Keys are preventDefault'd ONLY when menuOpen; when closed the library's Enter-send/paste/drop run untouched (D-09); the trigger is shouldOpen (never a mid-text slash) — proven by the closed-menu Enter-send + literal-slash tests |
| T-37D-SC | Tampering | npm/pip/cargo installs | accept | 37D installs NO external packages (RESEARCH § Package Legitimacy Audit: N/A) |
</threat_model>

<verification>
- `cd web && npx vitest run src/chat/__tests__/sseAdapter.test.ts src/chat/__tests__/Composer.test.tsx src/chat/__tests__/ExternalStoreChat.skill.test.tsx src/chat/composer/useComposerSkills.test.ts` all green.
- `npx tsc --noEmit` clean; the existing Composer/ExternalStoreChat suites stay green (non-regression on Enter-send/paste/drop/attachments).
- No edits to any 37D-03-owned file (composer/api.ts, skillPickerModel.ts, SkillPicker.tsx, SkillPill.tsx, i18n/resources.ts).
</verification>

<success_criteria>
- End-to-end in-app: type `/` → filter → ↑/↓/Enter select → removable pill → send POSTs `aura.skill` (37D-02 applies it first); add-files/new-chat/clear are pure client actions; Enter-send/paste/drop preserved when the menu is closed; empty/unreachable skills degrade the menu to a no-op.
- The composer input carries the APG combobox ARIA (expanded/controls/activedescendant) with focus staying on the input.
</success_criteria>

<output>
Create `.planning/phases/37D-composer-skill-picker/37D-04-SUMMARY.md` when done.
</output>
