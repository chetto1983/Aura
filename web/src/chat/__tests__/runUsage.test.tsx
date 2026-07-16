import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { useRunUsageLifecycle, type RunUsageEvent } from '../runUsage';
import type { TurnUsage } from '../sseAdapter';

function usage(promptTokens: number): TurnUsage {
  return {
    promptTokens,
    completionTokens: promptTokens / 2,
    cacheHitTokens: promptTokens / 4,
    costUsd: promptTokens / 100_000,
  };
}

describe('useRunUsageLifecycle', () => {
  it('resets every run and settles it exactly once with its own last defined usage', () => {
    const events: RunUsageEvent[] = [];
    const { result } = renderHook(() =>
      useRunUsageLifecycle((event) => {
        events.push(event);
      }),
    );

    act(() => {
      const first = result.current.start();
      result.current.update(first, usage(10));
      result.current.update(first, undefined);
      result.current.settle(first);
      result.current.settle(first);
      const second = result.current.start();
      result.current.settle(second);
    });

    expect(events).toEqual([
      { runId: 1, phase: 'running', usage: undefined },
      { runId: 1, phase: 'running', usage: usage(10) },
      { runId: 1, phase: 'running', usage: usage(10) },
      { runId: 1, phase: 'settled', usage: usage(10) },
      { runId: 2, phase: 'running', usage: undefined },
      { runId: 2, phase: 'settled', usage: undefined },
    ]);
  });

  it('ignores stale and settled updates, then clears the current sequence', () => {
    const onUsage = vi.fn<(event: RunUsageEvent) => void>();
    const { result } = renderHook(() => useRunUsageLifecycle(onUsage));

    act(() => {
      const oldRun = result.current.start();
      const currentRun = result.current.start();
      result.current.update(oldRun, usage(999));
      result.current.update(currentRun, usage(20));
      result.current.settle(currentRun);
      result.current.update(currentRun, usage(888));
      result.current.clear();
    });

    expect(onUsage.mock.calls.map(([event]) => event)).toEqual([
      { runId: 1, phase: 'running', usage: undefined },
      { runId: 2, phase: 'running', usage: undefined },
      { runId: 2, phase: 'running', usage: usage(20) },
      { runId: 2, phase: 'settled', usage: usage(20) },
      { runId: 2, phase: 'idle', usage: undefined },
    ]);
  });

  it('returns stable lifecycle callbacks when the listener is unchanged', () => {
    const onUsage = vi.fn<(event: RunUsageEvent) => void>();
    const { result, rerender } = renderHook(() => useRunUsageLifecycle(onUsage));
    const lifecycle = result.current;

    rerender();

    expect(result.current).toBe(lifecycle);
  });
});
