import { useCallback, useMemo, useState } from 'react';
import type {
  ElicitationQuestion,
  ElicitationResolved,
  ElicitationSignal,
} from '../chat/sseAdapter_elicitation';

// useThreadElicitations holds the thread's MCP forms as the stream tells them: a question
// arrives, and its resolution settles it. The server persists nothing. A reload gets the forms
// back from the attach's replay, or from the run's own list when the replay no longer reaches
// them (ExternalStoreChat_liveRun).

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

/**
 * The cards still worth showing once the thread stops streaming: only those of the run the
 * server still names live. A form never outlives its run, and a tab can miss the resolution
 * that says so: a Stop aborts the stream first, and a cut stream loses whatever was in flight.
 */
export function keepLiveRun(
  items: readonly ElicitationItem[],
  liveRunId: string | undefined,
): readonly ElicitationItem[] {
  const kept = items.filter((item) => item.question.run_id === liveRunId);
  return kept.length === items.length ? items : kept;
}

interface ThreadForms {
  readonly scope: symbol;
  readonly items: readonly ElicitationItem[];
  readonly retired: ReadonlySet<string>;
}

function replaceItems(current: ThreadForms, items: readonly ElicitationItem[]): ThreadForms {
  const live = new Set(items.map((item) => item.question.run_id));
  const retired = new Set(current.retired);
  for (const item of current.items) {
    if (!live.has(item.question.run_id)) retired.add(item.question.run_id);
  }
  return { ...current, items, retired };
}

export function useThreadElicitations(
  threadId: string,
  isRunning: boolean,
  liveRunId: string | undefined,
): {
  readonly items: readonly ElicitationItem[];
  readonly onSignal: (signal: ElicitationSignal) => void;
  readonly onSnapshot: (runId: string, questions: readonly ElicitationQuestion[]) => void;
} {
  const scope = useMemo(() => Symbol(threadId), [threadId]);
  const [state, setState] = useState<ThreadForms>({ scope, items: [], retired: new Set() });
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
  // Might Not Need an Effect"): the pruned list is stored so a later run cannot bring it back.
  const held = state.scope === scope ? state.items : [];
  const items = isRunning ? held : keepLiveRun(held, liveRunId);
  if (state.scope !== scope) setState({ scope, items, retired: new Set() });
  else if (items !== held) setState(replaceItems(state, items));
  return { items, onSignal, onSnapshot };
}
