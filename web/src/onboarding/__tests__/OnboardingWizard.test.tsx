import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import type {
  OnboardingProvisionResponse,
  OnboardingStart,
  OnboardingStepResponse,
} from '../onboardingApi';

// OnboardingWizard test — the full-screen wizard shell + data layer. onboardingApi is mocked so the
// suite can drive the linear flow (credentials → capabilities → interview → review → complete), the
// start loading/error/error-auth contract, and the no-leak password invariant (the entered
// password value is never re-rendered as visible text anywhere in the wizard, T-28-06-01).

const startOnboarding = vi.fn();
const stepOnboarding = vi.fn();
const provisionOnboarding = vi.fn();
const fetchTelegramStatus = vi.fn();

vi.mock('../onboardingApi', () => ({
  startOnboarding: (...a: unknown[]) => startOnboarding(...a) as Promise<OnboardingStart>,
  stepOnboarding: (...a: unknown[]) => stepOnboarding(...a) as Promise<OnboardingStepResponse>,
  provisionOnboarding: (...a: unknown[]) =>
    provisionOnboarding(...a) as Promise<OnboardingProvisionResponse>,
  fetchTelegramStatus: (...a: unknown[]) =>
    fetchTelegramStatus(...a) as Promise<{ linked: boolean }>,
}));

const { default: OnboardingWizard } = await import('../OnboardingWizard');

const SESSION_TOKEN = 'sess-abc123';
const PASSWORD = 'Sup3r-Secret-PW-987';

