import { useCallback, useEffect, useMemo, useState } from 'react';
import { RefreshCw, Save, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import {
  deleteDocument,
  fetchDocumentDetail,
  fetchDocuments,
  retryDocumentAsset,
  updateDocument,
  type DocumentDetail,
  type DocumentItem,
  type DocumentScope,
  type DocumentVersion,
  type UpdateDocumentInput,
} from './documentApi';
import { DocumentEventsPanel } from './DocumentEventsPanel';
import { DocumentFileList } from './DocumentFileList';
import { DocumentFilterBar, type ScopeFilter, type ViewMode } from './DocumentFilterBar';
import { DocumentLibraryHeader } from './DocumentLibraryHeader';
import { formatBytes } from './documentFormat';
import { StorageOrphansPanel } from './StorageOrphansPanel';
import {
  activeVersionFor,
  formatDocumentDate,
  parseDocumentTags,
  statusToneFor,
  type DocumentTab,
} from './documentViewModel';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
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

type LoadStatus = 'loading' | 'ready' | 'error';

const defaultFilters = { query: '', tag: '', scope: 'all' as ScopeFilter };
const listLimit = 50;

export default function DocumentsWorkspace() {
  const { t } = useTranslation();
  const [query, setQuery] = useState(defaultFilters.query);
  const [tag, setTag] = useState(defaultFilters.tag);
  const [scope, setScope] = useState<ScopeFilter>(defaultFilters.scope);
  const [tab, setTab] = useState<DocumentTab>('all');
  const [viewMode, setViewMode] = useState<ViewMode>('list');
  const [documents, setDocuments] = useState<readonly DocumentItem[]>([]);
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(() => new Set());
  const [selectedId, setSelectedId] = useState('');
  const [detail, setDetail] = useState<DocumentDetail | undefined>(undefined);
  const [listStatus, setListStatus] = useState<LoadStatus>('loading');
  const [detailStatus, setDetailStatus] = useState<LoadStatus>('ready');
  const [tagDraft, setTagDraft] = useState('');
  const [savingTags, setSavingTags] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [deleting, setDeleting] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);

  const loadDetail = useCallback(async (id: string) => {
    setDetailStatus('loading');
    try {
      const next = await fetchDocumentDetail(id);
      setDetail(next);
      setTagDraft(next.document.tags.join(', '));
      setDetailStatus('ready');
    } catch {
      setDetail(undefined);
      setDetailStatus('error');
    }
  }, []);

  const loadDocuments = useCallback(
    async (filters: typeof defaultFilters, preferredId = '') => {
      setListStatus('loading');
      try {
        const params: {
          query: string;
          tag: string;
          limit: number;
          scope?: DocumentScope;
        } = {
          query: filters.query,
          tag: filters.tag,
          limit: listLimit,
        };
        if (filters.scope !== 'all') params.scope = filters.scope;
        const items = await fetchDocuments(params);
        setDocuments(items);
        setListStatus('ready');
        const nextId =
          preferredId !== '' && items.some((item) => item.id === preferredId)
            ? preferredId
            : (items[0]?.id ?? '');
        setSelectedId(nextId);
        if (nextId === '') {
          setDetail(undefined);
          setTagDraft('');
        } else {
          await loadDetail(nextId);
        }
      } catch {
        setDocuments([]);
        setListStatus('error');
        setDetail(undefined);
      }
    },
    [loadDetail],
  );

  useEffect(() => {
    void Promise.resolve().then(() => loadDocuments(defaultFilters));
  }, [loadDocuments]);

  const activeVersion = useMemo(() => activeVersionFor(detail), [detail]);
  const activeVersions = useMemo(() => {
    const versions = new Map<string, DocumentVersion | undefined>();
    if (detail !== undefined) versions.set(detail.document.id, activeVersion);
    return versions;
  }, [activeVersion, detail]);

  function searchDocuments() {
    void loadDocuments({ query, tag, scope }, selectedId);
  }

  async function openDocument(id: string) {
    setSelectedId(id);
    await loadDetail(id);
  }

  function toggleSelected(id: string) {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  async function saveTags() {
    const document = detail?.document;
    if (document === undefined || savingTags) return;
    const body: UpdateDocumentInput = {
      title: document.title,
      tags: parseDocumentTags(tagDraft),
      scope: document.scope,
      status: document.status,
      ...(document.active_version_id !== undefined && document.active_version_id !== ''
        ? { active_version_id: document.active_version_id }
        : {}),
      metadata: document.metadata,
    };
    setSavingTags(true);
    try {
      const updated = await updateDocument(document.id, body);
      setDetail((prev) => (prev === undefined ? prev : { ...prev, document: updated }));
      setDocuments((prev) => prev.map((item) => (item.id === updated.id ? updated : item)));
      setTagDraft(updated.tags.join(', '));
    } finally {
      setSavingTags(false);
    }
  }

  async function retryActiveAsset() {
    const assetID = activeVersion?.asset_id;
    if (detail === undefined || assetID === undefined || assetID === '' || retrying) return;
    setRetrying(true);
    try {
      await retryDocumentAsset(assetID);
      await loadDocuments({ query, tag, scope }, detail.document.id);
    } finally {
      setRetrying(false);
    }
  }

  async function confirmDelete() {
    const document = detail?.document;
    if (document === undefined || deleteConfirm !== 'DELETE' || deleting) return;
    setDeleting(true);
    try {
      await deleteDocument(document.id);
      setDeleteOpen(false);
      setDeleteConfirm('');
      await loadDocuments({ query, tag, scope });
    } finally {
      setDeleting(false);
    }
  }

  return (
    <section aria-label={t('documents.title')} className="flex h-full min-h-0 flex-col bg-bg">
      <DocumentLibraryHeader
        query={query}
        refreshing={listStatus === 'loading'}
        onQueryChange={setQuery}
        onSearch={searchDocuments}
        onRefresh={searchDocuments}
        onUpload={() => setUploadOpen(true)}
      />
      <DocumentFilterBar
        tab={tab}
        tag={tag}
        scope={scope}
        viewMode={viewMode}
        onTabChange={setTab}
        onTagChange={setTag}
        onScopeChange={setScope}
        onViewModeChange={setViewMode}
      />
      <main className="relative min-h-0 flex-1 overflow-y-auto">
        <DocumentFileList
          documents={documents}
          activeVersions={activeVersions}
          tab={tab}
          selectedIds={selectedIds}
          activeId={selectedId}
          loading={listStatus === 'loading'}
          error={listStatus === 'error'}
          onToggleSelected={toggleSelected}
          onOpenDetails={(id) => {
            void openDocument(id);
          }}
          onOpenActions={(id) => {
            void openDocument(id);
          }}
          onRefresh={searchDocuments}
        />
        <SelectedDocumentPanel
          detail={detail}
          status={detailStatus}
          activeVersion={activeVersion}
          tagDraft={tagDraft}
          savingTags={savingTags}
          retrying={retrying}
          onTagDraftChange={setTagDraft}
          onSaveTags={() => void saveTags()}
          onRetry={() => void retryActiveAsset()}
          onDelete={() => setDeleteOpen(true)}
        />
        <div className="mx-auto w-full max-w-6xl px-4 pb-5 sm:px-6">
          <StorageOrphansPanel />
        </div>
      </main>
      {uploadOpen ? (
        <div role="status" className="sr-only">
          {t('documents.actions.upload')}
        </div>
      ) : null}
      <DeleteDialog
        open={deleteOpen}
        document={detail?.document}
        confirm={deleteConfirm}
        deleting={deleting}
        onOpenChange={setDeleteOpen}
        onConfirmChange={setDeleteConfirm}
        onDelete={() => {
          void confirmDelete();
        }}
      />
    </section>
  );
}

function SelectedDocumentPanel({
  detail,
  status,
  activeVersion,
  tagDraft,
  savingTags,
  retrying,
  onTagDraftChange,
  onSaveTags,
  onRetry,
  onDelete,
}: {
  readonly detail: DocumentDetail | undefined;
  readonly status: LoadStatus;
  readonly activeVersion: DocumentVersion | undefined;
  readonly tagDraft: string;
  readonly savingTags: boolean;
  readonly retrying: boolean;
  readonly onTagDraftChange: (value: string) => void;
  readonly onSaveTags: () => void;
  readonly onRetry: () => void;
  readonly onDelete: () => void;
}) {
  const { t } = useTranslation();
  if (status === 'loading') return <StatusLine label={t('documents.loadingDetail')} />;
  if (status === 'error') {
    return (
      <div className="mx-auto w-full max-w-6xl px-4 py-4 sm:px-6">
        <Alert variant="destructive">
          <AlertDescription>{t('documents.error.detail')}</AlertDescription>
        </Alert>
      </div>
    );
  }
  if (detail === undefined) return null;
  return (
    <section className="mx-auto grid w-full max-w-6xl gap-4 border-t border-border px-4 py-5 sm:px-6">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="truncate text-[18px] font-semibold text-text">{detail.document.title}</h2>
          <p className="mt-1 text-[13px] text-text-muted">
            {formatDocumentDate(detail.document.updated_at ?? detail.document.created_at)}
          </p>
        </div>
        <Badge variant={statusToneFor(detail.document.status)}>{detail.document.status}</Badge>
      </header>
      <dl className="grid gap-2 text-[13px] text-text-muted sm:grid-cols-3">
        <MetaItem label="MIME" value={activeVersion?.content_type ?? '-'} />
        <MetaItem
          label={t('documents.view.size')}
          value={activeVersion === undefined ? '-' : formatBytes(activeVersion.size_bytes)}
        />
        <MetaItem label="SHA-256" value={activeVersion?.sha256 ?? '-'} />
      </dl>
      <div className="grid gap-1.5">
        <Label htmlFor="documents-tags">{t('documents.detail.tags')}</Label>
        <Input
          id="documents-tags"
          value={tagDraft}
          onChange={(event) => onTagDraftChange(event.target.value)}
        />
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" disabled={savingTags} onClick={onSaveTags}>
          {savingTags ? <Spinner /> : <Save aria-hidden="true" />}
          {t('documents.actions.saveTags')}
        </Button>
        {detail.document.status === 'failed' && activeVersion?.asset_id ? (
          <Button type="button" variant="outline" disabled={retrying} onClick={onRetry}>
            {retrying ? <Spinner /> : <RefreshCw aria-hidden="true" />}
            {t('documents.actions.retryProcessing')}
          </Button>
        ) : null}
        <Button type="button" variant="destructive" onClick={onDelete}>
          <Trash2 aria-hidden="true" />
          {t('documents.actions.delete')}
        </Button>
      </div>
      <section aria-labelledby="document-versions" className="grid gap-3">
        <h3 id="document-versions" className="text-[16px] font-semibold text-text">
          {t('documents.detail.versions')}
        </h3>
        <div className="grid gap-2">
          {detail.versions.map((version) => (
            <VersionRow
              key={version.id}
              version={version}
              active={version.id === detail.document.active_version_id}
            />
          ))}
        </div>
      </section>
      <DocumentEventsPanel key={detail.document.id} documentId={detail.document.id} />
    </section>
  );
}

function VersionRow({
  version,
  active,
}: {
  readonly version: DocumentVersion;
  readonly active: boolean;
}) {
  return (
    <div className="grid gap-2 rounded-md border border-border bg-surface px-3 py-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="text-[14px] font-semibold text-text">v{version.version_number}</span>
          {active ? <Badge variant="success">active</Badge> : null}
        </div>
        <Badge variant="secondary">{version.status}</Badge>
      </div>
      <dl className="grid gap-2 text-[13px] text-text-muted md:grid-cols-2 xl:grid-cols-4">
        <MetaItem label="MIME" value={version.content_type} />
        <MetaItem label="Size" value={formatBytes(version.size_bytes)} />
        <MetaItem label="Storage" value={version.storage_object_id} />
        <MetaItem label="SHA-256" value={version.sha256} />
      </dl>
    </div>
  );
}

function MetaItem({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="min-w-0">
      <dt className="text-[11px] font-semibold uppercase text-text-faint">{label}</dt>
      <dd className="break-all font-mono text-[12px] text-text">{value}</dd>
    </div>
  );
}

function StatusLine({ label }: { readonly label: string }) {
  return (
    <div role="status" className="grid min-h-40 place-items-center p-6 text-sm text-text-muted">
      {label}
    </div>
  );
}

function DeleteDialog({
  open,
  document,
  confirm,
  deleting,
  onOpenChange,
  onConfirmChange,
  onDelete,
}: {
  readonly open: boolean;
  readonly document: DocumentItem | undefined;
  readonly confirm: string;
  readonly deleting: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly onConfirmChange: (value: string) => void;
  readonly onDelete: () => void;
}) {
  const { t } = useTranslation();
  if (document === undefined) return null;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('documents.delete.title', { title: document.title })}</DialogTitle>
          <DialogDescription>{t('documents.delete.body')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-1.5">
          <Label htmlFor="documents-delete-confirm">{t('documents.delete.confirmLabel')}</Label>
          <Input
            id="documents-delete-confirm"
            value={confirm}
            onChange={(event) => onConfirmChange(event.target.value)}
          />
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t('documents.actions.cancel')}
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={confirm !== 'DELETE' || deleting}
            onClick={onDelete}
          >
            {deleting ? <Spinner /> : <Trash2 aria-hidden="true" />}
            {t('documents.actions.deletePermanently')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
