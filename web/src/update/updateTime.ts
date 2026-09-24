// Pure time rules for the update dialog. Every function takes its clock as an argument so the
// deadline and "tonight" arithmetic can be checked at any instant, midnight included.

const MINUTE_MS = 60_000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;
/** The updater's own idle window: quieter than this, it applies a pending build by itself. */
export const IDLE_AFTER_MS = 15 * MINUTE_MS;
const TONIGHT_HOUR = 3;

export type DeferChoice = 'hour' | 'fourHours' | 'tonight';
export const DEFER_CHOICES: readonly DeferChoice[] = ['hour', 'fourHours', 'tonight'];

export function shortRev(rev: string): string {
  return rev.slice(0, 9);
}

export function parseTime(value: string | null | undefined): number | undefined {
  if (value === null || value === undefined || value === '') return undefined;
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? undefined : ms;
}

/** The next 03:00 on the browser's wall clock, strictly after `now`. */
export function nextTonight(now: Date): Date {
  const target = new Date(now);
  target.setHours(TONIGHT_HOUR, 0, 0, 0);
  if (target.getTime() <= now.getTime()) target.setDate(target.getDate() + 1);
  return target;
}

export function deferTarget(choice: DeferChoice, now: Date): Date {
  if (choice === 'hour') return new Date(now.getTime() + HOUR_MS);
  if (choice === 'fourHours') return new Date(now.getTime() + 4 * HOUR_MS);
  return nextTonight(now);
}

/** RFC 3339 in UTC at whole seconds, the shape the daemon's time.RFC3339 parser expects. */
export function toRFC3339(date: Date): string {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

export interface WallClock {
  /** "today" / "tomorrow" in the locale's own words, a weekday and date beyond that. */
  readonly day: string;
  readonly time: string;
}

function calendarDays(from: number, to: number): number {
  const start = new Date(from);
  start.setHours(0, 0, 0, 0);
  const end = new Date(to);
  end.setHours(0, 0, 0, 0);
  // Rounded, because a daylight-saving day lasts 23 or 25 hours.
  return Math.round((end.getTime() - start.getTime()) / DAY_MS);
}

export function wallClock(at: number, now: number, language: string): WallClock {
  const time = new Intl.DateTimeFormat(language, { timeStyle: 'short' }).format(at);
  const days = calendarDays(now, at);
  const day =
    Math.abs(days) <= 1
      ? new Intl.RelativeTimeFormat(language, { numeric: 'auto' }).format(days, 'day')
      : new Intl.DateTimeFormat(language, {
          weekday: 'long',
          day: 'numeric',
          month: 'short',
        }).format(at);
  return { day, time };
}

export function formatDateTime(at: number, language: string): string {
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }).format(at);
}

function unit(language: string, name: 'minute' | 'hour', value: number): string {
  return new Intl.NumberFormat(language, { style: 'unit', unit: name, unitDisplay: 'long' }).format(
    value,
  );
}

/** A span such as "12 minutes" or "3 hours", never less than one minute. */
export function formatSpan(ms: number, language: string): string {
  const minutes = Math.max(1, Math.floor(ms / MINUTE_MS));
  if (minutes < 60) return unit(language, 'minute', minutes);
  return unit(language, 'hour', Math.floor(minutes / 60));
}

/** "now", "3 minutes ago", "2 hours ago" in the locale's own words. */
export function formatAgo(ms: number, language: string): string {
  const format = new Intl.RelativeTimeFormat(language, { numeric: 'auto' });
  const minutes = Math.floor(ms / MINUTE_MS);
  if (minutes < 1) return format.format(0, 'second');
  if (minutes < 60) return format.format(-minutes, 'minute');
  return format.format(-Math.floor(minutes / 60), 'hour');
}
