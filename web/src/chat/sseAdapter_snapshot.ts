import type { ThreadMessageLike } from '@assistant-ui/react';
import { isDisplayPayload } from './displays/types';
import { isMcpViewDescriptor } from './mcpapps/hostProtocol';
import {
  errorDetail,
  type ChatPart,
  type MessagesSnapshotFrame,
  type ReasoningPart,
  type SnapshotMessage,
  type SnapshotToolCall,
  type ToolPart,
} from './sseAdapter_frames';

// sseAdapter_snapshot — persisted history rehydration. Projects a one-shot
// MESSAGES_SNAPSHOT body (GET /threads/{id}/messages) onto the visible
// assistant-ui chat model. Pure functions; depends only on the leaf
// sseAdapter_frames module and the display types.

// ---------------------------------------------------------------------------
// Persisted history snapshots — GET /threads/{id}/messages (one-shot JSON).
// ---------------------------------------------------------------------------

function textFromSnapshotContent(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return '';
  }
}

function stripAuraContextEnvelope(text: string): string {
  const split = splitUserMessageMarker(text);
  if (split === null) return text;
  const prefix = text.slice(0, split.index);
  if (!isAuraContextEnvelope(prefix)) return text;
  return text.slice(split.index + split.length);
}

function splitUserMessageMarker(text: string): { index: number; length: number } | null {
  for (const marker of ['User message:\n', 'User message:\r\n']) {
    const index = text.indexOf(marker);
    if (index >= 0) return { index, length: marker.length };
  }
  return null;
}

function isAuraContextEnvelope(prefix: string): boolean {
  let rest = prefix;
  let seen = false;
  for (;;) {
    rest = rest.replace(/^[\t\n\r ]+/, '');
    if (rest.startsWith('<knowledge_base trust="operator_pinned_context">')) {
      const next = consumeAuraContextBlock(rest, '</knowledge_base>');
      if (next === null) return false;
      rest = next;
      seen = true;
      continue;
    }
    if (rest.startsWith('<attachments trust="untrusted_user_uploads">')) {
      const next = consumeAuraContextBlock(rest, '</attachments>');
      if (next === null) return false;
      rest = next;
      seen = true;
      continue;
    }
    return seen && rest.trim() === '';
  }
}

function consumeAuraContextBlock(text: string, closeTag: string): string | null {
  const end = text.indexOf(closeTag);
  if (end < 0) return null;
  return text.slice(end + closeTag.length);
}

function visibleTextForSnapshotRole(role: string, text: string): string {
  return role === 'user' ? stripAuraContextEnvelope(text) : text;
}

function asSnapshotMessages(value: unknown): readonly SnapshotMessage[] {
  if (typeof value !== 'object' || value === null) return [];
  const snapshot = value as MessagesSnapshotFrame;
  if (snapshot.type !== 'MESSAGES_SNAPSHOT' || !Array.isArray(snapshot.messages)) return [];
  return snapshot.messages.filter(
    (message): message is SnapshotMessage => typeof message === 'object' && message !== null,
  );
}

/** The turn seq a snapshot id names: msg-<seq>. A result the server synthesized for a call
 *  that never got one is msg-<seq>-<call> and names no turn of its own. */
function seqFromSnapshotId(id: string): number | undefined {
  const seq = /^msg-(\d+)$/.exec(id)?.[1];
  return seq === undefined ? undefined : Number(seq);
}

function toolCallsFromSnapshot(value: unknown): ToolPart[] {
  if (!Array.isArray(value)) return [];
  const parts: ToolPart[] = [];
  for (const raw of value) {
    if (typeof raw !== 'object' || raw === null) continue;
    const call = raw as SnapshotToolCall;
    if (typeof call.id !== 'string' || call.id.length === 0) continue;
    parts.push({
      type: 'tool-call',
      toolCallId: call.id,
      toolName:
        typeof call.function?.name === 'string' && call.function.name.length > 0
          ? call.function.name
          : '',
      argsText:
        typeof call.function?.arguments === 'string'
          ? call.function.arguments
          : textFromSnapshotContent(call.function?.arguments),
      // D-06: a re-derived display payload (when present) rides the tool part so
      // the DisplayRouter renders identically on replay. Tolerated when absent.
      ...(isDisplayPayload(call.display) ? { display: call.display } : {}),
      ...(isMcpViewDescriptor(call.mcpView) ? { mcpView: call.mcpView } : {}),
      ...(call.awaitingInput === true ? { awaitingInput: true } : {}),
    });
  }
  return parts;
}

