// timecode.ts — the "mm:ss.s" form the trim fields show and accept. The two functions are a
// pair: whatever formatTimecode writes, parseTimecode reads back to the same tenth, and whatever
// the operator types is rounded to the tenth the field then shows, so the cut is the one on
// screen. That is why the fields do not reuse chat/durationFormat.ts (a locale sentence, not an
// input format).

/** The one rounding both directions use, so a value and its text never disagree. */
function tenths(seconds: number): number {
  return Math.round(seconds * 10);
}

/** Seconds → "mm:ss.s", rounded to the tenth; negative time reads as zero. */
export function formatTimecode(seconds: number): string {
  const count = tenths(Math.max(0, seconds));
  const minutes = Math.floor(count / 600);
  const rest = (count % 600) / 10;
  return `${String(minutes).padStart(2, '0')}:${rest.toFixed(1).padStart(4, '0')}`;
}

/** A plain decimal: digits with at most one dot. Number() alone would also accept "0x10",
 *  "1e3" and signs, none of which is a time the operator typed. */
function decimal(part: string): number | undefined {
  if (part === '') return undefined;
  for (const char of part) {
    if (char !== '.' && (char < '0' || char > '9')) return undefined;
  }
  const value = Number(part);
  return Number.isFinite(value) ? value : undefined;
}

function seconds(text: string): number | undefined {
  const parts = text.trim().split(':');
  if (parts.length === 1) return decimal(parts[0] ?? '');
  if (parts.length !== 2) return undefined;
  const minutes = decimal(parts[0] ?? '');
  const rest = decimal(parts[1] ?? '');
  if (minutes === undefined || rest === undefined) return undefined;
  if (!Number.isInteger(minutes) || rest >= 60) return undefined;
  return minutes * 60 + rest;
}

/** "ss", "ss.s", "mm:ss" or "mm:ss.s" → seconds at the tenth formatTimecode shows (3.25 is the
 *  3.3 on screen); undefined for anything else. */
export function parseTimecode(text: string): number | undefined {
  const value = seconds(text);
  return value === undefined ? undefined : tenths(value) / 10;
}
