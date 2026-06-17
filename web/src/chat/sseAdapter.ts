import type { ThreadMessageLike } from '@assistant-ui/react';

// sseAdapter maps Aura's AG-UI/SSE event stream (POST /agent/run) onto
// assistant-ui's ThreadMessageLike model. It is a PURE reducer + a thin
// fetch+ReadableStream pump — NO React, NO rendering here (the components in
// ExternalStoreChat consume the output).
//
// The contract is the backend translator output (internal/agui/translator.go):
//   RUN_STARTED                            → open a running assistant message
//   TEXT_MESSAGE_START/CONTENT/END         → a single { type: "text" } part
//   REASONING_START/MESSAGE_*/END          → a { type: "reasoning" } part (drawer)
//   TOOL_CALL_START(name)/ARGS/END/RESULT  → a { type: "tool-call" } part (raw result)
//   STATE_DELTA { tool_call_id }           → a tool-result marker → tool part (Pitfall 2)
//   STATE_DELTA { usage keys }             → footer usage (NOT a message part)
//   RUN_FINISHED(success)                  → status: complete
//   RUN_FINISHED(interrupt)                → status: requires-action (inline approval, 25-02)
//   RUN_ERROR (sanitized)                  → status: incomplete + error part
//
// Trust the event TYPE, not the content: a STATE_DELTA carrying tool_call_id is a
// tool-result marker, NEVER assistant prose (translator.go toolResultCallID). The
// frame fixture (internal/agui/testdata/golden-events.json) drives the test.

// ---------------------------------------------------------------------------
// Wire types — the JSON shapes the AG-UI SSE writer emits (1:1 with the SDK
// events the Go translator yields). Only the fields the reducer reads are typed.
// ---------------------------------------------------------------------------

/** One RFC-6902 JSONPatch op as carried in a STATE_DELTA `delta` array. */
export interface JSONPatchOp {
  readonly op: string;
  readonly path: string;
  readonly value: unknown;
}

interface RunStartedFrame {
  readonly type: 'RUN_STARTED';
  readonly threadId?: string;
  readonly runId?: string;
}
interface TextMessageStartFrame {
  readonly type: 'TEXT_MESSAGE_START';
  readonly messageId: string;
}
interface TextMessageContentFrame {
  readonly type: 'TEXT_MESSAGE_CONTENT';
  readonly messageId: string;
  readonly delta: string;
}
interface TextMessageEndFrame {
  readonly type: 'TEXT_MESSAGE_END';
  readonly messageId: string;
}
interface ReasoningStartFrame {
  readonly type: 'REASONING_START';
  readonly messageId: string;
}
interface ReasoningMessageContentFrame {
  readonly type: 'REASONING_MESSAGE_CONTENT';
  readonly messageId: string;
  readonly delta: string;
}
interface ReasoningEndFrame {
  readonly type: 'REASONING_END';
  readonly messageId: string;
}
interface ToolCallStartFrame {
  readonly type: 'TOOL_CALL_START';
  readonly toolCallId: string;
  readonly toolCallName: string;
}
interface ToolCallArgsFrame {
  readonly type: 'TOOL_CALL_ARGS';
  readonly toolCallId: string;
  readonly delta: string;
}
interface ToolCallEndFrame {
  readonly type: 'TOOL_CALL_END';
  readonly toolCallId: string;
}
interface ToolCallResultFrame {
  readonly type: 'TOOL_CALL_RESULT';
  readonly toolCallId: string;
  readonly content: string;
}
interface StateDeltaFrame {
  readonly type: 'STATE_DELTA';
  readonly delta: readonly JSONPatchOp[];
}
interface RunFinishedFrame {
  readonly type: 'RUN_FINISHED';
  readonly outcome?: { readonly type: string; readonly interrupts?: readonly unknown[] };
}
interface RunErrorFrame {
  readonly type: 'RUN_ERROR';
  readonly message: string;
}

