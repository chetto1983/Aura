import { useCallback, useLayoutEffect, useMemo, useRef } from 'react';
import type { Approval, ResolveAction } from './useApprovals';
import { useApprovals } from './useApprovals';

export interface ApprovalResolution {
  readonly approval: Approval;
  readonly action: ResolveAction;
}

export interface ApprovalResolutionAttempt extends ApprovalResolution {
  readonly attemptId: string;
}

export function selectThreadApprovals(
  rows: readonly Approval[] | undefined,
  threadId: string,
): Approval[] {
  if (threadId.length === 0) return [];
  return (rows ?? []).filter((row) => row.conversation_id === threadId);
}

function selectPendingThreadApprovals(
  rows: readonly Approval[] | undefined,
  threadId: string,
): Approval[] {
  return selectThreadApprovals(rows, threadId).filter((row) => row.terminal !== true);
}

interface ApprovalGate {
  readonly threadId: string;
  readonly session: object;
  generation: number;
  hadPending: boolean;
  resumed: number;
  cancelled: number;
  settled: number;
  readonly cancelIntents: Set<string>;
  deferred: { readonly generation: number; readonly remaining: readonly Approval[] } | undefined;
}

function freshGate(threadId: string, session: object): ApprovalGate {
  return {
    threadId,
    session,
    generation: 0,
    hadPending: false,
    resumed: -1,
    cancelled: -1,
    settled: -1,
    cancelIntents: new Set<string>(),
    deferred: undefined,
  };
}

export function useThreadApprovals(
  threadId: string,
  onResume: () => Promise<void>,
  onFocusRequested: (next: Approval | undefined) => void,
) {
  const query = useApprovals();
  const approvals = useMemo(
    () => selectThreadApprovals(query.data, threadId),
    [query.data, threadId],
  );
  const pendingApprovals = useMemo(
    () => selectPendingThreadApprovals(query.data, threadId),
    [query.data, threadId],
  );
  const session = useMemo(() => ({ threadId }), [threadId]);
  const sessionEpoch = useRef(0);
  const gate = useRef<ApprovalGate>(freshGate(threadId, session));

  useLayoutEffect(() => {
    sessionEpoch.current += 1;
    gate.current = freshGate(threadId, session);
    return () => {
      sessionEpoch.current += 1;
    };
  }, [session, threadId]);

  useLayoutEffect(() => {
    if (pendingApprovals.length > 0 && !gate.current.hadPending) {
      gate.current.generation += 1;
      gate.current.hadPending = true;
      gate.current.cancelIntents.clear();
      gate.current.deferred = undefined;
    } else if (pendingApprovals.length === 0) {
      gate.current.hadPending = false;
    }
  }, [pendingApprovals.length, threadId]);

  const finishNonCancel = useCallback(
    async (generation: number, remaining: readonly Approval[]) => {
      if (
        gate.current.threadId !== threadId ||
        gate.current.session !== session ||
        gate.current.generation !== generation
      ) {
        return;
      }
      if (remaining.length > 0) {
        onFocusRequested(remaining[0]);
        return;
      }
      if (gate.current.settled === generation) return;
      gate.current.settled = generation;
      gate.current.hadPending = false;
      onFocusRequested(undefined);
      if (gate.current.resumed === generation) return;
      gate.current.resumed = generation;
      await onResume();
    },
    [onFocusRequested, onResume, session, threadId],
  );

  const onResolutionStarted = useCallback(
    (attempt: ApprovalResolutionAttempt) => {
      if (
        attempt.action !== 'cancel' ||
        attempt.approval.conversation_id !== threadId ||
        gate.current.threadId !== threadId ||
        gate.current.session !== session ||
        !gate.current.hadPending
      ) {
        return;
      }
      gate.current.cancelIntents.add(attempt.attemptId);
    },
    [session, threadId],
  );

  const onResolutionFailed = useCallback(
    async (attempt: ApprovalResolutionAttempt) => {
      if (
        attempt.action !== 'cancel' ||
        attempt.approval.conversation_id !== threadId ||
        gate.current.threadId !== threadId ||
        gate.current.session !== session
      ) {
        return;
      }
      const removed = gate.current.cancelIntents.delete(attempt.attemptId);
      const generation = gate.current.generation;
      if (
        !removed ||
        gate.current.cancelIntents.size > 0 ||
        gate.current.cancelled === generation
      ) {
        return;
      }
      const deferred = gate.current.deferred;
      if (deferred?.generation !== generation) return;
      gate.current.deferred = undefined;
      await finishNonCancel(generation, deferred.remaining);
    },
    [finishNonCancel, session, threadId],
  );

  const onResolved = useCallback(
    async ({ action }: ApprovalResolution) => {
      if (gate.current.threadId !== threadId || gate.current.session !== session) return;
      const epoch = sessionEpoch.current;
      const generation = gate.current.generation;
      if (action === 'cancel') {
        gate.current.cancelled = generation;
        gate.current.cancelIntents.clear();
        gate.current.deferred = undefined;
      }

      const refreshed = await query.refetch();
      if (
        sessionEpoch.current !== epoch ||
        gate.current.threadId !== threadId ||
        gate.current.session !== session ||
        gate.current.generation !== generation
      ) {
        return;
      }

      const remaining = selectPendingThreadApprovals(refreshed.data, threadId);
      if (action !== 'cancel' && gate.current.cancelled === generation) return;
      if (action !== 'cancel' && gate.current.cancelIntents.size > 0) {
        gate.current.deferred = { generation, remaining };
        return;
      }
      if (action === 'cancel') {
        if (gate.current.settled === generation) return;
        gate.current.settled = generation;
        gate.current.hadPending = remaining.length > 0;
        onFocusRequested(remaining[0]);
        return;
      }
      await finishNonCancel(generation, remaining);
    },
    [finishNonCancel, onFocusRequested, query, session, threadId],
  );

  return {
    approvals,
    isPending: pendingApprovals.length > 0,
    onResolutionStarted,
    onResolutionFailed,
    onResolved,
  };
}
