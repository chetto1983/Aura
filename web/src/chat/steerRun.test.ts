import { afterEach, describe, expect, it, vi } from 'vitest';
import { steerRun, SteerRefusal } from './steerRun';

// steerRun — the cockpit steer client (Phase 52 plan 07, Task 1). Proves: the exact route +
// method + headers + body, the Idempotency-Key minted ONCE and reused across a retry of the
// SAME logical send (T-52-60 — a fresh key per retry would enqueue the steer twice), and the
// 202/400/429/410 -> resolve/invalid/busy/ended classification by KIND, not by message text.

function jsonHeaders(res: Response, extra: Record<string, string> = {}): Response {
  const headers = new Headers(res.headers);
  for (const [k, v] of Object.entries(extra)) headers.set(k, v);
  return new Response(res.body, { status: res.status, headers });
}

/** mock.calls[n] is typed possibly-undefined under noUncheckedIndexedAccess; this asserts
 *  the call actually happened (the surrounding toHaveBeenCalledTimes already proves it). */
function callArgs(
  calls: readonly (readonly unknown[])[],
  index: number,
): [string, RequestInit | undefined] {
  const call = calls[index];
  if (call === undefined) throw new Error(`expected a fetch call at index ${String(index)}`);
  return call as [string, RequestInit | undefined];
}

describe('steerRun', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('POSTs to /agent/runs/{runID}/steer with same-origin credentials, JSON content-type and the text body', async () => {
    const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(() =>
      Promise.resolve(new Response(null, { status: 202 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    await steerRun('run-1', 'stop and check the invoice first').send();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = callArgs(fetchMock.mock.calls, 0);
    expect(url).toBe('/agent/runs/run-1/steer');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    const headers = new Headers(init?.headers);
    expect(headers.get('Content-Type')).toBe('application/json');
    expect(JSON.parse(init?.body as string)).toEqual({ text: 'stop and check the invoice first' });
  });

  it('encodes a runId containing reserved characters', async () => {
    const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(() =>
      Promise.resolve(new Response(null, { status: 202 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    await steerRun('run/with?weird chars', 'hi').send();

    const [url] = callArgs(fetchMock.mock.calls, 0);
    expect(url).toBe(`/agent/runs/${encodeURIComponent('run/with?weird chars')}/steer`);
  });

  it('mints an Idempotency-Key and reuses the SAME value across a retry of one logical send', async () => {
    const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(() =>
      Promise.resolve(new Response(null, { status: 202 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    const steer = steerRun('run-1', 'redirect this');
    await steer.send();
    await steer.send(); // a transport retry of the SAME logical send, not a fresh steer

    expect(fetchMock).toHaveBeenCalledTimes(2);
    const first = new Headers(callArgs(fetchMock.mock.calls, 0)[1]?.headers);
    const second = new Headers(callArgs(fetchMock.mock.calls, 1)[1]?.headers);
    const firstKey = first.get('Idempotency-Key');
    expect(firstKey).toBeTruthy();
    expect(second.get('Idempotency-Key')).toBe(firstKey);
  });

  it('mints a DIFFERENT key for a second, independent steerRun call', async () => {
    const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(() =>
      Promise.resolve(new Response(null, { status: 202 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    await steerRun('run-1', 'first steer').send();
    await steerRun('run-1', 'second steer').send();

    const first = new Headers(callArgs(fetchMock.mock.calls, 0)[1]?.headers);
    const second = new Headers(callArgs(fetchMock.mock.calls, 1)[1]?.headers);
    expect(first.get('Idempotency-Key')).not.toBe(second.get('Idempotency-Key'));
  });

  it('classifies a 400 as SteerRefusal("invalid")', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('empty text', { status: 400 }))),
    );
    await expect(steerRun('run-1', '').send()).rejects.toMatchObject(
      new SteerRefusal('invalid', 'empty text'),
    );
  });

  it('classifies a 429 as SteerRefusal("busy") carrying Retry-After when present', async () => {
    const res = jsonHeaders(new Response('queue full', { status: 429 }), { 'Retry-After': '5' });
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(res)),
    );

    let caught: unknown;
    try {
      await steerRun('run-1', 'hi').send();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(SteerRefusal);
    const refusal = caught as SteerRefusal;
    expect(refusal.kind).toBe('busy');
    expect(refusal.retryAfter).toBe('5');
  });

  it('classifies a 410 as SteerRefusal("ended")', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response('run has ended: message was not queued; send it as a normal turn', {
            status: 410,
          }),
        ),
      ),
    );
    let caught: unknown;
    try {
      await steerRun('run-1', 'hi').send();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(SteerRefusal);
    expect((caught as SteerRefusal).kind).toBe('ended');
  });

  it('resolves on 202 without throwing', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(null, { status: 202 }))),
    );
    await expect(steerRun('run-1', 'hi').send()).resolves.toBeUndefined();
  });

  it('throws a plain (non-SteerRefusal) Error carrying errorDetail(res) for any other status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('internal error', { status: 500 }))),
    );
    let caught: unknown;
    try {
      await steerRun('run-1', 'hi').send();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(Error);
    expect(caught).not.toBeInstanceOf(SteerRefusal);
    expect((caught as Error).message).toBe('internal error');
  });
});
