import { createContext, useContext } from 'react';

// collectedJobsContext — the job ids this thread has already collected, read by
// GenerationToolDisplay so a detached job's card disappears once its clip has been fetched.
// Kept in a NON-component `.ts` module (like assetSourceContext.ts) so no component shares
// this file: a `.tsx` exporting both a context and a component fails
// `react-refresh/only-export-components` and therefore the web gate.
//
// The default is empty, so every renderer and test that mounts no provider behaves exactly as
// before: nothing is hidden.

const NONE: ReadonlySet<string> = new Set<string>();

export const CollectedJobsContext = createContext(NONE);

/** The video jobs a later call has collected in this thread; empty without a provider. */
export function useCollectedJobs(): ReadonlySet<string> {
  return useContext(CollectedJobsContext);
}
