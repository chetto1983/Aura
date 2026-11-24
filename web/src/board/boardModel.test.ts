import { describe, expect, it } from 'vitest';
import type { BoardView, WidgetCard } from './boardApi';
import {
  boardTags,
  filterPredicate,
  orderViews,
  overLimitCss,
  parseTags,
  widgetColumn,
} from './boardModel';

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

describe('column limit', () => {
  const cards = (n: number) => Array.from({ length: n }, (_, i) => card({ id: String(i) }));
  const limited = widgetColumn({ id: 'done', label: 'Done', cardLimit: 1 });
  const view = {
    ...limited,
    cards: [],
    summaries: [],
    dataStatus: 'loaded',
    addCard: true,
    collapsed: false,
  };

  it('marks a column holding more cards than its limit', () => {
    expect(overLimitCss(cards(2), view as never)).toBe('wx-over-limit');
  });

  it('leaves a column at its limit, or without one, unmarked', () => {
    expect(overLimitCss(cards(1), view as never)).toBe('');
    const free = widgetColumn({ id: 'todo', label: 'To do' });
    expect(overLimitCss(cards(9), { ...free, cards: [] } as never)).toBe('');
  });

  it('shows the count against the limit and flags it over', () => {
    const summary = limited.summaries?.[0];
    expect(summary?.format?.(2, limited)).toBe('2/1');
    expect(summary?.css?.(2, limited)).toBe('wx-over');
    expect(summary?.css?.(1, limited)).toBe('');
  });

  it('gives a column without a limit no summary', () => {
    expect(widgetColumn({ id: 'todo', label: 'To do' })).toEqual({ id: 'todo', label: 'To do' });
  });
});
