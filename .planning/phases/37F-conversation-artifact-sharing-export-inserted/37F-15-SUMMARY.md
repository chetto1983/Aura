---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 15
subsystem: ui
tags: [react, typescript, i18n, accessibility, radix, share, clipboard, state-machine]

# Dependency graph
requires:
  - phase: 37F-05
    provides: "shareTypes.ts (ShareLink/Snapshot mirror), resources.share.ts (share i18n scaffold), AssetSourceContext precedent"
  - phase: 37F-14
    provides: "ShareToggle, useSharePanel (shareModalState/openShare/closeShare), the ChatWorkspaceControls mount point"
  - phase: 37F-08
    provides: "internal/share.Service (Create/Update/Revoke/ExpiryOption) — the domain contract shareApi.ts wraps"
provides:
  - "web/src/chat/share/shareApi.ts — typed client for the four owner-scoped share routes (create/list/update/revoke), written against 37F-10-PLAN.md's locked contract since the backend (wave 5) has not landed yet"
  - "web/src/chat/share/ShareModal.tsx — the tier-before-mint state machine (idle/creating/shared/updating/revoking/revoked) that makes D-01 (public is never default) real in the UI"
  - "web/src/chat/share/RevokeConfirmDialog.tsx — the destructive-revoke confirm, copied from DeleteConfirmDialog.tsx"
  - "web/src/chat/share/shareViewModel.ts — pure expiry/stale/date helpers extracted ahead of the component (100% covered)"
  - "ShareModal mounted in AppShell.tsx via useSharePanel's conditional-mount idiom"
  - "Additive ShareLink.snapshot_turn_count + Conversation.LastActiveAt/TurnCount fields (forward-compatible, unpopulated until a future Go change) backing the stale-detection affordance"
