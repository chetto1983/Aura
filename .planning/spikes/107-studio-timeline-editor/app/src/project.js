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
