// Two renders of the parity project side by side: per-window audio distance and the three frames.
// Usage: node s1-compare.mjs <out/x.mp4> <out/y.mp4> <label>
import { analyseFilm, audioDistance, framesDistance } from './lib/analyse.mjs';
import { writeJson } from './lib/measure.mjs';
import { ROOT } from './lib/server.mjs';

const [x, y, label] = process.argv.slice(2);
const a = analyseFilm(`${ROOT}${x}`);
const b = analyseFilm(`${ROOT}${y}`);
const record = {
  x,
  y,
  pixFmt: [a.info.video.pixFmt, b.info.video.pixFmt],
  bytes: [a.info.bytes, b.info.bytes],
  audio: audioDistance(a.powers, b.powers),
  frames: framesDistance(a, b),
};
writeJson(`${ROOT}out/s1-compare-${label}.json`, record);
console.log(JSON.stringify(record, null, 2));
