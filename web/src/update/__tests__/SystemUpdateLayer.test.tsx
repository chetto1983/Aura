import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import i18n from '../../i18n/i18n';
import { UpdateIndicator } from '../UpdateIndicator';
import {
  browserNow,
  AVAILABLE_REV,
  adminPending,
  at,
  json,
  member,
  renderCenter,
  stubDaemon,
} from './harness';

const DISMISSED_KEY = 'aura.update.dismissedRev';
const BANNER = 'Aura is updating in a few seconds: save what you are writing';

// Lets the answered poll reach the components before asserting that they stayed empty.
async function settle() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20));
  });
}

describe('SystemUpdateLayer — what everyone sees', () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(browserNow);
  });

  afterEach(async () => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    await i18n.changeLanguage('en');
  });

  it('warns a member that the restart is seconds away without blocking the page', async () => {
    stubDaemon(member('requested'));
    renderCenter();

    const banner = await screen.findByRole('status');

    expect(banner.textContent).toBe(BANNER);
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('holds the page behind a full-screen overlay while the update applies', async () => {
    stubDaemon(member('applying'));
    renderCenter();

    const overlay = await screen.findByRole('alertdialog', { name: 'Aura is updating…' });

    expect(overlay.getAttribute('aria-modal')).toBe('true');
    expect(within(overlay).getByText('Back in about 2 minutes')).toBeTruthy();
    expect(overlay.className).toContain('update-overlay');
    expect(screen.queryByText(BANNER)).toBeNull();

    fireEvent.keyDown(overlay, { key: 'Escape' });
    fireEvent.pointerDown(document.body);
    expect(screen.getByRole('alertdialog', { name: 'Aura is updating…' })).toBeTruthy();
  });

  // Seen live on the lab VM (2026-09-24): with the card's -50% translate left on a full-screen
  // element, its centre sat in the viewport's top-left corner and the title was cut off.
  it('lays the overlay over the whole viewport, not where the shared Dialog centres a card', async () => {
    stubDaemon(member('applying'));
    renderCenter();

    const classes = (await screen.findByRole('alertdialog')).className.split(' ');

    expect(classes).toEqual(
      expect.arrayContaining([
        'left-0',
        'top-0',
        'translate-x-0',
        'translate-y-0',
        'w-screen',
        'h-dvh',
      ]),
    );
    for (const centring of [
      'left-[50%]',
      'top-[50%]',
      'translate-x-[-50%]',
      'translate-y-[-50%]',
      'max-w-lg',
    ]) {
      expect(classes).not.toContain(centring);
    }
  });

  it('shows a member nothing while a build merely waits', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify(member('pending')), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderCenter();

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    await settle();

    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.queryByRole('status')).toBeNull();
    expect(container.querySelector('header')?.childElementCount).toBe(0);
  });

  it('renders nothing on a stack without an updater', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify({ managed: false }), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);
    const { container } = renderCenter();

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    await settle();

    expect(container.textContent).toBe('');
    expect(document.body.querySelector('[role="dialog"], [role="alertdialog"]')).toBeNull();
  });

  it('stays off the onboarding screens, even mid-restart', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify(member('applying')), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);
    renderCenter(true);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    await settle();

    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('speaks Italian in the banner and the overlay', async () => {
    await i18n.changeLanguage('it');
    stubDaemon(member('requested'));
    const first = renderCenter();

    expect((await screen.findByRole('status')).textContent).toBe(
      'Aura si aggiorna tra pochi secondi: salva quello che stai scrivendo',
    );
    first.unmount();

    stubDaemon(member('applying'));
    renderCenter();

    const overlay = await screen.findByRole('alertdialog', { name: 'Aura si sta aggiornando…' });
    expect(within(overlay).getByText('Torna disponibile in circa 2 minuti')).toBeTruthy();
  });
});

