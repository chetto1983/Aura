import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Input } from '../input';
import { Label } from '../label';

describe('Input', () => {
  it('renders at the 44px touch floor on the input token border', () => {
    render(<Input aria-label="source" />);
    const input = screen.getByLabelText('source');
    expect(input.className).toContain('min-h-[44px]');
    expect(input.className).toContain('border-input');
  });

  it('reflects aria-invalid to the danger border', () => {
    render(<Input aria-label="bad" aria-invalid />);
    expect(screen.getByLabelText('bad').getAttribute('aria-invalid')).toBe('true');
  });
});

describe('Label', () => {
  it('associates with a control via htmlFor', () => {
    render(
      <>
        <Label htmlFor="src">Source</Label>
        <Input id="src" />
      </>,
    );
    const label = screen.getByText('Source');
    expect(label.getAttribute('for')).toBe('src');
    expect(label.className).toContain('text-text');
  });
});
