# Aura Document Library Product UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. The user explicitly requested no worktrees and no parallel work, so execute inline in `D:\Aura`, one task at a time.

**Goal:** Replace the current inspector-style document workspace with a product-grade Aura file-manager library that supports scanning, search/filter chips, document actions, details, library upload, and E2E validation.

**Architecture:** Keep existing document catalog APIs and split the current large React workspace into focused view-model, list, toolbar, drawer, upload, and admin components. Add the smallest backend contract needed for true library uploads: an optional asset scope on presign that defaults to current thread behavior. Preserve existing tag, retry, delete, events, and orphan-cleanup contracts.

**Tech Stack:** Go 1.26, existing AG-UI HTTP handlers, `internal/assets`, React 19, TypeScript, Vite, Vitest, Testing Library, Playwright, Tailwind/Aura tokens, lucide-react icons.

---

## Execution Rules

- Work in `D:\Aura`; do not create a git worktree.
- Do not use parallel agents or parallel tool execution.
- Do not use `--no-verify`.
- Before each commit, run `git status --short` and stage only files for the active task.
- Existing dirty files outside the files listed in the active task are user work and must not be reverted.
- Use TDD where behavior changes: write or update the failing test, run it, implement, rerun.
- Keep the document workspace product-first: no hero section, no decorative page background, no nested cards, no storage-orphan panel in the first viewport.
- Final validation must include backend and frontend gates: `go vet ./...`, relevant `go test`, `npm.cmd run lint`, `npm.cmd run typecheck`, Vitest, Playwright, Docker Aura rebuild, and live user-style document flow.

## Current Dirty Worktree To Avoid

At plan creation time, unrelated changes existed in:

- `.planning/graphs/*`
- `internal/webui/dist/*`
- `web/src/components/ui/skeleton.tsx`
- `web/src/conversations/*`
- `web/src/governance/*`

Do not stage or revert those files unless the active task explicitly adds them to its file list.

## File Map

Backend library upload scope:

- Modify: `internal/assets/types.go` - ensure asset scope is part of the public asset model.
- Modify: `internal/assets/service.go` - accept optional `Scope` in `PresignRequest`, defaulting to thread.
- Modify: `internal/assets/service_test.go` - prove thread default and library scope.
- Modify: `internal/agui/assets_api.go` - accept optional `scope` in `/api/assets/presign`.
- Modify: `internal/agui/assets_api_test.go` - prove request body scope reaches the asset service.
- Modify: `web/src/chat/attachments/types.ts` - add optional `scope` to `PresignAssetRequest`.
- Existing reused: `web/src/chat/attachments/api.ts`.

Document product view model:

- Create: `web/src/documents/documentViewModel.ts` - active version lookup, type/kind inference, tab filtering, date/size labels, status labels, tag parsing.
- Create: `web/src/documents/__tests__/documentViewModel.test.ts` - pure unit tests for view-model helpers.
- Existing reused: `web/src/documents/documentFormat.ts`.

React document workspace:

- Replace: `web/src/documents/DocumentsWorkspace.tsx` - orchestration and layout only.
- Create: `web/src/documents/DocumentLibraryHeader.tsx` - title, search, new/upload menu.
- Create: `web/src/documents/DocumentFilterBar.tsx` - tabs and filter chips.
- Create: `web/src/documents/DocumentFileList.tsx` - dense list/table rows, selection, sort, empty/loading/error states.
- Create: `web/src/documents/DocumentActionMenu.tsx` - accessible row action popover.
- Create: `web/src/documents/DocumentDetailsDrawer.tsx` - detail drawer, tags, versions, events, advanced technical fields.
- Create: `web/src/documents/DocumentUploadDialog.tsx` - library upload flow using asset presign/finalize/get.
- Create: `web/src/documents/documentUpload.ts` - upload helper with XHR progress.
- Modify: `web/src/documents/StorageOrphansPanel.tsx` - keep as advanced/admin section, not first viewport.
- Modify: `web/src/documents/__tests__/DocumentsWorkspace.test.tsx` - product behavior tests.
- Create: `web/src/documents/__tests__/documentUpload.test.ts` - upload helper tests.
- Modify: `web/src/i18n/resources.documents.ts` - product labels for header, tabs, actions, drawer, upload, admin.

Browser E2E:

- Create: `web/e2e/documents-library.spec.ts` - authenticated library smoke and live upload/chat/delete path where environment permits.
- Existing reused: `web/e2e/auth.ts`.

---

## Task 1: Backend Contract For Library-Scoped Uploads

**Files:**
- Modify: `internal/assets/service.go`
- Modify: `internal/assets/service_test.go`
- Modify: `internal/agui/assets_api.go`
- Modify: `internal/agui/assets_api_test.go`
- Modify: `web/src/chat/attachments/types.ts`

- [ ] **Step 1: Write failing asset service test for library scope**

Append this test to `internal/assets/service_test.go` near existing presign tests:

```go
func TestServicePresignAcceptsLibraryScope(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	_, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		Scope:             ScopeLibrary,
		FileName:          "manual.pdf",
		MIMEType:          "application/pdf",
		DeclaredSizeBytes: 128,
		ModalityHint:      ModalityDocument,
	})
	if err != nil {
		t.Fatalf("Presign returned error: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created %d assets, want 1", len(store.created))
	}
	if store.created[0].Scope != ScopeLibrary {
		t.Fatalf("Create scope = %q, want %q", store.created[0].Scope, ScopeLibrary)
	}
}

func TestServicePresignDefaultsToThreadScope(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    100,
		MaxAudioBytes:    100,
	})

	_, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          "note.txt",
		MIMEType:          "text/plain",
		DeclaredSizeBytes: 32,
		ModalityHint:      ModalityDocument,
	})
	if err != nil {
		t.Fatalf("Presign returned error: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created %d assets, want 1", len(store.created))
	}
	if store.created[0].Scope != ScopeThread {
		t.Fatalf("Create scope = %q, want %q", store.created[0].Scope, ScopeThread)
	}
}
```

- [ ] **Step 2: Run service tests and verify failure**

Run:

```powershell
go test -count=1 ./internal/assets
```

Expected: FAIL because `PresignRequest` has no `Scope` field or because `Presign` still stores `ScopeThread`.

- [ ] **Step 3: Implement optional scope in the service**

In `internal/assets/service.go`, extend `PresignRequest`:

```go
type PresignRequest struct {
	IdentityID        string     `json:"identity_id"`
	SourceKind        SourceKind `json:"source_kind"`
	ThreadID          string     `json:"thread_id"`
	Scope             Scope      `json:"scope"`
	FileName          string     `json:"file_name"`
	MIMEType          string     `json:"mime_type"`
	DeclaredSizeBytes int64      `json:"size_bytes"`
	ModalityHint      Modality   `json:"modality_hint"`
}
```

Inside `Presign`, before `s.Store.Create`:

```go
scope := req.Scope
if scope == "" {
	scope = ScopeThread
}
if scope != ScopeThread && scope != ScopeLibrary {
	return PresignResponse{}, fmt.Errorf("unsupported asset scope %q", scope)
}
```

Change the create request field:

```go
Scope: scope,
```

- [ ] **Step 4: Write failing AG-UI API test for scope**

Append this test to `internal/agui/assets_api_test.go`:

