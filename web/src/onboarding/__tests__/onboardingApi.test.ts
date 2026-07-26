import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  fetchOnboardingStatus,
  fetchTelegramStatus,
  provisionOnboarding,
  startOnboarding,
  submitOnboardingProfile,
} from '../onboardingApi';

// onboardingApi test — the data layer over the onboarding REST routes. It asserts the same-origin
// throwing-fetch contract (a non-200, INCLUDING 401 AND 403, THROWS Error("HTTP <n>") so the
// surface routes the auth / no-permission / error state), the credentials: 'same-origin' belt, the
// method + URL + body each fetcher sends, and the distinct provision failure codes (403/409/502)
// the wizard maps to distinct copy.

const SEED = {
  name: 'José-María',
  lang: 'it',
  location: 'Caraglio',
  timezone: 'Europe/Rome',
  role: 'founder',
  company: 'PmSync',
};

function okJSON(body: unknown) {
  return vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 200 })));
}

function status(code: number) {
  return vi.fn(() => Promise.resolve(new Response('', { status: code })));
}

function provisionBody(seed = SEED) {
  return {
    email: 'a@b.com',
    password: 'pw',
    securityQuestion: 'First school?',
    securityAnswer: 'blue',
    capabilities: [],
    linkTelegram: true,
    seed,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('onboardingApi same-origin throwing fetch', () => {
  it('startOnboarding POSTs /api/onboarding/start with same-origin + Accept and returns the body', async () => {
    const body = { sessionToken: 'tok', capabilityOptions: ['skills.read'] };
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

  it('submitOnboardingProfile POSTs the whole seed to the token-less /api/onboarding/profile', async () => {
    const body = {
      completed: true,
      skipped: false,
      sessionToken: 'sess-1',
      deepLink: 'https://t.me/AuraBot?start=onb',
      qrSvg: '<svg/>',
    };
    const fetchMock = okJSON(body);
    vi.stubGlobal('fetch', fetchMock);

    await expect(submitOnboardingProfile(SEED)).resolves.toEqual(body);

    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/onboarding/profile');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    // The seed reaches the wire byte-identical — no trimming, no normalisation client-side.
    expect(JSON.parse(init.body as string)).toEqual(SEED);
  });

  it('submitOnboardingProfile sends an EMPTY object when nothing was filled (the derived skip)', async () => {
    const fetchMock = okJSON({ completed: false, skipped: true, sessionToken: 'sess-1' });
    vi.stubGlobal('fetch', fetchMock);

    await expect(submitOnboardingProfile({})).resolves.toEqual({
      completed: false,
      skipped: true,
      sessionToken: 'sess-1',
    });
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toEqual({});
  });

  it('provisionOnboarding POSTs the credentials + capabilities + seed to the {token}/provision path', async () => {
    const fetchMock = okJSON({ identityId: 'id-1', deepLink: 't.me/x', qrSvg: '<svg/>' });
    vi.stubGlobal('fetch', fetchMock);

    const res = await provisionOnboarding('a b', provisionBody());
    expect(res.identityId).toBe('id-1');
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/onboarding/a%20b/provision');
    expect(JSON.parse(init.body as string)).toEqual(provisionBody());
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

  it('fetchOnboardingStatus GETs /api/onboarding/status and parses the three flags', async () => {
    const fetchMock = okJSON({ required: true, completed: false, skipped: false });
    vi.stubGlobal('fetch', fetchMock);

    await expect(fetchOnboardingStatus()).resolves.toEqual({
      required: true,
      completed: false,
      skipped: false,
    });
    const [url] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/onboarding/status');
  });

  it('throws Error("HTTP 401") on a 401 so the surface routes the auth state', async () => {
    vi.stubGlobal('fetch', status(401));
    await expect(startOnboarding()).rejects.toThrow('HTTP 401');
    await expect(submitOnboardingProfile(SEED)).rejects.toThrow('HTTP 401');
  });

  it('throws Error("HTTP 403") on a 403 (no identity.create capability)', async () => {
    vi.stubGlobal('fetch', status(403));
    await expect(provisionOnboarding('tok', provisionBody())).rejects.toThrow('HTTP 403');
  });

  it('throws Error("HTTP 409") on a duplicate/empty email', async () => {
    vi.stubGlobal('fetch', status(409));
    await expect(provisionOnboarding('tok', provisionBody())).rejects.toThrow('HTTP 409');
  });

  it('throws Error("HTTP 502") on a rolled-back saga and on a failed profile write', async () => {
    vi.stubGlobal('fetch', status(502));
    await expect(provisionOnboarding('tok', provisionBody())).rejects.toThrow('HTTP 502');
    await expect(submitOnboardingProfile(SEED)).rejects.toThrow('HTTP 502');
  });

  it('throws Error("HTTP 400") when a seed field exceeds a server cap', async () => {
    vi.stubGlobal('fetch', status(400));
    await expect(submitOnboardingProfile({ name: 'x'.repeat(257) })).rejects.toThrow('HTTP 400');
  });

  it('throws Error("HTTP 404") on an expired telegram-status session', async () => {
    vi.stubGlobal('fetch', status(404));
    await expect(fetchTelegramStatus('gone')).rejects.toThrow('HTTP 404');
  });
});