/**
 * The reducer-relevant subset of AG-UI frames. Frames the chat lane ignores
 * (STEP_*, STATE_SNAPSHOT, MESSAGES_SNAPSHOT, CUSTOM/aura.artifact,
 * REASONING_MESSAGE_START/END) are not modelled — `reduceFrame` is a no-op for
 * any type it does not recognise, so unknown frames never corrupt the message.
 */
export type AguiFrame =
  | RunStartedFrame
  | TextMessageStartFrame
  | TextMessageContentFrame
  | TextMessageEndFrame
  | ReasoningStartFrame
  | ReasoningMessageContentFrame
  | ReasoningEndFrame
  | ToolCallStartFrame
  | ToolCallArgsFrame
  | ToolCallEndFrame
  | ToolCallResultFrame
  | StateDeltaFrame
  | RunFinishedFrame
  | RunErrorFrame;

// ---------------------------------------------------------------------------
// Usage — read off the final STATE_DELTA (cost/cache footer, D-10).
// ---------------------------------------------------------------------------

export interface TurnUsage {
  readonly promptTokens: number;
  readonly completionTokens: number;
  readonly cacheHitTokens: number;
  readonly costUsd?: number;
}

/** True when this STATE_DELTA carries usage keys (vs the tool-result marker). */
export function isUsageDelta(ops: readonly JSONPatchOp[]): boolean {
  return ops.some(
    (o) =>
      o.path === '/prompt_tokens' ||
      o.path === '/completion_tokens' ||
      o.path === '/cache_hit_tokens' ||
      o.path === '/cost_usd',
  );
}

/** The tool_call_id a STATE_DELTA marks (Pitfall 2 — tool-result, never prose). */
export function toolCallIdFromDelta(ops: readonly JSONPatchOp[]): string | undefined {
  const marker = ops.find((o) => o.path === '/tool_call_id');
  if (marker === undefined) return undefined;
  return typeof marker.value === 'string' ? marker.value : undefined;
}

/**
 * Project the usage JSONPatch ops onto a TurnUsage. Missing cost_usd → costUsd
 * undefined (D-10: provider may omit cost). Numeric coercion is defensive.
 */
export function usageFromStateDelta(ops: readonly JSONPatchOp[]): TurnUsage {
  const byPath = new Map(ops.map((o) => [o.path, o.value]));
  const cost = byPath.get('/cost_usd');
  return {
    promptTokens: Number(byPath.get('/prompt_tokens') ?? 0),
    completionTokens: Number(byPath.get('/completion_tokens') ?? 0),
    cacheHitTokens: Number(byPath.get('/cache_hit_tokens') ?? 0),
    ...(cost !== undefined ? { costUsd: Number(cost) } : {}),
  };
}

/**
 * Cache-hit ratio (0..1) for the footer. Guards divide-by-zero: promptTokens=0
 * returns 0, never NaN (matching cachemetrics.Aggregate "ratio left to caller").
 */
export function cacheHitRatio(usage: TurnUsage): number {
  if (usage.promptTokens <= 0) return 0;
  return usage.cacheHitTokens / usage.promptTokens;
}

// ---------------------------------------------------------------------------
// The reducer — folds frames onto a single assistant ThreadMessageLike.
// ---------------------------------------------------------------------------

interface ToolPart {
  readonly type: 'tool-call';
  readonly toolCallId: string;
  readonly toolName: string;
  readonly argsText: string;
  readonly result?: string;
}
interface TextPart {
  readonly type: 'text';
  readonly text: string;
}
interface ReasoningPart {
  readonly type: 'reasoning';
  readonly text: string;
}

/** The assistant message-part union the reducer emits. */
export type ChatPart = TextPart | ReasoningPart | ToolPart;

type AssistantStatus = ThreadMessageLike['status'];

/**
 * The accumulator for one assistant turn. Parts preserve arrival order; a
 * tool-call part is keyed by toolCallId so ARGS/END/RESULT (and a tool_call_id
 * STATE_DELTA marker) merge onto the part the START created.
 */
