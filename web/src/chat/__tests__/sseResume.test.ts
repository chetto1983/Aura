import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
// Driven by the REAL captured translator output (the same golden fixture the Go
// SSE tests and sseAdapter.test.ts use) so the resilience wrapper is exercised
// against realistic per-turn sequences, never synthetic shapes.
import goldenEvents from '../../../../internal/agui/testdata/golden-events.json';
import { installMutationIdempotency } from '../../api/idempotency';
import {
  messageParts,
  newAssistantTurn,
  reduceFrame,
  toThreadMessage,
  type AguiFrame,
} from '../sseAdapter';
import { attachRun, cancelRun, streamRunResilient } from '../sseResume';

const golden = goldenEvents as Record<string, AguiFrame>;

function frame(name: string): AguiFrame {
  const f = golden[name];
  if (f === undefined) throw new Error(`golden fixture missing "${name}"`);
  return f;
}

/** A realistic full turn: reasoning → tool call → answer → usage → finished. */
const TURN_FRAMES: readonly AguiFrame[] = [
  frame('RUN_STARTED'), // runId run-1
  frame('REASONING_START'),
  frame('REASONING_MESSAGE_START'),
  frame('REASONING_MESSAGE_CONTENT'),
  frame('REASONING_MESSAGE_END'),
  frame('REASONING_END'),
  frame('TOOL_CALL_START'),
  frame('TOOL_CALL_ARGS'),
  frame('TOOL_CALL_END'),
  frame('TOOL_CALL_RESULT'),
  frame('TEXT_MESSAGE_START'),
  frame('TEXT_MESSAGE_CONTENT'),
  frame('TEXT_MESSAGE_END'),
  frame('STATE_DELTA'), // /cost_usd usage
  frame('RUN_FINISHED(success)'),
];

/** Flag-on wire: every frame carries `id: <seq>` (1-based, strictly monotonic). */
function wireOf(frames: readonly AguiFrame[], startId: number): string {
  return frames
    .map((f, i) => `event: ${f.type}\nid: ${String(startId + i)}\ndata: ${JSON.stringify(f)}\n\n`)
    .join('');
}

/** Flag-off wire: no id lines — the pre-1.3B byte shape. */
function wireWithoutIds(frames: readonly AguiFrame[]): string {
  return frames.map((f) => `event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`).join('');
}

function sseResponse(wire: string): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

/** A stream that delivers its bytes then dies with a network error (mid-stream cut).
 *  pull-based: erroring inside start() would DISCARD the queued chunk (WHATWG
 *  streams drop the queue on error), simulating a pre-byte failure instead. */
