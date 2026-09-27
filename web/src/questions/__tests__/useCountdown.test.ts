import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { formatRemaining, secondsLeft, useCountdown } from '../useCountdown';

describe('useCountdown', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('counts down each second and stops at zero', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-25T10:00:00Z'));
    const { result } = renderHook(() => useCountdown('2026-09-25T10:00:02Z'));
    expect(result.current).toBe(2);
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(result.current).toBe(0);
  });

  it('reads an unparseable deadline as already passed, and shows m:ss', () => {
    expect(secondsLeft('never', Date.now())).toBe(0);
    expect(formatRemaining(300)).toBe('5:00');
    expect(formatRemaining(59)).toBe('0:59');
  });
});
