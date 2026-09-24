import { createContext, useContext } from 'react';
import type { SystemUpdateStatus } from './systemUpdateApi';

// The context object + consumer hook for the update center, kept apart from
// SystemUpdateProvider.tsx so that module stays component-only (only-export-components).
// Outside the provider (the isolated header tests, the login route) the hook answers null
// and every consumer renders nothing.

export interface DeferOutcome {
  readonly until: number;
  /** The daemon moved the time earlier than asked: the build's deadline came first. */
  readonly clamped: boolean;
}

export interface UpdateCenter {
  readonly status: SystemUpdateStatus | undefined;
  readonly restarting: boolean;
  readonly now: number;
  readonly dialogOpen: boolean;
  readonly indicatorVisible: boolean;
  readonly deferOutcome: DeferOutcome | undefined;
  readonly openDialog: () => void;
  /** Closing without choosing: not a decision, so the dialog stays shut for this build. */
  readonly dismissDialog: () => void;
  readonly settleDecision: (outcome?: DeferOutcome) => void;
  readonly closeOutcome: () => void;
}

export const UpdateCenterContext = createContext<UpdateCenter | null>(null);

export function useUpdateCenter(): UpdateCenter | null {
  return useContext(UpdateCenterContext);
}
