---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 16
subsystem: ui
tags: [react, react-router, i18n, share, artifacts, xss, sandboxed-iframe]

# Dependency graph
requires:
  - phase: 37F-05
    provides: "AssetSourceContext/useAssetSource (R-05 seam), shareTypes.ts (Snapshot/SnapshotTurn/SnapshotArtifact mirror), resources.share.ts (share i18n module)"
provides:
  - "web/src/routes/SharePage.tsx — the read-only snapshot page serving BOTH tiers (/s/:token public, /shared/:id internal bearer-within-auth) from one component"
  - "web/src/main.tsx — the two lazy /s/:token and /shared/:id routes"
  - "A fixed R-05 seam gap: web/src/chat/artifacts/useBlobPreview.ts (behind ImagePreview/PdfPreview) now resolves through useAssetSource() instead of a hardcoded identity-scoped URL — the share page's image/pdf artifacts were structurally broken without this"
  - "New share.public.{loading,error,turn.*,artifacts.*} i18n keys (en+it) in resources.share.ts"
affects: [37F-17]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "One component, two tiers: SharePage takes a `tier` prop from the <Route> element (never derived from the URL string); tier only changes the fetch URL and the AssetSourceContext provider value (dataRequestFor/assetSourceFor, an explicit inverse pair with a shared doc comment) — every renderer, the not-found body, and the no-identity posture stay identical for both tiers (D-07 one-canonical-snapshot)."
    - "Oracle-free failure collapse: any !res.ok status (404 unknown/expired/revoked/foreign-tier, 401 anonymous-on-internal) and any thrown fetch/parse error map to ONE of two render states (notFound vs. a generic network error) — the status code is read only for ok/not-ok, never branched further."
    - "Derived render-time short-circuit instead of a synchronous effect setState: an empty route param renders the not-found shell directly during render (not via a state transition dispatched synchronously inside useEffect), which is also what satisfies eslint-plugin-react-hooks's newer `set-state-in-effect` rule."
    - "Icon-selection split into its own component (ArtifactGlyph) so `categoryIcon()`'s call-expression result is only ever passed down as a prop and rendered by a component that never itself computes it — mirrors ArtifactRow.tsx's IconTile and is what satisfies eslint-plugin-react-hooks's `static-components` rule (a JSX tag whose binding is both computed AND rendered in the same scope is flagged as \"a component created during render\", even when the callee is a stable, already-declared Lucide icon)."

key-files:
  created:
    - web/src/routes/SharePage.tsx
    - web/src/routes/SharePage.test.tsx
  modified:
    - web/src/main.tsx
    - web/src/chat/artifacts/useBlobPreview.ts
    - web/src/i18n/resources.share.ts

key-decisions:
  - "Tier is taken from a `tier` prop set by the two <Route> elements in main.tsx, not derived from the URL/pathname (per the plan's own 'pick one and state it' instruction)."
  - "Turn text renders as a plain React child (`{turn.text}` inside a whitespace-pre-wrap <p>), never through the assistant-ui MarkdownText/react-markdown pipeline used by the live chat — the objective's 'reuse the same renderer' principle applies to the 37B ARTIFACT renderers (explicitly named throughout the plan), not to turn text; markdown-processing turn text would add an HTML-generation surface the XSS truth ('renders as text — no element is created from it') does not require and the plan never asks for."
  - "Artifacts render inline (no click-to-open PreviewModal): every artifact's previewKind-dispatched renderer mounts directly in its own bordered block on page load, because a public recipient has no interaction model to open a modal from and the plan's behavior rows describe artifacts rendering unconditionally, not on demand."
requirements-completed: [WEBSHARE-02, WEBSHARE-03]

# Metrics
duration: ~45min
completed: 2026-07-17
---

# Phase 37F Plan 16: SharePage — One-Component Read-Only Snapshot Renderer for Both Share Tiers Summary

**A single `SharePage` component serving `/s/:token` (anonymous, `credentials: 'omit'`) and `/shared/:id` (bearer-within-auth, `credentials: 'same-origin'`), reusing every 37B artifact renderer unedited through the R-05 asset-source seam, with an oracle-free 404/401 and a fixed image/pdf seam gap**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-07-17T15:35Z (approx.)
- **Completed:** 2026-07-17T16:17Z
- **Tasks:** 2 planned, both completed as specified
- **Files modified:** 5 (2 created, 3 modified)

