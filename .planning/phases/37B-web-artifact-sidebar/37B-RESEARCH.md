# Phase 37B: Web Artifact Sidebar - Research

**Researched:** 2026-07-08
**Domain:** React 19 / TypeScript cockpit-web — resizable panel, MIME-gated client-side file preview, TanStack-Query derived view, saved-conversation rehydration
**Confidence:** HIGH (mechanics verified against installed source + official docs; two MEDIUM items flagged)

> **Scope note.** The decisions (D-01..D-19) in `37B-CONTEXT.md` are the truth-source and are **not** re-litigated here. This document supplies the **implementation-ready HOW** the planner needs, corrects three CONTEXT.md assumptions that the actual code/registry contradict, and produces the mandatory Validation Architecture. Every claim is tagged with provenance.

---

## User Constraints (from CONTEXT.md)

`37B-CONTEXT.md` is authoritative and must be read by the planner. This research operates **inside** those constraints. Compact pointer only (not restated):

- **Locked decisions:** D-01..D-19 (panel placement/responsive, MIME-gated previews, live-merge via refetch, sequential download, saved-conversation persistence D-14/D-15, PRD-first D-19). Research THESE, no alternatives.
- **Claude's Discretion (this research resolves several):** panel width tokens; header toggle glyph; MIME/ext→category+icon map; empty/error i18n copy; `docx-preview`/`SheetJS` import surface; **and whether dynamic-panel membership needs a layout-key bump** — see Architecture Pattern 1 (answer: **no bump needed**).
- **Deferred / OUT OF SCOPE (do not build):** "Condiviso" share section + share-arrow (→37F); "Contenuto" thumbnail grid; `.pptx` inline render; ONLYOFFICE/Nutrient/Apryse; server-side zip; rendered-markdown preview; inline row thumbnails.

### Project Constraints (from CLAUDE.md)
- **≤600 LOC per file**, refactor-on-touch (split `<name>_<concern>.tsx`), no dead code, no god class. New panel = several small components, not one file.
- **Coverage floor 85%** — enforced in `web` by `vitest run --coverage` (thresholds fail the suite below 85% statements/branches/functions/lines across all of `src/**`). `[VERIFIED: web/vitest.config.ts:28-33]`
- **Mutation spot-check ≥70%** on critical files (Stryker, curated list in `vitest.stryker.config.ts`). `[VERIFIED: web/vitest.stryker.config.ts]`
- **PRD-first (absolute):** D-19 PRD-amendment commit lands **before** any code (new WEBART-05..08 group + sidebar surface + 2 web deps + sandboxed-iframe policy + D-14/D-15 behavior).
- **Prefer industrial libraries over bespoke** (MEMORY) — honored: reuse `Drawer`, `Dialog`, `listThreadAssets`, `useDefaultLayout`, mirror `onUsage`/`attachAssetsToUserMessages`.

---

## Phase Requirements

| ID | Description (from REQUIREMENTS.md:69-72) | Research Support |
|----|------------------------------------------|------------------|
| WEBART-05 | Right-side "Artefatti" panel in the `AppShell` `ResizablePanelGroup`, lists thread assets (name/size/mime/icon) newest-first, graceful empty-state, collapses to a drawer on mobile without breaking layout | Pattern 1 (dynamic 3rd `ResizablePanel` via v4 `panelIds`), Pattern 6 (Drawer `side='right'`), client-side newest-first sort (server is ASC), D-16 icon map via `CitationBubble` precedent |
| WEBART-06 | Each row downloads via `GET /api/assets/{id}/download`; "Scarica tutto" sequential client-side; no host/container path | Pattern 7 (throttled `<a download>` loop, skip degraded), reuse the 37A-proven anchor from `LocalArtifactDisplay.tsx:66` |
| WEBART-07 | Single derived view = `GET /api/assets?thread_id=` + live `aura.artifact` merge (no new store); ownership via `GetForIdentity`; non-owner 404 | Pattern 2 (`useQuery(['assets',threadId])` + `onArtifact`→invalidate), backend already identity-scoped `[VERIFIED: assets_api.go:171 + store.go:76]` |
| WEBART-08 | Non-regression: inline `local_artifact` chip keeps rendering; CLI/no-identity degrades. React unit (panel render + download-all) + Playwright e2e + web ≥85% | Pattern 3 (D-15 split-fold rehydration), Validation Architecture below |

---

## Summary

37B is **pure frontend** over a fully-shipped 37A substrate (asset ingest, `GET /api/assets/{id}/download`, `GET /api/assets?thread_id=`, the `aura.artifact` SSE event carrying `asset_id`/`filename`/`size_bytes`/`mime_type`). No backend change is expected — verified: the list endpoint is identity-scoped and the download route is attachment/octet-stream/nosniff/404-on-non-owner. `[VERIFIED: internal/agui/assets_api.go, internal/assets/store.go]`

The single riskiest mechanic — a toggleable third `ResizablePanel` entering/leaving the group at runtime — is **cleanly solved by the installed library** (`react-resizable-panels@4.12.0`, a **v4 rewrite** with a different API than v2/v3). Its `useDefaultLayout({ id, panelIds })` hook **auto-namespaces persisted layouts by the set of panel ids** (`react-resizable-panels:<id>:<id1>:<id2>[:<id3>]`), so the 2-panel and 3-panel arrangements persist under distinct keys with **no layout-key bump and zero disruption to existing users** — provided `panelIds` is driven dynamically to match the currently-mounted set. `[VERIFIED: node_modules/react-resizable-panels/dist/react-resizable-panels.js:1786-1868 + .d.ts:433-452]`

Three factual corrections to CONTEXT.md that change concrete steps: (1) **SheetJS must be installed from `cdn.sheetjs.com` (≥0.20.2), NOT `npm install xlsx`** — the public-npm `xlsx` is frozen at 0.18.5 with two unpatched CVEs. (2) The server list query is `ORDER BY created_at ASC` (**oldest-first**), so the panel must sort **newest-first client-side** (do NOT change the shared query — it is load-bearing for the existing user-upload fold). (3) The inline-chip rehydration (D-15) must **split assets by `source_kind`**, because the existing `attachAssetsToUserMessages` currently folds *all* thread assets (including post-37A `source_kind='agent'` deliverables) onto **user** turns — a latent mis-render.

**Primary recommendation:** Build the panel as a `threadId`-keyed `useQuery` over `listThreadAssets` (client-sorted newest-first, filtered to `source_kind='agent'`), a dynamically-membered third `ResizablePanel` (dynamic `panelIds`, artifacts panel `id="chat-artifacts"` + `defaultSize="19rem"`, no key bump), a Radix-`Dialog` preview modal (className-overridden to ~90vw/90vh) with lazy per-MIME renderer chunks, and an `onArtifact` callback threaded through `streamRun` (mirroring `onUsage`) that invalidates `['assets',threadId]` and drives the one-time auto-open. Fix D-15 by splitting `attachAssetsToUserMessages` and adding a positional assistant-side fold + a small assistant-attachment renderer.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Thread asset list (source of truth) | API / Backend (`GET /api/assets?thread_id=`) | — | Already identity-scoped via `ListForThread`→`GetForIdentity`; no client store. `[VERIFIED: assets_api.go:171]` |
| Derived live view + ordering | Browser (TanStack Query cache + client sort) | API (refetch) | Server stays authoritative; client only re-orders newest-first (server is ASC). |
| Download (per-row + all) | Browser (`<a download>` → API stream) | API (stream-through) | 37A download route is the auth path; browser triggers, server streams. |
| File preview render | Browser (blob-URL + lazy renderer chunks) | — | D-06/D-07 client-side; zero backend. bytes never re-served as top-level doc. |
| HTML/xlsx-table execution isolation | Browser (null-origin sandboxed `<iframe>`) | — | D-07 — untrusted markup runs in a null origin, cannot reach our session. |
| Live-merge trigger | Frontend Server chunk (`sseAdapter` `aura.artifact` frame) → Browser (`onArtifact`→invalidate) | API (refetch) | Mirrors the shipped `onUsage`/`invalidateRuntimeReads` pattern. |
| Saved-conversation persistence | Browser (`threadId`-keyed query = durable; positional fold = inline chip) | API (list + snapshot) | D-14 durable by query key; D-15 best-effort chip via message metadata. |

