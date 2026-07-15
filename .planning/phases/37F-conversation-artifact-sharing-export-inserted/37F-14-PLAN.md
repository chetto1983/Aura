---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 14
type: execute
wave: 3
depends_on: ["37F-05"]
files_modified:
  - web/src/shell/ShareShell.tsx
  - web/src/shell/useSharePanel.ts
  - web/src/shell/ArtifactsShell.tsx
  - web/src/AppShell.tsx
  - web/src/shell/ShareShell.test.tsx
autonomous: true
requirements: [WEBSHARE-02]

must_haves:
  truths:
    - "A share control renders in the floating cluster over the chat workspace, between the voice toggle and the artifacts toggle"
    - "The toggle signals an existing share without opening the modal — a live public link is visually distinct"
    - "The toggle announces itself as a dialog opener, not a pressed panel toggle"
    - "web/src/AppShell.tsx stays under 600 LOC"
    - "37B's 'the adjacent share-arrow is 37F, not built' comment is updated in the same commit that builds it"
  artifacts:
    - path: "web/src/shell/ShareShell.tsx"
      provides: "ShareToggle — the floating-cluster share control"
      min_lines: 30
    - path: "web/src/shell/useSharePanel.ts"
      provides: "share modal open/close state seam, extracted so AppShell stays under the cap"
      min_lines: 20
  key_links:
    - from: "web/src/AppShell.tsx"
      to: "web/src/shell/ShareShell.tsx"
      via: "mounted in the floating cluster between VoiceModeToggle and ArtifactsToggle"
      pattern: "ShareToggle"
  prohibitions:
    - "MUST NOT put the share control in ShellHeader.tsx — that is the APP-level header (nav, modes, approvals, theme, language, logout). Aura has no thread header; the seam is the floating cluster at AppShell.tsx:514-517, which 37B reserved in code."
    - "MUST NOT use aria-pressed on the share control — ArtifactsToggle uses it because it toggles a PANEL; the share control opens a DIALOG, so aria-pressed is an a11y bug. Use aria-haspopup=dialog."
    - "MUST NOT omit pointer-events-auto — the parent cluster is pointer-events-none, so a child without it is unclickable"
    - "MUST NOT copy useArtifactsPanel's localStorage-persist block or its desktop/mobile split — a share modal is not a persisted panel; persisting would resurrect a stale open modal across reloads"
    - "MUST NOT let AppShell.tsx exceed 600 LOC — it is at 591 and the delta must be ~4 LOC"
    - "MUST NOT leave the 37B 'not built' comment stale — it goes false the moment this ships"
    - "MUST NOT add a visible text label — the cluster is icon-only by established design"
---

<objective>
Build the share affordance in the one place Aura actually has for it — and honor the fact that 37B
**reserved the spot in code**.

D-05 was locked as a "thread-header share-arrow," but Aura has **no thread header**: `ShellHeader.tsx` is
the app-level header (nav, modes, approvals, theme, language, logout). The real top-right-of-thread is
the floating overlay cluster at `AppShell.tsx:514-517`, and `ArtifactsShell.tsx:20-22` says so outright:
*"the adjacent share-arrow is 37F, not built."* The locked intent is validated; only the DOM target moves.
That comment goes **false** the moment this plan ships, so it is updated in the same commit.

R-02 is the constraint: `AppShell.tsx` is at **591/600**. The state goes in a hook and the presentation in
a sibling module, so AppShell's delta is ~4 LOC.

One deliberate upgrade over the reference: open-webui gives **no signal on the affordance** — you must
open the modal to learn a share exists. That is a real discoverability gap. Aura can do better for ~4 LOC
using the `data-active` idiom the analog already uses.

Purpose: the entry point to the share surface.
Output: `ShareShell.tsx`, `useSharePanel.ts`, ~4 LOC of `AppShell.tsx`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@CLAUDE.md
</context>

## Artifacts this plan produces

