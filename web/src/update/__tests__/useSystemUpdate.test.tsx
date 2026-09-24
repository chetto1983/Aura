import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { UPDATE_FAST_POLL_MS, UPDATE_POLL_MS, UPDATE_QUERY_KEY } from '../systemUpdateApi';
import { useApplyUpdate, useDeferUpdate, useSystemUpdate } from '../useSystemUpdate';

type Answer = Record<string, unknown> | Error;

interface Recorded {
  readonly gets: string[];
  readonly posts: { url: string; body: string | undefined }[];
}

function body(fields: Record<string, unknown>): Record<string, unknown> {
  return {
    managed: true,
    state: 'current',
    running_rev: 'rev-old',
    available_rev: '',
    available_built_at: null,
    pending_since: null,
    deadline: null,
    deferred_until: null,
    checked_at: null,
    can_decide: false,
    ...fields,
  };
}

// GETs walk through `answers` one per poll and then keep repeating the last; an Error answer
// is a refused connection. POSTs answer from `posts`.
function stubDaemon(answers: Answer[], posts: Record<string, Response> = {}): Recorded {
  const recorded: Recorded = { gets: [], posts: [] };
  let next = 0;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (init?.method === 'POST') {
        recorded.posts.push({ url, body: typeof init.body === 'string' ? init.body : undefined });
        const answer = posts[url];
        return answer === undefined
          ? Promise.reject(new Error(`unexpected POST ${url}`))
          : Promise.resolve(answer.clone());
      }
      recorded.gets.push(url);
      const answer = answers[Math.min(next, answers.length - 1)];
      next += 1;
      if (answer instanceof Error) return Promise.reject(answer);
      return Promise.resolve(new Response(JSON.stringify(answer), { status: 200 }));
    }),
  );
  return recorded;
}

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  return { client, Wrapper };
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe('useSystemUpdate', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('polls every minute while nothing is happening', async () => {
    const recorded = stubDaemon([body({ state: 'pending', available_rev: 'rev-new' })]);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(vi.fn()), { wrapper: Wrapper });
    await waitFor(() => {
      expect(result.current.status?.state).toBe('pending');
    });
    const before = recorded.gets.length;

    await advance(UPDATE_POLL_MS - 1000);
    expect(recorded.gets).toHaveLength(before);
    await advance(1000);
    expect(recorded.gets).toHaveLength(before + 1);
    expect(recorded.gets.at(-1)).toBe('/api/system/update');
    expect(result.current.restarting).toBe(false);
  });

  it('polls every three seconds once an update is requested', async () => {
    const recorded = stubDaemon([body({ state: 'requested' })]);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(vi.fn()), { wrapper: Wrapper });
    await waitFor(() => {
      expect(result.current.status?.state).toBe('requested');
    });
    const before = recorded.gets.length;

    await advance(UPDATE_FAST_POLL_MS);
    expect(recorded.gets).toHaveLength(before + 1);
    await advance(UPDATE_FAST_POLL_MS);
    expect(recorded.gets).toHaveLength(before + 2);
    expect(result.current.restarting).toBe(false);
  });

  it('treats a daemon that stops answering after a request as a restart in progress', async () => {
    stubDaemon([body({ state: 'requested' }), new TypeError('Failed to fetch')]);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(vi.fn()), { wrapper: Wrapper });
    await waitFor(() => {
      expect(result.current.status?.state).toBe('requested');
    });
    expect(result.current.restarting).toBe(false);

    await advance(UPDATE_FAST_POLL_MS);

    await waitFor(() => {
      expect(result.current.restarting).toBe(true);
    });
    // The last answer the daemon gave is kept: the page still knows what it was waiting for.
    expect(result.current.status?.state).toBe('requested');
  });

  it('reports applying as a restart even while the daemon still answers', async () => {
    stubDaemon([body({ state: 'applying' })]);
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(vi.fn()), { wrapper: Wrapper });

    await waitFor(() => {
      expect(result.current.restarting).toBe(true);
    });
  });

  it('does not read a failed poll of a pending build as a restart', async () => {
    stubDaemon([body({ state: 'pending' }), new TypeError('Failed to fetch')]);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { client, Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(vi.fn()), { wrapper: Wrapper });
    await waitFor(() => {
      expect(result.current.status?.state).toBe('pending');
    });

    await advance(UPDATE_POLL_MS);

    await waitFor(() => {
      expect(client.getQueryState(UPDATE_QUERY_KEY)?.status).toBe('error');
    });
    expect(result.current.restarting).toBe(false);
    expect(result.current.status?.state).toBe('pending');
  });

  it('reloads the page once a new build answers current, and only then', async () => {
    stubDaemon([
      body({ state: 'requested', running_rev: 'rev-old' }),
      body({ state: 'applying', running_rev: 'rev-old' }),
      new TypeError('Failed to fetch'),
      body({ state: 'current', running_rev: 'rev-new' }),
    ]);
    const reload = vi.fn();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(reload), { wrapper: Wrapper });
    await waitFor(() => {
      expect(result.current.status?.state).toBe('requested');
    });

    await advance(UPDATE_FAST_POLL_MS);
    await waitFor(() => {
      expect(result.current.status?.state).toBe('applying');
    });
    await advance(UPDATE_FAST_POLL_MS);
    await waitFor(() => {
      expect(result.current.restarting).toBe(true);
    });
    expect(reload).not.toHaveBeenCalled();

    await advance(UPDATE_FAST_POLL_MS);
    await waitFor(() => {
      expect(reload).toHaveBeenCalledTimes(1);
    });
    expect(result.current.status?.running_rev).toBe('rev-new');
  });

  it('never reloads while the build it started with keeps answering', async () => {
    stubDaemon([
      body({ state: 'current', running_rev: 'rev-old' }),
      body({ state: 'pending', running_rev: 'rev-old', available_rev: 'rev-new' }),
      body({ state: 'current', running_rev: 'rev-old' }),
    ]);
    const reload = vi.fn();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(reload), { wrapper: Wrapper });
    await waitFor(() => {
      expect(result.current.status?.state).toBe('current');
    });

    await advance(UPDATE_POLL_MS);
    await waitFor(() => {
      expect(result.current.status?.state).toBe('pending');
    });
    await advance(UPDATE_POLL_MS);
    await waitFor(() => {
      expect(result.current.status?.state).toBe('current');
    });

    expect(reload).not.toHaveBeenCalled();
  });

  it('does not reload for a new build that is not current yet', async () => {
    stubDaemon([
      body({ state: 'current', running_rev: 'rev-old' }),
      body({ state: 'failed', running_rev: 'rev-new' }),
    ]);
    const reload = vi.fn();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(reload), { wrapper: Wrapper });
    await waitFor(() => {
      expect(result.current.status?.state).toBe('current');
    });

    await advance(UPDATE_POLL_MS);
    await waitFor(() => {
      expect(result.current.status?.state).toBe('failed');
    });
    expect(reload).not.toHaveBeenCalled();
  });

  it('reloads through the browser location unless told otherwise', async () => {
    const reload = vi.fn();
    vi.stubGlobal('location', { reload });
    stubDaemon([
      body({ state: 'applying', running_rev: 'rev-old' }),
      body({ state: 'current', running_rev: 'rev-new' }),
    ]);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(), { wrapper: Wrapper });
    await waitFor(() => {
      expect(result.current.status?.state).toBe('applying');
    });

    await advance(UPDATE_FAST_POLL_MS);

    await waitFor(() => {
      expect(reload).toHaveBeenCalledTimes(1);
    });
  });

  it('reports nothing on an unmanaged stack and stops asking', async () => {
    const recorded = stubDaemon([{ managed: false, deferred_until: null, can_decide: false }]);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { client, Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(vi.fn()), { wrapper: Wrapper });
    await waitFor(() => {
      expect(client.getQueryState(UPDATE_QUERY_KEY)?.status).toBe('success');
    });

    await advance(UPDATE_POLL_MS * 3);

    expect(recorded.gets).toHaveLength(1);
    expect(result.current.status).toBeUndefined();
    expect(result.current.restarting).toBe(false);
  });

  it('exposes when the status was read as the clock the dialog measures against', async () => {
    stubDaemon([body({ state: 'pending' })]);
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date(Date.UTC(2026, 8, 24, 8, 0)));
    const { Wrapper } = setup();
    const { result } = renderHook(() => useSystemUpdate(vi.fn()), { wrapper: Wrapper });

    await waitFor(() => {
      expect(result.current.now).toBe(Date.UTC(2026, 8, 24, 8, 0));
    });
  });
});

