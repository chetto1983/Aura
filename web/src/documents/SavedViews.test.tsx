import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';
import DocumentsSurface from './DocumentsSurface';
import { stubServer } from './documentsTestServer';
import i18n from '@/i18n/i18n';

afterEach(() => vi.unstubAllGlobals());
it('persists a query without UI-only keys, reopens it in a fresh session, and edits or deletes it', async () => {
  await act(() => i18n.changeLanguage('en'));
  stubServer();
  const original = fetch;
  const views: Record<string, unknown>[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (!url.startsWith('/api/documents/views')) return original(input, init);
      const method = init?.method ?? 'GET';
      if (method === 'GET') return new Response(JSON.stringify({ views }));
      if (method === 'DELETE') views.splice(0);
      else {
        const body = JSON.parse(typeof init?.body === 'string' ? init.body : '{}') as Record<
          string,
          unknown
        >;
        views[0] = { ...body, id: 'view1' };
      }
      return new Response(JSON.stringify({ view: views[0] }));
    }),
  );
  const mount = (path: string) => {
    const router = createMemoryRouter([{ path: '/documents/*', element: <DocumentsSurface /> }], {
      initialEntries: [path],
    });
    return {
      ...render(
        <QueryClientProvider
          client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
        >
          <RouterProvider router={router} />
        </QueryClientProvider>,
      ),
      router,
    };
  };
  const first = mount(
    '/documents/library?q=Cedar&file_type=code&display=cards&offset=50&from=https://evil.test',
  );
  fireEvent.click(await screen.findByRole('button', { name: 'Save this view' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'View name' }), {
    target: { value: 'Code contracts' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => {
    expect(views).toHaveLength(1);
  });
  const query = new URLSearchParams(String(views[0]?.query));
  expect(query.get('file_type')).toBe('code');
  expect(query.get('offset')).toBe('0');
  expect(query.has('display')).toBe(false);
  expect(query.has('from')).toBe(false);
  first.unmount();
  const second = mount('/documents/views');
  const name = await screen.findByRole('textbox', { name: 'View name' });
  fireEvent.change(name, { target: { value: 'Pinned contracts' } });
  fireEvent.click(screen.getByRole('checkbox', { name: 'Pin in navigation' }));
  fireEvent.click(screen.getByRole('checkbox', { name: 'Show on Overview' }));
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => {
    expect(views[0]).toMatchObject({ name: 'Pinned contracts', pinned: true, dashboard: true });
  });
  fireEvent.click(screen.getByRole('link', { name: 'Open view' }));
  await waitFor(() => {
    expect(second.router.state.location.pathname).toBe('/documents/library');
  });
  const applied = new URLSearchParams(second.router.state.location.search);
  expect(applied.get('q')).toBe('Cedar');
  expect(applied.get('display')).toBe('cards');
  expect(applied.get('offset')).toBe('0');
  await act(() => second.router.navigate('/documents/views'));
  await screen.findByRole('textbox', { name: 'View name' });
  fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
  fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
  await waitFor(() => {
    expect(views).toHaveLength(0);
  });
  second.unmount();
});
