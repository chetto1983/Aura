import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useCopyAction } from '../useCopyAction';

// useCopyAction: best-effort clipboard write + a transient `copied` flag. The suite
// pins all branches — success/flash/revert, write-rejection (swallowed, no flash),
// and the absent-clipboard no-op.

describe('useCopyAction', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('writes the text and flips `copied`, then reverts after the confirm window', async () => {
    vi.useFakeTimers();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });

    const { result } = renderHook(() => useCopyAction());
    expect(result.current.copied).toBe(false);

    await act(async () => {
      result.current.copy('hello');
      await Promise.resolve(); // let the resolved write settle into flash()
    });
    expect(writeText).toHaveBeenCalledWith('hello');
    expect(result.current.copied).toBe(true);

    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(result.current.copied).toBe(false);
  });

  describe('with a rejecting clipboard', () => {
    beforeEach(() => {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) },
      });
    });

    it('swallows a write rejection and never flips `copied`', async () => {
      const { result } = renderHook(() => useCopyAction());
      await act(async () => {
        result.current.copy('x');
        await Promise.resolve();
      });
      await waitFor(() => {
        expect(result.current.copied).toBe(false);
      });
    });
  });

  it('is a no-op when navigator.clipboard is unavailable', () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
    const { result } = renderHook(() => useCopyAction());
    act(() => {
      result.current.copy('x');
    });
    expect(result.current.copied).toBe(false);
  });
});
