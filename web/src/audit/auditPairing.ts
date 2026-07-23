import type { AuditEvent } from '../admin/adminApi';

// auditPairing — merge each tool start/end pair of the admin activity feed into
// ONE compact row (operator directive: two rows per invocation flood the page).
//
// The wire DTO ({source, action, target, detail, created_at}) carries NO
// correlation id — aura.tool_invocations HAS tool_call_id + duration_ms but
// audit_store.go does not project them (server gap, noted for fix-plan) — so
// pairing is CONSERVATIVE and client-side: walking oldest→newest, an `end` row
// pairs with the MOST RECENT unpaired `start` of the SAME tool name (a per-name
// stack, so interleaved different tools pair correctly). Unpairable rows pass
// through untouched; an orphan start (still running / crashed) renders as its
// own running-state row. The merged row sits at the pair's END position, so the
// feed stays reverse-chronological.

export type InvocationOutcome = 'ok' | 'error' | 'running';

export type AuditRow =
  | { readonly kind: 'event'; readonly event: AuditEvent }
  | {
      readonly kind: 'invocation';
      readonly toolName: string;
      readonly outcome: InvocationOutcome;
      /** The row's anchor timestamp: the END ts (or the start ts when running). */
      readonly at: string;
      /** start→end wallclock; absent for orphans or unparseable timestamps. */
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

function durationBetween(start: AuditEvent, end: AuditEvent): number | undefined {
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
  // Walk oldest→newest so each end can claim the most recent open start.
  const openStarts = new Map<string, number[]>(); // tool name → stack of indexes
  const pairedWith = new Map<number, number>(); // end index → start index
  const consumed = new Set<number>(); // start indexes merged into a pair
  for (let i = events.length - 1; i >= 0; i -= 1) {
    const event = events[i];
    if (event === undefined) continue;
    if (isToolEvent(event, 'start')) {
      const stack = openStarts.get(event.target) ?? [];
      stack.push(i);
      openStarts.set(event.target, stack);
      continue;
    }
    if (isToolEvent(event, 'end')) {
      const startIndex = openStarts.get(event.target)?.pop();
      if (startIndex !== undefined) {
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
      const durationMs = start === undefined ? undefined : durationBetween(start, event);
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
