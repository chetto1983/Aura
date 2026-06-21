import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../tooltip';

describe('Tooltip', () => {
  it('renders an accessible trigger', () => {
    render(
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger>info</TooltipTrigger>
          <TooltipContent>Helpful copy</TooltipContent>
        </Tooltip>
      </TooltipProvider>,
    );
    expect(screen.getByRole('button', { name: 'info' })).toBeTruthy();
  });

  it('renders the content when open', () => {
    render(
      <Tooltip open>
        <TooltipTrigger>info</TooltipTrigger>
        <TooltipContent>Helpful copy</TooltipContent>
      </Tooltip>,
    );
    // Radix renders the tooltip text into both the visible content node and an
    // sr-only live region, so assert at least one occurrence is present.
    expect(screen.getAllByText('Helpful copy').length).toBeGreaterThan(0);
  });
});
