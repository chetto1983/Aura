import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import type { OnboardingProvisionResponse, OnboardingStart } from '../onboardingApi';

// OnboardingWizard test — the full-screen wizard shell + data layer. onboardingApi is mocked so the
// suite can drive the linear flow (credentials + seed → capabilities → review → complete), the
// start loading/error/error-auth contract, the no-leak password invariant (the entered password
// value is never re-rendered as visible text anywhere, T-28-06-01), and Amendment #95's
// requirement that the typed seed reaches /provision byte-identical.

const startOnboarding = vi.fn();
const provisionOnboarding = vi.fn();
const fetchTelegramStatus = vi.fn();

vi.mock('../onboardingApi', () => ({
  startOnboarding: (...a: unknown[]) => startOnboarding(...a) as Promise<OnboardingStart>,
  provisionOnboarding: (...a: unknown[]) =>
    provisionOnboarding(...a) as Promise<OnboardingProvisionResponse>,
  fetchTelegramStatus: (...a: unknown[]) =>
    fetchTelegramStatus(...a) as Promise<{ linked: boolean }>,
}));

const { default: OnboardingWizard } = await import('../OnboardingWizard');

const SESSION_TOKEN = 'sess-abc123';
const PASSWORD = 'Sup3r-Secret-PW-987';
// A non-ASCII name proves the wire value is the typed value, not a normalised one.
const OPERATOR_NAME = 'José-María';

const START: OnboardingStart = {
  sessionToken: SESSION_TOKEN,
  capabilityOptions: ['skills.read', 'scheduler.read'],
};

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderWizard(onClose = vi.fn()) {
  return render(<OnboardingWizard onClose={onClose} />, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client()}>{children}</QueryClientProvider>
    ),
  });
}

async function fillCredentials() {
  await waitFor(() => {
    expect(screen.getByText('New operator credentials')).toBeTruthy();
  });
  fireEvent.change(screen.getByLabelText('Operator email'), {
    target: { value: 'new@example.com' },
  });
  fireEvent.change(screen.getByLabelText('Initial password'), { target: { value: PASSWORD } });
  fireEvent.change(screen.getByLabelText('Confirm initial password'), {
    target: { value: PASSWORD },
  });
  fireEvent.change(screen.getByLabelText('Security question'), {
    target: { value: 'First school?' },
  });
  fireEvent.change(screen.getByLabelText('Security answer'), { target: { value: 'blue' } });
}

function fillSeed() {
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: OPERATOR_NAME } });
  fireEvent.change(screen.getByLabelText('Language'), { target: { value: 'it' } });
  fireEvent.change(screen.getByLabelText('Where you are'), { target: { value: 'Caraglio' } });
  fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'Europe/Rome' } });
  fireEvent.change(screen.getByLabelText('What you do'), { target: { value: 'founder' } });
  fireEvent.change(screen.getByLabelText('Organisation'), { target: { value: 'PmSync' } });
}

async function advanceToReview() {
  await fillCredentials();
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
  await waitFor(() => {
    expect(screen.getByText('Capabilities for the new identity')).toBeTruthy();
  });
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
  await waitFor(() => {
    expect(screen.getByText('Review and create')).toBeTruthy();
  });
}

