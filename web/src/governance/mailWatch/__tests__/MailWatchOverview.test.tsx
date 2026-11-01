import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, within } from '@testing-library/react';
import '../../../i18n/i18n';
import { hang, mailWatchServer, renderWatch, watchStatus } from '../../../test/mailWatchServer';
import { MailWatchOverview } from '../MailWatchOverview';

// The line under the account list: the watch's terms, and a calm word when there is no watch to
// have. A deployment without the PIM is a fact about it, never an error.

const ROUTE = 'GET /api/mail-watch';

let server: ReturnType<typeof mailWatchServer>;
beforeEach(() => {
  server = mailWatchServer();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe('MailWatchOverview', () => {
  it('states the terms of the watch', async () => {
    renderWatch(<MailWatchOverview />);
    expect(await screen.findByText(/Digest at 18:00, at most 5 alerts a day/)).toBeTruthy();
  });

  it('pads the hour', async () => {
    server.on(ROUTE, () => ({
      body: watchStatus({ settings: { ...watchStatus().settings, digest_hour: 7 } }),
    }));
    renderWatch(<MailWatchOverview />);
    expect(await screen.findByText(/Digest at 07:00/)).toBeTruthy();
  });

  it('says plainly what a cap of 0 means', async () => {
    server.on(ROUTE, () => ({
      body: watchStatus({ settings: { ...watchStatus().settings, daily_alert_cap: 0 } }),
    }));
    renderWatch(<MailWatchOverview />);
    expect(await screen.findByText(/with no alerts/)).toBeTruthy();
  });

  it('opens what accounts share, with no day to show', async () => {
    renderWatch(<MailWatchOverview />);
    fireEvent.click(await screen.findByRole('button', { name: 'Rules, labels and times' }));
    const dialog = await screen.findByRole('dialog', { name: 'Mail and calendar watch' });
    const tabs = within(dialog)
      .getAllByRole('tab')
      .map((tab) => tab.textContent);
    expect(tabs).toEqual(['Rules', 'Labels', 'Settings']);
  });

  it('shows the reason calmly when the deployment cannot watch', async () => {
    server.on(ROUTE, () => ({
      body: watchStatus({ configured: false, reason: 'pim_not_configured', accounts: [] }),
    }));
    renderWatch(<MailWatchOverview />);
    const note = await screen.findByRole('note');
    expect(note.textContent).toContain('The watch is not available on this installation.');
    expect(note.textContent).toContain('It needs the PIM service and its authorization');
    expect(note.textContent).not.toContain('pim_not_configured');
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('says nothing more for a reason it cannot read', async () => {
    server.on(ROUTE, () => ({
      body: watchStatus({ configured: false, reason: 'something_new', accounts: [] }),
    }));
    renderWatch(<MailWatchOverview />);
    expect((await screen.findByRole('note')).textContent).toBe(
      'The watch is not available on this installation.',
    );
  });

  it('does without a reason', async () => {
    server.on(ROUTE, () => ({ body: watchStatus({ configured: false, accounts: [] }) }));
    renderWatch(<MailWatchOverview />);
    expect((await screen.findByRole('note')).textContent).toBe(
      'The watch is not available on this installation.',
    );
  });

  it('reads a 503 the same calm way', async () => {
    server.on(ROUTE, () => ({ status: 503, body: { error: 'mail_watch_unavailable' } }));
    renderWatch(<MailWatchOverview />);
    expect((await screen.findByRole('note')).textContent).toMatch(/not available/);
  });

  it('tells any other failure apart', async () => {
    server.on(ROUTE, () => ({ status: 500, body: { error: 'mail_watch_failed' } }));
    renderWatch(<MailWatchOverview />);
    expect((await screen.findByRole('note')).textContent).toMatch(/cannot be read/);
  });

  it('draws nothing while it waits', () => {
    server.on(ROUTE, hang);
    const { container } = renderWatch(<MailWatchOverview />);
    expect(container.textContent).toBe('');
  });
});
