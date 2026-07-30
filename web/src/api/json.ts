// HttpError carries the HTTP status so callers can branch on it (e.g. a 409 same-password reset)
// without string-parsing the message. It stays an Error subclass, so existing generic catches work.
export class HttpError extends Error {
  readonly status: number;

  constructor(status: number) {
    super(`HTTP ${String(status)}`);
    this.name = 'HttpError';
    this.status = status;
  }
}

export function isTrue(value: unknown): boolean {
  return value === true;
}

export async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
    ...(signal !== undefined ? { signal } : {}),
  });
  if (!res.ok) {
    throw new HttpError(res.status);
  }
  return (await res.json()) as T;
}

export async function postJSON<T>(url: string, body: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new HttpError(res.status);
  }
  return (await res.json()) as T;
}