describe('OnboardingWizard', () => {
  beforeEach(() => {
    startOnboarding.mockReset();
    provisionOnboarding.mockReset();
    fetchTelegramStatus.mockReset();
    fetchTelegramStatus.mockResolvedValue({ linked: false });
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows the starting state, then the credentials step once /start resolves', async () => {
    startOnboarding.mockResolvedValue(START);
    renderWizard();

    expect(screen.getByText('Starting…')).toBeTruthy();
    await waitFor(() => {
      expect(screen.getByText('New operator credentials')).toBeTruthy();
    });
    // It is a modal dialog overlay.
    expect(screen.getByRole('dialog')).toBeTruthy();
  });

  it('renders a visible auth-error (never a blank) when /start 401s', async () => {
    startOnboarding.mockRejectedValue(new Error('HTTP 401'));
    renderWizard();
    await waitFor(() => {
      expect(screen.getByText('Your session expired. Sign in again to continue.')).toBeTruthy();
    });
    expect(screen.getByRole('alert')).toBeTruthy();
  });

  it('renders no-permission copy when /start 403s', async () => {
    startOnboarding.mockRejectedValue(new Error('HTTP 403'));
    renderWizard();
    await waitFor(() => {
      expect(screen.getByText("You don't have permission to create an identity.")).toBeTruthy();
    });
    expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull();
  });

  it('renders the error state with a Retry that re-calls /start', async () => {
    startOnboarding.mockRejectedValueOnce(new Error('HTTP 502')).mockResolvedValueOnce(START);
    renderWizard();
    await waitFor(() => {
      expect(
        screen.getByText(/Couldn't start onboarding\. The service may be unavailable\./),
      ).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.getByText('New operator credentials')).toBeTruthy();
    });
    expect(startOnboarding).toHaveBeenCalledTimes(2);
  });

  it('carries the typed seed into /provision byte-identical and completes without an interview', async () => {
    startOnboarding.mockResolvedValue(START);
    provisionOnboarding.mockResolvedValue({
      identityId: 'id-1',
      deepLink: 'https://t.me/AuraBot?start=onb-xyz',
      qrSvg: '<svg xmlns="http://www.w3.org/2000/svg"></svg>',
    });

    const { container } = renderWizard();

    await fillCredentials();
    fillSeed();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    // Capabilities phase — the interview phase no longer exists between it and review.
    await waitFor(() => {
      expect(screen.getByText('Capabilities for the new identity')).toBeTruthy();
    });
    expect(screen.queryByLabelText('Your answer')).toBeNull();
    fireEvent.click(screen.getByRole('checkbox', { name: /skills.read/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    // Review phase.
    await waitFor(() => {
      expect(screen.getByText('Review and create')).toBeTruthy();
    });
    expect(screen.getByText('new@example.com')).toBeTruthy();
    expect(screen.getAllByText('skills.read').length).toBeGreaterThan(0);

    // The entered password value must NEVER appear as visible text anywhere (no-leak).
    expect(container.textContent).not.toContain(PASSWORD);

    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));
    await waitFor(() => {
      expect(screen.getByText('Identity created')).toBeTruthy();
    });
    const link = await screen.findByRole('link', { name: 'Open in Telegram' });
    expect(link.getAttribute('href')).toBe('https://t.me/AuraBot?start=onb-xyz');
    expect(screen.getByRole('img', { name: 'QR code to link Telegram' })).toBeTruthy();

    expect(provisionOnboarding).toHaveBeenCalledWith(SESSION_TOKEN, {
      email: 'new@example.com',
      password: PASSWORD,
      securityQuestion: 'First school?',
      securityAnswer: 'blue',
      capabilities: ['skills.read'],
      linkTelegram: true,
      seed: {
        name: OPERATOR_NAME,
        lang: 'it',
        location: 'Caraglio',
        timezone: 'Europe/Rome',
        role: 'founder',
        company: 'PmSync',
      },
    });
    expect(container.textContent).not.toContain(PASSWORD);
  });

  it('provisions with an EMPTY seed when the optional fields are left blank', async () => {
    startOnboarding.mockResolvedValue(START);
    provisionOnboarding.mockResolvedValue({ identityId: 'id-1' });

    renderWizard();
    await advanceToReview();
    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));

    await waitFor(() => {
      expect(screen.getByText('Identity created')).toBeTruthy();
    });
    expect(provisionOnboarding).toHaveBeenCalledWith(
      SESSION_TOKEN,
      expect.objectContaining({ seed: {} }),
    );
    // No link was minted → the no-link note, not a broken CTA.
    expect(screen.getByText('No Telegram link was generated for this identity.')).toBeTruthy();
  });

  it('blocks Continue while a seed field exceeds its server cap, and shows the inline error', async () => {
    startOnboarding.mockResolvedValue(START);
    renderWizard();
    await fillCredentials();

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'x'.repeat(257) } });
    expect(screen.getByLabelText('Name').getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByRole('alert').textContent).toContain('too long');
    expect(screen.getByRole('button', { name: 'Continue' }).hasAttribute('disabled')).toBe(true);

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Davide' } });
    // aria-invalid is OMITTED when valid, never rendered as "false".
    expect(screen.getByLabelText('Name').hasAttribute('aria-invalid')).toBe(false);
    expect(screen.getByRole('button', { name: 'Continue' }).hasAttribute('disabled')).toBe(false);
  });

  it('navigates back from capabilities to credentials (Back) and keeps the seed', async () => {
    startOnboarding.mockResolvedValue(START);
    renderWizard();
    await fillCredentials();
    fillSeed();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await waitFor(() => {
      expect(screen.getByText('Capabilities for the new identity')).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    await waitFor(() => {
      expect(screen.getByText('New operator credentials')).toBeTruthy();
    });
    expect(screen.getByLabelText<HTMLInputElement>('Name').value).toBe(OPERATOR_NAME);
  });

  it('always provisions with Telegram enabled because recovery depends on it', async () => {
    startOnboarding.mockResolvedValue(START);
    provisionOnboarding.mockResolvedValue({
      identityId: 'id-1',
      deepLink: 'https://t.me/AuraBot?start=onb-xyz',
      qrSvg: '<svg xmlns="http://www.w3.org/2000/svg"></svg>',
    });

    renderWizard();
    await advanceToReview();
    expect(screen.getByText('Required for password reset')).toBeTruthy();
    expect(screen.queryByRole('checkbox')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));

    await waitFor(() => {
      expect(screen.getByText('Identity created')).toBeTruthy();
    });
    expect(await screen.findByRole('link', { name: 'Open in Telegram' })).toBeTruthy();
    expect(provisionOnboarding).toHaveBeenCalledWith(
      SESSION_TOKEN,
      expect.objectContaining({ linkTelegram: true, seed: {} }),
    );
  });

  it('closes the wizard via the header close button', async () => {
    startOnboarding.mockResolvedValue(START);
    const onClose = vi.fn();
    renderWizard(onClose);
    await waitFor(() => {
      expect(screen.getByText('New operator credentials')).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('maps a 403 from /provision to the no-permission copy (constructive CTA, distinct error)', async () => {
    startOnboarding.mockResolvedValue(START);
    provisionOnboarding.mockRejectedValue(new Error('HTTP 403'));

    renderWizard();
    await advanceToReview();
    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));
    await waitFor(() => {
      expect(screen.getByText("You don't have permission to create an identity.")).toBeTruthy();
    });
  });

  it('routes a 401 from /provision to the auth-expired state', async () => {
    startOnboarding.mockResolvedValue(START);
    provisionOnboarding.mockRejectedValue(new Error('HTTP 401'));

    renderWizard();
    await advanceToReview();
    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));
    await waitFor(() => {
      expect(screen.getByText('Your session expired. Sign in again to continue.')).toBeTruthy();
    });
  });
});
