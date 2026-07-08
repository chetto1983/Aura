# Phase 37B: Web Artifact Sidebar - Pattern Map

**Mapped:** 2026-07-08
**Files analyzed:** 8 NEW component groups + 5 MODIFIED files
**Analogs found:** 13 / 13 (every new/modified file has a same-repo analog — this is a pure parity/mirror phase)

> All excerpts below are copied from the **current** files (line numbers verified at HEAD, 2026-07-08). Where they drifted from CONTEXT.md the delta is noted inline — none material; the CONTEXT.md refs are accurate.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `chat/artifacts/useThreadArtifacts.ts` (NEW) | hook | request-response (query) | `chat/ExternalStoreChat.tsx` useQueryClient + `attachments/api.ts:47` `listThreadAssets` | role+flow exact |
| `chat/artifacts/artifactMeta.ts` (NEW) | utility (pure) | transform | `chat/displays/CitationBubble.tsx:27` `ICON_FOR_KIND` map + `LocalArtifactDisplay.tsx:27` `formatSize` | role+flow exact |
| `chat/artifacts/ArtifactsPanel.tsx` (NEW) | component (container) | CRUD/list-render | `AppShell.tsx` `navigation` panel body + `LocalArtifactDisplay.tsx` degraded branch | role match |
| `chat/artifacts/ArtifactRow.tsx` (NEW) | component (row) | request-response (download) | `chat/displays/LocalArtifactDisplay.tsx:65-89` (download anchor) + `CitationBubble.tsx` icon | exact |
| `chat/artifacts/downloadAll.ts` (NEW) | utility (pure-ish) | batch | `LocalArtifactDisplay.tsx:66` anchor href convention (loop form) | role+flow exact |
| `chat/artifacts/useBlobPreview.ts` (NEW) | hook | file-I/O (fetch→blob) | `AppShell.tsx:63` `loadLogoutTarget` fetch + AbortController pattern in `ExternalStoreChat.tsx:287` | flow match |
| `chat/artifacts/PreviewModal.tsx` (NEW) | component (modal) | event-driven | `components/ui/dialog.tsx` (Radix Dialog) + `a11y/focusTrap.ts` | exact (Radix owns trap) |
| `chat/artifacts/renderers/*.tsx` (NEW ×6) | component (lazy chunk) | file-I/O / transform | `AppShell.tsx:29-47` `lazy(()=>import())` + `Suspense` | exact |
| `web/src/AppShell.tsx` (MODIFY) | component (shell) | request-response | self — mirror existing 2-panel group at `:441`, `onUsage` at `:396`, `autoOpenedOnboarding` at `:150` | self-mirror |
| `chat/ExternalStoreChat.tsx` (MODIFY) | component (runtime) | streaming + CRUD | self — mirror `attachAssetsToUserMessages` `:62`, `onUsage` prop `:134`, `invalidateRuntimeReads` `:162`, history fold `:303` | self-mirror |
| `chat/sseAdapter.ts` (MODIFY) | service (reducer) | streaming | self — `aura.artifact` branch `:345`, `streamSSE` frame loop `:512-514` | self-mirror |
| `chat/attachments/types.ts` (MODIFY) | model (type) | — | self — `Asset.source_kind` union `:19` (widen to add `'agent'`) | self-mirror |
| `chat/attachments/types.ts` D-15 fold helper (NEW fn near ExternalStoreChat) | utility (pure) | transform | `ExternalStoreChat.tsx:62` `attachAssetsToUserMessages` (positional zip) | exact |

---

## Pattern Assignments

### `chat/artifacts/useThreadArtifacts.ts` (NEW — hook, query)

**Analog:** `web/src/chat/attachments/api.ts:47` (query fn, reuse verbatim) + `ExternalStoreChat.tsx:3,153` (`useQueryClient`).

**Reuse the ready-made fetcher (`api.ts:47-57`) — no new client:**
```typescript
export async function listThreadAssets(threadId: string, signal?: AbortSignal): Promise<Asset[]> {
  const init: RequestInit = {
    method: 'GET',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  };
  if (signal !== undefined) init.signal = signal;
  const res = await fetch(`/api/assets?thread_id=${encodeURIComponent(threadId)}`, init);
  const value = await readJSON<unknown>(res);
  return Array.isArray(value) ? (value as Asset[]) : [];
}
```

