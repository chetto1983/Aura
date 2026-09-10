import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import type { TelegramAvailability } from '../../settings/settingsApi';

// TelegramTokenStep test — the onboarding Telegram-integration step. It mocks the Settings
// telegram-check + putSetting seam to drive: the already-configured auto-skip (stored token
// valid → Continue → onDone), the manual token happy path (verify → getMe ok → save hot-starts
// the channel → active + Continue → onDone), the saved-but-not-started path (channel_active
// false → the server's reason + Retry re-saves), and the rejected-token path (getMe fails →
// invalid message, no save).

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

  // REWRITTEN with the backend contract, not to make it pass: saving the token used to leave
  // the channel dormant until a restart, and this test pinned the "Restart Aura" note that an
  // appliance with no monitor and no SSH could never act on. The save now hot-starts the bot.
  it('verifies a pasted token, saves it, reports the channel active, and Continue advances', async () => {
    checkTelegramAvailability.mockImplementation((token?: string) =>
      Promise.resolve(token === '123:ABC' ? AVAILABLE : NOT_CONFIGURED),
    );
    putSetting.mockResolvedValue({
      key: 'TELEGRAM_BOT_TOKEN',
      channel_active: true,
      restart_required: false,
    });
    const onDone = vi.fn();
    render(<TelegramTokenStep onDone={onDone} />);

    const input = await screen.findByLabelText(/telegram bot token/i);
    fireEvent.change(input, { target: { value: '123:ABC' } });
    fireEvent.click(screen.getByRole('button', { name: /verify token/i }));

    await screen.findByText('Telegram channel active — @AuraBot');
    expect(putSetting).toHaveBeenCalledWith('TELEGRAM_BOT_TOKEN', '123:ABC');
    expect(screen.queryByText(/restart/i)).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: /continue/i }));
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it('shows the saved-but-not-started state with the server reason, and Retry re-saves', async () => {
    checkTelegramAvailability.mockImplementation((token?: string) =>
      Promise.resolve(token === '123:ABC' ? AVAILABLE : NOT_CONFIGURED),
    );
    putSetting
      .mockResolvedValueOnce({
        key: 'TELEGRAM_BOT_TOKEN',
        channel_active: false,
        channel_error: 'getUpdates: conflict with another bot instance',
        restart_required: true,
      })
      .mockResolvedValueOnce({
        key: 'TELEGRAM_BOT_TOKEN',
        channel_active: true,
        restart_required: false,
      });
    const onDone = vi.fn();
    render(<TelegramTokenStep onDone={onDone} />);

    const input = await screen.findByLabelText(/telegram bot token/i);
    fireEvent.change(input, { target: { value: '123:ABC' } });
    fireEvent.click(screen.getByRole('button', { name: /verify token/i }));

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain("didn't start");
    expect(screen.getByText('getUpdates: conflict with another bot instance')).toBeTruthy();
    expect(screen.queryByText(/restart/i)).toBeNull();
    expect(screen.queryByRole('button', { name: /continue/i })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    await screen.findByText('Telegram channel active — @AuraBot');
    expect(putSetting).toHaveBeenCalledTimes(2);
    expect(putSetting).toHaveBeenLastCalledWith('TELEGRAM_BOT_TOKEN', '123:ABC');
    // getMe already accepted this token; Retry is about starting the channel, not re-checking it.
    expect(
      checkTelegramAvailability.mock.calls.filter(([token]) => token !== undefined),
    ).toHaveLength(1);
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
