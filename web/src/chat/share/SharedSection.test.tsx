import { afterEach, describe, expect, it, vi } from 'vitest';
import { type ReactNode } from 'react';
import { fireEvent, render, renderHook, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { listShares, revokeShare } from './shareApi';
import type { ShareLink } from './shareTypes';
import { SharedSection } from './SharedSection';
import { selectActiveShares, useThreadShares } from './useThreadShares';

// SharedSection.test.tsx — covers every <behavior> row from 37F-17-PLAN.md Task 1: the pure
// selectActiveShares projection (no render, the reason it is exported — mirrors
// useThreadArtifacts.test.ts's own split), then the wired SharedSection component covering the
// empty state, row content (tier/created/relative expiry with the absolute date on `title`),
// the expired-but-not-swept inert row, the internal/public copy-link asymmetry, and the
// confirm-gated revoke flow.

vi.mock('./shareApi', () => ({
  listShares: vi.fn(),
  revokeShare: vi.fn(),
}));

const mockList = vi.mocked(listShares);
const mockRevoke = vi.mocked(revokeShare);

function link(over: Partial<ShareLink>): ShareLink {
  return {
    id: 'share-1',
    tier: 'internal',
    created_at: '2026-07-01T00:00:00Z',
    updated_at: '2026-07-01T00:00:00Z',
    ...over,
  };
}

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

function renderSection(threadId = 't-1') {
  return render(<SharedSection threadId={threadId} />, { wrapper: wrapper() });
}

describe('selectActiveShares (pure projection)', () => {
  it('drops revoked links and sorts newest-first', () => {
    const links = [
      link({ id: 'a', created_at: '2026-01-01T00:00:00Z' }),
      link({
        id: 'revoked',
        created_at: '2026-01-05T00:00:00Z',
        revoked_at: '2026-01-06T00:00:00Z',
      }),
      link({ id: 'b', created_at: '2026-01-03T00:00:00Z' }),
    ];
    expect(selectActiveShares(links).map((l) => l.id)).toEqual(['b', 'a']);
  });

  it('keeps an expired link — expiry is a display concern, not a filter', () => {
    const links = [link({ id: 'expired', expires_at: '2020-01-01T00:00:00Z' })];
    expect(selectActiveShares(links).map((l) => l.id)).toEqual(['expired']);
  });

  it('does not mutate the input array order (sorts a copy)', () => {
    const input = [
      link({ id: 'a', created_at: '2026-01-01T00:00:00Z' }),
      link({ id: 'b', created_at: '2026-01-05T00:00:00Z' }),
    ];
    const original = input.map((l) => l.id);
    selectActiveShares(input);
    expect(input.map((l) => l.id)).toEqual(original);
  });
});

describe('useThreadShares (wired query)', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('is disabled (idle, no fetch) for an empty threadId', () => {
    const { result } = renderHook(() => useThreadShares(''), { wrapper: wrapper() });
    expect(result.current.fetchStatus).toBe('idle');
    expect(mockList).not.toHaveBeenCalled();
  });
});

describe('SharedSection', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows the empty state when the thread has no shares', async () => {
    mockList.mockResolvedValue([]);
    renderSection();
    await waitFor(() => {
      expect(screen.getByText(/has not been shared/i)).toBeTruthy();
    });
  });

  it('renders rows newest-first with tier, created date, and relative expiry (absolute date on title)', async () => {
    mockList.mockResolvedValue([
      link({ id: 'old', tier: 'internal', created_at: '2026-01-01T00:00:00Z' }),
      link({
        id: 'new',
        tier: 'public',
        created_at: '2026-01-05T00:00:00Z',
        expires_at: '2099-01-01T00:00:00Z',
      }),
    ]);
    const { container } = renderSection();
    await waitFor(() => {
      expect(container.querySelectorAll('li')).toHaveLength(2);
    });
    const items = [...container.querySelectorAll('li')];
    expect(items[0]?.textContent).toContain('Public');
    expect(items[1]?.textContent).toContain('Internal');
    const expiryEl = within(items[0] as HTMLElement).getByTitle(/2099/);
    expect(expiryEl.textContent).toMatch(/expires in/i);
  });

  it('renders an expired-but-not-swept link as expired and visually inert (no copy)', async () => {
    mockList.mockResolvedValue([
      link({ id: 'x', tier: 'public', expires_at: '2020-01-01T00:00:00Z' }),
    ]);
    renderSection();
    await waitFor(() => {
      expect(screen.getByText('Expired')).toBeTruthy();
    });
    const row = document.querySelector('[data-expired="true"]');
    expect(row).toBeTruthy();
    expect(within(row as HTMLElement).queryByRole('button', { name: /copy/i })).toBeNull();
  });

  it('offers a copy-link on an internal row, resolving /shared/{id}', async () => {
    mockList.mockResolvedValue([link({ id: 'abc123', tier: 'internal' })]);
    const mockWriteText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: mockWriteText },
    });
    renderSection();
    const copyButton = await screen.findByRole('button', { name: /copy/i });
    fireEvent.click(copyButton);
    await waitFor(() => {
      expect(mockWriteText).toHaveBeenCalledWith(expect.stringContaining('/shared/abc123'));
    });
  });

  it('offers no copy-link on a public row (listShares never returns its token)', async () => {
    mockList.mockResolvedValue([link({ id: 'pub-1', tier: 'public' })]);
    renderSection();
    await waitFor(() => {
      expect(screen.getByText('Public')).toBeTruthy();
    });
    expect(screen.queryByRole('button', { name: /copy/i })).toBeNull();
  });

  it('confirms per-row revoke: revokeShare does not fire until the confirm is accepted', async () => {
    mockRevoke.mockResolvedValue(undefined);
    mockList.mockResolvedValue([link({ id: 'share-9' })]);
    renderSection();
    const revokeButton = await screen.findByRole('button', { name: 'Revoke' });
    fireEvent.click(revokeButton);
    expect(mockRevoke).not.toHaveBeenCalled();

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Revoke' }));

    await waitFor(() => {
      expect(mockRevoke).toHaveBeenCalledWith('share-9');
    });
  });

  it('cancelling the confirm dialog never calls revokeShare', async () => {
    mockList.mockResolvedValue([link({ id: 'share-cancel' })]);
    renderSection();
    const revokeButton = await screen.findByRole('button', { name: 'Revoke' });
    fireEvent.click(revokeButton);

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(mockRevoke).not.toHaveBeenCalled();
  });
});
