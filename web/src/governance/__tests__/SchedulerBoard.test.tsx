import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { SchedulerRun, SchedulerTask } from '../governanceApi';

// SchedulerBoard test (GOV-03). It mocks governanceApi to drive the task list + the paginated run
// history: selecting a task shows its runs with a "Showing X of Y" status and a Show-more control
// that requests the next page (limit grows by RUN_PAGE=25). A full first page (exactly 25 rows)
// keeps Show-more visible; a short final page hides it and reports shown===total.

const fetchSchedulerTasks = vi.fn();
const fetchSchedulerRuns = vi.fn();
const approveSchedulerTask = vi.fn<(id: string) => Promise<void>>(() => Promise.resolve());
const runSchedulerTask = vi.fn<(id: string) => Promise<void>>(() => Promise.resolve());
const cancelSchedulerTask = vi.fn<(id: string) => Promise<void>>(() => Promise.resolve());
const editSchedulerTask = vi.fn<(id: string, req: unknown) => Promise<void>>(() =>
  Promise.resolve(),
);

const MANAGEABLE = new Set(['reminder', 'agent_job', 'backup_postgres', 'backup_neo4j']);

vi.mock('../governanceApi', () => ({
  fetchSchedulerTasks: (...a: unknown[]) =>
    fetchSchedulerTasks(...a) as Promise<readonly SchedulerTask[]>,
  fetchSchedulerRuns: (...a: unknown[]) =>
    fetchSchedulerRuns(...a) as Promise<readonly SchedulerRun[]>,
  isUserManageableKind: (kind: string) => MANAGEABLE.has(kind),
  approveSchedulerTask: (id: string) => approveSchedulerTask(id),
  runSchedulerTask: (id: string) => runSchedulerTask(id),
  cancelSchedulerTask: (id: string) => cancelSchedulerTask(id),
  editSchedulerTask: (id: string, req: unknown) => editSchedulerTask(id, req),
}));

const { SchedulerBoard } = await import('../SchedulerBoard');

const TASK_ID = '11111111-1111-1111-1111-111111111111';

const TASKS: SchedulerTask[] = [
  {
    ID: TASK_ID,
    Kind: 'reminder',
    ScheduleKind: 'cron',
    CronExpr: '0 9 * * *',
    EveryMinutes: 0,
    RunAt: '0001-01-01T00:00:00Z',
    TZ: 'UTC',
    StepBudget: 10,
    Status: 'active',
    NextRunAt: '2026-06-21T09:00:00Z',
    NotifyRoute: 'telegram',
    CreatedAt: '2026-06-01T09:00:00Z',
    UpdatedAt: '2026-06-01T09:00:00Z',
  },
];

function run(i: number): SchedulerRun {
  return {
    ID: `run-${String(i)}`,
    TaskID: TASK_ID,
    Status: 'completed',
    StepBudget: 10,
    StartedAt: `2026-06-1${String(i % 9)}T09:00:00Z`,
    LastHeartbeatAt: `2026-06-1${String(i % 9)}T09:01:00Z`,
    CompletedWithHash: 'h',
    Summary: `run summary ${String(i)}`,
    LastError: '',
    MissedSince: '0001-01-01T00:00:00Z',
    CompletedAt: '2026-06-19T09:02:00Z',
  };
}

function baseTask(): SchedulerTask {
  const b = TASKS[0];
  if (b === undefined) throw new Error('base task fixture missing');
  return b;
}

