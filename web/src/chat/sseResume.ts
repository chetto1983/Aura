import type { ThreadMessageLike } from '@assistant-ui/react';
import { getJSON } from '../api/json';
import { buildAuraRunBody } from './auraRunBody';
import {
  artifactDescriptorValue,
  fetchThreadMessages,
  newAssistantTurn,
  readSSEFrames,
  reduceFrame,
  SSE_REQUEST_HEADERS,
  toThreadMessage,
  type AssistantTurnState,
  type StreamRunOptions,
  type TurnUsage,
} from './sseAdapter';
import { errorDetail } from './sseAdapter_frames';

// sseResume — the fix-plan 1.3 Tier B (RS-07) resilience wrapper around the
// /agent/run SSE stream. A sibling of sseAdapter (which owns the PURE reducer
// and wire parsing); this module owns the retry/reconnect POLICY: Last-Event-ID
// resume (design §4.1), page-reload attach (§4.2), the failure UX (§4.3), and
// the explicit Stop → cancel POST (§4.4). NO React, NO rendering.
//
// Resume engages only when the gateway advertised the capability: integer SSE
// `id:` lines AND a RUN_STARTED runId were both observed (AURA_AGUI_RUN_DETACH
// on). Otherwise every path degrades to today's single-shot behavior — a clean
// EOF returns, a mid-stream cut propagates to the caller's error surface.

const DEFAULT_MAX_RETRIES = 5;
const DEFAULT_BACKOFF_BASE_MS = 500;
const BACKOFF_CAP_MS = 10_000;

export interface ResilientRunOptions extends StreamRunOptions {
  /** Bounded reconnect budget (design §4.1); also caps pre-byte POST retries. */
  readonly maxRetries?: number;
  /** Backoff base; doubles per attempt, capped at 10s, ±25% jitter. */
  readonly backoffBaseMs?: number;
  /** Fires once when RUN_STARTED reveals the runId — the Stop→cancel key (§4.4). */
  readonly onRunId?: (runId: string) => void;
  /** i18n'd note shown on the incomplete message when resume is impossible (§4.3). */
  readonly connectionLostNote?: string;
  /** Receives the refetched snapshot projection on the §4.3/§5.2 fallback paths. */
  readonly onSnapshotReplace?: (messages: ThreadMessageLike[]) => void;
}

export interface AttachRunOptions {
  readonly threadId: string;
  /** The live run to attach to — the conversation DTO's live_run_id (§4.2). */
  readonly runId: string;
  readonly signal: AbortSignal;
  readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
  readonly onArtifact?: (assetId: string | undefined) => void;
  readonly newId?: () => string;
  readonly maxRetries?: number;
  readonly backoffBaseMs?: number;
  readonly connectionLostNote?: string;
  readonly onSnapshotReplace?: (messages: ThreadMessageLike[]) => void;
}

/** The option subset both entry points share — what makeEngine consumes. */
interface EngineOptions {
  readonly threadId: string;
  readonly signal: AbortSignal;
  readonly maxRetries?: number | undefined;
  readonly backoffBaseMs?: number | undefined;
  readonly connectionLostNote?: string | undefined;
  readonly onUpdate: (message: ThreadMessageLike, usage: TurnUsage | undefined) => void;
  readonly onArtifact?: ((assetId: string | undefined) => void) | undefined;
  readonly onRunId?: ((runId: string) => void) | undefined;
  readonly onSnapshotReplace?: ((messages: ThreadMessageLike[]) => void) | undefined;
}

/**
 * The mutable per-send accumulator: ONE AssistantTurnState shared across every
 * connection attempt (the state survives the cut — that is the whole point),
 * plus the resume cursor (lastEventId) and the run identity.
 */
interface ResumeEngine extends EngineOptions {
  readonly state: AssistantTurnState;
  readonly maxRetries: number;
  readonly backoffBaseMs: number;
  readonly connectionLostNote: string;
  runId?: string;
  lastEventId?: number;
  terminal: boolean;
}

