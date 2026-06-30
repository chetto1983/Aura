import { useCallback, useEffect, useMemo, useState } from 'react';
import type { FormEvent } from 'react';
import { FileText, RefreshCw, Save, Search, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  deleteDocument,
  fetchDocumentDetail,
  fetchDocuments,
  updateDocument,
  type DocumentDetail,
  type DocumentItem,
  type DocumentScope,
  type DocumentStatus,
  type DocumentVersion,
  type UpdateDocumentInput,
} from './documentApi';
import { DocumentEventsPanel } from './DocumentEventsPanel';
import { formatBytes } from './documentFormat';
import { StorageOrphansPanel } from './StorageOrphansPanel';
import { Spinner } from '../components/Spinner';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge, type BadgeProps } from '@/components/ui/badge';
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
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { cn } from '@/lib/utils';

type ScopeFilter = DocumentScope | 'all';
type LoadStatus = 'loading' | 'ready' | 'error';

const defaultFilters = { query: '', tag: '', scope: 'all' as ScopeFilter };
const listLimit = 50;

export default function DocumentsWorkspace() {
  const { t } = useTranslation();
  const [query, setQuery] = useState(defaultFilters.query);
  const [tag, setTag] = useState(defaultFilters.tag);
  const [scope, setScope] = useState<ScopeFilter>(defaultFilters.scope);
  const [documents, setDocuments] = useState<readonly DocumentItem[]>([]);
  const [selectedId, setSelectedId] = useState('');
  const [detail, setDetail] = useState<DocumentDetail | undefined>(undefined);
  const [listStatus, setListStatus] = useState<LoadStatus>('loading');
  const [detailStatus, setDetailStatus] = useState<LoadStatus>('ready');
  const [tagDraft, setTagDraft] = useState('');
  const [savingTags, setSavingTags] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [deleting, setDeleting] = useState(false);

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
    void loadDocuments(defaultFilters);
  }, [loadDocuments]);

  const selectedDocument = detail?.document ?? documents.find((item) => item.id === selectedId);
  const activeVersion = useMemo(() => {
    if (detail === undefined) return undefined;
    return (
      detail.versions.find((version) => version.id === detail.document.active_version_id) ??
      detail.versions[0]
    );
  }, [detail]);

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void loadDocuments({ query, tag, scope }, selectedId);
  }

  async function selectDocument(id: string) {
    setSelectedId(id);
    await loadDetail(id);
  }

  async function saveTags() {
    const document = detail?.document;
    if (document === undefined || savingTags) return;
    const tags = parseTags(tagDraft);
    const body: UpdateDocumentInput = {
      title: document.title,
      tags,
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
      <div className="shrink-0 border-b border-border bg-surface px-4 py-4 sm:px-6">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <div className="grid h-10 w-10 shrink-0 place-items-center rounded-[var(--radius-md)] border border-border bg-surface-2 text-accent-text">
              <FileText data-icon="icon" aria-hidden="true" focusable="false" />
            </div>
            <h1 className="min-w-0 truncate font-display text-xl font-semibold text-text">
              {t('documents.title')}
            </h1>
          </div>
          <Button
            type="button"
            variant="outline"
            onClick={() => void loadDocuments({ query, tag, scope }, selectedId)}
          >
            <RefreshCw aria-hidden="true" />
            {t('documents.actions.refresh')}
          </Button>
        </div>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[22rem_minmax(0,1fr)]">
        <aside className="flex min-h-0 flex-col border-b border-border bg-surface/60 lg:border-b-0 lg:border-r">
          <form onSubmit={submitSearch} className="grid gap-3 border-b border-border p-3">
            <div className="grid gap-1.5">
              <Label htmlFor="documents-search">{t('documents.filters.search')}</Label>
              <Input
                id="documents-search"
                value={query}
                onChange={(event) => {
                  setQuery(event.target.value);
                }}
              />
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-1">
              <div className="grid gap-1.5">
                <Label htmlFor="documents-tag">{t('documents.filters.tag')}</Label>
                <Input
                  id="documents-tag"
                  value={tag}
                  onChange={(event) => {
                    setTag(event.target.value);
                  }}
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="documents-scope">{t('documents.filters.scope')}</Label>
                <NativeSelect
                  id="documents-scope"
                  value={scope}
                  onChange={(event) => {
                    setScope(event.target.value as ScopeFilter);
                  }}
                >
                  <NativeSelectOption value="all">{t('documents.scope.all')}</NativeSelectOption>
                  <NativeSelectOption value="library">
                    {t('documents.scope.library')}
                  </NativeSelectOption>
                  <NativeSelectOption value="thread">
                    {t('documents.scope.thread')}
                  </NativeSelectOption>
                </NativeSelect>
              </div>
            </div>
            <Button type="submit">
              <Search aria-hidden="true" />
              {t('documents.actions.search')}
            </Button>
          </form>

          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {listStatus === 'loading' ? (
              <StatusLine label={t('documents.loading')} />
            ) : listStatus === 'error' ? (
              <Alert variant="destructive">
                <AlertDescription>{t('documents.error.list')}</AlertDescription>
              </Alert>
            ) : documents.length === 0 ? (
              <StatusLine label={t('documents.empty')} />
            ) : (
              <div className="grid gap-2">
                {documents.map((document) => (
                  <DocumentRow
                    key={document.id}
                    document={document}
                    selected={document.id === selectedId}
                    onSelect={() => void selectDocument(document.id)}
                  />
                ))}
              </div>
            )}
          </div>
        </aside>

        <main className="min-h-0 overflow-y-auto">
          <div className="mx-auto flex w-full max-w-6xl flex-col gap-5 px-4 py-5 sm:px-6">
            {detailStatus === 'loading' ? (
              <StatusLine label={t('documents.loadingDetail')} />
            ) : detailStatus === 'error' ? (
              <Alert variant="destructive">
                <AlertDescription>{t('documents.error.detail')}</AlertDescription>
              </Alert>
            ) : selectedDocument === undefined || detail === undefined ? (
              <StatusLine label={t('documents.empty')} />
            ) : (
              <>
                <DocumentHeader document={detail.document} activeVersion={activeVersion} />
                <section className="grid gap-3 border-y border-border py-4">
                  <div className="grid gap-1.5">
                    <Label htmlFor="documents-tags">{t('documents.detail.tags')}</Label>
                    <Input
                      id="documents-tags"
                      value={tagDraft}
                      onChange={(event) => {
                        setTagDraft(event.target.value);
                      }}
                    />
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button type="button" disabled={savingTags} onClick={() => void saveTags()}>
                      {savingTags ? <Spinner /> : <Save aria-hidden="true" />}
                      {t('documents.actions.saveTags')}
                    </Button>
                    <Button
                      type="button"
                      variant="destructive"
                      onClick={() => {
                        setDeleteOpen(true);
                      }}
                    >
                      <Trash2 aria-hidden="true" />
                      {t('documents.actions.delete')}
                    </Button>
                  </div>
                </section>
                <section aria-labelledby="document-versions" className="flex flex-col gap-3">
                  <h2 id="document-versions" className="text-[18px] font-semibold text-text">
                    {t('documents.detail.versions')}
                  </h2>
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
                <DocumentEventsPanel documentId={detail.document.id} />
              </>
            )}
            <StorageOrphansPanel />
          </div>
        </main>
      </div>

      <DeleteDialog
        open={deleteOpen}
        document={detail?.document}
        confirm={deleteConfirm}
        deleting={deleting}
        onOpenChange={setDeleteOpen}
        onConfirmChange={setDeleteConfirm}
        onDelete={() => void confirmDelete()}
      />
    </section>
  );
}

