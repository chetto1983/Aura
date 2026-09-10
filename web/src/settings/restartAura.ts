import { httpErrorFrom } from '../api/json';

// The in-app restart (POST /api/admin/restart) exists for an appliance with no monitor and no
// SSH: the daemon answers 202, exits, and Docker brings it back. The page cannot see the
// restart itself, only /healthz, so this module decides from those answers when the NEW
// process is serving. It stays free of React so its timing rules run under fake timers.

const POLL_INTERVAL_MS = 1500;
// A restart can finish between two polls, so never seeing the server down does not mean it
// never restarted; but a 200 in the first seconds may still be the old process draining.
// Past this window, two consecutive 200s are trusted.
const SETTLE_MS = 6000;
const TIMEOUT_MS = 120_000;

export type RestartRequestOutcome = 'accepted' | 'unsupported';

export async function requestRestart(): Promise<RestartRequestOutcome> {
  const res = await fetch('/api/admin/restart', {
    method: 'POST',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  if (res.status === 409) return 'unsupported';
  if (!res.ok) throw await httpErrorFrom(res);
  return 'accepted';
}

export interface RestartWatchDeps {
  readonly fetch: (url: string, init: RequestInit) => Promise<Response>;
  readonly reload: () => void;
  readonly setTimeout: (callback: () => void, ms: number) => number;
  readonly clearTimeout: (handle: number) => void;
}

export const browserRestartDeps: RestartWatchDeps = {
  fetch: (url, init) => fetch(url, init),
  reload: () => {
    window.location.reload();
  },
  setTimeout: (callback, ms) => window.setTimeout(callback, ms),
  clearTimeout: (handle) => {
    window.clearTimeout(handle);
  },
};

interface HealthProbe {
  readonly sawDown: boolean;
  readonly settledUps: number;
  readonly back: boolean;
}

function observeHealth(probe: HealthProbe, up: boolean, elapsedMs: number): HealthProbe {
  if (!up) return { sawDown: true, settledUps: 0, back: false };
  if (probe.sawDown) return { ...probe, back: true };
  const settledUps = elapsedMs >= SETTLE_MS ? probe.settledUps + 1 : 0;
  return { sawDown: false, settledUps, back: settledUps >= 2 };
}

async function isUp(deps: RestartWatchDeps, signal: AbortSignal): Promise<boolean> {
  try {
    const res = await deps.fetch('/healthz', {
      cache: 'no-store',
      credentials: 'same-origin',
      signal,
    });
    return res.status === 200;
  } catch {
    // A refused connection is the restart in progress, not a failure of the watch.
    return false;
  }
}

/**
 * watchRestart polls /healthz until the restarted daemon is serving, then reloads the page;
 * after ~120s without that it gives up and calls onTimeout. The returned function cancels it.
 */
export function watchRestart(deps: RestartWatchDeps, onTimeout: () => void): () => void {
  const controller = new AbortController();
  let probe: HealthProbe = { sawDown: false, settledUps: 0, back: false };
  let polls = 0;
  let pollTimer: number | undefined;
  // Its own timer, so a health request that never answers cannot hold the deadline open.
  const deadline = deps.setTimeout(() => {
    stop();
    onTimeout();
  }, TIMEOUT_MS);

  function stop() {
    controller.abort();
    deps.clearTimeout(deadline);
    if (pollTimer !== undefined) deps.clearTimeout(pollTimer);
  }

  function schedule() {
    pollTimer = deps.setTimeout(() => void poll(), POLL_INTERVAL_MS);
  }

  async function poll() {
    polls += 1;
    const up = await isUp(deps, controller.signal);
    if (controller.signal.aborted) return;
    // Polls are chained, never overlapped, so this undercounts the real elapsed time: the
    // settle window can only close late, never early.
    probe = observeHealth(probe, up, polls * POLL_INTERVAL_MS);
    if (probe.back) {
      stop();
      deps.reload();
      return;
    }
    schedule();
  }

  schedule();
  return stop;
}
