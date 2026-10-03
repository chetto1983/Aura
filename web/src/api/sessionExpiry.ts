// Pages that work without a session and render a 401 themselves.
const PUBLIC_PAGE_PREFIXES = ['/login', '/s/', '/shared/'];

/**
 * Sends the person to the login page as soon as the server says their session is gone.
 * Every cockpit call is a same-origin fetch, so a 401 from one means the session expired
 * or was revoked; before this, each panel showed its own error and only a manual reload
 * reached /login. Authula's own /auth/* routes answer 401 as part of their flows (a
 * refused sign-in, a sign-out of an ended session) and are left to their callers.
 */
export function installSessionExpiryRedirect(): void {
  const nativeFetch = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const response = await nativeFetch(input, init);
    if (response.status === 401 && isCockpitCall(input) && !onPublicPage()) {
      window.location.assign(expiredLoginPath(window.location));
    }
    return response;
  };
}

function isCockpitCall(input: RequestInfo | URL): boolean {
  const rawURL = input instanceof Request ? input.url : input instanceof URL ? input.href : input;
  const url = new URL(rawURL, window.location.href);
  return url.origin === window.location.origin && !url.pathname.startsWith('/auth/');
}

function onPublicPage(): boolean {
  const path = window.location.pathname;
  return PUBLIC_PAGE_PREFIXES.some((prefix) => path === prefix || path.startsWith(prefix));
}

export function expiredLoginPath(location: Pick<Location, 'pathname' | 'search'>): string {
  const params = new URLSearchParams({ expired: '1' });
  const back = `${location.pathname}${location.search}`;
  if (back !== '/') {
    params.set('next', back);
  }
  return `/login?${params.toString()}`;
}

/**
 * The path LoginPage returns to. Only a path on this origin is accepted, so a crafted
 * ?next= cannot send a freshly signed-in person somewhere else.
 */
export function safeReturnPath(raw: string | null): string {
  if (raw === null || !raw.startsWith('/') || raw.startsWith('//') || raw.startsWith('/\\')) {
    return '/';
  }
  return raw.startsWith('/login') ? '/' : raw;
}