**Tier-correctness callouts for the planner/plan-checker:** No capability belongs on the API tier that isn't already there (37A shipped it). Do **not** add a backend endpoint (no zip route — SC#2 YAGNI; no thumbnail route — deferred). All new work is Browser-tier + one Frontend-Server-chunk callback (`sseAdapter`/`ExternalStoreChat`).

---

## Standard Stack

### Core (already installed — verified in `web/package.json`)
| Library | Version | Purpose | Note |
|---------|---------|---------|------|
| `react` / `react-dom` | 19.2.x | UI | React 19 (`use`, Actions, RSC-era hooks available; Babel react-compiler on) `[VERIFIED: package.json:47-48]` |
| `react-resizable-panels` | **4.12.0** | Resizable group | **v4 API** — `Group`/`Panel`/`Separator`, `useDefaultLayout`, CSS-unit sizes. NOT the v2/v3 `PanelGroup`/percent API. `[VERIFIED: node_modules/.../package.json + .d.ts]` |
| `@tanstack/react-query` | 5.101.x | Derived asset view (D-10) | shared `queryClient` at `src/queryClient.ts`; test wrapper pattern `new QueryClient({defaultOptions:{queries:{retry:false}}})` `[VERIFIED]` |
| `@radix-ui/react-dialog` | 1.1.17 | Preview modal (D-09) | wrapped in `components/ui/dialog.tsx`; Radix owns focus-trap+Esc+backdrop+aria `[VERIFIED: dialog.tsx:8]` |
| `lucide-react` | 1.21.x | Category icons (D-16) | `File`,`FileText`,`Code2`,`Globe` proven in `CitationBubble.tsx:2`; `FileSpreadsheet`/`FileImage`/`FileCode` also available |
| `i18next` / `react-i18next` | 26 / 17 | Copy | en+it TS resource modules; **parity test enforces both** `[VERIFIED: i18n/__tests__/resources.parity.test.ts]` |
| `clsx`+`tailwind-merge` (`cn`) | — | class merge | `cn()` lets a passed `className` override the Dialog's default `max-w-lg` for the 90vw/90vh modal |

### Supporting (NEW web deps — D-07 preview renderers, lazy-loaded per D-08)
| Library | Version | Purpose | Install (EXACT) |
|---------|---------|---------|-----------------|
| `docx-preview` | **0.4.0** (Apache-2.0) | docx → HTML from Blob/ArrayBuffer (D-07 docx) | `npm i docx-preview` — pulls `jszip>=3.0.0` (new transitive dep; not currently present `[VERIFIED: npm ls jszip → empty]`) |
| `xlsx` (SheetJS CE) | **0.20.3** (Apache-2.0) | xlsx parse → `sheet_to_html` (D-07 xlsx) | **`npm i https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz`** — do **NOT** `npm i xlsx` (frozen 0.18.5, 2 CVEs). See Pitfall 2. |

**docx-preview API** `[CITED: github.com/VolodymyrBaydalka/docxjs]`:
```
renderAsync(data: Blob|ArrayBuffer|Uint8Array, bodyContainer: HTMLElement, styleContainer?: HTMLElement, options?: Options): Promise<WordDocument>
// import * as docx from 'docx-preview'
// options: { className='docx', inWrapper=true, ignoreLastRenderedPageBreak=true, ignoreWidth/Height/Fonts, breakPages, useBase64URL, renderHeaders/Footers, debug }
```
Styles inject into `styleContainer` (falls back to `bodyContainer`); **all CSS rules are prefixed by `className`** so a custom `className: 'aura-docx'` scopes them by class (not DOM position) — safe inside the modal, no `document.head` leak.

**SheetJS API** `[CITED: docs.sheetjs.com]`:
```
import { read, utils } from 'xlsx'
const wb = read(arrayBuffer, { type: 'array' })          // ArrayBuffer path
wb.SheetNames.forEach(n => utils.sheet_to_html(wb.Sheets[n]))  // one <table> per sheet
```

### Alternatives Considered (already vetted in CONTEXT canonical_refs — do not re-explore)
`react-file-viewer` (dead 2019), `@cyntler/react-doc-viewer` (frozen + needs public URL), `@onlyoffice/document-editor-react` (self-hosted 2GB Document Server, RAM-blocked), Nutrient/Apryse (paid WASM). Client-side MIME-gated path chosen. pdf.js/react-pdf **not** used — native `<iframe>` on blob URL renders PDF with zero bundle.

**Version verification performed:** `npm view docx-preview` → 0.4.0 / Apache-2.0 / dep `jszip>=3.0.0` / unpacked 975 KB `[VERIFIED: npm registry 2026-07-08]`. `npm view xlsx` → 0.18.5 / Apache-2.0 / last-modified 2024-10-22 (frozen) `[VERIFIED: npm registry 2026-07-08]`. Current 0.20.3 via CDN `[VERIFIED: WebSearch → docs.sheetjs.com + cdn.sheetjs.com]`.

---

## Package Legitimacy Audit

Ran `slopcheck install docx-preview xlsx jszip` (slopcheck 0.6.1, 2026-07-08). Scan completed **3 OK** before the install subprocess (Windows `npm` PATH quirk aborted only the install step, not the scan). `[VERIFIED: slopcheck 0.6.1]`

| Package | Registry | Age / Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----------------|-------------|-----------|-------------|
| `docx-preview` | npm | est. 4+ yrs, ~100k/wk | github.com/VolodymyrBaydalka/docxjs | [OK] | **Approved** (Apache-2.0; lazy-load) |
| `xlsx` (SheetJS CE) | **cdn.sheetjs.com** (not npm) | 8+ yrs, tens of M/wk | git.sheetjs.com/sheetjs/sheetjs | [OK]* | **Approved — install from CDN ≥0.20.2** |
| `jszip` | npm (transitive of docx-preview) | 10+ yrs, ~13M/wk | github.com/Stuk/jszip | [OK] | Approved (arrives via docx-preview) |

*slopcheck rated the **npm** `xlsx` [OK] (it is the legitimate SheetJS package) — but that registry copy is **frozen 0.18.5 with CVE-2023-30533 (proto-pollution, HIGH) + CVE-2024-22363 (ReDoS)**. Legitimacy ≠ currency: the **CDN 0.20.3** (same publisher, current, both CVEs fixed) is the required source. See Pitfall 2.

**Removed (SLOP):** none. **Flagged (SUS):** none for legitimacy — `xlsx` is flagged for **currency/CVE**, mitigated by installing from CDN.

**Planner action:** Since these names originate from CONTEXT.md discussion (not an authoritative session lookup), gate the two direct installs behind a `checkpoint:human-verify` **only if** you want the operator to confirm the CDN-tarball approach for `xlsx` (recommended, because a CDN dependency in `package.json` is an unusual supply-chain shape a reviewer should see). `docx-preview`+`jszip` are ordinary registry installs.

