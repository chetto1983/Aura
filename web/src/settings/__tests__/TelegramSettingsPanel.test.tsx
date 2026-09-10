import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { TelegramSettingsPanel } from '../TelegramSettingsPanel';

// Saving the bot token hot-starts the Telegram channel, and the save says whether it came up
// (channel_active / channel_error). The panel used to answer every save with "restart Aura",
// which an appliance with no monitor and no SSH cannot do: no copy here may ask for one.

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

const AVAILABLE = { configured: true, available: true, botUsername: 'AuraBot' };
const ACTIVE = { key: 'TELEGRAM_BOT_TOKEN', channel_active: true, restart_required: false };

interface Routes {
  readonly overridden?: boolean;
  readonly check?: Record<string, unknown>;
  readonly put?: Response;
  readonly del?: Response;
}

function stubFetch(routes: Routes = {}) {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = urlOf(input);
      const method = init?.method ?? 'GET';
      calls.push(`${method} ${url}`);
      if (url === '/api/settings') {
        return Promise.resolve(
          jsonResponse({
            restart_required: false,
            settings: [
              {
                key: 'TELEGRAM_BOT_TOKEN',
                label: 'Telegram bot token',
                kind: 'string',
                secret: true,
                value: '',
                has_value: routes.overridden === true,
                overridden: routes.overridden === true,
              },
            ],
          }),
        );
      }
      if (url === '/api/settings/telegram/check') {
        return Promise.resolve(jsonResponse(routes.check ?? AVAILABLE));
      }
      if (url === '/api/settings/TELEGRAM_BOT_TOKEN') {
        const res = method === 'DELETE' ? routes.del : routes.put;
        return Promise.resolve((res ?? jsonResponse(ACTIVE)).clone());
      }
      if (url === '/api/settings/telegram/link') {
        return Promise.resolve(
          jsonResponse({
            sessionToken: 'sess-1',
            deepLink: 'https://t.me/AuraBot?start=onb-1',
            qrSvg: '<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>',
          }),
        );
      }
      if (url === '/api/settings/telegram/sess-1/status') {
        return Promise.resolve(jsonResponse({ linked: true }));
      }
      return Promise.resolve(jsonResponse({}));
    }),
  );
  return calls;
}

async function typeToken() {
  expect(await screen.findByRole('heading', { name: 'Telegram' })).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Telegram bot token'), {
    target: { value: '123456:secret-token' },
  });
}

function expectNoRestartAdvice() {
  expect(document.body.textContent).not.toMatch(/restart/i);
}

describe('TelegramSettingsPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // REWRITTEN with the backend contract, not to make it pass: the save step asserted "Restart
  // Aura to start the bot with it", the dead end this change removes.
  it('saves a bot token, reports the channel active, mints a QR, and checks link status', async () => {
    const calls = stubFetch();
    render(<TelegramSettingsPanel />);
    await typeToken();

    fireEvent.click(screen.getByRole('button', { name: 'Check availability' }));
    expect(await screen.findByText('Bot available: @AuraBot')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Save Telegram token' }));
    expect(await screen.findByText('Telegram channel active — @AuraBot')).toBeTruthy();
    expectNoRestartAdvice();

    fireEvent.click(screen.getByRole('button', { name: 'Create Telegram QR' }));
    expect(await screen.findByAltText('Telegram setup QR code')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open in Telegram' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Check link status' }));
    expect(await screen.findByText('Telegram linked.')).toBeTruthy();

    await waitFor(() => {
      expect(calls).toContain('POST /api/settings/telegram/check');
      expect(calls).toContain('PUT /api/settings/TELEGRAM_BOT_TOKEN');
      expect(calls).toContain('POST /api/settings/telegram/link');
      expect(calls).toContain('GET /api/settings/telegram/sess-1/status');
    });
  });

  it('names the bot after a save even when nobody pressed Check first', async () => {
    const calls = stubFetch();
    render(<TelegramSettingsPanel />);
    await typeToken();

    fireEvent.click(screen.getByRole('button', { name: 'Save Telegram token' }));

    expect(await screen.findByText('Telegram channel active — @AuraBot')).toBeTruthy();
    expect(calls).toContain('POST /api/settings/telegram/check');
  });

  it('shows why the channel did not start instead of asking for a restart', async () => {
    stubFetch({
      check: { ...AVAILABLE, requiresRestart: true },
      put: jsonResponse({
        key: 'TELEGRAM_BOT_TOKEN',
        channel_active: false,
        channel_error: 'getUpdates: conflict with another bot instance',
        restart_required: true,
      }),
    });
    render(<TelegramSettingsPanel />);
    await typeToken();

    fireEvent.click(screen.getByRole('button', { name: 'Save Telegram token' }));

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain("didn't start");
    expect(alert.textContent).toContain('getUpdates: conflict with another bot instance');
    expect(screen.queryByText('Telegram channel active — @AuraBot')).toBeNull();
    expectNoRestartAdvice();
  });

  it('says a token the channel is not polling with starts when saved', async () => {
    stubFetch({ check: { ...AVAILABLE, requiresRestart: true } });
    render(<TelegramSettingsPanel />);
    await typeToken();

    fireEvent.click(screen.getByRole('button', { name: 'Check availability' }));

    expect(
      await screen.findByText(
        "The Telegram channel isn't running with this token yet. Saving the token starts it.",
      ),
    ).toBeTruthy();
    expectNoRestartAdvice();
  });

  it('resets the token and drops the channel report without mentioning a restart', async () => {
    const calls = stubFetch({
      overridden: true,
      del: jsonResponse({
        key: 'TELEGRAM_BOT_TOKEN',
        deleted: true,
        restart_required: false,
        channel_active: false,
      }),
    });
    render(<TelegramSettingsPanel />);
    await typeToken();
    fireEvent.click(screen.getByRole('button', { name: 'Save Telegram token' }));
    await screen.findByText('Telegram channel active — @AuraBot');

    fireEvent.click(screen.getByRole('button', { name: 'Reset' }));

    await waitFor(() => {
      expect(screen.queryByText('Telegram channel active — @AuraBot')).toBeNull();
    });
    expect(calls).toContain('DELETE /api/settings/TELEGRAM_BOT_TOKEN');
    expectNoRestartAdvice();
  });

  it('blames the token, not a restart, when an action fails', async () => {
    stubFetch({
      overridden: true,
      del: jsonResponse({ error: 'settings store unavailable' }, 502),
    });
    render(<TelegramSettingsPanel />);
    expect(await screen.findByRole('heading', { name: 'Telegram' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Reset' }));

    expect((await screen.findByRole('alert')).textContent).toContain('Telegram setup failed');
    expectNoRestartAdvice();
  });
});
