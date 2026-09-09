import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { ReviewStep } from '../ReviewStep';
import type { ProvisionErrorKind } from '../onboardingWizardModel';

// ReviewStep test (ONBD-01a / T-28-06-04 / CRED-02). The review summary shows email + the two
// fixed rows that replaced the capability picker (Access, Starting credit) + the required
// Telegram-link posture, a CONSTRUCTIVE Create CTA (accent, NOT danger-styled), the in-flight
// state, and the THREE distinct provision-error copy paths (403 no-capability / 409 duplicate-or-
// empty / rolled-back). The password is NEVER part of this surface (no-leak).

function renderReview(overrides: Partial<React.ComponentProps<typeof ReviewStep>> = {}) {
  const onCreate = vi.fn();
  const props: React.ComponentProps<typeof ReviewStep> = {
    email: 'new@example.com',
    provisioning: false,
    error: undefined,
    onCreate,
    ...overrides,
  };
  render(<ReviewStep {...props} />);
  return { onCreate };
}

describe('ReviewStep (ONBD-01a)', () => {
  it('shows the email and the required Telegram link posture', () => {
    renderReview();
    expect(screen.getByText('new@example.com')).toBeTruthy();
    expect(screen.getByText('Required for password reset')).toBeTruthy();
    expect(screen.queryByRole('checkbox')).toBeNull();
  });

  // RBAC-03 grants identity.UserSet() to every provisioned identity, so there is no per-identity
  // capability answer to render. The row states the uniform grant in prose instead — and the
  // badge list that used to carry per-capability chips is gone, not merely empty.
  it('states the uniform access grant as prose, with no badge list', () => {
    const { container } = render(
      <ReviewStep
        email="new@example.com"
        provisioning={false}
        error={undefined}
        onCreate={vi.fn()}
      />,
    );
    expect(screen.getByText('Access')).toBeTruthy();
    expect(screen.getByText(/Only user management stays admin-only\./)).toBeTruthy();
    expect(container.querySelector('[data-slot="badge"]')).toBeNull();
    expect(container.querySelector('ul')).toBeNull();
  });

  // CRED-02/D-09: the zero-cap start is the ONE place a "$0.00" is correct, because the sentence
  // keeps its consequence attached. The number alone would be the misleading form CRED-09 bans,
  // so the test pins the whole sentence, not the figure.
  it('states the zero starting credit together with its consequence', () => {
    renderReview();
    expect(screen.getByText('Starting credit')).toBeTruthy();
    expect(
      screen.getByText(
        "$0.00 — this identity can't run a turn until you add credit after creating it.",
      ),
    ).toBeTruthy();
  });

  it('the Create CTA is constructive (accent, not danger) and fires onCreate', () => {
    const { onCreate } = renderReview();
    const cta = screen.getByRole('button', { name: 'Create identity' });
    // Constructive: shadcn primary maps to the reserved accent token, never danger.
    expect(cta.className).toContain('bg-primary');
    expect(cta.getAttribute('data-slot')).toBe('button');
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
        provisioning={false}
        error={undefined}
        onCreate={vi.fn()}
      />,
    );
    // There is no password input or password text anywhere in the review step.
    expect(container.querySelector('input[type="password"]')).toBeNull();
  });
});
