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
const archiveSkill = vi.fn();
const restoreSkill = vi.fn();
const installSkill = vi.fn();
const searchSkillCatalog = vi.fn();

vi.mock('../governanceApi', () => ({
  fetchSkills: (...a: unknown[]) => fetchSkills(...a) as Promise<readonly SkillRow[]>,
  fetchSkillsAudit: (...a: unknown[]) => fetchSkillsAudit(...a) as Promise<readonly AuditRow[]>,
  archiveSkill: (...a: unknown[]) => archiveSkill(...a) as Promise<void>,
  restoreSkill: (...a: unknown[]) => restoreSkill(...a) as Promise<void>,
  installSkill: (...a: unknown[]) => installSkill(...a) as Promise<unknown>,
  searchSkillCatalog: (...a: unknown[]) => searchSkillCatalog(...a) as Promise<unknown>,
}));

const { SkillsBoard } = await import('../SkillsBoard');

const ACTIVE: SkillRow[] = [
  { name: 'golang-testing', description: 'Go test patterns', type: 'instruction' },
];
const PENDING: SkillRow[] = [
  { name: 'pending-skill', description: 'awaiting review', type: 'executable', language: 'python' },
];

function auditRow(
  over: Partial<AuditRow> & Pick<AuditRow, 'ID' | 'CreatedAt' | 'SkillName'>,
): AuditRow {
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
    archiveSkill.mockReset();
    restoreSkill.mockReset();
    installSkill.mockReset();
    searchSkillCatalog.mockReset();
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
    expect(tablist.getAttribute('data-slot')).toBe('tabs-list');
    expect(tablist.className).toContain('grid-cols-4');
    expect(tablist.className).toContain('min-w-0');
    expect(tablist.className).toContain('flex-1');
    const tabs = within(tablist).getAllByRole('tab');
    expect(tabs.map((t) => t.textContent)).toEqual(['Active', 'Pending', 'Archived', 'Audit']);
    expect(tabs.every((t) => t.getAttribute('data-slot') === 'tabs-trigger')).toBe(true);
    expect(tabs.every((t) => t.className.includes('min-w-0'))).toBe(true);
    expect(
      tabs.every((t) => t.querySelector('[data-tab-label]')?.className.includes('truncate')),
    ).toBe(true);
    const installButton = screen.getByRole('button', { name: 'Install skill' });
    expect(installButton.getAttribute('title')).toBe('Install skill');
    expect(installButton.querySelector('[data-action-label]')?.className).toContain('hidden');

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

    // The pending note is shown, and a PENDING ROW carries NO run/activate/execute/enable
    // affordance. The board-level "Install skill" CTA (which STAGES to pending → approval, never
    // activates a pending skill) is the one allowed install action and is excluded here; the
    // pending-note sentence ("…cannot be run.") must not be mistaken for a control.
    expect(screen.getAllByText('Pending — inactive and cannot be run.').length).toBeGreaterThan(0);
    expect(
      screen.queryByRole('button', { name: /^(run|activate|enable|execute|disable)\b/i }),
    ).toBeNull();
    // No per-row Archive/Restore on a pending row (those are active/archived controls only).
    expect(screen.queryByRole('button', { name: 'Archive skill' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Restore skill' })).toBeNull();
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
    // buttons are the tabs, the board-level Install-skill staging CTA, the row-select, and the
    // detail close ✕).
    await waitFor(() => {
      expect(screen.getAllByText('python').length).toBeGreaterThan(0);
    });
    expect(
      screen.queryByRole('button', { name: /^(run|activate|enable|execute|disable)\b/i }),
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
    const closeButton = screen.getAllByRole('button', { name: 'Close' })[0];
    if (closeButton === undefined) throw new Error('close button not found');
    fireEvent.click(closeButton);
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
    // The content hash now renders both as per-row metadata AND in the detail (Phase-29
    // SkillsBoard surfaces the hash on the row too) — at least one occurrence is present.
    expect(screen.getAllByText('deadbeefcafe').length).toBeGreaterThan(0);
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

  it('opens the install panel from the install CTA (reachable on any tab)', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    const installButton = screen.getByRole('button', { name: 'Install skill' });
    expect(installButton.getAttribute('data-slot')).toBe('button');
    expect(installButton.querySelector('svg[data-icon="inline-start"]')).not.toBeNull();
    fireEvent.click(installButton);

    // The no-ceremony install panel: catalog search + an `Install` CTA (no RISKY banner,
    // no validation-checklist heading).
    await waitFor(() => {
      expect(screen.getByPlaceholderText('Search the skills.sh catalog')).toBeTruthy();
    });
    expect(screen.getByRole('button', { name: 'Install' })).toBeTruthy();
    expect(screen.queryByText('Validation checklist')).toBeNull();
  });

  it('archives an active skill via the row Archive control', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);
    archiveSkill.mockResolvedValue(undefined);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Archive skill' }));
    await waitFor(() => {
      expect(archiveSkill).toHaveBeenCalledWith('golang-testing');
    });
  });

  it('renders the archive action and type chip with shadcn primitives', async () => {
    fetchSkills.mockResolvedValue(ACTIVE);

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    const archiveButton = screen.getByRole('button', { name: 'Archive skill' });
    expect(archiveButton.getAttribute('data-slot')).toBe('button');
    expect(archiveButton.className).toContain('h-[44px]');
    expect(archiveButton.className).toContain('w-[44px]');
    expect(archiveButton.className).not.toContain('shadow-[');
    expect(archiveButton.querySelector('svg[data-icon="icon"]')).not.toBeNull();
    expect(screen.getByText('instruction').getAttribute('data-slot')).toBe('badge');
    expect(
      screen.getByText('golang-testing').closest('[role="listitem"]')?.className,
    ).not.toContain('shadow-[');
  });

  it('restores an archived skill; a colliding restore (409) shows the inline safe error', async () => {
    fetchSkills.mockImplementation((stage: string) =>
      Promise.resolve(
        stage === 'archived'
          ? ([{ name: 'dup-skill', description: 'retired', type: 'instruction' }] as SkillRow[])
          : ACTIVE,
      ),
    );

    render(<SkillsBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });
    fireEvent.click(screen.getByRole('tab', { name: 'Archived' }));
    await waitFor(() => {
      expect(screen.getByText('dup-skill')).toBeTruthy();
    });

    // First restore collides (a 409 from a name-colliding active skill) → the inline error.
    restoreSkill.mockRejectedValueOnce(new Error('HTTP 409'));
    fireEvent.click(screen.getByRole('button', { name: 'Restore skill' }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
    expect(restoreSkill).toHaveBeenCalledWith('dup-skill');

    // A subsequent successful restore clears the inline error (collisionName reset).
    restoreSkill.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByRole('button', { name: 'Restore skill' }));
    await waitFor(() => {
      expect(screen.queryByRole('alert')).toBeNull();
    });
  });
});
