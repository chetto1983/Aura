---
phase: 26-typed-display-protocol-router
plan: 05
subsystem: ui
tags: [react, typescript, citations, rehype, shiki, image-proxy, ssrf, xss, code-split, a11y, i18n, vitest, stryker]

# Dependency graph
requires:
  - phase: 26-typed-display-protocol-router (plan 02)
    provides: "DisplayRouter switch shell + default raw-card fallback, DisplayPayload TS wire types (WebItem/DisplayDocument/DisplayCode/DisplaySource), DisplayPagination, DisplayCardShell, useCopyAction"
  - phase: 26-typed-display-protocol-router (plan 03)
    provides: "GET /api/image-proxy?url=… (SSRF-safe, RequireAuth) — the WebResultDisplay <img> route + query-param contract"
  - phase: 26-typed-display-protocol-router (plan 04)
    provides: "the data/status cards (table/chart/system_event/swarm_report/local_artifact) above the preserved default — 26-05 adds the evidence half"
provides:
  - "web/src/chat/displays/rehypeCitations.ts: inline-splice citation rehype plugin (fixes elysia end-append bug); registry-backed (unknown [n] stays literal, T-26-18)"
  - "web/src/chat/displays/CitationBubble.tsx: Radix hovercard numbered chip — focus+tap accessible, cited=accent/consulted=neutral, onOpenSource(refId) callback"
  - "web/src/chat/MarkdownText.tsx + markdownConfig.tsx: shared markdown pipeline (one sanitize chokepoint) hosting rehypeCitations via internal merge"
  - "web/src/chat/displays/DocumentDisplay.tsx: sanitized markdown (react-markdown) + inline citations + images + Copy text"
  - "web/src/chat/displays/WebResultDisplay.tsx: rich web_result card with image-proxy thumbnails + citations + in-card pagination (no raw external img)"
  - "web/src/chat/displays/CodeDisplay.tsx + shiki.ts: lazy escaped-span Shiki highlighter (code-split chunk) + copy + collapse"
  - "DisplayRouter cases: web_result, document, code (all 8 types now wired above the default)"
affects: [26-06 (Source Explorer wires onOpenSource(refId); dist rebuild)]

# Tech tracking
tech-stack:
  added:
    - "@radix-ui/react-hover-card@1.1.17 (MIT, exact-pinned) — citation hovercard chrome (D-04)"
    - "shiki@4.2.0 (MIT, exact-pinned) — lazy escaped-span code highlighter (D-10), fine-grained core + JS regex engine, code-split"
    - "react-markdown@10.1.0 (MIT, exact-pinned) — declared the already-locked engine of @assistant-ui/react-markdown so DocumentDisplay can render a standalone content_md string"
  patterns:
    - "Shared markdown pipeline (markdownConfig.tsx): the sanitize schema + base components + buildRehypePlugins live ONCE; the streaming chat host (MarkdownText) and the standalone DocumentDisplay both consume it — identical render, single chokepoint, rehype-sanitize ALWAYS last (T-26-17)"
    - "Registry-backed citation splice: rehypeCitations resolves [n] against the code-owned source registry by index; an unknown number is left as literal text (the model cannot fabricate a citation target, T-26-18)"
    - "Image-proxy as the ONLY external-image path: WebResultDisplay routes every thumbnail through /api/image-proxy?url=… (referrerpolicy=no-referrer + lazy); a broken image degrades to alt + domain chip; a test asserts no raw external <img src> (T-26-15)"
    - "Lazy code-split highlighter: shiki.ts pulls core + JS engine + selective grammars + themes behind dynamic import() so the 915kB Shiki chunk stays OFF the embedded critical-path bundle; CodeDisplay shows a plain escaped <pre> first and upgrades on resolve (A2, escaped spans never execute — T-26-16)"

