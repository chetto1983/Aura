import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { StudioRecord } from '../studioApi';
import {
  STUDIO_HISTORY_POLL_MS,
  hasActiveRecord,
  useCreateStudioVideo,
  useStudioHistory,
  useStudioLibrary,
  useStudioModels,
} from '../useStudio';

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function record(id: string, status: string): StudioRecord {
  return {
    id,
    kind: 'video',
    status,
    model: 'google/veo-3.1-lite',
    prompt: 'a cat in a hat',
    used: {},
    created_at: '2026-09-17T10:00:00Z',
  };
}

function jsonBody(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

/** A fetch that answers each Studio route from one place, and records every URL it was asked
 *  for, so a hook that calls the wrong route or the wrong page is visible. */
function studioFetch(records: readonly StudioRecord[] = []) {
  const urls: string[] = [];
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = urlOf(input);
    urls.push(url);
    if (url.startsWith('/api/studio/models')) {
      return Promise.resolve(jsonBody({ default: 'veo', models: [{ id: 'veo' }] }));
    }
    if (url.startsWith('/api/studio/history')) return Promise.resolve(jsonBody({ records }));
    if (url.startsWith('/api/studio/library')) return Promise.resolve(jsonBody({ assets: [] }));
    return Promise.resolve(jsonBody({ id: 'job-new' }));
  });
  vi.stubGlobal('fetch', fetchMock);
  return { fetchMock, urls };
}

describe('hasActiveRecord', () => {
  it('is true only while a loaded page still holds an unfinished generation', () => {
    expect(hasActiveRecord(undefined)).toBe(false);
    expect(hasActiveRecord([[]])).toBe(false);
    expect(hasActiveRecord([[record('a', 'completed')], [record('b', 'failed')]])).toBe(false);
    expect(hasActiveRecord([[record('a', 'completed')], [record('b', 'in_progress')]])).toBe(true);
    expect(hasActiveRecord([[record('a', 'pending')]])).toBe(true);
  });

  it('polls every 5 seconds', () => {
    expect(STUDIO_HISTORY_POLL_MS).toBe(5000);
  });
});

describe('useStudioModels', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('asks for one kind and reads the deployment default beside the rows', async () => {
    const { urls } = studioFetch();

    const { result } = renderHook(() => useStudioModels('image'), { wrapper: wrapper() });

    await waitFor(() => {
      expect(result.current.data).toBeDefined();
    });
    expect(urls).toEqual(['/api/studio/models?kind=image']);
    expect(result.current.data?.default).toBe('veo');
  });

  it('caches per kind, so switching kinds never serves the other catalog', async () => {
    const { urls } = studioFetch();
    const Wrapper = wrapper();

    const video = renderHook(() => useStudioModels('video'), { wrapper: Wrapper });
    await waitFor(() => {
      expect(video.result.current.data).toBeDefined();
    });
    const image = renderHook(() => useStudioModels('image'), { wrapper: Wrapper });
    await waitFor(() => {
      expect(image.result.current.data).toBeDefined();
    });

    // Two reads against one client: a key that forgot the kind would answer the second from
    // the first one's cache and offer video models to an image composer.
    expect(urls).toEqual(['/api/studio/models?kind=video', '/api/studio/models?kind=image']);
  });
});

describe('useStudioHistory', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('loads the first page for its kind and stops when the page is short', async () => {
    const { urls } = studioFetch([record('job-1', 'completed')]);

    const { result } = renderHook(() => useStudioHistory('video'), { wrapper: wrapper() });

    await waitFor(() => {
      expect(result.current.data?.pages).toHaveLength(1);
    });
    expect(urls).toEqual(['/api/studio/history?limit=24&kind=video']);
    // A page shorter than the server's own page size is the last one: offering "load more"
    // there costs a round trip that can only answer nothing.
    expect(result.current.hasNextPage).toBe(false);
  });

  it('pages on the oldest row of a full page', async () => {
    const full = Array.from({ length: 24 }, (_, index) => record(`job-${String(index)}`, 'failed'));
    const { urls } = studioFetch(full);

    const { result } = renderHook(() => useStudioHistory(undefined), { wrapper: wrapper() });

    await waitFor(() => {
      expect(result.current.hasNextPage).toBe(true);
    });
    // No kind filter when the panel shows everything.
    expect(urls).toEqual(['/api/studio/history?limit=24']);

    await result.current.fetchNextPage();

    await waitFor(() => {
      expect(urls).toHaveLength(2);
    });
    expect(urls[1]).toBe('/api/studio/history?limit=24&before=job-23');
  });

  it('keeps one list per kind rather than one list for all of them', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const kind = urlOf(input).includes('kind=image') ? 'image' : 'video';
        return Promise.resolve(jsonBody({ records: [record(`job-${kind}`, 'completed')] }));
      }),
    );
    const Wrapper = wrapper();

    const video = renderHook(() => useStudioHistory('video'), { wrapper: Wrapper });
    await waitFor(() => {
      expect(video.result.current.data?.pages).toHaveLength(1);
    });
    const image = renderHook(() => useStudioHistory('image'), { wrapper: Wrapper });
    await waitFor(() => {
      expect(image.result.current.data?.pages[0]?.[0]?.id).toBe('job-image');
    });

    // One shared key would hand the image list to the video panel the moment it loaded.
    expect(video.result.current.data?.pages[0]?.[0]?.id).toBe('job-video');
  });
});

describe('the create mutations', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('refreshes the history once the record exists', async () => {
    const { urls } = studioFetch([record('job-1', 'completed')]);
    const Wrapper = wrapper();
    const history = renderHook(() => useStudioHistory('video'), { wrapper: Wrapper });
    await waitFor(() => {
      expect(history.result.current.data?.pages).toHaveLength(1);
    });
    const historyCalls = urls.filter((url) => url.startsWith('/api/studio/history')).length;

    const create = renderHook(() => useCreateStudioVideo(), { wrapper: Wrapper });
    await create.result.current.mutateAsync({ model: 'veo', prompt: 'a cat in a hat' });

    // The new record is only visible once the list is asked again — without this the operator
    // presses Generate and nothing appears.
    await waitFor(() => {
      expect(urls.filter((url) => url.startsWith('/api/studio/history')).length).toBeGreaterThan(
        historyCalls,
      );
    });
    expect(urls).toContain('/api/studio/videos');
  });
});

describe('useStudioLibrary', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads the library only when the caller asks for it', async () => {
    const { urls } = studioFetch();

    const closed = renderHook(() => useStudioLibrary(false), { wrapper: wrapper() });
    await Promise.resolve();
    // The picker is behind a popover: opening the page must not list the library.
    expect(urls).toEqual([]);
    expect(closed.result.current.data).toBeUndefined();

    const open = renderHook(() => useStudioLibrary(true), { wrapper: wrapper() });
    await waitFor(() => {
      expect(open.result.current.data).toBeDefined();
    });
    expect(urls).toEqual(['/api/studio/library']);
  });
});
