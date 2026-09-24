import type { TFunction } from 'i18next';
import { readJSON } from './settingsApi';

// The embedding route changes only through a measured preview and a confirmed apply (spec
// §4): the generic PUT /api/settings/{key} answers 409 for these three keys.

export interface EmbeddingRoute {
  readonly AURA_EMBED_BASE_URL: string;
  readonly AURA_EMBED_MODEL: string;
  readonly AURA_EMBED_CLOUD_BASE_URL: string;
}

export interface TypeTally {
  readonly type: string;
  readonly in_space: number;
  readonly other_space: number;
  readonly no_vector: number;
  /** No vector, stamped with this space: the model refused the text. */
  readonly rejected: number;
}

export interface FamilyState {
  readonly family: string;
  readonly space: string;
  /** Dense retrieval serves the family only while no vector is in another space. */
  readonly open: boolean;
  readonly types: readonly TypeTally[];
}

export interface StuckDocument {
  readonly file_name: string;
  readonly source_key: string;
  readonly space?: string;
}

export interface TenantSpaceReport {
  readonly identity_id: string;
  readonly families: readonly FamilyState[];
  readonly stuck_documents: readonly StuckDocument[];
  readonly ingest_status?: string;
  readonly ingest_errors: number;
}

export interface EmbeddingSpaceState {
  readonly space: string;
  readonly space_label: string;
  readonly documents_space: string;
  readonly documents_space_label: string;
  /** Why the daemon cannot name its space; the tenants are then not counted. */
  readonly space_error?: string;
  readonly floors_calibrated: boolean;
  readonly tenants?: readonly TenantSpaceReport[];
}

export interface RouteRefusal {
  readonly code: string;
  readonly detail?: string;
}

/** A refusal in the cockpit's words; a code the cockpit predates shows the daemon's detail. */
export function refusalText(t: TFunction, refusal: RouteRefusal): string {
  return t(`embeddingRoute.refusals.${refusal.code}`, {
    detail: refusal.detail ?? '',
    defaultValue: refusal.detail ?? refusal.code,
  });
}

export interface EmbeddingRoutePreview {
  readonly space: string;
  readonly space_label: string;
  readonly memory_space: string;
  readonly native_width: number;
  readonly dimensions: number;
  readonly width_warning: boolean;
  readonly chars_per_second: number;
  readonly input_limit: number;
  readonly work: {
    readonly types?: readonly {
      readonly type: string;
      readonly rows: number;
      readonly chars: number;
    }[];
    readonly passages_over_limit: number;
  };
  readonly tokens: number;
  /** null: the catalogue publishes no price for this model. */
  readonly cost_usd: number | null;
  readonly local: boolean;
  readonly duration_seconds: number;
  readonly floors_calibrated: boolean;
  readonly refusals: readonly RouteRefusal[];
}

export interface EmbeddingRouteApplied {
  readonly space: string;
  readonly restarting: boolean;
  readonly restart_required: boolean;
}

export const EMBEDDING_SPACE_QUERY_KEY = ['settings', 'embedding-space'] as const;

export async function fetchEmbeddingSpace(): Promise<EmbeddingSpaceState> {
  const res = await fetch('/api/settings/embedding-space', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<EmbeddingSpaceState>(res);
}

function postRoute(path: string, body: unknown): Promise<Response> {
  return fetch(path, {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
}

export async function previewEmbeddingRoute(route: EmbeddingRoute): Promise<EmbeddingRoutePreview> {
  return readJSON<EmbeddingRoutePreview>(
    await postRoute('/api/settings/embedding-route/preview', route),
  );
}

/** The daemon refused the apply: the route's space moved since the preview (space_changed), or
 * its own re-probe refused the route (route_refused). The preview on screen no longer holds. */
export class EmbeddingRouteRejected extends Error {
  readonly refusals: readonly RouteRefusal[];

  constructor(reason: 'space_changed' | 'route_refused', refusals: readonly RouteRefusal[]) {
    super(reason);
    this.name = 'EmbeddingRouteRejected';
    this.refusals = refusals;
  }
}

/** confirmSpace is the preview's target: the daemon recomputes it and refuses a mismatch. */
export async function applyEmbeddingRoute(
  route: EmbeddingRoute,
  confirmSpace: string,
): Promise<EmbeddingRouteApplied> {
  const res = await postRoute('/api/settings/embedding-route', {
    ...route,
    confirm_space: confirmSpace,
  });
  if (res.status === 409 || res.status === 422) {
    const body = (await res
      .clone()
      .json()
      .catch(() => undefined)) as
      { readonly error?: unknown; readonly refusals?: readonly RouteRefusal[] } | undefined;
    if (body?.error === 'space_changed' || body?.error === 'route_refused') {
      throw new EmbeddingRouteRejected(body.error, body.refusals ?? []);
    }
  }
  return readJSON<EmbeddingRouteApplied>(res);
}