```go
func TestAssetPresignAcceptsLibraryScope(t *testing.T) {
	assetSvc := &fakeAssetService{}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(assetSvc)

	body := strings.NewReader(`{"file_name":"manual.pdf","mime_type":"application/pdf","size_bytes":128,"modality_hint":"document","scope":"library"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/assets/presign", body)
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, assetAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if assetSvc.presignReq.Scope != assets.ScopeLibrary {
		t.Fatalf("scope = %q, want %q", assetSvc.presignReq.Scope, assets.ScopeLibrary)
	}
}
```

- [ ] **Step 5: Implement API scope forwarding**

In `internal/agui/assets_api.go`, extend `assetPresignBody`:

```go
Scope string `json:"scope"`
```

Forward scope in `handleAssetPresign`:

```go
Scope: assets.Scope(body.Scope),
```

- [ ] **Step 6: Add frontend request type field**

In `web/src/chat/attachments/types.ts`, extend `PresignAssetRequest`:

```ts
export interface PresignAssetRequest {
  readonly thread_id: string;
  readonly scope?: 'thread' | 'library';
  readonly file_name: string;
  readonly mime_type: string;
  readonly size_bytes: number;
  readonly modality_hint: AssetModality;
}
```

- [ ] **Step 7: Run backend and attachment API tests**

Run:

```powershell
go test -count=1 ./internal/assets ./internal/agui
```

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/chat/attachments/__tests__/api.test.ts
```

Expected: PASS.

- [ ] **Step 8: Commit backend upload-scope contract**

```powershell
git status --short
git add internal/assets/service.go internal/assets/service_test.go internal/agui/assets_api.go internal/agui/assets_api_test.go web/src/chat/attachments/types.ts
git commit -m "feat: support library-scoped asset uploads"
```

---

## Task 2: Document View-Model Helpers

**Files:**
- Create: `web/src/documents/documentViewModel.ts`
- Create: `web/src/documents/__tests__/documentViewModel.test.ts`

- [ ] **Step 1: Write failing view-model tests**

Create `web/src/documents/__tests__/documentViewModel.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import type { DocumentDetail, DocumentItem } from '../documentApi';
import {
  activeVersionFor,
  documentKindFor,
  documentMatchesTab,
  formatDocumentDate,
  parseDocumentTags,
  statusToneFor,
  type DocumentTab,
} from '../documentViewModel';

const doc: DocumentItem = {
  id: 'doc-1',
  identity_id: 'identity-1',
  scope: 'library',
  title: 'Manual.PDF',
  tags: ['robot'],
  metadata: {},
  active_version_id: 'ver-2',
  status: 'ready',
  updated_at: '2026-06-30T08:10:00Z',
};

const detail: DocumentDetail = {
  document: doc,
  versions: [
    {
      id: 'ver-1',
      document_id: 'doc-1',
      version_number: 1,
      status: 'ready',
      sha256: 'old',
      content_type: 'text/plain',
      size_bytes: 10,
      storage_object_id: 'store-1',
    },
    {
      id: 'ver-2',
      document_id: 'doc-1',
      version_number: 2,
      status: 'ready',
      sha256: 'new',
      content_type: 'application/pdf',
      size_bytes: 20,
      storage_object_id: 'store-2',
    },
  ],
};

describe('documentViewModel', () => {
  it('finds the active version and document kind', () => {
    expect(activeVersionFor(detail)?.id).toBe('ver-2');
    expect(documentKindFor(doc, activeVersionFor(detail))).toBe('document');
  });

  it.each([
    ['all', true],
    ['documents', true],
    ['images', false],
    ['failed', false],
    ['processing', false],
  ] satisfies [DocumentTab, boolean][])('matches %s tab', (tab, expected) => {
    expect(documentMatchesTab(doc, activeVersionFor(detail), tab)).toBe(expected);
  });

  it('maps status tones and tag drafts', () => {
    expect(statusToneFor('ready')).toBe('success');
    expect(statusToneFor('failed')).toBe('danger');
    expect(statusToneFor('processing')).toBe('warning');
    expect(parseDocumentTags('robot, manual, robot')).toEqual(['robot', 'manual']);
  });

  it('formats dates compactly', () => {
    expect(formatDocumentDate('2026-06-30T08:10:00Z')).toMatch(/2026|Jun|30|giu|06/);
    expect(formatDocumentDate(undefined)).toBe('-');
  });
});
```

- [ ] **Step 2: Run test and verify failure**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/documentViewModel.test.ts
```

Expected: FAIL because `documentViewModel.ts` does not exist.

- [ ] **Step 3: Implement view-model helpers**

Create `web/src/documents/documentViewModel.ts`:

```ts
import type {
  DocumentDetail,
  DocumentItem,
  DocumentStatus,
  DocumentVersion,
} from './documentApi';

export type DocumentTab = 'all' | 'documents' | 'images' | 'files' | 'failed' | 'processing';
export type DocumentKind = 'document' | 'image' | 'file';
export type StatusTone = 'success' | 'danger' | 'warning' | 'secondary';

const imageTypes = new Set(['image/png', 'image/jpeg', 'image/webp', 'image/gif', 'image/svg+xml']);
const documentTypes = new Set([
  'application/pdf',
  'application/msword',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'application/vnd.ms-excel',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  'application/vnd.ms-powerpoint',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  'text/plain',
  'text/markdown',
  'text/csv',
  'text/html',
]);

export function activeVersionFor(detail: DocumentDetail | undefined): DocumentVersion | undefined {
  if (detail === undefined) return undefined;
  return (
    detail.versions.find((version) => version.id === detail.document.active_version_id) ??
    detail.versions[0]
  );
}

export function documentKindFor(
  document: DocumentItem,
  version: DocumentVersion | undefined,
): DocumentKind {
  const mime = version?.content_type.toLowerCase() ?? '';
  if (imageTypes.has(mime) || /\.(png|jpe?g|webp|gif|svg)$/i.test(document.title)) return 'image';
  if (documentTypes.has(mime) || /\.(pdf|docx?|xlsx?|pptx?|txt|md|csv|html)$/i.test(document.title)) {
    return 'document';
  }
  return 'file';
}

export function documentMatchesTab(
  document: DocumentItem,
  version: DocumentVersion | undefined,
  tab: DocumentTab,
): boolean {
  if (tab === 'all') return true;
  if (tab === 'failed') return document.status === 'failed';
  if (tab === 'processing') return document.status === 'queued' || document.status === 'processing';
  const kind = documentKindFor(document, version);
  if (tab === 'documents') return kind === 'document';
  if (tab === 'images') return kind === 'image';
  return kind === 'file';
}

export function statusToneFor(status: DocumentStatus): StatusTone {
  if (status === 'ready') return 'success';
  if (status === 'failed' || status === 'deleted') return 'danger';
  if (status === 'queued' || status === 'processing' || status === 'deleting') return 'warning';
  return 'secondary';
}

export function parseDocumentTags(value: string): string[] {
  const seen = new Set<string>();
  const tags: string[] = [];
  for (const raw of value.split(',')) {
    const tag = raw.trim();
    if (tag.length === 0 || seen.has(tag)) continue;
    seen.add(tag);
    tags.push(tag);
  }
  return tags;
}

