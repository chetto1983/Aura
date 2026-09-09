import { afterEach, describe, expect, it, vi } from 'vitest';
import { createElement, type ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  useAdminIdentities,
  useAudit,
  useCapabilities,
  useGrantCapability,
  useIdentityCredit,
  useRemoveIdentity,
  useRevokeCapability,
  useSetIdentityCredit,
} from '../useAdmin';

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return createElement(QueryClientProvider, { client }, children);
  };
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('useCapabilities', () => {
  it('derives isAdmin from identity.create, not governance.write', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({ identity_id: 'local', capabilities: ['identity.create', 'identity.delete'] }),
            { status: 200 },
          ),
        ),
      ),
    );
    const { result } = renderHook(() => useCapabilities(), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.isAdmin).toBe(true);
    });
    expect(result.current.identityId).toBe('local');
  });

  it('is NOT admin for the four-capability user set, even though it includes governance.write', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              identity_id: 'u2',
              capabilities: ['agent.run', 'governance.read', 'governance.write', 'share.public'],
            }),
            { status: 200 },
          ),
        ),
      ),
    );
    const { result } = renderHook(() => useCapabilities(), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });
    // This is the regression Phase 2's own narrowing (D-01) would introduce if isAdmin still
    // read governance.write: every identity holds it now, so the old derivation would report
    // true here.
    expect(result.current.isAdmin).toBe(false);
  });

  it('fails closed (not admin) when /api/me errors', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('no', { status: 500 }))),
    );
    const { result } = renderHook(() => useCapabilities(), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.isError).toBe(true);
    });
    expect(result.current.isAdmin).toBe(false);
  });

  // The cockpit footer gauge bug: /api/me now carries the live model context_window
  // (llm.Config.ContextWindow, e.g. a 128K local model) and useCapabilities must expose it
  // as contextWindow so AppShell can thread it into RuntimeFooter's windowTokens prop.
  it('exposes contextWindow from /api/me context_window', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({ identity_id: 'local', capabilities: ['identity.create'], context_window: 131_072 }),
            { status: 200 },
          ),
        ),
      ),
    );
    const { result } = renderHook(() => useCapabilities(), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.contextWindow).toBe(131_072);
    });
  });

  it('leaves contextWindow undefined when /api/me omits context_window (unwired daemon)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ identity_id: 'local', capabilities: ['identity.create'] }), {
            status: 200,
          }),
        ),
      ),
    );
    const { result } = renderHook(() => useCapabilities(), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });
    expect(result.current.contextWindow).toBeUndefined();
  });
});

describe('admin roster + mutations', () => {
  it('useAdminIdentities reads the roster', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              identities: [{ id: 'a', name: 'A', kind: 'user', capabilities: ['agent.run'] }],
            }),
            { status: 200 },
          ),
        ),
      ),
    );
    const { result } = renderHook(() => useAdminIdentities(), { wrapper: wrapper() });
    await waitFor(() => {
      expect(result.current.data).toHaveLength(1);
    });
  });

  it('useGrantCapability POSTs and useRevokeCapability DELETEs', async () => {
    const methods: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        methods.push(init?.method ?? 'GET');
        return Promise.resolve(
          new Response(JSON.stringify({ identity_id: 'a', capabilities: [] }), { status: 200 }),
        );
      }),
    );
    const w = wrapper();
    const grant = renderHook(() => useGrantCapability(), { wrapper: w });
    grant.result.current.mutate({ identityId: 'a', capability: 'x.y' });
    await waitFor(() => {
      expect(grant.result.current.isSuccess).toBe(true);
    });
    expect(methods).toContain('POST');

    const revoke = renderHook(() => useRevokeCapability(), { wrapper: w });
    revoke.result.current.mutate({ identityId: 'a', capability: 'x.y' });
    await waitFor(() => {
      expect(revoke.result.current.isSuccess).toBe(true);
    });
    expect(methods).toContain('DELETE');
  });
});

describe('useAudit', () => {
  it('is disabled (no fetch) until an identity is selected', async () => {
    const urls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        urls.push(urlOf(input));
        return Promise.resolve(
          new Response(JSON.stringify({ events: [], limit: 25, offset: 0 }), { status: 200 }),
        );
      }),
    );
    const { result, rerender } = renderHook(({ id }: { id: string }) => useAudit(id, 25, 0), {
      wrapper: wrapper(),
      initialProps: { id: '' },
    });
    expect(result.current.fetchStatus).toBe('idle');
    expect(urls).toHaveLength(0);

    rerender({ id: 'id-1' });
    await waitFor(() => {
      expect(result.current.data?.events).toBeDefined();
    });
    expect(urls.some((url) => url.includes('identity=id-1'))).toBe(true);
  });
});

describe('credit + removal hooks', () => {
  it('useIdentityCredit is disabled until an identity id is known, then reads the credit route', async () => {
    const urls: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        urls.push(urlOf(input));
        return Promise.resolve(
          new Response(
            JSON.stringify({
              identity_id: 'id-1',
              exempt: false,
              cap: 5,
              reset_interval: 'monthly',
              spend: 0,
              remaining: 5,
              percent_used: 0,
            }),
            { status: 200 },
          ),
        );
      }),
    );
    const { result, rerender } = renderHook(({ id }: { id: string }) => useIdentityCredit(id), {
      wrapper: wrapper(),
      initialProps: { id: '' },
    });
    expect(result.current.fetchStatus).toBe('idle');
    expect(urls).toHaveLength(0);

    rerender({ id: 'id-1' });
    await waitFor(() => {
      expect(result.current.data).toBeDefined();
    });
    expect(urls).toEqual(['/api/admin/identities/id-1/credit']);
  });

  it('useSetIdentityCredit POSTs the patch and invalidates the credit + roster queries', async () => {
    const methods: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        methods.push(init?.method ?? 'GET');
        return Promise.resolve(
          new Response(
            JSON.stringify({
              identity_id: 'id-1',
              cap: 5.13,
              reset_interval: 'monthly',
              store_applied: true,
              provider_applied: true,
            }),
            { status: 200 },
          ),
        );
      }),
    );
    const { result } = renderHook(() => useSetIdentityCredit(), { wrapper: wrapper() });
    result.current.mutate({ identityId: 'id-1', patch: { cap: '5.126' } });
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(methods).toContain('POST');
    expect(result.current.data?.cap).toBe(5.13);
  });

  it('useRemoveIdentity DELETEs the identity and invalidates the roster', async () => {
    const methods: string[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        methods.push(init?.method ?? 'GET');
        return Promise.resolve(
          new Response(JSON.stringify({ identity_id: 'id-1', status: 'removed' }), {
            status: 200,
          }),
        );
      }),
    );
    const { result } = renderHook(() => useRemoveIdentity(), { wrapper: wrapper() });
    result.current.mutate('id-1');
    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true);
    });
    expect(methods).toContain('DELETE');
  });
});
