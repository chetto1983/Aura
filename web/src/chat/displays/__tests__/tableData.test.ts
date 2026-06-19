import { describe, expect, it } from 'vitest';
import {
  compareCells,
  csvField,
  filterAndSort,
  isNumericCell,
  nextSort,
  toCSV,
  toTSV,
} from '../tableData';

// Pure table-data helpers — exact-output assertions that pin the serialization,
// numeric-sort, and filter/sort behaviour (mutation-resistant).

describe('isNumericCell', () => {
  it('is true for integers, decimals, and negatives', () => {
    expect(isNumericCell('10')).toBe(true);
    expect(isNumericCell('3.14')).toBe(true);
    expect(isNumericCell('-5')).toBe(true);
  });

  it('is false for empty, whitespace, and non-numeric strings', () => {
    expect(isNumericCell('')).toBe(false);
    expect(isNumericCell('   ')).toBe(false);
    expect(isNumericCell('abc')).toBe(false);
    expect(isNumericCell('1a')).toBe(false);
  });
});

describe('compareCells', () => {
  it('compares two numbers numerically, not lexically', () => {
    // Lexical would put "10" before "9"; numeric does not.
    expect(compareCells('10', '9')).toBeGreaterThan(0);
    expect(compareCells('2', '10')).toBeLessThan(0);
    expect(compareCells('3', '3')).toBe(0);
  });

  it('compares non-numeric cells as locale strings', () => {
    expect(compareCells('apple', 'banana')).toBeLessThan(0);
    expect(compareCells('banana', 'apple')).toBeGreaterThan(0);
  });

  it('treats a mixed numeric/text pair as strings', () => {
    // "10" vs "a": not both numeric → string compare ("1" < "a").
    expect(compareCells('10', 'a')).toBeLessThan(0);
  });
});

describe('csvField', () => {
  it('leaves a plain field untouched', () => {
    expect(csvField('plain')).toBe('plain');
  });

  it('quotes a field with a comma', () => {
    expect(csvField('a,b')).toBe('"a,b"');
  });

  it('quotes a field with a newline', () => {
    expect(csvField('a\nb')).toBe('"a\nb"');
  });

  it('quotes and doubles interior quotes', () => {
    expect(csvField('a"b')).toBe('"a""b"');
  });
});

describe('toCSV', () => {
  it('joins the header and rows with CRLF', () => {
    expect(
      toCSV(
        ['A', 'B'],
        [
          ['1', '2'],
          ['3', '4'],
        ],
      ),
    ).toBe('A,B\r\n1,2\r\n3,4');
  });

  it('emits just the header when there are no rows', () => {
    expect(toCSV(['A', 'B'], [])).toBe('A,B');
  });

  it('escapes a comma-bearing cell in the body', () => {
    expect(toCSV(['Note'], [['x, y']])).toBe('Note\r\n"x, y"');
  });
});

describe('toTSV', () => {
  it('joins the header and rows with tabs and newlines', () => {
    expect(toTSV(['A', 'B'], [['1', '2']])).toBe('A\tB\n1\t2');
  });

  it('emits just the header when there are no rows', () => {
    expect(toTSV(['A'], [])).toBe('A');
  });
});

describe('filterAndSort', () => {
  const rows = [
    ['Cherry', '3'],
    ['Apple', '10'],
    ['Banana', '1'],
  ];

  it('returns every row when the filter is blank', () => {
    expect(filterAndSort(rows, '', null)).toHaveLength(3);
  });

  it('keeps only rows containing the query (case-insensitive)', () => {
    const out = filterAndSort(rows, 'APP', null);
    expect(out).toEqual([['Apple', '10']]);
  });

  it('sorts ascending by a numeric column numerically', () => {
    const out = filterAndSort(rows, '', { col: 1, dir: 'asc' });
    expect(out.map((r) => r[0])).toEqual(['Banana', 'Cherry', 'Apple']);
  });

  it('sorts descending by reversing the ascending order', () => {
    const out = filterAndSort(rows, '', { col: 1, dir: 'desc' });
    expect(out.map((r) => r[0])).toEqual(['Apple', 'Cherry', 'Banana']);
  });

  it('does not mutate the source array', () => {
    const before = rows.map((r) => [...r]);
    filterAndSort(rows, '', { col: 0, dir: 'desc' });
    expect(rows).toEqual(before);
  });
});

describe('nextSort', () => {
  it('starts a fresh column ascending', () => {
    expect(nextSort(null, 2)).toEqual({ col: 2, dir: 'asc' });
    expect(nextSort({ col: 0, dir: 'asc' }, 2)).toEqual({ col: 2, dir: 'asc' });
  });

  it('flips the same column asc → desc → asc', () => {
    expect(nextSort({ col: 1, dir: 'asc' }, 1)).toEqual({ col: 1, dir: 'desc' });
    expect(nextSort({ col: 1, dir: 'desc' }, 1)).toEqual({ col: 1, dir: 'asc' });
  });
});
