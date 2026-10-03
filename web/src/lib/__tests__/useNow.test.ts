import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useNow } from '../useNow';

describe('useNow', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('re-reads the clock once per interval and not in between', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-10-03T10:00:00Z'));
    const start = Date.now();
    const { result } = renderHook(() => useNow(60_000));
    expect(result.current).toBe(start);

    act(() => {
      vi.advanceTimersByTime(59_999);
    });
    expect(result.current).toBe(start);

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe(start + 60_000);
  });

  it('stops ticking on unmount', () => {
    vi.useFakeTimers();
    const { unmount } = renderHook(() => useNow(1000));
    expect(vi.getTimerCount()).toBe(1);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
