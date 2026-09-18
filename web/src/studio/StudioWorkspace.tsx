import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { StudioBar } from './StudioBar';
import { StudioHistory } from './StudioHistory';
import { StudioStage } from './StudioStage';
import { initialModel, rememberModel } from './modelChoice';
import {
  StudioError,
  type StudioImageRef,
  type StudioKind,
  type StudioModel,
  type StudioRecord,
} from './studioApi';
import { studioErrorSentence } from './studioErrors';
import {
  draftFromRecord,
  reconcileDraft,
  requestBody,
  type StudioDraft,
  type StudioOptions,
} from './studioForm';
import {
  useCreateStudioImage,
  useCreateStudioVideo,
  useStudioHistory,
  useStudioLibrary,
  useStudioModels,
} from './useStudio';

// StudioWorkspace — one composer, one stage, one history, for the signed-in identity.
//
// The draft is DERIVED, not stored: the page holds what the operator typed and chose, and
// reconciles it against the current model on every render. That is what makes a mode switch
// keep the prompt and drop nothing else quietly — there is no second copy of the draft that
// can disagree with the catalog, and no effect racing the catalog's answer to write one.

/** The draft's starting point: nothing chosen. `reconcileDraft` fills every axis with the
 *  cheapest the model declares, so there is no second table of defaults here to drift. */
const NO_OPTIONS: StudioOptions = {
  resolution: '',
  duration: undefined,
  aspectRatio: '',
  audio: false,
  seed: undefined,
};

