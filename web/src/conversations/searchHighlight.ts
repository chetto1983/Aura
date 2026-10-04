// searchHighlight holds SearchPanel's pure snippet helpers (D-08), in their own module so
// SearchPanel.tsx exports only a component (react-refresh).
//
// searchSnippet cuts a hit down to the part worth showing; highlightSegments splits it
// around the (case-insensitive) query into ordered segments so the matched run can be
// wrapped in a <mark> via SAFE element composition — the panel never assembles raw HTML
// (T-25-15 XSS guard).

// The sidebar shows a hit in two lines of about 30 characters, so only a few words may
// precede the match: with the 60 that `aura chat search` keeps (cmd/aura/chat_repl.go
// excerpt) the match fell past the second line, measured on 2026-10-04.
const SNIPPET_LEAD = 20;
const SNIPPET_LENGTH = 120;

/**
 * The text from a few words before the first case-insensitive occurrence of the query,
 * cut at a word boundary. The search finds a word anywhere in a long message, so the
 * start of that message rarely shows it. A fuzzy match with no literal occurrence keeps
 * the start.
 */
export function searchSnippet(content: string, query: string): string {
  const flat = content.split(/\s+/).filter(Boolean).join(' ');
  const at = flat.toLowerCase().indexOf(query.trim().toLowerCase());
  let start = Math.max(at - SNIPPET_LEAD, 0);
  const space = flat.indexOf(' ', start);
  if (start > 0 && space !== -1 && space < at) start = space + 1;
  return clampSnippet(flat, start, start + SNIPPET_LENGTH);
}

function clampSnippet(text: string, from: number, to: number): string {
  let start = Math.max(from, 0);
  let end = Math.min(to, text.length);
  // Never cut a surrogate pair in half.
  if (isLowSurrogate(text, start)) start += 1;
  if (end < text.length && isLowSurrogate(text, end)) end -= 1;
  return (start > 0 ? '…' : '') + text.slice(start, end) + (end < text.length ? '…' : '');
}

function isLowSurrogate(text: string, index: number): boolean {
  const code = text.charCodeAt(index);
  return code >= 0xdc00 && code <= 0xdfff;
}

export interface HighlightSegment {
  readonly text: string;
  readonly match: boolean;
}

export function highlightSegments(content: string, query: string): readonly HighlightSegment[] {
  const needle = query.trim();
  if (needle.length === 0) return [{ text: content, match: false }];
  const lowerContent = content.toLowerCase();
  const lowerNeedle = needle.toLowerCase();
  const out: HighlightSegment[] = [];
  let from = 0;
  for (;;) {
    const at = lowerContent.indexOf(lowerNeedle, from);
    if (at === -1) {
      if (from < content.length) out.push({ text: content.slice(from), match: false });
      break;
    }
    if (at > from) out.push({ text: content.slice(from, at), match: false });
    out.push({ text: content.slice(at, at + needle.length), match: true });
    from = at + needle.length;
  }
  return out.length > 0 ? out : [{ text: content, match: false }];
}