/**
 * 1.12 rehydrate: project the snapshot's optional reasoning decoration
 * (`reasoning` + `reasoningDurationMs`, one per answer-shaped assistant
 * message) onto the SAME { type: 'reasoning' } part the live stream produces,
 * so the drawer renders identically on replay. Returns [] when the field is
 * absent/empty — the part list stays byte-identical to the pre-1.12 shape.
 */
function reasoningPartFromSnapshot(message: SnapshotMessage): ReasoningPart[] {
  if (typeof message.reasoning !== 'string' || message.reasoning.length === 0) return [];
  const duration = message.reasoningDurationMs;
  const durationMs =
    typeof duration === 'number' && Number.isFinite(duration) && duration > 0
      ? duration
      : undefined;
  return [
    {
      type: 'reasoning',
      text: message.reasoning,
      ...(durationMs !== undefined ? { durationMs } : {}),
    },
  ];
}

/** The declared attachment ids of a user turn, or [] when the field is absent (a turn
 *  older than migration 0116) or malformed. Never partially trusted: a non-string entry
 *  drops rather than becoming a lookup that cannot match. */
function attachmentIDsFromSnapshot(message: SnapshotMessage): string[] {
  if (!Array.isArray(message.attachmentIds)) return [];
  return message.attachmentIds.filter(
    (id): id is string => typeof id === 'string' && id.length > 0,
  );
}

/** The outcome the server read from the call's ledger row; absent keys for a success. */
function toolOutcomeFromSnapshot(message: SnapshotMessage): Pick<ToolPart, 'isError' | 'refused'> {
  return {
    ...(message.isError === true ? { isError: true } : {}),
    ...(message.refused === true ? { refused: true } : {}),
  };
}

/** Merge a tool result onto its call in the card, or report false when the card has none. */
function mergeToolResult(
  card: ThreadMessageLike | undefined,
  toolCallId: string,
  result: string,
  outcome: Pick<ToolPart, 'isError' | 'refused'>,
): ThreadMessageLike | undefined {
  if (card?.role !== 'assistant' || typeof card.content === 'string') return undefined;
  const parts = [...card.content];
  const partIndex = parts.findIndex(
    (part) => part.type === 'tool-call' && part.toolCallId === toolCallId,
  );
  const part = parts[partIndex];
  if (part?.type !== 'tool-call') return undefined;
  parts[partIndex] = { ...part, result, ...outcome };
  return { ...card, content: parts };
}

export interface ThreadTreeItem {
  readonly message: ThreadMessageLike;
  readonly parentId: string | null;
}

/** The conversation tree, in the order a repository imports it (parents first), and the
 *  message the thread opens on. */
export interface ThreadTree {
  readonly items: readonly ThreadTreeItem[];
  readonly headId: string | null;
}

interface DrawnMessage {
  message: ThreadMessageLike;
  readonly parentId: string | null;
  /** The last turn folded into the message: what a send from it continues. */
  leafSeq: number | undefined;
}

/** One drawn message per visible turn, and the drawn message every row shows as. */
class TreeBuilder {
  readonly drawn: DrawnMessage[] = [];
  private readonly byId = new Map<string, DrawnMessage>();
  private readonly shownAs = new Map<string, string | null>();
  private last: string | null = null;

  /** The drawn message a row hangs from. A snapshot without parentId is one branch, and a
   *  parent it does not carry falls back to the row before, never to a node the runtime
   *  cannot place. */
  parentOf(raw: SnapshotMessage): string | null {
    if (raw.parentId === null) return null;
    if (typeof raw.parentId === 'string') {
      const shown = this.shownAs.get(raw.parentId);
      if (shown !== undefined) return shown;
    }
    return this.last;
  }

  get(id: string | null): DrawnMessage | undefined {
    return id === null ? undefined : this.byId.get(id);
  }

  draw(
    message: ThreadMessageLike & { id: string },
    parentId: string | null,
    seq: number | undefined,
  ) {
    const drawn = { message, parentId, leafSeq: seq };
    this.drawn.push(drawn);
    this.byId.set(message.id, drawn);
    return message.id;
  }

  /** Record what the row shows as, and fold its seq into that message. */
  show(id: string, as: string | null, seq: number | undefined): void {
    this.shownAs.set(id, as);
    this.last = as;
    const target = this.get(as);
    if (target !== undefined && seq !== undefined)
      target.leafSeq = Math.max(target.leafSeq ?? 0, seq);
  }

  tree(): ThreadTree {
    const items = this.drawn.map(({ message, parentId, leafSeq }) => {
      const backendSeq = seqFromSnapshotId(message.id ?? '');
      const seqs = {
        ...(backendSeq !== undefined ? { backendSeq } : {}),
        ...(leafSeq !== undefined ? { leafSeq } : {}),
      };
      const custom = { ...seqs, ...(message.metadata?.custom ?? {}) };
      return {
        message:
          Object.keys(custom).length > 0
            ? { ...message, metadata: { ...message.metadata, custom } }
            : message,
        parentId,
      };
    });
    return { items, headId: this.last };
  }
}