## Accomplishments

- `SharePage.tsx`: one component renders BOTH the public (`/s/:token`) and internal (`/shared/:id`) share tiers. `dataRequestFor`/`assetSourceFor` are an explicit inverse pair (`credentials: 'omit'` vs `'same-origin'`), documented adjacently so a future "simplification" can't silently break one tier. Renders the snapshot title, date, turns (role + escaped text + tool-name provenance chips), and artifacts — reusing `ImagePreview`/`PdfPreview`/`TextPreview`/`HtmlPreview`/`DocxPreview`/`XlsxPreview` completely unedited via an `AssetSourceContext.Provider` whose value is tier-scoped. No owner name/avatar/identity (the `Snapshot` type structurally carries none), no composer/regenerate/continue/clone affordance, no redirect-home on a miss.
- Oracle-free failure handling: unknown, expired, revoked, a foreign-tier id, and an anonymous 401 on the internal route all collapse into the SAME not-found render (proven byte-identical across three different public tokens AND across the public-404/internal-401 pair in tests). Network errors (a rejected `fetch()` or a malformed response body) render a distinct `role="alert"` state instead of the not-found body — the plan's third behavior row.
- Security assertions pinned exactly as the plan's acceptance criteria demand: the HTML iframe's `sandbox` attribute equals `allow-scripts` with `allow-same-origin` asserted ABSENT (not a substring check), fed via `srcdoc` not `src`; an SVG artifact renders through the `previewKind` download gate with a scoped assertion that NO `<svg>`/`<img>` exists inside the artifact's own preview body (a page-wide query would false-positive on the Lucide icon SVGs elsewhere on the page); an `<img src=x onerror=alert(1)>` payload in turn text is asserted to produce zero `<img>` elements in the DOM.
- `main.tsx`: registered `/s/:token` (public) and `/shared/:id` (internal) as lazy routes with an inline comment stating, for both, that the server's 404/401 — not the router — is the real gate, and that `/shared/:id` is deliberately outside the `/s/` prefix.
- **Found and fixed a real gap in the R-05 seam** (see Deviations): `useBlobPreview.ts`, the hook behind `ImagePreview`/`PdfPreview`, still hardcoded `/api/assets/{id}/download` + `credentials: 'same-origin'` — plan 37F-05 re-pointed `useAssetContent.ts` (behind Text/Html/Docx/Xlsx) but never touched this sibling hook. Without the fix, every image or PDF artifact on either share tier would have silently 401/404'd against the wrong, identity-scoped route. Fixed by re-pointing through `useAssetSource()`; the existing `useBlobPreview.test.ts` (11 tests) passes completely unedited, because its no-provider-mounted default is byte-identical to the previous hardcoded behavior.
- 21 tests in `SharePage.test.tsx` cover every `<behavior>` row: both tiers' fetch URL + credentials, both tiers' asset URL + credentials, turn rendering with/without tool provenance, all seven `previewKind` branches (image/pdf/text/html/docx/xlsx/download), the XSS-as-text assertion, the oracle-free 404×3 and 404-vs-401 byte-equality, loading/error/malformed-response states, absence of any interactive/textbox affordance, percent-encoding of the route key, and a defensive-fallback test for a mismatched tier/route-param pairing.
- `web/src/routes/SharePage.tsx` coverage: **100% statements, 100% lines, 100% functions, 94.87% branches** — well above the 85% floor.
- Full project vitest suite (181 files, 1476 tests) green; `tsc --noEmit` clean; `eslint` clean (0 errors) on every touched/created file, including the newer `react-hooks/set-state-in-effect` and `react-hooks/static-components` rules; `prettier --check` clean; `check-file-size.sh` clean (SharePage.tsx 337 LOC, SharePage.test.tsx well under cap).

## Task Commits

Each task was committed atomically:

