import type { ThreadMessageLike } from '@assistant-ui/react';
import type { JSONPatchOp } from './sseAdapter_frames';

// messageSeq — the one mapping from a visible message to the backend turn seq behind it.
//
// Three features need it and they must agree: a branch edit forks at a seq, the compaction
// marker is drawn at a seq, and a search hit opens the thread at a seq. If they disagreed the
// marker would sit one turn away from the history it describes, which is precisely the kind
// of off-by-one nobody reports as a bug.
//
// A seq is known or it is not. Rehydrated messages carry theirs from the snapshot ids
// (msg-<seq>), and live ones receive theirs from the seqs their run reports. A message with
// neither has no seq: guessing it from the visible position, as the cockpit once did, named
// another turn as soon as the thread had branches (spec 2026-10-27, §4.4).

function positiveSeq(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 ? value : undefined;
}

/** The backend turn a visible message diverges from: its first turn. Undefined when it is not
 * known, which refuses an edit or a regenerate rather than forking at the wrong turn. */
export function backendSeqAt(
  messages: readonly ThreadMessageLike[],
  index: number,
): number | undefined {
  return positiveSeq(messages[index]?.metadata?.custom?.backendSeq);
}

/** The last turn folded into a message: what a send that follows it continues
 * (aura.parent_seq). A message whose run did not report one falls back to its own seq. */
export function leafSeqOf(message: ThreadMessageLike | undefined): number | undefined {
  const custom = message?.metadata?.custom;
  return positiveSeq(custom?.leafSeq) ?? positiveSeq(custom?.backendSeq);
}

/** The seqs a run reports on its STATE_DELTA (spec 2026-10-27, D7): head_seq, the turn it
 * starts from, and leaf_seq, the turn it ends on. */
export function turnSeqsFromDelta(ops: readonly JSONPatchOp[]): {
  headSeq?: number;
  leafSeq?: number;
} {
  const byPath = new Map(ops.map((op) => [op.path, op.value]));
  const headSeq = positiveSeq(byPath.get('/head_seq'));
  const leafSeq = positiveSeq(byPath.get('/leaf_seq'));
  return {
    ...(headSeq !== undefined ? { headSeq } : {}),
    ...(leafSeq !== undefined ? { leafSeq } : {}),
  };
}

/** The id of the message the compaction marker belongs AFTER: the last one the summary
 * speaks for. Undefined when nothing is compacted, or when the watermark falls before the
 * first visible message — a thread scrolled to a window that starts past it draws no marker
 * rather than one at the top pretending to describe turns that are not there. */
export function compactionAnchorId(
  messages: readonly ThreadMessageLike[],
  coversThroughSeq: number,
): string | undefined {
  for (let index = messages.length - 1; index >= 0; index--) {
    const seq = backendSeqAt(messages, index);
    if (seq !== undefined && seq <= coversThroughSeq) return messages[index]?.id;
  }
  return undefined;
}

/** The id of the visible message behind a backend turn seq, the inverse of backendSeqAt.
 * Undefined when no visible message carries it: a turn the thread does not draw, such as a
 * worker report or a tool result folded into its assistant card. */
export function messageIdAtSeq(
  messages: readonly ThreadMessageLike[],
  seq: number,
): string | undefined {
  return messages.find((_, index) => backendSeqAt(messages, index) === seq)?.id;
}
