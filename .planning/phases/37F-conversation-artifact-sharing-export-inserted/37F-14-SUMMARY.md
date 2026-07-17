---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 14
subsystem: ui
tags: [react, i18n, typescript, accessibility, tailwind, share]

# Dependency graph
requires:
  - phase: 37F-05
    provides: "web/src/i18n/resources.share.ts — share.toggle.label (en+it), consumed by ShareToggle's aria-label"
provides:
  - "web/src/shell/ShareShell.tsx — ShareToggle, the floating-cluster/workspace-controls-row share entry point (mirrors ArtifactsToggle with 3 documented deviations: aria-haspopup=dialog not aria-pressed, Share2 icon, tri-state data-shared)"
  - "web/src/shell/useSharePanel.ts — the ~30-LOC non-persisted share-modal open/closed state seam"
  - "web/src/shell/useLogoutSession.ts — the logout/session-teardown state seam, split out of AppShell.tsx under R-02 (pure extraction, no behavior change)"
  - "ShareToggle mounted in web/src/shell/ChatWorkspaceControls.tsx between VoiceModeToggle and ArtifactsToggle (the locked order), wired through AppShell.tsx's useSharePanel() call"
  - "ArtifactsShell.tsx's stale 'the adjacent share-arrow is 37F, not built' comment retired"
affects: [37F-15, 37F-16, 37F-17]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Tri-state data-attribute (data-shared: 'false'|'internal'|'public') instead of two independent boolean data-attributes, to keep mutually-exclusive visual states free of Tailwind CSS-cascade-order ambiguity."
    - "State-seam-in-hook + presentation-in-sibling-component (R-02): mirrors useArtifactsPanel.ts/ArtifactsShell.tsx exactly — a hook owns state, a component renders it, AppShell.tsx only calls the hook and threads props."
    - "Behavior-preserving pure extraction verified by re-running PRE-EXISTING tests unedited (AppShell.shell.test.tsx's logout tests), rather than writing new tests for the extracted useLogoutSession.ts — the same technique 37F-05 used for the asset-source seam."

key-files:
  created:
    - web/src/shell/ShareShell.tsx
    - web/src/shell/ShareShell.test.tsx
    - web/src/shell/useSharePanel.ts
    - web/src/shell/useSharePanel.test.ts
    - web/src/shell/useLogoutSession.ts
    - web/src/shell/ChatWorkspaceControls.test.tsx
  modified:
    - web/src/shell/ArtifactsShell.tsx
    - web/src/shell/ChatWorkspaceControls.tsx
    - web/src/AppShell.tsx

key-decisions:
  - "MOUNT-POINT CORRECTION (the plan's central deviation): every locked doc (37F-CONTEXT.md D-05, 37F-RESEARCH.md, 37F-PATTERNS.md, and this plan's own <read_first>) targets 'the floating overlay cluster at AppShell.tsx:514-517' — a `pointer-events-none absolute right-3 top-2.5 z-20` div wrapping VoiceModeToggle + ArtifactsToggle. That cluster no longer exists. Commit 3f687e456 ('fix(web): contain chat controls and message actions', 2026-07-15) — which landed AFTER 37F's research/patterns were mapped at commit 1a3252e64 (verified via `git merge-base --is-ancestor`) — replaced it with `ChatWorkspaceControls.tsx`, a normal-flow (non-floating, non-pointer-events-none) row already threaded through AppShell.tsx as `<ChatWorkspaceControls artifactsActive={...} onArtifactsToggle={...} />`. ShareToggle mounts THERE instead, in the exact same locked order `[VoiceModeToggle, ShareToggle, ArtifactsToggle]`. Every higher-level truth in the plan's must_haves is still satisfied (the control renders between voice and artifacts, at the top-right of the chat workspace, icon-only, dialog-opener semantics); only the literal DOM container changed, and ArtifactsShell.tsx's own current-state comment (before this plan touched it) already described 'ChatWorkspaceControls keeps it in the workspace's normal-flow controls row' — the codebase had already moved past the docs. `pointer-events-auto` is kept anyway (harmless, and it is this plan's own explicit threat mitigation T-37F-66) in case the cluster is ever re-floated."
  - "Tri-state `data-shared` ('false'|'internal'|'public') instead of a boolean `data-shared` plus a second boolean attribute for the public-warning treatment — avoids two `data-[x=true]:text-*` Tailwind selectors racing for the same CSS property with unspecified cascade precedence. Mutually exclusive by construction; public always wins when both an internal and a public share are active, matching T-37F-65's 'the state a user most needs to notice.'"
  - "R-02 AppShell split: AppShell.tsx was ALREADY at 597/600 (not the plan's assumed 591) before any Task-2 edit — 3f687e456 grew it. Rather than ship at/over the cap, the logout/session-teardown block (LogoutTarget, loadLogoutTarget, the logout useCallback, ~75 LOC) was extracted verbatim into web/src/shell/useLogoutSession.ts, a pure behavior-preserving move proven by AppShell.shell.test.tsx's pre-existing logout tests passing unedited. Net: AppShell.tsx now 529/600, well clear of the cap with real margin for 37F-15's modal mount."
  - "hasInternalShare/hasPublicShare are hardcoded `false` at the ChatWorkspaceControls call site — no share-list data hook exists yet in this codebase (verified: no useShares/listShares/shareApi anywhere). Plan 37F-15 builds shareApi.ts and re-touches AppShell.tsx to mount the real ShareModal; that is also where the real query will replace this stub. Tracked below as a Known Stub."

