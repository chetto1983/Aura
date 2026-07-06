import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import type { OnboardingStart, OnboardingStepResponse } from '../onboardingApi';

const startProfileOnboarding = vi.fn();
const stepOnboarding = vi.fn();
const completeProfileOnboarding = vi.fn();

vi.mock('../onboardingApi', () => ({
  startProfileOnboarding: (...a: unknown[]) =>
    startProfileOnboarding(...a) as Promise<OnboardingStart>,
  stepOnboarding: (...a: unknown[]) => stepOnboarding(...a) as Promise<OnboardingStepResponse>,
  completeProfileOnboarding: (...a: unknown[]) =>
    completeProfileOnboarding(...a) as Promise<{
      completed: boolean;
      skipped: boolean;
      deepLink?: string;
      qrSvg?: string;
    }>,
  fetchTelegramStatus: () => Promise.resolve({ linked: false }),
}));

const { default: ProfileOnboardingWizard } = await import('../ProfileOnboardingWizard');

const START: OnboardingStart = {
  sessionToken: 'profile-session',
  step: 'identity',
  content: 'What should Aura call you?',
  status: 'active',
  capabilityOptions: [],
};

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderWizard(onClose = vi.fn()) {
  return render(<ProfileOnboardingWizard onClose={onClose} />, {
    wrapper: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={client()}>{children}</QueryClientProvider>
    ),
  });
}

describe('ProfileOnboardingWizard', () => {
  beforeEach(() => {
    startProfileOnboarding.mockReset();
    stepOnboarding.mockReset();
    completeProfileOnboarding.mockReset();
    startProfileOnboarding.mockResolvedValue(START);
    completeProfileOnboarding.mockResolvedValue({
      completed: true,
      skipped: false,
      deepLink: 'https://t.me/AuraBot?start=profile-onb',
      qrSvg: '<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>',
    });
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        // The Telegram step probes the stored token via /api/settings/telegram/check; return an
        // already-configured bot so the wizard auto-surfaces "already configured" → Continue.
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
                {
                  key: 'AURA_LLM_BASE_URL',
                  label: 'Primary LLM base URL',
                  kind: 'string',
                  secret: false,
                  value: 'https://openrouter.ai/api/v1',
                  has_value: true,
                  overridden: false,
                },
                {
                  key: 'OPENROUTER_API_KEY',
                  label: 'OpenRouter API key',
                  kind: 'string',
                  secret: true,
                  value: '',
                  has_value: false,
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

  it('starts the current-profile interview without rendering credential fields', async () => {
    renderWizard();

    await waitFor(() => {
      expect(screen.getByText('Finish setting up Aura')).toBeTruthy();
    });
    expect(screen.getByRole('dialog', { name: 'Set up your profile' })).toBeTruthy();
    expect(screen.getAllByText('Step 1 of 8').length).toBeGreaterThan(0);
    expect(screen.getByText(/Tell Aura what to call you, your role, team or company/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Skip profile setup' })).toBeTruthy();
    expect(screen.queryByText('New operator credentials')).toBeNull();
    expect(startProfileOnboarding).toHaveBeenCalledTimes(1);
  });

  it('saves the current profile after the draft is confirmed', async () => {
    stepOnboarding
      .mockResolvedValueOnce({
        content: 'Draft ready.',
        step: 'draft',
        status: 'draft',
        draft: '# Agent.md\n\nName: Davide',
      })
      .mockResolvedValueOnce({
        content: 'Profile confirmed.',
        step: 'draft',
        status: 'completed',
        draft: '# Agent.md\n\nName: Davide',
      });

    renderWizard();

    await waitFor(() => {
      expect(screen.getByLabelText('Your answer')).toBeTruthy();
    });
    fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: 'Davide' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(screen.getByText(/Name: Davide/)).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: /Looks right.*continue/ }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Model routing' })).toBeTruthy();
    });
    expect(completeProfileOnboarding).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    // The model step now advances to the Telegram integration step; the stored bot is already
    // configured, so it auto-surfaces and its Continue triggers profile completion.
    await waitFor(() => {
      expect(screen.getByText(/Telegram bot @AuraBot is already configured/)).toBeTruthy();
    });
    expect(completeProfileOnboarding).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => {
      expect(screen.getByText('Profile ready')).toBeTruthy();
    });
    expect(screen.getByRole('link', { name: 'Open in Telegram' }).getAttribute('href')).toBe(
      'https://t.me/AuraBot?start=profile-onb',
    );
    expect(screen.getByRole('img', { name: 'QR code to link Telegram' })).toBeTruthy();
    expect(completeProfileOnboarding).toHaveBeenCalledWith('profile-session');
  });
});
