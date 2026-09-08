import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Send, Square } from 'lucide-react';
import { steerRun, SteerRefusal } from '../steerRun';
import { cancelRun } from '../sseResume';
import { workerControlHistory, workerControlKey } from './workerControlApi';
import type { WorkerStatus } from './workerStream';
import { Button } from '@/components/ui/button';

interface Attempt {
  readonly id: string;
  readonly runId: string;
  readonly kind: 'steer' | 'cancel';
  readonly phase: 'sending' | 'accepted' | 'failed';
  readonly errorKey?: string;
  readonly retry?: () => void;
}

export function WorkerControls({
  conversationId,
  childId,
  status,
}: {
  readonly conversationId: string;
  readonly childId: string;
  readonly status: WorkerStatus;
}) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [draft, setDraft] = useState({ value: '', revision: 0 });
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const active = useRef<AbortController | null>(null);
  const runId = status.run_id;
  const canSteer = status.can_steer === true && Boolean(runId);
  const historyKey = workerControlKey(conversationId, childId);
  const history = useQuery({
    queryKey: historyKey,
    queryFn: ({ signal }) => workerControlHistory(conversationId, childId, signal),
    retry: false,
    refetchInterval: (query) =>
      status.status === 'queued' ||
      query.state.data?.cancel_requested === true ||
      query.state.data?.receipts.some((r) => r.status === 'accepted') === true
        ? 1000
        : false,
  });
  const queuedTarget =
    runId === undefined && status.status === 'queued' ? history.data?.queued_target : undefined;
  const targetKey =
    runId ??
    (queuedTarget === undefined
      ? undefined
      : `${queuedTarget.job_id}:${String(queuedTarget.attempt_count)}`);
  const cancelRequested = history.data?.cancel_requested === true;
  const canCancel =
    !cancelRequested &&
    ((status.can_cancel === true && Boolean(runId)) || queuedTarget !== undefined);
  const visibleAttempt = attempt?.runId === targetKey ? attempt : null;
  const busy = visibleAttempt?.phase === 'sending';

  useEffect(
    () => () => {
      active.current?.abort();
    },
    [conversationId, childId, targetKey],
  );

  const perform = (kind: 'steer' | 'cancel') => {
    if (
      targetKey === undefined ||
      busy ||
      (kind === 'steer' ? !canSteer || draft.value.trim() === '' : !canCancel)
    )
      return;
    const id = crypto.randomUUID();
    const snapshot = draft;
    const target = { conversationId, childId };
    const send =
      kind === 'steer'
        ? steerRun(runId ?? '', snapshot.value, target).send
        : (signal?: AbortSignal) =>
            cancelRun(runId ?? '', {
              target,
              ...(queuedTarget === undefined ? {} : { queuedTarget }),
              ...(signal === undefined ? {} : { signal }),
              idempotencyKey: id,
            });
    const execute = async () => {
      active.current?.abort();
      const controller = new AbortController();
      active.current = controller;
      setAttempt({ id, runId: targetKey, kind, phase: 'sending' });
      try {
        await send(controller.signal);
        if (controller.signal.aborted) return;
        setAttempt({ id, runId: targetKey, kind, phase: 'accepted' });
        if (kind === 'steer') {
          setDraft((current) =>
            current.revision === snapshot.revision
              ? { value: '', revision: current.revision + 1 }
              : current,
          );
          setHistoryOpen(true);
        }
      } catch (error) {
        if (controller.signal.aborted) return;
        const errorKey =
          error instanceof SteerRefusal
            ? `chat.steer.refusal.${error.kind}`
            : 'swarm.controls.failed';
        setAttempt({
          id,
          runId: targetKey,
          kind,
          phase: 'failed',
          errorKey,
          ...(error instanceof SteerRefusal
            ? {}
            : {
                retry: () => {
                  void execute();
                },
              }),
        });
      } finally {
        if (active.current === controller) active.current = null;
        // A pending initial GET can predate this mutation. Settle that read
        // before fetching the receipt so an old empty snapshot cannot win.
        await client.cancelQueries({ queryKey: historyKey });
        await client.invalidateQueries({ queryKey: historyKey });
      }
    };
    void execute();
  };

  const receipts = history.data?.receipts ?? [];
  if (!canSteer && !canCancel && !cancelRequested && receipts.length === 0 && !history.isError)
    return null;

  return (
    <div className="mt-3 shrink-0 space-y-2 border-t border-border pt-3">
      {canSteer || canCancel ? (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            perform('steer');
          }}
          className="space-y-2"
        >
          {canSteer ? (
            <label className="block space-y-1 text-xs text-text-muted">
              <span>{t('swarm.controls.label')}</span>
              <textarea
                rows={2}
                value={draft.value}
                onChange={(event) => {
                  const value = event.target.value;
                  setDraft((current) => ({ value, revision: current.revision + 1 }));
                }}
                placeholder={t('swarm.controls.placeholder')}
                className="w-full resize-none rounded-[var(--radius-md)] border border-border bg-surface-2 p-2 text-sm text-text outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
            </label>
          ) : null}
          <div className="flex gap-2">
            {canSteer ? (
              <Button
                type="submit"
                disabled={busy || draft.value.trim() === ''}
                className="min-h-11 flex-1 gap-2"
              >
                <Send className="size-4" aria-hidden="true" />
                {t('swarm.controls.send')}
              </Button>
            ) : null}
            {canCancel ? (
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  perform('cancel');
                }}
                className="min-h-11 gap-2 text-danger"
              >
                <Square className="size-4" aria-hidden="true" />
                {t('swarm.controls.stop')}
              </Button>
            ) : null}
          </div>
          {canSteer ? (
            <p className="text-xs text-text-faint">{t('swarm.controls.boundary')}</p>
          ) : null}
        </form>
      ) : null}
      {visibleAttempt?.phase === 'sending' ? (
        <p role="status" className="text-xs text-text-muted">
          {t('swarm.controls.sending')}
        </p>
      ) : null}
      {cancelRequested ||
      (visibleAttempt?.phase === 'accepted' && visibleAttempt.kind === 'cancel') ? (
        <p role="status" className="text-xs text-text-muted">
          {t('swarm.controls.stopping')}
        </p>
      ) : null}
      {visibleAttempt?.phase === 'failed' ? (
        <div role="alert" className="text-xs text-danger">
          <p>{t(visibleAttempt.errorKey ?? 'swarm.controls.failed')}</p>
          {visibleAttempt.retry !== undefined ? (
            <Button
              type="button"
              variant="ghost"
              onClick={visibleAttempt.retry}
              className="min-h-11"
            >
              {t('swarm.controls.retry')}
            </Button>
          ) : null}
        </div>
      ) : null}
      {history.isError ? (
        <p role="alert" className="text-xs text-danger">
          {t('swarm.controls.historyError')}
        </p>
      ) : null}
      {receipts.length > 0 ? (
        <details
          open={historyOpen}
          onToggle={(event) => {
            setHistoryOpen(event.currentTarget.open);
          }}
        >
          <summary className="cursor-pointer py-1 text-xs text-text-muted">
            {t('swarm.controls.history')} ({receipts.length})
          </summary>
          <ol className="max-h-28 space-y-2 overflow-y-auto py-2 text-xs">
            {receipts.map((receipt) => (
              <li key={receipt.id} className="space-y-1 border-l-2 border-border pl-2">
                <p className="font-medium text-text">{t(`swarm.controls.${receipt.status}`)}</p>
                <p className="whitespace-pre-wrap break-words text-text-muted">{receipt.text}</p>
                {receipt.reason !== undefined ? (
                  <p className="text-text-faint">{t('swarm.controls.notApplied')}</p>
                ) : null}
              </li>
            ))}
          </ol>
        </details>
      ) : null}
    </div>
  );
}
