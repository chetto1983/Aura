---
phase: 37B-web-artifact-sidebar
plan: 07
type: execute
wave: 5
depends_on: ["37B-05", "37B-06"]
files_modified:
  - web/src/AppShell.tsx
  - web/src/AppShell.artifacts.test.tsx
autonomous: true
requirements: [WEBART-05, WEBART-07, WEBART-08]
must_haves:
  truths:
    - "a header doc-icon toggle shows/hides the Artefatti panel; open/closed state is persisted"
    - "on desktop (≥lg) the panel is a third ResizablePanel (id='chat-artifacts') with its own ResizableHandle, mounted after chat-workspace, only inside the showConversationNavigation branch; panelIds is driven dynamically so the 2-panel and 3-panel layouts persist under distinct keys (no layout-key bump)"
    - "below lg the panel content renders in a Drawer side='right' routed through useSurfaceRestore's overlay slot, opened by the same toggle"
    - "onArtifact invalidates ['assets', threadId] and auto-opens the panel exactly once per thread (ref-guarded, reset on thread change)"
    - "the existing persisted 2-panel nav layout is untouched when the panel is closed"
  artifacts:
    - path: "web/src/AppShell.tsx"
      provides: "third ResizablePanel + resizer + header toggle + mobile right Drawer + onArtifact handler (invalidate + one-time auto-open)"
      contains: "chat-artifacts"
  key_links:
    - from: "web/src/AppShell.tsx"
      to: "web/src/chat/ExternalStoreChat.tsx"
      via: "onArtifact handler prop"
      pattern: "onArtifact"
    - from: "web/src/AppShell.tsx"
      to: "web/src/chat/artifacts/ArtifactsPanel.tsx"
      via: "panel body (ResizablePanel + Drawer)"
      pattern: "ArtifactsPanel"
  prohibitions:
    - "MUST NOT bump CHAT_SHELL_LAYOUT_ID (keep 'aura-chat-shell-v3') — v4 auto-namespaces per panel-id set; drive panelIds dynamically instead (RESEARCH Pattern 1)"
    - "MUST NOT use static panelIds with a conditional panel (read/save keys diverge → layout shift)"
    - "MUST NOT auto-open on every aura.artifact frame or every remount — one-time per thread, ref-guarded, reset on thread change"
    - "MUST NOT add an 'order' prop (no such prop in react-resizable-panels v4) — DOM order + stable id is the mechanism"
    - "MUST NOT render the artifacts panel/handle outside the showConversationNavigation branch"
---

<objective>
Integrate the Artefatti panel into the AppShell chat shell: a header doc-icon toggle, a dynamically-membered third `ResizablePanel` (id `chat-artifacts`, resizable like the nav rail, dynamic `panelIds` so no layout-key bump and the existing 2-panel layout is untouched), a mobile `Drawer side='right'` through the overlay reducer, and the `onArtifact` handler that invalidates `['assets', threadId]` and auto-opens the panel once per thread. This is the sole AppShell owner (all AppShell edits for 37B live here).

Purpose: Wire the built panel + preview + producer signal into the running cockpit without corrupting existing users' saved layout.
Output: `AppShell.tsx` mounting the panel (desktop + mobile), the toggle, and the live-merge/auto-open handler; `AppShell.artifacts.test.tsx`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md
@.planning/phases/37B-web-artifact-sidebar/37B-PATTERNS.md
@web/src/AppShell.tsx
@web/src/shell/Drawer.tsx
@web/src/shell/useSurfaceRestore.ts
</context>

