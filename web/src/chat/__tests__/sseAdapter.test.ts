import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
// The test is driven by the REAL captured translator output — the golden frame
// fixture the Go SSE tests use (internal/agui/testdata/golden-events.json),
// imported as a JSON module so no synthetic inline shapes leak in (no-skip-as-
// green discipline). The fixture is a sibling of web/; vitest.config server.fs.allow
// permits the one-level-up read. The fixture is a map of canonical event-name → the
// exact JSON the AG-UI SSE writer emits for that event type; the tests assemble
// realistic per-turn SEQUENCES from those captured shapes.
import goldenEvents from '../../../../internal/agui/testdata/golden-events.json';
import {
  cacheHitRatio,
  fetchThreadMessages,
  isUsageDelta,
  messageParts,
  newAssistantTurn,
  parseSSEBlock,
  readSSEFrames,
  reduceFrame,
  snapshotToThreadMessages,
  streamRun,
  toThreadMessage,
  toolCallIdFromDelta,
  usageFromStateDelta,
  type AguiFrame,
  type JSONPatchOp,
} from '../sseAdapter';

const golden = goldenEvents as Record<string, AguiFrame>;

/** A STATE_DELTA frame narrowed to its delta ops (the fixture's STATE_DELTA). */
function deltaOps(f: AguiFrame): readonly JSONPatchOp[] {
  if (f.type !== 'STATE_DELTA') throw new Error('not a STATE_DELTA frame');
  return f.delta;
}

// The test is driven by the REAL captured translator output — the golden frame
// fixture the Go SSE tests use (internal/agui/testdata/golden-events.json), read
function frame(name: string): AguiFrame {
  const f = golden[name];
  if (f === undefined) throw new Error(`golden fixture missing "${name}"`);
  return f;
}

/** A reasoning STATE_DELTA / usage frame is not in the fixture as a usage shape;
 *  the fixture's STATE_DELTA carries only /cost_usd. Build a full usage frame
 *  from the SAME wire shape (op/path/value) the fixture demonstrates. */
function usageStateDelta(ops: readonly JSONPatchOp[]): AguiFrame {
  return { type: 'STATE_DELTA', delta: ops };
}

function fold(frames: readonly AguiFrame[]) {
  const state = newAssistantTurn('a1');
  for (const f of frames) reduceFrame(state, f);
  return state;
}

