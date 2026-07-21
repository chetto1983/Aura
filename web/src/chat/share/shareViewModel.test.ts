import { describe, expect, it } from 'vitest';
import {
  DEFAULT_MAX_EXPIRY_DAYS,
  absoluteShareUrl,
  computeStaleCount,
  daysUntil,
  expiryOptionForApi,
  formatShareDate,
  isCustomExpiryInvalid,
  isLinkExpired,
} from './shareViewModel';

describe('expiryOptionForApi', () => {
  it('passes fixed presets through verbatim', () => {
    expect(expiryOptionForApi('1d', 0)).toBe('1d');
    expect(expiryOptionForApi('7d', 0)).toBe('7d');
    expect(expiryOptionForApi('30d', 0)).toBe('30d');
  });

  it('maps custom to the numeric day count', () => {
    expect(expiryOptionForApi('custom', 45)).toBe(45);
  });
});

describe('isCustomExpiryInvalid', () => {
  it('is never invalid for a fixed preset, regardless of the days value', () => {
    expect(isCustomExpiryInvalid('7d', -5)).toBe(false);
    expect(isCustomExpiryInvalid('1d', 99999)).toBe(false);
  });

  it('rejects zero and negative custom day counts', () => {
    expect(isCustomExpiryInvalid('custom', 0)).toBe(true);
    expect(isCustomExpiryInvalid('custom', -1)).toBe(true);
  });

  it('rejects a custom count above the cap', () => {
    expect(isCustomExpiryInvalid('custom', DEFAULT_MAX_EXPIRY_DAYS + 1)).toBe(true);
  });

  it('accepts a custom count within (1, cap]', () => {
    expect(isCustomExpiryInvalid('custom', 1)).toBe(false);
    expect(isCustomExpiryInvalid('custom', DEFAULT_MAX_EXPIRY_DAYS)).toBe(false);
  });

  it('honors an explicit capDays override', () => {
    expect(isCustomExpiryInvalid('custom', 10, 5)).toBe(true);
    expect(isCustomExpiryInvalid('custom', 5, 5)).toBe(false);
  });
});

describe('computeStaleCount', () => {
  it('returns 0 when the conversation TurnCount is unavailable', () => {
    expect(computeStaleCount(undefined, { snapshot_turn_count: 3 })).toBe(0);
    expect(computeStaleCount({}, { snapshot_turn_count: 3 })).toBe(0);
  });

  it('returns 0 when the link snapshot_turn_count is unavailable', () => {
    expect(computeStaleCount({ TurnCount: 10 }, undefined)).toBe(0);
    expect(computeStaleCount({ TurnCount: 10 }, {})).toBe(0);
  });

  it('returns the positive delta when the thread has newer turns', () => {
    expect(computeStaleCount({ TurnCount: 10 }, { snapshot_turn_count: 7 })).toBe(3);
  });

  it('never returns a negative count when the snapshot is somehow ahead', () => {
    expect(computeStaleCount({ TurnCount: 5 }, { snapshot_turn_count: 9 })).toBe(0);
  });

  it('returns 0 when both counts are equal (no newer turns)', () => {
    expect(computeStaleCount({ TurnCount: 4 }, { snapshot_turn_count: 4 })).toBe(0);
  });
});

describe('daysUntil', () => {
  const now = new Date('2026-07-17T12:00:00Z');

  it('ceils a partial day up to 1', () => {
    expect(daysUntil('2026-07-17T23:00:00Z', now)).toBe(1);
  });

  it('computes a whole number of days out', () => {
    expect(daysUntil('2026-07-24T12:00:00Z', now)).toBe(7);
  });

  it('returns 0 for an already-past timestamp, never negative', () => {
    expect(daysUntil('2026-07-01T00:00:00Z', now)).toBe(0);
  });

  it('returns 0 for an invalid date string', () => {
    expect(daysUntil('not-a-date', now)).toBe(0);
  });
});

describe('isLinkExpired', () => {
  const now = new Date('2026-07-17T12:00:00Z');

  it('is false when expires_at is absent (internal tier, or a never-expiring link)', () => {
    expect(isLinkExpired({}, now)).toBe(false);
  });

  it('is false for a future expiry', () => {
    expect(isLinkExpired({ expires_at: '2026-07-24T12:00:00Z' }, now)).toBe(false);
  });

  it('is true for a past expiry (expired-but-not-swept)', () => {
    expect(isLinkExpired({ expires_at: '2026-07-01T00:00:00Z' }, now)).toBe(true);
  });

  it('is true exactly at the expiry instant', () => {
    expect(isLinkExpired({ expires_at: '2026-07-17T12:00:00Z' }, now)).toBe(true);
  });
});

describe('absoluteShareUrl', () => {
  it('prefixes a relative path with the current origin', () => {
    expect(absoluteShareUrl('/shared/share-1')).toBe(`${window.location.origin}/shared/share-1`);
    expect(absoluteShareUrl('/s/tok-abc')).toBe(`${window.location.origin}/s/tok-abc`);
  });
});

describe('formatShareDate', () => {
  it('returns "-" for undefined or empty input', () => {
    expect(formatShareDate(undefined)).toBe('-');
    expect(formatShareDate('')).toBe('-');
    expect(formatShareDate('   ')).toBe('-');
  });

  it('returns "-" for an invalid date string', () => {
    expect(formatShareDate('not-a-date')).toBe('-');
  });

  it('formats a valid ISO date', () => {
    const formatted = formatShareDate('2026-07-17T10:00:00Z');
    expect(formatted).not.toBe('-');
    expect(formatted.length).toBeGreaterThan(0);
  });
});
