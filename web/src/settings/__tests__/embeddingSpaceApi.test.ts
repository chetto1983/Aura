import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  applyEmbeddingRoute,
  fetchEmbeddingSpace,
  previewEmbeddingRoute,
} from '../embeddingSpaceApi';

const route = {
  AURA_EMBED_BASE_URL: 'http://aura-llama-embed:8081',
  AURA_EMBED_MODEL: 'vendor/embed',
  AURA_EMBED_CLOUD_BASE_URL: '',
};

function stub(status: number, body: unknown) {
  const calls: { url: string; init: RequestInit | undefined }[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      calls.push({ url, init });
      return Promise.resolve(
        new Response(JSON.stringify(body), {
          status,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }),
  );
  return calls;
}

describe('embeddingSpaceApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads the embedding space with same-origin credentials', async () => {
    const calls = stub(200, { space: 'es1-a', tenants: [] });
    expect((await fetchEmbeddingSpace()).space).toBe('es1-a');
    expect(calls[0]?.url).toBe('/api/settings/embedding-space');
    expect(calls[0]?.init?.credentials).toBe('same-origin');
  });

  it('posts the route to preview it, and the route with its confirmed space to apply it', async () => {
    const calls = stub(200, { space: 'es1-target', refusals: [] });
    await previewEmbeddingRoute(route);
    await applyEmbeddingRoute(route, 'es1-target');
    expect(calls.map((call) => [call.url, call.init?.method])).toEqual([
      ['/api/settings/embedding-route/preview', 'POST'],
      ['/api/settings/embedding-route', 'POST'],
    ]);
    const body = calls[1]?.init?.body;
    expect(typeof body === 'string' ? JSON.parse(body) : body).toEqual({
      ...route,
      confirm_space: 'es1-target',
    });
  });

  it('surfaces the reason the daemon gives for a refused apply', async () => {
    stub(409, { error: 'space_changed', space: 'es1-other' });
    await expect(applyEmbeddingRoute(route, 'es1-target')).rejects.toThrow('space_changed');
  });
});