export interface AssistantTurnState {
  readonly id: string;
  text: string;
  textOpen: boolean;
  reasoning: string;
  /** ordered tool parts, by first-seen toolCallId. */
  readonly toolOrder: string[];
  readonly tools: Map<string, ToolPart>;
  usage?: TurnUsage;
  error?: string;
  status: AssistantStatus;
}

export function newAssistantTurn(id: string): AssistantTurnState {
  return {
    id,
    text: '',
    textOpen: false,
    reasoning: '',
    toolOrder: [],
    tools: new Map(),
    status: { type: 'running' },
  };
}

function ensureTool(state: AssistantTurnState, toolCallId: string, toolName: string): ToolPart {
  const existing = state.tools.get(toolCallId);
  if (existing) return existing;
  const part: ToolPart = { type: 'tool-call', toolCallId, toolName, argsText: '' };
  state.tools.set(toolCallId, part);
  state.toolOrder.push(toolCallId);
  return part;
}

/**
 * Apply one frame to the turn state, mutating in place. Returns the same state
 * for chaining/readability. Unknown / ignored frame types are no-ops.
 *
 * CRITICAL (Pitfall 2): a STATE_DELTA carrying /tool_call_id is routed to the
 * tool part — it is NEVER appended to the assistant text. A TOOL_CALL_RESULT is
 * likewise a tool part, never prose.
 */
export function reduceFrame(state: AssistantTurnState, frame: AguiFrame): AssistantTurnState {
  switch (frame.type) {
    case 'TEXT_MESSAGE_START':
      state.textOpen = true;
      return state;
    case 'TEXT_MESSAGE_CONTENT':
      state.text += frame.delta;
      state.textOpen = true;
      return state;
    case 'TEXT_MESSAGE_END':
      state.textOpen = false;
      return state;
    case 'REASONING_MESSAGE_CONTENT':
      state.reasoning += frame.delta;
      return state;
    case 'TOOL_CALL_START':
      ensureTool(state, frame.toolCallId, frame.toolCallName);
      return state;
    case 'TOOL_CALL_ARGS': {
      const part = state.tools.get(frame.toolCallId);
      if (part) state.tools.set(frame.toolCallId, { ...part, argsText: part.argsText + frame.delta });
      return state;
    }
    case 'TOOL_CALL_RESULT': {
      // A tool result may arrive for a call we never saw START for (snapshot
      // rehydration); create the part on demand so the raw blob is never lost.
      const part = ensureTool(state, frame.toolCallId, state.tools.get(frame.toolCallId)?.toolName ?? '');
      state.tools.set(frame.toolCallId, { ...part, result: frame.content });
      return state;
    }
    case 'STATE_DELTA': {
      const toolId = toolCallIdFromDelta(frame.delta);
      if (toolId !== undefined) {
        // Pitfall 2: a tool_call_id marker is a tool-result correlation, NEVER
        // an assistant text delta. Attach it to the tool part (no prose write).
        ensureTool(state, toolId, state.tools.get(toolId)?.toolName ?? '');
        return state;
      }
      if (isUsageDelta(frame.delta)) {
        state.usage = usageFromStateDelta(frame.delta);
      }
      return state;
    }
    case 'RUN_FINISHED':
      state.textOpen = false;
      state.status =
        frame.outcome?.type === 'interrupt'
          ? { type: 'requires-action', reason: 'interrupt' }
          : { type: 'complete', reason: 'stop' };
      return state;
    case 'RUN_ERROR':
      state.textOpen = false;
      state.error = frame.message;
      state.status = { type: 'incomplete', reason: 'error' };
      return state;
    case 'RUN_STARTED':
    case 'REASONING_START':
    case 'REASONING_END':
    case 'TOOL_CALL_END':
      return state;
  }
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
 * Part order: reasoning (drawer-bound) → text → tool cards → error. An empty
 * turn still yields a (possibly empty) text part so the message renders.
 */
export function toThreadMessage(state: AssistantTurnState): ThreadMessageLike {
  const content: ChatPart[] = [];
  if (state.reasoning.length > 0) content.push({ type: 'reasoning', text: state.reasoning });
  content.push({ type: 'text', text: state.text });
  for (const id of state.toolOrder) {
    const part = state.tools.get(id);
    if (part) content.push(part);
  }
  if (state.error !== undefined) {
    content.push({ type: 'text', text: state.error });
  }
  return {
    id: state.id,
    role: 'assistant',
    content,
    status: state.status,
  };
}

// ---------------------------------------------------------------------------
// SSE frame parsing — split an SSE byte stream into AG-UI frames.
// ---------------------------------------------------------------------------

/**
 * Parse one SSE event block (lines between blank-line delimiters) into a frame.
 * The AG-UI SSE writer emits `event: <TYPE>` + `data: <json>`; we trust the JSON
 * body's own `type` field (it is authoritative and matches the event line).
 * Returns null for keep-alives / comment lines / unparseable blocks.
 */
export function parseSSEBlock(block: string): AguiFrame | null {
  const dataLines: string[] = [];
  for (const raw of block.split('\n')) {
    const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw;
    if (line.startsWith(':')) continue; // comment / keep-alive
    if (line.startsWith('data:')) dataLines.push(line.slice(5).replace(/^ /, ''));
  }
  if (dataLines.length === 0) return null;
  try {
    const parsed = JSON.parse(dataLines.join('\n')) as { type?: string };
    if (typeof parsed.type !== 'string') return null;
    return parsed as AguiFrame;
  } catch {
    return null;
  }
}

/** Async-iterate AG-UI frames from a fetch Response's SSE body. */
export async function* readSSEFrames(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<AguiFrame, void, unknown> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let sep = buffer.indexOf('\n\n');
      while (sep !== -1) {
        const block = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        const frame = parseSSEBlock(block);
        if (frame) yield frame;
        sep = buffer.indexOf('\n\n');
      }
    }
    // Flush a trailing block with no final blank-line delimiter.
    const tail = (buffer + decoder.decode()).trim();
    if (tail.length > 0) {
      const frame = parseSSEBlock(tail);
      if (frame) yield frame;
    }
  } finally {
    reader.releaseLock();
  }
}

