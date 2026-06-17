---
doc: cockpit-overhaul/02
title: Responsive App Shell + Sidebar + Navigation — Industrial SPEC
status: draft
created: 2026-06-17
owner: cockpit-overhaul milestone (v1.0.0 Deep Search Web Cockpit)
mobile_verdict: "shit" (390px live) — chat lane crushed to ~0 height; composer buried behind Runtime panel; sidebar "ugly"
design_system: Editorial Graphite (premium-calm) — see sibling 03-design-system-SPEC.md (the token anchor; PASS per 00-VALIDATION)
extends: .planning/phases/25-chat-approval-center/25-UI-SPEC.md, docs/design/aura-deep-search-figma/ux-spec.md
constraints: mobile-first (390px FIRST) · ≤600 LOC/file · vitest+Stryker ≥85%/≥70% · i18n en+it · WCAG 2.2
---

# 02 — Responsive App Shell + Sidebar + Navigation

> **Scope.** The cockpit chrome: the responsive layout system that holds the three
> regions — (a) conversation **sidebar**, (b) **chat lane** (the dominant region),
> (c) right **context/runtime panel** — plus the mode switcher and a redesigned
> `ConversationSidebar`. This SPEC does NOT redesign the chat thread, approval cards,
> or typed displays (those are Phase 25/26 and untouched here except where the shell
> hosts them). It fixes the **chrome that crushes the chat on mobile** and the
> **sidebar that looks unfinished**.

> **Token discipline.** Every color/space/radius/font below is a **named semantic token
> defined by `03-design-system-SPEC.md`** (the "Editorial Graphite" anchor; emitted into
> `web/src/styles/theme.css` `@theme`) — `--color-bg`, `--color-surface`,
> `--color-surface-2`, `--color-surface-3`, `--color-border`, `--color-border-strong`,
> `--color-text[-muted|-faint]`, `--color-accent`, `--color-accent-text`,
> `--color-accent-muted` (NOT the removed v1 `accent-dim`), `--color-on-accent`,
> `--color-ring`, `--color-success|warning|danger`, `--radius-sm|md|lg|xl|pill`,
> `--font-display|sans|mono`, `--shadow-1…4`, `--motion-dur-*`/`--motion-ease-*`, density
> `--space-unit`/`--row-h`. **No new colors, no new fonts.** Theme = "Editorial graphite
> (premium-calm)" as defined by 03.

---

## 0. Root cause — WHY the current grid overlaps/collapses on mobile

The live verdict ("chat crushed to ~0 height, composer buried behind the Runtime
panel") is a **deterministic CSS-grid track-collapse bug**, not a styling polish gap.
From `web/src/AppShell.tsx` (lines 62, 103, 107–141) and `web/src/chat/Composer.tsx`:

```
<div class="grid h-dvh [grid-template-rows:auto_1fr_auto]">   ← outer: header / MAIN / footer
  <header …/>                                                  ← auto
  <main class="grid grid-cols-1
               grid-rows-[auto_minmax(0,1fr)_auto]            ← MOBILE stack ↓
               lg:grid-cols-[14rem_minmax(0,1fr)_18rem] lg:grid-rows-1">
    <aside (sidebar) class="… border-b"/>                      ← row track 1: AUTO  (search + full conv list)
    <section (chat) class="flex flex-col"> … composer inside …</section>  ← row track 2: minmax(0,1fr)
    <aside (RuntimeHealthPanel) class="overflow-y-auto border-t"/>         ← row track 3: AUTO  (6+ status rows)
  </main>
  <RuntimeFooter/>                                             ← outer auto: Tokens·Cache·Cost·Context, flex-wrap
</div>
```

**Five compounding defects:**

1. **Two `auto` row tracks sandwich the one flexible track.** On mobile the `<main>`
   grid is `grid-rows-[auto_minmax(0,1fr)_auto]`. Track 1 (sidebar) and track 3
   (RuntimeHealthPanel) are **content-sized (`auto`) with no height cap**. The
   sidebar renders the search box + the **entire** conversation list; the right
   panel renders 6 status rows + last-checked. CSS grid resolves the two `auto`
   tracks to their full content height **first**, then hands whatever remains to the
   `minmax(0,1fr)` chat track. On a 390×~750 phone the two `auto` tracks routinely
   exceed the viewport, so the chat track resolves to its **minimum = 0** (that is
   precisely what `minmax(0,1fr)` permits). Chat ≈ 0 height. This is the headline bug.

2. **The composer lives *inside* the collapsed chat `<section>`.** `Composer.tsx`
   renders `ComposerPrimitive.Root` as a `border-t` strip at the bottom of the chat
   column (mounted by `ExternalStoreChat`, AppShell line 127). When the chat track
   collapses to ~0, the composer collapses/clips with it — it is not pinned to the
   viewport, it is pinned to a track that vanished.

3. **The right panel visually *overlaps* the composer** because it is the **next
   stacked block below** the (collapsed) chat, and then the outer-grid `RuntimeFooter`
   sits below *that*. So reading top→bottom on mobile the operator sees: header →
   thin sidebar → a sliver of chat → a **tall RuntimeHealthPanel** → the footer
   cluster. The composer (inside the sliver) is sandwiched/buried. There is no
   z-index overlap — it is **document-order burial**: three secondary chrome blocks
   are stacked between the operator and the one control they need.

4. **`h-dvh` on the outer grid is the wrong unit and is double-counted.** `dvh`
   reflows on every toolbar show/hide (jank), and because the inner tracks already
   overflow, `dvh` vs `svh` only changes *how badly* the chat is starved, not whether.

5. **The mode nav is non-functional decoration.** `MODES.map(... <span aria-current=…>)`
   renders 5 **`<span>`s** (AppShell line 71) — not buttons, not links, no `onClick`,
   `chat` hard-coded active. It wraps to `order-last w-full` on mobile, consuming a
   second header row. It is neither operable (WCAG 2.1.1) nor useful.

**The corrected model (specified in §2):** the **chat lane is the only flexible track
and is never sandwiched between two `auto` tracks**. On mobile the sidebar and the
right panel are **removed from the document flow** (they become overlay drawers/sheets
opened on demand), so the chat column owns the full viewport between header and a
**single bottom dock** (composer + collapsed footer), pinned with `100svh` +
`env(safe-area-inset-bottom)`. On desktop the three columns return as a real
3-column grid. Every flex/grid scroll parent gets `min-h-0`/`min-w-0` so a child can
shrink below content size (the canonical grid-collapse fix). The *reason* the columns
return only at `lg` and not sooner is the **380px chat-lane floor** (§1.1b): the side
regions stay overlays until the window is wide enough that the elastic chat track still
clears 380px after both side columns are reserved. This is correct per 2026 layout
guidance (svh over dvh for stable full-height; min-height:0 to let a grid child scroll
instead of overflow) — see §9 Citations.

---

## 1. Layout system — overview & the responsive contract

### 1.1 Three regions, three behaviors per breakpoint

| Region | Mobile (<640px) | Tablet (640–1023px) | Desktop (≥1024px) |
|---|---|---|---|
| **(a) Conversation sidebar** | **Off-canvas left drawer** (overlay, focus-trapped, scroll-locked). Trigger: header hamburger. Closed by default. | Off-canvas left drawer (same), trigger persists. | **Static left column** `15rem`, always visible, no overlay. |
| **(b) Chat lane** (dominant) | **Fills the viewport.** Single column between header and bottom dock. Internal scroll. Composer pinned to bottom dock. | Fills the remaining width beside the (collapsed) columns. | Center column `minmax(0,1fr)` — the elastic track. |
| **(c) Right context/runtime panel** | **Hidden from flow.** Opens as a **bottom sheet** (runtime health) from a header status chip; never inline, never above the composer. | **Right drawer** (overlay) from the header status chip. | **Static right column** `19rem`, always visible. |
| **Mode switcher** | **Bottom tab bar** (thumb zone) — 5 segmented items, `chat` active. | In-header segmented control. | In-header segmented control. |
| **Runtime footer (Tokens·Cache·Cost·Context)** | **Collapsed into the bottom dock** as a single-line compact strip above the composer; tap to expand the full cluster as part of the runtime bottom sheet. | Full cluster, single row, spans bottom. | Full cluster, single row, spans bottom. |

**Rationale (cited §9):** AI operator consoles (Claude.ai, ChatGPT) converge on a
collapsible/off-canvas left conversation rail + a chat region that owns the viewport
on small screens; the right "workspace" panel is demoted to an on-demand surface on
mobile. Bottom tab navigation is the 2025 gold standard for 3–5 primary destinations
because it sits in the thumb zone (~49% of users are one-thumb). We adopt exactly
this shape and keep the desktop 3-column cockpit intact.

### 1.1b The 380px chat-lane floor — the *content-driven* reason behind the breakpoints

The breakpoints in §1.2 are not arbitrary `sm`/`lg` picks; they are the viewport widths at
which **reserving the side regions would crush the chat lane below its usable minimum.** We
adopt odysseus's empirically-validated thresholds verbatim (`static/js/sidebar-layout.js:223-224`,
`MIN_CHAT_WIDTH = 380`, `AUTO_COLLAPSE_WIDTH = 700`) and make them a **hard invariant** of the
layout, not just a media-query side effect:

