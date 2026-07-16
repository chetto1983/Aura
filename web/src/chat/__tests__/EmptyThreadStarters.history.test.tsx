import type { ReactElement } from 'react';
import { act, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ExternalStoreChat } from '../ExternalStoreChat';

interface Deferred<T> {
  readonly promise: Promise<T>;
  readonly resolve: (value: T) => void;
  readonly reject: (reason?: unknown) => void;
}

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function emptySnapshot(): Response {
  return json({ type: 'MESSAGES_SNAPSHOT', messages: [] });
}

function populatedSnapshot(): Response {
  return json({
    type: 'MESSAGES_SNAPSHOT',
    messages: [
      {
        id: 'message-1',
        role: 'user',
        content: 'Persisted question',
      },
    ],
  });
}

function renderChat(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rendered = render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
  return {
    ...rendered,
    rerenderChat: (next: ReactElement) => {
      rendered.rerender(<QueryClientProvider client={client}>{next}</QueryClientProvider>);
    },
  };
}

function starter() {
  return screen.queryByRole('button', { name: 'Research a topic' });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('empty-thread starter history readiness', () => {
  it('reveals starters only after a successful empty history snapshot', async () => {
    const history = deferred<Response>();
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = requestURL(input);
        if (url === '/threads/conv-1/messages') return history.promise;
        if (url.startsWith('/api/assets')) return Promise.resolve(json([]));
        return Promise.resolve(json({}));
      }),
    );

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    expect(starter()).toBeNull();

    await act(async () => {
      history.resolve(emptySnapshot());
      await history.promise;
    });

    expect(await screen.findByRole('group', { name: 'Suggestions' })).toBeTruthy();
  });

  it('keeps starters absent before and after a populated history snapshot', async () => {
    const history = deferred<Response>();
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = requestURL(input);
        if (url === '/threads/conv-1/messages') return history.promise;
        if (url.startsWith('/api/assets')) return Promise.resolve(json([]));
        return Promise.resolve(json({}));
      }),
    );

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    expect(starter()).toBeNull();

    await act(async () => {
      history.resolve(populatedSnapshot());
      await history.promise;
    });

    expect(await screen.findByText('Persisted question')).toBeTruthy();
    expect(starter()).toBeNull();
  });

  it('keeps starters absent after history rejects', async () => {
    const history = deferred<Response>();
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (requestURL(input) === '/threads/conv-1/messages') return history.promise;
        return Promise.resolve(json({}));
      }),
    );

    renderChat(<ExternalStoreChat threadId="conv-1" />);
    expect(starter()).toBeNull();

    await act(async () => {
      history.reject(new Error('history unavailable'));
      await history.promise.catch(() => undefined);
    });

    expect(
      await screen.findByText(/Couldn't load this conversation's saved messages/),
    ).toBeTruthy();
    expect(starter()).toBeNull();
  });

  it('shows starters for the intentional no-thread state without loading history', async () => {
    const fetchSpy = vi.fn((input: RequestInfo | URL) => {
      void input;
      return Promise.resolve(json({}));
    });
    vi.stubGlobal('fetch', fetchSpy);

    renderChat(<ExternalStoreChat threadId="" />);

    expect(await screen.findByRole('group', { name: 'Suggestions' })).toBeTruthy();
    expect(fetchSpy.mock.calls.some(([input]) => requestURL(input).includes('/messages'))).toBe(
      false,
    );
  });

  it('resets readiness on navigation and ignores a stale completion', async () => {
    const first = deferred<Response>();
    const second = deferred<Response>();
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = requestURL(input);
        if (url === '/threads/conv-a/messages') return first.promise;
        if (url === '/threads/conv-b/messages') return second.promise;
        if (url.startsWith('/api/assets')) return Promise.resolve(json([]));
        return Promise.resolve(json({}));
      }),
    );

    const { rerenderChat } = renderChat(<ExternalStoreChat threadId="conv-a" />);
    expect(starter()).toBeNull();

    rerenderChat(<ExternalStoreChat threadId="conv-b" />);
    expect(starter()).toBeNull();

    await act(async () => {
      first.resolve(emptySnapshot());
      await first.promise;
    });
    expect(starter()).toBeNull();

    await act(async () => {
      second.resolve(emptySnapshot());
      await second.promise;
    });
    expect(await screen.findByRole('group', { name: 'Suggestions' })).toBeTruthy();
  });
});
