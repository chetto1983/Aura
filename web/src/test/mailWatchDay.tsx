import { fireEvent, screen, within } from '@testing-library/react';
import { vi } from 'vitest';
import { MailWatchDialog } from '../governance/mailWatch/MailWatchDialog';
import { decision, renderWatch, watchDay, type mailWatchServer } from './mailWatchServer';

// The day of account `work`, opened through the real dialog, and the ways a test finds a row in it.

export const DECISIONS = 'GET /api/mail-watch/accounts/work/decisions';
export const CORRECT = '/api/mail-watch/accounts/work/decisions/correct';

export const THREE = [
  decision({ item_id: 'a', subject: 'First', tier: 'now', from: 'a@x.org', from_name: 'A' }),
  decision({ item_id: 'b', subject: 'Second', from: 'b@x.org', from_name: 'B' }),
  decision({ item_id: 'c', subject: 'Third', from: 'c@x.org', from_name: 'C' }),
];

export function theDay(
  server: ReturnType<typeof mailWatchServer>,
  ...rows: readonly ReturnType<typeof decision>[]
) {
  server.on(DECISIONS, () => ({ body: watchDay(rows) }));
}

export function openDay() {
  return renderWatch(
    <MailWatchDialog open onOpenChange={vi.fn()} account={{ id: 'work', name: 'Work' }} />,
  );
}

export function dateInput(): HTMLElement {
  return screen.getByLabelText('Day', { selector: 'input' });
}

/** The button that is a row's stop in the tab order. Only a row's button says aria-expanded. */
export function rowOf(subject: string): HTMLElement {
  return screen.getByRole('button', { name: new RegExp(subject), expanded: false });
}

/** The whole row, controls included. */
export function itemAround(element: Element): HTMLElement {
  const item = element.closest('li');
  if (item === null) throw new Error('not inside a row');
  return item;
}

export function itemOf(subject: string): HTMLElement {
  return itemAround(rowOf(subject));
}

export function activeRow(): HTMLElement {
  const row = document.querySelector<HTMLElement>('[data-mail-watch-row][tabindex="0"]');
  if (row === null) throw new Error('no active row');
  return row;
}

export function press(key: string, target: Element = activeRow()) {
  fireEvent.keyDown(target, { key });
}

export function tierOf(subject: string): string | null {
  return (
    within(itemOf(subject))
      .queryAllByRole('radio')
      .find((radio) => radio.getAttribute('aria-checked') === 'true')
      ?.getAttribute('aria-label') ?? null
  );
}
