import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { requestRestart, watchRestart, type RestartWatchDeps } from '../restartAura';

// The in-app restart's timing rules, proven with fake timers. The browser cannot see the
// restart itself, only /healthz: down-then-up is the new process; a restart faster than the
// first poll never shows "down", so a steady 200 counts only once the old process has had
// ~6s to go away; and a box that never answers again must end in an error, not a spinner.

type HealthAnswer = number | 'offline';

function healthSequence(answers: readonly HealthAnswer[]) {
  let index = 0;
  return vi.fn<RestartWatchDeps['fetch']>(() => {
    const answer = answers[Math.min(index, answers.length - 1)] ?? 'offline';
    index += 1;
    return answer === 'offline'
      ? Promise.reject(new TypeError('Failed to fetch'))
      : Promise.resolve(new Response('{}', { status: answer }));
  });
}

function deps(fetchImpl: RestartWatchDeps['fetch'], reload: () => void): RestartWatchDeps {
  return {
    fetch: fetchImpl,
    reload,
    setTimeout: (callback, ms) => window.setTimeout(callback, ms),
    clearTimeout: (handle) => {
      window.clearTimeout(handle);
    },
  };
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('requestRestart', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('asks the daemon to restart and reads 202 as accepted', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(jsonResponse(202, { restarting: true })));
    vi.stubGlobal('fetch', fetchMock);

    await expect(requestRestart()).resolves.toBe('accepted');
    expect(fetchMock).toHaveBeenCalledWith('/api/admin/restart', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('reads 409 as an installation that cannot restart itself', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(409, { error: 'restart_unsupported' }))),
    );

    await expect(requestRestart()).resolves.toBe('unsupported');
  });

  it('throws the server reason for any other refusal', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(403, { error: 'forbidden' }))),
    );

    await expect(requestRestart()).rejects.toThrow('HTTP 403: forbidden');
  });
});

describe('watchRestart', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('reloads as soon as the server has gone down and come back', async () => {
    // The 200 at 1.5s is the old process still draining: on its own it proves nothing.
    const fetchMock = healthSequence([200, 'offline', 200]);
    const reload = vi.fn();
    const onTimeout = vi.fn();
    watchRestart(deps(fetchMock, reload), onTimeout);

    await vi.advanceTimersByTimeAsync(3000);
    expect(reload).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1500);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock).toHaveBeenCalledWith(
      '/healthz',
      expect.objectContaining({ cache: 'no-store' }),
    );

    await vi.advanceTimersByTimeAsync(130_000);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(onTimeout).not.toHaveBeenCalled();
  });

  it.each([
    ['a network error', 'offline' as const],
    ['a 502 from the proxy', 502],
    ['a 503 from a process still starting', 503],
  ])('counts %s as down', async (_label, down) => {
    const reload = vi.fn();
    watchRestart(deps(healthSequence([down, 200]), reload), vi.fn());

    await vi.advanceTimersByTimeAsync(1500);
    expect(reload).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1500);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('reloads on two consecutive 200s after ~6s when the restart beat the first poll', async () => {
    const reload = vi.fn();
    watchRestart(deps(healthSequence([200]), reload), vi.fn());

    // Polls at 1.5s, 3s, 4.5s and 6s all answer 200, but only the 6s one is past the window.
    await vi.advanceTimersByTimeAsync(6000);
    expect(reload).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1500);
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('gives up after ~120s when the server never comes back', async () => {
    const fetchMock = healthSequence(['offline']);
    const reload = vi.fn();
    const onTimeout = vi.fn();
    watchRestart(deps(fetchMock, reload), onTimeout);

    await vi.advanceTimersByTimeAsync(119_999);
    expect(onTimeout).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(1);
    expect(onTimeout).toHaveBeenCalledTimes(1);
    expect(reload).not.toHaveBeenCalled();

    const polls = fetchMock.mock.calls.length;
    await vi.advanceTimersByTimeAsync(10_000);
    expect(fetchMock.mock.calls.length).toBe(polls);
  });

  it('a health request that never answers cannot hold the deadline open', async () => {
    const fetchMock = vi.fn<RestartWatchDeps['fetch']>(
      () => new Promise<Response>(() => undefined),
    );
    const onTimeout = vi.fn();
    watchRestart(deps(fetchMock, vi.fn()), onTimeout);

    await vi.advanceTimersByTimeAsync(120_000);
    expect(onTimeout).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0]?.[1].signal?.aborted).toBe(true);
  });

  it('stops polling, and never times out, once the watch is cancelled', async () => {
    const fetchMock = healthSequence(['offline']);
    const onTimeout = vi.fn();
    const stop = watchRestart(deps(fetchMock, vi.fn()), onTimeout);

    await vi.advanceTimersByTimeAsync(1500);
    stop();
    await vi.advanceTimersByTimeAsync(130_000);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(onTimeout).not.toHaveBeenCalled();
  });
});