function cutResponse(wire: string): Response {
  const enc = new TextEncoder();
  let sent = false;
  const body = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (!sent) {
        sent = true;
        controller.enqueue(enc.encode(wire));
        return;
      }
      controller.error(new TypeError('network cut'));
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

function snapshotResponse(): Response {
  return new Response(
    JSON.stringify({
      type: 'MESSAGES_SNAPSHOT',
      messages: [{ id: 'msg-1', role: 'user', content: 'persisted prompt' }],
    }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  );
}

/** The uninterrupted control: fold every frame through the pure reducer. */
function foldAll(frames: readonly AguiFrame[]): ThreadMessageLike {
  const state = newAssistantTurn('fixed-id');
  for (const f of frames) reduceFrame(state, f);
  return toThreadMessage(state);
}

function urlOf(call: readonly unknown[]): string {
  return String(call[0]);
}

function initOf(call: readonly unknown[]): RequestInit | undefined {
  return call[1] as RequestInit | undefined;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('sseResume — split-replay reducer property (§4.1)', () => {
  it('fold(frames) === fold(frames[0..k]) + resume(frames[k+1..]) for every split k', () => {
    // The wrapper folds the resumed suffix into the SAME AssistantTurnState the
    // prefix built; the server replays strictly from n+1 (§3.3), so for every
    // possible cut point the continued fold must equal the uninterrupted fold.
    const control = foldAll(TURN_FRAMES);
    for (let k = 0; k <= TURN_FRAMES.length; k += 1) {
      const state = newAssistantTurn('fixed-id');
      for (const f of TURN_FRAMES.slice(0, k)) reduceFrame(state, f);
      for (const f of TURN_FRAMES.slice(k)) reduceFrame(state, f);
      expect(toThreadMessage(state)).toEqual(control);
    }
  });
});

describe('sseResume — streamRunResilient', () => {
  it('a clean id-carrying stream behaves single-shot (one fetch, explicit Idempotency-Key)', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(sseResponse(wireOf(TURN_FRAMES, 1))));
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    const usage = await streamRunResilient({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: (m) => updates.push(m),
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const init = initOf(fetchMock.mock.calls[0] as readonly unknown[]);
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    // ONE explicit key per logical send (§5.2) — set by the wrapper itself so a
    // retried POST re-sends the identical header.
    expect(new Headers(init?.headers).get('Idempotency-Key')).toMatch(/^[0-9a-f-]{36}$/);
    expect(JSON.parse(init?.body as string)).toEqual({
      threadId: 'thread-1',
      messages: [{ id: 'fixed-id', role: 'user', content: 'ciao' }],
    });
    expect(updates.at(-1)).toEqual(foldAll(TURN_FRAMES));
    expect(usage?.costUsd).toBe(0.0042);
  });

  it('reconnects after a mid-stream cut with Last-Event-ID and matches the uninterrupted control', async () => {
    const k = 9; // cut right after TOOL_CALL_RESULT (id 10)
    const fetchMock = vi.fn((url: string) => {
      if (url === '/agent/run') {
        return Promise.resolve(cutResponse(wireOf(TURN_FRAMES.slice(0, k + 1), 1)));
      }
      return Promise.resolve(sseResponse(wireOf(TURN_FRAMES.slice(k + 1), k + 2)));
    });
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    const runIds: string[] = [];
    const usage = await streamRunResilient({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      backoffBaseMs: 0,
      onRunId: (id) => runIds.push(id),
      onUpdate: (m) => updates.push(m),
    });

    const resume = fetchMock.mock.calls.find((c) => urlOf(c).startsWith('/agent/runs/'));
    if (resume === undefined) throw new Error('expected a resume GET');
    expect(urlOf(resume)).toBe('/agent/runs/run-1/events');
    const resumeInit = initOf(resume);
    expect(resumeInit?.method).toBe('GET');
    expect(resumeInit?.credentials).toBe('same-origin');
    // The cursor is the last acknowledged id — replay resumes strictly at n+1.
    expect(new Headers(resumeInit?.headers).get('Last-Event-ID')).toBe(String(k + 1));
    expect(runIds).toEqual(['run-1']);
    expect(updates.at(-1)).toEqual(foldAll(TURN_FRAMES));
    expect(usage?.costUsd).toBe(0.0042);
  });

  it('exhausted retries → incomplete + connection-lost note + snapshot refetch (§4.3)', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/agent/run') {
        return Promise.resolve(cutResponse(wireOf(TURN_FRAMES.slice(0, 3), 1)));
      }
      if (url.startsWith('/agent/runs/')) return Promise.reject(new TypeError('still down'));
      return Promise.resolve(snapshotResponse());
    });
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    const replaced: ThreadMessageLike[][] = [];
    await streamRunResilient({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      maxRetries: 2,
      backoffBaseMs: 0,
      connectionLostNote: 'connessione persa — nota',
      onSnapshotReplace: (m) => replaced.push(m),
      onUpdate: (m) => updates.push(m),
    });

    const last = updates.at(-1);
    if (last === undefined) throw new Error('expected onUpdate calls');
    expect(last.status).toEqual({ type: 'incomplete', reason: 'error' });
    expect(
      messageParts(last).some((p) => p.type === 'text' && p.text === 'connessione persa — nota'),
    ).toBe(true);
    // Exactly maxRetries resume attempts, then the snapshot fallback.
    expect(fetchMock.mock.calls.filter((c) => urlOf(c).startsWith('/agent/runs/')).length).toBe(2);
    expect(replaced).toHaveLength(1);
    expect(replaced[0]?.[0]).toMatchObject({ role: 'user' });
  });

  it.each([404, 410])(
    'a %d from the resume endpoint falls back immediately (no further retries)',
    async (status) => {
      const fetchMock = vi.fn((url: string) => {
        if (url === '/agent/run') {
          return Promise.resolve(cutResponse(wireOf(TURN_FRAMES.slice(0, 3), 1)));
        }
        if (url.startsWith('/agent/runs/')) {
          return Promise.resolve(new Response('{"error":"replay window exceeded"}', { status }));
        }
        return Promise.resolve(snapshotResponse());
      });
      vi.stubGlobal('fetch', fetchMock);

      const updates: ThreadMessageLike[] = [];
      const replaced: ThreadMessageLike[][] = [];
      await streamRunResilient({
        threadId: 'thread-1',
        userText: 'ciao',
        signal: new AbortController().signal,
        newId: () => 'fixed-id',
        maxRetries: 5,
        backoffBaseMs: 0,
        connectionLostNote: 'nota',
        onSnapshotReplace: (m) => replaced.push(m),
        onUpdate: (m) => updates.push(m),
      });

      expect(fetchMock.mock.calls.filter((c) => urlOf(c).startsWith('/agent/runs/')).length).toBe(
        1,
      );
      expect(updates.at(-1)?.status).toEqual({ type: 'incomplete', reason: 'error' });
      expect(replaced).toHaveLength(1);
    },
  );

  it('degrades to the single-shot contract when the wire has no integer ids (flag off)', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(cutResponse(wireWithoutIds(TURN_FRAMES.slice(0, 3)))),
    );
    vi.stubGlobal('fetch', fetchMock);

    await expect(
      streamRunResilient({
        threadId: 'thread-1',
        userText: 'ciao',
        signal: new AbortController().signal,
        newId: () => 'fixed-id',
        backoffBaseMs: 0,
        onUpdate: () => undefined,
      }),
    ).rejects.toThrow('network cut');
    // The resume endpoint was never touched — exactly today's failure surface.
    expect(fetchMock.mock.calls.filter((c) => urlOf(c).startsWith('/agent/runs/')).length).toBe(0);
  });

  it('abort during the reconnect backoff rethrows AbortError (user Stop, never §4.3)', async () => {
    const controller = new AbortController();
    const fetchMock = vi.fn((url: string) => {
      if (url === '/agent/run') {
        return Promise.resolve(cutResponse(wireOf(TURN_FRAMES.slice(0, 3), 1)));
      }
      return Promise.resolve(snapshotResponse());
    });
    vi.stubGlobal('fetch', fetchMock);

    const pending = streamRunResilient({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: controller.signal,
      newId: () => 'fixed-id',
      backoffBaseMs: 60_000, // park the wrapper inside the backoff sleep
      onUpdate: () => undefined,
    });
    await new Promise((resolve) => setTimeout(resolve, 10));
    controller.abort();
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('surfaces a non-OK POST body exactly like streamRun (WR-03)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(new Response('thread already has an in-flight run', { status: 409 })),
      ),
    );
    const updates: ThreadMessageLike[] = [];
    await streamRunResilient({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: (m) => updates.push(m),
    });
    const last = updates.at(-1);
    if (last === undefined) throw new Error('expected onUpdate calls');
    expect(last.status).toEqual({ type: 'incomplete', reason: 'error' });
    expect(
      messageParts(last).some(
        (p) => p.type === 'text' && p.text === 'thread already has an in-flight run',
      ),
    ).toBe(true);
  });
});

