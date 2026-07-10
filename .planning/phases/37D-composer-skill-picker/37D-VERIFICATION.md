---
phase: 37D-composer-skill-picker
verified: 2026-07-10T07:30:00Z
status: passed
human_verification_resolved: 2026-07-10T06:05:00Z — both manual-only items (visual parity + live-LLM Mechanism A) confirmed PASS by the operator on the rebaked live :9080 deployment (37D-UAT.md)
score: 28/28 must-haves verified (3 ROADMAP success criteria + 25 plan-level truths)
overrides_applied: 0
human_verification:
  - test: "Visual parity of the / picker (menu-above-input, grouped Quick-commands/Skills sections, icon+name+subtitle rows, removable pill) against the Claude reference screenshot cited in 37D-DISCUSSION-LOG.md"
    expected: "The rendered picker matches the intended look-and-feel (spacing, grouping, icon choice, pill styling) — a design-fidelity judgment, not a functional one"
    why_human: "Pixel/layout fidelity is subjective; jsdom-based unit tests and the golden-replay e2e assert DOM roles/attributes/counts, not visual appearance. 37D's own 37D-VALIDATION.md § Manual-Only Verifications flags exactly this item as manual-only."
  - test: "Drive one real (non-mocked) turn against a live LLM backend: type '/', pick a real installed skill, send a message, and confirm the model's reply is visibly influenced by the pinned skill's instructions (Mechanism A actually changes model behavior, not just the wire body)"
    expected: "The model's response reflects the pinned skill's instructions, proving the useAuthorityFrame + body prepend has the intended effect on a live LLM, not just on the captured HTTP body"
    why_human: "The e2e spec (composer-skills.spec.ts) and the Go unit tests prove the WIRE CONTRACT (the exact string is prepended to the model message and the POST body carries aura.skill) with golden-replay/mocked runners — by design, no test in this phase drives a live LLM. Mechanism A reuses the pre-existing, already-shipped `skill action=use` runtime contract (same useAuthorityFrame + TurnWithModelUserMessage seam), so the risk is low, but a live-model sanity check has not been captured in this phase's evidence and is real-time/external-service behavior per the verification playbook."
---

# Phase 37D: Composer Skill & Command Picker Verification Report

