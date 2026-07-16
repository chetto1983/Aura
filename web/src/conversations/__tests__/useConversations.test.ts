import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement, type ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  autoTitleFromPrompt,
  CONVERSATION_KEY,
  displayTitle,
  isArchived,
  useArchiveConversation,
  useCreateConversation,
  useConversationRotEvents,
  useConversationSearch,
  useConversations,
  useDeleteConversation,
  useRenameConversation,
  useUnarchiveConversation,
  type Conversation,
} from '../useConversations';

function wrapper(client = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return createElement(QueryClientProvider, { client }, children);
  };
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

const CONV: Conversation = {
  ID: 'c-1',
  Title: 'Run',
  TitleSet: true,
  IdentityID: 'op',
  Status: 'active',
  Model: 'm',
  TotalInputTokens: 1,
  TotalOutputTokens: 2,
  TotalCachedTokens: 3,
  TotalCostUSD: 0.01,
  CreatedAt: '2026-06-17T00:00:00Z',
};

describe('useConversations pure helpers', () => {
  it('isArchived reflects the status', () => {
    expect(isArchived(CONV)).toBe(false);
    expect(isArchived({ ...CONV, Status: 'archived' })).toBe(true);
  });
  it('displayTitle falls back to the untitled label when unset', () => {
    expect(displayTitle(CONV, 'Untitled')).toBe('Run');
    expect(displayTitle({ ...CONV, Title: '', TitleSet: false }, 'Untitled')).toBe('Untitled');
  });
  it('autoTitleFromPrompt compacts first-message text for a conversation title', () => {
    expect(autoTitleFromPrompt('  Deploy rollback plan?  ')).toBe('Deploy rollback plan');
    expect(autoTitleFromPrompt('line one\nline two')).toBe('line one line two');
    expect(autoTitleFromPrompt('x'.repeat(90))).toHaveLength(83);
  });
});

describe('useConversations queries', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lists same-origin and appends ?archived=true when requested', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify([CONV]), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useConversations(true), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.data).toBeDefined();
    });
    expect(fetchMock).toHaveBeenCalledWith('/api/conversations?archived=true', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('flags isError on a non-2xx list (the !res.ok throw path)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('nope', { status: 500 }))),
    );
    const { result } = renderHook(() => useConversations(false), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.isError).toBe(true);
    });
  });

  it('does not fetch search for an empty/whitespace query (disabled)', () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response('[]', { status: 200 })));
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useConversationSearch('   '), { wrapper: wrapper() });
    // Disabled query: no fetch, no data.
    expect(result.current.fetchStatus).toBe('idle');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('searches with the trimmed, encoded query', async () => {
    const seen: string[] = [];
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      seen.push(urlOf(input));
      return Promise.resolve(new Response('[]', { status: 200 }));
    });
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useConversationSearch(' a b '), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.data).toBeDefined();
    });
    expect(seen.some((u) => u.includes('q=a%20b'))).toBe(true);
  });

  it('reads rot-events for a non-empty conversation id', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify([
            {
              TS: '2026-06-17T00:00:00Z',
              Action: 'hard_drop_pairs',
              PairsDropped: 2,
              TokensBefore: 100,
              TokensAfter: 60,
            },
          ]),
          { status: 200 },
        ),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useConversationRotEvents('c-1'), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.data?.length).toBe(1);
    });
    expect(fetchMock).toHaveBeenCalledWith('/api/conversations/c-1/rot-events', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('does not fetch rot-events for an empty id (disabled)', () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response('[]', { status: 200 })));
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useConversationRotEvents(''), { wrapper: wrapper() });
    expect(result.current.fetchStatus).toBe('idle');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('useConversations mutations', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('rename POSTs the title body and resolves on 204', async () => {
    let captured: { url: string; init?: RequestInit } | undefined;
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      captured = { url: urlOf(input), ...(init ? { init } : {}) };
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useRenameConversation(), { wrapper: wrapper() });
    result.current.mutate({ id: 'c-1', title: 'New' });
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(captured?.url).toBe('/api/conversations/c-1/rename');
    expect(captured?.init?.method).toBe('POST');
    expect(captured?.init?.body).toBe(JSON.stringify({ title: 'New' }));
  });

  it('create POSTs an automatic title derived from the first prompt', async () => {
    let captured: { url: string; init?: RequestInit } | undefined;
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      captured = { url: urlOf(input), ...(init ? { init } : {}) };
      return Promise.resolve(new Response(JSON.stringify(CONV), { status: 201 }));
    });
    vi.stubGlobal('fetch', fetchMock);
    const { result } = renderHook(() => useCreateConversation(), { wrapper: wrapper() });
    result.current.mutate('  Deploy rollback plan?  ');
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(captured?.url).toBe('/api/conversations');
    expect(captured?.init?.method).toBe('POST');
    expect(captured?.init?.body).toBe(JSON.stringify({ title: 'Deploy rollback plan' }));
  });

  it('seeds the created conversation detail as the first-run usage baseline', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(JSON.stringify(CONV), { status: 201 }))),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useCreateConversation(), { wrapper: wrapper(client) });

    result.current.mutate('first prompt');
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });

    expect(client.getQueryData([CONVERSATION_KEY, CONV.ID])).toEqual(CONV);
  });

  it('archive/unarchive/delete hit their routes and surface errors on non-2xx', async () => {
    // First a happy archive, then a failing delete (the mutate !res.ok throw path).
    const seen: string[] = [];
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      seen.push(urlOf(input));
      if ((init?.method ?? 'GET') === 'DELETE') {
        return Promise.resolve(new Response('boom', { status: 500 }));
      }
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    vi.stubGlobal('fetch', fetchMock);

    const archive = renderHook(() => useArchiveConversation(), { wrapper: wrapper() });
    archive.result.current.mutate('c-1');
    await waitFor(() => {
      expect(archive.result.current.isSuccess).toBe(true);
    });

    const unarchive = renderHook(() => useUnarchiveConversation(), { wrapper: wrapper() });
    unarchive.result.current.mutate('c-1');
    await waitFor(() => {
      expect(unarchive.result.current.isSuccess).toBe(true);
    });

    const remove = renderHook(() => useDeleteConversation(), { wrapper: wrapper() });
    remove.result.current.mutate('c-1');
    await waitFor(() => {
      expect(remove.result.current.isError).toBe(true);
    });
    expect(seen.some((u) => u.endsWith('/c-1/archive'))).toBe(true);
    expect(seen.some((u) => u.endsWith('/c-1/unarchive'))).toBe(true);
    expect(seen.some((u) => u.endsWith('/api/conversations/c-1'))).toBe(true);
  });
});
