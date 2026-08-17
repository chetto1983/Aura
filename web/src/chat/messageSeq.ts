import type { ThreadMessageLike } from '@assistant-ui/react';

// messageSeq — the one mapping from a visible message to the backend turn seq behind it.
//
// Two features need it and they must agree: a branch edit forks at a seq, and the compaction
// marker is drawn at a seq. If they disagreed the marker would sit one turn away from the
// history it describes, which is precisely the kind of off-by-one nobody reports as a bug.

/** The backend turn seq a visible message diverges from.
 *
 * Rehydrated snapshots carry metadata.custom.backendSeq from the GET /threads/{id}/messages
 * ids (msg-1, msg-2, …). Fresh in-memory turns fall back to the visible index + 1, because a
 * normal Aura conversation starts at the first user turn (no persisted system row). */
export function backendSeqAt(messages: readonly ThreadMessageLike[], index: number): number {
  if (index < 0) return 0;
  const seq = messages[index]?.metadata?.custom?.backendSeq;
  return typeof seq === 'number' && Number.isFinite(seq) && seq > 0 ? seq : index + 1;
}

/** The id of the message the compaction marker belongs AFTER: the last one the summary
 * speaks for. Undefined when nothing is compacted, or when the watermark falls before the
 * first visible message — a thread scrolled to a window that starts past it draws no marker
 * rather than one at the top pretending to describe turns that are not there. */
export function compactionAnchorId(
  messages: readonly ThreadMessageLike[],
  coversThroughSeq: number,
): string | undefined {
  if (coversThroughSeq <= 0) return undefined;
  for (let index = messages.length - 1; index >= 0; index--) {
    if (backendSeqAt(messages, index) <= coversThroughSeq) return messages[index]?.id;
  }
  return undefined;
}
