// localStorage-backed bearer token store. The dashboard is single-tenant
// and the token is the only credential, so localStorage is the simplest
// home for it — survives page refreshes, scoped to origin, no
// server-side session required.
//
// Trade-off acknowledged in the design doc (phase-10-ui §10d): an XSS
// would let an attacker exfiltrate the token. Mitigations: dashboard
// hosts no user-generated HTML in attribute positions, and a logout
// revokes the token server-side so even a leaked copy stops working
// once the user signs out.

const STORAGE_KEY = 'aura_token';

export function getToken(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null; // localStorage may throw in private mode
  }
}

export function setToken(token: string): void {
  try {
    localStorage.setItem(STORAGE_KEY, token);
  } catch {
    // ignore — caller will see the next API call fail with 401
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // ignore
  }
}
