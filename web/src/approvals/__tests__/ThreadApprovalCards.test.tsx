import { afterEach, describe, expect, it, vi } from 'vitest';
import { useState, type ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ThreadApprovalCards } from '../ThreadApprovalCards';
import type { Approval } from '../useApprovals';
import type { ApprovalResolution } from '../useThreadApprovals';

function approval(over: Partial<Approval> & Pick<Approval, 'token' | 'conversation_id'>): Approval {
  return { kind: 'clarification', question: 'Pick one', priority: 0, ...over };
}

interface ResolveCall {
  readonly url: string;
  readonly body: unknown;
}

// Resolve answers 200 + the ResolveDirective (Phase A); 'continue' is the in-session verdict
// these thread tests exercise.
function stubResolve(calls: ResolveCall[]) {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    calls.push({
      url,
      body: init?.body !== undefined ? JSON.parse(init.body as string) : undefined,
    });
    return Promise.resolve(directiveResponse('continue'));
  });
}

function directiveResponse(outcome: string, remaining = 0): Response {
  return new Response(JSON.stringify({ outcome, remaining }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function client() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function first<T>(rows: readonly T[]): T {
  const value = rows[0];
  if (value === undefined) throw new Error('expected at least one row');
  return value;
}

function renderCards(
  approvals: readonly Approval[],
  onResolved?: (resolution: unknown) => void | Promise<void>,
) {
  const qc = client();
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return render(
    <ThreadApprovalCards
      approvals={approvals}
      {...(onResolved !== undefined ? { onResolved } : {})}
    />,
    { wrapper: Wrapper },
  );
}

function StatefulCards({
  initial,
  onResolved,
}: {
  readonly initial: readonly Approval[];
  readonly onResolved: (resolution: unknown) => void;
}) {
  const [rows, setRows] = useState(initial);
  return (
    <ThreadApprovalCards
      approvals={rows}
      onResolved={(resolution) => {
        const token = (resolution as { approval: Approval }).approval.token;
        setRows((current) => current.filter((row) => row.token !== token));
        onResolved(resolution);
      }}
    />
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ThreadApprovalCards active-thread presentation', () => {
  it('preserves backend order and exposes focus targets without rendering tokens as text', () => {
    vi.stubGlobal('fetch', stubResolve([]));
    const rows = [
      approval({ token: 't-1', conversation_id: 'c-1', question: 'First' }),
      approval({ token: 't-2', conversation_id: 'c-1', question: 'Second' }),
    ];
    renderCards(rows);

    const cards = screen.getByTestId('thread-approvals');
    expect(cards.textContent?.indexOf('First')).toBeLessThan(
      cards.textContent?.indexOf('Second') ?? 0,
    );
    expect(
      cards.querySelectorAll('[data-approval-token]')[0]?.getAttribute('data-approval-token'),
    ).toBe('t-1');
    expect(screen.queryByText('t-1')).toBeNull();
    expect(screen.queryByText('t-2')).toBeNull();
  });

  it('keeps a keyed one-time announcement alive when the resolved row disappears', async () => {
    const calls: ResolveCall[] = [];
    vi.stubGlobal('fetch', stubResolve(calls));
    const onResolved = vi.fn();
    const qc = client();
    render(
      <QueryClientProvider client={qc}>
        <StatefulCards
          initial={[
            approval({
              token: 't-1',
              conversation_id: 'c-1',
              options: ['Yes'],
            }),
          ]}
          onResolved={onResolved}
        />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Yes' }));
    await waitFor(() => {
      expect(onResolved).toHaveBeenCalledTimes(1);
    });
    const resolution = onResolved.mock.calls[0]?.[0] as ApprovalResolution | undefined;
    expect(resolution?.approval.token).toBe('t-1');
    expect(resolution?.action).toBe('accept');
    expect(screen.queryByText('Pick one')).toBeNull();
    const status = screen.getByRole('status');
    expect(status.textContent).toBe('Answered.');
    expect(status.getAttribute('aria-live')).toBe('polite');
    expect(status.getAttribute('aria-atomic')).toBe('true');
    expect(calls[0]?.url).toContain('/api/approvals/t-1/resolve');
  });

  it('keeps the announcement region mounted when the approval list is empty', () => {
    vi.stubGlobal('fetch', stubResolve([]));
    renderCards([]);

    expect(screen.getByRole('status').textContent).toBe('');
  });

  it('forwards resolution-start lifecycle before the successful outcome', async () => {
    vi.stubGlobal('fetch', stubResolve([]));
    const order: string[] = [];
    const qc = client();
    render(
      <QueryClientProvider client={qc}>
        <ThreadApprovalCards
          approvals={[approval({ token: 't-1', conversation_id: 'c-1' })]}
          onResolutionStarted={() => {
            order.push('start');
          }}
          onResolved={() => {
            order.push('resolved');
          }}
        />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));

    await waitFor(() => {
      expect(order).toContain('resolved');
    });
    expect(order).toEqual(['start', 'resolved']);
  });

  it('keys repeated local outcomes so each intermediate answer gets a fresh announcement node', async () => {
    vi.stubGlobal('fetch', stubResolve([]));
    const onResolved = vi.fn();
    const qc = client();
    render(
      <QueryClientProvider client={qc}>
        <StatefulCards
          initial={[
            approval({
              token: 't-1',
              conversation_id: 'c-1',
              question: 'First',
              options: ['Yes'],
            }),
            approval({
              token: 't-2',
              conversation_id: 'c-1',
              question: 'Second',
              options: ['Yes'],
            }),
          ]}
          onResolved={onResolved}
        />
      </QueryClientProvider>,
    );

    fireEvent.click(first(screen.getAllByRole('button', { name: 'Yes' })));
    await waitFor(() => {
      expect(onResolved).toHaveBeenCalledTimes(1);
    });
    const firstAnnouncement = screen.getByRole('status');
    expect(firstAnnouncement.textContent).toBe('Answered.');

    fireEvent.click(screen.getByRole('button', { name: 'Yes' }));
    await waitFor(() => {
      expect(onResolved).toHaveBeenCalledTimes(2);
    });
    expect(screen.getByRole('status')).not.toBe(firstAnnouncement);
    expect(screen.getByRole('status').textContent).toBe('Answered.');
  });
});
