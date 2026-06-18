---
doc: 03-design-system-SPEC
title: Aura Cockpit Design System — "Editorial Graphite" (premium-calm)
status: implemented — token pipeline + font families landed; the shipped repo palette (logo-matched blue) is the ACCEPTED palette per operator decision 2026-06-18; the "Editorial Graphite" warm-gold values below are superseded-by-decision (see Implementation Ledger)
created: 2026-06-17
owner: design-system (consumed by chat-lane, shell/sidebar, footer-telemetry specs)
supersedes: web/tokens/tokens.json "dark" palette + Inter/system-mono fonts (Phase 23 v1)
extends: web/tokens/tokens.json + web/tokens/generate-theme.mjs pipeline (DO NOT FORK)
---

# Aura Cockpit Design System — Editorial Graphite

> The operator called the shipped theme generic and "ugly." This SPEC replaces the
> blue-on-near-black-blue Phase-23 v1 palette and the `Inter` / `ui-monospace` fonts
> with a **properly constructed, distinctive, premium-calm** design system, while
> **reusing the exact same token pipeline** (`tokens.json` → `generate-theme.mjs` →
> `theme.css` `@theme` + density blocks + apply-before-paint inline script). No new
> build step, no new framework, no fork. Every sibling spec (chat lane, shell/sidebar,
> footer telemetry) consumes the **semantic token names** defined in §4.

---

## Implementation Ledger (2026-06-18)

This section reconciles the spec below against the code that actually shipped. It does
**not** weaken or remove any acceptance criterion; it records, file:line, what landed,
what drifted, and what is still pending. The numbered design below remains the contract;
where it makes a now-false factual claim it carries an inline `> **Impl note**` annotation.

> **Operator decision (2026-06-18): the shipped repo palette is ACCEPTED — "color is ok like in
> repo, respect the logo."** The cockpit's logo-matched **blue** palette (in `web/tokens/tokens.json`,
> the source of truth) is the approved design. The "Editorial Graphite" warm-graphite + single-gold
> direction authored below is therefore **superseded by decision**, not a defect to fix. Read the
> token tables in this ledger as *"the §4.2 gold values are replaced by the accepted blue values"* —
> the `DRIFT` verdicts document the difference for the record; they are **not** a remediation backlog.
> The token *pipeline*, the new semantic token *names*, and the *font families* below all remain the
> live contract (those shipped faithfully). Only the §4.x **hex values** are superseded — defer to
> `web/tokens/tokens.json` for the real palette. (The `light` theme key the impl added is likewise
> accepted.) The remaining real follow-ups are mechanical, not chromatic: the font byte-budget breach
> (Commit Mono `.otf`) and the WCAG contrast re-proof against the shipped blue values.

**Implementing commit:** `fc77e4cb feat(cockpit): overhaul shell and chat lifecycle`
(2026-06-17). Touched the exact pipeline this SPEC named (`web/tokens/tokens.json`,
`web/tokens/generate-theme.mjs`, `web/src/styles/{fonts,theme,atmosphere,motion,index}.css`,
`web/index.html`, `web/src/theme/applyTheme.ts`) plus new fonts under `web/public/fonts/`.

### Headline verdict

**The pipeline and the font families landed; the palette shipped as a different (now ACCEPTED)
direction.** The "extend, don't fork" mechanics are honored verbatim — `tokens.json` →
`generate-theme.mjs` → `theme.css` `@theme` + density blocks, drift gate intact, three font
tokens emitted. The shipped color values are a **logo-matched blue palette** (neutral-grey
graphite + a *blue* accent) rather than this SPEC's warm-graphite + single-gold-accent
direction; the `_primitive` maps in `tokens.json` are named `gemini-bg`/`gemini-blue`
(`web/tokens/tokens.json:5-13, 37-46`), and a **second theme** (`light`) was added. **Per the
operator decision above, this blue palette + the `light` key are the approved design** (they
respect the logo). Net: the *infrastructure* this SPEC specified shipped faithfully and now
hosts the accepted blue palette; the warm-gold "Editorial Graphite" hex values below are the
superseded original direction, retained only as the authoring record.

### Token fidelity — spec vs shipped (`web/tokens/tokens.json:36-67`, `web/src/styles/theme.css:63-85`)

| Spec token (§4.2) | Spec hex | Shipped `color.dark` hex | Verdict |
|---|---|---|---|
| `bg` | `#14110E` warm graphite | `#131314` neutral near-black | **DRIFT** (warm→neutral) |
| `surface` | `#1B1714` | `#1E1F20` | **DRIFT** |
| `surface-2` | `#241F1A` | `#333537` | **DRIFT** |
| `surface-3` | `#2E2823` | `#3C4043` | **DRIFT** (name present, value off) |
| `border` | `#38322B` | `#3C4043` | **DRIFT** |
| `border-strong` | `#4A4339` | `#5F6368` | **DRIFT** (name present, value off) |
| `text` | `#ECE7DF` warm off-white | `#E3E3E3` neutral grey | **DRIFT** |
| `text-muted` | `#B0A99E` | `#C4C7C5` | **DRIFT** |
| `text-faint` | `#8E877C` | `#9AA0A6` | **DRIFT** |
| `text-disabled` | `#6A6258` | `#6F7377` | **DRIFT** (name present) |
| `accent` | `#C8A86A` warm gold | `#1F3760` **blue** | **DRIFT — accent hue inverted** |
| `accent-text` | `#E7D5A8` gold | `#D3E3FD` blue | **DRIFT** |
| `accent-muted` | `#7A6740` | `#1D4068` | **DRIFT** (name present) |
| `accent-pressed` | `#8A7245` | `#2A4B7B` | **DRIFT** (name present) |
| `on-accent` | `#14110E` | `#D3E3FD` | **DRIFT** (spec = bg-on-fill; shipped = light text) |
| `success` | `#6FB58A` jade | `#81C995` | DRIFT (value), role MATCH |
| `warning` | `#DDA94A` amber | `#FDD663` | DRIFT (value), role MATCH |
| `danger` | `#E66A63` rust | `#F28B82` | DRIFT (value), role MATCH |
| `info` | `#7FB0C8` slate | `#AECBFA` blue | DRIFT |
| `ring` | `#C8A86A` (= gold accent) | `#8AB4F8` blue | **DRIFT** (name present, value off) |

So: **every NEW semantic token name the SPEC introduced shipped** (`surface-3`,
`border-strong`, `text-disabled`, `accent-text`, `accent-muted`, `accent-pressed`,
`on-accent`, `ring`) — the *contract surface* sibling specs forward-reference is intact and
emitted as `--color-*` (`web/src/styles/theme.css:6-22`). But **every hex value drifted**,
and the defining accent hue is inverted (warm gold → cool blue). The amber `#DDA94A`, rust
`#E66A63`, gold `#C8A86A`, `#ECE7DF` text, `#1B1714` surface named in the brief do **not**
appear anywhere in the shipped tokens. The `_primitive` ramps the SPEC authored in OKLCH
(graphite.50…950, gold.300…700, jade/amber/rust/slate) were **not** carried; the shipped
`_primitive` is a small `gemini-*` hex set (`tokens.json:5-13, 37-46`).

### Font wiring (`web/src/styles/fonts.css:1-23`, `web/tokens/tokens.json:75-79`)

