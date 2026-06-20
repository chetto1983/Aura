import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { AuditRow, SkillRow } from '../governanceApi';

// SkillsBoard test (GOV-02 / T-28-03-02). It mocks governanceApi to drive the four lifecycle
// sub-tabs and asserts the read-only enforcement: a PENDING row carries NO run/activate/install
// control (only a select-to-view button), the four tabs are a role=tablist, and the audit tab
// lists rows newest-first.

const fetchSkills = vi.fn();
const fetchSkillsAudit = vi.fn();

vi.mock('../governanceApi', () => ({
  fetchSkills: (...a: unknown[]) => fetchSkills(...a) as Promise<readonly SkillRow[]>,
  fetchSkillsAudit: (...a: unknown[]) => fetchSkillsAudit(...a) as Promise<readonly AuditRow[]>,
}));

const { SkillsBoard } = await import('../SkillsBoard');

const ACTIVE: SkillRow[] = [
  { name: 'golang-testing', description: 'Go test patterns', type: 'instruction' },
];
const PENDING: SkillRow[] = [
  { name: 'pending-skill', description: 'awaiting review', type: 'executable', language: 'python' },
];

function auditRow(over: Partial<AuditRow> & Pick<AuditRow, 'ID' | 'CreatedAt' | 'SkillName'>): AuditRow {
  return {
    ActorID: 'local',
    Action: 'install',
    ContentHash: 'abc',
    ApprovalSource: 'cli',
    GateRecommended: false,
    GateTaken: false,
    BlocklistOverride: false,
    ...over,
  };
}

