import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef } from 'react';

/**
 * useRunSignals turns the named CUSTOM frames a run emits into the cache invalidations the
 * surfaces beside the conversation need. It is the one place that knows which frame touches
 * which query, so a new signal is a line here rather than another block in AppShell — which is
 * how AppShell reached its 600-LOC cap.
 *
 * An aura.scheduler signal lived here briefly and was removed once it was measured: the cockpit
 * is one route whose surfaces are mutually exclusive, so the governance board is never mounted
 * while a run streams to the same tab, and a run in one tab reaches no board in another. What
 * carries a board left open in a second tab is SchedulerBoard's own refetchOnWindowFocus.
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

  // The handler reads the thread and the opener through refs so it can stay STABLE, and it
  // has to stay stable because of who holds it: the SSE pump captures onArtifact once, when
  // the run starts, and keeps that reference for the whole stream (sseResume.makeEngine).
  //
  // A handler closed over activeThreadId is therefore frozen at the value it had when the
  // run began — and the commonest way to deliver an artifact is to send the first message
  // from the home, where the conversation is created TOGETHER with the run. The pump then
  // held a closure whose activeThreadId was still "", the frame arrived, the guard below
  // took the early return, and no refetch ever happened.
  //
  // Measured live 2026-09-09: a real turn delivered bundle.html at 09:09:09 and the panel
  // read "Nessun artefatto" for the whole 300s that followed, with exactly one
  // GET /api/assets — the one at mount, before the artifact existed. Reopening the
  // conversation showed it, because a fresh mount refetches. That is why this looked like
  // an artifact that "disappears" rather than one that never arrives.
  const threadIdRef = useRef(activeThreadId);
  const openArtifactsRef = useRef(openArtifacts);
  useEffect(() => {
    threadIdRef.current = activeThreadId;
  }, [activeThreadId]);
  useEffect(() => {
    openArtifactsRef.current = openArtifacts;
  }, [openArtifacts]);

  const onArtifact = useCallback(() => {
    const threadId = threadIdRef.current;
    if (threadId.length === 0) return;
    void queryClient.invalidateQueries({ queryKey: ['assets', threadId] });
    if (autoOpenedThreads.current.has(threadId)) return;
    autoOpenedThreads.current.add(threadId);
    openArtifactsRef.current();
  }, [queryClient]);

  return { onArtifact };
}