<artifacts_produced>
This plan produces (all in `web/src/AppShell.tsx`):
- Dynamic `panelIds` (base `['chat-navigation','chat-workspace']`, + `'chat-artifacts'` when open) passed to `useDefaultLayout` (layout id unchanged: `aura-chat-shell-v3`).
- `artifactsOpen` persisted state + a header doc-icon toggle (adjacent share-arrow is 37F, NOT built).
- Third `ResizablePanel id="chat-artifacts" defaultSize="19rem" minSize="16rem" maxSize="32rem" groupResizeBehavior="preserve-pixel-size"` + a `ResizableHandle id="chat-artifacts-resizer"` mounted after `chat-workspace`, inside the `showConversationNavigation` branch.
- A mobile `<Drawer side="right" title={t('artifacts.title')}>` routed through `useSurfaceRestore` `openOverlay`/`closeOverlay`/`overlayOpen`.
- `handleArtifact(assetId?)` passed as `onArtifact` to `ExternalStoreChat`: invalidates `['assets', activeThreadId]` + one-time-per-thread auto-open (ref guard, reset on thread change).
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Third ResizablePanel + header toggle + mobile right Drawer</name>
  <behavior>
    - closed by default: chat is full-width, panelIds = ['chat-navigation','chat-workspace']; the persisted 2-panel layout key is untouched
    - toggling open: panelIds = [...base, 'chat-artifacts']; a third ResizablePanel + its handle mount after chat-workspace
    - the 3-panel arrangement persists under a distinct storage key (…v3:chat-navigation:chat-workspace:chat-artifacts); toggling does not corrupt the 2-panel key
    - below lg (matchMedia mock), the panel content renders in a Drawer side='right' (not the ResizablePanel), opened by the same toggle, routed through overlayOpen/closeOverlay
    - open/closed state persists across remount
  </behavior>
  <read_first>
    - web/src/AppShell.tsx:29-47 (lazy import), :49-50 (CHAT_SHELL ids), :150,155,187 (autoOpenedOnboarding one-time-guard + thread-change resets), :286-290 (useDefaultLayout), :439 (showConversationNavigation branch), :441-476 (the 2-panel group + nav resizer to mirror), :488-490 (the left Drawer to twin) — all cited with excerpts in PATTERNS "AppShell.tsx (MODIFY)".
    - web/src/shell/Drawer.tsx:25,67-69 (side='right' support) + web/src/shell/useSurfaceRestore.ts:75-81 (the unused overlay slot).
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Pattern 1" (dynamic panelIds, no key bump, exact tokens) + "Pattern 6" (mobile Drawer).
  </read_first>
  <action>
    In `web/src/AppShell.tsx`: keep `CHAT_SHELL_LAYOUT_ID='aura-chat-shell-v3'`; introduce `artifactsOpen` state persisted to localStorage (per D-03) and a dynamic `panelIds` (base ids + `'chat-artifacts'` when open) passed to the existing `useDefaultLayout` call. Add a header doc-icon toggle (a lucide file-ish glyph; `artifacts.toggleAria`) that flips `artifactsOpen`. Inside the `showConversationNavigation` branch, after the `chat-workspace` panel, conditionally render `<ResizableHandle id="chat-artifacts-resizer" aria-label={t('shell.resizeArtifacts')} withHandle />` then `<ResizablePanel id="chat-artifacts" defaultSize="19rem" minSize="16rem" maxSize="32rem" groupResizeBehavior="preserve-pixel-size" className="h-full min-h-0"><ArtifactsPanel threadId={activeThreadId} onClose={() => setArtifactsOpen(false)} /></ResizablePanel>` — only at/above `lg`. Below `lg`, add a `<Drawer open={surfaces.overlayOpen} side="right" title={t('artifacts.title')} onClose={(i) => surfaces.closeOverlay(i ?? 'explicit')}><ArtifactsPanel .../></Drawer>` twin of the existing left nav Drawer, opened via `surfaces.openOverlay()` from the same toggle. Import `ArtifactsPanel` (lazy is fine, mirroring the existing lazy pattern). Write `AppShell.artifacts.test.tsx` asserting: distinct localStorage layout keys for closed vs open (no 2-panel corruption), the `<lg` Drawer vs `≥lg` ResizablePanel branch (matchMedia mock), and toggle open/close + persistence.
  </action>
  <acceptance_criteria>
    - `CHAT_SHELL_LAYOUT_ID` still equals `'aura-chat-shell-v3'` (no bump); `panelIds` is dynamic (contains `'chat-artifacts'` only when open).
    - the artifacts `ResizablePanel` has `id="chat-artifacts"` and its handle is mounted before it, both inside the `showConversationNavigation` branch.
    - no `order` prop is used on any ResizablePanel.
    - a mobile-viewport test renders the right `Drawer`; a desktop test renders the `ResizablePanel`.
    - test asserts the persisted 2-panel storage key is unchanged when the panel is closed.
    - `cd web && npx vitest run src/AppShell.artifacts.test.tsx` exits 0; `npx tsc --noEmit` exits 0.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q "chat-artifacts" src/AppShell.tsx && grep -q "aura-chat-shell-v3" src/AppShell.tsx && ! grep -q "aura-chat-shell-v4" src/AppShell.tsx && npx vitest run src/AppShell.artifacts.test.tsx && npx tsc --noEmit && echo APPSHELL_PANEL_OK</automated>
  </verify>
  <done>Toggleable third ResizablePanel (desktop) + right Drawer (mobile), dynamic panelIds, no key bump, existing layout preserved; test green.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: onArtifact handler — invalidate query + one-time auto-open</name>
  <behavior>
    - passing onArtifact to ExternalStoreChat: when fired, it invalidates ['assets', activeThreadId] (refetch pulls the new asset)
    - the FIRST artifact in a thread auto-opens the panel exactly once; after the user closes it, subsequent artifacts do NOT reopen it
    - the auto-open guard resets on thread change (a new thread's first artifact auto-opens again)
    - onArtifact is wired alongside the existing onUsage prop
  </behavior>
  <read_first>
    - web/src/AppShell.tsx:150,160-166 (autoOpenedOnboarding ref-guard precedent), :155,187 (usage reset on thread change — mirror for the auto-open ref), :392-398 (the ExternalStoreChat mount with onUsage to add onArtifact beside).
    - web/src/chat/ExternalStoreChat.tsx — the `onArtifact` prop from plan 05 (contract to consume).
    - .planning/phases/37B-web-artifact-sidebar/37B-RESEARCH.md "Pattern 3" + Pitfall 6 (one-time auto-open guard).
  </read_first>
  <action>
    In `web/src/AppShell.tsx`: add `handleArtifact = useCallback((assetId?) => { queryClient.invalidateQueries({ queryKey: ['assets', activeThreadId] }); /* one-time auto-open */ }, [...])` and pass `onArtifact={handleArtifact}` on the `ExternalStoreChat` mount alongside `onUsage`. Guard the auto-open with a `useRef<Set<string>>` (or a threadId-keyed ref) mirroring `autoOpenedOnboarding`: auto-open transitions closed→open only when the ref has not fired for `activeThreadId`; reset the ref on thread change (mirror the existing `usage` reset at `:155,187`). Extend `AppShell.artifacts.test.tsx` (or a sibling) to assert: onArtifact invalidates `['assets', threadId]`, the first artifact auto-opens once, a re-fire after manual close does not reopen, and a thread change re-arms the auto-open.
  </action>
  <acceptance_criteria>
    - `onArtifact={handleArtifact}` is passed to `ExternalStoreChat` beside `onUsage`.
    - `handleArtifact` invalidates `['assets', activeThreadId]`.
    - test asserts one-time auto-open + no-reopen-after-close + re-arm on thread change.
    - `cd web && npx vitest run src/AppShell.artifacts.test.tsx` exits 0; `npx tsc --noEmit` exits 0.
  </acceptance_criteria>
  <verify>
    <automated>cd D:/Repo/Aura/web && grep -q "onArtifact" src/AppShell.tsx && grep -q "invalidateQueries" src/AppShell.tsx && npx vitest run src/AppShell.artifacts.test.tsx && npx tsc --noEmit && echo APPSHELL_ONARTIFACT_OK</automated>
  </verify>
  <done>onArtifact invalidates the assets query and auto-opens the panel once per thread (ref-guarded, thread-change-reset); test green.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| persisted layout state → panel group | A conditional panel must not corrupt existing users' saved 2-panel layout |
| SSE artifact signal → UI open/refetch | An event must not be able to force the panel open against the user's will repeatedly |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37B-18 | Tampering (layout corruption) | dynamic panelIds | mitigate | Drive `panelIds` dynamically so v4 read/save keys match the rendered set; 2-panel key untouched when closed; no key bump, no `order` prop |
| T-37B-19 | Denial of Service (UX) / annoyance | auto-open | mitigate | One-time-per-thread ref guard (reset on thread change) — cannot reopen after user closes it |
| T-37B-17 | Information Disclosure (IDOR) | onArtifact invalidate | accept | Invalidation only triggers a refetch of the already identity-scoped list endpoint; no new surface |
</threat_model>

<verification>
- `npx vitest run src/AppShell.artifacts.test.tsx` green.
- Existing AppShell tests stay green (2-panel layout non-regression).
- `npx tsc --noEmit` clean; grep confirms `chat-artifacts` present, `aura-chat-shell-v4` absent, no `order` prop.
</verification>

<success_criteria>
- The Artefatti panel is toggleable, resizable like the nav rail on desktop, a right Drawer on mobile, and auto-opens once per thread on the first artifact — without corrupting the existing persisted layout.
- Live artifacts refetch into the panel via query invalidation.
</success_criteria>

<output>
Create `.planning/phases/37B-web-artifact-sidebar/37B-07-SUMMARY.md` when done.
</output>
