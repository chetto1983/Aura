const DEFAULT_AURA_TIME_ZONE = 'Europe/Rome';

type DateTimeParts = {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  second: number;
};

export function defaultAuraTimeZone(): string {
  return DEFAULT_AURA_TIME_ZONE;
}

export function isValidTimeZone(timeZone: string): boolean {
  if (!timeZone.trim()) return false;
  try {
    new Intl.DateTimeFormat('en-US', { timeZone }).format(new Date());
    return true;
  } catch {
    return false;
  }
}

export function toDateTimeLocalValueInTimeZone(iso: string | undefined, timeZone: string): string {
  if (!iso || iso.startsWith('0001')) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const parts = dateTimePartsInZone(date, safeTimeZone(timeZone));
  return `${pad4(parts.year)}-${pad2(parts.month)}-${pad2(parts.day)}T${pad2(parts.hour)}:${pad2(parts.minute)}`;
}

export function fromDateTimeLocalValueInTimeZone(value: string, timeZone: string): string | undefined {
  const parts = parseDateTimeLocalValue(value);
  if (!parts) return undefined;
  const zone = safeTimeZone(timeZone);
  const wallAsUTC = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second);
  let instant = wallAsUTC - getTimeZoneOffsetMs(new Date(wallAsUTC), zone);
  for (let i = 0; i < 4; i++) {
    const next = wallAsUTC - getTimeZoneOffsetMs(new Date(instant), zone);
    if (next === instant) break;
    instant = next;
  }
  return new Date(instant).toISOString();
}

export function activeAuraTimeZoneFromSettings(
  items: Array<{ key: string; value: string; active_value: string }>,
): string {
  const item = items.find((it) => it.key === 'AURA_TIMEZONE');
  const candidate = item?.active_value || item?.value || DEFAULT_AURA_TIME_ZONE;
  return safeTimeZone(candidate);
}

function safeTimeZone(timeZone: string): string {
  const trimmed = timeZone.trim();
  return isValidTimeZone(trimmed) ? trimmed : DEFAULT_AURA_TIME_ZONE;
}

function parseDateTimeLocalValue(value: string): DateTimeParts | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value);
  if (!match) return null;
  const [, year, month, day, hour, minute, second = '0'] = match;
  return {
    year: Number(year),
    month: Number(month),
    day: Number(day),
    hour: Number(hour),
    minute: Number(minute),
    second: Number(second),
  };
}

function dateTimePartsInZone(date: Date, timeZone: string): DateTimeParts {
  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  });
  const values: Record<string, string> = {};
  for (const part of formatter.formatToParts(date)) {
    if (part.type !== 'literal') values[part.type] = part.value;
  }
  return {
    year: Number(values.year),
    month: Number(values.month),
    day: Number(values.day),
    hour: Number(values.hour),
    minute: Number(values.minute),
    second: Number(values.second),
  };
}

function getTimeZoneOffsetMs(date: Date, timeZone: string): number {
  const parts = dateTimePartsInZone(date, timeZone);
  const zoneAsUTC = Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second);
  return zoneAsUTC - date.getTime();
}

function pad2(value: number): string {
  return String(value).padStart(2, '0');
}

function pad4(value: number): string {
  return String(value).padStart(4, '0');
}
