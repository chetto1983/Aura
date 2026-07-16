import { useState } from 'react';
import { addTurn, type SessionTotals } from './footerMetrics';
import type { RunUsageEvent } from './runUsage';
import type { TurnUsage } from './sseAdapter';

interface RunSessionSnapshot {
  readonly conversationId: string;
  readonly seedReady: boolean;
  readonly seed: SessionTotals;
  readonly runId: number;
  readonly phase: RunUsageEvent['phase'];
  readonly usage: TurnUsage | undefined;
  readonly committed: SessionTotals;
  readonly baseline: SessionTotals;
  readonly visible: SessionTotals;
}

export function useRunSessionUsage(
  conversationId: string,
  seed: SessionTotals,
  seedReady: boolean,
  event: RunUsageEvent,
): SessionTotals {
  const [snapshot, setSnapshot] = useState<RunSessionSnapshot>(() =>
    advanceRunSession(undefined, conversationId, seed, seedReady, event),
  );
  const next = advanceRunSession(snapshot, conversationId, seed, seedReady, event);
  if (next !== snapshot) {
    setSnapshot(next);
    return next.visible;
  }
  return snapshot.visible;
}

function advanceRunSession(
  previous: RunSessionSnapshot | undefined,
  conversationId: string,
  seed: SessionTotals,
  seedReady: boolean,
  event: RunUsageEvent,
): RunSessionSnapshot {
  if (
    previous?.conversationId === conversationId &&
    previous.seedReady === seedReady &&
    sameTotals(previous.seed, seed) &&
    previous.runId === event.runId &&
    previous.phase === event.phase &&
    sameUsage(previous.usage, event.usage)
  ) {
    return previous;
  }

  const sameConversation = previous?.conversationId === conversationId;
  const priorCommitted = sameConversation ? previous.committed : seed;
  const committedSeed = maximumTotals(priorCommitted, seed);
  const sameRun = sameConversation && previous.runId === event.runId;
  const baseline = sameRun
    ? !previous.seedReady && seedReady
      ? seed
      : previous.baseline
    : committedSeed;

  if (event.phase === 'running') {
    return {
      conversationId,
      seedReady,
      seed,
      runId: event.runId,
      phase: event.phase,
      usage: event.usage,
      committed: committedSeed,
      baseline,
      visible: event.usage === undefined ? baseline : addTurn(baseline, event.usage),
    };
  }

  if (event.phase === 'settled') {
    const finalized = event.usage === undefined ? baseline : addTurn(baseline, event.usage);
    const committed = maximumTotals(committedSeed, finalized);
    return {
      conversationId,
      seedReady,
      seed,
      runId: event.runId,
      phase: event.phase,
      usage: event.usage,
      committed,
      baseline,
      visible: committed,
    };
  }

  return {
    conversationId,
    seedReady,
    seed,
    runId: event.runId,
    phase: event.phase,
    usage: undefined,
    committed: committedSeed,
    baseline: committedSeed,
    visible: committedSeed,
  };
}

function maximumTotals(left: SessionTotals, right: SessionTotals): SessionTotals {
  return {
    promptTokens: Math.max(left.promptTokens, right.promptTokens),
    completionTokens: Math.max(left.completionTokens, right.completionTokens),
    cacheHitTokens: Math.max(left.cacheHitTokens, right.cacheHitTokens),
    costUsd: Math.max(left.costUsd, right.costUsd),
    hasCost: left.hasCost || right.hasCost,
  };
}

function sameTotals(left: SessionTotals, right: SessionTotals): boolean {
  return (
    left.promptTokens === right.promptTokens &&
    left.completionTokens === right.completionTokens &&
    left.cacheHitTokens === right.cacheHitTokens &&
    left.costUsd === right.costUsd &&
    left.hasCost === right.hasCost
  );
}

function sameUsage(left: TurnUsage | undefined, right: TurnUsage | undefined): boolean {
  return (
    left === right ||
    (left?.promptTokens === right?.promptTokens &&
      left?.completionTokens === right?.completionTokens &&
      left?.cacheHitTokens === right?.cacheHitTokens &&
      left?.costUsd === right?.costUsd)
  );
}