**Phase Goal:** A slash "/" menu in the web Composer (parity with Claude's skill/command picker) that lists the skills available to the authenticated identity + quick commands, keyboard-filterable, to invoke/attach a skill inline in the turn — instead of only managing them in the Governance admin board.

**Verified:** 2026-07-10T07:30:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

**Scope reconciliation applied (per verification brief):** The ROADMAP/REQUIREMENTS.md wording "identity-scoped / via the governance skills API" is verified against the reconciled contract from the 37D-01 PRD amendment (#81): a GLOBAL active-skills snapshot behind plain `RequireAuth` at a NEW `GET /api/composer/skills` route (NOT `governance.read`), with per-identity scoping explicitly DEFERRED. This is not flagged as a gap — see Amendment #81 evidence below.

## Goal Achievement

### Observable Truths — ROADMAP Success Criteria (the contract)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Typing "/" at line-start opens a menu listing the skills available to the authenticated identity (global active-skills snapshot behind plain RequireAuth), with incremental filter + ↑/↓/Enter/Esc + a per-row description | VERIFIED | `Composer.tsx:93-120` reads composer text reactively, computes `menuOpen = shouldOpen(...) && ...`, renders `<SkillPicker>` above the input; `skillPickerModel.ts` `shouldOpen`/`filterPickerItems`/`pickerKeyAction`/`nextActiveIndex` unit-tested (skillPickerModel.test.ts, part of the 92 tests I ran green); `SkillPicker.tsx` renders grouped `role=option` rows with name + description subtitle. Backend: `GET /api/composer/skills` (`internal/agui/composer_api.go`) mounted BARE on the parent mux (`cmd/aura/serve_webui_composer.go:32-34`, confirmed NOT wrapped in `RequireCapability`, contrast `governanceSkillsRoute` at `serve_webui.go:421` which IS). `TestComposerSkills_RequireAuthNotCapability` proves the SAME non-admin identity gets 200 non-empty on the composer route and 403 on the governance-gated route — I ran this test directly, PASS. |
| 2 | Selecting an entry injects the skill into the turn via the existing runtime contract (Mechanism A); no new source of truth for skills | VERIFIED | `Composer.tsx:127-141` `handlePickItem` calls `onPinSkill?.(row)` for a skill selection; `ExternalStoreChat.tsx:151` folds `skill: pinnedSkill.name` into `streamRun`; `auraRunBody.ts` folds it into the SAME `aura` envelope as `attachment_ids`; `internal/agui/server_run_request.go` decodes `req.Aura.Skill`; `server.go:342-346` resolves it via `s.governance.Skills.SkillBody(name)` and prepends `tools.UseAuthorityFrame + body` to the model message via the EXISTING `TurnWithModelUserMessage` split. `tools.UseAuthorityFrame` is the same exported literal `skill action=use` emits (confirmed by reading `skill_read.go`; zero lowercase `useAuthorityFrame` remnants — `grep -rn` returned no hits). One loader (`skillsBoardAdapter` in `serve_governance.go`) backs both `ActiveSkills()` (the list) and `SkillBody()` (the resolve) — confirmed both delegate to `Loader.List()`/`Loader.Get()` reading the SAME `l.snapshot` map (`internal/skills/loader.go:91-109`). `TestRun_PinnedSkill_Applied`, `TestRun_PinnedSkill_UnknownName_NoOp`, `TestComposerListSubsetOfResolvable` — all run directly, PASS. |
| 3 | Accessible (ARIA combobox/listbox), preserves Composer paste/drop/Enter-to-send, degrades to a no-op when the skills API is empty/unreachable; unit + e2e; coverage ≥85% | VERIFIED | `Composer.tsx:370-383`: `ComposerPrimitive.Input` carries `role="combobox"`, `aria-expanded`, `aria-controls`, `aria-activedescendant`, `aria-haspopup="listbox"`, `aria-autocomplete="list"`. `SkillPicker.tsx` renders `role="listbox"`/`role="option"`/`aria-selected` + JS `scrollIntoView`. Paste/drop handlers remain on `ComposerPrimitive.Root`, untouched by 37D. Enter-to-send preservation independently verified by reading the ACTUAL installed `@assistant-ui/react@0.14.22` source (`ComposerInput.tsx:365`: `onKeyDown: composeEventHandlers(onKeyDown, handleKeyPress)`) and `@radix-ui/primitive`'s `composeEventHandlers` (calls the caller's handler first, then the library's `handleKeyPress` UNLESS `event.defaultPrevented`) — Composer's handler only calls `preventDefault()` when `menuOpen`, so Enter-to-send/paste/drop are provably intact when the menu is closed, not just asserted by a test that mocks the library away. Degrade-to-no-op: `fetchComposerSkills` catches any throw → `[]` (`api.ts:29-31`); `shouldOpen(text, 0) === false`. e2e: `web/e2e/composer-skills.spec.ts` (322 LOC) — read in full; contains real counted assertions (`domAssertions` guards, exact-equality `aura.skill` checks, `runCount`/`createCount` route interceptors), not vacuous. Coverage ≥85%: SUMMARY-evidenced (see note below) — web vitest 92.6%/86.68%/92.77%/94.34%, owned-surface Go 85.5% (internal/agui 86.8%), both measured on an isolated throwaway DB per the 37D-05 SUMMARY; NOT re-run in this verification pass per the explicit DB-safety constraint. I independently ran a SCOPED vitest subset (9 files / 92 tests covering all 37D composer files + i18n parity) — 100% pass, consistent with the claimed aggregate. |

### Observable Truths — Plan-Level Detail (25 truths across 5 plans)

| # | Plan | Truth | Status | Evidence |
|---|------|-------|--------|----------|
| 1 | 37D-01 | prd.md documents WEBSKILL-01..03 as a named subsection (Amendment #81) | VERIFIED | `grep -n "Amendment #81" prd.md` → line 2973, full text read; transcribes REQUIREMENTS.md:87-89 |
| 2 | 37D-01 | prd.md documents `GET /api/composer/skills` behind plain RequireAuth, NOT governance.read | VERIFIED | Amendment #81 text read in full; matches delivered code |
| 3 | 37D-01 | prd.md documents the D-01 pinned-skill wire path (aura.skill + Mechanism A) | VERIFIED | Amendment #81 text names `useAuthorityFrame`, `TurnWithModelUserMessage`, zero-runner-change |
| 4 | 37D-01 | prd.md reconciles "identity-scoped" wording with GLOBAL snapshot + DEFERRED per-identity | VERIFIED | Amendment #81 contains "DEFERRED"; matches the verification brief's scope-reconciliation note |
| 5 | 37D-01 | prd.md records quick-command actions as pure client UI + pinned skill as removable pill | VERIFIED | Amendment #81 text + confirmed in delivered `Composer.tsx`/`SkillPill.tsx` |
| 6 | 37D-02 | `GET /api/composer/skills` returns 200 `{skills:[...]}` for any authenticated identity; 503 when nil | VERIFIED | `TestComposerSkills_Active` + `TestComposerSkills_NilProvider` — ran directly, PASS |
| 7 | 37D-02 | Mounted behind plain RequireAuth, NOT governance.read | VERIFIED | `TestComposerSkills_RequireAuthNotCapability` — ran directly, PASS; source read of `serve_webui.go`/`serve_webui_composer.go` confirms the differential |
| 8 | 37D-02 | `runAgentRequest.Aura` carries `Skill` string on both typed + ext-decode structs | VERIFIED | `server_run_request.go` read in full; `TestDecodeRunAgentRequest_Skill` — ran directly, PASS |
| 9 | 37D-02 | `handleRun` prepends `UseAuthorityFrame + body` when known; no-op when unknown | VERIFIED | `server.go:342-346` read; `TestRun_PinnedSkill_Applied` + `TestRun_PinnedSkill_UnknownName_NoOp` + `TestRun_NoSkill_Unchanged` — ran directly, PASS |
| 10 | 37D-02 | For every `ActiveSkills()` name, `SkillBody(name)` returns ok=true (list ⊆ resolvable) | VERIFIED | `TestComposerListSubsetOfResolvable` — ran directly, PASS; structurally confirmed via `loader.go` (`List`/`Get` share one `l.snapshot` map) |
| 11 | 37D-02 | Authority-frame literal reused verbatim (`tools.UseAuthorityFrame`), not re-invented | VERIFIED | `grep -rn "useAuthorityFrame" internal/` → zero hits (only the exported `UseAuthorityFrame` remains); `server.go` imports and uses it |
| 12 | 37D-03 | `fetchComposerSkills()` GETs via `getJSON`, returns `body.skills ?? []`, degrades to `[]` on throw | VERIFIED | `api.ts` read in full; `api.test.ts` part of my 92-test scoped vitest run, PASS |
| 13 | 37D-03 | `skillPickerModel` pure helpers (shouldOpen/filterPickerItems/pickerKeyAction/etc.) | VERIFIED | `skillPickerModel.ts` read in full (React/DOM-free); `skillPickerModel.test.ts` PASS |
| 14 | 37D-03 | `SkillPicker` renders `role=listbox` + grouped `role=option` + `aria-selected` + JS-scroll | VERIFIED | `SkillPicker.tsx` read in full; `SkillPicker.test.tsx` PASS |
| 15 | 37D-03 | `SkillPill` renders a removable pill mirroring `AttachmentChip` | VERIFIED | `SkillPill.tsx` read in full — bordered inline-flex, ghost X, localized aria-label |
| 16 | 37D-03 | `chat.skillPicker.*` strings exist in both en and it (parity green) | VERIFIED | `resources.composer.ts` read in full — both locale objects present with matching keys; `resources.parity.test.ts` PASS |
| 17 | 37D-04 | `streamRun` folds optional skill into the SAME `aura` envelope as `attachment_ids` | VERIFIED | `auraRunBody.ts` read in full; `sseAdapter.test.ts` PASS (part of scoped run) |
| 18 | 37D-04 | `ExternalStoreChat` lifts `pinnedSkill`, fetches skills once, reads into `streamRun`, clears after send | VERIFIED | `ExternalStoreChat.tsx:92-93,151,169` read directly; `ExternalStoreChat.skill.test.tsx` PASS |
| 19 | 37D-04 | Typing `/` opens SkillPicker; filters incrementally; arrows/Enter/Esc work; Enter sends when closed | VERIFIED | `Composer.tsx` full integration read; 12 picker RTL cases in `Composer.test.tsx` PASS; independently confirmed via assistant-ui/radix source (composeEventHandlers) |
| 20 | 37D-04 | Selecting pins the pill + clears filter; add-files/new-chat/clear are pure client (no agent round-trip) | VERIFIED | `Composer.tsx:127-141` `handlePickItem` read; no `streamRun`/`/agent/run` call in any of the three command branches |
| 21 | 37D-04 | Composer input carries APG combobox ARIA; focus stays on the input | VERIFIED | `Composer.tsx:374-382` (role/aria-expanded/aria-controls/aria-activedescendant); `SkillPicker.tsx` `onMouseDown` preventDefault keeps focus on input |
| 22 | 37D-05 | `composer-skills.spec.ts` drives full flow; `aura.skill` === selected name (golden-replay) | VERIFIED (via SUMMARY evidence + spec-content review) | Spec file read in full (322 LOC) — real `page.route` mocks, `page.on('request')` interceptor, exact-equality `expect(parsed.aura?.skill).toBe(SKILL_NAME)`. NOT independently re-run (requires a live `aura serve` + Authula stack; out of scope per DB-safety constraints) |
| 23 | 37D-05 | Spec proves quick actions (new-chat/clear) as pure client (no `/agent/run`) | VERIFIED (via spec-content review) | `expect(tracker.runCount).toBe(0)` present in both the new-chat and clear test cases |
| 24 | 37D-05 | Every assertion is a COUNTED DOM/route fact guarded > 0 (no-skip-as-green) | VERIFIED | `domAssertions` counters + `expect(domAssertions).toBeGreaterThanOrEqual(N)` guards present in all 4 tests |
| 25 | 37D-05 | `internal/webui/dist` rebuilt from 37D-03/04 source | VERIFIED | `grep -rl "skillPicker" internal/webui/dist/` found the token in `assets/index-B4oZRVbr.js` + `assets/ExternalStoreChat-CPTH1nIt.js`; file timestamps (Jul 10 02:32) consistent with the claimed rebuild |

**Score:** 28/28 truths verified (3 ROADMAP SCs + 25 plan-level truths). 0 FAILED. 0 UNCERTAIN-blocking. 2 items routed to human verification (see below) per the Escalation Gate design — this does not reduce the verified count.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `prd.md` (Amendment #81) | WEBSKILL-01..03 + endpoint + envelope + Mechanism A + reconciliation | VERIFIED | Read in full at line 2973; all required tokens present |
| `internal/agui/composer_api.go` | `handleComposerSkills` + `registerComposerRoutes` | VERIFIED | 38 LOC, read in full, wired into `server.go` Mux() |
| `internal/agui/composer_api_test.go` | Endpoint suite (active/RequireAuth-not-capability/401/503) | VERIFIED | 4 tests, ran directly, all PASS |
| `internal/agui/governance_seam.go` | `SkillBody` on `SkillsBoardProvider` | VERIFIED | Interface + doc-comment read; 3/3 implementers confirmed via grep |
| `internal/agui/server_run_request.go` | `Skill` field on both structs | VERIFIED | Read in full; `TestDecodeRunAgentRequest_Skill` PASS |
| `internal/agui/server.go` | Mechanism-A prepend in `handleRun` | VERIFIED | Lines 342-346 read; 540 LOC (≤600 cap) |
| `internal/agui/server_skill_run_test.go` | Pinned-skill run suite + subset guard | VERIFIED | 4 tests, ran directly, all PASS |
| `internal/agent/tools/skill_read.go` | Exported `UseAuthorityFrame` | VERIFIED | Const + doc-comment read; zero lowercase remnants |
| `cmd/aura/serve_webui_composer.go` | Bare RequireAuth-only mount | VERIFIED | 34 LOC, read in full; NO `RequireCapability` on the mount line |
| `cmd/aura/serve_webui.go` | One-line `registerComposerRoutes` call | VERIFIED | 556 LOC (≤600 cap); line 508 confirmed |
| `web/src/chat/composer/api.ts` | `fetchComposerSkills` + degrade-to-[] | VERIFIED | Read in full; tests PASS |
| `web/src/chat/composer/skillPickerModel.ts` | Pure combobox model | VERIFIED | Read in full (161 lines of logic, React/DOM-free); tests PASS |
| `web/src/chat/composer/SkillPicker.tsx` | ARIA listbox | VERIFIED | 167 LOC, read in full; tests PASS |
| `web/src/chat/composer/SkillPill.tsx` | Removable pill | VERIFIED | Read in full |
| `web/src/chat/composer/useComposerSkills.ts` | One-shot degrade-safe fetch hook | VERIFIED | Read in full; tests PASS |
| `web/src/chat/composer/usePinnedSkill.ts` | Lifted pinnedSkill seam | VERIFIED | Read in full; tests PASS |
| `web/src/chat/auraRunBody.ts` | `buildAuraRunBody` fold | VERIFIED | Read in full; one aura object confirmed |
| `web/src/chat/sseAdapter.ts` | `StreamRunOptions.skill?` | VERIFIED | 598 LOC (≤600 cap); delegates to `buildAuraRunBody` |
| `web/src/chat/Composer.tsx` | Full `/` picker integration | VERIFIED | 403 LOC, read in full end-to-end |
| `web/src/chat/ExternalStoreChat.tsx` | `pinnedSkill` lift + send + clear | VERIFIED | 518 LOC (≤600 cap); onNew flow read directly |
| `web/src/AppShell.tsx` | `onNewChat={startNewConversation}` | VERIFIED | 591 LOC (≤600 cap); wiring confirmed at line 435 |
| `web/src/i18n/resources.composer.ts` | en+it `chat.skillPicker.*` | VERIFIED | Read in full; parity test PASS |
| `web/e2e/composer-skills.spec.ts` | Golden-replay e2e (4 tests × 2 projects) | VERIFIED (content) | 322 LOC, read in full; real counted assertions confirmed; PASS claim not independently re-run |
| `internal/webui/dist` | Rebuilt embed with the picker | VERIFIED | `skillPicker` token found in built JS; timestamp consistent |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `internal/agui/server.go` | `internal/agui/composer_api.go` | `s.registerComposerRoutes(mux)` in `Mux()` | WIRED | Confirmed at server.go:211 |
| `internal/agui/server.go` | `internal/agent/tools` | `tools.UseAuthorityFrame` prepend in `handleRun` | WIRED | Confirmed at server.go:344 |
| `cmd/aura/serve_webui.go` | `cmd/aura/serve_webui_composer.go` | `registerComposerRoutes(mux, aguiHandler, auth)` | WIRED | Confirmed at serve_webui.go:508 |
| `internal/agui/server.go` | `internal/agui/governance_seam.go` | `s.governance.Skills.SkillBody(name)` | WIRED | Confirmed at server.go:343 |
| `web/src/chat/composer/api.ts` | `/api/composer/skills` | `getJSON` same-origin fetch | WIRED | Confirmed; `api.test.ts` PASS |
| `web/src/chat/composer/SkillPicker.tsx` | `web/src/chat/composer/skillPickerModel.ts` | Import of `optionId`/`PickerGroup`/`PickerItem` | WIRED | Confirmed import at top of SkillPicker.tsx |
| `web/src/chat/ExternalStoreChat.tsx` | `web/src/chat/sseAdapter.ts` | `streamRun({..., skill: pinnedSkill?.name})` | WIRED | Confirmed at ExternalStoreChat.tsx:151 |
| `web/src/chat/Composer.tsx` | `web/src/chat/composer/skillPickerModel.ts` | `shouldOpen`/`filterPickerItems`/`pickerKeyAction`/`optionId` | WIRED | Confirmed imports + call sites throughout Composer.tsx |
| `web/src/AppShell.tsx` | `web/src/chat/ExternalStoreChat.tsx` | `onNewChat={startNewConversation}` | WIRED | Confirmed at AppShell.tsx:435 |
| `web/src/chat/sseAdapter.ts` | `web/src/chat/auraRunBody.ts` | `buildAuraRunBody(id, opts)` | WIRED | Confirmed at sseAdapter.ts:584 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `SkillPicker` (via `useComposerSkills`) | `skills` rows | `GET /api/composer/skills` → `activeSkillRows(loader.List())` | Yes — `Loader.List()` re-scans configured root directories on disk (`refreshLocked()`), not a static array | FLOWING |
| `handleRun` pinned-skill body | `body` | `SkillsBoardProvider.SkillBody(name)` → `Loader.Get(name)` | Yes — reads the SAME `l.snapshot` map `List()` populates from disk-scanned skill files | FLOWING |
| `ExternalStoreChat` → `Composer` `skills` prop | `skills` | `useComposerSkills()` hook, fetched once on mount | Yes — not a hardcoded empty array at the call site (`skills={skills}` at ExternalStoreChat.tsx:509) | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Backend composer/pinned-skill suite | `go test -count=1 ./internal/agui/ -run 'TestComposerSkills\|TestRun_PinnedSkill\|TestRun_NoSkill\|TestComposerListSubsetOfResolvable\|TestDecodeRunAgentRequest_Skill' -v` | 9/9 PASS | PASS |
| Full untagged package suites (touched packages) | `go test -count=1 ./internal/agui/ ./internal/agent/tools/ ./cmd/aura/` | All green (8.4s/12.2s/8.7s) | PASS |
| Go build + vet | `go build ./...` && `go vet ./internal/agui/ ./internal/agent/tools/ ./cmd/aura/` | Both clean, exit 0 | PASS |
| Interface-completeness guard | `grep -rn "func.*ActiveSkills()" \| wc -l` / `SkillBody(` | 3/3 each | PASS |
| Frontend 37D composer + integration suite | `npx vitest run src/chat/composer/ src/chat/__tests__/Composer.test.tsx src/chat/__tests__/ExternalStoreChat.skill.test.tsx src/chat/__tests__/sseAdapter.test.ts src/i18n/__tests__/resources.parity.test.ts` | 9 files / 92 tests, all PASS | PASS |
| TypeScript compiles | `npx tsc --noEmit` | Clean, exit 0 | PASS |
| Anti-pattern scan (debt markers) | `grep -nE "TBD\|FIXME\|XXX\|TODO\|HACK\|PLACEHOLDER"` across all 30 touched files | Zero hits | PASS |
| XSS sink scan | `grep -rn "dangerouslySetInnerHTML" web/src/chat/composer/` | Zero hits | PASS |
| Git commit integrity | `git log --oneline --all \| grep <9 claimed hashes>` | All 9 commit hashes present with matching messages | PASS |
| Dist rebuild proof | `grep -rl "skillPicker" internal/webui/dist/` | Token found in 2 built JS assets | PASS |

### Probe Execution

Not applicable — 37D is a web-feature phase (not a migration/tooling phase); no `scripts/*/tests/probe-*.sh` files exist in the repo and none are declared in any 37D PLAN/SUMMARY. Skipped per Step 7c criteria.

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|--------------|--------|----------|
| WEBSKILL-01 | 37D-01, 37D-02, 37D-03, 37D-04 | "/" menu listing skills, keyboard-filterable, per-row description | SATISFIED | Endpoint + picker + trigger all independently verified above; REQUIREMENTS.md line 87 marked `[x]` |
| WEBSKILL-02 | 37D-01, 37D-02, 37D-04 | Selecting injects skill via existing runtime contract; no new source of truth | SATISFIED | Mechanism A + one-loader-snapshot guard independently verified above; REQUIREMENTS.md line 88 marked `[x]` |
| WEBSKILL-03 | 37D-01, 37D-03, 37D-04, 37D-05 | ARIA combobox/listbox, preserves paste/drop/Enter-send, degrades to no-op, unit+e2e, coverage ≥85% | SATISFIED | ARIA attrs + composeEventHandlers proof + degrade-to-[] + e2e spec content all independently verified above; coverage figures verified via SUMMARY evidence (not re-run — DB safety); REQUIREMENTS.md line 89 marked `[x]` |

**Orphan check:** `grep -E "Phase 37D" .planning/REQUIREMENTS.md` returns exactly WEBSKILL-01/02/03 — all three appear in at least one plan's `requirements:` frontmatter field (37D-01 declares all three for documentation purposes; 37D-02 declares 01/02; 37D-03 declares 01/03; 37D-04 declares all three; 37D-05 declares 03 as the terminal gate that formally marks all three complete). **No orphaned requirements.**

### Anti-Patterns Found

None. Zero debt markers (TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER) across all 30 files touched by this phase. Two `return []`/`return null` instances found (`api.ts:30` inside the `fetchComposerSkills` catch block; `SkillPicker.tsx:93` for the empty-groups case) were checked against the Chesterton's Fence model and confirmed as the deliberate, tested, documented D-09 degrade-to-no-op behavior — not stubs. No `dangerouslySetInnerHTML` anywhere in `web/src/chat/composer/`.

### Human Verification Required

#### 1. Visual parity of the picker

**Test:** Open the web cockpit, focus the composer, type `/`, and visually compare the rendered menu (grouping, icon+name+subtitle rows, section headers, pill styling) against the Claude reference screenshot cited in `37D-DISCUSSION-LOG.md`.
**Expected:** The picker looks intentional and polished — correct spacing, icon choice, truncation, and the removable pill visually matches the attachment-chip language it mirrors.
**Why human:** Pixel/layout fidelity is a subjective design judgment that DOM-level unit tests and route-mocked e2e assertions cannot evaluate. This is explicitly flagged as the phase's own "Manual-Only Verification" in `37D-VALIDATION.md`.

#### 2. Live-LLM sanity check of Mechanism A

**Test:** In a real (non-mocked) conversation, type `/`, pin an actually-installed skill (e.g. `skill-creator`), send a message, and confirm the model's reply is visibly shaped by the pinned skill's instructions.
**Expected:** The model behaves as if it received the skill's authority-framed instructions first (e.g., follows the skill's documented process) — proving Mechanism A has the intended effect on live model behavior, not just on the captured HTTP request body.
**Why human:** All automated coverage of Mechanism A (Go unit tests, the Playwright e2e) proves the WIRE CONTRACT — the exact string is prepended to the model message, and the POST body carries `aura.skill` — using a scripted/mocked runner or a golden-replay SSE fixture, by design (no live-agent-loop is driven in this phase's tests). Mechanism A reuses the pre-existing, already-shipped `skill action=use` runtime seam (`useAuthorityFrame` + `TurnWithModelUserMessage`), which lowers risk, but no test in this phase's evidence exercises a live LLM call with the pinned skill, and real-time/external-service behavior is outside what static/mocked verification can confirm.

### Gaps Summary

No functional gaps found. All 28 must-haves (3 ROADMAP success criteria + 25 plan-level truths across 5 plans) are VERIFIED against the actual codebase with direct evidence: source reads, passing untagged Go tests (9 targeted + 3 full-package suites), passing scoped frontend tests (92 tests / 9 files), clean `go build`/`go vet`/`tsc --noEmit`, a genuine git-log audit of all 9 claimed commits, an independent confirmation that the built `internal/webui/dist` embed contains the picker, and — notably — a direct read of the actual installed `@assistant-ui/react`/`@radix-ui/primitive` source to confirm the Enter-to-send-preservation claim holds at the library-composition level (not merely asserted by a test that mocks the library away).

Status is `human_needed` rather than `passed` solely because of two escalated items: (1) a design-fidelity visual check the phase's own validation plan already flagged as manual-only, and (2) a recommended (not blocking) live-LLM sanity check that this phase's test suite does not and — by its golden-replay/mocked-runner design — cannot cover. Per the verification workflow's decision tree, any non-empty human-verification list routes the phase to `human_needed` regardless of how high the automated score is; this reflects the Escalation Gate pattern this verifier implements, not a quality doubt about the delivered code.

**Coverage figures note (per the explicit DB-safety brief):** The web vitest (92.6%/86.68%/92.77%/94.34%) and owned-surface Go (85.5%, internal/agui 86.8%) coverage percentages are **verified via SUMMARY evidence only** (37D-05-SUMMARY.md, measured on an isolated throwaway DB `aura_cov37d05` after the documented live-DB incident and its remediation) — they were **NOT re-run** during this verification pass, per the explicit prohibition on running `db_integration`-tagged tests, `scripts/coverage_docker.sh`, `scripts/coverage_gate.sh`, or `make coverage` against this host's live production database. A scoped, safe vitest subset (9 files / 92 tests covering every 37D-touched frontend file) was run independently and passed 100%, which is consistent with — though not a full re-proof of — the claimed aggregate percentages.

---

_Verified: 2026-07-10T07:30:00Z_
_Verifier: Claude (gsd-verifier)_
