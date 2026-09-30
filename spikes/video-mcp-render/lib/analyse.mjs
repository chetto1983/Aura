// The parity project's measurements, read off a rendered file (see lib/project.mjs for the plan).
import { FRAMES, WINDOWS } from './project.mjs';
import { compareFrames, frameAt, levelBetween, meanRgb, probe, WINDOW, windowPowers } from './measure.mjs';

const mean = (xs) => Number((xs.reduce((a, b) => a + b, 0) / xs.length).toFixed(2));

export function analyseFilm(file, pngPrefix) {
  const powers = windowPowers(file);
  const at = (span) => levelBetween(powers, span[0], span[1]);
  const up = mean(WINDOWS.musicUp.map(at));
  const ducked = mean(WINDOWS.musicDucked.map(at));
  const half = at(WINDOWS.musicEnvelopeHalf);
  const frames = {};
  for (const [name, t] of Object.entries(FRAMES)) frames[name] = frameAt(file, t, pngPrefix && `${pngPrefix}-${name}.png`);
  return {
    file,
    info: probe(file),
    levels: {
      clipATone: at(WINDOWS.clipTone),
      musicFadeIn: [at([5, 6]), at([6, 7])],
      musicUp: up,
      musicDucked: ducked,
      duckDepth: Number((up - ducked).toFixed(2)),
      musicEnvelopeHalf: half,
      envelopeDrop: Number((up - half).toFixed(2)),
      musicFadeOut: [at([52, 53]), at([53, 54]), at([54, 55])],
      clipCTone: at(WINDOWS.clipCTone),
    },
    powers,
    frames,
    frameMeanRgb: Object.fromEntries(Object.entries(frames).map(([k, v]) => [k, meanRgb(v)])),
  };
}

/** How far two films' audio sits apart, window by window, where either is above −80 dBFS. */
export function audioDistance(a, b) {
  const db = (p) => 10 * Math.log10(Math.max(p, 1e-20));
  let worst = 0;
  let worstAt = null;
  let compared = 0;
  for (let i = 0; i < Math.min(a.length, b.length); i += 1) {
    if (db(a[i]) < -80 && db(b[i]) < -80) continue;
    compared += 1;
    const d = Math.abs(db(a[i]) - db(b[i]));
    if (d > worst) {
      worst = d;
      worstAt = Number((i * WINDOW).toFixed(1));
    }
  }
  return { windows: compared, maxDb: Number(worst.toFixed(2)), at: worstAt, lengths: [a.length, b.length] };
}

export function framesDistance(a, b) {
  return Object.fromEntries(Object.keys(FRAMES).map((name) => [name, compareFrames(a.frames[name], b.frames[name])]));
}

/** The analysis without the raw buffers, for the JSON record. */
export function summary(film) {
  const { powers: _p, frames: _f, ...rest } = film;
  return rest;
}