---

## Architecture Patterns

### System Architecture Diagram (data flow)

```
                         ┌───────────────────────────── AppShell.tsx ─────────────────────────────┐
 live run                │                                                                          │
 POST /agent/run ──SSE──▶ ExternalStoreChat ──onArtifact(assetId?)──▶ [invalidate ['assets',tid]]  │
   (aura.artifact frame)  │   │  (mirror of onUsage)                    └─▶ [auto-open panel ×1]    │
   sseAdapter.ts:345 ─────┘   │                                                                      │
                              ▼                                                                      │
 GET /api/assets?thread_id ─▶ useQuery(['assets',tid], listThreadAssets)                            │
   (server: created_at ASC) ─────────▶ select: filter source_kind==='agent' + sort DESC (newest)   │
                              │                        │                                             │
                              │                        ▼                                             │
                              │              ArtifactsPanel (3rd ResizablePanel  |  right Drawer)    │
                              │                 rows: icon+name+cat·EXT + ⬇        (mobile <lg)       │
                              │                   │ row body click        │ ⬇ click / "Scarica tutto"│
                              │                   ▼                       ▼                          │
                              │           PreviewModal (Radix Dialog 90vw/90vh)   <a download> loop  │
                              │            fetch(cred:same-origin) → branch by MIME:                 │
                              │              image/pdf → objectURL → <img>/<iframe>                  │
                              │              text/html → res.text() → <pre> / sandboxed <iframe>     │
                              │              docx     → Blob   → docx-preview.renderAsync (lazy)     │
                              │              xlsx     → ArrayBuffer → sheet_to_html → sandboxed iframe│
                              │              pptx/svg/else → download-only card                      │
 saved-conv open:            │                                                                      │
 GET /threads/{id}/messages ─┴─▶ snapshotToThreadMessages ──(D-15)── split assets by source_kind:  │
                                   attachAssetsToUserMessages(uploads)  +  foldAgentOntoAssistant()  │
                                                                                                     ┘
```
(Component-to-file mapping is in the Recommended Structure below, not the diagram.)

### Recommended Project Structure (planner discretion, ≤600 LOC each)
```
web/src/chat/artifacts/            # NEW — the panel feature
├── useThreadArtifacts.ts          # useQuery(['assets',threadId]) + select(filter agent + sort DESC)
├── artifactMeta.ts                # previewKind(mime,filename) + categoryLabel/icon (D-16) — PURE, mutation-tested
├── ArtifactsPanel.tsx             # list container: header ("Artefatti" + "Scarica tutto"), rows, empty/degraded states
├── ArtifactRow.tsx                # icon + name + "Categoria · EXT" + trailing ⬇ (row body → preview)
├── downloadAll.ts                 # throttled sequential <a download> loop + N/M progress — PURE-ish, mutation-tested
├── useBlobPreview.ts              # fetch→relabel Blob→objectURL→revoke (D-06) — cleanup-correct
├── PreviewModal.tsx               # Radix Dialog (90vw/90vh) + per-MIME dispatch + loading/error
└── renderers/                     # D-08 lazy chunks (one React.lazy import each)
    ├── ImagePreview.tsx           #   blob <img> (SVG excluded upstream)
    ├── PdfPreview.tsx             #   native <iframe src={objectURL}>
    ├── TextPreview.tsx            #   <pre>{text}</pre>
    ├── HtmlPreview.tsx            #   <iframe srcDoc={text} sandbox="allow-scripts">  (null origin)
    ├── DocxPreview.tsx            #   docx-preview.renderAsync (docx-preview+jszip land ONLY here)
    └── XlsxPreview.tsx            #   SheetJS read→sheet_to_html → rendered in sandboxed <iframe srcDoc>
```
Panel toggle state (open/closed, persisted) + `onArtifact` wiring live in `AppShell.tsx`; D-15 fold lives in `ExternalStoreChat.tsx`; the `onArtifact` frame-signal lives in `sseAdapter.ts`/`ExternalStoreChat.tsx`.

---

### Pattern 1 — Dynamically-membered third `ResizablePanel` (THE riskiest mechanic; resolved)

**What:** Add a toggleable 3rd panel to the existing 2-panel `ResizablePanelGroup` in `AppShell.tsx:441` without corrupting the persisted 2-panel layout.

**Verified library mechanics (v4.12.0):** `[VERIFIED: node_modules/react-resizable-panels/dist/react-resizable-panels.js]`
- Storage key builder: `` `react-resizable-panels:${[id, ...panelIds].join(":")}` `` (`he()`, line 1786-1788).
- **Read** key uses the passed `panelIds` (line 1831); **save** key uses `Object.keys(currentLayout)` = the panels actually rendered (line 1866). They match **iff** the passed `panelIds` equals the rendered set in DOM order.
- The `.d.ts` documents `panelIds` for exactly this case: *"For Groups that contain conditionally-rendered Panels, this prop can be used to save and restore multiple layouts… Panel ids must match the Panels rendered within the Group during mount."* (line 446-452).
- **No `order` prop exists in v4** (checked `PanelProps`) — DOM order + `id` is the mechanism. The CONTEXT.md gotcha's "explicit id/order" is a v2/v3 memory; v4 needs only stable `id`.

**Answer to the discretion question (D-02 / code_context gotcha): NO layout-key bump is needed.** Keep `CHAT_SHELL_LAYOUT_ID='aura-chat-shell-v3'`. Drive `panelIds` **dynamically**:

```tsx
// AppShell.tsx
const CHAT_SHELL_PANEL_IDS_BASE = ['chat-navigation', 'chat-workspace'] as const;
const [artifactsOpen, setArtifactsOpen] = useState(false);           // persisted separately (localStorage), per D-03
const panelIds = artifactsOpen
  ? [...CHAT_SHELL_PANEL_IDS_BASE, 'chat-artifacts']                 // DOM order — rightmost last
  : [...CHAT_SHELL_PANEL_IDS_BASE];
const chatShellLayout = useDefaultLayout({
  id: CHAT_SHELL_LAYOUT_ID, panelIds, onlySaveAfterUserInteractions: true,
});
// …inside the group, AFTER the workspace panel:
{artifactsOpen && (
  <>
    <ResizableHandle id="chat-artifacts-resizer" aria-label={t('shell.resizeArtifacts')} withHandle />
    <ResizablePanel
      id="chat-artifacts"
      defaultSize="19rem" minSize="16rem" maxSize="32rem"      // D-02 suggested tokens
      groupResizeBehavior="preserve-pixel-size"                 // like the nav rail
      className="h-full min-h-0"
    >
      <ArtifactsPanel threadId={activeThreadId} onClose={() => setArtifactsOpen(false)} />
    </ResizablePanel>
  </>
)}
```

**Why it is safe / non-disruptive:**
- Existing users' saved 2-panel width lives at `…v3:chat-navigation:chat-workspace` and is **untouched** (same key when closed).
- The 3-panel arrangement gets a fresh key `…v3:chat-navigation:chat-workspace:chat-artifacts`; first open auto-distributes using the artifacts panel's `defaultSize="19rem"` (hook returns `undefined` defaultLayout for an unseen key → Group falls back to panel `defaultSize` — line 1481). Thereafter the resized width persists under that key.
- Constraint satisfied: v4 requires ≥1 panel with `preserve-relative-size`; `chat-workspace` has no `groupResizeBehavior` (defaults to relative) `[VERIFIED: .d.ts:313]`, so nav+artifacts can both be `preserve-pixel-size`.

