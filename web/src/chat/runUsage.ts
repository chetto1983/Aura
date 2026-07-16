import { useCallback, useMemo, useRef } from 'react';
import type { TurnUsage } from './sseAdapter';

export type RunUsagePhase = 'idle' | 'running' | 'settled';

export interface RunUsageEvent {
  readonly runId: number;
  readonly phase: RunUsagePhase;
  readonly usage: TurnUsage | undefined;
}

interface ActiveRunUsage {
  readonly runId: number;
  usage: TurnUsage | undefined;
  settled: boolean;
}

export function useRunUsageLifecycle(
  onUsage?: (event: RunUsageEvent) => void,
  allocateRunId?: () => number,
) {
  const sequence = useRef(0);
  const active = useRef<ActiveRunUsage | null>(null);

  const start = useCallback(() => {
    const runId = allocateRunId?.() ?? ++sequence.current;
    sequence.current = Math.max(sequence.current, runId);
    active.current = { runId, usage: undefined, settled: false };
    onUsage?.({ runId, phase: 'running', usage: undefined });
    return runId;
  }, [allocateRunId, onUsage]);

  const update = useCallback(
    (runId: number, usage: TurnUsage | undefined) => {
      if (active.current?.runId !== runId || active.current.settled) return;
      if (usage === undefined || sameUsage(active.current.usage, usage)) return;
      active.current.usage = usage;
      onUsage?.({ runId, phase: 'running', usage });
    },
    [onUsage],
  );

  const settle = useCallback(
    (runId: number) => {
      if (active.current?.runId !== runId || active.current.settled) return;
      active.current.settled = true;
      onUsage?.({ runId, phase: 'settled', usage: active.current.usage });
    },
    [onUsage],
  );

  const clear = useCallback(() => {
    const runId = active.current?.runId ?? sequence.current;
    active.current = null;
    onUsage?.({ runId, phase: 'idle', usage: undefined });
  }, [onUsage]);

  return useMemo(() => ({ start, update, settle, clear }), [clear, settle, start, update]);
}

function sameUsage(left: TurnUsage | undefined, right: TurnUsage): boolean {
  return (
    left !== undefined &&
    Object.is(left.promptTokens, right.promptTokens) &&
    Object.is(left.completionTokens, right.completionTokens) &&
    Object.is(left.cacheHitTokens, right.cacheHitTokens) &&
    Object.is(left.costUsd, right.costUsd)
  );
}
