import { useMemo, useRef, useState } from 'react';
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
// The draft is DERIVED, not stored: the page holds what the operator chose PER KIND plus one
// shared prompt, and reconciles the pair against the current model on every render. Per kind
// is what makes the state order-independent — writing back the image draft cannot reach the
// video one, so editing a prompt after a mode switch no longer quietly discards the options
// the other mode is still holding. The prompt is shared because A1 says a mode switch keeps
// it and reloads the rest from that mode's model.

/** One kind's half of the composer. `model: undefined` means "whatever the catalog and the
 *  remembered choice say"; a concrete id is a pick made in this session. */
interface KindDraft {
  readonly model: string | undefined;
  readonly options: StudioOptions;
  readonly images: readonly StudioImageRef[];
  readonly endFrame: StudioImageRef | undefined;
}

/** Nothing chosen. `reconcileDraft` fills every axis with the cheapest the model declares, so
 *  there is no second table of defaults here to drift. */
const BLANK: KindDraft = {
  model: undefined,
  options: { resolution: '', duration: undefined, aspectRatio: '', audio: false, seed: undefined },
  images: [],
  endFrame: undefined,
};

export default function StudioWorkspace() {
  const { t } = useTranslation();
  const [kind, setKind] = useState<StudioKind>('video');
  const [prompt, setPrompt] = useState('');
  // Both halves are kept: the in-session model too, because localStorage is only the durable
  // half of that choice and a browser that refuses it must still honour the pick just made.
  const [byKind, setByKind] = useState<Record<StudioKind, KindDraft>>({
    image: BLANK,
    video: BLANK,
  });
  const [selectedId, setSelectedId] = useState<string>();
  const [failure, setFailure] = useState<string>();
  const inFlight = useRef(false);

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
  const current = byKind[kind];
  const inSession = current.model;
  const modelId =
    inSession !== undefined && listed.some((row) => row.id === inSession) ? inSession : remembered;
  const model: StudioModel | undefined = listed.find((row) => row.id === modelId);

  const draft: StudioDraft | undefined =
    model === undefined
      ? undefined
      : reconcileDraft({ ...current, kind, model: model.id, prompt }, model);

  const records = useMemo(
    () => (history.data?.pages ?? []).flat() as readonly StudioRecord[],
    [history.data],
  );
  const shown = records.find((record) => record.id === selectedId) ?? records[0];
  const submitting = createVideo.isPending || createImage.isPending;

  /** Write one kind's half back. The bar never changes the mode or the model through this
   *  path — each has its own callback, because each needs a reconcile — and writing only
   *  `into` is what keeps a prompt edit in one mode out of the other mode's options. */
  function keep(into: StudioKind, next: StudioDraft, modelId?: string) {
    setPrompt(next.prompt);
    setByKind((held) => ({
      ...held,
      [into]: {
        model: modelId ?? held[into].model,
        options: next.options,
        images: next.images,
        endFrame: next.endFrame,
      },
    }));
  }

  function applyDraft(next: StudioDraft) {
    keep(kind, next);
  }

  function chooseModel(nextId: string) {
    const chosen = listed.find((row) => row.id === nextId);
    if (chosen === undefined) return;
    rememberModel(kind, nextId);
    // Re-aimed on the way in, so what is stored is what this model declares rather than a
    // value the pills are only hiding.
    keep(kind, reconcileDraft({ ...current, kind, model: nextId, prompt }, chosen), nextId);
  }

  function submit() {
    // A ref, not `submitting`: that prop is a render behind, so two invocations inside one
    // frame — the shortcut fired twice, a click racing the keystroke — would both pass a
    // check on it. Every headerless POST gets a fresh Idempotency-Key from the fetch wrapper
    // (api/idempotency.ts), so the server would not coalesce them: that is two paid
    // generations, and the only thing that can stop them is synchronous.
    if (inFlight.current) return;
    if (draft === undefined || model === undefined) return;
    inFlight.current = true;
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
      onSettled: () => {
        inFlight.current = false;
      },
    };
    // The request carries the route it belongs to, so the body and the mutation cannot be
    // paired wrongly: the compiler rejects it rather than the server.
    const request = requestBody(draft, model);
    if (request.kind === 'image') createImage.mutate(request.body, handlers);
    else createVideo.mutate(request.body, handlers);
  }

  function reuse(record: StudioRecord) {
    const reused = draftFromRecord(record, library.data ?? []);
    setKind(record.kind);
    keep(record.kind, reused, record.model);
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
