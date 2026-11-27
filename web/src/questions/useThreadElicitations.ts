import { useCallback, useMemo, useState } from 'react';
import type {
  ElicitationQuestion,
  ElicitationResolved,
  ElicitationSignal,
} from '../chat/sseAdapter_elicitation';

// useThreadElicitations holds the thread's MCP forms as the stream tells them: a question
// arrives, and its resolution settles it. The server persists nothing. A reload gets the forms
// back from the attach's replay, or from the run's own list when the replay no longer reaches
// them (ExternalStoreChat_liveRun). A settled form that names the call it arrived in becomes
// that call's receipt, drawn in the call's row for as long as the thread is open. One that
// names no call stays above the composer until the next run, as an ask_user receipt does.

export type ElicitationOutcome = 'accepted' | 'declined' | 'cancelled' | 'expired';

export interface ElicitationItem {
  readonly question: ElicitationQuestion;
  readonly outcome?: ElicitationOutcome;
}

function outcomeOf(resolved: ElicitationResolved): ElicitationOutcome {
  if (resolved.expired === true) return 'expired';
  if (resolved.action === 'accept') return 'accepted';
  return resolved.action === 'decline' ? 'declined' : 'cancelled';
}

/**
 * Fold one signal into the list. A question already held (a replay) changes nothing. A thread
 * runs one run at a time, so the first question of a new run drops every card of earlier ones.
 * A resolution settles its question once and is ignored for a question never seen.
 */
export function applyElicitationSignal(
  items: readonly ElicitationItem[],
  signal: ElicitationSignal,
): readonly ElicitationItem[] {
  if (signal.kind === 'question') {
    const { question } = signal;
    if (items.some((item) => item.question.id === question.id)) return items;
    const sameRun = items.filter((item) => item.question.run_id === question.run_id);
    return [...sameRun, { question }];
  }
  const outcome = outcomeOf(signal.resolved);
  return items.map((item) =>
    item.question.id === signal.resolved.id && item.outcome === undefined
      ? { ...item, outcome }
      : item,
  );
}

export interface ThreadForms {
  readonly scope: symbol;
  readonly items: readonly ElicitationItem[];
  readonly retired: ReadonlySet<string>;
  /** The receipts drawn in their call's row. Only grows: a later run does not take them. */
  readonly anchored: readonly ElicitationItem[];
}

const isAnchored = (item: ElicitationItem) =>
  item.outcome !== undefined && item.question.tool_call_id !== undefined;

/** The forms drawn above the composer: the open ones, and receipts that name no call. */
export function unanchored(forms: ThreadForms): readonly ElicitationItem[] {
  return forms.items.filter((item) => !isAnchored(item));
}

/** Copies each newly settled form that names its call into the anchored receipts. Returns
 *  forms itself when there is none. */
export function anchorReceipts(forms: ThreadForms): ThreadForms {
  const held = new Set(forms.anchored.map((item) => item.question.id));
  const fresh = forms.items.filter((item) => isAnchored(item) && !held.has(item.question.id));
  return fresh.length === 0 ? forms : { ...forms, anchored: [...forms.anchored, ...fresh] };
}

/**
 * The forms as the thread's run state leaves them. Once the thread stops streaming, every run but
 * the one the server still names live has ended: it is retired, so a late replay cannot bring it
 * back, and its unanswered forms go, since a form never outlives its run and a tab can miss the
 * resolution that says so (a Stop aborts the stream first, a cut stream loses what was in
 * flight). Its receipts stay. While a run streams, the receipts of retired runs go: the thread
 * runs one run at a time, so a new one has started. Returns forms itself when nothing changes.
 */
export function settleForms(
  forms: ThreadForms,
  isRunning: boolean,
  liveRunId: string | undefined,
): ThreadForms {
  if (isRunning) {
    const items = forms.items.filter((item) => !forms.retired.has(item.question.run_id));
    return items.length === forms.items.length ? forms : { ...forms, items };
  }
  const ended = forms.items
    .map((item) => item.question.run_id)
    .filter((runId) => runId !== liveRunId && !forms.retired.has(runId));
  const items = forms.items.filter(
    (item) => item.question.run_id === liveRunId || item.outcome !== undefined,
  );
  if (ended.length === 0 && items.length === forms.items.length) return forms;
  return { ...forms, items, retired: new Set([...forms.retired, ...ended]) };
}

function replaceItems(current: ThreadForms, items: readonly ElicitationItem[]): ThreadForms {
  const live = new Set(items.map((item) => item.question.run_id));
  const retired = new Set(current.retired);
  for (const item of current.items) {
    if (!live.has(item.question.run_id)) retired.add(item.question.run_id);
  }
  // Every settlement arrives through here, so a receipt is anchored in the same update that
  // settles it, before a later question in the same batch can drop it with its run.
  return anchorReceipts({ ...current, items, retired });
}

function emptyForms(scope: symbol): ThreadForms {
  return { scope, items: [], retired: new Set(), anchored: [] };
}

export function useThreadElicitations(
  threadId: string,
  isRunning: boolean,
  liveRunId: string | undefined,
): {
  /** The forms drawn above the composer. */
  readonly items: readonly ElicitationItem[];
  /** The receipts drawn in their call's row. */
  readonly receipts: readonly ElicitationItem[];
  readonly onSignal: (signal: ElicitationSignal) => void;
  readonly onSnapshot: (runId: string, questions: readonly ElicitationQuestion[]) => void;
} {
  const scope = useMemo(() => Symbol(threadId), [threadId]);
  const [state, setState] = useState<ThreadForms>(() => emptyForms(scope));
  const onSignal = useCallback(
    (signal: ElicitationSignal) => {
      setState((current) => {
        if (current.scope !== scope) return current;
        if (signal.kind === 'question' && current.retired.has(signal.question.run_id))
          return current;
        return replaceItems(current, applyElicitationSignal(current.items, signal));
      });
    },
    [scope],
  );
  const onSnapshot = useCallback(
    (runId: string, questions: readonly ElicitationQuestion[]) => {
      setState((current) => {
        if (current.scope !== scope || current.retired.has(runId)) return current;
        const sameRun = current.items.filter((item) => item.question.run_id === runId);
        const ordered = questions
          .filter((question) => question.run_id === runId)
          .map(
            (question) => sameRun.find((item) => item.question.id === question.id) ?? { question },
          );
        const listed = new Set(ordered.map((item) => item.question.id));
        return replaceItems(current, [
          ...ordered,
          ...sameRun.filter((item) => !listed.has(item.question.id)),
        ]);
      });
    },
    [scope],
  );
  // Adjusted while rendering, the way React adjusts state that follows a prop (react.dev, "You
  // Might Not Need an Effect"): the settled list is stored so a later run cannot bring it back.
  const current = state.scope === scope ? state : emptyForms(scope);
  const settled = settleForms(current, isRunning, liveRunId);
  if (settled !== state) setState(settled);
  return { items: unanchored(settled), receipts: settled.anchored, onSignal, onSnapshot };
}
