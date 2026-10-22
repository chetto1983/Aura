import { lazy, Suspense, useState, type ReactNode } from 'react';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import { Files, PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { DocumentsNavigation } from './DocumentsNavigation';
import { DocumentsOverview } from './DocumentsOverview';
import { DocumentUpload, UploadQueue } from './DocumentUpload';
import { IngestionStatus } from './IngestionStatus';
import { useDocumentUploads } from './useDocumentUploads';
import { useIngestion, useSavedViews } from './useDocumentWorkspace';
import { documentHref, readDocumentQuery, resultHref, writeDocumentQuery } from './documentRoute';
import { EMPTY_FILTER, withField } from './documentsApi';
import { Drawer } from '@/shell/Drawer';
import { Button } from '@/components/ui/button';
import { useCapabilities } from '@/admin/useAdmin';

const DocumentsWorkspace = lazy(() => import('./DocumentsWorkspace'));
const FilesWorkspace = lazy(() => import('@/files/FilesWorkspace'));
const ListsPage = lazy(() =>
  import('./ListsPage').then((module) => ({ default: module.ListsPage })),
);
const DocumentDetail = lazy(() =>
  import('./DocumentDetail').then((module) => ({ default: module.DocumentDetail })),
);
const SavedViews = lazy(() =>
  import('./SavedViews').then((module) => ({ default: module.SavedViews })),
);

export default function DocumentsSurface({ mobileMenu }: { readonly mobileMenu?: ReactNode }) {
  const { identityId, isError } = useCapabilities();
  const { t } = useTranslation();
  if (!identityId)
    return (
      <p role={isError ? 'alert' : 'status'} className="p-5">
        {t(isError ? 'documents.loadFailed' : 'documents.loading')}
      </p>
    );
  return <Surface key={identityId} identity={identityId} mobileMenu={mobileMenu} />;
}
function Surface({
  identity,
  mobileMenu,
}: {
  readonly identity: string;
  readonly mobileMenu?: ReactNode;
}) {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const { '*': tail = '' } = useParams();
  const destination = tail === '' ? 'overview' : (tail.split('/')[0] ?? 'overview');
  const docId = destination === 'document' ? tail.slice('document/'.length) : '';
  const [drawer, setDrawer] = useState(false);
  const [compact, setCompact] = useState(false);
  const uploads = useDocumentUploads(identity);
  const views = useSavedViews(identity);
  const ingestion = useIngestion(identity);
  const query = readDocumentQuery(params.toString());
  const title =
    destination === 'document' ? t('documents.title') : t(`documents.workspace.${destination}`);
  const uploadButton = <DocumentUpload onFiles={uploads.add} />;
  const nav = (
    <DocumentsNavigation
      identity={identity}
      views={views.data ?? []}
      compact={compact}
      onNavigate={() => {
        setDrawer(false);
      }}
    />
  );
  let content: ReactNode;
  switch (destination) {
    case 'overview':
      content = (
        <DocumentsOverview identity={identity} views={views.data ?? []} upload={uploadButton} />
      );
      break;
    case 'library':
    case 'inbox':
      content = <DocumentsWorkspace upload={uploadButton} />;
      break;
    case 'processing':
      content = (
        <div className="h-full overflow-y-auto p-4 md:p-6">
          <h1 className="font-display text-2xl">{title}</h1>
          <p className="my-3 max-w-2xl text-sm text-text-muted">
            {t('documents.workspace.processingHint')}
          </p>
          <div className="mb-5 rounded-xl border border-border bg-surface p-4">
            <h2 className="mb-2 font-semibold">{t('documents.workspace.indexing')}</h2>
            {ingestion.isError ? (
              <p role="alert">
                {t('documents.loadFailed')}{' '}
                <Button
                  onClick={() => {
                    void ingestion.refetch();
                  }}
                >
                  {t('documents.workspace.retry')}
                </Button>
              </p>
            ) : (
              <IngestionStatus state={ingestion.data} />
            )}
          </div>
          {uploadButton}
          <UploadQueue queue={uploads} />
        </div>
      );
      break;
    case 'views':
      content = (
        <SavedViews
          identity={identity}
          views={views.data ?? []}
          error={views.isError}
          onRetry={() => {
            void views.refetch();
          }}
        />
      );
      break;
    case 'lists':
      content = (
        <ListsPage
          onClose={() => {
            void navigate('/documents/library');
          }}
          onFilter={(field, id) => {
            void navigate(
              `/documents/library?${writeDocumentQuery({ ...query, filter: withField({ ...EMPTY_FILTER, fileType: 'all' }, 'include', field, id), page: { ...query.page, offset: 0 } })}`,
            );
          }}
        />
      );
      break;
    case 'files':
      content = <FilesWorkspace />;
      break;
    case 'document':
      content = (
        <DocumentDetail
          key={docId}
          id={docId}
          onClose={() => {
            void navigate(resultHref(params));
          }}
          onOpen={(id, offset) => {
            void navigate(
              documentHref(id, params.get('from') ?? '/documents/library', {
                ...query,
                page: { ...query.page, offset: offset ?? query.page.offset },
              }),
            );
          }}
        />
      );
      break;
    default:
      content = (
        <div className="p-6">
          <p>{t('documents.loadFailed')}</p>
          <Link to="/documents/library">{t('documents.workspace.library')}</Link>
        </div>
      );
  }
  return (
    <div
      className="flex h-full min-h-0 min-w-0 bg-bg"
      onDragOver={(event) => {
        if (destination === 'files') return;
        if (event.dataTransfer.types.includes('Files')) event.preventDefault();
      }}
      onDrop={(event) => {
        if (destination === 'files') return;
        if (!event.dataTransfer.files.length) return;
        event.preventDefault();
        uploads.add(Array.from(event.dataTransfer.files));
      }}
    >
      <aside
        className={`${compact ? 'w-16' : 'w-60'} hidden shrink-0 flex-col border-r border-border bg-surface lg:flex`}
      >
        <div className="flex h-14 items-center justify-between border-b border-border px-3">
          {!compact && (
            <span className="font-display text-lg font-semibold">{t('documents.title')}</span>
          )}
          <Button
            variant="ghost"
            size="icon"
            aria-label={t(`documents.workspace.${compact ? 'expand' : 'collapse'}`)}
            onClick={() => {
              setCompact(!compact);
            }}
          >
            {compact ? <PanelLeftOpen /> : <PanelLeftClose />}
          </Button>
        </div>
        {nav}
      </aside>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-2 lg:hidden">
          {mobileMenu}
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('documents.workspace.nav')}
            onClick={() => {
              setDrawer(true);
            }}
          >
            <Files aria-hidden="true" />
          </Button>
          <span className="font-semibold">{title}</span>
        </div>
        {uploads.items.length > 0 && destination !== 'processing' && (
          <Link
            to="/documents/processing"
            className="border-b border-border bg-surface-2 px-4 py-2 text-sm text-info"
          >
            {t('documents.workspace.uploaded')} · {uploads.items.length}
          </Link>
        )}
        <Suspense
          fallback={
            <p role="status" className="p-6">
              {t('documents.loading')}
            </p>
          }
        >
          <div
            className="min-h-0 min-w-0 flex-1 overflow-hidden"
            data-document-destination={pathname}
          >
            {content}
          </div>
        </Suspense>
      </div>
      <Drawer
        open={drawer}
        side="left"
        title={t('documents.title')}
        onClose={() => {
          setDrawer(false);
        }}
      >
        <DocumentsNavigation
          identity={identity}
          views={views.data ?? []}
          onNavigate={() => {
            setDrawer(false);
          }}
        />
      </Drawer>
    </div>
  );
}