- **`--chat-lane-min: 380px`** — the chat lane (region b) **must never render narrower than
  380px**. Below 380px a chat lane is unusable (the operator's "shit on 390px" verdict is the
  same observation). This is the single most transferable number from the emphasized candidate.
- **`--window-floor: 700px`** — below a 700px *window* width the side regions cannot coexist
  in-flow with a ≥380px chat lane (380 + 15rem sidebar + 19rem panel ≫ 700), so they are
  **forced out of flow** into the drawer/sheet overlays of §1.1.

**The rule (the reason `lg:` is `1024px` and `sm` exists):**

> *If reserving sidebar and/or right-panel width would push the chat lane below `--chat-lane-min`
> (380px), those regions are NOT laid out in flow — they collapse to overlay drawers/sheets
> instead.* The desktop 3-column grid (`15rem · minmax(0,1fr) · 19rem`) is only entered when the
> window is wide enough that the elastic chat track still resolves ≥380px after both side columns
> are reserved (`15rem + 19rem ≈ 544px` of chrome ⇒ the chat lane clears 380px only past
> ~960–1024px — hence `lg:` is the correct flip, *derived* from the floor, not guessed). The
> `sm` tier (640px) keeps the sidebar a drawer precisely because 640 − 240 drawer ≪ a comfortable
> chat width; only the chat lane is in flow there.

Concretely the chat lane carries `min-w-[380px]` only inside the desktop grid track (where the
`minmax(0,1fr)` could otherwise be squeezed by an over-wide neighbor); below `lg` the lane is the
*sole* in-flow region so it is structurally ≥ the full content width and the floor is satisfied by
construction. The desktop grid additionally guards against a future docked panel (Phase 27+) eating
the lane: any in-flow surface that would drop the lane under `--chat-lane-min` must instead open as
an overlay (the same discipline as the mobile drawers). Verified by **AC-MOBILE-5** (§10).

### 1.2 Breakpoints (Tailwind v4 defaults — do not invent new ones)

| Name | Min width | Tailwind prefix | What flips |
|---|---|---|---|
| **mobile** (base) | 0 | (none) | Drawers + bottom tab bar + bottom dock; single column. |
| **sm** | 640px | `sm:` | Mode switcher moves to header; right panel becomes a side **drawer** (still overlay); footer shows full cluster. |
| **lg** | 1024px | `lg:` | Full **static 3-column** grid; drawers retire (sidebar + right panel become permanent columns); bottom tab bar hides (header switcher only). |

We use **two flips** (`sm`, `lg`). Below `sm` = phone. `sm`–`lg` = tablet/narrow laptop
(sidebar still a drawer to protect chat width; right panel a drawer). `lg`+ = the full
cockpit. This matches the shipped convention (`AppShell` already keys its only flip on
`lg`); we ADD the `sm` tier for the tablet middle ground and keep the same prefixes so
no `@theme` screen tokens change.

### 1.3 Media queries vs container queries — the 2026 decision

- **Layout-level region orchestration = media queries** (`sm:`/`lg:`). The shell is a
  *page* layout responding to the *viewport*; that is media queries' job and they
  remain correct for full-page layouts (cited §9). Drawer-vs-static, bottom-bar-vs-
  header, dock-collapse are all viewport decisions.
- **Component-internal adaptation = container queries** where a region must look right
  at *any* width it is given (drawer vs static column). Specifically: the
  **`ConversationRow`** and the **runtime metric cluster** read their *container* width
  via Tailwind v4 `@container` so the same component renders correctly whether it is
  in a `15rem` static column, a `min(88vw,20rem)` mobile drawer, or a desktop column —
  without each call site re-deriving breakpoints. This is the canonical container-query
  use (self-contained component reacts to its space, not the screen). We do **not**
  use container *style* queries (partial support) and keep container queries scoped to
  these two leaf components (overuse has a perf cost — cited §9).

```css
/* Tailwind v4: mark the sidebar scroll region as a query container */
@container sidebar (min-width: 16rem) { /* show secondary metadata line */ }
```
Implementation: add `@container/sidebar` to the list `<ul>` and use `@[16rem]:` variants
on the row's metadata; add `@container/footer` to the metric cluster.

---

## 2. The grid mechanics (exact, copy-pasteable)

### 2.1 Outer shell — `AppShell` root

**Replace** `h-dvh … [grid-template-rows:auto_1fr_auto]` with an `svh`-based shell whose
**only flexible track is the main region**, and whose bottom dock is a *single* track
(composer + collapsed footer), so the chat is never sandwiched.

```tsx
// AppShell root — mobile-first. svh = stable full height (no toolbar reflow jank).
// The dynamic delta is handled by the dock's env() padding + interactive-widget meta.
<div className="grid h-[100svh] min-h-0 overflow-hidden bg-bg text-text
                grid-rows-[auto_minmax(0,1fr)_auto]">
  <ShellHeader … />                 {/* row 1: auto */}
  <main className="min-h-0 …">…</main>   {/* row 2: minmax(0,1fr) — the elastic region */}
  <BottomDock … />                  {/* row 3: auto — composer (+ compact footer on mobile) */}
  {/* Mobile-only thumb nav is part of BottomDock; drawers/sheets are portaled, NOT in this grid */}
</div>
```

Key fixes vs today:
- `h-[100svh]` not `h-dvh` (stable; no reflow on toolbar collapse — §9).
- **The right panel and footer are NOT outer-grid tracks on mobile.** The footer is
  folded into `BottomDock`; the right panel is a portaled sheet. So the outer grid has
  exactly three tracks and the chat region (`minmax(0,1fr)`) gets everything the header
  and dock don't.
- `min-h-0` on root and on `<main>` so the elastic track can actually shrink-to-scroll
  rather than be pushed to overflow (the grid/flex min-content trap — §9).

### 2.2 `<main>` — region container

Mobile/tablet: `<main>` is a **single column**; the chat lane is its only in-flow child.
Sidebar + right panel are **portaled overlays** (not children of `<main>`), so they
cannot consume a track. Desktop (`lg:`): `<main>` becomes the static 3-column grid and
the same sidebar/right-panel components render **in-flow** (the drawer chrome is a
no-op at `lg`).

```tsx
<main
  aria-label={t('shell.workspace')}
  className="grid min-h-0 min-w-0 grid-cols-1
             lg:grid-cols-[15rem_minmax(0,1fr)_19rem]"
>
  {/* lg: in-flow sidebar column. <lg: rendered by the portaled Drawer instead. */}
  <ConversationSidebarRegion className="hidden lg:flex" … />

  {/* The dominant chat lane — ALWAYS in flow, ALWAYS the elastic track. */}
  <ChatLaneRegion className="min-h-0 min-w-0" … />

  {/* lg: in-flow right column. <lg: rendered by the portaled Sheet/Drawer instead. */}
  <RuntimePanelRegion className="hidden lg:block" … />
</main>
```

> The desktop columns are `15rem` / `minmax(0,1fr)` / `19rem` (was `14rem`/`18rem` — a
> 1rem bump each so the redesigned sidebar rows + the runtime panel breathe; still
> within the cockpit footprint). `minmax(0,1fr)` keeps the **0 lower-bound on the chat
> column** so long unbroken tool-result lines never blow out the grid width (the
> companion to the `min-w-0` fix). This is intentional and identical in spirit to the
> shipped value; only the side widths change.

### 2.3 Chat lane region — internal mechanics

```tsx
<section aria-label={t('shell.chatRegion')}
         className="flex min-h-0 min-w-0 flex-col bg-bg">
  {/* Scroll viewport — the ONLY thing that scrolls inside the lane. min-h-0 lets it
      shrink below content so it scrolls instead of pushing the dock off-screen. */}
  <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
    <ExternalStoreChat … />          {/* thread messages + inline approval cards */}
  </div>
</section>
```

The composer is **removed from inside the chat lane** and lifted into `BottomDock`
(§2.4) so it is pinned to the viewport, not to the scroll region. `ExternalStoreChat`
keeps owning the runtime + message store; the `ComposerPrimitive.Root` is rendered by
`BottomDock` **inside the same `AssistantRuntimeProvider`** (the provider wraps the
whole shell main+dock, not just the lane) so Send/Stop still bind the runtime. This is
a wiring move, not a logic change — see §5 file targets.

### 2.4 Bottom dock — the composer's permanent home

```tsx
// BottomDock: outer-grid row 3. Pinned by being a grid track (not position:fixed),
// so it never overlaps the scroll region and needs no z-index. Safe-area + keyboard
// handled here.
<div className="border-t border-border bg-bg
                pb-[env(safe-area-inset-bottom)]">  {/* notch / home-indicator */}
  <RuntimeFooterCompact className="sm:hidden" … />   {/* mobile: 1-line strip, tap→sheet */}
  <RuntimeFooter className="hidden sm:flex" … />      {/* sm+: full cluster row */}
  <Composer … />                                      {/* ComposerPrimitive.Root */}
  <ModeTabBar className="lg:hidden" … />              {/* mobile/tablet thumb nav */}
</div>
```

**Why a grid track, not `position: fixed`:** a fixed composer must reserve space (padding
on the scroll region) or it overlaps content; getting that padding right across keyboard
states is exactly the class of bug we are fixing. As an outer-grid `auto` row the dock
*is* layout — it reserves its own height, the elastic chat track gets the rest, and
there is structurally nothing to overlap. (Cited §9: fixed bottom bars are the common
source of occlusion; grid/flex tracks avoid it.)