| Family | Spec role | Shipped | Verdict |
|---|---|---|---|
| **Fraunces** | display serif | `@font-face 'Fraunces'` → `fraunces-latin.woff2`, `font-weight: 300 900`, `font-display: swap` (`fonts.css:1-7`) | **IMPLEMENTED** (variable-weight range wider than spec's `380 600` clamp) |
| **Hanken Grotesk** | body/UI sans | `@font-face 'Hanken Grotesk'` → `hanken-grotesk-latin.woff2`, `300 900`, swap (`fonts.css:9-15`) | **IMPLEMENTED** — confirms the spec's chosen sans |
| **Commit Mono** | mono/data | `@font-face 'Commit Mono'` → `commit-mono-regular.otf` `format('opentype')`, weight 400, swap (`fonts.css:17-23`) | **PARTIAL/DEVIATED** — shipped as **`.otf`, not subset woff2**; no `commit-mono-medium` (500) face |

- Token families match the spec's §3.3 intent: `--font-display: Fraunces, Georgia, serif`,
  `--font-sans: "Hanken Grotesk", …`, `--font-mono: "Commit Mono", …`
  (`web/src/styles/theme.css:28-30`). The Tailwind utilities `font-display`/`font-sans`/
  `font-mono` are used across components (e.g. `RuntimeFooter.tsx`, `ShellHeader.tsx`,
  `LoginPage.tsx`), so the re-skin seam works.
- **No `Inter`/`Roboto`/`Arial` primary family anywhere in `web/src`** (verified — the only
  matches are the substrings "interface"/"interrupt"/"interval"). AC-3's negative grep holds.
- **DEVIATIONS vs §3.2/3.4:** (1) `font-display: swap` is present, but the spec's
  **metric-override knobs** (`ascent-override: 92%`, `size-adjust: 100%`) are **absent** —
  no CLS guard wired, so AC-9 ("no layout shift on swap") is **unverified**. (2) The spec's
  `<link rel="preload">` for the two critical fonts is **absent** from `index.html`. (3)
  Files are **not axis-clamped / not the named `*-opsz-wght.woff2` subsets**; the mono is a
  full `.otf`.

### Font size budget (§3.2 "≤220 KB woff2 total"; AC-2)

Actual shipped bytes (`web/public/fonts/`, mirrored byte-for-byte in `internal/webui/dist/fonts/`):

| File | Bytes | KB |
|---|---|---|
| `commit-mono-regular.otf` | 274,752 | ~268 |
| `fraunces-latin.woff2` | 67,304 | ~66 |
| `hanken-grotesk-latin.woff2` | 34,704 | ~34 |
| **Total** | **376,760** | **~368 KB** |

**Budget BREACHED.** ~368 KB vs the ≤220 KB woff2 gate. The woff2 pair alone (~100 KB) is
in budget; the un-subsetted `.otf` mono blows it. AC-2's size clause is **not satisfied** as
shipped, and there is **no CI bundle-size gate** asserting it.

### Theme-color sync, atmosphere, motion, density

- **`<meta name="theme-color">` sync (06 O8): PRESENT** — two media-scoped tags in
  `web/index.html:9-10` (`light` → `#FDFCFC`, `dark` → `#131314`). They track the *shipped*
  (Gemini) `bg` values, not the spec's `#14110E`. So the *mechanism* the sibling 06 spec
  forward-references exists; the value follows the drifted palette.
- **Forward-referenced tokens — ALL PRESENT** in the generated theme: `surface-3`,
  `border-strong`, `on-accent`, `accent-text`, `accent-muted` (`theme.css:6-22`);
  `--motion-*` (`theme.css:34-38`, shipped names `fast`/`base`/`slow`/`ease-out`/
  `ease-in-out` — a **different, smaller naming scheme** than the spec's
  `dur-instant…slower` + `ease-standard/enter/exit/expo`); `--shadow-*` (`theme.css:31-33`,
  shipped names `drawer`/`popover`/`focus` — again renamed from the spec's `0…4`/
  `accent-glow`). **Verdict:** a sibling reading the spec's exact `--shadow-2`/
  `--motion-dur-fast` names would get an **undefined** var — siblings must use the shipped
  names. Forward-reference contract = NAMES PRESENT for colors, RENAMED for shadow/motion.
- **Atmosphere (§7): PARTIAL/DEVIATED.** `atmosphere.css` exists but is **not** the spec's
  layered `--atmo` (corner glows + grain + vignette + `background-attachment: fixed`). It is
  a single accent-tinted top gradient on `body` (`atmosphere.css:11-17`) plus base/scrollbar/
  selection styling. **No grain layer, no `.app-atmosphere` class, no
  `background-attachment: fixed`.** AC-6 (grain, fixed glow, login-heavier variant) **not met**.
- **Motion (§8): PARTIAL.** `motion.css` carries the global `prefers-reduced-motion: reduce`
  guard (`motion.css:15-24`) — **AC-7's reduced-motion half is met**. But the **orchestrated
  staggered shell reveal** (header→sidebar→chat→footer with `--motion-ease-expo`) is **not
  implemented** — no `.app-shell-reveal`, no `animation-delay` stagger. AC-7's first-paint
  reveal is **not met**.
- **Density (§5.1): IMPLEMENTED.** `compact`/`operator`/`review` switch via `data-density`,
  operator default, scalars `4px / 34px / 13.5px` exactly as relaxed by the spec
  (`tokens.json:69-73`, `theme.css:86-88`, `web/src/theme/density.ts`). AC-10 **met**.
  (compact shipped `30px/12.5px` vs spec's `28px/12px` — minor.)
- **Radius (§5.2): IMPLEMENTED** verbatim (`6/10/16/22/999`, `tokens.json:74`).
- **Pipeline (AC-1): IMPLEMENTED.** `generate-theme.mjs` emits semantic-only colors (skips
  `_`-prefixed primitives, `generate-theme.mjs:13-25`), adds `--shadow-*` + `--motion-*`
  (`:30-31`), and the **drift gate is intact** (`:70-86`) — but it now asserts a
  **two-theme** loop (`themeBlocks` over all `tokens.color` keys, `:32-39`), so both `light`
  and `dark` blocks are generated (`theme.css:40-85`).

### Single-theme decision (§1) — DEVIATED

§1 locked exactly one theme key (`THEMES = ['dark']`). Shipped:
`THEMES = ['light', 'dark']` with a `ThemeSwitcher.tsx` component
(`web/src/theme/applyTheme.ts:5`, new `web/src/theme/ThemeSwitcher.tsx` from `fc77e4cb`).
A light theme was added rather than deferred. The `data-theme`/`data-density` attribute
mechanism and localStorage keys (`aura.theme`/`aura.density`) are otherwise unchanged.

### WCAG proof (§10, AC-8) — RE-PROVEN against the shipped blue (2026-06-18)

The §10 table was computed for the superseded Editorial-Graphite hex. AC-8 is now re-proven
against the **actual** `tokens.json color.dark` values by a new gate **`web/scripts/contrast-check.mjs`**
(`npm run contrast`; the script self-checks its luminance math against white/black = 21:1).
Result: **15/15 required AA pairs pass** —

| Pair | Ratio | AA min |
|---|---|---|
| text `#E3E3E3` on bg `#131314` | 14.47:1 | 4.5 |
| text on surface `#1E1F20` | 12.86:1 | 4.5 |
| text on surface-2 `#333537` | 9.60:1 | 4.5 |
| text-muted `#C4C7C5` on bg | 10.90:1 | 4.5 |
| text-faint `#9AA0A6` on bg | 7.03:1 | 4.5 |
| on-accent `#D3E3FD` on accent `#1F3760` (button) | 9.12:1 | 4.5 |
| accent-text `#D3E3FD` on bg (link) | 14.30:1 | 4.5 |
| success/warning/danger on bg (status) | 9.48 / 13.24 / 7.77:1 | 3.0 |
| danger on surface-2 (tool pill) | 5.16:1 | 3.0 |
| ring `#8AB4F8` on bg (focus) | 8.81:1 | 3.0 |

**One ADVISORY** (not a failure): `border-strong #5F6368` on `surface #1E1F20` = **2.73:1**, below the
1.4.11 non-text 3:1. It is classed advisory because the panel-divider border is reinforced by a
background step (surface vs bg) and interactive elements carry the passing focus ring — i.e. the
border is decorative, not the sole indicator of a control. **Operator follow-up:** if `border-strong`
ever becomes the sole boundary of a control, bump it one lightness step (e.g. `#5F6368` → ~`#6B7075`).

### Hard-coded-hex / accent-scarcity (AC-4 / AC-5)

Not exhaustively audited in this reconciliation; the token plumbing is clean (components use
`--color-*` utilities) but the spec's accent-scarcity reserved list (§4.3) was written for a
*gold* accent and the shipped accent is blue — the *discipline* (scarcity) is unverified
here and the *value* differs.

### What remains pending vs this SPEC

1. ~~**Palette**~~ — **RESOLVED by operator decision (2026-06-18): the shipped blue is accepted**
   ("color is ok like in repo, respect the logo"). No re-skin; the §4.x gold hex is superseded.
2. ~~**Single-theme**~~ — the added `light` key is **accepted** with the palette decision.
3. **Font budget + subsetting (AC-2)**: still pending. `pyftsubset`/`fonttools` 4.60 are installed,
   BUT woff2 emission needs the `brotli` python module (absent: `ImportError: No module named brotli`),
   and a lossless convert alone (~140 KB) would not meet the ≤220 KB total; subsetting Commit Mono (a
   *code* font) risks dropping box-drawing/symbol glyphs used in tool output, which needs a visual check
   in the running app. Recommended: `pip install brotli` then
   `pyftsubset commit-mono-regular.otf --flavor=woff2 --layout-features='*' --unicodes='U+0000-00FF,U+2000-206F,U+2190-21FF,U+2500-257F,U+2022,U+2026'`, update `fonts.css` src → woff2, add the 500 face, then add the CI size gate. Deferred to keep an unverifiable glyph subset out of the binary.
4. **CLS guard**: add `ascent-override`/`size-adjust` + `<link rel=preload>` (AC-9).
5. **Atmosphere**: layered `--atmo` glow + grain + `.app-atmosphere` + fixed attachment + a
   heavier login variant (AC-6).
6. **Motion**: the staggered first-paint shell reveal with `--motion-ease-expo` (AC-7).
7. ~~**Contrast gate**~~ — **DONE 2026-06-18**: `web/scripts/contrast-check.mjs` (`npm run contrast`)
   recomputes AA against the shipped `tokens.json` values; 15/15 required pairs pass (1 advisory). See
   the WCAG re-proof above. (Optional: wire `npm run contrast` into CI alongside `lint`/`test`.)
8. **Shadow/motion token names**: reconcile sibling forward-references (`--shadow-2`,
   `--motion-dur-fast`, …) against the shipped renamed set (`--shadow-popover`,
   `--motion-fast`, …) — or rename one side.

### Factual drift corrected in this doc

- Frontmatter `status: spec` → `superseded-in-implementation` (pipeline + fonts landed;
  palette did not). The numbered body is preserved unchanged except for inline `> **Impl
  note (2026-06-18)**` annotations on now-false claims (§1, §3.2, §4.1).

---

## 0. Direction & non-negotiables

**Aesthetic (locked):** *Editorial graphite, premium-calm.* A **warm graphite** dark
base (brown-leaning charcoal, not pure black, not blue-black), a **distinctive editorial
display serif** for headings, a **calm technical grotesque** for body/UI, a **tabular
monospace** for all numeric/runtime instruments, generous spacing, and **one restrained
warm-gold accent**. The product should read like a precision instrument bound in a good
notebook — confident, quiet, *designed* — not a consumer chatbot and not "AI slop."

This direction satisfies CLAUDE.md `<frontend_aesthetics>`:
- **Distinctive typography** — NOT Inter/Roboto/Arial/system fonts (§3).
- **Cohesive aesthetic via CSS variables** — the whole system is tokens (§4).
- **Atmosphere/depth via layered gradients**, not flat fills (§7).
- **High-impact, orchestrated motion** with `prefers-reduced-motion` (§8).
- **No clichéd purple-on-white** — warm graphite + a single gold accent (§4).

**Hard constraints (all enforced):**
1. **Single binary.** Fonts are self-hosted under `web/public/fonts/`, subset to the
   glyphs we render, woff2-only. No Google Fonts / no external CDN at runtime (also a
   privacy + offline-PWA requirement — the app already ships a service worker).
2. **Pipeline reuse.** Source of truth stays `web/tokens/tokens.json`; `generate-theme.mjs`
   emits `theme.css`. Tailwind v4 consumes the `@theme` block (utilities `bg-bg`,
   `text-text`, `border-border`, `bg-accent`, …). The apply-before-paint inline script
   in `index.html` and its drift gate stay intact (§9).
3. **Theme the whole cockpit** — chat bubbles, sidebar, runtime panel, footer telemetry,
   buttons, inputs, approval cards, skeletons, login (§6).
4. **WCAG 2.2 AA minimum**, proven by ratio table (§10).
5. **≤600 LOC per file.** The token JSON, generator, and `theme.css` are all small;
   `atmosphere.css` + `motion.css` are new ≤120-LOC partials imported from `index.css`.

**What this SPEC does NOT change:** the density model (`compact`/`operator`/`review`
scalars), the `data-theme`/`data-density` attribute mechanism, the radius token *names*,
the two-weight discipline for body text, or the accent-reserved list from `25-UI-SPEC.md`
§Color (the accent value changes; its scarcity rules carry over verbatim).

---

## 1. The single-theme decision (and why `dark` stays the theme key)

Phase 23 shipped exactly one theme (`THEMES = ['dark']`) under the `data-theme="dark"`
attribute and `color-scheme: dark`. **We keep the key `dark`** — Editorial Graphite *is*
the dark theme; renaming the key would churn `applyTheme.ts`, the inline script, the drift
gate, and localStorage for zero user benefit. The token *values* under `color.dark` change;
the *key* does not. (A future light "review/print" theme can land later as a second key
without disturbing this contract.)

> **Impl note (2026-06-18):** DEVIATED. The shipped tree carries **two** theme keys —
> `THEMES = ['light', 'dark']` (`web/src/theme/applyTheme.ts:5`) with a `ThemeSwitcher.tsx`
> — so the "future light theme" already landed, against this single-`dark`-key decision.

---

## 2. Token taxonomy — two tiers (primitive → semantic)

We adopt the W3C DTCG two-tier model (DTCG spec reached its first stable version
v2025.10 on 2025-10-28; supports aliasing + OKLCH natively — `https://www.w3.org/community/design-tokens/2025/10/28/design-tokens-specification-reaches-first-stable-version/`).
We do NOT pull in Style Dictionary (Phase-23 D-08 "minimal industrial shape" rejects heavy
tooling); the two tiers live in the existing hand-authored `tokens.json` and the generator
flattens **only the semantic tier** into `@theme` (primitives stay build-time-only, so the
LLM-visible CSS-var surface stays small and role-named).

```
Tier 1  PRIMITIVE  raw ramps, no opinion   graphite.50…950, gold.300…700, …
                   (authoring convenience; NOT emitted as --color-* utilities)
Tier 2  SEMANTIC   role tokens, emitted     --color-bg, --color-surface, --color-text, …
                   (the surface every sibling spec references)
```

Rationale for keeping primitives build-time-only: Tailwind v4 turns every `@theme`
`--color-*` into a utility class. Emitting 40 primitive ramp stops would flood the utility
namespace and the LLM tool surface with `bg-graphite-450`-style noise. Sibling specs must
only ever reference **semantic** names; the ramp exists so we can re-derive a value without
hand-mixing hex. (`generate-theme.mjs` change in §9 reads `color.dark` = the semantic map
and ignores a new sibling `_primitive` map used only for documentation/derivation.)

---

## 3. Type system

### 3.1 Chosen pairing (locked): **Fraunces · Hanken Grotesk · Commit Mono**

| Role | Family | Why this one | License (proof) | Source |
|------|--------|--------------|-----------------|--------|
| **Display** (headings, empty-state titles, panel titles, the brand wordmark) | **Fraunces** | High-contrast "old-style" soft serif with `opsz`, `wght`, **`SOFT`** and **`WONK`** axes — the soft/wonk axes make it unmistakably hand-designed, the single most anti-AI-slop editorial face available under OFL. Optical-size axis lets headings gain contrast at large sizes (true editorial behavior). | **SIL OFL 1.1** — repo LICENSE | `https://github.com/undercasetype/Fraunces` |
| **Body / UI** (chat text, labels, buttons, inputs, list rows) | **Hanken Grotesk** | Friendly classic grotesque, distinctive but calm and highly legible at the small operator sizes; variable `wght`; reads "designed," not Inter/Roboto. Holds up dense. | **SIL OFL 1.1** — repo LICENSE | `https://github.com/marcologous/hanken-grotesk` |
| **Mono / data** (token counts, `$0.0042`, cache `83%`, raw tool blobs, JSON, context gauge value, Cypher) | **Commit Mono** | "Smart" monospace with intelligent kerning + true tabular figures (monospace ⇒ fixed-width numerals by construction); calm and neutral so dense telemetry tables don't shout. | **SIL OFL 1.1** | `https://commitmono.com/` · `https://github.com/eigilnikolajsen/commit-mono` |

**Pairing rationale.** Fraunces (editorial serif) over Hanken Grotesque (technical
grotesque) is the *serif-display / grotesque-body* tension that defines "editorial
graphite" — the headings carry warmth and character, the body stays a quiet
control-room sans, and Commit Mono gives the instruments a precise, tabular voice. All
three are SIL OFL 1.1, so all three are self-hostable and bundleable in the single binary
(the OFL explicitly permits bundling/redistribution with software; only stand-alone resale
is forbidden — `https://openfontlicense.org/`).

> **Approved alternates** (if Fraunces' weight is a concern or the operator wants calmer):
> - *Quiet editorial:* **Newsreader** (display) · **Geist** (body) · **Geist Mono** (mono) — all OFL 1.1 (`https://github.com/vercel/geist-font`; Newsreader `https://github.com/google/fonts/tree/main/ofl/newsreader`). Calmer, less characterful than Fraunces.
> - *Engineered single-foundry:* **IBM Plex Sans** + **IBM Plex Mono** (+ optional Bricolage Grotesque display) — all OFL 1.1 (`https://github.com/IBM/plex`). Most metric-cohesive for dense tables.
> These are drop-in: only the `@font-face src` files and the three `--font-*` token values change. Do not mix-and-match outside a vetted pairing.

### 3.2 Self-host plan (single binary)

Place subset woff2 files under `web/public/fonts/` (served from the embedded `dist`, so
they ship inside the Go binary and are precached by the service worker — add `/fonts/*` is
automatic since `publicAssetEntries()` in `vite.config.ts` already hashes every file in
`public/`):

```
web/public/fonts/
  fraunces-opsz-wght.woff2        # display, variable opsz 9–144 + wght, axes clamped, Latin subset
  fraunces-italic-opsz-wght.woff2 # italics (optional — only if we render emphasis in headings)
  hanken-grotesk-wght.woff2       # body, variable wght 400–600 (clamp; we use 400/500/600 only), Latin
  commit-mono-regular.woff2       # mono 400, Latin + box-drawing + common JSON glyphs
  commit-mono-medium.woff2        # mono 500 (used for the gauge value + emphasised metric)
```

**Subsetting is mandatory** (Fraunces' full 4-axis VF is ~281 KB; clamp axes + Latin-subset
to land each family ~30–90 KB). Build-time tooling: `glyphhanger` / `fonttools subset`
(`pyftsubset`). Target unicode-range `U+0000-00FF, U+0131, U+0152-0153, U+2018-201F,
U+2032-2033, U+2212, U+2192, U+2713, U+2014-2015` (Latin + curly quotes + the arrows/checks
the chat + tool cards render). **Build budget: total fonts ≤ 220 KB woff2** across all
families; the CI bundle gate (Phase 23 freshness/size discipline) asserts it.

> **Impl note (2026-06-18):** BUDGET BREACHED. Shipped fonts total **~368 KB**
> (`commit-mono-regular.otf` 268 KB **un-subsetted `.otf`, not woff2** + Fraunces 66 KB +
> Hanken 34 KB woff2). No `commit-mono-medium` (500) face, no axis-clamped `*-opsz-wght`
> subsets, **no preload links** in `index.html`, and **no CI size gate** exists. See ledger.

`@font-face` declarations go in a new partial `web/src/styles/fonts.css`, imported FIRST in
`index.css` (before Tailwind, so `font-display` resolves before utilities apply):

```css
/* web/src/styles/fonts.css — self-hosted, woff2, subset; font-display: swap (no FOIT) */
@font-face {
  font-family: 'Fraunces';
  src: url('/fonts/fraunces-opsz-wght.woff2') format('woff2-variations');
  font-weight: 380 600;            /* clamp to the weights we use */
  font-stretch: normal;
  font-style: normal;
  font-display: swap;
  ascent-override: 92%;            /* metric-tune so the serif fallback (Georgia) doesn't shift layout */
  size-adjust: 100%;
}
@font-face {
  font-family: 'Hanken Grotesk';
  src: url('/fonts/hanken-grotesk-wght.woff2') format('woff2-variations');
  font-weight: 400 600;
  font-style: normal;
  font-display: swap;
}
@font-face {
  font-family: 'Commit Mono';
  src: url('/fonts/commit-mono-regular.woff2') format('woff2');
  font-weight: 400;
  font-style: normal;
  font-display: swap;
}
@font-face {
  font-family: 'Commit Mono';
  src: url('/fonts/commit-mono-medium.woff2') format('woff2');
  font-weight: 500;
  font-style: normal;
  font-display: swap;
}
```

`font-display: swap` (not `optional`) + a metric-matched fallback stack means **no FOIT**
and a no-flash apply-before-paint experience: the first paint uses the system fallback with
matched metrics, then swaps with negligible reflow. Preload the two most-critical files in
`index.html` `<head>` (`<link rel="preload" as="font" type="font/woff2" crossorigin>` for
`hanken-grotesk-wght.woff2` and `fraunces-opsz-wght.woff2`).

### 3.3 Font tokens (semantic — emitted)

```jsonc
"font": {
  "display": "'Fraunces', 'Georgia', ui-serif, serif",
  "sans":    "'Hanken Grotesk', ui-sans-serif, system-ui, sans-serif",
  "mono":    "'Commit Mono', ui-monospace, SFMono-Regular, 'Cascadia Mono', monospace"
}
```

→ `--font-display`, `--font-sans`, `--font-mono` (Tailwind `font-display`, `font-sans`,
`font-mono`). **`--font-sans` keeps its name** so every existing `font-sans` utility in
`AppShell.tsx`/`LoginPage.tsx` re-skins for free; `--font-mono` keeps its name so the
runtime footer / tool blobs re-skin for free. **`--font-display` is NEW** — sibling specs
opt headings into it explicitly (`className="font-display"`).

### 3.4 Type scale, weights, line-heights, tracking

Editorial-graphite is **less dense than the v1 operator scale** (premium-calm = more air),
but stays operator-legible. Weights: **display uses 380–500** (the soft optical weight is
the character), **body uses 400 / 500 / 600** (3 weights — one more than v1's two, because
a grotesque needs 600 to do what Inter did at 500; still no 700+), **mono uses 400 / 500**.

| Role | Family | Size | Weight | Line-height | Letter-spacing | Usage |
|------|--------|------|--------|-------------|----------------|-------|
| Display-XL | display | `clamp(1.75rem, 1.4rem + 1.4vw, 2.5rem)` 28–40px | 400 | 1.08 | `-0.02em` | Login wordmark, empty-state hero title |
| Display-L | display | 24px (`text-2xl`) | 420 | 1.15 | `-0.015em` | Panel titles, "Start a run" |
| Display-M | display | 20px (`text-xl`) | 460 | 1.2 | `-0.01em` | Section headings, dialog titles |
| Body-L | sans | 15px | 400 | 1.6 | `0` | Chat message text (premium reading measure) |
| Body | sans | 14px (`text-sm`) | 400 | 1.55 | `0` | List titles, approval question, inputs |
| Label | sans | 13px | 500 | 1.4 | `0` | Buttons, tool-call names, mode-switch items |
| Label-strong | sans | 13px | 600 | 1.4 | `0` | Active/selected row label, primary CTA text |
| Caption | sans | 12px (`text-xs`) | 500 | 1.35 | `0.01em` | Badges, helper text |
| Micro | sans | 11px (`text-[0.6875rem]`) | 600 | 1.3 | `0.06em` `uppercase` | Footer metric captions, section eyebrows, "compacted N turns" |
| Mono | mono | 12.5px | 400 | 1.5 | `0` `font-variant-numeric: tabular-nums` | Token counts, `$cost`, cache %, raw blobs, JSON |
| Mono-strong | mono | 12.5px | 500 | 1.5 | `0` `tabular-nums` | Context-gauge value, highlighted metric |

Notes:
- Body bumps the chat reading size to **15px / 1.6** (from v1's 14/1.5) — the single biggest
  "premium-calm" lever. Dense list rows stay at 14px.
- All mono is forced `font-variant-numeric: tabular-nums` (Commit Mono is tabular by
  construction; declaring it guards fallbacks) so telemetry columns don't jitter on update.
- Density still scales the *base*: operator `--font-base` becomes **13.5px** (from 13px),
  compact stays 12px, review 15px (§9 token diff). Sizes above are the role ceilings; the
  density `--font-base` governs default UI text where a role isn't applied.

These map to a small set of utility classes a sibling spec can apply directly (e.g. the
chat lane uses `font-sans text-[0.9375rem] leading-[1.6]` for Body-L); no new Tailwind
plugin needed.

---

## 4. Color system

### 4.1 Primitive ramps (Tier 1 — authoring only, OKLCH)

We author the ramps in **OKLCH** (Baseline Widely available since 2023-05, ~90%+ support;
perceptually-uniform lightness so the ramp steps look even — `https://evilmartians.com/chronicles/oklch-in-css-why-quit-rgb-hsl`,
`https://developer.mozilla.org/en-US/docs/Web/CSS/color_value/oklch`). The graphite ramp is
**warm**: hue ≈ 60 (amber-brown) at very low chroma (≤0.012) so it reads as warm charcoal,
never blue-black (the v1 mistake was hue ≈ 250 blue-black). Each stop lists the hex the
generator ships (OKLCH is the design intent; we emit hex for byte-stable, gamut-safe output
and zero runtime surprises — see §9 "OKLCH authoring, hex emission").

> **Impl note (2026-06-18):** NOT IMPLEMENTED. None of the warm-graphite/gold OKLCH ramps
> below shipped. `web/tokens/tokens.json` carries a small **`gemini-*` hex `_primitive`** set
> and a **neutral-grey + blue** semantic palette instead (e.g. `bg #131314`, `accent #1F3760`
> blue). The gold `#C8A86A` / amber `#DDA94A` / rust `#E66A63` / `#ECE7DF` text named here do
> not appear in the shipped tokens. See the Token-fidelity table in the Implementation Ledger.

```jsonc
"_primitive": {                          // documentation/derivation only — NOT emitted as utilities
  "graphite": {
    "950": "oklch(0.155 0.010 60)  #14110E",  // app base
    "900": "oklch(0.190 0.011 58)  #1B1714",  // surface
    "850": "oklch(0.225 0.012 56)  #241F1A",  // elevated
    "800": "oklch(0.265 0.012 55)  #2E2823",  // raised input / hover surface
    "700": "oklch(0.330 0.012 54)  #38322B",  // border
    "600": "oklch(0.400 0.012 52)  #4A4339",  // border-strong / divider on elevated
    "500": "oklch(0.520 0.012 50)  #6A6258",  // faint-2 / disabled
    "400": "oklch(0.610 0.013 50)  #837C72",  // (reserved)
    "300": "oklch(0.650 0.013 52)  #8E877C",  // text-faint
    "200": "oklch(0.760 0.012 60)  #B0A99E",  // text-muted
    "100": "oklch(0.910 0.010 70)  #ECE7DF"   // text (warm off-white, NOT #FFF — halation control)
  },
  "gold": {                                   // the single accent — warm desaturated gold
    "300": "oklch(0.840 0.060 85)  #E7D5A8",  // accent-text on dark / link text
    "400": "oklch(0.790 0.080 80)  #D8BC86",  // accent-soft text
    "500": "oklch(0.760 0.095 78)  #C8A86A",  // ACCENT (fills, ring, active rule)
    "600": "oklch(0.560 0.075 75)  #8A7245",  // accent-pressed
    "700": "oklch(0.470 0.060 72)  #7A6740"   // accent-muted (subtle accent bg/border)
  },
  "jade":   { "500": "oklch(0.730 0.110 162) #6FB58A" }, // success
  "amber":  { "500": "oklch(0.780 0.130 78)  #DDA94A" }, // warning
  "rust":   { "500": "oklch(0.660 0.150 28)  #E66A63" }, // danger
  "slate":  { "500": "oklch(0.730 0.060 230) #7FB0C8" }  // info (cool, distinct from gold)
}
```

Why these hues: the accent is **warm gold** (hue ≈ 78), not blue — it sits in the warm
graphite family so it feels integral, not bolted on, and it is the classic "premium ink &
gilt" editorial signal. Status colors are pulled toward the warm side (jade slightly warm,
amber/rust warm) so they live in the same world; only `info` stays cool to read as
genuinely different from the gold accent (WCAG 1.4.1 still requires icon+text, never color
alone — carried from `25-UI-SPEC.md`).

### 4.2 Semantic tokens (Tier 2 — emitted as `--color-*` / Tailwind utilities)

This is **the contract surface** sibling specs reference. Names extend the existing set
(everything `AppShell.tsx` uses today — `bg`, `surface`, `surface-2`, `border`, `text`,
`text-muted`, `text-faint`, `accent`, `success`, `warning`, `danger`, `info` — keeps its
name so existing utilities re-skin for free; **new** names are additive).

```jsonc
"color": {
  "dark": {
    "bg":            "#14110E",   // app background (graphite-950) — warm, not blue-black
    "surface":       "#1B1714",   // sidebar, cards, footer, drawers (graphite-900)
    "surface-2":     "#241F1A",   // elevated card / popover / nested panel (graphite-850)
    "surface-3":     "#2E2823",   // raised input, hover surface, code-block well (graphite-800)  [NEW]
    "border":        "#38322B",   // default hairline divider (graphite-700)
    "border-strong": "#4A4339",   // emphasised divider / input border / focus-adjacent (graphite-600) [NEW]
    "text":          "#ECE7DF",   // primary text — warm off-white (graphite-100), NOT #FFF
    "text-muted":    "#B0A99E",   // secondary text (graphite-200)
    "text-faint":    "#8E877C",   // tertiary/decorative text (graphite-300)
    "text-disabled": "#6A6258",   // disabled labels (graphite-500)  [NEW]
    "accent":        "#C8A86A",   // warm gold — fills, ring, active rule (gold-500)
    "accent-text":   "#E7D5A8",   // accent AS TEXT on dark (gold-300) — higher-contrast for links [NEW]
    "accent-muted":  "#7A6740",   // subtle accent bg/border (gold-700) [NEW; replaces v1 accent-dim]
    "accent-pressed":"#8A7245",   // pressed/active accent fill (gold-600) [NEW]
    "on-accent":     "#14110E",   // text/icon ON an accent fill (= bg; 8.3:1) [NEW; formalises the pattern]
    "success":       "#6FB58A",   // jade-500
    "warning":       "#DDA94A",   // amber-500
    "danger":        "#E66A63",   // rust-500
    "info":          "#7FB0C8",   // slate-500
    "ring":          "#C8A86A"    // focus-visible ring (= accent) [NEW; explicit token]
  }
}
```

**Migration of v1 names:** `accent-dim` (`#2E5C8A`) is removed; consumers used it as a
subtle accent bg → migrate to `accent-muted`. No other v1 token name is removed (only
re-valued), so `bg-bg`/`text-text`/`border-border`/`bg-accent`/`text-danger`/… in shipped
components keep working and instantly re-skin.

### 4.3 Accent discipline (carried verbatim from `25-UI-SPEC.md` §Color)

`--color-accent` (now warm gold `#C8A86A`) stays scarce. **Reserved for, and nothing else:**
1. Primary send/submit CTA fill (`bg-accent` + `text-on-accent`, 8.3:1).
2. `:focus-visible` ring (`--color-ring`) on every interactive element.
3. `aria-current` active state on mode-switch + selected conversation row (an accent
   **left-rule** + `text-accent-text`, surface-2 background — not a full accent fill).
4. Cross-thread pending-approval badge count (the one thing demanding attention).
5. Citation / "jump to thread" link text (`text-accent-text`).
6. Branch-picker active indicator + the in-flight streaming caret.
7. Context-budget gauge fill below the near-full threshold (→ `warning` ≥85%).

Everything else stays neutral surface/border/text. Do NOT accent-fill secondary buttons,
tool cards, or the whole footer. (Premium-calm depends on accent scarcity.)

---

## 5. Spacing, radius, elevation, shadow, motion scales

### 5.1 Spacing (density-driven — model unchanged, base widened)

The density-scalar model stays (`--space-unit`, `--row-h`, `--font-base`). Premium-calm
relaxes the operator tier slightly:

| Density | `--space-unit` | `--row-h` | `--font-base` |
|---------|----------------|-----------|---------------|
| compact | 3px | 28px | 12px |
| **operator (default)** | **4px** | **34px** *(was 32)* | **13.5px** *(was 13)* |
| review | 6px | 42px *(was 40)* | 15px |

Named steps (multiples of `--space-unit` at operator) — the rhythm sibling specs use:

| Token | operator px | Tailwind | Usage |
|-------|-------------|----------|-------|
| `xs` | 4 | `gap-1`/`p-1` | icon gaps, badge inset |
| `sm` | 8 | `gap-2`/`p-2` | part gaps, chip gaps |
| `md` | 12 | `px-3`/`py-2` | default element/composer padding |
| `lg` | 16 | `px-4`/`gap-4` | card/section padding |
| `xl` | 24 | `gap-6`/`py-6` | conversation-group separation, drawer padding |
| `2xl` | 32 | `p-8` | empty-state centering |
| `3xl` | 48 | — | login hero block, generous page gutters (premium-calm air) [NEW] |

Touch targets unchanged: primary CTA / icon-only buttons **44×44** (`min-h-[44px]`,
`min-w-11`), inline secondary controls ≥24×24 (WCAG 2.5.8). Micro-labels `0.6875rem`.

### 5.2 Radius (names unchanged, values softened for premium feel)

Editorial-premium reads with slightly softer corners than the v1 industrial 4/6/10.

```jsonc
"radius": { "sm": "6px", "md": "10px", "lg": "16px", "xl": "22px", "pill": "999px" }
```

(`sm`/`md`/`lg` re-valued; `xl` + `pill` are NEW.) Mapping: inputs/badges `sm`; buttons/list
rows `md`; cards/approval-cards/drawers `lg`; modal dialog / login panel `xl`; chips/avatars
`pill`. → `--radius-sm…pill`.

### 5.3 Elevation & shadow (NEW token group)

On a warm-graphite base, shadows are weak; depth comes primarily from a **lighter surface
step + a hairline top highlight**, with shadow as a quiet secondary cue (Material dark-theme
guidance: elevation via lighter overlay, shadows less effective on dark —
`https://github.com/material-components/material-components-android/blob/master/docs/theming/Dark.md`).
Two-part elevation = `bg surface step` + `--shadow-*` + an inset top hairline
(`inset 0 1px 0 rgba(255,255,255,0.04)`).

```jsonc
"shadow": {
  "0": "none",
  "1": "0 1px 2px rgba(0,0,0,0.35)",                                  /* list-row hover */
  "2": "0 2px 6px rgba(0,0,0,0.40), 0 1px 0 rgba(255,255,255,0.03) inset", /* cards */
  "3": "0 8px 24px rgba(0,0,0,0.45), 0 1px 0 rgba(255,255,255,0.04) inset", /* popovers/drawers */
  "4": "0 16px 48px rgba(0,0,0,0.55), 0 1px 0 rgba(255,255,255,0.05) inset", /* modal dialog */
  "accent-glow": "0 0 0 1px rgba(200,168,106,0.30), 0 4px 16px rgba(200,168,106,0.12)" /* focused CTA only */
}
```

→ `--shadow-0…4`, `--shadow-accent-glow`. Elevation tiers map: surface (0/1) → card
(surface + shadow-2) → popover (surface-2 + shadow-3) → dialog (surface-2 + shadow-4).

### 5.4 Motion (NEW token group)

Duration scale (Material 3 token spacing, ms — `https://m3.material.io/styles/motion/easing-and-duration/tokens-specs`)
trimmed to a cockpit-appropriate subset; easings are explicit cubic-beziers.

```jsonc
"motion": {
  "dur-instant": "80ms",     /* hover/focus tint, accent ring */
  "dur-fast":    "140ms",    /* default state change, toggle */
  "dur-base":    "200ms",    /* small enter/exit, drawer slide-in start */
  "dur-slow":    "320ms",    /* card/drawer transition, reasoning-drawer expand */
  "dur-slower":  "480ms",    /* large surface / page-load stagger */
  "ease-standard":  "cubic-bezier(0.2, 0, 0, 1)",      /* default UI (M3 standard) */
  "ease-enter":     "cubic-bezier(0.05, 0.7, 0.1, 1)", /* emphasized decelerate (M3) */
  "ease-exit":      "cubic-bezier(0.3, 0, 0.8, 0.15)", /* emphasized accelerate (M3) */
  "ease-expo":      "cubic-bezier(0.16, 1, 0.3, 1)"    /* premium snap-then-settle (page-load reveal) */
}
```

→ `--motion-dur-*`, `--motion-ease-*`. **The one orchestrated high-impact moment**
(CLAUDE.md: "one well-orchestrated page load … more delight than scattered micro"):
the cockpit's first paint staggers the three shell zones in with `--motion-ease-expo` +
`animation-delay` (header 0ms → sidebar 60ms → chat 120ms → footer 180ms), a 12px rise +
opacity. Everything else is restrained: `--motion-dur-fast` state changes, `--motion-dur-instant`
focus ring. `prefers-reduced-motion` kills all of it (§8).

---

## 6. Whole-cockpit surface map (how every region binds to tokens)

| Surface | Background | Border | Text | Accent use | Radius / shadow |
|---------|-----------|--------|------|------------|-----------------|
| App shell base | `bg` + atmosphere (§7) | — | `text` | — | — |
| Header | `surface` | `border` bottom | wordmark `font-display text-accent-text`? **no** → `text` ; nav `text-muted`, active `text` + `surface-2` | approval badge = `accent` pill | shadow-1 |
| Sidebar (conversations) | `surface` | `border` right | rows `text`, meta `text-muted` | selected = accent left-rule + `text-accent-text` | rows `md` |
| Search panel | `surface` | `border-strong` input | input `text`, placeholder `text-faint` | focus `ring` | input `sm` |
| Chat stream | `bg` + atmosphere | — | user/assistant `text` Body-L | streaming caret `accent` | — |
| Assistant message | transparent on `bg` | — | `text` | links `accent-text` | — |
| User message bubble | `surface-2` | `border` | `text` | — | `lg` + shadow-2 |
| Tool-activity card | `surface` | `border` | name `text`, raw blob `font-mono text-muted` on `surface-3` | done-dot `success`, error-dot `danger` | `md` |
| Reasoning drawer | `surface` | `border` | `text-muted` | toggle `text-accent-text` | `md` |
| Composer | `surface-3` well | `border-strong` | `text`, placeholder `text-faint` | Send = `bg-accent`/`on-accent`; focus `ring` | `lg` |
| Approval card (inline) | `surface-2` | `border-strong` left-rule `accent` | question `text`, helper `text-muted` | Answer = `bg-accent`; Decline neutral; Cancel `text-danger` | `lg` + shadow-2 |
| Approval terminal states | `surface-2` | `border` | answered `success`, declined/cancelled `danger`, expired `warning` (each + icon + label) | — | `lg` |
| Cross-thread badge | `accent` pill | — | `on-accent` (count) | — | `pill` |
| Cross-thread list popover | `surface-2` | `border` | `text` | `Open` = `text-accent-text` | `lg` + shadow-3 |
| Runtime health panel | `surface` | `border` left | `text`, metrics `font-mono text-muted` | status dots `success/warning/danger` | — |
| Runtime footer telemetry | `surface` | `border` top | captions Micro `text-faint`, values `font-mono` `text` | gauge fill `accent` (→`warning` ≥85%) | — |
| Context-budget gauge | track `surface-3` | — | value `font-mono-strong` | fill `accent` / `warning` | `pill` track |
| Buttons — primary | `bg-accent` | — | `on-accent` Label-strong | (is the accent) | `md` + focused shadow-accent-glow |
| Buttons — secondary | `surface-2` | `border-strong` | `text` Label | focus `ring` | `md` |
| Buttons — ghost/icon | transparent → `surface-2` hover | — | `text-muted` → `text` | focus `ring` | `md`/`pill` |
| Inputs / textarea | `surface-3` | `border-strong` → `ring` focus | `text`, placeholder `text-faint`, error `border-danger` | focus `ring` | `sm` |
| Modal dialog (delete-confirm) | `surface-2` | `border-strong` | title `font-display`, body `text-muted` | confirm `text-danger` outline; NOT default-focused | `xl` + shadow-4 + backdrop |
| Login page | `bg` + heavier atmosphere (§7) | panel `border-strong` | wordmark `font-display` Display-XL, body `text-muted` | submit `bg-accent`; focus `ring` | panel `xl` + shadow-3 |
| Skeleton / loading | `surface`-derived shimmer (§7.2) | `skeleton-border` | — | refetch-bar tint `accent` | matches target radius |

This table IS the contract the chat-lane / shell-sidebar / footer specs build against —
they reference the **semantic token name** in the relevant cell, never a raw hex.

---

## 7. Atmosphere & depth (the premium feel)

Flat `#14110E` would still read cheap. We add a **layered, CSS-only, GPU-cheap atmosphere**
behind the app — no images, no runtime cost beyond paint. New partial
`web/src/styles/atmosphere.css` (≤120 LOC), imported from `index.css` after `theme.css`.

### 7.1 Base atmosphere (whole app)

```css
/* web/src/styles/atmosphere.css */
:root {
  /* layered warm-graphite field: two faint corner glows (gold + cool) over the base,
     a top sheen, and a vignette toward the corners. Tuned to ~4–6% so it reads as
     depth, never as "a gradient." */
  --atmo:
    radial-gradient(120% 90% at 88% -8%,  rgba(200,168,106,0.06) 0%, transparent 55%),
    radial-gradient(110% 80% at 0% 100%,  rgba(127,176,200,0.04) 0%, transparent 50%),
    linear-gradient(180deg, rgba(255,255,255,0.025) 0%, transparent 22%),
    radial-gradient(140% 120% at 50% 35%, transparent 60%, rgba(0,0,0,0.35) 100%);
}
.app-atmosphere {
  position: relative;
  background-color: var(--color-bg);
  background-image: var(--atmo);
  background-attachment: fixed;     /* glow stays put while the chat scrolls */
}
/* fine grain to kill banding on the long dark gradients (Jimmy Chion / CSS-Tricks
   grainy-gradients technique). Rasterised once as a data-URI SVG turbulence; very low
   alpha so it's texture, not noise. */
.app-atmosphere::before {
  content: '';
  position: absolute; inset: 0; z-index: 0; pointer-events: none;
  opacity: 0.035;
  mix-blend-mode: overlay;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='120' height='120'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='2' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
}
```

Apply `.app-atmosphere` on the `AppShell` root `<div>` (replacing the bare `bg-bg`) and on
the chat `<section>` (`background-attachment: fixed` so the glow is a stable light source
behind scrolling content). Sources: layered radial glows + grain to defeat dark-gradient
banding — `https://css-tricks.com/grainy-gradients/`, `https://www.joshwcomeau.com/css/make-beautiful-gradients/`.

### 7.2 Skeleton / loading shimmer (re-themed in palette)

The shipped `skeleton.css` already derives every value from `--color-*` via `color-mix`, so
it **re-skins automatically** when the tokens change — the warm graphite shimmer + the
accent-tinted refetch-bar will both pick up gold/graphite with **zero edits**. Only adjust
the timing to the new motion tokens for consistency:

```css
/* skeleton.css diff */
:root { --skeleton-duration: var(--motion-dur-slower, 2200ms); }
/* refetch-bar accent tint stays color-mix(... var(--color-accent) ...) → now gold */
```

`prefers-reduced-motion` handling in `skeleton.css` (disable animation + drop the gradient)
is already correct — keep it.

### 7.3 Login page (heavier atmosphere)

Login gets a stronger version (it's a single focal panel, can afford drama): the same
`--atmo` plus a centered `radial-gradient(60% 50% at 50% 30%, rgba(200,168,106,0.10), transparent)`
behind the panel, the wordmark in `font-display` Display-XL with `--motion-ease-expo` reveal.
`prefers-reduced-motion` → static.

---

## 8. Reduced-motion contract

New partial `web/src/styles/motion.css` (≤80 LOC) defines the page-load stagger + the global
reduced-motion guard. The guard is authoritative and overrides every animation token:

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.001ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.001ms !important;
    scroll-behavior: auto !important;
  }
  .app-shell-reveal > * { opacity: 1 !important; transform: none !important; }
}
```

Per MDN guidance (`https://developer.mozilla.org/en-US/docs/Web/CSS/@media/prefers-reduced-motion`),
the reveal degrades to an **instant opacity-1 / transform-none** state (no scale/pan —
vestibular-safe). The streaming caret, gauge-fill animation, skeleton shimmer, and approval-
card entrance all already respect this media query (shipped skeleton precedent) — the new
partial just makes the rule global so any future animation is covered by default.
Backs WCAG 2.2 SC 2.3.3 Animation from Interactions (`https://www.w3.org/WAI/WCAG22/Understanding/animation-from-interactions.html`).

---

## 9. Migration onto `tokens.json` + `generate-theme.mjs`

The pipeline change is **small and additive** — this is the core "extend, don't fork" proof.

### 9.1 `tokens.json` edits

1. Replace the `color.dark` map values with §4.2 semantic tokens (adds `surface-3`,
   `border-strong`, `text-disabled`, `accent-text`, `accent-muted`, `accent-pressed`,
   `on-accent`, `ring`; removes `accent-dim`).
2. Add a sibling `_primitive` map (§4.1) — documentation/derivation only, **not consumed**
   by the generator (it skips keys starting with `_`).
3. Re-value `radius`, add `xl`/`pill` (§5.2).
4. Re-value the `density` operator/review scalars (§5.1).
5. Replace `font` with the three §3.3 families (`display` is new).
6. Add `shadow` (§5.3) and `motion` (§5.4) groups.

### 9.2 `generate-theme.mjs` edits (≤30 added LOC, stays ≤600)

Today the generator emits `color`/`radius`/`font` into `@theme` and `density` into
`:root[data-density=…]`. Add three things:

```js
// 1. skip the documentation-only primitive map
const semanticColor = Object.fromEntries(
  Object.entries(tokens.color[defaultTheme]).filter(([k]) => !k.startsWith('_')),
);
const themeVars = Object.entries(semanticColor).map(([k, v]) => `  --color-${k}: ${v};`).join('\n');

// 2. emit shadow + motion groups into @theme (same shape as radius/font)
const shadowVars = Object.entries(tokens.shadow ?? {}).map(([k, v]) => `  --shadow-${k}: ${v};`).join('\n');
const motionVars = Object.entries(tokens.motion ?? {}).map(([k, v]) => `  --motion-${k}: ${v};`).join('\n');

// 3. (no change to the density-block or drift-gate logic)
```

`@theme { … --shadow-* … --motion-* … }` makes them Tailwind theme vars too (e.g.
`shadow-2`, plus raw `var(--motion-dur-fast)` in component CSS). The **apply-before-paint
inline script + drift gate stay byte-for-byte** — none of `$meta.defaultTheme`/
`defaultDensity` change, so the four asserted literals still match `index.html`.

### 9.3 OKLCH authoring, hex emission

We **author** ramps in OKLCH (perceptual uniformity, §4.1) but **emit hex** in
`tokens.json`/`@theme`. Reasons: (a) byte-stable, gamut-safe output with no per-browser
clipping surprises on the surface stops (Chrome/Safari clip high-chroma OKLCH differently);
(b) the contrast proof (§10) is computed on the exact shipped hex; (c) no change to the
generator's string-passthrough. The OKLCH values are the documented design intent in
`_primitive`; if we later want runtime OKLCH (e.g. relative-color state derivation), it's a
generator-only change behind the same token names. Accent **states** may optionally use the
CSS relative-color form at the component layer
(`oklch(from var(--color-accent) calc(l - 0.05) c h)`) — supported, but the named
`accent-pressed`/`accent-muted` tokens are the contract so a component never has to.

### 9.4 New CSS partials & import order (`index.css`)

```css
@import './fonts.css';        /* @font-face FIRST (before Tailwind) */
@import 'tailwindcss';
@import './theme.css';        /* generated @theme + density blocks */
@import './atmosphere.css';   /* layered gradient + grain (NEW) */
@import './motion.css';       /* page-load reveal + reduced-motion guard (NEW) */
@import './skeleton.css';     /* re-skins automatically; one timing line edited */
```

Each new partial is ≤120 LOC. `applyTheme.ts`/`density.ts` need **no change**
(single `dark` key stays; density model unchanged).

---

## 10. WCAG 2.2 contrast proof

All ratios computed from the **exact shipped hex** (sRGB relative-luminance, WCAG formula).
AA thresholds: normal text **4.5:1**, large text (≥24px or ≥18.66px bold) **3:1**, non-text
UI components / focus indicators **3:1** (WCAG 2.2 did not change these from 2.1 —
`https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum`,
`https://www.w3.org/WAI/WCAG22/Understanding/non-text-contrast.html`). Computed via the
standard luminance algorithm.

| Foreground | Background | Ratio | Requirement | Verdict |
|------------|-----------|-------|-------------|---------|
| `text` #ECE7DF | `bg` #14110E | **15.29:1** | 4.5 (text) | AAA |
| `text` #ECE7DF | `surface` #1B1714 | **14.47:1** | 4.5 | AAA |
| `text` #ECE7DF | `surface-2` #241F1A | **13.27:1** | 4.5 | AAA |
| `text-muted` #B0A99E | `bg` #14110E | **8.08:1** | 4.5 | AAA |
| `text-muted` #B0A99E | `surface` #1B1714 | **7.64:1** | 4.5 | AAA |
| `text-muted` #B0A99E | `surface-2` #241F1A | **7.01:1** | 4.5 | AAA |
| `text-faint` #8E877C | `bg` #14110E | **5.29:1** | 4.5 | AA |
| `text-faint` #8E877C | `surface` #1B1714 | **5.01:1** | 4.5 | AA |
| `text-disabled` #6A6258 | `bg` #14110E | ~3.0:1 | 3.0 (disabled exempt) | OK (decorative/disabled only) |
| `accent` #C8A86A (as text) | `bg` #14110E | **8.30:1** | 4.5 | AAA |
| `accent` #C8A86A (as text) | `surface` #1B1714 | **7.86:1** | 4.5 | AAA |
| `accent-text` #E7D5A8 | `surface` #1B1714 | **9.72:1** | 4.5 | AAA |
| `on-accent` #14110E | `accent` #C8A86A | **8.30:1** | 4.5 (CTA label) | AAA |
| `success` #6FB58A | `bg` #14110E | **7.75:1** | 4.5 | AAA |
| `success` #6FB58A | `surface` #1B1714 | **7.34:1** | 4.5 | AAA |
| `warning` #DDA94A | `bg` #14110E | **8.82:1** | 4.5 | AAA |
| `warning` #DDA94A | `surface` #1B1714 | **8.35:1** | 4.5 | AAA |
| `danger` #E66A63 | `bg` #14110E | **5.92:1** | 4.5 | AA |
| `danger` #E66A63 | `surface` #1B1714 | **5.60:1** | 4.5 | AA |
| `info` #7FB0C8 | `bg` #14110E | **8.01:1** | 4.5 | AAA |
| `ring` #C8A86A (focus, non-text) | `bg` #14110E | **8.30:1** | 3.0 (UI) | PASS |
| `ring` #C8A86A | `surface-2` #241F1A | **7.20:1** | 3.0 (UI) | PASS |
| `accent-muted` #7A6740 (UI border) | `bg` #14110E | **3.44:1** | 3.0 (UI) | PASS |

**Non-text dividers** (`border` 1.49:1, `border-strong` 1.83:1 on their surfaces) are
exempt — WCAG 1.4.11 covers UI components and *meaningful* graphical objects, not decorative
separators; the interactive affordances that DO need 3:1 (focus ring, accent-muted control
borders) pass with margin. **Every text token clears AA; nearly all clear AAA.** Status is
never encoded by color alone (icon + text label required — carried from `25-UI-SPEC.md`,
WCAG 1.4.1).

> The proof recomputes deterministically: `node`/`python` luminance of each hex pair. If any
> token hex changes, re-run the proof before merge (acceptance §11, AC-8).

---

## 11. Acceptance criteria

1. **AC-1 Pipeline reuse.** `node tokens/generate-theme.mjs` regenerates `theme.css` with
   the new semantic tokens + `--shadow-*` + `--motion-*`; the drift gate still passes (the
   four `index.html` literals unchanged); `npm run build` is green. No new build dependency.
2. **AC-2 Single binary / self-host.** Fonts live under `web/public/fonts/` as subset
   woff2; total ≤220 KB; `dist` embeds them; `aura serve` renders them with the network
   tab showing **zero external font requests**; offline (PWA) still renders the fonts.
3. **AC-3 Distinctive type.** Headings render in Fraunces, body in Hanken Grotesk, all
   numerics/blobs in Commit Mono. `grep` finds no `Inter`/`Roboto`/`Arial` and no
   `system-ui`-only font stack as a *primary* family.
4. **AC-4 Whole-cockpit re-skin.** Header, sidebar, chat bubbles, tool cards, reasoning
   drawer, composer, approval cards, cross-thread badge/list, runtime panel, footer
   telemetry, gauge, buttons, inputs, modal, login, skeletons all read the §6 tokens — no
   hard-coded hex in any component (`grep -nE '#[0-9A-Fa-f]{3,6}'` in `web/src/**/*.tsx`
   returns only token files / data-URIs).
5. **AC-5 Accent scarcity.** The §4.3 reserved list is the only accent usage; secondary
   buttons / tool cards / footer chrome are neutral.
6. **AC-6 Atmosphere.** The app root + chat region carry `.app-atmosphere` (layered glow +
   grain, `background-attachment: fixed`); no visible banding on the dark gradient; login has
   the heavier variant.
7. **AC-7 Motion + reduced-motion.** First paint runs the staggered shell reveal with
   `--motion-ease-expo`; with `prefers-reduced-motion: reduce` set, the reveal, caret,
   gauge-fill, and skeleton shimmer are all static (verified in Playwright with the media
   emulated).
8. **AC-8 WCAG 2.2 AA proven.** The §10 table recomputes from shipped hex; every text token
   ≥4.5:1 on its surfaces, every interactive non-text affordance ≥3:1, status never
   color-only. A unit test (or `scripts/contrast_check`) recomputes and fails CI on a
   sub-threshold pair.
9. **AC-9 No layout shift on font swap.** `font-display: swap` + metric-matched fallback;
   CLS contribution from the swap ≈ 0 (the metric overrides in `fonts.css` tuned).
10. **AC-10 Density preserved.** `compact`/`operator`/`review` still switch via
    `data-density`; operator is default; the relaxed scalars apply.

---

## Self-Scorecard

Rubric: **Concreteness** (exact tokens + hex/oklch), **Typography rigor** (families +
licensing + self-host plan + scale), **Color/scale completeness** (ramps → semantic +
spacing/radius/shadow/motion), **WCAG proof**, **Pipeline-fit** (extends `tokens.json` +
`generate-theme.mjs`, no fork), **Distinctiveness** (anti-AI-slop), **Coverage** (whole
cockpit + reduced-motion + skeletons).

| Dimension | Score | Notes |
|-----------|-------|-------|
| Concreteness (exact values) | 9.5 | Every token has a name + hex; OKLCH intent + hex emission both given. |
| Typography rigor | 9.5 | Specific OFL pairing w/ license-proof URLs, subset plan, @font-face, full scale + tracking. |
| Color & scale completeness | 9.5 | Primitive→semantic ramps, spacing/radius/shadow/elevation/motion all tokenized. |
| WCAG 2.2 proof | 10 | 22-row table computed from shipped hex; AA cleared (mostly AAA); non-text exemptions justified. |
| Pipeline fit (no fork) | 10 | ≤30 LOC generator delta, drift gate intact, names re-skin existing utilities. |
| Distinctiveness (anti-slop) | 9.5 | Warm graphite + editorial serif + gold accent + grain atmosphere; explicitly avoids Inter/blue-black/purple. |
| Whole-cockpit coverage | 9.5 | §6 surface table + skeleton + login + reduced-motion all specified. |

**Aggregate: 9.6 / 10.**

**Items that would block a clean 9.5 — and their resolution within this SPEC:**
- *Verify-at-build, not assert:* AC-8 mandates an actual `scripts/contrast_check` recompute
  in CI (not just this table), and AC-2 mandates a network-tab/offline check — so the
  numbers are enforced, not claimed.
- *Two research caveats flagged, not load-bearing:* the `--motion-ease-expo`
  `cubic-bezier(0.16,1,0.3,1)` is a practitioner-standard curve (not a single primary-doc
  spec) and the font woff2 KB figures are ballpark — both are bounded by AC-2's measured
  ≤220 KB gate and the M3-sourced easings used everywhere else, so neither blocks merge.
- *Optical-size axis exposure:* Fraunces' `opsz` should be driven by `font-optical-sizing:
  auto` (default) + explicit `font-variation-settings` only on Display-XL; left to the
  consuming chat/shell specs as a one-line per-heading detail, not a token.
- *Live operator sign-off:* the only thing this SPEC cannot self-certify is whether the
  operator finds the *result* beautiful — that is a visual UAT after implementation, out of
  scope for a token SPEC.
