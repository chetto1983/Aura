import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SecretInput } from '../secret-input';

describe('SecretInput', () => {
  it('keeps the reveal control at an exact 44px target', () => {
    render(<SecretInput showLabel="Show token" hideLabel="Hide token" />);

    const toggle = screen.getByRole('button', { name: 'Show token' });
    expect(toggle.className).toContain('min-h-[44px]');
    expect(toggle.className).toContain('w-[44px]');
  });
});