const START: OnboardingStart = {
  sessionToken: SESSION_TOKEN,
  step: 'identity',
  content: 'What should Aura call you?',
  status: 'active',
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

async function fillCredentialsAndAdvance() {
  await waitFor(() => {
    expect(screen.getByText('New operator credentials')).toBeTruthy();
  });
  fireEvent.change(screen.getByLabelText('Operator email'), {
    target: { value: 'new@example.com' },
  });
  fireEvent.change(screen.getByLabelText('Initial password'), { target: { value: PASSWORD } });
  // The Continue CTA (the credentials-phase WizardNav next button).
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
}

describe('OnboardingWizard', () => {
  beforeEach(() => {
    startOnboarding.mockReset();
    stepOnboarding.mockReset();
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

  it('drives the full flow to the Identity-created completion and never re-renders the password', async () => {
    startOnboarding.mockResolvedValue(START);
    // The interview: one answer advances to a terminal draft-confirmed flow. To keep the test
    // focused on the shell, the first answer returns a terminal completed status.
    stepOnboarding.mockResolvedValue({
      content: 'Profile confirmed.',
      step: 'draft',
      status: 'completed',
      draft: '# Agent.md',
    });
    provisionOnboarding.mockResolvedValue({
      identityId: 'id-1',
      deepLink: 'https://t.me/AuraBot?start=onb-xyz',
      qrSvg: '<svg xmlns="http://www.w3.org/2000/svg"></svg>',
    });

    const { container } = renderWizard();

    await fillCredentialsAndAdvance();

    // Capabilities phase.
    await waitFor(() => {
      expect(screen.getByText('Capabilities for the new identity')).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('checkbox', { name: /skills.read/ }));
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    // Interview phase — answer once → the mock returns a terminal status → advance to review.
    await waitFor(() => {
      expect(screen.getByLabelText('Your answer')).toBeTruthy();
    });
    fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: 'Call me Dav' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    // Review phase.
    await waitFor(() => {
      expect(screen.getByText('Review and create')).toBeTruthy();
    });
    expect(screen.getByText('new@example.com')).toBeTruthy();
    // The chosen capability is shown.
    expect(screen.getAllByText('skills.read').length).toBeGreaterThan(0);

    // The entered password value must NEVER appear as visible text anywhere (no-leak).
    expect(container.textContent).not.toContain(PASSWORD);

    // Create → /provision → completion.
    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));
    await waitFor(() => {
      expect(screen.getByText('Identity created')).toBeTruthy();
    });
    // The provision result's deep-link + QR flow into the Telegram step (optional-chaining wired).
    const link = await screen.findByRole('link', { name: 'Open in Telegram' });
    expect(link.getAttribute('href')).toBe('https://t.me/AuraBot?start=onb-xyz');
    expect(screen.getByRole('img', { name: 'QR code to link Telegram' })).toBeTruthy();
    expect(provisionOnboarding).toHaveBeenCalledWith(
      SESSION_TOKEN,
      expect.objectContaining({
        email: 'new@example.com',
        password: PASSWORD,
        capabilities: ['skills.read'],
        linkTelegram: true,
      }),
    );
    // Still no password in the DOM at completion.
    expect(container.textContent).not.toContain(PASSWORD);
  });

  it('navigates back from capabilities to credentials (Back)', async () => {
    startOnboarding.mockResolvedValue(START);
    renderWizard();
    await fillCredentialsAndAdvance();
    await waitFor(() => {
      expect(screen.getByText('Capabilities for the new identity')).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    await waitFor(() => {
      expect(screen.getByText('New operator credentials')).toBeTruthy();
    });
  });

  it('drives confirm then edit through the wizard (intent confirm/edit) before terminal', async () => {
    startOnboarding.mockResolvedValue(START);
    // 1st answer → draft (active draft); confirm → completed terminal.
    stepOnboarding
      .mockResolvedValueOnce({
        content: 'Draft ready.',
        step: 'draft',
        status: 'draft',
        draft: '# Agent.md\n\nName: Dav',
      })
      .mockResolvedValueOnce({
        content: 'Draft updated.',
        step: 'draft',
        status: 'draft',
        draft: '# Agent.md\n\nName: Davide',
      })
      .mockResolvedValueOnce({ content: 'done', step: 'draft', status: 'completed' });
    provisionOnboarding.mockResolvedValue({ identityId: 'id-1' });

    renderWizard();
    await fillCredentialsAndAdvance();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' })); // caps → interview

    await waitFor(() => {
      expect(screen.getByLabelText('Your answer')).toBeTruthy();
    });
    fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: 'Dav' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' })); // answer → draft

    // Draft mode: edit the draft (intent edit → re-renders).
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Edit answer' })).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Edit answer' }));
    // The edit form's first field (its label is reused from the credentials email label).
    fireEvent.change(screen.getByLabelText('Operator email'), { target: { value: 'Davide' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => {
      expect(screen.getByText(/Name: Davide/)).toBeTruthy();
    });
    expect(stepOnboarding).toHaveBeenCalledWith(SESSION_TOKEN, {
      intent: 'edit',
      answers: { name: 'Davide' },
    });

    // Confirm → terminal → review.
    fireEvent.click(screen.getByRole('button', { name: 'Looks right — continue' }));
    await waitFor(() => {
      expect(screen.getByText('Review and create')).toBeTruthy();
    });
    expect(stepOnboarding).toHaveBeenCalledWith(SESSION_TOKEN, { intent: 'confirm' });
  });

  it('completes WITHOUT a Telegram link when linkTelegram is unchecked', async () => {
    startOnboarding.mockResolvedValue(START);
    stepOnboarding.mockResolvedValue({ content: 'done', step: 'draft', status: 'skipped' });
    provisionOnboarding.mockResolvedValue({ identityId: 'id-1' });

    renderWizard();
    await fillCredentialsAndAdvance();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' })); // caps → interview
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Skip this step' })).toBeTruthy();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Skip this step' }));

    await waitFor(() => {
      expect(screen.getByText('Review and create')).toBeTruthy();
    });
    // Uncheck Telegram before creating.
    fireEvent.click(screen.getByRole('checkbox'));
    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));

    await waitFor(() => {
      expect(screen.getByText('Identity created')).toBeTruthy();
    });
    // No Telegram deep-link rendered (linkTelegram was false → no TelegramLinkStep).
    expect(screen.queryByRole('link', { name: 'Open in Telegram' })).toBeNull();
    expect(provisionOnboarding).toHaveBeenCalledWith(
      SESSION_TOKEN,
      expect.objectContaining({ linkTelegram: false }),
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
    stepOnboarding.mockResolvedValue({
      content: 'done',
      step: 'draft',
      status: 'skipped',
    });
    provisionOnboarding.mockRejectedValue(new Error('HTTP 403'));

    renderWizard();
    await fillCredentialsAndAdvance();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' })); // skip caps → interview

    await waitFor(() => {
      expect(screen.getByLabelText('Your answer')).toBeTruthy();
    });
    // Skip the interview → terminal → review.
    fireEvent.click(screen.getByRole('button', { name: 'Skip this step' }));
    await waitFor(() => {
      expect(screen.getByText('Review and create')).toBeTruthy();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Create identity' }));
    await waitFor(() => {
      expect(screen.getByText("You don't have permission to create an identity.")).toBeTruthy();
    });
  });
});