affects: [37F-17]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Pure view-model extraction ahead of the component (shareViewModel.ts mirrors documents/documentViewModel.ts's split) — no fetch, no i18n t(), no React in the helpers; kept ShareModal.tsx well clear of the 600-LOC cap and pushed the directory's aggregate coverage to 94.81%/90.57%/94.28%/99.13%."
    - "Client written against a locked plan contract, not a running backend (37F-10 is wave 5; this plan is wave 4) — proven entirely with mocked fetch, same resolution 37F-16/SharePage.tsx already used for the identical wave-ordering gap."
    - "Reuse an existing, already-tested clipboard hook (useCopyAction) instead of inlining navigator.clipboard.writeText — sidesteps the Safari gesture-loss bug by construction rather than by convention."
    - "Conditional-mount idiom for a modal (mounted only while open) rather than an always-mounted Dialog toggling `open` — resets ShareModal's internal state fresh on every open, matching D-01's 'never resume a stale tier selection'."

key-files:
  created:
    - web/src/chat/share/shareApi.ts
    - web/src/chat/share/shareApi.test.ts
    - web/src/chat/share/ShareModal.tsx
    - web/src/chat/share/ShareModal.test.tsx
    - web/src/chat/share/RevokeConfirmDialog.tsx
    - web/src/chat/share/shareViewModel.ts
    - web/src/chat/share/shareViewModel.test.ts
  modified:
    - web/src/chat/share/shareTypes.ts
    - web/src/conversations/useConversations.ts
    - web/src/i18n/resources.share.ts
    - web/src/AppShell.tsx

key-decisions:
  - "shareApi.ts written against 37F-10-PLAN.md's locked route table, not against internal/agui/share_api*.go (which does not exist yet — plan 37F-10 is wave 5, this plan is wave 4, and the two frontmatter wave numbers do not imply 37F-10 lands first despite the read_first's file reference). Same precedent as plan 37F-16's SharePage.tsx."
  - "Wire field is conversation_id (internal/share's own Go domain name — ConversationID, matching the DB column and every doc comment in that package) while the TS parameter stays threadId (this codebase's established frontend convention). internal/assets calls the identical concept ThreadID/thread_id in ITS OWN package; the two names coexist per-domain in this codebase already, so this follows share's, since shareApi.ts wraps share's routes — documented in the file header to prevent future drift toward the wrong precedent."
  - "A numeric ExpiryOption (not a 4th createShare parameter) encodes a custom day count — matches the plan's literal 3-arg signature (threadId, tier, expiryOption) while still carrying the count; shareApi.ts's expiryBody helper shapes it into {expiry_option:'custom', custom_expiry_days:N} on the wire."
  - "ShareModal is a self-contained mint-and-manage session, NOT a resume surface: it never calls listShares() to check for an existing share on open — it always starts at the tier-selection form. Resuming an already-shared thread across modal (re)opens belongs to plan 37F-17's 'Condiviso' section (useThreadShares), a separately-scoped surface per that plan's own file list (no ShareModal/shareModalState/openShare reference anywhere in 37F-17-PLAN.md)."
  - "'revoked' is a real, distinct Phase value (not folded into 'idle') so the transition sequence idle->...->revoking->revoked is observable, but its JSX branch is intentionally identical to 'idle' — RESEARCH.md §5 states 'the modal returns to idle' after a successful revoke, and re-entering the create form (rather than a dead-end confirmation screen) lets the owner immediately mint a replacement link in the same session."
  - "Stale-detection's exact count needs data neither internal/conversations.Conversation nor share.ShareLink expose today (conversations.last_active_at is a real DB column per RESEARCH.md but is not projected into the JSON API response; no per-snapshot turn count exists on ShareLink either). Resolved by adding two ADDITIVE, forward-compatible optional fields — Conversation.LastActiveAt/TurnCount and ShareLink.snapshot_turn_count — computed via shareViewModel.computeStaleCount, which safely degrades to 'not stale' (0) when either is absent (true in production today; both are exercised via mocks in tests). This is a TS-only, non-Go-touching change consistent with the plan's explicit 'no Go changes' scope; a future Go plan populating these fields activates the feature with zero frontend changes."
  - "grep -n \"clipboard\" ShareModal.tsx finds nothing (documented deviation below) because the write goes through the existing, already-tested useCopyAction hook rather than a duplicated inline navigator.clipboard.writeText call — the underlying security property (writeText reached synchronously, no await, no ClipboardItem) holds regardless of which file the literal string lives in."

requirements-completed: [WEBSHARE-02]

coverage:
  - id: D1
    description: "shareApi.ts wraps createShare/listShares/updateShareSnapshot/revokeShare against 37F-10-PLAN.md's locked contract — same-origin, AbortSignal-threaded, non-2xx throws the server message, typed against shareTypes.ShareLink with no local duplicate type"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/shareApi.test.ts (18 tests: all 6 behavior rows + AbortSignal threading on every function + null-body error fallback)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The tier is chosen BEFORE minting; internal is preselected and public is NEVER preselected (D-01) — the single most important assertion in this plan"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/ShareModal.test.tsx > idle state > 'preselects the internal tier and NEVER the public tier — the single most important assertion in this plan'"
        status: pass
    human_judgment: false
  - id: D3
    description: "The public warning (ChatGPT honesty copy, stated at mint time) renders ONLY when public is selected and is aria-describedby-linked to the public radio; the expiry chips reveal with 7 days preselected only for public"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/ShareModal.test.tsx > idle state > 'reveals the public warning...', 'reveals the expiry chips...', 'hides the warning and expiry chips...'"
        status: pass
    human_judgment: false
  - id: D4
    description: "Mint-then-copy on a direct gesture with no await before the clipboard write (sidesteps the Safari ClipboardItem bug), the label swaps and announces via aria-live"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/ShareModal.test.tsx > shared state > 'copies the URL on a direct click with no preceding await...'"
        status: pass
      - kind: other
        ref: "grep -cE 'onClick=\\{async' ShareModal.tsx -> 0; grep -c 'ClipboardItem' ShareModal.tsx -> 0"
        status: pass
    human_judgment: false
  - id: D5
    description: "The rendered URL is the server-shaped, absolute, same-origin form per tier — /shared/{id} for internal, /s/{token} for public — never constructed client-side and never crossed between tiers"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/ShareModal.test.tsx > minting > 'mints internal...', 'mints public...never a /shared/{id} form'"
        status: pass
    human_judgment: false
  - id: D6
    description: "Revoke is destructive and gated behind RevokeConfirmDialog — it never fires until the confirm is accepted, and cancelling returns to 'shared' without revoking"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/ShareModal.test.tsx > revoke > all 4 tests (does-not-fire-until-confirmed, cancel-returns-to-shared, confirm-then-idle, failed-revoke-shows-alert)"
        status: pass
    human_judgment: false
  - id: D7
    description: "The stale-snapshot affordance shows the count of newer turns and is absent when there are none, expired-but-not-swept links render inert, and custom-expiry validation sets aria-invalid only when actually invalid (omitted, never 'false', when valid)"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/shareViewModel.test.ts (24 tests, 100% coverage) + ShareModal.test.tsx > 'shows the stale note...', 'shows no stale note...', 'expired-but-not-swept', 'custom expiry validation' (3 tests)"
        status: pass
    human_judgment: false
  - id: D8
    description: "ShareModal is correctly wired end-to-end in the running app: mounted via AppShell's conditional-mount idiom, opened by the existing ShareToggle, all four states visually correct against the real design system"
    requirement: "WEBSHARE-02"
    verification: []
    human_judgment: true
    rationale: "The plan's own <verification> block calls for this specifically as a Manual-Only visual inspection ('open the modal in all four states...inspect artifact visually, not just PASS status'); no dev server was started this session (no browser available to the executor), and no existing AppShell-level test exercises the click-ShareToggle-then-see-ShareModal path end-to-end (ShareModal's own 68 tests prove its internal behavior in isolation; the 34 pre-existing AppShell/ShareToggle tests pass unedited, proving no regression, but neither combination proves the two are wired together correctly by inspection)."

# Metrics
duration: ~70min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 15: ShareModal — Tier-Before-Mint State Machine Summary

**A `ShareModal` state machine (idle→creating→shared→updating→revoking→revoked) that makes D-01 ("public is never default") real: internal preselected, public never preselected, the honesty warning conditional and `aria-describedby`-linked, mint-then-copy via the existing `useCopyAction` hook, and revoke gated behind a real confirm dialog — plus the `shareApi.ts` client it runs on and the `shareViewModel.ts` pure helpers it delegates to.**

## Performance

- **Duration:** ~70 min (includes extensive research into a cross-wave plan-ordering gap before any code was written)
- **Started:** ~2026-07-17T18:10Z (estimated; not captured at session start)
- **Completed:** 2026-07-17T19:20:55Z
- **Tasks:** 2 planned, both completed
- **Files modified:** 11 (7 created, 4 modified)

## Accomplishments

- **`shareApi.ts`** — the typed client for `POST/GET/PATCH/DELETE /api/shares...`, following `web/src/chat/attachments/api.ts`'s exact fetch/credentials/error-shaping idiom. Written against `37F-10-PLAN.md`'s locked route table rather than a running backend, because plan 37F-10 (wave 5, `internal/agui/share_api*.go`) has not executed yet even though this plan is wave 4 — the same wave-ordering gap plan 37F-16 (`SharePage.tsx`) already hit and documented the same way in its own SUMMARY. Every behavior is proven with mocked `fetch`.
- **`ShareModal.tsx`** — the phase's densest UI. The tier radio group defaults to internal and **never** preselects public (D-01); selecting public reveals the ChatGPT-derived honesty warning (`role="note"`, `aria-describedby`-linked to the radio) and the 7-day-preselected expiry chips, both via one orchestrated `animate-in` reveal per CLAUDE.md's Frontend_aesthetics. Minting transitions `creating` (disabled primary, `role="status"`) → `shared`, where the URL renders read-only and is copied by a **separate gesture** through the existing, already-tested `useCopyAction` hook — sidestepping the Safari `ClipboardItem` bug by construction, not convention. The per-tier URL (`/shared/{id}` vs `/s/{token}`) is rendered exactly as the server shapes it, never constructed or guessed client-side. Update re-snapshots and returns to `shared` with the same URL; Revoke is gated behind `RevokeConfirmDialog` and, on success, returns to the idle create form (RESEARCH §5) rather than a dead end. Errors from any of the three mutating calls render in `role="alert"`.
- **`RevokeConfirmDialog.tsx`** — copied from `conversations/DeleteConfirmDialog.tsx` per the plan's explicit instruction, over the shared, portal-backed, focus-trapped `ConfirmDialog` primitive.
- **`shareViewModel.ts`** — pure helpers (`expiryOptionForApi`, `isCustomExpiryInvalid`, `computeStaleCount`, `daysUntil`, `isLinkExpired`, `absoluteShareUrl`, `formatShareDate`) extracted ahead of the component, mirroring `documents/documentViewModel.ts`'s established split. **100% statement/branch/function/line coverage.**
- **Stale-detection wiring**: two additive, forward-compatible optional fields — `ShareLink.snapshot_turn_count` and `Conversation.LastActiveAt`/`TurnCount` — back the "N new messages are not in this link" affordance. Neither is populated by any live Go endpoint today (documented as a Known Stub below), so the note safely never renders in production yet; the full logic is implemented and unit-tested via mocks, ready to activate the moment a future backend plan projects these fields.
- **AppShell wiring**: `ShareModal` mounts via `useSharePanel`'s `shareModalState`/`closeShare`, using the exact conditional-mount idiom already used for the onboarding wizards (mounted only while open — resets the modal's internal state fresh every time, per D-01). `AppShell.tsx` grows 529 → 536 LOC, comfortably under the 600 cap.
- **Verification**: `web/src/chat/share` — 68/68 tests pass (18 shareApi + 26 ShareModal + 24 shareViewModel); `tsc --noEmit` clean; `eslint` 0 errors on every touched file; `prettier --check` clean; coverage 94.81% statements / 90.57% branches / 94.28% functions / 99.13% lines (≥85% floor, `shareViewModel.ts` at a perfect 100%); `check-file-size.sh` clean (`ShareModal.tsx` 387 LOC, `AppShell.tsx` 536 LOC); the full web suite (184 files / 1552 tests) passes with zero regressions, including all 34 pre-existing AppShell/ShareToggle tests unedited.

