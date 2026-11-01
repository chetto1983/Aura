import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import axe from 'axe-core';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import '../../../i18n/i18n';
import { decision, label, mailWatchServer, renderWatch, rule } from '../../../test/mailWatchServer';
import { MailWatchDialog } from '../MailWatchDialog';

// The dialog that holds the watch: its tabs, its close, and the two things only a dialog can get
// wrong. Escape must stop at it, because the governance board's mobile sheet listens on the
// document. And it must be clean to axe: jsdom has no layout, so contrast is not measured here.

let server: ReturnType<typeof mailWatchServer>;
beforeEach(() => {
  server = mailWatchServer();
  server.on('GET /api/mail-watch/accounts/work/decisions', () => ({
    body: [
      decision({ item_id: 'a', subject: 'First', tier: 'now', label_id: 'l1' }),
      decision({ item_id: 'b', subject: 'Second', auth_failed: true }),
      decision({ item_id: 'c', gone: true, from: '', from_name: '', subject: '' }),
      decision({ item_id: 'd', resource: 'calendar', from_name: '', subject: 'Meeting' }),
    ],
  }));
  server.on('GET /api/mail-watch/labels', () => ({ body: [label({ id: 'l1' })] }));
  server.on('GET /api/mail-watch/rules', () => ({ body: [rule()] }));
});
afterEach(() => {
  vi.unstubAllGlobals();
});

const ACCOUNT = { id: 'work', name: 'Work' };

async function violations(context: Element) {
  const result = await axe.run(context, {
    rules: { 'color-contrast': { enabled: false } },
  });
  return result.violations.map((violation) => violation.id);
}

describe('MailWatchDialog', () => {
  it('opens on the day, with a tab for each thing the person can set', async () => {
    renderWatch(<MailWatchDialog open onOpenChange={vi.fn()} account={ACCOUNT} />);
    const dialog = await screen.findByRole('dialog', { name: 'Work' });
    const tabs = within(dialog).getAllByRole('tab');
    expect(tabs.map((tab) => tab.textContent)).toEqual(['Day', 'Rules', 'Labels', 'Settings']);
    expect(tabs[0]?.getAttribute('aria-selected')).toBe('true');
  });

  it('says what the dialog is, for the account and for what accounts share', async () => {
    const { unmount } = renderWatch(
      <MailWatchDialog open onOpenChange={vi.fn()} account={ACCOUNT} />,
    );
    expect(await screen.findByText(/What Aura decided about your mail/)).toBeTruthy();
    unmount();
    renderWatch(<MailWatchDialog open onOpenChange={vi.fn()} />);
    expect(await screen.findByText(/every watched account shares/)).toBeTruthy();
  });

  it('closes from its button', async () => {
    const onOpenChange = vi.fn();
    renderWatch(<MailWatchDialog open onOpenChange={onOpenChange} account={ACCOUNT} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Close' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('keeps Escape from the sheet behind it', async () => {
    const onDocument = vi.fn<(event: KeyboardEvent) => void>();
    document.addEventListener('keydown', onDocument);
    renderWatch(<MailWatchDialog open onOpenChange={vi.fn()} account={ACCOUNT} />);
    const dialog = await screen.findByRole('dialog');
    fireEvent.keyDown(dialog, { key: 'Escape' });
    fireEvent.keyDown(dialog, { key: 'a' });
    document.removeEventListener('keydown', onDocument);
    expect(onDocument.mock.calls.map(([event]) => event.key)).toEqual(['a']);
  });

  it('reads the day on the screen for the rule preview', async () => {
    renderWatch(<MailWatchDialog open onOpenChange={vi.fn()} account={ACCOUNT} />);
    await screen.findByText('First');
    fireEvent.change(screen.getByLabelText('Day', { selector: 'input' }), {
      target: { value: '2026-10-28' },
    });
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Rules' }));
    expect(await screen.findByRole('heading', { name: 'Rules' })).toBeTruthy();
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Day' }));
    await waitFor(() => {
      const field = screen.getByLabelText<HTMLInputElement>('Day', { selector: 'input' });
      expect(field.value).toBe('2026-10-28');
    });
  });

  it.each([
    ['Rules', 'Rules'],
    ['Labels', 'Labels'],
    ['Settings', 'Times'],
  ])('shows %s', async (tab, heading) => {
    renderWatch(<MailWatchDialog open onOpenChange={vi.fn()} account={ACCOUNT} />);
    fireEvent.mouseDown(await screen.findByRole('tab', { name: tab }));
    expect(await screen.findByRole('heading', { name: heading })).toBeTruthy();
  });

  it('does not take the focus back after the person has moved on', async () => {
    renderWatch(<MailWatchDialog open onOpenChange={vi.fn()} account={ACCOUNT} />);
    await screen.findByText('First');
    const rulesTab = screen.getByRole('tab', { name: 'Rules' });
    fireEvent.mouseDown(rulesTab);
    await screen.findByRole('heading', { name: 'Rules' });
    const dayTab = screen.getByRole('tab', { name: 'Day' });
    fireEvent.mouseDown(dayTab);
    dayTab.focus();
    await screen.findByText('First');
    expect(document.activeElement).toBe(dayTab);
  });
});

describe('axe', () => {
  it('finds nothing on a full day', async () => {
    renderWatch(<MailWatchDialog open onOpenChange={vi.fn()} account={ACCOUNT} />);
    const dialog = await screen.findByRole('dialog');
    await screen.findByText('First');
    await expect(violations(dialog)).resolves.toEqual([]);
  });

  it.each([['Rules'], ['Labels'], ['Settings']])('finds nothing on %s', async (tab) => {
    renderWatch(<MailWatchDialog open onOpenChange={vi.fn()} account={ACCOUNT} />);
    const dialog = await screen.findByRole('dialog');
    fireEvent.mouseDown(await screen.findByRole('tab', { name: tab }));
    await screen.findByRole('tabpanel');
    await waitFor(() => {
      expect(screen.queryByText('Loading…')).toBeNull();
    });
    await expect(violations(dialog)).resolves.toEqual([]);
  });
});
