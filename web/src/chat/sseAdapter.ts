import type { ThreadMessageLike } from '@assistant-ui/react';
import { isDisplayPayload, type DisplayPayload } from './displays/types';
import { isMcpViewDescriptor } from './mcpapps/hostProtocol';
import { errorDetail, type AguiFrame } from './sseAdapter_frames';
import {
  ensureReasoning,
  ensureText,
  ensureTool,
  frameTimestamp,
  newAssistantTurn,
  stampReasoning,
  toThreadMessage,
  updateReasoning,
  updateText,
  writeTool,
  type AssistantTurnState,
} from './sseAdapter_parts';
import {
  isUsageDelta,
  toolCallIdFromDelta,
  usageFromStateDelta,
  type TurnUsage,
} from './sseAdapter_usage';
import { buildAuraRunBody } from './auraRunBody';

// sseAdapter maps Aura's AG-UI/SSE event stream (POST /agent/run) onto
// assistant-ui's ThreadMessageLike model. It is a PURE reducer + a thin
// fetch+ReadableStream pump — NO React, NO rendering here (the components in
// ExternalStoreChat consume the output).
//
// The contract is the backend translator output (internal/agui/translator.go):
//   RUN_STARTED                            → open a running assistant message
//   TEXT_MESSAGE_START/CONTENT/END         → a single { type: "text" } part
//   REASONING_START/MESSAGE_*/END          → a { type: "reasoning" } part (pill) + span timestamps
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
// errorDetail helper live in ./sseAdapter_frames (the leaf); the assistant-turn
// accumulator + its part builders live in ./sseAdapter_parts; persisted-history
// rehydration lives in ./sseAdapter_snapshot; the D-10 usage projection lives in
// ./sseAdapter_usage. This module keeps the frame → builder dispatch (reduceFrame),
// SSE parsing and the stream runners, then re-exports the siblings so the public
// import surface (`./sseAdapter`) stays unchanged.

/**
 * The `aura.artifact` CUSTOM descriptor (37A-02 enriched: a delivered send_file).
 * It is NOT a DisplayPayload (it has no `type` field) — the reducer synthesizes a
 * local_artifact payload FROM it. A descriptor is actionable only when it carries
 * the `tool_call_id` correlation key (the ensureTool/writeTool attach key) and a
 * `filename`; a malformed / pre-enrichment descriptor missing either is ignored
 * (no card, no corruption). `path` MAY ride the descriptor for Telegram parity
 * (D-01) but is deliberately absent from this shape — the reducer never reads it.
 */
