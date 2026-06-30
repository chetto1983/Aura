import { useCallback, useEffect, useMemo, useState } from 'react';
import { Settings2, Trash2 } from 'lucide-react';
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
import { DocumentActionMenu } from './DocumentActionMenu';
import { DocumentDetailsDrawer } from './DocumentDetailsDrawer';
import { DocumentFileList } from './DocumentFileList';
import { DocumentFilterBar, type ScopeFilter, type ViewMode } from './DocumentFilterBar';
import { DocumentLibraryHeader } from './DocumentLibraryHeader';
import { DocumentUploadDialog } from './DocumentUploadDialog';
import { StorageOrphansPanel } from './StorageOrphansPanel';
import { activeVersionFor, parseDocumentTags, type DocumentTab } from './documentViewModel';
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

interface DocumentsWorkspaceProps {
  readonly onAskDocument?: (document: DocumentItem) => void;
}

export default function DocumentsWorkspace({ onAskDocument }: DocumentsWorkspaceProps) {
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
  const [actionMenuId, setActionMenuId] = useState('');
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

  const activeVersion = useMemo(() => activeVersionFor(detail), [detail]);
  const activeVersions = useMemo(() => {
    const versions = new Map<string, DocumentVersion | undefined>();
    if (detail !== undefined) versions.set(detail.document.id, activeVersion);
    return versions;
  }, [activeVersion, detail]);
  const selectedDocument = detail?.document ?? documents.find((item) => item.id === selectedId);
  const actionDocument = documents.find((item) => item.id === actionMenuId);

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
        onQueryChange={setQuery}
        onSearch={searchDocuments}
        onRefresh={searchDocuments}
        onUpload={() => { setUploadOpen(true); }}
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
            setDrawerOpen(true);
            setActionMenuId('');
            void openDocument(id);
          }}
          onOpenActions={(id) => {
            setActionMenuId((current) => (current === id ? '' : id));
            void openDocument(id);
          }}
          onRefresh={searchDocuments}
        />
        <DocumentActionMenu
          document={actionDocument}
          open={actionMenuId !== ''}
          onClose={() => { setActionMenuId(''); }}
          onAsk={() => {
            if (actionDocument !== undefined) {
              if (onAskDocument !== undefined) {
                onAskDocument(actionDocument);
              } else {
                setDrawerOpen(true);
                void openDocument(actionDocument.id);
              }
            }
            setActionMenuId('');
          }}
          onEditTags={() => {
            if (actionDocument !== undefined) {
              setDrawerOpen(true);
              void openDocument(actionDocument.id);
            }
            setActionMenuId('');
          }}
          onRetry={() => {
            const id = actionMenuId;
            setActionMenuId('');
            void retryDocument(id);
          }}
          onDelete={() => {
            if (actionDocument !== undefined) {
              setDeleteTarget(actionDocument);
              setSelectedId(actionDocument.id);
              setDeleteOpen(true);
            }
            setActionMenuId('');
          }}
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
          onClose={() => { setDrawerOpen(false); }}
        />
        <section className="border-t border-border px-4 py-3 sm:px-6">
          <div className="mx-auto w-full max-w-6xl">
            <Button
              type="button"
              variant="ghost"
              aria-expanded={adminOpen}
              onClick={() => { setAdminOpen((open) => !open); }}
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
        onUploaded={() => {
          void loadDocuments({ query, tag, scope }, selectedId);
        }}
      />
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
            onChange={(event) => { onConfirmChange(event.target.value); }}
          />
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => { onOpenChange(false); }}>
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