**`svh` for the shell, `dvh`+keyboard-inset for the dock — the explicit reconciliation.**
These are two *different* units on two *different* elements, not a conflict (06 §5.2-item-4
reconciles this directly; odysseus uses `dvh` for keyboard adaptivity, this SPEC uses `svh`
for the outer grid):
- **Outer shell = `100svh`** (§2.1). The *small* viewport height is the stable, never-reflows
  full-height: it does not grow/shrink as the browser toolbar shows/hides, so the shell grid
  never janks. This is the right unit for the *page chrome*.
- **Composer dock = `dvh` + keyboard-inset semantics** (here). The dock is the one surface that
  *should* react to the keyboard. It does NOT set its own height in `dvh`; instead it relies on
  the layout viewport itself resizing (below) so the `svh` shell shrinks and the dock — being the
  bottom grid track — rides up with it. Where an engine exposes the keyboard inset, the dock reads
  it directly in CSS. Both behaviors agree; the shell stays stable, the dock tracks the keyboard.

**Keyboard occlusion (mobile):** the layout viewport units (`svh`) do **not** shrink for
the virtual keyboard on their own (§9). Two-layer defense:
1. **`<meta name="viewport" content="…, interactive-widget=resizes-content">`** in
   `index.html` so Chromium resizes the layout viewport when the keyboard opens →
   `100svh` shrinks → the dock rides up above the keyboard for free.
2. Progressive enhancement where supported: the dock adds
   `padding-bottom: max(env(safe-area-inset-bottom), env(keyboard-inset-height, 0px))`
   so on engines exposing `keyboard-inset-height` (the `dvh`/keyboard-inset tier) the composer
   clears the keyboard purely in CSS. Fallback engines get (1). No JS scroll-into-view hacks.

### 2.5 Z-index / stacking ladder (for drawers + sheets only)

The shell itself uses **zero z-index** (pure document/grid order). Overlays are portaled
to `document.body` and own a tiny, named ladder:

| Layer | z | Element |
|---|---|---|
| base | (auto) | shell grid, regions, dock |
| `--z-drawer-scrim` | 40 | drawer/sheet backdrop |
| `--z-drawer` | 50 | sidebar drawer / right sheet panel |
| `--z-dialog` | 60 | delete-confirm `<dialog>` (already native top-layer) |

Native `<dialog showModal()>` uses the browser top layer and beats everything; the
custom drawers sit below it so a delete-confirm opened from inside the sidebar drawer
still renders above the drawer.

---

## 3. Navigation — drawer mechanism, triggers, focus & scroll behavior

### 3.1 The `Drawer` primitive (one reusable component, two placements)

A single `Drawer` component backs both the **left conversation drawer** and the **right
runtime sheet/drawer**. It is the shipped a11y contract applied to an off-canvas panel.

**Implementation — native `<dialog>`, not a hand-rolled trap.** Per 2026 guidance
(cited §9) the native `<dialog>` element with `showModal()` gives focus-trap +
`Esc`-to-close + top-layer + backdrop **for free**; do NOT re-implement a JS focus trap
or `aria-modal`. We style `<dialog>` as an edge-anchored panel and animate it.

```tsx
interface DrawerProps {
  open: boolean;
  side: 'left' | 'right' | 'bottom';   // left=sidebar, right=runtime (sm), bottom=runtime (mobile sheet)
  labelledById: string;                 // points at the panel's own heading
  onClose: () => void;                  // backdrop click, Esc, close button
  children: ReactNode;
}
```

Behavior contract (all from the shipped `DeleteConfirmDialog` precedent, extended):
- `showModal()` on open; `close()` on `open=false`. The browser traps focus inside.
- **Focus on open:** the panel's first focusable (the search input for the sidebar; the
  close button for the runtime sheet). **Focus on close:** restored to the trigger
  (`restoreRef` pattern from `DeleteConfirmDialog.tsx`).
- **`Esc`** → native `cancel` event → `onClose` (we `preventDefault` + call `onClose`,
  exactly as `DeleteConfirmDialog` does).
- **Backdrop click** closes (click target === dialog element, outside the inner panel).
- **Scroll lock:** native `showModal()` makes the backdrop inert and the page behind
  non-interactive; we additionally set `overflow-hidden` on `<html>` while any drawer is
  open (one shared `useScrollLock` hook, ref-counted) and `overscroll-contain` on the
  panel's scroll area so the body never rubber-bands behind it.
- **Reduced motion:** slide-in transform + backdrop fade are wrapped in
  `motion-safe:`; `@media (prefers-reduced-motion: reduce)` → no transform, instant
  show (shipped skeleton precedent).
