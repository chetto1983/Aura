import { afterEach, describe, expect, it, vi } from 'vitest';
import { deleteRemoteAccess } from '../remoteAccessApi';
import { remoteAccessPollInterval } from '../useRemoteAccess';

describe('remote access API', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('sends the typed public hostname to the controlled delete endpoint', async () => {
    const received: {
      url?: string | undefined;
      method?: string | undefined;
      body?: string | undefined;
    } = {};
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        received.url =
          typeof _input === 'string' ? _input : _input instanceof URL ? _input.href : _input.url;
        received.method = init?.method;
        received.body = init?.body as string;
        return Promise.resolve(
          new Response(JSON.stringify({ status: 'accepted' }), { status: 202 }),
        );
      }),
    );

    await deleteRemoteAccess('aura.example.com');

    expect(received).toEqual({
      url: '/api/settings/remote-access',
      method: 'DELETE',
      body: JSON.stringify({ hostname: 'aura.example.com' }),
    });
  });

  it('polls only transient work quickly and configured health slowly', () => {
    expect(remoteAccessPollInterval('validating')).toBe(2_000);
    expect(remoteAccessPollInterval('waiting_nameservers')).toBe(2_000);
    expect(remoteAccessPollInterval('healthy')).toBe(30_000);
    expect(remoteAccessPollInterval('degraded')).toBe(30_000);
    expect(remoteAccessPollInterval('disabled')).toBe(false);
    expect(remoteAccessPollInterval('error')).toBe(false);
  });
});
