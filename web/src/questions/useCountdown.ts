import { useNow } from '@/lib/useNow';

/** Seconds left until an RFC 3339 deadline, re-read every second. Never negative. */
export function useCountdown(deadline: string): number {
  return secondsLeft(deadline, useNow(1000));
}

export function secondsLeft(deadline: string, now: number): number {
  const at = Date.parse(deadline);
  return Number.isNaN(at) ? 0 : Math.max(0, Math.ceil((at - now) / 1000));
}

/** The countdown's face, m:ss. */
export function formatRemaining(seconds: number): string {
  return `${String(Math.floor(seconds / 60))}:${String(seconds % 60).padStart(2, '0')}`;
}
