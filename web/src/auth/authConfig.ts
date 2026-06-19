// Shared parsing helpers for the `/api/auth/config` response + CSRF cookie.
// Used by both AppShell (logout target) and LoginPage (sign-in flow); kept in one
// module so the authula provider-detection logic is defined exactly once.

export async function readJSON(res: Response): Promise<unknown> {
  const text = await res.text();
  if (text.trim() === '') return {};
  return JSON.parse(text) as unknown;
}

export function stringField(source: unknown, key: string): string {
  if (source === null || typeof source !== 'object') return '';
  const value = (source as Record<string, unknown>)[key];
  return typeof value === 'string' ? value : '';
}

export function booleanField(source: unknown, key: string): boolean {
  if (source === null || typeof source !== 'object') return false;
  const value = (source as Record<string, unknown>)[key];
  return value === true || value === 'true';
}

export function valueOrFallback(value: string, fallback: string): string {
  return value === '' ? fallback : value;
}

export function readCookie(name: string): string {
  const prefix = `${name}=`;
  return (
    document.cookie
      .split(';')
      .map((part) => part.trim())
      .find((part) => part.startsWith(prefix))
      ?.slice(prefix.length) ?? ''
  );
}