function makeEngine(state: AssistantTurnState, opts: EngineOptions): ResumeEngine {
  return {
    state,
    threadId: opts.threadId,
    signal: opts.signal,
    maxRetries: opts.maxRetries ?? DEFAULT_MAX_RETRIES,
    backoffBaseMs: opts.backoffBaseMs ?? DEFAULT_BACKOFF_BASE_MS,
    connectionLostNote: opts.connectionLostNote ?? 'connection lost',
    onUpdate: opts.onUpdate,
    onArtifact: opts.onArtifact,
    onRunId: opts.onRunId,
    onSnapshotReplace: opts.onSnapshotReplace,
    terminal: false,
  };
}

/** Exponential backoff with ±25% jitter (design §4.1): base·2^attempt, cap 10s. */
function backoffDelayMs(attempt: number, baseMs: number): number {
  const exp = Math.min(baseMs * 2 ** attempt, BACKOFF_CAP_MS);
  return Math.max(0, Math.round(exp + exp * 0.25 * (Math.random() * 2 - 1)));
}

function abortError(signal: AbortSignal): Error {
  const reason: unknown = signal.reason;
  return reason instanceof Error
    ? reason
    : new DOMException('The user aborted a request.', 'AbortError');
}

/** Abort-aware sleep: user Stop must never wait out a backoff window. */
function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(abortError(signal));
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(abortError(signal));
    };
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

/**
 * Fold one SSE response body into the engine state. Tracks the resume cursor
 * (integer `id:` values) and the run identity, and fires the pump-level
 * onArtifact signal exactly like streamSSE (T-37B-15: never from the pure
 * reducer). Returns 'terminal' once a RUN_FINISHED/RUN_ERROR folded — the turn
 * is complete regardless of how the connection ends afterwards.
 */
async function pumpBody(
  body: ReadableStream<Uint8Array>,
  eng: ResumeEngine,
): Promise<'terminal' | 'end'> {
  for await (const { frame, id } of readSSEFrames(body)) {
    if (id !== undefined) eng.lastEventId = id;
    if (
      frame.type === 'RUN_STARTED' &&
      eng.runId === undefined &&
      typeof frame.runId === 'string' &&
      frame.runId.length > 0
    ) {
      eng.runId = frame.runId;
      eng.onRunId?.(frame.runId);
    }
    reduceFrame(eng.state, frame);
    const artifact = artifactDescriptorValue(frame);
    if (artifact !== null) eng.onArtifact?.(artifact.asset_id);
    if (frame.type === 'RUN_FINISHED' || frame.type === 'RUN_ERROR') eng.terminal = true;
    eng.onUpdate(toThreadMessage(eng.state), eng.state.usage);
  }
  return eng.terminal ? 'terminal' : 'end';
}

/** Surface a non-OK response exactly like streamSSE does (WR-03 detail body). */
async function surfaceHTTPError(eng: ResumeEngine, res: Response): Promise<TurnUsage | undefined> {
  eng.state.error = await errorDetail(res);
  eng.state.status = { type: 'incomplete', reason: 'error' };
  eng.onUpdate(toThreadMessage(eng.state), eng.state.usage);
  return eng.state.usage;
}

/** Best-effort snapshot refetch → onSnapshotReplace; a failure keeps the partial view. */
async function replaceFromSnapshot(eng: ResumeEngine): Promise<void> {
  if (eng.onSnapshotReplace === undefined) return;
  try {
    eng.onSnapshotReplace(await fetchThreadMessages(eng.threadId, eng.signal));
  } catch {
    // Best-effort recovery: nothing here is worse than today's total loss.
  }
}

/**
 * §4.3 failure UX — resume impossible (retries exhausted / 404 / 410): mark the
 * assistant message incomplete with the i18n'd connection-lost note, then
 * refetch the persisted snapshot. If the run finished server-side the persisted
 * turn replaces the partial; if it is still live, the conversation refetch
 * re-exposes live_run_id for a later attach.
 */
async function failWithSnapshot(eng: ResumeEngine): Promise<TurnUsage | undefined> {
  eng.state.error = eng.connectionLostNote;
  eng.state.status = { type: 'incomplete', reason: 'error' };
  eng.onUpdate(toThreadMessage(eng.state), eng.state.usage);
  await replaceFromSnapshot(eng);
  return eng.state.usage;
}

