import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useBeforeUnload, useBlocker, useSearchParams } from 'react-router';
import { ArrowLeft, ArrowRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { DetailFields } from './DetailFields';
import { formDiff, formWithDiff } from './detailModel';
import { detailNames } from './detailNames';
import { DocumentHistory } from './DocumentHistory';
import { DocumentContent } from './DocumentContent';
import { DocumentPreview } from './DocumentPreview';
import {
  fetchDocument,
  isEmptyDiff,
  patchDocument,
  reviewDocument,
  errorMessage,
  type DocumentDetail as Detail,
  type FieldValue,
  type FieldsDiff,
} from './documentsApi';
import { useDocumentLists } from './useDocumentLists';
import { fetchNeighbors, type DocumentNeighbor } from './documentNeighbors';
import { readDocumentQuery } from './documentRoute';
import { useWideView } from './useWideView';
import { useCapabilities } from '@/admin/useAdmin';
import { PreviewCloseGuardContext } from '@/chat/artifacts/renderers/previewCloseGuardContext';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from '@/components/ui/resizable';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

export interface DocumentDetailProps {
  readonly id: string;
  readonly onClose: () => void;
  readonly onOpen: (id: string, offset?: number) => void;
}
const EMPTY_DIFF: FieldsDiff = { set: [], remove: [] };

export function DocumentDetail({ id, ...navigation }: DocumentDetailProps) {
  const { t } = useTranslation();
  const { identityId } = useCapabilities();
  const detail = useQuery({
    queryKey: ['document', identityId, id],
    enabled: !!identityId,
    queryFn: ({ signal }) => fetchDocument(id, signal),
  });
  if (detail.data)
    return (
      <DetailPage
        key={`${identityId}:${id}`}
        doc={detail.data}
        identity={identityId}
        {...navigation}
      />
    );
  return (
    <div className="grid flex-1 place-content-center justify-items-center gap-3 p-6 text-sm">
      <p role={detail.isError ? 'alert' : 'status'}>
        {t(detail.isError ? 'documents.detail.loadFailed' : 'documents.detail.loading')}
      </p>
      {detail.isError && (
        <Button variant="outline" onClick={navigation.onClose}>
          {t('documents.detail.back')}
        </Button>
      )}
    </div>
  );
}

function DetailPage({
  doc,
  identity,
  onClose,
  onOpen,
}: Omit<DocumentDetailProps, 'id'> & { readonly doc: Detail; readonly identity: string }) {
  const { t, i18n } = useTranslation();
  const client = useQueryClient();
  const lists = useDocumentLists();
  const [params] = useSearchParams();
  const search = params.toString();
  const neighbors = useQuery({
    queryKey: ['documents', identity, 'neighbors', doc.id, search],
    queryFn: ({ signal }) => fetchNeighbors(doc.id, readDocumentQuery(search), signal),
  });
  const [diff, setDiff] = useState<FieldsDiff>(EMPTY_DIFF);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [fileDirty, setFileDirty] = useState(false);
  const [tab, setTab] = useState('details');
  const wide = useWideView('(max-width: 1023px)');
  const [pending, setPending] = useState<(() => void) | null>(null);
  const allowNavigation = useRef(false);
  const inFlight = useRef(false);
  const lifetime = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => {
      controller.abort();
    };
  }, []);
  const dirty = !isEmptyDiff(diff);
  const blocker = useBlocker(() => !allowNavigation.current && (dirty || fileDirty || busy));
  useBeforeUnload((event) => {
    if (dirty || fileDirty || busy) event.preventDefault();
  });
  const shown = detailNames(lists.names, lists.customFields, t, i18n.language);
  const form = formWithDiff(doc, diff);
  const asking = pending !== null || blocker.state === 'blocked';

  const refresh = () =>
    Promise.all([
      client.invalidateQueries({ queryKey: ['document', identity, doc.id] }),
      client.invalidateQueries({ queryKey: ['documents', identity] }),
      client.invalidateQueries({ queryKey: ['documents-content', identity, doc.id] }),
      client.invalidateQueries({
        predicate: (query) =>
          query.queryKey[0] === 'documents-labels' && query.queryKey[2] === identity,
      }),
    ]);
  const run = (work: (signal: AbortSignal) => Promise<void>) => {
    const signal = lifetime.current?.signal;
    if (!signal || signal.aborted || inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError('');
    void work(signal)
      .catch((err: unknown) => {
        if (!signal.aborted) setError(t('documents.detail.failed', { message: errorMessage(err) }));
      })
      .finally(() => {
        inFlight.current = false;
        if (!signal.aborted) setBusy(false);
      });
  };
  const go = (neighbor?: DocumentNeighbor) => {
    if (neighbor) onOpen(neighbor.id, neighbor.offset);
    else onClose();
  };
  const guarded = (action: () => void) => {
    if (busy) return;
    if (dirty || fileDirty)
      setPending(() => () => {
        allowNavigation.current = true;
        try {
          action();
        } finally {
          allowNavigation.current = false;
        }
      });
    else action();
  };
  const save = (next: boolean) => {
    const work = () => {
      run(async (signal) => {
        const active = () => !signal.aborted;
        if (dirty) await patchDocument(doc.id, diff, signal);
        if (!active()) return;
        if (next) await reviewDocument(doc.id, signal);
        if (!active()) return;
        setDiff(EMPTY_DIFF);
        if (next) {
          allowNavigation.current = true;
          try {
            go(neighbors.data?.next);
          } finally {
            allowNavigation.current = false;
          }
          void refresh();
        } else await refresh();
      });
    };
    // Ask before any metadata/review request: cancel must leave both drafts untouched.
    if (next && fileDirty) setPending(() => work);
    else work();
  };
  const reject = (value: FieldValue) => {
    run(async (signal) => {
      await patchDocument(
        doc.id,
        { set: [], remove: [{ field: value.field, value_key: value.value_key }] },
        signal,
      );
      if (!signal.aborted) await refresh();
    });
  };
  const cancel = () => {
    setPending(null);
    if (blocker.state === 'blocked') blocker.reset();
  };
  const discard = () => {
    setPending(null);
    if (blocker.state === 'blocked') blocker.proceed();
    else pending?.();
  };

  return (
    <section aria-label={doc.file_name} className="flex h-full min-h-0 flex-col bg-bg">
      <header className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border px-4 py-3">
        <div className="min-w-0 flex-1 basis-60">
          <button
            type="button"
            onClick={() => {
              guarded(onClose);
            }}
            className="inline-flex min-h-[44px] items-center gap-1 text-sm text-info"
          >
            <ArrowLeft aria-hidden className="size-4" />
            {t('documents.detail.back')}
          </button>
          <h1 title={doc.file_name} className="truncate font-display text-xl font-semibold">
            {doc.file_name}
          </h1>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            aria-label={t('documents.detail.previous')}
            disabled={busy || !neighbors.data?.previous}
            onClick={() => {
              guarded(() => {
                go(neighbors.data?.previous);
              });
            }}
          >
            <ArrowLeft aria-hidden className="size-4" />
          </Button>
          <Button
            variant="outline"
            aria-label={t('documents.detail.next')}
            disabled={busy || !neighbors.data?.next}
            onClick={() => {
              guarded(() => {
                go(neighbors.data?.next);
              });
            }}
          >
            <ArrowRight aria-hidden className="size-4" />
          </Button>
          <Button
            variant="ghost"
            disabled={!dirty || busy}
            onClick={() => {
              setDiff(EMPTY_DIFF);
            }}
          >
            {t('documents.detail.undo')}
          </Button>
          <Button
            variant="outline"
            disabled={!dirty || busy || lists.failed}
            onClick={() => {
              save(false);
            }}
          >
            {t('documents.detail.save')}
          </Button>
          <Button
            disabled={busy || neighbors.isPending || neighbors.isError || lists.failed}
            onClick={() => {
              save(true);
            }}
          >
            {t('documents.detail.reviewNext')}
          </Button>
        </div>
      </header>
      {error && (
        <p role="alert" className="px-4 py-2 text-sm text-danger">
          {error}
        </p>
      )}
      {neighbors.isError && (
        <p role="alert" className="px-4 py-2 text-sm">
          {t('documents.detail.navigationFailed')}{' '}
          <Button
            variant="outline"
            onClick={() => {
              void neighbors.refetch();
            }}
          >
            {t('documents.workspace.retry')}
          </Button>
        </p>
      )}
      {lists.failed && (
        <p role="alert" className="px-4 py-2 text-sm">
          {t('documents.detail.listsFailed')}{' '}
          <Button variant="outline" onClick={lists.retry}>
            {t('documents.detail.retryLists')}
          </Button>
        </p>
      )}
      <PreviewCloseGuardContext.Provider value={{ setUnsaved: setFileDirty, held: false }}>
        <Tabs
          value={wide && tab === 'preview' ? 'details' : tab}
          onValueChange={setTab}
          className="min-h-0 flex-1 gap-0"
        >
          <TabsList className="mx-4 my-2 max-w-full shrink-0">
            <TabsTrigger value="details">{t('documents.detail.details')}</TabsTrigger>
            <TabsTrigger value="content">{t('documents.detail.content')}</TabsTrigger>
            <TabsTrigger value="history">{t('documents.detail.history')}</TabsTrigger>
            <TabsTrigger value="preview" className="lg:hidden">
              {t('documents.detail.preview')}
            </TabsTrigger>
          </TabsList>
          <ResizablePanelGroup orientation="horizontal" className="min-h-0 flex-1">
            <ResizablePanel
              defaultSize="35%"
              minSize={wide ? '280px' : tab === 'preview' ? '0%' : '100%'}
              maxSize={wide || tab !== 'preview' ? '100%' : '0%'}
            >
              <div
                className="h-full overflow-y-auto p-4"
                inert={!wide && tab === 'preview'}
                aria-hidden={!wide && tab === 'preview'}
              >
                <TabsContent value="content">
                  <DocumentContent id={doc.id} identity={identity} />
                </TabsContent>
                <TabsContent value="history">
                  <DocumentHistory doc={doc} names={shown} />
                </TabsContent>
                <TabsContent value="details">
                  <p className="mb-4 break-all text-xs text-text-muted">
                    {t('documents.detail.storedFilename')}: {doc.file_name}
                  </p>
                  {lists.settled ? (
                    <DetailFields
                      doc={doc}
                      form={form}
                      onChange={(next) => {
                        setDiff(formDiff(doc, next));
                      }}
                      onReject={reject}
                      lists={lists.lists}
                      customFields={lists.customFields}
                      names={shown}
                      busy={busy || lists.failed}
                    />
                  ) : (
                    <p role="status">{t('documents.detail.loading')}</p>
                  )}
                </TabsContent>
              </div>
            </ResizablePanel>
            <ResizableHandle withHandle className="hidden lg:flex" />
            <ResizablePanel
              defaultSize="65%"
              minSize={wide ? '320px' : tab === 'preview' ? '100%' : '0%'}
              maxSize={wide || tab === 'preview' ? '100%' : '0%'}
            >
              <TabsContent
                value="preview"
                forceMount
                hidden={!wide && tab !== 'preview'}
                className="h-full"
                inert={!wide && tab !== 'preview'}
                aria-hidden={!wide && tab !== 'preview'}
              >
                <DocumentPreview doc={doc} />
              </TabsContent>
            </ResizablePanel>
          </ResizablePanelGroup>
        </Tabs>
      </PreviewCloseGuardContext.Provider>
      <ConfirmDialog
        open={asking}
        onOpenChange={(open) => {
          if (!open) cancel();
        }}
        title={t('documents.detail.unsavedTitle')}
        description={t('documents.detail.unsavedBody')}
        cancelLabel={t('documents.detail.stay')}
        confirmLabel={t('documents.detail.discard')}
        onConfirm={discard}
      />
    </section>
  );
}
