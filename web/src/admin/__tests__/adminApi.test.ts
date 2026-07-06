import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  fetchAdminIdentities,
  fetchAudit,
  fetchMe,
  grantCapability,
  hasCapability,
  revokeCapability,
} from '../adminApi';

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

describe('adminApi hasCapability', () => {
  it('grants everything on the wildcard', () => {
    expect(hasCapability(['*'], 'governance.write')).toBe(true);
    expect(hasCapability(['*'], 'anything.else')).toBe(true);
  });
  it('exact-matches otherwise', () => {
    expect(hasCapability(['governance.write'], 'governance.write')).toBe(true);
    expect(hasCapability(['agent.run'], 'governance.write')).toBe(false);
    expect(hasCapability([], 'governance.write')).toBe(false);
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

  it('grantCapability POSTs the capability body', async () => {
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
          new Response(JSON.stringify({ identity_id: 'id-1', capabilities: ['x.y'] }), {
            status: 200,
          }),
        );
      }),
    );
    await grantCapability('id-1', 'x.y');
    expect(seen?.url).toBe('/api/admin/identities/id-1/capabilities');
    expect(seen?.method).toBe('POST');
    expect(JSON.parse(seen?.body ?? '{}')).toEqual({ capability: 'x.y' });
  });

  it('revokeCapability DELETEs the path-encoded capability and throws on non-2xx', async () => {
    let seen: { url: string; method: string } | undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        seen = { url: urlOf(input), method: init?.method ?? 'GET' };
        return Promise.resolve(
          new Response(JSON.stringify({ identity_id: 'id-1', capabilities: [] }), { status: 200 }),
        );
      }),
    );
    await revokeCapability('id-1', 'governance.write');
    expect(seen?.url).toBe('/api/admin/identities/id-1/capabilities/governance.write');
    expect(seen?.method).toBe('DELETE');

    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('nope', { status: 403 }))),
    );
    await expect(revokeCapability('id-1', 'x')).rejects.toThrow('HTTP 403');
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
});
