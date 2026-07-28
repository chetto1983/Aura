import { describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { SchedulerTask } from '../governanceApi';

// SchedulerEditDialog test (fix-plan 2.1 / operator BUG-2). The notify dropdown is the only
// place an operator can name a delivery route, and until now it offered whatsapp, email and
// stdout — so an operator whose one live channel is Telegram had no way to ask for it, and
// the scheduled output went to a server console nobody reads.

const editSchedulerTask = vi.fn<(id: string, req: unknown) => Promise<void>>(() =>
  Promise.resolve(),
);

vi.mock('../governanceApi', () => ({
  editSchedulerTask: (id: string, req: unknown) => editSchedulerTask(id, req),
}));

const { SchedulerEditDialog } = await import('../SchedulerEditDialog');

const TASK: SchedulerTask = {
  ID: '11111111-1111-1111-1111-111111111111',
  Kind: 'reminder',
  ScheduleKind: 'cron',
  CronExpr: '0 9 * * *',
  EveryMinutes: 0,
  RunAt: '0001-01-01T00:00:00Z',
  TZ: 'Europe/Rome',
  Status: 'active',
  NextRunAt: '2026-08-01T09:00:00Z',
  NotifyRoute: '',
} as SchedulerTask;

function wrapper({ children }: { readonly children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('SchedulerEditDialog', () => {
  it('offers telegram alongside the other delivery routes', () => {
    render(<SchedulerEditDialog task={TASK} open onClose={vi.fn()} />, { wrapper });

    const select = screen.getByLabelText(/notif/i);
    const values = Array.from(select.querySelectorAll('option')).map((o) =>
      o.getAttribute('value'),
    );
    // The empty value is "use the scheduler default"; the four named routes must all be
    // reachable, and telegram is the one the operator could not previously choose.
    expect(values).toEqual(expect.arrayContaining(['', 'telegram', 'whatsapp', 'email', 'stdout']));
  });

  it('submits the chosen telegram route', async () => {
    render(<SchedulerEditDialog task={TASK} open onClose={vi.fn()} />, { wrapper });

    fireEvent.change(screen.getByLabelText(/notif/i), { target: { value: 'telegram' } });
    fireEvent.click(screen.getByRole('button', { name: /salva|save/i }));

    // react-query's mutate() defers the mutationFn, so the call lands after this tick.
    await waitFor(() => {
      expect(editSchedulerTask).toHaveBeenCalledWith(
        TASK.ID,
        expect.objectContaining({ notify: 'telegram' }),
      );
    });
  });
});