describe('sseAdapter — golden-frame reducer', () => {
  it('imports the captured golden fixture (not synthetic frames)', () => {
    // Sanity: the fixture is the real captured map, keyed by event name. Every
    // shape the reducer tests fold comes from this committed AG-UI golden file.
    expect(frame('TEXT_MESSAGE_START').type).toBe('TEXT_MESSAGE_START');
    expect(frame('STATE_DELTA').type).toBe('STATE_DELTA');
    expect(frame('TOOL_CALL_RESULT').type).toBe('TOOL_CALL_RESULT');
    expect(frame('REASONING_MESSAGE_CONTENT').type).toBe('REASONING_MESSAGE_CONTENT');
  });

  it('TEXT_MESSAGE_START/CONTENT/END → a single text part (no double-print of the final)', () => {
    const state = fold([
      frame('RUN_STARTED'),
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'), // delta "Ciao"
      frame('TEXT_MESSAGE_END'),
      frame('RUN_FINISHED(success)'),
    ]);
    const msg = toThreadMessage(state);
    const textParts = messageParts(msg).filter((p) => p.type === 'text');
    expect(textParts).toHaveLength(1);
    expect(textParts[0]).toMatchObject({ type: 'text', text: 'Ciao' });
    // The translator treats the final Event as END-only; folding the END does
    // not re-append the delta → "Ciao", never "CiaoCiao".
    expect(state.text).toBe('Ciao');
    expect(state.status).toEqual({ type: 'complete', reason: 'stop' });
  });

  it('REASONING_*  → a reasoning part bound to the drawer', () => {
    const state = fold([
      frame('REASONING_START'),
      frame('REASONING_MESSAGE_START'),
      frame('REASONING_MESSAGE_CONTENT'), // delta "thinking…"
      frame('REASONING_MESSAGE_END'),
      frame('REASONING_END'),
    ]);
    expect(state.reasoning).toBe('thinking…');
    const msg = toThreadMessage(state);
    const reasoning = messageParts(msg).filter((p) => p.type === 'reasoning');
    expect(reasoning).toHaveLength(1);
    expect(reasoning[0]).toMatchObject({ type: 'reasoning', text: 'thinking…' });
  });

  it('TOOL_CALL_START/ARGS/END/RESULT → one tool part with the raw result preview', () => {
    const state = fold([
      frame('TOOL_CALL_START'), // web_search, call-1
      frame('TOOL_CALL_ARGS'), // {"query":"meteo
      frame('TOOL_CALL_END'),
      frame('TOOL_CALL_RESULT'), // content "result preview + footer"
    ]);
    const msg = toThreadMessage(state);
    const tools = messageParts(msg).filter((p) => p.type === 'tool-call');
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({
      type: 'tool-call',
      toolCallId: 'call-1',
      toolName: 'web_search',
      argsText: '{"query":"meteo',
      result: 'result preview + footer',
    });
    // The raw result is NEVER folded into the assistant prose.
    expect(state.text).toBe('');
  });

  it('orders completed tool parts before assistant prose', () => {
    const state = fold([
      frame('TOOL_CALL_START'),
      frame('TOOL_CALL_ARGS'),
      frame('TOOL_CALL_END'),
      frame('TOOL_CALL_RESULT'),
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'),
      frame('TEXT_MESSAGE_END'),
    ]);

    expect(messageParts(toThreadMessage(state)).map((p) => p.type)).toEqual(['tool-call', 'text']);
  });

  it('Pitfall 2: a STATE_DELTA carrying tool_call_id → tool part, NEVER a text delta', () => {
    const marker = usageStateDelta([{ op: 'replace', path: '/tool_call_id', value: 'call-9' }]);
    expect(toolCallIdFromDelta(deltaOps(marker))).toBe('call-9');

    const state = fold([
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'), // "Ciao" assistant prose
      marker, // tool-result correlation marker
    ]);
    // The marker created a tool part …
    const msg = toThreadMessage(state);
    const tools = messageParts(msg).filter((p) => p.type === 'tool-call');
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({ type: 'tool-call', toolCallId: 'call-9' });
    // … and did NOT append to the assistant text (still just the real prose).
    expect(state.text).toBe('Ciao');
  });

  it('RUN_FINISHED(interrupt) → requires-action status (inline approval surface)', () => {
    const state = fold([frame('RUN_STARTED'), frame('RUN_FINISHED(interrupt)')]);
    expect(state.status).toEqual({ type: 'requires-action', reason: 'interrupt' });
  });

  it('RUN_ERROR (sanitized) → incomplete status + an error text part', () => {
    const state = fold([frame('RUN_STARTED'), frame('RUN_ERROR')]);
    expect(state.status).toEqual({ type: 'incomplete', reason: 'error' });
    expect(state.error).toBe('LLM 5xx finale');
    const msg = toThreadMessage(state);
    const texts = messageParts(msg).filter((p) => p.type === 'text');
    // empty assistant text part + the error text part
    expect(texts.map((p) => p.text)).toContain('LLM 5xx finale');
  });
});

describe('sseAdapter — usageFromStateDelta', () => {
  it('reads the four usage keys off the JSONPatch ops', () => {
    const usage = usageFromStateDelta([
      { op: 'replace', path: '/prompt_tokens', value: 1200 },
      { op: 'replace', path: '/completion_tokens', value: 340 },
      { op: 'replace', path: '/cache_hit_tokens', value: 1000 },
      { op: 'replace', path: '/cost_usd', value: 0.0042 },
    ]);
    expect(usage).toEqual({
      promptTokens: 1200,
      completionTokens: 340,
      cacheHitTokens: 1000,
      costUsd: 0.0042,
    });
  });

  it('uses the captured /cost_usd shape from the golden STATE_DELTA', () => {
    const goldenDelta = deltaOps(frame('STATE_DELTA'));
    const usage = usageFromStateDelta(goldenDelta);
    expect(usage.costUsd).toBe(0.0042);
    // no token keys present → all zero, never NaN
    expect(usage.promptTokens).toBe(0);
  });

  it('missing cost_usd → costUsd undefined (omit-when-absent)', () => {
    const usage = usageFromStateDelta([{ op: 'replace', path: '/prompt_tokens', value: 10 }]);
    expect(usage.costUsd).toBeUndefined();
    expect('costUsd' in usage).toBe(false);
  });

  it('cacheHitRatio guards /0 (promptTokens=0 → 0, not NaN)', () => {
    expect(cacheHitRatio({ promptTokens: 0, completionTokens: 0, cacheHitTokens: 5 })).toBe(0);
    expect(
      Number.isNaN(cacheHitRatio({ promptTokens: 0, completionTokens: 0, cacheHitTokens: 5 })),
    ).toBe(false);
    expect(
      cacheHitRatio({ promptTokens: 100, completionTokens: 0, cacheHitTokens: 80 }),
    ).toBeCloseTo(0.8);
  });
});