describe('sseResume — pre-byte POST retry recipe (§5.2)', () => {
  it('retries a pre-byte TypeError with the SAME Idempotency-Key and body', async () => {
    let postCount = 0;
    const keys: (string | null)[] = [];
    const bodies: (BodyInit | null | undefined)[] = [];
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url !== '/agent/run') throw new Error(`unexpected fetch ${url}`);
      postCount += 1;
      keys.push(new Headers(init?.headers).get('Idempotency-Key'));
      bodies.push(init?.body);
      if (postCount === 1) return Promise.reject(new TypeError('pre-byte failure'));
      return Promise.resolve(sseResponse(wireOf(TURN_FRAMES, 1)));
    });
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    await streamRunResilient({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      backoffBaseMs: 0,
      onUpdate: (m) => updates.push(m),
    });

    expect(postCount).toBe(2);
    expect(keys[0]).toMatch(/^[0-9a-f-]{36}$/);
    expect(keys[1]).toBe(keys[0]);
    expect(bodies[1]).toBe(bodies[0]);
    expect(updates.at(-1)).toEqual(foldAll(TURN_FRAMES));
  });

  it('409 + Retry-After on the retried key → discovers live_run_id and attaches read-only', async () => {
    let postCount = 0;
    const fetchMock = vi.fn((url: string) => {
      if (url === '/agent/run') {
        postCount += 1;
        if (postCount === 1) return Promise.reject(new TypeError('pre-byte failure'));
        return Promise.resolve(
          new Response('{"error":"operation is still in progress"}', {
            status: 409,
            headers: { 'Retry-After': '2' },
          }),
        );
      }
      if (url === '/api/conversations/thread-1') {
        return Promise.resolve(
          new Response(JSON.stringify({ ID: 'thread-1', live_run_id: 'run-9' }), { status: 200 }),
        );
      }
      if (url === '/agent/runs/run-9/events') {
        return Promise.resolve(sseResponse(wireOf(TURN_FRAMES, 1)));
      }
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    const runIds: string[] = [];
    await streamRunResilient({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      backoffBaseMs: 0,
      onRunId: (id) => runIds.push(id),
      onUpdate: (m) => updates.push(m),
    });

    expect(postCount).toBe(2); // never a third POST — no duplicate turn
    expect(runIds).toEqual(['run-9']);
    const attach = fetchMock.mock.calls.find((c) => urlOf(c) === '/agent/runs/run-9/events');
    if (attach === undefined) throw new Error('expected the read-only attach GET');
    // Full replay: the retried sender has nothing — no Last-Event-ID header.
    expect(new Headers(initOf(attach)?.headers).get('Last-Event-ID')).toBeNull();
    expect(updates.at(-1)).toEqual(foldAll(TURN_FRAMES));
  });

  it('422 on the retried key (indeterminate: run finished) → snapshot refetch', async () => {
    let postCount = 0;
    const fetchMock = vi.fn((url: string) => {
      if (url === '/agent/run') {
        postCount += 1;
        if (postCount === 1) return Promise.reject(new TypeError('pre-byte failure'));
        return Promise.resolve(new Response('operation already completed', { status: 422 }));
      }
      return Promise.resolve(snapshotResponse());
    });
    vi.stubGlobal('fetch', fetchMock);

    const replaced: ThreadMessageLike[][] = [];
    const usage = await streamRunResilient({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      backoffBaseMs: 0,
      onSnapshotReplace: (m) => replaced.push(m),
      onUpdate: () => undefined,
    });

    expect(usage).toBeUndefined();
    expect(replaced).toHaveLength(1);
    expect(replaced[0]?.[0]).toMatchObject({ role: 'user' });
    expect(fetchMock.mock.calls.filter((c) => urlOf(c).startsWith('/agent/runs/')).length).toBe(0);
  });
});

