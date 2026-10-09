import { describe, expect, it } from 'vitest';
import type { BoardView, WidgetCard } from './boardApi';
import { boardTags, filterPredicate, orderViews, parseTags } from './boardModel';

const NOW = new Date('2026-10-09T12:00:00Z');

function card(patch: Partial<WidgetCard>): WidgetCard {
  return {
    id: 'c',
    label: 'call the supplier',
    description: '',
    column: 'todo',
    priority: 2,
    tags: [],
    source: 'cockpit',
    updated_by: 'operator',
    updated_at: '2026-10-09T10:00:00Z',
    ...patch,
  };
}

describe('filterPredicate', () => {
  it('filters nothing when no filter is set', () => {
    expect(filterPredicate({}, NOW)).toBeNull();
  });

  it('requires every filter that is set', () => {
    const match = filterPredicate({ source: 'chat', priority: 3, tag: 'ops' }, NOW);
    expect(match?.(card({ source: 'chat', priority: 3, tags: ['ops', 'billing'] }))).toBe(true);
    expect(match?.(card({ source: 'cockpit', priority: 3, tags: ['ops'] }))).toBe(false);
    expect(match?.(card({ source: 'chat', priority: 2, tags: ['ops'] }))).toBe(false);
    expect(match?.(card({ source: 'chat', priority: 3, tags: ['billing'] }))).toBe(false);
  });

  it('reads due dates against now', () => {
    const overdue = filterPredicate({ due: 'overdue' }, NOW);
    const week = filterPredicate({ due: 'week' }, NOW);
    const yesterday = card({ deadline: new Date('2026-10-08T12:00:00Z') });
    const inThreeDays = card({ deadline: new Date('2026-10-12T12:00:00Z') });
    const inTenDays = card({ deadline: new Date('2026-10-19T12:00:00Z') });
    const undated = card({});
    expect([yesterday, inThreeDays, inTenDays, undated].map((c) => overdue?.(c))).toEqual([
      true,
      false,
      false,
      false,
    ]);
    expect([yesterday, inThreeDays, inTenDays, undated].map((c) => week?.(c))).toEqual([
      false,
      true,
      false,
      false,
    ]);
    expect(week?.(card({ deadline: new Date('2026-10-16T12:00:00Z') }))).toBe(true);
  });
});

describe('board helpers', () => {
  it('lists every tag once, sorted', () => {
    expect(
      boardTags([card({ tags: ['ops', 'billing'] }), card({ tags: ['ops', 'admin'] })]),
    ).toEqual(['admin', 'billing', 'ops']);
  });

  it('parses a tag line without blanks or repeats', () => {
    expect(parseTags(' ops, billing,, ops ,')).toEqual(['ops', 'billing']);
    expect(parseTags('')).toEqual([]);
  });

  it('puts pinned views first, then orders by name', () => {
    const view = (name: string, pinned: boolean): BoardView => ({
      id: name,
      name,
      filters: {},
      pinned,
    });
    expect(
      orderViews([
        view('zeta', false),
        view('beta', true),
        view('alpha', false),
        view('omega', true),
      ]).map((v) => v.name),
    ).toEqual(['beta', 'omega', 'alpha', 'zeta']);
  });
});
