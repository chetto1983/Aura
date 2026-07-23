import type { DisplayPayload } from './displays/types';

// sseAdapter_frames — the LEAF module of the SSE adapter split: the wire-frame
// types the AG-UI SSE writer emits, the reducer part-union types, the snapshot
// wire shapes, and the shared `errorDetail` helper. It imports nothing from the
// sibling adapter modules so it can never form a cycle; both sseAdapter.ts (core
// reducer + stream runners) and sseAdapter_snapshot.ts depend on it.

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
  readonly timestamp?: number;
  readonly messageId: string;
}
interface TextMessageContentFrame {
  readonly type: 'TEXT_MESSAGE_CONTENT';
  readonly timestamp?: number;
  readonly messageId: string;
  readonly delta: string;
}
interface TextMessageEndFrame {
  readonly type: 'TEXT_MESSAGE_END';
  readonly timestamp?: number;
  readonly messageId: string;
}
interface ReasoningStartFrame {
  readonly type: 'REASONING_START';
  readonly timestamp?: number;
  readonly messageId: string;
}
interface ReasoningMessageStartFrame {
  readonly type: 'REASONING_MESSAGE_START';
  readonly timestamp?: number;
  readonly messageId: string;
}
interface ReasoningMessageContentFrame {
  readonly type: 'REASONING_MESSAGE_CONTENT';
  readonly timestamp?: number;
  readonly messageId: string;
  readonly delta: string;
}
interface ReasoningMessageEndFrame {
  readonly type: 'REASONING_MESSAGE_END';
  readonly timestamp?: number;
  readonly messageId: string;
}
interface ReasoningEndFrame {
  readonly type: 'REASONING_END';
  readonly timestamp?: number;
  readonly messageId: string;
}
interface ToolCallStartFrame {
  readonly type: 'TOOL_CALL_START';
  readonly timestamp?: number;
  readonly toolCallId: string;
  readonly toolCallName: string;
}
interface ToolCallArgsFrame {
  readonly type: 'TOOL_CALL_ARGS';
  readonly timestamp?: number;
  readonly toolCallId: string;
  readonly delta: string;
}
interface ToolCallEndFrame {
  readonly type: 'TOOL_CALL_END';
  readonly timestamp?: number;
  readonly toolCallId: string;
}
interface ToolCallResultFrame {
  readonly type: 'TOOL_CALL_RESULT';
  readonly timestamp?: number;
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
interface CustomFrame {
  readonly type: 'CUSTOM';
  readonly name: string;
  readonly value: unknown;
}

/**
 * The reducer-relevant subset of AG-UI frames. The CUSTOM frame carries a `name`:
 * `aura.display` (Phase 26) attaches a typed DisplayPayload to a tool part by
 * toolCallId, and `aura.artifact` (37A) synthesizes a local_artifact display from
 * the send_file descriptor (correlated by its tool_call_id). The frames the chat
 * lane ignores (STEP_*, STATE_SNAPSHOT, MESSAGES_SNAPSHOT) and any other CUSTOM
 * name are not modelled — `reduceFrame` returns the state unchanged for any
 * type/name it does not recognise, so unknown frames never corrupt the message.
 */
export type AguiFrame =
  | RunStartedFrame
  | TextMessageStartFrame
  | TextMessageContentFrame
  | TextMessageEndFrame
  | ReasoningStartFrame
  | ReasoningMessageStartFrame
  | ReasoningMessageContentFrame
  | ReasoningMessageEndFrame
  | ReasoningEndFrame
  | ToolCallStartFrame
  | ToolCallArgsFrame
  | ToolCallEndFrame
  | ToolCallResultFrame
  | StateDeltaFrame
  | RunFinishedFrame
  | RunErrorFrame
  | CustomFrame;

// ---------------------------------------------------------------------------
// Reducer part-union — the assistant message parts the reducer emits.
// ---------------------------------------------------------------------------

export interface ToolPart {
  readonly type: 'tool-call';
  readonly toolCallId: string;
  readonly toolName: string;
  readonly argsText: string;
  readonly result?: string;
  /** Epoch-ms timing copied from AG-UI event timestamps when present. */
  readonly startedAt?: number;
  readonly finishedAt?: number;
  /** Typed display attached by the CUSTOM/aura.display frame (live) or the
   *  re-derived snapshot tool turn (replay), keyed by toolCallId. */
  readonly display?: DisplayPayload;
}
export interface TextPart {
  readonly type: 'text';
  readonly text: string;
}
export interface ReasoningPart {
  readonly type: 'reasoning';
  readonly text: string;
  /** 1.12 rehydrate: elapsed reasoning wallclock (ms) from the snapshot's
   *  `reasoningDurationMs` decoration. Absent on live-streamed parts (they carry
   *  startedAt/finishedAt instead); the ReasoningPill consumes either. */
  readonly durationMs?: number;
  /** Epoch-ms from the REASONING_* frame timestamps (compact-chat spec §5.2.1;
   *  mirror of ToolPart.startedAt/finishedAt). Absent on rehydrated parts. */
  readonly startedAt?: number;
  readonly finishedAt?: number;
}

/** The assistant message-part union the reducer emits. */
export type ChatPart = TextPart | ReasoningPart | ToolPart;

// ---------------------------------------------------------------------------
// Snapshot wire shapes — the persisted MESSAGES_SNAPSHOT body the history
// rehydration reads (GET /threads/{id}/messages).
// ---------------------------------------------------------------------------

export interface SnapshotToolFunction {
  readonly name?: unknown;
  readonly arguments?: unknown;
}

export interface SnapshotToolCall {
  readonly id?: unknown;
  readonly function?: SnapshotToolFunction;
  /** Re-derived typed display (D-06): the backend re-runs the display normalizer
   *  per tool turn at snapshot projection time and attaches it here. */
  readonly display?: unknown;
}

export interface SnapshotMessage {
  readonly id?: unknown;
  readonly role?: unknown;
  readonly content?: unknown;
  readonly toolCallId?: unknown;
  readonly toolCalls?: unknown;
  /** 1.12: optional reasoning decoration on answer-shaped assistant messages
   *  (no tool calls) — camelCase, omitempty on the Go side. */
  readonly reasoning?: unknown;
  readonly reasoningDurationMs?: unknown;
}

export interface MessagesSnapshotFrame {
  readonly type?: unknown;
  readonly messages?: unknown;
}

// ---------------------------------------------------------------------------
// Shared error helper — used by both the snapshot fetch and the stream runners.
// ---------------------------------------------------------------------------

/**
 * Surface a non-OK response as a human-readable error (WR-03). The backend sends
 * meaningful, ALREADY-sanitized text bodies (e.g. "thread already has an in-flight run"
 * on 409, "diverge_seq must be a positive turn seq" on 400, sanitizeErr-redacted 500s);
 * collapsing every failure to a bare "HTTP <status>" hides the cause from the operator —
 * the only viewer. Read the body and prefer it; fall back to the status when it is empty
 * or unreadable.
 */
export async function errorDetail(res: Response): Promise<string> {
  const status = `HTTP ${String(res.status)}`;
  if (res.body === null) return status;
  const detail = (await res.text().catch(() => '')).trim();
  return detail.length > 0 ? detail : status;
}
