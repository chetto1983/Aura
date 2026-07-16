import { useCallback, useRef, useState } from 'react';
import type { RunUsageEvent } from './runUsage';

const INITIAL_USAGE_STATE: RunUsageEvent = {
  runId: 0,
  phase: 'idle',
  usage: undefined,
};

export function useRunUsageOwner() {
  const sequence = useRef(0);
  const [usageState, setUsageState] = useState<RunUsageEvent>(INITIAL_USAGE_STATE);

  const allocateUsageRunId = useCallback(() => ++sequence.current, []);

  const acceptUsage = useCallback((event: RunUsageEvent) => {
    sequence.current = Math.max(sequence.current, event.runId);
    setUsageState((current) => (event.runId < current.runId ? current : event));
  }, []);

  const resetUsage = useCallback(() => {
    const runId = ++sequence.current;
    setUsageState({ runId, phase: 'idle', usage: undefined });
  }, []);

  return { usageState, allocateUsageRunId, acceptUsage, resetUsage };
}
