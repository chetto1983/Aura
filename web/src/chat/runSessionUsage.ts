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
  readonly eventBaseline: SessionTotals | null | undefined;
  readonly committed: SessionTotals;
  readonly baseline: SessionTotals;
  readonly visible: SessionTotals;
}

export interface RunSessionBaselineEvent {
  readonly runId: number;
  readonly baseline: SessionTotals | null;
}

export function useRunSessionUsage(
  conversationId: string,
  seed: SessionTotals,
  seedReady: boolean,
  event: RunUsageEvent,
  baselineEvent?: RunSessionBaselineEvent,
): SessionTotals {
  const [snapshot, setSnapshot] = useState<RunSessionSnapshot>(() =>
    advanceRunSession(undefined, conversationId, seed, seedReady, event, baselineEvent),
  );
  const next = advanceRunSession(snapshot, conversationId, seed, seedReady, event, baselineEvent);
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
  baselineEvent: RunSessionBaselineEvent | undefined,
): RunSessionSnapshot {
  const eventBaseline = baselineEvent?.runId === event.runId ? baselineEvent.baseline : undefined;
  if (
    previous?.conversationId === conversationId &&
    previous.seedReady === seedReady &&
    sameTotals(previous.seed, seed) &&
    previous.runId === event.runId &&
    previous.phase === event.phase &&
    sameUsage(previous.usage, event.usage) &&
    sameBaseline(previous.eventBaseline, eventBaseline)
  ) {
    return previous;
  }

  const sameConversation = previous?.conversationId === conversationId;
  const priorCommitted = sameConversation ? previous.committed : seedReady ? seed : emptySession();
  const committedSeed = seedReady ? maximumTotals(priorCommitted, seed) : priorCommitted;
  const sameRun = sameConversation && previous.runId === event.runId;
  const eventBaselineChanged = !sameRun || !sameBaseline(previous.eventBaseline, eventBaseline);
  const baseline = eventBaselineChanged
    ? eventBaseline === undefined
      ? committedSeed
      : eventBaseline === null
        ? emptySession()
        : maximumTotals(priorCommitted, eventBaseline)
    : eventBaseline === undefined && !previous.seedReady && seedReady
      ? seed
      : previous.baseline;

  if (eventBaseline === null) {
    const turnOnly =
      event.usage === undefined ? emptySession() : addTurn(emptySession(), event.usage);
    const visible = seedReady ? seed : turnOnly;
    return {
      conversationId,
      seedReady,
      seed,
      runId: event.runId,
      phase: event.phase,
      usage: event.usage,
      eventBaseline: null,
      committed: visible,
      baseline: emptySession(),
      visible,
    };
  }

  if (event.phase === 'running') {
    return {
      conversationId,
      seedReady,
      seed,
      runId: event.runId,
      phase: event.phase,
      usage: event.usage,
      eventBaseline,
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
      eventBaseline,
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
    eventBaseline,
    committed: committedSeed,
    baseline: committedSeed,
    visible: committedSeed,
  };
}

function emptySession(): SessionTotals {
  return {
    promptTokens: 0,
    completionTokens: 0,
    cacheHitTokens: 0,
    costUsd: 0,
    hasCost: false,
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

function sameBaseline(
  left: SessionTotals | null | undefined,
  right: SessionTotals | null | undefined,
): boolean {
  return left === right || (left !== null && right !== null && sameOptionalTotals(left, right));
}

function sameOptionalTotals(
  left: SessionTotals | undefined,
  right: SessionTotals | undefined,
): boolean {
  return left === undefined || right === undefined ? left === right : sameTotals(left, right);
}
