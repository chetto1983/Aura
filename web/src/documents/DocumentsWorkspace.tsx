import type { ReactNode } from 'react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Settings2, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import type { Asset } from '../chat/attachments/types';
import { waitForDocumentIngestion } from './documentUpload';
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
import { DocumentDetailsDrawer } from './DocumentDetailsDrawer';
import { DocumentFileList } from './DocumentFileList';
import { DocumentFilterBar, type ScopeFilter, type ViewMode } from './DocumentFilterBar';
import { DocumentLibraryHeader } from './DocumentLibraryHeader';
import { DocumentUploadDialog } from './DocumentUploadDialog';
import { StorageOrphansPanel } from './StorageOrphansPanel';
import { activeVersionFor, parseDocumentTags, type DocumentTab } from './documentViewModel';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

type LoadStatus = 'loading' | 'ready' | 'error';

const defaultFilters = { query: '', tag: '', scope: 'all' as ScopeFilter };
const listLimit = 50;

interface DocumentsWorkspaceProps {
  readonly mobileMenu?: ReactNode;
  readonly onAskDocument?: (document: DocumentItem) => void;
}

export default function DocumentsWorkspace({ mobileMenu, onAskDocument }: DocumentsWorkspaceProps) {
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
  const [tagDraft, setTagDraft] = useState('');
  const [savingTags, setSavingTags] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<DocumentItem | undefined>(undefined);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState('');
  const [deleting, setDeleting] = useState(false);
  const [adminOpen, setAdminOpen] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);

  const loadDetail = useCallback(async (id: string) => {
    try {
      const next = await fetchDocumentDetail(id);
      setDetail(next);
      setTagDraft(next.document.tags.join(', '));
    } catch {
      setDetail(undefined);
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

  // The upload dialog closes as soon as the bytes are accepted, so nothing is on screen
  // waiting for ingestion any more. Poll it here instead and refresh once it settles, so a
  // row leaves the processing tab without the operator hitting refresh.
  const ingestionWatchers = useRef<Set<AbortController>>(new Set());
  useEffect(() => {
    const watchers = ingestionWatchers.current;
    return () => {
      for (const controller of watchers) controller.abort();
      watchers.clear();
    };
  }, []);

  const watchIngestion = useCallback(async (asset: Asset, refresh: () => void) => {
    const controller = new AbortController();
    ingestionWatchers.current.add(controller);
    try {
      await waitForDocumentIngestion(asset, { signal: controller.signal });
      if (!controller.signal.aborted) refresh();
    } catch {
      // A dropped poll leaves the row at its last known status; refresh reconciles it.
    } finally {
      ingestionWatchers.current.delete(controller);
    }
  }, []);

  const activeVersion = useMemo(() => activeVersionFor(detail), [detail]);
  const activeVersions = useMemo(() => {
    const versions = new Map<string, DocumentVersion | undefined>();
    if (detail !== undefined) versions.set(detail.document.id, activeVersion);
    return versions;
  }, [activeVersion, detail]);
  const selectedDocument = detail?.document ?? documents.find((item) => item.id === selectedId);

  function searchDocuments() {
    void loadDocuments({ query, tag, scope }, selectedId);
  }

  async function openDocument(id: string) {
    setSelectedId(id);
    const document = documents.find((item) => item.id === id);
    if (document !== undefined) setTagDraft(document.tags.join(', '));
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

  async function retryDocument(id: string) {
    if (retrying || id === '') return;
    setRetrying(true);
    try {
      const next = await fetchDocumentDetail(id);
      const version =
        next.versions.find((item) => item.id === next.document.active_version_id) ??
        next.versions[0];
      if (version?.asset_id !== undefined && version.asset_id !== '') {
        await retryDocumentAsset(version.asset_id);
      }
      await loadDocuments({ query, tag, scope }, id);
    } finally {
      setRetrying(false);
    }
  }

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

  return (
    <section aria-label={t('documents.title')} className="flex h-full min-h-0 flex-col bg-bg">
      <DocumentLibraryHeader
        query={query}
        refreshing={listStatus === 'loading'}
        mobileMenu={mobileMenu}
        onQueryChange={setQuery}
        onSearch={searchDocuments}
        onRefresh={searchDocuments}
        onUpload={() => {
          setUploadOpen(true);
        }}
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
      <main className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
        <DocumentFileList
          documents={documents}
          activeVersions={activeVersions}
          tab={tab}
          viewMode={viewMode}
          selectedIds={selectedIds}
          activeId={selectedId}
          loading={listStatus === 'loading'}
          error={listStatus === 'error'}
          onToggleSelected={toggleSelected}
          onOpenDetails={(id) => {
            setDrawerOpen(true);
            void openDocument(id);
          }}
          onOpenActions={(id) => {
            void openDocument(id);
          }}
          onAskDocument={(document) => {
            if (onAskDocument !== undefined) {
              onAskDocument(document);
            } else {
              setDrawerOpen(true);
              void openDocument(document.id);
            }
          }}
          onEditTags={(document) => {
            setDrawerOpen(true);
            void openDocument(document.id);
          }}
          onRetry={(document) => {
            void retryDocument(document.id);
          }}
          onDelete={(document) => {
            setDeleteTarget(document);
            setSelectedId(document.id);
            setDeleteOpen(true);
          }}
          onRefresh={searchDocuments}
        />
        <DocumentDetailsDrawer
          open={drawerOpen}
          document={selectedDocument}
          detail={detail}
          activeVersion={activeVersion}
          tagDraft={tagDraft}
          savingTags={savingTags}
          onTagDraftChange={setTagDraft}
          onSaveTags={() => void saveTags()}
          onDelete={() => {
            setDeleteTarget(selectedDocument);
            setDeleteOpen(true);
          }}
          onClose={() => {
            setDrawerOpen(false);
          }}
        />
        <section className="hidden border-t border-border px-4 py-3 sm:block sm:px-6">
          <div className="mx-auto w-full max-w-6xl">
            <Button
              type="button"
              variant="ghost"
              aria-expanded={adminOpen}
              onClick={() => {
                setAdminOpen((open) => !open);
              }}
            >
              <Settings2 aria-hidden="true" />
              {t('documents.admin.maintenance')}
            </Button>
            {adminOpen ? (
              <div className="mt-3">
                <StorageOrphansPanel />
              </div>
            ) : null}
          </div>
        </section>
      </main>
      <DocumentUploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        onUploaded={(asset) => {
          const refresh = () => {
            void loadDocuments({ query, tag, scope }, selectedId);
          };
          refresh();
          void watchIngestion(asset, refresh);
        }}
      />
      <DeleteDialog
        open={deleteOpen}
        document={deleteTarget ?? detail?.document}
        confirm={deleteConfirm}
        deleting={deleting}
        onOpenChange={(open) => {
          setDeleteOpen(open);
          if (!open) {
            setDeleteTarget(undefined);
            setDeleteConfirm('');
          }
        }}
        onConfirmChange={setDeleteConfirm}
        onDelete={() => {
          void confirmDelete(deleteTarget ?? detail?.document);
        }}
      />
    </section>
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
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      role="alertdialog"
      title={t('documents.delete.title', { title: document.title })}
      description={t('documents.delete.body')}
      cancelLabel={t('documents.actions.cancel')}
      confirmLabel={t('documents.actions.deletePermanently')}
      confirmDisabled={confirm !== 'DELETE'}
      confirmPending={deleting}
      confirmIcon={deleting ? <Spinner /> : <Trash2 data-icon aria-hidden="true" />}
      onConfirm={onDelete}
    >
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="documents-delete-confirm">{t('documents.delete.confirmLabel')}</Label>
        <Input
          id="documents-delete-confirm"
          value={confirm}
          onChange={(event) => {
            onConfirmChange(event.target.value);
          }}
        />
      </div>
    </ConfirmDialog>
  );
}
