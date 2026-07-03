import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
// Network-boundary half of the sseAdapter suite (split from sseAdapter.test.ts to
// keep each file within the 600-LOC cap): the persisted MESSAGES_SNAPSHOT
// rehydration and the streamRun POST /agent/run + AbortController paths. The pure
// reducer / frame-parsing tests stay in sseAdapter.test.ts. Both halves are driven
// by the SAME captured golden fixture (internal/agui/testdata/golden-events.json).
import goldenEvents from '../../../../internal/agui/testdata/golden-events.json';
import {
  fetchThreadMessages,
  messageParts,
  snapshotToThreadMessages,
  streamRun,
  type AguiFrame,
  type JSONPatchOp,
} from '../sseAdapter';

const golden = goldenEvents as Record<string, AguiFrame>;

function frame(name: string): AguiFrame {
  const f = golden[name];
  if (f === undefined) throw new Error(`golden fixture missing "${name}"`);
  return f;
}

/** Build a full usage STATE_DELTA from the SAME wire shape (op/path/value) the
 *  fixture demonstrates (the fixture's STATE_DELTA carries only /cost_usd). */
function usageStateDelta(ops: readonly JSONPatchOp[]): AguiFrame {
  return { type: 'STATE_DELTA', delta: ops };
}

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

  it('includes attachment ids in the /agent/run aura extension body', async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve(sseResponse([frame('RUN_STARTED'), frame('RUN_FINISHED(success)')])),
    );
    vi.stubGlobal('fetch', fetchMock);

    await streamRun({
      threadId: 'thread-1',
      userText: 'summarize these',
      attachmentIds: ['asset-1', 'asset-2'],
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
    });

    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toMatchObject({
      aura: { attachment_ids: ['asset-1', 'asset-2'] },
    });
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
