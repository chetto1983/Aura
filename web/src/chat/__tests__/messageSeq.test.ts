import type { ThreadMessageLike } from '@assistant-ui/react';
import { describe, expect, it } from 'vitest';
import {
  backendSeqAt,
  compactionAnchorId,
  leafSeqOf,
  messageIdAtSeq,
  turnSeqsFromDelta,
} from '../messageSeq';

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

  // Spec 2026-10-27, §4.4: a turn whose seq is not known has none. The visible position the
  // cockpit used to guess named another turn as soon as the thread had branches.
  it('knows no seq for a turn that is not persisted yet', () => {
    expect(backendSeqAt([fresh('a'), fresh('b')], 1)).toBeUndefined();
  });

  it('knows no seq before the first message', () => {
    expect(backendSeqAt([persisted(1)], -1)).toBeUndefined();
  });

  it('knows no seq for metadata without a custom block', () => {
    expect(backendSeqAt([{ ...fresh('a'), metadata: {} }], 0)).toBeUndefined();
  });

  // A seq is a positive integer the backend wrote; anything else is not one.
  it.each([['7'], [Number.NaN], [Number.POSITIVE_INFINITY], [0], [-3], [2.5]])(
    'knows no seq for a backendSeq of %s',
    (backendSeq) => {
      expect(
        backendSeqAt([{ ...fresh('a'), metadata: { custom: { backendSeq } } }], 0),
      ).toBeUndefined();
    },
  );
});

describe('leafSeqOf', () => {
  it('is the last turn folded into the message', () => {
    expect(
      leafSeqOf({ ...persisted(2), metadata: { custom: { backendSeq: 2, leafSeq: 5 } } }),
    ).toBe(5);
  });

  it('falls back to the message seq when no later turn folded into it', () => {
    expect(leafSeqOf(persisted(4))).toBe(4);
  });

  it('is unknown for a fresh turn and for no message at all', () => {
    expect(leafSeqOf(fresh('a'))).toBeUndefined();
    expect(leafSeqOf(undefined)).toBeUndefined();
  });
});

describe('turnSeqsFromDelta', () => {
  it('reads the head a run starts from and the leaf it ends on', () => {
    expect(
      turnSeqsFromDelta([
        { op: 'replace', path: '/leaf_seq', value: 9 },
        { op: 'replace', path: '/head_seq', value: 7 },
      ]),
    ).toEqual({ headSeq: 7, leafSeq: 9 });
  });

  it('ignores a delta that carries neither, and a value that is not a seq', () => {
    expect(turnSeqsFromDelta([{ op: 'replace', path: '/prompt_tokens', value: 3 }])).toEqual({});
    expect(turnSeqsFromDelta([{ op: 'replace', path: '/head_seq', value: '7' }])).toEqual({});
  });
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

  it('finds nothing behind a turn that is not persisted yet', () => {
    expect(messageIdAtSeq([persisted(1), fresh('typed')], 2)).toBeUndefined();
  });

  it('finds nothing in an empty thread', () => {
    expect(messageIdAtSeq([], 1)).toBeUndefined();
  });
});
