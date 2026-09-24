import { afterEach, describe, expect, it, vi } from 'vitest';
import { HttpError } from '../../api/json';
import {
  UPDATE_FAST_POLL_MS,
  UPDATE_POLL_MS,
  applyUpdate,
  deferUpdate,
  fetchSystemUpdate,
  isUpdating,
  updatePollInterval,
  type SystemUpdateStatus,
} from '../systemUpdateApi';

interface Call {
  readonly url: string;
  readonly init: RequestInit | undefined;
}

function stubFetch(status: number, body: unknown): Call[] {
  const calls: Call[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        url: typeof input === 'string' ? input : input instanceof URL ? input.href : input.url,
        init,
      });
      return Promise.resolve(new Response(JSON.stringify(body), { status }));
    }),
  );
  return calls;
}

const adminAnswer = {
  managed: true,
  state: 'pending',
  running_rev: '7886200e5c1a',
  available_rev: 'abc1234def56',
  available_built_at: '2026-09-24T07:12:00Z',
  pending_since: '2026-09-24T07:30:00Z',
  deadline: '2026-09-25T07:30:00Z',
  deferred_until: null,
  checked_at: '2026-09-24T09:59:00Z',
  can_decide: true,
  error: 'compose pull failed',
  last_activity_at: '2026-09-24T09:57:00Z',
  live_runs: 2,
};

function normalized(fields: Partial<SystemUpdateStatus>): SystemUpdateStatus {
  return {
    managed: false,
    state: 'unknown',
    running_rev: '',
    available_rev: '',
    available_built_at: null,
    pending_since: null,
    deadline: null,
    deferred_until: null,
    checked_at: null,
    can_decide: false,
    error: '',
    last_activity_at: null,
    live_runs: 0,
    ...fields,
  };
}

describe('fetchSystemUpdate', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('GETs the status same-origin as JSON and keeps every field an admin receives', async () => {
    const calls = stubFetch(200, adminAnswer);
    const signal = new AbortController().signal;

    await expect(fetchSystemUpdate(signal)).resolves.toEqual(adminAnswer);

    expect(calls).toEqual([
      {
        url: '/api/system/update',
        init: { headers: { Accept: 'application/json' }, credentials: 'same-origin', signal },
      },
    ]);
  });

  it('fills the fields the daemon leaves out for a member with their empty values', async () => {
    stubFetch(200, { managed: true, state: 'requested', running_rev: 'r1', can_decide: false });

    await expect(fetchSystemUpdate()).resolves.toEqual(
      normalized({ managed: true, state: 'requested', running_rev: 'r1' }),
    );
  });

  it('reads an unmanaged stack, and any answer that is not a status, as unmanaged', async () => {
    stubFetch(200, { managed: false, deferred_until: null, can_decide: false });
    await expect(fetchSystemUpdate()).resolves.toEqual(normalized({}));

    vi.unstubAllGlobals();
    stubFetch(200, null);
    await expect(fetchSystemUpdate()).resolves.toEqual(normalized({}));

    vi.unstubAllGlobals();
    stubFetch(200, { managed: 'yes', can_decide: 'yes' });
    await expect(fetchSystemUpdate()).resolves.toEqual(normalized({}));
  });

  it('rejects a non-2xx answer with its status', async () => {
    stubFetch(503, { error: 'restarting' });

    const err: unknown = await fetchSystemUpdate().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(HttpError);
    expect((err as HttpError).status).toBe(503);
  });
});

describe('applyUpdate / deferUpdate', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('POSTs apply with no body', async () => {
    const calls = stubFetch(202, { state: 'requested' });

    await expect(applyUpdate()).resolves.toEqual({ state: 'requested' });

    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toBe('/api/system/update/apply');
    expect(calls[0]?.init?.method).toBe('POST');
    expect(calls[0]?.init?.body).toBeUndefined();
    expect(calls[0]?.init?.credentials).toBe('same-origin');
  });

  it('POSTs the absolute until of a deferral and returns the time the daemon kept', async () => {
    const calls = stubFetch(202, { deferred_until: '2026-09-25T01:00:00Z' });

    await expect(deferUpdate('2026-09-25T03:00:00Z')).resolves.toEqual({
      deferred_until: '2026-09-25T01:00:00Z',
    });

    expect(calls[0]?.url).toBe('/api/system/update/defer');
    expect(calls[0]?.init?.method).toBe('POST');
    expect(calls[0]?.init?.body).toBe('{"until":"2026-09-25T03:00:00Z"}');
  });

  it('rejects a refused decision with its status', async () => {
    stubFetch(409, { error: 'no update waiting to be applied' });

    const err: unknown = await applyUpdate().catch((e: unknown) => e);
    expect((err as HttpError).status).toBe(409);
  });
});

describe('polling cadence', () => {
  it('polls fast only while the daemon is about to go away', () => {
    expect(UPDATE_POLL_MS).toBe(60_000);
    expect(UPDATE_FAST_POLL_MS).toBe(3_000);
    expect(isUpdating('requested')).toBe(true);
    expect(isUpdating('applying')).toBe(true);
    for (const state of ['current', 'pending', 'failed', 'unknown', undefined] as const) {
      expect(isUpdating(state)).toBe(false);
    }
  });

  it('stops polling an unmanaged stack and keeps polling before the first answer', () => {
    expect(updatePollInterval(undefined)).toBe(UPDATE_POLL_MS);
    expect(updatePollInterval(normalized({}))).toBe(false);
    expect(updatePollInterval(normalized({ managed: true, state: 'pending' }))).toBe(
      UPDATE_POLL_MS,
    );
    expect(updatePollInterval(normalized({ managed: true, state: 'applying' }))).toBe(
      UPDATE_FAST_POLL_MS,
    );
  });
});