requirements-completed: [WEBSHARE-02]

coverage:
  - id: D1
    description: "ShareToggle renders in the workspace-controls cluster (the corrected DOM target — see key-decisions), signals no-share/internal/public-live states via a tri-state data-shared attribute with the public tier getting the distinct warning treatment, announces itself as a dialog-opener (aria-haspopup=dialog, aria-pressed explicitly absent), has no visible text label, and calls onOpen exactly once per click"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/shell/ShareShell.test.tsx (9 tests: all 8 <behavior> rows + the negative aria-pressed assertion)"
        status: pass
      - kind: unit
        ref: "web/src/shell/ChatWorkspaceControls.test.tsx (3 tests: DOM order VoiceModeToggle→ShareToggle→ArtifactsToggle, onShareOpen wiring, neutral data-shared with no data source wired)"
        status: pass
    human_judgment: false
  - id: D2
    description: "useSharePanel: a ~30-LOC non-persisted modal-state seam (closed/open, openShare/closeShare, useCallback-stable), with no localStorage/sessionStorage and no desktop/mobile split, wired into AppShell.tsx and threaded to ChatWorkspaceControls"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/shell/useSharePanel.test.ts (5 tests: starts closed, open/close, no-persistence across remount, grep-sentinel for storage APIs, referentially-stable callbacks)"
        status: pass
      - kind: other
        ref: "grep -cE \"localStorage|sessionStorage\" web/src/shell/useSharePanel.ts -> 0; grep -cE \"isDesktop|useIsArtifacts|matchMedia\" -> 0; wc -l -> 36 (<=50 smell threshold)"
        status: pass
    human_judgment: false
  - id: D3
    description: "AppShell.tsx stays under the 600-LOC cap after mounting the share entry point, via an R-02 extraction of the logout/session-teardown block into useLogoutSession.ts with zero behavior change"
    requirement: "WEBSHARE-02"
    verification:
      - kind: other
        ref: "wc -l web/src/AppShell.tsx -> 529 (cap 600); bash scripts/check-file-size.sh -> exit 0 (\"all 1957 tracked source file(s) within the 600-LOC cap\")"
        status: pass
      - kind: unit
        ref: "web/src/__tests__/AppShell.shell.test.tsx ('does not fall back to the legacy passphrase logout route', 'uses the Authula sign-out endpoint with its CSRF token when configured') — both pass UNEDITED post-extraction, proving byte-identical logout behavior"
        status: pass
    human_judgment: false

duration: 32min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 14: ShareToggle — The Floating-Cluster Share Entry Point Summary

**`ShareToggle` (mirrors `ArtifactsToggle` with 3 documented deviations) mounted in the actual current workspace-controls row — not the plan's assumed-but-since-refactored floating `AppShell.tsx` overlay — plus the `useSharePanel` modal-state seam and an R-02 `useLogoutSession` split that returns `AppShell.tsx` to 529/600 LOC**

## Performance

- **Duration:** ~32 min
- **Started:** 2026-07-17T12:53Z
- **Completed:** 2026-07-17T13:25Z
- **Tasks:** 2 planned, both completed (with one significant, thoroughly-documented mount-point deviation)
- **Files modified:** 9 (6 created, 3 modified)

## Accomplishments

