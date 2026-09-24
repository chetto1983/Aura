import { describe, expect, it } from 'vitest';
import {
  EMBEDDING_ROUTE_KEYS,
  embeddingBackendChoice,
  embeddingBackendValues,
  embeddingRouteOf,
  normalizeEmbeddingCloudBaseURL,
  sameEmbeddingRoute,
} from '../embeddingBackendState';

describe('embedding backend state', () => {
  it('reads the route the three rows name, trimmed, and compares two of them', () => {
    const route = embeddingRouteOf({
      AURA_EMBED_MODEL: ' vendor/embed ',
      AURA_STT_CLOUD_MODEL: 'x',
    });
    expect(route).toEqual({
      AURA_EMBED_BASE_URL: '',
      AURA_EMBED_MODEL: 'vendor/embed',
      AURA_EMBED_CLOUD_BASE_URL: '',
    });
    expect(sameEmbeddingRoute(route, { ...route })).toBe(true);
    expect(sameEmbeddingRoute(route, { ...route, AURA_EMBED_BASE_URL: 'http://embed:8081' })).toBe(
      false,
    );
    expect(sameEmbeddingRoute(route, { ...route, AURA_EMBED_MODEL: '' })).toBe(false);
    expect(
      sameEmbeddingRoute(route, { ...route, AURA_EMBED_CLOUD_BASE_URL: 'https://e.example' }),
    ).toBe(false);
    expect([...EMBEDDING_ROUTE_KEYS].sort()).toEqual(Object.keys(route).sort());
  });

  it('derives the route arm from the model switch and cloud endpoint', () => {
    expect(embeddingBackendChoice('', '')).toBe('local');
    expect(embeddingBackendChoice('qwen/embed', '')).toBe('openrouter');
    expect(embeddingBackendChoice('qwen/embed', 'https://embed.example')).toBe('manual');
  });

  it('clears values owned by the previous embedding route arm', () => {
    expect(embeddingBackendValues('local')).toEqual({
      AURA_EMBED_MODEL: '',
      AURA_EMBED_CLOUD_BASE_URL: '',
    });
    expect(embeddingBackendValues('openrouter')).toEqual({
      AURA_EMBED_CLOUD_BASE_URL: '',
    });
    expect(embeddingBackendValues('manual')).toEqual({
      AURA_EMBED_CLOUD_BASE_URL: '',
    });
  });

  it('strips the /v1 suffix the embedding client appends itself', () => {
    expect(normalizeEmbeddingCloudBaseURL(' https://embed.example/v1/ ')).toBe(
      'https://embed.example',
    );
  });
});
