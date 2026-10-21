// HttpError carries the HTTP status so callers can branch on it (e.g. a 409 same-password reset)
// without string-parsing the message. It stays an Error subclass, so existing generic catches work.
export class HttpError extends Error {
  readonly status: number;

  /** The server's own `{error}` sentence, empty when it sent none. A bare status hides the one
   * thing that tells the operator what to change: measured 2026-08-22 on the calendar connect
   * wizard, where the sidecar named the offending field and the browser showed only "HTTP 400". */
  readonly reason: string;

  /** The server's `{message}` beside a code in `{error}`: the sentence the operator reads, empty
   * when it sent none. */
  readonly detail: string;

  constructor(status: number, reason = '', detail = '') {
    super(reason === '' ? `HTTP ${String(status)}` : `HTTP ${String(status)}: ${reason}`);
    this.name = 'HttpError';
    this.status = status;
    this.reason = reason;
    this.detail = detail;
  }
}

/** httpErrorFrom builds the HttpError a failed response deserves, carrying the `{error}` sentence,
 * and the `{message}` beside it, when the body is JSON that has them. A non-JSON body (an HTML
 * gateway page) yields a bare status — there is nothing to surface, and guessing at HTML would be
 * worse than saying only the number. */
export async function httpErrorFrom(res: Response): Promise<HttpError> {
  const body = await errorBody(res);
  return new HttpError(res.status, stringField(body, 'error'), stringField(body, 'message'));
}

async function errorBody(res: Response): Promise<unknown> {
  try {
    return await res.json();
  } catch {
    // Not JSON, or an empty body: nothing to surface.
    return null;
  }
}

function stringField(body: unknown, name: string): string {
  if (body === null || typeof body !== 'object' || !(name in body)) return '';
  const raw: unknown = (body as Record<string, unknown>)[name];
  return typeof raw === 'string' ? raw.trim() : '';
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

function jsonRequest(
  method: string,
  url: string,
  body: unknown,
  signal?: AbortSignal,
): Promise<Response> {
  return fetch(url, {
    method,
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    ...(signal ? { signal } : {}),
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
}

export async function postJSON<T>(url: string, body: unknown): Promise<T> {
  const res = await jsonRequest('POST', url, body);
  if (!res.ok) {
    throw new HttpError(res.status);
  }
  return (await res.json()) as T;
}

/** sendJSON is any JSON write whose failure keeps the server's `{error}` code: the caller
 * branches on HttpError.reason to say what to change. A body-less call still labels itself
 * application/json, the CSRF floor the server's write boundary checks. */
export async function sendJSON<T>(
  method: string,
  url: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const res = await jsonRequest(method, url, body, signal);
  if (!res.ok) {
    throw await httpErrorFrom(res);
  }
  return (await res.json()) as T;
}
