import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  fetchTelegramStatus,
  provisionOnboarding,
  startOnboarding,
  stepOnboarding,
} from '../onboardingApi';

// onboardingApi test — the data layer over the four Plan-05 onboarding routes. It asserts the
// same-origin throwing-fetch contract (a non-200, INCLUDING 401 AND 403, THROWS Error("HTTP <n>")
// so the wizard routes the auth / no-permission / error state), the credentials: 'same-origin'
// belt, the method + URL + body each fetcher sends, and the distinct provision failure codes
// (403/409/502) the wizard maps to distinct copy.

function okJSON(body: unknown) {
  return vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 200 })));
}

function status(code: number) {
  return vi.fn(() => Promise.resolve(new Response('', { status: code })));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('onboardingApi same-origin throwing fetch', () => {
  it('startOnboarding POSTs /api/onboarding/start with same-origin + Accept and returns the body', async () => {
    const body = {
      sessionToken: 'tok',
      step: 'identity',
      content: 'q',
      status: 'active',
      capabilityOptions: ['skills.read'],
    };
    const fetchMock = okJSON(body);
    vi.stubGlobal('fetch', fetchMock);

    const res = await startOnboarding();
    expect(res).toEqual(body);

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/onboarding/start');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    expect((init.headers as Record<string, string>).Accept).toBe('application/json');
  });

  it('stepOnboarding POSTs the intent body to the encoded {token}/step path', async () => {
    const fetchMock = okJSON({ content: 'next', step: 'work', status: 'active' });
    vi.stubGlobal('fetch', fetchMock);

    await stepOnboarding('a b', { intent: 'answer', text: 'hi' });
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/onboarding/a%20b/step');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ intent: 'answer', text: 'hi' });
  });

  it('provisionOnboarding POSTs the credentials + capabilities to the {token}/provision path', async () => {
    const fetchMock = okJSON({ identityId: 'id-1', deepLink: 't.me/x', qrSvg: '<svg/>' });
    vi.stubGlobal('fetch', fetchMock);

    const res = await provisionOnboarding('tok', {
      email: 'a@b.com',
      password: 'pw',
      capabilities: ['skills.read'],
      linkTelegram: true,
    });
    expect(res.identityId).toBe('id-1');
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/onboarding/tok/provision');
    expect(JSON.parse(init.body as string)).toEqual({
      email: 'a@b.com',
      password: 'pw',
      capabilities: ['skills.read'],
      linkTelegram: true,
    });
  });

  it('fetchTelegramStatus GETs the {token}/telegram-status path and parses {linked}', async () => {
    const fetchMock = okJSON({ linked: true });
    vi.stubGlobal('fetch', fetchMock);

    const res = await fetchTelegramStatus('tok');
    expect(res.linked).toBe(true);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/onboarding/tok/telegram-status');
    expect(init.credentials).toBe('same-origin');
    // GET has no method override → undefined (fetch default).
    expect(init.method).toBeUndefined();
  });

  it('throws Error("HTTP 401") on a 401 so the wizard routes the auth state', async () => {
    vi.stubGlobal('fetch', status(401));
    await expect(startOnboarding()).rejects.toThrow('HTTP 401');
  });

  it('throws Error("HTTP 403") on a 403 (no identity.create capability)', async () => {
    vi.stubGlobal('fetch', status(403));
    await expect(
      provisionOnboarding('tok', {
        email: 'a@b.com',
        password: 'pw',
        capabilities: [],
        linkTelegram: false,
      }),
    ).rejects.toThrow('HTTP 403');
  });

  it('throws Error("HTTP 409") on a duplicate/empty email', async () => {
    vi.stubGlobal('fetch', status(409));
    await expect(
      provisionOnboarding('tok', {
        email: 'dup@b.com',
        password: 'pw',
        capabilities: [],
        linkTelegram: false,
      }),
    ).rejects.toThrow('HTTP 409');
  });

  it('throws Error("HTTP 502") on a rolled-back saga failure', async () => {
    vi.stubGlobal('fetch', status(502));
    await expect(
      provisionOnboarding('tok', {
        email: 'a@b.com',
        password: 'pw',
        capabilities: [],
        linkTelegram: false,
      }),
    ).rejects.toThrow('HTTP 502');
  });

  it('throws Error("HTTP 404") on an expired session for step + telegram-status', async () => {
    vi.stubGlobal('fetch', status(404));
    await expect(stepOnboarding('gone', { intent: 'skip' })).rejects.toThrow('HTTP 404');
    await expect(fetchTelegramStatus('gone')).rejects.toThrow('HTTP 404');
  });
});