describe('sseAdapter — SSE frame parsing', () => {
  it('parseSSEBlock reads a data: line and trusts the JSON type', () => {
    const block = `event: TEXT_MESSAGE_CONTENT\ndata: ${JSON.stringify(golden.TEXT_MESSAGE_CONTENT)}`;
    const f = parseSSEBlock(block);
    expect(f?.type).toBe('TEXT_MESSAGE_CONTENT');
  });

  it('parseSSEBlock ignores comments / keep-alives / non-data blocks', () => {
    expect(parseSSEBlock(': keep-alive')).toBeNull();
    expect(parseSSEBlock('event: PING')).toBeNull();
    expect(parseSSEBlock('data: {not json')).toBeNull();
    expect(parseSSEBlock('data: 42')).toBeNull(); // no type field
  });

  it('readSSEFrames decodes a chunked SSE byte stream into frames', async () => {
    const enc = new TextEncoder();
    const wire =
      `event: RUN_STARTED\ndata: ${JSON.stringify(golden.RUN_STARTED)}\n\n` +
      `event: TEXT_MESSAGE_CONTENT\ndata: ${JSON.stringify(golden.TEXT_MESSAGE_CONTENT)}\n\n` +
      `event: RUN_FINISHED\ndata: ${JSON.stringify(golden['RUN_FINISHED(success)'])}\n\n`;
    // Split across chunk boundaries to exercise the buffering.
    const mid = Math.floor(wire.length / 2);
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(enc.encode(wire.slice(0, mid)));
        controller.enqueue(enc.encode(wire.slice(mid)));
        controller.close();
      },
    });
    const frames: AguiFrame[] = [];
    for await (const f of readSSEFrames(stream)) frames.push(f);
    expect(frames.map((f) => f.type)).toEqual([
      'RUN_STARTED',
      'TEXT_MESSAGE_CONTENT',
      'RUN_FINISHED',
    ]);
  });

  it('readSSEFrames flushes a trailing block with no final blank-line delimiter', async () => {
    const enc = new TextEncoder();
    const wire = `event: TEXT_MESSAGE_CONTENT\ndata: ${JSON.stringify(golden.TEXT_MESSAGE_CONTENT)}`;
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(enc.encode(wire));
        controller.close();
      },
    });
    const frames: AguiFrame[] = [];
    for await (const f of readSSEFrames(stream)) frames.push(f);
    expect(frames.map((f) => f.type)).toEqual(['TEXT_MESSAGE_CONTENT']);
  });
});

describe('sseAdapter — reducer edge branches', () => {
  it('isUsageDelta distinguishes usage ops from a bare tool_call_id marker', () => {
    expect(isUsageDelta([{ op: 'replace', path: '/completion_tokens', value: 1 }])).toBe(true);
    expect(isUsageDelta([{ op: 'replace', path: '/tool_call_id', value: 'x' }])).toBe(false);
  });

  it('a non-string tool_call_id value is not treated as a marker', () => {
    expect(
      toolCallIdFromDelta([{ op: 'replace', path: '/tool_call_id', value: 42 }]),
    ).toBeUndefined();
  });

  it('TOOL_CALL_ARGS for an unknown call id is a no-op (no phantom part)', () => {
    const state = newAssistantTurn('a1');
    reduceFrame(state, { type: 'TOOL_CALL_ARGS', toolCallId: 'ghost', delta: 'x' });
    expect(state.tools.size).toBe(0);
  });

  it('a TOOL_CALL_RESULT with no prior START still materialises a tool part', () => {
    const state = newAssistantTurn('a1');
    reduceFrame(state, frame('TOOL_CALL_RESULT')); // call-1, content preview
    const tools = messageParts(toThreadMessage(state)).filter((p) => p.type === 'tool-call');
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({ toolCallId: 'call-1', result: 'result preview + footer' });
  });

  it('an empty turn still renders a (single empty) text part', () => {
    const msg = toThreadMessage(newAssistantTurn('a1'));
    expect(msg.content).toEqual([{ type: 'text', text: '' }]);
    expect(msg.role).toBe('assistant');
  });
});

