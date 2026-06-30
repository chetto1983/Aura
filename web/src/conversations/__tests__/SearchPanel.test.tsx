import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import '../../i18n/i18n';
import { SearchPanel } from '../SearchPanel';
import { highlightSegments } from '../searchHighlight';
import type { Conversation, SearchResult } from '../useConversations';

// Capture navigate() calls so the /c/:id deep-link assertion is exact.
const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

const CONVERSATIONS: Conversation[] = [
  {
    ID: 'c-1',
    Title: 'Weather investigation',
    TitleSet: true,
    IdentityID: 'op-1',
    Status: 'active',
    Model: 'deepseek-v4',
    TotalInputTokens: 0,
    TotalOutputTokens: 0,
    TotalCachedTokens: 0,
    TotalCostUSD: 0,
    CreatedAt: '2026-06-17T10:00:00Z',
  },
];

const HITS: SearchResult[] = [
  { ConversationID: 'c-1', Seq: 4, Content: 'the meteo report says sunny', Similarity: 0.9 },
];

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function stubFetch(searchBody: SearchResult[]) {
  return vi.fn((input: RequestInfo | URL) => {
    const url = urlOf(input);
    if (url.includes('/api/conversations/search')) {
      return Promise.resolve(new Response(JSON.stringify(searchBody), { status: 200 }));
    }
    // The conversation list (title enrichment).
    return Promise.resolve(new Response(JSON.stringify(CONVERSATIONS), { status: 200 }));
  });
}

function renderPanel(onOpen = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
  render(<SearchPanel onOpen={onOpen} />, { wrapper: Wrapper });
  return onOpen;
}

describe('highlightSegments', () => {
  it('splits the snippet around the match (case-insensitive) into safe segments', () => {
    const segs = highlightSegments('The Meteo report', 'meteo');
    expect(segs).toEqual([
      { text: 'The ', match: false },
      { text: 'Meteo', match: true },
      { text: ' report', match: false },
    ]);
  });
  it('returns the whole content as one segment for an empty query', () => {
    expect(highlightSegments('hello', '')).toEqual([{ text: 'hello', match: false }]);
  });
  it('returns the whole content unmatched when the query is absent', () => {
    expect(highlightSegments('hello world', 'zzz')).toEqual([
      { text: 'hello world', match: false },
    ]);
  });
  it('handles a match at the very start (no leading segment)', () => {
    expect(highlightSegments('meteo today', 'meteo')).toEqual([
      { text: 'meteo', match: true },
      { text: ' today', match: false },
    ]);
  });
  it('handles back-to-back matches (no gap segment between)', () => {
    expect(highlightSegments('aa', 'a')).toEqual([
      { text: 'a', match: true },
      { text: 'a', match: true },
    ]);
  });
  it('handles a trailing match (no trailing unmatched segment)', () => {
    expect(highlightSegments('say meteo', 'meteo')).toEqual([
      { text: 'say ', match: false },
      { text: 'meteo', match: true },
    ]);
  });
});

describe('SearchPanel (CHAT-02 / D-08)', () => {
  beforeEach(() => {
    navigateMock.mockReset();
    vi.stubGlobal('fetch', stubFetch(HITS));
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('announces an in-flight search via a role=status skeleton (loading state)', async () => {
    // The search request hangs → isFetching with no results yet → the loading skeleton renders
    // inside a role=status live region so assistive tech announces the in-flight search.
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (urlOf(input).includes('/api/conversations/search')) {
          return new Promise<Response>(() => undefined); // never resolves
        }
        return Promise.resolve(new Response(JSON.stringify(CONVERSATIONS), { status: 200 }));
      }),
    );
    renderPanel();
    const search = screen.getByPlaceholderText('Search conversations');
    fireEvent.change(search, { target: { value: 'meteo' } });
    const status = await screen.findByRole('status', { name: 'Searching...' });
    expect(status.querySelectorAll('.skeleton-block')).toHaveLength(2);
  });

  it('shows snippet rows with the conversation title and highlights the match', async () => {
    renderPanel();
    const search = screen.getByPlaceholderText('Search conversations');
    expect(search.getAttribute('data-slot')).toBe('input');
    fireEvent.change(search, {
      target: { value: 'meteo' },
    });
    await waitFor(() => {
      expect(screen.getByText('Weather investigation')).toBeTruthy();
    });
    // The matched term is wrapped in a <mark> (safe element composition, no raw HTML).
    const marks = document.querySelectorAll('mark');
    expect(Array.from(marks).some((m) => m.textContent === 'meteo')).toBe(true);
  });

  it('opens the matched thread at the seq and navigates to /c/:id on click', async () => {
    const onOpen = renderPanel();
    fireEvent.change(screen.getByPlaceholderText('Search conversations'), {
      target: { value: 'meteo' },
    });
    const row = await screen.findByRole('button', { name: /Weather investigation/ });
    fireEvent.click(row);
    expect(onOpen).toHaveBeenCalledWith('c-1', 4);
    expect(navigateMock).toHaveBeenCalledWith('/c/c-1');
  });

  it('shows the No matches empty state with the query interpolated', async () => {
    vi.stubGlobal('fetch', stubFetch([]));
    renderPanel();
    fireEvent.change(screen.getByPlaceholderText('Search conversations'), {
      target: { value: 'zzz' },
    });
    await waitFor(() => {
      expect(screen.getByText('No matches')).toBeTruthy();
    });
    expect(screen.getByText(/No conversations contain "zzz"/)).toBeTruthy();
  });

  it('shows nothing for an empty query (idle panel, no fetch)', () => {
    renderPanel();
    expect(screen.queryByText('No matches')).toBeNull();
  });
});
