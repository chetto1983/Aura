import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { useDocumentUploads } from './useDocumentUploads';

afterEach(() => {
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      {children}
    </QueryClientProvider>
  );
}
const json = (body: unknown) => new Response(JSON.stringify(body));
it('retries only a failed item and reuses its collision-safe folder', async () => {
  const folders: string[] = [];
  let failed = true;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string) => {
      const url = new URL(input, 'http://test');
      if (url.pathname.includes('upload')) {
        folders.push(url.searchParams.get('id') ?? '');
        if (failed) {
          failed = false;
          return Promise.resolve(
            new Response(JSON.stringify({ error: 'Store unavailable' }), { status: 503 }),
          );
        }
        return Promise.resolve(json({ id: `${folders.at(-1) ?? ''}one.pdf` }));
      }
      return Promise.resolve(json({ rows: [], total: 0, facets: {} }));
    }),
  );
  const { result } = renderHook(() => useDocumentUploads('alice'), { wrapper });
  act(() => {
    result.current.add([new File(['a'], 'one.pdf')]);
  });
  await waitFor(() => {
    expect(result.current.items[0]?.status).toBe('failed');
  });
  expect(result.current.items[0]?.error).toBe('Store unavailable');
  act(() => {
    result.current.retry(result.current.items[0]?.key ?? '');
  });
  await waitFor(() => {
    expect(result.current.items[0]?.status).toBe('waiting');
  });
  expect(folders).toHaveLength(2);
  expect(folders[0]).toBe(folders[1]);
});
it('uploads same-named entries to different folders and polls the returned keys across all file types', async () => {
  const uploads: string[] = [];
  const lookups: URLSearchParams[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: string, init?: RequestInit) => {
      const url = new URL(input, 'http://test');
      if (url.pathname.includes('upload')) {
        expect(init?.body).toBeInstanceOf(FormData);
        expect(init?.headers).not.toHaveProperty('Content-Type');
        uploads.push(url.searchParams.get('id') ?? '');
        return Promise.resolve(
          json({ id: `${uploads.at(-1) ?? ''}server-name.mp4`, type: 'file' }),
        );
      }
      lookups.push(url.searchParams);
      return Promise.resolve(
        json({
          rows: uploads.map((folder, i) => ({
            id: `d${String(i)}`,
            source_key: `${folder.slice(1)}server-name.mp4`,
          })),
          total: uploads.length,
          facets: {},
        }),
      );
    }),
  );
  const { result } = renderHook(() => useDocumentUploads('alice'), { wrapper });
  act(() => {
    result.current.add([new File(['a'], 'same.mp4'), new File(['b'], 'same.mp4')]);
  });
  await waitFor(() => {
    expect(result.current.items.every((item) => item.status === 'ready')).toBe(true);
  });
  expect(uploads).toHaveLength(2);
  expect(new Set(uploads).size).toBe(2);
  expect(lookups.every((query) => query.get('file_type') === 'all')).toBe(true);
  expect(lookups.flatMap((query) => query.getAll('source_key'))).toContain(
    `${uploads[0]?.slice(1) ?? ''}server-name.mp4`,
  );
});
it('does not continue queued files after identity changes', async () => {
  let finish: (response: Response) => void = () => undefined;
  const request = vi.fn(
    () =>
      new Promise<Response>((resolve) => {
        finish = resolve;
      }),
  );
  vi.stubGlobal('fetch', request);
  const hook = renderHook(({ identity }) => useDocumentUploads(identity), {
    wrapper,
    initialProps: { identity: 'alice' },
  });
  act(() => {
    hook.result.current.add([new File(['a'], 'one.pdf'), new File(['b'], 'two.pdf')]);
  });
  await waitFor(() => {
    expect(request).toHaveBeenCalledTimes(1);
  });
  hook.rerender({ identity: 'bob' });
  await act(async () => {
    finish(json({ id: '/uploads/old/one.pdf' }));
    await Promise.resolve();
  });
  expect(request).toHaveBeenCalledTimes(1);
  expect(hook.result.current.items).toEqual([]);
});
it('resumes stored successful uploads by lookup without replaying bytes after refresh', async () => {
  sessionStorage.setItem(
    'aura.documents.uploads.v1.alice',
    JSON.stringify([
      {
        key: 'one',
        name: 'one.pdf',
        folder: '/uploads/one/',
        sourceKey: 'uploads/one/one.pdf',
        status: 'waiting',
      },
    ]),
  );
  const requests = vi.fn((_input: string) =>
    Promise.resolve(json({ rows: [], total: 0, facets: {} })),
  );
  vi.stubGlobal('fetch', requests);
  const { result } = renderHook(() => useDocumentUploads('alice'), { wrapper });
  await waitFor(() => {
    expect(requests).toHaveBeenCalled();
  });
  expect(result.current.items[0]?.status).toBe('waiting');
  expect(requests.mock.calls.every((call) => call[0].startsWith('/api/documents?'))).toBe(true);
});

it('requires reselecting file bytes after a failed upload is restored', () => {
  sessionStorage.setItem(
    'aura.documents.uploads.v1.alice',
    JSON.stringify([
      {
        key: 'failed',
        name: 'failed.pdf',
        folder: '/uploads/failed/',
        status: 'failed',
        error: 'offline',
      },
    ]),
  );
  const request = vi.fn();
  vi.stubGlobal('fetch', request);
  const { result } = renderHook(() => useDocumentUploads('alice'), { wrapper });
  expect(result.current.items[0]?.status).toBe('needs_file');
  act(() => {
    result.current.retry('failed');
  });
  expect(request).not.toHaveBeenCalled();
});
