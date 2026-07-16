import { useCallback, useRef, useState } from 'react';
import type { RunUsageEvent } from './runUsage';
import type { RunSessionBaselineEvent } from './runSessionUsage';

const INITIAL_USAGE_STATE: RunUsageEvent = {
  runId: 0,
  phase: 'idle',
  usage: undefined,
};

export function useRunUsageOwner() {
  const sequence = useRef(0);
  const [usageState, setUsageState] = useState<RunUsageEvent>(INITIAL_USAGE_STATE);
  const [sessionBaseline, setSessionBaseline] = useState<RunSessionBaselineEvent>();

  const allocateUsageRunId = useCallback(() => ++sequence.current, []);

  const acceptUsage = useCallback((event: RunUsageEvent) => {
    sequence.current = Math.max(sequence.current, event.runId);
    setUsageState((current) => (event.runId < current.runId ? current : event));
  }, []);

  const acceptUsageBaseline = useCallback((event: RunSessionBaselineEvent) => {
    if (event.runId < sequence.current) return;
    setSessionBaseline(event);
  }, []);

  const resetUsage = useCallback(() => {
    const runId = ++sequence.current;
    setUsageState({ runId, phase: 'idle', usage: undefined });
    setSessionBaseline(undefined);
  }, []);

  return {
    usageState,
    sessionBaseline,
    allocateUsageRunId,
    acceptUsage,
    acceptUsageBaseline,
    resetUsage,
  };
}
