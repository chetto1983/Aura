import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Badge } from '../badge';

describe('Badge', () => {
  it('renders the default variant on the accent token', () => {
    render(<Badge>default</Badge>);
    expect(screen.getByText('default').className).toContain('bg-primary');
  });

  it.each([
    ['secondary', 'bg-secondary'],
    ['warning', 'bg-warning/15'],
    ['danger', 'bg-danger/15'],
    ['success', 'bg-success/15'],
  ] as const)('renders the %s status variant on its token tint', (variant, marker) => {
    render(<Badge variant={variant}>{variant}</Badge>);
    expect(screen.getByText(variant).className).toContain(marker);
  });

  it('keeps the token text colour on status variants', () => {
    render(<Badge variant="warning">w</Badge>);
    expect(screen.getByText('w').className).toContain('text-warning');
  });

  it('renders as a child element when asChild', () => {
    render(
      <Badge asChild>
        <a href="/x">chip</a>
      </Badge>,
    );
    const link = screen.getByRole('link', { name: 'chip' });
    expect(link.tagName).toBe('A');
    expect(link.className).toContain('bg-primary');
  });
});
