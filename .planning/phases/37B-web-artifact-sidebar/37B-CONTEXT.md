# Phase 37B: Web Artifact Sidebar - Context

**Gathered:** 2026-07-08
**Status:** Ready for planning

<domain>
## Phase Boundary

The artifacts an agent delivers in a thread are aggregated into a right-side **"Artefatti"** panel in the web cockpit (parity with Telegram + the Claude UI): a per-thread file list with per-file download + "Scarica tutto", **click-to-preview**, and **no host/container path ever exposed to the browser**. Built entirely on Phase 37A's `asset_id` + `GET /api/assets/{id}/download` — **no new source of truth**. First of the three cockpit-web parity areas from the voice/artifact/skill audit (37C = voice, 37D = "/" picker).

**In scope:** the right-side Artefatti `ResizablePanel` (resizable + toggleable + mobile Drawer), the derived list view (`GET /api/assets?thread_id=` base + live `aura.artifact` merge via refetch), per-row download + preview, "Scarica tutto", the click-to-preview modal with MIME-gated renderers (image / pdf / text / html / docx / xlsx), and a **fix to the inline `local_artifact` chip so agent-deliverable downloads survive saved-conversation open + reload**.

**Out of scope (→ other phases / deferred):** the "Condiviso" (shared link) section + the share-arrow icon → **Phase 37F** (sharing/export); the "Contenuto" thumbnail grid (conversation content, not delivered files) → not a 37B concern; the "/" command picker → **Phase 37D**; `.pptx` inline rendering + a paid/self-hosted Office viewer (Nutrient / ONLYOFFICE) → deferred; server-side zip for "Scarica tutto" → deferred (YAGNI).
</domain>

<decisions>
## Implementation Decisions

Success criteria WEBART-05..08 are effectively locked (see canonical refs). These are the HOW decisions the discussion resolved. Grounded in the user's reference screenshots (the Claude.ai right panel) and the 37A reference clones still on disk at `D:\tmp` (LibreChat, open-webui).