## Task Commits

Each task was committed atomically (Task 2 as its own RED/GREEN pair per its `tdd="true"` attribute):

1. **Task 1 RED: shareApi.ts failing tests** - `ec7660a1` (test)
2. **Task 1 GREEN: shareApi.ts implementation** - `bd5d9c49` (feat)
3. **Task 2 RED: ShareModal + shareViewModel failing tests** - `ded235a2` (test)
4. **Task 2 GREEN: ShareModal, RevokeConfirmDialog, shareViewModel** - `343d3345` (feat)
5. **AppShell integration: mount ShareModal via useSharePanel** - `bdc0ac7c` (feat)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `web/src/chat/share/shareApi.ts` - typed client for the 4 owner-scoped share routes
- `web/src/chat/share/shareApi.test.ts` - 18 tests
- `web/src/chat/share/ShareModal.tsx` - the tier-before-mint state machine (387 LOC)
- `web/src/chat/share/ShareModal.test.tsx` - 26 tests covering every behavior row
- `web/src/chat/share/RevokeConfirmDialog.tsx` - the destructive-revoke confirm
- `web/src/chat/share/shareViewModel.ts` - pure expiry/stale/date helpers (100% covered)
- `web/src/chat/share/shareViewModel.test.ts` - 24 tests
- `web/src/chat/share/shareTypes.ts` - +`ShareLink.snapshot_turn_count` (additive, optional)
- `web/src/conversations/useConversations.ts` - +`Conversation.LastActiveAt`/`TurnCount` (additive, optional)
- `web/src/i18n/resources.share.ts` - +4 key pairs (en+it): `expiry.customDaysLabel`, `shared.expiresInDays`, `shared.metaNoExpiry`, `shared.revoked.announcement`
- `web/src/AppShell.tsx` - mounted `ShareModal` (529 → 536 LOC)

