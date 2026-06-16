import { useQuery } from '@tanstack/react-query';

// The two existing same-origin REST endpoints (D-07: no new backend endpoint).
//   GET /healthz → 200 {"ok":true,"scheduler_last_tick":"...","bind_address":"...","build_version":"..."}
//                  503 {"ok":false,"error":"..."}
//   GET /readyz  → 200 {"ready":true,"deps":{"postgres":"ok","neo4j":"ok"}}
//                  503 {"ready":false,"deps":{"postgres":"<err>","neo4j":"ok"}}
// We poll both with React Query (REST, not SSE) and surface dataUpdatedAt for the
// "Last checked" caption.

export interface HealthzBody {
  ok: boolean;
  scheduler_last_tick?: string;
  bind_address?: string;
  build_version?: string;
  error?: string;
}

export interface ReadyzBody {
  ready: boolean;
  deps: Record<string, string>;
}

export const HEALTH_REFETCH_INTERVAL_MS = 5000;

async function fetchHealthz(): Promise<{ status: number; body: HealthzBody }> {
  // Same-origin: the SPA is served by the very binary that exposes these routes.
  // /healthz is public; the cookie (if any) rides along same-origin.
  const res = await fetch('/healthz', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  const body = (await res.json()) as HealthzBody;
  return { status: res.status, body };
}

async function fetchReadyz(): Promise<{ status: number; body: ReadyzBody }> {
  const res = await fetch('/readyz', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  const body = (await res.json()) as ReadyzBody;
  return { status: res.status, body };
}

export interface UseRuntimeHealthResult {
  healthz: { status: number; body: HealthzBody } | undefined;
  readyz: { status: number; body: ReadyzBody } | undefined;
  healthzError: boolean;
  readyzError: boolean;
  isPending: boolean;
  /** epoch ms of the most recent successful update across both probes (0 if none). */
  lastChecked: number;
}

export function useRuntimeHealth(): UseRuntimeHealthResult {
  const healthz = useQuery({
    queryKey: ['healthz'],
    queryFn: fetchHealthz,
    refetchInterval: HEALTH_REFETCH_INTERVAL_MS,
    retry: false,
  });
  const readyz = useQuery({
    queryKey: ['readyz'],
    queryFn: fetchReadyz,
    refetchInterval: HEALTH_REFETCH_INTERVAL_MS,
    retry: false,
  });

  return {
    healthz: healthz.data,
    readyz: readyz.data,
    healthzError: healthz.isError,
    readyzError: readyz.isError,
    isPending: healthz.isPending || readyz.isPending,
    lastChecked: Math.max(healthz.dataUpdatedAt, readyz.dataUpdatedAt),
  };
}