describe('sseResume — attachRun (§4.2 reload-attach)', () => {
  it('GETs the resume endpoint with NO Last-Event-ID and folds the full replay', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(sseResponse(wireOf(TURN_FRAMES, 1))));
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    const usage = await attachRun({
      threadId: 'thread-1',
      runId: 'run-1',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: (m) => updates.push(m),
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const call = fetchMock.mock.calls[0] as readonly unknown[];
    expect(urlOf(call)).toBe('/agent/runs/run-1/events');
    expect(new Headers(initOf(call)?.headers).get('Last-Event-ID')).toBeNull();
    expect(updates.at(-1)).toEqual(foldAll(TURN_FRAMES));
    expect(usage?.costUsd).toBe(0.0042);
  });

  it('a cut mid-attach reconnects from the acknowledged cursor', async () => {
    const k = 11; // cut inside the answer text
    let resumeCalls = 0;
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url !== '/agent/runs/run-1/events') throw new Error(`unexpected fetch ${url}`);
      resumeCalls += 1;
      if (resumeCalls === 1) {
        expect(new Headers(init?.headers).get('Last-Event-ID')).toBeNull();
        return Promise.resolve(cutResponse(wireOf(TURN_FRAMES.slice(0, k + 1), 1)));
      }
      expect(new Headers(init?.headers).get('Last-Event-ID')).toBe(String(k + 1));
      return Promise.resolve(sseResponse(wireOf(TURN_FRAMES.slice(k + 1), k + 2)));
    });
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    await attachRun({
      threadId: 'thread-1',
      runId: 'run-1',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      backoffBaseMs: 0,
      onUpdate: (m) => updates.push(m),
    });

    expect(resumeCalls).toBe(2);
    expect(updates.at(-1)).toEqual(foldAll(TURN_FRAMES));
  });

  it('404 (reaped/foreign/flag-off) → connection-lost fallback + snapshot refetch', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url.startsWith('/agent/runs/')) {
        return Promise.resolve(new Response('run not found', { status: 404 }));
      }
      return Promise.resolve(snapshotResponse());
    });
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    const replaced: ThreadMessageLike[][] = [];
    await attachRun({
      threadId: 'thread-1',
      runId: 'run-gone',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      connectionLostNote: 'nota',
      onSnapshotReplace: (m) => replaced.push(m),
      onUpdate: (m) => updates.push(m),
    });

    expect(updates.at(-1)?.status).toEqual({ type: 'incomplete', reason: 'error' });
    expect(replaced).toHaveLength(1);
  });
});

