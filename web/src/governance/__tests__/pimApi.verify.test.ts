import { afterEach, describe, expect, it, vi } from 'vitest';
import { HttpError, httpErrorFrom } from '../../api/json';
import {
  discoverPim,
  patchPimProviderConfig,
  pimErrorKey,
  pimVerifyFailure,
  verifyPimAccount,
} from '../pimApi';

afterEach(() => {
  vi.unstubAllGlobals();
});

function stubFetch(body: unknown, status = 200) {
  const fn = vi.fn((_url: string, _init?: RequestInit) =>
    Promise.resolve(new Response(JSON.stringify(body), { status })),
  );
  vi.stubGlobal('fetch', fn);
  return fn;
}

async function failureOf(status: number, body: unknown) {
  return pimVerifyFailure(await httpErrorFrom(new Response(JSON.stringify(body), { status })));
}

describe('pimVerifyFailure', () => {
  it('reads the step and reason from a real 422 response', async () => {
    expect(
      await failureOf(422, { error: 'verify_failed', step: 'smtp', reason: 'smtp_auth_failed' }),
    ).toEqual({ step: 'smtp', reason: 'smtp_auth_failed' });
  });

  it('is null on a 500, another 422 code, an unknown reason or step, or a non-HttpError', async () => {
    const ok = { error: 'verify_failed', step: 'imap', reason: 'auth_failed' };
    expect(await failureOf(500, ok)).toBeNull();
    expect(await failureOf(422, { ...ok, error: 'other' })).toBeNull();
    expect(await failureOf(422, { ...ok, reason: 'weird' })).toBeNull();
    expect(await failureOf(422, { ...ok, step: 'pop' })).toBeNull();
    expect(pimVerifyFailure(new Error('x'))).toBeNull();
    expect(pimVerifyFailure(new HttpError(422, 'verify_failed'))).toBeNull();
  });

  it.each([
    'auth_failed',
    'unreachable',
    'tls_failed',
    'mailbox_refused',
    'smtp_auth_failed',
    'smtp_unreachable',
  ])('accepts the reason %s', async (reason) => {
    expect((await failureOf(422, { error: 'verify_failed', step: 'imap', reason }))?.reason).toBe(
      reason,
    );
  });
});

describe('pimErrorKey', () => {
  it.each<[string, unknown, string]>([
    ['502', new HttpError(502), 'settings.mail.error.pimDown'],
    ['timeout', new DOMException('timed out', 'TimeoutError'), 'settings.mail.error.timeout'],
    ['abort', new DOMException('aborted', 'AbortError'), 'settings.mail.error.timeout'],
    [
      '400 invalid_email',
      new HttpError(400, 'invalid_email'),
      'settings.mail.error.invalidAddress',
    ],
    ['401', new HttpError(401, 'calendar authorization required'), 'settings.mail.error.notReady'],
    ['429', new HttpError(429, 'too_many_attempts'), 'settings.mail.error.tooMany'],
    [
      '400 password_required',
      new HttpError(400, 'password_required'),
      'settings.mail.error.passwordRequired',
    ],
    ['409 changed', new HttpError(409, 'changed'), 'settings.mail.error.changed'],
    ['400 other', new HttpError(400, 'tls_required'), 'settings.mail.error.generic'],
    ['409 other', new HttpError(409, 'duplicate'), 'settings.mail.error.generic'],
    ['503', new HttpError(503), 'settings.mail.error.generic'],
    ['not an HttpError', new Error('boom'), 'settings.mail.error.generic'],
  ])('maps %s', (_name, error, key) => {
    expect(pimErrorKey(error)).toBe(key);
  });
});

describe('the relay calls', () => {
  it('discoverPim posts the address and returns the answer', async () => {
    const fn = stubFetch({ kind: 'imap', imapHost: 'imap.example.com', imapPort: 993 });
    await expect(discoverPim('a@example.com')).resolves.toEqual({
      kind: 'imap',
      imapHost: 'imap.example.com',
      imapPort: 993,
    });
    expect(fn.mock.calls[0]?.[0]).toBe('/api/connect/pim/discover');
    expect(fn.mock.calls[0]?.[1]?.method).toBe('POST');
    expect(fn.mock.calls[0]?.[1]?.body).toBe(JSON.stringify({ email: 'a@example.com' }));
  });

  it('verifyPimAccount posts to the encoded id and carries the warning', async () => {
    const fn = stubFetch({ smtpWarning: 'tls_failed' });
    await expect(verifyPimAccount('a b')).resolves.toEqual({ smtpWarning: 'tls_failed' });
    expect(fn.mock.calls[0]?.[0]).toBe('/api/connect/pim/accounts/a%20b/verify');
    expect(fn.mock.calls[0]?.[1]?.method).toBe('POST');
  });

  it('verifyPimAccount surfaces a 422 as an error pimVerifyFailure can read', async () => {
    stubFetch({ error: 'verify_failed', step: 'imap', reason: 'unreachable' }, 422);
    const err: unknown = await verifyPimAccount('a').catch((e: unknown) => e);
    expect(pimVerifyFailure(err)).toEqual({ step: 'imap', reason: 'unreachable' });
  });

  it('patchPimProviderConfig patches the provider-config route', async () => {
    const fn = stubFetch({ id: 'a', provider: 'imap' });
    await patchPimProviderConfig('a', { imapHost: 'imap.example.com' });
    expect(fn.mock.calls[0]?.[0]).toBe('/api/connect/pim/accounts/a/provider-config');
    expect(fn.mock.calls[0]?.[1]?.method).toBe('PATCH');
    expect(fn.mock.calls[0]?.[1]?.body).toBe(
      JSON.stringify({ providerConfig: { imapHost: 'imap.example.com' } }),
    );
  });
});
