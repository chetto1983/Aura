import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { configure, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { AppShell } from '../AppShell';

// This file mounts the FULL AppShell (QueryClient + SSE chat lane + multi-fetch
// round-trips) and runs in parallel with the rest of the suite under coverage
// instrumentation. The default 1000ms async-utility timeout raced the create →
// /agent/run → SSE → render chain under that CPU pressure and flaked intermittently
// (different rows on different runs). Widen the async-wait tolerance for this heavy
// file only; the assertions and behaviour are unchanged. 60s still flaked on a loaded
// CI runner (the create→/agent/run→SSE→render chain starved past 60s while the file
// itself runs in ~4s locally), so the headroom is 120s — a slow CI run completes, a
// genuine hang still fails (just later).
const ASYNC_TIMEOUT_MS = 120000;

function sseBody(frames: readonly Record<string, unknown>[]): string {
  return frames.map((f) => `event: ${String(f.type)}\ndata: ${JSON.stringify(f)}\n\n`).join('');
}

function sseResponse(frames: readonly Record<string, unknown>[]): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(sseBody(frames)));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

// A richer fetch double: a populated list + a single-conversation aggregate +
// empty rot-events, so selecting a row exercises the activeThreadId → chat lane +
// footer wiring (the 25-03 stub resolution).
function shellWithConversations() {
  return vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    if (url.includes('/rot-events')) {
      return Promise.resolve(new Response('[]', { status: 200 }));
    }
    if (/\/api\/conversations\/[^/?]+$/.test(url)) {
      // GET /api/conversations/{id} aggregate seed.
      return Promise.resolve(
        new Response(
          JSON.stringify({
            ID: 'c-1',
            Title: 'Pinned run',
            TitleSet: true,
            IdentityID: 'op',
            Status: 'active',
            Model: 'm',
            TotalInputTokens: 10,
            TotalOutputTokens: 5,
            TotalCachedTokens: 2,
            TotalCostUSD: 0.02,
            CreatedAt: '2026-06-17T00:00:00Z',
          }),
          { status: 200 },
        ),
      );
    }
    if (url.includes('/api/conversations')) {
      return Promise.resolve(
        new Response(
          JSON.stringify([
            {
              ID: 'c-1',
              Title: 'Pinned run',
              TitleSet: true,
              IdentityID: 'op',
              Status: 'active',
              Model: 'm',
              TotalInputTokens: 0,
              TotalOutputTokens: 0,
              TotalCachedTokens: 0,
              TotalCostUSD: 0,
              CreatedAt: '2026-06-17T00:00:00Z',
            },
          ]),
          { status: 200 },
        ),
      );
    }
    return Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }));
  });
}

