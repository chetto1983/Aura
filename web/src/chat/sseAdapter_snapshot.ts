import type { ThreadMessageLike } from '@assistant-ui/react';
import { isDisplayPayload } from './displays/types';
import {
  errorDetail,
  type ChatPart,
  type MessagesSnapshotFrame,
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

function metadataFromSnapshotId(id: string | undefined): ThreadMessageLike['metadata'] | undefined {
  const seq = /^msg-(\d+)$/.exec(id ?? '')?.[1];
  if (seq === undefined) return undefined;
  return { custom: { backendSeq: Number(seq) } };
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
    });
  }
  return parts;
}

function mergeToolResult(
  messages: ThreadMessageLike[],
  toolCallId: string,
  result: string,
): boolean {
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    const candidate = messages[i];
    if (candidate?.role !== 'assistant') continue;
    const parts = typeof candidate.content === 'string' ? [] : [...candidate.content];
    const partIndex = parts.findIndex(
      (part) => part.type === 'tool-call' && part.toolCallId === toolCallId,
    );
    if (partIndex < 0) continue;
    const part = parts[partIndex];
    if (part?.type !== 'tool-call') continue;
    parts[partIndex] = { ...part, result };
    messages[i] = { ...candidate, content: parts };
    return true;
  }
  return false;
}

/**
 * Project a persisted MESSAGES_SNAPSHOT body onto the visible assistant-ui chat
 * model. System turns stay out of the UI (branch/edit seq math depends on that);
 * tool result rows merge into the preceding assistant tool-call card.
 */
export function snapshotToThreadMessages(snapshot: unknown): ThreadMessageLike[] {
  const messages: ThreadMessageLike[] = [];
  for (const raw of asSnapshotMessages(snapshot)) {
    const id = typeof raw.id === 'string' && raw.id.length > 0 ? raw.id : undefined;
    const metadata = metadataFromSnapshotId(id);
    const role = typeof raw.role === 'string' ? raw.role : '';
    const text = visibleTextForSnapshotRole(role, textFromSnapshotContent(raw.content));
    if (role === 'system') continue;
    if (role === 'tool') {
      if (typeof raw.toolCallId === 'string' && raw.toolCallId.length > 0) {
        if (!mergeToolResult(messages, raw.toolCallId, text)) {
          messages.push({
            ...(id !== undefined ? { id } : {}),
            ...(metadata !== undefined ? { metadata } : {}),
            role: 'assistant',
            content: [
              {
                type: 'tool-call',
                toolCallId: raw.toolCallId,
                toolName: '',
                argsText: '',
                result: text,
              },
            ],
            status: { type: 'complete', reason: 'stop' },
          });
        }
      }
      continue;
    }
    if (role === 'user') {
      messages.push({
        ...(id !== undefined ? { id } : {}),
        ...(metadata !== undefined ? { metadata } : {}),
        role: 'user',
        content: [{ type: 'text', text }],
      });
      continue;
    }
    if (role === 'assistant') {
      const content: ChatPart[] = toolCallsFromSnapshot(raw.toolCalls);
      if (text.length > 0 || content.length === 0) content.push({ type: 'text', text });
      messages.push({
        ...(id !== undefined ? { id } : {}),
        ...(metadata !== undefined ? { metadata } : {}),
        role: 'assistant',
        content,
        status: { type: 'complete', reason: 'stop' },
      });
    }
  }
  return messages;
}

export async function fetchThreadMessages(
  threadId: string,
  signal?: AbortSignal,
): Promise<ThreadMessageLike[]> {
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
  return snapshotToThreadMessages(await res.json());
}
