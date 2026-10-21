import type { ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router';
import { useTranslation } from 'react-i18next';
import { EMPTY_FILTER, fetchDocuments } from './documentsApi';
import { documentHref, readDocumentQuery, savedViewHref } from './documentRoute';
import type { SavedView } from './workspaceApi';
import { Button } from '@/components/ui/button';

export function DocumentsOverview({
  identity,
  views,
  upload,
}: {
  readonly identity: string;
  readonly views: readonly SavedView[];
  readonly upload: ReactNode;
}) {
  const { t } = useTranslation();
  const recent = useQuery({
    queryKey: ['documents', identity, 'recent'],
    queryFn: ({ signal }) =>
      fetchDocuments(EMPTY_FILTER, { sort: '-indexed_at', offset: 0, limit: 6 }, signal),
  });
  const inbox = useQuery({
    queryKey: ['documents', identity, 'inbox-count'],
    queryFn: ({ signal }) =>
      fetchDocuments({ ...EMPTY_FILTER, inbox: true }, { sort: '', offset: 0, limit: 1 }, signal),
  });
  return (
    <section className="h-full overflow-y-auto p-4 md:p-8">
      <header className="mb-7 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="font-display text-3xl font-semibold tracking-tight">
            {t('documents.workspace.overview')}
          </h1>
          <p className="mt-2 text-sm text-text-muted">{t('documents.workspace.subtitle')}</p>
        </div>
        {upload}
      </header>
      <div className="mb-7 grid gap-3 sm:grid-cols-2">
        <Link to="/documents/library" className="rounded-xl border border-border bg-surface p-5">
          <span className="text-sm text-text-muted">{t('documents.workspace.library')}</span>
          <strong className="mt-2 block font-display text-4xl">{recent.data?.total ?? '—'}</strong>
        </Link>
        <Link to="/documents/inbox" className="rounded-xl border border-border bg-info/5 p-5">
          <span className="text-sm text-info">{t('documents.workspace.inbox')}</span>
          <strong className="mt-2 block font-display text-4xl">{inbox.data?.total ?? '—'}</strong>
          <p className="mt-2 text-xs text-text-muted">{t('documents.workspace.inboxHint')}</p>
        </Link>
      </div>
      {(recent.isError || inbox.isError) && (
        <p role="alert">
          {t('documents.loadFailed')}{' '}
          <Button
            variant="outline"
            onClick={() => {
              void recent.refetch();
              void inbox.refetch();
            }}
          >
            {t('documents.workspace.retry')}
          </Button>
        </p>
      )}
      <div className="mb-7 rounded-xl border border-dashed border-border p-5 text-sm text-text-muted">
        {t('documents.workspace.drop')}
      </div>
      <h2 className="mb-3 font-display text-xl font-semibold">{t('documents.workspace.recent')}</h2>
      {recent.isPending ? (
        <p role="status">{t('documents.loading')}</p>
      ) : recent.data?.rows.length === 0 ? (
        <p>{t('documents.workspace.emptyHint')}</p>
      ) : (
        <ul className="divide-y divide-border rounded-xl border border-border bg-surface">
          {recent.data?.rows.map((row) => (
            <li key={row.id}>
              <Link
                to={documentHref(row.id, '/documents/library', readDocumentQuery(''))}
                className="flex flex-wrap items-center justify-between gap-2 p-4 hover:bg-surface-2"
              >
                <span className="min-w-0 flex-[1_1_16rem] break-words text-sm font-medium">
                  {row.file_name}
                </span>
                <span className="text-xs text-text-muted">{row.indexed_at.slice(0, 10)}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
      {views.some((view) => view.dashboard) && (
        <>
          <h2 className="mt-7 mb-3 font-display text-xl font-semibold">
            {t('documents.workspace.views')}
          </h2>
          <div className="grid gap-3 sm:grid-cols-2">
            {views
              .filter((view) => view.dashboard)
              .map((view) => (
                <Link
                  key={view.id}
                  to={savedViewHref(view)}
                  className="rounded-xl border border-border bg-surface p-4 text-sm font-semibold"
                >
                  {view.name}
                </Link>
              ))}
          </div>
        </>
      )}
    </section>
  );
}
