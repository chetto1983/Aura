import type { ThreadMessageLike } from '@assistant-ui/react';
import type { AguiFrame, ChatPart, ReasoningPart, TextPart, ToolPart } from './sseAdapter_frames';
import type { BudgetLimit, TurnUsage } from './sseAdapter_usage';

// sseAdapter_parts — the assistant-turn accumulator + its part builders, split
// out of sseAdapter.ts (600-LOC cap, the sseAdapter_frames/_usage sibling
// convention). Pure state manipulation: NO fetch, NO SSE parsing — reduceFrame
// (the frame → builder dispatch) stays in sseAdapter.ts and drives these.

type AssistantStatus = ThreadMessageLike['status'];

/**
 * The accumulator for one assistant turn. Parts preserve arrival order; a
 * tool-call part is keyed by toolCallId so ARGS/END/RESULT (and a tool_call_id
 * STATE_DELTA marker) merge onto the part the START created.
 */
export interface AssistantTurnState {
  readonly id: string;
  /** Ordered assistant parts exactly as they arrived on the AG-UI stream. */
  readonly content: ChatPart[];
  text: string;
  textOpen: boolean;
  reasoning: string;
  readonly textById: Map<string, number>;
  readonly reasoningById: Map<string, number>;
  /** ordered tool parts, by first-seen toolCallId. */
  readonly toolOrder: string[];
  readonly tools: Map<string, ToolPart>;
  readonly toolIndexById: Map<string, number>;
  usage?: TurnUsage;
  /** Set when the terminal STATE_DELTA says the loop budget cut this turn. */
  limit?: BudgetLimit;
  error?: string;
  status: AssistantStatus;
}

export function newAssistantTurn(id: string): AssistantTurnState {
  return {
    id,
    content: [],
    text: '',
    textOpen: false,
    reasoning: '',
    textById: new Map(),
    reasoningById: new Map(),
    toolOrder: [],
    tools: new Map(),
    toolIndexById: new Map(),
    status: { type: 'running' },
  };
}

export function frameTimestamp(frame: AguiFrame): number | undefined {
  const t = (frame as { readonly timestamp?: unknown }).timestamp;
  return typeof t === 'number' ? t : undefined;
}

export function ensureText(state: AssistantTurnState, messageId: string): TextPart {
  const existingIndex = state.textById.get(messageId);
  if (existingIndex !== undefined) {
    const existing = state.content[existingIndex];
    if (existing?.type === 'text') return existing;
  }
  const part: TextPart = { type: 'text', text: '' };
  state.textById.set(messageId, state.content.length);
  state.content.push(part);
  return part;
}

export function updateText(state: AssistantTurnState, messageId: string, text: string): void {
  const part = ensureText(state, messageId);
  const index = state.textById.get(messageId);
  if (index === undefined) return;
  state.content[index] = { ...part, text };
}

/** The `aura.discard` CUSTOM payload (amendment #191): the agent repudiated the prose
 *  it streamed on `message_id` — a draft the completion gate vetoed, or a partial a
 *  mid-stream retry replaced. */
export function isDiscardNotice(value: unknown): value is { readonly message_id: string } {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { readonly message_id?: unknown }).message_id === 'string'
  );
}

/**
 * Drop the text part a repudiated message streamed. The part leaves `content`, every
 * index map shifts down past it, and `text` is rebuilt from what remains, so the real
 * answer renders alone — measured live 2026-08-30, the cockpit showed two vetoed
 * drafts and never the answer that followed them.
 */
export function discardText(state: AssistantTurnState, messageId: string): void {
  const index = state.textById.get(messageId);
  if (index === undefined) return;
  state.content.splice(index, 1);
  state.textById.delete(messageId);
  for (const map of [state.textById, state.reasoningById, state.toolIndexById]) {
    for (const [key, i] of map) if (i > index) map.set(key, i - 1);
  }
  state.text = state.content.map((part) => (part.type === 'text' ? part.text : '')).join('');
  state.textOpen = false;
}

