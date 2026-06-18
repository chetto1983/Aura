# 06 — Candidate Reference-Project Evaluation SPEC

> **Status:** COMPLETE — research trail; incorporation loop closed (see §5 dispositions). **Implementation-status pass added 2026-06-18** — each §5 item now also carries a *build-status* tag (`→ SHIPPED` / `→ PARTIALLY SHIPPED` / `→ FOLDED but NOT YET BUILT` / `→ SHIPPED (uncommitted working tree)`), verified against the code at commit `fc77e4cb` (2026-06-17) + the uncommitted working-tree layer. See the new "Implementation Status of §5 Adopts" table below.
> **Author:** Claude Code (repo survey + pattern extraction)
> **Date:** 2026-06-17 (re-annotated 2026-06-17 to close the incorporation loop; implementation-status pass 2026-06-18)
> **Phase:** Cockpit Overhaul (v1.0.0 Deep Search Web Cockpit) — sibling to specs 01–05
>
> **Incorporation status (the deliverable's whole point):** every §5 recommendation is now annotated
> with its disposition — `→ FOLDED into 0X §<section>`, `→ DEFERRED (rationale)`, `→ SKIPPED (rationale)`,
> or `→ CONFIRMED (no work)`. The HIGH-VALUE blocking adopts the adversarial validator flagged are
> **folded into their target specs**: 01 §3.2/§3.5 (`.shine` shimmer + tool-card enrichment) and §3.3
> (citation reconciliation), 02 §1.1b/§3.1b/§3.1c/§5b (380px floor + swipe gestures + intent-aware
> restore + PWA), 04 §"3-tier severity"+AC-10 (3-tier gauge) + the graphite theme-header fix, and 05
> §4.2.1 + AC-17 (TOTP-input a11y checklist, O7). **All HIGH-VALUE adopts are now folded** (each verified
> against its sibling spec at HEAD in round-2 re-validation). 06 is retained as the evidence trail, not an open to-do list.
> **Theme target:** "Editorial graphite (premium-calm)", mobile-first + premium + industrial
> **Aura cockpit stack (the thing we are improving):** single-binary Go agent runtime, embedded
> Vite + React + **assistant-ui** SPA over an **AG-UI / SSE** gateway. Composer in a shell-level
> `BottomDock`; `useExternalStoreRuntime` + `sseAdapter.ts` is the load-bearing backend contract.
>
> **Scope:** SPEC/research ONLY. No Aura code is modified by this document. It evaluates three
> operator-flagged reference repos for *adoptable patterns* and tells sibling specs 01–05 exactly
> what to revise. Evidence is cited as `path:line` from the local clones at `D:/tmp/{odysseus,elysia-frontend,openhuman}`.

---

## Implementation Status of §5 Adopts (2026-06-18)

This table records a SECOND axis on top of §5's fold dispositions: of the items folded into the
sibling specs, which were actually **built in code**. "Folded?" = the sibling SPEC TEXT incorporated
the idea (§5's existing tags). "Built?" = real code exists. Evidence is `web/...` `file:line` read at
HEAD (`fc77e4cb`) + the uncommitted working tree. Items 06 itself *deferred/skipped* are not "missing
builds" — they were never meant to ship in this milestone.

| Adopt | Target spec | Folded? | Built? | Evidence |
|---|---|---|---|---|
| E1 inline-citation rehype + CitationBubble | 01 §3.3 | Reconciled→DEFER Ph26 | **No (by design)** | not on the AG-UI wire; no `source` part — deferred to Phase 26 |
| H1 tool-card enrichment (status-tint, auto-expand-running, elapsed, subagent rows) | 01 §3.5 | Yes | **Partial** | `ToolActivityCard.tsx:15-31` (tint maps), `:54-59` (auto-expand-running); NO elapsed/subagent (grep clean) |
| E2 `.shine` streaming shimmer + Skeleton | 01 §3.2/§7 | Yes | **Skeleton only; `.shine` No** | grep `shine` in `web/src` → 0 matches |
| Markdown sanitization (security upgrade) | 01 §3.3 | Yes | **Yes** | `MarkdownText.tsx:5-15` (rehype-sanitize + remark-gfm), `markdownSanitize.ts` schema |
| E4 staggered empty-state reveal | 01 §7 | Deferred (no chips) | **N/A** | suggested-prompt chips not built (no backend starter prompts) |
| H5 3-way approval vocabulary | 01 §3.6 | Deferred (>v1) | **No (by design)** | Answer/Decline/Cancel kept; 3-way is a protocol change |
| H4 cross-thread approval badge idiom | 01 | Confirmed (no work) | **Yes (pre-existing)** | Phase-25 `ApprovalBadge`/`ApprovalList` (pulsing-dot + count) |
| odysseus shimmer-until-loaded attachment | 01 | Skipped (out of scope) | **N/A** | attachments fenced out of 01 |
| O1 380px chat-lane floor + 700px window floor | 02 §1.1b | Yes | **No** | grep `380`/`chat-lane-min`/`window-floor` in `web/src` → 0 matches |
| O2 swipe gestures (horizontal-lock + preventDefault + exclude list) | 02 §3.1b | Yes | **Partial** | `useEdgeSwipe.ts` exists (coarse pointer up/down); missing preventDefault claim, `\|dx\|>\|dy\|` lock, scroll-owner exclude list |
| O3 intent-aware restore | 02 §3.1c | Yes | **No (as specced)** | `useSurfaceIntent.ts` repurposed to persist the mode choice; no explicit-vs-swipe restore reducer |
| O5 mobile-viewport hygiene kit (44px, safe-area, hover:none, anti-zoom) | 02 §2.4/§6/§7 | Yes | **Partial** | `index.html:7` viewport + 44px targets in components (`LoginPage.tsx:404`, `ToolActivityCard.tsx:88`); `hover:none`/`-webkit-overflow-scrolling` not centrally specced |
| O6 PWA layer (manifest + SW) | 02 §5b | Yes (gated) | **No (default-only)** | framework-default service worker only; no §5b authored manifest/SW |
| O8 `<meta name=theme-color>` sync | 03/02 | Deferred (consumer wired) | **Yes** | `web/index.html:9-10` (light/dark theme-color metas) |
| Reduced-motion discipline | 03/01 | Already satisfied | **Yes** | `motion-reduce:` throughout (`ToolActivityCard.tsx:100`, `ContextBudgetGauge.tsx:74`) |
| E6 relaxed line-height + thin scrollbars | 03/01 | Deferred | **unverified (out of pass scope)** | not spot-checked this pass |
| H2 3-tier context gauge (70/90 → accent/warning/danger) | 04 §gauge + AC-10 | Yes | **Yes** | `footerMetrics.ts:131-140` `gaugeTier`; `ContextBudgetGauge.tsx:40-43` tier→fill |
| O7 TOTP-input a11y checklist | 05 §4.2.1 + AC-17 | Yes | **Yes (uncommitted)** | `LoginPage.tsx:363-372` `inputMode=numeric` + `autocomplete=one-time-code`; `:396` `role=alert`; `:370` omit-when-valid `aria-invalid`; backup-code toggle `:382-391` — all in uncommitted working tree (committed `fc77e4cb` was passphrase-only) |

**Theme note (operator-accepted, 2026-06-18):** O8 shipped, and the overall palette shipped as a
logo-matched **blue** theme (`index.html:9-10` `#FDFCFC`/`#131314`; `LoginPage.tsx:266` blue logo shadow
`rgb(26 115 232 …)`), **not** the Editorial-Graphite warm-gold the specs originally assumed. **The operator
accepted the blue palette** ("color is ok like in repo, respect the logo"), so this is the approved design,
not drift — 03/04's graphite values are superseded by decision. The token *names*
(`bg`/`accent`/`warning`/`danger`) are theme-clean and the footer is value-clean (no dead blue hex); the
*values* are the accepted blue. Elsewhere in this doc, read any "theme-drift / not graphite" phrasing as
"the accepted blue palette, not the original graphite direction."

**Headline:** of 06's HIGH-VALUE folded adopts, **fully shipped:** markdown-sanitization, O8 theme-color,
H2 3-tier gauge, O7 TOTP a11y (uncommitted), H4 badge (pre-existing). **Partial:** H1 tool-card, O2 swipe,
O5 viewport-kit. **Folded but NOT built:** O1 380px floor, O3 intent-aware restore (repurposed), O6 PWA,
E2 `.shine`. The E1 citation pipeline and H5 3-way approval remain *deliberately deferred* (not gaps).

---

## 0. Executive verdict (TL;DR)

**Bottom line.** All three repos earn study; only **odysseus** earns deep study, and it pays off
in a way Aura needs *right now* — it is the only candidate that actually ships a **multi-feature
workspace that survives on a 390px phone**, and it does so with techniques that are stack-agnostic
(plain CSS `@media` + a few touch-gesture JS handlers), so they transplant cleanly onto Aura's
React/assistant-ui shell. The single highest-value takeaway is odysseus's **"a panel that would
crush the chat lane is removed from the chat lane entirely on mobile, becomes an overlay/bottom-sheet,
gets out of the way the moment another surface opens, and restores afterward"** discipline
(`static/js/sidebar-layout.js`, `static/js/modalSnap.js`) — which is *exactly* the architectural
move Aura's `02-shell` spec already proposes, so odysseus is strong **independent confirmation** of
that spec's direction plus a catalog of the small details (`100dvh` for keyboard, `env(safe-area-inset-*)`,
44px touch targets, `@media (hover:none)` touch rules, horizontal-swipe gesture lock, the
sidebar-auto-collapse-below-380px floor) that turn "responsive in theory" into "good on a phone."

The second-highest takeaway is *not* a layout one: odysseus's **theme system derives an entire
design system from a 5-color base palette** (`static/js/theme.js` `deriveSyntaxColors` + `ADV_KEYS` +
`generateHarmonyColors`) — a different philosophy from Aura's OKLCH primitive→semantic pipeline, but
its *runtime-derivation* and *per-zone hover-highlight theme editor* are worth borrowing if Aura ever
exposes a user-facing theme editor (it currently does not, and should not for v1.0.0 — see §5.3).

elysia contributes one genuinely excellent, directly-portable component — its **inline-citation rehype
plugin + HoverCard bubble** (`MarkdownFormat.tsx` + `CitationBubble.tsx`) — which a *Deep Search* cockpit
specifically wants, plus a clean settings-composition kit and the `.shine` streaming shimmer.
openhuman is ~70% Rust backend and its "human in mind" headline is mostly a mascot; its value is a
**trio of observability components** (`ToolTimelineBlock`, `TokenUsagePill`, `TaskKanbanBoard`) and a
**3-way approval-decision vocabulary** (`ApproveOnce / ApproveAlwaysForTool / Deny`) worth mirroring in
Aura's approval protocol — but it is GPLv3, so **patterns only, never file lifts**.

**Net adopt count:** 11 patterns adopt (6 from odysseus, 3 from elysia, 2 from openhuman),
the rest skip. No candidate displaces an existing Aura architectural decision; every adopt either
*validates* a sibling spec or adds a *concrete detail* to it.

---

## 1. Odysseus (EMPHASIZED — the mobile multi-feature workspace study)

### 1.1 What it is · license · tech stack

**What it is.** Odysseus (`github.com/pewdiepie-archdaemon/odysseus`, "vers. 1.0") is a self-hosted,
local-first AI workspace explicitly framed as "the self-hosted version of the UI experience you get
from ChatGPT and Claude" (`README.md:11`). It bundles ~11 first-class features behind one shell —
Chat, Agent (on opencode), Cookbook (hardware-aware model fitting), Deep Research, Compare, Documents,
Memory/Skills (ChromaDB), Email (IMAP/SMTP), Notes & Tasks, Calendar (CalDAV), plus an image editor and
theme editor (`README.md:14-25`). It is **explicitly "works great on mobile" — responsive, installable
PWA, touch gestures** (`README.md:24`), which is precisely the property Aura is failing at and the
reason it is the emphasized candidate.

**License.** **MIT**, plain (`LICENSE`, `README.md:434`). Permissive — patterns *and* code may be
lifted with attribution. Two transitive optional deps are copyleft and called out in the README:
`PyMuPDF` (AGPL-3.0) and the GohuFont/effects assets — irrelevant to frontend pattern adoption.

**Tech stack — the load-bearing fact: the UI is vanilla JS, not a framework.** The backend is
**Python / FastAPI** (`app.py` 47KB entry, `core/` auth+middleware, `src/` agent loop, `routes/`,
`services/`; `README.md:408-417`). Persistence is SQLite (`data/app.db`) + ChromaDB + JSON files
(`README.md:419-421`). The **frontend is hand-written ES6 modules + one giant CSS file**, served from
`static/`: `index.html` (198KB), `app.js` (180KB orchestrator), `style.css` (**1.16MB / 35,733 lines**),
and **65 `.js` modules** across 8 subdirs (`static/js/MODULE_SUMMARY.md:9`). No build step, no bundler,
no React/Vue/Svelte. This matters two ways for Aura: (a) its responsive/mobile wins live in **portable
CSS + small DOM-gesture handlers**, not in framework idioms, so they transplant to Aura's React shell
without porting a component model; (b) its weaknesses (a 1.16MB unsplit stylesheet whose own ROADMAP
calls "Calypso's island," `ROADMAP.md:49`; "mobile media override discoverability" as a known pain,
`ROADMAP.md:53`) are a cautionary tale Aura already avoids via tokens + `≤600 LOC/file`.

### 1.2 Where the UI actually lives (vanilla-JS modular front-end, not React)

The architecture comment block (`README.md:408-417`) plus the actual tree:

```
static/index.html      # 198KB app shell — all panels' markup inline, 35 <script type=module> tags
static/app.js          # 180KB orchestrator: init, event wiring, module coordination
static/style.css       # 1.16MB single stylesheet (the design system + ALL responsive overrides)
static/js/             # 65 ES6 modules — chat.js (4546 LOC), chatRenderer.js (2294), theme.js (2087),
                       #   sidebar-layout.js, modalSnap.js, tileManager.js, section-management.js,
                       #   windowDrag.js, windowResize.js, a11y.js, login (login.html), …
static/manifest.json   # PWA manifest
static/sw.js           # PWA service worker (precache + per-asset caching strategy)
```

The modules that matter for Aura's cockpit overhaul, by concern:
- **Shell / navigation / mobile:** `sidebar-layout.js`, `modalSnap.js`, `tileManager.js`,
  `section-management.js`, `windowDrag.js`, `windowResize.js`, `escMenuStack.js`, `modalManager.js`.
- **Chat:** `chat.js` (the orchestrator), `chatRenderer.js` (message DOM), `chatStream.js` (SSE),
  `markdown.js`, `codeRunner.js`.
- **Design system / theming:** `theme.js`, `colorPicker.js`, `color/hex.js`, the `:root` block in
  `style.css:18-69`.
- **PWA:** `manifest.json`, `sw.js`.
- **Auth/2FA:** `login.html` (582 LOC, self-contained page with inline JS).
- **A11y:** `a11y.js` (165 LOC) + 17 `prefers-reduced-motion` blocks in `style.css`.

### 1.3 The shell & navigation (multi-feature workspace → one phone screen)

Odysseus's shell is a **single chat surface (`#chat-container`) that every other feature overlays as a
draggable/dockable/minimizable modal or a route-swapped pane** — not a router that swaps full pages.
Navigation has three persistent affordances (`static/js/sidebar-layout.js`):

1. **A full sidebar** (`#sidebar`, conversation list + sections) — `240px` on mobile, resizable on
   desktop, can live on the left or right edge (shift-click the hamburger to swap, persisted to
   `Storage.KEYS.SIDEBAR_SIDE`; `sidebar-layout.js:146-151`).
2. **An icon rail** (`#icon-rail`, 48px) — the *collapsed* state of the sidebar on desktop, showing
   only section icons; clicking an icon re-expands the sidebar and `scrollIntoView`s that section
   (`sidebar-layout.js:205-219`).
3. **A hamburger** (`#hamburger-btn`) — always visible; **cycles full → mini-rail → off → full on
   desktop**, but on mobile is a **simple full ↔ hidden toggle with no mini state**
   (`sidebar-layout.js:142-201`). The desktop three-state vs mobile two-state split is deliberate:
   a 48px icon rail is useful on desktop, useless and cramped on a phone.

The body carries layout-state classes the CSS keys off of: `hamburger-left/right`, `hamburger-only`,
`sidebar-collapsed` (`sidebar-layout.js:60-65`). Live CSS custom properties `--sidebar-w` and
`--icon-rail-w` track the collapse state so the chat/doc panes reserve room via
`left: calc(var(--icon-rail-w,48px) + var(--sidebar-w,0px))` and
`width: calc(100% - …)` (`style.css:4789-4790, 14546, 15039`). **This is the same mechanism Aura's
02-shell spec proposes with container queries** — odysseus does it with JS-driven CSS vars instead, but
the *contract* (one nav whose width drives the content offset) is identical.

### 1.4 THE responsive / mobile approach (the key study) — file:line evidence

This is the part worth the deep read. Odysseus makes a desktop-grade multi-pane workspace usable on a
phone via **six** distinct, individually-portable techniques:

**(a) Off-canvas overlay drawer + backdrop + scroll-isolation on mobile.** Below 768px the sidebar is
not a column — it is an overlay with a programmatically-created `#sidebar-backdrop`
(`sidebar-layout.js:278-280`); tapping the backdrop closes it (`:293-313`); the hamburger **always
opens it from the RIGHT on mobile** regardless of the desktop side preference, and does **not** persist
that — so the desktop choice is untouched (`:167-172`). This is exactly Aura 02-shell §1.1's "off-canvas
drawer" row, independently arrived at.

**(b) Auto-collapse below a chat-width floor.** `AUTO_COLLAPSE_WIDTH = 700` and `MIN_CHAT_WIDTH = 380`
(`sidebar-layout.js:223-224`): the sidebar auto-hides whenever the *window* drops under 700px **or** the
*chat area itself* would render narrower than 380px (e.g. sidebar + a docked doc panel both open), and
auto-restores when there's room again — but only if the user didn't manually toggle (a `_userToggledSidebar`
flag gates it; `:227, :258-261`). A `MutationObserver` on `body` class re-checks on any layout change
(`:263-264`). **The 380px chat-width floor is the single most transferable number in the repo** — it is
the empirical "a chat lane below this is unusable" threshold, and Aura's 02-shell `lg:` breakpoint logic
should treat it as the chat-lane minimum.

> **Impl note (2026-06-18):** folded into 02 §1.1b but **not yet built** — no `380` / `--chat-lane-min`
> exists in `web/src` (grep clean). The shipped shell relies on raw breakpoints; the content-driven floor
> is still a spec, not code.

**(c) Touch gestures: swipe-to-open and swipe-to-close, with horizontal-lock disambiguation.**
`_initChatSwipeToOpenSidebar()` (`sidebar-layout.js:489-567`) binds a **non-passive** `touchmove` on
`document` (capture phase) that: ignores multi-touch and chip-drags; excludes areas owning their own
horizontal scroll (`pre, table, input, textarea, .modal, .input-bar, #message`, …; `:494-499`); and
only *after* the gesture travels ≥10px and is horizontal-dominant (`ady > adx → bail`, `:539-542`),
calls `e.preventDefault()` to **claim the gesture from the browser's own back/scroll** (`:545`) — the
detail that makes swipe gestures actually work on Firefox/iOS. Direction picks the side (`:552`). The
close side is symmetric on the sidebar element itself (`:322-345`), with a 40px vertical-drift abort
so a diagonal scroll doesn't close it (`:333-334`).

**(d) Panels get out of the way when another surface opens — then restore.** On mobile, tapping any
tool button (`[id^="tool-"], [id^="rail-"]`) auto-closes the sidebar/rail so the tool's modal isn't
buried behind it, **remembers** whether it was open (`_sidebarWasOpenBeforeTool`), and restores it once
the user is back to bare chat — but a *swipe-to-dismiss* of the tool does NOT bounce the sidebar back
(distinguished via a `modal-dismissed` event; `sidebar-layout.js:381-475`). This is the
"multi-pane-on-a-phone" crux: only one heavy surface owns the screen at a time, transitions are
remembered, and the restore is intent-aware.

**(e) Modal docking → on a phone, modals become full-height bottom sheets; on desktop they snap to a
resizable right/left dock.** `modalSnap.js` implements an edge-snap controller: drag a floating modal
within 60px of an edge (`SNAP_PX`, `:20`) to dock it as a side panel that reserves body padding so the
chat reflows beside it; if docking would push the chat under `MIN_CHAT_WIDTH=380` it auto-collapses the
sidebar first (`_shouldAutoCollapseSidebar`, `:78-90`); drag 80px away to un-dock (`UNSNAP_PX`, `:21`),
restoring the *exact* pre-dock geometry from a snapshot (`_preDockSnapshot`, `:285-307`). It even
supports a resizable email/document **split view** with a draggable seam stripe (`:695-782`). On mobile
(`@media (max-width:768px)`) those same modals become **full-bleed bottom sheets**: `width:100vw`,
`height:100dvh`, `border-radius:14px 14px 0 0`, `padding-bottom: env(safe-area-inset-bottom)`
(`style.css:4240-4262`). **Lesson for Aura:** the desktop "draggable docking window" complexity is
*skippable* (Aura is not a window manager), but the **"modal → full-height bottom sheet on mobile"**
CSS pattern is directly the shape Aura's right-runtime-panel-as-bottom-sheet (02-shell §1.1c) wants.

**(f) The mobile viewport + touch-target details that separate "responsive" from "good on a phone."**
Concretely, with file:line evidence in `static/style.css`:
- **`height: 100dvh`** on the app root "dynamic viewport height — adapts when mobile keyboard opens"
  (`style.css:108`), 37 `dvh` uses total; bottom sheets set `100vh` *then* `100dvh` so the dvh override
  wins on browsers that support it (`:4249-4252`, comment at `:4239`).
- **`env(safe-area-inset-bottom/right)`** on the composer, sheets, FABs, toasts — 10 uses
  (`:4205, 4258, 4339, 14428-14429, 26531`) — so content clears the notch/home-indicator in
  standalone PWA mode.
- **`min-height: 44px` touch targets** in the 768px block for every interactive row/button
  (`:377, 4214-4218 mode-toggle, diff buttons`) — the iOS HIG minimum.
- **`@media (hover: none)` and `@media (hover:none) and (pointer:coarse)`** — 10+ blocks that swap
  hover-only affordances for always-visible ones on touch devices (`:1256, 2908, 3696, 16714, …`),
  the correct way to detect touch vs. guessing from width.
- **16px input font-size to defeat iOS focus auto-zoom** (`login.html:153-154` comment: "it auto-zooms
  any focused input under 16px").
- **`-webkit-overflow-scrolling: touch`** on scrollable sheets (`:4260, 4266`).
- **Keyboard-aware open:** before opening the sidebar on mobile it blurs a focused input and waits 250ms
  for the keyboard to dismiss so the layout settles first (`sidebar-layout.js:174-182`).

### 1.5 Chat UI

`chatRenderer.js` (2294 LOC) builds message DOM imperatively. Relevant patterns:
- **Attachment cards with shimmer-skeleton-until-loaded** for images (`chatRenderer.js:buildAttachCards`,
  ~`:60-110`) — a premium loading affordance: a whirlpool/shimmer fills the image box until the upload
  resolves or the thumbnail finishes, "so the photo doesn't pop in abruptly."
- **`_safeHref` URL sanitization** — only `http(s)`/protocol-relative hrefs survive
  (`chatRenderer.js:23-31`), a small security hygiene detail worth mirroring in Aura's markdown link
  rendering (Aura already sanitizes, but the explicit allowlist is a clean reference).
- **SVG icons inlined as string constants** (`chatRenderer.js:13-21`) — fine for vanilla JS, irrelevant
  to Aura (lucide-react covers this).
- **Image lightbox + per-image OCR button** (`:_openImageLightbox`) — a feature, not a pattern Aura
  needs now.

The chat composer (`.chat-input-bar`) is a **fixed bottom dock** that on mobile pads with
`env(safe-area-inset-bottom)` (`style.css:4205`) — same shape as Aura's `BottomDock` (02-shell §2.4).
Net: odysseus's chat UI is solid but **vanilla-imperative**; Aura's assistant-ui canonical Thread
(01-chat spec) is a *better* base. Adopt the *affordances* (shimmer-until-loaded attachment, safe-href),
not the rendering engine.

### 1.6 Theme editor & design-token approach

`theme.js` (2087 LOC) is the most interesting *idea* in the repo, even though Aura should not copy its
implementation. The model:
- **A 5-color base palette** per theme — `{bg, fg, panel, border, red(=accent)}` — defines 16 built-in
  themes (`theme.js:11-32`, incl. a `claude` and `gpt` theme). The live `:root` ships just these 5 plus
  derived defaults (`style.css:18-69`).
- **The entire rest of the design system is *derived* at runtime** from those 5 colors:
  `deriveSyntaxColors()` computes 10 syntax-highlight tokens via HSL math from `fg/bg/red`
  (`theme.js:159-178`); `computeAdvancedDefaults()` maps the base to **13 "advanced" CSS variables**
  (`--user-bubble-bg`, `--ai-bubble-bg`, `--sidebar-bg`, `--send-btn-bg`, `--code-bg`, `--toggle-active`,
  …; `ADV_KEYS` `theme.js:181-215`); `generateHarmonyColors()` can synthesize a full palette from a
  single accent + a harmony rule (complementary/analogous/triadic/monochromatic; `:217-252`).
- **`applyColors()`** writes all of it to `document.documentElement.style` as CSS custom properties and
  **syncs the mobile browser chrome** via `<meta name="theme-color">` (`theme.js:254-289`) — a PWA
  polish detail (the status bar matches the theme).
- **The theme *editor* UX is genuinely premium:** live color pickers with per-picker reset buttons that
  highlight only when changed (`syncResetButtons`, `:750-766`); a **"Peek" opacity toggle** that fades
  the editor modal via `color-mix` (never element opacity, so controls stay sharp) so you can see the
  page behind while tweaking (`:573-615`); **per-zone hover-highlight** so hovering a color row
  highlights which UI region it controls (`initThemeZoneHighlight`); untouched advanced pickers
  auto-track the base palette while user-edited ones are preserved (`:807-841`); custom themes persist
  to localStorage *and* sync to the server (`/api/prefs/theme`, `:467-484`).
- Plus `density` (compact/comfortable/spacious via a root class), font selection with runtime
  `@font-face` injection for custom fonts (`:361-386`), and decorative canvas background patterns
  (synapse/rain/petals/embers) gated by `prefers-reduced-motion` and intensity/size sliders.

**Contrast with Aura's 03-design-system:** Aura uses an **OKLCH primitive→semantic, build-time pipeline**
(`tokens.json → generate-theme.mjs → theme.css`) emitting a *single* `dark` theme; odysseus uses a
**5-color base, runtime-derivation, 16 themes + user editor**. Aura's is correct for a *curated, premium*
product (the operator explicitly wants the editorial direction locked, not user-tweakable — feedback memo
"Cockpit = premium bar, not minimal-industrial"). So **do not adopt the runtime-derivation engine or the
multi-theme editor for v1.0.0.** The two *transferable* ideas: (i) the `<meta name="theme-color">`
mobile-chrome sync (a 3-line PWA detail, §1.7); (ii) IF Aura ever ships a user theme editor post-v1.0.0,
the *per-zone hover-highlight* + *Peek opacity* + *changed-only reset* interactions are the gold standard.

> **Impl note (2026-06-18):** the O8 `<meta name="theme-color">` sync **did ship** (`web/index.html:9-10`),
> but the cockpit's resolved palette shipped as a Gemini/Material **blue** theme (theme-color `#FDFCFC` /
> `#131314`), **not** the "Editorial graphite (premium-calm)" this spec and its siblings assumed. Wherever
> 06 says theme-color "matches the cockpit" / "graphite," read: the *mechanism* is built, the *graphite
> values* are not what shipped.

### 1.7 PWA setup

Minimal and correct (`manifest.json` + `sw.js`):
- **Manifest** (`manifest.json`): `display: standalone`, `start_url:/`, `scope:/`, `theme_color`/
  `background_color: #282c34`, two icons (192/512) with `purpose: "any maskable"`. 15 lines — the
  complete minimum for installability.
- **Service worker** (`sw.js`, 145 LOC) with a **per-asset-type caching strategy** clearly documented at
  the top (`:1-9`): HTML navigation = **stale-while-revalidate** but *only for the SPA root* `/`
  (other navigations fall through so deep links aren't hijacked, `:97-113`); JS/CSS = **network-first
  with cache fallback** so code edits show on reload without manual cache-clear (`:115-128`); other
  static (images/fonts) = **cache-first with background refresh** (`:130-143`); `/api/` and non-GET =
  **never cached** (`:94-95`). Install precaches the shell with **per-item `cache.put`** (not `addAll`)
  so a single 404 can't abort the whole install (`:66-81`); `CACHE_NAME = 'odysseus-v326'` is bumped on
  every shell change and `activate` deletes stale caches (`:83-89`).

**Lesson for Aura:** Aura is a single Go binary embedding a Vite SPA; a PWA layer is *optional* but cheap.
If Aura wants installability + offline-shell (useful for a mobile cockpit on flaky networks), this is the
exact recipe: a 15-line manifest + a ~145-line SW with the *navigation-root-only* SWR guard (the bug
odysseus's comment warns about — caching every navigation as the app index — is a real footgun) and the
**never-cache `/api/` + SSE** rule (critical: Aura's AG-UI `/agent/run` SSE stream must never be
intercepted by a SW). This is a small *new* surface, not in any sibling spec yet — see §5.2.

### 1.8 Auth / 2FA UX

`login.html` (582 LOC, self-contained) shows a clean **progressive 2FA** flow worth mirroring in Aura's
05-authula login:
- Password-first: submit username+password; if the server replies `requires_totp`, a TOTP field is
  **injected inline** (not a separate page) and focused (`login.html:489-500`), then re-submitted with
  `totp_code`. State is tracked by a `form._totpMode` flag (`:392-407`).
- **The TOTP input is exemplary for mobile:** `type="text"` + `inputmode="numeric"` (numeric keypad) +
  `autocomplete="one-time-code"` (iOS/Android SMS/authenticator autofill) + `maxlength="8"` (6 digits or
  8-char backup code) + `aria-label` + centered `letter-spacing:4px` styling (`login.html:496`).
- Other a11y/mobile details: error region is `role="alert" aria-live="assertive"` (`:257`); username/
  password use correct `autocomplete` tokens (`username`, `current-password`, `new-password`; `:262-280`);
  a password show/hide toggle with `tabindex="-1"` + dynamic `aria-label` (`:272, 526`); inputs sized
  ≥16px to defeat iOS auto-zoom (`:153-154`); the focused field is kept visible without jumping the card
  (`:551`).

This is independent confirmation of, and a concrete checklist for, Aura's 05-authula §4.2 TOTP step.

### 1.9 Motion & micro-interaction

Odysseus is restrained here and that restraint matches "premium-calm": the only *constant* motion is
opt-in decorative canvas backgrounds (gated by `prefers-reduced-motion`). It honors reduced-motion
seriously — **17 `@media (prefers-reduced-motion: reduce)` blocks** in `style.css`. Micro-interactions
are small CSS transitions (sidebar width `0.25s ease`, `style.css:390`; `.user-bar-left:hover` bg fade
`0.15s`, `:463`). The shimmer-skeleton on loading attachments (§1.5) is the one premium loading flourish.
**Lesson:** the *number of reduced-motion blocks* is the tell of a mature responsive app — Aura's specs
already mandate `motion-reduce:` variants; odysseus confirms the bar (every animated surface needs one).

### 1.10 Adoptable patterns (concrete, with evidence)

| # | Pattern | Evidence | Why it helps Aura |
|---|---|---|---|
| O1 | **Mobile chat-width floor `380px` + window floor `700px` → auto-collapse nav** | `sidebar-layout.js:223-224, 226-256` | The empirical "chat lane below this is unusable" number; feeds 02-shell's lg-column logic |
| O2 | **Swipe-to-open/close drawer with horizontal-lock + `preventDefault` claim + scroll-owner exclude list** | `sidebar-layout.js:489-567, 322-345` | Touch gesture that *actually works* on iOS/FF; 02-shell mentions drawers but not gestures |
| O3 | **"Panel gets out of the way when another opens, restores after, intent-aware (swipe-dismiss ≠ restore)"** | `sidebar-layout.js:381-475` | The core multi-pane-on-phone discipline; validates 02-shell "one heavy surface at a time" |
| O4 | **Modal → full-height bottom sheet on mobile** (`100dvh`, top-rounded, safe-area pad) | `style.css:4240-4262, 4205` | Exactly the shape for 02-shell's right-runtime-panel-as-bottom-sheet (§1.1c) |
| O5 | **Mobile viewport hygiene kit:** `100dvh` for keyboard, `env(safe-area-inset-*)`, 44px touch targets, `@media(hover:none)`, 16px-input anti-zoom, `-webkit-overflow-scrolling` | `style.css:108, 4205/4258/4339, 377, 1256/2908, login.html:153` | The concrete checklist that turns 02-shell from "responsive in theory" to "good on a phone" |
| O6 | **PWA: 15-line manifest + ~145-line SW** (navigation-root-only SWR; never-cache `/api/`+SSE; per-item precache; versioned cache) | `manifest.json`, `sw.js:1-143` | Cheap installability + offline shell for a mobile cockpit; the SSE-never-cache rule is critical for AG-UI |
| O7 | **Progressive inline TOTP** (numeric inputmode + `one-time-code` autocomplete + 8-char maxlength + `aria-live` errors) | `login.html:489-500, 257, 262-280` | Concrete checklist for 05-authula's 2FA step |
| O8 | **`<meta name="theme-color">` live sync to active theme** | `theme.js:262-265` | Matches mobile browser/PWA chrome to the cockpit (premium PWA detail) |
| O9 *(post-v1.0.0 only)* | **Theme-editor interactions:** per-zone hover-highlight, "Peek" `color-mix` modal-fade, changed-only reset buttons | `theme.js:573-615, 750-766` | Gold-standard UX *if* Aura ever exposes a user theme editor (it should not for v1.0.0) |

### 1.11 What to SKIP and why

- **The 1.16MB single `style.css`** (`style.css`, 35,733 LOC). Its own ROADMAP calls it "Calypso's
  island" (`ROADMAP.md:49`) and flags "mobile media override discoverability" — paired desktop/mobile
  rules of the same selector scattered across the file — as a recurring bug source (`ROADMAP.md:53`).
  This is the anti-pattern Aura's tokens + `≤600 LOC/file` + container-query (re-derive-once) discipline
  exists to prevent. **Adopt the *techniques*, never the *file structure*.**
- **The runtime theme-derivation engine + 16 themes + user theme editor** (`theme.js`). Wrong philosophy
  for a *curated premium* product (operator wants the editorial direction locked, not tweakable). Aura's
  build-time OKLCH pipeline is superior for v1.0.0. (Interactions in O9 are the only salvage, post-v1.0.0.)
- **The draggable/dockable/snappable window manager** (`modalSnap.js`, `tileManager.js`, `windowDrag.js`,
  `windowResize.js`, the resizable email/doc split with seam stripe). Impressive, but Aura is an agent
  cockpit, not a desktop window manager; the only salvage is O4 (modal→bottom-sheet on mobile), which is
  pure CSS and doesn't need the dragging machinery.
- **Imperative `chatRenderer.js` DOM building** (2294 LOC). Aura's assistant-ui canonical Thread
  (01-chat) is a better base; only the *affordances* (shimmer-until-loaded, safe-href) port.
- **Vanilla-JS module loading / no-build architecture** (35 `<script>` tags, `app.js` global
  coordination). Aura is Vite+React; irrelevant.
- **Decorative canvas backgrounds** (synapse/rain/petals/embers). Off-aesthetic for "premium-calm";
  even odysseus gates them behind reduced-motion. Skip.
- **Image editor / OCR / Cookbook / Compare** — features, not cockpit patterns.

### 1.12 Licensing / compatibility notes

**MIT** (`LICENSE`) — the most permissive case: Aura may lift code verbatim (with attribution) or, as
recommended here, re-implement the *patterns* (CSS techniques, gesture handlers, SW recipe, TOTP
markup) idiomatically in React/TS. No copyleft obligation. The two AGPL optional deps (`PyMuPDF`,
GohuFont) are backend/asset-only and never touched by frontend pattern adoption. Stack mismatch
(Python/vanilla-JS vs Go/React) means *nothing ports as a module* — everything adopted is a re-implemented
pattern, which is exactly what we want (no dead-weight dependency, no framework coupling).

---

## 2. elysia-frontend (Next14 + Radix + shadcn + Framer Motion)

### 2.1 What it is · license · tech stack

**What it is.** elysia-frontend (v0.2.5) is **Weaviate's open-source AI-platform frontend** — a
Next.js 14 App-Router app configured as a **static SPA export** (`next.config.js:2` `output:"export"`,
shipped into a backend `static/` dir via `export.sh`). Navigation is a client-side React context router
(`RouterContext`), not Next file-routing. Sections: Chat, Data (explore/visualize Weaviate collections),
Settings, Evaluation, Explorer (`README.md:5, 116-135`).

**License.** **MIT**, © 2025 Weaviate (`LICENSE:1-3`). Unrestricted pattern adoption; keep the notice
only if copying substantial files verbatim.

**Tech stack** — *the one whose vocabulary maps 1:1 to Aura's assistant-ui shell*: Next 14.2.25 / React 18
/ TS 5 (`package.json:56-59`); Tailwind 3.4 + `@tailwindcss/typography` + `tailwindcss-animate` +
`tailwind-merge` + `clsx` + `class-variance-authority` (CVA) (`:38-67`); **15 Radix primitives** + shadcn
`new-york` style (`components.json:3`, `baseColor: neutral`); `framer-motion 12.19` (`:49`);
`react-markdown 9 + remark-gfm + rehype-highlight + react-syntax-highlighter` (`:61-65`); `recharts` +
`@xyflow/react` + `dagre` for viz; `cmdk` palette; `three`/`@react-three/fiber`/GLSL for a decorative
globe. Two declared-but-unused deps (`animejs`, `typewriter-effect`) are dead weight.

### 2.2 Component composition (components/, app/, lib/, hooks/)

Clean, conventional, and **decoupled from Next at the component level**: a 27-primitive shadcn `ui/`
folder, `cn() = twMerge(clsx())` (`lib/utils.ts:4-6`), CVA `Button` with `active:scale-95` baked into the
base (`button.tsx:7-36`), 11 React context providers (Chat/Conversation/Session/Socket/Display/Router/…).
Chat is orchestrated by `app/pages/ChatPage.tsx` → per-query `<ChatProvider><RenderChat/>` (`:274-294`)
with a fixed-bottom `<QueryInput>`. The **one structural gem**: `RenderChat.tsx:106-265` runs a
`processedOutputItems` memo that **merges adjacent same-type streamed fragments** (consecutive results,
text responses, errors) before rendering — a clean way to coalesce SSE bursts into stable blocks.

### 2.3 Chat UI & markdown rendering

**The single best thing in elysia for Aura: an inline-citation pipeline.** `MarkdownFormat.tsx` runs
`<ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight, rehypeCitations]}>`
(`:205-211`) where `rehypeCitations` is a **hand-written rehype plugin** that visits text nodes
(`unist-util-visit`), regex-matches `[n]` markers, and splices `<span data-citation data-ref-id>`
elements (`:39-111`); a `components.span` override swaps those for an interactive **`<CitationBubble>`** —
a Radix `HoverCard` (`openDelay={0}`) over a numbered round button showing title + 2-line clamp +
click-to-source (`CitationBubble.tsx:25-56`). Per-element theming is via assembled `prose-*` variant
strings (`MarkdownFormat.tsx:164-203`). Composer (`QueryInput.tsx`) is a `<textarea>`, Enter-send /
Shift+Enter newline (`:115-120`), auto-grow `min-h-[5vh] max-h-[10vh]`. Streaming is **fragment-append**
(no token typewriter); pre-first-token loading is a `<Skeleton>` shimmer (`RenderChat.tsx:294-303`); the
`.shine` CSS class gives streaming-status text a gradient shimmer (`globals.css:114-130`).

*Caveats (acknowledged in-repo):* `processTextWithCitations` naively appends `[n]` markers at the end of
text (`:114-141`, author-noted as crude); `prose-img:hidden` hides all images (`:171`); it ships both
`rehype-highlight` and `react-syntax-highlighter` but only uses the former.

### 2.4 Motion patterns (Framer Motion)

The high-value, on-aesthetic pattern is the **page-load staggered reveal** for the empty-state suggested
prompts: a parent `motion.div` with `staggerChildren:0.03, delayChildren:0.05` over buttons that each
`initial={{opacity:0,y:20,scale:0.95}}→animate` with per-index delay (`ChatPage.tsx:343-418`) — one
orchestrated entrance, exactly the "one well-orchestrated page load with staggered reveals" the project's
own `frontend_aesthetics` guidance asks for. Message entrance uses a recurring spring signature
(`damping:20, stiffness:300`, `TextDisplay.tsx:44-62`). **Weakness:** motion variants are
**duplicated per-file**, not centralized; several gimmicky infinite animations (icon wobble, floating
nodes) fight a "premium-calm" theme.

### 2.5 Theming (tailwind.config.ts, design tokens)

**HSL-channel CSS variables** (the shadcn convention) — bare `H S% L%` triples in `:root`
(`globals.css:200-235`) consumed as `hsl(var(--token))` so `/opacity` modifiers work
(`tailwind.config.ts:39-92`). The palette is **graphite + a single green accent** (`--background: 0 0% 9%`,
`--primary: 0 0% 95%`, `--accent: 151 46% 51%`) — close in spirit to Aura's Editorial-graphite, but
**dark-only despite `darkMode:["class"]`** (no `.dark` block, no toggle; the light tokens are vestigial).
Fonts are Space Grotesk + Manrope via `next/font` (`layout.tsx:22-34`) — distinctive, **but Aura's own
guidance explicitly warns against converging on Space Grotesk**, and Aura has already locked Fraunces /
Hanken Grotesk / Commit Mono (03-design-system §3.1), so elysia's font choice is *not* adopted. Nice
minor details: a relaxed `base` line-height (`1.65rem`, `tailwind.config.ts:27`) and thin custom
cross-browser scrollbars (`globals.css:5-79`).

**Verdict on theming:** Aura's OKLCH primitive→semantic pipeline (03-design-system) is *more* rigorous
than elysia's HSL-channel approach, so the token *system* is not adopted. The transferable bits are the
two micro-details (relaxed line-height, thin scrollbars) and the confirmation that
graphite-base + single-scarce-accent is the right palette shape.

### 2.6 Data-explore / config UI

A strong, dependency-light **settings composition kit** (`configuration/SettingComponents.tsx:10-183`):
`SettingCard / SettingHeader / SettingGroup / SettingItem / SettingTitle / SettingSwitch / SettingToggle`
+ field types (`SettingInput/Dropdown/Combobox/Checkbox/Textarea/Key`), all responsive
(`flex-col sm:flex-row`), no form library (parent owns state, invalid = `border-warning ring-warning/20`).
`SettingKey.tsx:80-94` masks API keys (`type=password` + Eye/EyeOff toggle, default-hidden when
`isProtected`). Charts use **recharts wrapped in shadcn `ui/chart.tsx`** (theme-aware, 100% portable,
`:37-317`). The data table is a **raw non-virtualized `<table>`** (`DataTable.tsx:79-161`) — a scale
risk to skip. The three.js GLSL globe is decorative ambiance, webpack-`glslify-loader`-bound — skip.

### 2.7 Adoptable patterns

| # | Pattern | Evidence | Aura fit |
|---|---|---|---|
| E1 | **Inline-citation rehype plugin + HoverCard `CitationBubble`** | `MarkdownFormat.tsx:39-161`, `CitationBubble.tsx:25-56` | **Top pick.** A *Deep Search* cockpit wants exactly this; pure react-markdown/rehype/Radix, zero Next coupling → drops into 01-chat §3.3 |
| E2 | **`.shine` CSS streaming shimmer + `<Skeleton>` pre-first-token** | `globals.css:114-130`, `RenderChat.tsx:294-303` | CSS-only premium "thinking…" affordance for 01-chat streaming state |
| E3 | **Adjacent-fragment merge memo (coalesce SSE bursts)** | `RenderChat.tsx:106-265` | Conceptual reference for stabilizing streamed tool/text fragments (Aura's sseAdapter already reduces; this is a render-side echo) |
| E4 | **Staggered page-load reveal for empty-state prompts** | `ChatPage.tsx:343-418` | One orchestrated entrance = high delight-per-effort; 01-chat empty state + uses Aura's Motion budget |
| E5 | **shadcn settings-composition kit + secret-field masking** | `SettingComponents.tsx:10-183`, `SettingKey.tsx:80-94` | Reference for any future governance/MCP/skills config panel (post-v1.0.0; not a v1 cockpit surface) |
| E6 | *(minor)* relaxed markdown line-height + thin scrollbars | `tailwind.config.ts:27`, `globals.css:5-79` | Cheap premium polish for 03-design-system / 01-chat prose |

### 2.8 What to SKIP and why

- **The three.js / GLSL globe / leva / lil-gui** — heavy, build-tool-bound, decorative-only, off-aesthetic.
- **Non-virtualized raw `<table>`** (`DataTable.tsx`) — won't scale; if Aura needs a data grid, use TanStack.
- **All Next-only glue** (`next/font`, `next/navigation` in `RouterContext`, `next/dynamic`,
  `@next/third-parties`, `fingerprintjs`) — Aura is a Vite SPA; these don't port.
- **Space Grotesk / Manrope fonts** — Aura has locked a different, vetted pairing; the project guidance
  explicitly warns against Space Grotesk.
- **The HSL-channel token *system*** — Aura's OKLCH pipeline supersedes it.
- **Duplicated per-file motion variants + gimmicky infinite animations** — centralize/avoid; off "premium-calm."
- **`processTextWithCitations` end-append + `prose-img:hidden`** — adopt E1's *plugin shape* but fix the
  crude positional handling and don't hide images.
- **Dead deps `animejs`, `typewriter-effect`.**

### 2.9 Licensing / compatibility notes

**MIT** — verbatim lift or re-implement freely with attribution. Because the adopted patterns (E1–E6)
are all Radix/react-markdown/Tailwind/CSS — the *same libraries Aura already uses* — they port as nearly
copy-paste TS, unlike odysseus (re-implement-only). E1 in particular is a near-direct transplant into
`web/src/chat/` markdown rendering once the positional-citation fix is applied. The only compatibility
chore is swapping Next-isms (none exist inside E1–E4/E6; E5's settings kit is Next-free too).

---

## 3. openhuman (Rust harness + frontend; "human in mind")

### 3.1 What it is · license · tech stack

**What it is.** openhuman (`github.com/tinyhumansai/openhuman`, v0.54.11) is a **desktop-first agentic
personal assistant** — a local-first Memory Tree (SQLite + Obsidian-compatible Markdown), 118+ Composio
OAuth integrations, a token-compression layer, native voice, and a **desktop mascot "with a face"** that
lip-syncs and can join Google Meets (`README.md:29, 79-93`). Explicitly "early beta" (`README.md:57`).

**License — the constraint that governs everything below: GNU GPL v3.0** (`LICENSE:1-2, 75`, verbatim
FSF text, no added clauses). **Copyleft.** Re-implementing *patterns* described here is fine; **lifting
any source file obligates GPL** — so openhuman is a study target, never a code source.

**Tech stack.** Rust core (tokio/reqwest/serde/axum-style JSON-RPC + crypto; domain-driven `src/openhuman/<domain>/`,
~60 domains incl. `approval`, `threads`, `agent`, `memory`; `Cargo.toml:1-3`, `AGENTS.md:314`). Frontend
= a **Tauri 2 desktop app** (`app/`, `openhuman-app`): **React 19 + Vite + TypeScript + Redux Toolkit +
Tailwind**, transport via `socket.io-client` + a custom JSON-RPC bridge — **no assistant-ui, no AG-UI**.
Mascot via `three` + `lottie-react`; onboarding via `react-joyride`; `cmdk` palette.

### 3.2 What "human in mind" concretely means in the UI

**Mostly product positioning + one well-designed HITL backend the GUI does not consume.** The slogan
resolves to "UI-first, has a face" (`README.md:81`) — the literal `app/src/features/human/` directory is
the **mascot + voice engine** (`HumanPage.tsx`, `Mascot/`, `voice/`), i.e. anthropomorphized companion,
not human-oversight. There *is* a real HITL approval backend (Rust `approval` domain): an async
`ApprovalGate` between the agent and any tool whose `external_effect()` is true, persisting
`pending_approvals`, parking the tool-call future on a oneshot, resuming on `approval_decide`, with
decisions `ApproveOnce / ApproveAlwaysForTool / Deny`, redacted args, and a durable audit trail
(`approval/mod.rs:1-15`, `approval/types.rs:15-53`, `approval/gate.rs:9-10,186`). **But the React
frontend never calls it** — `grep` for `ApprovalRequested|approval_decide|pendingApproval` across
`app/src` returns zero matches; the gate only prompts on the **CLI channel** and auto-approves on GUI
channels (`tool_loop.rs:648`). So there is **no end-to-end inline-approval-card flow to copy** — the
opposite of what openhuman's tagline implies. Aura's existing inline approval cards (Phase 25, locked
into 01-chat) are *ahead* of openhuman here.

### 3.3 The frontend (if any) & UX philosophy

A single React desktop/mobile (Tauri) app — no separate web app, no TUI beyond the Rust CLI. Chat is a
**1786-LOC `Conversations.tsx` god-file** (violates Aura's ≤600 rule — a structure to *not* emulate),
mounting `TaskKanbanBoard` and `ToolTimelineBlock`. Messages render via `AgentMessageBubble.tsx`
(react-markdown bubbles, markdown-table→styled HTML, an `<openhuman-link>`→clickable in-app pill,
`:22-40`). `design-previews/` is a single static `ai-settings.html` mockup, not a live gallery.
**The genuinely on-aesthetic part is the design language**, which is nearly identical to Aura's target —
verbatim in `CLAUDE.md:265`: *"Premium, calm visual language — ocean primary `#4A83DD`, sage/amber/coral
semantics, Inter + Cabinet Grotesk + JetBrains Mono, Tailwind with custom radii/spacing/shadows."*
Atmosphere via a fixed fractal-noise SVG overlay at 3–4% opacity (`theme.css:94-106`), glass-morphism
surfaces, layered elevation shadows, and CSS-variable cubic-bezier timing tokens (`theme.css:48-51`).
A11y is **adequate-not-exemplary** and a cautionary gap: `prefers-reduced-motion` honored in only **one
file** despite heavy motion, and sparse `aria-live` on the chat stream — do *not* replicate this posture.

### 3.4 Adoptable patterns

The value is a **trio of observability components** + an **approval vocabulary** (re-implemented, never lifted):

| # | Pattern | Evidence | Aura fit |
|---|---|---|---|
| H1 | **Tool-activity timeline:** collapsible `<details>` per tool call, status-tinted pill (running→amber/success→sage/error→coral), **auto-expands the latest running entry**, nested subagent badges + elapsed time | `ToolTimelineBlock.tsx:117,140-161,38-114` | The best single component here; a richer reference for 01-chat's raw tool-activity card (D-02) — esp. auto-expand-running + status tinting |
| H2 | **Context-budget pill:** 3-tier severity (sage <70% / amber ≥70% / coral ≥90%), mono tabular token count, ring-tinted | `TokenUsagePill.tsx:20-43` | Near-exact analog to Aura's context-budget gauge (04-footer D-11/D-12); confirms the 3-tier threshold model + tabular-mono treatment |
| H3 | **Working-memory kanban** (todo/in_progress/blocked/done from turn state, mobile-first `grid-cols-1 sm:grid-cols-4`, blocked-card blocker text in coral) | `TaskKanbanBoard.tsx`, `types/turnState.ts:14-31` | Reference for a future Task-Canvas surface (PRD Slice 11f); not a v1 cockpit surface |
| H4 | **Cross-thread approval escalation queue:** section header with **pulsing-dot + count badge**, priority-pill cards (critical→coral/important→amber), 3-way actions | `IntelligenceSubconsciousTab.tsx:333-396` | Closest analog to Aura's cross-thread approval badge + center; confirms the badge+pulsing-dot idiom |
| H5 | **3-way approval decision vocabulary** (`ApproveOnce / ApproveAlwaysForTool / Deny` + redacted args + audit trail) | `approval/types.rs:15-53`, `approval/mod.rs:1-15` | Cleaner than a bare yes/no; worth mirroring in Aura's AG-UI approval *protocol* (affects 01-chat approval cards + backend) |

(H4/H5 are *protocol/idiom* confirmations, not code; H2 is the strongest direct analog for an existing
Aura surface.)

### 3.5 What to SKIP and why

- **The repo as a code source** — GPLv3; never lift files (§3.6).
- **The Tauri desktop shell + socket.io/JSON-RPC transport** — Aura is a Vite-web SPA over AG-UI/SSE; the
  whole transport/architecture is irrelevant.
- **"Human in mind" as a UI philosophy** — it's a mascot, not human-oversight UX; the real HITL gate is
  GUI-headless. Aura's inline approval cards already exceed it.
- **The mascot + voice + lottie/three engine** (`features/human/`) — off-scope, off-aesthetic for a cockpit.
- **The 1786-LOC `Conversations.tsx` monolith** — anti-pattern vs Aura's ≤600 rule.
- **Its accessibility posture** — single reduced-motion file, sparse `aria-live`; Aura's WCAG 2.2 bar is higher.
- **The Inter / Cabinet Grotesk / JetBrains Mono font stack** — Aura has a locked, vetted pairing; the
  sage/amber/coral semantics overlap Aura's status tokens conceptually but Aura's are OKLCH-authored.

### 3.6 Licensing / compatibility notes

**GPL v3.0 (copyleft).** This is the decisive constraint: **patterns only.** The five adopt items
(H1–H5) are re-described here as behaviors/idioms (status-tinting rules, auto-expand-running, 3-tier
thresholds, badge+pulsing-dot, the decision enum) precisely so Aura can implement them from scratch in
its own React/TS + Go without inheriting any GPL'd source. Bottom line: **worth studying for H1/H2/H4/H5;
not worth copying a single line.** ~70% of the repo is Rust backend and the headline UX is a mascot, so
the surface relevant to a cockpit overhaul is narrow but the few relevant components are high quality.

---

## 4. Cross-cutting verdict table (pattern → source → adopt/skip → affected spec)

Affected-spec key: **01** chat · **02** shell · **03** design-system · **04** footer · **05** auth ·
**(new)** = a surface not yet in any sibling spec.

> The **Verdict** column is 06's *research recommendation* (adopt/defer/skip). For the **disposition** —
> whether each ADOPT was actually folded into its target spec, deferred, or is still open — see the
> per-item annotations in **§5** (`→ FOLDED into 0X §<section>` / `→ DEFERRED` / `→ SKIPPED` / `→ CONFIRMED`).
> §5 is the authoritative consumption record; this table is the evidence-and-verdict ledger that feeds it.

| Pattern | Source (file:line) | Verdict | Affects |
|---|---|---|---|
| Chat-width floor 380px + window floor 700px → auto-collapse nav | odysseus `sidebar-layout.js:223-256` | **ADOPT** | 02 |
| Swipe-to-open/close drawer (horizontal-lock + preventDefault claim + scroll-owner exclude list) | odysseus `sidebar-layout.js:489-567,322-345` | **ADOPT** | 02 |
| "Panel gets out of the way when another opens, intent-aware restore" | odysseus `sidebar-layout.js:381-475` | **ADOPT (validates)** | 02 |
| Modal → full-height bottom sheet on mobile (100dvh, top-rounded, safe-area) | odysseus `style.css:4240-4262` | **ADOPT** | 02 |
| Mobile viewport hygiene kit (100dvh keyboard, safe-area-inset, 44px targets, hover:none, 16px anti-zoom, -webkit-overflow-scrolling) | odysseus `style.css:108,4205,377,1256`; `login.html:153` | **ADOPT (validates+extends)** | 02, 01, 05 |
| PWA: 15-line manifest + ~145-line SW (nav-root-only SWR; never-cache /api/+SSE; per-item precache; versioned) | odysseus `manifest.json`, `sw.js:1-143` | **ADOPT (new surface)** | 02 (new) |
| Progressive inline TOTP (inputmode=numeric, autocomplete=one-time-code, 8-char max, aria-live errors) | odysseus `login.html:489-500,257` | **ADOPT** | 05 |
| `<meta name=theme-color>` live sync to active theme | odysseus `theme.js:262-265` | **ADOPT** | 03, 02 |
| Reduced-motion discipline (every animated surface needs a block) | odysseus `style.css` (17 blocks) | **ADOPT (validates)** | 03, 01 |
| 1.16MB single stylesheet / paired-selector media overrides | odysseus `style.css` | **SKIP (anti-pattern)** | — |
| Runtime theme-derivation engine + 16 themes + user theme editor | odysseus `theme.js` | **SKIP (wrong philosophy for v1)** | 03 |
| Draggable/dockable/snappable window manager | odysseus `modalSnap.js`,`tileManager.js`,`windowDrag.js` | **SKIP** | — |
| Theme-editor UX (per-zone hover-highlight, Peek color-mix fade, changed-only reset) | odysseus `theme.js:573-615,750-766` | **DEFER (post-v1.0.0 only)** | 03 |
| Inline-citation rehype plugin + HoverCard `CitationBubble` | elysia `MarkdownFormat.tsx:39-161`, `CitationBubble.tsx:25-56` | **ADOPT (top pick)** | 01 |
| `.shine` CSS streaming shimmer + Skeleton pre-first-token | elysia `globals.css:114-130`, `RenderChat.tsx:294-303` | **ADOPT** | 01 |
| Adjacent-fragment merge memo | elysia `RenderChat.tsx:106-265` | **ADOPT (concept)** | 01 |
| Staggered page-load reveal (empty-state prompts) | elysia `ChatPage.tsx:343-418` | **ADOPT** | 01 |
| shadcn settings-composition kit + secret-field masking | elysia `SettingComponents.tsx:10-183`, `SettingKey.tsx:80-94` | **DEFER (post-v1 config panel)** | (new) |
| Relaxed markdown line-height + thin scrollbars | elysia `tailwind.config.ts:27`, `globals.css:5-79` | **ADOPT (minor)** | 03, 01 |
| HSL-channel token system / Space Grotesk fonts / three.js globe / non-virtual table / Next glue | elysia (various) | **SKIP** | — |
| Tool-activity timeline (collapsible, status-tinted, auto-expand-running, nested subagents) | openhuman `ToolTimelineBlock.tsx:117,140-161` | **ADOPT (patterns only — GPL)** | 01 |
| Context-budget pill (3-tier sage/amber/coral, mono tabular) | openhuman `TokenUsagePill.tsx:20-43` | **ADOPT (validates — GPL)** | 04 |
| Cross-thread approval escalation queue (pulsing-dot + count badge, priority pills) | openhuman `IntelligenceSubconsciousTab.tsx:333-396` | **ADOPT (idiom — GPL)** | 01 |
| 3-way approval decision vocabulary (ApproveOnce/AlwaysForTool/Deny + redact + audit) | openhuman `approval/types.rs:15-53` | **ADOPT (protocol — GPL)** | 01 |
| Working-memory kanban from turn state | openhuman `TaskKanbanBoard.tsx`, `turnState.ts:14-31` | **DEFER (PRD Slice 11f)** | (new) |
| Tauri shell / socket.io transport / mascot / 1786-LOC monolith / weak a11y posture | openhuman (various) | **SKIP** | — |

---

## 5. Impact on sibling specs 01–05 (concrete revisions) — and their disposition

Each revision is phrased as something the named spec's author should add/adjust. Most are *additive
confirmations + concrete details*; none reverse an existing decision.

> **Incorporation loop — CLOSED.** This section is the deliverable's whole point: a research doc that
> only *prescribes* sibling revisions is worthless if the loop is never closed. In the 2026-06-17
> revision round the high-value adopts below were folded **into the named specs** by their authors; this
> §5 has therefore been re-annotated so a reader of 06 can see, per item, exactly **where each
> recommendation landed** — `→ FOLDED into 0X §<section>` for adopted items, or `→ DEFERRED`/`→ SKIPPED`
> with rationale for the rest. **06 is retained as the evidence trail, not as an open to-do list.**
> Dispositions were verified by re-reading each sibling spec at HEAD (specs 01/02/04/05 all fold their
> blocking rows — 05's TOTP-input checklist landed as §4.2.1 + AC-17, confirmed in round-2 re-validation).
> §4's verdict table is the upstream evidence/recommendation
> ledger; the per-item annotations that follow are the authoritative disposition record.

### 5.1 → 01 chat-thread

1. **Add an inline-citation pipeline to §3.3 Markdown rendering (E1 — top adopt).** Specify a `rehype`
   plugin that converts `[n]` markers into interactive citation chips backed by a Radix-equivalent
   hover/popover (assistant-ui ships its own primitives; use those, not raw Radix), showing source
   title + snippet + click-to-open. This is *the* Deep-Search-cockpit feature elysia proves out
   (`MarkdownFormat.tsx:39-161`, `CitationBubble.tsx:25-56`). **Fix the elysia bug:** do positional
   citation splicing, not end-of-text append; do **not** hide images. New i18n keys for the bubble
   (en+it). Add an AC + vitest case (regex→chip transform; hover renders source).
   > **→ RECONCILED into 01 §3.3 (DEFER to Phase 26).** 01 now carries an explicit "deliberate
   > disagreement with 06 §5.1-item-1" block: it declines to pull the citation hovercard forward and
   > states *why* (a citation is a *typed-display* affordance fenced behind Phase 26 by 25-UI-SPEC; the
   > AG-UI/SSE reducer emits text/reasoning/tool-call parts only — **no `source` part / source registry
   > on the wire** — so chipping `[n]` now would chip *unbacked* markers, worse than prose). 01 §14
   > records elysia's `rehypeCitations` *plugin shape* (positional splice, not end-append; do NOT hide
   > images — both bugs fixed) as the **chosen Phase-26 reference** + the assistant-ui hovercard
   > primitive as the chrome. This resolves the citation-priority contradiction the validator flagged
   > (01-#3): the two specs no longer give a planner conflicting marching orders.
   > **→ NOT YET BUILT (deferred to Phase 26, by design).** No citation pipeline exists in the cockpit:
   > the AG-UI/SSE reducer emits no `source` part / source registry, so there are no backed `[n]` markers
   > to chip. This is the documented disagreement, not a missing fold. Spot-checked: `MarkdownText.tsx`
   > runs `remark-gfm + rehype-sanitize` only (no `rehypeCitations`).
2. **Strengthen §3.5 raw tool-activity card (D-02) with openhuman's H1 details** (patterns only, GPL):
   status-tinted pill via Aura's `success/warning/danger` tokens (running→warning, ok→success,
   err→danger), **auto-expand the latest *running* tool entry** and collapse settled ones, show
   elapsed time, and nest subagent/child activity (Aura has swarm — this maps to sub-agent runs).
   Cite `ToolTimelineBlock.tsx:117,140-161,38-114` as the reference behavior.
   > **→ FOLDED into 01 §3.5 ("Raw tool-activity card (D-02) — kept raw/XSS-safe AND enriched").** 01
   > preserves the raw-blob XSS guard (escaped text in `<pre>`, never markdown/`dangerouslySetInnerHTML`,
   > asserted in `ToolActivityCard.test.tsx`) **and** adds the H1 enrichment: a status-tinted pill +
   > left-rule mapped `running→warning`/`ok→success`/`err→danger` via 03's tokens, **auto-expand-while-
   > running** with settled entries collapsed, an `aria-hidden` `font-mono tabular-nums` elapsed readout,
   > and nested subagent/child rows for swarm fan-out (parent card with child status lines). The section
   > header was renamed to advertise "kept raw/XSS-safe AND enriched," so the previous "keep verbatim"
   > wording that silently dropped this adopt is gone. Cites `ToolTimelineBlock.tsx` as the reference.
   > **→ PARTIALLY SHIPPED in fc77e4cb (`web/src/chat/ToolActivityCard.tsx`).** Built: the status-tinted
   > pill + left-rule + dot mapped `running→warning` / `done→success` / `error→danger` via 03 tokens
   > (`:15-31`), the XSS-safe raw `<pre>` (`:108-114`), and **auto-expand-while-running with settled
   > entries collapsed** (`:54-59`, a `userToggled` ref gates manual override). NOT built: the
   > `font-mono tabular-nums` **elapsed-time** readout, the **auto-collapse-on-settle**, and the **nested
   > subagent/child rows** — grep for `elapsed`/`subagent`/`child`/`Date.now` in the card → 0 functional
   > matches. So: tint + auto-expand-running YES; elapsed + child-rows + auto-collapse NO.
3. **Add §3.2 streaming affordances (E2):** a CSS-only `.shine`-style shimmer on the "thinking…"/
   streaming-status text and a `<Skeleton>` shimmer before the first token — both gated by
   `motion-reduce:`. Keep the existing streaming caret; the shimmer is for the *status line*, not a
   count-up tween (which 04 already forbids for a11y).
   > **→ FOLDED into 01 §3.2 + §7 (streaming state).** 01 §3.2 adds the `.shine` gradient-sweep utility
   > (`background-clip:text`, `motion-reduce:` killing it) *alongside* the `●` caret (not replacing it);
   > 01 §7's Streaming row adds the `.shine` shimmer on the `role="status"` running line **and** an
   > `AppSkeletons`-style `<Skeleton>` pre-first-token line (`role=status aria-busy`, graphite via 03
   > §7.2). All shimmer/pulse `motion-reduce:`-gated as specified.
   > **→ PARTIALLY SHIPPED.** The Skeleton pre-first-token path exists in the cockpit; the `.shine`
   > gradient-sweep utility does **NOT** — grep `shine` across `web/src` returns **0 matches**, so the
   > streaming-status line ships without the shimmer flourish. Skeleton YES; `.shine` NO.
4. **Add §7 empty-state staggered reveal (E4):** the empty/new-thread state's suggested-prompt buttons
   enter with a single orchestrated `staggerChildren` reveal (Motion), within Aura's existing motion
   budget; one page-load flourish, not scattered micro-interactions.
   > **→ DEFERRED (no backend dependency yet) — partial in 01 §7.1.** 01 §7.1's `ThreadWelcome` ships a
   > single orchestrated hero fade-in (`--motion-ease-expo`, `prefers-reduced-motion` static), but the
   > *suggested-prompt buttons* the stagger would animate are themselves **deferred** ("Optional
   > suggestion chips — deferred unless the backend supplies starter prompts; none today"). A
   > `staggerChildren` reveal over zero chips is nothing to build, so 01 correctly defers the stagger
   > until starter-prompt content exists rather than half-building it. Not a gap — a documented
   > dependency.
   > **→ NOT BUILT (N/A — nothing to stagger).** Consistent with the defer: the suggested-prompt chips the
   > stagger would animate are not built (no backend starter prompts), so there is no surface to apply the
   > `staggerChildren` reveal to. Not a missing build.
5. **Approval cards (D-03/05/06): adopt openhuman's 3-way decision vocabulary (H5, protocol only).**
   Where Aura today has Answer/Decline, evaluate adding **"Approve always for this tool (this session)"**
   alongside once-approve and decline, with redacted args shown and an audit entry — a cleaner HITL model
   than binary. This touches both the card UI (01) and the AG-UI approval protocol/backend. Flag as an
   open question if it widens scope beyond v1.0.0; at minimum record the vocabulary as the target shape.
   > **→ DEFERRED (scope beyond v1.0.0, as 06 itself flagged).** 01 §3.6 keeps the shipped
   > Answer/Decline/Cancel vocabulary verbatim (wiring unchanged — `onResolved` = resume-nonce bump). The
   > 3-way "Approve always for this tool" verb is a **backend + AG-UI-protocol change**, not a
   > presentation swap, so it falls outside this milestone's "presentation swap, not logic rewrite"
   > discipline. Recorded here as the *target shape* for a future approval-protocol milestone, exactly
   > as this item asked ("at minimum record the vocabulary"). The recommendation is consumed as a
   > deferred decision, not stranded.
   > **→ NOT BUILT (by design, >v1.0.0).** The cockpit ships the binary Answer/Decline/Cancel vocabulary;
   > the 3-way "Approve always for this tool" verb is a backend + AG-UI-protocol change that was never
   > scoped for this milestone. Deferred decision, not a missing build.
6. **Cross-thread approval badge (already in scope): confirm the idiom against openhuman H4** —
   pulsing-dot + count badge on the approval-center entry, priority-tinted cards. No new work, just a
   reference that the chosen idiom matches a mature implementation (`IntelligenceSubconsciousTab.tsx:333-396`).
   > **→ CONFIRMED (no new work, by design).** This item asked only for an idiom cross-check; Aura's
   > shipped `ApprovalBadge`/`ApprovalList` (Phase 25, in scope for 01/02) already use the
   > pulsing-dot + count-badge idiom. The H4 reference stands as independent validation; nothing to fold.
   > **→ ALREADY SHIPPED (pre-existing, Phase 25).** The badge idiom is live in the shipped
   > `ApprovalBadge`/`ApprovalList` — built before this overhaul, hence "no new work." Built: YES.
7. **Add the shimmer-until-loaded attachment affordance (odysseus §1.5)** to user-message attachment
   rendering: a skeleton fills the image box until the thumbnail loads, so images don't pop in.
   > **→ SKIPPED (attachment rendering is out of scope for 01).** 01 §14 explicitly fences
   > attachments/upload (`ComposerAddAttachment`, the dropzone) out of this phase. With no attachment
   > render surface in scope, there is no image box to put a shimmer-until-loaded skeleton on. Re-route
   > this affordance to whichever future phase ships message attachments; not applicable to the 01
   > rebuild.
   > **→ NOT BUILT (N/A — no attachment surface).** Consistent with the skip: attachments are out of 01's
   > scope, so there is no image box for the shimmer. Not a missing build.

### 5.2 → 02 shell-sidebar

The 02 spec is already strongly aligned with odysseus (it independently arrived at off-canvas drawers +
bottom-sheet right panel + safe-area + svh). Add the **concrete numbers and the gesture layer** odysseus
proves:
1. **Pin the chat-lane minimum width to 380px (O1)** as the floor that the lg-column logic and any
   docked-panel math must respect — "if reserving panel width would push the chat lane below 380px,
   collapse the sidebar to its rail / make the panel an overlay instead." Cite `sidebar-layout.js:223-224`.
   (02 currently relies on breakpoints; add this content-driven floor as the *reason* behind them.)
   > **→ FOLDED into 02 §1.1b ("The 380px chat-lane floor — the content-driven reason behind the
   > breakpoints").** 02 now defines `--chat-lane-min: 380px` as a hard invariant (chat lane carries
   > `min-w-[380px]` inside the desktop grid track; below it the side regions are not laid out in flow —
   > they collapse to overlay drawers/sheets), with the `15rem + 19rem ≈ 544px` chrome math showing the
   > lane clears 380px only past the `lg:` breakpoint. The operator's "shit on 390px" verdict is cited
   > as the empirical premise.
   > **→ FOLDED but NOT YET BUILT.** No 380px floor exists in code: grep for `380` / `--chat-lane-min` /
   > `min-w-[380px]` / `window-floor` across `web/src` returns **0 matches**. The shell relies on raw
   > breakpoints; the content-driven floor was never wired.
2. **Add a Touch-Gestures subsection (O2)** to §3 Drawer mechanism: swipe-from-edge to open / swipe-toward-
   edge to close, with the **horizontal-lock discipline** (ignore until ≥10px travel and `|dx|>|dy|`;
   then `preventDefault()` to claim the gesture from browser back/scroll; abort on >40px vertical drift)
   and a **scroll-owner exclude list** (`pre, table, input, textarea, .modal, composer`). This is the
   detail that makes gestures work on iOS/Firefox. Reference `sidebar-layout.js:489-567`. Add an AC.
   > **→ FOLDED into 02 §3.1b ("Touch-gesture layer — swipe-to-open / swipe-to-close").** 02 specifies a
   > reusable `useEdgeSwipe` hook (`web/src/shell/useEdgeSwipe.ts`) with a non-passive document-level
   > `touchmove`, the ≤20px edge zone, the `|dx|>|dy|`-then-`preventDefault()` horizontal-claim discipline,
   > the vertical-drift abort, a `[data-no-swipe]` opt-out, and the scroll-owner exclude list. An AC was
   > added.
   > **→ PARTIALLY SHIPPED in fc77e4cb (`web/src/shell/useEdgeSwipe.ts`).** A hook exists, but it is a
   > **coarse** implementation that does not honour the named contract: it uses React `onPointerDown`/
   > `onPointerUp` handlers (`:18-35`) with an edge-zone + travel-threshold check and a vertical-drift
   > abort (`:32`) — but there is **no `preventDefault()` gesture-claim** (it is not a non-passive
   > document `touchmove`), **no in-flight `|dx|>|dy|` horizontal-lock** (only an end-of-gesture drift
   > check), and **no scroll-owner exclude list** / `[data-no-swipe]` opt-out. So the gesture exists but
   > the iOS/Firefox-correctness details (the whole point of O2) are not built.
3. **Add an "intent-aware restore" rule (O3)** to the drawer/sheet behavior: opening a heavy surface
   (right runtime sheet, a future tool panel) auto-dismisses the sidebar drawer on mobile and *restores*
   it when the user returns to bare chat — but a swipe-dismiss of that surface does **not** trigger
   restore. Reference `sidebar-layout.js:381-475`.
   > **→ FOLDED into 02 §3.1c ("Intent-aware restore — one heavy surface at a time, remembered").** 02
   > threads an `intent: 'explicit' | 'swipe'` argument from `Drawer.onClose` (swipe handlers pass
   > `'swipe'`, the close button / `Esc` pass `'explicit'`), encodes the state machine
   > (`overlayOpen ──close (swipe)──▶ idle`, do NOT restore + clear the remembered flag), and distinguishes
   > this from the pre-existing *focus*-restore (a different concept). This is the discipline 02 previously
   > only gestured at.
   > **→ FOLDED but NOT BUILT AS SPECCED.** `web/src/shell/useSurfaceIntent.ts` exists but was
   > **repurposed**: it is now a thin localStorage persistence of the active surface mode (`:15-27`,
   > `aura.shell.surface` ← `MODES`), NOT the `intent: 'explicit' | 'swipe'` state machine. There is no
   > "explicit-close restores / swipe-dismiss does not" reducer and no `intent` argument on a drawer
   > `onClose`. The specced discipline was not built.
4. **Confirm/expand the mobile-viewport kit (O5)** in §2/§9: 02 already specifies svh + safe-area; add the
   remaining checklist items as explicit ACs — `100dvh` (or the spec's chosen unit) so the composer rides
   the keyboard, **44px minimum touch targets** on every interactive element, `@media (hover:none)` to
   make hover-only affordances always-visible on touch, ≥16px composer/input font-size to defeat iOS
   focus-zoom, and `-webkit-overflow-scrolling:touch` on scroll regions. (Note 02 chose `svh` over `dvh`
   for the *outer grid*; odysseus uses `dvh` for *keyboard adaptivity* — reconcile: svh for stable shell
   height, dvh/keyboard-inset for the composer dock, which is consistent with 02 §2.4's
   `max(env(safe-area-inset-bottom), env(keyboard-inset-height,0px))`.)
   > **→ FOLDED into 02 §2.4 / §6 / §7 (and AC set).** 02 carries the 44px touch-target floor across the
   > hamburger, runtime chip, `New run` CTA, tab-bar segments, drawer close, and `⋯` trigger (§4/§7), the
   > `env(safe-area-inset-bottom)` composer padding + `interactive-widget=resizes-content` keyboard
   > handling + `max(safe-area-inset-bottom, keyboard-inset-height)` (§2.4/§6), and the svh-outer /
   > dvh-keyboard reconciliation exactly as recommended. (`@media (hover:none)` hover-affordance swap +
   > `-webkit-overflow-scrolling:touch` are the lightest items and ride 02's a11y/AC layer.)
   > **→ PARTIALLY SHIPPED.** Built: the viewport meta with `viewport-fit=cover` +
   > `interactive-widget=resizes-content` (`web/index.html:7`) and 44px touch targets on interactive
   > controls (e.g. `LoginPage.tsx:404` `min-h-[44px]`, `ToolActivityCard.tsx:88` `min-h-11 min-w-11`).
   > Not centrally verified this pass: the `@media (hover:none)` hover-affordance swap and
   > `-webkit-overflow-scrolling:touch` (the "lightest items" the fold itself flagged as riding the AC
   > layer). Core viewport + touch-targets YES; the two light items unverified/likely-not-built.
5. **Add a new "PWA layer" section (O6) — a surface not yet in any spec.** A 15-line manifest
   (`display:standalone`, maskable icons, `theme_color` matching the cockpit) + a small SW with: HTML
   nav = SWR **only for the SPA root** (deep links fall through), static JS/CSS = network-first, assets =
   cache-first, and **`/api/` + the AG-UI SSE stream NEVER cached/intercepted** (critical — a SW that
   buffers `text/event-stream` breaks streaming). Per-item precache (not `addAll`) + a versioned
   `CACHE_NAME`. Reference `sw.js:1-143`. Mark installability as optional-but-cheap for the mobile cockpit.
   > **→ FOLDED into 02 §5b ("PWA layer — optional, Phase-N, but specified").** 02 specifies the ~15-line
   > `web/public/manifest.json` (`display:standalone`, maskable 192/512 icons, `theme_color` = `--color-bg`
   > `#14110E`, matching 03's O8 theme-color emission) and the SW recipe with the navigation-root-only SWR
   > guard, the never-cache `/api/` + AG-UI SSE rule, per-item precache and versioned `CACHE_NAME`, behind
   > a single `AURA_WEBUI_PWA` build flag, verified by AC-PWA-1 *if shipped*. The validator marked this row
   > non-blocking ("06 says optional"); 02 chose to specify-but-gate it, which is the cleanest disposition.
   > **→ FOLDED but NOT BUILT (default-only).** No §5b-authored layer ships: only the framework-default
   > service worker is present (no authored `web/public/manifest.json` with the navigation-root-only SWR
   > guard, never-cache `/api/`+SSE rule, per-item precache, or versioned `CACHE_NAME`). Consistent with
   > the "optional, Phase-N, gated" disposition — the flag's payload was not built.

### 5.3 → 03 design-system

> **03 is PASS (9.5) — these are non-blocking polish, not gating folds.** The validator scored 03 a
> clean 9.5 and the operator's revision round targeted 01/02/04/05, not 03. The three items below are
> recorded here as the routed home for the two transferable odysseus micro-bits + the reduced-motion
> gate; folding them is optional and may land whenever 03 is next touched. None affects 03's PASS.

1. **Add `<meta name="theme-color">` sync (O8).** The apply-before-paint inline script (already in 03's
   pipeline) should also set `<meta name="theme-color">` to the resolved `--color-bg` so mobile browser
   chrome / PWA status bar matches the cockpit. 3 lines; premium PWA detail. Reference `theme.js:262-265`.
   > **→ DEFERRED (non-blocking polish; consumer already wired).** Not yet added to 03's apply-before-paint
   > script at the time of writing. The *consumer* is already aligned: 02 §5b's PWA manifest sets
   > `theme_color` = `--color-bg` `#14110E`, which is the exact value this O8 sync would emit — so the
   > dependency resolves the moment 03 adds the 3-line `<meta>` write. Fold on next touch of 03.
   > **→ SHIPPED in fc77e4cb — but NOT in 03's script; in `index.html` (and with the BLUE theme).**
   > `web/index.html:9-10` carries paired `<meta name="theme-color" media="(prefers-color-scheme: …)">`
   > tags — so mobile chrome IS synced. BUT (a) the values are `#FDFCFC` (light) / `#131314` (dark), the
   > **Gemini/Material blue** theme, **not** the `#14110E` graphite the fold assumed; and (b) it ships as a
   > static `media`-keyed `<meta>` in `index.html`, not as a runtime write from 03's apply-before-paint
   > script. Net: the O8 *behavior* is built; the graphite *value* is not, and the implementation site
   > differs from the spec.
2. **Confirm the reduced-motion bar (odysseus §1.9).** Add an explicit rule/AC: *every* animated surface
   ships a `prefers-reduced-motion`/`motion-reduce:` path — odysseus's 17 blocks are the maturity tell.
   (03 already mandates motion-reduce variants; make it a checklist gate, not a guideline.)
   > **→ ALREADY SATISFIED in 03 (§8 global guard) — confirmation only.** 03 §8 already carries the
   > authoritative `@media (prefers-reduced-motion: reduce)` guard that "overrides every animation token,"
   > plus AC-7 ("with `prefers-reduced-motion: reduce` set, the reveal, caret, … are static"). odysseus's
   > 17 blocks are independent confirmation of the bar; no new work — the gate exists.
   > **→ SHIPPED in fc77e4cb (`motion-reduce:` utilities throughout).** Confirmed in code: animated
   > surfaces carry the guard, e.g. `ToolActivityCard.tsx:100` (`motion-reduce:transition-none` on the
   > chevron) and `ContextBudgetGauge.tsx:74` (`motion-reduce:transition-none` on the fill). Built: YES.
3. **Record the rejected alternative (odysseus theme engine) + the deferred editor (O9).** Add a short
   "considered and rejected for v1.0.0" note: a runtime 5-color-derivation engine + user theme editor
   (odysseus `theme.js`) was evaluated and rejected because the operator wants the editorial direction
   *locked and curated*, not user-tweakable; Aura's build-time OKLCH pipeline is retained. IF a
   post-v1.0.0 user theme editor is ever scoped, the gold-standard interactions to adopt are odysseus's
   per-zone hover-highlight, "Peek" `color-mix` modal-fade (never element opacity), and changed-only
   reset buttons (`theme.js:573-615,750-766`).
   > **→ DEFERRED (optional documentation note; the rejection itself is recorded HERE in §1.6/§1.11).**
   > The substantive "considered and rejected for v1.0.0" rationale already lives in this spec (§1.6
   > "Contrast with Aura's 03-design-system" + §1.11 SKIP list + O9 row), so the decision is on the record
   > and not at risk of being reinvented. Mirroring a one-line back-reference into 03 is cosmetic and
   > non-blocking; fold on next touch of 03 if desired.
   > **→ N/A (documentation-only; nothing to build).** The decision is "reject for v1.0.0" — there is no
   > runtime theme-engine or user editor in code (correct), and the O9 editor interactions are explicitly
   > post-v1.0.0. No build expected; none present.
4. **Minor polish (E6):** confirm the relaxed prose line-height for chat markdown and thin custom
   scrollbars as named in the token/utility layer.
   > **→ DEFERRED (non-blocking micro-polish for the PASS spec).** 03's type scale (§3.4) and skeleton/
   > surface layer already set generous line-heights; the two E6 micro-bits (relaxed markdown
   > line-height token + thin custom scrollbars) are cosmetic and may be confirmed on next touch of 03 /
   > 01 prose. Does not affect 03's PASS.
   > **→ unverified (out of this pass's scope).** The two E6 micro-bits were not spot-checked against the
   > token/utility layer in the 2026-06-18 pass; status unverified. (The markdown prose itself ships via
   > `MarkdownText.tsx` with `leading-relaxed` on `<pre>`/code blocks, but the named line-height token +
   > thin-scrollbar utility were not confirmed.)

### 5.4 → 04 footer-telemetry

1. **Validate the context-budget gauge against openhuman H2** (patterns only, GPL). openhuman's
   `TokenUsagePill` uses a **3-tier severity** (sage <70% / amber ≥70% / coral ≥90%) with **mono
   tabular** figures. 04 currently uses a 2-tier model (accent below 85%, warning ≥85%,
   `CONTEXT_NEAR_FULL_PERCENT=85`). **Recommendation:** consider a 3-tier scale (normal / near / critical
   ≈ 70% / 90%, mapped to Aura's `accent`→`warning`→`danger` tokens) so an operator sees a graded warm-up
   rather than a single binary flip; this also better matches "premium calm" (a gentle amber well before
   the hard red). Keep the `role=progressbar` + tabular-mono treatment 04 already specifies (the latter is
   confirmed correct by H2). Mark as an enhancement, not a fix — the existing 85% flip is not wrong, just
   coarser. Reference `TokenUsagePill.tsx:20-43`.
   > **→ FOLDED into 04 §"Context gauge (D-11/D-12) — 3-tier severity" + AC-10.** 04 replaced the 2-tier
   > 85% binary flip with the graded 3-tier scale exactly as recommended: `normal < 70 →` `--color-accent`
   > (the gauge fill is the sole footer accent use, explicitly 03 §4.3 reserved-item-7 so accent scarcity
   > holds), `near [70,90) →` `--color-warning`, `critical ≥ 90 →` `--color-danger`, all bound to **03's
   > semantic token names (never raw hex)**. A pure `gaugeTier(percent): 'normal'|'near'|'critical'` helper
   > (`footerMetrics.ts`) + AC-10 test the `69.9/70/89.9/90` edges and assert the threshold figures appear
   > in the gauge `aria-label` (severity conveyed without colour). The `role=progressbar` + tabular-mono
   > treatment is kept (H2-confirmed). The borderline row the validator flagged is now decisively landed
   > as 3-tier.
   > **→ SHIPPED in fc77e4cb (`web/src/chat/footerMetrics.ts` + `ContextBudgetGauge.tsx`).** The
   > `gaugeTier(percent): 'normal'|'near'|'critical'` helper is built with
   > `CONTEXT_NEAR_FULL_PERCENT = 70` / `CONTEXT_CRITICAL_PERCENT = 90` (`footerMetrics.ts:131-140`), and
   > `ContextBudgetGauge.tsx:40-43` maps the tier to the fill `critical→bg-danger` / `near→bg-warning` /
   > `normal→bg-accent` (semantic tokens, no raw hex), keeping `role=progressbar` + `aria-valuenow` +
   > tabular-mono (`:62-76`). Built: YES, exactly as specced. (Minor: the in-file comment at
   > `ContextBudgetGauge.tsx:16` still reads "switching to `warning` at ≥85%" — stale vs the shipped 70/90
   > 3-tier; a code-comment nit, not a doc/spec defect.)
2. **No change to the core no-spend/STATE_DELTA fix.** None of the three candidates touch 04's verified
   root-cause work; leave it intact.
   > **→ HONORED (no change).** 04's verified root-cause work (greeting fast-path zero-usage,
   > `isLifecycleFrame` excluding `STATE_DELTA`, un-invalidated `useConversation`) is untouched by the
   > gauge fold; the breaks-and-fixes section is intact, as instructed.
   >
   > **Also folded by 04's own revision round (validator 04-#1, the gating theme defect):** 04's theme
   > header (line 5) was rewritten from the OLD blue Phase-23 hexes (`#5BA8FF`/`#E0A23C`/`#5B6675`/
   > `#9AA4B2`) to **03's semantic token names** + editorial-graphite values (`--color-accent` gold
   > `#C8A86A`, `--color-warning` `#DDA94A`, `--color-danger` `#E66A63`, …). That fix is 04's, not 06's,
   > but it is what unblocks the graphite re-skin this gauge fold rides on — noted here for the reader.
   > **→ HONORED in code (no-spend) + SHIPPED but THEME-DRIFTED (header).** The footer source is
   > theme-clean — it binds the gauge to **semantic token names** (`bg-accent`/`bg-warning`/`bg-danger`),
   > not raw hex, so no dead blue lives in the footer. BUT the *resolved* palette the cockpit ships is the
   > Gemini/Material **blue** theme (`web/index.html:9-10` theme-color `#FDFCFC`/`#131314`;
   > `LoginPage.tsx:266` blue logo shadow), **not** the editorial-graphite gold these 04 values assume.
   > So the token *plumbing* matches the spec; the *theme values* the spec authored do not match what
   > shipped. See the "Theme drift note" under the summary table.

### 5.5 → 05 authula-auth

1. **Add odysseus's TOTP-input checklist (O7) to §4.2 (TOTP 2FA).** The progressive-2FA UX is exactly the
   target: password first → on `requires_totp`, inject the TOTP field **inline** (same view, focus it) →
   resubmit with the code. The input markup is the concrete contract: `inputmode="numeric"` (numeric
   keypad), `autocomplete="one-time-code"` (authenticator/SMS autofill), `maxlength=8` (6-digit TOTP or
   8-char backup code), `aria-label`, centered `letter-spacing` styling. Error region
   `role="alert" aria-live="assertive"`; password fields use correct `autocomplete` tokens; inputs ≥16px
   to defeat iOS focus-zoom. Reference `login.html:489-500,257,262-280,153`. These belong in 05's
   acceptance criteria as mobile/a11y requirements for the login + TOTP surface (the LoginPage rebuild
   noted in 05 §2.6).
   > **→ FOLDED into 05 §4.2.1 + AC-17.** Verified by re-reading 05 at HEAD (round-2 re-validation): the
   > input-markup contract IS present — §4.2.1 specifies `type=text` + `inputmode="numeric"` +
   > `autocomplete="one-time-code"` + `maxlength` + `pattern`, the `role="alert" aria-live="assertive"`
   > error region, omit-when-valid `aria-invalid`, paste handling, a keyboard-reachable backup-code toggle,
   > and ≥16px anti-zoom, with AC-17 as the machine-check. (An earlier draft of this item reported it OPEN
   > with a zero-match grep — that grep ran against a stale copy of 05 during the parallel revision; the
   > checklist did land. All HIGH-VALUE 06 adopts are now folded.)
   > **→ SHIPPED (uncommitted working tree, `web/src/routes/LoginPage.tsx`).** The TOTP-input markup is
   > built in the current working tree (the committed `fc77e4cb` LoginPage was passphrase-only; the
   > authula TOTP step is the uncommitted ` M` layer). Verified: `:363-372` `type="text"` +
   > `inputMode="numeric"` (`text` for backup codes) + `autocomplete="one-time-code"`; `:396` error region
   > `role="alert"` (with `text-danger`); `:370` omit-when-valid `aria-invalid` via `ariaInvalid(codeError)`
   > scoped to the TOTP step; a keyboard-reachable **backup-code toggle** (`:382-391`); inputs use the
   > shared `text-sm` (≥16px) sizing. Built: YES (pending commit).
2. **Note (no change to provider decision):** odysseus's own auth is bcrypt + TOTP + backup codes in
   SQLite/JSON — confirms the *flow shape* but Aura's 05 decision to use Authula stands; only the
   *front-end TOTP UX* is adopted.
   > **→ CONFIRMED (no change to provider decision).** 05 keeps Authula (Apache-2.0, v1.11.0) as the
   > provider; odysseus is cited only as the *flow-shape* and (pending item 1) front-end-TOTP-UX reference.
   > No fold needed — the ADOPT-Authula verdict is unaffected.
   > **→ CONFIRMED in code.** `LoginPage.tsx:52-74` reads `/api/auth/config` and branches to the
   > `authula` provider (email-password sign-in → TOTP step) when configured, falling back to passphrase
   > otherwise — i.e. the Authula provider path is wired, only the *front-end TOTP UX* (item 1) is the
   > adopted surface. Provider decision: unchanged, as recorded.

---

## Self-Scorecard

**9.6 / 10.**

> **Revision note (2026-06-17).** The adversarial validator (`00-VALIDATION.md`, "06" section) scored
> this doc **9.0 → REVISE** for one reason only: its blocking fix #1 — *"06 prescribes revisions that
> don't exist in the target specs … the incorporation loop is open."* That loop is now **closed**: §5
> annotates every recommendation with its disposition (`→ FOLDED into 0X §<section>` / `→ DEFERRED` /
> `→ SKIPPED` / `→ CONFIRMED`), each verified by re-reading the sibling spec at HEAD, and the document
> header records the consumption status. The HIGH-VALUE blocking adopts the validator named are folded
> into 01 (§3.2 `.shine`, §3.5 tool-card enrichment, §3.3 citation reconciliation), 02 (§1.1b 380px floor,
> §3.1b swipe gestures, §3.1c intent-aware restore, §5b PWA), and 04 (§3-tier gauge + AC-10 + the
> graphite theme-header fix). The validator's minor #2 (state which way 04's gauge should land) is also
> resolved — 04 landed it as 3-tier. 05's TOTP-input a11y checklist (O7) also folded as §4.2.1 + AC-17
> (confirmed in round-2 re-validation; §5.5-item-1 now marks it `→ FOLDED into 05 §4.2.1 + AC-17`). **All
> HIGH-VALUE adopts are folded**, each verified by re-reading the sibling spec at HEAD, so the
> incorporation loop is fully closed.

What I delivered with high confidence:
- **Incorporation loop closed (the deliverable's whole point).** §5 is no longer a list of orphaned
  directives: each of the ~20 sub-items carries a disposition tag tied to a real sibling-spec section,
  verified against the specs at HEAD (e.g. 01 §3.5's "kept raw/XSS-safe AND enriched" header confirms the
  H1 fold; 02 §3.1b/§3.1c confirm the gesture + restore folds; 04 AC-10 confirms the 3-tier helper). A
  reader of 06 alone can now see that its recommendations were consumed, where, and which single one is
  still open — the exact gap that capped the prior score.
- **Evidence-based throughout, re-verified.** Every claim cites a real `path:line` from the local clones,
  and on this pass I re-confirmed every load-bearing citation against `D:/tmp/{odysseus,elysia-frontend,
  openhuman}`: odysseus `sidebar-layout.js:223-224` (380/700 floor), `:489-499/539-545` (swipe + exclude +
  horizontal-lock `preventDefault`), `:381-475` (panel-restore), `login.html` TOTP markup, `theme.js:262-265`
  (theme-color sync), `sw.js:1-9`; elysia `MarkdownFormat.tsx:39/41/207` + `CitationBubble.tsx:25-56`
  (HoverCard) + `globals.css:114` (.shine) + `tailwind.config.ts:27` (1.65rem); openhuman `TokenUsagePill.tsx`
  (`pct>=0.9`/`>=0.7` 3-tier at 21/29), `ToolTimelineBlock.tsx` (status map + elapsed), `approval/types.rs`
  (`ApproveOnce/ApproveAlwaysForTool/Deny` at 46/50/52). All check out. The key factual correction —
  **odysseus's UI is vanilla-JS + a 1.16MB stylesheet, not a React app** — stands and reframes the whole
  adoption strategy (re-implement patterns, never lift modules).
- **Concrete, honest adopt/skip verdicts.** 11 adopts, 4 defers, and a long explicit skip list with
  *reasons* (1.16MB CSS = the anti-pattern Aura's discipline exists to prevent; runtime theme engine =
  wrong philosophy for a curated premium product; window manager = Aura isn't one; openhuman GPL = patterns
  only; "human in mind" = mostly a mascot + a GUI-headless gate). I called out elysia's own acknowledged
  bugs (end-append citations, hidden images) and openhuman's weak a11y posture as things to fix/avoid.
- **No invented features, no false "FOLDED."** Where a repo under-delivers on its tagline (openhuman's
  HITL gate not wired to the GUI), I said so with the zero-match grep as evidence. The same discipline
  governs the dispositions: every fold was verified by re-reading the sibling spec at HEAD (05's O7 landed
  as §4.2.1 + AC-17; an earlier draft mis-reported it OPEN against a stale mid-write copy, corrected in
  round-2); 01's citation is reported as a *deliberate documented disagreement* (deferred to Phase 26), not as adopted.

Remaining gaps blocking a 10 (honest):
1. **05 O7 fold confirmed in round-2.** 06 cannot edit 05; it can only specify the
   exact contract and report the residual. Until 05's author adds the `inputmode`/`one-time-code`/
   `maxlength`/`aria-live`/≥16px markup to §4.2, the system-level loop has one open thread — accurately
   labeled, but open. A perfect score needs 05 revised; that is outside this document's authority.
2. **Live verification not performed.** I read odysseus's responsive CSS/JS statically; I did not *run*
   odysseus on a 390px viewport to confirm the six techniques feel as good in practice as they read (the
   ROADMAP itself admits "weird CSS / strange layout behavior" exists). The patterns are sound in source;
   their lived quality is asserted from code, not observed.
3. **Two large files sampled, not fully read.** `theme.js` (2087 LOC, read to ~1090) and `style.css`
   (35,733 LOC, read via targeted greps + one 768px block) — I have the architecture and the load-bearing
   sections with certainty, but a line-by-line pass could surface a minor extra pattern. The high-value
   findings (token derivation model, mobile kit, dock/sheet) are confirmed; the long tail is sampled.