/** The drawn message of one snapshot row, or undefined for a row shown as its parent. */
function drawnFromSnapshot(
  raw: SnapshotMessage,
  id: string,
  role: string,
  text: string,
): (ThreadMessageLike & { id: string }) | undefined {
  if (role === 'tool') {
    return {
      id,
      role: 'assistant',
      content: [
        {
          type: 'tool-call',
          toolCallId: raw.toolCallId as string,
          toolName: '',
          argsText: '',
          result: text,
          ...toolOutcomeFromSnapshot(raw),
        },
      ],
      status: { type: 'complete', reason: 'stop' },
    };
  }
  if (role === 'user') {
    // The turn's OWN record of what was sent with it (migration 0116). It rides
    // metadata.custom because that is where the fold already looks for the resolved
    // assets, and it is the ids that let that fold stop guessing by position.
    const declared = attachmentIDsFromSnapshot(raw);
    return {
      id,
      ...(declared.length > 0 ? { metadata: { custom: { attachmentIds: declared } } } : {}),
      role: 'user',
      content: [{ type: 'text', text }],
    };
  }
  if (role !== 'assistant' || raw.workerReport === true) return undefined;
  // Reasoning first (live order: the CoT span precedes the answer text).
  const content: ChatPart[] = [
    ...reasoningPartFromSnapshot(raw),
    ...toolCallsFromSnapshot(raw.toolCalls),
  ];
  if (text.length > 0 || content.length === 0) content.push({ type: 'text', text });
  return { id, role: 'assistant', content, status: { type: 'complete', reason: 'stop' } };
}

/**
 * Project a persisted MESSAGES_SNAPSHOT body onto the assistant-ui conversation tree. System
 * turns and worker reports are not drawn, and a tool result folds into the card of the call
 * it answers: each shows as the message it hangs from, and a child of it hangs from that
 * message. Every drawn message carries backendSeq, its own turn, and leafSeq, the last turn
 * folded into it.
 */
export function snapshotToThreadTree(snapshot: unknown): ThreadTree {
  const builder = new TreeBuilder();
  asSnapshotMessages(snapshot).forEach((raw, position) => {
    const id =
      typeof raw.id === 'string' && raw.id.length > 0 ? raw.id : `snapshot-${String(position)}`;
    const seq = seqFromSnapshotId(id);
    const parentId = builder.parentOf(raw);
    const role = typeof raw.role === 'string' ? raw.role : '';
    const text = visibleTextForSnapshotRole(role, textFromSnapshotContent(raw.content));
    const toolCallId = typeof raw.toolCallId === 'string' ? raw.toolCallId : '';
    if (role === 'tool' && toolCallId.length === 0) {
      builder.show(id, parentId, seq);
      return;
    }
    if (role === 'tool') {
      const card = builder.get(parentId);
      const merged = mergeToolResult(card?.message, toolCallId, text, toolOutcomeFromSnapshot(raw));
      if (card !== undefined && merged !== undefined) {
        card.message = merged;
        builder.show(id, parentId, seq);
        return;
      }
    }
    const drawn = drawnFromSnapshot(raw, id, role, text);
    builder.show(id, drawn === undefined ? parentId : builder.draw(drawn, parentId, seq), seq);
  });
  return builder.tree();
}

/** The branch a tree opens on: its head and every message above it, root first. */
export function treeHeadPath(tree: ThreadTree): ThreadMessageLike[] {
  const byId = new Map(tree.items.map((item) => [item.message.id, item]));
  const path: ThreadMessageLike[] = [];
  for (let id = tree.headId; id !== null;) {
    const item = byId.get(id);
    if (item === undefined) break;
    path.push(item.message);
    id = item.parentId;
  }
  return path.reverse();
}

/** The branch the persisted snapshot opens on, for a caller that replaces the visible list. */
export function snapshotToThreadMessages(snapshot: unknown): ThreadMessageLike[] {
  return treeHeadPath(snapshotToThreadTree(snapshot));
}

export async function fetchThreadTree(threadId: string, signal?: AbortSignal): Promise<ThreadTree> {
  const init: RequestInit = {
    method: 'GET',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  };
  if (signal !== undefined) init.signal = signal;
  const res = await fetch(`/threads/${encodeURIComponent(threadId)}/messages`, init);
  if (!res.ok) {
    throw new Error(await errorDetail(res));
  }
  return snapshotToThreadTree(await res.json());
}
