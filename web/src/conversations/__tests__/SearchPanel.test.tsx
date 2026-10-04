import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router';
import '../../i18n/i18n';
import { SearchPanel } from '../SearchPanel';
import { highlightSegments, searchSnippet } from '../searchHighlight';
import type { Conversation, SearchResult } from '../useConversations';

// Capture navigate() calls so the /c/:id deep-link assertion is exact.
const navigateMock = vi.fn();
vi.mock('react-router', async () => {
  const actual = await vi.importActual<typeof import('react-router')>('react-router');
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

// The message measured on 2026-10-04 (prd.md §7): the word sits 70 characters in.
const LONG_MESSAGE =
  'Ciao Aura, ti scrivo per organizzare la settimana prossima: lunedì devo mandare la fattura a ' +
  "Bianchi per il lavoro di ristrutturazione del bagno, martedì c'è la riunione con il " +
  'commercialista per la dichiarazione dei redditi.';

describe('searchSnippet', () => {
  it('starts a few words before a word deep in a long message, at a word boundary', () => {
    const snippet = searchSnippet(LONG_MESSAGE, 'commercialista');
    expect(snippet.startsWith('…')).toBe(true);
    expect(snippet.indexOf('commercialista')).toBeLessThanOrEqual(21);
    expect(LONG_MESSAGE).toContain(` ${snippet.slice(1, 12)}`);
    expect(snippet).not.toContain('Ciao Aura');
  });
  it('keeps the start, without a leading ellipsis, when the word is near it', () => {
    const snippet = searchSnippet(LONG_MESSAGE, 'scrivo');
    expect(snippet.startsWith('Ciao Aura, ti scrivo')).toBe(true);
    expect(snippet.endsWith('…')).toBe(true);
  });
  it('shows the start when only a fuzzy match found the message', () => {
    expect(searchSnippet(LONG_MESSAGE, 'fatura')).toBe(`${LONG_MESSAGE.slice(0, 120)}…`);
  });
  it('flattens whitespace and matches without regard to case or surrounding spaces', () => {
    expect(searchSnippet('\n one\n\n  Two   three ', ' TWO ')).toBe('one Two three');
    expect(searchSnippet(LONG_MESSAGE, '  commercialista ').startsWith('…')).toBe(true);
  });
  it('never starts after the match when the word before it has no space to cut at', () => {
    const text = `aaaa ${'b'.repeat(30)}needle and more`;
    expect(searchSnippet(text, 'needle')).toBe(`…${'b'.repeat(20)}needle and more`);
  });
  it('never cuts a surrogate pair in half', () => {
    const emoji = '😀';
    // With no space to cut at, the window starts on the first emoji's second half and ends
    // on the second emoji's.
    const text = `${emoji}${'a'.repeat(19)}needle${'b'.repeat(93)}${emoji}tail`;
    expect(searchSnippet(text, 'needle')).toBe(`…${'a'.repeat(19)}needle${'b'.repeat(93)}…`);
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

  it('shows the matched word of a long message, not just its start', async () => {
    vi.stubGlobal(
      'fetch',
      stubFetch([{ ConversationID: 'c-1', Seq: 1, Content: LONG_MESSAGE, Similarity: 1 }]),
    );
    renderPanel();
    fireEvent.change(screen.getByPlaceholderText('Search conversations'), {
      target: { value: 'commercialista' },
    });
    await waitFor(() => {
      expect(
        Array.from(document.querySelectorAll('mark')).some(
          (m) => m.textContent === 'commercialista',
        ),
      ).toBe(true);
    });
    expect(screen.queryByText(/Ciao Aura/)).toBeNull();
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
