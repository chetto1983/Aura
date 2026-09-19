// timecode.ts — the "mm:ss.s" form the trim fields show and accept. The two functions are a
// pair: whatever formatTimecode writes, parseTimecode reads back to the same tenth, which is
// why the fields do not reuse chat/durationFormat.ts (a locale sentence, not an input format).

/** Seconds → "mm:ss.s", rounded to the tenth; negative time reads as zero. */
export function formatTimecode(seconds: number): string {
  const tenths = Math.round(Math.max(0, seconds) * 10);
  const minutes = Math.floor(tenths / 600);
  const rest = (tenths % 600) / 10;
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

/** "ss", "ss.s", "mm:ss" or "mm:ss.s" → seconds; undefined for anything else. */
export function parseTimecode(text: string): number | undefined {
  const parts = text.trim().split(':');
  if (parts.length === 1) return decimal(parts[0] ?? '');
  if (parts.length !== 2) return undefined;
  const minutes = decimal(parts[0] ?? '');
  const seconds = decimal(parts[1] ?? '');
  if (minutes === undefined || seconds === undefined) return undefined;
  if (!Number.isInteger(minutes) || seconds >= 60) return undefined;
  return minutes * 60 + seconds;
}
