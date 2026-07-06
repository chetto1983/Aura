import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import type { TelegramAvailability } from '../../settings/settingsApi';

// TelegramTokenStep test — the onboarding Telegram-integration step. It mocks the Settings
// telegram-check + putSetting seam to drive: the already-configured auto-skip (stored token
// valid → Continue → onDone), the manual token happy path (verify → getMe ok → save → valid +
// Continue → onDone), and the rejected-token path (getMe fails → invalid message, no save).

const checkTelegramAvailability = vi.fn();
const putSetting = vi.fn();

vi.mock('../../settings/settingsApi', () => ({
  checkTelegramAvailability: (...a: unknown[]) =>
    checkTelegramAvailability(...a) as Promise<unknown>,
  putSetting: (...a: unknown[]) => putSetting(...a) as Promise<unknown>,
}));

const { TelegramTokenStep } = await import('../TelegramTokenStep');

const AVAILABLE: TelegramAvailability = {
  configured: true,
  available: true,
  botUsername: 'AuraBot',
  requiresRestart: false,
};
const NOT_CONFIGURED: TelegramAvailability = {
  configured: false,
  available: false,
  requiresRestart: false,
};
const REJECTED: TelegramAvailability = {
  configured: true,
  available: false,
  requiresRestart: false,
  error: 'bot token validation failed',
};

// The mocks are argument-aware (not order-queued) so React StrictMode's double-invoked mount
// effect cannot desync the probe/verify responses: a no-arg call is the stored-token probe, a
// call WITH a token is the live verify.
beforeEach(() => {
  checkTelegramAvailability.mockReset();
  putSetting.mockReset();
});

describe('TelegramTokenStep', () => {
  it('auto-surfaces already-configured and Continue calls onDone', async () => {
    checkTelegramAvailability.mockImplementation((token?: string) =>
      Promise.resolve(token === undefined ? AVAILABLE : REJECTED),
    );
    const onDone = vi.fn();
    render(<TelegramTokenStep onDone={onDone} />);

    await screen.findByText(/already configured/i);
    fireEvent.click(screen.getByRole('button', { name: /continue/i }));
    expect(onDone).toHaveBeenCalledTimes(1);
    // The stored-token probe passes no argument.
    expect(checkTelegramAvailability).toHaveBeenCalledWith();
  });

  it('verifies a pasted token, saves it, and Continue advances', async () => {
    checkTelegramAvailability.mockImplementation((token?: string) =>
      Promise.resolve(token === '123:ABC' ? AVAILABLE : NOT_CONFIGURED),
    );
    putSetting.mockResolvedValue({});
    const onDone = vi.fn();
    render(<TelegramTokenStep onDone={onDone} />);

    const input = await screen.findByLabelText(/telegram bot token/i);
    fireEvent.change(input, { target: { value: '123:ABC' } });
    fireEvent.click(screen.getByRole('button', { name: /verify token/i }));

    await screen.findByText(/valid — bot @AuraBot/i);
    expect(putSetting).toHaveBeenCalledWith('TELEGRAM_BOT_TOKEN', '123:ABC');

    fireEvent.click(screen.getByRole('button', { name: /continue/i }));
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it('shows the invalid message and never saves when getMe rejects the token', async () => {
    checkTelegramAvailability.mockImplementation((token?: string) =>
      Promise.resolve(token === undefined ? NOT_CONFIGURED : REJECTED),
    );
    const onDone = vi.fn();
    render(<TelegramTokenStep onDone={onDone} />);

    const input = await screen.findByLabelText(/telegram bot token/i);
    fireEvent.change(input, { target: { value: 'bad-token' } });
    fireEvent.click(screen.getByRole('button', { name: /verify token/i }));

    await screen.findByRole('alert');
    await waitFor(() => {
      expect(putSetting).not.toHaveBeenCalled();
    });
    expect(onDone).not.toHaveBeenCalled();
  });
});
