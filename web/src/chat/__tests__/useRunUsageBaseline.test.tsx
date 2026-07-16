import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { RUN_USAGE_BASELINE_TIMEOUT_MS, useRunUsageBaseline } from '../useRunUsageBaseline';

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function conversationResponse(): Response {
  return new Response(
    JSON.stringify({
      ID: 'conv-1',
      Title: 'Existing',
      TitleSet: true,
      IdentityID: 'operator',
      Status: 'active',
      Model: 'test',
      TotalInputTokens: 1000,
      TotalOutputTokens: 500,
      TotalCachedTokens: 400,
      TotalCostUSD: 0.05,
      CreatedAt: '2026-07-16T00:00:00Z',
    }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

describe('useRunUsageBaseline cleanup', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('cleans its timeout and listener and ignores a late detail resolution', async () => {
    vi.useFakeTimers();
    const detail = deferred<Response>();
    let querySignal: AbortSignal | undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        querySignal = init?.signal ?? undefined;
        return detail.promise;
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: Infinity } },
    });
    const onBaseline = vi.fn();
    const controller = new AbortController();
    const addListener = vi.spyOn(controller.signal, 'addEventListener');
    const removeListener = vi.spyOn(controller.signal, 'removeEventListener');
    const wrapper = ({ children }: { readonly children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useRunUsageBaseline(onBaseline), { wrapper });
    let completed: boolean | undefined;

    act(() => {
      void result.current('conv-1', 7, controller.signal).then((value) => {
        completed = value;
      });
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(RUN_USAGE_BASELINE_TIMEOUT_MS);
      await Promise.resolve();
    });

    expect(completed).toBe(true);
    expect(onBaseline).toHaveBeenCalledTimes(1);
    expect(onBaseline).toHaveBeenCalledWith({ runId: 7, baseline: null });
    expect(querySignal?.aborted).toBe(true);
    expect(removeListener.mock.calls.filter(([type]) => type === 'abort')).toHaveLength(
      addListener.mock.calls.filter(([type]) => type === 'abort').length,
    );
    expect(vi.getTimerCount()).toBe(0);

    detail.resolve(conversationResponse());
    await act(async () => {
      await Promise.resolve();
      await vi.runAllTimersAsync();
    });
    controller.abort();
    expect(onBaseline).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
  });
});
