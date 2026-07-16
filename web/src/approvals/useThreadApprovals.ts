import { useCallback, useEffect, useMemo, useRef } from 'react';
import type { Approval, ResolveAction } from './useApprovals';
import { useApprovals } from './useApprovals';

export interface ApprovalResolution {
  readonly approval: Approval;
  readonly action: ResolveAction;
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
  generation: number;
  hadPending: boolean;
  resumed: number;
  cancelled: number;
  settled: number;
}

function freshGate(threadId: string): ApprovalGate {
  return {
    threadId,
    generation: 0,
    hadPending: false,
    resumed: -1,
    cancelled: -1,
    settled: -1,
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
  const sessionEpoch = useRef(0);
  const gate = useRef<ApprovalGate>(freshGate(threadId));

  useEffect(() => {
    sessionEpoch.current += 1;
    gate.current = freshGate(threadId);
    return () => {
      sessionEpoch.current += 1;
    };
  }, [threadId]);

  useEffect(() => {
    if (pendingApprovals.length > 0 && !gate.current.hadPending) {
      gate.current.generation += 1;
      gate.current.hadPending = true;
    } else if (pendingApprovals.length === 0) {
      gate.current.hadPending = false;
    }
  }, [pendingApprovals.length, threadId]);

  const onResolved = useCallback(
    async ({ action }: ApprovalResolution) => {
      if (gate.current.threadId !== threadId) return;
      const epoch = sessionEpoch.current;
      const generation = gate.current.generation;
      if (action === 'cancel') gate.current.cancelled = generation;

      const refreshed = await query.refetch();
      if (
        sessionEpoch.current !== epoch ||
        gate.current.threadId !== threadId ||
        gate.current.generation !== generation
      ) {
        return;
      }

      const cancelled = gate.current.cancelled === generation;
      if (cancelled && action !== 'cancel') return;

      const remaining = selectPendingThreadApprovals(refreshed.data, threadId);
      if (cancelled) {
        if (gate.current.settled === generation) return;
        gate.current.settled = generation;
        gate.current.hadPending = remaining.length > 0;
        onFocusRequested(remaining[0]);
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
    [onFocusRequested, onResume, query, threadId],
  );

  return { approvals, isPending: pendingApprovals.length > 0, onResolved };
}
