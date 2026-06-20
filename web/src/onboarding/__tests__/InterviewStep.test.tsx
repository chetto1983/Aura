import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { InterviewStep } from '../InterviewStep';
import type { OnboardingStepResponse } from '../onboardingApi';

// InterviewStep test (ONBD-02 / D-03). Proves the active-question flow (answer -> onAnswer with the
// typed text) and the draft-review flow (confirm/edit/skip mapping to the right callbacks, the edit
// form re-render, skip advancing). Backend content/draft render as React-escaped text.

const ACTIVE: OnboardingStepResponse = {
  content: 'What should Aura call you?',
  step: 'identity',
  status: 'active',
};

const DRAFT: OnboardingStepResponse = {
  content: 'Draft ready. Confirm, edit, or skip.',
  step: 'draft',
  status: 'draft',
  draft: '# Agent.md\n\nName: Dav',
};

function handlers() {
  return {
    onAnswer: vi.fn(),
    onConfirm: vi.fn(),
    onEdit: vi.fn(),
    onSkip: vi.fn(),
  };
}

describe('InterviewStep (active question)', () => {
  it('renders the question and submits the typed answer via onAnswer (intent answer)', () => {
    const h = handlers();
    render(<InterviewStep step={ACTIVE} busy={false} {...h} />);

    expect(screen.getByText('What should Aura call you?')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: 'Call me Dav' } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    expect(h.onAnswer).toHaveBeenCalledWith('Call me Dav');
  });

  it('skip on an active step advances without a profile (intent skip)', () => {
    const h = handlers();
    render(<InterviewStep step={ACTIVE} busy={false} {...h} />);
    fireEvent.click(screen.getByRole('button', { name: 'Skip this step' }));
    expect(h.onSkip).toHaveBeenCalledTimes(1);
    expect(h.onAnswer).not.toHaveBeenCalled();
  });

  it('disables the CTAs while busy', () => {
    const h = handlers();
    render(<InterviewStep step={ACTIVE} busy {...h} />);
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Continue' }).disabled).toBe(true);
  });

  it('shows the empty-answer note when the backend content is blank', () => {
    const h = handlers();
    render(<InterviewStep step={{ ...ACTIVE, content: '' }} busy={false} {...h} />);
    expect(screen.getByText('No answer recorded for this step.')).toBeTruthy();
  });
});

describe('InterviewStep (draft review)', () => {
  it('renders the draft and maps confirm to onConfirm', () => {
    const h = handlers();
    render(<InterviewStep step={DRAFT} busy={false} {...h} />);
    expect(screen.getByText(/Name: Dav/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Looks right — continue' }));
    expect(h.onConfirm).toHaveBeenCalledTimes(1);
  });

  it('skip on the draft maps to onSkip', () => {
    const h = handlers();
    render(<InterviewStep step={DRAFT} busy={false} {...h} />);
    fireEvent.click(screen.getByRole('button', { name: 'Skip this step' }));
    expect(h.onSkip).toHaveBeenCalledTimes(1);
  });

  it('reveals the edit form and submits structured overrides via onEdit (intent edit)', () => {
    const h = handlers();
    render(<InterviewStep step={DRAFT} busy={false} {...h} />);

    // The edit toggle reveals the structured edit form.
    fireEvent.click(screen.getByRole('button', { name: 'Edit answer' }));
    // Two inputs appear (name + role-style fields) — fill BOTH so both overrides flow through.
    const [nameInput, roleInput] = screen.getAllByRole('textbox');
    if (nameInput === undefined || roleInput === undefined) {
      throw new Error('edit form inputs not found');
    }
    fireEvent.change(nameInput, { target: { value: 'Davide' } });
    fireEvent.change(roleInput, { target: { value: 'Engineer' } });

    // The form submit ("Save changes") dispatches the edit with the structured overrides.
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    expect(h.onEdit).toHaveBeenCalledWith({ name: 'Davide', role: 'Engineer' });
  });

  it('treats a response carrying a draft as draft mode even when status is not "draft"', () => {
    const h = handlers();
    render(
      <InterviewStep step={{ ...DRAFT, status: 'active' }} busy={false} {...h} />,
    );
    // The confirm CTA (draft-mode only) is present because a draft string exists.
    expect(screen.getByRole('button', { name: 'Looks right — continue' })).toBeTruthy();
  });
});
