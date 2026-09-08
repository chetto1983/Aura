import { useEffect, useState } from 'react';
import {
  openWorkerStatusStream,
  type WorkerStatus,
  type CoordinatorRunStatus,
} from './workerStream';

const EMPTY_STATUSES: ReadonlyMap<string, WorkerStatus> = new Map();

interface StatusState {
  readonly conversationId: string;
  readonly statuses: ReadonlyMap<string, WorkerStatus>;
  readonly coordinator?: CoordinatorRunStatus;
}

export function useWorkerStatuses(conversationId: string): StatusState {
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
          return previous.conversationId === conversationId
            ? { ...previous, statuses }
            : { conversationId, statuses };
        });
      },
      onCoordinator: (coordinator) => {
        setState((previous) => ({
          conversationId,
          statuses: previous.conversationId === conversationId ? previous.statuses : EMPTY_STATUSES,
          coordinator,
        }));
      },
      onError: () => undefined,
    }).close;
  }, [conversationId]);

  if (conversationId.length === 0 || state.conversationId !== conversationId) {
    return { conversationId, statuses: EMPTY_STATUSES };
  }
  return state;
}
