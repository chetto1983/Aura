import { describe, expect, it } from 'vitest';
import type { AudioItem, VideoProject } from '../project';
import {
  addedItem,
  holds,
  overlayCount,
  splitEdit,
  unplayableClips,
} from '../VideoStudio_selection';

// What the workspace asks of a project around its selection, asked without a workspace.

const BED: AudioItem = {
  id: 'bed',
  sourceId: 'src-m',
  anchor: { clipId: 'clip-1', offset: 0 },
  sourceStart: 0,
  duration: 8,
  volume: 1,
  muted: false,
};

function project(sounds: readonly AudioItem[] = [BED]): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 30,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 10, size: { width: 320, height: 180 } },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [
      {
        id: 'o',
        items: [
          {
            id: 'title',
            kind: 'text',
            anchor: { clipId: 'clip-1', offset: 0 },
            duration: 1,
            props: {},
          },
        ],
      },
    ],
    audio: [{ id: 'lane', items: sounds }],
  };
}

describe('the selection helpers', () => {
  it('knows a clip, an overlay and a sound by id, and nothing else', () => {
    for (const id of ['clip-2', 'title', 'bed']) expect(holds(project(), id)).toBe(true);
    expect(holds(project(), 'gone')).toBe(false);
    expect(holds(project(), undefined)).toBe(false);
  });

  it('names the overlay or sound an edit added, for the workspace to select', () => {
    expect(addedItem(project(), project([BED, { ...BED, id: 'voice' }]))).toBe('voice');
    expect(addedItem(project(), project())).toBeUndefined();
  });

  it('counts overlays and names the clips whose source is gone', () => {
    expect(overlayCount(project())).toBe(1);
    expect(unplayableClips(project(), ['src-a'])).toEqual(['clip-1', 'clip-2']);
    expect(unplayableClips(project(), [])).toEqual([]);
  });

  it('splits the selected sound at the time, and the clip under it when no sound is selected', () => {
    const cutSound = splitEdit('bed', 2)(project());
    expect(cutSound.audio?.[0]?.items).toHaveLength(2);
    expect(cutSound.video).toHaveLength(2);
    expect(splitEdit('clip-1', 2)(project()).video).toHaveLength(3);
    expect(splitEdit(undefined, 2)(project()).video).toHaveLength(3);
  });
});
