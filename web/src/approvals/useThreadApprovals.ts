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
  const gate = useRef({
    threadId,
    generation: 0,
    hadPending: false,
    resumed: -1,
  });

  useEffect(() => {
    if (gate.current.threadId !== threadId) {
      gate.current = {
        threadId,
        generation: 0,
        hadPending: false,
        resumed: -1,
      };
    }
    if (approvals.length > 0 && !gate.current.hadPending) {
      gate.current.generation += 1;
      gate.current.hadPending = true;
    }
  }, [approvals.length, threadId]);

  const onResolved = useCallback(
    async ({ action }: ApprovalResolution) => {
      const generation = gate.current.generation;
      const refreshed = await query.refetch();
      const remaining = selectThreadApprovals(refreshed.data, threadId);
      if (remaining.length > 0) {
        onFocusRequested(remaining[0]);
        return;
      }
      gate.current.hadPending = false;
      onFocusRequested(undefined);
      if (action === 'cancel' || gate.current.resumed === generation) return;
      gate.current.resumed = generation;
      await onResume();
    },
    [onFocusRequested, onResume, query, threadId],
  );

  return { approvals, isPending: approvals.length > 0, onResolved };
}