export function formatDocumentDate(value: string | undefined): string {
  if (value === undefined || value.trim() === '') return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
  }).format(date);
}
```

- [ ] **Step 4: Run test and verify pass**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/documentViewModel.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit view-model helpers**

```powershell
git status --short
git add web/src/documents/documentViewModel.ts web/src/documents/__tests__/documentViewModel.test.ts
git commit -m "feat: add document library view model"
```

---

## Task 3: Product Library Shell, Tabs, Search, And Dense List

**Files:**
- Replace: `web/src/documents/DocumentsWorkspace.tsx`
- Create: `web/src/documents/DocumentLibraryHeader.tsx`
- Create: `web/src/documents/DocumentFilterBar.tsx`
- Create: `web/src/documents/DocumentFileList.tsx`
- Modify: `web/src/documents/__tests__/DocumentsWorkspace.test.tsx`
- Modify: `web/src/i18n/resources.documents.ts`

- [ ] **Step 1: Rewrite first workspace test around product layout**

In `web/src/documents/__tests__/DocumentsWorkspace.test.tsx`, replace the first test expectations with product-level expectations:

```ts
expect(await screen.findByRole('heading', { name: 'Document library' })).toBeTruthy();
expect(screen.getByRole('searchbox', { name: 'Search documents' })).toBeTruthy();
expect(screen.getByRole('tab', { name: 'All' })).toBeTruthy();
expect(screen.getByRole('tab', { name: 'Documents' })).toBeTruthy();
expect(screen.getByRole('tab', { name: 'Images' })).toBeTruthy();
expect(await screen.findByRole('row', { name: /Handbook\.pdf.*ready.*2 KB/i })).toBeTruthy();
```

Keep the existing fetch mock data and search assertion, but change the search interaction to:

```ts
fireEvent.change(screen.getByRole('searchbox', { name: 'Search documents' }), {
  target: { value: 'handbook' },
});
fireEvent.change(screen.getByLabelText('Tag filter'), { target: { value: 'ops' } });
fireEvent.click(screen.getByRole('button', { name: 'Search' }));
```

- [ ] **Step 2: Run test and verify failure**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/DocumentsWorkspace.test.tsx
```

Expected: FAIL because the current layout has sidebar cards instead of a product list/table.

- [ ] **Step 3: Add translations for product shell**

In `web/src/i18n/resources.documents.ts`, add these keys under both `documentsEn.documents` and `documentsIt.documents`:

Under `actions`, add:

```ts
upload: 'Upload',
```

Italian:

```ts
upload: 'Carica',
```

```ts
tabs: {
  all: 'All',
  documents: 'Documents',
  images: 'Images',
  files: 'Files',
  failed: 'Failed',
  processing: 'Processing',
},
view: {
  list: 'List view',
  grid: 'Grid view',
  filters: 'Filters',
  modified: 'Modified',
  size: 'Size',
  status: 'Status',
  name: 'Name',
},
```

Italian values:

```ts
tabs: {
  all: 'Tutti',
  documents: 'Documenti',
  images: 'Immagini',
  files: 'File',
  failed: 'Falliti',
  processing: 'In elaborazione',
},
view: {
  list: 'Vista elenco',
  grid: 'Vista griglia',
  filters: 'Filtri',
  modified: 'Modificato',
  size: 'Dimensioni',
  status: 'Stato',
  name: 'Nome',
},
```

- [ ] **Step 4: Create `DocumentLibraryHeader.tsx`**

Create:

```tsx
import { RefreshCw, Search, Upload } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

interface DocumentLibraryHeaderProps {
  readonly query: string;
  readonly refreshing: boolean;
  readonly onQueryChange: (value: string) => void;
  readonly onSearch: () => void;
  readonly onRefresh: () => void;
  readonly onUpload: () => void;
}

export function DocumentLibraryHeader({
  query,
  refreshing,
  onQueryChange,
  onSearch,
  onRefresh,
  onUpload,
}: DocumentLibraryHeaderProps) {
  const { t } = useTranslation();
  return (
    <header className="shrink-0 border-b border-border bg-bg px-4 py-5 sm:px-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <h1 className="min-w-0 truncate font-display text-[28px] font-semibold text-text">
            {t('documents.title')}
          </h1>
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" disabled={refreshing} onClick={onRefresh}>
              <RefreshCw aria-hidden="true" />
              {t('documents.actions.refresh')}
            </Button>
            <Button type="button" onClick={onUpload}>
              <Upload aria-hidden="true" />
              {t('documents.actions.upload')}
            </Button>
          </div>
        </div>
        <form
          role="search"
          className="flex min-w-0 flex-col gap-2 sm:max-w-xl sm:flex-row"
          onSubmit={(event) => {
            event.preventDefault();
            onSearch();
          }}
        >
          <div className="relative min-w-0 flex-1">
            <Search
              className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-text-faint"
              aria-hidden="true"
            />
            <Input
              type="search"
              role="searchbox"
              aria-label={t('documents.filters.search')}
              value={query}
              onChange={(event) => {
                onQueryChange(event.target.value);
              }}
              className="pl-9"
            />
          </div>
          <Button type="submit">
            <Search aria-hidden="true" />
            {t('documents.actions.search')}
          </Button>
        </form>
      </div>
    </header>
  );
}
```

- [ ] **Step 5: Create `DocumentFilterBar.tsx`**

Create:

```tsx
import { Filter, Grid2X2, List } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import type { DocumentScope } from './documentApi';
import type { DocumentTab } from './documentViewModel';

type ScopeFilter = DocumentScope | 'all';
type ViewMode = 'list' | 'grid';

interface DocumentFilterBarProps {
  readonly tab: DocumentTab;
  readonly tag: string;
  readonly scope: ScopeFilter;
  readonly viewMode: ViewMode;
  readonly onTabChange: (value: DocumentTab) => void;
  readonly onTagChange: (value: string) => void;
  readonly onScopeChange: (value: ScopeFilter) => void;
  readonly onViewModeChange: (value: ViewMode) => void;
}

export function DocumentFilterBar({
  tab,
  tag,
  scope,
  viewMode,
  onTabChange,
  onTagChange,
  onScopeChange,
  onViewModeChange,
}: DocumentFilterBarProps) {
  const { t } = useTranslation();
  return (
    <div className="border-b border-border bg-bg px-4 py-3 sm:px-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-3">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <Tabs value={tab} onValueChange={(value) => onTabChange(value as DocumentTab)}>
            <TabsList className="overflow-x-auto">
              {(['all', 'documents', 'images', 'files', 'failed', 'processing'] as const).map(
                (item) => (
                  <TabsTrigger key={item} value={item}>
                    {t(`documents.tabs.${item}`)}
                  </TabsTrigger>
                ),
              )}
            </TabsList>
          </Tabs>
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant={viewMode === 'list' ? 'default' : 'outline'}
              aria-label={t('documents.view.list')}
              onClick={() => onViewModeChange('list')}
            >
              <List aria-hidden="true" />
            </Button>
            <Button
              type="button"
              variant={viewMode === 'grid' ? 'default' : 'outline'}
              aria-label={t('documents.view.grid')}
              onClick={() => onViewModeChange('grid')}
            >
              <Grid2X2 aria-hidden="true" />
            </Button>
          </div>
        </div>
        <div className="grid gap-3 sm:grid-cols-[minmax(0,16rem)_minmax(0,12rem)]">
          <div className="grid gap-1.5">
            <Label htmlFor="documents-tag">{t('documents.filters.tag')}</Label>
            <Input
              id="documents-tag"
              value={tag}
              onChange={(event) => onTagChange(event.target.value)}
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="documents-scope">{t('documents.filters.scope')}</Label>
            <NativeSelect
              id="documents-scope"
              value={scope}
              onChange={(event) => onScopeChange(event.target.value as ScopeFilter)}
            >
              <NativeSelectOption value="all">{t('documents.scope.all')}</NativeSelectOption>
              <NativeSelectOption value="library">{t('documents.scope.library')}</NativeSelectOption>
              <NativeSelectOption value="thread">{t('documents.scope.thread')}</NativeSelectOption>
            </NativeSelect>
          </div>
          <div className="hidden items-end sm:flex">
            <span className="inline-flex items-center gap-2 text-[13px] text-text-muted">
              <Filter className="size-4" aria-hidden="true" />
              {t('documents.view.filters')}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 6: Create `DocumentFileList.tsx` with dense accessible table**

Create:

```tsx
import { FileText, Image, MoreHorizontal, PackageOpen } from 'lucide-react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { cn } from '@/lib/utils';
import type { DocumentItem, DocumentVersion } from './documentApi';
import { formatBytes } from './documentFormat';
import {
  documentKindFor,
  documentMatchesTab,
  formatDocumentDate,
  statusToneFor,
  type DocumentTab,
} from './documentViewModel';

