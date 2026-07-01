import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { fetchThreadMessages, snapshotToThreadMessages } from '../sseAdapter_snapshot';

// sseAdapter_snapshot tests — persisted-history rehydration (GET /threads/{id}/messages).
// Pure projection of a MESSAGES_SNAPSHOT body onto the assistant-ui chat model: role
// handling (system dropped, tool results merged), tool-call shaping, metadata seq, and
// the defensive content coercion. fetchThreadMessages wraps it with the HTTP read.

function snap(messages: unknown[]): unknown {
  return { type: 'MESSAGES_SNAPSHOT', messages };
}

function partsOf(message: ThreadMessageLike) {
  return typeof message.content === 'string' ? [] : message.content;
}

function messageAt(messages: readonly ThreadMessageLike[], index: number): ThreadMessageLike {
  const message = messages[index];
  if (message === undefined) {
    throw new Error(`missing message at index ${String(index)}`);
  }
  return message;
}

describe('snapshotToThreadMessages — guard cases', () => {
  it('returns [] for non-object / null / wrong-type / non-array messages', () => {
    expect(snapshotToThreadMessages(null)).toEqual([]);
    expect(snapshotToThreadMessages('nope')).toEqual([]);
    expect(snapshotToThreadMessages({ type: 'OTHER', messages: [] })).toEqual([]);
    expect(snapshotToThreadMessages({ type: 'MESSAGES_SNAPSHOT', messages: 'x' })).toEqual([]);
  });

  it('drops non-object message entries and system turns', () => {
    const out = snapshotToThreadMessages(
      snap([null, 'str', { role: 'system', content: 'hidden' }, { role: 'user', content: 'hi' }]),
    );
    expect(out).toHaveLength(1);
    expect(out[0]?.role).toBe('user');
  });
});

describe('snapshotToThreadMessages — roles', () => {
  it('projects a user turn with a text part and seq metadata from msg-<n> id', () => {
    const out = snapshotToThreadMessages(snap([{ id: 'msg-7', role: 'user', content: 'ciao' }]));
    expect(out[0]).toMatchObject({
      id: 'msg-7',
      role: 'user',
      content: [{ type: 'text', text: 'ciao' }],
      metadata: { custom: { backendSeq: 7 } },
    });
  });

  it('strips Aura model context envelopes from legacy user snapshots', () => {
    const visible = 'Nel documento Corso Base Robot, quali tipi di robot sono elencati?';
    const out = snapshotToThreadMessages(
      snap([
        {
          id: 'msg-1',
          role: 'user',
          content:
            '<knowledge_base trust="operator_pinned_context">\n' +
            'These documents the user uploaded earlier are indexed.\n' +
            '- [1] document_id=doc-1 filename=Corso Base Robot.docx\n' +
            '</knowledge_base>\n\nUser message:\n' +
            visible,
        },
      ]),
    );

    expect(partsOf(messageAt(out, 0))).toEqual([{ type: 'text', text: visible }]);
  });

  it('omits id + metadata when the id is absent or not the msg-<n> shape', () => {
    const out = snapshotToThreadMessages(snap([{ id: 'weird', role: 'user', content: 'x' }]));
    expect(out[0]?.id).toBe('weird');
    expect(out[0]?.metadata).toBeUndefined();
    const out2 = snapshotToThreadMessages(snap([{ role: 'user', content: 'x' }]));
    expect(out2[0]?.id).toBeUndefined();
  });

  it('projects an assistant text-only turn as complete', () => {
    const out = snapshotToThreadMessages(snap([{ role: 'assistant', content: 'done' }]));
    expect(out[0]).toMatchObject({
      role: 'assistant',
      status: { type: 'complete', reason: 'stop' },
    });
    expect(partsOf(messageAt(out, 0))).toEqual([{ type: 'text', text: 'done' }]);
  });

  it('an assistant turn with no text and no tool calls still gets one empty text part', () => {
    const out = snapshotToThreadMessages(snap([{ role: 'assistant', content: '' }]));
    expect(partsOf(messageAt(out, 0))).toEqual([{ type: 'text', text: '' }]);
  });

  it('ignores an unknown role entirely', () => {
    expect(snapshotToThreadMessages(snap([{ role: 'moderator', content: 'x' }]))).toEqual([]);
  });
});

