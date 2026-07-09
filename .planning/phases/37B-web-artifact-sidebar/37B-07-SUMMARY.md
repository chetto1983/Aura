---
phase: 37B-web-artifact-sidebar
plan: 07
subsystem: ui
tags: [webart, appshell, resizable-panels, drawer, react-query, matchmedia, auto-open, vitest, react]

# Dependency graph
requires:
  - phase: 37B-web-artifact-sidebar (plan 05)
    provides: "onArtifact pump-signal threaded through streamSSE and surfaced as an ExternalStoreChat prop (fires frame.value.asset_id on aura.artifact; undefined on degraded)"
  - phase: 37B-web-artifact-sidebar (plan 06)
    provides: "ArtifactsPanel({ threadId, onClose }) — the self-contained Artefatti surface (derived list + rows + Scarica tutto + lazy preview)"
  - phase: 37A-web-artifact-delivery-lane
    provides: "GET /api/assets?thread_id= (identity-scoped list the ['assets', threadId] query invalidation refetches)"
provides:
  - "AppShell mounts ArtifactsPanel as a toggleable third right-side ResizablePanel (id chat-artifacts) on desktop and a right Drawer below lg, driven by a header doc-icon toggle"
  - "Dynamic panelIds ([...base] + 'chat-artifacts' when open) so react-resizable-panels v4 namespaces the 2- vs 3-panel layout under distinct localStorage keys — no CHAT_SHELL_LAYOUT_ID bump, existing saved layout untouched (D-02)"
  - "onArtifact handler: invalidates ['assets', activeThreadId] (live-merge refetch) + one-time-per-thread auto-open (threadId-keyed ref, re-arms on thread change) (D-11)"
  - "shell/useArtifactsPanel.ts (state seam) + shell/ArtifactsShell.tsx (toggle/panel/drawer components) — extracted so AppShell.tsx stays <600 LOC"
affects: [37B-08]

# Tech tracking
tech-stack:
  added: []   # client-side only — no new deps, no backend change, no migration, no env
  patterns:
    - "Dynamic panel membership: a conditional ResizablePanel joins the group only when open + desktop; panelIds is derived, so v4's per-panel-id-set storage keys keep the 2-panel layout byte-identical while the 3-panel arrangement persists separately (no key bump, no `order` prop)"
    - "JS viewport gate for a dynamic panel: unlike the nav rail (pure CSS container-query hide), the artifacts panel must decide DOM membership in JS (matchMedia (min-width: 64rem)) — a CSS-hidden panel would still claim group width"
    - "Dual-surface single intent: the toggle + auto-open route to the desktop ResizablePanel or the mobile overlay Drawer depending on viewport, through one openArtifacts/toggleArtifacts pair"
    - "One-time-per-thread guard via threadId-keyed useRef<Set<string>>: auto-open fires once per thread and never reopens after a manual close, while a new thread is absent from the set so it re-arms — the 'reset on thread change' contract without a separate reset effect (mirrors autoOpenedOnboarding)"
    - "Refactor-on-touch extraction: shell integration split into a .ts state hook + a .tsx components file to satisfy both the 600-LOC cap and react-refresh/only-export-components"

key-files:
  created:
    - web/src/shell/useArtifactsPanel.ts
    - web/src/shell/ArtifactsShell.tsx
    - web/src/AppShell.artifacts.test.tsx
  modified:
    - web/src/AppShell.tsx

key-decisions:
  - "Extracted the integration seam into two siblings (useArtifactsPanel.ts + ArtifactsShell.tsx) rather than inlining — the inline version pushed AppShell.tsx to 685 LOC (over the 600 cap); the split lands AppShell at 585 and keeps a component-only .tsx (react-refresh clean)"
  - "matchMedia (min-width: 64rem) drives the desktop-vs-drawer branch (not the CSS container-query the nav rail uses) because the artifacts panel enters/leaves the ResizablePanelGroup dynamically — a CSS-hidden Panel would still consume group width"
  - "Auto-open uses a threadId-keyed Set (not a boolean + explicit reset): a thread already in the set never reopens after a manual close; a new thread re-arms automatically, satisfying D-11's 'reset on thread change' without touching the existing usage-reset block"
  - "The header doc toggle floats over the chat workspace (absolute top-right, pointer-events managed) rather than editing ShellHeader — ShellHeader is out of this plan's files_modified scope; the float keeps chat layout unshifted"

