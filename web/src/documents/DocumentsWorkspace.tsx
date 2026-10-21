import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { EMPTY_FILTER, fetchDocuments, withField, type DocumentsFilter } from './documentsApi';
import { BulkBar } from './BulkBar';
import { BulkConfirmDialog } from './BulkConfirmDialog';
import { FilterBar } from './FilterBar';
import { useBulkEdit } from './useBulkEdit';
import { readShownFields, saveShownFields } from './documentsPrefs';
import { useDocumentLists } from './useDocumentLists';
import { useWideView } from './useWideView';
import {
  documentHref,
  readDocumentQuery,
  writeDocumentQuery,
  type DocumentQuery,
} from './documentRoute';
import { DocumentTable } from './DocumentTable';
import { DocumentCards } from './DocumentCards';
import { SaveViewButton } from './SavedViews';
import { Select } from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { useCapabilities } from '@/admin/useAdmin';

export default function DocumentsWorkspace({ upload }: { readonly upload?: ReactNode }) {
  const { t } = useTranslation();
  const { identityId } = useCapabilities();
  const { pathname, search } = useLocation();
  const navigate = useNavigate();
  const query = useMemo(() => {
    const state = readDocumentQuery(search);
    return { ...state, filter: { ...state.filter, inbox: pathname === '/documents/inbox' } };
  }, [pathname, search]);
  const { filter, page: request, display } = query;
  const changeQuery = useCallback(
    (next: DocumentQuery) => {
      void navigate(`${pathname}?${writeDocumentQuery(next)}`);
    },
    [navigate, pathname],
  );
  const changeFilter = (next: DocumentsFilter) => {
    changeQuery({ ...query, filter: next, page: { ...request, offset: 0 } });
  };
  const [shownFields, setShownFields] = useState(readShownFields);
  const { lists, names, customFields } = useDocumentLists();
  const wide = useWideView();
  const page = useQuery({
    queryKey: ['documents', identityId, filter, request],
    queryFn: ({ signal }) => fetchDocuments(filter, request, signal),
    enabled: !!identityId,
  });
  const count = useQuery({
    queryKey: ['documents', identityId, 'suggestions', filter.fileType],
    queryFn: ({ signal }) =>
      fetchDocuments(
        { ...EMPTY_FILTER, fileType: filter.fileType, needsReview: true },
        { sort: '', offset: 0, limit: 1 },
        signal,
      ),
    enabled: !!identityId,
  });
  const rows = page.data?.rows ?? [];
  const total = page.data?.total ?? 0;
  const bulk = useBulkEdit({ rows, total, filter, facets: page.data?.facets ?? {} });
  const open = (id: string) => {
    void navigate(documentHref(id, pathname, query));
  };
  const sortKey = request.sort.replace(/^-/, '');
  const descending = request.sort.startsWith('-');
  const sort = (value: string) => {
    changeQuery({ ...query, page: { ...request, sort: value, offset: 0 } });
  };
  const controls = (
    <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
      <Button
        size="sm"
        variant={filter.needsReview ? 'default' : 'outline'}
        aria-pressed={filter.needsReview}
        onClick={() => {
          changeFilter({ ...filter, needsReview: !filter.needsReview });
        }}
      >
        {t('documents.workspace.suggestions')}{' '}
        <span className="ml-1">{count.data?.total ?? '—'}</span>
      </Button>
      <Select
        size="sm"
        className="min-h-11 w-auto md:min-h-8"
        aria-label={t('documents.workspace.sort')}
        value={sortKey}
        options={['indexed_at', 'document_date', 'file_name'].map((value) => ({
          value,
          label: t(`documents.workspace.${value}`),
        }))}
        onValueChange={(value) => {
          sort(`${descending ? '-' : ''}${value}`);
        }}
      />
      <Button
        size="sm"
        variant="ghost"
        aria-label={t('documents.workspace.direction')}
        onClick={() => {
          sort(`${descending ? '' : '-'}${sortKey}`);
        }}
      >
        {t(`documents.workspace.${descending ? 'descending' : 'ascending'}`)}
      </Button>
      <Select
        size="sm"
        className="min-h-11 w-auto md:min-h-8"
        aria-label={t('documents.workspace.pageSize')}
        value={String(request.limit)}
        options={[25, 50, 100].map((value) => ({ value: String(value), label: String(value) }))}
        onValueChange={(value) => {
          changeQuery({ ...query, page: { ...request, limit: Number(value), offset: 0 } });
        }}
      />
      <div
        className="ml-auto flex gap-1"
        role="group"
        aria-label={t('documents.workspace.display')}
      >
        {(['table', 'compact', 'cards'] as const).map((mode) => (
          <Button
            key={mode}
            size="sm"
            variant={display === mode ? 'secondary' : 'ghost'}
            aria-pressed={display === mode}
            onClick={() => {
              changeQuery({ ...query, display: mode });
            }}
          >
            {t(`documents.workspace.${mode}`)}
          </Button>
        ))}
      </div>
    </div>
  );

  return (
    <section
      aria-label={t('documents.title')}
      className="flex h-full min-h-0 min-w-0 flex-col bg-bg"
    >
      <header className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
        <div className="mr-auto">
          <h1 className="font-display text-xl font-semibold">
            {t(`documents.workspace.${filter.inbox ? 'inbox' : 'library'}`)}
          </h1>
          <span className="text-xs text-text-muted">{t('documents.total', { count: total })}</span>
        </div>
        {upload}
        <SaveViewButton identity={identityId} query={query} />
      </header>
      <div className="shrink-0">
        {bulk.active ? (
          <BulkBar {...bulk.bar} lists={lists} customFields={customFields} />
        ) : (
          <FilterBar
            compact={!wide}
            children={!wide ? controls : undefined}
            filter={filter}
            onChange={changeFilter}
            facets={page.data?.facets ?? {}}
            lists={lists}
            names={names}
            customFields={customFields}
            shownFields={shownFields}
            onShownFieldsChange={(ids) => {
              setShownFields(ids);
              saveShownFields(ids);
            }}
          />
        )}
        {wide && controls}
      </div>
      <BulkConfirmDialog {...bulk.confirm} names={names} customFields={customFields} />
      {page.isError ? (
        <div role="alert" className="p-5 text-danger">
          {t('documents.loadFailed')}{' '}
          <Button
            variant="outline"
            onClick={() => {
              void page.refetch();
            }}
          >
            {t('documents.workspace.retry')}
          </Button>
        </div>
      ) : page.isPending ? (
        <p role="status" className="p-5 text-text-muted">
          {t('documents.loading')}
        </p>
      ) : rows.length === 0 ? (
        <div className="grid flex-1 place-content-center gap-3 p-6 text-center">
          <h2 className="font-display text-xl">{t('documents.workspace.empty')}</h2>
          <p className="text-sm text-text-muted">{t('documents.workspace.emptyHint')}</p>
          <Button
            variant="outline"
            onClick={() => {
              changeFilter({ ...EMPTY_FILTER, inbox: filter.inbox });
            }}
          >
            {t('documents.workspace.clear')}
          </Button>
        </div>
      ) : display === 'table' && wide ? (
        <DocumentTable
          rows={rows}
          names={names}
          customFields={customFields}
          shownFields={shownFields}
          selection={bulk.selectionApi}
          sort={request.sort}
          onSort={sort}
          onOpen={open}
          onFilter={(field, id) => {
            changeFilter(withField(filter, 'include', field, id));
          }}
        />
      ) : (
        <DocumentCards
          rows={rows}
          names={names}
          cards={display === 'cards'}
          selection={bulk.selectionApi}
          onOpen={open}
        />
      )}
      {total > 0 && (
        <footer className="mt-auto flex shrink-0 items-center justify-between gap-2 border-t border-border px-3 py-2">
          <Button
            size="sm"
            variant="outline"
            disabled={request.offset === 0 || page.isPending}
            onClick={() => {
              changeQuery({
                ...query,
                page: { ...request, offset: Math.max(0, request.offset - request.limit) },
              });
            }}
          >
            {t('documents.workspace.previous')}
          </Button>
          <span className="text-xs text-text-muted">
            {t('documents.workspace.page', {
              from: Math.min(request.offset + 1, total),
              to: Math.min(request.offset + request.limit, total),
              total,
            })}
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={request.offset + request.limit >= total || page.isPending}
            onClick={() => {
              changeQuery({
                ...query,
                page: { ...request, offset: request.offset + request.limit },
              });
            }}
          >
            {t('documents.workspace.next')}
          </Button>
        </footer>
      )}
    </section>
  );
}