describe('snapshotToThreadMessages — tool calls + results', () => {
  it('shapes assistant tool calls and tolerates a missing display / non-array toolCalls', () => {
    const out = snapshotToThreadMessages(
      snap([
        {
          role: 'assistant',
          content: '',
          toolCalls: [
            { id: 'call-1', function: { name: 'web_search', arguments: '{"q":"x"}' } },
            { id: '', function: { name: 'skip' } }, // empty id → dropped
            'not-an-object', // → skipped
            { function: { name: 'no-id' } }, // missing id → dropped
          ],
        },
      ]),
    );
    const parts = partsOf(messageAt(out, 0));
    expect(parts).toHaveLength(1);
    expect(parts[0]).toMatchObject({
      type: 'tool-call',
      toolCallId: 'call-1',
      toolName: 'web_search',
      argsText: '{"q":"x"}',
    });
  });

  it('coerces non-string tool arguments and defaults an absent name to empty', () => {
    const out = snapshotToThreadMessages(
      snap([
        {
          role: 'assistant',
          content: '',
          toolCalls: [{ id: 'call-2', function: { arguments: { a: 1 } } }],
        },
      ]),
    );
    const part = partsOf(messageAt(out, 0))[0];
    expect(part).toMatchObject({ toolName: '', argsText: '{"a":1}' });
  });

  it('merges a tool result row into the preceding assistant tool-call card', () => {
    const out = snapshotToThreadMessages(
      snap([
        {
          role: 'assistant',
          content: '',
          toolCalls: [{ id: 'call-3', function: { name: 'calc', arguments: '{}' } }],
        },
        { role: 'tool', toolCallId: 'call-3', content: '42' },
      ]),
    );
    expect(out).toHaveLength(1);
    expect(partsOf(messageAt(out, 0))[0]).toMatchObject({ toolCallId: 'call-3', result: '42' });
  });

  it('synthesizes an assistant card for an orphan tool result (no matching call)', () => {
    const out = snapshotToThreadMessages(
      snap([{ role: 'tool', toolCallId: 'orphan', content: 'r' }]),
    );
    expect(out).toHaveLength(1);
    expect(out[0]?.role).toBe('assistant');
    expect(partsOf(messageAt(out, 0))[0]).toMatchObject({
      toolCallId: 'orphan',
      result: 'r',
      toolName: '',
    });
  });

  it('drops a tool row that carries no toolCallId', () => {
    expect(snapshotToThreadMessages(snap([{ role: 'tool', content: 'r' }]))).toEqual([]);
  });
});

describe('snapshotToThreadMessages — content coercion', () => {
  it('coerces null/number/boolean/object content, and empties an unstringifiable value', () => {
    const circular: Record<string, unknown> = {};
    circular.self = circular;
    const out = snapshotToThreadMessages(
      snap([
        { role: 'user', content: null },
        { role: 'user', content: 5 },
        { role: 'user', content: true },
        { role: 'user', content: { k: 'v' } },
        { role: 'user', content: circular },
      ]),
    );
    expect(partsOf(messageAt(out, 0))).toEqual([{ type: 'text', text: '' }]);
    expect(partsOf(messageAt(out, 1))).toEqual([{ type: 'text', text: '5' }]);
    expect(partsOf(messageAt(out, 2))).toEqual([{ type: 'text', text: 'true' }]);
    expect(partsOf(messageAt(out, 3))).toEqual([{ type: 'text', text: '{"k":"v"}' }]);
    expect(partsOf(messageAt(out, 4))).toEqual([{ type: 'text', text: '' }]);
  });
});

describe('fetchThreadMessages', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('GETs the snapshot and projects it', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(JSON.stringify(snap([{ role: 'user', content: 'hi' }])), { status: 200 }),
        ),
      ),
    );
    const out = await fetchThreadMessages('c-1');
    expect(out[0]?.role).toBe('user');
  });

  it('throws on a non-ok response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('nope', { status: 500 }))),
    );
    await expect(fetchThreadMessages('c-1')).rejects.toThrow();
  });
});
