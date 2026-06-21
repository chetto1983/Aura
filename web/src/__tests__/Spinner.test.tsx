import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Spinner } from '../components/Spinner';

// Spinner test — the in-flight indicator is purely decorative: it must be aria-hidden (the
// host button carries aria-busy for assistive tech) and must disable its spin under
// prefers-reduced-motion (the motion-reduce convention shared with ContextBudgetGauge).
describe('Spinner', () => {
  it('renders a decorative, aria-hidden spinner', () => {
    render(<Spinner />);
    const el = screen.getByTestId('spinner');
    expect(el.getAttribute('aria-hidden')).toBe('true');
    expect(el.className).toContain('animate-spin');
  });

  it('disables the spin under prefers-reduced-motion', () => {
    render(<Spinner />);
    expect(screen.getByTestId('spinner').className).toContain('motion-reduce:animate-none');
  });
});
