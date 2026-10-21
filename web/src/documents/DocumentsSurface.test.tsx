import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { stubServer } from './documentsTestServer';
import DocumentsSurface from './DocumentsSurface';
import i18n from '@/i18n/i18n';

vi.mock('@/files/FilesWorkspace', () => ({ default: () => <p>file manager</p> }));

afterEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.unstubAllGlobals();
});

it('accepts a file drop and keeps successful uploads waiting across document destinations', async () => {
  await act(() => i18n.changeLanguage('en'));
  stubServer();
  const original = fetch;
  const folders: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      const url = new URL(raw, 'http://test');
      if (url.pathname === '/api/filemanager/upload') {
        const folder = url.searchParams.get('id') ?? '';
        folders.push(folder);
        return Promise.resolve(new Response(JSON.stringify({ id: `${folder}new.pdf` })));
      }
      return original(input, init);
    }),
  );
  mount('/documents/processing');
  const heading = await screen.findByRole('heading', { name: 'Processing' });
  expect(screen.getAllByRole('button', { name: 'Upload files' })).toHaveLength(1);
  fireEvent.drop(heading, {
    dataTransfer: {
      files: [new File(['one'], 'new.pdf'), new File(['two'], 'new.pdf')],
      types: ['Files'],
    },
  });
  await waitFor(() => {
    expect(folders).toHaveLength(2);
  });
  expect(await screen.findAllByText('Stored — waiting for indexing')).toHaveLength(2);
  fireEvent.click(screen.getByRole('link', { name: 'All documents' }));
  expect(await screen.findByRole('heading', { name: 'All documents' })).toBeTruthy();
  fireEvent.click(screen.getByRole('link', { name: 'Processing' }));
  expect(await screen.findAllByText('Stored — waiting for indexing')).toHaveLength(2);
  expect(folders).toHaveLength(2);
  fireEvent.click(
    within(screen.getByRole('navigation', { name: 'Document navigation' })).getByRole('link', {
      name: 'Files / folders',
    }),
  );
  const files = await screen.findByText('file manager');
  fireEvent.drop(files, {
    dataTransfer: { files: [new File(['owned by file manager'], 'other.pdf')], types: ['Files'] },
  });
  expect(folders).toHaveLength(2);
});

function mount(path = '/documents') {
  const router = createMemoryRouter([{ path: '/documents/*', element: <DocumentsSurface /> }], {
    initialEntries: [path],
  });
  const result = render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...result, router };
}
it('opens Overview and distinguishes Inbox from Suggestions through back and reload', async () => {
  await act(() => i18n.changeLanguage('en'));
  const server = stubServer();
  const { router, unmount } = mount();
  expect(await screen.findByRole('heading', { name: 'Overview' })).toBeTruthy();
  expect(screen.queryByText('file manager')).toBeNull();
  const nav = screen.getByRole('navigation', { name: 'Document navigation' });
  fireEvent.click(within(nav).getByRole('link', { name: 'Inbox' }));
  await waitFor(() => {
    expect(server.lists.at(-1)?.get('inbox')).toBe('true');
  });
  expect(server.lists.at(-1)?.get('needs_review')).toBeNull();
  fireEvent.click(await screen.findByRole('button', { name: /Suggestions/ }));
  await waitFor(() => {
    expect(server.lists.at(-1)?.get('needs_review')).toBe('true');
  });
  const path = router.state.location.pathname + router.state.location.search;
  await act(() => router.navigate(-1));
  expect(router.state.location.search).not.toContain('needs_review');
  unmount();
  mount(path);
  expect(
    (await screen.findByRole('button', { name: /Suggestions/ })).getAttribute('aria-pressed'),
  ).toBe('true');
});

it('keeps mobile search and file type visible while secondary controls open on request', async () => {
  await act(() => i18n.changeLanguage('en'));
  stubServer();
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches: query === '(max-width: 767px)',
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  mount('/documents/library?type=t-fattura');
  expect(await screen.findByRole('searchbox', { name: 'Search documents' })).toBeTruthy();
  expect(screen.getByRole('combobox', { name: 'File type' })).toBeTruthy();
  expect(screen.queryByRole('combobox', { name: 'Sort by' })).toBeNull();
  expect(await screen.findByRole('button', { name: /Type: Fattura/ })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Filters' }));
  expect(await screen.findByRole('combobox', { name: 'Sort by' })).toBeTruthy();
  expect(screen.getByRole('group', { name: 'Display' })).toBeTruthy();
});
