import type { ThreadMessageLike } from '@assistant-ui/react';
import { describe, expect, it } from 'vitest';
import { backendSeqAt, compactionAnchorId } from '../messageSeq';

// One mapping, two callers: a branch edit forks at a seq and the compaction marker is drawn
// at one. An off-by-one between them would put the marker a turn away from the history it
// describes — the kind of wrong nobody reports.

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
});

describe('compactionAnchorId', () => {
  const thread = [persisted(1), persisted(2), persisted(3), persisted(4)];

  it('anchors on the last message the summary speaks for', () => {
    expect(compactionAnchorId(thread, 2)).toBe('msg-2');
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
