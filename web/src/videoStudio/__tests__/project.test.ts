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
        fps: 30,
      },
      {
        id: 'src-b',
        assetId: 'asset-b',
        kind: 'image',
        duration: 0,
        size: { width: 800, height: 600 },
        fps: 0,
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
            anchor: { clipId: 'clip-2', offset: 0.5 },
            duration: 1,
            props: { text: 'ciao' },
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
});

describe('a clip names its source rather than carrying it', () => {
  it('resolves the source a clip points at, and nothing for an id no source carries', () => {
    expect(sourceOf(project(), 'src-b')?.assetId).toBe('asset-b');
    expect(sourceOf(project(), 'src-gone')).toBeUndefined();
  });
});
