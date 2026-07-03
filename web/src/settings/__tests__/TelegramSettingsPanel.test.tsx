import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { TelegramSettingsPanel } from '../TelegramSettingsPanel';

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

describe('TelegramSettingsPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('saves a bot token, checks availability, mints a QR, and checks link status', async () => {
    const calls: { url: string; method: string; body: string | undefined }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url = urlOf(input);
        const method = init?.method ?? 'GET';
        calls.push({ url, method, body: typeof init?.body === 'string' ? init.body : undefined });
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
                  has_value: false,
                  overridden: false,
                },
              ],
            }),
          );
        }
        if (url === '/api/settings/telegram/check') {
          return Promise.resolve(
            jsonResponse({ configured: true, available: true, botUsername: 'AuraBot' }),
          );
        }
        if (url === '/api/settings/TELEGRAM_BOT_TOKEN') {
          return Promise.resolve(jsonResponse({ ok: true }));
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

    render(<TelegramSettingsPanel />);

    expect(await screen.findByRole('heading', { name: 'Telegram' })).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Telegram bot token'), {
      target: { value: '123456:secret-token' },
    });

    fireEvent.click(screen.getByRole('button', { name: 'Check availability' }));
    expect(await screen.findByText('Bot available: @AuraBot')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Save Telegram token' }));
    expect(
      await screen.findByText('Telegram token saved. Restart Aura to start the bot with it.'),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Create Telegram QR' }));
    expect(await screen.findByAltText('Telegram setup QR code')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Open in Telegram' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Check link status' }));
    expect(await screen.findByText('Telegram linked.')).toBeTruthy();

    await waitFor(() => {
      expect(calls.some((call) => call.url === '/api/settings/telegram/check')).toBe(true);
      expect(calls.some((call) => call.url === '/api/settings/TELEGRAM_BOT_TOKEN')).toBe(true);
      expect(calls.some((call) => call.url === '/api/settings/telegram/link')).toBe(true);
      expect(calls.some((call) => call.url === '/api/settings/telegram/sess-1/status')).toBe(true);
    });
  });
});
