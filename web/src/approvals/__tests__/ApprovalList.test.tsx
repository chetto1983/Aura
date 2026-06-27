import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import '../../i18n/i18n';
import { ApprovalBadge } from '../ApprovalBadge';
import { ApprovalList } from '../ApprovalList';
import type { Approval } from '../useApprovals';

// Capture navigate() so the /c/:id jump (D-04) is asserted exactly.
const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

function approval(over: Partial<Approval> & Pick<Approval, 'token' | 'conversation_id'>): Approval {
  return {
    kind: 'clarification',
    question: 'Pick a city',
    priority: 0,
    ...over,
  };
}

const PENDING: Approval[] = [
  approval({ token: 't-1', conversation_id: 'c-1', question: 'Which city?' }),
  approval({
    token: 't-2',
    conversation_id: 'c-2',
    question: 'Approve the deploy?',
    kind: 'approval',
  }),
];

const EXPIRED: Approval = approval({
  token: 't-3',
  conversation_id: 'c-3',
  question: 'Stale interrupt',
  terminal: true,
});

function stubFetch(body: Approval[]) {
  return vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status: 200 })));
}

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children, qc }: { children: ReactNode; qc: QueryClient }) {
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe('ApprovalBadge (APRV-01 / D-04)', () => {
  beforeEach(() => {
    navigateMock.mockReset();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders the pending count as an accent pill with the count aria-label', async () => {
    vi.stubGlobal('fetch', stubFetch(PENDING));
    const qc = client();
    render(<ApprovalBadge expanded={false} onToggle={() => undefined} />, {
      wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper>,
    });
    const badge = await screen.findByRole('button', { name: '2 approvals waiting' });
    expect(badge.textContent).toContain('2');
    expect(badge.getAttribute('data-slot')).toBe('button');
  });

  it('announces a count change via an aria-live="polite" region (not assertive)', async () => {
    vi.stubGlobal('fetch', stubFetch(PENDING));
    const qc = client();
    const { container } = render(<ApprovalBadge expanded={false} onToggle={() => undefined} />, {
      wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper>,
    });
    await screen.findByRole('button', { name: '2 approvals waiting' });
    const live = container.querySelector('[aria-live]');
    expect(live).not.toBeNull();
    expect(live?.getAttribute('aria-live')).toBe('polite');
    // The polite region carries the new total after the count changed from 0 → 2.
    await waitFor(() => {
      expect(live?.textContent).toContain('2 approvals waiting');
    });
  });

  it('renders NO pill when nothing is pending (never a stale count)', async () => {
    vi.stubGlobal('fetch', stubFetch([]));
    const qc = client();
    render(<ApprovalBadge expanded={false} onToggle={() => undefined} />, {
      wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper>,
    });
    // Give the query a tick to settle, then assert no badge button exists.
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: /approval/i })).toBeNull();
    });
  });

  it('toggles the list via onToggle', async () => {
    vi.stubGlobal('fetch', stubFetch(PENDING));
    const onToggle = vi.fn();
    const qc = client();
    render(<ApprovalBadge expanded={false} onToggle={onToggle} />, {
      wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper>,
    });
    fireEvent.click(await screen.findByRole('button', { name: '2 approvals waiting' }));
    expect(onToggle).toHaveBeenCalled();
  });
});

describe('ApprovalList (APRV-01 / D-04/D-06)', () => {
  beforeEach(() => {
    navigateMock.mockReset();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders a cross-thread row per pending interrupt (title · question)', async () => {
    vi.stubGlobal('fetch', stubFetch(PENDING));
    const qc = client();
    render(<ApprovalList titleFor={(id) => (id === 'c-1' ? 'Weather run' : undefined)} />, {
      wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper>,
    });
    await waitFor(() => {
      expect(screen.getByText('Which city?')).toBeTruthy();
    });
    expect(screen.getByText('Which city?').closest('[data-slot="card"]')).not.toBeNull();
    expect(screen.getByText('Weather run')).toBeTruthy();
    expect(screen.getByText('Approve the deploy?')).toBeTruthy();
  });

  it('Open navigates to /c/:conversationId and calls onOpen (the D-04 jump)', async () => {
    vi.stubGlobal('fetch', stubFetch(PENDING));
    const onOpen = vi.fn();
    const qc = client();
    render(<ApprovalList onOpen={onOpen} />, {
      wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper>,
    });
    await screen.findByText('Which city?');
    // Scope to the row whose question is "Which city?" (conversation c-1) so the
    // assertion is exact regardless of row order.
    const row = screen.getByText('Which city?').closest('li') as HTMLElement;
    fireEvent.click(within(row).getByRole('button', { name: 'Open' }));
    expect(navigateMock).toHaveBeenCalledWith('/c/c-1');
    expect(onOpen).toHaveBeenCalledWith('c-1');
  });

  it('renders an expired/auto-terminated row terminal state with warning (D-06, never omitted)', async () => {
    vi.stubGlobal('fetch', stubFetch([EXPIRED]));
    const qc = client();
    const { container } = render(<ApprovalList />, {
      wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper>,
    });
    await waitFor(() => {
      expect(screen.getByText('Stale interrupt')).toBeTruthy();
    });
    // The terminal copy renders (never silently dropped) with the warning tone.
    const terminal = screen.getByText('Expired — auto-resolved.');
    expect(terminal).toBeTruthy();
    expect(terminal.className).toContain('text-warning');
    expect(container.querySelector('.bg-warning')).not.toBeNull();
  });

  it('shows the empty state when nothing is pending', async () => {
    vi.stubGlobal('fetch', stubFetch([]));
    const qc = client();
    render(<ApprovalList />, {
      wrapper: ({ children }) => <Wrapper qc={qc}>{children}</Wrapper>,
    });
    await waitFor(() => {
      expect(screen.getByText('Nothing waiting on you.')).toBeTruthy();
    });
  });
});