describe('sseResume — cancelRun (§4.4 Stop)', () => {
  it('POSTs the cancel route same-origin and resolves on 202', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response('{"status":"cancelling"}', { status: 202 })),
    );
    vi.stubGlobal('fetch', fetchMock);
    await cancelRun('run-1');
    expect(fetchMock).toHaveBeenCalledWith(
      '/agent/runs/run-1/cancel',
      expect.objectContaining({ method: 'POST', credentials: 'same-origin' }),
    );
  });

  it('rides the installMutationIdempotency wrapper: the Idempotency-Key is auto-attached', async () => {
    const inner = vi.fn(() =>
      Promise.resolve(new Response('{"status":"cancelling"}', { status: 202 })),
    );
    vi.stubGlobal('fetch', inner);
    // The wrapper is what production installs at boot (main.tsx): it must see
    // the cancel POST as a mutation and stamp the required Idempotency-Key.
    installMutationIdempotency();
    await cancelRun('run-1');
    expect(inner).toHaveBeenCalledTimes(1);
    const init = initOf(inner.mock.calls[0] as readonly unknown[]);
    expect(new Headers(init?.headers).get('Idempotency-Key')).toMatch(/^[0-9a-f-]{36}$/);
  });

  it('throws the sanitized backend detail on a non-OK response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('run not found', { status: 404 }))),
    );
    await expect(cancelRun('run-x')).rejects.toThrow('run not found');
  });
});
