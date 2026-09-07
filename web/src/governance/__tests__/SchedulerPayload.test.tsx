import { describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { SchedulerTask } from '../governanceApi';

// SchedulerPayload test (operator report 2026-09-07): a reminder they dictated themselves was
// unreadable in the cockpit until it fired on Telegram, and the edit dialog offered an empty box
// that would silently keep the old text — so changing it meant retyping it blind.
//
// The API row stripped the payload as "private prompt/context material". The board is the
// operator's own console; the payload is the sentence they said. These two tests pin that they
// can now READ it and EDIT it.

const editSchedulerTask = vi.fn<(id: string, req: unknown) => Promise<void>>(() =>
  Promise.resolve(),
);

vi.mock('../governanceApi', () => ({
  editSchedulerTask: (id: string, req: unknown) => editSchedulerTask(id, req),
}));

const { SchedulerEditDialog } = await import('../SchedulerEditDialog');
const { payloadText } = await import('../schedulerPayload');

const REMINDER: SchedulerTask = {
  ID: '11111111-1111-1111-1111-111111111111',
  Kind: 'reminder',
  ScheduleKind: 'at',
  CronExpr: '',
  EveryMinutes: 0,
  RunAt: '2026-09-07T10:22:10Z',
  TZ: 'Europe/Rome',
  Status: 'active',
  NextRunAt: '2026-09-07T10:22:10Z',
  NotifyRoute: 'telegram',
  Payload: { text: 'Telefona ad Andrea' },
} as unknown as SchedulerTask;

function wrapper({ children }: { readonly children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('the scheduled text an operator dictated', () => {
  it('is readable on the row, so it does not take a click to find out what fires', () => {
    expect(payloadText(REMINDER.Payload)).toBe('Telefona ad Andrea');
    expect(payloadText({ goal: 'chiudi la milestone' })).toBe('chiudi la milestone');
    // Anything the dialog cannot edit renders nothing rather than a JSON blob.
    expect(payloadText({ table: 'aura.runs' })).toBe('');
    expect(payloadText(undefined)).toBe('');
  });

  it('prefills the edit dialog so a change is an edit, not a retype', () => {
    render(<SchedulerEditDialog task={REMINDER} open onClose={() => undefined} />, { wrapper });

    // getByDisplayValue asserts the rendered value directly, so the test needs no cast that
    // tsc and the typed linter disagree about.
    expect(screen.getByDisplayValue('Telefona ad Andrea')).toBeTruthy();
  });
});
