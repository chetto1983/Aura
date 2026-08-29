import { useEffect, useState } from 'react';
import { openWorkerStatusStream, type WorkerStatus } from './workerStream';

const EMPTY_STATUSES: ReadonlyMap<string, WorkerStatus> = new Map();

interface StatusState {
  readonly conversationId: string;
  readonly statuses: ReadonlyMap<string, WorkerStatus>;
}

export function useWorkerStatuses(conversationId: string): ReadonlyMap<string, WorkerStatus> {
  const [state, setState] = useState<StatusState>({ conversationId, statuses: EMPTY_STATUSES });

  useEffect(() => {
    if (conversationId.length === 0 || typeof EventSource !== 'function') return;
    return openWorkerStatusStream(conversationId, {
      onStatus: (status) => {
        setState((previous) => {
          const statuses =
            previous.conversationId === conversationId
              ? new Map(previous.statuses)
              : new Map<string, WorkerStatus>();
          statuses.set(status.child_id, status);
          return { conversationId, statuses };
        });
      },
      onError: () => undefined,
    }).close;
  }, [conversationId]);

  if (conversationId.length === 0 || state.conversationId !== conversationId) {
    return EMPTY_STATUSES;
  }
  return state.statuses;
}