## Decisions Made

See frontmatter `key-decisions` for full rationale on: writing `shareApi.ts` against the locked plan contract instead of a not-yet-existing backend file; the `conversation_id`-wire/`threadId`-TS naming split; the numeric-`ExpiryOption` encoding for custom days; `ShareModal`'s self-contained (non-resuming) session scope; `'revoked'` as a distinct-but-identically-rendered phase; the additive stale-detection fields; and the `useCopyAction` reuse deviation from the plan's literal grep.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `internal/agui/share_api.go` (plan 37F-10) does not exist — wrote `shareApi.ts` against the locked plan contract instead**
- **Found during:** Task 1 read_first (the plan names this file as ground truth for routes/shapes/status codes)
- **Issue:** Plan 37F-15 is wave 4; plan 37F-10 (which creates `internal/agui/share_api*.go`) is wave 5 and has not been executed (no commit, no SUMMARY, file absent from disk — confirmed via `git log` and a direct read of the directory). The wave assignment does not match the plan's own content dependency.
- **Fix:** Read `37F-10-PLAN.md` in full (routes, request/response shapes, status codes, the exact 8-route table) and implemented `shareApi.ts` against that locked, detailed specification instead of the not-yet-written Go source. Cross-checked against `internal/share/service.go` (37F-08, already shipped) for the real `CreateRequest`/`ExpiryOption` field names, and `internal/share/expiry.go` for the exact `ExpiryOption` string values. This is the identical resolution plan 37F-16 (`SharePage.tsx`) already used and documented for the same wave-ordering gap, one wave earlier.
- **Files modified:** `web/src/chat/share/shareApi.ts` (header comment states this explicitly)
- **Verification:** All 18 `shareApi.test.ts` tests pass against a mocked `fetch`; the request/response shapes are internally consistent with `shareTypes.ts`'s already-locked `ShareLink` contract.
- **Committed in:** `bd5d9c49` (Task 1 GREEN)

