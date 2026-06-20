import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { SeedFilterPanel } from '../SeedFilterPanel';
import type { GraphSchema } from '../types';

// SeedFilterPanel (left pane): the seed CTA, the live-schema filter toggles, and the read-only
// Cypher preview (display-only, NEVER an input — D-04/D-09).

const SCHEMA: GraphSchema = { labels: ['Entity', 'Document'], rel_types: ['MENTIONS'] };

function renderPanel(over: Partial<React.ComponentProps<typeof SeedFilterPanel>> = {}) {
  const props: React.ComponentProps<typeof SeedFilterPanel> = {
    schema: SCHEMA,
    activeLabels: new Set(),
    activeRelTypes: new Set(),
    query: 'MATCH (e:Entity) RETURN e',
    canSeed: true,
    onSeed: vi.fn(),
    onToggleLabel: vi.fn(),
    onToggleRelType: vi.fn(),
    ...over,
  };
  render(<SeedFilterPanel {...props} />);
  return props;
}

describe('SeedFilterPanel', () => {
  it('raises onSeed when the primary CTA is clicked', () => {
    const props = renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'Explore this conversation' }));
    expect(props.onSeed).toHaveBeenCalledTimes(1);
  });

  it('disables the seed CTA when canSeed is false', () => {
    renderPanel({ canSeed: false });
    const seed = screen.getByRole('button', { name: 'Explore this conversation' });
    expect(seed).toHaveProperty('disabled', true);
    expect(seed.firstElementChild?.className).toContain('overflow-wrap-anywhere');
  });

  it('renders the live-schema label + rel-type toggles and raises the toggle callbacks', () => {
    const props = renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'Entity' }));
    expect(props.onToggleLabel).toHaveBeenCalledWith('Entity');
    fireEvent.click(screen.getByRole('button', { name: 'MENTIONS' }));
    expect(props.onToggleRelType).toHaveBeenCalledWith('MENTIONS');
  });

  it('marks an active filter chip with aria-pressed', () => {
    renderPanel({ activeLabels: new Set(['Document']) });
    expect(screen.getByRole('button', { name: 'Document' }).getAttribute('aria-pressed')).toBe(
      'true',
    );
  });

  it('Show Cypher reveals the read-only query text — never an editable input', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'Show Cypher' }));
    expect(screen.getByText('MATCH (e:Entity) RETURN e')).toBeTruthy();
    expect(screen.queryByRole('textbox')).toBeNull();
    // Toggling hides it again.
    fireEvent.click(screen.getByRole('button', { name: 'Hide Cypher' }));
    expect(screen.queryByText('MATCH (e:Entity) RETURN e')).toBeNull();
  });

  it('renders nothing for filters / Cypher when the schema is absent and query empty', () => {
    renderPanel({ schema: undefined, query: '' });
    expect(screen.queryByRole('button', { name: 'Show Cypher' })).toBeNull();
    expect(screen.queryByText('Node types')).toBeNull();
  });
});
