import type { ThreadMessageLike } from '@assistant-ui/react';
import { isDisplayPayload, type DisplayPayload } from './displays/types';
import {
  errorDetail,
  type AguiFrame,
  type ChatPart,
  type JSONPatchOp,
  type ReasoningPart,
  type TextPart,
  type ToolPart,
} from './sseAdapter_frames';

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
//
// The wire-frame types, the part-union types, the snapshot shapes and the shared
// errorDetail helper live in ./sseAdapter_frames (the leaf); persisted-history
// rehydration lives in ./sseAdapter_snapshot. This module keeps the live reducer,
// SSE parsing and the stream runners, then re-exports both siblings so the public
// import surface (`./sseAdapter`) stays unchanged.

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

function frameTimestamp(frame: AguiFrame): number | undefined {
  const t = (frame as { readonly timestamp?: unknown }).timestamp;
  return typeof t === 'number' ? t : undefined;
}

function ensureText(state: AssistantTurnState, messageId: string): TextPart {
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

function updateText(state: AssistantTurnState, messageId: string, text: string): void {
  const part = ensureText(state, messageId);
  const index = state.textById.get(messageId);
  if (index === undefined) return;
  state.content[index] = { ...part, text };
}

function ensureReasoning(state: AssistantTurnState, messageId: string): ReasoningPart {
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

function updateReasoning(state: AssistantTurnState, messageId: string, text: string): void {
  const part = ensureReasoning(state, messageId);
  const index = state.reasoningById.get(messageId);
  if (index === undefined) return;
  state.content[index] = { ...part, text };
}

function writeTool(state: AssistantTurnState, part: ToolPart): ToolPart {
  state.tools.set(part.toolCallId, part);
  const index = state.toolIndexById.get(part.toolCallId);
  if (index !== undefined) state.content[index] = part;
  return part;
}

function ensureTool(
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
 * The `aura.artifact` CUSTOM descriptor (37A-02 enriched: a delivered send_file).
 * It is NOT a DisplayPayload (it has no `type` field) — the reducer synthesizes a
 * local_artifact payload FROM it. A descriptor is actionable only when it carries
 * the `tool_call_id` correlation key (the ensureTool/writeTool attach key) and a
 * `filename`; a malformed / pre-enrichment descriptor missing either is ignored
 * (no card, no corruption). `path` MAY ride the descriptor for Telegram parity
 * (D-01) but is deliberately absent from this shape — the reducer never reads it.
 */
interface ArtifactDescriptor {
  readonly tool_call_id: string;
  readonly filename: string;
  readonly size_bytes?: number;
  readonly asset_id?: string;
  readonly mime_type?: string;
}

function isArtifactDescriptor(value: unknown): value is ArtifactDescriptor {
  if (typeof value !== 'object' || value === null) return false;
  const candidate = value as { tool_call_id?: unknown; filename?: unknown };
  return typeof candidate.tool_call_id === 'string' && typeof candidate.filename === 'string';
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
      ensureText(state, frame.messageId);
      state.textOpen = true;
      return state;
    case 'TEXT_MESSAGE_CONTENT': {
      const part = ensureText(state, frame.messageId);
      updateText(state, frame.messageId, part.text + frame.delta);
      state.text += frame.delta;
      state.textOpen = true;
      return state;
    }
    case 'TEXT_MESSAGE_END':
      state.textOpen = false;
      return state;
    case 'REASONING_MESSAGE_START':
      ensureReasoning(state, frame.messageId);
      return state;
    case 'REASONING_MESSAGE_CONTENT': {
      const part = ensureReasoning(state, frame.messageId);
      updateReasoning(state, frame.messageId, part.text + frame.delta);
      state.reasoning += frame.delta;
      return state;
    }
    case 'REASONING_MESSAGE_END':
      return state;
    case 'TOOL_CALL_START':
      ensureTool(state, frame.toolCallId, frame.toolCallName, frameTimestamp(frame));
      return state;
    case 'TOOL_CALL_ARGS': {
      const part = state.tools.get(frame.toolCallId);
      if (part) writeTool(state, { ...part, argsText: part.argsText + frame.delta });
      return state;
    }
    case 'TOOL_CALL_END': {
      const part = state.tools.get(frame.toolCallId);
      const finishedAt = frameTimestamp(frame);
      if (part && part.finishedAt === undefined && finishedAt !== undefined)
        writeTool(state, { ...part, finishedAt });
      return state;
    }
    case 'TOOL_CALL_RESULT': {
      // A tool result may arrive for a call we never saw START for (snapshot
      // rehydration); create the part on demand so the raw blob is never lost.
      const part = ensureTool(
        state,
        frame.toolCallId,
        state.tools.get(frame.toolCallId)?.toolName ?? '',
      );
      const finishedAt = frameTimestamp(frame);
      writeTool(state, {
        ...part,
        result: frame.content,
        ...(finishedAt !== undefined ? { finishedAt } : {}),
      });
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
    case 'CUSTOM': {
      // Phase 26: aura.display carries a typed DisplayPayload to attach to the
      // tool part by tool_call_id. Like TOOL_CALL_RESULT, ensureTool tolerates a
      // payload arriving before/without the call's START.
      if (frame.name === 'aura.display' && isDisplayPayload(frame.value)) {
        const id = frame.value.tool_call_id;
        const part = ensureTool(state, id, state.tools.get(id)?.toolName ?? '');
        writeTool(state, { ...part, display: frame.value });
      }
      // 37A (WEBART-04): aura.display never fires for send_file (normalizeCode
      // only synthesizes a local_artifact for code-producing tools), so consume
      // the aura.artifact descriptor here — synthesize a local_artifact payload
      // correlated by tool_call_id. The raw host/container `path` is NEVER copied
      // into the payload, in EITHER branch (asset_id present → download button;
      // asset_id absent → degraded card): the browser must never receive a raw
      // path for any authenticated session (D-13). `path` stays a backend/
      // Telegram-only field (D-01).
      if (frame.name === 'aura.artifact' && isArtifactDescriptor(frame.value)) {
        const d = frame.value;
        const part = ensureTool(
          state,
          d.tool_call_id,
          state.tools.get(d.tool_call_id)?.toolName ?? '',
        );
        const display: DisplayPayload = {
          type: 'local_artifact',
          tool_call_id: d.tool_call_id,
          artifact: {
            filename: d.filename,
            ...(d.size_bytes !== undefined ? { size_bytes: d.size_bytes } : {}),
            ...(d.asset_id !== undefined ? { asset_id: d.asset_id } : {}),
            ...(d.mime_type !== undefined ? { mime_type: d.mime_type } : {}),
          },
        };
        writeTool(state, { ...part, display });
      }
      return state;
    }
    case 'RUN_STARTED':
    case 'REASONING_START':
    case 'REASONING_END':
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
  readonly attachmentIds?: readonly string[];
  /** Called after each frame folds into the turn (drives setMessages). */
  readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
  /** Mints the assistant message id; defaults to crypto.randomUUID. */
  readonly newId?: () => string;
}

export interface StreamPostOptions {
  /** The endpoint to POST (e.g. /api/conversations/{id}/edit or .../select). */
  readonly url: string;
  /** The JSON request body (undefined → empty body, e.g. a branch-select re-run). */
  readonly body?: unknown;
  readonly signal: AbortSignal;
  readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
  readonly newId?: () => string;
}

const SSE_REQUEST_HEADERS: HeadersInit = {
  'Content-Type': 'application/json',
  Accept: 'text/event-stream',
};

interface StreamSSEOptions {
  readonly request: (assistantId: string) => readonly [string, RequestInit];
  readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
  readonly newId?: (() => string) | undefined;
}

async function streamSSE(opts: StreamSSEOptions): Promise<TurnUsage | undefined> {
  const id = (opts.newId ?? (() => crypto.randomUUID()))();
  const state = newAssistantTurn(id);
  opts.onUpdate(toThreadMessage(state), state.usage);

  const [url, init] = opts.request(id);
  const res = await fetch(url, init);

  if (!res.ok || res.body === null) {
    state.error = await errorDetail(res);
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

/**
 * POST an arbitrary SSE re-run endpoint (the D-09 branch edit / select / regenerate
 * routes) and fold the AG-UI reply onto one assistant message, exactly like streamRun.
 * The branch routes stream the same translated turn shape as /agent/run, so the same
 * reducer drives both — the only difference is the URL + body. Returns the turn usage.
 */
export async function streamPost(opts: StreamPostOptions): Promise<TurnUsage | undefined> {
  return streamSSE({
    newId: opts.newId,
    onUpdate: opts.onUpdate,
    request: () => [
      opts.url,
      {
        method: 'POST',
        headers: SSE_REQUEST_HEADERS,
        credentials: 'same-origin',
        ...(opts.body === undefined ? {} : { body: JSON.stringify(opts.body) }),
        signal: opts.signal,
      },
    ],
  });
}

/**
 * POST /agent/run and stream the reply, folding each AG-UI frame onto one
 * assistant message and invoking onUpdate. Uses fetch + ReadableStream (NOT
 * EventSource — it cannot POST a body); the caller's AbortSignal is the Stop
 * affordance (the server's streamSSE unwinds cleanly on ctx.Done).
 */
export async function streamRun(opts: StreamRunOptions): Promise<TurnUsage | undefined> {
  return streamSSE({
    newId: opts.newId,
    onUpdate: opts.onUpdate,
    request: (id) => [
      '/agent/run',
      {
        method: 'POST',
        headers: SSE_REQUEST_HEADERS,
        credentials: 'same-origin',
        // The backend decodes an AG-UI RunAgentInput and requires an existing thread.
        body: JSON.stringify({
          threadId: opts.threadId,
          messages: [{ id: id, role: 'user', content: opts.userText }],
          ...(opts.attachmentIds !== undefined && opts.attachmentIds.length > 0
            ? { aura: { attachment_ids: opts.attachmentIds } }
            : {}),
        }),
        signal: opts.signal,
      },
    ],
  });
}

// ---------------------------------------------------------------------------
// Barrel — keep the public `./sseAdapter` import surface unchanged. Everything
// moved into the leaf (wire/part types) and the snapshot module is re-exported
// here so consumers and tests import from this path exactly as before.
// ---------------------------------------------------------------------------

export type { AguiFrame, ChatPart, JSONPatchOp } from './sseAdapter_frames';
export { fetchThreadMessages, snapshotToThreadMessages } from './sseAdapter_snapshot';
