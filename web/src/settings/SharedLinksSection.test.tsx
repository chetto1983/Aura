import { afterEach, describe, expect, it, vi } from 'vitest';
import { type ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../i18n/i18n';
import { listShares, revokeShare } from '../chat/share/shareApi';
import type { ShareLink } from '../chat/share/shareTypes';
import { SharedLinksSection } from './SharedLinksSection';

// SharedLinksSection.test.tsx — covers every <behavior> row from 37F-17-PLAN.md Task 2: the
// global list (newest-first, non-revoked only, reusing Task 1's row + select — the same
// tier/created/expiry shape SharedSection.test.tsx already proves at the unit level), the
// expired-inert row, both confirm-gated revoke paths (per-row and bulk), the aria-live revoke
// announcement, and the role="alert" load-error surface.

vi.mock('../chat/share/shareApi', () => ({
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

function renderSection() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }
  return render(<SharedLinksSection />, { wrapper: Wrapper });
}

describe('SharedLinksSection', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('lists with no thread filter (the global surface)', async () => {
    mockList.mockResolvedValue([]);
    renderSection();
    await waitFor(() => {
      expect(mockList).toHaveBeenCalledWith(undefined, expect.anything());
    });
  });

  it('shows the empty state when the owner has no shared links', async () => {
    mockList.mockResolvedValue([]);
    renderSection();
    await waitFor(() => {
      expect(screen.getByText(/no shared conversations/i)).toBeTruthy();
    });
  });

  it('lists non-revoked links newest-first with tier, created, and relative expiry', async () => {
    mockList.mockResolvedValue([
      link({ id: 'old', tier: 'internal', created_at: '2026-01-01T00:00:00Z' }),
      link({
        id: 'new',
        tier: 'public',
        created_at: '2026-01-05T00:00:00Z',
        expires_at: '2099-01-01T00:00:00Z',
      }),
      link({
        id: 'gone',
        created_at: '2026-01-09T00:00:00Z',
        revoked_at: '2026-01-10T00:00:00Z',
      }),
    ]);
    const { container } = renderSection();
    await waitFor(() => {
      expect(container.querySelectorAll('li')).toHaveLength(2);
    });
    const items = [...container.querySelectorAll('li')];
    expect(items[0]?.textContent).toContain('Public');
    expect(items[1]?.textContent).toContain('Internal');
  });

  it('renders an expired row as expired and inert', async () => {
    mockList.mockResolvedValue([
      link({ id: 'x', tier: 'public', expires_at: '2020-01-01T00:00:00Z' }),
    ]);
    renderSection();
    await waitFor(() => {
      expect(screen.getByText('Expired')).toBeTruthy();
    });
    expect(document.querySelector('[data-expired="true"]')).toBeTruthy();
  });

  it('confirms per-row revoke, then removes the row from the list', async () => {
    mockRevoke.mockResolvedValue(undefined);
    mockList.mockResolvedValueOnce([link({ id: 'r-1' })]).mockResolvedValueOnce([]);
    renderSection();
    const revokeButton = await screen.findByRole('button', { name: 'Revoke' });
    fireEvent.click(revokeButton);
    expect(mockRevoke).not.toHaveBeenCalled();

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Revoke' }));

    await waitFor(() => {
      expect(mockRevoke).toHaveBeenCalledWith('r-1');
    });
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Revoke' })).toBeNull();
    });
  });

  it('confirms revoke-all before clearing the list, and announces via aria-live', async () => {
    mockRevoke.mockResolvedValue(undefined);
    mockList
      .mockResolvedValueOnce([link({ id: 'a' }), link({ id: 'b', tier: 'public' })])
      .mockResolvedValueOnce([]);
    renderSection();
    const revokeAllButton = await screen.findByRole('button', { name: 'Revoke all' });
    fireEvent.click(revokeAllButton);
    expect(mockRevoke).not.toHaveBeenCalled();

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Revoke all' }));

    await waitFor(() => {
      expect(mockRevoke).toHaveBeenCalledWith('a');
      expect(mockRevoke).toHaveBeenCalledWith('b');
    });
    await waitFor(() => {
      expect(screen.getByRole('status').textContent).toBe('Link revoked');
    });
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Revoke all' })).toBeNull();
    });
  });

  it('renders a load error in role="alert" with a working retry', async () => {
    mockList.mockRejectedValueOnce(new Error('boom')).mockResolvedValueOnce([]);
    renderSection();
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toMatch(/couldn.t load/i);

    fireEvent.click(within(alert).getByRole('button', { name: /retry/i }));
    await waitFor(() => {
      expect(screen.queryByRole('alert')).toBeNull();
    });
    expect(mockList).toHaveBeenCalledTimes(2);
  });

  it('cancelling per-row revoke never calls revokeShare', async () => {
    mockList.mockResolvedValue([link({ id: 'r-cancel' })]);
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
