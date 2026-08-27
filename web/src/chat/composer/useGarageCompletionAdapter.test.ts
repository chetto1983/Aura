import type { Unstable_TriggerItem } from '@assistant-ui/react';
import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useGarageCompletionAdapter } from './useGarageCompletionAdapter';

const ITEM: Unstable_TriggerItem = {
  id: 'file:finance/q1.pdf',
  type: 'garage-file',
  label: 'q1.pdf',
};

async function search(
  adapter: { search?: (query: string) => readonly Unstable_TriggerItem[] },
  query: string,
) {
  let items: readonly Unstable_TriggerItem[] = [];
  await act(async () => {
    items = adapter.search?.(query) ?? [];
    await Promise.resolve();
  });
  return items;
}

describe('Garage async TriggerAdapter bridge', () => {
  afterEach(() => vi.useRealTimers());

  it('debounces one query and publishes its result through the synchronous adapter', async () => {
    vi.useFakeTimers();
    const fetcher = vi.fn(() => Promise.resolve([ITEM]));
    const { result } = renderHook(() => useGarageCompletionAdapter(fetcher, true));

    expect(await search(result.current.adapter, 'finance/q')).toEqual([]);
    expect(result.current.isLoading).toBe(true);
    await act(async () => vi.advanceTimersByTimeAsync(59));
    expect(fetcher).not.toHaveBeenCalled();
    await act(async () => vi.advanceTimersByTimeAsync(1));

    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher).toHaveBeenCalledWith('finance/q');
    expect(result.current.adapter.search?.('finance/q')).toEqual([ITEM]);
    expect(result.current.isLoading).toBe(false);
  });

  it('cancels an in-flight different query when the operator returns to the cached one', async () => {
    vi.useFakeTimers();
    let resolveSecond: ((items: readonly Unstable_TriggerItem[]) => void) | undefined;
    const fetcher = vi
      .fn<(query: string) => Promise<readonly Unstable_TriggerItem[]>>()
      .mockResolvedValueOnce([ITEM])
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            resolveSecond = resolve;
          }),
      );
    const { result } = renderHook(() => useGarageCompletionAdapter(fetcher, true, 0));

    await search(result.current.adapter, 'finance/q1');
    await act(async () => vi.advanceTimersByTimeAsync(0));
    expect(result.current.adapter.search?.('finance/q1')).toEqual([ITEM]);

    await search(result.current.adapter, 'finance/q2');
    await act(async () => vi.advanceTimersByTimeAsync(0));
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(await search(result.current.adapter, 'finance/q1')).toEqual([ITEM]);
    act(() => resolveSecond?.([{ ...ITEM, id: 'file:finance/q2.pdf' }]));

    expect(result.current.adapter.search?.('finance/q1')).toEqual([ITEM]);
  });
});
