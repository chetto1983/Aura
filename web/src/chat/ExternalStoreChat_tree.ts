import {
  ExportedMessageRepository,
  bindExternalStoreMessage,
  getExternalStoreMessages,
  type ThreadMessage,
  type ThreadMessageLike,
} from '@assistant-ui/react';
import { placeAssistant } from './ExternalStoreChat_folds';
import type { ThreadTree } from './sseAdapter_snapshot';

// ExternalStoreChat_tree — the conversation tree inside the runtime (spec 2026-10-27).
//
// The history load imports every branch, so the BranchPicker moves between them without
// asking the server again. A switch hands back the host messages of the branch picked, which
// is why every imported message is bound to the host object it came from: the runtime only
// keeps the host objects of the branch on screen, and getExternalStoreMessages finds nothing
// for a message no one bound. A run's seqs land on the messages it produced, so the next
// send, edit or regenerate addresses real turns.

export interface TreeImporter {
  readonly import: (repository: ExportedMessageRepository) => void;
}

/** Replace the runtime's whole repository with the tree: no message of the previous thread,
 * or of a stale snapshot, survives. The runtime then hands the branch on screen to onImport. */
export function importThreadTree(thread: TreeImporter, tree: ThreadTree): void {
  const repository = ExportedMessageRepository.fromBranchableArray(tree.items, {
    headId: tree.headId,
  });
  repository.messages.forEach(({ message }, index) => {
    const host = tree.items[index]?.message;
    // oxlint-disable-next-line typescript/no-deprecated -- assistant-ui 0.15.31 marks it experimental, "not scheduled for removal"; without it a switch to an imported branch hands back no host messages (pinned by ExternalStoreChat.branches.test.tsx).
    if (host !== undefined) bindExternalStoreMessage(message, host);
  });
  thread.import(repository);
}

/** The tree with its messages passed through a per-message fold, such as the assets each
 * user turn declares. */
export function mapTreeMessages(
  tree: ThreadTree,
  fold: (messages: readonly ThreadMessageLike[]) => ThreadMessageLike[],
): ThreadTree {
  const messages = fold(tree.items.map((item) => item.message));
  return {
    headId: tree.headId,
    items: tree.items.map((item, index) => ({ ...item, message: messages[index] ?? item.message })),
  };
}

/** The host messages behind the runtime's messages: what onImport and setMessages store. */
export function hostMessages(messages: readonly ThreadMessage[]): ThreadMessageLike[] {
  return messages.flatMap((message) => getExternalStoreMessages<ThreadMessageLike>(message));
}

function withSeqs(message: ThreadMessageLike, seqs: { backendSeq: number; leafSeq?: number }) {
  const custom = { ...(message.metadata?.custom ?? {}), ...seqs };
  return { ...message, metadata: { ...message.metadata, custom } };
}

function reportedSeq(value: unknown): number | undefined {
  return typeof value === 'number' ? value : undefined;
}

/**
 * Place the assistant message of a send, an edit or a regenerate, and give the turns its run
 * reported their seqs. The run starts from head_seq and the first turn it writes is the one
 * after it, the assistant's own. When the run was given a user message (sentUser), head_seq is
 * that message. A stream that reported no head is placed as it is. A resume or an attach does
 * not come through here: what it writes first is the paused call's result, not its answer, so
 * its message keeps only the leafSeq a send from it continues.
 */
export function placeTurn(
  messages: readonly ThreadMessageLike[],
  index: number,
  assistant: ThreadMessageLike,
  sentUser: boolean,
): ThreadMessageLike[] {
  const custom = assistant.metadata?.custom as { headSeq?: unknown } | undefined;
  const head = reportedSeq(custom?.headSeq);
  if (head === undefined) return placeAssistant(messages, index, assistant);
  const next = placeAssistant(messages, index, withSeqs(assistant, { backendSeq: head + 1 }));
  const placedAt = index >= 0 && index < messages.length ? index : next.length - 1;
  const user = next[placedAt - 1];
  if (sentUser && user?.role === 'user') {
    next[placedAt - 1] = withSeqs(user, { backendSeq: head, leafSeq: head });
  }
  return next;
}
