import { useCallback, useMemo, useState, type ReactNode } from 'react';
import type { DisplayChildReport } from '../displays/types';
import { WorkerWatchContext, type WorkerWatchController } from './workerWatchControls';

export function WorkerWatchProvider({
  children,
  onWatchWorker,
  onViewReport,
}: {
  readonly children: ReactNode;
  readonly onWatchWorker: (childId: string) => void;
  readonly onViewReport: () => void;
}) {
  const [workers, setWorkers] = useState<readonly DisplayChildReport[]>([]);

  const watchWorker = useCallback(
    (childId: string, nextWorkers?: readonly DisplayChildReport[]) => {
      if (nextWorkers !== undefined) setWorkers(nextWorkers);
      onWatchWorker(childId);
    },
    [onWatchWorker],
  );

  const controller = useMemo<WorkerWatchController>(
    () => ({ workers, watchWorker, viewReport: onViewReport }),
    [onViewReport, watchWorker, workers],
  );

  return <WorkerWatchContext.Provider value={controller}>{children}</WorkerWatchContext.Provider>;
}