- `ShareToggle` (`web/src/shell/ShareShell.tsx`) — a ghost icon button carrying the lucide `Share2` glyph, `aria-haspopup="dialog"` (never `aria-pressed`, since it opens a modal, not a panel), and a tri-state `data-shared` attribute (`'false' | 'internal' | 'public'`) that gives Aura a genuine discoverability win over open-webui's reference behavior (which gives no signal at all): a live public link renders with the distinct `text-warning` treatment, the state a user most needs to notice.
- `useSharePanel` (`web/src/shell/useSharePanel.ts`) — a 36-LOC non-persisted modal-state seam (`closed`/`open`, `openShare`/`closeShare`, both `useCallback`-stable), deliberately not copying `useArtifactsPanel`'s browser-storage persistence or its desktop/mobile split.
- **The central deviation, fully documented:** every locked artifact (CONTEXT.md D-05, RESEARCH.md, PATTERNS.md, and this plan's own `<read_first>`) targets "the floating overlay cluster at `AppShell.tsx:514-517`" — a `pointer-events-none absolute` div. That cluster was refactored away by commit `3f687e456` ("fix(web): contain chat controls and message actions", 2026-07-15), which landed *after* 37F's research/patterns were mapped (verified via `git merge-base --is-ancestor 1a3252e64 3f687e456`). The real, current seam is `web/src/shell/ChatWorkspaceControls.tsx`, a normal-flow row already threaded through `AppShell.tsx`. `ShareToggle` mounts there instead, in the exact locked order `[VoiceModeToggle, ShareToggle, ArtifactsToggle]` — every higher-level truth in the plan's `must_haves` holds; only the literal DOM container was corrected to match reality.
- Retired `ArtifactsShell.tsx`'s stale "the adjacent share-arrow is 37F, not built" comment in the same commit that built it, replacing it with an accurate description of the current mount.
- **R-02 AppShell split, made necessary and larger than planned:** `AppShell.tsx` was already at 597/600 (not the plan's assumed 591) before any Task 2 edit — the same `3f687e456` commit grew it. Rather than land at or over the cap, the entire logout/session-teardown block (`LogoutTarget`, `loadLogoutTarget`, the `logout` `useCallback`, ~75 LOC) was extracted verbatim into `web/src/shell/useLogoutSession.ts` — proven behavior-preserving by running `AppShell.shell.test.tsx`'s pre-existing logout tests *unedited* after the move. Net result: `AppShell.tsx` is now **529/600 LOC**, with real margin for plan 37F-15's modal mount.
- `hasInternalShare`/`hasPublicShare` are honestly hardcoded `false` at the `ChatWorkspaceControls` call site (no share-list data hook exists in the codebase yet — verified by search) and tracked below as a Known Stub for plan 37F-15 to resolve.
- Full verification green: 63 tests across `web/src/shell` (7 files), `tsc --noEmit` clean, `eslint` 0 errors on every touched/created file, `scripts/check-file-size.sh` clean (1957 tracked files), and a full-repo vitest run (1455 tests) green except one pre-existing, unrelated flake in `DocumentsWorkspace.test.tsx` that passes in isolation (documented under Issues Encountered).

## Task Commits

Each task was committed atomically:

1. **Task 1: ShareToggle — mirror ArtifactsToggle, with three deliberate deviations** - `27ce6ab6a` (feat)
2. **Task 2: useSharePanel + the AppShell mount, within a 4-LOC budget** - `e8948d744` (feat)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `web/src/shell/ShareShell.tsx` - `ShareToggle` component: `Share2` icon, `aria-haspopup="dialog"`, tri-state `data-shared`, `pointer-events-auto`
- `web/src/shell/ShareShell.test.tsx` - 9 tests covering every `<behavior>` row incl. the negative `aria-pressed` assertion
- `web/src/shell/useSharePanel.ts` - `useSharePanel()`: 36-LOC non-persisted modal-state hook
- `web/src/shell/useSharePanel.test.ts` - 5 tests incl. the no-persistence-across-remount assertion and a storage-API grep sentinel
- `web/src/shell/useLogoutSession.ts` - **new (R-02 deviation):** the logout/session-teardown state seam extracted from `AppShell.tsx`
- `web/src/shell/ChatWorkspaceControls.tsx` - **modified (deviation):** now mounts `ShareToggle` between `VoiceModeToggle` and `ArtifactsToggle`
- `web/src/shell/ChatWorkspaceControls.test.tsx` - **new (deviation):** DOM-order + wiring test, since the plan's own order-assertion target moved out of `AppShell.tsx`
- `web/src/shell/ArtifactsShell.tsx` - retired the stale "not built" comment
- `web/src/AppShell.tsx` - removed the logout block (now in `useLogoutSession.ts`), added `useSharePanel()` call, threaded `onShareOpen` into `ChatWorkspaceControls`; net 597 → 529 LOC

## Decisions Made

See frontmatter `key-decisions` for the full rationale on: the mount-point correction, the tri-state `data-shared` design, the R-02 `useLogoutSession` split, and the hardcoded-false share-state stub.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The plan's locked mount point (a floating `pointer-events-none` cluster in `AppShell.tsx`) no longer exists**
- **Found during:** Task 1 read_first (re-reading `AppShell.tsx:514-517` before writing `ShareShell.tsx`)
- **Issue:** `37F-CONTEXT.md` D-05, `37F-RESEARCH.md`, `37F-PATTERNS.md`, and this plan's own `<read_first>`/`<action>` all describe the target as a `pointer-events-none absolute right-3 top-2.5 z-20 flex items-center gap-1` div in `AppShell.tsx` wrapping `VoiceModeToggle` + `ArtifactsToggle`. Reading the actual current file showed no such div — `AppShell.tsx` instead renders `<ChatWorkspaceControls artifactsActive={...} onArtifactsToggle={...} />`, and `ChatWorkspaceControls.tsx` (a separate, already-extracted, non-floating, normal-flow row) is what actually renders `VoiceModeToggle` + `ArtifactsToggle` today. `git merge-base --is-ancestor 1a3252e64 3f687e456` confirmed commit `3f687e456` ("fix(web): contain chat controls and message actions", 2026-07-15) landed after 37F's research/patterns were mapped at `1a3252e64` — the docs went stale, not wrong at authoring time.
- **Fix:** Mounted `ShareToggle` in `ChatWorkspaceControls.tsx` instead, in the identical locked order `[VoiceModeToggle, ShareToggle, ArtifactsToggle]`. Kept `pointer-events-auto` on `ShareToggle` regardless (harmless on the current non-`pointer-events-none` ancestor, and it is this plan's own T-37F-66 threat mitigation — defensive against the cluster ever being re-floated).
- **Files modified:** `web/src/shell/ShareShell.tsx` (placement note in the file header), `web/src/shell/ChatWorkspaceControls.tsx`, `web/src/shell/ChatWorkspaceControls.test.tsx` (new)
- **Verification:** `ChatWorkspaceControls.test.tsx` proves the DOM order and wiring; full `web/src/shell` + `AppShell.*.test.tsx` suites (88 tests) pass.
- **Committed in:** `27ce6ab6a` (Task 1, ShareShell.tsx placement note), `e8948d744` (Task 2, actual mount)

**2. [Rule 3 - Blocking] `AppShell.tsx` was already at 597/600, not the plan's assumed 591, before any edit**
- **Found during:** Task 2 read_first ("RE-MEASURE FIRST" instruction)
- **Issue:** `wc -l web/src/AppShell.tsx` returned 597, not the plan-time 591 both `37F-RESEARCH.md` and `37F-PATTERNS.md` cite. The same `3f687e456` commit that moved the workspace-controls row also grew `AppShell.tsx` by ~6 LOC. Landing even a 3-LOC ShareToggle-wiring delta would put the file at exactly 600 — zero margin, and already over the plan's own `≤595` acceptance threshold before any of this plan's work.
- **Fix:** Per the plan's own R-02 instruction ("If the file lands above 595 ... extract a further state seam into `web/src/shell/` in the SAME commit ... following the exact move `useArtifactsPanel.ts`'s header documents"), extracted the self-contained logout/session-teardown block (`LogoutTarget` interface, 3 consts, `loadLogoutTarget`, the `logout` `useCallback`, `logoutPending` state — ~75 LOC total) into a new `web/src/shell/useLogoutSession.ts` hook, mirroring `useArtifactsPanel.ts`'s "extracted from AppShell.tsx so the shell stays under the 600-LOC cap" framing.
- **Files modified:** `web/src/shell/useLogoutSession.ts` (new), `web/src/AppShell.tsx`
- **Verification:** Pure extraction, zero behavior change — proven by running `web/src/__tests__/AppShell.shell.test.tsx`'s pre-existing logout tests ("does not fall back to the legacy passphrase logout route", "uses the Authula sign-out endpoint with its CSRF token when configured") **unedited** after the move; both pass. `wc -l web/src/AppShell.tsx` now returns 529.
- **Committed in:** `e8948d744` (Task 2)

**3. [Rule 1 - Bug] A plan acceptance-criteria grep produced a false positive against a reasonable header comment**
- **Found during:** Task 2 self-verification (`grep -cE "localStorage|sessionStorage" web/src/shell/useSharePanel.ts`)
- **Issue:** `useSharePanel.ts`'s header comment originally said "Deliberately NOT copied from useArtifactsPanel: the localStorage-persist block..." — the literal word "localStorage" in a comment explaining what was NOT done tripped the same grep meant to catch actual persistence code, returning `1` instead of the required `0`.
- **Fix:** Reworded to "its browser-storage persistence block" — same meaning, no literal `localStorage`/`sessionStorage` token in the file.
- **Files modified:** `web/src/shell/useSharePanel.ts`
- **Verification:** `grep -cE "localStorage|sessionStorage" web/src/shell/useSharePanel.ts` now returns `0`; `useSharePanel.test.ts`'s grep-sentinel test (checking `useSharePanel.toString()`, i.e. the function body, not module comments) was unaffected throughout and still passes.
- **Committed in:** `e8948d744` (Task 2)

---

**Total deviations:** 3 auto-fixed (2 Rule 3 — blocking issues from stale plan artifacts vs. current codebase state; 1 Rule 1 — a grep false-positive against a first-draft comment, the same class of issue 37F-05 encountered and fixed the same way).
**Impact on plan:** All three are corrections to keep the implementation aligned with the ACTUAL current codebase (which drifted from the phase's research/patterns docs via an unrelated commit that landed between mapping and execution) or to satisfy the plan's own explicit LOC-budget mandate. No scope creep, no functional behavior invented beyond what the plan specified — every locked `must_haves` truth and every threat-model mitigation (T-37F-65/66/67/68) is met; only the literal DOM mount point and the extraction target moved.

## Known Stubs

- **`hasInternalShare`/`hasPublicShare` are hardcoded `false`** at `web/src/shell/ChatWorkspaceControls.tsx`'s `<ShareToggle ... />` call site. No share-list data hook (`useShares`/`listShares`/`shareApi`) exists anywhere in the codebase yet (verified by search before writing this plan's code). `ShareToggle` therefore always renders in its neutral state today, regardless of whether shares actually exist for the active thread. Plan **37F-15** (`web/src/chat/share/shareApi.ts` + the `ShareModal` mount, which also touches `AppShell.tsx` per its own frontmatter) is the plan that wires the real query. This does not block WEBSHARE-02 — the toggle's states, ARIA contract, and click wiring are fully implemented and tested; only the live data source is deferred, by design, to the plan that builds it.

## Issues Encountered

- A full-repo `npx vitest run` (1455 tests) surfaced one failure in `src/documents/__tests__/DocumentsWorkspace.test.tsx` ("Not implemented: navigation to another Document", a jsdom timeout) — a file this plan never touched. Re-running that single file in isolation passed cleanly (4/4), confirming it is a pre-existing, unrelated flake under full-suite parallel load, not a regression introduced here. Logged for awareness; not added to `deferred-items.md` since it is a flaky-under-load pre-existing test outside this plan's scope, not a newly discovered defect in files this plan modified.

## User Setup Required

None - no external service configuration, no new dependency, no migration, no env var.

## Next Phase Readiness

- `ShareToggle`, `useSharePanel`, and the `ChatWorkspaceControls` mount point are ready for plan **37F-15**, which builds `shareApi.ts` (create/update/revoke/list) and mounts the actual `ShareModal`, wired to `openShare`/`shareModalState`/`closeShare` already exposed by `useSharePanel()` in `AppShell.tsx`.
- 37F-15 should also replace the `hasInternalShare={false} hasPublicShare={false}` stub in `ChatWorkspaceControls.tsx` with real data once its share-list query exists.
- `AppShell.tsx` sits at 529/600 LOC — comfortable margin for 37F-15's modal mount (which its own plan frontmatter lists as touching `AppShell.tsx`).
- No blockers. The mount-point and LOC-budget deviations are fully documented above so a future reader (or auditor) understands why the implementation differs from the phase's locked research/patterns docs — the docs describe an earlier codebase state that an unrelated, later commit (`3f687e456`) superseded.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 9 created/modified files (`web/src/shell/ShareShell.tsx`, `ShareShell.test.tsx`,
`useSharePanel.ts`, `useSharePanel.test.ts`, `useLogoutSession.ts`,
`ChatWorkspaceControls.test.tsx`, `ChatWorkspaceControls.tsx`, `ArtifactsShell.tsx`,
`web/src/AppShell.tsx`) verified present on disk; both task commit hashes (`27ce6ab6a`,
`e8948d744`) verified present in `git log --oneline --all`.