**2. [Rule 2 - Missing Critical] Added 4 i18n key pairs `resources.share.ts` didn't yet have**
- **Found during:** Task 2, writing the custom-expiry input label, the shared-state metadata line, the internal-tier (no-expiry) metadata line, and the revoke-completion announcement — all required by the plan's own acceptance criteria ("No literal user-facing strings — every visible string comes from `t('share.…')`") and behavior rows.
- **Issue:** `resources.share.ts` (built in 37F-05) had `modal.expiry.{1d,7d,30d,custom}` but no label for the custom-days numeric input; `shared.meta` composes `{{expiry}}` as a bare fragment but no key produced that fragment; the internal tier has no `expires_at` at all, and reusing `meta`'s "expires in {{expiry}}" template with an empty/placeholder value would read as "expires in no expiry" (not a sentence in either language); no key announced a successful revoke.
- **Fix:** Added `expiry.customDaysLabel`, `shared.expiresInDays`, `shared.metaNoExpiry` (a separate template, not `meta` with a placeholder), and `shared.revoked.announcement`, in both `shareEn`/`shareIt`, matching the file's existing typography conventions.
- **Files modified:** `web/src/i18n/resources.share.ts`
- **Verification:** The pre-existing recursive key-tree parity test (`resources.share.test.ts`, 5 tests) passes unedited — it walks the tree rather than asserting a fixed key list.
- **Committed in:** `343d3345` (Task 2 GREEN)