`ShareToggle`, `useSharePanel`, `ShareModalState`.

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: ShareToggle — mirror ArtifactsToggle, with three deliberate deviations</name>
  <read_first>
    - `web/src/shell/ArtifactsShell.tsx` — **the whole file. `ArtifactsToggle` at `:23-45` is the exact template.** Copy: `readonly` props inline-typed; `useTranslation()`; `variant="ghost" size="icon"`; the `className` **character-for-character** (`pointer-events-auto rounded-full bg-surface/70 text-text-muted backdrop-blur hover:bg-surface-2 hover:text-text data-[active=true]:bg-surface-2 data-[active=true]:text-accent-text`); `aria-hidden="true" focusable="false"` on the lucide icon. `:20-22` is the reserved-spot comment — **read it, then update it in this commit.**
    - `web/src/shell/ShellHeader.tsx` — read enough to confirm for yourself it is the **app-level** header (nav trigger, mode switcher, approvals, runtime chip, theme, language, logout). This is what the share control must NOT join.
    - `web/src/AppShell.tsx:514-517` — the floating cluster: a `pointer-events-none absolute right-3 top-2.5 z-20 flex items-center gap-1` div wrapping `VoiceModeToggle` and `ArtifactsToggle`. Children **must** set `pointer-events-auto`.
    - `web/src/chat/displays/LocalArtifactDisplay.tsx:81` — the `text-warning` token exists; it is the treatment for a live public link.
    - `web/src/i18n/resources.share.ts` — the toggle aria-label key (plan 37F-05)
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"UI/UX Research" §1 — the placement argument, the icon choice, and the "already shared" state signal
  </read_first>
  <behavior>
    - Renders a ghost icon button carrying the share glyph
    - No shares ⇒ neutral (`text-text-muted`), `data-shared` false
    - ≥1 active internal share ⇒ accent treatment, `data-shared` true
    - ≥1 active **public** share ⇒ the warning treatment (a live public link is the state a user most needs to notice)
    - Clicking calls `onOpen` exactly once
    - `aria-haspopup="dialog"` is present; `aria-pressed` is **absent**
    - `aria-label` is translated; there is no visible text label
    - The icon is `aria-hidden="true" focusable="false"`
    - The root carries `pointer-events-auto`
  </behavior>
  <action>
    Create `web/src/shell/ShareShell.tsx` exporting `ShareToggle`, mirroring `ArtifactsToggle` at
    `ArtifactsShell.tsx:23-45`.

    **Three deliberate deviations — record the reason for each in a comment**, or they read as sloppy
    copies:
    1. **`aria-haspopup="dialog"`, NOT `aria-pressed`.** `ArtifactsToggle` toggles a *panel*, so
       `aria-pressed` is right there. `ShareToggle` opens a *modal*; `aria-pressed` on a dialog-opener is
       an a11y bug.
    2. **Icon: lucide `Share2`** (the arrow-node glyph — the "share arrow" D-05 names), not `FileText`.
       `Link` is open-webui's choice but reads as "copy URL", not "share".
    3. **`data-shared` alongside `data-active`**, styled with the same `data-[…=true]:` idiom the analog
       already uses at `:40` — so `data-[shared=true]:text-accent-text`, plus the `text-warning` treatment
       for a live public link. This is the "beat open-webui" win and it costs ~4 LOC **because the pattern
       already exists**: open-webui gives no signal on the affordance at all, so a user cannot tell an
       active public link exists without clicking.

    Props: `readonly` inline-typed — the share counts (or a small summary object distinguishing
    internal-vs-public) and `onOpen`.

    **Update `ArtifactsShell.tsx:20-22` in this commit.** Its comment says *"the adjacent share-arrow is
    37F, not built."* That is false once this ships. Rewrite it to state that the share control now sits
    beside it and that both float over the chat workspace so they read as header controls without editing
    `ShellHeader`. (CLAUDE.md: comments-updated in the SAME commit.) Refactor-on-touch applies to that file
    as well.

    Write `web/src/shell/ShareShell.test.tsx` (vitest + @testing-library/react) covering every
    `<behavior>` row — including the **negative** a11y assertion that `aria-pressed` is absent, and the
    `pointer-events-auto` assertion (without it the button is unclickable inside the
    `pointer-events-none` cluster, which no visual review would catch).
  </action>
  <verify>
    <automated>npx vitest run web/src/shell && npx tsc --noEmit -p web/tsconfig.json</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/shell` passes, covering all 8 `<behavior>` rows.
    - **`aria-pressed` is absent and `aria-haspopup="dialog"` present** — asserted as an explicit negative test, not merely omitted from the source.
    - `grep -q "pointer-events-auto" web/src/shell/ShareShell.tsx`, and a test asserts it on the rendered root.
    - `grep -q "Share2" web/src/shell/ShareShell.tsx` — the lucide share glyph.
    - `grep -q "data-shared\|data-\[shared" web/src/shell/ShareShell.tsx`, and tests cover the three states (none / internal / public-warning).
    - **The 37B comment is updated:** `grep -c "not built" web/src/shell/ArtifactsShell.tsx` returns `0`.
    - The share control is NOT in the app header: `grep -c "ShareToggle" web/src/shell/ShellHeader.tsx` returns `0`.
    - No visible text label: the button's only child is the icon.
    - `npx tsc --noEmit -p web/tsconfig.json` clean; `npx eslint web/src/shell/ShareShell.tsx` reports 0 errors.
    - Both files ≤600 LOC.
  </acceptance_criteria>
  <done>`ShareToggle` mirrors `ArtifactsToggle` with three documented deviations (haspopup-not-pressed, Share2, data-shared), signals internal vs live-public state, and 37B's "not built" comment is gone in the same commit.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: useSharePanel + the AppShell mount, within a 4-LOC budget</name>
  <read_first>
    - **RE-MEASURE FIRST:** `wc -l web/src/AppShell.tsx`. It was **591/600** at plan time. The delta must be ~4 LOC → ~595, leaving 5 LOC of margin.
    - `web/src/shell/useArtifactsPanel.ts` — the analog, but **read it for what NOT to copy as much as what to copy.** Copy: the header's stated reason for existing (`:4-7` — "extracted from AppShell.tsx so the shell stays under the 600-LOC cap (refactor-on-touch)"), the exported-state-interface shape with a doc line per member (`:47-61`), and `useCallback` on every returned function (`:81-101`). **Do NOT copy** the localStorage-persist block (`:73-79`) or `useIsArtifactsDesktop` (`:27-45`).
    - `web/src/AppShell.tsx:514-517` — the cluster to mount into; `:522-524` — the conditional-mount idiom if the modal needs one
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md` §`useSharePanel.ts` — the explicit warning: this hook should be **~30 LOC, not 117**; if it ends up mirroring `useArtifactsPanel` closely, that is a smell that the wrong analog was followed
  </read_first>
  <behavior>
    - `useSharePanel()` returns a modal state (`closed` initially), an `openShare` callback, and a `closeShare` callback
    - `openShare` sets the state to open; `closeShare` returns it to closed
    - The state is **not** persisted — a reload starts closed
    - There is no desktop/mobile split
    - Every returned function is referentially stable across re-renders (`useCallback`)
  </behavior>
  <action>
    Create `web/src/shell/useSharePanel.ts`. Header: state the reason it exists, mirroring
    `useArtifactsPanel.ts:4-7` — extracted from `AppShell.tsx` so the shell stays under the 600-LOC cap
    (refactor-on-touch), with the presentational pieces in `./ShareShell`.

    Keep it **~30 LOC**: `useState` over the modal state plus `useCallback`'d open/close and the
    active-thread wiring. Export the state interface with a doc line per member.

    **Explicitly do not persist.** Add a one-line comment saying why the analog's localStorage block was
    not copied: a share modal is not a persisted panel, and persisting the open flag would resurrect a
    stale "share modal open" across reloads. And do not add the desktop/mobile split — a modal has none.
    If this hook approaches `useArtifactsPanel`'s 117 lines, stop: the wrong analog was followed.

    Mount in `web/src/AppShell.tsx` with **at most 4 LOC**: one import, one hook call, one `<ShareToggle>`
    element in the cluster, one conditional modal mount (plan 37F-15 supplies the modal; until then mount
    nothing or a placeholder that plan replaces — state which in the SUMMARY). Order in the cluster:
    `[VoiceModeToggle] [ShareToggle] [ArtifactsToggle]` — artifacts stays rightmost because it opens the
    right panel (spatial affinity); share is an action, not a panel toggle.

    **R-02 flags AppShell for a refactor-on-touch split *in this phase*.** 591 + 4 = 595 leaves 5 LOC of
    margin, which is not a margin. If the file lands above 595 — or if plan 37F-15's modal mount would push
    it over — extract a further state seam into `web/src/shell/` in the SAME commit, following the exact
    move `useArtifactsPanel.ts`'s header documents. Do not ship at the cap and leave the next plan to
    discover it.

    Write the `useSharePanel` tests (vitest, `renderHook`) covering every `<behavior>` row, including the
    **no-persistence** assertion (open, remount, assert closed) — the mutant this kills is a copied
    localStorage block.
  </action>
  <verify>
    <automated>npx vitest run web/src/shell && npx tsc --noEmit -p web/tsconfig.json && npx eslint web/src/shell/useSharePanel.ts web/src/AppShell.tsx</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/shell` passes, including the no-persistence test (open → remount → closed).
    - `wc -l web/src/AppShell.tsx` returns ≤ 595, and `bash scripts/check-file-size.sh` exits 0. If a split was needed, it landed in this commit and the SUMMARY names it.
    - `wc -l web/src/shell/useSharePanel.ts` returns ≤ 50 — the PATTERNS smell test (~30 LOC expected, not 117).
    - `grep -cE "localStorage|sessionStorage" web/src/shell/useSharePanel.ts` returns `0`.
    - `grep -cE "isDesktop|useIsArtifacts|matchMedia" web/src/shell/useSharePanel.ts` returns `0` — no desktop/mobile split.
    - The cluster order is `VoiceModeToggle`, `ShareToggle`, `ArtifactsToggle` — asserted by their line order in `AppShell.tsx`.
    - Every returned function is `useCallback`-wrapped.
    - `npx tsc --noEmit -p web/tsconfig.json` clean; `npx eslint` reports 0 errors on both files.
  </acceptance_criteria>
  <done>`useSharePanel` is a ~30-LOC non-persisted modal-state seam with no desktop split, `ShareToggle` is mounted between the voice and artifacts toggles, and `AppShell.tsx` is ≤595 LOC (or was split further in the same commit).</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| UI affordance → user's mental model of exposure | A live public link is the highest-consequence state in the phase. If the affordance does not signal it, a user cannot notice an active public share without clicking. open-webui has exactly this gap. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-65 | Information Disclosure | a user unaware a live public link exists on this thread | mitigate | `data-shared` on the toggle, with a distinct `text-warning` treatment for the public tier — the state a user most needs to notice. Tested across all three states. |
| T-37F-66 | Denial of Service | an unclickable control inside the `pointer-events-none` cluster | mitigate | `pointer-events-auto` on the root, asserted by test — a defect no visual review would catch. |
| T-37F-67 | Repudiation | a screen-reader user told the control is a pressed toggle when it opens a dialog | mitigate | `aria-haspopup="dialog"` with `aria-pressed` explicitly absent, asserted as a negative test. |
| T-37F-60 | Denial of Service | an `AppShell.tsx` LOC breach blocking every commit tree-wide | mitigate | Delta capped at ~4 LOC with a `wc -l ≤ 595` gate; state extracted to a hook; a further split lands in this commit if needed (R-02). |
| T-37F-68 | Tampering | a stale "share modal open" resurrected across reloads | mitigate | The persistence block from the analog is deliberately not copied; asserted by an open → remount → closed test. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | `lucide-react`, `react-i18next`, and the UI primitives are already present. No new web dependency. |
</threat_model>

<verification>
- `npx vitest run web/src/shell` (Windows Git Bash — WSL has no node)
- `npx tsc --noEmit -p web/tsconfig.json`
- `npx eslint web/src/shell/ web/src/AppShell.tsx`
- `wc -l web/src/AppShell.tsx` → ≤ 595
- `grep -c "not built" web/src/shell/ArtifactsShell.tsx` → 0
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
The share control lives in the floating cluster 37B reserved for it — not the app header — announces
itself as a dialog opener, and signals an active share (distinctly for a live public one) without the user
opening anything. `AppShell.tsx` stays under the cap, the state seam does not persist, and 37B's "not
built" comment is retired in the same commit that makes it false.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-14-SUMMARY.md` when done.
Record the post-edit `wc -l web/src/AppShell.tsx` and whether a further split was required.
</output>
