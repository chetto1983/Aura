import { useCallback, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { readCookie, readJSON, stringField, valueOrFallback } from '../auth/authConfig';

// 37F plan 14 — the logout/session-teardown state seam, extracted from AppShell.tsx so the shell
// stays under the 600-LOC cap (refactor-on-touch, R-02): AppShell.tsx was already at 597/600
// BEFORE this plan's ShareToggle wiring landed, leaving no margin for even a few new lines. This
// is a pure extraction with NO behavior change — mirrors useArtifactsPanel.ts's stated reason for
// existing (:4-7) — proven by AppShell.shell.test.tsx's pre-existing logout tests
// ("does not fall back to the legacy passphrase logout route" / "uses the Authula sign-out
// endpoint...") passing unedited after the move.

interface LogoutTarget {
  path: string;
  headers?: Record<string, string>;
  body?: string;
}

const defaultAuthulaBasePath = '/auth';
const defaultCSRFCookieName = '__Host-authula_csrf_token';
const defaultCSRFHeaderName = 'X-AUTHULA-CSRF-TOKEN';

async function loadLogoutTarget(): Promise<LogoutTarget | null> {
  try {
    const res = await fetch('/api/auth/config', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
    if (!res.ok) return null;
    const raw = await readJSON(res);
    if (stringField(raw, 'provider') !== 'authula') return null;

    const authBasePath = valueOrFallback(
      stringField(raw, 'auth_base_path'),
      defaultAuthulaBasePath,
    );
    const csrfCookieName = valueOrFallback(
      stringField(raw, 'csrf_cookie_name'),
      defaultCSRFCookieName,
    );
    const csrfHeaderName = valueOrFallback(
      stringField(raw, 'csrf_header_name'),
      defaultCSRFHeaderName,
    );
    const csrfToken = valueOrFallback(
      stringField(raw, 'csrf_token'),
      res.headers.get(csrfHeaderName) ?? readCookie(csrfCookieName),
    );
    if (csrfToken === '') return null;

    return {
      path: `${authBasePath}/sign-out`,
      headers: {
        'Content-Type': 'application/json',
        [csrfHeaderName]: csrfToken,
      },
      body: '{}',
    };
  } catch {
    return null;
  }
}

export interface LogoutSession {
  readonly logoutPending: boolean;
  readonly logout: () => Promise<void>;
}

// Loads the Authula sign-out target (if the configured provider is Authula), posts to it with
// its CSRF header, then routes to /login. A non-Authula provider or a load failure leaves the
// operator in the cockpit rather than navigating away from an unclear state.
export function useLogoutSession(): LogoutSession {
  const navigate = useNavigate();
  const [logoutPending, setLogoutPending] = useState(false);

  const logout = useCallback(async () => {
    if (logoutPending) return;
    setLogoutPending(true);
    try {
      const target = await loadLogoutTarget();
      if (target === null) {
        setLogoutPending(false);
        return;
      }
      const init: RequestInit = {
        method: 'POST',
        credentials: 'same-origin',
      };
      if (target.headers !== undefined) init.headers = target.headers;
      if (target.body !== undefined) init.body = target.body;
      await fetch(target.path, init);
      void navigate('/login', { replace: true });
      return;
    } catch {
      // Keep the operator in the cockpit if the server could not clear the session.
    }
    setLogoutPending(false);
  }, [logoutPending, navigate]);

  return { logoutPending, logout };
}