**Query shape** — `useQuery({ queryKey: ['assets', threadId], queryFn: ({signal}) => listThreadAssets(threadId, signal) })`. Server returns `created_at ASC` (RESEARCH Anti-Pattern) → sort **DESC client-side in `select`**, filter `source_kind === 'agent'` and drop `deleted`/`canceled`. **Requires the `types.ts:19` union widen first** (see below), else `=== 'agent'` type-errors.

---

### `chat/artifacts/artifactMeta.ts` (NEW — pure utility, mutation target)

**Analog:** `chat/displays/CitationBubble.tsx:27-37` (icon-per-category via `Partial<Record<…,LucideIcon>>` + `File` fallback).

**Icon-map precedent to mirror (`CitationBubble.tsx:2,27-37`):**
```typescript
import { Code2, File, FileText, Globe, type LucideIcon } from 'lucide-react';

const ICON_FOR_KIND: Partial<Record<DisplayKind, LucideIcon>> = {
  web_result: Globe,
  document: FileText,
  code: Code2,
};

function KindIcon({ kind }: { readonly kind?: DisplayKind }) {
  const Icon = (kind !== undefined ? ICON_FOR_KIND[kind] : undefined) ?? File;
  return <Icon data-icon aria-hidden="true" className="shrink-0 text-text-faint" />;
}
```
For D-16, extend the same shape to a mime/ext→category map (add `FileSpreadsheet`/`FileImage`/`FileCode` from `lucide-react`). `previewKind(mime, filename)` (RESEARCH Code Examples) also lives here — pure, 100%-testable, SVG-download gated first.

**Size formatter to reuse (`LocalArtifactDisplay.tsx:22-32`):**
```typescript
const KB = 1024; const MB = KB * 1024; const GB = MB * 1024;
function formatSize(bytes: number, t: TFunction): string {
  if (bytes < KB) return t('display.artifact.sizeBytes', { count: bytes });
  if (bytes < MB) return t('display.artifact.sizeKb', { value: (bytes / KB).toFixed(1) });
  if (bytes < MB * 1024) return t('display.artifact.sizeMb', { value: (bytes / MB).toFixed(1) });
  return t('display.artifact.sizeGb', { value: (bytes / GB).toFixed(1) });
}
```
Prefer extracting this to `artifactMeta.ts` and importing it from `LocalArtifactDisplay.tsx` (DRY / refactor-on-touch) rather than duplicating.

---

### `chat/artifacts/ArtifactRow.tsx` (NEW — component, download)

**Analog:** `chat/displays/LocalArtifactDisplay.tsx:65-113` — the exact same-origin download anchor AND the degraded branch to mirror (D-18).

**Download anchor to copy verbatim (`LocalArtifactDisplay.tsx:65-89`) — the 37A-proven auth path (D-12):**
```tsx
{assetId ? (
  <a
    href={`/api/assets/${assetId}/download`}
    download={filename}
    aria-label={t('display.artifact.downloadAria', { filename })}
    className="group inline-flex w-fit items-center gap-2 rounded-[var(--radius-sm)] border border-accent/40 bg-surface-2 px-3 py-1.5 text-sm font-medium text-accent-text transition-colors hover:border-accent hover:bg-surface focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
  >
    …download glyph…
    {t('display.artifact.download')}
  </a>
) : (
  <span role="note" className="… text-warning">
    …warning glyph…
    {t('display.artifact.deliveryUnavailable')}
  </span>
)}
```
The `asset_id`-present → anchor / `asset_id`-absent → degraded `role="note"` split is D-18's degraded-row pattern. **Never render `object_key`/`object_bucket`** (RESEARCH Anti-Pattern — the `Asset` JSON carries them; use only `id`). Row body click → `PreviewModal`; the trailing anchor downloads directly (D-05).

---

### `chat/artifacts/downloadAll.ts` (NEW — pure-ish utility, mutation target)

**Analog:** same `LocalArtifactDisplay.tsx:66` href convention, driven as a throttled loop. Full recipe in RESEARCH Pattern 7 (skip `status !== 'accepted'`, ~500ms delay, `document.createElement('a')` + `.click()`, `N/M` progress, abortable). Pure logic → test with fake timers + spy on `HTMLAnchorElement.prototype.click`.

---

### `chat/artifacts/useBlobPreview.ts` (NEW — hook, file-I/O)