describe('AppShell conversation binding (CHAT-02 / 25-03 stub resolution)', () => {
  beforeAll(() => {
    // Raise the global RTL async-wait timeout for this file's lifetime (default 1000ms).
    // The per-test vitest timeout is widened via the explicit third arg on each it()
    // below (a runtime vi.setConfig in beforeAll does NOT re-time an already-scheduled
    // test, which left it racing the 5000ms default).
    configure({ asyncUtilTimeout: ASYNC_TIMEOUT_MS });
  });
  afterAll(() => {
    // Restore the default so the widened timeout never leaks into other files.
    configure({ asyncUtilTimeout: 1000 });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function renderAt(initialPath: string) {
    vi.stubGlobal('fetch', shellWithConversations());
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    // Mount through the real routes so useParams resolves the /c/:id deep link.
    return render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[initialPath]}>
          <Routes>
            <Route path="/" element={<AppShell />} />
            <Route path="/c/:id" element={<AppShell />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  }

  it(
    'selecting a conversation marks it active (binds the chat lane threadId)',
    async () => {
      renderAt('/');
      const row = await screen.findByRole('button', { name: 'Pinned run' });
      expect(row.getAttribute('aria-current')).toBeNull();
      fireEvent.click(row);
      await waitFor(() => {
        expect(
          screen.getByRole('button', { name: 'Pinned run' }).getAttribute('aria-current'),
        ).toBe('true');
      });
    },
    ASYNC_TIMEOUT_MS,
  );

  it(
    'seeds the active thread from a /c/:id deep link',
    async () => {
      renderAt('/c/c-1');
      const row = await screen.findByRole('button', { name: 'Pinned run' });
      // The route param seeded the active selection (aria-current) without a click.
      await waitFor(() => {
        expect(row.getAttribute('aria-current')).toBe('true');
      });
    },
    ASYNC_TIMEOUT_MS,
  );

  it(
    'creates and selects a conversation before sending from an empty shell',
    async () => {
      const created = {
        ID: '22222222-2222-2222-2222-222222222222',
        Title: '',
        TitleSet: false,
        IdentityID: 'op',
        Status: 'active',
        Model: 'm',
        TotalInputTokens: 0,
        TotalOutputTokens: 0,
        TotalCachedTokens: 0,
        TotalCostUSD: 0,
        CreatedAt: '2026-06-17T00:00:00Z',
      };
      const calls: string[] = [];
      let runBody: { threadId?: string } | undefined;
      let createBody: { title?: string } | undefined;
      vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
          const url =
            typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
          const method = init?.method ?? 'GET';
          calls.push(`${method} ${url}`);
          if (url === '/api/conversations' && method === 'POST') {
            if (typeof init?.body !== 'string') throw new Error('expected JSON request body');
            createBody = JSON.parse(init.body) as { title?: string };
            return Promise.resolve(new Response(JSON.stringify(created), { status: 201 }));
          }
          if (url === '/agent/run') {
            if (typeof init?.body !== 'string') throw new Error('expected JSON request body');
            runBody = JSON.parse(init.body) as { threadId?: string };
            return Promise.resolve(
              sseResponse([
                { type: 'RUN_STARTED', threadId: created.ID, runId: 'run-1' },
                { type: 'TEXT_MESSAGE_START', messageId: 'msg-1' },
                { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-1', delta: 'Ciao.' },
                { type: 'TEXT_MESSAGE_END', messageId: 'msg-1' },
                {
                  type: 'RUN_FINISHED',
                  threadId: created.ID,
                  runId: 'run-1',
                  outcome: { type: 'success' },
                },
              ]),
            );
          }
          if (url.includes('/rot-events')) {
            return Promise.resolve(new Response('[]', { status: 200 }));
          }
          if (/\/api\/conversations\/[^/?]+$/.test(url)) {
            return Promise.resolve(new Response(JSON.stringify(created), { status: 200 }));
          }
          if (url.includes('/api/approvals') || url.includes('/api/conversations')) {
            return Promise.resolve(new Response('[]', { status: 200 }));
          }
          return Promise.resolve(
            new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }),
          );
        }),
      );
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      render(
        <QueryClientProvider client={client}>
          <MemoryRouter initialEntries={['/']}>
            <Routes>
              <Route path="/" element={<AppShell />} />
              <Route path="/c/:id" element={<AppShell />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      );

      const composer = await screen.findByPlaceholderText('Ask Aura');
      fireEvent.change(composer, { target: { value: 'ciao' } });
      fireEvent.keyDown(composer, { key: 'Enter', code: 'Enter' });

      await waitFor(() => {
        expect(screen.getByText('Ciao.')).toBeTruthy();
      });
      expect(runBody?.threadId).toBe(created.ID);
      expect(createBody?.title).toBe('ciao');
      expect(calls.indexOf('POST /api/conversations')).toBeLessThan(
        calls.indexOf('POST /agent/run'),
      );
    },
    ASYNC_TIMEOUT_MS,
  );

  it(
    'renders the cross-thread approval badge + list and Open binds the thread (APRV-01/D-04)',
    async () => {
      // The approvals poll returns one pending interrupt in a background thread.
      const runCalls: string[] = [];
      vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
          const url =
            typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
          if (url.includes('/api/approvals') && (init?.method ?? 'GET') === 'POST') {
            return Promise.resolve(new Response(null, { status: 204 }));
          }
          if (url === '/agent/run') {
            runCalls.push(url);
            return Promise.resolve(new Response('', { status: 200 }));
          }
          if (url.includes('/api/approvals')) {
            return Promise.resolve(
              new Response(
                JSON.stringify([
                  {
                    token: 't-1',
                    conversation_id: 'c-1',
                    kind: 'clarification',
                    question: 'Which city?',
                    priority: 0,
                  },
                ]),
                { status: 200 },
              ),
            );
          }
          if (url.includes('/rot-events')) {
            return Promise.resolve(new Response('[]', { status: 200 }));
          }
          if (/\/api\/conversations\/[^/?]+$/.test(url)) {
            return Promise.resolve(
              new Response(
                JSON.stringify({
                  ID: 'c-1',
                  Title: 'Bg run',
                  TitleSet: true,
                  IdentityID: 'op',
                  Status: 'active',
                  Model: 'm',
                  TotalInputTokens: 0,
                  TotalOutputTokens: 0,
                  TotalCachedTokens: 0,
                  TotalCostUSD: 0,
                  CreatedAt: '2026-06-17T00:00:00Z',
                }),
                { status: 200 },
              ),
            );
          }
          if (url.includes('/api/conversations')) {
            return Promise.resolve(new Response('[]', { status: 200 }));
          }
          return Promise.resolve(
            new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }),
          );
        }),
      );
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      render(
        <QueryClientProvider client={client}>
          <MemoryRouter initialEntries={['/']}>
            <Routes>
              <Route path="/" element={<AppShell />} />
              <Route path="/c/:id" element={<AppShell />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      );
      // The badge shows the cross-thread count and opens the list.
      const badge = await screen.findByRole('button', { name: '1 approval waiting' });
      fireEvent.click(badge);
      // The list row renders; Open jumps to /c/c-1 and binds the active thread.
      const open = await screen.findByRole('button', { name: 'Open' });
      fireEvent.click(open);
      // After Open, the inline card for c-1 surfaces in the chat lane (verbatim question).
      await waitFor(() => {
        expect(screen.getAllByText('Which city?').length).toBeGreaterThan(0);
      });
      // Answering the inline card re-drives the run (continue-after-resume): the
      // AppShell redriveRun callback POSTs /agent/run for the resumed thread (D-05).
      fireEvent.change(screen.getByPlaceholderText('Type your answer'), {
        target: { value: 'Rome' },
      });
      fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
      await waitFor(() => {
        expect(runCalls).toContain('/agent/run');
      });
    },
    ASYNC_TIMEOUT_MS,
  );

  it(
    'opening a search hit navigates + binds the active thread (route change after mount)',
    async () => {
      // The search route returns one hit for c-1; clicking it navigates to /c/c-1,
      // which the route-change branch folds into the active selection.
      vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL) => {
          const url =
            typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
          if (url.includes('/api/conversations/search')) {
            return Promise.resolve(
              new Response(
                JSON.stringify([
                  { ConversationID: 'c-1', Seq: 2, Content: 'meteo report', Similarity: 0.9 },
                ]),
                { status: 200 },
              ),
            );
          }
          if (url.includes('/rot-events')) {
            return Promise.resolve(new Response('[]', { status: 200 }));
          }
          if (/\/api\/conversations\/[^/?]+$/.test(url)) {
            return Promise.resolve(
              new Response(
                JSON.stringify({
                  ID: 'c-1',
                  Title: 'Pinned run',
                  TitleSet: true,
                  IdentityID: 'op',
                  Status: 'active',
                  Model: 'm',
                  TotalInputTokens: 0,
                  TotalOutputTokens: 0,
                  TotalCachedTokens: 0,
                  TotalCostUSD: 0,
                  CreatedAt: '2026-06-17T00:00:00Z',
                }),
                { status: 200 },
              ),
            );
          }
          if (url.includes('/api/conversations')) {
            return Promise.resolve(
              new Response(
                JSON.stringify([
                  {
                    ID: 'c-1',
                    Title: 'Pinned run',
                    TitleSet: true,
                    IdentityID: 'op',
                    Status: 'active',
                    Model: 'm',
                    TotalInputTokens: 0,
                    TotalOutputTokens: 0,
                    TotalCachedTokens: 0,
                    TotalCostUSD: 0,
                    CreatedAt: '2026-06-17T00:00:00Z',
                  },
                ]),
                { status: 200 },
              ),
            );
          }
          return Promise.resolve(
            new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }),
          );
        }),
      );
      const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      render(
        <QueryClientProvider client={client}>
          <MemoryRouter initialEntries={['/']}>
            <Routes>
              <Route path="/" element={<AppShell />} />
              <Route path="/c/:id" element={<AppShell />} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      );
      fireEvent.change(screen.getByPlaceholderText('Search conversations'), {
        target: { value: 'meteo' },
      });
      const hit = await screen.findByRole('button', { name: /Pinned run/ });
      fireEvent.click(hit);
      await waitFor(() => {
        // After navigation the sidebar row for c-1 is marked active.
        const rows = screen.getAllByRole('button', { name: 'Pinned run' });
        expect(rows.some((r) => r.getAttribute('aria-current') === 'true')).toBe(true);
      });
    },
    ASYNC_TIMEOUT_MS,
  );
});
