import { describe, expect, it } from 'vitest';
import { formatTimecode, parseTimecode } from '../timecode';

describe('formatTimecode', () => {
  it.each([
    [0, '00:00.0'],
    [2.5, '00:02.5'],
    [59.96, '01:00.0'],
    [75.25, '01:15.3'],
    [-3, '00:00.0'],
  ])('writes %s as %s', (seconds, text) => {
    expect(formatTimecode(seconds)).toBe(text);
  });
});

describe('parseTimecode', () => {
  it.each([
    ['2.5', 2.5],
    ['00:02.5', 2.5],
    ['1:15.3', 75.3],
    ['10', 10],
    [' 00:03 ', 3],
  ])('reads %s as %s seconds', (text, seconds) => {
    expect(parseTimecode(text)).toBeCloseTo(seconds, 5);
  });

  it.each(['', 'abc', '1:2:3', '1:60', '1.5:10', '-1', '0x10', '1e3', '1..2', ':5', '.', '1:.'])(
    'refuses %j',
    (text) => {
      expect(parseTimecode(text)).toBeUndefined();
    },
  );

  it.each([
    ['3.25', 3.3],
    ['00:03.24', 3.2],
    ['1:15.25', 75.3],
    ['0.05', 0.1],
    ['59.97', 60],
  ])('rounds %s to the tenth the field shows, %s', (text, seconds) => {
    const parsed = parseTimecode(text);
    expect(parsed).toBe(seconds);
    expect(formatTimecode(parsed ?? Number.NaN)).toBe(formatTimecode(seconds));
  });

  it('reads back every tenth it writes over ten minutes', () => {
    for (let tenth = 0; tenth < 6000; tenth += 1) {
      expect(parseTimecode(formatTimecode(tenth / 10))).toBeCloseTo(tenth / 10, 5);
    }
  });
});