export default function StudioWorkspace() {
  const { t } = useTranslation();
  const [kind, setKind] = useState<StudioKind>('video');
  const [prompt, setPrompt] = useState('');
  const [options, setOptions] = useState<StudioOptions>(NO_OPTIONS);
  const [images, setImages] = useState<readonly StudioImageRef[]>([]);
  const [endFrame, setEndFrame] = useState<StudioImageRef>();
  // The in-session choice per kind. localStorage is the durable half and can throw, so a
  // browser that refuses it still keeps the model the operator just picked.
  const [picked, setPicked] = useState<Partial<Record<StudioKind, string>>>({});
  const [selectedId, setSelectedId] = useState<string>();
  const [failure, setFailure] = useState<string>();

  const models = useStudioModels(kind);
  const history = useStudioHistory(undefined);
  // Read at page level, not only while a picker is open: Reuse has to turn a record's asset
  // ids back into images the bar can show, and it cannot wait for a fetch mid-click.
  const library = useStudioLibrary(true);
  const createVideo = useCreateStudioVideo();
  const createImage = useCreateStudioImage();

  // Memoized because it is a dependency below: `?? []` would otherwise mint a new empty array
  // on every render and re-read localStorage with it.
  const listed = useMemo(() => models.data?.models ?? [], [models.data]);
  const catalogDefault = models.data?.default ?? '';
  const remembered = useMemo(
    () => initialModel(kind, listed, catalogDefault),
    [kind, listed, catalogDefault],
  );
  const inSession = picked[kind];
  const modelId =
    inSession !== undefined && listed.some((row) => row.id === inSession) ? inSession : remembered;
  const model: StudioModel | undefined = listed.find((row) => row.id === modelId);

  const draft: StudioDraft | undefined =
    model === undefined
      ? undefined
      : reconcileDraft({ kind, model: model.id, prompt, options, images, endFrame }, model);

  const records = useMemo(
    () => (history.data?.pages ?? []).flat() as readonly StudioRecord[],
    [history.data],
  );
  const shown = records.find((record) => record.id === selectedId) ?? records[0];
  const submitting = createVideo.isPending || createImage.isPending;

  /** Write back the axes the operator owns. The bar never changes the mode or the model
   *  through this path — each has its own callback, because each needs a reconcile. */
  function applyDraft(next: StudioDraft) {
    setPrompt(next.prompt);
    setOptions(next.options);
    setImages(next.images);
    setEndFrame(next.endFrame);
  }

  /** Re-aim the stored draft at a model, so what is kept is what that model declares rather
   *  than a value the pills are only hiding. */
  function reaim(at: StudioModel, nextKind: StudioKind) {
    applyDraft(
      reconcileDraft({ kind: nextKind, model: at.id, prompt, options, images, endFrame }, at),
    );
  }

  function chooseModel(nextId: string) {
    const chosen = listed.find((row) => row.id === nextId);
    if (chosen === undefined) return;
    rememberModel(kind, nextId);
    setPicked({ ...picked, [kind]: nextId });
    reaim(chosen, kind);
  }

  function submit() {
    if (draft === undefined || model === undefined) return;
    setFailure(undefined);
    const handlers = {
      onSuccess: (record: StudioRecord) => {
        setSelectedId(record.id);
      },
      onError: (error: unknown) => {
        setFailure(
          error instanceof StudioError
            ? studioErrorSentence(t, error.code, error.message)
            : t('studio.error.generic'),
        );
      },
    };
    // `requestBody` shapes the body from the same `draft.kind` this branches on, so the two
    // cannot disagree; the body types overlap structurally, which is why neither needs a cast.
    const body = requestBody(draft, model);
    if (draft.kind === 'image') createImage.mutate(body, handlers);
    else createVideo.mutate(body, handlers);
  }

  function reuse(record: StudioRecord) {
    const reused = draftFromRecord(record, library.data ?? []);
    setKind(record.kind);
    setPicked({ ...picked, [record.kind]: record.model });
    applyDraft(reused);
  }

  return (
    <section
      aria-label={t('studio.title')}
      className="relative flex h-full min-h-0 overflow-hidden bg-bg"
    >
      <div className="flex min-w-0 flex-1 flex-col items-center gap-3 overflow-y-auto px-4 py-4">
        <div className="flex min-h-0 w-full flex-1 items-center justify-center py-4">
          <StudioStage record={shown} onReuse={reuse} />
        </div>

        {failure === undefined ? null : (
          <p
            role="alert"
            className="w-full max-w-4xl rounded-[var(--radius-md)] border border-danger/40 bg-surface px-3 py-2 text-xs text-danger"
          >
            {failure}
          </p>
        )}

        <CatalogState
          error={models.error}
          pending={models.isPending}
          empty={models.isSuccess && listed.length === 0}
        />

        {draft === undefined || model === undefined ? null : (
          <StudioBar
            draft={draft}
            model={model}
            models={listed}
            submitting={submitting}
            onDraftChange={applyDraft}
            onKindChange={setKind}
            onModelChange={chooseModel}
            onSubmit={submit}
          />
        )}
      </div>

      <StudioHistory
        records={records}
        selectedId={shown?.id}
        hasMore={history.hasNextPage}
        loadingMore={history.isFetchingNextPage}
        onSelect={(record) => {
          setSelectedId(record.id);
        }}
        onLoadMore={() => {
          void history.fetchNextPage();
        }}
      />
    </section>
  );
}

/** Why there is no composer, when there is none. A deployment routed away from OpenRouter,
 *  missing its key, or whose catalog lists nothing for this kind cannot generate at all, and
 *  saying which is more useful than a page with a headline and no bar under it. */
function CatalogState({
  error,
  pending,
  empty,
}: {
  readonly error: unknown;
  readonly pending: boolean;
  readonly empty: boolean;
}) {
  const { t } = useTranslation();
  if (error instanceof StudioError) {
    return (
      <p role="alert" className="max-w-md text-center text-sm text-text-muted">
        {studioErrorSentence(t, error.code, error.message)}
      </p>
    );
  }
  if (error !== null && error !== undefined) {
    return (
      <p role="alert" className="max-w-md text-center text-sm text-text-muted">
        {t('studio.stage.unavailable')}
      </p>
    );
  }
  if (empty) {
    return (
      <p role="status" className="max-w-md text-center text-sm text-text-muted">
        {t('studio.model.none')}
      </p>
    );
  }
  return pending ? (
    <p role="status" className="text-sm text-text-muted">
      {t('studio.loading')}
    </p>
  ) : null;
}
