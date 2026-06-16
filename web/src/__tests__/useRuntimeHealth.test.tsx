import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { HEALTH_REFETCH_INTERVAL_MS, useRuntimeHealth } from '../health/useRuntimeHealth';

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

describe('useRuntimeHealth', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('polls /healthz + /readyz same-origin as JSON every 5s and surfaces both bodies', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const body = urlOf(input).includes('/readyz')
        ? { ready: true, deps: { postgres: 'ok', neo4j: 'ok' } }
        : { ok: true, bind_address: '127.0.0.1:9080', build_version: 'dev' };
      return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useRuntimeHealth(), { wrapper: wrapper() });

    await waitFor(() => {
      expect(result.current.healthz).toBeDefined();
      expect(result.current.readyz).toBeDefined();
    });

    // The poll interval constant is exactly 5s (mutating the literal must fail).
    expect(HEALTH_REFETCH_INTERVAL_MS).toBe(5000);

    // Exact request shape: path, JSON Accept header, same-origin credentials.
    expect(fetchMock).toHaveBeenCalledWith('/healthz', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
    expect(fetchMock).toHaveBeenCalledWith('/readyz', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });

    // Bodies + status are surfaced verbatim.
    expect(result.current.healthz?.status).toBe(200);
    expect(result.current.healthz?.body.ok).toBe(true);
    expect(result.current.healthz?.body.bind_address).toBe('127.0.0.1:9080');
    expect(result.current.readyz?.body.ready).toBe(true);
    expect(result.current.readyz?.body.deps.postgres).toBe('ok');

    // No errors; not pending after both resolve; lastChecked is a real epoch (> 0).
    expect(result.current.healthzError).toBe(false);
    expect(result.current.readyzError).toBe(false);
    expect(result.current.isPending).toBe(false);
    expect(result.current.lastChecked).toBeGreaterThan(0);
  });

  it('flags healthzError when /healthz rejects (retry disabled)', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      if (urlOf(input).includes('/readyz')) {
        return Promise.resolve(
          new Response(JSON.stringify({ ready: true, deps: {} }), { status: 200 }),
        );
      }
      return Promise.reject(new Error('healthz down'));
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useRuntimeHealth(), { wrapper: wrapper() });

    await waitFor(() => {
      expect(result.current.healthzError).toBe(true);
    });
    expect(result.current.healthz).toBeUndefined();
    // readyz still resolved → readyzError stays false (independent queries).
    expect(result.current.readyzError).toBe(false);
  });
});
