import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../i18n/i18n';
import { ShareToggle } from './ShareShell';

// ShareShell.test.tsx — covers every <behavior> row from 37F-14-PLAN.md Task 1, including the
// negative a11y assertion (aria-pressed absent) and the pointer-events-auto assertion (a defect
// no visual review would catch, T-37F-66).

describe('ShareToggle', () => {
  it('renders neutral with data-shared="false" when there is no share', () => {
    render(<ShareToggle hasInternalShare={false} hasPublicShare={false} onOpen={vi.fn()} />);
    const button = screen.getByRole('button', { name: 'Share conversation' });
    expect(button.getAttribute('data-shared')).toBe('false');
    expect(button.className).toContain('text-text-muted');
  });

  it('signals accent treatment with data-shared="internal" for an active internal share', () => {
    render(<ShareToggle hasInternalShare={true} hasPublicShare={false} onOpen={vi.fn()} />);
    const button = screen.getByRole('button', { name: 'Share conversation' });
    expect(button.getAttribute('data-shared')).toBe('internal');
  });

  it('signals the warning treatment with data-shared="public" for a live public share', () => {
    render(<ShareToggle hasInternalShare={false} hasPublicShare={true} onOpen={vi.fn()} />);
    const button = screen.getByRole('button', { name: 'Share conversation' });
    expect(button.getAttribute('data-shared')).toBe('public');
  });

  it('prioritizes the public warning when both an internal and a public share are active', () => {
    render(<ShareToggle hasInternalShare={true} hasPublicShare={true} onOpen={vi.fn()} />);
    const button = screen.getByRole('button', { name: 'Share conversation' });
    expect(button.getAttribute('data-shared')).toBe('public');
  });

  it('calls onOpen exactly once when clicked', () => {
    const onOpen = vi.fn();
    render(<ShareToggle hasInternalShare={false} hasPublicShare={false} onOpen={onOpen} />);
    fireEvent.click(screen.getByRole('button', { name: 'Share conversation' }));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it('announces itself as a dialog opener, never a pressed toggle', () => {
    render(<ShareToggle hasInternalShare={false} hasPublicShare={false} onOpen={vi.fn()} />);
    const button = screen.getByRole('button', { name: 'Share conversation' });
    expect(button.getAttribute('aria-haspopup')).toBe('dialog');
    // Negative assertion — ArtifactsToggle uses aria-pressed because it toggles a panel;
    // ShareToggle opens a dialog, so aria-pressed here would be an a11y bug (T-37F-67).
    expect(button.hasAttribute('aria-pressed')).toBe(false);
  });

  it('carries pointer-events-auto on the root (unclickable otherwise inside a pointer-events-none cluster)', () => {
    render(<ShareToggle hasInternalShare={false} hasPublicShare={false} onOpen={vi.fn()} />);
    const button = screen.getByRole('button', { name: 'Share conversation' });
    expect(button.className).toContain('pointer-events-auto');
  });

  it('has no visible text label — the icon is its only child', () => {
    render(<ShareToggle hasInternalShare={false} hasPublicShare={false} onOpen={vi.fn()} />);
    const button = screen.getByRole('button', { name: 'Share conversation' });
    expect(button.textContent).toBe('');
    expect(button.querySelector('svg')).toBeTruthy();
  });

  it('marks the icon as decorative (aria-hidden, not focusable)', () => {
    render(<ShareToggle hasInternalShare={false} hasPublicShare={false} onOpen={vi.fn()} />);
    const icon = screen.getByRole('button', { name: 'Share conversation' }).querySelector('svg');
    expect(icon?.getAttribute('aria-hidden')).toBe('true');
    expect(icon?.getAttribute('focusable')).toBe('false');
  });
});
