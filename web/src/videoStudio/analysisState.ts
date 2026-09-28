import { useEffect, useRef, useState } from 'react';

// analysisState.ts — an analysis in the browser as the operator sees it: working, failed and why,
// or nothing to say. Shared by the controls that analyse before they edit (Inspector_audioClean.tsx)
// and by ducking's own listening (VideoStudio_ducking.ts), so the two read alike; analysisStatus.tsx
// says it on screen.

export type AnalysisState =
  | { readonly state: 'idle' }
  | { readonly state: 'working' }
  | { readonly state: 'failed'; readonly reason: string };

export function reasonOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** One analysis at a time for a control, aborted when the control goes: mounted with the item's id
 *  as its key, a control that goes is also a control whose selection changed. */
export function useAnalysis() {
  const [state, setState] = useState<AnalysisState>({ state: 'idle' });
  const running = useRef<AbortController>(undefined);
  useEffect(
    () => () => {
      running.current?.abort();
    },
    [],
  );
  async function run(task: (signal: AbortSignal) => Promise<void>) {
    running.current?.abort();
    const controller = new AbortController();
    running.current = controller;
    setState({ state: 'working' });
    try {
      await task(controller.signal);
      if (!controller.signal.aborted) setState({ state: 'idle' });
    } catch (error) {
      if (controller.signal.aborted) return;
      setState({ state: 'failed', reason: reasonOf(error) });
    }
  }
  return { state, run };
}