**Must-dos:** pass `panelIds` in DOM order; every Panel and Separator keeps a stable `id` (existing two already do); mount the artifacts `ResizableHandle` **before** the artifacts `ResizablePanel`; render both only inside the existing `showConversationNavigation` (chat surface) branch (`AppShell.tsx:439`).

### Pattern 2 — Derived live view (D-10/D-11)
```tsx
// useThreadArtifacts.ts
export function useThreadArtifacts(threadId: string) {
  return useQuery({
    queryKey: ['assets', threadId],
    enabled: threadId.length > 0,
    queryFn: ({ signal }) => listThreadAssets(threadId, signal),   // reuse api.ts:47 verbatim
    select: (assets) => assets
      .filter((a) => a.source_kind === 'agent' && a.status !== 'deleted' && a.status !== 'canceled')
      .slice()
      .sort((a, b) => (b.created_at ?? '').localeCompare(a.created_at ?? '')),  // newest-first CLIENT-SIDE
  });
}
```
Live-merge = **invalidate, not push** (D-11): in `AppShell`, on `onArtifact` → `queryClient.invalidateQueries({ queryKey: ['assets', threadId] })`. 37A persists to Garage+DB **before** emitting the frame, so the refetch always sees the new row. No manual dedup/order (server is the set-of-record).

### Pattern 3 — `onArtifact` signal + D-15 split-fold rehydration
**onArtifact (mirror `onUsage`):** thread an optional callback through `streamRun`/`streamPost` → `streamSSE`, fired in the frame loop when an `aura.artifact` CUSTOM frame with a valid descriptor is seen (right where `reduceFrame` runs, `sseAdapter.ts:512-514`). `ExternalStoreChat` exposes `onArtifact?: (assetId: string | undefined) => void`; `AppShell` passes a handler that invalidates the query and drives the **one-time** auto-open (a `useRef` keyed by threadId, reset on thread change — mirror `autoOpenedOnboarding` at `AppShell.tsx:150`).

**D-15 — split by source_kind (correctness fix, not just a parallel fold):**
`attachAssetsToUserMessages` (`ExternalStoreChat.tsx:62`) currently folds **all** non-deleted assets onto **user** messages. Post-37A the thread list also contains `source_kind='agent'` deliverables → they would wrongly attach to user turns. The fix on history load (`ExternalStoreChat.tsx:303`):
```ts
const uploads = assets.filter((a) => a.source_kind !== 'agent');   // web/telegram/cli
const agent   = assets.filter((a) => a.source_kind === 'agent');
setMessages(foldAgentOntoAssistant(attachAssetsToUserMessages(loaded, uploads), agent));
```
`foldAgentOntoAssistant` mirrors the positional heuristic but targets **assistant** messages and writes `metadata.custom.attachments` (same envelope `withMessageAttachments` uses). A small assistant-side renderer reads `messageAttachments(message)` and renders the same authenticated download chip as `LocalArtifactDisplay` (reuse the anchor at `LocalArtifactDisplay.tsx:66`). **Root cause of the bug** `[VERIFIED: sseAdapter_snapshot.ts:96-120]`: `toolCallsFromSnapshot` only re-attaches a tool-part `display` when the persisted snapshot carries one; send_file's `local_artifact` display is synthesized **client-side** from the live `aura.artifact` frame and is never persisted → the tool card rehydrates without `asset_id`. The panel (D-14) is the durable fix; the fold is best-effort chip parity.

### Pattern 4 — Preview modal (D-09) reuses Radix `Dialog`, not the custom Drawer trap
```tsx
<Dialog open={!!active} onOpenChange={(o) => !o && setActive(undefined)}>
  <DialogContent className="h-[90vh] w-[90vw] max-w-[90vw] gap-0 p-0">  {/* cn() overrides default max-w-lg */}
    <DialogHeader className="flex-row items-center gap-3 border-b border-border px-4 py-3">
      <DialogTitle className="truncate">{active.file_name}</DialogTitle>
      <a href={`/api/assets/${active.id}/download`} download={active.file_name} …>⬇</a>
    </DialogHeader>
    <div className="min-h-0 flex-1 overflow-auto">{/* per-MIME renderer (Suspense-wrapped lazy) */}</div>
  </DialogContent>
</Dialog>
```
Radix supplies focus-trap + Esc + backdrop + `aria-labelledby` `[VERIFIED: dialog.tsx:8]`. Its content is `z-[100]` over overlay `z-[90]`, both above the `z-50` Drawer — so a preview opened from within the mobile Drawer layers correctly `[VERIFIED: dialog.tsx:26,51]`. Include a `DialogDescription` (or `aria-describedby`) to silence Radix's a11y warning.

### Pattern 5 — Blob-URL lifecycle & the fetch/branch table (D-06)
One `fetch(url, { credentials: 'same-origin', signal })`, then branch by target kind (image/pdf need an object URL; text/html need text; docx needs the Blob; xlsx needs an ArrayBuffer):

```tsx
// useBlobPreview.ts — for image/pdf ONLY (the object-URL kinds)
useEffect(() => {
  const ctrl = new AbortController(); let objectUrl: string | undefined;
  (async () => {
    const res = await fetch(`/api/assets/${assetId}/download`, { credentials: 'same-origin', signal: ctrl.signal });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const raw = await res.blob();
    const blob = mimeType ? new Blob([raw], { type: mimeType }) : raw;   // relabel: server serves octet-stream
    objectUrl = URL.createObjectURL(blob); setUrl(objectUrl);
  })().catch((e) => { if (!ctrl.signal.aborted) setError(String(e)); });
  return () => { ctrl.abort(); if (objectUrl) URL.revokeObjectURL(objectUrl); };  // revoke on unmount / rapid re-open
}, [assetId, mimeType]);
```
`AbortController` + `revokeObjectURL` in the same cleanup prevents leaks on rapid open/close. docx/xlsx do **not** use an object URL (they consume bytes directly) — no revoke needed there.

### Pattern 6 — Mobile Drawer (D-04) through the existing overlay reducer
Reuse `<Drawer side="right" title={t('artifacts.title')} …>` verbatim (`Drawer.tsx:25` already supports `side='right'`, portal+backdrop+focusTrap+Esc+scroll-lock). Route it through `useSurfaceRestore`'s **currently-unused** overlay slot (`openOverlay`/`closeOverlay`/`overlayOpen`, `useSurfaceRestore.ts:75-81`) so it obeys "one heavy overlay at a time" against the nav drawer. Below `lg`, render the panel content in the Drawer; at/above `lg`, render it as the `ResizablePanel`. Same header toggle drives both.