1. **Task 1: SharePage — the read-only snapshot renderer, tier-parameterised, with a scoped asset provider** - `cb75ef525` (feat)
2. **Task 2: main.tsx — the lazy /s/:token and /shared/:id routes** - `e2cddac46` (feat)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `web/src/routes/SharePage.tsx` - the tier-parameterised read-only snapshot page (337 LOC)
- `web/src/routes/SharePage.test.tsx` - 21 tests covering every behavior row + the security centrepiece assertions
- `web/src/main.tsx` - +13 LOC: one lazy import, two `<Route>` elements, one inline decision comment
- `web/src/chat/artifacts/useBlobPreview.ts` - re-pointed through `useAssetSource()` (deviation, see below)
- `web/src/i18n/resources.share.ts` - added `share.public.{loading,error,turn.*,artifacts.*}` in en+it (deviation, see below)

## Decisions Made

- **Tier via prop, not URL-derivation** — `SharePage`'s `tier: 'public' | 'internal'` is set explicitly by the two `<Route element={<SharePage tier="..." />} />` elements in `main.tsx`. The caller already knows which route matched; re-deriving it from `location.pathname` inside the component would be a second, redundant source of truth for the exact same fact.
- **Turn text is plain React text, not markdown** — the objective's "reuse the same renderer, read-only flag" principle is explicitly and repeatedly scoped to the 37B ARTIFACT renderers throughout the plan (assetSourceContext, HtmlPreview, previewKind); no read_first reference, truth, or acceptance criterion asks for the assistant-ui `MarkdownText`/`react-markdown` pipeline to render turn text. Rendering `{turn.text}` as a plain child inside a `whitespace-pre-wrap` `<p>` satisfies the literal XSS truth ("renders as text — no element is created from it") with zero HTML-generation surface, and avoids depending on assistant-ui's runtime/message-part context that this standalone page does not otherwise need.
- **Artifacts render inline unconditionally, not behind a click-to-open modal** — the plan's behavior rows describe artifacts rendering directly ("Artifacts render through the 37B renderers..."), not gated behind a `PreviewModal`-style interaction a public recipient has no natural entry point into. Each artifact mounts in its own bordered block with a header (icon/filename/size/download link) and its `previewKind`-dispatched renderer body, `Suspense`-wrapped exactly like `PreviewModal` does.
- **`StatusPage`'s loading/error/not-found states share one shell**, distinguished only by the ARIA `role` passed in (`status`/`alert`/none) — a smaller, more auditable surface than three separate components, and it structurally guarantees the not-found and loading bodies can never accidentally diverge in markup shape.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `useBlobPreview.ts` still hardcoded the identity-scoped asset route — the R-05 seam had a real gap**
- **Found during:** Task 1, while wiring `AssetSourceContext.Provider` around the artifact tree and reasoning through which renderers actually resolve bytes through it.
- **Issue:** Plan 37F-05's summary states three call sites were re-pointed through `useAssetSource()`: `useAssetContent.ts` (behind Text/Html/Docx/Xlsx previews) and `PreviewModal.tsx`'s two download anchors. It does not mention `useBlobPreview.ts` — the SEPARATE hook behind `ImagePreview` and `PdfPreview` — which still called `fetch('/api/assets/${id}/download', { credentials: 'same-origin' })` directly. On the public tier this sends no cookies to the WRONG, identity-scoped URL (which 404s for an anonymous caller); on the internal tier it sends the session cookie to a URL the bearer does not own (which 404s for a non-owner, per D-06). Either way, every image or PDF artifact on a shared conversation would silently fail to render — directly contradicting this plan's own truth "Artifacts render through the 37B renderers, resolving bytes via the token-scoped provider," which does not carve out an exception for image/pdf.
- **Fix:** Re-pointed `useBlobPreview` to call `useAssetSource()` and fetch through `assetUrl(assetId)` with the resolved `credentials`, mirroring `useAssetContent.ts`'s exact pattern (including the `[assetId, mimeType, key, assetUrl, credentials]` effect-dependency shape). `ImagePreview.tsx`/`PdfPreview.tsx` themselves are untouched — the fix lives entirely in the shared hook, one directory above `renderers/`, so it does not appear in the plan's `git diff --name-only -- web/src/chat/artifacts/renderers/` no-fork gate.
- **Files modified:** `web/src/chat/artifacts/useBlobPreview.ts`
- **Verification:** The pre-existing `useBlobPreview.test.ts` (11 tests, no provider mounted in any of them) passes completely unedited — its default expectation (`/api/assets/asset-1/download`, `credentials: 'same-origin'`) is byte-identical to `AssetSourceContext`'s own `IDENTITY_SCOPED` default, proving zero behavior change for every existing (non-share) call site. A new `SharePage.test.tsx` test ("renders an image artifact through the token-scoped provider (useBlobPreview fix)") asserts the fetch is actually called against `/s/tok-123/asset/a2` with `credentials: 'omit'` when mounted under the public tier — this is the test that would have failed before the fix.
- **Committed in:** `cb75ef525` (Task 1 commit)