key-files:
  created:
    - web/src/chat/displays/rehypeCitations.ts
    - web/src/chat/displays/CitationBubble.tsx
    - web/src/chat/displays/DocumentDisplay.tsx
    - web/src/chat/displays/WebResultDisplay.tsx
    - web/src/chat/displays/CodeDisplay.tsx
    - web/src/chat/displays/shiki.ts
    - web/src/chat/markdownConfig.tsx
    - web/src/chat/displays/__tests__/rehypeCitations.test.ts
    - web/src/chat/displays/__tests__/CitationBubble.test.tsx
    - web/src/chat/displays/__tests__/DocumentDisplay.test.tsx
    - web/src/chat/displays/__tests__/WebResultDisplay.test.tsx
    - web/src/chat/displays/__tests__/CodeDisplay.test.tsx
    - web/src/chat/displays/__tests__/shiki.test.ts
  modified:
    - web/package.json
    - web/package-lock.json
    - web/src/chat/MarkdownText.tsx
    - web/src/chat/displays/DisplayRouter.tsx
    - web/src/chat/displays/__tests__/DisplayRouter.test.tsx
    - web/src/chat/__tests__/ExternalStoreChat.test.tsx
    - web/src/i18n/resources.display.ts
    - web/stryker.config.json
    - web/vite.config.ts

key-decisions:
  - "react-markdown@10.1.0 declared as a DIRECT dep (was an already-locked transitive of @assistant-ui/react-markdown): MarkdownTextPrimitive structurally cannot render a standalone string (it reads the current message part), so DocumentDisplay renders content_md via react-markdown reusing the SAME shared markdownConfig pipeline. No new download/supply-chain surface — promotes an already-present, already-audited engine to declared status (Deviation 1)"
  - "extracted markdownConfig.tsx as the single markdown source of truth (sanitize schema + components + plugin assembly) rather than forking a second markdown component (T-26-17 / OQ3): MarkdownText (streaming chat host) and DocumentDisplay (standalone string) both consume it; rehype-sanitize is appended LAST by buildRehypePlugins so an injected plugin can never bypass the chokepoint"
  - "CitationBubble is a controlled Radix HoverCard: hover+focus open natively; a tap/click ALSO sets open AND fires onOpenSource — so the preview is reachable without a pointer (hover is never the only path, D-16)"
  - "shiki uses the JS regex engine (createJavaScriptRegexEngine), NOT oniguruma WASM — smaller chunk, jsdom-testable; selective grammar allow-list (12 langs + aliases), unknown lang → plain escaped <pre> (graceful degrade)"
  - "micro-labels use text-[0.75rem] (11.6px), NOT the UI-SPEC's text-[0.6875rem] (10.66px): the committed readabilityTokens gate enforces rem*15.5 ≥ 11 (Deviation 2, same as 26-02)"

patterns-established:
  - "DisplayRouter now carries all 8 cases above the preserved default raw-card fallback (HARDEN-08 / D-FALLBACK intact)"
  - "Citation chip contract for 26-06: CitationBubble onOpenSource(refId: string) is the callback the Source Explorer wires — DocumentDisplay + WebResultDisplay both forward an optional onOpenSource prop; the DisplayRouter does not yet thread it (26-06 wires the Source Explorer open)"

requirements-completed: [DISP-02, DISP-03]

# Metrics
duration: ~35min
completed: 2026-06-18
---

# Phase 26 Plan 05: Typed-Display Evidence Cards + Citation Pipeline Summary

**The display router now renders the three "evidence" types — `web_result` (domain chip + snippet + relevance meter + published_at + citation bubbles + thumbnails through the SSRF-safe image-proxy + in-card pagination), `document` (sanitized markdown with inline citations + images), and `code` (lazy escaped-span Shiki highlight in a code-split chunk + copy + collapse) — plus the full citation pipeline (an inline-splice `rehypeCitations` rehype plugin, a focus+tap-accessible Radix `CitationBubble`, and a refactor of `MarkdownText` onto a shared markdown config that hosts the plugin via an internal merge), behind the two operator-approved exact-pinned MIT deps. All 8 router cases are now wired above the preserved raw-card fallback.**

## Performance
- **Duration:** ~35 min
- **Started:** 2026-06-18T19:26:02Z
- **Completed:** 2026-06-18T20:01:18Z
- **Tasks:** 3
- **Files:** 22 (13 created + 9 modified)

