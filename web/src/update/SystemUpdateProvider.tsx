import { useState, type ReactNode } from 'react';
import { readSessionValue, writeSessionValue } from '../lib/sessionValue';
import { UpdateCenterContext, type DeferOutcome, type UpdateCenter } from './updateCenterContext';
import { awaitsDecision, deferredUntil } from './updateModel';
import { useSystemUpdate } from './useSystemUpdate';

const DISMISSED_KEY = 'aura.update.dismissedRev';

const readDismissed = () => readSessionValue(DISMISSED_KEY);
const writeDismissed = (rev: string | undefined) => {
  writeSessionValue(DISMISSED_KEY, rev);
};

// The admin dialog opens by itself once per build and browser session. A dismissal is
// remembered against the build it dismissed, so a newer build asks again; a decision clears
// it, so the dialog returns when a deferral runs out or an apply fails.
export function SystemUpdateProvider({ children }: { readonly children: ReactNode }) {
  const { status, restarting, now } = useSystemUpdate();
  const [dismissedRev, setDismissedRev] = useState(readDismissed);
  const [forcedRev, setForcedRev] = useState<string | undefined>(undefined);
  const [deferred, setDeferred] = useState<{ rev: string; outcome: DeferOutcome } | undefined>(
    undefined,
  );

  const deciding = awaitsDecision(status);
  const rev = status?.available_rev ?? '';
  // Every piece of dialog state names the build it belongs to, so none of it can resurface
  // over a newer one.
  const deferOutcome = deferred?.rev === rev ? deferred.outcome : undefined;
  const prompted = deciding && deferredUntil(status, now) === undefined && dismissedRev !== rev;
  const dialogOpen = deciding && (prompted || forcedRev === rev || deferOutcome !== undefined);

  const center: UpdateCenter = {
    status,
    restarting,
    now,
    dialogOpen,
    indicatorVisible: deciding && !dialogOpen,
    deferOutcome,
    openDialog: () => {
      setForcedRev(rev);
    },
    dismissDialog: () => {
      writeDismissed(rev);
      setDismissedRev(rev);
      setForcedRev(undefined);
    },
    settleDecision: (outcome) => {
      writeDismissed(undefined);
      setDismissedRev(undefined);
      setForcedRev(undefined);
      setDeferred(outcome === undefined ? undefined : { rev, outcome });
    },
    closeOutcome: () => {
      setDeferred(undefined);
    },
  };

  return <UpdateCenterContext.Provider value={center}>{children}</UpdateCenterContext.Provider>;
}
