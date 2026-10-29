import { useCallback } from 'react';
import type { AppendMessage, ThreadMessageLike } from '@assistant-ui/react';
import { appendMessageText, userMessage } from './ExternalStoreChat_folds';
import { backendSeqAt } from './messageSeq';

// ExternalStoreChat_branches — the D-09 branch-edit callbacks split out of
// ExternalStoreChat.tsx (600-LOC cap, refactor-on-touch when the RS-07 resilient
// stream adoption landed): the onEdit/onReload fork-and-re-run handlers. The stream
// scaffolding (foldReRun) stays in ./ExternalStoreChat_streams; this hook only maps visible
// messages → diverge seqs → re-run posts. The visible-message → seq mapping itself lives in
// ./messageSeq, which the compaction marker reads too.

export interface BranchEditsArgs {
  readonly threadId: string;
  readonly messages: readonly ThreadMessageLike[];
  readonly foldReRun: (
    url: string,
    body: unknown,
    base: ThreadMessageLike[],
    sentUser: boolean,
  ) => Promise<void>;
}

export function useBranchEdits({ threadId, messages, foldReRun }: BranchEditsArgs) {
  // onEdit (D-09): edit a USER turn → slice to the parent, append the edited user turn (a
  // fresh id), then POST /edit (fork a sibling branch off the diverging turn's parent +
  // re-run). The runtime tracks the prior user turn + its answer as a sibling branch. A turn
  // whose seq is not known is not forked: any number sent in its place names another turn.
  const onEdit = useCallback(
    async (message: AppendMessage) => {
      const text = appendMessageText(message);
      const sourceId = message.sourceId ?? message.parentId;
      const sourceIndex = messages.findIndex((m) => m.id === sourceId);
      const seq = backendSeqAt(messages, sourceIndex);
      if (seq === undefined) return;
      // The edited user turn replaces the old one (a fresh id); everything after the
      // edited source is dropped from THIS branch (the runtime keeps it as the sibling).
      const base: ThreadMessageLike[] = [...messages.slice(0, sourceIndex), userMessage(text)];
      await foldReRun(
        `/api/conversations/${threadId}/edit`,
        { diverge_seq: seq, role: 'user', content: text },
        base,
        true,
      );
    },
    [threadId, messages, foldReRun],
  );

  // onReload (D-09): regenerate an ASSISTANT turn → slice to the parent user turn, then
  // POST /edit (role assistant, no content) so the agent produces a fresh assistant turn on
  // a new sibling branch. parentId is the user turn the assistant answered, and the turn
  // that forks is the regenerated message's own: after a resumed call it is not the turn
  // after the user's, which is the call's result.
  const onReload = useCallback(
    async (parentId: string | null) => {
      const parentIndex = messages.findIndex((m) => m.id === parentId);
      // WR-02: an assistant turn's parent (the user turn it answered) is ALWAYS in the
      // visible list, so a not-found parent here is a stale/unknown id — never the legitimate
      // first-turn case (unlike onEdit, whose parent can be the invisible system/root). An
      // unknown parent would replace the whole visible thread (base = slice(0,0)) — a silent
      // destructive re-run. Bail before it.
      if (parentIndex < 0) return;
      const seq = backendSeqAt(messages, parentIndex + 1);
      if (seq === undefined) return;
      const base: ThreadMessageLike[] = messages.slice(0, parentIndex + 1);
      await foldReRun(
        `/api/conversations/${threadId}/edit`,
        { diverge_seq: seq, role: 'assistant', content: '' },
        base,
        false,
      );
    },
    [threadId, messages, foldReRun],
  );

  return { onEdit, onReload };
}
