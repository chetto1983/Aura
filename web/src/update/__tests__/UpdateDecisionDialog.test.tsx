import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import i18n from '../../i18n/i18n';
import { toRFC3339 } from '../updateTime';
import {
  browserNow,
  AVAILABLE_REV,
  adminPending,
  at,
  clock,
  json,
  renderCenter,
  stubDaemon,
} from './harness';

// The admin's dialog, rendered through the real provider and layer against a fake daemon:
// every time below is the frozen browser clock of the harness (24 Sep 2026, 10:00 local).

async function openDialog(): Promise<HTMLElement> {
  return screen.findByRole('dialog', { name: 'Update available' });
}

function button(scope: HTMLElement, name: string | RegExp): HTMLButtonElement {
  return within(scope).getByRole('button', { name }) as HTMLButtonElement;
}

function deferGroup(dialog: HTMLElement): HTMLElement {
  return within(dialog).getByRole('group', { name: 'Postpone' });
}

describe('UpdateDecisionDialog', () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(browserNow);
  });

  afterEach(async () => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    await i18n.changeLanguage('en');
  });

  it('shows the build, how long it has waited, its deadline and what it would interrupt', async () => {
    stubDaemon(adminPending());
    renderCenter();

    const dialog = await openDialog();

    expect(within(dialog).getByText('abc1234de').tagName).toBe('CODE');
    const built = new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(
      new Date(2026, 8, 24, 7, 12),
    );
    expect(within(dialog).getByText('Built').nextElementSibling?.textContent).toBe(built);
    expect(within(dialog).getByText('Waiting for').nextElementSibling?.textContent).toBe('3 hours');
    expect(within(dialog).getByText(/^It will install itself/).textContent).toBe(
      `It will install itself at the first quiet moment, by ${clock(new Date(2026, 8, 25, 7))} tomorrow.`,
    );
    expect(within(dialog).getByText('Right now').nextElementSibling?.textContent).toBe(
      'last activity 3 minutes ago',
    );
    expect(dialog.querySelector('[data-kind]')?.getAttribute('data-kind')).toBe('recent');
    expect(button(dialog, 'Update now').disabled).toBe(false);
    expect(button(dialog, 'Later')).toBeTruthy();
    expect(within(dialog).queryByText('The last attempt did not succeed')).toBeNull();
  });

  it('previews each postponement on the wall clock', async () => {
    stubDaemon(adminPending());
    renderCenter();

    const group = deferGroup(await openDialog());

    expect(button(group, /^1 hour/).textContent).toBe(`1 hour${clock(new Date(2026, 8, 24, 11))}`);
    expect(button(group, /^4 hours/).textContent).toBe(
      `4 hours${clock(new Date(2026, 8, 24, 14))}`,
    );
    expect(button(group, /^Until tonight/).textContent).toBe(
      `Until tonight${clock(new Date(2026, 8, 25, 3))}`,
    );
  });

  it('counts the work an immediate update would cut short', async () => {
    stubDaemon(adminPending({ live_runs: 2 }));
    renderCenter();

    const dialog = await openDialog();

    expect(within(dialog).getByText('2 tasks in progress')).toBeTruthy();
    expect(dialog.querySelector('[data-kind]')?.getAttribute('data-kind')).toBe('running');
  });

  it('names a single task in the singular', async () => {
    stubDaemon(adminPending({ live_runs: 1 }));
    renderCenter();

    expect(within(await openDialog()).getByText('1 task in progress')).toBeTruthy();
  });

  it('says nobody is using Aura once it has been quiet past the idle window', async () => {
    stubDaemon(adminPending({ last_activity_at: at(24, 8) }));
    renderCenter();

    const dialog = await openDialog();

    expect(within(dialog).getByText('nobody is using Aura')).toBeTruthy();
    expect(dialog.querySelector('[data-kind]')?.getAttribute('data-kind')).toBe('idle');
  });

  it('leaves out what the daemon does not know', async () => {
    stubDaemon(adminPending({ available_built_at: null, pending_since: null, deadline: null }));
    renderCenter();

    const dialog = await openDialog();

    expect(
      within(dialog).getByText('It will install itself at the first quiet moment.'),
    ).toBeTruthy();
    expect(within(dialog).queryByText('Built')).toBeNull();
    expect(within(dialog).queryByText('Waiting for')).toBeNull();
    expect(button(deferGroup(dialog), /^Until tonight/).textContent).toBe(
      `Until tonight${clock(new Date(2026, 8, 25, 3))}`,
    );
  });

  it('applies now and hands over to the restart banner', async () => {
    const daemon = stubDaemon(adminPending());
    renderCenter();

    fireEvent.click(button(await openDialog(), 'Update now'));

    expect(
      await screen.findByText('Aura is updating in a few seconds: save what you are writing'),
    ).toBeTruthy();
    expect(daemon.posts).toEqual([{ url: '/api/system/update/apply', body: undefined }]);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('postpones by one hour with an absolute time and confirms the time the daemon kept', async () => {
    const daemon = stubDaemon(adminPending());
    renderCenter();

    fireEvent.click(button(deferGroup(await openDialog()), /^1 hour/));

    const notice = await screen.findByRole('dialog', { name: 'Update postponed' });
    const asked = toRFC3339(new Date(2026, 8, 24, 11));
    expect(daemon.posts).toEqual([{ url: '/api/system/update/defer', body: { until: asked } }]);
    expect(within(notice).getByText(/^No restart before/).textContent).toBe(
      `No restart before ${clock(new Date(2026, 8, 24, 11))} today.`,
    );
    expect(within(notice).queryByText(/as far as it can go/)).toBeNull();

    fireEvent.click(button(notice, 'OK'));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(screen.getByRole('button', { name: 'Update available' })).toBeTruthy();
  });

  it('asks for four hours from the same clock', async () => {
    const daemon = stubDaemon(adminPending());
    renderCenter();

    fireEvent.click(button(deferGroup(await openDialog()), /^4 hours/));

    await screen.findByRole('dialog', { name: 'Update postponed' });
    expect(daemon.posts[0]?.body).toEqual({ until: toRFC3339(new Date(2026, 8, 24, 14)) });
  });

  it('postpones until tonight across midnight', async () => {
    vi.setSystemTime(new Date(2026, 8, 24, 23, 30));
    const daemon = stubDaemon(adminPending({ deadline: at(25, 20) }));
    renderCenter();

    const group = deferGroup(await openDialog());
    expect(button(group, /^Until tonight/).textContent).toBe(
      `Until tonight${clock(new Date(2026, 8, 25, 3))}`,
    );
    fireEvent.click(button(group, /^Until tonight/));

    const notice = await screen.findByRole('dialog', { name: 'Update postponed' });
    expect(daemon.posts[0]?.body).toEqual({ until: toRFC3339(new Date(2026, 8, 25, 3)) });
    expect(within(notice).getByText(/^No restart before/).textContent).toBe(
      `No restart before ${clock(new Date(2026, 8, 25, 3))} tomorrow.`,
    );
  });

  it('shows the deadline the daemon clamps a long postponement to', async () => {
    const deadline = at(24, 12, 30);
    const daemon = stubDaemon(adminPending({ deadline }), (url) =>
      url.endsWith('/defer') ? json(202, { deferred_until: deadline }) : undefined,
    );
    renderCenter();

    const group = deferGroup(await openDialog());
    // The preview already stops at the deadline the daemon will enforce.
    expect(button(group, /^4 hours/).textContent).toBe(
      `4 hours${clock(new Date(2026, 8, 24, 12, 30))}`,
    );
    fireEvent.click(button(group, /^4 hours/));

    const notice = await screen.findByRole('dialog', { name: 'Update postponed' });
    expect(daemon.posts[0]?.body).toEqual({ until: toRFC3339(new Date(2026, 8, 24, 14)) });
    expect(within(notice).getByText(/^No restart before/).textContent).toBe(
      `No restart before ${clock(new Date(2026, 8, 24, 12, 30))} today.`,
    );
    expect(
      within(notice).getByText(
        'That is as far as it can go: the update has to be installed by then.',
      ),
    ).toBeTruthy();
  });

  it('falls back to the asked time when the daemon does not echo one', async () => {
    stubDaemon(adminPending(), (url) => (url.endsWith('/defer') ? json(202, {}) : undefined));
    renderCenter();

    fireEvent.click(button(deferGroup(await openDialog()), /^1 hour/));

    const notice = await screen.findByRole('dialog', { name: 'Update postponed' });
    expect(within(notice).getByText(/^No restart before/).textContent).toBe(
      `No restart before ${clock(new Date(2026, 8, 24, 11))} today.`,
    );
    expect(within(notice).queryByText(/as far as it can go/)).toBeNull();
  });

  it('shows the failure and offers to try again', async () => {
    const daemon = stubDaemon(
      adminPending({ state: 'failed', error: 'compose up: image not found' }),
    );
    renderCenter();

    const dialog = await openDialog();

    const alert = within(dialog).getByRole('alert');
    expect(within(alert).getByText('The last attempt did not succeed')).toBeTruthy();
    expect(within(alert).getByText('compose up: image not found')).toBeTruthy();
    expect(within(dialog).queryByRole('button', { name: 'Update now' })).toBeNull();

    fireEvent.click(button(dialog, 'Retry'));

    await waitFor(() => {
      expect(daemon.posts).toEqual([{ url: '/api/system/update/apply', body: undefined }]);
    });
  });

  it('shows a failure without a reason as the heading alone', async () => {
    stubDaemon(adminPending({ state: 'failed', error: '' }));
    renderCenter();

    const alert = within(await openDialog()).getByRole('alert');

    expect(alert.textContent).toBe('The last attempt did not succeed');
  });

  it('offers no postponement once the deadline has passed', async () => {
    stubDaemon(adminPending({ deadline: at(24, 9) }));
    renderCenter();

    const dialog = await openDialog();

    expect(
      within(dialog).getByText('Mandatory update: it will start at the first quiet moment'),
    ).toBeTruthy();
    expect(within(dialog).queryByRole('group', { name: 'Postpone' })).toBeNull();
    expect(within(dialog).queryByRole('button', { name: 'Later' })).toBeNull();
    expect(button(dialog, 'Update now')).toBeTruthy();

    // The footer's own Close, not the dialog's corner cross that shares its name.
    const close = within(dialog)
      .getAllByRole('button', { name: 'Close' })
      .find((el) => el.getAttribute('data-slot') === 'button');
    if (close === undefined) throw new Error('expected the footer Close button');
    fireEvent.click(close);

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(window.sessionStorage.getItem('aura.update.dismissedRev')).toBe(AVAILABLE_REV);
  });

  it.each([
    [409, 'The update changed state in the meantime; this is where it stands now.'],
    [403, 'Only an administrator can decide on updates.'],
    [500, 'Aura could not record the choice. Try again.'],
  ])('explains a refused apply (HTTP %i) and keeps the dialog', async (status, copy) => {
    stubDaemon(adminPending(), () => json(status, { error: 'refused' }));
    renderCenter();

    const dialog = await openDialog();
    fireEvent.click(button(dialog, 'Update now'));

    expect((await within(dialog).findByRole('alert')).textContent).toBe(copy);
    expect(button(dialog, 'Update now').disabled).toBe(false);
  });

  it('reads a request that never reached the daemon as a failure to retry', async () => {
    stubDaemon(adminPending(), () => new TypeError('Failed to fetch'));
    renderCenter();

    const dialog = await openDialog();
    fireEvent.click(button(dialog, 'Update now'));

    expect((await within(dialog).findByRole('alert')).textContent).toBe(
      'Aura could not record the choice. Try again.',
    );
  });

  it('explains a postponement the daemon found invalid', async () => {
    stubDaemon(adminPending(), () => json(400, { error: 'until must be a future time' }));
    renderCenter();

    const dialog = await openDialog();
    fireEvent.click(button(deferGroup(dialog), /^1 hour/));

    expect((await within(dialog).findByRole('alert')).textContent).toBe(
      'That time is no longer valid. Choose again.',
    );
    expect(screen.queryByRole('dialog', { name: 'Update postponed' })).toBeNull();
  });

  it('disables every choice while a decision is on its way', async () => {
    let release: (res: Response) => void = () => undefined;
    stubDaemon(adminPending());
    const answered = new Promise<Response>((resolve) => {
      release = resolve;
    });
    const fetchMock = vi.mocked(fetch);
    const answerGet = fetchMock.getMockImplementation();
    fetchMock.mockImplementation((input, init) =>
      init?.method === 'POST' ? answered : (answerGet?.(input, init) ?? answered),
    );
    renderCenter();

    const dialog = await openDialog();
    fireEvent.click(button(dialog, 'Update now'));

    await waitFor(() => {
      expect(button(dialog, 'Update now').getAttribute('aria-busy')).toBe('true');
    });
    expect(button(dialog, 'Update now').disabled).toBe(true);
    expect(button(deferGroup(dialog), /^1 hour/).disabled).toBe(true);
    expect(within(dialog).getByTestId('spinner')).toBeTruthy();

    release(json(409, {}));
    expect(await within(dialog).findByRole('alert')).toBeTruthy();
  });

  it('speaks Italian', async () => {
    await i18n.changeLanguage('it');
    stubDaemon(adminPending({ live_runs: 3 }));
    renderCenter();

    const dialog = await screen.findByRole('dialog', { name: 'Aggiornamento disponibile' });

    expect(
      within(dialog).getByText('Si installerà da solo alla prima pausa entro domani alle 07:00.'),
    ).toBeTruthy();
    expect(within(dialog).getByText('3 attività in corso')).toBeTruthy();
    expect(within(dialog).getByText('In attesa da').nextElementSibling?.textContent).toBe('3 ore');
    const group = within(dialog).getByRole('group', { name: 'Rimanda' });
    expect(button(group, /^1 ora/).textContent).toBe('1 ora11:00');
    expect(button(group, /^4 ore/).textContent).toBe('4 ore14:00');
    expect(button(group, /^Fino a stanotte/).textContent).toBe('Fino a stanotte03:00');
    expect(button(dialog, 'Aggiorna subito')).toBeTruthy();
    expect(button(dialog, 'Più tardi')).toBeTruthy();
  });
});
