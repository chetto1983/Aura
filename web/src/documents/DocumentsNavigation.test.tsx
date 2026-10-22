import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';
import DocumentsSurface from './DocumentsSurface';
import { stubServer } from './documentsTestServer';
import type { SavedView } from './workspaceApi';
import i18n from '@/i18n/i18n';

afterEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

async function mountPinnedViews() {
  await act(() => i18n.changeLanguage('en'));
  stubServer();
  const original = fetch;
  let views: SavedView[] = ['Cedar contracts', 'Birch invoices'].map((name, index) => ({
    id: `view-${String(index)}`,
    name,
    version: 1,
    query: 'q=Cedar',
    display: 'cards',
    pinned: true,
    dashboard: true,
  }));
  let refused = false;
  const writes: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const method = init?.method ?? 'GET';
    if (method !== 'GET') writes.push(`${method} ${url}`);
    if (url === '/api/documents/views') return new Response(JSON.stringify({ views }));
    if (url.startsWith('/api/documents/views/') && method === 'DELETE') {
      if (refused)
        return new Response(JSON.stringify({ error: 'Delete refused' }), { status: 503 });
      views = views.filter((view) => url !== `/api/documents/views/${view.id}`);
      return new Response(JSON.stringify({ ok: true }));
    }
    return original(input, init);
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(['documents-views', 'other-identity'], [{ id: 'private-view' }]);
  const router = createMemoryRouter([{ path: '/documents/*', element: <DocumentsSurface /> }], {
    initialEntries: ['/documents/library?q=Cedar&display=cards'],
  });
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await screen.findByRole('link', { name: 'Cedar contracts' });
  return {
    client,
    router,
    writes,
    refuse: (value: boolean) => {
      refused = value;
    },
  };
}

it('names the pinned view before deleting and sends no write when cancelled', async () => {
  const fixture = await mountPinnedViews();
  const location = fixture.router.state.location;
  const nav = screen.getByRole('navigation', { name: 'Document navigation' });
  const action = within(nav).getByRole('button', { name: 'Delete: Cedar contracts' });
  action.focus();
  fireEvent.click(action);
  const dialog = screen.getByRole('dialog', { name: 'Delete this saved view?' });
  expect(within(dialog).getByText('Cedar contracts', { exact: true })).toBeTruthy();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  expect(screen.queryByRole('dialog', { name: 'Delete this saved view?' })).toBeNull();
  await waitFor(() => {
    expect(document.activeElement).toBe(action);
  });
  expect(fixture.writes).toEqual([]);
  expect(within(nav).getByRole('link', { name: 'Cedar contracts' })).toBeTruthy();
  expect(fixture.router.state.location).toBe(location);
});

it('deletes only the selected saved view and refreshes its identity without changing documents or location', async () => {
  const fixture = await mountPinnedViews();
  const location = fixture.router.state.location;
  const nav = screen.getByRole('navigation', { name: 'Document navigation' });
  fireEvent.click(within(nav).getByRole('button', { name: 'Delete: Cedar contracts' }));
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  await waitFor(() => {
    expect(within(nav).queryByRole('link', { name: 'Cedar contracts' })).toBeNull();
  });
  expect(fixture.writes).toEqual(['DELETE /api/documents/views/view-0']);
  expect(within(nav).getByRole('link', { name: 'Birch invoices' })).toBeTruthy();
  expect(fixture.client.getQueryData(['documents-views', 'documents-test'])).toMatchObject([
    { id: 'view-1', name: 'Birch invoices' },
  ]);
  expect(fixture.client.getQueryState(['documents-views', 'other-identity'])?.isInvalidated).toBe(
    false,
  );
  expect(fixture.client.getQueryData(['documents-views', 'other-identity'])).toEqual([
    { id: 'private-view' },
  ]);
  expect(fixture.router.state.location).toBe(location);
  expect(await screen.findByText('fattura_2026_031.pdf', { exact: true })).toBeTruthy();
  await act(() => fixture.router.navigate('/documents/views'));
  expect((await screen.findByRole('textbox', { name: 'View name' })).getAttribute('value')).toBe(
    'Birch invoices',
  );
});

it('keeps a refused view and its confirmation visible so deletion can be retried', async () => {
  const fixture = await mountPinnedViews();
  fixture.refuse(true);
  fireEvent.click(screen.getByRole('button', { name: 'Delete: Cedar contracts' }));
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  const dialog = screen.getByRole('dialog', { name: 'Delete this saved view?' });
  expect((await within(dialog).findByRole('alert')).textContent).toContain('Delete refused');
  expect(
    fixture.client.getQueryData<SavedView[]>(['documents-views', 'documents-test']),
  ).toHaveLength(2);
  fixture.refuse(false);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Retry' }));
  await waitFor(() => {
    expect(screen.queryByRole('dialog', { name: 'Delete this saved view?' })).toBeNull();
  });
  expect(fixture.writes).toEqual([
    'DELETE /api/documents/views/view-0',
    'DELETE /api/documents/views/view-0',
  ]);
  expect(screen.getByRole('link', { name: 'Birch invoices' })).toBeTruthy();
});