**3. [Rule 2 - Missing Critical] Extended `Conversation`/`ShareLink` with additive stale-detection fields; no exact "N new messages" count is derivable from any existing frontend-accessible data**
- **Found during:** Task 2, implementing the plan's required "stale-snapshot affordance" (`must_haves.truths`: "tells the user how many new messages are missing from the link").
- **Issue:** RESEARCH.md's "compare `conversations.last_active_at` against `shared_links.updated_at`" undersells the gap: `internal/conversations.Conversation` (the Go struct the `/api/conversations/{id}` JSON response serializes) has no `LastActiveAt` field at all (verified by reading the struct directly — `last_active_at` is a real DB column used only in an internal `ORDER BY`, never projected to the API), and neither raw timestamp comparison would yield an exact turn COUNT regardless. This plan's `web_context` explicitly forbids Go changes.
- **Fix:** Added two additive, optional TS fields — `Conversation.LastActiveAt`/`TurnCount` and `ShareLink.snapshot_turn_count` — documented as unpopulated until a future Go plan projects them, with `shareViewModel.computeStaleCount` safely degrading to 0 ("not stale") when either is absent. The component is fully implemented and tested against mocked values; in live production today the note will not render (documented as a Known Stub below), matching the spec's own "no newer turns -> no note" branch for the right reason.
- **Files modified:** `web/src/chat/share/shareTypes.ts`, `web/src/conversations/useConversations.ts`
- **Verification:** `shareViewModel.test.ts`'s `computeStaleCount` suite (5 tests) covers both-absent, one-absent, positive-delta, and never-negative cases; `ShareModal.test.tsx`'s stale-note tests inject both fields via a mocked `useConversation`.
- **Committed in:** `343d3345` (Task 2 GREEN)

**4. [Rule 1 - Bug] `exactOptionalPropertyTypes` violations in test fixtures**
- **Found during:** Task 1 GREEN verification (`tsc --noEmit`)
- **Issue:** `{ ...publicLink, url: undefined }` and reading an optional source field back into another object's same-named optional field both fail under this project's `exactOptionalPropertyTypes: true` — the wire contract is "key absent," not "key present with value `undefined`."
- **Fix:** Built the affected test fixtures with the optional key omitted from the start (or a literal replacement value) instead of spreading-then-overriding.
- **Files modified:** `web/src/chat/share/shareApi.test.ts`
- **Verification:** `tsc --noEmit -p tsconfig.json` clean.
- **Committed in:** `bd5d9c49` (Task 1 GREEN)

**5. [Rule 1 - Bug] A cross-tool tsc/eslint disagreement on `screen.getByRole(...)`'s inferred return type**
- **Found during:** Task 2 GREEN verification — `tsc --noEmit` required `as HTMLInputElement`/`as HTMLButtonElement` casts on ~11 `getByRole` calls to access `.checked`/`.disabled`/`.value`, while `eslint`'s `@typescript-eslint/no-unnecessary-type-assertion` flagged those exact casts as unnecessary against the same `tsconfig.json`.
- **Fix:** Introduced two tiny local helpers (`asInput`/`asButton`) whose PARAMETER is explicitly typed `HTMLElement` — the cast inside the helper is then unambiguous to both tools regardless of how each resolves testing-library's overloads, and call sites read `asInput(screen.getByRole(...))` instead of a bare inline cast.
- **Files modified:** `web/src/chat/share/ShareModal.test.tsx`
- **Verification:** `tsc --noEmit` and `eslint` both clean simultaneously; all 26 tests still pass.
- **Committed in:** `ded235a2`/`343d3345`

**6. [Rule 1 - Bug] `container.querySelector` missed portal-rendered Dialog content**
- **Found during:** Task 2 GREEN verification — the "renders a real fieldset..." test failed with `fieldset` = null despite the JSX clearly containing one.
- **Issue:** Radix `Dialog` portals its content to `document.body`, outside the `container` div React Testing Library's `render()` returns and wraps the component in.
- **Fix:** Queried `document.querySelector`/`document.querySelectorAll` instead of `container.querySelector`, matching how `screen` itself resolves (portal-aware).
- **Files modified:** `web/src/chat/share/ShareModal.test.tsx`
- **Verification:** Test passes; documented inline as a comment for future maintainers.
- **Committed in:** `ded235a2`/`343d3345`

