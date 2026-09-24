import { describe, expect, it } from 'vitest';
import type { SystemUpdateStatus } from '../systemUpdateApi';
import { awaitsDecision, deadlinePassed, deferredUntil, impactOf } from '../updateModel';

const NOW = Date.UTC(2026, 8, 24, 10, 0);
const MINUTE = 60_000;

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

function status(fields: Partial<SystemUpdateStatus> = {}): SystemUpdateStatus {
  return {
    managed: true,
    state: 'pending',
    running_rev: 'aaaaaaaaaaaa',
    available_rev: 'bbbbbbbbbbbb',
    available_built_at: null,
    pending_since: null,
    deadline: null,
    deferred_until: null,
    checked_at: null,
    can_decide: true,
    error: '',
    last_activity_at: null,
    live_runs: 0,
    ...fields,
  };
}

describe('awaitsDecision', () => {
  it('asks an admin about a pending or failed build', () => {
    expect(awaitsDecision(status({ state: 'pending' }))).toBe(true);
    expect(awaitsDecision(status({ state: 'failed' }))).toBe(true);
  });

  it('never asks a member, and never about any other state', () => {
    expect(awaitsDecision(status({ can_decide: false }))).toBe(false);
    for (const state of ['current', 'requested', 'applying', 'unknown'] as const) {
      expect(awaitsDecision(status({ state }))).toBe(false);
    }
    expect(awaitsDecision(undefined)).toBe(false);
  });
});

describe('deferredUntil', () => {
  it('reports a deferral only while it still lies ahead', () => {
    const ahead = NOW + 30 * MINUTE;
    expect(deferredUntil(status({ deferred_until: iso(ahead) }), NOW)).toBe(ahead);
    expect(deferredUntil(status({ deferred_until: iso(NOW) }), NOW)).toBeUndefined();
    expect(deferredUntil(status({ deferred_until: iso(NOW - MINUTE) }), NOW)).toBeUndefined();
    expect(deferredUntil(status(), NOW)).toBeUndefined();
  });
});

describe('deadlinePassed', () => {
  it('is true from the deadline instant on, false before and without one', () => {
    expect(deadlinePassed(status({ deadline: iso(NOW) }), NOW)).toBe(true);
    expect(deadlinePassed(status({ deadline: iso(NOW - MINUTE) }), NOW)).toBe(true);
    expect(deadlinePassed(status({ deadline: iso(NOW + 1000) }), NOW)).toBe(false);
    expect(deadlinePassed(status(), NOW)).toBe(false);
  });
});

describe('impactOf', () => {
  it('puts work in flight first, whatever the last activity says', () => {
    expect(impactOf(status({ live_runs: 2, last_activity_at: iso(NOW) }), NOW)).toEqual({
      kind: 'running',
      count: 2,
    });
    expect(impactOf(status({ live_runs: 1 }), NOW)).toEqual({ kind: 'running', count: 1 });
  });

  it('reports recent activity inside the fifteen-minute idle window', () => {
    expect(impactOf(status({ last_activity_at: iso(NOW - 3 * MINUTE) }), NOW)).toEqual({
      kind: 'recent',
      agoMs: 3 * MINUTE,
    });
    expect(impactOf(status({ last_activity_at: iso(NOW - 15 * MINUTE + 1000) }), NOW)).toEqual({
      kind: 'recent',
      agoMs: 15 * MINUTE - 1000,
    });
  });

  it('reads a server clock ahead of the browser as activity just now', () => {
    expect(impactOf(status({ last_activity_at: iso(NOW + 5000) }), NOW)).toEqual({
      kind: 'recent',
      agoMs: 0,
    });
  });

  it('calls Aura idle past the window or with no activity at all', () => {
    expect(impactOf(status({ last_activity_at: iso(NOW - 15 * MINUTE) }), NOW)).toEqual({
      kind: 'idle',
    });
    expect(impactOf(status(), NOW)).toEqual({ kind: 'idle' });
  });
});
