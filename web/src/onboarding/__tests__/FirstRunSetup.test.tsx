import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { OnboardingProfileComplete } from '../onboardingApi';

// FirstRunSetup test (Amendment #95) — first access after sign-in. It proves:
//   - there is NO /start round-trip and NO interview: the form is the first thing rendered;
//   - the ONE submission happens LAST, after the Telegram token step, because the server mints
//     the deep-link before writing the profile and needs the saved bot token to resolve the name;
//   - the typed name reaches the API byte-identical;
//   - Skip walks on with an EMPTY seed (the server derives the skip) rather than short-circuiting
//     to a submission that would 502 on an instance with no bot configured;
//   - a 502 renders the retry, a 401 renders the auth-expired state.

const submitOnboardingProfile = vi.fn();

vi.mock('../onboardingApi', () => ({
  submitOnboardingProfile: (...a: unknown[]) =>
    submitOnboardingProfile(...a) as Promise<OnboardingProfileComplete>,
  fetchTelegramStatus: () => Promise.resolve({ linked: false }),
}));

const { default: FirstRunSetup } = await import('../FirstRunSetup');

const OPERATOR_NAME = 'José-María';

const COMPLETED: OnboardingProfileComplete = {
  completed: true,
  skipped: false,
  sessionToken: 'profile-session',
  deepLink: 'https://t.me/AuraBot?start=profile-onb',
  qrSvg: '<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>',
};

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderSetup(onClose = vi.fn()) {
  return render(<FirstRunSetup onClose={onClose} />, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client()}>{children}</QueryClientProvider>
    ),
  });
}

/** Walk the model step then the Telegram step, whose Continue triggers the single submission. */
async function walkToSubmit() {
  await waitFor(() => {
    expect(screen.getByRole('heading', { name: 'Model routing' })).toBeTruthy();
  });
  expect(submitOnboardingProfile).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

  await waitFor(() => {
    expect(screen.getByText(/Telegram bot @AuraBot is already configured/)).toBeTruthy();
  });
  // Still nothing submitted: the token must be in place BEFORE the mint-then-write server saga.
  expect(submitOnboardingProfile).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
}

