import { afterEach, describe, expect, it, vi } from 'vitest';
import { createShare, listShares, revokeShare, updateShareSnapshot } from './shareApi';
import type { ShareLink } from './shareTypes';

// shareApi.test.ts — covers every <behavior> row from 37F-15-PLAN.md Task 1. The backend
// (internal/agui/share_api*.go, plan 37F-10) has not landed yet — this plan is wave 4, 37F-10
// is wave 5. Every fetch call/response shape here is taken from 37F-10-PLAN.md's locked route
// table, the same resolution plan 37F-16 (SharePage.tsx) already documented for the identical
// wave-ordering gap. Follows web/src/chat/attachments/__tests__/api.test.ts's mocking
// convention: vi.stubGlobal('fetch', ...) + vi.unstubAllGlobals() in afterEach.

const internalLink: ShareLink = {
  id: 'share-1',
  tier: 'internal',
  url: '/shared/share-1',
  created_at: '2026-07-17T10:00:00Z',
  updated_at: '2026-07-17T10:00:00Z',
};

const publicLink: ShareLink = {
  id: 'share-2',
  tier: 'public',
  url: '/s/tok-abc123',
  expires_at: '2026-07-24T10:00:00Z',
  created_at: '2026-07-17T10:00:00Z',
  updated_at: '2026-07-17T10:00:00Z',
};

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('share API client', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe('createShare', () => {
    it('posts to /api/shares and returns the created internal ShareLink (no token, derivable URL)', async () => {
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(internalLink, 201)));
      vi.stubGlobal('fetch', fetchMock);

      await expect(createShare('conv-1', 'internal')).resolves.toEqual(internalLink);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares',
        expect.objectContaining({
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
          body: JSON.stringify({ conversation_id: 'conv-1', tier: 'internal' }),
        }),
      );
    });

    it('posts a public tier request and returns the one-time plaintext /s/{token} URL', async () => {
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(publicLink, 201)));
      vi.stubGlobal('fetch', fetchMock);

      const result = await createShare('conv-1', 'public', '7d');

      expect(result.url).toBe('/s/tok-abc123');
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares',
        expect.objectContaining({
          body: JSON.stringify({ conversation_id: 'conv-1', tier: 'public', expiry_option: '7d' }),
        }),
      );
    });

    it('maps a numeric expiryOption to expiry_option="custom" + custom_expiry_days', async () => {
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(publicLink, 201)));
      vi.stubGlobal('fetch', fetchMock);

      await createShare('conv-1', 'public', 45);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares',
        expect.objectContaining({
          body: JSON.stringify({
            conversation_id: 'conv-1',
            tier: 'public',
            expiry_option: 'custom',
            custom_expiry_days: 45,
          }),
        }),
      );
    });

    it('threads an AbortSignal through to fetch', async () => {
      const controller = new AbortController();
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(internalLink, 201)));
      vi.stubGlobal('fetch', fetchMock);

      await createShare('conv-1', 'internal', undefined, controller.signal);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares',
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe('listShares', () => {
    it("lists the owner's links with no filter", async () => {
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse([internalLink, publicLink])));
      vi.stubGlobal('fetch', fetchMock);

      await expect(listShares()).resolves.toEqual([internalLink, publicLink]);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares',
        expect.objectContaining({
          method: 'GET',
          credentials: 'same-origin',
          headers: { Accept: 'application/json' },
        }),
      );
    });

    it('filters to one thread via an encoded conversation_id query param', async () => {
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse([internalLink])));
      vi.stubGlobal('fetch', fetchMock);

      await expect(listShares('conv/1')).resolves.toEqual([internalLink]);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares?conversation_id=conv%2F1',
        expect.objectContaining({ method: 'GET' }),
      );
    });

    it('defends against a non-array response by returning an empty list', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(jsonResponse({ not: 'an array' }))),
      );

      await expect(listShares()).resolves.toEqual([]);
    });

    it('a public row from listShares carries NO url (D-13: token returned once, at create only)', async () => {
      // exactOptionalPropertyTypes forbids `url: undefined` on ShareLink — the wire contract
      // is "key absent", not "key present with an undefined value" — so the key is built
      // absent from the start rather than spread-and-overridden.
      const listedPublicRow: ShareLink = {
        id: publicLink.id,
        tier: publicLink.tier,
        expires_at: '2026-07-24T10:00:00Z',
        created_at: publicLink.created_at,
        updated_at: publicLink.updated_at,
      };
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(jsonResponse([listedPublicRow]))),
      );

      const [row] = await listShares();
      expect(row?.url).toBeUndefined();
    });

    it('threads an AbortSignal through to fetch', async () => {
      const controller = new AbortController();
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse([internalLink])));
      vi.stubGlobal('fetch', fetchMock);

      await listShares(undefined, controller.signal);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares',
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe('updateShareSnapshot', () => {
    it('PATCHes /api/shares/{id}/snapshot and returns the refreshed link', async () => {
      const refreshed = { ...internalLink, updated_at: '2026-07-18T10:00:00Z' };
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(refreshed)));
      vi.stubGlobal('fetch', fetchMock);

      await expect(updateShareSnapshot('share-1')).resolves.toEqual(refreshed);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares/share-1/snapshot',
        expect.objectContaining({ method: 'PATCH', credentials: 'same-origin' }),
      );
    });

    it('threads an AbortSignal through to fetch', async () => {
      const controller = new AbortController();
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(internalLink)));
      vi.stubGlobal('fetch', fetchMock);

      await updateShareSnapshot('share-1', controller.signal);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares/share-1/snapshot',
        expect.objectContaining({ signal: controller.signal }),
      );
    });

    it('encodes the id', async () => {
      const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(internalLink)));
      vi.stubGlobal('fetch', fetchMock);

      await updateShareSnapshot('share/1');

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares/share%2F1/snapshot',
        expect.objectContaining({ method: 'PATCH' }),
      );
    });
  });

  describe('revokeShare', () => {
    it('DELETEs /api/shares/{id} and resolves void', async () => {
      const fetchMock = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
      vi.stubGlobal('fetch', fetchMock);

      await expect(revokeShare('share-1')).resolves.toBeUndefined();

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares/share-1',
        expect.objectContaining({ method: 'DELETE', credentials: 'same-origin' }),
      );
    });

    it('threads an AbortSignal through to fetch', async () => {
      const controller = new AbortController();
      const fetchMock = vi.fn(() => Promise.resolve(new Response(null, { status: 204 })));
      vi.stubGlobal('fetch', fetchMock);

      await revokeShare('share-1', controller.signal);

      expect(fetchMock).toHaveBeenCalledWith(
        '/api/shares/share-1',
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe('error shaping', () => {
    it('a non-2xx response throws an Error carrying the server body', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(new Response('share not found', { status: 404 }))),
      );

      await expect(updateShareSnapshot('missing')).rejects.toThrow('share not found');
    });

    it('falls back to the HTTP status when the non-2xx body is empty', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(new Response('', { status: 403 }))),
      );

      await expect(createShare('conv-1', 'public')).rejects.toThrow('HTTP 403');
    });

    it('falls back to the HTTP status when the non-2xx response has no body at all', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(new Response(null, { status: 500 }))),
      );

      await expect(createShare('conv-1', 'internal')).rejects.toThrow('HTTP 500');
    });

    it('revokeShare also throws the server message on a non-2xx', async () => {
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(new Response('forbidden', { status: 403 }))),
      );

      await expect(revokeShare('share-1')).rejects.toThrow('forbidden');
    });
  });
});
