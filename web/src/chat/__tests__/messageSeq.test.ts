import type { ThreadMessageLike } from '@assistant-ui/react';
import { describe, expect, it } from 'vitest';
import { backendSeqAt, compactionAnchorId, messageIdAtSeq } from '../messageSeq';

// One mapping, three callers: a branch edit forks at a seq, the compaction marker is drawn at
// one and a search hit opens the thread at one. An off-by-one between them would put the
// marker a turn away from the history it describes — the kind of wrong nobody reports.

function persisted(seq: number): ThreadMessageLike {
  return {
    id: `msg-${String(seq)}`,
    role: seq % 2 === 1 ? 'user' : 'assistant',
    content: [{ type: 'text', text: `turn ${String(seq)}` }],
    metadata: { custom: { backendSeq: seq } },
  };
}

function fresh(id: string): ThreadMessageLike {
  return { id, role: 'user', content: [{ type: 'text', text: 'just typed' }] };
}

describe('backendSeqAt', () => {
  it('reads the persisted seq a rehydrated snapshot carries', () => {
    expect(backendSeqAt([persisted(4), persisted(5)], 1)).toBe(5);
  });

  // Fresh in-memory turns have no persisted seq; a normal Aura conversation starts at the
  // first user turn, so the visible position is the honest stand-in.
  it('falls back to the visible position for a turn that is not persisted yet', () => {
    expect(backendSeqAt([fresh('a'), fresh('b')], 1)).toBe(2);
  });

  it('is 0 before the first message', () => {
    expect(backendSeqAt([persisted(1)], -1)).toBe(0);
  });

  it('falls back to the visible position for metadata without a custom block', () => {
    expect(backendSeqAt([{ ...fresh('a'), metadata: {} }], 0)).toBe(1);
  });

  // A seq is a positive integer the backend wrote; anything else is not one.
  it.each([['7'], [Number.NaN], [Number.POSITIVE_INFINITY], [0], [-3]])(
    'falls back to the visible position for a backendSeq of %s',
    (backendSeq) => {
      expect(backendSeqAt([{ ...fresh('a'), metadata: { custom: { backendSeq } } }], 0)).toBe(1);
    },
  );
});

describe('compactionAnchorId', () => {
  const thread = [persisted(1), persisted(2), persisted(3), persisted(4)];

  it('anchors on the last message the summary speaks for', () => {
    expect(compactionAnchorId(thread, 2)).toBe('msg-2');
  });

  it('anchors on the first message when the summary speaks for it alone', () => {
    expect(compactionAnchorId(thread, 1)).toBe('msg-1');
  });

  it('anchors on the last message when the watermark covers everything', () => {
    expect(compactionAnchorId(thread, 99)).toBe('msg-4');
  });

  it('draws no marker for an uncompacted thread', () => {
    expect(compactionAnchorId(thread, 0)).toBeUndefined();
  });

  // A window that starts past the watermark draws nothing rather than a marker at the top
  // claiming to describe turns that are not on screen.
  it('draws no marker when the watermark falls before every visible message', () => {
    expect(compactionAnchorId([persisted(8), persisted(9)], 4)).toBeUndefined();
  });

  it('draws no marker for an empty thread', () => {
    expect(compactionAnchorId([], 4)).toBeUndefined();
  });
});

describe('messageIdAtSeq', () => {
  it('finds the visible message behind a persisted seq', () => {
    expect(messageIdAtSeq([persisted(1), persisted(2), persisted(3)], 2)).toBe('msg-2');
  });

  // A tool result folds into its assistant card, so its seq has no message of its own.
  it('finds nothing for a seq no visible message carries', () => {
    expect(messageIdAtSeq([persisted(1), persisted(2), persisted(4)], 3)).toBeUndefined();
  });

  it('agrees with backendSeqAt for a turn that is not persisted yet', () => {
    expect(messageIdAtSeq([persisted(1), fresh('typed')], 2)).toBe('typed');
  });

  it('finds nothing in an empty thread', () => {
    expect(messageIdAtSeq([], 1)).toBeUndefined();
  });
});
