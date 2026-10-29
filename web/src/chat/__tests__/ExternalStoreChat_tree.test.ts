import { describe, expect, it } from 'vitest';
import {
  getExternalStoreMessages,
  type ExportedMessageRepository,
  type ThreadMessageLike,
} from '@assistant-ui/react';
import {
  hostMessages,
  importThreadTree,
  mapTreeMessages,
  placeTurn,
} from '../ExternalStoreChat_tree';
import type { ThreadTree } from '../sseAdapter_snapshot';

function text(id: string, role: 'user' | 'assistant', value: string): ThreadMessageLike {
  return { id, role, content: [{ type: 'text', text: value }] };
}

function custom(message: ThreadMessageLike | undefined): Record<string, unknown> | undefined {
  return message?.metadata?.custom;
}

// A worker report written last shows as its parent, so the head is not the last message drawn:
// the import must name it rather than fall back to the end of the list.
const treeOpeningOnAnEarlierBranch: ThreadTree = {
  headId: 'msg-2',
  items: [
    { message: text('msg-1', 'user', 'U1'), parentId: null },
    { message: text('msg-2', 'assistant', 'A1'), parentId: 'msg-1' },
    { message: text('msg-3', 'user', 'U1 rewritten'), parentId: null },
    { message: text('msg-4', 'assistant', 'A1 to the rewrite'), parentId: 'msg-3' },
  ],
};

function imported(tree: ThreadTree): ExportedMessageRepository {
  let got: ExportedMessageRepository | undefined;
  importThreadTree({ import: (repository) => (got = repository) }, tree);
  if (got === undefined) throw new Error('nothing was imported');
  return got;
}

describe('importThreadTree', () => {
  it('opens on the head the tree names, not on its last message', () => {
    expect(imported(treeOpeningOnAnEarlierBranch).headId).toBe('msg-2');
  });

  it('binds every message to the host object it came from', () => {
    const repository = imported(treeOpeningOnAnEarlierBranch);
    const hosts = repository.messages.map(({ message }) => getExternalStoreMessages(message)[0]);
    expect(hosts).toEqual(treeOpeningOnAnEarlierBranch.items.map(({ message }) => message));
    expect(hostMessages(repository.messages.map(({ message }) => message))).toHaveLength(4);
  });

  it('keeps each parent', () => {
    expect(imported(treeOpeningOnAnEarlierBranch).messages.map(({ parentId }) => parentId)).toEqual(
      [null, 'msg-1', null, 'msg-3'],
    );
  });
});

describe('mapTreeMessages', () => {
  it('folds every message and keeps the head and the parents', () => {
    const mapped = mapTreeMessages(treeOpeningOnAnEarlierBranch, (all) =>
      all.map((message) => ({ ...message, metadata: { custom: { folded: true } } })),
    );
    expect(mapped.headId).toBe('msg-2');
    expect(mapped.items.map(({ parentId }) => parentId)).toEqual([null, 'msg-1', null, 'msg-3']);
    expect(mapped.items.every(({ message }) => custom(message)?.folded === true)).toBe(true);
  });

  it('keeps a message the fold did not return', () => {
    const mapped = mapTreeMessages(treeOpeningOnAnEarlierBranch, () => []);
    expect(mapped.items.map(({ message }) => message.id)).toEqual([
      'msg-1',
      'msg-2',
      'msg-3',
      'msg-4',
    ]);
  });
});

describe('placeTurn', () => {
  const asked = text('client-user', 'user', 'next question');
  const answer = (metadata: Record<string, unknown>): ThreadMessageLike => ({
    ...text('client-answer', 'assistant', 'the answer'),
    metadata: { custom: metadata },
  });

  it('gives a sent user message the head and the answer the turn after it', () => {
    const next = placeTurn([asked], 1, answer({ headSeq: 5, leafSeq: 6 }), true);
    expect(custom(next[0])).toEqual({ backendSeq: 5, leafSeq: 5 });
    expect(custom(next[1])).toEqual({ headSeq: 5, leafSeq: 6, backendSeq: 6 });
  });

  it('replaces the answer in place as the stream grows', () => {
    const streaming = placeTurn([asked], 1, answer({}), true);
    const next = placeTurn(streaming, 1, answer({ headSeq: 5 }), true);
    expect(next).toHaveLength(2);
    expect(custom(next[1])).toMatchObject({ backendSeq: 6 });
    expect(custom(next[0])).toMatchObject({ backendSeq: 5 });
  });

  it('leaves the user message alone when the run was not given one (a regenerate)', () => {
    const next = placeTurn([asked], 1, answer({ headSeq: 9 }), false);
    expect(next[0]).toBe(asked);
    expect(custom(next[1])).toMatchObject({ backendSeq: 10 });
  });

  it('stamps only a user message', () => {
    const before = text('msg-7', 'assistant', 'a card');
    const next = placeTurn([before], 1, answer({ headSeq: 7 }), true);
    expect(next[0]).toBe(before);
  });

  it('keeps what the user message already carried', () => {
    const withFile: ThreadMessageLike = { ...asked, metadata: { custom: { attachments: ['a'] } } };
    const next = placeTurn([withFile], 1, answer({ headSeq: 5 }), true);
    expect(custom(next[0])).toEqual({ attachments: ['a'], backendSeq: 5, leafSeq: 5 });
  });

  it('stamps nothing before a first message', () => {
    const next = placeTurn([], 0, answer({ headSeq: 1 }), true);
    expect(next.map((message) => custom(message)?.backendSeq)).toEqual([2]);
  });

  it('places a message whose run reported no head as it is', () => {
    for (const metadata of [{}, { headSeq: '5' }]) {
      const placed = answer(metadata);
      const next = placeTurn([asked], 1, placed, true);
      expect(next).toEqual([asked, placed]);
    }
  });
});
