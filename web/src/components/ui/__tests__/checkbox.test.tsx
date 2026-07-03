import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Checkbox } from '../checkbox';

describe('Checkbox', () => {
  it('keeps a compact 32px hit target around a 16px visual box', () => {
    render(<Checkbox aria-label="Include archived" />);

    const checkbox = screen.getByRole('checkbox', { name: 'Include archived' });
    expect(checkbox.className).toContain('size-[32px]');
    expect(checkbox.querySelector('[aria-hidden="true"]')?.className).toContain('size-4');
  });
});