**Analog:** the `fetch(..., { credentials: 'same-origin' })` convention (everywhere, e.g. `api.ts:52`, `AppShell.tsx:67`) + the `AbortController` cleanup pattern in `ExternalStoreChat.tsx:287,319-321`:
```typescript
const controller = new AbortController();
historyAbortRef.current = controller;
// …
return () => { controller.abort(); };
```
Full blob relabel + `revokeObjectURL`-on-unmount recipe in RESEARCH Pattern 5. The `Blob` is re-labelled with the SSE-carried `mime_type` (server serves octet-stream). `AbortController` + `URL.revokeObjectURL` in the **same** cleanup keyed on `assetId` (Pitfall 5).

---

### `chat/artifacts/PreviewModal.tsx` (NEW — component, modal)

**Analog:** `components/ui/dialog.tsx` (Radix wrapper — owns focus-trap/Esc/backdrop/aria). Do NOT hand-roll with `a11y/focusTrap.ts` (that is the portal Drawer's trap only).

**Key facts from `dialog.tsx`:**
- Content default is `max-w-lg` (`:51`) — override to 90vw/90vh via a passed `className` (the wrapper merges via `cn()`), e.g. `<DialogContent className="h-[90vh] w-[90vw] max-w-[90vw] gap-0 p-0">`.
- Overlay `z-[90]` (`:26`), content `z-[100]` (`:51`) — both above the Drawer's `z-50`, so a preview opened from inside the mobile Drawer layers correctly.
- Export surface: `Dialog`, `DialogContent` (accepts `showCloseButton`), `DialogHeader`, `DialogTitle`, `DialogDescription`, `DialogClose`. Include a `DialogDescription`/`aria-describedby` to silence Radix's a11y warning.
- Modal header = filename + the same download anchor (`LocalArtifactDisplay.tsx:66`) + built-in close.

---

### `chat/artifacts/renderers/*.tsx` (NEW ×6 — lazy chunks)

**Analog:** `AppShell.tsx:29-47` — the established `lazy(() => import())` + `Suspense` split (`GraphExplorer`/`GovernanceWorkspace` keep heavy deps out of the main bundle):
```tsx
const GraphExplorer = lazy(() => import('./graph/GraphExplorer'));
const GovernanceWorkspace = lazy(() => import('./governance/GovernanceWorkspace'));
// …consumed under <Suspense fallback={…}>
```
Each renderer (`ImagePreview`/`PdfPreview`/`TextPreview`/`HtmlPreview`/`DocxPreview`/`XlsxPreview`) is one `React.lazy` import so `docx-preview`+`jszip` and SheetJS land only in their chunk (D-08). Renderer bodies (sandbox attrs, `renderAsync`, `sheet_to_html`) are in RESEARCH Code Examples. Coverage: keep DOM-injection glue thin, mock `docx-preview`/`xlsx` in tests (Pitfall 4).

---

### `web/src/AppShell.tsx` (MODIFY — self-mirror)

**Analog:** its own existing 2-panel group. Mirror the nav panel + resizer for the 3rd.

**`CHAT_SHELL_PANEL_IDS` (`:49-50`) — drive dynamically (RESEARCH Pattern 1, NO key bump):**
```tsx
const CHAT_SHELL_LAYOUT_ID = 'aura-chat-shell-v3';
const CHAT_SHELL_PANEL_IDS = ['chat-navigation', 'chat-workspace'];
```

**`useDefaultLayout` (`:286-290`) — pass a dynamic `panelIds`:**
```tsx
const chatShellLayout = useDefaultLayout({
  id: CHAT_SHELL_LAYOUT_ID,
  panelIds: CHAT_SHELL_PANEL_IDS,
  onlySaveAfterUserInteractions: true,
});
```

**The panel + resizer to mirror (`:441-476`) — the artifacts panel mounts AFTER `chat-workspace`, its `ResizableHandle` BEFORE it:**
```tsx
<ResizablePanelGroup {...chatShellLayout} id={CHAT_SHELL_LAYOUT_ID} orientation="horizontal"
  resizeTargetMinimumSize={{ coarse: 32, fine: 12 }} className="shell-main-resizable min-w-0">
  <ResizablePanel id="chat-navigation" defaultSize="15rem" minSize="13rem" maxSize="28rem"
    groupResizeBehavior="preserve-pixel-size" className="h-full min-h-0">…</ResizablePanel>
  <ResizableHandle id="chat-navigation-resizer" aria-label={t('shell.resizeNavigation')}
    className="shell-nav-resize-handle" withHandle />
  <ResizablePanel id="chat-workspace" minSize={CHAT_WORKSPACE_MIN_WIDTH} className="h-full min-h-0">
    {workspace}
  </ResizablePanel>
</ResizablePanelGroup>
```
New artifacts panel tokens (D-02): `id="chat-artifacts" defaultSize="19rem" minSize="16rem" maxSize="32rem" groupResizeBehavior="preserve-pixel-size"`. Only render inside the existing `showConversationNavigation` branch (`:439`).

**`onUsage` wiring to mirror for `onArtifact` (`:392-398` — the `ExternalStoreChat` mount):**
```tsx
<ExternalStoreChat
  threadId={activeThreadId}
  onEnsureThread={ensureThread}
  onUsage={setUsage}
  resumeNonce={resumeNonce}
  draftPrompt={documentDraftPrompt}
/>
```
Add `onArtifact={handleArtifact}` alongside `onUsage`. `handleArtifact` invalidates `['assets', activeThreadId]` (D-11) and drives one-time auto-open.

**One-time-guard precedent to mirror for auto-open (`:150,160-166`):**
```tsx
const autoOpenedOnboarding = useRef(false);
useEffect(() => {
  if (searchParams.get('onboarding') !== '1' || autoOpenedOnboarding.current) return;
  autoOpenedOnboarding.current = true;
  …
}, [closeNav, navigate, searchParams]);
```
Use a `useRef<Set<string>>`/threadId-keyed guard, reset on thread change (Pitfall 6). Note `:155,187` already reset `usage` on thread change — do the same for the auto-open ref.

**Mobile Drawer mount to mirror (`:488-490`) — add a `side="right"` twin:**
```tsx
<Drawer open={surfaces.navOpen} side="left" title="Aura" onClose={surfaces.closeNav}>
  {mobileNavigation}
</Drawer>
```
Add `<Drawer open={surfaces.overlayOpen} side="right" title={t('artifacts.title')} onClose={(i) => surfaces.closeOverlay(i ?? 'explicit')}>`, routed through the currently-unused overlay slot (see Shared Patterns).

---

### `chat/ExternalStoreChat.tsx` (MODIFY — self-mirror)

**`onUsage` prop shape to mirror for `onArtifact` (`:133-134`):**
```typescript
/** 25-04 seam: receives the latest per-turn usage off the SSE STATE_DELTA. */
readonly onUsage?: (usage: TurnUsage | undefined) => void;
```
Add `readonly onArtifact?: (assetId: string | undefined) => void;`. It fires in the `onUpdate` frame loop (`:219,369,413`) alongside `onUsage?.(usage)` when an `aura.artifact` descriptor with an `asset_id` is seen — or, cleaner, threaded through `streamRun`/`streamSSE` (see `sseAdapter.ts` below).

**`invalidateRuntimeReads` (`:162-169`) — the invalidate-after-run precedent (extend for `['assets', id]`):**
```typescript
const invalidateRuntimeReads = useCallback(
  (id = threadId) => {
    if (id.length === 0) return;
    void queryClient.invalidateQueries({ queryKey: [CONVERSATION_KEY, id] });
    void queryClient.invalidateQueries({ queryKey: [CONVERSATION_ROT_EVENTS_KEY, id] });
  },
  [queryClient, threadId],
);
```

**`attachAssetsToUserMessages` (`:62-85`) — the fold to SPLIT by `source_kind` for D-15 (positional-zip heuristic to mirror onto ASSISTANT turns):**
```typescript
function attachAssetsToUserMessages(
  messages: readonly ThreadMessageLike[],
  assets: readonly Asset[],
): ThreadMessageLike[] {
  const visibleAssets = assets.filter(
    (asset) => asset.status !== 'deleted' && asset.status !== 'canceled',
  );
  if (visibleAssets.length === 0) return [...messages];
  const userIndexes = messages
    .map((message, index) => (message.role === 'user' ? index : -1))
    .filter((index) => index >= 0);
  // …positional zip: groups.set(userIndexes[min(assetIndex, len-1)], asset)…
}
```

**The history-load call site to change (`:295-303`) — split the list before folding (RESEARCH Pattern 3):**
```typescript
let assets: Asset[] = [];
try { assets = await listThreadAssets(threadId, controller.signal); }
catch (err) { if (err instanceof DOMException && err.name === 'AbortError') return; }
if (isAbortSignalAborted(controller.signal) || request !== historyRequestRef.current) return;
setMessages(attachAssetsToUserMessages(loaded, assets));   // ← CURRENTLY folds ALL onto USER turns (the bug)
```
Change to: `const uploads = assets.filter(a => a.source_kind !== 'agent'); const agent = assets.filter(a => a.source_kind === 'agent'); setMessages(foldAgentOntoAssistant(attachAssetsToUserMessages(loaded, uploads), agent));`

**Attachment metadata envelope to reuse for `foldAgentOntoAssistant` (`:49-60`):**
```typescript
function messageAttachments(message: ThreadMessageLike): readonly Asset[] {
  const metadata = message.metadata?.custom as { attachments?: readonly Asset[] } | undefined;
  return metadata?.attachments ?? [];
}
function withMessageAttachments(message, attachments): ThreadMessageLike {
  const custom = { ...(message.metadata?.custom ?? {}), attachments };
  return { ...message, metadata: { ...message.metadata, custom } };
}
```
`foldAgentOntoAssistant` targets `role === 'assistant'` and writes the same `metadata.custom.attachments` envelope; a small assistant-side renderer reads `messageAttachments` and renders the `LocalArtifactDisplay.tsx:66` anchor.

---

### `chat/sseAdapter.ts` (MODIFY — self-mirror)

**The `aura.artifact` reducer branch (`:345-363`) — where `onArtifact` surfaces the descriptor:**
```typescript
if (frame.name === 'aura.artifact' && isArtifactDescriptor(frame.value)) {
  const d = frame.value;
  const part = ensureTool(state, d.tool_call_id, state.tools.get(d.tool_call_id)?.toolName ?? '');
  const display: DisplayPayload = {
    type: 'local_artifact',
    tool_call_id: d.tool_call_id,
    artifact: {
      filename: d.filename,
      ...(d.size_bytes !== undefined ? { size_bytes: d.size_bytes } : {}),
      ...(d.asset_id !== undefined ? { asset_id: d.asset_id } : {}),
      ...(d.mime_type !== undefined ? { mime_type: d.mime_type } : {}),
    },
  };
  writeTool(state, { ...part, display });
}
```
`ArtifactDescriptor` (`:223-229`) carries `tool_call_id`/`filename`/`size_bytes?`/`asset_id?`/`mime_type?`. **`reduceFrame` is a pure reducer — do NOT emit a callback here.** Thread the signal at the pump instead.

**The pump call-site to thread `onArtifact` through (`streamSSE`, `:497-517`):**
```typescript
for await (const frame of readSSEFrames(res.body)) {
  reduceFrame(state, frame);
  opts.onUpdate(toThreadMessage(state), state.usage);
}
```
Add an optional `onArtifact?: (assetId: string | undefined) => void` to `StreamSSEOptions`/`StreamRunOptions`/`StreamPostOptions` (mirror how `onUpdate`/`newId` are threaded), and fire it when `frame.type === 'CUSTOM' && frame.name === 'aura.artifact' && isArtifactDescriptor(frame.value)` — passing `frame.value.asset_id`. `ExternalStoreChat` forwards its `onArtifact` prop into `streamRun`/`streamPost`.

**Root cause of the rehydration bug (`sseAdapter_snapshot.ts:96-120`) — for the planner's context:** `toolCallsFromSnapshot` only re-attaches a `display` when the persisted snapshot carries one (`:114-116` `...(isDisplayPayload(call.display) ? { display: call.display } : {})`). send_file's `local_artifact` display is synthesized **client-side** from the live frame and never persisted, so the tool card rehydrates without `asset_id`. D-14 (the query-keyed panel) is the durable fix; D-15's fold is best-effort chip parity.

---

### `chat/attachments/types.ts` (MODIFY — widen the union)

**Analog:** self. The `Asset.source_kind` union (`:19`) is **missing `'agent'`** (RESEARCH Pitfall 3 — 37A widened the backend via migration 0035 but not the TS type):
```typescript
export interface Asset {
  readonly id: string;
  readonly source_kind?: 'web' | 'telegram' | 'cli';   // ← ADD 'agent'
  readonly thread_id?: string;
  …
  readonly created_at?: string;
}
```
Change to `'web' | 'telegram' | 'cli' | 'agent'`. Without this, `source_kind === 'agent'` in `useThreadArtifacts` and the D-15 split type-errors.

---

## Shared Patterns

### Auth on fetch (applies to every new fetch: `useBlobPreview`, `downloadAll` anchors)
**Source:** `chat/attachments/api.ts:47-57`, `AppShell.tsx:63-68`
**Apply to:** all preview/download fetches — always `credentials: 'same-origin'` so the session cookie rides the request. The `<a href download>` inherits it automatically; `useBlobPreview`'s explicit `fetch` must set it.

### Mobile overlay routing (applies to the right Drawer)
**Source:** `shell/useSurfaceRestore.ts:75-81` (the currently-**unused** overlay slot) + `shell/Drawer.tsx:25` (`side='right'` already supported)
```typescript
// useSurfaceRestore exposes: overlayOpen, openOverlay(), closeOverlay(intent)
export interface SurfaceRestore extends SurfaceState {
  readonly openNav: () => void;
  readonly closeNav: () => void;
  readonly openOverlay: () => void;
  readonly closeOverlay: (intent: CloseIntent) => void;
}
```
```tsx
// Drawer.tsx:25 — side='right' branches at :67-69: right-0 border-l
export function Drawer({ open, title, side, onClose, children }: DrawerProps) { … }
```
**Apply to:** the mobile Artefatti drawer — route it through `openOverlay`/`closeOverlay`/`overlayOpen` so it obeys "one heavy overlay at a time" against the nav drawer. Reuse `Drawer side="right"` verbatim (portal + backdrop + focus-trap + Esc + scroll-lock already inside).

### Dialog primitive (applies to PreviewModal)
**Source:** `components/ui/dialog.tsx` — Radix owns focus-trap/Esc/backdrop/aria. Override `max-w-lg` (`:51`) via a passed `className` merged by `cn()`. Content `z-[100]` > overlay `z-[90]` > Drawer `z-50`.

### Icon-per-category + size format (applies to ArtifactRow / artifactMeta)
**Source:** `CitationBubble.tsx:27-37` (`Partial<Record<…,LucideIcon>>` + `File` fallback) and `LocalArtifactDisplay.tsx:22-32` (`formatSize` with i18n size keys). Extract `formatSize` to the shared `artifactMeta.ts` and import from both sites (DRY).

### Lazy chunk split (applies to all 6 renderers)
**Source:** `AppShell.tsx:29-47` — `lazy(() => import('./…'))` consumed under `<Suspense fallback={…}>`. Keeps heavy deps (`docx-preview`+`jszip`, SheetJS) out of the main bundle (D-08).

---

## No Analog Found

None. Every new/modified file mirrors an existing same-repo pattern — 37B is an aggregation/parity layer over 37A, not new infrastructure. The two NEW web deps (`docx-preview`, SheetJS CE from CDN) are lazy-loaded library calls inside `renderers/`, with signatures documented in RESEARCH Standard Stack (do not re-explore).

---

## Discrepancies vs CONTEXT.md assumptions

| CONTEXT.md ref | Actual (verified) | Impact |
|----------------|-------------------|--------|
| line numbers `:441`, `:49-50`, `:286`, `:463`, `:488`, `:62`, `:134`, `:162`, `:345`, `:68` | **all accurate at HEAD** | none — refs are current |
| `ExternalStoreChat.tsx:134` onUsage / `:62` fold | confirmed exact | none |
| history-load fold path "`:303`" | the `setMessages(attachAssetsToUserMessages(...))` call is at **`:303`** | none |
| `Asset.source_kind` union | confirmed `'web'\|'telegram'\|'cli'` at `:19` — **missing `'agent'`** (RESEARCH Pitfall 3) | must widen before the `=== 'agent'` filter compiles |
| CONTEXT.md D-07: "docx-preview (MIT)" | RESEARCH A4: docx-preview is **Apache-2.0**, not MIT | record correct license in the D-19 PRD-amendment |
| CONTEXT.md gotcha: "explicit id/order … layout-key bump v3→v4" | RESEARCH Pattern 1: v4 has **no `order` prop**; **no key bump** needed — drive `panelIds` dynamically | simplifies the AppShell change |

---

## Metadata

**Analog search scope:** `web/src/{AppShell.tsx, chat/**, shell/**, components/ui/**, a11y/**}`
**Files scanned (read in full or targeted):** 11 — `AppShell.tsx`, `attachments/api.ts`, `attachments/types.ts`, `ExternalStoreChat.tsx`, `displays/LocalArtifactDisplay.tsx`, `displays/CitationBubble.tsx`, `sseAdapter.ts`, `sseAdapter_snapshot.ts`, `shell/Drawer.tsx`, `components/ui/dialog.tsx`, `shell/useSurfaceRestore.ts`, `components/ui/resizable.tsx`
**Pattern extraction date:** 2026-07-08
