import type { EmbeddingRoute } from './embeddingSpaceApi';

export type EmbeddingBackendChoice = 'local' | 'openrouter' | 'manual';

/** The rows that decide the route; they are written together, by the confirmed apply only. */
export const EMBEDDING_ROUTE_KEYS: ReadonlySet<string> = new Set([
  'AURA_EMBED_BASE_URL',
  'AURA_EMBED_MODEL',
  'AURA_EMBED_CLOUD_BASE_URL',
]);

export function embeddingRouteOf(
  values: Readonly<Record<string, string | undefined>>,
): EmbeddingRoute {
  return {
    AURA_EMBED_BASE_URL: (values.AURA_EMBED_BASE_URL ?? '').trim(),
    AURA_EMBED_MODEL: (values.AURA_EMBED_MODEL ?? '').trim(),
    AURA_EMBED_CLOUD_BASE_URL: (values.AURA_EMBED_CLOUD_BASE_URL ?? '').trim(),
  };
}

export function sameEmbeddingRoute(a: EmbeddingRoute, b: EmbeddingRoute): boolean {
  return (
    a.AURA_EMBED_BASE_URL === b.AURA_EMBED_BASE_URL &&
    a.AURA_EMBED_MODEL === b.AURA_EMBED_MODEL &&
    a.AURA_EMBED_CLOUD_BASE_URL === b.AURA_EMBED_CLOUD_BASE_URL
  );
}

export function embeddingBackendChoice(
  model: string,
  cloudBaseURL: string,
): EmbeddingBackendChoice {
  if (model.trim() === '') return 'local';
  return cloudBaseURL.trim() === '' ? 'openrouter' : 'manual';
}

/** Values an arm owns and must clear when an operator changes route. */
export function embeddingBackendValues(
  choice: EmbeddingBackendChoice,
): Partial<Record<'AURA_EMBED_MODEL' | 'AURA_EMBED_CLOUD_BASE_URL', string>> {
  switch (choice) {
    case 'local':
      return { AURA_EMBED_MODEL: '', AURA_EMBED_CLOUD_BASE_URL: '' };
    case 'openrouter':
    case 'manual':
      return { AURA_EMBED_CLOUD_BASE_URL: '' };
  }
}

// EmbeddingClient appends /v1/embeddings itself, so the base must not retain /v1.
export function normalizeEmbeddingCloudBaseURL(value: string): string {
  return value.trim().replace(/\/v1\/?$/i, '');
}