## Accomplishments

- **Task 1 (supply-chain checkpoint):** installed `@radix-ui/react-hover-card@1.1.17` + `shiki@4.2.0` exact-pinned (no caret). Operator-approved 2026-06-18, npm-registry-verified (both MIT, repos radix-ui/primitives + shikijs/shiki, no install/postinstall/preinstall scripts), 0 vulnerabilities. `@radix-ui/react-hover-card` was already physically present in node_modules (transitive via the radix-ui meta-package); shiki was the only net new download.
- **Task 2 (citation pipeline + DocumentDisplay):** `rehypeCitations` walks `text` nodes, matches `/\[(\d+)\]/g`, and splices a `<span data-ref-id data-citation-number>` chip INLINE at the claim position (dropped elysia's `processTextWithCitations` end-append bug); an `[n]` not in the registry stays literal text (T-26-18). `CitationBubble` is a Radix hovercard `<button>` chip — accent when cited, neutral when consulted — opening on hover+focus+tap and firing `onOpenSource(refId)` on click. `MarkdownText` was refactored onto a new shared `markdownConfig.tsx` (sanitize schema + base components + plugin assembly) and accepts `extraRehypePlugins`/`extraComponents` via an internal merge with rehype-sanitize always last (T-26-17). `DocumentDisplay` renders `content_md` via react-markdown through that pipeline with the citation plugin + the citation `span` renderer, renders images (dropped elysia's `prose-img:hidden`), and copies the raw markdown.
- **Task 3 (WebResultDisplay + CodeDisplay):** `WebResultDisplay` renders each `WebItem` as a rich card (domain chip + snippet + an INFO-tinted relevance meter + published_at + a citation bubble) with thumbnails routed through `/api/image-proxy?url=…` (referrerpolicy=no-referrer + loading=lazy; broken-image degrade to alt + domain chip) and in-card pagination at 3/page — a test asserts NO raw external `<img src="http…">` (T-26-15). `CodeDisplay` shows a plain escaped `<pre>` immediately, then upgrades to the lazy-Shiki escaped-span highlight once the code-split chunk resolves (a `<script>` body renders as escaped text, never executes — T-26-16), with `Copy code` + collapse-long-body. `shiki.ts` is the lazy loader (fine-grained core + JS regex engine + selective grammars/themes behind dynamic `import()`); `vite.config.ts` forces the Shiki chunk into its own code-split group.

## Image-proxy contract consumed (from 26-03)
WebResultDisplay's only external-image path is:
`<img src="/api/image-proxy?url={encodeURIComponent(thumbnail)}" referrerPolicy="no-referrer" loading="lazy" alt={title}>`. A broken load (`onError`) hides the `<img>` and the card degrades to alt + domain chip. No raw external host ever reaches an `<img src>`.

## CitationBubble onOpenSource(refId) callback contract (for 26-06)
`CitationBubble` exposes `onOpenSource?: (refId: string) => void`, fired on chip click (after opening the hovercard). `DocumentDisplay` and `WebResultDisplay` both forward an optional `onOpenSource` prop down to each chip. **26-06 wires the Source Explorer open** by threading an `onOpenSource` handler from `ExternalStoreChat` → `DisplayRouter` → the per-type evidence cards (the DisplayRouter currently does not pass it, so chips open their hovercard preview without a destination until 26-06 lands the Source Explorer sheet).

