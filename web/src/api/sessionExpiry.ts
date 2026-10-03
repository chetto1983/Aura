// Pages that work without a session and render a 401 themselves.
const PUBLIC_PAGE_PREFIXES = ['/login', '/s/', '/shared/'];

// The WWW-Authenticate value the server's auth gate puts on the 401 that means the
// request has no live session (sessionChallenge, internal/agui/auth.go).
const SESSION_CHALLENGE = 'Session';

/**
 * Sends the person to the login page as soon as the server says their session is gone.
 * Only the auth gate's 401 says that: it carries the session challenge. Other 401s
 * arrive while the session is valid (the calendar not yet authorized, an upstream
 * refusal relayed, Authula refusing a sign-in) and stay with their callers. Before
 * this, each panel showed its own error and only a manual reload reached /login.
 */
export function installSessionExpiryRedirect(): void {
  const nativeFetch = window.fetch.bind(window);
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const response = await nativeFetch(input, init);
    if (endsSession(response) && isSameOrigin(input) && !onPublicPage()) {
      window.location.assign(expiredLoginPath(window.location));
    }
    return response;
  };
}

function endsSession(response: Response): boolean {
  return response.status === 401 && response.headers.get('WWW-Authenticate') === SESSION_CHALLENGE;
}

function isSameOrigin(input: RequestInfo | URL): boolean {
  const rawURL = input instanceof Request ? input.url : input instanceof URL ? input.href : input;
  return new URL(rawURL, window.location.href).origin === window.location.origin;
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