// Returned deliberately oldest-first to prove the board re-orders newest-first.
const AUDIT: AuditRow[] = [
  auditRow({ ID: 'a-old', CreatedAt: '2026-06-01T10:00:00Z', SkillName: 'older-skill' }),
  auditRow({ ID: 'a-new', CreatedAt: '2026-06-19T10:00:00Z', SkillName: 'newer-skill' }),
];

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children, qc }: { children: ReactNode; qc: QueryClient }) {
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe('SkillsBoard (GOV-02)', () => {
  beforeEach(() => {
    fetchSkills.mockReset();
    fetchSkillsAudit.mockReset();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the four lifecycle sub-tabs as a role=tablist with roving aria-selected/tabindex', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    const tablist = screen.getByRole('tablist', { name: 'Skills' });
    const tabs = within(tablist).getAllByRole('tab');
    expect(tabs.map((t) => t.textContent)).toEqual(['Active', 'Pending', 'Archived', 'Audit']);

    // Active is selected by default (roving tabindex 0); the others are -1 and not selected.
    const activeTab = screen.getByRole('tab', { name: 'Active' });
    const pendingTab = screen.getByRole('tab', { name: 'Pending' });
    expect(activeTab.getAttribute('aria-selected')).toBe('true');
    expect(activeTab.getAttribute('tabindex')).toBe('0');
    expect(pendingTab.getAttribute('aria-selected')).toBe('false');
    expect(pendingTab.getAttribute('tabindex')).toBe('-1');

    // Selecting Pending flips the roving state.
    fireEvent.click(pendingTab);
    await waitFor(() => {
      expect(pendingTab.getAttribute('aria-selected')).toBe('true');
    });
    expect(pendingTab.getAttribute('tabindex')).toBe('0');
    expect(activeTab.getAttribute('tabindex')).toBe('-1');
  });

  it('marks the selected master row with aria-pressed', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });
    const row = screen.getByText('golang-testing').closest('button');
    if (row === null) throw new Error('row button not found');
    expect(row.getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(row);
    await waitFor(() => {
      expect(row.getAttribute('aria-pressed')).toBe('true');
    });
  });

  it('pending rows render with NO run/activate/install control (T-28-03-02)', async () => {
    fetchSkills.mockImplementation((stage: string) =>
      Promise.resolve(stage === 'pending' ? PENDING : ACTIVE),
    );

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    // Switch to the pending tab.
    fireEvent.click(screen.getByRole('tab', { name: 'Pending' }));
    await waitFor(() => {
      expect(screen.getByText('pending-skill')).toBeTruthy();
    });

    // The pending note is shown, and NO run/activate/install affordance exists anywhere. An
    // action control would be a button whose accessible name STARTS with an action verb; the
    // pending-note sentence ("…cannot be run.") must not be mistaken for one, so anchor the verb.
    expect(screen.getAllByText('Pending — inactive and cannot be run.').length).toBeGreaterThan(0);
    expect(
      screen.queryByRole('button', { name: /^(run|activate|install|enable|execute|disable)\b/i }),
    ).toBeNull();
  });

  it('the audit tab lists ledger rows newest-first', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);
    fetchSkillsAudit.mockResolvedValue(AUDIT);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    fireEvent.click(screen.getByRole('tab', { name: 'Audit' }));
    await waitFor(() => {
      expect(screen.getByText('newer-skill')).toBeTruthy();
    });

    // The newest row (newer-skill, 2026-06-19) precedes the older one in DOM order.
    const items = screen.getAllByRole('listitem');
    const text = items.map((li) => li.textContent ?? '').join('||');
    const newerIdx = text.indexOf('newer-skill');
    const olderIdx = text.indexOf('older-skill');
    expect(newerIdx).toBeGreaterThanOrEqual(0);
    expect(olderIdx).toBeGreaterThan(newerIdx);

    // Each audit row renders its action, actor, and created-at (kills the AuditList field mutants).
    expect(screen.getAllByText('install').length).toBeGreaterThan(0);
    expect(screen.getAllByText('local').length).toBeGreaterThan(0);
    expect(screen.getByText('2026-06-19T10:00:00Z')).toBeTruthy();
  });

  it('shows the empty state for an empty stage', async () => {
    fetchSkills.mockResolvedValue([]);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('No skills yet')).toBeTruthy();
    });
  });

  it('shows the audit-empty copy when the ledger is empty', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);
    fetchSkillsAudit.mockResolvedValue([]);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    fireEvent.click(screen.getByRole('tab', { name: 'Audit' }));
    await waitFor(() => {
      expect(screen.getByText('No audit entries yet.')).toBeTruthy();
    });
  });

  it('opens a read-only detail (with the pending note) when a pending row is selected', async () => {
    fetchSkills.mockImplementation((stage: string) =>
      Promise.resolve(stage === 'pending' ? PENDING : ACTIVE),
    );

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    fireEvent.click(screen.getByRole('tab', { name: 'Pending' }));
    await waitFor(() => {
      expect(screen.getByText('pending-skill')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('pending-skill'));

    // Detail shows the language metadata (read-only) — still no run/activate control (the only
    // buttons are the tabs, the row-select, and the detail close ✕).
    await waitFor(() => {
      expect(screen.getAllByText('python').length).toBeGreaterThan(0);
    });
    expect(
      screen.queryByRole('button', { name: /^(run|activate|install|enable|execute|disable)\b/i }),
    ).toBeNull();
  });

  it('renders a visible auth-error when the stage fetch 401s', async () => {
    fetchSkills.mockRejectedValue(new Error('HTTP 401'));

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('Your session expired. Sign in again to continue.')).toBeTruthy();
    });
  });

  it('the active-skill detail renders dashes for missing language/content-hash and closes', async () => {
    // ACTIVE has no language/contentHash → the detail shows the "—" placeholder for both.
    fetchSkills.mockResolvedValue(ACTIVE);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('golang-testing'));

    await waitFor(() => {
      // Type label resolves; the missing language/hash render as the dash placeholder.
      expect(screen.getByText('Go test patterns')).toBeTruthy();
    });
    expect(screen.getAllByText('—').length).toBeGreaterThan(0);

    // Close returns to the detail-empty state (no pending note on an active skill). Both the
    // detail ✕ and the lg:hidden backdrop carry "Close"; the detail ✕ is first in DOM order.
    fireEvent.click(screen.getAllByRole('button', { name: 'Close' })[0]!);
    await waitFor(() => {
      expect(screen.getByText('Select a row to see details')).toBeTruthy();
    });
  });

  it('roams the sub-tabs with the arrow keys (both directions)', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);
    fetchSkillsAudit.mockResolvedValue([]);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    const activeTab = screen.getByRole('tab', { name: 'Active' });
    // ArrowLeft from the first wraps to the last (Audit).
    fireEvent.keyDown(activeTab, { key: 'ArrowLeft' });
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: 'Audit' }).getAttribute('aria-selected')).toBe('true');
    });
    // ArrowRight from Audit wraps back to Active.
    fireEvent.keyDown(screen.getByRole('tab', { name: 'Audit' }), { key: 'ArrowRight' });
    await waitFor(() => {
      expect(activeTab.getAttribute('aria-selected')).toBe('true');
    });
  });

  it('renders a fully-populated skill detail (type + language + content hash, no dashes)', async () => {
    const full: SkillRow[] = [
      {
        name: 'rich-skill',
        description: 'fully described',
        type: 'executable',
        language: 'go',
        contentHash: 'deadbeefcafe',
      },
    ];
    fetchSkills.mockResolvedValue(full);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('rich-skill')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('rich-skill'));

    await waitFor(() => {
      expect(screen.getByText('go')).toBeTruthy();
    });
    expect(screen.getByText('deadbeefcafe')).toBeTruthy();
    expect(screen.getByText('fully described')).toBeTruthy();
    // A fully-populated skill detail renders no dash placeholder.
    expect(screen.queryByText('—')).toBeNull();
  });

  it('the audit tab shows the board error + retry when the ledger fetch fails (non-401)', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);
    fetchSkillsAudit.mockRejectedValueOnce(new Error('HTTP 502'));

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    fireEvent.click(screen.getByRole('tab', { name: 'Audit' }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });

    // Retry re-runs the audit query → now it succeeds and the ledger renders.
    fetchSkillsAudit.mockResolvedValueOnce(AUDIT);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.getByText('newer-skill')).toBeTruthy();
    });
  });

  it('the archived tab fetches the archived stage', async () => {
    fetchSkills.mockImplementation((stage: string) =>
      Promise.resolve(
        stage === 'archived'
          ? ([{ name: 'old-skill', description: 'retired', type: 'instruction' }] as SkillRow[])
          : ACTIVE,
      ),
    );

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    fireEvent.click(screen.getByRole('tab', { name: 'Archived' }));
    await waitFor(() => {
      expect(screen.getByText('old-skill')).toBeTruthy();
    });
    expect(fetchSkills).toHaveBeenCalledWith('archived');
  });
});