## Shiki chunk-size measurement (A2)
`npm run vite build` (to a throwaway out dir, the committed `internal/webui/dist` was NOT touched — that rebuild is 26-06's) produced:
- `shiki-BM9IzjFr.js` — **915.52 kB (151.38 kB gzip)** as a **SEPARATE code-split chunk**.
- main `index-*.js` 333.84 kB (105.91 kB gz); `ExternalStoreChat-*.js` 506.04 kB (151.06 kB gz) — neither contains Shiki.

Shiki is therefore entirely off the embedded critical-path bundle; it is fetched lazily the first time a `code` display renders. The chunk is named via `build.rolldownOptions.output.codeSplitting.groups` (`{ name: 'shiki', test: /node_modules\/(shiki|@shikijs)\// }`).

## Quality gates
- `npm run typecheck` clean; `npm run lint` clean (jsx-a11y on every chip/button).
- Full Vitest **440/440** green, coverage **93.8% stmts / 85.53% branch / 96.03% funcs / 95.63% lines** (≥85% floor held). Per new surface: shiki.ts 97.7%, CodeDisplay.tsx 96.9%, rehypeCitations/CitationBubble/DocumentDisplay/WebResultDisplay/markdownConfig all fully covered (not listed = 100%).
- Stryker mutation (≥70% break): **rehypeCitations.ts 85.0%**, **shiki.ts 94.2%** (the two logic-bearing new surfaces in the mutate list); overall 89.29% killed.
- Build confirms the Shiki code-split (above).

## Task Commits
1. **Task 1: add @radix-ui/react-hover-card + shiki (operator-approved deps)** — `fd86d421` (chore)
2. **Task 2: citation pipeline + DocumentDisplay** — `8244f7ca` (feat)
3. **Task 3: WebResultDisplay (image-proxy) + CodeDisplay (lazy Shiki)** — `8434a085` (feat)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Declared react-markdown@10.1.0 as a direct dep to render a standalone document string**
- **Found during:** Task 2 (DocumentDisplay)
- **Issue:** The plan says "DocumentDisplay using the refactored MarkdownText", but `MarkdownTextPrimitive` (the assistant-ui host MarkdownText wraps) structurally CANNOT render an arbitrary string — it reads the current assistant-ui message part. DocumentDisplay has a plain `content_md` string. The underlying markdown engine, `react-markdown@10.1.0`, was already locked (a direct dep of `@assistant-ui/react-markdown`, which we depend on directly) but not declared, so importing it directly was a phantom dependency.
- **Fix:** Declared `react-markdown@10.1.0` exact-pinned (`npm install --save-exact` added 0 packages — it was already present). Extracted a shared `markdownConfig.tsx` (sanitize schema + components + plugin assembly) consumed by BOTH `MarkdownText` (the streaming host) and `DocumentDisplay` (react-markdown over the string) so they render identically through ONE sanitize chokepoint — not a forked component (honors T-26-17). The package is MIT, already-audited (it ships inside our existing assistant-ui dep), and has no install scripts — no new supply-chain surface beyond the two checkpoint-approved deps.
- **Files modified:** web/package.json, web/package-lock.json, web/src/chat/markdownConfig.tsx (new), web/src/chat/MarkdownText.tsx, web/src/chat/displays/DocumentDisplay.tsx
- **Commit:** 8244f7ca

**2. [Rule 1 - Bug] Micro-label font size violated the enforced readability gate**
- **Found during:** Task 2 (full-suite run)
- **Issue:** I used `text-[0.6875rem]` (11px per the UI-SPEC) in CitationBubble. The committed `readabilityTokens.test.ts` gate requires arbitrary-rem text utilities to resolve to ≥11px at the 15.5px operator base (`rem*15.5 ≥ 11` → `rem ≥ 0.71`); `0.6875rem` = 10.66px FAILED — the same gate 26-02 hit.
- **Fix:** Replaced all `text-[0.6875rem]` with `text-[0.75rem]` (11.6px effective) in CitationBubble, the shipped post-readability micro-label floor.
- **Files modified:** web/src/chat/displays/CitationBubble.tsx
- **Commit:** 8244f7ca

**3. [Rule 1 - Test update] Updated two prior-wave tests pinned to the superseded Wave-1 shell behavior**
- **Found during:** Task 2 (DisplayRouter test) + Task 3 (ExternalStoreChat test, full-suite run)
- **Issue:** `DisplayRouter.test.tsx` and `ExternalStoreChat.test.tsx` (written in 26-02) asserted that a `web_result` payload falls through to the RAW ToolActivityCard, because the router was a shell with no per-type case. 26-05 intentionally wires `web_result` → WebResultDisplay, so those assertions described behavior that has changed by design.
- **Fix:** Updated both tests to assert the rich WebResultDisplay render (the result snippet + the "Web results" label) instead of the raw card. The behavior change is the plan's deliverable, not a regression; the test was rewritten with explicit justification (CLAUDE.md: test rewrite allowed when the test itself describes superseded behavior).
- **Files modified:** web/src/chat/displays/__tests__/DisplayRouter.test.tsx, web/src/chat/__tests__/ExternalStoreChat.test.tsx
- **Commit:** 8244f7ca (DisplayRouter) / 8434a085 (ExternalStoreChat)

---
**Total deviations:** 3 auto-fixed (1 blocking dep-declaration, 1 readability-gate bug, 1 prior-wave test update for intended behavior change). No architectural changes; no scope creep — the three evidence cards + the citation pipeline landed exactly as specified, plus the dependency-hygiene declaration required to render a standalone document string.

## Known Stubs
None. All three evidence cards render real payload data. The `onOpenSource(refId)` callback is intentionally NOT yet threaded from the DisplayRouter — the Source Explorer it opens is 26-06 scope (documented above as the 26-06 hand-off contract), not a data stub: the citation chips fully render their hovercard preview today; only the click-through destination awaits 26-06.

## Threat Model Adherence
- **T-26-15 (client-side SSRF via external img → mitigate):** WebResultDisplay routes every thumbnail through `/api/image-proxy?url=…` (referrerpolicy=no-referrer + lazy); a test asserts no raw external `<img src="http…">` to an arbitrary host; broken image degrades to alt + domain chip.
- **T-26-16 (XSS via code display → mitigate):** CodeDisplay's plain fallback is a React-escaped `<pre>` text node; the Shiki upgrade emits HTML-entity-escaped spans (a `<script>` body becomes `&#x3C;script&#x3E;` text, never a live element) — asserted in both CodeDisplay and shiki tests.
- **T-26-17 (XSS via document markdown → mitigate):** the shared markdownConfig keeps the existing rehype-sanitize + markdownSanitizeSchema chokepoint; buildRehypePlugins appends sanitize LAST so an injected plugin can never bypass it; the schema only WIDENS by two inert citation data-* attributes. A test feeds a `<script>` markdown body and asserts it is stripped + window not polluted.
- **T-26-18 (citation spoofing → mitigate):** rehypeCitations resolves `[n]` against the code-owned source registry by index; an unknown number renders no live chip (stays literal text) — asserted in rehypeCitations + DocumentDisplay tests.
- **T-26-SC (supply chain → mitigate):** the two net-new deps are exact-pinned MIT, behind the blocking operator-approved package-legitimacy checkpoint (Task 1), no install scripts; Shiki is code-split to bound bundle weight.

## Threat Flags
None — no new security-relevant surface beyond the plan's `<threat_model>`. The image-proxy egress is the backend's (26-03) inside the existing SSRF guard; the frontend only constructs the proxied URL.

## Issues Encountered
- `react-markdown` had to be declared directly (Deviation 1) — `MarkdownTextPrimitive` cannot render a standalone string.
- The `AppShell.conversation.test.tsx` parallel-load flake (carried from 26-02/26-04) surfaced once under full-coverage CPU load and passed on re-run; it is unrelated to this plan's changes (no AppShell files touched).
- Shiki escapes `<` as the hex entity `&#x3C;` (not `&lt;`); the T-26-16 assertion was written against the actual escaping.

## User Setup Required
None.

## Self-Check: PASSED

All 13 created files exist on disk; all 3 task commits (`fd86d421`, `8244f7ca`, `8434a085`) are present in git history; all three evidence `case` branches (`web_result`, `document`, `code`) are in DisplayRouter.tsx; both deps are exact-pinned in web/package.json. Re-run: `npm run typecheck` clean, `npm run lint` clean, full Vitest 440/440 with coverage ≥85% on every metric, Stryker ≥70% (rehypeCitations 85.0% / shiki 94.2%), and the Shiki chunk is code-split (915kB/151kB gz, separate, off the main bundle).

---
*Phase: 26-typed-display-protocol-router*
*Completed: 2026-06-18*
