import { Suspense, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, ArrowRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { DetailFields } from './DetailFields';
import { formDiff, formOf, type DetailForm } from './detailModel';
import { detailNames } from './detailNames';
import { DocumentHistory } from './DocumentHistory';
import {
  fetchDocument,
  fetchNextToReview,
  fetchReviewCount,
  isEmptyDiff,
  patchDocument,
  reviewDocument,
  type DocumentDetail as Detail,
  type FieldValue,
} from './documentsApi';
import { useDocumentLists, type DocumentLists } from './useDocumentLists';
import { useWrite } from './useWrite';
import { useCapabilities } from '@/admin/useAdmin';
import { previewKind } from '@/chat/artifacts/artifactMeta';
import { AssetSourceContext } from '@/chat/artifacts/renderers/assetSourceContext';
import { PreviewLoading } from '@/chat/artifacts/renderers/PreviewStatus';
import { PreviewActionsContext } from '@/chat/artifacts/renderers/previewActionsContext';
import { PreviewByKind } from '@/chat/artifacts/renderers/previewDispatch';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { directURL, FILE_MANAGER_SOURCE } from '@/files/filesApi';

export interface DocumentDetailProps {
  readonly id: string;
  /** Back to the list. */
  readonly onClose: () => void;
  /** Opens another document in place: the next one waiting for review. */
  readonly onOpen: (id: string) => void;
}

/**
 * One document's detail and review (spec decision 4, mockup 3): its preview, the form of its
 * fields with the suggestions under each, and its history. Save writes what changed; "Rivisto,
 * salva e avanti" also marks it reviewed and opens the next document waiting for review.
 */
export function DocumentDetail({ id, onClose, onOpen }: DocumentDetailProps) {
  const { t } = useTranslation();
  const { identityId } = useCapabilities();
  const detail = useQuery({
    queryKey: ['document', identityId, id],
    enabled: !!identityId,
    queryFn: ({ signal }) => fetchDocument(id, signal),
  });
  // The page waits for the lists too, so no label shows as unknown while they load.
  const { settled, ...lists } = useDocumentLists();
  if (detail.data !== undefined && settled) {
    return (
      <DetailPage key={id} doc={detail.data} lists={lists} onClose={onClose} onOpen={onOpen} />
    );
  }
  return (
    <div className="grid flex-1 place-content-center justify-items-center gap-3 p-6 text-sm">
      {detail.isError ? (
        <>
          <p role="alert" className="text-danger">
            {t('documents.detail.loadFailed')}
          </p>
          <Button type="button" variant="outline" onClick={onClose}>
            {t('documents.detail.back')}
          </Button>
        </>
      ) : (
        <p role="status" className="text-text-muted">
          {t('documents.detail.loading')}
        </p>
      )}
    </div>
  );
}

interface DetailPageProps {
  readonly doc: Detail;
  readonly lists: DocumentLists;
  readonly onClose: () => void;
  readonly onOpen: (id: string) => void;
}

function DetailPage({ doc, lists: operatorLists, onClose, onOpen }: DetailPageProps) {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const [previewActions, setPreviewActions] = useState<HTMLElement | null>(null);
  const { lists, names, customFields } = operatorLists;
  const reviewCount = useQuery({
    queryKey: ['documents-review'],
    queryFn: ({ signal }) => fetchReviewCount(signal),
  });
  // The operator's edits, over the document as last read; null is the document unchanged.
  const [edits, setEdits] = useState<DetailForm | null>(null);
  const { busy, error, run } = useWrite('documents.detail.failed');
  const [asking, setAsking] = useState(false);

  const form = edits ?? formOf(doc);
  const diff = formDiff(doc, form);
  const dirty = !isEmptyDiff(diff);
  const shown = detailNames(names, customFields, t, i18n.language);

  const refresh = () =>
    Promise.all(
      ['document', 'documents', 'documents-review', 'documents-labels'].map((key) =>
        queryClient.invalidateQueries({ queryKey: [key] }),
      ),
    );
  const save = async () => {
    if (dirty) await patchDocument(doc.id, diff);
  };
  const onSave = () => {
    run(async () => {
      await save();
      await refresh();
      setEdits(null);
    });
  };
  const onReviewNext = () => {
    run(async () => {
      await save();
      await reviewDocument(doc.id);
      const next = await fetchNextToReview(doc.id);
      void refresh();
      if (next === null) onClose();
      else onOpen(next);
    });
  };
  const onReject = (value: FieldValue) => {
    run(async () => {
      await patchDocument(doc.id, {
        set: [],
        remove: [{ field: value.field, value_key: value.value_key }],
      });
      await refresh();
    });
  };
  const back = () => {
    if (dirty) setAsking(true);
    else onClose();
  };

  const review = reviewCount.data ?? 0;
  return (
    <section aria-label={doc.file_name} className="flex h-full min-h-0 flex-col bg-bg">
      <header className="flex flex-wrap items-end gap-x-6 gap-y-3 border-b border-border px-4 py-3">
        <div className="flex min-w-0 flex-1 basis-64 flex-col gap-1">
          <div className="flex items-center gap-2 text-[13px]">
            <button
              type="button"
              onClick={back}
              className="inline-flex items-center gap-1 text-info underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-ring"
            >
              <ArrowLeft aria-hidden="true" className="size-3.5" />
              {t('documents.detail.back')}
            </button>
            {review > 0 && (
              <span className="text-text-muted">
                · {t('documents.detail.toReview', { count: review })}
              </span>
            )}
          </div>
          <h1 className="truncate font-display text-xl font-semibold text-text">{doc.file_name}</h1>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="ghost"
            disabled={!dirty || busy}
            onClick={() => {
              setEdits(null);
            }}
          >
            {t('documents.detail.undo')}
          </Button>
          <Button type="button" variant="outline" disabled={!dirty || busy} onClick={onSave}>
            {t('documents.detail.save')}
          </Button>
          <Button type="button" disabled={busy} onClick={onReviewNext} className="gap-2">
            {t('documents.detail.reviewNext')}
            <ArrowRight aria-hidden="true" className="size-4" />
          </Button>
        </div>
      </header>
      {error !== '' && (
        <p role="alert" className="border-b border-border px-4 py-2 text-sm text-danger">
          {error}
        </p>
      )}
      <div className="grid min-h-0 flex-1 overflow-y-auto lg:grid-cols-[minmax(0,1fr)_minmax(340px,440px)] lg:overflow-hidden">
        <div className="flex h-[45vh] min-h-0 flex-col border-b border-border lg:h-auto lg:border-r lg:border-b-0">
          <div className="flex min-h-12 items-center justify-between gap-3 px-4 pt-3">
            <h2 className="text-xs font-semibold tracking-[0.08em] text-text-muted uppercase">
              {t('documents.detail.preview')}
            </h2>
            <span ref={setPreviewActions} className="flex items-center gap-3" />
          </div>
          <div className="min-h-0 flex-1 p-3">
            <AssetSourceContext.Provider value={FILE_MANAGER_SOURCE}>
              <PreviewActionsContext.Provider value={previewActions}>
                <Suspense fallback={<PreviewLoading />}>
                  <PreviewByKind
                    kind={previewKind('', doc.file_name)}
                    asset={{ assetId: `/${doc.source_key}`, mimeType: '', fileName: doc.file_name }}
                    downloadFallback={
                      <a
                        href={directURL(`/${doc.source_key}`, true)}
                        className="grid h-full place-items-center text-sm text-info underline"
                      >
                        {t('artifacts.preview.downloadFallback')}
                      </a>
                    }
                  />
                </Suspense>
              </PreviewActionsContext.Provider>
            </AssetSourceContext.Provider>
          </div>
        </div>
        <div className="flex flex-col gap-6 p-4 lg:overflow-y-auto">
          <DetailFields
            doc={doc}
            form={form}
            onChange={setEdits}
            onReject={onReject}
            lists={lists}
            customFields={customFields}
            names={shown}
            busy={busy}
          />
          <DocumentHistory doc={doc} names={shown} />
        </div>
      </div>
      <ConfirmDialog
        open={asking}
        onOpenChange={setAsking}
        title={t('documents.detail.unsavedTitle')}
        description={t('documents.detail.unsavedBody')}
        cancelLabel={t('documents.detail.stay')}
        confirmLabel={t('documents.detail.discard')}
        onConfirm={onClose}
      />
    </section>
  );
}