requirements-completed: []  # WEBART-05/07/08 stay open — phase-spanning; live browser + aggregate land at terminal 37B-08

coverage:
  - id: D1
    description: "AppShell mounts the Artefatti panel as a toggleable third ResizablePanel on desktop (id chat-artifacts, after chat-workspace) and a right Drawer below lg, driven by one header doc toggle"
    requirement: "WEBART-05"
    verification:
      - kind: unit
        ref: "web/src/AppShell.artifacts.test.tsx#desktop toggle mounts the third ResizablePanel (aside) without corrupting the 2-panel key"
        status: pass
      - kind: unit
        ref: "web/src/AppShell.artifacts.test.tsx#below lg the toggle routes to the right Drawer, not the ResizablePanel (D-04)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Dynamic panelIds keep the persisted 2-panel layout untouched (no CHAT_SHELL_LAYOUT_ID bump) and the open/closed intent persists across remount"
    requirement: "WEBART-05"
    verification:
      - kind: unit
        ref: "web/src/AppShell.artifacts.test.tsx#is closed by default: no artifacts panel, and the persisted 2-panel key is untouched"
        status: pass
      - kind: unit
        ref: "web/src/AppShell.artifacts.test.tsx#persists open state across remount (D-03)"
        status: pass
    human_judgment: false
  - id: D3
    description: "onArtifact invalidates ['assets', threadId] (live-merge refetch) and auto-opens the panel exactly once per thread, re-arming on thread change"
    requirement: "WEBART-07"
    verification:
      - kind: unit
        ref: "web/src/AppShell.artifacts.test.tsx#is wired to ExternalStoreChat and invalidates [\"assets\", threadId] when fired"
        status: pass
      - kind: unit
        ref: "web/src/AppShell.artifacts.test.tsx#auto-opens once per thread: no reopen after manual close, re-arms on thread change"
        status: pass
    human_judgment: false
  - id: D4
    description: "The integrated cockpit reads/behaves correctly in a live browser (resize parity with the nav rail, mobile drawer gestures, real artifact auto-open, no layout regression)"
    requirement: "WEBART-08"
    verification: []
    human_judgment: true
    rationale: "Live-browser integration behaviour + visual/layout non-regression is a human judgment; it lands at the terminal 37B-08 e2e/UAT."

# Metrics
duration: ~35min
completed: 2026-07-09
status: complete
---

# Phase 37B Plan 07: AppShell Artefatti Integration Summary

**The Artefatti panel is wired into the running cockpit — a header doc-icon toggle mounts it as a resizable third ResizablePanel on desktop (dynamic panelIds, no layout-key bump, the saved 2-panel layout untouched) or a right Drawer below lg, and a live `aura.artifact` frame invalidates `['assets', threadId]` and auto-opens the panel once per thread.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-09T01:08Z
- **Completed:** 2026-07-09T01:33Z
- **Tasks:** 2 completed
- **Files created:** 3 (2 shell siblings + 1 test) · **Files modified:** 1 (AppShell.tsx)

## Accomplishments

- **Task 1 (`d4f25893`) — toggleable panel + mobile drawer.** A header doc-icon `ArtifactsToggle` (lucide `FileText`, `artifacts.toggleAria`) flips the panel on the live surface; the open/closed intent persists to `localStorage['aura.shell.artifacts-open']` (D-03). On desktop (`≥lg`) a third `ResizablePanel id="chat-artifacts"` (`19rem`/`16rem`/`32rem`, `preserve-pixel-size`) + its `ResizableHandle` mount **after** `chat-workspace` inside the `showConversationNavigation` branch, mirroring the nav rail. `panelIds` is derived (`[...CHAT_SHELL_PANEL_IDS, 'chat-artifacts']` only when open), so react-resizable-panels v4 namespaces the persisted layout per panel-id set — `CHAT_SHELL_LAYOUT_ID` stays `aura-chat-shell-v3`, no key bump, no `order` prop, the saved 2-panel layout untouched (D-02 / RESEARCH Pattern 1). Below `lg` the panel content routes through the shared right `Drawer` via the `useSurfaceRestore` overlay slot (D-04), opened by the same toggle.
- **Task 2 (`b8cd3ac8`) — onArtifact live-merge + one-time auto-open.** `onArtifact={handleArtifact}` is passed to `ExternalStoreChat` beside `onUsage`. `handleArtifact` invalidates `['assets', activeThreadId]` (reusing the invalidate-after-run pattern; 37A persists the asset before emitting the frame, so the refetch always finds it — D-11) and auto-opens the panel exactly once per thread via a `useRef<Set<string>>`: a thread already in the set never reopens after a manual close, and a new thread re-arms automatically (the "reset on thread change" contract, no separate reset effect — mitigates T-37B-19 UX-DoS).

