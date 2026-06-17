import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ThreadApprovalCards } from '../ThreadApprovalCards';
import type { Approval } from '../useApprovals';

function approval(over: Partial<Approval> & Pick<Approval, 'token' | 'conversation_id'>): Approval {
  return { kind: 'clarification', question: 'Pick one', priority: 0, ...over };
}

interface ResolveCall {
  readonly url: string;
  readonly body: unknown;
}

// fetch double: the approvals poll returns the given rows; /resolve → 204; the
// /agent/run re-drive (continue-after-resume) is recorded so the redrive is asserted.
function stubFetch(rows: Approval[], calls: ResolveCall[]) {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    if (url.includes('/api/approvals') && (init?.method ?? 'GET') === 'GET') {
      return Promise.resolve(new Response(JSON.stringify(rows), { status: 200 }));
    }
    calls.push({
      url,
      body: init?.body !== undefined ? JSON.parse(init.body as string) : undefined,
    });
    return Promise.resolve(new Response(null, { status: 204 }));
  });
}

function renderCards(props: { conversationId: string; onResolved?: (id: string) => void }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  return render(
    <ThreadApprovalCards
      conversationId={props.conversationId}
      {...(props.onResolved !== undefined ? { onResolved: props.onResolved } : {})}
    />,
    { wrapper: Wrapper },
  );
}

describe('ThreadApprovalCards (D-03 inline mount)', () => {
  let calls: ResolveCall[];
  beforeEach(() => {
    calls = [];
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders nothing for an empty conversation id', () => {
    vi.stubGlobal('fetch', stubFetch([], calls));
    const { container } = renderCards({ conversationId: '' });
    expect(container.textContent).toBe('');
  });

  it("renders only the active thread's pending interrupts (filters cross-thread)", async () => {
    vi.stubGlobal(
      'fetch',
      stubFetch(
        [
          approval({ token: 't-1', conversation_id: 'c-1', question: 'For c-1' }),
          approval({ token: 't-2', conversation_id: 'c-2', question: 'For c-2' }),
        ],
        calls,
      ),
    );
    renderCards({ conversationId: 'c-1' });
    await waitFor(() => {
      expect(screen.getByText('For c-1')).toBeTruthy();
    });
    // The other thread's interrupt does NOT render in this lane (it stays in the badge).
    expect(screen.queryByText('For c-2')).toBeNull();
  });

  it('renders nothing when the active thread has no pending interrupt', async () => {
    vi.stubGlobal('fetch', stubFetch([approval({ token: 't-2', conversation_id: 'c-2' })], calls));
    const { container } = renderCards({ conversationId: 'c-1' });
    // Let the poll settle; c-1 has nothing pending → no card.
    await waitFor(() => {
      expect(container.querySelector('p')).toBeNull();
    });
  });

  it('answering an inline card re-drives the run via onResolved (continue-after-resume)', async () => {
    vi.stubGlobal(
      'fetch',
      stubFetch(
        [approval({ token: 't-1', conversation_id: 'c-1', options: ['Yes', 'No'] })],
        calls,
      ),
    );
    const onResolved = vi.fn();
    renderCards({ conversationId: 'c-1', onResolved });
    fireEvent.click(await screen.findByRole('button', { name: 'Yes' }));
    await waitFor(() => {
      expect(onResolved).toHaveBeenCalledWith('c-1');
    });
    expect(calls.some((c) => c.url.includes('/api/approvals/t-1/resolve'))).toBe(true);
  });
});
