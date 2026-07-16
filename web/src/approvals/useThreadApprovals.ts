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
  readonly recoveryKind?: 'resume' | 'focus';
}

interface RecoveryOutcome {
  readonly attemptId: string;
  readonly generation: number;
  readonly resolvedToken: string;
  readonly kind: 'resume' | 'focus';
}

interface ApprovalGate {
  readonly threadId: string;
  readonly session: object;
  generation: number;
  hadPending: boolean;
  resumed: number;
  cancelled: number;
  settled: number;
  focusedToken: string | undefined;
  readonly recoveries: Map<string, RecoveryOutcome>;
  pendingTokens: Set<string>;
  pendingRows: readonly Approval[];
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
    focusedToken: undefined,
    recoveries: new Map<string, RecoveryOutcome>(),
    pendingTokens: new Set<string>(),
    pendingRows: [],
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
  gate.focusedToken = undefined;
  gate.recoveries.clear();
  gate.pendingTokens = pendingTokens;
  gate.attempts.clear();
  gate.cancelIntents.clear();
  gate.deferred = undefined;
}

function focusPendingOnce(
  gate: ApprovalGate,
  next: Approval | undefined,
  onFocusRequested: (approval: Approval | undefined) => void,
): void {
  if (next === undefined || gate.focusedToken === next.token) return;
  gate.focusedToken = next.token;
  onFocusRequested(next);
}

function clearGenerationRecoveries(gate: ApprovalGate, generation: number): void {
  for (const [attemptId, recovery] of gate.recoveries) {
    if (recovery.generation === generation) gate.recoveries.delete(attemptId);
  }
}