**7. [Rule 1 - Bug] `@typescript-eslint/unbound-method` on `navigator.clipboard.writeText` used as an assertion target**
- **Found during:** Task 2 GREEN verification (`eslint`)
- **Fix:** Captured the mock function in a local `mockWriteText` variable at mock-setup time and asserted against that local instead of re-reading `navigator.clipboard.writeText` as a method expression.
- **Files modified:** `web/src/chat/share/ShareModal.test.tsx`
- **Verification:** `eslint` clean; test still passes.
- **Committed in:** `ded235a2`/`343d3345`

**8. [Rule 1 - Bug] `@typescript-eslint/no-unnecessary-condition` false-positive-shaped redundant narrowing**
- **Found during:** Task 2 GREEN verification (`eslint`)
- **Issue:** `{showShared && link !== undefined ? (...) : null}` re-checked `link !== undefined` a second time after a boolean flag (`showShared`) had already incorporated it, which the linter flagged.
- **Fix:** Replaced the boolean-flag-plus-redundant-check pattern with a single derived, properly-narrowed `sharedLink: ShareLink | undefined` value, used consistently through that whole JSX branch instead of the raw `link` state variable. Objectively cleaner code, not just a lint appeasement.
- **Files modified:** `web/src/chat/share/ShareModal.tsx`
- **Verification:** `eslint` clean; all tests still pass; also simplified the `expired` computation to depend on the same `sharedLink`.
- **Committed in:** `343d3345`

**9. [Rule 3 - Blocking] A concurrent process committed to `master` mid-session, briefly diverting one commit onto an unrelated branch**
- **Found during:** Task 1 GREEN commit — the commit landed on `fix/ci-red-37f-drift` instead of `master`, because another active process (unrelated to this plan — 3 commits fixing sqlc/migration-0039/deploy-contract CI drift) checked out that branch in this SAME shared working tree between this executor's RED and GREEN commits (confirmed via `git reflog`: `checkout: moving from master to fix/ci-red-37f-drift` timestamped inside this executor's own verification window).
- **Fix:** Verified the working tree was clean and the stray commit touched only this plan's 2 files, then `git checkout master` (returning to the RED commit, already correctly on master) followed by `git cherry-pick` of the diverted GREEN commit — recovering the exact same content onto `master` under a new hash, with zero destructive operations and the other process's branch left completely untouched.
- **Files modified:** none (git history operation only)
- **Verification:** `git log`/`git diff` confirmed byte-identical content between the cherry-picked commit and the original; `master`'s branch pointer verified before every subsequent commit for the remainder of the session.
- **Committed in:** `bd5d9c49` (the recovered commit)

---

**Total deviations:** 9 auto-fixed (2 × Rule 3 — blocking issues from a wave-ordering gap and a shared-worktree branch collision; 2 × Rule 2 — missing critical i18n keys and stale-detection data plumbing; 5 × Rule 1 — bugs/tooling fixes, four of them lint/type tooling disagreements caught before commit).
**Impact on plan:** All auto-fixes were necessary for the plan's own stated truths/acceptance-criteria to hold, or were pre-existing environment conditions (the wave gap, the concurrent process) this plan had to work around without altering its own scope. No scope creep: no file outside this plan's `files_modified` intent was touched except the two additive, optional-field type extensions, which are documented, zero-behavior-change, and explicitly justified by the plan's own required stale-detection truth.

## Known Stubs

