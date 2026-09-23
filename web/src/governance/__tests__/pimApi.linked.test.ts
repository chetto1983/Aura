import { afterEach, describe, expect, it, vi } from 'vitest';
import { pimAccountLinked } from '../pimApi';

// pimAccountLinked reads the sidecar's `linked` from GET …/{id}/status: a boolean only for Google
// accounts, absent for providers with nothing to link. Anything but a boolean must read as null so
// the Google panel never mistakes a missing field for a completed link.

function statusResponse(body: unknown) {
  return vi.fn((_url: string) =>
    Promise.resolve(new Response(JSON.stringify(body), { status: 200 })),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('pimAccountLinked', () => {
  it('reads true and hits the encoded account status URL', async () => {
    const fetchMock = statusResponse({
      accountId: 'tenant__work',
      provider: 'google',
      linked: true,
    });
    vi.stubGlobal('fetch', fetchMock);

    await expect(pimAccountLinked('tenant__work')).resolves.toBe(true);
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/connect/pim/accounts/tenant__work/status');
  });

  it('reads false while the Google token is not stored yet', async () => {
    vi.stubGlobal('fetch', statusResponse({ provider: 'google', linked: false }));

    await expect(pimAccountLinked('work')).resolves.toBe(false);
  });

  it('reads null when the provider reports no link state', async () => {
    vi.stubGlobal(
      'fetch',
      statusResponse({ provider: 'microsoft365', linked: null, authFlow: null }),
    );

    await expect(pimAccountLinked('work')).resolves.toBeNull();
  });

  it('does not treat a truthy non-boolean as linked', async () => {
    vi.stubGlobal('fetch', statusResponse({ provider: 'google', linked: 'true' }));

    await expect(pimAccountLinked('work')).resolves.toBeNull();
  });

  it('throws on a non-200 so the panel keeps polling instead of claiming success', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('{}', { status: 503 }))),
    );

    await expect(pimAccountLinked('work')).rejects.toThrow();
  });
});