describe('sseAdapter — CUSTOM/aura.display frame (DISP-02)', () => {
  it('attaches the typed payload to the matching tool part by toolCallId', () => {
    // The display frame's value.tool_call_id is "call-1", the same id the
    // TOOL_CALL_START/RESULT golden frames carry — the payload upgrades that part.
    const state = fold([
      frame('TOOL_CALL_START'), // web_search, call-1
      frame('TOOL_CALL_ARGS'),
      frame('TOOL_CALL_END'),
      frame('TOOL_CALL_RESULT'), // call-1, raw preview
      frame('CUSTOM_DISPLAY'), // aura.display for call-1
    ]);
    const part = state.tools.get('call-1');
    if (part === undefined) throw new Error('expected the call-1 tool part');
    // The raw result is preserved AND the typed display is attached.
    expect(part.result).toBe('result preview + footer');
    expect(part.display).toBeDefined();
    expect(part.display?.type).toBe('web_result');
    expect(part.display?.tool_call_id).toBe('call-1');
    expect(part.display?.web_results?.[0]).toMatchObject({ title: 'Forecast' });
    // Exactly one tool part — the display merged onto it, never a phantom part.
    expect(state.tools.size).toBe(1);
  });

  it('creates the tool part on demand when the display arrives before any START', () => {
    // Tolerant like TOOL_CALL_RESULT: a display can land for a call we never saw a
    // START for (snapshot/ordering), and the payload is not lost.
    const state = fold([frame('CUSTOM_DISPLAY')]);
    const part = state.tools.get('call-1');
    if (part === undefined) throw new Error('expected an ensured call-1 tool part');
    expect(part.display?.type).toBe('web_result');
    expect(state.toolOrder).toEqual(['call-1']);
  });

  it('an unrecognized CUSTOM name (aura.artifact) is a no-op — no corruption', () => {
    const state = fold([
      frame('TEXT_MESSAGE_START'),
      frame('TEXT_MESSAGE_CONTENT'), // "Ciao"
      frame('CUSTOM'), // aura.artifact — NOT modelled
    ]);
    // No tool part created, the prose is untouched.
    expect(state.tools.size).toBe(0);
    expect(state.text).toBe('Ciao');
  });

  it('an aura.display frame with a non-DisplayPayload value is a no-op', () => {
    const state = newAssistantTurn('a1');
    reduceFrame(state, { type: 'CUSTOM', name: 'aura.display', value: { no: 'tool_call_id' } });
    expect(state.tools.size).toBe(0);
  });
});

describe('sseAdapter — persisted MESSAGES_SNAPSHOT rehydration', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('projects visible user/assistant history and merges tool result rows', () => {
    const messages = snapshotToThreadMessages({
      type: 'MESSAGES_SNAPSHOT',
      messages: [
        { id: 'msg-1', role: 'system', content: 'hidden system prompt' },
        { id: 'msg-2', role: 'user', content: 'persisted prompt' },
        {
          id: 'msg-3',
          role: 'assistant',
          content: '',
          toolCalls: [
            {
              id: 'call-1',
              type: 'function',
              function: { name: 'web_search', arguments: '{"q":"meteo"}' },
            },
          ],
        },
        { id: 'msg-4', role: 'tool', toolCallId: 'call-1', content: 'sunny 25C' },
        { id: 'msg-5', role: 'assistant', content: 'It is sunny.' },
      ],
    });

    expect(messages.map((message) => message.role)).toEqual(['user', 'assistant', 'assistant']);
    expect(messages[0]).toMatchObject({
      id: 'msg-2',
      role: 'user',
      content: [{ type: 'text', text: 'persisted prompt' }],
      metadata: { custom: { backendSeq: 2 } },
    });

    const toolAssistant = messages[1];
    if (toolAssistant === undefined) throw new Error('expected assistant tool message');
    const toolParts = messageParts(toolAssistant).filter((part) => part.type === 'tool-call');
    expect(toolParts).toHaveLength(1);
    expect(toolParts[0]).toMatchObject({
      toolCallId: 'call-1',
      toolName: 'web_search',
      argsText: '{"q":"meteo"}',
      result: 'sunny 25C',
    });

    const finalAssistant = messages[2];
    if (finalAssistant === undefined) throw new Error('expected final assistant message');
    expect(messageParts(finalAssistant)).toEqual([{ type: 'text', text: 'It is sunny.' }]);
  });

  it('fetchThreadMessages throws the sanitized backend detail on a non-OK status (replay error path)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('conversation not found', { status: 404 }))),
    );
    await expect(fetchThreadMessages('missing-thread')).rejects.toThrow('conversation not found');
  });

  it('fetchThreadMessages falls back to HTTP <status> when the error body is empty', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 500 }))),
    );
    await expect(fetchThreadMessages('t')).rejects.toThrow('HTTP 500');
  });

  it('fetchThreadMessages GETs the snapshot endpoint with same-origin credentials', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            type: 'MESSAGES_SNAPSHOT',
            messages: [{ id: 'msg-1', role: 'user', content: 'ciao' }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    const signal = new AbortController().signal;

    const messages = await fetchThreadMessages('thread/with slash', signal);

    const call = fetchMock.mock.calls[0] as [string, RequestInit] | undefined;
    if (call === undefined) throw new Error('expected fetch call');
    expect(call[0]).toBe('/threads/thread%2Fwith%20slash/messages');
    expect(call[1]).toMatchObject({
      method: 'GET',
      credentials: 'same-origin',
      signal,
    });
    expect(messages).toHaveLength(1);
    expect(messages[0]).toMatchObject({ role: 'user' });
  });
});

