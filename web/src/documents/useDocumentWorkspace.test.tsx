import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { useIngestion } from './useDocumentWorkspace';
import type { IngestionState } from './workspaceApi';

afterEach(() => {
  vi.unstubAllGlobals();
});
it('refreshes only the current identity when an observed ingest snapshot changes without polling itself recursively', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData(['documents', 'alice', 'list'], { total: 1 });
  client.setQueryData(['documents', 'bob', 'list'], { total: 1 });
  let observation = 'one';
  const request = vi.fn(() =>
    Promise.resolve(
      new Response(
        JSON.stringify({
          indexing: {
            status: 'ready',
            observed_at: observation,
            in_progress: 8,
            finished: 4,
            errors: 2,
          },
        }),
      ),
    ),
  );
  vi.stubGlobal('fetch', request);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(() => useIngestion('alice'), { wrapper });
  await waitFor(() => {
    expect(result.current.data?.observed_at).toBe('one');
  });
  expect(client.getQueryState(['documents', 'alice', 'list'])?.isInvalidated).toBe(false);
  observation = 'two';
  await act(() => result.current.refetch());
  await waitFor(() => {
    expect(client.getQueryState(['documents', 'alice', 'list'])?.isInvalidated).toBe(true);
  });
  expect(client.getQueryState(['documents', 'bob', 'list'])?.isInvalidated).toBe(false);
  expect(request).toHaveBeenCalledTimes(2);
  expect(result.current.data).toMatchObject({ status: 'ready', in_progress: 8, errors: 2 });
});

function ingestionFixture(initial: IngestionState | null) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const keys = ['documents', 'document', 'documents-content'];
  for (const key of keys) {
    for (const identity of ['alice', 'bob']) {
      client.setQueryData([key, identity, 'cached'], { total: 0 });
    }
  }
  let snapshot = initial;
  let failed = false;
  vi.stubGlobal('fetch', () =>
    Promise.resolve(
      failed
        ? new Response(JSON.stringify({ error: 'temporarily unavailable' }), { status: 503 })
        : new Response(JSON.stringify({ indexing: snapshot })),
    ),
  );
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return {
    client,
    wrapper,
    observe(value: IngestionState | null) {
      snapshot = value;
    },
    fail(value: boolean) {
      failed = value;
    },
    expectInvalidation(identity: string, invalidated: boolean) {
      for (const key of keys) {
        expect(client.getQueryState([key, identity, 'cached'])?.isInvalidated).toBe(invalidated);
      }
    },
  };
}

const ready: IngestionState = { status: 'ready', in_progress: 0, finished: 1, errors: 0 };

it.each([
  { from: null, to: ready, transition: 'empty to indexed' },
  { from: ready, to: null, transition: 'indexed to empty' },
])(
  'refreshes current document caches on a successful $transition snapshot',
  async ({ from, to }) => {
    const fixture = ingestionFixture(from);
    const { result } = renderHook(() => useIngestion('alice'), { wrapper: fixture.wrapper });
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    fixture.expectInvalidation('alice', false);
    fixture.observe(to);
    await act(() => result.current.refetch());
    await waitFor(() => {
      fixture.expectInvalidation('alice', true);
    });
    fixture.expectInvalidation('bob', false);
  },
);

it('keeps cached documents fresh across repeated successful empty snapshots', async () => {
  const fixture = ingestionFixture(null);
  const { result } = renderHook(() => useIngestion('alice'), { wrapper: fixture.wrapper });
  await waitFor(() => {
    expect(result.current.isSuccess).toBe(true);
  });
  await act(() => result.current.refetch());
  fixture.expectInvalidation('alice', false);
  fixture.expectInvalidation('bob', false);
});

it('does not treat loading or failed initial requests as a successful empty snapshot', async () => {
  const fixture = ingestionFixture(ready);
  fixture.fail(true);
  const { result } = renderHook(() => useIngestion('alice'), { wrapper: fixture.wrapper });
  expect(result.current.data).toBeUndefined();
  fixture.expectInvalidation('alice', false);
  await waitFor(() => {
    expect(result.current.isError).toBe(true);
  });
  fixture.fail(false);
  await act(() => result.current.refetch());
  await waitFor(() => {
    expect(result.current.data).toEqual(ready);
  });
  fixture.expectInvalidation('alice', false);
  fixture.expectInvalidation('bob', false);
});

it('starts a separate observation when identity changes after an empty snapshot', async () => {
  const fixture = ingestionFixture(null);
  const { result, rerender } = renderHook(({ identity }) => useIngestion(identity), {
    initialProps: { identity: 'alice' },
    wrapper: fixture.wrapper,
  });
  await waitFor(() => {
    expect(result.current.isSuccess).toBe(true);
  });
  fixture.observe(ready);
  rerender({ identity: 'bob' });
  await waitFor(() => {
    expect(result.current.data).toEqual(ready);
  });
  fixture.expectInvalidation('alice', false);
  fixture.expectInvalidation('bob', false);
  fixture.observe(null);
  await act(() => result.current.refetch());
  await waitFor(() => {
    fixture.expectInvalidation('bob', true);
  });
  fixture.expectInvalidation('alice', false);
});
