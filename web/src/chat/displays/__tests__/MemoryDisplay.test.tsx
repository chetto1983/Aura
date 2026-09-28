import { afterEach, describe, expect, it } from 'vitest';
import { act, render, screen, within } from '@testing-library/react';
import i18n from '../../../i18n/i18n';
import { DisplayRouter } from '../DisplayRouter';
import type { DisplayPayload } from '../types';

afterEach(async () => {
  await act(async () => {
    await i18n.changeLanguage('en');
  });
});

describe('trusted memory displays', () => {
  it('keeps fact markup as text and announces omitted rows', () => {
    const payload = {
      type: 'table',
      title: 'memory_facts',
      tool_call_id: 'm1',
      table: {
        columns: ['Fact', 'Subject', 'Relation', 'Object', 'Valid', 'Sources', 'Fact key'],
        rows: [['<img src=x>', 'A', 'knows', 'B', '2026-01-01', 'run-1', 'key-1']],
        omitted_rows: 3,
      },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="alias__memory_search" result="raw" />);
    const table = screen.getByRole('table', { name: 'Memory facts' });
    expect(within(table).getByText('<img src=x>')).toBeTruthy();
    expect(document.querySelector('img')).toBeNull();
    expect(screen.getByText('3 more rows not shown')).toBeTruthy();
  });

  it('renders trusted graph counts in a localized responsive stats grid', async () => {
    await act(async () => {
      await i18n.changeLanguage('it');
    });
    const payload = {
      type: 'stats',
      title: 'memory_graph',
      tool_call_id: 'g1',
      stats: {
        items: [
          { label: 'nodes', value: 12345 },
          { label: 'edges', value: 9 },
        ],
      },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="alias__graph_diagnostics" result="raw" />);
    expect(screen.getByText('Grafo della memoria')).toBeTruthy();
    expect(screen.getByText('Nodi')).toBeTruthy();
    expect(screen.getByText('12.345')).toBeTruthy();
    expect(screen.getByText('Archi')).toBeTruthy();
    expect(document.querySelector('[data-slot="stats-display"]')).toBeTruthy();
  });

  it('leaves malformed stats in the escaped raw panel', () => {
    const payload = {
      type: 'stats',
      tool_call_id: 'g2',
      stats: { items: [{ label: 'nodes', value: -1 }] },
    } as DisplayPayload;
    render(
      <DisplayRouter payload={payload} toolName="alias__graph_diagnostics" result="raw stats" />,
    );
    expect(screen.getByText('raw stats').tagName.toLowerCase()).toBe('pre');
  });

  it('leaves malformed memory rows in the escaped raw panel', () => {
    const payload = {
      type: 'table',
      title: 'memory_facts',
      tool_call_id: 'm2',
      table: { columns: ['Fact'], rows: [['A']] },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="alias__memory_search" result="raw facts" />);
    expect(screen.getByText('raw facts').tagName.toLowerCase()).toBe('pre');
  });
});
