import { describe, expect, it } from 'vitest';
import {
  DEFER_CHOICES,
  IDLE_AFTER_MS,
  deferTarget,
  formatAgo,
  formatDateTime,
  formatSpan,
  nextTonight,
  parseTime,
  shortRev,
  toRFC3339,
  wallClock,
} from '../updateTime';

// Every expectation is built from local wall-clock dates (new Date(y, m, d, h)), so the suite
// holds in any time zone: "tonight" is 03:00 where the browser is, whatever UTC says.

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

describe('shortRev', () => {
  it('keeps the first nine characters of a git sha', () => {
    expect(shortRev('7886200e5c1a2b3c4d')).toBe('7886200e5');
    expect(shortRev('abc')).toBe('abc');
    expect(shortRev('')).toBe('');
  });
});

describe('parseTime', () => {
  it('reads an RFC 3339 instant and nothing else', () => {
    expect(parseTime('2026-09-24T08:00:00Z')).toBe(Date.UTC(2026, 8, 24, 8));
    expect(parseTime(null)).toBeUndefined();
    expect(parseTime(undefined)).toBeUndefined();
    expect(parseTime('')).toBeUndefined();
    expect(parseTime('not a time')).toBeUndefined();
  });
});

describe('nextTonight', () => {
  it('lands on 03:00 of the next day during the day', () => {
    expect(nextTonight(new Date(2026, 8, 24, 10, 15))).toEqual(new Date(2026, 8, 25, 3, 0));
  });

  it('crosses midnight from the late evening', () => {
    expect(nextTonight(new Date(2026, 8, 24, 23, 30))).toEqual(new Date(2026, 8, 25, 3, 0));
  });

  it('stays on the same date after midnight but before three', () => {
    expect(nextTonight(new Date(2026, 8, 25, 0, 30))).toEqual(new Date(2026, 8, 25, 3, 0));
    expect(nextTonight(new Date(2026, 8, 25, 2, 59, 59))).toEqual(new Date(2026, 8, 25, 3, 0));
  });

  it('never answers the present instant: at 03:00 sharp it is the next night', () => {
    expect(nextTonight(new Date(2026, 8, 25, 3, 0, 0))).toEqual(new Date(2026, 8, 26, 3, 0));
  });

  it('crosses a month end', () => {
    expect(nextTonight(new Date(2026, 8, 30, 22, 0))).toEqual(new Date(2026, 9, 1, 3, 0));
  });
});

describe('deferTarget', () => {
  const now = new Date(2026, 8, 24, 22, 10);

  it('offers exactly one hour, four hours and tonight, in that order', () => {
    expect(DEFER_CHOICES).toEqual(['hour', 'fourHours', 'tonight']);
  });

  it('adds one and four hours to the clock', () => {
    expect(deferTarget('hour', now).getTime() - now.getTime()).toBe(HOUR);
    expect(deferTarget('fourHours', now).getTime() - now.getTime()).toBe(4 * HOUR);
  });

  it('turns tonight into the next 03:00', () => {
    expect(deferTarget('tonight', now)).toEqual(new Date(2026, 8, 25, 3, 0));
  });
});

describe('toRFC3339', () => {
  it('writes UTC at whole seconds', () => {
    expect(toRFC3339(new Date(Date.UTC(2026, 8, 24, 8, 30, 15, 987)))).toBe('2026-09-24T08:30:15Z');
    expect(toRFC3339(new Date(Date.UTC(2026, 8, 24, 8, 30, 15)))).toBe('2026-09-24T08:30:15Z');
  });
});

describe('wallClock', () => {
  const now = new Date(2026, 8, 24, 10, 0).getTime();

  it('names today and tomorrow in the locale', () => {
    expect(wallClock(new Date(2026, 8, 24, 15, 30).getTime(), now, 'it')).toEqual({
      day: 'oggi',
      time: '15:30',
    });
    expect(wallClock(new Date(2026, 8, 25, 3, 0).getTime(), now, 'it')).toEqual({
      day: 'domani',
      time: '03:00',
    });
    expect(wallClock(new Date(2026, 8, 25, 3, 0).getTime(), now, 'en').day).toBe('tomorrow');
    expect(wallClock(new Date(2026, 8, 23, 9, 0).getTime(), now, 'it').day).toBe('ieri');
  });

  it('counts calendar days, not 24-hour spans', () => {
    const lateEvening = new Date(2026, 8, 24, 23, 50).getTime();
    expect(wallClock(new Date(2026, 8, 25, 0, 10).getTime(), lateEvening, 'it').day).toBe('domani');
  });

  it('falls back to the weekday and date beyond tomorrow', () => {
    const at = new Date(2026, 8, 26, 7, 12).getTime();
    expect(wallClock(at, now, 'it')).toEqual({
      day: new Intl.DateTimeFormat('it', {
        weekday: 'long',
        day: 'numeric',
        month: 'short',
      }).format(at),
      time: '07:12',
    });
    expect(wallClock(at, now, 'it').day).toContain('sabato');
  });
});

describe('formatDateTime', () => {
  it('uses the medium date and short time of the locale', () => {
    const at = new Date(2026, 8, 24, 7, 12).getTime();
    expect(formatDateTime(at, 'it')).toBe(
      new Intl.DateTimeFormat('it', { dateStyle: 'medium', timeStyle: 'short' }).format(at),
    );
    expect(formatDateTime(at, 'it')).toContain('07:12');
  });
});

describe('formatSpan', () => {
  it('reads minutes below an hour and whole hours above', () => {
    expect(formatSpan(12 * MINUTE + 59_000, 'it')).toBe('12 minuti');
    expect(formatSpan(59 * MINUTE, 'en')).toBe('59 minutes');
    expect(formatSpan(60 * MINUTE, 'en')).toBe('1 hour');
    expect(formatSpan(3 * HOUR + 59 * MINUTE, 'it')).toBe('3 ore');
  });

  it('never says less than a minute, even for a clock that runs behind', () => {
    expect(formatSpan(10_000, 'en')).toBe('1 minute');
    expect(formatSpan(-5 * MINUTE, 'en')).toBe('1 minute');
  });
});

describe('formatAgo', () => {
  it('says now under a minute, then minutes, then hours', () => {
    expect(formatAgo(59_999, 'it')).toBe('ora');
    expect(formatAgo(MINUTE, 'en')).toBe('1 minute ago');
    expect(formatAgo(3 * MINUTE + 30_000, 'it')).toBe('3 minuti fa');
    expect(formatAgo(59 * MINUTE, 'en')).toBe('59 minutes ago');
    expect(formatAgo(2 * HOUR + 10 * MINUTE, 'it')).toBe('2 ore fa');
  });
});

describe('IDLE_AFTER_MS', () => {
  it('matches the updater idle window of fifteen minutes', () => {
    expect(IDLE_AFTER_MS).toBe(15 * MINUTE);
  });
});
