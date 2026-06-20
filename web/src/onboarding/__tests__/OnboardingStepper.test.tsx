import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { OnboardingStepper } from '../OnboardingStepper';

// OnboardingStepper test — asserts the per-step visual state (done/active/upcoming via data-state +
// aria-current) and the mobile "Step N of M" progress text, so the stepper's state-branch +
// index-math mutants are killable (the 28-03 mutation-hardening playbook: assert the rendered
// aria/data-state, not just className).

function steps() {
  return screen.getAllByRole('listitem');
}

describe('OnboardingStepper', () => {
  it('marks the active step and classifies the others done/upcoming (phaseIndex=2)', () => {
    render(<OnboardingStepper phaseIndex={2} />);
    const items = steps();
    expect(items).toHaveLength(5);
    expect(items.map((li) => li.getAttribute('data-state'))).toEqual([
      'done',
      'done',
      'active',
      'upcoming',
      'upcoming',
    ]);
    // The active step (interview) carries aria-current="step"; no other does.
    const current = screen.getAllByText(
      (_t, el) => el?.getAttribute('aria-current') === 'step',
    );
    expect(current).toHaveLength(1);
    expect(current[0]?.textContent).toBe('Interview');
  });

  it('the first step is active at phaseIndex=0 (none done)', () => {
    render(<OnboardingStepper phaseIndex={0} />);
    expect(steps().map((li) => li.getAttribute('data-state'))).toEqual([
      'active',
      'upcoming',
      'upcoming',
      'upcoming',
      'upcoming',
    ]);
  });

  it('the last step is active at phaseIndex=4 (all prior done)', () => {
    render(<OnboardingStepper phaseIndex={4} />);
    expect(steps().map((li) => li.getAttribute('data-state'))).toEqual([
      'done',
      'done',
      'done',
      'done',
      'active',
    ]);
  });

  it('the active dot uses the accent tone, a done dot uses success, an upcoming dot surface-3', () => {
    render(<OnboardingStepper phaseIndex={1} />);
    const [done, active, upcoming] = steps();
    if (done === undefined || active === undefined || upcoming === undefined) {
      throw new Error('expected at least three stepper items');
    }
    // dot is the first child span of each li.
    const dot = (li: Element) => li.querySelector('span[aria-hidden="true"]');
    expect(dot(done)?.className).toContain('bg-success'); // done
    expect(dot(active)?.className).toContain('bg-accent-text'); // active
    expect(dot(upcoming)?.className).toContain('bg-surface-3'); // upcoming
  });

  it('renders the mobile "Step N of M" progress with the active label', () => {
    render(<OnboardingStepper phaseIndex={3} />);
    // 1-based current = 4, total = 5; the active label is Review.
    expect(screen.getByText(/Step 4 of 5/)).toBeTruthy();
    // The active label appears (both the desktop strip + the mobile indicator render "Review").
    expect(screen.getAllByText('Review').length).toBeGreaterThan(0);
  });
});
