import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { CitationBubble } from '../CitationBubble';
import type { DisplaySource } from '../types';

// CitationBubble (DISP-03 / D-04): the numbered chip is a real <button> that opens
// the hovercard on hover AND focus AND tap (hover is NEVER the only path, D-16);
// shows the source title + snippet; is accent when `cited`, neutral when only
// `consulted`; and fires onOpenSource(refId) on click.

function source(over: Partial<DisplaySource> = {}): DisplaySource {
  return {
    ref_id: 'src-1',
    index: 1,
    type: 'web_result',
    title: 'Weather forecast',
    snippet: 'Sunny with a high of 24C.',
    url: 'https://example.com/weather',
    cited: true,
    ...over,
  };
}

describe('CitationBubble (DISP-03 / D-04)', () => {
  it('renders the numbered chip as a <button> with the source aria-label', () => {
    render(<CitationBubble number={1} source={source()} />);
    const chip = screen.getByRole('button', { name: 'Source 1: Weather forecast' });
    expect(chip.tagName.toLowerCase()).toBe('button');
    expect(chip.textContent).toBe('1');
  });

  it('a `cited` chip is accent; a `consulted`-only chip is neutral (Color rule)', () => {
    const { rerender } = render(<CitationBubble number={1} source={source({ cited: true })} />);
    const cited = screen.getByRole('button');
    expect(cited.className).toContain('bg-accent');
    expect(cited.className).not.toContain('bg-surface-3');

    rerender(<CitationBubble number={1} source={source({ cited: false })} />);
    const consulted = screen.getByRole('button');
    expect(consulted.className).toContain('bg-surface-3');
    expect(consulted.className).not.toContain('bg-accent');
  });

  it('opens the hovercard on FOCUS (keyboard path — not hover-only, D-16)', async () => {
    render(<CitationBubble number={1} source={source()} />);
    const chip = screen.getByRole('button');
    fireEvent.focus(chip);
    await waitFor(() => {
      expect(screen.getByText('Weather forecast')).toBeTruthy();
    });
    expect(screen.getByText('Sunny with a high of 24C.')).toBeTruthy();
  });

  it('opens the hovercard on TAP/click (touch path) and fires onOpenSource', async () => {
    const onOpenSource = vi.fn();
    render(<CitationBubble number={1} source={source()} onOpenSource={onOpenSource} />);
    const chip = screen.getByRole('button');
    fireEvent.click(chip);
    // Tap reveals the preview...
    await waitFor(() => {
      expect(screen.getByText('Weather forecast')).toBeTruthy();
    });
    // ...and navigates to the source.
    expect(onOpenSource).toHaveBeenCalledWith('src-1');
  });

  it('shows the Cited tag for a cited source and Consulted for a consulted one', async () => {
    const { rerender } = render(<CitationBubble number={2} source={source({ cited: true })} />);
    fireEvent.focus(screen.getByRole('button'));
    await waitFor(() => {
      expect(screen.getByText('Cited')).toBeTruthy();
    });

    rerender(<CitationBubble number={2} source={source({ cited: false })} />);
    fireEvent.focus(screen.getByRole('button'));
    await waitFor(() => {
      expect(screen.getByText('Consulted')).toBeTruthy();
    });
  });

  it('falls back to a generic title when the source has none', () => {
    const noTitle: DisplaySource = { ref_id: 'src-x', index: 3, cited: true };
    render(<CitationBubble number={3} source={noTitle} />);
    expect(screen.getByRole('button', { name: 'Source 3: Untitled source' })).toBeTruthy();
  });

  it('does not throw when onOpenSource is omitted (click is a no-op nav)', () => {
    render(<CitationBubble number={1} source={source()} />);
    expect(() => {
      fireEvent.click(screen.getByRole('button'));
    }).not.toThrow();
  });
});