- **Inert siblings:** because `showModal()` already makes the rest of the document
  inert via the top layer, no manual `inert` attribute is needed. (Documented so a
  reviewer doesn't "add it back".)

Animation (motion-safe): `translate-x-[-100%]→0` (left), `translate-x-full→0` (right),
`translate-y-full→0` (bottom sheet), `--motion-dur-slow` (320ms) `--motion-ease-enter` — the
same `--color`-derived scrim as `DeleteConfirmDialog`'s `backdrop:bg-black/50`.

### 3.1b Touch-gesture layer — swipe-to-open / swipe-to-close (the iOS/Firefox-grade detail)

A button-only drawer is "responsive in theory"; the operator's "shit on 390px" verdict is what a
keyboard-grade-only drawer feels like under a thumb. Odysseus's swipe handlers
(`static/js/sidebar-layout.js:489-567` open, `:322-345` close) are the missing layer, and the
*discipline* — not the code — is what makes a swipe gesture coexist with native back-navigation and
vertical scroll. We add a single reusable `useEdgeSwipe` hook (`web/src/shell/useEdgeSwipe.ts`)
driving the same `Drawer` open/close state.

**Event model (the contract):** one **non-passive** `touchstart`/`touchmove`/`touchend` listener
(must be non-passive so `preventDefault()` is allowed) attached at the **document/shell level in the
capture phase**, active **only below `lg`** (no gestures on desktop static columns).

1. **Open (swipe-from-edge):** a `touchstart` whose `clientX` is within an **edge zone** (≤20px from
   the left screen edge for the sidebar; ≤20px from the right edge for the runtime sheet) arms a
   candidate gesture. The sidebar opens on a left→right drag from the left edge; the runtime sheet
   opens on a right→left drag from the right edge.
2. **Horizontal-lock disambiguation (the crux):** on `touchmove`, accumulate `dx`/`dy` from the
   start point. **Do nothing** until total travel ≥ **10px**. At that threshold, decide *once*:
   - if `|dx| > |dy|` → this is a **horizontal** gesture: **claim it** with `e.preventDefault()`
     (this is the one call that stops the browser from running its own back-swipe / overscroll) and
     begin translating the drawer with the finger (1:1, clamped to `[closed, open]`).
   - if `|dx| ≤ |dy|` → this is a **vertical** gesture: **bail permanently** for this touch (never
     `preventDefault`, never claim) so the chat/list scroll is untouched.
   - Abort an in-progress horizontal drag if vertical drift later exceeds **40px** (a diagonal
     scroll must not yank the drawer — odysseus `:333-334`).
3. **Commit on `touchend`:** snap open/closed by the **further-than-50%-OR-flung** rule (past the
   half-open point, *or* release velocity over a small threshold → open; otherwise spring back). The
   snap animation reuses the `Drawer` motion tokens and is `motion-safe:` gated.
4. **Close (swipe-toward-edge):** the symmetric gesture starting **inside an open panel** drags it
   back toward its edge; same 10px arm / `|dx|>|dy|` lock / 40px vertical-abort / 50%-or-fling commit.

**Scroll-owner exclude list (so vertical scroll is never hijacked).** Before arming, walk up from
`event.target`; if the touch originates inside an element that owns its own horizontal scroll or is a
text-entry/overlay surface, **do not arm the gesture at all.** Excluded (odysseus `:494-499`,
mapped to Aura's DOM):

```
pre, code, table,                         /* horizontally-scrollable tool-result / JSON blobs */
input, textarea, [contenteditable],       /* text entry — the composer included */
[role="dialog"], [data-drawer],           /* already-open overlays own their own gestures */
[data-no-swipe]                           /* opt-out escape hatch for any future widget */
```

The chat scroll viewport and the conversation list are **vertical** scrollers, so the `|dx|>|dy|`
lock already protects them — they are *not* in the exclude list (excluding them would kill the
swipe-to-open from over the chat, which is the whole point). The exclude list is only for
*horizontal* scrollers + text entry + overlays. Verified by **AC-NAV-4** (§10).

**Reduced motion:** the live finger-tracking translate is a direct manipulation (not an animation),
so it runs regardless; only the *snap-to-rest* tween is `motion-safe:` (instant settle under
`prefers-reduced-motion`).

### 3.1c Intent-aware restore — "one heavy surface at a time, remembered"

This is the multi-pane-on-a-phone crux odysseus formalizes (`sidebar-layout.js:381-475`) and the
discipline §0/§5 already gesture at but never specify. Distinct from **focus**-restore (§3.1, which
returns DOM focus to the trigger on close); **intent**-restore is about which *surface* is visible.

Rule, mobile/tablet only (at `lg` every region is a permanent column, so this is a no-op):

- **Only one heavy overlay owns the screen at a time.** Opening the **runtime sheet** (or any future
  heavy tool panel) while the **sidebar drawer** is open **auto-dismisses the sidebar** first, and
  **remembers** that it was open in a `sidebarWasOpenBeforeOverlay` flag (the shell owns this state,
  not the drawer).
- **Auto-restore on return to bare chat *only when the dismiss was system-initiated.*** When the
  operator closes the heavy overlay **via its close button or `Esc`** (an explicit "I'm done here,
  take me back" intent), the shell **restores** the sidebar to its remembered state. The operator is
  returned to exactly the surface configuration they had.
- **Swipe-dismiss does NOT restore.** When the operator **swipe-dismisses** the heavy overlay (a
  "get this out of my way, I want the chat" intent), the sidebar is **left collapsed** — restoring it
  would fight the operator's evident desire for a clean chat. The two dismiss paths are distinguished
  by an `intent: 'explicit' | 'swipe'` argument threaded from `Drawer.onClose` (swipe handlers pass
  `'swipe'`; the close button / `Esc` path passes `'explicit'`).
- Backdrop-tap is treated as **explicit** (tapping outside the panel is a deliberate dismiss), so it
  *does* restore — matching the operator's mental model that "tap-away" is a considered close.

State machine (shell-level, one reducer):

```
idle ──open sidebar──▶ sidebarOpen
sidebarOpen ──open runtime overlay──▶ overlayOpen   (remember sidebarWasOpen=true, dismiss sidebar)
overlayOpen ──close overlay (explicit|backdrop)──▶ restore sidebar if remembered ▶ sidebarOpen|idle
overlayOpen ──close overlay (swipe)──▶ idle          (do NOT restore; clear the remembered flag)
```

Verified by **AC-NAV-5** (§10).

### 3.2 Triggers (header)

`ShellHeader` (mobile/tablet) carries:
- **Hamburger button** (left) — `aria-label={t('shell.openConversations')}`,
  `aria-expanded`, `aria-controls={sidebarDrawerId}`. 44×44. Opens the left drawer.
  Hidden at `lg:` (sidebar is a permanent column).
- **Brand mark + "Aura"** (center-left) — unchanged `logo.png`.
- **Runtime status chip** (right) — a single pill showing the worst-of liveness/readiness
  tone + a one-word status (`Live` / `Degraded` / `Down`); `aria-label` announces it;
  opens the runtime **bottom sheet** (mobile) / **right drawer** (sm). Hidden at `lg:`
  (panel is a permanent column). This replaces burying a 6-row panel inline.
- **Approval badge + popover** — unchanged (`ApprovalBadge`/`ApprovalList`), already
  `ml-auto`. On mobile its popover opens as a bottom sheet variant of `Drawer` for
  reachability (same component, `side="bottom"`).
- **`LanguageSwitcher`** — unchanged; collapses into an overflow menu only if header
  crowds (optional, see §8).

The header is **one row at every width** (today it wraps the nav to a 2nd row). The mode
switcher leaves the header on mobile (→ bottom tab bar), freeing the space.

### 3.3 Mode switcher — `ModeSwitcher` (segmented) + `ModeTabBar` (mobile)

Replace the 5 inert `<span>`s with **real controls**.

- **Roving-tabindex segmented control**, `role="tablist"` semantics are wrong here (no
  tabpanels yet — only `chat` is live); instead use a `<nav>` of **`<button>`s** (or
  `<a>` once routes exist) with `aria-current="page"` on the active mode and
  `aria-disabled` + `title` on the not-yet-built modes (`tree`/`graph`/`displays`/
  `settings` are Phase 27/28). This is honest: they are visible (roadmap signal) but
  announce as unavailable, never as dead text.
- **Desktop/tablet (`sm:`)** = in-header horizontal segmented control (graphite track
  `bg-surface-2`, active item `bg-surface text-text` with an accent underline rule —
  accent reserved per 03-spec §4.3).
- **Mobile** = `ModeTabBar` in the bottom dock: 5 equal segments, each ≥44×44 (icon +
  micro label), thumb-zone, `aria-current` active. Segment min target 24×24 is the AA
  floor; we ship 44 (cited §9).
- Switching modes is a single `onMode(mode)` callback the shell owns (state today
  hard-codes `chat`); only `chat` resolves to a live region this milestone, the rest
  are disabled, so the callback is a no-op guard for now but the control is real.

---

## 4. Redesigned `ConversationSidebar`

The current list is the "ugly" surface: flat rows, bare "Untitled", a weak single-line
loading string, no recency structure, actions only on hover (invisible on touch).
Redesign — premium-calm, touch-first, token-pure.

### 4.1 Recency grouping (the structural fix)

Group the recent-first list (the store already orders by `CreatedAt`) under
**sticky section headers**: `Today` · `Yesterday` · `Previous 7 days` ·
`Previous 30 days` · `Older`. (Cited §9 — Copilot/ChatGPT recency grouping is the
expected pattern; operators repeatedly ask for it back when removed.)

- Bucketing is pure date math on `CreatedAt` (client tz, `Intl`), in a tested helper
  `groupByRecency(convs): {label: RecencyKey, items: Conversation[]}[]` — empty buckets
  omitted. `RecencyKey` is an enum; labels come from `t('conversations.groups.*')`.
- Headers are `text-[0.6875rem] uppercase tracking-wider text-text-faint`, `position:
  sticky top-0 bg-surface/95 backdrop-blur` so they pin while their group scrolls
  (the shipped micro-label scale; no new type role).
- Archived rows still gated behind the existing `includeArchived` toggle; when shown
  they collapse into a final **`Archived`** group rather than interleaving (clearer than
  today's inline `Archived` tag, which stays as a secondary marker).

### 4.2 Row design — `ConversationRow` (premium, touch-first)

Each row is a single accessible `<button>` (today's pattern, kept) but restyled and
restructured, container-query aware (§1.3):

```
┌───────────────────────────────────────────────┐
│ ▍ Meteo report for Rome                    ⋯   │   ← title (text-sm, truncate), accent left-rule when active
│   3:42 PM · 12k tok · $0.02                     │   ← @[16rem]: secondary meta line (mono numerics), text-faint
└───────────────────────────────────────────────┘
```

- **Title:** `displayTitle()` (kept). When unset, **stop showing the bare localized
  "Untitled"** — show a *derived* label: `t('conversations.untitledRun', { time })`
  → e.g. *"Untitled · 3:42 PM"*, so no two rows read identically and the row never
  looks blank/unfinished. (The auto-title backfills it later; this is the empty-title
  affordance, not a data change.)
- **Active state:** `aria-current="true"` + `border-l-2 border-l-accent bg-surface-2`
  (kept idiom; accent is reserved for active row per 03-spec §4.3 list item 3).
- **Hover/focus (desktop):** row raises to `bg-surface-2`, the `⋯` overflow trigger
  fades in (`group-hover`/`group-focus-within`). **Touch:** the `⋯` is **always
  visible** below `lg` (no hover on touch) — this is the single biggest touch defect
  today (actions only appear on `group-hover`, which never fires on a phone).
- **Secondary meta line** (`@[16rem]:flex`, hidden in a narrow drawer): relative time +
  token count + cost, all `font-mono` (the instrument signature from 03-spec §3.4). Hidden
  when the container is narrow so the title never gets crowded.
- **Min height** `min-h-[2.75rem]` (44px) so the whole row is a comfortable touch target
  (today rows are `py-1.5`, ~28px — below AA comfortable).

### 4.3 Row actions — rename / archive / delete

- **Overflow menu (`⋯`)** replaces the always-rendered inline action row. It is a small
  menu (`<button>` trigger → portaled `role="menu"` with `Rename` / `Archive`-or-
  `Unarchive` / `Delete permanently`). Keyboard: `Enter`/`Space` opens, arrow keys move,
  `Esc` closes + restores focus. This keeps the row clean and works on touch.
  - Implementation note (≤600 LOC discipline): the menu is its own file
    `RowActionMenu.tsx`; it may reuse the `Drawer`/native-`<dialog>`-popover idiom or a
    lightweight `role="menu"` — both acceptable; the menu MUST be keyboard-operable and
    focus-restoring.
- **Rename** = inline edit (kept exactly — `input` swap, Enter commits, Esc cancels,
  `aria-invalid` omit-when-valid, the shipped pattern in `ConversationSidebar.tsx`).
- **Archive/Unarchive** = reversible, no dialog (D-07, kept). After archive, a brief
  inline **Undo** affordance (`role="status"`, 6s) is a nice-to-have, not required.
- **Delete permanently** = routes through the existing `DeleteConfirmDialog`
  (unchanged; Cancel is default focus, focus restores). Untouched.

All mutation wiring (`useRenameConversation`/`useArchive…`/`useDelete…` in
`useConversations.ts`) is **reused verbatim** — this redesign is presentational +
structural, the data layer does not change.

### 4.4 Search — `SearchPanel`

Kept functionally (FTS over `/api/conversations/search`, snippet highlight, deep-link
open). Visual change: it becomes a **persistent header inside the sidebar region**
(sticky `top-0`, above the recency groups), with a leading magnifier SVG
(`aria-hidden`, 24×24 viewBox, `stroke=currentColor`, the shipped icon idiom) and a
clear-button when non-empty. On mobile the drawer opens with the search input focused.
No data/behavior change.

### 4.5 States — empty / loading / error (premium skeletons)

| State | Spec |
|---|---|
| **Loading** | Replace the single `<p>Loading conversations…</p>` with a **`ConversationListSkeleton`**: one faint group header + 5 `SkeletonBlock` rows (title bar + meta bar), built from the shipped `web/src/components/skeleton` primitives (`SkeletonText`, `SkeletonBlock`), `aria-busy`, `role=status`, `sr-only` "Loading conversations". Honors reduced-motion (skeleton.css already disables the wave). |
| **Empty (no conversations)** | Centered block (kept copy `conversations.empty.heading/body`) + a **primary `New run` CTA** (`bg-accent`, 44px) so the empty state is actionable, not just descriptive. Subtle graphite illustration optional (CSS-only, no asset). |
| **Empty (archived filter, none)** | "No archived conversations." (new key) — distinct from the no-conversations-at-all state. |
| **Error** | Kept `role="alert"` danger text + a **Retry** button (`refetch()`), so the operator isn't stuck. |
| **Search: searching / no hits** | Kept (`conversations.search.searching` / `…empty.*`); skeleton optional. |

### 4.6 Sidebar region header

Above search: brand-adjacent **`New run`** button (primary, 44px, `bg-accent`) +
the `Show archived` toggle (kept). On mobile this is the drawer's top bar with a close
(`×`) button at the trailing edge (44×44, `aria-label`).

---

## 5. The right Runtime/health panel — collapse + the overlap fix

**Root-cause recap (from §0):** the panel overlaps the composer because on mobile it is
an in-flow `auto` block stacked *between* the (collapsed) chat and the footer. It does
not z-overlap; it *document-order buries*.

**The fix:** the panel is **never in the mobile/tablet flow**.

| Breakpoint | Right panel |
|---|---|
| **Desktop `lg:`** | Static right column (`19rem`), `RuntimePanelRegion` in-flow, `RuntimeHealthPanel` unchanged. |
| **Tablet `sm:`** | Off-canvas **right `Drawer`**, opened by the header status chip. |
| **Mobile** | **Bottom sheet** (`Drawer side="bottom"`), opened by the header status chip; max-height `min(70svh, 32rem)`, its own internal scroll, `overscroll-contain`. It opens *over* the chat, closes back to chat — it can never sit between the chat and the composer because it is portaled to `document.body`, not stacked in `<main>`. |

The **header status chip** is the always-visible summary so liveness/readiness signal is
never lost on mobile (it's the "worst tone" of the existing rows). `RuntimeHealthPanel`'s
internal markup is **reused verbatim** inside the sheet/drawer/column — only its
*placement* changes. `useRuntimeHealth` polling is untouched.

The **runtime footer** (`Tokens·Cache·Cost·Context`) is a *different* surface (per-turn
+ session metrics, not health) and is handled in the bottom dock (§2.4): full cluster
`sm:`+, a one-line compact strip on mobile that expands into the same bottom sheet
(tabbed: Health | Metrics) or its own sheet. Either is acceptable; the contract is
**neither footer nor health panel may sit above the composer on mobile**.

---

## 5b. PWA layer (manifest + service worker) — optional, Phase-N, but specified

> **Status: optional / Phase-N (cheap-but-deferrable).** This is a genuinely *new* surface not in
> any sibling spec; 06 §5.2-item-5 marks it "optional-but-cheap." A mobile cockpit on flaky
> networks benefits from installability + an offline shell, and Aura already ships a service worker
> reference in the 03-design-system context (fonts precache). We specify the recipe here so a
> planner knows the shape and the **one critical constraint** even if v1.0.0 ships without it. If
> de-scoped, it lands behind a single `AURA_WEBUI_PWA` build flag in a later phase; nothing in
> §§1–5 depends on it.

Odysseus's PWA is the reference (`manifest.json` 15 lines, `sw.js` ~145 LOC, O6).

**Manifest (`web/public/manifest.json`, ~15 lines):** `display: "standalone"`, `start_url: "/"`,
`scope: "/"`, `name`/`short_name`, `theme_color` + `background_color` set to the resolved
`--color-bg` (`#14110E` — keeps PWA chrome on-theme; this is the same value the 03 `<meta
name="theme-color">` sync emits, O8), and two maskable icons (192/512, `purpose: "any maskable"`).
Linked from `index.html`.

**Service worker (`web/public/sw.js`, ≤150 LOC) — per-asset-type strategy, with the SSE rule as the
load-bearing constraint:**

| Request | Strategy | Why |
|---|---|---|
| **`/api/*` and the AG-UI `/agent/run` SSE stream** | **NEVER cached / NEVER intercepted** — bypass the SW entirely | **Critical.** A SW that buffers a `text/event-stream` response breaks streaming (the stream never "completes," so a `cache.put`/`respondWith(fetch())` clone stalls the dock). The SSE backend (`sseAdapter.ts`) must reach the network untouched. This is the one rule that, if violated, silently kills the whole chat. |
| **HTML navigation to the SPA root `/` only** | **stale-while-revalidate** | instant shell paint; deep links (`/c/:id`) **fall through** to network so they aren't hijacked by the cached index (odysseus's documented footgun). |
| **JS/CSS** | **network-first, cache fallback** | a code deploy shows on reload without a manual cache-clear. |
| **static assets (fonts/images)** | **cache-first, background refresh** | the subset woff2 fonts (03) load instantly offline. |

Install uses **per-item `cache.put`** (not `addAll`, so a single 404 can't abort the whole install)
and a **versioned `CACHE_NAME`** bumped on every shell change with `activate` deleting stale caches.

Registration is **opt-in and same-origin** (the SPA is embedded same-origin behind Authula auth, so
the SW scope is the cockpit only). Verified, *if shipped*, by **AC-PWA-1** (§10); skipped cleanly if
the phase de-scopes it.

---

## 6. The composer dock — sticky, safe-area, mobile keyboard (consolidated)

(Mechanics specified in §2.4; this is the acceptance-facing summary.)

1. **Pinned by layout, not by `position:fixed`** — the dock is the outer-grid bottom
   `auto` track. It reserves its own height; the chat scroll region gets the rest. No
   overlap, no z-index, no reserved-padding bookkeeping.
2. **Safe area:** `padding-bottom: env(safe-area-inset-bottom)` so the composer clears
   the iOS home indicator / Android gesture bar. Requires `viewport-fit=cover` in the
   viewport meta (add it).
3. **Keyboard:** `interactive-widget=resizes-content` in the viewport meta (Chromium
   resizes layout viewport → `100svh` shrinks → dock rides above keyboard) +
   `max(env(safe-area-inset-bottom), env(keyboard-inset-height,0px))` where supported.
4. **Composer itself** = the shipped `ComposerPrimitive` (`Composer.tsx`) — Send↔Stop
   swap, Enter sends / Shift+Enter newline / Esc cancels, 44×44 buttons — **kept
   verbatim**, only relocated into the dock and wrapped by the shell-level
   `AssistantRuntimeProvider`.
5. **Mobile composer max-height** `40svh` so a long draft never eats the whole screen;
   internal scroll on the textarea (kept `max-h-40` lifted to a viewport-relative cap on
   mobile).

---

## 7. Accessibility contract (WCAG 2.2)

Builds on the shipped Phase-23 a11y floor (`eslint-plugin-jsx-a11y` recommended is a
blocking gate; honor it). Net-new obligations for this SPEC:

- **2.1.1 Keyboard / 2.1.2 No trap:** mode switcher, hamburger, status chip, `⋯` menu,
  drawers all keyboard-operable; native `<dialog>` provides the only (correct) trap and
  `Esc` always escapes it.
- **2.4.3 Focus order:** drawer open → focus moves into panel; close → focus restored to
  trigger (`restoreRef`). Bottom tab bar is reachable in DOM order after the dock.
- **2.4.7 Focus visible:** `outline-2 outline-offset-2 outline-accent` on every new
  control (shipped pattern). `scroll-margin` on focus targets so the sticky header/dock
  never cover a focused element.
- **2.5.8 Target size (AA):** every primary/icon control ≥24×24 floor; hamburger, status
  chip, `New run`, tab-bar segments, drawer close, `⋯` trigger ship **44×44**.
- **1.3.1 / 1.4.1:** mode `aria-current="page"`; disabled modes `aria-disabled`; status
  chip pairs tone color with a text word (`Live`/`Degraded`/`Down`) — never color alone
  (reuses the `StatusDot` decorative-`aria-hidden` + text-label idiom).
- **4.1.2 Name/role/value:** hamburger/status chip carry `aria-expanded` +
  `aria-controls` pointing at the portaled drawer id; drawer `<dialog aria-labelledby>`
  points at its own heading.
- **Live regions:** drawer open/close need no announcement (focus move suffices); the
  status chip tone change announces `polite`; list errors `role="alert"`.
- **1.4.10 Reflow (320px):** no horizontal scroll at 320px CSS px — the single-column
  mobile shell + drawers guarantee it (the chat lane `min-w-0` + `minmax(0,1fr)` kill
  any long-line blowout). This is an explicit acceptance check.
- **Reduced motion:** all drawer/sheet/skeleton motion under `motion-safe:` +
  `prefers-reduced-motion` fallback.
- **i18n:** every new string in `t()`, present in **en + it**, under the existing
  namespaces (`shell.*`, `conversations.*`) — new keys listed in §8. Rebuild `web/dist`
  after copy changes.

---

## 8. New i18n keys (en + it, same commit)

Add under existing namespaces in `web/src/i18n/resources.ts` (shapes shown en; executor
adds it):

```
shell.openConversations        "Open conversations"        / "Apri conversazioni"
shell.closeConversations       "Close conversations"       / "Chiudi conversazioni"
shell.openRuntime              "Runtime status"            / "Stato runtime"
shell.workspace                "Workspace"                 / "Area di lavoro"
shell.newRun                   "New run"                   / "Nuova esecuzione"
shell.runtimeChip.live         "Live"                      / "Attivo"
shell.runtimeChip.degraded     "Degraded"                  / "Degradato"
shell.runtimeChip.down         "Down"                      / "Offline"
shell.modeUnavailable          "Coming soon"               / "In arrivo"
conversations.groups.today        "Today"                  / "Oggi"
conversations.groups.yesterday    "Yesterday"              / "Ieri"
conversations.groups.previous7    "Previous 7 days"        / "Ultimi 7 giorni"
conversations.groups.previous30   "Previous 30 days"       / "Ultimi 30 giorni"
conversations.groups.older        "Older"                  / "Meno recenti"
conversations.groups.archived     "Archived"               / "Archiviate"
conversations.untitledRun         "Untitled · {{time}}"    / "Senza titolo · {{time}}"
conversations.emptyArchived        "No archived conversations." / "Nessuna conversazione archiviata."
conversations.retry                "Retry"                  / "Riprova"
conversations.actions.more         "More actions"           / "Altre azioni"
conversations.menuLabel            "Conversation actions"   / "Azioni conversazione"
```

(`shell.modes.*`, `conversations.heading/loading/empty/search/delete/*` etc. already
exist — reuse, don't duplicate.)

---

## 9. File targets (all ≤600 LOC; refactor-on-touch)

| File | Action | Notes |
|---|---|---|
| `web/index.html` | edit | viewport meta → `width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content`. |
| `web/src/AppShell.tsx` | rewrite shell | `100svh` grid, three tracks, portaled drawers, region wiring; lift `AssistantRuntimeProvider` to wrap main+dock. Should SHRINK (logic moves out to focused files). |
| `web/src/shell/ShellHeader.tsx` | new | brand + hamburger + status chip + approval badge + lang switcher; one row at all widths. |
| `web/src/shell/BottomDock.tsx` | new | composer + compact/full footer + `ModeTabBar`; safe-area + keyboard env padding. |
| `web/src/shell/Drawer.tsx` | new | native-`<dialog>` off-canvas primitive (left/right/bottom); scroll-lock hook; focus restore; reduced-motion; `onClose(intent)` threads `'explicit'｜'swipe'`. ≤200 LOC. |
| `web/src/shell/useScrollLock.ts` | new | ref-counted `<html> overflow-hidden`. |
| `web/src/shell/useEdgeSwipe.ts` | new | non-passive capture-phase touch handler: edge-zone arm, 10px horizontal-lock (`\|dx\|>\|dy\|`), `preventDefault` claim, 40px vertical-abort, scroll-owner exclude list, 50%-or-fling commit. ≤200 LOC. |
| `web/src/shell/useSurfaceIntent.ts` | new | shell-level reducer for intent-aware restore (one-heavy-overlay state machine, `sidebarWasOpenBeforeOverlay`, explicit-vs-swipe restore). |
| `web/src/shell/ModeSwitcher.tsx` | new | header segmented control (real buttons, `aria-current`, disabled future modes). |
| `web/src/shell/ModeTabBar.tsx` | new | mobile thumb tab bar (shares mode model with ModeSwitcher). |
| `web/src/shell/RuntimeStatusChip.tsx` | new | worst-tone summary pill → opens runtime sheet/drawer. |
| `web/src/conversations/ConversationSidebar.tsx` | rewrite | recency groups, sticky headers, region header, states; delegate row + menu to sub-files to stay ≤600. |
| `web/src/conversations/ConversationRow.tsx` | new (extract) | premium row, container-query meta line, touch-visible `⋯`. |
| `web/src/conversations/RowActionMenu.tsx` | new | keyboard-operable overflow menu. |
| `web/src/conversations/recency.ts` | new | `groupByRecency()` pure helper (heavily unit-tested). |
| `web/src/components/skeleton/AppSkeletons.tsx` | edit | add `ConversationListSkeleton`; update `AppShellSkeleton` to the new grid (svh, drawer-aware) so the loading shell matches the real shell. |
| `web/src/i18n/resources.ts` | edit | new keys §8 (en+it). |
| `web/src/styles/index.css` (or theme additions) | edit | `--z-drawer*` vars; `--chat-lane-min: 380px` + `--window-floor: 700px`; `@container` names if not inline. No new colors. |
| `web/src/chat/Composer.tsx` | minor | remove the self-`border-t` (the dock owns the top border); otherwise kept. |
| `web/public/manifest.json` | new *(PWA §5b — optional/Phase-N)* | ~15-line install manifest; `theme_color`/`background_color` = `--color-bg`; maskable 192/512 icons. |
| `web/public/sw.js` | new *(PWA §5b — optional/Phase-N)* | ≤150 LOC; per-asset strategy; **`/api/*` + SSE never intercepted**; SWR for SPA root only; per-item precache; versioned cache. |

No backend, no `useConversations.ts`/`useRuntimeHealth.ts` data-layer change.

---

## 10. Acceptance criteria

Machine-checkable. **AC-MOBILE-1/2/3 are the regression guards for the live "shit"
verdict** and MUST be a Playwright mobile-viewport (`devices['Pixel 7']` ≈ 390px) check.

| # | Criterion | Verified by |
|---|---|---|
| **AC-MOBILE-1** | At 390×~750, the **composer input is visible and reachable** without scrolling the page: the composer's bounding box bottom ≤ viewport height and it receives focus on `click`. | Playwright mobile project: `composer.boundingBox().y + height <= viewport.height` and `expect(composer).toBeVisible()` then `.click()` focuses it. |
| **AC-MOBILE-2** | At 390px the **chat scroll region has usable height** — its measured height ≥ **45%** of viewport height (today ≈ 0). | Playwright: `chatRegion.boundingBox().height / viewport.height >= 0.45`. |
| **AC-MOBILE-3** | At 390px the **right runtime panel is NOT in flow** above the composer — `RuntimeHealthPanel` rows are not visible until the status chip is tapped; tapping opens a sheet that overlays (does not displace) the chat. | Playwright: health rows `toBeHidden()` initially; after chip click `toBeVisible()`; composer box unchanged after open. |
| **AC-MOBILE-4** | At 390px there is **no horizontal scroll** (1.4.10 reflow): `document.scrollingElement.scrollWidth <= clientWidth`. | Playwright assertion. |
| **AC-MOBILE-5** | The **chat lane never renders below 380px** (the floor, §1.1b). At any width where the side regions would reduce the lane below 380px, those regions are out of flow (drawer/sheet), not in the grid: at the `lg` breakpoint the chat track resolves ≥380px; just below `lg` the sidebar/panel are not in `<main>`. | Playwright: sweep widths around `lg`; assert `chatRegion.boundingBox().width >= 380` and side regions absent from `<main>` below `lg`. |
| **AC-NAV-1** | Hamburger opens the conversation drawer; focus moves into it (search input focused); `Esc` closes and restores focus to the hamburger. | Playwright + vitest (Drawer unit). |
| **AC-NAV-2** | Drawer open locks body scroll (`<html>` `overflow:hidden`); closing unlocks. | vitest (useScrollLock) + Playwright. |
| **AC-NAV-3** | Mode switcher renders **real buttons** with `aria-current="page"` on `chat`; future modes are `aria-disabled`, not plain text; no inert `<span>`. | vitest: `getByRole('button', {name:'Chat'})` has `aria-current`; tree/graph/etc. `aria-disabled`. |
| **AC-NAV-4** | **Touch swipe-to-open/close** (§3.1b): a horizontal swipe from the left edge opens the sidebar drawer; a horizontal swipe inside the open panel closes it; a **vertical** swipe over the chat/list scrolls and does **NOT** move the drawer (horizontal-lock); a touch starting inside `pre`/`table`/`textarea`/composer never arms the gesture (exclude list). | Playwright touch (`hasTouch`, `page.touchscreen`/dispatched `touch*`): assert drawer open after horizontal edge-drag; assert drawer position unchanged + scroll moved after a vertical drag over chat; assert no drawer move from a drag started in a `<pre>`. |
| **AC-NAV-5** | **Intent-aware restore** (§3.1c): with the sidebar open, opening the runtime sheet dismisses the sidebar; closing the sheet via its **close button / Esc / backdrop** restores the sidebar; **swipe-dismissing** the sheet leaves the sidebar collapsed. | vitest on the shell reducer (state-machine transitions) + Playwright (explicit-close restores; swipe-dismiss does not). |
| **AC-DESK-1** | At ≥1024px the static 3-column grid renders (sidebar + chat + runtime panel all visible, no drawers); chat column = elastic track. | vitest (matchMedia mock) + Playwright desktop. |
| **AC-SIDE-1** | Conversation list renders **recency group headers** (`Today`/`Yesterday`/…); empty buckets omitted; bucketing tested across boundaries (00:00 today, 23:59 yesterday, day-7/day-8, day-30/day-31). | vitest on `groupByRecency()` (table-driven, fixed clock). |
| **AC-SIDE-2** | Untitled conversations render a **non-bare** label (`Untitled · {{time}}`), never two identical "Untitled" rows. | vitest. |
| **AC-SIDE-3** | Row actions (`⋯` → Rename/Archive/Delete) are **reachable on touch** (always rendered below `lg`, not hover-gated) and keyboard-operable; delete still routes through the confirm dialog. | vitest. |
| **AC-SIDE-4** | Loading shows `ConversationListSkeleton` (`role=status`, `aria-busy`); error shows `role=alert` + Retry that refetches. | vitest. |
| **AC-A11Y-1** | `eslint-plugin-jsx-a11y` clean; no `outline:none` without replacement; new icon buttons carry `aria-label`, SVG `aria-hidden`. | lint gate + vitest axe-style assertions where practical. |
| **AC-A11Y-2** | Reduced-motion: drawers/skeletons have no transform/animation under `prefers-reduced-motion: reduce`. | vitest (matchMedia) / Playwright `colorScheme`/`reducedMotion: 'reduce'`. |
| **AC-I18N-1** | Every new string present in en+it; switching to it relabels hamburger/chip/groups; no hard-coded user text. | vitest (switch language, assert IT labels). |
| **AC-COV-1** | vitest coverage ≥85% (statements/branches/functions/lines — the configured gate); Stryker ≥70% killed on `recency.ts`, `Drawer.tsx`, `BottomDock.tsx`, `useEdgeSwipe.ts`, and the intent-restore reducer. | `npm test` gate + `npm run mutation`. |
| **AC-PWA-1** *(only if the PWA layer §5b ships this phase; skipped cleanly if de-scoped)* | App is installable (valid `manifest.json`, maskable icons, `theme_color` = `--color-bg`); the SW **never intercepts `/api/*` or the AG-UI SSE stream** (a live chat turn still streams with the SW active); the SPA root is SWR but deep links fall through. | Playwright: `manifest` validity + Lighthouse-PWA install check; assert a streamed turn completes with the SW registered; assert SSE request bypasses the SW (no `respondWith`). |

---

## 11. Test plan

**Unit (vitest + @testing-library/react + jsdom), per the shipped harness:**
- `recency.test.ts` — table-driven `groupByRecency()` with an injected fixed clock
  (`vi.setSystemTime`): boundary fixtures (now, −1m, −23h59m, −24h01m, −7d, −8d, −30d,
  −31d), tz stability, empty input, archived split. This is the highest-mutation-value
  file → Stryker target.
- `Drawer.test.tsx` — open mounts dialog + `showModal` called (jsdom stub), focus to
  first focusable, `Esc`/backdrop/close-button → `onClose` with the right `intent`
  (`'explicit'` for Esc/close/backdrop), focus restored, scroll-lock applied/released,
  reduced-motion class gating.
- `useEdgeSwipe.test.ts` — synthesised `touchstart/move/end` sequences: edge-zone arm vs
  ignore; the 10px threshold + `|dx|>|dy|` horizontal-lock decision (horizontal → claim +
  `preventDefault` called; vertical → never `preventDefault`); 40px vertical-abort of an
  in-progress drag; exclude-list (a touch from within a mocked `<pre>`/`<textarea>` never
  arms); 50%-or-fling commit. High mutation value → Stryker target.
- `useSurfaceIntent.test.ts` — reducer transitions: open-sidebar→open-overlay dismisses +
  remembers; explicit/backdrop close restores; swipe close does NOT restore + clears the
  flag; no-op at `lg`. High mutation value → Stryker target.
- `ConversationSidebar.test.tsx` — extend the existing test: group headers render,
  empty buckets omitted, untitled label non-bare, skeleton on pending, alert+Retry on
  error, `⋯` menu visible without hover (simulate `lg`-off), rename/archive/delete still
  fire the existing mutations, language switch relabels groups.
- `ConversationRow.test.tsx` — active `aria-current` + accent rule, container-query meta
  line presence (mock `ResizeObserver`/container width), touch-visible actions.
- `ModeSwitcher.test.tsx` / `ModeTabBar.test.tsx` — real buttons, `aria-current`,
  disabled future modes, `onMode` fired only for live mode.
- `BottomDock.test.tsx` — composer present, full-vs-compact footer by width, tab bar
  only below `lg`, safe-area class present.
- `AppShell.test.tsx` — extend: at `lg` (matchMedia true) all three regions in flow; at
  mobile the sidebar/runtime are NOT in `<main>` (rendered via drawer triggers); the
  existing conversation-binding + approval-badge + search-deep-link tests still pass
  against the relocated regions.

**E2E (Playwright) — add a `mobile` project to `playwright.config.ts`:**
```ts
projects: [
  { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  { name: 'mobile',   use: { ...devices['Pixel 7'] } },   // ≈ 390px width
]
```
- `e2e/shell-mobile.spec.ts` (project `mobile`) — AC-MOBILE-1..4: composer visible +
  reachable + focusable; chat region height ≥45% vh; runtime rows hidden until chip tap
  then overlay (composer box unchanged); no horizontal scroll. Uses the existing
  `gotoAuthenticated` helper + the golden-replay `page.route()` mocks (serviceWorkers
  already blocked in config).
- `e2e/shell.spec.ts` (project `chromium`) — extend: 3-column visible at desktop; brand
  + no-marketing assertions kept.
- `e2e/nav.spec.ts` — hamburger opens drawer, focus trapped, `Esc` closes (both
  projects). Reduced-motion run variant.
- `e2e/gestures-mobile.spec.ts` (project `mobile`, `hasTouch`) — AC-NAV-4/AC-NAV-5:
  horizontal edge-swipe opens the sidebar; horizontal swipe inside closes it; a vertical
  swipe over the chat scrolls without moving the drawer; a swipe started in a `<pre>` does
  nothing; opening the runtime sheet dismisses the sidebar, an explicit close restores it,
  a swipe-dismiss does not. Uses dispatched `touch*` events / `page.touchscreen`.

**Gates (parity with backend, per project memory):** vitest coverage ≥85% (configured
in `vitest.config.ts`), Stryker ≥70% killed on the new logic files, lint
(`eslint --max-warnings=0`) clean, `tsc --noEmit` clean, CI runs the `mobile` Playwright
project (web-e2e job). No skip-as-green: the mobile project runs in CI, not just locally.

---

## 12. Out of scope (do not pull forward)

- Chat thread / message / approval-card redesign (Phase 25 — only relocation here).
- Typed-display router (Phase 26); Graph explorer (Phase 27); governance/onboarding
  (Phase 28); MCP/skills UI (Phase 29).
- Odysseus operator-OS shell: dockable tool windows, dock chips, adaptive icon rail,
  command palette, slash actions, AI `ui_control` events (ux-spec Frame 07 — follow-up
  milestone). The mode switcher here is the minimal honest version, not the rail. (We adopt
  odysseus's *portable* mobile techniques — the 380px floor §1.1b, swipe gestures §3.1b,
  intent restore §3.1c, PWA §5b — but **NOT** its draggable/dockable/snappable window manager,
  which 06 §1.11 also marks SKIP; Aura is an agent cockpit, not a window manager.)
- A new font / new palette (03-spec owns tokens; this SPEC consumes them).
- VirtualKeyboard JS API geometry listeners — CSS `interactive-widget` +
  `keyboard-inset-height` is the contract; JS only if a future device gap forces it.

---

## 13. Citations (2026 industrial best practice)

- **svh over dvh for stable full-height; svh unaffected by virtual keyboard; pair with
  `env(safe-area-inset-bottom)`** — OpenReplay, "When 100vh Lies"
  (https://blog.openreplay.com/fix-100vh-mobile-viewport/); DEV "New CSS Viewport Units"
  (https://dev.to/web_dev-usman/the-new-css-viewport-units-that-finally-fix-mobile-layouts-2cjd);
  MDN `env()` (https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Values/env).
- **`interactive-widget=resizes-content` + `keyboard-inset-height`** — HTMHell advent
  2024 (https://www.htmhell.dev/adventcalendar/2024/4/); Ahmad Shadeed, VirtualKeyboard
  API (https://ishadeed.com/article/virtual-keyboard-api/); bram.us viewport-resize
  explainer (https://github.com/bramus/viewport-resize-behavior).
- **Container queries for self-contained components, media queries for page layout; CQ
  not a silver bullet (perf, partial style-query support)** — LogRocket "Container
  queries in 2026" (https://blog.logrocket.com/container-queries-2026/); caisy
  (https://caisy.io/blog/css-container-queries).
- **Native `<dialog showModal()>` gives focus-trap + Esc + top-layer + scroll behavior
  for free; do NOT hand-roll a JS trap or `aria-modal`; use `inert` only where dialog
  isn't** — Medium/Beisel "Stop Rebuilding Modals"
  (https://medium.com/@beiselanja/stop-rebuilding-modals-a-deep-dive-into-the-dialog-element-4580cdbb7b20);
  UXPin "Accessible Modals with Focus Traps (2026)"
  (https://www.uxpin.com/studio/blog/how-to-build-accessible-modals-with-focus-traps/);
  Jared Cunha (https://jaredcunha.com/blog/html-dialog-getting-accessibility-and-ux-right);
  OpenReplay scroll-lock (https://blog.openreplay.com/stop-page-scrolling-dialog-open/).
- **Collapsible/off-canvas conversation rail + chat owns the viewport on mobile (Claude.ai
  / ChatGPT pattern)** — IntuitionLabs "Conversational AI UI Comparison 2025"
  (https://intuitionlabs.ai/articles/conversational-ai-ui-comparison-2025).
- **Recency grouping (Today/Yesterday/Previous 7/30 days) is the expected conversation-
  list pattern; users ask for it back when removed** — OpenAI community thread
  (https://community.openai.com/t/please-bring-back-the-chronological-sidebar-grouping/1280889);
  Copilot date grouping (https://www.llmnesia.com/blog/how-to-find-old-microsoft-copilot-conversations).
- **Bottom tab bar = 2025 standard for 3–5 destinations, thumb zone; segmented control
  segment min target 24×24, ship 44×44 (WCAG 2.2 AA 2.5.8 = 24, AAA = 44)** — Medium
  "User-Friendly Mobile Navigation 2025"
  (https://medium.com/@secuodsoft/the-complete-guide-to-creating-user-friendly-mobile-navigation-in-2025-59c9dd620c1d);
  Primer SegmentedControl a11y (https://primer.style/product/components/segmented-control/accessibility/);
  W3C Mobile-A11y (https://w3c.github.io/Mobile-A11y-TF-Note/TouchProposal.html).
- **`min-height:0`/`min-width:0` to let a flex/grid child shrink-to-scroll instead of
  overflowing (the track-collapse fix)** — corollary of the grid min-content sizing
  documented across the viewport sources above; the shipped `minmax(0,1fr)` usage in
  `AppShell.tsx` already relies on the `0` lower bound for the chat column.
- **380px chat-lane floor + 700px window floor + auto-collapse (O1)** — odysseus
  `static/js/sidebar-layout.js:223-224` (`MIN_CHAT_WIDTH=380`, `AUTO_COLLAPSE_WIDTH=700`),
  the empirically-validated "a chat lane below this is unusable" thresholds; documented in
  06-candidates-eval-SPEC §1.4(b)/§1.10 O1/§5.2-item-1.
- **Touch swipe with horizontal-lock + `preventDefault` claim + scroll-owner exclude list
  (O2)** — odysseus `static/js/sidebar-layout.js:489-567` (open), `:322-345` (close),
  `:494-499` (exclude list), `:539-545` (10px arm + `|dx|>|dy|` lock + `preventDefault`),
  `:333-334` (40px vertical-abort); 06 §1.4(c)/§5.2-item-2. The `preventDefault`-on-
  horizontal-intent is the detail that claims the gesture from the browser back/overscroll on
  iOS/Firefox.
- **Intent-aware restore: panel gets out of the way, restores by intent (O3)** — odysseus
  `static/js/sidebar-layout.js:381-475` (`_sidebarWasOpenBeforeTool`, `modal-dismissed`
  event distinguishing swipe-dismiss from explicit close); 06 §1.4(d)/§5.2-item-3.
- **Modal → full-height bottom sheet on mobile, mobile-viewport hygiene kit (O4/O5)** —
  odysseus `static/style.css:4240-4262` (bottom-sheet `100dvh`, top-rounded, safe-area pad),
  `:108` (`dvh` keyboard adaptivity), `:377` (44px touch targets), `@media (hover:none)`
  touch rules; 06 §1.4(e)(f)/§5.2-item-4. Reconciled with this SPEC's `svh` outer shell:
  `svh` for stable shell height, `dvh`/keyboard-inset for the composer dock (06 §5.2-item-4).
- **PWA: 15-line manifest + ~145-LOC SW; never-cache `/api/`+SSE; SWR for SPA-root only;
  per-item precache; versioned cache (O6)** — odysseus `static/manifest.json`,
  `static/sw.js:1-143` (`:94-95` never-cache `/api/`+non-GET, `:97-113` root-only SWR,
  `:66-81` per-item `cache.put`); 06 §1.7/§5.2-item-5. The SSE-never-intercept rule is
  critical for the AG-UI `/agent/run` stream.

---

## Self-Scorecard

Rubric (target 9.5/10): concrete breakpoints + layout mechanics; exact component
structure; every state; a11y; correctness vs 2026 best practice (cited); fits Aura.

> **Revision (2026-06-17).** Closes the adversarial validator's three blocking gaps
> (00-VALIDATION §02, gate score was 8.5): added the **380px chat-lane floor + 700px window
> floor** as the content-driven reason behind the breakpoints (§1.1b, AC-MOBILE-5); a
> **touch-gesture layer** — edge-swipe open/close with the 10px horizontal-lock,
> `preventDefault`-on-horizontal-intent claim, 40px vertical-abort, and scroll-owner exclude
> list (§3.1b, AC-NAV-4); **intent-aware restore** — one-heavy-overlay-at-a-time with
> explicit-vs-swipe dismiss distinction (§3.1c, AC-NAV-5); and the **PWA layer** as an
> optional/Phase-N section with the load-bearing SSE-never-intercept rule (§5b, AC-PWA-1).
> The dvh-vs-svh question is made explicit (§2.4): `svh` for the stable shell, `dvh`/keyboard-
> inset for the composer dock — no conflict (06 §5.2-item-4). All token names stay 03's
> semantic names. The KEEP items (root-cause + corrected layout model) are untouched.

| Dimension | Score | Note |
|---|---|---|
| Root-cause precision | 10 | Track-collapse + document-order burial identified at file:line; 5 compounding defects; corrected model is deterministic. Untouched by this revision. |
| Responsive mechanics (breakpoints, grid/flex, container vs media) | 9.6 | Copy-pasteable `svh` grid, three-track model, `min-h-0`/`min-w-0`, drawer/sheet placement per tier, CQ scoped to two leaf components; **now anchored by the 380px content floor that *derives* the breakpoints** (§1.1b) + explicit svh-shell/dvh-dock reconciliation (§2.4). |
| Component structure | 9.5 | 21 file targets, all ≤600 LOC, reuse-vs-new called out, data layer untouched; new `useEdgeSwipe`/`useSurfaceIntent` hooks + optional PWA assets enumerated. |
| Every state (mobile/tablet/desktop, empty/loading/error) | 9.5 | Per-region table × breakpoint; sidebar 6 states incl. archived-empty + retry; premium skeleton specced. |
| Accessibility (WCAG 2.2) | 9.5 | Native-dialog trap, focus restore, target size, reflow@320, reduced-motion, live regions, mode-switcher honesty; touch gestures coexist with vertical scroll via horizontal-lock (no scroll hijack). |
| 2026 correctness + citations | 9.6 | svh/dvh, interactive-widget, CQ-vs-MQ, native dialog, bottom-tab, recency grouping cited to 2024–2026; **+ the four odysseus techniques (O1/O2/O3/O6) cited to `path:line` via 06**. |
| Fits Aura (tokens, i18n, test gates, GSD shape) | 9.5 | Token-pure (03 semantic names, incl. `--motion-*`/`--color-bg`), en+it keys listed, ≥85%/≥70% gates extended to the new hooks, Playwright mobile + gesture projects, reuses shipped Drawer/skeleton/mutation patterns. |
| Acceptance + test plan | 9.6 | 20 ACs incl. the three original mobile regression guards **+ AC-MOBILE-5 (380px floor), AC-NAV-4 (gestures), AC-NAV-5 (intent restore), AC-PWA-1 (conditional)**; new vitest + Playwright-touch coverage specified. |

**Overall: 9.5 / 10.** (min dimension = 9.5; the three validator blockers are resolved
in-spec, not deferred.)

**Carry into plan-phase (decisions, not gaps in this SPEC):**
1. **Sibling `03-design-system-SPEC.md` is the token anchor** (PASS per 00-VALIDATION). This
   SPEC consumes its semantic names (`--color-*`, `--radius-*`, `--font-*`, `--motion-*`,
   `--shadow-*`); 03 must land first so "Editorial graphite" naming is authoritative.
2. **Runtime bottom-sheet content split (Health vs Metrics).** Spec leaves "one tabbed
   sheet vs two sheets" to the planner (both satisfy the contract that neither sits above
   the composer); pick one in `/gsd-plan-phase`.
3. **`AssistantRuntimeProvider` hoist.** Lifting the provider to wrap main+dock so the
   relocated composer keeps its runtime binding is a "wiring move"; the plan must verify
   assistant-ui allows the composer to live in a sibling subtree of the thread under one
   provider (context-based — confirm against the pinned `@assistant-ui/react` 0.14.22 before
   execution — one spike check; same as 01 blocker).
4. **PWA scope decision (§5b).** Decide in `/gsd-plan-phase` whether the PWA layer ships this
   phase or lands behind a later `AURA_WEBUI_PWA` flag; §§1–5 do not depend on it. If shipped,
   AC-PWA-1 applies and the SSE-never-intercept rule is mandatory.
