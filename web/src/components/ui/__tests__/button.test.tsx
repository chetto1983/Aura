import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Button } from '../button';

describe('Button', () => {
  it('renders the default variant with the 44px touch floor', () => {
    render(<Button>Stage</Button>);
    const btn = screen.getByRole('button', { name: 'Stage' });
    expect(btn.className).toContain('bg-primary');
    expect(btn.className).toContain('min-h-[44px]');
  });

  it.each([
    ['secondary', 'bg-secondary'],
    ['outline', 'border-border-strong'],
    ['ghost', 'hover:bg-surface-2'],
    ['destructive', 'bg-destructive'],
  ] as const)('renders the %s variant', (variant, marker) => {
    render(<Button variant={variant}>x</Button>);
    expect(screen.getByRole('button').className).toContain(marker);
  });

  it.each([
    ['sm', 'min-h-[44px]'],
    ['icon', 'h-11'],
  ] as const)('renders the %s size at the touch floor', (size, marker) => {
    render(<Button size={size}>i</Button>);
    expect(screen.getByRole('button').className).toContain(marker);
  });

  it('renders the icon size at 44×44', () => {
    render(<Button size="icon">i</Button>);
    const cls = screen.getByRole('button').className;
    expect(cls).toContain('h-11');
    expect(cls).toContain('w-11');
  });

  it('renders as a child element when asChild', () => {
    render(
      <Button asChild>
        <a href="/x">Link</a>
      </Button>,
    );
    const link = screen.getByRole('link', { name: 'Link' });
    expect(link.tagName).toBe('A');
    expect(link.className).toContain('bg-primary');
  });
});
