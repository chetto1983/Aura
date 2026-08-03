import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { GraphControls } from '../GraphControls';
import type { GraphSchema } from '../types';

const SCHEMA: GraphSchema = { labels: ['Entity', 'Document'], rel_types: ['MENTIONS'] };

function renderControls(over: Partial<React.ComponentProps<typeof GraphControls>> = {}) {
  const props: React.ComponentProps<typeof GraphControls> = {
    schema: SCHEMA,
    activeLabels: new Set(),
    activeRelTypes: new Set(),
    query: 'SELECT FROM `Entity` LIMIT 75',
    onRefresh: vi.fn(),
    onToggleLabel: vi.fn(),
    onToggleRelType: vi.fn(),
    ...over,
  };
  render(<GraphControls {...props} />);
  return props;
}

describe('GraphControls', () => {
  it('refreshes the authenticated identity memory graph from the primary action', () => {
    const props = renderControls();
    const refresh = screen.getByRole('button', { name: 'Load memory graph' });
    expect(refresh).toHaveProperty('disabled', false);
    expect(refresh.firstElementChild?.className).toContain('overflow-wrap-anywhere');
    fireEvent.click(refresh);
    expect(props.onRefresh).toHaveBeenCalledTimes(1);
  });

  it('renders the live-schema filters and raises their callbacks', () => {
    const props = renderControls();
    fireEvent.click(screen.getByRole('button', { name: 'Entity' }));
    expect(props.onToggleLabel).toHaveBeenCalledWith('Entity');
    fireEvent.click(screen.getByRole('button', { name: 'MENTIONS' }));
    expect(props.onToggleRelType).toHaveBeenCalledWith('MENTIONS');
  });

  it('marks an active filter chip with aria-pressed', () => {
    renderControls({ activeLabels: new Set(['Document']) });
    expect(screen.getByRole('button', { name: 'Document' }).getAttribute('aria-pressed')).toBe(
      'true',
    );
  });

  it('reveals read-only ArcadeDB SQL without an editable query surface', () => {
    renderControls();
    fireEvent.click(screen.getByRole('button', { name: 'Show ArcadeDB SQL' }));
    expect(screen.getByText('SELECT FROM `Entity` LIMIT 75')).toBeTruthy();
    expect(screen.queryByRole('textbox')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Hide ArcadeDB SQL' }));
    expect(screen.queryByText('SELECT FROM `Entity` LIMIT 75')).toBeNull();
  });

  it('omits filters and SQL when the schema and query are absent', () => {
    renderControls({ schema: undefined, query: '' });
    expect(screen.queryByRole('button', { name: 'Show ArcadeDB SQL' })).toBeNull();
    expect(screen.queryByText('Node types')).toBeNull();
  });
});