### Panel placement & responsive (WEBART-05, fork b)
- **D-01 — Toggleable third `ResizablePanel` on the right.** Add a third right-side panel to the existing chat-shell `ResizablePanelGroup` in `AppShell.tsx`, shown/hidden by a **header doc-icon toggle** (matches the reference's top-right icon; the adjacent share-arrow is 37F, NOT built here). The chat surface goes `[nav | workspace | artefatti]`.
- **D-02 — Resizable exactly like the left nav rail (user-directed).** The panel gets its own `ResizableHandle` (mirroring `chat-navigation-resizer`) with `min`/`max`/`default` widths + `groupResizeBehavior="preserve-pixel-size"`, and its width is **persisted** by adding `chat-artifacts` to `CHAT_SHELL_PANEL_IDS` in `useDefaultLayout`. Suggested sizing (planner discretion): `default ~19rem, min ~16rem, max ~32rem` (wider than the nav's 15/13/28rem since it shows file rows).
- **D-03 — Closed by default; auto-open once on the first artifact.** Panel starts hidden (chat full-width when nothing to show); the first `aura.artifact` in a thread auto-opens it a single time, then the user controls it; open/closed state persisted.
- **D-04 — Mobile/tablet: reuse `Drawer side='right'` titled "Artefatti".** Below the `lg` breakpoint the panel collapses into the **existing `web/src/shell/Drawer.tsx` with `side='right'`** (already supported), mirroring the nav drawer exactly — portal, backdrop, focus-trap, Esc, scroll-lock, explicit-vs-swipe close — wired through the `useSurfaceRestore` "one heavy overlay at a time" reducer, opened by the same header toggle. Not resizable on mobile (neither is the nav drawer).

### Previews / "anteprime" (WEBART-05, the marquee feature)
- **D-05 — Icon rows + click-to-preview modal (LibreChat `FilePreviewDialog` pattern).** Rows stay icon + name + type-label + trailing download icon (faithful to the reference — the visual thumbnails in the screenshot are the separate "Contenuto" section, out of scope). Clicking the **row body** opens the preview; the **trailing download icon** downloads directly.
- **D-06 — Preview flow: `fetch()` → re-labelled `Blob` → `objectURL` → render by MIME → `revokeObjectURL` on unmount.** The `fetch()` carries the session cookie (same-origin); the `Blob` is re-labelled with the SSE-carried `mime_type` (37A ships it, server-sniffed). **This needs ZERO backend change and does NOT weaken 37A's D-10 XSS guard** — the `attachment` + `octet-stream` headers only govern top-level navigation; bytes render in a blob-URL element, never as a top-level document from our origin.
- **D-07 — MIME-gated renderers (everything else → download-only card inside the modal):**
  - **Images** (png/jpg/gif/webp) → blob `<img>`. **SVG deliberately EXCLUDED → download-only** (a blob `<img>` of SVG can execute embedded script — the one XSS edge).
  - **PDF** → native `<iframe>` on the blob URL (zero dependency, no pdf.js/react-pdf bundle).
  - **Text family** (txt/md/csv/json/log/sh/yaml/xml/etc.) → fetch text, **raw monospace `<pre>`** (no rendered markdown — zero injection surface, honest "this is the file").
  - **HTML** → **sandboxed `<iframe srcdoc>` with `sandbox="allow-scripts"` and NO `allow-same-origin`** (null origin). Interactive HTML/JS renders live but cannot touch our session/cookies/DOM/Garage. Cannot point `<iframe src>` at the download URL (its `attachment` disposition forces a download) — must go `fetch()` → `srcdoc`.
  - **docx** → **`docx-preview` (MIT)** rendered client-side from the fetched `ArrayBuffer` (no public URL, no server).
  - **xlsx** → **SheetJS Community Edition (Apache-2.0)** → `sheet_to_html` table.
  - **pptx** → **download-only card** (no good free client-side renderer; not worth a rough dep or a paid SDK now).
- **D-08 — All preview renderers are lazy-loaded chunks** (mirroring the existing `GraphExplorer`/Sigma + Governance lazy-import pattern in `AppShell.tsx`) so `docx-preview`/`SheetJS`/etc. never land in the main bundle — only fetched when a user opens that file type.
- **D-09 — Preview opens as a large / near-fullscreen modal over `components/ui/dialog.tsx`** (Esc/backdrop/focus-trap built in), sized ~90vw/90vh so pdf/docx/xlsx/html have room. Modal header = filename + download + close. One consistent surface for all types; loading + error states live inside.

### Live-merge / data layer (WEBART-07, fork a)
- **D-10 — `useQuery(['assets', threadId], listThreadAssets)` is the base; the React Query cache is the derived view (server stays the source of truth).** Reuses the existing `listThreadAssets` client (`web/src/chat/attachments/api.ts:47`) and the `GET /api/assets?thread_id=` endpoint (already identity-scoped via `ListForThread` → `GetForIdentity`). Newest-first ordering + dedup come from the server, not client merge logic.
- **D-11 — Live merge = event-triggered, server-authoritative refetch.** Add an `onArtifact` callback from `ExternalStoreChat` (mirroring the existing `onUsage` callback); when a run emits `aura.artifact`, `AppShell` invalidates `['assets', threadId]` → refetch pulls the new asset (37A persists it to Garage+DB *before* emitting the event, so it's always there). Reuses the established `invalidateRuntimeReads` invalidate-after-run pattern. **The same `onArtifact` signal also drives D-03's auto-open.** (Chosen over client-side `setQueryData` push-merge: the refetch is trivial here and avoids manual dedup/ordering.)

### Downloads (WEBART-06)
- **D-12 — Per-row download = same-origin `<a href="/api/assets/{id}/download" download>`** (the exact 37A-proven auth path). No blob buffering.
- **D-13 — "Scarica tutto" = sequential `<a download>` clicks, throttled (~400–600ms) + `N/M` progress, degraded rows skipped.** The small inter-download delay avoids the browser's multi-download burst block. No server-side zip (SC#2; YAGNI). Button disabled during the run.

### Saved-conversation download persistence (the correctness item the user flagged)
- **D-14 — The panel makes downloads durable on saved-conversation open BY DESIGN.** Because D-10's `useQuery` is keyed on `threadId`, opening any saved conversation refetches the server's asset list — downloads are present with **no reload**, independent of the live message stream (which is where the current inline chip loses its `asset_id`). This is the panel's core value-add over the inline chip.
- **D-15 — ALSO rehydrate the inline assistant `local_artifact` chip on history load (in-scope for 37B, "fix on touch").** On history load, fold `source_kind=agent` assets from `listThreadAssets` onto their assistant messages (mirroring `attachAssetsToUserMessages` in `ExternalStoreChat.tsx:62`, which already does this for user *uploads*). Fixes the "download disappears on saved-conversation open until reload" bug at **both** surfaces (inline chip + panel), not just the panel.

### Type labels, empty/error states (WEBART-05/08)
- **D-16 — Row type-label = friendly category word + uppercase extension** (e.g. "Documento · MD", "Foglio · XLSX", "Immagine · PNG", "Codice · SH"), derived from mime/extension, with a lucide icon per category following the `CitationBubble.tsx` precedent (`document: FileText`, `Code2`, `Globe`, `File`). Matches the reference screenshot.
- **D-17 — Graceful empty-state** when the panel is opened with zero artifacts (icon + i18n copy, e.g. "Nessun artefatto in questa conversazione"). Newest-first ordering (SC#1).
- **D-18 — Degraded / error states.** A degraded asset (no `asset_id` / status not accepted, per 37A D-02) renders a disabled "delivery unavailable" row mirroring the existing `LocalArtifactDisplay` degraded branch; a preview `fetch` failure shows an error card + a download fallback inside the modal; "Scarica tutto" skips degraded rows and continues on individual error.

### PRD-first (mandatory before code)
- **D-19 — A PRD-amendment commit is required before implementation** (the ROADMAP already marks 37B "PRD-first: richiede PRD-amendment"). The amendment must cover: the new requirement group **WEBART-05..08**, the **Artefatti sidebar surface** (not documented in the PRD), the **preview renderer set** + the two new web deps (`docx-preview`, `SheetJS`) + the sandboxed-iframe HTML policy, and the **saved-conversation download-persistence** behavior (D-14/D-15). Planner writes the amendment first (see PRD §Q&A revision protocol).

### Claude's Discretion
- Exact panel width tokens (D-02), the header toggle icon glyph, the precise MIME/extension→category-label + icon mapping (D-16), empty-state/error i18n copy keys, the `docx-preview`/`SheetJS` import surface, and whether the toggleable-panel dynamic membership needs a layout-key bump (see code_context gotcha) — all planner/executor discretion, provided the decisions above hold.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase spec (locked success criteria)
- `.planning/ROADMAP.md` §"Phase 37B: Web Artifact Sidebar (INSERTED)" (lines 410-427) — goal, 4 success criteria, the three flagged design forks (a/b/c), and the PRD-first note.
- `.planning/REQUIREMENTS.md` WEBART-05..08 (lines 69-72) — acceptance text (locked).

### Upstream phase (the substrate this panel aggregates — READ FIRST)
- `.planning/phases/37A-web-artifact-delivery-lane/37A-CONTEXT.md` — the constraints that bind 37B: **D-09** (stream-through Go download, no presign), **D-10** (`Content-Disposition: attachment` + `octet-stream` XSS guard — the thing D-06/D-07 work *with*, not against), **D-12** (ownership via `GetForIdentity`, 404 on miss), **D-13** (`sseAdapter` routes `aura.artifact` → `LocalArtifactDisplay`).

### Backend (already complete — 37B is nearly pure frontend)
- `internal/agui/assets_api.go` — `handleAssetList` (`:161`, `GET /api/assets?thread_id=` → `ListForThread`, identity-scoped) and `handleAssetDownload` (the per-file stream); both behind `RequireAuth` + `principalIdentityID`.
- `internal/assets/service.go` — `ListForThread` (`:198`) and `GetForIdentity` (`:174`) enforce ownership.

### Web (the surface to build/extend)
- `web/src/AppShell.tsx` — the 2-panel chat-shell `ResizablePanelGroup` (`:441`), `CHAT_SHELL_LAYOUT_ID`/`CHAT_SHELL_PANEL_IDS` (`:49-50`), `useDefaultLayout` (`:286`), the `chat-navigation-resizer` `ResizableHandle` (`:463`) to mirror, the lazy-import pattern (`:29-47`), the `Drawer` mount (`:488`), and the `onUsage` wiring to mirror for `onArtifact`.
- `web/src/shell/Drawer.tsx` — already supports `side='right'` (`:68`); reuse verbatim for the mobile Artefatti drawer.
- `web/src/shell/useSurfaceRestore.ts` — the "one heavy overlay at a time" reducer (`openOverlay`/`closeOverlay` with explicit-vs-swipe restore) to route the mobile drawer through.
- `web/src/chat/attachments/api.ts:47` — `listThreadAssets(threadId, signal)` — the ready-made list fetcher (reuse as the D-10 query fn).
- `web/src/chat/ExternalStoreChat.tsx` — `attachAssetsToUserMessages` (`:62`, the fold pattern to mirror for D-15), the `onUsage` callback shape (`:134`, mirror for `onArtifact`), `invalidateRuntimeReads` (`:162`), and the history-load path.
- `web/src/chat/sseAdapter.ts` — the `aura.artifact` reducer branch (`:345`) synthesizing `local_artifact`; the place to also surface the `onArtifact` signal.
- `web/src/chat/displays/LocalArtifactDisplay.tsx` — the inline chip (D-15 rehydration target) + its degraded-branch UX to mirror (D-18); `formatSize` helper.
- `web/src/components/ui/dialog.tsx` — the preview modal primitive (D-09); `web/src/a11y/focusTrap.ts` — focus-trap utils; `web/src/chat/displays/CitationBubble.tsx` — the type→lucide-icon mapping precedent (D-16).

### External reference implementations (cloned to `D:\tmp`, still on disk)
- `D:\tmp\LibreChat\client\src\components\Chat\Messages\Content\FilePreviewDialog.tsx` — the exact click-to-preview pattern: `canPreviewByMime → 'pdf' | 'text' | false`, images handled separately, download-only fallback (D-05/D-07).
- `D:\tmp\LibreChat\client\src\components\SidePanel\Files\Panel.tsx` (+ `PanelFileCell.tsx`, `PanelTable.tsx`) — a right-side file panel structure.
- `D:\tmp\LibreChat\client\src\components\Artifacts\` — HTML artifact rendering via Sandpack (the *heavy* alternative D-07's sandboxed-iframe chose against for static-file preview).
- `D:\tmp\open-webui\src\lib\components\common\FileItem.svelte` + `FileItemModal.svelte` — click-to-open-modal + per-row action pattern.

### Preview libraries (new web deps — evaluated 2026-07-08)
- `docx-preview` (npm, **MIT**) — client-side .docx → HTML from `ArrayBuffer`, no public URL / no server (D-07 docx).
- `xlsx` / **SheetJS Community Edition** (npm, **Apache-2.0**) — client-side .xlsx parse → `sheet_to_html` (D-07 xlsx).
- **Rejected/deferred:** `react-file-viewer` (dead since 2019); `@cyntler/react-doc-viewer` (frozen Sep 2025 + Office path needs a *public* URL — breaks private auth); `@onlyoffice/document-editor-react` v2.2.0 (excellent, active, covers pptx + editing, but requires a **self-hosted ONLYOFFICE Document Server** container ~2GB+ RAM + JWT-signed asset exposure — wrong weight class for a delivery-lane preview and blocked by the box's RAM constraint; the documented upgrade path for a future Office viewer/editor phase); Nutrient Web SDK (paid + heavy WASM).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `listThreadAssets(threadId, signal)` (`web/src/chat/attachments/api.ts:47`): the D-10 query fn — returns identity-scoped `Asset[]` for the thread. No new client needed.
- `Drawer` with `side='right'` (`web/src/shell/Drawer.tsx`): the mobile panel — already implemented, portal + backdrop + focus-trap + Esc + scroll-lock + explicit/swipe intent.
- `useSurfaceRestore` (`web/src/shell/useSurfaceRestore.ts`): the "one heavy overlay" reducer to route the mobile drawer through.
- `attachAssetsToUserMessages` (`web/src/chat/ExternalStoreChat.tsx:62`): the exact fold-assets-onto-messages pattern to mirror for D-15 (inline chip rehydration), but targeting **assistant** messages with `source_kind=agent` assets.
- `components/ui/dialog.tsx` + `a11y/focusTrap.ts`: the preview modal + a11y (D-09).
- `LocalArtifactDisplay.tsx` `formatSize` + degraded-branch markup: reuse for row sizing + D-18 degraded rows.
- `CitationBubble.tsx` type→icon map: the D-16 icon-per-category precedent.

### Established Patterns
- **Lazy chunks:** `AppShell.tsx` `lazy(() => import(...))` + `Suspense` for `GraphExplorer`/`GovernanceWorkspace`/etc. — the D-08 pattern for the preview renderers.
- **Persisted resizable layout:** `useDefaultLayout({ id: CHAT_SHELL_LAYOUT_ID, panelIds })` + `ResizablePanel`/`ResizableHandle` with `min/max/defaultSize` + `groupResizeBehavior="preserve-pixel-size"` — the D-02 pattern (add `chat-artifacts` to `panelIds`).
- **Callback lift-up:** `onUsage?.(usage)` from `ExternalStoreChat` to `AppShell` — the D-11 `onArtifact` shape.
- **React Query invalidate-after-run:** `invalidateRuntimeReads` → `queryClient.invalidateQueries` — the D-11 merge mechanism.
- **Auth on fetch:** always `credentials: 'same-origin'` (the D-06 blob fetch must carry the cookie).

### Integration Points
- `AppShell.tsx`: add the third `ResizablePanel` + `ResizableHandle` inside the `showConversationNavigation` branch of the chat-shell group; add `chat-artifacts` to `CHAT_SHELL_PANEL_IDS`; add the header toggle; mount a right `Drawer` for mobile; wire `onArtifact` (invalidate + auto-open).
- `ExternalStoreChat.tsx`: surface an `onArtifact` prop (fires on `aura.artifact` frames carrying `asset_id`); extend history-load to fold agent assets onto assistant messages (D-15).
- `sseAdapter.ts`: emit the `onArtifact` signal from the existing `aura.artifact` reducer branch (`:345`).
- New components (planner discretion, ≤600 LOC each, split by concern): the panel container + query hook, the row, the "Scarica tutto" control, the preview modal + per-MIME renderer chunks.

### Planning gotcha (record it)
- The panel is **toggleable**, so it enters/leaves the `ResizablePanelGroup` dynamically. react-resizable-panels needs explicit `id`/`order` on all three panels and may need a **layout-key bump (`aura-chat-shell-v3` → `v4`)** or a distinct persisted key for the 3-panel arrangement so the saved 2-panel layout doesn't fight the 3-panel one.

</code_context>

<specifics>
## Specific Ideas

- The user provided the **Claude.ai right-panel screenshot** (`D:\Immagini\Screenshots\Screenshot 2026-07-08 104850.png` and the 19:39 shots) as the target: an **"Artefatti"** header + "Scarica tutto", icon rows with category + extension labels + a **trailing per-row download icon**, and a header doc-icon toggle. The "Condiviso" (→ 37F) and "Contenuto" (conversation-content thumbnails) sections in the same screenshot are explicitly NOT this phase.
- The user directed the file-viewer research (Nutrient blog, `@cyntler/react-doc-viewer`, `@onlyoffice/document-editor-react`) and wants an **honest industrial-fit verdict** — the outcome is the lightweight MIME-gated client-side path over the frozen/public-URL/paid/self-hosted alternatives (see canonical refs).
- The user flagged the **saved-conversation download-disappears** bug and wants it fixed (D-14/D-15), and wants the panel **resizable like the left nav rail** (D-02).
- Strong preference throughout for smallest-blast-radius, reference-backed reuse (reuse `Drawer side='right'`, reuse `listThreadAssets`, reuse `dialog.tsx`, mirror `onUsage`/`attachAssetsToUserMessages`) — this is a parity/aggregation layer over 37A, not new infrastructure.

</specifics>

<deferred>
## Deferred Ideas

- **"Condiviso" (shared link) section + share-arrow** — conversation/artifact sharing → **Phase 37F** (Conversation & Artifact Sharing / Export).
- **"Contenuto" thumbnail grid** — a visual gallery of conversation *content* (pasted images, cards), distinct from delivered files; not a delivered-artifact concern.
- **`.pptx` inline rendering** — no good free client-side renderer today; revisit if pptx demand appears.
- **ONLYOFFICE Document Server (or Nutrient/Apryse)** — full-fidelity docx/xlsx/pptx + editing via a self-hosted service; the documented upgrade path for a future dedicated **Office viewer/editor phase**, especially post-32GB-RAM upgrade and if *editing* (not just preview) becomes a goal.
- **Server-side zip for "Scarica tutto"** — a single-archive endpoint; deferred until client-side sequential download proves insufficient (SC#2 YAGNI).
- **Rendered (sanitized) markdown preview** — prettier README previews vs the chosen raw `<pre>`; only if the existing markdown renderer's XSS-safety is verified and it's judged worth it.
- **Inline row thumbnails for images** — considered and rejected for 37B (N concurrent authenticated fetches on list render; most artifacts have no natural thumbnail); the click-to-preview modal covers the need.

### Reviewed Todos (not folded)
None — no pending-todo matches surfaced for this phase (`todo.match-phase 37B` → 0).

</deferred>

---

*Phase: 37B-Web Artifact Sidebar*
*Context gathered: 2026-07-08*