### Pattern 7 — "Scarica tutto" (D-13)
```ts
export async function downloadAll(assets: Asset[], onProgress: (done: number, total: number) => void, opts?: { delayMs?: number; signal?: AbortSignal }) {
  const delay = opts?.delayMs ?? 500;                                   // 400–600ms avoids Chromium multi-download burst-block
  const rows = assets.filter((a) => a.status === 'accepted');          // skip degraded (D-18)
  for (let i = 0; i < rows.length; i++) {
    if (opts?.signal?.aborted) break;
    const a = rows[i]; const link = document.createElement('a');
    link.href = `/api/assets/${a.id}/download`; link.download = a.file_name;
    document.body.appendChild(link); link.click(); link.remove();
    onProgress(i + 1, rows.length);
    if (i < rows.length - 1) await new Promise((r) => setTimeout(r, delay));
  }
}
```
Button disabled during the run; continue-on-individual-error (each click is independent). No server zip (SC#2). First multi-download may prompt the browser's "allow multiple downloads" — expected, one-time per origin.

### Anti-Patterns to Avoid
- **Changing the server list order to DESC.** `ListAssetsForThread` is `created_at ASC` and that order is load-bearing for `attachAssetsToUserMessages`' positional zip `[VERIFIED: queries/assets.sql:23-28]`. Sort in the panel's `select`, not the query.
- **`npm install xlsx`.** Pulls frozen 0.18.5 (2 CVEs). Use the CDN tarball. (Pitfall 2)
- **Static `panelIds` with a conditional panel.** Read/save keys diverge → layout shift. Drive `panelIds` dynamically. (Pattern 1)
- **Pointing `<iframe src>` at the download URL for HTML.** The `attachment` disposition forces a download, not a render. Must `fetch()`→`srcDoc`. (D-07)
- **`allow-scripts` + `allow-same-origin` together.** That lets sandboxed script remove its own sandbox. Use `allow-scripts` alone (null origin). (Pattern below / D-07)
- **Blob `<img>` for SVG.** Executes embedded `<script>` in our origin — SVG is download-only (D-07). Gate it in `previewKind()` before the `image/*` branch.
- **Rendering `object_key`/`object_bucket`.** The list JSON includes internal store keys (`Asset` Go struct serializes them `[VERIFIED: types.go:88-90]`). The panel must use only `id` for the download URL and never surface storage keys.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Multi-layout persistence for the toggleable panel | A custom localStorage layout keyer | `useDefaultLayout({ id, panelIds })` (dynamic panelIds) | v4 already namespaces per panel-set; hand-rolling re-introduces the exact drift the library solved. |
| Modal focus-trap / Esc / backdrop | Reuse `a11y/focusTrap.ts` by hand | `components/ui/dialog.tsx` (Radix) | Radix ships WAI-ARIA-correct trap; the custom trap is for the portal Drawer only. |
| Thread asset fetch | A new fetch client | `listThreadAssets` (`api.ts:47`) | Already identity-scoped, AbortSignal-ready. |
| Auth download | `fetch`→blob→`FileSaver` | same-origin `<a href download>` | 37A-proven auth path; no blob buffering; RFC-6266 filename handled server-side. |
| docx render | OOXML parser | `docx-preview` | Full DrawingML/tables/styles; 4+ yrs maintained. |
| xlsx render | XLSX parser | SheetJS `read`+`sheet_to_html` | Formats/dates/merged cells handled; escaping built in. |
| PDF render | pdf.js/react-pdf integration | native `<iframe src={objectURL}>` | Zero bundle; browsers render PDF natively. |
| Icon-per-category | New icon set | `lucide-react` + the `CitationBubble.tsx:27` `Partial<Record<…,LucideIcon>>` + `File` fallback | Established precedent (D-16). |

**Key insight:** every hard part here already has a shipped, tested owner in this repo or a mature lib — 37B is an aggregation/parity layer, not new infrastructure.

---

## Common Pitfalls

### Pitfall 1 — Treating `react-resizable-panels` as the v2/v3 library
**What goes wrong:** Applying `PanelGroup`/`Panel`/`PanelResizeHandle`, numeric percent `defaultSize`, `direction`, `autoSaveId`, `order`, `collapsedSize` from memory. **Why:** v4.12.0 is a **ground-up rewrite** — `Group`/`Panel`/`Separator`, `useDefaultLayout`, CSS-unit string sizes, `orientation`, `groupResizeBehavior`, `resizeTargetMinimumSize`; **no `order`/`autoSaveId`**. **Avoid:** mirror the *existing* usage at `AppShell.tsx:441-476` exactly; consult the bundled `.d.ts` (it is the source of truth for this pinned version). **Warning sign:** TS errors on `direction`/`order`, or persisted layout not restoring.

### Pitfall 2 — `xlsx` from the public npm registry (frozen + CVEs)
**What goes wrong:** `npm i xlsx` installs 0.18.5 (last modified 2024-10-22) carrying **CVE-2023-30533** (prototype pollution, HIGH; fixed 0.19.3) and **CVE-2024-22363** (ReDoS; fixed 0.20.2). **Why:** SheetJS moved current releases off npm (token/2FA + legal); the registry copy is stuck. **Avoid:** install the CDN tarball `npm i https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz` (Apache-2.0, both CVEs fixed). If any transitive dep drags 0.18.5, pin via `package.json` `overrides`. **Warning sign:** `npm audit` HIGH on `xlsx`; `xlsx` resolving to 0.18.5 in the lockfile. `[VERIFIED: npm registry + WebSearch docs.sheetjs.com/security]`

### Pitfall 3 — `source_kind='agent'` invisible to the frontend type + fold
**What goes wrong:** The TS `Asset.source_kind` union is `'web'|'telegram'|'cli'` — **missing `'agent'`** (37A widened the backend via migration 0035 but not the TS type) `[VERIFIED: web/src/chat/attachments/types.ts:19 vs internal/assets/types.go:70]`. Filtering `source_kind==='agent'` type-errors, and `attachAssetsToUserMessages` folds agent deliverables onto user turns. **Avoid:** widen the union to include `'agent'`, then split the fold (Pattern 3). **Warning sign:** agent files appearing as *user* attachments on saved-conversation open.

### Pitfall 4 — Coverage regression from lazy renderer chunks
**What goes wrong:** The 85% gate spans **all** `src/**`; thin renderer wrappers that call `renderAsync`/`sheet_to_html` into the DOM are hard to exercise in jsdom → they drag the aggregate under 85% and fail `npm test`. **Why:** `include: ['src/**/*.{ts,tsx}']`, no per-file carve-out `[VERIFIED: vitest.config.ts:22]`. **Avoid:** keep the untestable DOM-injection glue minimal and either (a) unit-test it with `docx-preview`/`xlsx` **mocked** (`vi.mock`) asserting the wrapper calls the lib with the right args + renders loading/error, or (b) wrap the irreducible glue in `/* v8 ignore start … stop */`. Put all the *decision* logic (`previewKind`, category/icon map, `downloadAll` sequencing, blob relabel) in **pure** modules that test to 100%. **Warning sign:** coverage dropping toward 84.x after adding `renderers/`.

### Pitfall 5 — Blob-URL leaks on rapid open/close
**What goes wrong:** Opening/closing previews fast without revoking leaks object URLs (memory) and can render a stale file. **Avoid:** `AbortController` + `URL.revokeObjectURL` in the **same** `useEffect` cleanup keyed on `assetId` (Pattern 5). **Warning sign:** growing `blob:` entries in DevTools memory; wrong file flashing on quick switches.

### Pitfall 6 — Auto-open firing on every artifact / every remount
**What goes wrong:** D-03 says auto-open **once** per thread; naive wiring reopens the panel after the user closed it, or on every `aura.artifact` frame. **Avoid:** a `useRef<Set<string>>`/threadId-keyed ref guard (mirror `autoOpenedOnboarding` at `AppShell.tsx:150`), reset on thread change; auto-open only transitions closed→open when the ref hasn't fired for this thread. **Warning sign:** panel "won't stay closed."

---

## Code Examples

### Sandboxed HTML preview (D-07) — null-origin isolation
```tsx
// HtmlPreview.tsx  (htmlText came from res.text(), NOT an object URL)
export default function HtmlPreview({ htmlText, title }: { htmlText: string; title: string }) {
  return (
    <iframe
      srcDoc={htmlText}
      // allow-scripts WITHOUT allow-same-origin → the frame is a *null* origin:
      // scripts run, but document.cookie is empty, window.parent is cross-origin (throws),
      // and fetch('/api/…') has no ambient session — it cannot touch our cookies/DOM/Garage.
      sandbox="allow-scripts"
      className="h-full w-full border-0 bg-white"
      title={title}
    />
  );
}
```
`src` cannot point at `/api/assets/{id}/download` — its `Content-Disposition: attachment` forces a download rather than a render `[VERIFIED: assets_api.go:55]`; hence `fetch()`→`srcDoc`. (No app-wide CSP `frame-src` restriction was found that would block a `srcdoc` frame; the planner should confirm no CSP header is later added that omits `frame-src 'self'`/`blob:` — currently none observed in the web serve path.)

### xlsx rendered inside the SAME sandboxed frame (defense-in-depth)
```tsx
// XlsxPreview.tsx  (data = ArrayBuffer)
import { read, utils } from 'xlsx';
const wb = read(data, { type: 'array' });
const body = wb.SheetNames.map((n) =>
  `<section><h3>${escapeHtml(n)}</h3>${utils.sheet_to_html(wb.Sheets[n])}</section>`).join('');
// render `body` as srcDoc in a sandbox="allow-scripts"-free iframe (static table → no scripts needed):
return <iframe srcDoc={`<!doctype html><meta charset=utf-8><style>…</style>${body}`} sandbox="" className="h-full w-full border-0 bg-white" title={filename} />;
```
`sheet_to_html` HTML-escapes cell **values**; we escape the sheet **name** ourselves. Rendering the table string inside an **empty-`sandbox`** iframe (no allow-scripts) is a hardened default — even if escaping regressed, nothing executes. (See Assumption A1 — escaping is asserted, verify in the installed version.)

### docx lazy renderer (docx-preview + jszip land only in this chunk)
```tsx
// DocxPreview.tsx  (blob = the fetched Blob; docx-preview reads bytes directly)
import { renderAsync } from 'docx-preview';
export default function DocxPreview({ blob }: { blob: Blob }) {
  const body = useRef<HTMLDivElement>(null), style = useRef<HTMLDivElement>(null);
  const [err, setErr] = useState<string>();
  useEffect(() => {
    const b = body.current; if (!b) return; let dead = false;
    renderAsync(blob, b, style.current ?? undefined, { className: 'aura-docx', inWrapper: true, ignoreLastRenderedPageBreak: true })
      .catch((e) => { if (!dead) setErr(String(e)); });
    return () => { dead = true; if (b) b.innerHTML = ''; };
  }, [blob]);
  return err ? <ErrorCard msg={err} /> : <div className="overflow-auto"><div ref={style} /><div ref={body} /></div>;
}
// Consumer: const DocxPreview = lazy(() => import('./renderers/DocxPreview'));  // D-08
```

### previewKind — the pure MIME gate (mutation-test target)
```ts
export type PreviewKind = 'image'|'pdf'|'text'|'html'|'docx'|'xlsx'|'download';
const TEXT_EXT = new Set(['txt','md','csv','json','log','sh','yaml','yml','xml','ts','js','py','go']);
export function previewKind(mime: string, filename: string): PreviewKind {
  const ext = filename.toLowerCase().split('.').pop() ?? '';
  if (mime === 'image/svg+xml' || ext === 'svg') return 'download';                 // SVG XSS exclusion FIRST
  if (mime.startsWith('image/')) return 'image';
  if (mime === 'application/pdf' || ext === 'pdf') return 'pdf';
  if (mime === 'text/html' || ext === 'html' || ext === 'htm') return 'html';
  if (ext === 'docx') return 'docx';
  if (ext === 'xlsx') return 'xlsx';
  if (mime.startsWith('text/') || TEXT_EXT.has(ext)) return 'text';
  return 'download';                                                                // pptx + anything else
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `react-resizable-panels` v2/v3 (`PanelGroup`/`Panel`/`PanelResizeHandle`, numeric %, `autoSaveId`, `order`) | v4 (`Group`/`Panel`/`Separator`, `useDefaultLayout`, CSS-unit strings, `panelIds` multi-layout) | v4.x (installed 4.12.0) | Conditional panels are first-class; **no manual key bump**. Training-data v2/v3 snippets are wrong for this repo. |
| `npm i xlsx` for SheetJS | CDN tarball `cdn.sheetjs.com/xlsx-0.20.3/…tgz` | ~2023 (npm freeze at 0.18.5) | Registry install ships 2 unpatched CVEs; CDN is the only current, patched channel. |
| Office preview via public-URL viewers / self-hosted servers | Client-side per-MIME (docx-preview, SheetJS, native iframe) | — | No public URL (private auth preserved); no 2GB server (RAM-blocked); pptx→download-only. |

**Deprecated/outdated for this phase:** `react-file-viewer` (dead 2019); `@cyntler/react-doc-viewer` (frozen + public-URL Office path); pdf.js/react-pdf (unneeded — native iframe).

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `XLSX.utils.sheet_to_html` HTML-escapes cell **values** in 0.20.3 | Pattern/Code (xlsx), Security | LOW — mitigated by rendering the table inside an empty-`sandbox` iframe (no script execution regardless). Verify in the installed version; the sandbox makes it non-load-bearing. |
| A2 | The panel should show **only** `source_kind='agent'` deliverables (not user uploads) | Pattern 2, Open Q1 | MEDIUM — if the product wants "all thread files," change the `select` filter. Recommend agent-only (matches the Claude "Artefatti" reference + D-15's split). |
| A3 | No app-wide CSP header currently restricts `frame-src`/`srcdoc` or `blob:` | Pattern 4/HTML, Security | LOW-MEDIUM — none observed in the serve path, but not exhaustively audited. If a strict CSP is added later, the sandboxed iframe + blob previews need `frame-src 'self' blob:`. |
| A4 | docx-preview license is **Apache-2.0** (CONTEXT.md said "MIT") | Standard Stack | NONE for use (both permissive); note the correction so the PRD-amendment records the right license. |

**These `[ASSUMED]`/refinement items should be confirmed by the planner or surfaced in discuss before locking.** All other claims are `[VERIFIED]` (installed source / registry / running-code reads) or `[CITED]` (official docs).

---

## Open Questions (RESOLVED)

1. **Panel contents: agent-only vs all thread files?** RESOLVED — adopted agent-only; plan 06 `useThreadArtifacts` applies the `source_kind==='agent'` select filter. *Know:* `listThreadAssets` returns uploads (web/telegram/cli) **and** deliverables (agent). *Unclear:* whether "Artefatti" = agent deliverables only or every file in the thread. *Recommendation:* **agent-only** (matches the reference screenshot + D-15's source_kind split); trivially changed in the `select` filter if the operator wants all.
2. **Assistant-turn correlation for D-15 chip.** RESOLVED — adopted the positional fold; plan 05 `foldAgentOntoAssistant` mirrors `attachAssetsToUserMessages` onto assistant turns. *Know:* an `Asset` carries `thread_id`+`created_at` but **no** message/`tool_call_id`. *Unclear:* which assistant turn a rehydrated agent asset belongs to. *Recommendation:* positional fold (mirror `attachAssetsToUserMessages`'s heuristic) — imprecise but parity-adequate; the panel (D-14) is the authoritative durable surface, so chip precision is non-critical.
3. **Degraded rows in the panel.** RESOLVED — render defensively; plan 06 `ArtifactRow` includes the `status !== 'accepted'` degraded branch. *Know:* a fully path-only degraded delivery (37A D-02) creates **no** Asset row → it never appears in the panel (only the inline chip shows its degraded card). *Unclear:* whether D-18's "disabled delivery-unavailable row" is reachable for agent assets (they're `MarkAccepted` on success or absent). *Recommendation:* render the degraded row defensively for any `status !== 'accepted'`, but expect it to be rare; the primary degraded surface stays the inline chip.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Node/npm | build+install | ✓ | node ≥24.16, npm 11.17 (engines) | — |
| `docx-preview` + `jszip` | docx preview | ✗ (to add) | 0.4.0 / >=3.0.0 | none — docx→download-only if not added |
| SheetJS `xlsx` (CDN) | xlsx preview | ✗ (to add) | 0.20.3 | none — xlsx→download-only if not added |
| Vitest + jsdom + RTL | unit tests | ✓ | vitest 4.1.9, jsdom 29, RTL 16.3 | — |
| Playwright (chromium/mobile) | e2e | ✓ | 1.61 | — |
| Stryker | mutation spot-check | ✓ | 9.6.1 | — |
| Live `aura serve` + Postgres + Garage | e2e "artifact in panel + download" (live variant) | ✓ in CI web-e2e job | — | golden-replay `page.route` mock (no live agent turn needed) |

**No missing blocking dependencies.** The two new libs are the only additions; both install cleanly and are lazy-loaded so they never enter the main bundle (D-08).

---

## Validation Architecture

> `workflow.nyquist_validation: true` `[VERIFIED: .planning/config.json:19]` — this section generates VALIDATION.md.

### Test Framework
| Property | Value |
|----------|-------|
| Unit framework | Vitest 4.1.9 + jsdom 29 + @testing-library/react 16.3 (`web/vitest.config.ts`) |
| Config file | `web/vitest.config.ts` (coverage v8, **85% floor on all thresholds**, fails below) |
| Quick run command | `cd web && npx vitest run src/chat/artifacts/` (feature dir) |
| Full suite command | `cd web && npm test` (= `vitest run --coverage`, enforces ≥85%) |
| E2E | Playwright 1.61 (`web/playwright.config.ts`) — chromium + mobile-chrome (+ mobile-safari on HTTPS); golden-replay via `page.route`, SW blocked |
| E2E command | `cd web && npx playwright test e2e/artifacts.spec.ts` |
| Mutation | Stryker 9.6.1 (`web/vitest.stryker.config.ts`) — add pure-logic tests to the curated `mutationTests` list; target ≥70% killed on `artifactMeta.ts`/`downloadAll.ts` |
| Test harness | wrap in `<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>` (repo convention, 30+ examples); i18n en+it parity via `resources.parity.test.ts` |

### Phase Requirements → Test Map
| Req | Behavior | Type | Automated command | File Exists? |
|-----|----------|------|-------------------|--------------|
| WEBART-05 | `previewKind`/category-label/icon map is correct per MIME+ext (incl. SVG→download) | unit (pure) | `npx vitest run src/chat/artifacts/__tests__/artifactMeta.test.ts` | ❌ Wave 0 |
| WEBART-05 | Panel renders rows newest-first from a `['assets',tid]` query; empty-state when none | unit (RTL) | `npx vitest run src/chat/artifacts/__tests__/ArtifactsPanel.test.tsx` | ❌ Wave 0 |
| WEBART-05 | Client-side sort is DESC even though server returns ASC | unit (pure) | covered in `useThreadArtifacts.test.ts` (mock `listThreadAssets` ASC → assert DESC) | ❌ Wave 0 |
| WEBART-05 | `<lg` renders the right `Drawer`; `≥lg` renders the `ResizablePanel` (no layout break) | unit (RTL, matchMedia) + e2e | `ArtifactsPanel.responsive.test.tsx` + `artifacts.spec.ts` mobile project | ❌ Wave 0 |
| WEBART-05 | Toggling artifacts panel does not corrupt the persisted 2-panel layout (distinct storage keys) | unit (RTL, assert localStorage keys) | `AppShell.artifacts.test.tsx` | ❌ Wave 0 |
| WEBART-06 | Per-row `<a>` targets `/api/assets/{id}/download` with `download={file_name}` | unit (RTL) | `ArtifactRow.test.tsx` | ❌ Wave 0 |
| WEBART-06 | `downloadAll` sequences N clicks, throttled, skips degraded, reports N/M | unit (pure, fake timers, spy `HTMLAnchorElement.click`) | `downloadAll.test.ts` | ❌ Wave 0 |
| WEBART-06 | No host/container path in DOM/href (only `id`) | unit (RTL, negative assertion) | `ArtifactRow.test.tsx` / `PreviewModal.test.tsx` | ❌ Wave 0 |
| WEBART-07 | `onArtifact` → `invalidateQueries(['assets',tid])` → refetch shows the new asset | unit (RTL, mock fetch sequence) | `ExternalStoreChat.onArtifact.test.tsx` | ❌ Wave 0 |
| WEBART-07 | Non-owner/nonexistent id → 404 (no unauth surface) | backend (already green) + e2e negative | 37A `TestAssetDownload_NonOwner` (PASS) + `artifacts.spec.ts` 404 route | ✅ (backend) / ❌ e2e |
| WEBART-07 | Preview blob lifecycle: relabel + revoke on unmount; abort on rapid close | unit (RTL, mock `URL.createObjectURL`/`revokeObjectURL`) | `useBlobPreview.test.ts` | ❌ Wave 0 |
| WEBART-07 | Sandboxed HTML iframe has `sandbox="allow-scripts"` and NO `allow-same-origin` | unit (RTL, assert attributes) | `HtmlPreview.test.tsx` | ❌ Wave 0 |
| WEBART-08 | **Regression:** inline `local_artifact` chip still renders (asset_id present + degraded) | unit (RTL) | existing `LocalArtifactDisplay.test.tsx` stays green | ✅ (extend) |
| WEBART-08 | **D-15:** saved-conversation load folds agent assets onto assistant messages, NOT user messages | unit (pure fold + RTL) | `ExternalStoreChat.rehydration.test.tsx` | ❌ Wave 0 |
| WEBART-08 | **Degradation:** CLI/no-identity path unchanged (panel simply empty; chip degraded) | unit (RTL) | `ArtifactsPanel.test.tsx` empty + degraded cases | ❌ Wave 0 |
| WEBART-08 | E2E: artifact appears in panel after `aura.artifact`, download click hits the route | e2e | `npx playwright test e2e/artifacts.spec.ts` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `cd web && npx vitest run <touched dir>` + `npx tsc --noEmit` + `npx eslint --max-warnings=0 <files>`.
- **Per wave merge:** `cd web && npm test` (full unit + ≥85% coverage gate).
- **Phase gate:** full `npm test` green + `npx playwright test e2e/artifacts.spec.ts` green + Stryker ≥70% on `artifactMeta.ts`/`downloadAll.ts` + **`internal/webui/dist` rebuilt & committed** (embedded bundle freshness — 37A precedent, `go build ./...` embeds it) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `src/chat/artifacts/__tests__/artifactMeta.test.ts` — `previewKind` + category/icon map (WEBART-05) — **pure, mutation target**
- [ ] `src/chat/artifacts/__tests__/downloadAll.test.ts` — sequential/throttle/skip-degraded (WEBART-06) — **pure, mutation target**
- [ ] `src/chat/artifacts/__tests__/useThreadArtifacts.test.ts` — ASC→DESC sort + agent filter (WEBART-05/07)
- [ ] `src/chat/artifacts/__tests__/ArtifactsPanel.test.tsx` + `ArtifactRow.test.tsx` — render/empty/degraded/download href
- [ ] `src/chat/artifacts/__tests__/PreviewModal.test.tsx` + per-renderer tests (mock `docx-preview`/`xlsx`, assert lib called + loading/error; sandbox attributes)
- [ ] `src/chat/artifacts/__tests__/useBlobPreview.test.ts` — relabel + revoke + abort (mock `URL.*`)
- [ ] `src/chat/__tests__/ExternalStoreChat.rehydration.test.tsx` + `ExternalStoreChat.onArtifact.test.tsx` — D-15 split-fold + D-11 invalidate
- [ ] `src/__tests__/AppShell.artifacts.test.tsx` — dynamic-panel storage-key isolation + one-time auto-open + mobile Drawer routing
- [ ] `web/e2e/artifacts.spec.ts` — golden-replay `aura.artifact` frame → panel row → download route (mirror `replay.spec.ts` `sseFromFrames` + `assets.spec.ts` fixtures)
- [ ] Add `docx-preview`/`xlsx` **mocks** in the renderer tests (avoid the 85%-gate drag — Pitfall 4)
- [ ] Add `artifactMeta.test.ts` + `downloadAll.test.ts` to `vitest.stryker.config.ts` `mutationTests`
- [ ] Extend `i18n/resources.display.ts` (en+it) with `artifacts.*` keys — the parity test will fail until both are present

---

## Security Domain

`security_enforcement` absent in config ⇒ enabled. This phase's surface is **client-side rendering of agent-produced files** + **authenticated downloads** — the injection/XSS axis dominates.

### Applicable ASVS Categories
| ASVS | Applies | Standard Control (this phase) |
|------|---------|-------------------------------|
| V1 Architecture | yes | No new source of truth; server stays authoritative (D-10). |
| V4 Access Control (IDOR) | yes | Ownership via `GetForIdentity`; non-owner/nonexistent → **404** (existence-hiding), already shipped + tested (37A `TestAssetDownload_NonOwner`). Panel adds no new endpoint. |
| V5 Input Validation / Output Encoding | **yes (primary)** | `previewKind` gates SVG→download; text→`<pre>` (React-escaped); HTML→null-origin sandbox; xlsx table→escaped + empty-sandbox iframe; docx→className-scoped DOM. |
| V6 Cryptography | no | No crypto in this phase. |
| V12 Files & Resources | **yes** | Download served `attachment; octet-stream; nosniff` (37A D-10, unchanged); previews render bytes only in blob/iframe/DOM, never as a top-level document from our origin (D-06). |
| V14 Config | yes | Confirm no CSP addition breaks `srcdoc`/`blob:` (Assumption A3); CDN dep for `xlsx` recorded in PRD-amendment. |

### Known Threat Patterns for {React SPA + client-side file preview}
| Pattern | STRIDE | Standard Mitigation (this phase) |
|---------|--------|----------------------------------|
| Stored XSS via SVG rendered as `<img>` | Tampering / Elevation | SVG excluded → download-only (D-07); gated **first** in `previewKind`. |
| Stored XSS via untrusted HTML executing in our origin | Elevation | `<iframe srcDoc sandbox="allow-scripts">` **without** `allow-same-origin` = null origin; cannot read cookies/DOM/session. Cannot `src`→download URL (attachment). |
| XSS via spreadsheet cell content in `sheet_to_html` | Tampering | SheetJS escapes cell values (A1) **and** render inside empty-`sandbox` iframe (no script exec) — defense-in-depth. |
| Prototype pollution / ReDoS via crafted xlsx (CVE-2023-30533 / CVE-2024-22363) | DoS / Tampering | Install SheetJS **≥0.20.2 from CDN** (both fixed); never the frozen npm 0.18.5. |
| IDOR — reading another identity's asset | Info Disclosure | `GetForIdentity` 404 (shipped); panel uses only `id`, never `object_key`. |
| Host/container path leaking to browser | Info Disclosure | Panel/chip use `asset_id` only; `sseAdapter` never copies `path` into the payload (37A D-13, verified). |
| Multi-download abuse / tab hang | DoS (self) | Throttled sequential loop; button disabled during run; abortable. |
| docx CSS bleed into the app | Tampering (visual) | `className: 'aura-docx'` prefixes all injected rules; scoped by class. |

---

## Sources

### Primary (HIGH — installed source / running code / registry)
- `node_modules/react-resizable-panels/dist/react-resizable-panels.{d.ts,js}` (v4.12.0) — `Group/Panel/Separator`, `useDefaultLayout`, storage-key `he()` (js:1786-1868), `panelIds` conditional-render contract (d.ts:433-452). **The authoritative API for the pinned version.**
- `web/AppShell.tsx`, `components/ui/{resizable,dialog}.tsx`, `shell/{Drawer,useSurfaceRestore}.ts`, `chat/{ExternalStoreChat,sseAdapter,sseAdapter_snapshot}.ts`, `chat/attachments/{api,types}.ts`, `chat/displays/{LocalArtifactDisplay,CitationBubble,types}.tsx` — patterns cited at file:line throughout.
- `internal/agui/assets_api.go`, `internal/assets/{store,types}.go`, `internal/db/queries/assets.sql` — list endpoint identity-scoping, `Asset` JSON shape, **`ORDER BY created_at ASC`**.
- `web/{vitest,vitest.stryker,playwright}.config.ts`, `web/e2e/{replay,assets,auth}.ts`, `web/src/test/setup.ts` — test infra + golden-replay pattern.
- npm registry (2026-07-08): `docx-preview` 0.4.0 / Apache-2.0 / jszip dep; `xlsx` 0.18.5 (frozen). slopcheck 0.6.1 → 3 OK.

### Secondary (MEDIUM — official docs via WebFetch/WebSearch, cross-checked)
- docx-preview `renderAsync` signature + options + Apache-2.0 (`github.com/VolodymyrBaydalka/docxjs`).
- SheetJS distribution off npm + current 0.20.3 CDN install (`docs.sheetjs.com`, `git.sheetjs.com` issues #2667/#2961/#3098).
- CVE-2023-30533 (fixed 0.19.3) + CVE-2024-22363 (fixed 0.20.2) (`nvd.nist.gov`, `security.snyk.io`, Snyk SNYK-JS-XLSX-5457926).

### Tertiary (LOW — flagged, see Assumptions)
- A1 `sheet_to_html` cell-value escaping in 0.20.3 (SheetJS HTML utilities doc returned 403; mitigated by sandbox, verify on install).

---

## Metadata

**Confidence breakdown:**
- Resizable dynamic-membership (Pattern 1): **HIGH** — read the compiled implementation + type defs directly; no layout-key bump needed, mechanism verified.
- Standard stack + versions: **HIGH** — registry-verified; SheetJS CDN caveat cross-checked across 4 sources.
- Data layer / ordering / source_kind / D-15 root cause: **HIGH** — verified against SQL, Go structs, and the snapshot rehydration code.
- Preview renderers (docx/xlsx/html/blob lifecycle): **HIGH** for API/pattern; **MEDIUM** for `sheet_to_html` escaping (A1) and CSP interaction (A3) — both de-risked by the sandbox.
- Validation architecture: **HIGH** — configs read directly; commands runnable as written.

**Research date:** 2026-07-08
**Valid until:** ~2026-08-07 (30 days) — except SheetJS/xlsx CVE + CDN-version facts (re-check the current CDN version at plan time; 0.20.3 is current as of research) and `react-resizable-panels` (pinned at 4.12.0 — stable until an intentional bump).
