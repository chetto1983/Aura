import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { AuditRow, SkillRow } from '../governanceApi';

// SkillsBoard test (GOV-02, rebuilt for amendment #97). The board is ONE list of both
// lifecycle stages with state filters, an audit toggle, an inline reversible verb per row,
// and a delete that is only reachable through the detail behind a confirm.
//
// The load-bearing assertions here are the safety ones: 'pending' is gone from the wire and
// the UI, archive fires with no ceremony, and delete NEVER calls the API until the confirm
// is accepted — a destructive verb that fires on the first click is exactly the bug this
// design exists to prevent.

const fetchSkills = vi.fn();
const fetchSkillsAudit = vi.fn();
const fetchSkillBody = vi.fn();
const archiveSkill = vi.fn();
const restoreSkill = vi.fn();
const deleteSkill = vi.fn();
const validateSkill = vi.fn();
const createSkill = vi.fn();
const updateSkill = vi.fn();
const installSkill = vi.fn();
const searchSkillCatalog = vi.fn();

vi.mock('../governanceApi', () => ({
  fetchSkills: (...a: unknown[]) => fetchSkills(...a) as Promise<readonly SkillRow[]>,
  fetchSkillsAudit: (...a: unknown[]) => fetchSkillsAudit(...a) as Promise<readonly AuditRow[]>,
  fetchSkillBody: (...a: unknown[]) => fetchSkillBody(...a) as Promise<string>,
  archiveSkill: (...a: unknown[]) => archiveSkill(...a) as Promise<void>,
  restoreSkill: (...a: unknown[]) => restoreSkill(...a) as Promise<void>,
  deleteSkill: (...a: unknown[]) => deleteSkill(...a) as Promise<void>,
  validateSkill: (...a: unknown[]) => validateSkill(...a) as Promise<unknown>,
  createSkill: (...a: unknown[]) => createSkill(...a) as Promise<unknown>,
  updateSkill: (...a: unknown[]) => updateSkill(...a) as Promise<unknown>,
  installSkill: (...a: unknown[]) => installSkill(...a) as Promise<unknown>,
  searchSkillCatalog: (...a: unknown[]) => searchSkillCatalog(...a) as Promise<unknown>,
}));

const { SkillsBoard } = await import('../SkillsBoard');

const ACTIVE: SkillRow[] = [
  { name: 'golang-testing', description: 'Go test patterns', type: 'instruction' },
  { name: 'find-skills', description: 'discover skills', type: 'instruction', always: true },
];
const ARCHIVED: SkillRow[] = [
  { name: 'old-skill', description: 'retired', type: 'snippet', language: 'python' },
];

/** Route the mocked stage fetch to the right fixture. */
function stageFixtures(active: SkillRow[] = ACTIVE, archived: SkillRow[] = ARCHIVED) {
  fetchSkills.mockImplementation((stage: string) =>
    Promise.resolve(stage === 'archived' ? archived : active),
  );
}

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

function renderBoard() {
  return render(<SkillsBoard />, {
    wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
  });
}

/** Open the detail for a named skill and wait for it to render. */
async function openDetail(name: string) {
  await waitFor(() => {
    expect(screen.getByText(name)).toBeTruthy();
  });
  fireEvent.click(screen.getByText(name));
  await waitFor(() => {
    expect(screen.getByRole('button', { name: 'Delete' })).toBeTruthy();
  });
}

