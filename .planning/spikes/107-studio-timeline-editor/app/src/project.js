import VideoFlow from '@videoflow/core';

export const W = 320;
export const H = 180;
export const FPS = 24;

// The composition every lab shares: the 4 s clip, a text layer, and photo.png sliding and fading in.
// Built with the fluent API, then compiled to the VideoJSON that every renderer and the editor use.
export async function buildComposition({ source = '/clip.mp4', photo = '/photo.png' } = {}) {
  const $ = new VideoFlow({ name: 'spike-107', width: W, height: H, fps: FPS, backgroundColor: '#000000' });
  $.addVideo({ fit: 'cover' }, { name: 'clip', source });
  $.addText(
    {
      text: 'AURA 107',
      fontFamily: 'Atkinson Hyperlegible Next',
      fontSize: 9,
      fontWeight: 800,
      color: '#ffffff',
      backgroundColor: '#000000',
      position: [0.3, 0.82],
    },
    { name: 'title', startTime: 0, sourceDuration: 4 },
  );
  const img = $.addImage({ fit: 'contain', scale: 0.35 }, { name: 'photo', source: photo, startTime: 0, sourceDuration: 4 });
  img.animate({ position: [0.2, 0.3], opacity: 0 }, { position: [0.8, 0.4], opacity: 1 }, { duration: '3s', easing: 'linear', wait: false });
  $.wait('4s');
  return $.compile();
}

// Split through the model: the clip becomes two layers over the same source with complementary
// source windows. Keyframe times are source-media time, so both halves keep their animation.
export function splitLayer(json, layerId, atTimelineSec) {
  const i = json.layers.findIndex((l) => l.id === layerId);
  const a = json.layers[i];
  const speed = a.settings.speed ?? 1;
  const offset = (atTimelineSec - a.settings.startTime) * Math.abs(speed);
  if (offset <= 0 || offset >= a.settings.sourceDuration) throw new Error('split point outside the clip');
  const left = structuredClone(a);
  const right = structuredClone(a);
  left.settings.sourceDuration = offset;
  right.id = `${a.id}-b`;
  right.settings.startTime = atTimelineSec;
  right.settings.sourceStart = (a.settings.sourceStart ?? 0) + offset;
  right.settings.sourceDuration = a.settings.sourceDuration - offset;
  const layers = [...json.layers];
  layers.splice(i, 1, left, right);
  return { ...json, layers };
}

// Two clips with audio, the second split in two, the third muted: the shape the editor's
// video lane produces, and the one spike 107 never rendered.
// `fps` and the sources are parameters so the SAME composition can be rendered at 24 and at 30, and
// over a 24 fps or a 30 fps source: frame rate, sampling ratio and composition each move alone.
export async function buildAudioComposition({ a = '/clip-a.mp4', b = '/clip-b.mp4', fps = 30 } = {}) {
  const $ = new VideoFlow({ name: `spike-108-${fps}fps`, width: 320, height: 180, fps, backgroundColor: '#000000' });
  $.addVideo({ fit: 'cover' }, { name: 'one', source: a, startTime: 0, sourceStart: 0, sourceDuration: 4 });
  $.addVideo({ fit: 'cover' }, { name: 'two', source: b, startTime: 4, sourceStart: 0, sourceDuration: 2 });
  $.addVideo({ fit: 'cover' }, { name: 'three', source: b, startTime: 6, sourceStart: 2, sourceDuration: 2, muted: true });
  $.wait('8s');
  return $.compile();
}

// Spike 108: one source cut into `n` equal layers, to measure whether the mixer's cost tracks the
// number of layers or the number of distinct files. `seconds` is the source's full length.
export async function buildSplitComposition(n, { a = '/clip-a.mp4', seconds = 4 } = {}) {
  const $ = new VideoFlow({ name: `spike-108-split-${n}`, width: 320, height: 180, fps: 30, backgroundColor: '#000000' });
  const slice = seconds / n;
  for (let i = 0; i < n; i++) {
    $.addVideo({ fit: 'cover' }, { name: `part-${i}`, source: a, startTime: i * slice, sourceStart: i * slice, sourceDuration: slice });
  }
  $.wait(`${seconds}s`);
  return $.compile();
}
