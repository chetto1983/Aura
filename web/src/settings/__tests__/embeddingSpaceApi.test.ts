import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  EmbeddingRouteRejected,
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

  it('carries the refusals of an apply the daemon re-probed and refused', async () => {
    stub(422, { error: 'route_refused', refusals: [{ code: 'probe_failed', detail: 'HTTP 429' }] });
    const rejected = await applyEmbeddingRoute(route, 'es1-target').catch((err: unknown) => err);
    expect(rejected).toBeInstanceOf(EmbeddingRouteRejected);
    expect((rejected as EmbeddingRouteRejected).refusals).toEqual([
      { code: 'probe_failed', detail: 'HTTP 429' },
    ]);
  });

  it('keeps any other failure a plain error with the daemon’s message', async () => {
    stub(409, { error: 'idempotency key reused' });
    const rejected = await applyEmbeddingRoute(route, 'es1-target').catch((err: unknown) => err);
    expect(rejected).not.toBeInstanceOf(EmbeddingRouteRejected);
    expect((rejected as Error).message).toBe('idempotency key reused');
  });
});