export interface ArtifactDescriptor {
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
 * Narrow a frame to its actionable `aura.artifact` descriptor (or null). The
 * single decision point for the PUMP-level onArtifact signal (T-37B-15: never
 * emitted from the pure reduceFrame) — shared by streamSSE here and by the
 * resilient wrapper in ./sseResume so the two pumps cannot drift.
 */
export function artifactDescriptorValue(frame: AguiFrame): ArtifactDescriptor | null {
  if (frame.type !== 'CUSTOM' || frame.name !== 'aura.artifact') return null;
  return isArtifactDescriptor(frame.value) ? frame.value : null;
}

/**
 * The `aura.steer` CUSTOM payload (amendment #132, STEER-03): the SAME structural shape
 * drainSteer (internal/agent/llm_agent_steer.go) and the leftover auto-delivery
 * (internal/runner/runner_steer.go's leftoverSteerNoticeEvent) both build — read from the
 * Go source, not inferred here. `steers[].delivery` distinguishes a mid-run drain
 * ("tool_result_append" | "user_message_fallback") from the 52-05 leftover form
 * ("auto_delivery_next_turn"), which the client renders with different copy.
 */
export interface SteerNoticeEntry {
  readonly id: string;
  readonly source: string;
  readonly text: string;
  readonly delivery: string;
}
export interface SteerNotice {
  readonly conversation_id: string;
  readonly round: number;
  readonly steers: readonly SteerNoticeEntry[];
}

function isSteerNoticeEntry(value: unknown): value is SteerNoticeEntry {
  if (typeof value !== 'object' || value === null) return false;
  const c = value as { id?: unknown; source?: unknown; text?: unknown; delivery?: unknown };
  return (
    typeof c.id === 'string' &&
    typeof c.source === 'string' &&
    typeof c.text === 'string' &&
    typeof c.delivery === 'string'
  );
}

function isSteerNotice(value: unknown): value is SteerNotice {
  if (typeof value !== 'object' || value === null) return false;
  const c = value as { conversation_id?: unknown; round?: unknown; steers?: unknown };
  return (
    typeof c.conversation_id === 'string' &&
    typeof c.round === 'number' &&
    Array.isArray(c.steers) &&
    c.steers.every(isSteerNoticeEntry)
  );
}

/**
 * Narrow a frame to its `aura.steer` notice (or null) — the steer twin of
 * artifactDescriptorValue above. This is a PUMP-level signal only: reduceFrame below
 * deliberately produces NO message part for an `aura.steer` frame, because the steer is
 * already persisted server-side as an ordinary user turn (52-04) — a synthetic part here
 * would be a second representation of the same fact the runtime could branch from.
 */
export function steerNoticeValue(frame: AguiFrame): SteerNotice | null {
  if (frame.type !== 'CUSTOM' || frame.name !== 'aura.steer') return null;
  return isSteerNotice(frame.value) ? frame.value : null;
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
      stampReasoning(state, frame.messageId, 'startedAt', frameTimestamp(frame));
      return state;
    case 'REASONING_MESSAGE_CONTENT': {
      const part = ensureReasoning(state, frame.messageId);
      updateReasoning(state, frame.messageId, part.text + frame.delta);
      state.reasoning += frame.delta;
      return state;
    }
    case 'REASONING_MESSAGE_END':
      stampReasoning(state, frame.messageId, 'finishedAt', frameTimestamp(frame));
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
      // MCP Apps (SEP-1865): a mounted server bound this tool to a `ui://`
      // document. The frame is what renders it; the reducer only correlates the
      // descriptor to its tool part, exactly like the two branches above. The HTML
      // is NOT here and never will be — it is static per server and is fetched
      // once from GET /api/mcp/view, so the stream carries the payload only.
      if (frame.name === 'aura.mcp_view' && isMcpViewDescriptor(frame.value)) {
        const d = frame.value;
        const part = ensureTool(
          state,
          d.tool_call_id,
          state.tools.get(d.tool_call_id)?.toolName ?? '',
        );
        writeTool(state, {
          ...part,
          display: { type: 'mcp_view', tool_call_id: d.tool_call_id, mcp_view: d },
        });
      }
      // aura.steer (amendment #132, STEER-03) is deliberately NOT handled here: it falls
      // through to the default no-op below every branch above, exactly as an unrecognized
      // frame does. steerNoticeValue (above) is the ONLY decision point for it, fired from
      // the PUMP (streamSSE below, and sseResume.ts's pumpBody) — never from this reducer.
      return state;
    }
    case 'REASONING_START':
      stampReasoning(state, frame.messageId, 'startedAt', frameTimestamp(frame));
      return state;
    case 'REASONING_END':
      stampReasoning(state, frame.messageId, 'finishedAt', frameTimestamp(frame));
      return state;
    case 'RUN_STARTED':
      return state;
  }
}

// ---------------------------------------------------------------------------
// SSE frame parsing — split an SSE byte stream into AG-UI frames.
// ---------------------------------------------------------------------------

/**
 * One parsed SSE block: the AG-UI frame plus its `id:` when the resume-capable
 * gateway stamped an INTEGER sequence (fix-plan 1.3B — 1-based, strictly
 * monotonic per run). A non-integer id (the SDK's `<TYPE>_<timestampMs>` shape,
 * flag off) yields `id` undefined — "no resume capability" — so callers degrade
 * to the single-shot contract.
 */
export interface SSEFrameEvent {
  readonly frame: AguiFrame;
  readonly id?: number;
}

/**
 * Parse one SSE event block (lines between blank-line delimiters) into a frame.
 * The AG-UI SSE writer emits `event: <TYPE>` [+ `id: <seq>`] + `data: <json>`;
 * we trust the JSON body's own `type` field (it is authoritative and matches
 * the event line). Returns null for keep-alives / comment lines / unparseable
 * blocks — heartbeat comments (`:hb`) never advance the resume cursor.
 */
export function parseSSEBlock(block: string): SSEFrameEvent | null {
  const dataLines: string[] = [];
  let id: number | undefined;
  for (const raw of block.split('\n')) {
    const line = raw.endsWith('\r') ? raw.slice(0, -1) : raw;
    if (line.startsWith(':')) continue; // comment / keep-alive
    if (line.startsWith('id:')) {
      const value = line.slice(3).replace(/^ /, '');
      if (/^\d+$/.test(value)) id = Number(value);
      continue;
    }
    if (line.startsWith('data:')) dataLines.push(line.slice(5).replace(/^ /, ''));
  }
  if (dataLines.length === 0) return null;
  try {
    const parsed = JSON.parse(dataLines.join('\n')) as { type?: string };
    if (typeof parsed.type !== 'string') return null;
    return { frame: parsed as AguiFrame, ...(id !== undefined ? { id } : {}) };
  } catch {
    return null;
  }
}

