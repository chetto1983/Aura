import type { SystemUpdateStatus } from './systemUpdateApi';
import { IDLE_AFTER_MS, parseTime } from './updateTime';

/** An admin has a build to decide on: one is waiting, or the last attempt to apply it failed. */
export function awaitsDecision(
  status: SystemUpdateStatus | undefined,
): status is SystemUpdateStatus {
  return status?.can_decide === true && (status.state === 'pending' || status.state === 'failed');
}

/** The instant a deferral still holds the build back, or undefined when none does. */
export function deferredUntil(status: SystemUpdateStatus, now: number): number | undefined {
  const until = parseTime(status.deferred_until);
  return until !== undefined && until > now ? until : undefined;
}

export function deadlinePassed(status: SystemUpdateStatus, now: number): boolean {
  const deadline = parseTime(status.deadline);
  return deadline !== undefined && deadline <= now;
}

export type Impact =
  | { readonly kind: 'running'; readonly count: number }
  | { readonly kind: 'recent'; readonly agoMs: number }
  | { readonly kind: 'idle' };

/** What "update now" would interrupt, read from the admin-only activity fields. */
export function impactOf(status: SystemUpdateStatus, now: number): Impact {
  if (status.live_runs > 0) return { kind: 'running', count: status.live_runs };
  const last = parseTime(status.last_activity_at);
  if (last !== undefined && now - last < IDLE_AFTER_MS) {
    return { kind: 'recent', agoMs: Math.max(0, now - last) };
  }
  return { kind: 'idle' };
}
