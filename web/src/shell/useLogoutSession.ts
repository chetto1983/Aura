import { useCallback, useState } from 'react';
import { useNavigate } from 'react-router';
import { readCookie, readJSON, stringField, valueOrFallback } from '../auth/authConfig';

// Kept out of AppShell.tsx, which sits at the 600-line cap.

interface LogoutTarget {
  path: string;
  headers: Record<string, string>;
  body: string;
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

// A 401 means the server holds no session for this cookie, so there is nothing left to end.
async function endSession(target: LogoutTarget): Promise<boolean> {
  const res = await fetch(target.path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: target.headers,
    body: target.body,
  });
  return res.ok || res.status === 401;
}

export interface LogoutSession {
  readonly logoutPending: boolean;
  readonly logout: () => Promise<void>;
}

// Posts the Authula sign-out with its CSRF header and routes to /login only once the server
// has ended the session. A non-Authula provider, a load failure or a refused sign-out leaves
// the operator in the cockpit: navigating away would show a logout while the session lives on.
export function useLogoutSession(): LogoutSession {
  const navigate = useNavigate();
  const [logoutPending, setLogoutPending] = useState(false);

  const logout = useCallback(async () => {
    if (logoutPending) return;
    setLogoutPending(true);
    try {
      const target = await loadLogoutTarget();
      if (target !== null && (await endSession(target))) {
        void navigate('/login', { replace: true });
        return;
      }
    } catch {
      // A network failure is a refused sign-out too.
    }
    setLogoutPending(false);
  }, [logoutPending, navigate]);

  return { logoutPending, logout };
}
