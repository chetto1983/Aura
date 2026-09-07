import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useRef } from 'react';
import { SCHEDULER_QUERY_KEY } from '../governance/useSchedulerMutations';

/**
 * useRunSignals turns the named CUSTOM frames a run emits into the cache invalidations the
 * surfaces beside the conversation need. It is the one place that knows which frame touches
 * which query, so a new signal is a line here rather than another block in AppShell — which is
 * how AppShell reached its 600-LOC cap.
 *
 * Each handler is deliberately narrow: the frame carries the FACT that something changed, and
 * the surface refetches through its own authenticated route. Rendering payload from a chat
 * frame would let one surface's authorization leak into another's.
 */
export function useRunSignals(activeThreadId: string, openArtifacts: () => void) {
  const queryClient = useQueryClient();

  // D-11: a run that emits `aura.artifact` invalidates the identity-scoped assets query so the
  // panel refetches the new asset (37A persists it before the event, so it is always there),
  // and auto-opens the panel exactly once per thread. The Set is keyed by threadId: a thread
  // the user already saw an artifact in never re-opens after a manual close, while a NEW thread
  // re-arms — the "reset on thread change" contract without a separate reset effect.
  const autoOpenedThreads = useRef<Set<string>>(new Set());
  const onArtifact = useCallback(() => {
    if (activeThreadId.length === 0) return;
    void queryClient.invalidateQueries({ queryKey: ['assets', activeThreadId] });
    if (autoOpenedThreads.current.has(activeThreadId)) return;
    autoOpenedThreads.current.add(activeThreadId);
    openArtifacts();
  }, [activeThreadId, queryClient, openArtifacts]);

  // A run that emits `aura.scheduler` changed the schedule, so the governance board must reread
  // itself. It needs no threadId, unlike the artifact twin: a schedule belongs to the identity,
  // not to the conversation that happened to create it.
  //
  // KNOWN LIMIT, measured 2026-09-07 rather than assumed: the cockpit is one route whose
  // surfaces are mutually exclusive (AppShell renders chat OR governance), so the board is
  // never mounted while a run streams to the same tab, and a run in one tab reaches no board in
  // another. This invalidation therefore has no consumer anybody has named yet. The mechanism
  // that does carry a second tab is SchedulerBoard's own refetchOnWindowFocus.
  const onScheduler = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: SCHEDULER_QUERY_KEY });
  }, [queryClient]);

  return { onArtifact, onScheduler };
}