- **The stale-snapshot note will not render in production today.** `Conversation.LastActiveAt`/`TurnCount` and `ShareLink.snapshot_turn_count` are real, additive TypeScript fields with full, tested rendering logic (`shareViewModel.computeStaleCount`, exercised via `ShareModal.test.tsx`'s mocked `useConversation`), but **no live Go endpoint populates any of the three today** — `internal/conversations.Conversation` does not project `last_active_at` into its JSON response, and no share route (37F-10, not yet built) computes or returns a snapshot turn count. `computeStaleCount` safely degrades to 0 whenever either input is absent, so this is inert-but-correct rather than broken: the "no newer turns" branch is what actually renders for every real user today. **Resolving this requires a future Go-side plan** to (a) project `last_active_at` (and ideally a turn count) onto the `/api/conversations/{id}` response, and (b) have `internal/agui/share_api.go` (plan 37F-10) include `snapshot_turn_count` in `ShareLink` JSON responses. No frontend change will be needed when that lands — the fields are already typed and the component already reads them.
- **`web/src/shell/ChatWorkspaceControls.tsx`'s `hasInternalShare={false} hasPublicShare={false}` stub (introduced by plan 37F-14) is still outstanding** — this plan did not touch that file (it is not in `37F-15-PLAN.md`'s `files_modified`), and per 37F-14-SUMMARY.md's own "Next Phase Readiness," the real share-list query for `ShareToggle`'s indicator is plan 37F-17's responsibility (`useThreadShares`), not this plan's.
- **`ShareModal` never resumes an existing share on reopen** (by design — see the "self-contained session" key-decision above). A thread that already has a live internal or public share still opens the modal at the tier-selection `idle` form on every open, rather than jumping straight to `shared` with the existing link's data. This is a deliberate scope boundary, not an oversight; plan 37F-17's "Condiviso" section is the surface designed to show/manage already-existing shares.

## Issues Encountered

None beyond the deviations documented above — all were resolved inline within this plan's own execution.

## User Setup Required

None - no external service configuration, no new dependency, no migration, no env var. (No Go changes in this plan, per its `web_context`.)

## Next Phase Readiness

- `shareApi.ts`'s four functions, `ShareModal.tsx`, and `RevokeConfirmDialog.tsx` are ready for plan 37F-17 to build on: `listShares(threadId)` (already implemented, tested, and exported) is exactly what 37F-17's `useThreadShares(threadId)` needs to wrap in a `useQuery`.
- Live end-to-end verification (mint a real share, watch the modal talk to a real server) is blocked on plan 37F-10 (`internal/agui/share_api*.go`, wave 5) landing — this plan's own frontend code is fully provable via mocked `fetch` today (68/68 tests green) and needs no changes once 37F-10 ships, provided the real routes match the locked contract this client and 37F-16's `SharePage.tsx` were both written against.
- The Manual-Only visual inspection the plan's `<verification>` block calls for (open the modal in all four states, confirm public is never preselected, confirm the warning renders only for public) has NOT been performed this session — no dev server was started and no browser is available to this executor. Recorded as `human_judgment: true` (D8) for the verifier to pick up; all underlying behavior is unit-test-proven.
- Stale-detection is feature-complete but inert until a future Go plan projects `last_active_at`/a turn count onto the conversations and share APIs (see Known Stubs above) — tracked, not silently dropped.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 11 created/modified files verified present on disk:
`web/src/chat/share/shareApi.ts`, `shareApi.test.ts`, `ShareModal.tsx`, `ShareModal.test.tsx`,
`RevokeConfirmDialog.tsx`, `shareViewModel.ts`, `shareViewModel.test.ts`, `shareTypes.ts`,
`web/src/conversations/useConversations.ts`, `web/src/i18n/resources.share.ts`,
`web/src/AppShell.tsx`.

All 5 commit hashes verified present in `git log --oneline --all`:
`ec7660a1`, `bd5d9c49`, `ded235a2`, `343d3345`, `bdc0ac7c`.

Acceptance-criteria commands re-run and confirmed at summary time:
`grep -c "onClick" ShareModal.tsx` → 7 (no div-onClick tier selection);
`grep -cE "onClick=\{async" ShareModal.tsx` → 0;
`grep -c "ClipboardItem" ShareModal.tsx` → 0;
`wc -l web/src/AppShell.tsx` → 536 (≤600);
`bash scripts/check-file-size.sh` → all 1977 tracked files within cap;
`npx vitest run web/src/chat/share` → 68/68 passed;
`npx tsc --noEmit -p tsconfig.json` (web/) → clean;
`npx eslint web/src/chat/share/ web/src/AppShell.tsx` → clean;
scoped coverage for `web/src/chat/share/` → 94.81% stmts / 90.57% branches / 94.28% funcs / 99.13% lines (≥85% floor);
full web suite → 184 files / 1552 tests passed, zero regressions.
