import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useMediaModelCatalog } from '../useMediaModelCatalog';
import type { MediaCatalogModel } from '../mediaModelCatalog';

const VIDEO_MODELS: readonly MediaCatalogModel[] = [
  {
    kind: 'video',
    id: 'minimax/hailuo-3-max',
    has_price: true,
    second_min_usd: 0.05,
    second_max_usd: 0.08,
    duration_min: 5,
    duration_max: 15,
    resolutions: ['768p', '480p'],
    image_to_video: true,
  },
];

const CLOUD_ROUTE = 'openrouter https://openrouter.ai/api/v1';

interface FetchCall {
  readonly url: string;
  readonly signal: AbortSignal | null | undefined;
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status });
}

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function stubFetch(answer: (call: FetchCall) => Promise<Response>) {
  const calls: FetchCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const call = { url: urlOf(input), signal: init?.signal };
      calls.push(call);
      return answer(call);
    }),
  );
  return calls;
}

// pendingUntilAborted never answers; an abort rejects it the way the browser does, so the
// hook's invalidation is exercised on a real AbortSignal.
function pendingUntilAborted(call: FetchCall): Promise<Response> {
  return new Promise<Response>((_resolve, fail) => {
    call.signal?.addEventListener('abort', () => {
      fail(new DOMException('The operation was aborted.', 'AbortError'));
    });
  });
}