/** Async-iterate AG-UI frames (with their resume ids) from an SSE body. */
export async function* readSSEFrames(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<SSEFrameEvent, void, unknown> {
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
        const event = parseSSEBlock(block);
        if (event) yield event;
        sep = buffer.indexOf('\n\n');
      }
    }
    // Flush a trailing block with no final blank-line delimiter.
    const tail = (buffer + decoder.decode()).trim();
    if (tail.length > 0) {
      const event = parseSSEBlock(tail);
      if (event) yield event;
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
  /** 37D pinned skill name (a SkillPicker selection); folded into the same aura envelope. */
  readonly skill?: string;
  readonly effort?: string; // 37E reasoning-effort symbol; folded into aura for fixed levels, omitted for 'auto'.
  /** Called after each frame folds into the turn (drives setMessages). */
  readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
  /**
   * 37B seam (mirrors onUsage): fires from the streamSSE pump when an
   * `aura.artifact` descriptor frame is seen, carrying its `asset_id` (undefined
   * when the delivery degraded). Drives the Artefatti panel's invalidate + auto-open.
   */
  readonly onArtifact?: (assetId: string | undefined) => void;
  /** Fires once per `aura.steer` frame (amendment #132, STEER-03) — the mid-turn redirect
   *  echo, from the PUMP, never from reduceFrame. Drives the cockpit's SteerNotice. */
  readonly onSteer?: (notice: SteerNotice) => void;
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
  /** 37B seam (mirrors onUsage): fires on an `aura.artifact` descriptor frame. */
  readonly onArtifact?: (assetId: string | undefined) => void;
  /** Mirrors StreamRunOptions.onSteer — the mid-turn redirect echo. */
  readonly onSteer?: (notice: SteerNotice) => void;
  readonly newId?: () => string;
}

export const SSE_REQUEST_HEADERS: Record<string, string> = {
  'Content-Type': 'application/json',
  Accept: 'text/event-stream',
};

interface StreamSSEOptions {
  readonly request: (assistantId: string) => readonly [string, RequestInit];
  readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
  readonly onArtifact?: ((assetId: string | undefined) => void) | undefined;
  readonly onSteer?: ((notice: SteerNotice) => void) | undefined;
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

  for await (const { frame } of readSSEFrames(res.body)) {
    reduceFrame(state, frame);
    // onArtifact is a PUMP-level signal, NEVER emitted from the pure reduceFrame
    // (Pitfall/T-37B-15): fire it here when the descriptor frame lands, passing the
    // asset_id (undefined on a degraded delivery — the panel still auto-opens).
    const artifact = artifactDescriptorValue(frame);
    if (artifact !== null) opts.onArtifact?.(artifact.asset_id);
    const steer = steerNoticeValue(frame);
    if (steer !== null) opts.onSteer?.(steer);
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
    onArtifact: opts.onArtifact,
    onSteer: opts.onSteer,
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
    onArtifact: opts.onArtifact,
    onSteer: opts.onSteer,
    request: (id) => [
      '/agent/run',
      {
        method: 'POST',
        headers: SSE_REQUEST_HEADERS,
        credentials: 'same-origin',
        // The backend decodes an AG-UI RunAgentInput and requires an existing thread; the aura
        // envelope (attachment_ids + the 37D pinned skill) is assembled by buildAuraRunBody.
        body: JSON.stringify(buildAuraRunBody(id, opts)),
        signal: opts.signal,
      },
    ],
  });
}

// ---------------------------------------------------------------------------
// Barrel — keep the public `./sseAdapter` import surface unchanged. Everything
// moved into the leaves (wire/part types, usage projection) and the snapshot
// module is re-exported here so consumers and tests import from this path
// exactly as before.
// ---------------------------------------------------------------------------

export type { AguiFrame, ChatPart, JSONPatchOp } from './sseAdapter_frames';
export {
  messageParts,
  newAssistantTurn,
  toThreadMessage,
  type AssistantTurnState,
} from './sseAdapter_parts';
export { fetchThreadMessages, snapshotToThreadMessages } from './sseAdapter_snapshot';
export {
  cacheHitRatio,
  isUsageDelta,
  toolCallIdFromDelta,
  usageFromStateDelta,
  type TurnUsage,
} from './sseAdapter_usage';