describe('SkillsBoard (GOV-02)', () => {
  beforeEach(() => {
    for (const m of [
      fetchSkills,
      fetchSkillsAudit,
      fetchSkillBody,
      archiveSkill,
      restoreSkill,
      deleteSkill,
      validateSkill,
      createSkill,
      updateSkill,
      installSkill,
      searchSkillCatalog,
    ]) {
      m.mockReset();
    }
    fetchSkillBody.mockResolvedValue('# body');
    stageFixtures();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('lists both stages in one list and counts them per filter', async () => {
    renderBoard();

    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });
    // Active and archived rows coexist — no tab to switch between them.
    expect(screen.getByText('old-skill')).toBeTruthy();
    expect(fetchSkills).toHaveBeenCalledWith('active');
    expect(fetchSkills).toHaveBeenCalledWith('archived');

    const group = screen.getByRole('group', { name: 'Filter by state' });
    const chips = within(group).getAllByRole('button');
    expect(chips.map((c) => c.textContent)).toEqual(['All3', 'Active2', 'Archived1']);
    expect(chips[0]?.getAttribute('aria-pressed')).toBe('true');
  });

  it('has no Pending affordance anywhere — the stage is gone (amendment #97)', async () => {
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    expect(screen.queryByRole('tab')).toBeNull();
    expect(screen.queryByText(/Pending/i)).toBeNull();
    expect(screen.queryByText('Pending — inactive and cannot be run.')).toBeNull();
    // The retired stage is never requested from the server (it answers 400).
    expect(fetchSkills.mock.calls.flat()).not.toContain('pending');
  });

  it('filters the list by state without refetching', async () => {
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('old-skill')).toBeTruthy();
    });
    const callsBefore = fetchSkills.mock.calls.length;

    fireEvent.click(screen.getByRole('button', { name: /^Active2$/ }));
    await waitFor(() => {
      expect(screen.queryByText('old-skill')).toBeNull();
    });
    expect(screen.getByText('golang-testing')).toBeTruthy();
    expect(fetchSkills.mock.calls.length).toBe(callsBefore);

    fireEvent.click(screen.getByRole('button', { name: /^Archived1$/ }));
    await waitFor(() => {
      expect(screen.getByText('old-skill')).toBeTruthy();
    });
    expect(screen.queryByText('golang-testing')).toBeNull();
  });

  it('searches by name and description', async () => {
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    fireEvent.change(screen.getByPlaceholderText('Search skills…'), {
      target: { value: 'retired' },
    });
    await waitFor(() => {
      expect(screen.queryByText('golang-testing')).toBeNull();
    });
    // Matched on the DESCRIPTION, not the name.
    expect(screen.getByText('old-skill')).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText('Search skills…'), {
      target: { value: 'nothing-matches-this' },
    });
    await waitFor(() => {
      expect(screen.getByText('No skills yet')).toBeTruthy();
    });
  });

  it('marks an always-on skill on its row', async () => {
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('find-skills')).toBeTruthy();
    });
    const row = screen.getByText('find-skills').closest('[role="listitem"]');
    expect(within(row as HTMLElement).getByText('Always')).toBeTruthy();
  });

  it('archives an active skill from the row with no confirmation', async () => {
    archiveSkill.mockResolvedValue(undefined);
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    const row = screen.getByText('golang-testing').closest('[role="listitem"]');
    fireEvent.click(within(row as HTMLElement).getByRole('button', { name: 'Archive skill' }));
    await waitFor(() => {
      expect(archiveSkill).toHaveBeenCalledWith('golang-testing');
    });
    // Archive is reversible, so it asks nothing.
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('restores an archived skill; a colliding restore (409) shows the inline safe error', async () => {
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('old-skill')).toBeTruthy();
    });

    restoreSkill.mockRejectedValueOnce(new Error('HTTP 409'));
    fireEvent.click(screen.getByRole('button', { name: 'Restore skill' }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
    expect(restoreSkill).toHaveBeenCalledWith('old-skill');

    restoreSkill.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByRole('button', { name: 'Restore skill' }));
    await waitFor(() => {
      expect(screen.queryByRole('alert')).toBeNull();
    });
  });

  it('never deletes without the confirm — and deletes with it', async () => {
    deleteSkill.mockResolvedValue(undefined);
    renderBoard();
    await openDetail('golang-testing');

    // Delete is NOT on the row: the only Delete control is the detail's.
    expect(screen.getAllByRole('button', { name: 'Delete' })).toHaveLength(1);

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('alertdialog');
    // Opening the dialog must not have deleted anything yet.
    expect(deleteSkill).not.toHaveBeenCalled();
    // The confirm names the skill and offers the reversible alternative in the copy.
    expect(within(dialog).getByText('Delete “golang-testing”?')).toBeTruthy();
    expect(within(dialog).getByText(/archive it instead/)).toBeTruthy();

    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));
    await waitFor(() => {
      expect(deleteSkill).toHaveBeenCalledWith('golang-testing');
    });
  });

  it('cancelling the confirm deletes nothing', async () => {
    renderBoard();
    await openDetail('golang-testing');

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    const dialog = await screen.findByRole('alertdialog');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));

    await waitFor(() => {
      expect(screen.queryByRole('alertdialog')).toBeNull();
    });
    expect(deleteSkill).not.toHaveBeenCalled();
  });

  it('opens the editor from the toolbar, and closing it returns to the list', async () => {
    validateSkill.mockResolvedValue({ ok: true });
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: 'New skill' }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save and activate' })).toBeTruthy();
    });
    // The editor takes the whole board — the list is not behind it.
    expect(screen.queryByText('golang-testing')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });
  });

  it('opens the editor on an existing skill seeded with its loaded body', async () => {
    validateSkill.mockResolvedValue({ ok: true });
    fetchSkillBody.mockResolvedValue('## Existing body');
    renderBoard();
    await openDetail('golang-testing');

    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    await waitFor(() => {
      expect(screen.getByRole('textbox', { name: 'Name' })).toBeTruthy();
    });
    // Editing an existing skill locks the name: it is the directory on disk.
    expect(screen.getByRole('textbox', { name: 'Name' }).getAttribute('readonly')).not.toBeNull();
    expect(screen.getByRole('textbox', { name: 'Name' }).getAttribute('value')).toBe(
      'golang-testing',
    );
  });

  it('offers no Edit for an archived skill — there is no body to edit', async () => {
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('old-skill')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('old-skill'));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Delete' })).toBeTruthy();
    });
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
  });

  it('marks the selected master row with aria-pressed', async () => {
    renderBoard();
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

  it('shows the ledger behind the Audit toggle, newest-first', async () => {
    fetchSkillsAudit.mockResolvedValue(AUDIT);
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });
    // The ledger is not fetched until it is asked for.
    expect(fetchSkillsAudit).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Audit' }));
    await waitFor(() => {
      expect(screen.getByText('newer-skill')).toBeTruthy();
    });

    const items = screen.getAllByRole('listitem');
    const text = items.map((li) => li.textContent ?? '').join('||');
    expect(text.indexOf('older-skill')).toBeGreaterThan(text.indexOf('newer-skill'));
    expect(screen.getAllByText('install').length).toBeGreaterThan(0);
    expect(screen.getAllByText('local').length).toBeGreaterThan(0);
    expect(screen.getByText('2026-06-19T10:00:00Z')).toBeTruthy();

    // Toggling back returns to the list.
    fireEvent.click(screen.getByRole('button', { name: 'Audit' }));
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });
  });

  it('shows the audit-empty copy when the ledger is empty', async () => {
    fetchSkillsAudit.mockResolvedValue([]);
    renderBoard();

    fireEvent.click(screen.getByRole('button', { name: 'Audit' }));
    await waitFor(() => {
      expect(screen.getByText('No audit entries yet.')).toBeTruthy();
    });
  });

  it('shows the empty state when there are no skills at all', async () => {
    stageFixtures([], []);
    renderBoard();

    await waitFor(() => {
      expect(screen.getByText('No skills yet')).toBeTruthy();
    });
  });

  it('renders a visible auth-error when a stage fetch 401s', async () => {
    fetchSkills.mockRejectedValue(new Error('HTTP 401'));
    renderBoard();

    await waitFor(() => {
      expect(screen.getByText('Your session expired. Sign in again to continue.')).toBeTruthy();
    });
  });

  it('shows the board error + retry when a stage fetch fails (non-401)', async () => {
    fetchSkills
      .mockRejectedValueOnce(new Error('HTTP 502'))
      .mockRejectedValueOnce(new Error('HTTP 502'));
    renderBoard();
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });

    stageFixtures();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });
  });

  it('opens the install panel from the install CTA', async () => {
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    const installButton = screen.getByRole('button', { name: 'Install skill' });
    expect(installButton.getAttribute('data-slot')).toBe('button');
    expect(installButton.querySelector('svg[data-icon="inline-start"]')).not.toBeNull();
    fireEvent.click(installButton);

    await waitFor(() => {
      expect(screen.getByPlaceholderText('Search the skills.sh catalog')).toBeTruthy();
    });
    expect(screen.getByRole('button', { name: 'Install' })).toBeTruthy();
    expect(screen.queryByText('Validation checklist')).toBeNull();
  });

  it('renders the row action and type chip with shadcn primitives', async () => {
    renderBoard();
    await waitFor(() => {
      expect(screen.getByText('golang-testing')).toBeTruthy();
    });

    const row = screen.getByText('golang-testing').closest('[role="listitem"]');
    const archiveButton = within(row as HTMLElement).getByRole('button', { name: 'Archive skill' });
    expect(archiveButton.getAttribute('data-slot')).toBe('button');
    expect(archiveButton.className).toContain('h-[44px]');
    expect(archiveButton.className).toContain('w-[44px]');
    expect(archiveButton.className).not.toContain('shadow-[');
    expect(archiveButton.querySelector('svg[data-icon="icon"]')).not.toBeNull();
    expect(
      within(row as HTMLElement)
        .getByText('instruction')
        .getAttribute('data-slot'),
    ).toBe('badge');
    expect(row?.className).not.toContain('shadow-[');
  });
});