const PAGE_1 = Array.from({ length: 25 }, (_, i) => run(i)); // exactly one full page
const PAGE_2 = Array.from({ length: 27 }, (_, i) => run(i)); // 25 + 2 → short final page

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Wrapper({ children, qc }: { children: ReactNode; qc: QueryClient }) {
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe('SchedulerBoard (GOV-03)', () => {
  beforeEach(() => {
    fetchSchedulerTasks.mockReset();
    fetchSchedulerRuns.mockReset();
    approveSchedulerTask.mockClear();
    runSchedulerTask.mockClear();
    cancelSchedulerTask.mockClear();
    editSchedulerTask.mockClear();
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders task rows with the cron schedule + status, and marks the selected row aria-pressed', async () => {
    fetchSchedulerTasks.mockResolvedValue(TASKS);
    fetchSchedulerRuns.mockResolvedValue([]);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('reminder')).toBeTruthy();
    });
    expect(screen.getByText('0 9 * * *')).toBeTruthy();
    // The row shows the task status + the compact (T→space) next-run time.
    expect(screen.getByText('active')).toBeTruthy();
    expect(screen.getByText('2026-06-21 09:00:00Z')).toBeTruthy();

    const row = screen.getByText('reminder').closest('button');
    if (row === null) throw new Error('task row button not found');
    expect(row.getAttribute('aria-pressed')).toBe('false');
    fireEvent.click(row);
    await waitFor(() => {
      expect(row.getAttribute('aria-pressed')).toBe('true');
    });
  });

  it('selecting a task shows paginated run history with Show-more + "Showing X of Y"', async () => {
    fetchSchedulerTasks.mockResolvedValue(TASKS);
    // First page = exactly 25 rows (a full page → Show-more visible, total unknown "25+").
    fetchSchedulerRuns.mockImplementation((_id: string, limit: number) =>
      Promise.resolve(limit > 25 ? PAGE_2 : PAGE_1),
    );

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('reminder')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('reminder'));

    // The run-history pane shows the page status + a Show-more control on a full first page.
    await waitFor(() => {
      expect(screen.getByText('Showing 25 of 25+')).toBeTruthy();
    });
    const showMore = screen.getByRole('button', { name: 'Show more' });
    expect(showMore).toBeTruthy();

    // Clicking Show-more requests the next page (limit 50) → 27 rows, the short final page.
    fireEvent.click(showMore);
    await waitFor(() => {
      expect(screen.getByText('Showing 27 of 27')).toBeTruthy();
    });
    // The final short page hides Show-more.
    expect(screen.queryByRole('button', { name: 'Show more' })).toBeNull();
    expect(fetchSchedulerRuns).toHaveBeenCalledWith(TASK_ID, 50, 0);
  });

  it('renders each run row with its status + started time, and hides the summary when empty', async () => {
    fetchSchedulerTasks.mockResolvedValue(TASKS);
    const withSummary = run(1);
    const noSummary: SchedulerRun = { ...run(2), ID: 'run-nosum', Status: 'failed', Summary: '' };
    fetchSchedulerRuns.mockResolvedValue([withSummary, noSummary]);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('reminder')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('reminder'));

    // Wait for the runs query to resolve (the summary text appears with the run rows).
    await waitFor(() => {
      expect(screen.getByText('run summary 1')).toBeTruthy();
    });
    // The run status text + the started timestamp render (kills the status/started mutants). The
    // status label + value are separate text nodes, so match on the run-row container text.
    const runText = screen
      .getAllByRole('listitem')
      .map((li) => li.textContent ?? '')
      .join('||');
    expect(runText).toContain('completed');
    expect(runText).toContain('failed');
    expect(runText).toContain('Status');
    expect(runText).toContain('Heartbeat');
    expect(screen.getByText(withSummary.StartedAt)).toBeTruthy();
    expect(screen.getByText(withSummary.LastHeartbeatAt, { exact: false })).toBeTruthy();
    // The non-empty summary renders; the empty-summary run shows no summary text.
    expect(screen.getByText('run summary 1')).toBeTruthy();
    // A short page (2 < 25) hides Show-more and reports the exact total.
    expect(screen.getByText('Showing 2 of 2')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Show more' })).toBeNull();
  });

  it('shows the no-runs copy for a task with an empty run history', async () => {
    fetchSchedulerTasks.mockResolvedValue(TASKS);
    fetchSchedulerRuns.mockResolvedValue([]);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('reminder')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('reminder'));

    await waitFor(() => {
      expect(screen.getByText('No runs recorded for this task yet.')).toBeTruthy();
    });
  });

  it('shows the empty state when there are no scheduled tasks', async () => {
    fetchSchedulerTasks.mockResolvedValue([]);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('No scheduled tasks')).toBeTruthy();
    });
  });

  it('renders a visible auth-error when the task fetch 401s', async () => {
    fetchSchedulerTasks.mockRejectedValue(new Error('HTTP 401'));

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('Your session expired. Sign in again to continue.')).toBeTruthy();
    });
  });

  it('renders the schedule fallbacks: every-N-minutes, one-shot RunAt, and the dash', async () => {
    const base = TASKS[0];
    if (base === undefined) throw new Error('base task fixture missing');
    const variants: SchedulerTask[] = [
      { ...base, ID: 'every', Kind: 'agent_job', CronExpr: '', EveryMinutes: 15 },
      {
        ...base,
        ID: 'oneshot',
        Kind: 'backup_postgres',
        CronExpr: '',
        EveryMinutes: 0,
        RunAt: '2026-07-01T00:00:00Z',
      },
      {
        ...base,
        ID: 'none',
        Kind: 'backup_neo4j',
        CronExpr: '',
        EveryMinutes: 0,
        RunAt: '0001-01-01T00:00:00Z',
      },
    ];
    fetchSchedulerTasks.mockResolvedValue(variants);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('every 15m')).toBeTruthy();
    });
    expect(screen.getByText('2026-07-01T00:00:00Z')).toBeTruthy();
    // The no-schedule task shows the dash placeholder (—) at least once.
    expect(screen.getAllByText('—').length).toBeGreaterThan(0);
  });

  it('a pending_approval task shows Approve and clicking it calls the approve API', async () => {
    const pending: SchedulerTask = { ...baseTask(), ID: 'pend', Status: 'pending_approval' };
    fetchSchedulerTasks.mockResolvedValue([pending]);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    const approveBtn = await screen.findByRole('button', { name: 'Approve' });
    fireEvent.click(approveBtn);
    await waitFor(() => {
      expect(approveSchedulerTask).toHaveBeenCalledWith('pend');
    });
  });

  it('an active task shows Run now and clicking it calls the run API', async () => {
    fetchSchedulerTasks.mockResolvedValue(TASKS);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    const runBtn = await screen.findByRole('button', { name: 'Run now' });
    fireEvent.click(runBtn);
    await waitFor(() => {
      expect(runSchedulerTask).toHaveBeenCalledWith(TASK_ID);
    });
  });

  it('deleting a task routes through the confirm dialog before calling the cancel API', async () => {
    fetchSchedulerTasks.mockResolvedValue(TASKS);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }));
    // The hard delete never fires blind — the confirm dialog gates it.
    expect(cancelSchedulerTask).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel task' }));
    await waitFor(() => {
      expect(cancelSchedulerTask).toHaveBeenCalledWith(TASK_ID);
    });
  });

  it('a system-seeded task is read-only: a SYSTEM tag, no operator verbs', async () => {
    const sys: SchedulerTask = {
      ...baseTask(),
      ID: 'sys',
      Kind: 'identity_purge',
      Status: 'active',
    };
    fetchSchedulerTasks.mockResolvedValue([sys]);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('identity_purge')).toBeTruthy();
    });
    expect(screen.getByText('System')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Run now' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
  });

  it('closing the run-history detail returns to the detail-empty state', async () => {
    fetchSchedulerTasks.mockResolvedValue(TASKS);
    fetchSchedulerRuns.mockResolvedValue([]);

    render(<SchedulerBoard />, {
      wrapper: ({ children }) => <Wrapper qc={client()}>{children}</Wrapper>,
    });

    await waitFor(() => {
      expect(screen.getByText('reminder')).toBeTruthy();
    });
    fireEvent.click(screen.getByText('reminder'));
    await waitFor(() => {
      expect(screen.getByText('Run history')).toBeTruthy();
    });

    // Both the detail ✕ and the lg:hidden backdrop carry "Close"; the detail ✕ is first.
    const closeButton = screen.getAllByRole('button', { name: 'Close' })[0];
    if (closeButton === undefined) throw new Error('close button not found');
    fireEvent.click(closeButton);
    await waitFor(() => {
      expect(screen.getByText('Select a row to see details')).toBeTruthy();
    });
  });
});