function fetchResumeEvents(
  runId: string,
  lastEventId: number | undefined,
  signal: AbortSignal,
): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'text/event-stream' };
  // Absent header ⇒ full replay from seq 1; present ⇒ replay strictly from n+1,
  // so the reducer never re-folds an acknowledged frame (§3.3 exact contract).
  if (lastEventId !== undefined) headers['Last-Event-ID'] = String(lastEventId);
  return fetch(`/agent/runs/${encodeURIComponent(runId)}/events`, {
    method: 'GET',
    headers,
    credentials: 'same-origin',
    signal,
  });
}

/**
 * Bounded reconnect (design §4.1): GET the resume endpoint from the cursor and
 * keep folding into the SAME turn state. 404 (unknown/reaped/foreign/flag-off)
 * and 410 (replay window exceeded) are unrecoverable — the snapshot fallback
 * takes over. Any other failure consumes one backoff attempt.
 */
async function reconnectLoop(eng: ResumeEngine, runId: string): Promise<TurnUsage | undefined> {
  for (let attempt = 0; attempt < eng.maxRetries; attempt += 1) {
    await sleep(backoffDelayMs(attempt, eng.backoffBaseMs), eng.signal);
    let res: Response;
    try {
      res = await fetchResumeEvents(runId, eng.lastEventId, eng.signal);
    } catch (err) {
      if (eng.signal.aborted) throw err;
      continue;
    }
    if (res.status === 404 || res.status === 410) return failWithSnapshot(eng);
    if (!res.ok || res.body === null) continue;
    try {
      if ((await pumpBody(res.body, eng)) === 'terminal') return eng.state.usage;
      // A clean close without a terminal frame is another cut — retry.
    } catch (err) {
      if (eng.signal.aborted) throw err;
      if (eng.terminal) return eng.state.usage;
    }
  }
  return failWithSnapshot(eng);
}

/** First resume GET + pump + reconnect — shared by attachRun and §5.2 recovery. */
async function attachEngine(eng: ResumeEngine, runId: string): Promise<TurnUsage | undefined> {
  let res: Response;
  try {
    res = await fetchResumeEvents(runId, eng.lastEventId, eng.signal);
  } catch (err) {
    if (eng.signal.aborted) throw err;
    return reconnectLoop(eng, runId);
  }
  if (res.status === 404 || res.status === 410) return failWithSnapshot(eng);
  if (!res.ok || res.body === null) return surfaceHTTPError(eng, res);
  try {
    if ((await pumpBody(res.body, eng)) === 'terminal') return eng.state.usage;
  } catch (err) {
    if (eng.signal.aborted) throw err;
    if (eng.terminal) return eng.state.usage;
  }
  return reconnectLoop(eng, runId);
}

/**
 * §5.2 recovery after a retried POST answered 409 + Retry-After ("operation is
 * still in progress"): the run exists — discover its id off the conversation
 * DTO and attach read-only (full replay). No live_run_id (already finished and
 * evicted) ⇒ the snapshot is the truth.
 */
async function attachAfterInProgress(eng: ResumeEngine): Promise<TurnUsage | undefined> {
  let liveRunId: string | undefined;
  try {
    const conversation = await getJSON<{ readonly live_run_id?: string }>(
      `/api/conversations/${encodeURIComponent(eng.threadId)}`,
      eng.signal,
    );
    liveRunId = conversation.live_run_id;
  } catch (err) {
    if (eng.signal.aborted) throw err;
  }
  if (liveRunId === undefined || liveRunId.length === 0) return failWithSnapshot(eng);
  eng.runId = liveRunId;
  eng.onRunId?.(liveRunId);
  return attachEngine(eng, liveRunId);
}

/**
 * POST /agent/run with ONE Idempotency-Key for this logical send. A pre-byte
 * network failure (fetch TypeError — no response byte arrived) retries the SAME
 * key under the bounded backoff budget; the §5.2 registry outcomes on a retried
 * key are handled here: 409 + Retry-After ⇒ the run exists — attach read-only;
 * 422 (indeterminate) ⇒ the run finished — snapshot refetch. Returns null when
 * recovery already produced the final UI state.
 */
