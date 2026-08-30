import { createContext, useContext } from 'react';
import type { DisplayChildReport } from '../displays/types';
import type { WorkerStatus } from './workerStream';

export interface WorkerWatchController {
  readonly workers: readonly DisplayChildReport[];
  readonly statuses: ReadonlyMap<string, WorkerStatus>;
  readonly ownsWorker: (childId: string) => boolean;
  readonly registerWorkers: (workers: readonly DisplayChildReport[]) => void;
  readonly watchWorker: (childId: string, workers?: readonly DisplayChildReport[]) => void;
  readonly viewReport: () => void;
}

const NOOP_CONTROLLER: WorkerWatchController = {
  workers: [],
  statuses: new Map(),
  ownsWorker: () => true,
  registerWorkers: () => undefined,
  watchWorker: () => undefined,
  viewReport: () => undefined,
};

export const WorkerWatchContext = createContext<WorkerWatchController | null>(null);

export function useWatchWorker(): WorkerWatchController {
  return useContext(WorkerWatchContext) ?? NOOP_CONTROLLER;
}
