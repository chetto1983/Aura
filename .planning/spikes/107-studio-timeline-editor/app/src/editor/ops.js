// Editor operations over a VideoFlow VideoJSON draft (immer draft or a plain clone). Every op
// mutates in place and keeps `duration` equal to the last layer's end.
import { layerEnd } from '../util.js';

const layer = (d, id) => {
  const l = d.layers.find((x) => x.id === id);
  if (!l) throw new Error(`no layer ${id}`);
  return l;
};

const refit = (d) => { d.duration = Math.max(0, ...d.layers.map(layerEnd)); };

export const ops = {
  addLayer: (d, json) => { d.layers.push(json); refit(d); },
  removeLayer: (d, id) => { d.layers = d.layers.filter((l) => l.id !== id); refit(d); },
  move: (d, id, startTime, track) => {
    const l = layer(d, id);
    l.settings.startTime = Math.max(0, startTime);
    if (track !== undefined) l.track = track;
    refit(d);
  },
  // Trim an edge on the timeline. The left edge moves the source window with it, so the frames
  // under the rest of the clip stay put — the NLE convention.
  trim: (d, id, edge, timelineSec) => {
    const l = layer(d, id);
    const s = l.settings;
    const speed = Math.abs(s.speed ?? 1);
    const media = s.mediaDuration ?? Infinity;
    if (edge === 'end') {
      const maxSrc = media - (s.sourceStart ?? 0);
      s.sourceDuration = Math.min(maxSrc, Math.max(1 / 24, (timelineSec - s.startTime) * speed));
    } else {
      const end = layerEnd(l);
      const newStart = Math.min(end - 1 / 24, Math.max(0, timelineSec));
      const srcShift = (newStart - s.startTime) * speed;
      const newSourceStart = (s.sourceStart ?? 0) + srcShift;
      if (newSourceStart < 0) return;
      s.startTime = newStart;
      s.sourceStart = newSourceStart;
      s.sourceDuration -= srcShift;
    }
    refit(d);
  },
  setProperty: (d, id, prop, value) => { layer(d, id).properties[prop] = value; },
  setSpeed: (d, id, speed) => { layer(d, id).settings.speed = speed; refit(d); },
  split: (d, id, atSec) => {
    const i = d.layers.findIndex((l) => l.id === id);
    const a = d.layers[i];
    const offset = (atSec - a.settings.startTime) * Math.abs(a.settings.speed ?? 1);
    if (offset <= 0 || offset >= a.settings.sourceDuration) return;
    const right = JSON.parse(JSON.stringify(a));
    right.id = `${a.id}~${Math.round(atSec * 1000)}`;
    right.settings.startTime = atSec;
    right.settings.sourceStart = (a.settings.sourceStart ?? 0) + offset;
    right.settings.sourceDuration = a.settings.sourceDuration - offset;
    a.settings.sourceDuration = offset;
    d.layers.splice(i + 1, 0, right);
  },
};
