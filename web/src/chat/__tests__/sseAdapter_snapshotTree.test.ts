import { describe, expect, it } from 'vitest';
import type { ThreadMessageLike } from '@assistant-ui/react';
import { snapshotToThreadMessages, snapshotToThreadTree } from '../sseAdapter_snapshot';

// The history snapshot is the conversation tree (spec 2026-10-27, D5): each message names the
// one it continues. The cockpit draws fewer messages than the snapshot carries, because a tool
// result folds into its card and a worker report is not drawn, so every row maps to the
// message it shows as, and a child hangs from that.

function snapshot(messages: readonly Record<string, unknown>[]): unknown {
  return { type: 'MESSAGES_SNAPSHOT', messages };
}

function custom(message: ThreadMessageLike | undefined): Record<string, unknown> | undefined {
  return message?.metadata?.custom;
}

function tree(messages: readonly Record<string, unknown>[]) {
  const projected = snapshotToThreadTree(snapshot(messages));
  return {
    ...projected,
    edges: projected.items.map(({ message, parentId }) => [message.id, parentId]),
    byId: new Map(projected.items.map(({ message }) => [message.id, message])),
  };
}

const searchCall = {
  id: 'call-1',
  type: 'function',
  function: { name: 'web_search', arguments: '{"query":"rome"}' },
};