describe('useMediaModelCatalog', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('lists the kind’s catalogue while cloud mode is active', async () => {
    const calls = stubFetch(() => Promise.resolve(jsonResponse({ models: VIDEO_MODELS })));
    const { result } = renderHook(() => useMediaModelCatalog('video', true, CLOUD_ROUTE));

    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });
    expect(result.current.models).toEqual(VIDEO_MODELS);
    expect(result.current.error).toBeUndefined();
    expect(calls.map((call) => call.url)).toEqual(['/api/settings/video-models']);
  });

  it('asks nothing while cloud mode is off', async () => {
    const calls = stubFetch(() => Promise.resolve(jsonResponse({ models: VIDEO_MODELS })));
    const { result } = renderHook(() => useMediaModelCatalog('image', false, CLOUD_ROUTE));

    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(result.current.status).toBe('idle');
    expect(result.current.models).toEqual([]);
    expect(calls).toEqual([]);
  });

  it('bypasses the daemon cache when the operator refreshes', async () => {
    const calls = stubFetch(() => Promise.resolve(jsonResponse({ models: VIDEO_MODELS })));
    const { result } = renderHook(() => useMediaModelCatalog('image', true, CLOUD_ROUTE));
    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });

    act(() => {
      result.current.reload();
    });
    await waitFor(() => {
      expect(calls.map((call) => call.url)).toEqual([
        '/api/settings/image-models',
        '/api/settings/image-models?refresh=1',
      ]);
    });
    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });
  });

  it('keeps the failure reason instead of an empty list with no explanation', async () => {
    stubFetch(() =>
      Promise.resolve(
        jsonResponse(
          { error: 'image and video models are listed only on the OpenRouter route' },
          409,
        ),
      ),
    );
    const { result } = renderHook(() => useMediaModelCatalog('video', true, CLOUD_ROUTE));

    await waitFor(() => {
      expect(result.current.status).toBe('error');
    });
    expect(result.current.error).toContain('OpenRouter route');
    expect(result.current.models).toEqual([]);
  });

  it('names the status when a failure carries no message', async () => {
    stubFetch(() => Promise.reject(new Error('')));
    const { result } = renderHook(() => useMediaModelCatalog('video', true, CLOUD_ROUTE));

    await waitFor(() => {
      expect(result.current.status).toBe('error');
    });
    expect(result.current.error).toBe('Error');
  });

  it('abandons an in-flight answer when cloud mode turns off', async () => {
    const calls = stubFetch(pendingUntilAborted);
    const { result, rerender } = renderHook(
      ({ enabled }) => useMediaModelCatalog('video', enabled, CLOUD_ROUTE),
      {
        initialProps: { enabled: true },
      },
    );
    await waitFor(() => {
      expect(result.current.status).toBe('loading');
    });

    rerender({ enabled: false });

    await waitFor(() => {
      expect(result.current.status).toBe('idle');
    });
    expect(calls[0]?.signal?.aborted).toBe(true);
    expect(result.current.models).toEqual([]);
    expect(result.current.error).toBeUndefined();
  });

  it('never lets a superseded answer overwrite the newer one', async () => {
    let first = true;
    let releaseStale: (response: Response) => void = () => undefined;
    const calls = stubFetch(() => {
      if (!first) return Promise.resolve(jsonResponse({ models: VIDEO_MODELS }));
      first = false;
      // A server that ignores the abort still answers late; the sequence ticket is what
      // keeps its stale list off the screen.
      return new Promise<Response>((resolve) => {
        releaseStale = resolve;
      });
    });
    const { result } = renderHook(() => useMediaModelCatalog('video', true, CLOUD_ROUTE));
    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });

    act(() => {
      result.current.reload();
    });
    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });
    await act(async () => {
      releaseStale(jsonResponse({ models: [] }));
      await Promise.resolve();
    });

    expect(calls[0]?.signal?.aborted).toBe(true);
    expect(result.current.models).toEqual(VIDEO_MODELS);
    expect(result.current.status).toBe('ready');
  });

  it('keeps the listed models on screen while a refresh is in flight', async () => {
    let answered = 0;
    stubFetch((call) => {
      answered += 1;
      return answered === 1
        ? Promise.resolve(jsonResponse({ models: VIDEO_MODELS }))
        : pendingUntilAborted(call);
    });
    const { result } = renderHook(() => useMediaModelCatalog('video', true, CLOUD_ROUTE));
    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });

    act(() => {
      result.current.reload();
    });

    expect(result.current.status).toBe('loading');
    expect(result.current.models).toEqual(VIDEO_MODELS);
  });

  it('reads the daemon cache again when the rows come back after a refresh', async () => {
    const calls = stubFetch(() => Promise.resolve(jsonResponse({ models: VIDEO_MODELS })));
    const { result, rerender } = renderHook(
      ({ enabled }) => useMediaModelCatalog('video', enabled, CLOUD_ROUTE),
      { initialProps: { enabled: true } },
    );
    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });
    act(() => {
      result.current.reload();
    });
    await waitFor(() => {
      expect(calls).toHaveLength(2);
    });

    rerender({ enabled: false });
    rerender({ enabled: true });

    await waitFor(() => {
      expect(calls.map((call) => call.url)).toEqual([
        '/api/settings/video-models',
        '/api/settings/video-models?refresh=1',
        '/api/settings/video-models',
      ]);
    });
  });

  it('never shows one kind’s models as another’s', async () => {
    const calls = stubFetch((call) =>
      call.url.startsWith('/api/settings/video-models')
        ? Promise.resolve(jsonResponse({ models: VIDEO_MODELS }))
        : pendingUntilAborted(call),
    );
    const { result, rerender } = renderHook(
      ({ kind }) => useMediaModelCatalog(kind, true, CLOUD_ROUTE),
      {
        initialProps: { kind: 'video' as 'image' | 'video' },
      },
    );
    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });

    rerender({ kind: 'image' });

    expect(result.current.status).toBe('loading');
    expect(result.current.models).toEqual([]);
    await waitFor(() => {
      expect(calls.map((call) => call.url)).toEqual([
        '/api/settings/video-models',
        '/api/settings/image-models',
      ]);
    });
  });

  it('reads as loading, not as the earlier answer, while the rows come back', async () => {
    let answered = 0;
    stubFetch((call) => {
      answered += 1;
      return answered === 1
        ? Promise.resolve(jsonResponse({ error: 'GET videos/models: 503' }, 502))
        : pendingUntilAborted(call);
    });
    const { result, rerender } = renderHook(
      ({ enabled }) => useMediaModelCatalog('video', enabled, CLOUD_ROUTE),
      { initialProps: { enabled: true } },
    );
    await waitFor(() => {
      expect(result.current.status).toBe('error');
    });

    rerender({ enabled: false });
    expect(result.current.status).toBe('idle');
    rerender({ enabled: true });

    expect(result.current.status).toBe('loading');
    expect(result.current.error).toBeUndefined();
  });

  it('asks again when the saved route changes, without the old route’s models', async () => {
    const calls = stubFetch((call) =>
      calls.length === 1
        ? Promise.resolve(jsonResponse({ models: VIDEO_MODELS }))
        : pendingUntilAborted(call),
    );
    const { result, rerender } = renderHook(
      ({ route }) => useMediaModelCatalog('video', true, route),
      { initialProps: { route: CLOUD_ROUTE } },
    );
    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });

    rerender({ route: 'openrouter https://eu.openrouter.ai/api/v1' });

    expect(result.current.status).toBe('loading');
    expect(result.current.models).toEqual([]);
    await waitFor(() => {
      expect(calls).toHaveLength(2);
    });
    expect(calls[1]?.url).toBe('/api/settings/video-models');
  });

  it('cancels its request when the pane unmounts', async () => {
    const calls = stubFetch(pendingUntilAborted);
    const { unmount } = renderHook(() => useMediaModelCatalog('image', true, CLOUD_ROUTE));
    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });

    unmount();

    expect(calls[0]?.signal?.aborted).toBe(true);
  });
});