function generationRecoveryKind(
  gate: ApprovalGate,
  generation: number,
): 'resume' | 'focus' | undefined {
  let result: 'resume' | undefined;
  for (const recovery of gate.recoveries.values()) {
    if (recovery.generation !== generation) continue;
    if (recovery.kind === 'focus') return 'focus';
    result = 'resume';
  }
  return result;
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
      current.pendingRows = [];
      current.hadPending = false;
      return;
    }
    if (!current.hadPending || !overlaps(current.pendingTokens, pendingTokens)) {
      rotateGeneration(current, pendingTokens);
      current.pendingRows = pendingApprovals;
      return;
    }
    current.pendingTokens = pendingTokens;
    current.pendingRows = pendingApprovals;
    let recoveredCandidateRemoved = false;
    for (const [attemptId, recovery] of current.recoveries) {
      if (
        recovery.generation === current.generation &&
        !pendingTokens.has(recovery.resolvedToken)
      ) {
        current.recoveries.delete(attemptId);
        recoveredCandidateRemoved = true;
      }
    }
    if (recoveredCandidateRemoved) {
      focusPendingOnce(current, pendingApprovals[0], onFocusRequested);
    }
  }, [onFocusRequested, pendingApprovals, pendingTokens, session, threadId]);

  const finishNonCancel = useCallback(
    async (generation: number, remaining: readonly Approval[]) => {
      const current = gate.current;
      if (
        current.threadId !== threadId ||
        current.session !== session ||
        current.generation !== generation ||
        current.cancelled === generation
      ) {
        return;
      }
      if (remaining.length > 0) {
        current.hadPending = true;
        current.pendingTokens = tokensFor(remaining);
        current.pendingRows = remaining;
        focusPendingOnce(current, remaining[0], onFocusRequested);
        return;
      }
      if (current.settled === generation) return;
      current.settled = generation;
      clearGenerationRecoveries(current, generation);
      current.hadPending = false;
      current.pendingTokens = new Set<string>();
      current.pendingRows = [];
      current.focusedToken = undefined;
      onFocusRequested(undefined);
      if (current.resumed === generation) return;
      current.resumed = generation;
      await onResume();
    },
    [onFocusRequested, onResume, session, threadId],
  );

  const finishCancel = useCallback(
    (generation: number, remaining: readonly Approval[]) => {
      const current = gate.current;
      if (
        current.threadId !== threadId ||
        current.session !== session ||
        current.generation !== generation
      ) {
        return;
      }
      clearGenerationRecoveries(current, generation);
      if (remaining.length > 0) {
        current.hadPending = true;
        current.pendingTokens = tokensFor(remaining);
        current.pendingRows = remaining;
        focusPendingOnce(current, remaining[0], onFocusRequested);
        return;
      }
      if (current.settled === generation) return;
      current.settled = generation;
      current.hadPending = false;
      current.pendingTokens = new Set<string>();
      current.pendingRows = [];
      current.focusedToken = undefined;
      onFocusRequested(undefined);
    },
    [onFocusRequested, session, threadId],
  );

  useLayoutEffect(() => {
    const current = gate.current;
    const generation = current.generation;
    const recoveryKind = generationRecoveryKind(current, generation);
    if (
      pendingApprovals.length !== 0 ||
      current.threadId !== threadId ||
      current.session !== session ||
      recoveryKind === undefined ||
      current.settled === generation
    ) {
      return;
    }
    if (current.cancelIntents.size > 0) {
      current.deferred = { generation, remaining: [], recoveryKind };
      return;
    }
    clearGenerationRecoveries(current, generation);
    if (recoveryKind === 'focus') {
      finishCancel(generation, []);
      return;
    }
    void finishNonCancel(generation, []);
  }, [finishCancel, finishNonCancel, pendingApprovals.length, session, threadId]);

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
      current.recoveries.delete(owner.attemptId);
      if (
        deferred.resolvedToken !== undefined &&
        deferred.remaining.some((approval) => approval.token === deferred.resolvedToken)
      ) {
        return;
      }
      if (deferred.recoveryKind === 'focus') finishCancel(owner.generation, deferred.remaining);
      else await finishNonCancel(owner.generation, deferred.remaining);
    },
    [finishCancel, finishNonCancel, takeAttempt],
  );

  const onResolved = useCallback(
    async (attempt: ApprovalResolution) => {
      const owner = takeAttempt(attempt);
      if (owner === undefined) return;
      const current = gate.current;
      const generation = owner.generation;
      if (owner.action === 'cancel') {
        current.cancelled = generation;
        clearGenerationRecoveries(current, generation);
        current.cancelIntents.clear();
        current.attempts.clear();
        current.deferred = undefined;
      }

      const installRecovery = (): void => {
        const active = gate.current;
        if (
          sessionEpoch.current !== owner.epoch ||
          active.threadId !== threadId ||
          active.session !== session ||
          active.generation !== generation ||
          active.settled === generation ||
          (owner.action !== 'cancel' && active.cancelled === generation)
        ) {
          return;
        }
        if (active.pendingTokens.size > 0 && !active.pendingTokens.has(owner.token)) {
          focusPendingOnce(active, active.pendingRows[0], onFocusRequested);
          return;
        }
        const recovery: RecoveryOutcome = {
          attemptId: owner.attemptId,
          generation,
          resolvedToken: owner.token,
          kind: owner.action === 'cancel' ? 'focus' : 'resume',
        };
        active.recoveries.set(owner.attemptId, recovery);
        if (active.pendingTokens.size !== 0) return;
        if (active.cancelIntents.size > 0) {
          active.deferred = { generation, remaining: [], recoveryKind: recovery.kind };
          return;
        }
        clearGenerationRecoveries(active, generation);
        if (recovery.kind === 'focus') finishCancel(generation, []);
        else void finishNonCancel(generation, []);
      };

      let refreshed: Awaited<ReturnType<typeof query.refetch>>;
      try {
        refreshed = await query.refetch();
      } catch {
        installRecovery();
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
      if (refreshed.error != null) {
        installRecovery();
        return;
      }

      const remaining = selectPendingThreadApprovals(refreshed.data, threadId);
      if (remaining.some((approval) => approval.token === owner.token)) {
        installRecovery();
        return;
      }
      const remainingTokens = tokensFor(remaining);
      if (remaining.length > 0 && !overlaps(gate.current.pendingTokens, remainingTokens)) {
        rotateGeneration(gate.current, remainingTokens);
        gate.current.pendingRows = remaining;
        focusPendingOnce(gate.current, remaining[0], onFocusRequested);
        return;
      }
      gate.current.recoveries.delete(owner.attemptId);
      if (owner.action !== 'cancel' && gate.current.cancelled === generation) return;
      if (owner.action !== 'cancel' && gate.current.cancelIntents.size > 0) {
        gate.current.deferred = { generation, remaining, resolvedToken: owner.token };
        return;
      }
      if (owner.action === 'cancel') {
        finishCancel(generation, remaining);
        return;
      }
      await finishNonCancel(generation, remaining);
    },
    [finishCancel, finishNonCancel, onFocusRequested, query, session, takeAttempt, threadId],
  );

  return {
    approvals,
    isPending: pendingApprovals.length > 0,
    onResolutionStarted,
    onResolutionFailed,
    onResolved,
  };
}
