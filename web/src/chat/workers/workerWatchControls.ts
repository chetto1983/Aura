import { createContext, useContext } from 'react';
import type { DisplayChildReport } from '../displays/types';

export interface WorkerWatchController {
  readonly workers: readonly DisplayChildReport[];
  readonly watchWorker: (childId: string, workers?: readonly DisplayChildReport[]) => void;
  readonly viewReport: () => void;
}

const NOOP_CONTROLLER: WorkerWatchController = {
  workers: [],
  watchWorker: () => undefined,
  viewReport: () => undefined,
};

export const WorkerWatchContext = createContext<WorkerWatchController | null>(null);

export function useWatchWorker(): WorkerWatchController {
  return useContext(WorkerWatchContext) ?? NOOP_CONTROLLER;
}
