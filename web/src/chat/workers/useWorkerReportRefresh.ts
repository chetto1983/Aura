import { useEffect, useRef, type Dispatch, type SetStateAction } from 'react';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { fetchThreadMessages } from '../sseAdapter';
import { useWatchWorker } from './workerWatchControls';

// Live turns have client IDs; snapshots have database IDs. Replacing the IDs of
// the same turns makes assistant-ui create edit branches. Match the unchanged
// user sequence by position (including repeated prompts), never by text alone.
export function preserveMessageIDs(
  previous: readonly ThreadMessageLike[],
  snapshot: ThreadMessageLike[],
): ThreadMessageLike[] {
  const users = (messages: readonly ThreadMessageLike[]) =>
    messages.flatMap((message, index) => (message.role === 'user' ? [index] : []));
  const before = users(previous),
    after = users(snapshot);
  if (
    before.length !== after.length ||
    before.some(
      (index, i) =>
        JSON.stringify(previous[index]?.content) !==
        JSON.stringify(snapshot[after[i] ?? -1]?.content),
    )
  )
    return snapshot;
  const aliases = new Map<number, string>();
  before.forEach((index, i) => {
    const target = after[i];
    if (target === undefined) return;
    const id = previous[index]?.id;
    if (id !== undefined) aliases.set(target, id);
    const assistant = previous[index + 1];
    if (
      assistant?.role === 'assistant' &&
      assistant.id !== undefined &&
      snapshot[target + 1]?.role === 'assistant'
    )
      aliases.set(target + 1, assistant.id);
  });
  return snapshot.map((message, index) => {
    const id = aliases.get(index);
    return id === undefined ? message : { ...message, id };
  });
}

export function useWorkerReportRefresh({
  threadId,
  isRunning,
  historyRequestRef,
  setMessages,
  onCoordinatorRun,
}: {
  readonly threadId: string;
  readonly isRunning: boolean;
  readonly historyRequestRef: { readonly current: number };
  readonly setMessages: Dispatch<SetStateAction<ThreadMessageLike[]>>;
  readonly onCoordinatorRun?: (threadId: string) => void;
}) {
  const { statuses, coordinator } = useWatchWorker();
  const reported = [...statuses.values()]
    .filter((worker) => worker.reported)
    .map((worker) => worker.child_id)
    .sort()
    .join(',');
  const applied = useRef('');
  const coordinatorRevision =
    coordinator === undefined ? '' : `${coordinator.run_id}:${coordinator.status}`;
  useEffect(() => {
    if (coordinatorRevision.length > 0) onCoordinatorRun?.(threadId);
  }, [threadId, coordinatorRevision, onCoordinatorRun]);
  useEffect(() => {
    if (
      threadId.length === 0 ||
      (reported.length === 0 && coordinatorRevision.length === 0) ||
      isRunning
    )
      return;
    const key = `${threadId}:${reported}:${coordinatorRevision}`;
    if (applied.current === key) return;
    const generation = historyRequestRef.current;
    const controller = new AbortController();
    void fetchThreadMessages(threadId, controller.signal)
      .then((messages) => {
        // A send or conversation switch owns the history once it advances this
        // existing generation. A late report refresh must never replace that work.
        if (controller.signal.aborted || historyRequestRef.current !== generation) return;
        applied.current = key;
        setMessages((previous) => preserveMessageIDs(previous, messages));
      })
      .catch(() => undefined);
    return () => {
      controller.abort();
    };
  }, [threadId, reported, coordinatorRevision, isRunning, historyRequestRef, setMessages]);
}
