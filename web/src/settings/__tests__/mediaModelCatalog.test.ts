import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchMediaModels, type MediaCatalogModel } from '../mediaModelCatalog';

const IMAGE_MODELS: readonly MediaCatalogModel[] = [
  { kind: 'image', id: 'microsoft/mai-image-2.6', has_price: false, reference_max: 5 },
];

function stubFetch(response: Response) {
  const mock = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => Promise.resolve(response));
  vi.stubGlobal('fetch', mock);
  return mock;
}

describe('fetchMediaModels', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads the catalogue of the route the daemon runs on, never naming a base URL', async () => {
    const fetchMock = stubFetch(
      new Response(JSON.stringify({ models: IMAGE_MODELS }), { status: 200 }),
    );

    await expect(fetchMediaModels('image')).resolves.toEqual(IMAGE_MODELS);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe('/api/settings/image-models');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.headers).toEqual({ Accept: 'application/json' });
    expect(init?.signal).toBeNull();
  });

  it('asks the daemon to bypass its cache on a refresh and forwards the abort signal', async () => {
    const fetchMock = stubFetch(new Response(JSON.stringify({ models: [] }), { status: 200 }));
    const controller = new AbortController();

    await expect(fetchMediaModels('video', true, controller.signal)).resolves.toEqual([]);

    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe('/api/settings/video-models?refresh=1');
    expect(init?.signal).toBe(controller.signal);
  });

  it('treats a body with no model list as an empty catalogue', async () => {
    stubFetch(new Response('{}', { status: 200 }));
    await expect(fetchMediaModels('image')).resolves.toEqual([]);
  });

  it('surfaces the daemon’s reason when the catalogue cannot be read', async () => {
    stubFetch(
      new Response(
        JSON.stringify({ error: 'media model catalog unavailable: GET videos/models: 503' }),
        {
          status: 502,
        },
      ),
    );
    await expect(fetchMediaModels('video')).rejects.toThrow('GET videos/models: 503');
  });
});
