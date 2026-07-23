import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { configure, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { AppShell } from '../AppShell';

// Operator report (live screenshot): deleting the ACTIVE conversation left the
// MAIN pane bound to the dead threadId — messages + composer stayed live and
// the next send hit thread-not-found. On a confirmed delete of the active row
// the pane must reset to the pristine new-chat state (cleared selection +
// route '/': EmptyThreadStarters, next send creates via ensureThread). A
// non-active delete must NOT touch the pane. Mirrors the heavy AppShell
// harness (AppShell.conversation.test.tsx) incl. its widened async timeout.

const ASYNC_TIMEOUT_MS = 120000;

interface Row {
  ID: string;
  Title: string;
}

function conversationBody(id: string, title: string): Record<string, unknown> {
  return {
    ID: id,
    Title: title,
    TitleSet: true,
    IdentityID: 'op',
    Status: 'active',
    Model: 'm',
    TotalInputTokens: 0,
    TotalOutputTokens: 0,
    TotalCachedTokens: 0,
    TotalCostUSD: 0,
    CreatedAt: '2026-07-23T00:00:00Z',
  };
}

// A mutable fixture: DELETE removes the row, the list refetch reflects it, and
// the active thread's snapshot carries a visible message so the stale-pane bug
// would be observable.
function shellWithDeletableRows(rows: Row[]) {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const method = init?.method ?? 'GET';
    const rowId = /\/api\/conversations\/([^/?]+)$/.exec(url)?.[1];
    if (method === 'DELETE' && rowId !== undefined) {
      const index = rows.findIndex((row) => row.ID === rowId);
      if (index >= 0) rows.splice(index, 1);
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    if (url.includes('/threads/') && url.endsWith('/messages')) {
      return Promise.resolve(
        new Response(
          JSON.stringify({
            type: 'MESSAGES_SNAPSHOT',
            messages: [
              { id: 'msg-0', role: 'user', content: 'saved question' },
              { id: 'msg-1', role: 'assistant', content: 'Saved answer body.' },
            ],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      );
    }
    if (url.includes('/rot-events')) {
      return Promise.resolve(new Response('[]', { status: 200 }));
    }
    if (method === 'GET' && rowId !== undefined) {
      const row = rows.find((r) => r.ID === rowId);
      return Promise.resolve(
        new Response(JSON.stringify(conversationBody(rowId, row?.Title ?? 'gone')), {
          status: row === undefined ? 404 : 200,
        }),
      );
    }
    if (url.includes('/api/conversations')) {
      return Promise.resolve(
        new Response(JSON.stringify(rows.map((row) => conversationBody(row.ID, row.Title))), {
          status: 200,
        }),
      );
    }
    if (url.includes('/api/assets')) {
      return Promise.resolve(new Response('[]', { status: 200 }));
    }
    return Promise.resolve(new Response('{"ok":true,"ready":true,"deps":{}}', { status: 200 }));
  });
}

function renderAt(initialPath: string, rows: Row[]) {
  vi.stubGlobal('fetch', shellWithDeletableRows(rows));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
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

async function deleteViaMenu(title: string) {
  const row = screen.getByRole('button', { name: title }).closest('li');
  if (row === null) throw new Error(`no sidebar row for ${title}`);
  fireEvent.click(within(row).getByRole('button', { name: 'Conversation actions' }));
  const menu = await screen.findByRole('menu', { name: 'Conversation actions' });
  fireEvent.click(within(menu).getByRole('menuitem', { name: 'Delete permanently' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Delete permanently' }));
}

describe('AppShell — deleting the active conversation resets the main pane', () => {
  beforeAll(() => {
    configure({ asyncUtilTimeout: ASYNC_TIMEOUT_MS });
  });
  afterAll(() => {
    configure({ asyncUtilTimeout: 1000 });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it(
    'delete ACTIVE (others remain) → pane resets to the new-chat empty state',
    async () => {
      renderAt('/c/c-1', [
        { ID: 'c-1', Title: 'Active run' },
        { ID: 'c-2', Title: 'Other run' },
      ]);
      // The active conversation's saved messages render in the pane.
      await screen.findByText('Saved answer body.');

      await deleteViaMenu('Active run');

      // The pane resets: deleted messages gone, pristine empty-thread state in.
      await waitFor(() => {
        expect(screen.queryByText('Saved answer body.')).toBeNull();
      });
      expect(await screen.findByText('Ask Aura', { selector: 'h2' })).toBeTruthy();
      // The surviving row is not silently promoted to active (reset-to-new-chat
      // convention, least surprising after an explicit delete).
      await waitFor(() => {
        expect(
          screen.getByRole('button', { name: 'Other run' }).getAttribute('aria-current'),
        ).toBeNull();
      });
    },
    ASYNC_TIMEOUT_MS,
  );

  it(
    'delete NON-active → the pane keeps the active conversation untouched',
    async () => {
      renderAt('/c/c-1', [
        { ID: 'c-1', Title: 'Active run' },
        { ID: 'c-2', Title: 'Other run' },
      ]);
      await screen.findByText('Saved answer body.');

      await deleteViaMenu('Other run');

      // The deleted row leaves the sidebar; the pane stays on the active thread.
      await waitFor(() => {
        expect(screen.queryByRole('button', { name: 'Other run' })).toBeNull();
      });
      expect(screen.getByText('Saved answer body.')).toBeTruthy();
      expect(screen.getByRole('button', { name: 'Active run' }).getAttribute('aria-current')).toBe(
        'true',
      );
      expect(screen.queryByText('Ask Aura', { selector: 'h2' })).toBeNull();
    },
    ASYNC_TIMEOUT_MS,
  );

  it(
    'delete the LAST remaining conversation → new-chat pane + empty sidebar',
    async () => {
      renderAt('/c/c-1', [{ ID: 'c-1', Title: 'Only run' }]);
      await screen.findByText('Saved answer body.');

      await deleteViaMenu('Only run');

      await waitFor(() => {
        expect(screen.queryByText('Saved answer body.')).toBeNull();
      });
      expect(await screen.findByText('Ask Aura', { selector: 'h2' })).toBeTruthy();
      // Sidebar shows its empty state (no rows left).
      expect(await screen.findByText('Start a run')).toBeTruthy();
    },
    ASYNC_TIMEOUT_MS,
  );
});