async function postRun(
  id: string,
  opts: ResilientRunOptions,
  eng: ResumeEngine,
): Promise<Response | null> {
  const idempotencyKey = crypto.randomUUID();
  const body = JSON.stringify(buildAuraRunBody(id, opts));
  let retried = false;
  for (let attempt = 0; ; attempt += 1) {
    let res: Response;
    try {
      res = await fetch('/agent/run', {
        method: 'POST',
        headers: { ...SSE_REQUEST_HEADERS, 'Idempotency-Key': idempotencyKey },
        credentials: 'same-origin',
        body,
        signal: opts.signal,
      });
    } catch (err) {
      if (opts.signal.aborted) throw err;
      if (!(err instanceof TypeError) || attempt >= eng.maxRetries) throw err;
      retried = true;
      await sleep(backoffDelayMs(attempt, eng.backoffBaseMs), opts.signal);
      continue;
    }
    if (retried && res.status === 409 && res.headers.has('Retry-After')) {
      await attachAfterInProgress(eng);
      return null;
    }
    if (retried && res.status === 422) {
      await replaceFromSnapshot(eng);
      return null;
    }
    return res;
  }
}

/**
 * streamRun's resilient twin (design §4.1): same POST body, same reducer fold,
 * but the assistant turn survives a mid-stream network cut by reconnecting to
 * GET /agent/runs/{runId}/events with the Last-Event-ID cursor. Abort (user
 * Stop) always rethrows — the caller owns the §4.4 cancel POST.
 */
export async function streamRunResilient(
  opts: ResilientRunOptions,
): Promise<TurnUsage | undefined> {
  const id = (opts.newId ?? (() => crypto.randomUUID()))();
  const state = newAssistantTurn(id);
  opts.onUpdate(toThreadMessage(state), state.usage);
  const eng = makeEngine(state, opts);

  const res = await postRun(id, opts, eng);
  if (res === null) return eng.state.usage; // recovered via §5.2 attach/snapshot
  if (!res.ok || res.body === null) return surfaceHTTPError(eng, res);

  let cutError: unknown;
  let cleanEnd = false;
  try {
    if ((await pumpBody(res.body, eng)) === 'terminal') return eng.state.usage;
    cleanEnd = true;
  } catch (err) {
    if (eng.signal.aborted) throw err;
    if (eng.terminal) return eng.state.usage;
    cutError = err;
  }
  const runId = eng.runId;
  if (runId === undefined || eng.lastEventId === undefined) {
    // No resume capability observed (flag off / SDK non-integer ids): behave
    // exactly like today's single-shot streamRun — a clean EOF returns, a
    // network cut propagates to the caller's error surface.
    if (cleanEnd) return eng.state.usage;
    throw cutError;
  }
  return reconnectLoop(eng, runId);
}

/**
 * Page-reload attach (design §4.2): fold a live run discovered via the
 * conversation DTO's live_run_id onto a FRESH assistant message. No
 * Last-Event-ID on the first GET — the full-buffer replay rebuilds the
 * in-flight partial turn (text so far, open tool cards, reasoning drawer),
 * then the stream continues live through the same reducer.
 */
export async function attachRun(opts: AttachRunOptions): Promise<TurnUsage | undefined> {
  const id = (opts.newId ?? (() => crypto.randomUUID()))();
  const state = newAssistantTurn(id);
  opts.onUpdate(toThreadMessage(state), state.usage);
  const eng = makeEngine(state, opts);
  eng.runId = opts.runId;
  return attachEngine(eng, opts.runId);
}

/**
 * The explicit Stop (design §4.4): with run-detach on, aborting the fetch only
 * detaches the viewer, so Stop maps to abort() THEN this cancel POST. The
 * installMutationIdempotency fetch wrapper auto-attaches the Idempotency-Key
 * (the route is in the gateway's mutation inventory); the 202 response is
 * idempotent, also on an already-terminal run.
 */
export async function cancelRun(runId: string): Promise<void> {
  const res = await fetch(`/agent/runs/${encodeURIComponent(runId)}/cancel`, {
    method: 'POST',
    credentials: 'same-origin',
  });
  if (!res.ok) throw new Error(await errorDetail(res));
}
