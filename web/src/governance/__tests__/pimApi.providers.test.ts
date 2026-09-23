import { afterEach, describe, expect, it, vi } from 'vitest';
import { listPimProviderApps, savePimProviderApp } from '../pimApi';

function respond(body: unknown, status = 200) {
  return vi.fn((_url: string, _init?: RequestInit) =>
    Promise.resolve(new Response(JSON.stringify(body), { status })),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('listPimProviderApps', () => {
  it('reads the member shape with empty admin fields', async () => {
    vi.stubGlobal(
      'fetch',
      respond({
        providers: [
          { provider: 'google', configured: true },
          { provider: 'outlook.com', configured: false },
        ],
      }),
    );
    await expect(listPimProviderApps()).resolves.toEqual([
      {
        provider: 'google',
        configured: true,
        clientId: '',
        tenantId: '',
        secretSet: false,
        redirectUri: '',
      },
      {
        provider: 'outlook.com',
        configured: false,
        clientId: '',
        tenantId: '',
        secretSet: false,
        redirectUri: '',
      },
    ]);
  });

  it('drops rows for providers it does not manage', async () => {
    vi.stubGlobal('fetch', respond({ providers: [{ provider: 'imap', configured: true }, null] }));
    await expect(listPimProviderApps()).resolves.toEqual([]);
  });
});

describe('savePimProviderApp', () => {
  it('PUTs the input to the encoded provider path', async () => {
    const fetchMock = respond({
      provider: 'outlook.com',
      configured: true,
      clientId: 'm',
      tenantId: 'consumers',
      secretSet: false,
    });
    vi.stubGlobal('fetch', fetchMock);
    const saved = await savePimProviderApp('outlook.com', { clientId: 'm', tenantId: 'consumers' });
    expect(saved.tenantId).toBe('consumers');
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe('/api/connect/pim/providers/outlook.com');
    expect(init?.method).toBe('PUT');
    expect(typeof init?.body).toBe('string');
    expect(JSON.parse(init?.body as string)).toEqual({ clientId: 'm', tenantId: 'consumers' });
  });

  it('surfaces the server reason on a 400', async () => {
    vi.stubGlobal(
      'fetch',
      respond({ error: 'clientSecret is required for a new Google client' }, 400),
    );
    await expect(savePimProviderApp('google', { clientId: 'x' })).rejects.toThrow(
      /clientSecret is required/,
    );
  });

  it('rejects a response that is not a managed provider app', async () => {
    vi.stubGlobal('fetch', respond({ provider: 'imap' }));
    await expect(savePimProviderApp('google', { clientId: 'x' })).rejects.toThrow(/malformed/);
  });
});
