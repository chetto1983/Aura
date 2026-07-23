// toolGrouping — pure run detection for the ToolGroup stack (compact-chat spec
// docs/superpowers/specs/2026-07-23-cockpit-compact-chat-ui-spec.md §3.3).
// Runs of ≥ TOOL_GROUP_MIN consecutive SETTLED tool parts collapse into one
// group header; the currently running tool is never a member, and the inline
// display exceptions (system_event / local_artifact) break a run.

/** 2 rows collapse to a header + nothing saved — not worth the indirection. */
export const TOOL_GROUP_MIN = 3;

/** The part shape this module inspects (the reducer's ToolPart superset). */
export interface GroupablePart {
  readonly type?: unknown;
  readonly toolCallId?: unknown;
  readonly result?: unknown;
  readonly isError?: unknown;
  readonly display?: { readonly type?: unknown } | undefined;
}

export interface ToolRunInfo {
  readonly startIndex: number;
  readonly ids: readonly string[];
}

const INLINE_TYPES = new Set<unknown>(['system_event', 'local_artifact']);

/** Settled tool part, not an inline-exception display — a group member candidate. */
export function isGroupableToolPart(part: unknown): part is GroupablePart {
  if (typeof part !== 'object' || part === null) return false;
  const p = part as GroupablePart;
  if (p.type !== 'tool-call' || typeof p.toolCallId !== 'string') return false;
  if (typeof p.result !== 'string' && p.isError !== true) return false; // running
  if (p.display !== undefined && INLINE_TYPES.has(p.display.type)) return false;
  return true;
}

/**
 * Locate the contiguous groupable run containing `toolCallId`, or null when the
 * part is not part of a ≥ TOOL_GROUP_MIN run (render individually). The FIRST
 * member's renderer draws the whole group; later members render nothing.
 */
export function toolRun(content: readonly unknown[], toolCallId: string): ToolRunInfo | null {
  const index = content.findIndex(
    (part) =>
      typeof part === 'object' &&
      part !== null &&
      (part as GroupablePart).type === 'tool-call' &&
      (part as GroupablePart).toolCallId === toolCallId,
  );
  if (index < 0 || !isGroupableToolPart(content[index])) return null;

  let start = index;
  while (start > 0 && isGroupableToolPart(content[start - 1])) start -= 1;
  let end = index;
  while (end + 1 < content.length && isGroupableToolPart(content[end + 1])) end += 1;

  if (end - start + 1 < TOOL_GROUP_MIN) return null;
  const ids: string[] = [];
  for (let i = start; i <= end; i += 1) {
    ids.push((content[i] as GroupablePart).toolCallId as string);
  }
  return { startIndex: start, ids };
}
