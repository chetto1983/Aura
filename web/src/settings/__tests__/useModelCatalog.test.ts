import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useModelCatalog } from '../useModelCatalog';

const MODELS = [{ id: 'gemma-4-12b', context_window: 131_072, has_price: false }];

function stubFetch(impl: () => Promise<Response>) {
  const mock = vi.fn(impl);
  vi.stubGlobal('fetch', mock);
  return mock;
}

describe('useModelCatalog', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('probes the form route and reports what it serves', async () => {
    const fetchMock = stubFetch(() =>
      Promise.resolve(new Response(JSON.stringify({ models: MODELS }), { status: 200 })),
    );
    const { result } = renderHook(() =>
      useModelCatalog('llamacpp', 'http://host.docker.internal:8084/v1'),
    );

    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });
    expect(result.current.models).toEqual(MODELS);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('does not probe a route that cannot be one', async () => {
    // The base URL is a text box: a half-typed value must not cost a round trip and a 400.
    const fetchMock = stubFetch(() => Promise.resolve(new Response('{}', { status: 200 })));
    for (const [provider, baseURL] of [
      ['llamacpp', 'http://'],
      ['llamacpp', '/v1'],
      ['llamacpp', ''],
      ['llamacpp', 'ftp://host/v1'],
      ['', 'http://host:8084/v1'],
    ] as const) {
      const { result } = renderHook(() => useModelCatalog(provider, baseURL));
      await waitFor(() => {
        expect(result.current.status).toBe('idle');
      });
    }
    await new Promise((resolve) => setTimeout(resolve, 750));
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('keeps the failure reason instead of an empty list with no explanation', async () => {
    stubFetch(() =>
      Promise.resolve(
        new Response(JSON.stringify({ error: 'GET /models returned 401' }), { status: 502 }),
      ),
    );
    const { result } = renderHook(() =>
      useModelCatalog('openrouter', 'https://openrouter.ai/api/v1'),
    );

    await waitFor(() => {
      expect(result.current.status).toBe('error');
    });
    expect(result.current.error).toContain('401');
    expect(result.current.models).toEqual([]);
  });

  it('re-probes on demand without waiting out the debounce', async () => {
    const fetchMock = stubFetch(() =>
      Promise.resolve(new Response(JSON.stringify({ models: MODELS }), { status: 200 })),
    );
    const { result } = renderHook(() =>
      useModelCatalog('ollama', 'http://host.docker.internal:11434/v1'),
    );
    await waitFor(() => {
      expect(result.current.status).toBe('ready');
    });

    act(() => {
      result.current.reload();
    });
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(2);
    });
  });
});
