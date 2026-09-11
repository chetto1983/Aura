import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

// The restart banner used to be advice that the operator of a headless appliance could not
// follow. When the daemon says it can come back by itself (restart_supported), the banner now
// carries the restart, and a blocking overlay holds the page until the new process answers.
// The /healthz timing rules live in restartAura.test.ts; this proves the wiring around them.

function listBody(restartSupported: boolean | undefined) {
  return {
    restart_required: true,
    restart_supported: restartSupported,
    restart_keys: ['AURA_STT_CLOUD_MODEL'],
    settings: [
      {
        key: 'AURA_STT_CLOUD_MODEL',
        label: 'AURA_STT_CLOUD_MODEL',
        kind: 'string',
        secret: false,
        value: 'whisper-persisted',
        has_value: true,
        overridden: true,
        applied: 'restart',
      },
    ],
  };
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

// /healthz never answers here: a box that stays down is the only path that can end in the
// timeout without reaching window.location.reload, which jsdom cannot perform.
function stubFetch(restartSupported: boolean | undefined, restart = jsonResponse(202, {})) {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = urlOf(input);
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      if (url === '/api/admin/restart') return Promise.resolve(restart.clone());
      if (url === '/healthz') return Promise.reject(new TypeError('Failed to fetch'));
      return Promise.resolve(jsonResponse(200, listBody(restartSupported)));
    }),
  );
  return calls;
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ModelSettingsPanel groups={['routing', 'tokens', 'backends']} />
    </QueryClientProvider>,
  );
}

// Real timers until the button is on screen (findBy* polls with them), fake ones after, so
// the two-minute deadline can be crossed without waiting for it.
async function startRestartUnderFakeTimers(calls: readonly string[]) {
  const button = await screen.findByRole('button', { name: 'Restart Aura' });
  vi.useFakeTimers();
  fireEvent.click(button);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(calls.filter((call) => call === 'POST /api/admin/restart')).toHaveLength(1);
}

async function crossTheDeadline() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(120_000);
  });
}

describe('ModelSettingsPanel — in-app restart', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it.each([
    ['says it cannot restart itself', false],
    ['is an older daemon that does not say', undefined],
  ])('offers no restart button when the daemon %s', async (_label, supported) => {
    stubFetch(supported);
    renderPanel();

    expect((await screen.findByRole('note')).textContent).toContain('AURA_STT_CLOUD_MODEL');
    expect(screen.queryByRole('button', { name: 'Restart Aura' })).toBeNull();
  });

  it('restarts from the banner and blocks the page while Aura comes back', async () => {
    const calls = stubFetch(true);
    renderPanel();

    const note = await screen.findByRole('note');
    fireEvent.click(await screen.findByRole('button', { name: 'Restart Aura' }));

    const overlay = await screen.findByRole('alertdialog', { name: 'Restarting Aura…' });
    expect(overlay.textContent).toContain('This page reloads by itself as soon as Aura is back.');
    expect(calls).toContain('POST /api/admin/restart');
    expect(note.textContent).toContain('AURA_STT_CLOUD_MODEL');
  });

  it('says so when this installation cannot restart after all', async () => {
    stubFetch(true, jsonResponse(409, { error: 'restart_unsupported' }));
    renderPanel();

    fireEvent.click(await screen.findByRole('button', { name: 'Restart Aura' }));

    expect(await screen.findByText('Restart is not available on this installation.')).toBeTruthy();
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('carries the server reason when the restart is refused', async () => {
    stubFetch(true, jsonResponse(403, { error: 'forbidden' }));
    renderPanel();

    fireEvent.click(await screen.findByRole('button', { name: 'Restart Aura' }));

    expect((await screen.findByRole('alert')).textContent).toContain('forbidden');
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('turns the overlay into an error after ~120s, and Try again restarts again', async () => {
    const calls = stubFetch(true);
    renderPanel();
    await startRestartUnderFakeTimers(calls);
    expect(screen.getByRole('alertdialog', { name: 'Restarting Aura…' })).toBeTruthy();

    await crossTheDeadline();

    const failed = screen.getByRole('alertdialog', { name: 'Aura did not come back' });
    expect(failed.textContent).toContain('Check the box or try again.');

    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(calls.filter((call) => call === 'POST /api/admin/restart')).toHaveLength(2);
    expect(screen.getByRole('alertdialog', { name: 'Restarting Aura…' })).toBeTruthy();
  });

  it('lets the operator close the timed-out overlay and get the settings back', async () => {
    const calls = stubFetch(true);
    renderPanel();
    await startRestartUnderFakeTimers(calls);
    await crossTheDeadline();

    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(screen.getByRole('button', { name: 'Restart Aura' })).toBeTruthy();
  });
});
