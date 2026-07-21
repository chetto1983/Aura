/** Shared HTTP response helpers for the chat API clients (attachments, share, …).
 *  Defined once here so the errorDetail/readJSON pair is not copied per client — the
 *  per-file copies tripped the jscpd duplication gate (Go `dupl` parity). */

export async function errorDetail(res: Response): Promise<string> {
  const status = `HTTP ${String(res.status)}`;
  if (res.body === null) return status;
  const detail = (await res.text().catch(() => '')).trim();
  return detail.length > 0 ? detail : status;
}

export async function readJSON<T>(res: Response): Promise<T> {
  if (!res.ok) throw new Error(await errorDetail(res));
  return (await res.json()) as T;
}
