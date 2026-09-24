import { getJSON, postJSON } from '../api/json';

// The host updater downloads builds freely but applies one only when an admin asks, when Aura
// has been idle long enough, or at the latest by `deadline`. The daemon reports that queue here;
// `managed: false` is a dev stack with no updater, where the cockpit shows nothing at all.

export type UpdateState = 'current' | 'pending' | 'requested' | 'applying' | 'failed' | 'unknown';

export interface SystemUpdateStatus {
  readonly managed: boolean;
  readonly state: UpdateState;
  readonly running_rev: string;
  readonly available_rev: string;
  readonly available_built_at: string | null;
  readonly pending_since: string | null;
  readonly deadline: string | null;
  readonly deferred_until: string | null;
  readonly checked_at: string | null;
  readonly can_decide: boolean;
  /** The admin-only fields: the daemon omits them for a member, read here as "nothing". */
  readonly error: string;
  readonly last_activity_at: string | null;
  readonly live_runs: number;
}

export interface ApplyResponse {
  readonly state: UpdateState;
}

export interface DeferResponse {
  readonly deferred_until: string;
}

export const UPDATE_QUERY_KEY = ['system', 'update'] as const;
export const UPDATE_POLL_MS = 60_000;
export const UPDATE_FAST_POLL_MS = 3_000;

const ROUTE = '/api/system/update';

/** requested and applying are the states in which the daemon is about to go away. */
export function isUpdating(state: UpdateState | undefined): boolean {
  return state === 'requested' || state === 'applying';
}

export function updatePollInterval(status: SystemUpdateStatus | undefined): number | false {
  if (status?.managed === false) return false;
  return isUpdating(status?.state) ? UPDATE_FAST_POLL_MS : UPDATE_POLL_MS;
}

// The daemon leaves empty fields out of its JSON; every absent one becomes its empty value
// here, so a consumer never has to tell "missing" from "unknown".
export async function fetchSystemUpdate(signal?: AbortSignal): Promise<SystemUpdateStatus> {
  const raw = await getJSON<Partial<SystemUpdateStatus> | null>(ROUTE, signal);
  return {
    managed: raw?.managed === true,
    state: raw?.state ?? 'unknown',
    running_rev: raw?.running_rev ?? '',
    available_rev: raw?.available_rev ?? '',
    available_built_at: raw?.available_built_at ?? null,
    pending_since: raw?.pending_since ?? null,
    deadline: raw?.deadline ?? null,
    deferred_until: raw?.deferred_until ?? null,
    checked_at: raw?.checked_at ?? null,
    can_decide: raw?.can_decide === true,
    error: raw?.error ?? '',
    last_activity_at: raw?.last_activity_at ?? null,
    live_runs: raw?.live_runs ?? 0,
  };
}

export function applyUpdate(): Promise<ApplyResponse> {
  // The route takes no body: JSON.stringify(undefined) sends none.
  return postJSON<ApplyResponse>(`${ROUTE}/apply`, undefined);
}

export function deferUpdate(until: string): Promise<DeferResponse> {
  return postJSON<DeferResponse>(`${ROUTE}/defer`, { until });
}
