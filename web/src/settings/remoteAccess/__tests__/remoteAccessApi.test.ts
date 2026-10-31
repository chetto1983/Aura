import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  RemoteAccessError,
  configureRemoteAccess,
  deleteRemoteAccess,
  dismissAbandonedRemoteAccess,
  verifyRemoteAccessToken,
} from '../remoteAccessApi';
import { remoteAccessPollInterval } from '../useRemoteAccess';

function answer(body: string, status: number) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response(body, { status }))),
  );
}

const refusal = (run: () => Promise<unknown>) =>
  run().then(
    () => {
      throw new Error('the request should have been refused');
    },
    (err: unknown) => err as RemoteAccessError,
  );

const save = () =>
  configureRemoteAccess({
    enabled: true,
    generation: 1,
    account_id: 'account',
    zone_name: 'example.com',
    public_label: 'aura',
  });

describe('remote access API', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('reads the reason code the API error writer sends', async () => {
    answer(JSON.stringify({ error: 'invalid', reason: 'domain_invalid' }), 400);
    const err = await refusal(save);
    expect(err).toBeInstanceOf(RemoteAccessError);
    expect(err.status).toBe(400);
    expect(err.code).toBe('domain_invalid');
    expect(err.reason).toBe('invalid');
  });

  it('a stale generation carries no reason', async () => {
    answer(JSON.stringify({ error: 'configuration changed; refresh and retry', reason: '' }), 409);
    const err = await refusal(save);
    expect(err.status).toBe(409);
    expect(err.code).toBe('');
  });

  it('a body written outside the error writer has no reason, not a failure code', async () => {
    answer(JSON.stringify({ error: 'governance.write required' }), 403);
    expect((await refusal(save)).code).toBe('');
  });

  it('a body that is not JSON has no reason', async () => {
    answer('<html>bad gateway</html>', 502);
    const err = await refusal(save);
    expect(err.status).toBe(502);
    expect(err.code).toBe('');
  });

  it('a code this cockpit does not know is no reason', async () => {
    answer(JSON.stringify({ error: 'x', reason: 'from_a_newer_server' }), 400);
    expect((await refusal(save)).code).toBe('');
  });

  it('a verify refusal carries its reason too', async () => {
    answer(JSON.stringify({ error: 'invalid', reason: 'no_accounts' }), 400);
    expect((await refusal(() => verifyRemoteAccessToken('t'))).code).toBe('no_accounts');
  });

  it('dismisses the abandoned list at its own route', async () => {
    const seen = vi.fn((_url: string, _init: RequestInit) =>
      Promise.resolve(new Response('{"status":"accepted"}', { status: 202 })),
    );
    vi.stubGlobal('fetch', seen);
    await dismissAbandonedRemoteAccess();
    expect(seen).toHaveBeenCalledOnce();
    const [url, init] = seen.mock.calls[0] ?? [];
    expect(url).toBe('/api/settings/remote-access/abandoned/dismiss');
    expect(init?.method).toBe('POST');
  });

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
    for (const phase of ['validating', 'provisioning', 'connecting', 'deleting'] as const) {
      expect(remoteAccessPollInterval(phase)).toBe(2_000);
    }
    expect(remoteAccessPollInterval('healthy')).toBe(30_000);
    expect(remoteAccessPollInterval('degraded')).toBe(30_000);
    expect(remoteAccessPollInterval('disabled')).toBe(false);
    expect(remoteAccessPollInterval('error')).toBe(false);
  });

  it('waiting for nameservers can last days and calls Cloudflare, so it polls gently', () => {
    expect(remoteAccessPollInterval('waiting_nameservers')).toBe(30_000);
  });
});
