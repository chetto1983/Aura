import { describe, expect, it } from 'vitest';
import {
  embeddingBackendChoice,
  embeddingBackendValues,
  normalizeEmbeddingCloudBaseURL,
} from '../embeddingBackendState';

describe('embedding backend state', () => {
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
