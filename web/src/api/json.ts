// HttpError carries the HTTP status so callers can branch on it (e.g. a 409 same-password reset)
// without string-parsing the message. It stays an Error subclass, so existing generic catches work.
export class HttpError extends Error {
  readonly status: number;

  /** The server's own `{error}` sentence, empty when it sent none. A bare status hides the one
   * thing that tells the operator what to change: measured 2026-08-22 on the calendar connect
   * wizard, where the sidecar named the offending field and the browser showed only "HTTP 400". */
  readonly reason: string;

  constructor(status: number, reason = '') {
    super(reason === '' ? `HTTP ${String(status)}` : `HTTP ${String(status)}: ${reason}`);
    this.name = 'HttpError';
    this.status = status;
    this.reason = reason;
  }
}

/** httpErrorFrom builds the HttpError a failed response deserves, carrying the `{error}` sentence
 * when the body is JSON that has one. A non-JSON body (an HTML gateway page) yields a bare status —
 * there is nothing to surface, and guessing at HTML would be worse than saying only the number. */
export async function httpErrorFrom(res: Response): Promise<HttpError> {
  return new HttpError(res.status, await errorReason(res));
}

async function errorReason(res: Response): Promise<string> {
  try {
    const body: unknown = await res.json();
    if (body !== null && typeof body === 'object' && 'error' in body) {
      const raw: unknown = body.error;
      if (typeof raw === 'string') return raw.trim();
    }
  } catch {
    // Not JSON, or an empty body: no reason to surface.
  }
  return '';
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