## How the panel mounts (desktop ResizablePanel / mobile Drawer)

`useArtifactsPanel(surfaces, CHAT_SHELL_PANEL_IDS)` decides the surface. `useIsArtifactsDesktop()` reads `window.matchMedia('(min-width: 64rem)')` (a JS gate, unlike the nav rail's CSS container-query, because a conditional Panel must not be merely CSS-hidden — it would still claim group width). When desktop + open, `artifactsPanelMounted` is true → `AppShell` renders `<ArtifactsResizablePanel>` (a fragment of `ResizableHandle` + `ResizablePanel` wrapping the lazy `ArtifactsPanel` in an `<aside aria-label="Artifacts">`) as a direct child of the group, and `panelIds` gains `chat-artifacts`. Below `lg`, `artifactsPanelMounted` is false and the toggle instead drives `surfaces.openOverlay()`; `<ArtifactsDrawer>` (a `Drawer side="right"`) renders the same lazy `ArtifactsPanel` through the overlay reducer.

## How the toggle + onArtifact live-wiring work

- **Toggle:** `toggleArtifacts` flips `artifactsOpen` on desktop (persisted) or toggles `surfaces.overlayOpen` on mobile. `artifactsActive` (`isDesktop ? artifactsOpen : surfaces.overlayOpen`) drives the toggle's `aria-pressed`/`data-active`.
- **onArtifact:** the plan-05 pump fires `onArtifact(asset_id)` on each `aura.artifact` frame. `handleArtifact` (a) `queryClient.invalidateQueries({ queryKey: ['assets', activeThreadId] })` so `useThreadArtifacts` refetches the new asset, and (b) if `activeThreadId` is not yet in `autoOpenedThreads`, adds it and calls `openArtifacts()` (desktop → open the panel; mobile → open the overlay). The Set is per-instance and per-thread, so the auto-open is one-shot per thread and re-arms on a thread change.

## Task Commits

1. **Task 1: toggleable ResizablePanel + mobile Drawer** — `d4f25893` (feat)
2. **Task 2: onArtifact invalidate + one-time auto-open** — `b8cd3ac8` (feat)

_Both tasks are `tdd="true"`; each ships impl + tests in the AppShell.artifacts.test.tsx suite (6 tests total)._

## Files Created/Modified

- `web/src/AppShell.tsx` (modified, 585 LOC) — mounts `ArtifactsToggle` (floating over chat workspace), the desktop `ArtifactsResizablePanel` (conditional on `artifactsPanelMounted`), and the mobile `ArtifactsDrawer`; consumes `useArtifactsPanel` for state + `panelIds`; adds `handleArtifact` and `onArtifact={handleArtifact}` on the chat mount.
- `web/src/shell/useArtifactsPanel.ts` (created, 117 LOC) — the state seam: desktop detection, persisted open intent, dynamic `panelIds`, `openArtifacts`/`toggleArtifacts`/`closeDesktopPanel`, and `CHAT_ARTIFACTS_PANEL_ID`.
- `web/src/shell/ArtifactsShell.tsx` (created, 119 LOC) — the presentational pieces: `ArtifactsToggle`, `ArtifactsResizablePanel`, `ArtifactsDrawer` (each lazy-loading the plan-06 `ArtifactsPanel`).
- `web/src/AppShell.artifacts.test.tsx` (created, 230 LOC) — 6 tests: closed-by-default + 2-panel key untouched, desktop ResizablePanel mount + no corruption, open persistence across remount, mobile Drawer branch (matchMedia mock), onArtifact invalidate, one-time auto-open + no-reopen + re-arm on thread change.

## Decisions Made

See `key-decisions` frontmatter. In short: the 600-LOC cap forced a refactor-on-touch two-file extraction; matchMedia (not CSS) gates the dynamic panel; a threadId-keyed Set gives the one-time auto-open + re-arm; the toggle floats over chat rather than editing the out-of-scope ShellHeader.

## Deviations from Plan

### Refactor-on-touch (CLAUDE.md 600-LOC cap / plan_specifics-sanctioned)

**1. [Rule 3 - Blocking / CLAUDE.md NO GOD CLASS] Extracted the integration seam into two siblings**
- **Found during:** Task 1 (mounting the panel + toggle + drawer inline)
- **Issue:** The inline integration pushed `AppShell.tsx` to 685 LOC, over the hard 600-LOC cap (the pre-commit `file-size` hook would reject it). A single `.tsx` extraction still tripped `react-refresh/only-export-components` (a hook + constant exported beside components).
- **Fix:** Split into `web/src/shell/useArtifactsPanel.ts` (state hook + `CHAT_ARTIFACTS_PANEL_ID` + helpers) and `web/src/shell/ArtifactsShell.tsx` (the three presentational components). `AppShell.tsx` lands at **585 LOC**; the `.tsx` exports only components (react-refresh clean).
- **Files modified:** web/src/AppShell.tsx; created web/src/shell/useArtifactsPanel.ts, web/src/shell/ArtifactsShell.tsx
- **Verification:** pre-commit `file-size` hook green on both commits ("all source files within the 600-LOC cap"); `eslint --max-warnings=0` clean on all files.
- **Committed in:** `d4f25893` (Task 1 commit)

This is the exact sibling-extraction the plan's `<plan_specifics>` sanctioned ("plus any refactor-on-touch sibling extraction the 600-LOC cap forces — note it as a deviation"). No behavioral deviation from the plan's contract; `files_modified` (`AppShell.tsx` + test) is honored, with the two shell siblings added as the sanctioned overflow.

---

**Total deviations:** 1 (refactor-on-touch, plan-sanctioned). **Impact:** structural only — no scope creep, no contract change; keeps the shell under the cap.

## Issues Encountered

- The `import/first` ESLint rule referenced in an initial `eslint-disable` comment does not exist in this config (the plugin is `import-x`); removed the comment and moved all imports contiguous above the `vi.mock` block (vitest hoists mocks regardless), resolving both the phantom-rule error and an `import-x/order` group warning.
- `require-await` / `no-misused-promises` on the test's `act(async () => …)` and the router `navigate(...)` onClick — the fired `onArtifact` is synchronous, so the `act` calls were made sync and the `navigate` call `void`-wrapped.

## Validation

- `npx tsc --noEmit` — clean.
- `npx vitest run src/AppShell.artifacts.test.tsx` — 6/6 pass.
- `npx vitest run` (full web suite) — **138 files / 1142 tests pass** (no regression; the existing shell/chat suites incl. `sseAdapter.onArtifact` + `ExternalStoreChat.rehydration` stay green).
- `eslint --max-warnings=0` + `prettier --check` — clean on `AppShell.tsx`, `ArtifactsShell.tsx`, `useArtifactsPanel.ts`, `AppShell.artifacts.test.tsx`.
- Grep gates — `chat-artifacts` present, `aura-chat-shell-v3` present, `aura-chat-shell-v4` absent, no `order=` prop, `onArtifact` + `invalidateQueries` present (both plan verify blocks: `APPSHELL_PANEL_OK` + `APPSHELL_ONARTIFACT_OK`).
- File sizes — AppShell.tsx **585**, useArtifactsPanel.ts 117, ArtifactsShell.tsx 119 (all ≤600); pre-commit `file-size`/`dup`/`vet` hooks green on both commits (no `--no-verify`).

No backend change, no server list-order change, no new deps/migrations/env — client-side only. WEBART-05/07/08 stay `[ ]` (phase-spanning: live-browser integration + aggregate e2e land at the terminal 37B-08, matching the 37B-04/05/06 precedent); `requirements mark-complete` intentionally NOT run.

## Next Phase Readiness

- The full Artefatti surface is now live in the cockpit: derived list (06) + preview (04) + producer signal (05) wired through the AppShell (07). 37B-08 is the terminal live-browser UAT + aggregate coverage/e2e that closes WEBART-05..08.
- No blockers. The desktop resize parity, mobile drawer gestures, and real artifact auto-open are exercised in unit tests with mocked children; the live-browser sign-off (D4) is reserved for 37B-08.

## Self-Check: PASSED

- Files created: `web/src/shell/useArtifactsPanel.ts`, `web/src/shell/ArtifactsShell.tsx`, `web/src/AppShell.artifacts.test.tsx` — all FOUND on disk.
- Commits: `d4f25893`, `b8cd3ac8` — both present in `git log`.
- AppShell.tsx 585 LOC (≤600); tsc + full vitest suite (1142) green; lint/prettier clean.

---
*Phase: 37B-web-artifact-sidebar*
*Completed: 2026-07-09*
