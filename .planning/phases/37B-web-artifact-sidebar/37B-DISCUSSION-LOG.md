# Phase 37B: Web Artifact Sidebar - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-08
**Phase:** 37B-web-artifact-sidebar
**Areas discussed:** Panel placement & responsive, Previews depth (anteprime), Live-merge mechanics, Scarica tutto UX, Saved-conversation download persistence, Office/preview library evaluation, Preview modal & type labels

---

## Panel placement & responsive

| Option | Description | Selected |
|--------|-------------|----------|
| Toggleable 3rd ResizablePanel | Third right panel in the chat-shell group, header doc-icon toggle, Drawer on mobile | ✓ |
| Header-toggled overlay (hidden by default) | Right-side sheet over the chat, same on desktop + mobile | |
| Always-on persistent panel (no toggle) | Third panel always visible on desktop | |

**User's choice:** Toggleable 3rd ResizablePanel.
**Follow-ups:** Default state = **closed, auto-open on first artifact**. Mobile = **right-side Drawer** ("look also how the left side is made" → examined `Drawer.tsx` + `useSurfaceRestore.ts`; the Drawer already supports `side='right'`, so the mobile panel mirrors the nav drawer). Later user directive: **make the panel resizable like the left nav rail** → own `ResizableHandle` + persisted width via `CHAT_SHELL_PANEL_IDS`.

---

## Previews depth (anteprime)

| Option | Description | Selected |
|--------|-------------|----------|
| Icon rows + click-to-preview modal | Rows icon-only; click opens a MIME-gated preview modal (LibreChat FilePreviewDialog) | ✓ |
| Inline thumbnails in each row + click to expand | Image assets render a blob thumbnail per row | |
| No preview — icon rows + download only | List + download only, no in-app preview | |

**User's choice:** Icon rows + click-to-preview modal.
**Renderers selected (multi-select):** Images, PDF, Text family, Office (docx/xlsx/pptx), **+ "html page too"**. Text render = **raw monospace `<pre>`** (not rendered markdown).
**Resolution:** images (SVG→download-only), pdf (`<iframe>`), text (`<pre>`), **html → sandboxed `<iframe srcdoc>`**, **docx → docx-preview (MIT)**, **xlsx → SheetJS CE (Apache-2.0)**, **pptx → download-only** (no good free client-side renderer). All lazy-loaded. Key unblock recorded: 37A's `attachment`+`octet-stream` does NOT block `fetch()`→blob→objectURL preview and the XSS guard stays intact.

---

## HTML sandbox script policy

| Option | Description | Selected |
|--------|-------------|----------|
| allow-scripts, NO allow-same-origin | Scripts run in a null origin isolated from our session | ✓ |
| Fully locked sandbox (no scripts) | Static HTML/CSS only, JS inert | |
| Source view only (raw <pre>) | Show markup as text, never render | |

**User's choice:** `allow-scripts`, no `allow-same-origin` (confirmed after clarifying that "we use garage for file" doesn't change the in-browser render-safety choice — Garage is storage; the question is *where the HTML executes*).

---

## PPTX handling

| Option | Description | Selected |
|--------|-------------|----------|
| Download-only card for pptx | docx+xlsx render inline; pptx falls back to download | ✓ |
| Add a free pptx renderer despite roughness | Community pptx-to-HTML lib, lower fidelity | |
| Accept paid SDK (Nutrient/Apryse) | Commercial WASM viewer, full Office | |

**User's choice:** Download-only card for pptx.

---

## Office / preview library evaluation

| Option | Description | Selected |
|--------|-------------|----------|
| Keep client-side; defer ONLYOFFICE | docx-preview + SheetJS + sandboxed html; document ONLYOFFICE as upgrade path | ✓ |
| Commit to ONLYOFFICE Document Server now | Self-hosted container + JWT asset exposure for full Office + editing | |

