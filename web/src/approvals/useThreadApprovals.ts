import { useCallback, useLayoutEffect, useMemo, useRef } from 'react';
import type { Approval, ResolveAction } from './useApprovals';
import { useApprovals } from './useApprovals';

export interface ApprovalResolution {
  readonly approval: Approval;
  readonly action: ResolveAction;
  readonly attemptId: string;
}

export type ApprovalResolutionAttempt = ApprovalResolution;

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

interface AttemptOwner {
  readonly attemptId: string;
  readonly action: ResolveAction;
  readonly token: string;
  readonly generation: number;
  readonly session: object;
  readonly epoch: number;
}

interface DeferredOutcome {
  readonly generation: number;
  readonly remaining: readonly Approval[];
  readonly resolvedToken?: string;
}

interface ApprovalGate {
  readonly threadId: string;
  readonly session: object;
  generation: number;
  hadPending: boolean;
  resumed: number;
  cancelled: number;
  settled: number;
  resumeEligible: number;
  pendingTokens: Set<string>;
  readonly attempts: Map<string, AttemptOwner>;
  readonly cancelIntents: Set<string>;
  deferred: DeferredOutcome | undefined;
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
    resumeEligible: -1,
    pendingTokens: new Set<string>(),
    attempts: new Map<string, AttemptOwner>(),
    cancelIntents: new Set<string>(),
    deferred: undefined,
  };
}

function tokensFor(approvals: readonly Approval[]): Set<string> {
  return new Set(approvals.map((approval) => approval.token));
}

function overlaps(left: ReadonlySet<string>, right: ReadonlySet<string>): boolean {
  for (const token of left) {
    if (right.has(token)) return true;
  }
  return false;
}

function rotateGeneration(gate: ApprovalGate, pendingTokens: Set<string>): void {
  gate.generation += 1;
  gate.hadPending = true;
  gate.resumed = -1;
  gate.cancelled = -1;
  gate.settled = -1;
  gate.resumeEligible = -1;
  gate.pendingTokens = pendingTokens;
  gate.attempts.clear();
  gate.cancelIntents.clear();
  gate.deferred = undefined;
}

