import { describe, expect, it } from 'vitest';
import {
  clipAt,
  clipStarts,
  emptyProject,
  overlayWindow,
  projectDuration,
  sourceOf,
  type VideoProject,
} from '../project';

function project(): VideoProject {
  return {
    ...emptyProject('demo', { width: 1920, height: 1080 }, 30),
    sources: [
      {
        id: 'src-a',
        assetId: 'asset-a',
        kind: 'video',
        duration: 8,
        size: { width: 1920, height: 1080 },
      },
      {
        id: 'src-b',
        assetId: 'asset-b',
        kind: 'image',
        duration: 0,
        size: { width: 800, height: 600 },
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 3, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 2, sourceStart: 4, muted: true },
    ],
    overlays: [
      {
        id: 'lane-1',
        items: [
          {
            id: 'title',
            kind: 'text',
            anchor: { clipId: 'clip-2', offset: 0.5 },
            duration: 1,
            props: { text: 'ciao' },
          },
          {
            id: 'logo',
            kind: 'image',
            anchor: { clipId: 'clip-1', offset: 0 },
            duration: 1,
            props: { assetId: 'asset-b' },
          },
        ],
      },
    ],
  };
}

describe('the video lane is a sequence', () => {
  it('gives every clip the start its predecessors leave it', () => {
    expect(clipStarts(project())).toEqual([0, 3]);
  });

  it('is as long as its clips together', () => {
    expect(projectDuration(project())).toBe(5);
  });

  it('answers which clip covers an instant, and which does not', () => {
    expect(clipAt(project(), 2.9)?.id).toBe('clip-1');
    expect(clipAt(project(), 3)?.id).toBe('clip-2');
    expect(clipAt(project(), 5)).toBeUndefined();
    expect(clipAt(project(), -1)).toBeUndefined();
  });
});

describe('an overlay follows its clip', () => {
  it('resolves against the clip it is anchored to', () => {
    expect(overlayWindow(project(), { clipId: 'clip-2', offset: 0.5 }, 1)).toEqual({
      start: 3.5,
      end: 4.5,
    });
  });

  it('is clipped to its clip rather than spilling past it', () => {
    expect(overlayWindow(project(), { clipId: 'clip-2', offset: 1.5 }, 3)).toEqual({
      start: 4.5,
      end: 5,
    });
  });

  it('collapses to nothing when the clip it hung on is gone', () => {
    expect(overlayWindow(project(), { clipId: 'clip-gone', offset: 0.5 }, 1)).toEqual({
      start: 0,
      end: 0,
    });
  });

  it('is an empty window at the clip boundary, never an inverted one', () => {
    expect(overlayWindow(project(), { clipId: 'clip-2', offset: 2 }, 1)).toEqual({
      start: 5,
      end: 5,
    });
  });

  it('stays inside the clip when the offset is past its end', () => {
    expect(overlayWindow(project(), { clipId: 'clip-2', offset: 3 }, 1)).toEqual({
      start: 5,
      end: 5,
    });
  });

  it('never ends before it starts, whatever duration it is given', () => {
    expect(overlayWindow(project(), { clipId: 'clip-2', offset: 0.5 }, -1)).toEqual({
      start: 3.5,
      end: 3.5,
    });
  });
});

describe('an overlay item declares what it is', () => {
  it('keeps its kind, and each kind resolves against its own clip', () => {
    const items = project().overlays.flatMap((lane) => lane.items);
    expect(items.map((item) => item.kind)).toEqual(['text', 'image']);
    expect(items.map((item) => overlayWindow(project(), item.anchor, item.duration))).toEqual([
      { start: 3.5, end: 4.5 },
      { start: 0, end: 1 },
    ]);
  });
});

describe('a clip names its source rather than carrying it', () => {
  it('resolves the source a clip points at, and nothing for an id no source carries', () => {
    expect(sourceOf(project(), 'src-b')?.assetId).toBe('asset-b');
    expect(sourceOf(project(), 'src-gone')).toBeUndefined();
  });
});