**User's choice:** Keep client-side; defer ONLYOFFICE.
**Notes:** User asked to evaluate the Nutrient blog, `@cyntler/react-doc-viewer`, and `@onlyoffice/document-editor-react`. Honest verdict recorded in CONTEXT canonical refs: react-file-viewer dead; react-doc-viewer frozen + Office needs public URL; ONLYOFFICE excellent + active + covers pptx/editing but requires a heavyweight self-hosted Document Server + JWT-signed private-asset exposure (wrong weight class + RAM-blocked). Client-side path chosen; ONLYOFFICE = future Office-viewer/editor phase.

---

## Live-merge mechanics

| Option | Description | Selected |
|--------|-------------|----------|
| Event-triggered refetch, server-authoritative | useQuery base + onArtifact → invalidate → refetch; server order/dedup | ✓ |
| Client-side push-merge by asset_id | Lift descriptors + setQueryData upsert, no round-trip | |

**User's choice:** Event-triggered refetch. One `onArtifact` signal (mirroring `onUsage`) drives both list refresh and auto-open.

---

## Saved-conversation download persistence (correctness item)

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — fix inline chip too | Fold source_kind=agent assets onto assistant messages on history load | ✓ |
| Panel-only for 37B; note inline-chip gap as follow-up | Ship durable panel fix, leave inline chip as-is | |

**User's choice:** Fix the inline chip too.
**Notes:** User flagged "on saved conversation link download disappear we must reload on open conversation." Confirmed in code: the inline `local_artifact` chip is synthesized only from the live stream; history load drops the `asset_id`. Panel's `threadId`-keyed query fixes it durably; the inline chip is additionally rehydrated by mirroring `attachAssetsToUserMessages` onto assistant messages.

---

## Scarica tutto UX

| Option | Description | Selected |
|--------|-------------|----------|
| Sequential <a download> clicks, throttled + progress | ~400–600ms delay, N/M progress, skip degraded rows | ✓ |
| Sequential fetch→blob→save | Buffer each file, FileSaver-style | |

**User's choice:** Sequential `<a download>` clicks, throttled + progress.

---

## Per-row interaction

| Option | Description | Selected |
|--------|-------------|----------|
| Row body → preview modal; download icon → download | Two affordances, matches reference | ✓ |
| Whole row → download; no preview affordance | Entire row downloads | |

**User's choice:** Row body → preview modal; trailing download icon → download.

---

## Preview surface size

| Option | Description | Selected |
|--------|-------------|----------|
| Large/near-fullscreen modal over dialog.tsx | ~90vw/90vh, room for pdf/docx/xlsx/html | ✓ |
| Compact centered lightbox | Small, tuned for images | |
| Full-height right Sheet | Slide-in panel like SourceExplorerSheet | |

**User's choice:** Large/near-fullscreen modal over `components/ui/dialog.tsx`.

---

## Row type-label richness

| Option | Description | Selected |
|--------|-------------|----------|
| Category word + uppercase extension | "Documento · MD", "Foglio · XLSX" + lucide icon per category | ✓ |
| Minimal — filename + size only | No category word | |

**User's choice:** Category word + uppercase extension (per `CitationBubble` icon precedent).

---

## Claude's Discretion

- Exact panel width tokens, header toggle icon glyph, precise MIME/extension→category-label + icon mapping, empty-state/error i18n copy, `docx-preview`/`SheetJS` import surface, and whether the toggleable-panel dynamic membership needs a layout-key bump.

## Deferred Ideas

- "Condiviso" shared-link section + share-arrow → Phase 37F.
- "Contenuto" thumbnail grid (conversation content) → not a delivered-artifact concern.
- `.pptx` inline rendering; ONLYOFFICE/Nutrient/Apryse full Office + editing → future Office-viewer phase (post-32GB).
- Server-side zip for "Scarica tutto"; rendered/sanitized markdown preview; inline row thumbnails (considered + rejected).
