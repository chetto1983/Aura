import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { Server, Sparkles } from 'lucide-react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { SectionRail } from '../SectionRail';

// SectionRail is the one section navigator behind Settings and Governance. This suite owns the
// contract both surfaces lean on: the two layouts, the captions that survive at every width,
// the selected row, and the remembered column width.

const GROUPS = [
  { id: 'first', caption: 'First group', items: [{ id: 'a', icon: Server, label: 'Alpha' }] },
  { id: 'second', caption: 'Second group', items: [{ id: 'b', icon: Sparkles, label: 'Beta' }] },
];

function renderRail(activeId = 'a', onSelect = vi.fn()) {
  render(
    <SectionRail
      id="test"
      label="Test sections"
      groups={GROUPS}
      activeId={activeId}
      onSelect={onSelect}
    />,
  );
  return onSelect;
}

describe('SectionRail', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('is one nav with two layouts: a strip below lg, a sidebar column from lg up', () => {
    renderRail();
    const rail = screen.getByRole('navigation', { name: 'Test sections' });

    expect(rail.className).toContain('overflow-x-auto');
    expect(rail.className).toContain('border-b');
    expect(rail.className).toContain('lg:flex-col');
    expect(rail.className).toContain('lg:border-r');
    expect(rail.className).toContain('lg:border-b-0');
    // The width lives in a CSS variable so ONE tree serves both regimes — swapping trees at
    // the breakpoint would remount the pane beside it.
    expect(rail.className).toContain('lg:w-[var(--rail-w)]');
    expect(rail.getAttribute('style')).toContain('--rail-w: 240px');
  });

  it('keeps every group caption labelling its list at every width', () => {
    renderRail();
    const caption = screen.getByText('First group');
    // sr-only rather than hidden: the caption still labels its list for a screen reader on
    // the narrow strip, where there is no room to paint it.
    expect(caption.className).toContain('max-lg:sr-only');
    expect(screen.getByRole('list', { name: 'First group' })).toBeTruthy();
  });

  it('omits the caption and the list label for a flat rail', () => {
    render(
      <SectionRail
        id="flat"
        label="Flat sections"
        groups={[{ id: 'only', items: [{ id: 'a', icon: Server, label: 'Alpha' }] }]}
        activeId="a"
        onSelect={vi.fn()}
      />,
    );
    const list = screen.getByRole('list');
    expect(list.getAttribute('aria-labelledby')).toBeNull();
  });

  it('marks the active row aria-current and reports selections by id', () => {
    const onSelect = renderRail('a');

    const alpha = screen.getByRole('button', { name: 'Alpha' });
    const beta = screen.getByRole('button', { name: 'Beta' });
    expect(alpha.getAttribute('aria-current')).toBe('page');
    expect(beta.getAttribute('aria-current')).toBeNull();

    fireEvent.click(beta);
    expect(onSelect).toHaveBeenCalledExactlyOnceWith('b');
  });

  it('keeps the 44px touch floor in real pixels', () => {
    renderRail();
    // In rem (`h-11`) this measured 42.6px live: Aura's root font is not 16px. The floor has
    // to come from the Button's own px-based minimum.
    expect(screen.getByRole('button', { name: 'Alpha' }).className).toContain('min-h-[44px]');
  });

  it('drops a group with no items rather than painting an empty caption', () => {
    render(
      <SectionRail
        id="sparse"
        label="Sparse sections"
        groups={[
          { id: 'empty', caption: 'Empty group', items: [] },
          { id: 'full', caption: 'Full group', items: [{ id: 'a', icon: Server, label: 'Alpha' }] },
        ]}
        activeId="a"
        onSelect={vi.fn()}
      />,
    );
    expect(screen.queryByText('Empty group')).toBeNull();
    expect(screen.getByText('Full group')).toBeTruthy();
  });

  it('resizes the column by keyboard and remembers the width per rail', () => {
    renderRail();
    const handle = screen.getByRole('separator', { name: 'Resize the sections rail' });
    expect(handle.getAttribute('aria-valuenow')).toBe('240');

    fireEvent.keyDown(handle, { key: 'ArrowRight' });
    expect(handle.getAttribute('aria-valuenow')).toBe('256');
    expect(localStorage.getItem('aura.rail.test.width')).toBe('256');

    // Shift takes a bigger step; Home and End jump to the bounds.
    fireEvent.keyDown(handle, { key: 'ArrowRight', shiftKey: true });
    expect(handle.getAttribute('aria-valuenow')).toBe('304');
    fireEvent.keyDown(handle, { key: 'End' });
    expect(handle.getAttribute('aria-valuenow')).toBe('448');
    fireEvent.keyDown(handle, { key: 'Home' });
    expect(handle.getAttribute('aria-valuenow')).toBe('208');
  });

  it('clamps a stored width from a wider monitor', () => {
    localStorage.setItem('aura.rail.test.width', '5000');
    renderRail();
    expect(
      screen
        .getByRole('separator', { name: 'Resize the sections rail' })
        .getAttribute('aria-valuenow'),
    ).toBe('448');
  });
});