function DocumentRow({
  document,
  selected,
  onSelect,
}: {
  readonly document: DocumentItem;
  readonly selected: boolean;
  readonly onSelect: () => void;
}) {
  return (
    <button
      type="button"
      aria-current={selected ? 'page' : undefined}
      onClick={onSelect}
      className={cn(
        'grid min-h-20 w-full gap-2 rounded-md border px-3 py-3 text-left transition-colors',
        selected
          ? 'border-border-strong bg-surface-3'
          : 'border-border bg-surface hover:bg-surface-2',
      )}
    >
      <span className="flex min-w-0 items-center justify-between gap-2">
        <span className="min-w-0 truncate text-[14px] font-semibold text-text">
          {document.title}
        </span>
        <StatusBadge status={document.status} />
      </span>
      <span className="flex flex-wrap gap-1">
        {document.tags.map((tag) => (
          <Badge key={tag} variant="secondary">
            {tag}
          </Badge>
        ))}
      </span>
    </button>
  );
}

function DocumentHeader({
  document,
  activeVersion,
}: {
  readonly document: DocumentItem;
  readonly activeVersion: DocumentVersion | undefined;
}) {
  return (
    <header className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="break-words font-display text-[24px] font-semibold text-text">
            {document.title}
          </h2>
          <p className="mt-1 break-all text-[13px] text-text-faint">{document.id}</p>
        </div>
        <StatusBadge status={document.status} />
      </div>
      {activeVersion === undefined ? null : (
        <dl className="grid gap-2 text-[13px] text-text-muted sm:grid-cols-3">
          <MetaItem label="MIME" value={activeVersion.content_type} />
          <MetaItem label="Size" value={formatBytes(activeVersion.size_bytes)} />
          <MetaItem label="SHA-256" value={activeVersion.sha256} />
        </dl>
      )}
    </header>
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
    <div className="grid gap-3 rounded-md border border-border bg-surface px-3 py-3">
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

function StatusBadge({ status }: { readonly status: DocumentStatus }) {
  const variant: BadgeProps['variant'] =
    status === 'ready'
      ? 'success'
      : status === 'failed' || status === 'deleted'
        ? 'danger'
        : status === 'processing' || status === 'queued'
          ? 'warning'
          : 'secondary';
  return <Badge variant={variant}>{status}</Badge>;
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
            onChange={(event) => {
              onConfirmChange(event.target.value);
            }}
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

function parseTags(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}
