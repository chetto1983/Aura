// searchHighlight is the pure snippet-splitter for SearchPanel (D-08). It lives
// in its own module so SearchPanel.tsx exports only a component (react-refresh).
//
// It splits a snippet around the (case-insensitive) query into ordered segments
// so the matched run can be wrapped in a <mark> via SAFE element composition —
// the panel never assembles raw HTML (T-25-15 XSS guard).

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
