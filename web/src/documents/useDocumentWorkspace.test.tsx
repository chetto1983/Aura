import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { useIngestion } from './useDocumentWorkspace';

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
