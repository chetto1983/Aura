import type { ReactElement, ReactNode } from 'react';
import { act, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { vi } from 'vitest';
import type {
  MailWatchAccount,
  MailWatchDecision,
  MailWatchLabel,
  MailWatchRule,
  MailWatchStatus,
} from '../governance/mailWatchApi';

// A fake of /api/mail-watch at the fetch boundary, so a component test runs through the real API
// module, hooks and error handling. A route answers from `on`, or from the defaults below.

export interface MailWatchCall {
  readonly method: string;
  readonly path: string;
  readonly query: string;
  readonly body: unknown;
}

interface Answer {
  readonly status?: number;
  readonly body: unknown;
}

type Handler = (call: MailWatchCall) => Answer | Promise<Answer>;

export function decision(fields: Partial<MailWatchDecision> = {}): MailWatchDecision {
  return {
    account_id: 'work',
    resource: 'mail',
    item_id: 'm1',
    from: 'ada@example.com',
    from_name: 'Ada Lovelace',
    subject: 'Quarterly numbers',
    start: null,
    gone: false,
    auth_failed: false,
    tier: 'today',
    decided_by: 'default',
    decided_at: '2026-11-01T09:12:00Z',
    ...fields,
  };
}

export function rule(fields: Partial<MailWatchRule> = {}): MailWatchRule {
  return {
    id: 'r1',
    key: 'ada@example.com',
    key_kind: 'sender',
    tier: 'later',
    source: 'person',
    created_at: '2026-10-20T08:00:00Z',
    ...fields,
  };
}

/** A rule that gives a label and no tier. */
export function labelRule(labelId: string, fields: Partial<MailWatchRule> = {}): MailWatchRule {
  const { tier: _tier, ...base } = rule(fields);
  return { ...base, label_id: labelId };
}

export function label(fields: Partial<MailWatchLabel> = {}): MailWatchLabel {
  return { id: 'l1', name: 'Client', position: 0, ...fields };
}

export function account(fields: Partial<MailWatchAccount> = {}): MailWatchAccount {
  return {
    account_id: 'work',
    paused: false,
    next_run_at: '2026-11-01T14:35:00Z',
    alerts_today: 2,
    resources: [],
    ...fields,
  };
}

export function watchStatus(fields: Partial<MailWatchStatus> = {}): MailWatchStatus {
  return {
    configured: true,
    settings: {
      digest_hour: 18,
      daily_alert_cap: 5,
      interval_minutes: 5,
      last_digest_at: null,
    },
    accounts: [account()],
    ...fields,
  };
}

/** Never answers: the pending state. */
export function hang(): Promise<Answer> {
  return new Promise(() => undefined);
}

export function mailWatchServer() {
  const calls: MailWatchCall[] = [];
  const routes = new Map<string, Handler>([
    ['GET /api/mail-watch', () => ({ body: watchStatus() })],
    ['GET /api/mail-watch/rules', () => ({ body: [] })],
    ['GET /api/mail-watch/labels', () => ({ body: [] })],
  ]);

  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : String(input), 'http://cockpit');
    const raw = init?.body;
    const call: MailWatchCall = {
      method: init?.method ?? 'GET',
      path: url.pathname,
      query: url.search,
      body: typeof raw === 'string' ? (JSON.parse(raw) as unknown) : undefined,
    };
    calls.push(call);
    const handler = routes.get(`${call.method} ${call.path}`);
    const answer = (await handler?.(call)) ?? { status: 404, body: { error: 'not_found' } };
    return new Response(JSON.stringify(answer.body), {
      status: answer.status ?? 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fetchMock);

  return {
    calls,
    /** Answers `method path` from now on. */
    on(route: string, handler: Handler) {
      routes.set(route, handler);
    },
    /** The calls with this method to this path. */
    writes(method: string, path: string) {
      return calls.filter((call) => call.method === method && call.path === path);
    },
  };
}

/** A mutation reaches fetch a few microtasks after the event, so "nothing was sent" read straight
 * after a click passes whatever the code did. This waits for a write that was on its way. */
export async function settled() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

/** Queries scoped to the list row that holds this text. */
export function rowHolding(text: string) {
  const row = screen.getByText(text).closest('li');
  if (row === null) throw new Error(`no row holds ${text}`);
  return within(row);
}

export function renderWatch(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { readonly children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  return { client, ...render(ui, { wrapper: Wrapper }) };
}
