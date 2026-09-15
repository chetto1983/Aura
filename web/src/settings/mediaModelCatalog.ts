import { readJSON, type LLMCatalogModel } from './settingsApi';

export type MediaKind = 'image' | 'video';

// A capability or price the catalogue did not declare is absent, never zero: zero is a real
// price, and an invented limit would clamp nothing the tools actually clamp.

export interface ImageCatalogModel {
  readonly kind: 'image';
  readonly id: string;
  readonly reference_max?: number;
  readonly image_min_usd?: number;
  readonly image_max_usd?: number;
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

export type MediaCatalogModel = ImageCatalogModel | VideoCatalogModel;

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