function sseResponse(frames: readonly AguiFrame[]): Response {
  const enc = new TextEncoder();
  const wire = frames.map((f) => `event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`).join('');
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

describe('sseAdapter — streamRun (POST /agent/run + AbortController)', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('POSTs same-origin with a JSON body and folds the stream onto one message', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(
        sseResponse([
          frame('RUN_STARTED'),
          frame('TEXT_MESSAGE_START'),
          frame('TEXT_MESSAGE_CONTENT'),
          frame('TEXT_MESSAGE_END'),
          usageStateDelta([
            { op: 'replace', path: '/prompt_tokens', value: 100 },
            { op: 'replace', path: '/cost_usd', value: 0.0042 },
          ]),
          frame('RUN_FINISHED(success)'),
        ]),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const updates: ThreadMessageLike[] = [];
    const ctrl = new AbortController();
    const usage = await streamRun({
      threadId: 'thread-1',
      userText: 'ciao',
      signal: ctrl.signal,
      newId: () => 'fixed-id',
      onUpdate: (m) => updates.push(m),
    });

    // Request shape: POST /agent/run, JSON body, same-origin, carries the signal.
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const call = fetchMock.mock.calls[0];
    expect(call).toBeDefined();
    const [url, init] = call as unknown as [string, RequestInit];
    expect(url).toBe('/agent/run');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    // The AG-UI RunAgentInput wire shape: threadId + messages[] (the gateway
    // drives the turn off the last user message — internal/agui/server.go).
    expect(JSON.parse(init.body as string)).toEqual({
      threadId: 'thread-1',
      messages: [{ id: 'fixed-id', role: 'user', content: 'ciao' }],
    });
    expect(init.signal).toBe(ctrl.signal);

    // Final message: a single text part with the streamed answer; usage parsed.
    const last = updates.at(-1);
    if (last === undefined) throw new Error('expected at least one onUpdate call');
    expect(last.id).toBe('fixed-id');
    expect(last.status).toEqual({ type: 'complete', reason: 'stop' });
    const texts = messageParts(last).filter((p) => p.type === 'text');
    expect(texts).toHaveLength(1);
    expect(texts[0]).toMatchObject({ text: 'Ciao' });
    expect(usage).toMatchObject({ promptTokens: 100, costUsd: 0.0042 });
  });

  it('a non-OK response surfaces the sanitized backend body as the error (WR-03)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(new Response('thread already has an in-flight run', { status: 409 })),
      ),
    );
    const updates: ThreadMessageLike[] = [];
    await streamRun({
      threadId: 't',
      userText: 'x',
      signal: new AbortController().signal,
      newId: () => 'id',
      onUpdate: (m) => updates.push(m),
    });
    const last = updates.at(-1);
    expect(last?.status).toEqual({ type: 'incomplete', reason: 'error' });
    const texts = last
      ? messageParts(last).flatMap((p) => (p.type === 'text' ? [p.text] : []))
      : [];
    // The operator sees the backend's reason (409 conflict), not a bare "HTTP 409".
    expect(texts).toContain('thread already has an in-flight run');
  });

  it('a non-OK response with an empty body falls back to HTTP <status> (WR-03)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 502 }))),
    );
    const updates: ThreadMessageLike[] = [];
    await streamRun({
      threadId: 't',
      userText: 'x',
      signal: new AbortController().signal,
      newId: () => 'id',
      onUpdate: (m) => updates.push(m),
    });
    const last = updates.at(-1);
    const texts = last
      ? messageParts(last).flatMap((p) => (p.type === 'text' ? [p.text] : []))
      : [];
    expect(texts).toContain('HTTP 502');
  });
});
