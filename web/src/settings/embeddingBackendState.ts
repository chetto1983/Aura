export type EmbeddingBackendChoice = 'local' | 'openrouter' | 'manual';

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