**2. [Rule 1 - Bug] Stray embedded NUL byte in `useBlobPreview.ts`, unrelated to this plan, found while touching the file**
- **Found during:** Task 1, while attempting an `Edit` on the function body — the exact-match tool repeatedly failed against text that displayed identically to the Read tool's output.
- **Issue:** A single `0x00` byte was embedded in the `` `${assetId} ${mimeType ?? ''}` `` template literal, in place of the visible space character (confirmed via `xxd`; `file` reported the whole file as "data" rather than "ASCII text" because of it). Pre-existing corruption, unrelated to any change this plan makes — most likely a stray byte from an earlier authoring tool's output.
- **Fix:** Rewrote the file (`Write`, since the byte broke exact-string `Edit` matching) with a normal space character in its place. Confirmed via `python3 -c "... .count(b'\x00')"` that the NUL count is now 0, and `file` now reports the path as UTF-8 text.
- **Files modified:** `web/src/chat/artifacts/useBlobPreview.ts` (same file, same commit as deviation #1 above)
- **Verification:** `file web/src/chat/artifacts/useBlobPreview.ts` → "JavaScript source, Unicode text, UTF-8 text"; all 11 pre-existing tests + the new share-page test still pass.
- **Committed in:** `cb75ef525` (Task 1 commit)

**3. [Rule 2 - Missing Critical] Added `share.public.{loading,error,turn.*,artifacts.*}` i18n keys**
- **Found during:** Task 1, writing the loading/error/turn/artifact-heading strings the plan's acceptance criteria requires ("No literal user-facing strings — every visible string comes from `t('share.…')`.") and the `<behavior>` rows require ("Loading shows a `role=\"status\"`; a network error shows a `role=\"alert\"`.").
- **Issue:** `resources.share.ts` (landed in plan 37F-05) only carries `share.public.{snapshotDate,mark,notFound}` — no keys existed yet for the loading state, the network-error state, the per-turn role labels, the tool-provenance label, or the artifacts-section heading/download/preview-unavailable copy this plan's page needs.
- **Fix:** Added `loading`, `error`, `turn.{user,assistant,toolsUsed}`, and `artifacts.{heading,download,previewUnavailable}` under the existing `share.public` namespace, in both `shareEn` and `shareIt`, matching the file's existing Italian typography conventions (curly apostrophes, `…` ellipsis).
- **Files modified:** `web/src/i18n/resources.share.ts`
- **Verification:** The pre-existing recursive key-tree parity test (`resources.share.test.ts`) and the whole-bundle parity test (`resources.parity.test.ts`) both pass unedited — they walk the tree rather than asserting a fixed key list, so new balanced en/it leaves need no test changes.
- **Committed in:** `cb75ef525` (Task 1 commit)

**4. [Rule 1 - Bug] Two ESLint errors from `eslint-plugin-react-hooks`'s newer React-Compiler rules, not anticipated by the plan**
- **Found during:** Task 1's `npx eslint` verification step.
- **Issue:** (a) `react-hooks/static-components` flagged `<Icon aria-hidden .../>` where `Icon = categoryIcon(...)` was computed and rendered in the SAME function (`ArtifactBlock`) — even though `categoryIcon` always resolves to one of a fixed set of already-declared, stable Lucide exports, never a genuinely new component. (b) `react-hooks/set-state-in-effect` flagged two `setState(...)` calls made synchronously and directly in the `useEffect` body (not inside the async fetch callback): one for the empty-routeKey early return, one resetting to `'loading'` before starting a fetch.
- **Fix:** (a) Split icon rendering into a new `ArtifactGlyph({ Icon })` component that only ever RECEIVES the icon as a prop and renders it — mirroring the exact shape `ArtifactRow.tsx`'s pre-existing `IconTile` already uses for the identical `categoryIcon()` call, which passes this same rule today. (b) Moved the empty-routeKey case out of the effect entirely into a derived render-time branch (`if (routeKey === '') return <StatusPage ... />`, checked at render time, not dispatched via a state transition), and moved the `'loading'` reset to be the first statement INSIDE the async fetch callback rather than synchronously at the top of the effect body.
- **Files modified:** `web/src/routes/SharePage.tsx` (both fixes are internal to this newly-created file, folded into Task 1's single commit — not a separate deviation commit)
- **Verification:** `npx eslint web/src/routes/SharePage.tsx` → 0 errors, 0 warnings (previously 2 errors). `npx tsc --noEmit` clean. All 21 tests still pass, including the two new tests this restructuring makes reachable (an empty-token defensive-fallback test, and the malformed-response `role="alert"` test).
- **Committed in:** `cb75ef525` (Task 1 commit)

---

**Total deviations:** 4 auto-fixed (3 × Rule 1 — bug fixes; 1 × Rule 2 — missing critical i18n strings). All fixes were required for this plan's own stated truths/acceptance-criteria to hold (image/pdf artifacts actually rendering; every visible string coming from `t()`; a clean lint gate) or were pre-existing corruption discovered incidentally while touching the exact line. No scope creep — no renderer under `web/src/chat/artifacts/renderers/` was edited (`git diff --name-only -- web/src/chat/artifacts/renderers/` is empty), and no file outside this plan's direct dependency chain was touched.

## Known discrepancy versus a plan acceptance-criterion (not a deviation, a formatter constraint)

Task 2's acceptance criteria state `grep -c "SharePage" web/src/main.tsx` should return `3` ("the lazy import + two route elements"). The actual result is `4`: this project's Prettier config (`printWidth: 100`) wraps the lazy import's named-export mapping across two lines — `const SharePage = lazy(() =>` and `import('./routes/SharePage').then((mod) => ({ default: mod.SharePage })),` — exactly mirroring the pre-existing `LoginPage`/`NotFoundView` lazy imports already in this same file (both of which are also 9-character names and wrap identically). A single-line form would be 103 characters, violating `prettier --check`. The qualitative intent — one lazy import, two route registrations — is fully met; `grep -q 'path="/s/:token"'` and `grep -q 'path="/shared/:id"'` both succeed, and `git diff web/src/main.tsx` shows only additions.

## Issues Encountered

- The `Edit` tool repeatedly failed an exact-string match against `useBlobPreview.ts` despite the `Read` tool showing identical text; root-caused to a stray embedded NUL byte in the file (see Deviation #2) invisible to `Read`'s rendering but present on disk. Resolved by rewriting the file with `Write` instead.

## User Setup Required

None - no external service configuration required. Frontend-only change; no new dependency, no migration, no env var.

## Next Phase Readiness

- `SharePage` is live at both `/s/:token` and `/shared/:id`, ready for plan 37F-17's management surface (settings page listing/revoking shared links) to link to.
- The `useBlobPreview.ts` fix means the R-05 seam (`AssetSourceContext`) now covers ALL six 37B renderers, not four of six — any future consumer of the seam (e.g., a future export-preview surface) inherits image/pdf support automatically.
- Manual verification (VALIDATION.md's manual-only row — mint a public link, open `/s/{token}` in a private window, inspect the iframe in devtools) is NOT executed by this plan; it requires plans 37F-08 through 37F-13 (the share store/API/route-admission backend) to be live first. `cmd/aura/serve_webui_share.go`/`isPublicShareRoute` referenced by this plan's read_first section do not exist on disk yet (plan 37F-12 has a PLAN.md but no SUMMARY.md) — this plan's frontend code is written against the documented, locked contract (`GET /s/{token}/data`, `GET /api/shares/{id}/data`, the asset sibling routes, exact request/response shapes per 37F-10-PLAN.md) rather than against a running backend. No blocker for THIS plan (all behavior is provable with mocked `fetch`), but the manual/live verification step is deferred until the backend plans land.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 5 created/modified plan files (`web/src/routes/SharePage.tsx`, `web/src/routes/SharePage.test.tsx`,
`web/src/main.tsx`, `web/src/chat/artifacts/useBlobPreview.ts`, `web/src/i18n/resources.share.ts`)
verified present on disk; both task commit hashes (`cb75ef525`, `e2cddac46`) verified present in
`git log --oneline --all`.
