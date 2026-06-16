import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ErrorBoundary } from '../ErrorBoundary';

function Boom(): never {
  throw new Error('kaboom');
}

describe('ErrorBoundary', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders children when no error is thrown', () => {
    render(
      <ErrorBoundary>
        <p>healthy child</p>
      </ErrorBoundary>,
    );
    expect(screen.getByText('healthy child')).toBeTruthy();
  });

  it('swaps in the themed role=alert fallback when a child throws (no white screen, D-13)', () => {
    // React re-throws + logs the caught error to console.error; silence it here.
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>,
    );
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain('Aura could not render this view.');
    expect(alert.textContent).toContain('Reload to try again.');
  });
});
