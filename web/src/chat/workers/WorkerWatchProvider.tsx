import { useCallback, useMemo, useState, type ReactNode } from 'react';
import type { DisplayChildReport } from '../displays/types';
import { useWorkerStatuses } from './useWorkerStatuses';
import { WorkerWatchContext, type WorkerWatchController } from './workerWatchControls';

interface WorkerRegistry {
  readonly conversationId: string;
  readonly workers: readonly DisplayChildReport[];
}

const EMPTY_WORKERS: readonly DisplayChildReport[] = [];

export function WorkerWatchProvider({
  children,
  conversationId,
  onWatchWorker,
  onViewReport,
}: {
  readonly children: ReactNode;
  readonly conversationId: string;
  readonly onWatchWorker: (childId: string) => void;
  readonly onViewReport: () => void;
}) {
  const [registry, setRegistry] = useState<WorkerRegistry>({ conversationId, workers: [] });
  const workers = registry.conversationId === conversationId ? registry.workers : EMPTY_WORKERS;
  const statuses = useWorkerStatuses(workers.length > 0 ? conversationId : '');
  const registerWorkers = useCallback(
    (nextWorkers: readonly DisplayChildReport[]) => {
      setRegistry({ conversationId, workers: nextWorkers });
    },
    [conversationId],
  );

  const ownsWorker = useCallback(
    (childId: string) => workers.some((worker) => worker.child_id === childId),
    [workers],
  );

  const watchWorker = useCallback(
    (childId: string, nextWorkers?: readonly DisplayChildReport[]) => {
      if (nextWorkers !== undefined) setRegistry({ conversationId, workers: nextWorkers });
      onWatchWorker(childId);
    },
    [conversationId, onWatchWorker],
  );

  const controller = useMemo<WorkerWatchController>(
    () => ({ workers, statuses, ownsWorker, registerWorkers, watchWorker, viewReport: onViewReport }),
    [onViewReport, ownsWorker, registerWorkers, statuses, watchWorker, workers],
  );

  return <WorkerWatchContext.Provider value={controller}>{children}</WorkerWatchContext.Provider>;
}
