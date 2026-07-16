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

export function useRunUsageLifecycle(onUsage?: (event: RunUsageEvent) => void) {
  const sequence = useRef(0);
  const active = useRef<ActiveRunUsage | null>(null);

  const start = useCallback(() => {
    const runId = ++sequence.current;
    active.current = { runId, usage: undefined, settled: false };
    onUsage?.({ runId, phase: 'running', usage: undefined });
    return runId;
  }, [onUsage]);

  const update = useCallback(
    (runId: number, usage: TurnUsage | undefined) => {
      if (active.current?.runId !== runId || active.current.settled) return;
      if (usage !== undefined) active.current.usage = usage;
      onUsage?.({ runId, phase: 'running', usage: active.current.usage });
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
