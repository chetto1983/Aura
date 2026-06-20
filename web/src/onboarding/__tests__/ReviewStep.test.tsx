import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ReviewStep } from '../ReviewStep';
import type { ProvisionErrorKind } from '../onboardingWizardModel';

// ReviewStep test (ONBD-01a / T-28-06-04). The review summary shows email + chosen capabilities +
// the Telegram-link choice, a CONSTRUCTIVE Create CTA (accent, NOT danger-styled), the in-flight
// state, and the THREE distinct provision-error copy paths (403 no-capability / 409 duplicate-or-
// empty / rolled-back). The password is NEVER part of this surface (no-leak).

function renderReview(overrides: Partial<React.ComponentProps<typeof ReviewStep>> = {}) {
  const onToggleTelegram = vi.fn();
  const onCreate = vi.fn();
  const props: React.ComponentProps<typeof ReviewStep> = {
    email: 'new@example.com',
    capabilities: ['skills.read', 'scheduler.read'],
    linkTelegram: true,
    onToggleTelegram,
    provisioning: false,
    error: undefined,
    onCreate,
    ...overrides,
  };
  render(<ReviewStep {...props} />);
  return { onToggleTelegram, onCreate };
}

describe('ReviewStep (ONBD-01a)', () => {
  it('shows the email, chosen capabilities, and the Telegram-link choice', () => {
    renderReview();
    expect(screen.getByText('new@example.com')).toBeTruthy();
    expect(screen.getByText('skills.read')).toBeTruthy();
    expect(screen.getByText('scheduler.read')).toBeTruthy();
    expect(screen.getByText('A link will be generated')).toBeTruthy();
  });

  it('renders "None" when no capabilities were chosen', () => {
    renderReview({ capabilities: [] });
    expect(screen.getByText('None')).toBeTruthy();
  });

  it('the Create CTA is constructive (accent, not danger) and fires onCreate', () => {
    const { onCreate } = renderReview();
    const cta = screen.getByRole('button', { name: 'Create identity' });
    // Constructive: the reserved accent background, never a danger token.
    expect(cta.className).toContain('bg-accent');
    expect(cta.className).not.toContain('danger');
    fireEvent.click(cta);
    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it('shows the in-flight "Creating identity…" label and disables the CTA while provisioning', () => {
    renderReview({ provisioning: true });
    const cta = screen.getByRole('button', { name: 'Creating identity…' });
    expect((cta as HTMLButtonElement).disabled).toBe(true);
    expect(cta.getAttribute('aria-busy')).toBe('true');
  });

  it('toggling the Telegram checkbox calls onToggleTelegram with the new value', () => {
    const { onToggleTelegram } = renderReview({ linkTelegram: false });
    expect(screen.getByText('Not linked')).toBeTruthy();
    fireEvent.click(screen.getByRole('checkbox'));
    expect(onToggleTelegram).toHaveBeenCalledWith(true);
  });

  const ERROR_CASES: readonly { kind: ProvisionErrorKind; copy: string }[] = [
    { kind: 'noCapability', copy: "You don't have permission to create an identity." },
    { kind: 'duplicate', copy: 'That email is empty or already in use. Choose another.' },
    {
      kind: 'rolledBack',
      copy: "Couldn't finish creating the identity, so nothing was saved. Try again.",
    },
  ];

  it.each(ERROR_CASES)('renders the distinct $kind error copy', ({ kind, copy }) => {
    renderReview({ error: kind });
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toBe(copy);
  });

  it('renders no alert when there is no error', () => {
    renderReview({ error: undefined });
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('never renders a password (the review surface holds no secret)', () => {
    const { container } = render(
      <ReviewStep
        email="new@example.com"
        capabilities={['skills.read']}
        linkTelegram
        onToggleTelegram={vi.fn()}
        provisioning={false}
        error={undefined}
        onCreate={vi.fn()}
      />,
    );
    // There is no password input or password text anywhere in the review step.
    expect(container.querySelector('input[type="password"]')).toBeNull();
  });
});