describe('update decisions', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function json(status: number, payload: unknown): Response {
    return new Response(JSON.stringify(payload), { status });
  }

  it('marks the build requested as soon as the daemon accepts an apply', async () => {
    const recorded = stubDaemon([body({ state: 'pending', can_decide: true })], {
      '/api/system/update/apply': json(202, { state: 'requested' }),
    });
    const { client, Wrapper } = setup();
    const { result } = renderHook(
      () => ({ view: useSystemUpdate(vi.fn()), apply: useApplyUpdate() }),
      { wrapper: Wrapper },
    );
    await waitFor(() => {
      expect(result.current.view.status?.state).toBe('pending');
    });
    // Hold the refetch the apply triggers, so the patched answer is what the page shows.
    const invalidate = vi.spyOn(client, 'invalidateQueries').mockResolvedValue(undefined);

    await act(async () => {
      await result.current.apply.mutateAsync();
    });

    expect(recorded.posts).toEqual([{ url: '/api/system/update/apply', body: undefined }]);
    // React Query hands the patched answer to its observers on the next tick.
    await waitFor(() => {
      expect(result.current.view.status?.state).toBe('requested');
    });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: UPDATE_QUERY_KEY });
  });

  it('records the deferral the daemon kept, not the one that was asked for', async () => {
    const recorded = stubDaemon([body({ state: 'pending', can_decide: true })], {
      '/api/system/update/defer': json(202, { deferred_until: '2026-09-25T01:00:00Z' }),
    });
    const { client, Wrapper } = setup();
    const { result } = renderHook(
      () => ({ view: useSystemUpdate(vi.fn()), defer: useDeferUpdate() }),
      { wrapper: Wrapper },
    );
    await waitFor(() => {
      expect(result.current.view.status?.state).toBe('pending');
    });
    vi.spyOn(client, 'invalidateQueries').mockResolvedValue(undefined);

    await act(async () => {
      await result.current.defer.mutateAsync('2026-09-25T03:00:00Z');
    });

    expect(recorded.posts).toEqual([
      { url: '/api/system/update/defer', body: '{"until":"2026-09-25T03:00:00Z"}' },
    ]);
    await waitFor(() => {
      expect(result.current.view.status?.deferred_until).toBe('2026-09-25T01:00:00Z');
    });
  });

  it('leaves the status alone when the daemon refuses, and still refreshes it', async () => {
    stubDaemon([body({ state: 'pending', can_decide: true })], {
      '/api/system/update/apply': json(409, { error: 'no update waiting to be applied' }),
    });
    const { client, Wrapper } = setup();
    const { result } = renderHook(
      () => ({ view: useSystemUpdate(vi.fn()), apply: useApplyUpdate() }),
      { wrapper: Wrapper },
    );
    await waitFor(() => {
      expect(result.current.view.status?.state).toBe('pending');
    });
    const invalidate = vi.spyOn(client, 'invalidateQueries').mockResolvedValue(undefined);

    await act(async () => {
      await result.current.apply.mutateAsync().catch(() => undefined);
    });

    expect(result.current.view.status?.state).toBe('pending');
    expect(invalidate).toHaveBeenCalledWith({ queryKey: UPDATE_QUERY_KEY });
  });

  it('writes nothing into an empty cache', async () => {
    stubDaemon([new TypeError('Failed to fetch')], {
      '/api/system/update/apply': json(202, { state: 'requested' }),
    });
    const { client, Wrapper } = setup();
    const { result } = renderHook(() => useApplyUpdate(), { wrapper: Wrapper });
    vi.spyOn(client, 'invalidateQueries').mockResolvedValue(undefined);

    await act(async () => {
      await result.current.mutateAsync();
    });

    expect(client.getQueryData(UPDATE_QUERY_KEY)).toBeUndefined();
  });
});