describe('FirstRunSetup', () => {
  beforeEach(() => {
    submitOnboardingProfile.mockReset();
    submitOnboardingProfile.mockResolvedValue(COMPLETED);
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        // The Telegram step probes the stored token; return an already-configured bot so it
        // auto-surfaces "already configured" → Continue.
        if (String(url).includes('/api/settings/telegram/check')) {
          return Promise.resolve(
            new Response(
              JSON.stringify({
                configured: true,
                available: true,
                botUsername: 'AuraBot',
                requiresRestart: false,
              }),
              { status: 200 },
            ),
          );
        }
        return Promise.resolve(
          new Response(
            JSON.stringify({
              restart_required: false,
              settings: [
                {
                  key: 'AURA_LLM_MODEL',
                  label: 'Primary LLM model',
                  kind: 'string',
                  secret: false,
                  value: 'deepseek/deepseek-v4-flash:nitro',
                  has_value: true,
                  overridden: false,
                },
              ],
            }),
            { status: 200 },
          ),
        );
      }),
    );
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it('opens straight onto the seed form — no /start, no interview, no loading gate', () => {
    renderSetup();

    expect(screen.getByRole('dialog', { name: 'Set up your profile' })).toBeTruthy();
    expect(screen.getByLabelText('Name')).toBeTruthy();
    expect(screen.queryByLabelText('Your answer')).toBeNull();
    expect(screen.queryByText('Starting…')).toBeNull();
    expect(screen.getAllByText('Step 1 of 3').length).toBeGreaterThan(0);
  });

  it('submits the typed seed ONCE, at the end, byte-identical', async () => {
    renderSetup();

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: OPERATOR_NAME } });
    fireEvent.change(screen.getByLabelText('Language'), { target: { value: 'it' } });
    fireEvent.change(screen.getByLabelText('Where you are'), { target: { value: 'Caraglio' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await walkToSubmit();

    await waitFor(() => {
      expect(screen.getByText('Profile ready')).toBeTruthy();
    });
    expect(submitOnboardingProfile).toHaveBeenCalledTimes(1);
    expect(submitOnboardingProfile).toHaveBeenCalledWith({
      name: OPERATOR_NAME,
      lang: 'it',
      location: 'Caraglio',
    });
    expect(screen.getByRole('link', { name: 'Open in Telegram' }).getAttribute('href')).toBe(
      'https://t.me/AuraBot?start=profile-onb',
    );
    expect(screen.getByRole('img', { name: 'QR code to link Telegram' })).toBeTruthy();
  });

  it('Skip clears the seed, keeps the remaining steps, and submits an EMPTY seed', async () => {
    submitOnboardingProfile.mockResolvedValue({
      completed: false,
      skipped: true,
      sessionToken: 'profile-session',
    });
    renderSetup();

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Davide' } });
    fireEvent.click(screen.getByRole('button', { name: 'Skip profile setup' }));

    await walkToSubmit();

    await waitFor(() => {
      expect(screen.getByText(/Aura will build your profile from how you work/)).toBeTruthy();
    });
    expect(submitOnboardingProfile).toHaveBeenCalledWith({});
    // No deep-link was minted for a skip in this fixture → the no-link note, not a broken CTA.
    expect(screen.getByText('No Telegram link was generated for this identity.')).toBeTruthy();
  });

  it('blocks Continue while a seed field is over its cap', () => {
    renderSetup();
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'x'.repeat(257) } });
    expect(screen.getByRole('button', { name: 'Continue' }).hasAttribute('disabled')).toBe(true);
    // Skip is still reachable — an over-long draft must not trap the operator.
    expect(
      screen.getByRole('button', { name: 'Skip profile setup' }).hasAttribute('disabled'),
    ).toBe(false);
  });

  // The regression this guards: on an instance with no @BotFather token TelegramTokenStep renders
  // ONLY a Verify button that stays disabled until a token is typed. Without a skip beside it the
  // last step is a trap — the submission never fires, the skip sentinel is never written, and
  // AppShell re-opens this dialog on every login forever.
  it('reaches the submission from the Telegram step even when no bot is configured', async () => {
    submitOnboardingProfile.mockResolvedValue({
      completed: false,
      skipped: true,
      sessionToken: '',
    });
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        if (String(url).includes('/api/settings/telegram/check')) {
          return Promise.resolve(
            new Response(JSON.stringify({ configured: false, available: false }), { status: 200 }),
          );
        }
        return Promise.resolve(
          new Response(JSON.stringify({ restart_required: false, settings: [] }), { status: 200 }),
        );
      }),
    );
    renderSetup();

    fireEvent.click(screen.getByRole('button', { name: 'Skip profile setup' }));
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Model routing' })).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(screen.getByLabelText('Telegram bot token')).toBeTruthy();
    });
    // The only other control is a Verify button that is disabled while the field is empty.
    expect(screen.getByRole('button', { name: 'Verify token' }).hasAttribute('disabled')).toBe(
      true,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Skip Telegram setup' }));

    await waitFor(() => {
      expect(screen.getByText(/Aura will build your profile from how you work/)).toBeTruthy();
    });
    expect(submitOnboardingProfile).toHaveBeenCalledWith({});
  });

  it('renders a retry on a failed save and re-submits the SAME seed', async () => {
    submitOnboardingProfile.mockRejectedValueOnce(new Error('HTTP 502'));
    renderSetup();

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Davide' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await walkToSubmit();

    await waitFor(() => {
      expect(screen.getByText("Couldn't save the profile. Try again.")).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.getByText('Profile ready')).toBeTruthy();
    });
    expect(submitOnboardingProfile).toHaveBeenNthCalledWith(2, { name: 'Davide' });
  });

  it('renders the auth-expired state (never a blank) when the submission 401s', async () => {
    submitOnboardingProfile.mockRejectedValue(new Error('HTTP 401'));
    renderSetup();

    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await walkToSubmit();

    await waitFor(() => {
      expect(screen.getByText('Your session expired. Sign in again to continue.')).toBeTruthy();
    });
    expect(screen.getByRole('alert')).toBeTruthy();
  });

  it('closes via the header close button', () => {
    const onClose = vi.fn();
    renderSetup(onClose);
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