interface DocumentFileListProps {
  readonly documents: readonly DocumentItem[];
  readonly activeVersions: ReadonlyMap<string, DocumentVersion | undefined>;
  readonly tab: DocumentTab;
  readonly selectedIds: ReadonlySet<string>;
  readonly activeId: string;
  readonly loading: boolean;
  readonly error: boolean;
  readonly onToggleSelected: (id: string) => void;
  readonly onOpenDetails: (id: string) => void;
  readonly onOpenActions: (id: string) => void;
  readonly onRetry: () => void;
}

export function DocumentFileList({
  documents,
  activeVersions,
  tab,
  selectedIds,
  activeId,
  loading,
  error,
  onToggleSelected,
  onOpenDetails,
  onOpenActions,
  onRetry,
}: DocumentFileListProps) {
  const { t } = useTranslation();
  const visible = useMemo(
    () => documents.filter((document) => documentMatchesTab(document, activeVersions.get(document.id), tab)),
    [activeVersions, documents, tab],
  );

  if (loading) return <DocumentLoadingRows />;
  if (error) {
    return (
      <div role="alert" className="mx-auto grid min-h-64 w-full max-w-6xl place-items-center px-4 text-center">
        <div className="grid gap-3">
          <p className="text-sm text-danger">{t('documents.error.list')}</p>
          <Button type="button" variant="outline" onClick={onRetry}>
            {t('documents.actions.refresh')}
          </Button>
        </div>
      </div>
    );
  }
  if (visible.length === 0) {
    return (
      <div className="mx-auto grid min-h-64 w-full max-w-6xl place-items-center px-4 text-center">
        <div className="grid justify-items-center gap-3">
          <PackageOpen className="size-8 text-text-faint" aria-hidden="true" />
          <p className="text-sm font-semibold text-text">{t('documents.empty')}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-0 flex-1 overflow-auto px-4 py-3 sm:px-6">
      <table className="mx-auto w-full max-w-6xl table-fixed border-separate border-spacing-y-1">
        <caption className="sr-only">{t('documents.title')}</caption>
        <thead className="text-left text-[12px] font-semibold text-text-muted">
          <tr>
            <th className="w-10 px-2 py-2" scope="col">
              <span className="sr-only">Select</span>
            </th>
            <th className="px-2 py-2" scope="col">{t('documents.view.name')}</th>
            <th className="hidden w-36 px-2 py-2 md:table-cell" scope="col">{t('documents.view.status')}</th>
            <th className="hidden w-36 px-2 py-2 lg:table-cell" scope="col">{t('documents.view.modified')}</th>
            <th className="hidden w-28 px-2 py-2 sm:table-cell" scope="col">{t('documents.view.size')}</th>
            <th className="w-12 px-2 py-2" scope="col">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {visible.map((document) => {
            const version = activeVersions.get(document.id);
            const kind = documentKindFor(document, version);
            const selected = selectedIds.has(document.id);
            return (
              <tr
                key={document.id}
                aria-selected={selected}
                className={cn(
                  'group h-14 rounded-md bg-bg text-[14px] text-text transition-colors hover:bg-surface',
                  activeId === document.id ? 'bg-surface-2' : '',
                )}
              >
                <td className="rounded-l-md px-2 py-2">
                  <Checkbox
                    aria-label={`Select ${document.title}`}
                    checked={selected}
                    onCheckedChange={() => onToggleSelected(document.id)}
                  />
                </td>
                <td className="min-w-0 px-2 py-2">
                  <button
                    type="button"
                    className="flex min-w-0 items-center gap-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    onClick={() => onOpenDetails(document.id)}
                  >
                    <span className="grid size-9 shrink-0 place-items-center rounded-md border border-border bg-surface text-accent-text">
                      {kind === 'image' ? <Image className="size-4" aria-hidden="true" /> : <FileText className="size-4" aria-hidden="true" />}
                    </span>
                    <span className="min-w-0">
                      <span className="block truncate font-semibold">{document.title}</span>
                      <span className="mt-0.5 flex flex-wrap gap-1">
                        {document.tags.slice(0, 2).map((tag) => (
                          <Badge key={tag} variant="secondary">{tag}</Badge>
                        ))}
                      </span>
                    </span>
                  </button>
                </td>
                <td className="hidden px-2 py-2 md:table-cell">
                  <Badge variant={statusToneFor(document.status)}>{document.status}</Badge>
                </td>
                <td className="hidden px-2 py-2 text-text-muted lg:table-cell">
                  {formatDocumentDate(document.updated_at ?? document.created_at)}
                </td>
                <td className="hidden px-2 py-2 text-text-muted sm:table-cell">
                  {version === undefined ? '-' : formatBytes(version.size_bytes)}
                </td>
                <td className="rounded-r-md px-2 py-2 text-right">
                  <Button
                    type="button"
                    variant="ghost"
                    aria-label={`Actions for ${document.title}`}
                    onClick={() => onOpenActions(document.id)}
                  >
                    <MoreHorizontal aria-hidden="true" />
                  </Button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function DocumentLoadingRows() {
  return (
    <div role="status" className="mx-auto grid w-full max-w-6xl gap-2 px-4 py-4 sm:px-6">
      {Array.from({ length: 6 }, (_, index) => (
        <div key={index} className="h-14 rounded-md border border-border bg-surface/70" />
      ))}
    </div>
  );
}
```

- [ ] **Step 7: Replace `DocumentsWorkspace.tsx` with orchestration shell**

Replace the file with a shell that owns query/tag/scope/tab/view state, calls existing APIs, computes `activeVersions` from loaded detail for the selected document, and renders `DocumentLibraryHeader`, `DocumentFilterBar`, and `DocumentFileList`. Keep a simple details status for Task 4:

```tsx
// The file should import the three new components and current API functions.
// Keep `listLimit = 50`, `defaultFilters`, and `ScopeFilter`.
// Keep `loadDocuments`, `loadDetail`, `submitSearch`, and selected ID state.
// Render no sidebar. The content area is `flex h-full min-h-0 flex-col bg-bg`.
```

The final file for this step must still support:

```tsx
<DocumentLibraryHeader ... />
<DocumentFilterBar ... />
<DocumentFileList ... />
```

Keep upload dialog state ready for Task 6:

```tsx
const [uploadOpen, setUploadOpen] = useState(false);
```

Then pass:

```tsx
onUpload={() => setUploadOpen(true)}
```

No upload dialog is rendered in Task 3.

- [ ] **Step 8: Run tests and fix type issues**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/DocumentsWorkspace.test.tsx src/documents/__tests__/documentViewModel.test.ts
.\node_modules\.bin\tsc.cmd --noEmit
```

Expected: PASS.

- [ ] **Step 9: Commit product shell and dense list**

```powershell
git status --short
git add web/src/documents/DocumentsWorkspace.tsx web/src/documents/DocumentLibraryHeader.tsx web/src/documents/DocumentFilterBar.tsx web/src/documents/DocumentFileList.tsx web/src/documents/__tests__/DocumentsWorkspace.test.tsx web/src/i18n/resources.documents.ts
git commit -m "feat: redesign document library shell"
```

---

## Task 4: Row Action Menu And Details Drawer

**Files:**
- Create: `web/src/documents/DocumentActionMenu.tsx`
- Create: `web/src/documents/DocumentDetailsDrawer.tsx`
- Modify: `web/src/documents/DocumentsWorkspace.tsx`
- Modify: `web/src/documents/__tests__/DocumentsWorkspace.test.tsx`
- Modify: `web/src/i18n/resources.documents.ts`

- [ ] **Step 1: Update tests for action menu and drawer**

Add assertions to `DocumentsWorkspace.test.tsx`:

```ts
fireEvent.click(await screen.findByRole('button', { name: /Handbook\.pdf/ }));
const drawer = await screen.findByRole('dialog', { name: 'Handbook.pdf' });
expect(within(drawer).getByText('Versions')).toBeTruthy();
expect(within(drawer).getByText('sha256-current')).toBeTruthy();
expect(await within(drawer).findByText('ingestion_job.succeeded')).toBeTruthy();

fireEvent.click(screen.getByRole('button', { name: 'Actions for Handbook.pdf' }));
const menu = await screen.findByRole('menu', { name: 'Actions for Handbook.pdf' });
expect(within(menu).getByRole('menuitem', { name: 'Edit tags' })).toBeTruthy();
expect(within(menu).getByRole('menuitem', { name: 'Delete document' })).toBeTruthy();
```

- [ ] **Step 2: Run test and verify failure**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/DocumentsWorkspace.test.tsx
```

Expected: FAIL because drawer/menu are not implemented.

- [ ] **Step 3: Add translations for drawer/menu**

Add under `documents.actions`:

```ts
editTags: 'Edit tags',
askDocument: 'Ask this document',
close: 'Close',
```

Italian:

```ts
editTags: 'Modifica tag',
askDocument: 'Chiedi a questo documento',
close: 'Chiudi',
```

Add under `documents.detail`:

```ts
overview: 'Overview',
processing: 'Processing',
technical: 'Technical',
advanced: 'Advanced',
```

Italian:

```ts
overview: 'Panoramica',
processing: 'Elaborazione',
technical: 'Tecnico',
advanced: 'Avanzate',
```

- [ ] **Step 4: Create `DocumentActionMenu.tsx`**

Create a simple anchored menu rendered near the row action trigger:

```tsx
import { MessageSquareText, Pencil, RefreshCw, Trash2, X } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import type { DocumentItem } from './documentApi';

interface DocumentActionMenuProps {
  readonly document: DocumentItem | undefined;
  readonly open: boolean;
  readonly onClose: () => void;
  readonly onAsk: () => void;
  readonly onEditTags: () => void;
  readonly onRetry: () => void;
  readonly onDelete: () => void;
}

export function DocumentActionMenu({
  document,
  open,
  onClose,
  onAsk,
  onEditTags,
  onRetry,
  onDelete,
}: DocumentActionMenuProps) {
  const { t } = useTranslation();
  if (!open || document === undefined) return null;
  const menuLabel = `Actions for ${document.title}`;
  return (
    <div className="absolute right-6 top-32 z-20 w-56 rounded-md border border-border bg-surface p-1 shadow-popover">
      <div className="flex items-center justify-between px-2 py-1 text-[12px] font-semibold text-text-muted">
        <span className="truncate">{document.title}</span>
        <Button type="button" variant="ghost" aria-label={t('documents.actions.close')} onClick={onClose}>
          <X aria-hidden="true" />
        </Button>
      </div>
      <div role="menu" aria-label={menuLabel} className="grid gap-1">
        <MenuButton icon={<MessageSquareText aria-hidden="true" />} label={t('documents.actions.askDocument')} onClick={onAsk} />
        <MenuButton icon={<Pencil aria-hidden="true" />} label={t('documents.actions.editTags')} onClick={onEditTags} />
        {document.status === 'failed' ? (
          <MenuButton icon={<RefreshCw aria-hidden="true" />} label={t('documents.actions.retryProcessing')} onClick={onRetry} />
        ) : null}
        <MenuButton danger icon={<Trash2 aria-hidden="true" />} label={t('documents.actions.delete')} onClick={onDelete} />
      </div>
    </div>
  );
}

function MenuButton({
  icon,
  label,
  danger = false,
  onClick,
}: {
  readonly icon: ReactNode;
  readonly label: string;
  readonly danger?: boolean;
  readonly onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="menuitem"
      className={`flex min-h-10 items-center gap-2 rounded-sm px-3 text-left text-[14px] outline-none hover:bg-surface-2 focus-visible:ring-2 focus-visible:ring-ring ${
        danger ? 'text-danger' : 'text-text'
      }`}
      onClick={onClick}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}
```

- [ ] **Step 5: Create `DocumentDetailsDrawer.tsx`**

Create a right-side fixed dialog:

```tsx
import { Save, Trash2, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import type { DocumentDetail, DocumentItem, DocumentVersion } from './documentApi';
import { DocumentEventsPanel } from './DocumentEventsPanel';
import { formatBytes } from './documentFormat';
import { formatDocumentDate, statusToneFor } from './documentViewModel';

interface DocumentDetailsDrawerProps {
  readonly open: boolean;
  readonly document: DocumentItem | undefined;
  readonly detail: DocumentDetail | undefined;
  readonly activeVersion: DocumentVersion | undefined;
  readonly tagDraft: string;
  readonly savingTags: boolean;
  readonly onTagDraftChange: (value: string) => void;
  readonly onSaveTags: () => void;
  readonly onDelete: () => void;
  readonly onClose: () => void;
}

export function DocumentDetailsDrawer({
  open,
  document,
  detail,
  activeVersion,
  tagDraft,
  savingTags,
  onTagDraftChange,
  onSaveTags,
  onDelete,
  onClose,
}: DocumentDetailsDrawerProps) {
  const { t } = useTranslation();
  if (!open || document === undefined) return null;
  return (
    <aside
      role="dialog"
      aria-modal="false"
      aria-label={document.title}
      className="fixed inset-y-0 right-0 z-30 flex w-full max-w-md flex-col border-l border-border bg-bg shadow-drawer"
    >
      <header className="flex items-start justify-between gap-3 border-b border-border p-4">
        <div className="min-w-0">
          <h2 className="truncate text-[18px] font-semibold text-text">{document.title}</h2>
          <p className="mt-1 text-[13px] text-text-muted">
            {formatDocumentDate(document.updated_at ?? document.created_at)}
          </p>
        </div>
        <Button type="button" variant="ghost" aria-label={t('documents.actions.close')} onClick={onClose}>
          <X aria-hidden="true" />
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">
        <section className="grid gap-3 border-b border-border pb-4">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-[14px] font-semibold text-text">{t('documents.detail.overview')}</h3>
            <Badge variant={statusToneFor(document.status)}>{document.status}</Badge>
          </div>
          <dl className="grid gap-2 text-[13px] text-text-muted">
            <MetaItem label={t('documents.view.size')} value={activeVersion === undefined ? '-' : formatBytes(activeVersion.size_bytes)} />
            <MetaItem label="MIME" value={activeVersion?.content_type ?? '-'} />
            <MetaItem label="Scope" value={document.scope} />
          </dl>
          <div className="grid gap-1.5">
            <Label htmlFor="documents-tags">{t('documents.detail.tags')}</Label>
            <Input id="documents-tags" value={tagDraft} onChange={(event) => onTagDraftChange(event.target.value)} />
          </div>
          <div className="flex flex-wrap gap-2">
            <Button type="button" disabled={savingTags} onClick={onSaveTags}>
              {savingTags ? <Spinner /> : <Save aria-hidden="true" />}
              {t('documents.actions.saveTags')}
            </Button>
            <Button type="button" variant="destructive" onClick={onDelete}>
              <Trash2 aria-hidden="true" />
              {t('documents.actions.delete')}
            </Button>
          </div>
        </section>
        <section className="grid gap-3 border-b border-border py-4">
          <h3 className="text-[14px] font-semibold text-text">{t('documents.detail.versions')}</h3>
          {detail?.versions.map((version) => (
            <div key={version.id} className="rounded-md border border-border bg-surface px-3 py-2 text-[13px] text-text">
              v{version.version_number} · {version.status} · {formatBytes(version.size_bytes)}
            </div>
          ))}
        </section>
        <section className="grid gap-3 py-4">
          <h3 className="text-[14px] font-semibold text-text">{t('documents.detail.processing')}</h3>
          <DocumentEventsPanel key={document.id} documentId={document.id} />
        </section>
        <details className="border-t border-border pt-4">
          <summary className="cursor-pointer text-[14px] font-semibold text-text">
            {t('documents.detail.advanced')}
          </summary>
          <dl className="mt-3 grid gap-2 text-[12px] text-text-muted">
            <MetaItem label="Document ID" value={document.id} />
            <MetaItem label="Version ID" value={activeVersion?.id ?? '-'} />
            <MetaItem label="SHA-256" value={activeVersion?.sha256 ?? '-'} />
            <MetaItem label="Storage" value={activeVersion?.storage_object_id ?? '-'} />
          </dl>
        </details>
      </div>
    </aside>
  );
}

function MetaItem({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] font-semibold uppercase text-text-faint">{label}</dt>
      <dd className="break-all text-text">{value}</dd>
    </div>
  );
}
```

- [ ] **Step 6: Wire menu and drawer in `DocumentsWorkspace.tsx`**

Add state:

```tsx
const [actionMenuId, setActionMenuId] = useState('');
const [deleteTarget, setDeleteTarget] = useState<DocumentItem | undefined>(undefined);
const [drawerOpen, setDrawerOpen] = useState(false);
```

When a row opens details:

```tsx
async function openDetails(id: string) {
  setSelectedId(id);
  setDrawerOpen(true);
  await loadDetail(id);
}
```

Pass `onOpenDetails={openDetails}` and `onOpenActions={setActionMenuId}` to `DocumentFileList`.

Render:

```tsx
<DocumentActionMenu
  document={documents.find((item) => item.id === actionMenuId)}
  open={actionMenuId !== ''}
  onClose={() => setActionMenuId('')}
  onAsk={() => setActionMenuId('')}
  onEditTags={() => {
    const id = actionMenuId;
    setDrawerOpen(true);
    setActionMenuId('');
    setSelectedId(id);
    void loadDetail(id);
  }}
  onRetry={() => {
    const id = actionMenuId;
    setActionMenuId('');
    void retryDocument(id);
  }}
  onDelete={() => {
    const target = documents.find((item) => item.id === actionMenuId);
    setDeleteTarget(target);
    if (target !== undefined) setSelectedId(target.id);
    setActionMenuId('');
    setDeleteOpen(true);
  }}
/>
<DocumentDetailsDrawer
  open={drawerOpen}
  document={detail?.document ?? documents.find((item) => item.id === selectedId)}
  detail={detail}
  activeVersion={activeVersion}
  tagDraft={tagDraft}
  savingTags={savingTags}
  onTagDraftChange={setTagDraft}
  onSaveTags={() => void saveTags()}
  onDelete={() => setDeleteOpen(true)}
  onClose={() => setDrawerOpen(false)}
/>
```

Add a row-targeted retry helper:

```tsx
async function retryDocument(id: string) {
  if (retrying || id === '') return;
  setRetrying(true);
  try {
    const next = await fetchDocumentDetail(id);
    const version =
      next.versions.find((item) => item.id === next.document.active_version_id) ?? next.versions[0];
    if (version?.asset_id === undefined || version.asset_id === '') return;
    await retryDocumentAsset(version.asset_id);
    await loadDocuments({ query, tag, scope }, id);
  } finally {
    setRetrying(false);
  }
}
```

Update the delete dialog target so row actions do not depend on stale drawer state:

```tsx
<DeleteDialog
  open={deleteOpen}
  document={deleteTarget ?? detail?.document}
  confirm={deleteConfirm}
  deleting={deleting}
  onOpenChange={(open) => {
    setDeleteOpen(open);
    if (!open) setDeleteTarget(undefined);
  }}
  onConfirmChange={setDeleteConfirm}
  onDelete={() => {
    void confirmDelete(deleteTarget ?? detail?.document);
  }}
/>
```

Change `confirmDelete` to accept the target document:

```tsx
async function confirmDelete(document: DocumentItem | undefined) {
  if (document === undefined || deleteConfirm !== 'DELETE' || deleting) return;
  setDeleting(true);
  try {
    await deleteDocument(document.id);
    setDeleteOpen(false);
    setDeleteTarget(undefined);
    setDeleteConfirm('');
    await loadDocuments({ query, tag, scope });
  } finally {
    setDeleting(false);
  }
}
```

- [ ] **Step 7: Run tests and typecheck**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/DocumentsWorkspace.test.tsx
.\node_modules\.bin\tsc.cmd --noEmit
```

Expected: PASS.

- [ ] **Step 8: Commit menu and drawer**

```powershell
git status --short
git add web/src/documents/DocumentsWorkspace.tsx web/src/documents/DocumentActionMenu.tsx web/src/documents/DocumentDetailsDrawer.tsx web/src/documents/__tests__/DocumentsWorkspace.test.tsx web/src/i18n/resources.documents.ts
git commit -m "feat: add document actions drawer"
```

---

## Task 5: Advanced Admin Panel For Storage Orphans

**Files:**
- Modify: `web/src/documents/DocumentsWorkspace.tsx`
- Modify: `web/src/documents/StorageOrphansPanel.tsx`
- Modify: `web/src/documents/__tests__/DocumentsWorkspace.test.tsx`
- Modify: `web/src/i18n/resources.documents.ts`

- [ ] **Step 1: Update orphan cleanup test to open advanced admin**

In the orphan test, before interacting with bucket input:

```ts
expect(screen.queryByLabelText('Storage bucket')).toBeNull();
fireEvent.click(await screen.findByRole('button', { name: 'Advanced document maintenance' }));
fireEvent.change(await screen.findByLabelText('Storage bucket'), {
  target: { value: 'assets' },
});
```

- [ ] **Step 2: Run test and verify failure**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/DocumentsWorkspace.test.tsx
```

Expected: FAIL because the storage panel is still visible in the main page.

- [ ] **Step 3: Add admin translations**

Add:

```ts
admin: {
  maintenance: 'Advanced document maintenance',
},
```

Italian:

```ts
admin: {
  maintenance: 'Manutenzione documenti avanzata',
},
```

- [ ] **Step 4: Move `StorageOrphansPanel` behind disclosure**

In `DocumentsWorkspace.tsx`, render below the file list:

```tsx
<section className="border-t border-border px-4 py-3 sm:px-6">
  <details className="mx-auto w-full max-w-6xl">
    <summary className="cursor-pointer text-[13px] font-semibold text-text-muted">
      {t('documents.admin.maintenance')}
    </summary>
    <div className="mt-3">
      <StorageOrphansPanel />
    </div>
  </details>
</section>
```

In `StorageOrphansPanel.tsx`, remove any outer styling that makes it look like a primary page section. Keep form fields, dry run, confirmation, and deletion behavior unchanged.

- [ ] **Step 5: Run tests**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/DocumentsWorkspace.test.tsx
```

Expected: PASS.

- [ ] **Step 6: Commit admin relocation**

```powershell
git status --short
git add web/src/documents/DocumentsWorkspace.tsx web/src/documents/StorageOrphansPanel.tsx web/src/documents/__tests__/DocumentsWorkspace.test.tsx web/src/i18n/resources.documents.ts
git commit -m "feat: tuck document maintenance into advanced panel"
```

---

## Task 6: Library Upload Dialog

**Files:**
- Create: `web/src/documents/documentUpload.ts`
- Create: `web/src/documents/__tests__/documentUpload.test.ts`
- Create: `web/src/documents/DocumentUploadDialog.tsx`
- Modify: `web/src/documents/DocumentsWorkspace.tsx`
- Modify: `web/src/documents/__tests__/DocumentsWorkspace.test.tsx`
- Modify: `web/src/i18n/resources.documents.ts`

- [ ] **Step 1: Write upload helper tests**

Create `web/src/documents/__tests__/documentUpload.test.ts`:

```ts
import { describe, expect, it, vi } from 'vitest';
import { uploadLibraryDocument } from '../documentUpload';

class FakeXHR {
  static instances: FakeXHR[] = [];
  readonly upload = { onprogress: null as ((event: ProgressEvent) => void) | null };
  status = 200;
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  method = '';
  url = '';
  body: BodyInit | null = null;

  constructor() {
    FakeXHR.instances.push(this);
  }

  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }

  setRequestHeader() {}

  send(body: BodyInit | null) {
    this.body = body;
    this.upload.onprogress?.({ lengthComputable: true, loaded: 5, total: 10 } as ProgressEvent);
    this.onload?.();
  }
}

describe('uploadLibraryDocument', () => {
  it('presigns as library scope, uploads, finalizes, polls, and reports progress', async () => {
    const progress: number[] = [];
    vi.stubGlobal('XMLHttpRequest', FakeXHR);
    vi.stubGlobal(
      'fetch',
      vi.fn()
        .mockResolvedValueOnce(
          new Response(
            JSON.stringify({
              asset: { id: 'asset-1', status: 'presigned', modality: 'document', file_name: 'Manual.pdf', mime_type: 'application/pdf', declared_size_bytes: 10, size_bytes: 0 },
              upload: { upload_url: 'https://assets.test/upload', method: 'PUT', required_headers: {}, expires_at: '2026-06-30T00:00:00Z' },
            }),
          ),
        )
        .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'asset-1', status: 'processing', modality: 'document', file_name: 'Manual.pdf', mime_type: 'application/pdf', declared_size_bytes: 10, size_bytes: 10 })))
        .mockResolvedValueOnce(new Response(JSON.stringify({ id: 'asset-1', status: 'searchable', modality: 'document', file_name: 'Manual.pdf', mime_type: 'application/pdf', declared_size_bytes: 10, size_bytes: 10, document_id: 'doc-1' }))),
    );

    const file = new File(['0123456789'], 'Manual.pdf', { type: 'application/pdf' });
    const asset = await uploadLibraryDocument(file, (value) => progress.push(value));

    expect(asset.document_id).toBe('doc-1');
    expect(progress).toContain(0.5);
    const fetchMock = vi.mocked(fetch);
    expect(JSON.parse(fetchMock.mock.calls[0]?.[1]?.body as string)).toMatchObject({
      scope: 'library',
      thread_id: '',
      file_name: 'Manual.pdf',
      modality_hint: 'document',
    });
    expect(FakeXHR.instances[0]?.method).toBe('PUT');
  });
});
```

- [ ] **Step 2: Run upload helper test and verify failure**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/documentUpload.test.ts
```

Expected: FAIL because `documentUpload.ts` does not exist.

- [ ] **Step 3: Implement `documentUpload.ts`**

Create:

```ts
import { finalizeAsset, getAsset, presignAsset } from '../chat/attachments/api';
import type { Asset } from '../chat/attachments/types';

type ProgressHandler = (progress: number) => void;

export async function uploadLibraryDocument(
  file: File,
  onProgress: ProgressHandler = () => undefined,
): Promise<Asset> {
  const presigned = await presignAsset({
    thread_id: '',
    scope: 'library',
    file_name: file.name,
    mime_type: file.type || 'application/octet-stream',
    size_bytes: file.size,
    modality_hint: 'document',
  });
  await uploadToPresignedURL(
    presigned.upload.upload_url,
    presigned.upload.method,
    presigned.upload.required_headers,
    file,
    onProgress,
  );
  const finalized = await finalizeAsset(presigned.asset.id);
  return pollUntilDocumentReady(finalized);
}

async function pollUntilDocumentReady(asset: Asset): Promise<Asset> {
  if (asset.status === 'searchable' || asset.status === 'complete') return asset;
  if (asset.status === 'failed' || asset.status === 'refused') return asset;
  await new Promise((resolve) => window.setTimeout(resolve, 1000));
  return pollUntilDocumentReady(await getAsset(asset.id));
}

function uploadToPresignedURL(
  url: string,
  method: string,
  headers: Record<string, string>,
  file: File,
  onProgress: ProgressHandler,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(method, url);
    for (const [key, value] of Object.entries(headers)) xhr.setRequestHeader(key, value);
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && event.total > 0) onProgress(event.loaded / event.total);
    };
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve();
      else reject(new Error(`upload failed: HTTP ${String(xhr.status)}`));
    };
    xhr.onerror = () => reject(new Error('upload failed'));
    xhr.send(file);
  });
}
```

- [ ] **Step 4: Create upload dialog**

Create `web/src/documents/DocumentUploadDialog.tsx`:

```tsx
import { Upload } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { uploadLibraryDocument } from './documentUpload';

interface DocumentUploadDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly onUploaded: () => void;
}

export function DocumentUploadDialog({ open, onOpenChange, onUploaded }: DocumentUploadDialogProps) {
  const { t } = useTranslation();
  const [file, setFile] = useState<File | undefined>(undefined);
  const [progress, setProgress] = useState(0);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState('');

  async function upload() {
    if (file === undefined || uploading) return;
    setUploading(true);
    setError('');
    try {
      await uploadLibraryDocument(file, setProgress);
      onOpenChange(false);
      setFile(undefined);
      setProgress(0);
      onUploaded();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'upload failed');
    } finally {
      setUploading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('documents.upload.title')}</DialogTitle>
          <DialogDescription>{t('documents.upload.body')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="document-upload-file">{t('documents.upload.file')}</Label>
          <Input
            id="document-upload-file"
            type="file"
            onChange={(event) => setFile(event.target.files?.[0])}
          />
          {uploading ? (
            <div role="status" className="text-[13px] text-text-muted">
              {t('documents.upload.progress', { progress: Math.round(progress * 100) })}
            </div>
          ) : null}
          {error.length > 0 ? <div role="alert" className="text-[13px] text-danger">{error}</div> : null}
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('documents.actions.cancel')}
          </Button>
          <Button type="button" disabled={file === undefined || uploading} onClick={() => void upload()}>
            {uploading ? <Spinner /> : <Upload aria-hidden="true" />}
            {t('documents.actions.upload')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

- [ ] **Step 5: Add upload dialog translations**

Add:

```ts
upload: {
  title: 'Upload document',
  body: 'Add a document to the searchable library.',
  file: 'Document file',
  progress: 'Uploading {{progress}}%',
},
```

Italian:

```ts
upload: {
  title: 'Carica documento',
  body: 'Aggiungi un documento alla libreria ricercabile.',
  file: 'File documento',
  progress: 'Caricamento {{progress}}%',
},
```

- [ ] **Step 6: Wire upload dialog in workspace**

In `DocumentsWorkspace.tsx`, render:

```tsx
<DocumentUploadDialog
  open={uploadOpen}
  onOpenChange={setUploadOpen}
  onUploaded={() => {
    void loadDocuments({ query, tag, scope }, selectedId);
  }}
/>
```

- [ ] **Step 7: Update workspace test for upload entry**

Add:

```ts
fireEvent.click(screen.getByRole('button', { name: 'Upload' }));
expect(await screen.findByRole('dialog', { name: 'Upload document' })).toBeTruthy();
```

- [ ] **Step 8: Run tests and typecheck**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/documentUpload.test.ts src/documents/__tests__/DocumentsWorkspace.test.tsx
.\node_modules\.bin\tsc.cmd --noEmit
```

Expected: PASS.

- [ ] **Step 9: Commit library upload UI**

```powershell
git status --short
git add web/src/documents/documentUpload.ts web/src/documents/__tests__/documentUpload.test.ts web/src/documents/DocumentUploadDialog.tsx web/src/documents/DocumentsWorkspace.tsx web/src/documents/__tests__/DocumentsWorkspace.test.tsx web/src/i18n/resources.documents.ts
git commit -m "feat: add document library uploads"
```

---

## Task 7: Browser E2E For Product Library Flow

**Files:**
- Create: `web/e2e/documents-library.spec.ts`

- [ ] **Step 1: Create Playwright smoke test**

Create `web/e2e/documents-library.spec.ts`:

```ts
import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

test('document library exposes product file manager controls', async ({ page }) => {
  await gotoAuthenticated(page, '/?surface=documents');

  await expect(page.getByRole('heading', { name: /Document library|Libreria documenti/ })).toBeVisible();
  await expect(page.getByRole('searchbox', { name: /Search documents|Cerca documenti/ })).toBeVisible();
  await expect(page.getByRole('tab', { name: /All|Tutti/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /Upload|Carica/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /List view|Vista elenco/ })).toBeVisible();
});
```

- [ ] **Step 2: Run Playwright smoke**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\playwright.cmd test e2e/documents-library.spec.ts --project=chromium
```

Expected: PASS when the local app and auth test environment are available. If the app is not running, start the existing dev/server path used by current Playwright tests and rerun.

- [ ] **Step 3: Commit Playwright smoke**

```powershell
git status --short
git add web/e2e/documents-library.spec.ts
git commit -m "test: cover document library product shell"
```

---

## Task 8: Full Validation And Live User Flow

**Files:**
- No planned source edits.
- Read/use: `D:\tmp\Rag_docs`

- [ ] **Step 1: Run backend checks**

Run:

```powershell
go test -count=1 ./internal/assets ./internal/agui ./internal/documents ./cmd/aura
go vet ./...
```

Expected: PASS.

- [ ] **Step 2: Run frontend checks**

Run from `D:\Aura\web`:

```powershell
npm.cmd run lint
npm.cmd run typecheck
.\node_modules\.bin\vitest.cmd run src/documents/__tests__/documentViewModel.test.ts src/documents/__tests__/documentUpload.test.ts src/documents/__tests__/DocumentsWorkspace.test.tsx src/documents/__tests__/documentApi.test.ts
```

Expected: PASS.

- [ ] **Step 3: Rebuild web and Aura container**

Run from `D:\Aura\web`:

```powershell
npm.cmd run build
```

Run from `D:\Aura`:

```powershell
docker compose up -d --build aura
docker compose ps aura
```

Expected: `aura` is `Up` and healthy.

- [ ] **Step 4: Run Playwright product smoke**

Run from `D:\Aura\web`:

```powershell
.\node_modules\.bin\playwright.cmd test e2e/documents-library.spec.ts --project=chromium
```

Expected: PASS.

- [ ] **Step 5: Run live user-style upload/chat/delete**

Use Playwright or manual browser automation against the running Aura container:

1. Open authenticated Aura.
2. Navigate to the document surface.
3. Click `Upload`.
4. Upload `D:\tmp\Rag_docs\Corso Base Robot.docx`.
5. Wait for the library list to refresh and show the uploaded document.
6. Open details and verify status is `ready`, `searchable`, or another successful searchable state surfaced by the backend.
7. Open chat and ask a question about the uploaded document.
8. Verify the answer references retrieved document content.
9. Return to the library and delete the uploaded document.
10. Ask the same document-specific question again and verify the deleted document is not used as a source.

Expected: Upload, processing visibility, chat retrieval, delete, and stale retrieval prevention all work. If a backend condition prevents one step, capture the exact failing request/status and continue with the smallest aligned fix.

- [ ] **Step 6: Final status and commit any validation-only docs**

Run:

```powershell
git status --short
```

If validation required no source changes, do not commit. If validation produced a deliberate E2E note or fixture, stage only that file and commit:

```powershell
git add <exact-file>
git commit -m "test: document library live validation"
```

---

## Self-Review Checklist

- Spec coverage:
  - Product-first library shell: Tasks 3 and 7.
  - Search/filter chips and tabs: Tasks 2 and 3.
  - Dense file rows with status/date/size/actions: Task 3.
  - Details drawer with tags/versions/events/technical fields: Task 4.
  - Safe delete/retry/tag edit: Task 4, preserving existing API calls.
  - Advanced orphan cleanup outside first viewport: Task 5.
  - Real library upload: Tasks 1 and 6.
  - E2E and live user flow: Tasks 7 and 8.
- Scope is focused: no folders, no SVAR dependency, no backend version-binding feature beyond upload scope.
- User preferences included: no worktree, no parallel work, no `--no-verify`.
- Validation includes backend and frontend lint/test gates.
