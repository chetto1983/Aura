import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';

// CalendarConnect: what the calendar MCP server's detail in Governance says about accounts. The
// accounts themselves are connected in Settings → «Posta e calendario», so the section links there;
// an administrator still finds the provider OAuth apps here. Nothing in it lists or creates an
// account, so no IMAP form survives in Governance.

const listPimAccounts = vi.fn();
const listPimProviderApps = vi.fn();
let caps = { isAdmin: false };

vi.mock('../../admin/useAdmin', () => ({ useCapabilities: () => caps }));

vi.mock('../pimApi', async () => {
  const actual = await vi.importActual<typeof import('../pimApi')>('../pimApi');
  return {
    ...actual,
    listPimAccounts: (...a: unknown[]) => listPimAccounts(...a) as Promise<unknown>,
    listPimProviderApps: (...a: unknown[]) => listPimProviderApps(...a) as Promise<unknown>,
  };
});

const { CalendarConnect } = await import('../CalendarConnect');
const { isCalendarServer } = await import('../pimApi');

function renderConnect() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <CalendarConnect />
    </QueryClientProvider>,
  );
}

describe('isCalendarServer', () => {
  it('matches by name, recipe source, and pim source substring; rejects others', () => {
    expect(isCalendarServer({ name: 'calendar', source: '' })).toBe(true);
    expect(isCalendarServer({ name: 'cal', source: 'recipe:calendar' })).toBe(true);
    expect(isCalendarServer({ name: 'cal', source: 'ghcr.io/.../aura-pim-mcp' })).toBe(true);
    expect(isCalendarServer({ name: 'cal', source: 'something-pim-thing' })).toBe(true);
    expect(isCalendarServer({ name: 'whatsapp', source: 'recipe:whatsapp' })).toBe(false);
    expect(isCalendarServer({ name: 'github', source: 'manual' })).toBe(false);
  });
});

describe('CalendarConnect', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    caps = { isAdmin: false };
    listPimAccounts.mockResolvedValue({ accounts: [] });
    listPimProviderApps.mockResolvedValue([]);
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('GovernanceLinksToTheSection', () => {
    renderConnect();

    const link = screen.getByRole('link', {
      name: 'Accounts are connected in Settings → Mail and calendar',
    });
    expect(link.getAttribute('href')).toBe('/?settings=mail');
    expect(screen.queryByRole('button', { name: 'Create account' })).toBeNull();
    expect(screen.queryByRole('combobox', { name: 'Provider' })).toBeNull();
    expect(screen.queryByLabelText('Email address')).toBeNull();
    expect(listPimAccounts).not.toHaveBeenCalled();
  });

  it('GovernanceStillShowsProviderAppsToAnAdmin', async () => {
    caps = { isAdmin: true };
    renderConnect();
    expect(await screen.findByText(/provider oauth apps/i)).toBeTruthy();
    expect(screen.getByRole('link', { name: /Settings → Mail and calendar/ })).toBeTruthy();
  });

  it('hides the provider-app panel from members', () => {
    renderConnect();
    expect(screen.queryByText(/provider oauth apps/i)).toBeNull();
  });
});
