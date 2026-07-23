import { useCallback, useEffect, useRef, type Dispatch, type SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { fetchConversation } from '../conversations/useConversations';
import { attachRun } from './sseResume';
import type { TurnUsage } from './sseAdapter';

// ExternalStoreChat_liveRun — the RS-07 §4.2 reload-attach split out of
// ExternalStoreChat.tsx (600-LOC cap): when a thread opens while its detached
// run is still live (conversation DTO live_run_id), this hook appends a fresh
// running assistant message and folds the full-buffer replay through the same
// reducer, then continues live. The stream scaffolding (foldAppendedStream)
// stays in the component; this hook owns only the discovery + attach policy.

type AppendedStreamFold = (
  runThreadId: string,
  run: (
    controller: AbortController,
    onUpdate: (assistant: ThreadMessageLike, usage: TurnUsage | undefined) => void,
  ) => Promise<unknown>,
) => Promise<void>;

export interface LiveRunAttachArgs {
  readonly threadId: string;
  /** The conversation DTO's additive live_run_id (absent ⇒ nothing to attach). */
  readonly liveRunId: string | undefined;
  readonly historyReadiness: {
    readonly threadId: string;
    readonly status: 'loading' | 'ready' | 'error';
  };
  readonly isRunningRef: { readonly current: boolean };
  readonly activeRunIdRef: { current: string | null };
  readonly foldAppendedStream: AppendedStreamFold;
  readonly setMessages: Dispatch<SetStateAction<ThreadMessageLike[]>>;
  readonly onArtifact?: ((assetId: string | undefined) => void) | undefined;
}

export function useLiveRunAttach({
  threadId,
  liveRunId,
  historyReadiness,
  isRunningRef,
  activeRunIdRef,
  foldAppendedStream,
  setMessages,
  onArtifact,
}: LiveRunAttachArgs): void {
  const { t } = useTranslation();
  /** One reload-attach attempt per (thread, run): a 404/410 must not re-loop. */
  const attachedRunsRef = useRef<Set<string>>(new Set());

  const attachLiveRun = useCallback(
    (runId: string) =>
      foldAppendedStream(threadId, (controller, onUpdate) => {
        activeRunIdRef.current = runId;
        return attachRun({
          threadId,
          runId,
          signal: controller.signal,
          connectionLostNote: t('chat.error.connectionLost'),
          onSnapshotReplace: setMessages,
          ...(onArtifact !== undefined ? { onArtifact } : {}),
          onUpdate,
        });
      }),
    [foldAppendedStream, threadId, t, onArtifact, activeRunIdRef, setMessages],
  );

  // Reload-attach discovery (design §4.2): once the snapshot is rendered, a set
  // live_run_id re-attaches the thread's in-flight run. One attempt per
  // (thread, run) — a 404/410 fallback must not re-loop through the
  // invalidate → refetch → attach cycle — and a FRESH conversation read guards
  // the stale-cache race (run finished between conversation fetch and
  // history-ready ⇒ the snapshot already renders the turn; attaching again
  // would replay it twice).
  useEffect(() => {
    if (threadId.length === 0 || liveRunId === undefined || liveRunId.length === 0) return;
    if (historyReadiness.threadId !== threadId || historyReadiness.status !== 'ready') return;
    if (isRunningRef.current) return;
    const key = `${threadId}:${liveRunId}`;
    if (attachedRunsRef.current.has(key)) return;
    attachedRunsRef.current.add(key);
    void fetchConversation(threadId)
      .then((fresh) => {
        if (fresh.live_run_id !== liveRunId || isRunningRef.current) return undefined;
        return attachLiveRun(liveRunId);
      })
      .catch(() => undefined);
  }, [threadId, liveRunId, historyReadiness, isRunningRef, attachLiveRun]);
}
