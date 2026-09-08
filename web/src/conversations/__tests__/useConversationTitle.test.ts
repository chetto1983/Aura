import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement, type ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useConversationTitle } from '../useConversationTitle';
import { useConversation, useConversations } from '../useConversations';

function wrapper(client: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client }, children);
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('asynchronous conversation titles', () => {
  it.each(['Generated title', 'A newer manual title'])(
    'refreshes sidebar and detail with authoritative %s',
    async (storedTitle) => {
      vi.useFakeTimers();
      let ready = false;
      let titleReads = 0;
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL) => {
          const path =
            typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
          if (path.endsWith('/title')) {
            titleReads++;
            if (titleReads === 1) return Promise.resolve(new Response('', { status: 404 }));
            ready = true;
            return Promise.resolve(Response.json({ title: 'Generated title' }));
          }
          const row = { ID: 'c-1', Title: ready ? storedTitle : '', TitleSet: ready };
          return Promise.resolve(Response.json(path === '/api/conversations' ? [row] : row));
        }),
      );
      const { result, unmount } = renderHook(
        () => {
          const detail = useConversation('c-1');
          const list = useConversations(false);
          useConversationTitle('c-1', detail.data?.TitleSet === false);
          return { detail, list };
        },
        { wrapper: wrapper(client) },
      );
      await act(async () => {
        await vi.advanceTimersByTimeAsync(100);
      });
      expect(result.current.list.data?.[0]?.Title).toBe('');
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5100);
      });
      expect(result.current.list.data?.[0]?.Title).toBe(storedTitle);
      expect(result.current.detail.data?.Title).toBe(storedTitle);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(45000);
      });
      expect(titleReads).toBe(2);
      unmount();
      client.clear();
    },
  );

  it.each([
    { status: 404, calls: 9 },
    { status: 401, calls: 1 },
    { status: 500, calls: 1 },
  ])('bounds retries for HTTP$status', async ({ status, calls }) => {
    vi.useFakeTimers();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const fetch = vi.fn(() => Promise.resolve(new Response('', { status })));
    vi.stubGlobal('fetch', fetch);
    const { result, unmount } = renderHook(
      () => ({ isError: useConversationTitle('c-1', true).isError }),
      {
        wrapper: wrapper(client),
      },
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50000);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(fetch).toHaveBeenCalledTimes(calls);
    expect(result.current.isError).toBe(true);
    unmount();
    client.clear();
  });

  it('does not request a title for an empty or inactive conversation', async () => {
    const client = new QueryClient();
    const fetch = vi.fn();
    vi.stubGlobal('fetch', fetch);
    const { unmount } = renderHook(
      () => {
        useConversationTitle('', true);
        useConversationTitle('c-1', false);
      },
      { wrapper: wrapper(client) },
    );
    await act(async () => {
      await Promise.resolve();
    });
    expect(fetch).not.toHaveBeenCalled();
    unmount();
    client.clear();
  });
});
