import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { formatElapsed, useElapsed } from '../durationFormat';

if (i18n.language !== 'en') void i18n.changeLanguage('en');

// durationFormat — dedicated mutation net for the shared elapsed formatter + live
// ticker hook (compact-chat spec §5.3): the pill/tool/group consumers exercise only
// the happy path, so every branch is pinned here.

const t = i18n.getFixedT('en');

describe('formatElapsed', () => {
  it('renders sub-10s with two fraction digits', () => {
    expect(formatElapsed(1280, 'en', t)).toBe('1.28 s');
  });

  it('renders 10-59s with whole seconds', () => {
    expect(formatElapsed(47_000, 'en', t)).toBe('47 s');
  });

  it('clamps negative input to zero, never NaN or negatives', () => {
    expect(formatElapsed(-500, 'en', t)).toBe('0 s');
  });

  it('rolls to the minutes template at exactly 60s', () => {
    expect(formatElapsed(60_000, 'en', t)).toBe('1 min 0 s');
  });

  it('floors minutes and residual seconds', () => {
    expect(formatElapsed(3 * 60_000 + 42_500, 'en', t)).toBe('3 min 42 s');
  });
});

describe('useElapsed', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-07-23T12:00:10.000Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const t0 = Date.parse('2026-07-23T12:00:00.000Z');

  it('returns null without a start timestamp', () => {
    const { result } = renderHook(() => useElapsed(undefined, undefined, true));
    expect(result.current).toBeNull();
  });

  it('uses finishedAt as the authoritative endpoint when present', () => {
    const { result } = renderHook(() => useElapsed(t0, t0 + 2_000, false));
    expect(result.current).toBe('2 s');
  });

  it('ticks while running and stops advancing after settlement', () => {
    const { result, rerender } = renderHook(
      ({ running }: { running: boolean }) => useElapsed(t0, undefined, running),
      { initialProps: { running: true } },
    );
    expect(result.current).toBe('10 s');
    act(() => {
      vi.advanceTimersByTime(3_000);
    });
    expect(result.current).toBe('13 s');
    // Settlement without finishedAt captures the settle instant once.
    rerender({ running: false });
    act(() => {
      vi.advanceTimersByTime(0); // flush the queued microtask
    });
    const settled = result.current;
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    expect(result.current).toBe(settled);
  });
});
