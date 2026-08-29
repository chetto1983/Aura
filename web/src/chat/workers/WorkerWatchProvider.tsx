import { useCallback, useMemo, useState, type ReactNode } from 'react';
import type { DisplayChildReport } from '../displays/types';
import { useWorkerStatuses } from './useWorkerStatuses';
import { WorkerWatchContext, type WorkerWatchController } from './workerWatchControls';

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
  const [workers, setWorkers] = useState<readonly DisplayChildReport[]>([]);
  const statuses = useWorkerStatuses(workers.length > 0 ? conversationId : '');
  const registerWorkers = useCallback((nextWorkers: readonly DisplayChildReport[]) => {
    setWorkers(nextWorkers);
  }, []);

  const watchWorker = useCallback(
    (childId: string, nextWorkers?: readonly DisplayChildReport[]) => {
      if (nextWorkers !== undefined) setWorkers(nextWorkers);
      onWatchWorker(childId);
    },
    [onWatchWorker],
  );

  const controller = useMemo<WorkerWatchController>(
    () => ({ workers, statuses, registerWorkers, watchWorker, viewReport: onViewReport }),
    [onViewReport, registerWorkers, statuses, watchWorker, workers],
  );

  return <WorkerWatchContext.Provider value={controller}>{children}</WorkerWatchContext.Provider>;
}
