import { useId, useState } from 'react';
import { useQueries, useQuery } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CustomFieldsAdmin } from './CustomFieldsAdmin';
import { EMPTY_FILTER, fetchDocuments, type FilterField, type LabelKind } from './documentsApi';
import { KIND_OF } from './documentsModel';
import { LabelsAdmin } from './LabelsAdmin';
import { useDocumentLists } from './useDocumentLists';
import { useCapabilities } from '@/admin/useAdmin';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';

/** The list pages in tab order, each with the filter field its values filter on. */
const LISTS: readonly { readonly kind: LabelKind; readonly field: FilterField }[] = (
  ['type', 'counterparty', 'tags'] as const
).map((field) => ({ kind: KIND_OF[field], field }));

const FIELDS_TAB = 'fields';

interface ListsPageProps {
  readonly onClose: () => void;
  /** Back to the list, filtered on one value. */
  readonly onFilter: (field: FilterField, id: string) => void;
}

/**
 * The operator's lists (spec decision 4): one page each for types, counterparties, tags and
 * custom fields. A value's count is how many documents of the whole library hold it, read from
 * the facets of the unfiltered list.
 */
export function ListsPage({ onClose, onFilter }: ListsPageProps) {
  const { t } = useTranslation();
  const { identityId } = useCapabilities();
  const headingId = useId();
  const { lists, customFields, failed, retry } = useDocumentLists();
  const [tab, setTab] = useState<string>('type');
  const library = useQuery({
    queryKey: ['documents', identityId, 'organization-counts'],
    enabled: !!identityId,
    queryFn: ({ signal }) =>
      fetchDocuments(
        { ...EMPTY_FILTER, fileType: 'all' },
        { sort: '', offset: 0, limit: 1 },
        signal,
      ),
  });
  const fieldCounts = useQueries({
    queries: customFields.map((field) => ({
      queryKey: ['documents', identityId, 'field-count', field.id],
      enabled: !!identityId && tab === FIELDS_TAB,
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        fetchDocuments(
          {
            ...EMPTY_FILTER,
            fileType: 'all',
            custom: [{ field: `custom:${field.id}`, operator: 'unassigned' }],
          },
          { sort: '', offset: 0, limit: 1 },
          signal,
        ),
    })),
  });
  const counts: Record<string, number> = {};
  customFields.forEach((field, index) => {
    const unassigned = fieldCounts[index]?.data?.total;
    if (unassigned !== undefined && library.data)
      counts[field.id] = Math.max(0, library.data.total - unassigned);
  });

  return (
    <section aria-labelledby={headingId} className="flex h-full min-h-0 flex-col bg-bg">
      <header className="flex flex-col gap-1 border-b border-border px-4 py-3">
        <button
          type="button"
          onClick={onClose}
          className="inline-flex w-fit items-center gap-1 text-[13px] text-info underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring"
        >
          <ArrowLeft aria-hidden="true" className="size-3.5" />
          {t('documents.lists.back')}
        </button>
        <h1 id={headingId} className="font-display text-xl font-semibold text-text">
          {t('documents.lists.title')}
        </h1>
      </header>
      {failed && (
        <p role="alert" className="p-4">
          {t('documents.detail.listsFailed')}{' '}
          <Button onClick={retry}>{t('documents.detail.retryLists')}</Button>
        </p>
      )}
      {(library.isError || fieldCounts.some((query) => query.isError)) && (
        <p role="alert" className="p-4">
          {t('documents.lists.countsFailed')}{' '}
          <Button
            onClick={() => {
              void library.refetch();
              for (const query of fieldCounts) if (query.isError) void query.refetch();
            }}
          >
            {t('documents.workspace.retry')}
          </Button>
        </p>
      )}
      <Tabs value={tab} onValueChange={setTab} className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        <TabsList className="max-w-full flex-wrap justify-start">
          {LISTS.map(({ kind }) => (
            <TabsTrigger key={kind} value={kind} className="flex-none">
              {t(`documents.lists.tabs.${kind}`)}
            </TabsTrigger>
          ))}
          <TabsTrigger value={FIELDS_TAB} className="flex-none">
            {t('documents.lists.tabs.fields')}
          </TabsTrigger>
        </TabsList>
        {LISTS.map(({ kind, field }) => (
          <TabsContent key={kind} value={kind} className="max-w-3xl">
            <LabelsAdmin
              kind={kind}
              labels={lists[field]}
              counts={library.data === undefined ? undefined : (library.data.facets[field] ?? {})}
              onFilter={(id) => {
                onFilter(field, id);
              }}
            />
          </TabsContent>
        ))}
        <TabsContent value={FIELDS_TAB} className="max-w-3xl">
          <CustomFieldsAdmin fields={customFields} counts={counts} />
        </TabsContent>
      </Tabs>
    </section>
  );
}