export interface StreamRunOptions {
  readonly threadId: string;
  readonly userText: string;
  readonly signal: AbortSignal;
  /** Called after each frame folds into the turn (drives setMessages). */
  readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
  /** Mints the assistant message id; defaults to crypto.randomUUID. */
  readonly newId?: () => string;
}

/**
 * POST /agent/run and stream the reply, folding each AG-UI frame onto one
 * assistant message and invoking onUpdate. Uses fetch + ReadableStream (NOT
 * EventSource — it cannot POST a body); the caller's AbortSignal is the Stop
 * affordance (the server's streamSSE unwinds cleanly on ctx.Done).
 */
export async function streamRun(opts: StreamRunOptions): Promise<TurnUsage | undefined> {
  const id = (opts.newId ?? (() => crypto.randomUUID()))();
  const state = newAssistantTurn(id);
  opts.onUpdate(toThreadMessage(state), state.usage);

  const res = await fetch('/agent/run', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
    credentials: 'same-origin',
    body: JSON.stringify({ threadId: opts.threadId, message: opts.userText }),
    signal: opts.signal,
  });

  if (!res.ok || res.body === null) {
    state.error = `HTTP ${String(res.status)}`;
    state.status = { type: 'incomplete', reason: 'error' };
    opts.onUpdate(toThreadMessage(state), state.usage);
    return state.usage;
  }

  for await (const frame of readSSEFrames(res.body)) {
    reduceFrame(state, frame);
    opts.onUpdate(toThreadMessage(state), state.usage);
  }
  return state.usage;
}