export function ensureReasoning(state: AssistantTurnState, messageId: string): ReasoningPart {
  const existingIndex = state.reasoningById.get(messageId);
  if (existingIndex !== undefined) {
    const existing = state.content[existingIndex];
    if (existing?.type === 'reasoning') return existing;
  }
  const part: ReasoningPart = { type: 'reasoning', text: '' };
  state.reasoningById.set(messageId, state.content.length);
  state.content.push(part);
  return part;
}

export function updateReasoning(state: AssistantTurnState, messageId: string, text: string): void {
  const part = ensureReasoning(state, messageId);
  const index = state.reasoningById.get(messageId);
  if (index === undefined) return;
  state.content[index] = { ...part, text };
}

/** Ensure the span part exists, then stamp a reasoning-span timestamp
 *  (compact-chat spec §5.2.1). `startedAt` is first-wins (the span's opening
 *  frame); `finishedAt` is last-wins (spans are keyed by messageId, so a later
 *  END simply refreshes the endpoint). A frame without a timestamp still
 *  ensures the part (the pre-spec START behavior). */
export function stampReasoning(
  state: AssistantTurnState,
  messageId: string,
  field: 'startedAt' | 'finishedAt',
  timestamp: number | undefined,
): void {
  const part = ensureReasoning(state, messageId);
  if (timestamp === undefined) return;
  if (field === 'startedAt' && part.startedAt !== undefined) return;
  const index = state.reasoningById.get(messageId);
  if (index === undefined) return;
  state.content[index] = { ...part, [field]: timestamp };
}

export function writeTool(state: AssistantTurnState, part: ToolPart): ToolPart {
  state.tools.set(part.toolCallId, part);
  const index = state.toolIndexById.get(part.toolCallId);
  if (index !== undefined) state.content[index] = part;
  return part;
}

export function ensureTool(
  state: AssistantTurnState,
  toolCallId: string,
  toolName: string,
  startedAt?: number,
): ToolPart {
  const existing = state.tools.get(toolCallId);
  if (existing) {
    if (toolName !== '' && existing.toolName === '') {
      return writeTool(state, { ...existing, toolName });
    }
    return existing;
  }
  const part: ToolPart = {
    type: 'tool-call',
    toolCallId,
    toolName,
    argsText: '',
    ...(startedAt !== undefined ? { startedAt } : {}),
  };
  state.tools.set(toolCallId, part);
  state.toolOrder.push(toolCallId);
  state.toolIndexById.set(toolCallId, state.content.length);
  state.content.push(part);
  return part;
}

/**
 * Narrow a ThreadMessageLike's content (string | parts[]) to the parts array.
 * A string content (never produced by this reducer) wraps into a single text
 * part so callers always see an array.
 */
export function messageParts(message: ThreadMessageLike): readonly ChatPart[] {
  if (typeof message.content === 'string') return [{ type: 'text', text: message.content }];
  return message.content as readonly ChatPart[];
}

/**
 * Materialise the accumulated turn into a ThreadMessageLike for the runtime.
 * Part order preserves frame arrival order: reasoning spans, tool cards, and
 * text appear exactly where the stream first introduced them. An empty turn still
 * yields a (possibly empty) text part so the message renders.
 */
export function toThreadMessage(state: AssistantTurnState): ThreadMessageLike {
  const content: ChatPart[] = [...state.content];
  if (state.text.length > 0 || content.length === 0) {
    const hasText = content.some((part) => part.type === 'text');
    if (!hasText) content.push({ type: 'text', text: state.text });
  }
  if (state.error !== undefined) {
    content.push({ type: 'text', text: state.error });
  }
  return {
    id: state.id,
    role: 'assistant',
    content,
    status: state.status,
    // The budget trip rides as metadata, not as a text part: a synthetic part would be
    // prose the runtime could branch from or re-send (the CompactionMarker/SteerNotice
    // argument), while metadata stays a fact ABOUT this turn that AssistantMessage renders.
    ...(state.limit === undefined ? {} : { metadata: { custom: { budgetLimit: state.limit } } }),
  };
}
