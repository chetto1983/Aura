import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import type { DisplayChildReport } from '../displays/types';
import type { WorkerStatus } from './workerStream';
import { WorkerPicker } from './WorkerPicker';

const workers: readonly DisplayChildReport[] = [
  { goal_index: 0, child_id: 'w1', status: 'running', goal: 'Collect the release evidence' },
  { goal_index: 1, child_id: 'w2', status: 'stalled', goal: 'Inspect the very long build log' },
  { goal_index: 2, child_id: 'w3', status: 'ok', goal: 'Write the concise report' },
];

const statuses = new Map<string, WorkerStatus>([
  [
    'w2',
    {
      child_id: 'w2',
      status: 'stalled',
      last_event_at: '2026-08-29T20:00:00Z',
      events: 3,
      duration_sec: 12,
    },
  ],
]);

function tabAt(tabs: readonly HTMLElement[], index: number): HTMLElement {
  const tab = tabs[index];
  if (tab === undefined) throw new Error(`worker tab ${String(index)} missing`);
  return tab;
}

describe('WorkerPicker', () => {
  it('renders the same selected entry layout for exactly one worker', () => {
    render(
      <WorkerPicker
        workers={workers.slice(0, 1)}
        statuses={new Map()}
        watchedChildId="w1"
        onSelect={vi.fn()}
      />,
    );

    const only = screen.getByRole('tab');
    expect(only.getAttribute('aria-selected')).toBe('true');
    expect(only.getAttribute('tabindex')).toBe('0');
  });

  it('keeps all entries visible, selected, titled, and at least 44px tall', () => {
    render(
      <WorkerPicker workers={workers} statuses={statuses} watchedChildId="w2" onSelect={vi.fn()} />,
    );

    expect(screen.getByRole('tablist', { name: 'Workers' })).toBeTruthy();
    const tabs = screen.getAllByRole('tab');
    expect(tabs).toHaveLength(3);
    expect(tabs[1]?.getAttribute('aria-selected')).toBe('true');
    expect(tabs[1]?.getAttribute('title')).toBe('Inspect the very long build log');
    expect(tabs[1]?.className).toContain('min-h-[44px]');
  });

  it('supports arrows, Home, End, Enter, and Space with roving focus', () => {
    const onSelect = vi.fn();
    render(
      <WorkerPicker
        workers={workers}
        statuses={statuses}
        watchedChildId="w2"
        onSelect={onSelect}
      />,
    );
    const tabs = screen.getAllByRole('tab');
    const middle = tabAt(tabs, 1);
    middle.focus();

    fireEvent.keyDown(middle, { key: 'ArrowRight' });
    expect(document.activeElement).toBe(tabAt(tabs, 2));
    fireEvent.keyDown(tabAt(tabs, 2), { key: 'Enter' });
    expect(onSelect).toHaveBeenLastCalledWith('w3');

    fireEvent.keyDown(tabAt(tabs, 2), { key: 'ArrowLeft' });
    fireEvent.keyDown(tabAt(tabs, 1), { key: 'Home' });
    expect(document.activeElement).toBe(tabAt(tabs, 0));
    fireEvent.keyDown(tabAt(tabs, 0), { key: 'End' });
    expect(document.activeElement).toBe(tabAt(tabs, 2));
    fireEvent.keyDown(tabAt(tabs, 2), { key: ' ' });
    expect(onSelect).toHaveBeenLastCalledWith('w3');

    fireEvent.keyDown(tabAt(tabs, 2), { key: 'ArrowUp' });
    expect(document.activeElement).toBe(tabAt(tabs, 1));
    fireEvent.keyDown(tabAt(tabs, 1), { key: 'ArrowDown' });
    expect(document.activeElement).toBe(tabAt(tabs, 2));
  });
});
