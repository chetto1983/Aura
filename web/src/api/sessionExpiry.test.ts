import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { expiredLoginPath, installSessionExpiryRedirect, safeReturnPath } from './sessionExpiry';

const originalFetch = window.fetch.bind(window);
const originalLocation = window.location;
let assign: ReturnType<typeof vi.fn>;

function at(pathname: string, search = '') {
  assign = vi.fn();
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: {
      origin: originalLocation.origin,
      href: `${originalLocation.origin}${pathname}${search}`,
      pathname,
      search,
      assign,
    },
  });
}

// The auth gate's refusal: a 401 carrying the session challenge (internal/agui/auth.go).
const SESSION_ENDED = { status: 401, headers: { 'WWW-Authenticate': 'Session' } };

function answering(init: ResponseInit) {
  window.fetch = vi.fn().mockResolvedValue(new Response(null, init));
  installSessionExpiryRedirect();
}

beforeEach(() => {
  at('/c/thread-1');
});

afterEach(() => {
  window.fetch = originalFetch;
  Object.defineProperty(window, 'location', { configurable: true, value: originalLocation });
  vi.restoreAllMocks();
});

describe('installSessionExpiryRedirect', () => {
  it('sends an ended session to the login page and keeps where the person was', async () => {
    answering(SESSION_ENDED);
    const res = await window.fetch('/api/me');
    expect(res.status).toBe(401);
    expect(assign).toHaveBeenCalledWith('/login?expired=1&next=%2Fc%2Fthread-1');
  });

  it('leaves other statuses alone', async () => {
    answering({ status: 403, headers: SESSION_ENDED.headers });
    await window.fetch('/api/settings');
    expect(assign).not.toHaveBeenCalled();
  });

  // The cockpit's calendar panel answers 401 "calendar authorization required" until the
  // person authorizes the calendar, and Authula answers 401 to a refused sign-in: neither
  // ends the session, so neither carries the challenge.
  it('leaves a 401 without the session challenge to its caller', async () => {
    answering({ status: 401 });
    await window.fetch('/api/connect/pim/accounts');
    await window.fetch(new Request(`${originalLocation.origin}/auth/email-password/sign-in`));
    expect(assign).not.toHaveBeenCalled();
  });

  it('ignores another origin', async () => {
    answering(SESSION_ENDED);
    await window.fetch('https://objects.example/upload');
    expect(assign).not.toHaveBeenCalled();
  });

  it.each(['/login', '/s/public-token', '/shared/share-1'])(
    'does not redirect from the public page %s',
    async (page) => {
      at(page);
      answering(SESSION_ENDED);
      await window.fetch('/api/shares/share-1/data');
      expect(assign).not.toHaveBeenCalled();
    },
  );
});

describe('expiredLoginPath', () => {
  it('omits next for the cockpit root', () => {
    expect(expiredLoginPath({ pathname: '/', search: '' })).toBe('/login?expired=1');
  });

  it('carries the query string back', () => {
    expect(expiredLoginPath({ pathname: '/c/t1', search: '?tab=files' })).toBe(
      '/login?expired=1&next=%2Fc%2Ft1%3Ftab%3Dfiles',
    );
  });
});

describe('safeReturnPath', () => {
  it.each([
    [null, '/'],
    ['', '/'],
    ['/c/thread-1', '/c/thread-1'],
    ['/c/t1?tab=files', '/c/t1?tab=files'],
    ['https://evil.example/', '/'],
    ['//evil.example/', '/'],
    ['/\\evil.example', '/'],
    ['/login?expired=1', '/'],
  ])('%s returns %s', (raw, want) => {
    expect(safeReturnPath(raw)).toBe(want);
  });
});
