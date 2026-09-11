import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  IDENTITY_CREATE,
  IDENTITY_DELETE,
  fetchAdminIdentities,
  fetchAudit,
  fetchIdentityCredit,
  fetchMe,
  fetchSpendOverview,
  hasCapability,
  removeIdentity,
  setIdentityCredit,
} from '../adminApi';

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

describe('adminApi hasCapability', () => {
  it('is a plain exact match — no wildcard branch', () => {
    expect(hasCapability(['governance.write'], 'governance.write')).toBe(true);
    expect(hasCapability(['agent.run'], 'governance.write')).toBe(false);
    expect(hasCapability([], 'governance.write')).toBe(false);
    // A '*' entry (a value the server no longer emits, per D-01) grants nothing extra — the
    // wildcard branch is gone, not merely unreachable.
    expect(hasCapability(['*'], 'governance.write')).toBe(false);
  });
  it('exposes the administrative capability names matching the Go constants', () => {
    expect(IDENTITY_CREATE).toBe('identity.create');
    expect(IDENTITY_DELETE).toBe('identity.delete');
  });
});

describe('adminApi fetchers', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('fetchMe reads /api/me', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify({ identity_id: 'id-1', capabilities: ['*'] }), {
            status: 200,
          }),
        ),
      ),
    );
    await expect(fetchMe()).resolves.toEqual({ identity_id: 'id-1', capabilities: ['*'] });
  });

  it('fetchAdminIdentities unwraps the identities array (and tolerates an absent field)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              identities: [{ id: 'a', name: 'A', kind: 'user', capabilities: [] }],
            }),
            {
              status: 200,
            },
          ),
        ),
      ),
    );
    await expect(fetchAdminIdentities()).resolves.toHaveLength(1);

    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('{}', { status: 200 }))),
    );
    await expect(fetchAdminIdentities()).resolves.toEqual([]);
  });

  it('fetchAudit builds the identity/limit/offset query', async () => {
    let capturedUrl = '';
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        capturedUrl = urlOf(input);
        return Promise.resolve(
          new Response(JSON.stringify({ events: [], limit: 25, offset: 50 }), { status: 200 }),
        );
      }),
    );
    await fetchAudit('id-9', 25, 50);
    expect(capturedUrl).toContain('/api/admin/audit?');
    expect(capturedUrl).toContain('identity=id-9');
    expect(capturedUrl).toContain('limit=25');
    expect(capturedUrl).toContain('offset=50');
  });

  it('fetchIdentityCredit reads the per-identity credit route', async () => {
    let capturedUrl = '';
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        capturedUrl = urlOf(input);
        return Promise.resolve(
          new Response(
            JSON.stringify({
              identity_id: 'id-1',
              exempt: false,
              cap: 5,
              reset_interval: 'monthly',
              spend: 0.000004158,
              remaining: 4.999995842,
              percent_used: 0,
            }),
            { status: 200 },
          ),
        );
      }),
    );
    await expect(fetchIdentityCredit('id-1')).resolves.toMatchObject({
      identity_id: 'id-1',
      exempt: false,
      spend: 0.000004158,
    });
    expect(capturedUrl).toBe('/api/admin/identities/id-1/credit');
  });

  // A missing key is a state of the identity, not a failed read: the 409 carries why.
  it('fetchIdentityCredit turns a 409 into the no-key state with its cause', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              error: 'identity has no OpenRouter key yet',
              cause: 'management_key_unset',
            }),
            { status: 409 },
          ),
        ),
      ),
    );
    await expect(fetchIdentityCredit('id-1')).resolves.toEqual({
      identity_id: 'id-1',
      no_key: true,
      cause: 'management_key_unset',
    });
  });

  it('fetchIdentityCredit still rejects any other failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(new Response('{"error":"credit store unavailable"}', { status: 502 })),
      ),
    );
    await expect(fetchIdentityCredit('id-1')).rejects.toMatchObject({ status: 502 });
  });

  it('setIdentityCredit POSTs the cap/reset-interval patch', async () => {
    let seen: { url: string; method: string; body: string } | undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        seen = {
          url: urlOf(input),
          method: init?.method ?? 'GET',
          body: typeof init?.body === 'string' ? init.body : '',
        };
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
    const result = await setIdentityCredit('id-1', { cap: '5.126' });
    expect(seen?.url).toBe('/api/admin/identities/id-1/credit');
    expect(seen?.method).toBe('POST');
    expect(JSON.parse(seen?.body ?? '{}')).toEqual({ cap: '5.126' });
    expect(result.cap).toBe(5.13);
  });

  it('removeIdentity DELETEs the identity and surfaces the server reason on failure', async () => {
    let seen: { url: string; method: string } | undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        seen = { url: urlOf(input), method: init?.method ?? 'GET' };
        return Promise.resolve(
          new Response(JSON.stringify({ identity_id: 'id-1', status: 'removed' }), {
            status: 200,
          }),
        );
      }),
    );
    await expect(removeIdentity('id-1')).resolves.toEqual({
      identity_id: 'id-1',
      status: 'removed',
    });
    expect(seen?.url).toBe('/api/admin/identities/id-1');
    expect(seen?.method).toBe('DELETE');

    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              error: 'the last administrative identity cannot remove or deactivate itself',
            }),
            { status: 403 },
          ),
        ),
      ),
    );
    await expect(removeIdentity('id-1')).rejects.toThrow(
      'the last administrative identity cannot remove or deactivate itself',
    );
  });

  it('fetchSpendOverview reads the account-wide reconciliation route', async () => {
    let capturedUrl = '';
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        capturedUrl = urlOf(input);
        return Promise.resolve(
          new Response(
            JSON.stringify({
              kpis: [{ metric: 'total_usage', value: 5.52, delta_percent: 12.5, series: [1, 2] }],
              top_identities: [
                {
                  identity_id: 'id-1',
                  name: 'alice@example.com',
                  masked_label: 'sk-or-v1-caa...61c',
                  lifetime_spend: 5.52,
                },
              ],
              over_allocation: { triggered: false, sum_caps: 10, available: 20 },
            }),
            { status: 200 },
          ),
        );
      }),
    );
    const result = await fetchSpendOverview();
    expect(capturedUrl).toBe('/api/admin/spend/overview');
    expect(result.kpis).toHaveLength(1);
    expect(result.top_identities[0]?.name).toBe('alice@example.com');
    expect(result.over_allocation.triggered).toBe(false);
  });
});
