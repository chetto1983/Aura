import type { AuditEvent } from '../admin/adminApi';

// auditPairing — merge each tool start/end pair of the admin activity feed into
// ONE compact row (operator directive: two rows per invocation flood the page).
//
// Pairing is EXACT-first: the server projects the tool leg's `correlation`
// (aura.tool_invocations.tool_call_id) and persisted `duration_ms`, so an end
// row claims the start row with the SAME correlation. Rows without a
// correlation (older feeds, defensive) fall back to the conservative per-name
// stack: walking oldest→newest, an `end` pairs with the most recent unpaired
// `start` of the same tool name. Unpairable rows pass through untouched; an
// orphan start (still running / crashed) renders as its own running-state row.
// The merged row sits at the pair's END position, so the feed stays
// reverse-chronological.

export type InvocationOutcome = 'ok' | 'error' | 'running';

export type AuditRow =
  | { readonly kind: 'event'; readonly event: AuditEvent }
  | {
      readonly kind: 'invocation';
      readonly toolName: string;
      readonly outcome: InvocationOutcome;
      /** The row's anchor timestamp: the END ts (or the start ts when running). */
      readonly at: string;
      /** Server-persisted duration when present, else start→end wallclock. */
      readonly durationMs?: number;
      /** The end row's status detail when it is not the plain 'ok'. */
      readonly detail?: string;
    };

function isToolEvent(event: AuditEvent, action: string): boolean {
  return event.source === 'tool' && event.action === action;
}

function outcomeOf(end: AuditEvent): InvocationOutcome {
  const status = end.detail?.trim() ?? '';
  return status === '' || status === 'ok' ? 'ok' : 'error';
}

function durationOf(start: AuditEvent | undefined, end: AuditEvent): number | undefined {
  // The persisted server duration is the truth when the end row carries it.
  if (typeof end.duration_ms === 'number' && end.duration_ms > 0) return end.duration_ms;
  if (start === undefined) return undefined;
  const startMs = Date.parse(start.created_at);
  const endMs = Date.parse(end.created_at);
  if (Number.isNaN(startMs) || Number.isNaN(endMs) || endMs < startMs) return undefined;
  return endMs - startMs;
}

/**
 * Project the newest-first event feed onto compact rows: tool start/end pairs
 * merge into one invocation row at the end's feed position; every other event
 * (mcp/skill/share, orphan tool rows) stays a plain event row in place.
 */
export function pairAuditEvents(events: readonly AuditEvent[]): readonly AuditRow[] {
  // Walk oldest→newest so each end can claim its start. Exact correlation wins;
  // the per-name stack is the fallback. A consumed start is lazily skipped when
  // the other registry still references it.
  const openByCorrelation = new Map<string, number>(); // correlation → start index
  const openStarts = new Map<string, number[]>(); // tool name → stack of indexes
  const pairedWith = new Map<number, number>(); // end index → start index
  const consumed = new Set<number>(); // start indexes merged into a pair
  for (let i = events.length - 1; i >= 0; i -= 1) {
    const event = events[i];
    if (event === undefined) continue;
    if (isToolEvent(event, 'start')) {
      if (event.correlation) openByCorrelation.set(event.correlation, i);
      const stack = openStarts.get(event.target) ?? [];
      stack.push(i);
      openStarts.set(event.target, stack);
      continue;
    }
    if (isToolEvent(event, 'end')) {
      let startIndex: number | undefined;
      if (event.correlation) {
        startIndex = openByCorrelation.get(event.correlation);
        if (startIndex !== undefined) openByCorrelation.delete(event.correlation);
      }
      if (startIndex === undefined) {
        const stack = openStarts.get(event.target);
        while (stack !== undefined && stack.length > 0) {
          const candidate = stack.pop();
          if (candidate === undefined || consumed.has(candidate)) continue;
          // Never cross-claim: a candidate carrying a DIFFERENT correlation
          // belongs to another invocation (still reachable via the exact map).
          const candidateCorr = events[candidate]?.correlation;
          if (candidateCorr && event.correlation && candidateCorr !== event.correlation) continue;
          startIndex = candidate;
          break;
        }
      }
      if (startIndex !== undefined && !consumed.has(startIndex)) {
        pairedWith.set(i, startIndex);
        consumed.add(startIndex);
      }
    }
  }

  const rows: AuditRow[] = [];
  for (let i = 0; i < events.length; i += 1) {
    const event = events[i];
    if (event === undefined || consumed.has(i)) continue;
    const startIndex = pairedWith.get(i);
    if (startIndex !== undefined) {
      const start = events[startIndex];
      const outcome = outcomeOf(event);
      const durationMs = durationOf(start, event);
      rows.push({
        kind: 'invocation',
        toolName: event.target,
        outcome,
        at: event.created_at,
        ...(durationMs !== undefined ? { durationMs } : {}),
        ...(outcome === 'error' && event.detail !== undefined ? { detail: event.detail } : {}),
      });
      continue;
    }
    if (isToolEvent(event, 'start')) {
      // Orphan start: still running (or crashed) — its own row, running state.
      rows.push({
        kind: 'invocation',
        toolName: event.target,
        outcome: 'running',
        at: event.created_at,
      });
      continue;
    }
    rows.push({ kind: 'event', event });
  }
  return rows;
}