describe('snapshotToThreadTree', () => {
  it('keeps each branch under the message it continues and opens on the branch written last', () => {
    const got = tree([
      { id: 'msg-1', parentId: null, role: 'user', content: 'U1' },
      { id: 'msg-2', parentId: 'msg-1', role: 'assistant', content: 'A1' },
      { id: 'msg-3', parentId: null, role: 'user', content: 'U1 rewritten' },
      { id: 'msg-4', parentId: 'msg-3', role: 'assistant', content: 'A1 to the rewrite' },
    ]);

    expect(got.edges).toEqual([
      ['msg-1', null],
      ['msg-2', 'msg-1'],
      ['msg-3', null],
      ['msg-4', 'msg-3'],
    ]);
    expect(got.headId).toBe('msg-4');
  });

  it('folds a tool result into its card and hangs the answer after it from the card', () => {
    const got = tree([
      { id: 'msg-1', parentId: null, role: 'user', content: 'weather?' },
      { id: 'msg-2', parentId: 'msg-1', role: 'assistant', toolCalls: [searchCall] },
      { id: 'msg-3', parentId: 'msg-2', role: 'tool', toolCallId: 'call-1', content: 'sunny' },
      { id: 'msg-4', parentId: 'msg-3', role: 'assistant', content: 'It is sunny.' },
    ]);

    expect(got.edges).toEqual([
      ['msg-1', null],
      ['msg-2', 'msg-1'],
      ['msg-4', 'msg-2'],
    ]);
    const card = got.byId.get('msg-2');
    expect(card?.content).toEqual([
      expect.objectContaining({ type: 'tool-call', toolCallId: 'call-1', result: 'sunny' }),
    ]);
    expect(custom(card)).toEqual({ backendSeq: 2, leafSeq: 3 });
    expect(custom(got.byId.get('msg-4'))).toEqual({ backendSeq: 4, leafSeq: 4 });
  });

  it('does not draw a worker report, and the turn after it continues the report', () => {
    const got = tree([
      { id: 'msg-1', parentId: null, role: 'user', content: 'run the task' },
      { id: 'msg-2', parentId: 'msg-1', role: 'assistant', content: 'launched' },
      {
        id: 'msg-3',
        parentId: 'msg-2',
        role: 'assistant',
        content: 'internal report',
        workerReport: true,
      },
      { id: 'msg-4', parentId: 'msg-3', role: 'assistant', content: 'consolidated' },
    ]);

    expect(got.edges).toEqual([
      ['msg-1', null],
      ['msg-2', 'msg-1'],
      ['msg-4', 'msg-2'],
    ]);
    // A send from "launched" continues the report, not a sibling of it: the model keeps it.
    expect(custom(got.byId.get('msg-2'))).toEqual({ backendSeq: 2, leafSeq: 3 });
  });

  it('opens on the message the last turn shows as when that turn is not drawn', () => {
    const got = tree([
      { id: 'msg-1', parentId: null, role: 'user', content: 'run the task' },
      { id: 'msg-2', parentId: 'msg-1', role: 'assistant', content: 'launched' },
      { id: 'msg-3', parentId: 'msg-2', role: 'assistant', content: 'report', workerReport: true },
    ]);

    expect(got.headId).toBe('msg-2');
  });

  it('hangs a result recovery synthesized from its card, with no seq of its own', () => {
    const got = tree([
      { id: 'msg-1', parentId: null, role: 'user', content: 'compute' },
      { id: 'msg-2', parentId: 'msg-1', role: 'assistant', toolCalls: [searchCall] },
      {
        id: 'msg-2-call-1',
        parentId: 'msg-2',
        role: 'tool',
        toolCallId: 'call-1',
        content: 'error: previous result unknown',
        isError: true,
      },
      { id: 'msg-3', parentId: 'msg-2', role: 'user', content: 'and now?' },
    ]);

    expect(got.edges).toEqual([
      ['msg-1', null],
      ['msg-2', 'msg-1'],
      ['msg-3', 'msg-2'],
    ]);
    expect(custom(got.byId.get('msg-2'))).toEqual({ backendSeq: 2, leafSeq: 2 });
  });

  it('draws a result whose card is not its parent as a card of its own', () => {
    const got = tree([
      { id: 'msg-1', parentId: null, role: 'user', content: 'U1' },
      { id: 'msg-2', parentId: 'msg-1', role: 'tool', toolCallId: 'stray', content: 'orphan' },
    ]);

    expect(got.edges).toEqual([
      ['msg-1', null],
      ['msg-2', 'msg-1'],
    ]);
    expect(got.byId.get('msg-2')?.content).toEqual([
      expect.objectContaining({ type: 'tool-call', toolCallId: 'stray', result: 'orphan' }),
    ]);
  });

  // A snapshot without parentId is the shape every server sent before branches were drawn:
  // each message continues the one before it.
  it('reads a snapshot without parentId as one branch', () => {
    const got = tree([
      { id: 'msg-1', role: 'system', content: 'you are aura' },
      { id: 'msg-2', role: 'user', content: 'ciao' },
      { id: 'msg-3', role: 'assistant', content: 'salve' },
    ]);

    expect(got.edges).toEqual([
      ['msg-2', null],
      ['msg-3', 'msg-2'],
    ]);
    expect(got.headId).toBe('msg-3');
  });

  it('hangs a message whose parent the snapshot does not carry from the one before it', () => {
    const got = tree([
      { id: 'msg-1', parentId: null, role: 'user', content: 'U1' },
      { id: 'msg-2', parentId: 'msg-99', role: 'assistant', content: 'A1' },
    ]);

    expect(got.edges).toEqual([
      ['msg-1', null],
      ['msg-2', 'msg-1'],
    ]);
  });

  it('is empty for a body that is not a snapshot', () => {
    expect(snapshotToThreadTree({ type: 'RUN_STARTED' })).toEqual({ items: [], headId: null });
  });
});

describe('snapshotToThreadMessages', () => {
  it('is the branch the thread opens on', () => {
    const visible = snapshotToThreadMessages(
      snapshot([
        { id: 'msg-1', parentId: null, role: 'user', content: 'U1' },
        { id: 'msg-2', parentId: 'msg-1', role: 'assistant', content: 'A1' },
        { id: 'msg-3', parentId: null, role: 'user', content: 'U1 rewritten' },
        { id: 'msg-4', parentId: 'msg-3', role: 'assistant', content: 'A1 to the rewrite' },
      ]),
    );

    expect(visible.map((message) => message.id)).toEqual(['msg-3', 'msg-4']);
  });
});
