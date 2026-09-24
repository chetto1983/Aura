import { vi } from 'vitest';
import { render } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SystemUpdateLayer } from '../SystemUpdateLayer';
import { SystemUpdateProvider } from '../SystemUpdateProvider';
import { UpdateIndicator } from '../UpdateIndicator';

// A frozen browser clock (Thursday 24 September 2026, 10:00 local time) and a fake daemon
// that remembers the decisions it is sent, the way the real one reports them straight away.

export const browserNow = new Date(2026, 8, 24, 10, 0, 0);
export const AVAILABLE_REV = 'abc1234def5678';

/** An RFC 3339 instant for a local wall-clock time in September 2026. */
export function at(day: number, hour: number, minute = 0): string {
  return new Date(2026, 8, day, hour, minute).toISOString();
}

export function adminPending(fields: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    managed: true,
    state: 'pending',
    running_rev: '7886200e5c1a2b',
    available_rev: AVAILABLE_REV,
    available_built_at: at(24, 7, 12),
    pending_since: at(24, 7),
    deadline: at(25, 7),
    deferred_until: null,
    checked_at: at(24, 9, 59),
    can_decide: true,
    error: '',
    last_activity_at: at(24, 9, 57),
    live_runs: 0,
    ...fields,
  };
}

export function member(state: string): Record<string, unknown> {
  return {
    managed: true,
    state,
    running_rev: '7886200e5c1a2b',
    available_rev: AVAILABLE_REV,
    deferred_until: null,
    can_decide: false,
  };
}

export function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status });
}

export interface FakeDaemon {
  readonly posts: { url: string; body: unknown }[];
}

interface Daemon {
  current: Record<string, unknown>;
}

type Answer = (url: string, body: unknown, daemon: Daemon) => Response | Error | undefined;

function decide(daemon: Daemon, url: string, body: unknown): Response {
  if (url.endsWith('/apply')) {
    daemon.current = { ...daemon.current, state: 'requested' };
    return json(202, { state: 'requested' });
  }
  const until = (body as { until: string }).until;
  daemon.current = { ...daemon.current, deferred_until: until };
  return json(202, { deferred_until: until });
}

/** `answer` overrides the daemon's reply to a POST (an Error is a refused connection) and may
 * change what it reports next; returning undefined keeps the default. The cockpit always
 * fetches by string URL. */
export function stubDaemon(initial: Record<string, unknown>, answer?: Answer): FakeDaemon {
  const daemon: Daemon = { current: initial };
  const posts: FakeDaemon['posts'] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method !== 'POST') return Promise.resolve(json(200, daemon.current));
      const body: unknown = typeof init.body === 'string' ? JSON.parse(init.body) : undefined;
      posts.push({ url, body });
      const reply = answer?.(url, body, daemon) ?? decide(daemon, url, body);
      return reply instanceof Error ? Promise.reject(reply) : Promise.resolve(reply);
    }),
  );
  return { posts };
}

export function renderCenter(suppressed = false) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <SystemUpdateProvider>
        <header>
          <UpdateIndicator />
        </header>
        <SystemUpdateLayer suppressed={suppressed} />
      </SystemUpdateProvider>
    </QueryClientProvider>,
  );
}

/** The browser's own short time for a local instant ("3:00 AM" in English). */
export function clock(date: Date): string {
  return new Intl.DateTimeFormat('en', { timeStyle: 'short' }).format(date);
}