describe('SystemUpdateLayer — the admin decision', () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(browserNow);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('opens by itself and hides the header indicator while it is open', async () => {
    stubDaemon(adminPending());
    renderCenter();

    expect(await screen.findByRole('dialog', { name: 'Update available' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Update available' })).toBeNull();
  });

  it('treats closing as no decision: shut for this build, reopened from the header', async () => {
    const daemon = stubDaemon(adminPending());
    renderCenter();

    const dialog = await screen.findByRole('dialog', { name: 'Update available' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Later' }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(window.sessionStorage.getItem(DISMISSED_KEY)).toBe(AVAILABLE_REV);
    expect(daemon.posts).toEqual([]);

    const indicator = screen.getByRole('button', { name: 'Update available' });
    expect(indicator.getAttribute('data-state')).toBe('pending');
    fireEvent.click(indicator);

    expect(await screen.findByRole('dialog', { name: 'Update available' })).toBeTruthy();
  });

  it('closes on Escape exactly like Later', async () => {
    stubDaemon(adminPending());
    renderCenter();

    const dialog = await screen.findByRole('dialog', { name: 'Update available' });
    fireEvent.keyDown(dialog, { key: 'Escape' });

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(window.sessionStorage.getItem(DISMISSED_KEY)).toBe(AVAILABLE_REV);
  });

  it('does not reopen a build dismissed earlier in the session', async () => {
    window.sessionStorage.setItem(DISMISSED_KEY, AVAILABLE_REV);
    stubDaemon(adminPending());
    renderCenter();

    expect(await screen.findByRole('button', { name: 'Update available' })).toBeTruthy();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('asks again about a newer build than the one dismissed', async () => {
    window.sessionStorage.setItem(DISMISSED_KEY, 'an-older-build');
    stubDaemon(adminPending());
    renderCenter();

    expect(await screen.findByRole('dialog', { name: 'Update available' })).toBeTruthy();
  });

  it('keeps working when the browser refuses session storage', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError');
    });
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError');
    });
    stubDaemon(adminPending());
    renderCenter();

    const dialog = await screen.findByRole('dialog', { name: 'Update available' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Later' }));

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(setItem).toHaveBeenCalledWith(DISMISSED_KEY, AVAILABLE_REV);
    expect(screen.getByRole('button', { name: 'Update available' })).toBeTruthy();
  });

  it('holds back while a postponement runs, and shows it when reopened', async () => {
    stubDaemon(adminPending({ deferred_until: at(24, 11, 30) }));
    renderCenter();

    fireEvent.click(await screen.findByRole('button', { name: 'Update available' }));

    const dialog = await screen.findByRole('dialog', { name: 'Update available' });
    expect(within(dialog).getByText(/^Postponed: no restart before/).textContent).toBe(
      `Postponed: no restart before ${new Intl.DateTimeFormat('en', { timeStyle: 'short' }).format(
        new Date(2026, 8, 24, 11, 30),
      )} today.`,
    );
  });

  it('asks again once a postponement has run out', async () => {
    stubDaemon(adminPending({ deferred_until: at(24, 9, 30) }));
    renderCenter();

    const dialog = await screen.findByRole('dialog', { name: 'Update available' });
    expect(within(dialog).queryByText(/^Postponed/)).toBeNull();
  });

  it('forgets a dismissal once a decision is made, so an expired postponement asks again', async () => {
    window.sessionStorage.setItem(DISMISSED_KEY, AVAILABLE_REV);
    stubDaemon(adminPending());
    renderCenter();

    fireEvent.click(await screen.findByRole('button', { name: 'Update available' }));
    const dialog = await screen.findByRole('dialog', { name: 'Update available' });
    fireEvent.click(within(dialog).getByRole('button', { name: /^1 hour/ }));
    const notice = await screen.findByRole('dialog', { name: 'Update postponed' });

    expect(window.sessionStorage.getItem(DISMISSED_KEY)).toBeNull();
    fireEvent.keyDown(notice, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(screen.getByRole('button', { name: 'Update available' })).toBeTruthy();
  });

  it('never leaves a postponement notice over a newer build', async () => {
    stubDaemon(adminPending(), (url, body, daemon) => {
      if (!url.endsWith('/defer')) return undefined;
      daemon.current = adminPending({ available_rev: 'fedcba9876543210' });
      return json(202, { deferred_until: (body as { until: string }).until });
    });
    renderCenter();

    const dialog = await screen.findByRole('dialog', { name: 'Update available' });
    fireEvent.click(within(dialog).getByRole('button', { name: /^1 hour/ }));

    expect(await screen.findByText('fedcba987')).toBeTruthy();
    expect(screen.queryByRole('dialog', { name: 'Update postponed' })).toBeNull();
    expect(screen.getByRole('dialog', { name: 'Update available' })).toBeTruthy();
  });

  it('flags a failed build in the header', async () => {
    window.sessionStorage.setItem(DISMISSED_KEY, AVAILABLE_REV);
    stubDaemon(adminPending({ state: 'failed', error: 'boom' }));
    renderCenter();

    const indicator = await screen.findByRole('button', { name: 'Update failed' });

    expect(indicator.getAttribute('data-state')).toBe('failed');
    expect(indicator.getAttribute('title')).toBe('Update failed');
  });

  it('renders no indicator outside the update provider', () => {
    const { container } = render(<UpdateIndicator />);

    expect(container.childElementCount).toBe(0);
  });
});
