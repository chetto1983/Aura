import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import { createElement, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useCompactionHistory, useRestoreCompaction } from '../useCompactions';

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client }, children);
}

describe('useCompactions', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('fetches bounded owner-scoped history with encoded id', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
      Promise.resolve(
        new Response(
          JSON.stringify({ status: 'disabled', activated: false, entries: [], input, init }),
          { status: 200 },
        ),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useCompactionHistory('c/1'), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/conversations/c%2F1/compact/history');
  });

  it('restore always declares a safe point', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      void input;
      void init;
      return Promise.resolve(
        new Response(JSON.stringify({ status: 'recovered', activated: false }), { status: 200 }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useRestoreCompaction('c-1'), { wrapper: wrapper() });
    result.current.mutate('cp-1');
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(fetchMock.mock.calls[0]?.[1]?.body).toContain('"safePoint":true');
  });
});
