import { readJSON, type LLMCatalogModel } from './settingsApi';

export type MediaKind = 'image' | 'video' | 'transcription' | 'speech' | 'embeddings';

// A capability or price the catalogue did not declare is absent, never zero: zero is a real
// price, and an invented limit would clamp nothing the tools actually clamp.

export interface ImageCatalogModel {
  readonly kind: 'image';
  readonly id: string;
  readonly reference_max?: number;
  readonly image_min_usd?: number;
  readonly image_max_usd?: number;
  /** Output-token rate per million for a token-billed model, which has no per-image price. */
  readonly image_token_min_per_1m?: number;
  readonly image_token_max_per_1m?: number;
  readonly has_price: boolean;
}

export interface VideoCatalogModel {
  readonly kind: 'video';
  readonly id: string;
  readonly duration_min?: number;
  readonly duration_max?: number;
  readonly resolutions?: readonly string[];
  readonly image_to_video?: boolean;
  readonly second_min_usd?: number;
  readonly second_max_usd?: number;
  readonly has_price: boolean;
}

/**
 * A speech-to-text ('transcription') or text-to-speech ('speech') model. OpenRouter publishes
 * its rate with no unit, so the row carries the id and nothing a unit would be guessed for.
 */
export interface VoiceCatalogModel {
  readonly kind: 'transcription' | 'speech';
  readonly id: string;
  /** Exact model-specific ids accepted by OpenRouter /audio/speech. */
  readonly voices?: readonly string[];
  readonly has_price: boolean;
}

/**
 * A cloud embedding model. Like the voice rows OpenRouter publishes its rate with no unit, so
 * the row carries the id and nothing a unit would have to be guessed for.
 */
export interface EmbeddingCatalogModel {
  readonly kind: 'embeddings';
  readonly id: string;
  readonly has_price: boolean;
}

export type MediaCatalogModel =
  ImageCatalogModel | VideoCatalogModel | VoiceCatalogModel | EmbeddingCatalogModel;

/** A row of any catalogue the model picker lists. */
export type ModelRow = LLMCatalogModel | MediaCatalogModel;

// The daemon lists the catalogue of the route it runs on, shared with the generation tools,
// so the browser names no base URL and receives no credential.
export async function fetchMediaModels(
  kind: MediaKind,
  refresh = false,
  signal?: AbortSignal,
): Promise<readonly MediaCatalogModel[]> {
  const res = await fetch(`/api/settings/${kind}-models${refresh ? '?refresh=1' : ''}`, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
    signal: signal ?? null,
  });
  const body = await readJSON<{ readonly models?: readonly MediaCatalogModel[] }>(res);
  return body.models ?? [];
}
