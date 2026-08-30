import { useCallback, useMemo, useState, type ReactNode } from 'react';
import type { DisplayChildReport } from '../displays/types';
import { useWorkerStatuses } from './useWorkerStatuses';
import { WorkerWatchContext, type WorkerWatchController } from './workerWatchControls';

interface WorkerRegistryState {
  readonly conversationId: string;
  readonly registrations: ReadonlyMap<string, readonly DisplayChildReport[]>;
}

const EMPTY_WORKERS: readonly DisplayChildReport[] = [];
const EMPTY_REGISTRATIONS: ReadonlyMap<string, readonly DisplayChildReport[]> = new Map();

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
  const [registry, setRegistry] = useState<WorkerRegistryState>({
    conversationId,
    registrations: EMPTY_REGISTRATIONS,
  });
  const registrations =
    registry.conversationId === conversationId ? registry.registrations : EMPTY_REGISTRATIONS;
  const registryReady = registrations.size > 0;
  const workers = useMemo(() => {
    if (registrations.size === 0) return EMPTY_WORKERS;
    const merged = new Map<string, DisplayChildReport>();
    for (const reports of registrations.values()) {
      for (const report of reports) merged.set(report.child_id, report);
    }
    return [...merged.values()];
  }, [registrations]);
  const statuses = useWorkerStatuses(workers.length > 0 ? conversationId : '');
  const registerWorkers = useCallback(
    (registrationId: string, nextWorkers: readonly DisplayChildReport[]) => {
      setRegistry((previous) => {
        const next =
          previous.conversationId === conversationId
            ? new Map(previous.registrations)
            : new Map<string, readonly DisplayChildReport[]>();
        next.set(registrationId, nextWorkers);
        return { conversationId, registrations: next };
      });
      return () => {
        setRegistry((previous) => {
          if (previous.conversationId !== conversationId) return previous;
          const next = new Map(previous.registrations);
          next.delete(registrationId);
          return { conversationId, registrations: next };
        });
      };
    },
    [conversationId],
  );

  const ownsWorker = useCallback(
    (childId: string) => workers.some((worker) => worker.child_id === childId),
    [workers],
  );

  const watchWorker = useCallback(
    (childId: string, nextWorkers?: readonly DisplayChildReport[]) => {
      const selectable =
        workers.some((worker) => worker.child_id === childId) ||
        nextWorkers?.some((worker) => worker.child_id === childId) === true;
      if (!selectable) return;
      onWatchWorker(childId);
    },
    [onWatchWorker, workers],
  );

  const controller = useMemo<WorkerWatchController>(
    () => ({
      workers,
      statuses,
      registryReady,
      ownsWorker,
      registerWorkers,
      watchWorker,
      viewReport: onViewReport,
    }),
    [onViewReport, ownsWorker, registerWorkers, registryReady, statuses, watchWorker, workers],
  );

  return <WorkerWatchContext.Provider value={controller}>{children}</WorkerWatchContext.Provider>;
}