function matchesAttempt(owner: AttemptOwner, attempt: ApprovalResolutionAttempt): boolean {
  return (
    owner.attemptId === attempt.attemptId &&
    owner.action === attempt.action &&
    owner.token === attempt.approval.token
  );
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
  const pendingTokens = useMemo(() => tokensFor(pendingApprovals), [pendingApprovals]);
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
    const current = gate.current;
    if (current.threadId !== threadId || current.session !== session) return;
    if (pendingTokens.size === 0) {
      current.pendingTokens = pendingTokens;
      current.hadPending = false;
      return;
    }
    if (!current.hadPending || !overlaps(current.pendingTokens, pendingTokens)) {
      rotateGeneration(current, pendingTokens);
      return;
    }
    current.pendingTokens = pendingTokens;
  }, [pendingTokens, session, threadId]);

  const finishNonCancel = useCallback(
    async (generation: number, remaining: readonly Approval[]) => {
      const current = gate.current;
      if (
        current.threadId !== threadId ||
        current.session !== session ||
        current.generation !== generation ||
        current.resumeEligible !== generation ||
        current.cancelled === generation
      ) {
        return;
      }
      if (remaining.length > 0) {
        current.hadPending = true;
        current.pendingTokens = tokensFor(remaining);
        onFocusRequested(remaining[0]);
        return;
      }
      if (current.settled === generation) return;
      current.settled = generation;
      current.hadPending = false;
      current.pendingTokens = new Set<string>();
      onFocusRequested(undefined);
      if (current.resumed === generation) return;
      current.resumed = generation;
      await onResume();
    },
    [onFocusRequested, onResume, session, threadId],
  );

  useLayoutEffect(() => {
    const current = gate.current;
    const generation = current.generation;
    if (
      pendingApprovals.length !== 0 ||
      current.threadId !== threadId ||
      current.session !== session ||
      current.resumeEligible !== generation ||
      current.cancelled === generation ||
      current.settled === generation
    ) {
      return;
    }
    if (current.cancelIntents.size > 0) {
      current.deferred = { generation, remaining: [] };
      return;
    }
    void finishNonCancel(generation, []);
  }, [finishNonCancel, pendingApprovals.length, session, threadId]);

  const onResolutionStarted = useCallback(
    (attempt: ApprovalResolutionAttempt) => {
      const current = gate.current;
      if (
        attempt.approval.conversation_id !== threadId ||
        current.threadId !== threadId ||
        current.session !== session ||
        !current.hadPending ||
        !current.pendingTokens.has(attempt.approval.token) ||
        current.cancelled === current.generation
      ) {
        return;
      }
      const existing = current.attempts.get(attempt.attemptId);
      if (existing !== undefined) return;
      const owner: AttemptOwner = {
        attemptId: attempt.attemptId,
        action: attempt.action,
        token: attempt.approval.token,
        generation: current.generation,
        session,
        epoch: sessionEpoch.current,
      };
      current.attempts.set(attempt.attemptId, owner);
      if (attempt.action === 'cancel') current.cancelIntents.add(attempt.attemptId);
    },
    [session, threadId],
  );

  const takeAttempt = useCallback(
    (attempt: ApprovalResolutionAttempt): AttemptOwner | undefined => {
      const current = gate.current;
      const owner = current.attempts.get(attempt.attemptId);
      if (
        owner === undefined ||
        !matchesAttempt(owner, attempt) ||
        owner.session !== session ||
        owner.epoch !== sessionEpoch.current ||
        owner.generation !== current.generation ||
        current.threadId !== threadId ||
        current.session !== session
      ) {
        return undefined;
      }
      current.attempts.delete(attempt.attemptId);
      return owner;
    },
    [session, threadId],
  );

  const onResolutionFailed = useCallback(
    async (attempt: ApprovalResolutionAttempt) => {
      const owner = takeAttempt(attempt);
      if (owner?.action !== 'cancel') return;
      const current = gate.current;
      const removed = current.cancelIntents.delete(owner.attemptId);
      if (!removed || current.cancelIntents.size > 0 || current.cancelled === owner.generation) {
        return;
      }
      const deferred = current.deferred;
      if (deferred?.generation !== owner.generation) return;
      current.deferred = undefined;
      if (
        deferred.resolvedToken !== undefined &&
        deferred.remaining.some((approval) => approval.token === deferred.resolvedToken)
      ) {
        return;
      }
      await finishNonCancel(owner.generation, deferred.remaining);
    },
    [finishNonCancel, takeAttempt],
  );

  const onResolved = useCallback(
    async (attempt: ApprovalResolution) => {
      const owner = takeAttempt(attempt);
      if (owner === undefined) return;
      const current = gate.current;
      const generation = owner.generation;
      if (owner.action === 'cancel') {
        current.cancelled = generation;
        current.resumeEligible = -1;
        current.cancelIntents.clear();
        current.attempts.clear();
        current.deferred = undefined;
      } else {
        current.resumeEligible = generation;
      }

      let refreshed: Awaited<ReturnType<typeof query.refetch>>;
      try {
        refreshed = await query.refetch();
      } catch {
        return;
      }
      if (
        sessionEpoch.current !== owner.epoch ||
        gate.current.threadId !== threadId ||
        gate.current.session !== session ||
        gate.current.generation !== generation
      ) {
        return;
      }
      if (refreshed.error != null) return;

      const remaining = selectPendingThreadApprovals(refreshed.data, threadId);
      if (remaining.some((approval) => approval.token === owner.token)) return;
      if (owner.action !== 'cancel' && gate.current.cancelled === generation) return;
      if (owner.action !== 'cancel' && gate.current.cancelIntents.size > 0) {
        gate.current.deferred = { generation, remaining, resolvedToken: owner.token };
        return;
      }
      if (owner.action === 'cancel') {
        if (gate.current.settled === generation) return;
        gate.current.settled = generation;
        gate.current.hadPending = remaining.length > 0;
        gate.current.pendingTokens = tokensFor(remaining);
        onFocusRequested(remaining[0]);
        return;
      }
      await finishNonCancel(generation, remaining);
    },
    [finishNonCancel, onFocusRequested, query, session, takeAttempt, threadId],
  );

  return {
    approvals,
    isPending: pendingApprovals.length > 0,
    onResolutionStarted,
    onResolutionFailed,
    onResolved,
  };
}
