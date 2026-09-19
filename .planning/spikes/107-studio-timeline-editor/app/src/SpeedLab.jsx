import { useState } from 'react';
import { spectrumPeak } from './speed/fft.js';
import { stretchNaive, stretchSignalsmith, stretchSoundtouch, stretchVideoFlow } from './speed/stretch.js';
import { muxRetimed } from './speed/mux.js';
import { blobToBase64 } from './util.js';
import { log } from './log.js';

const METHODS = { naive: stretchNaive, signalsmith: stretchSignalsmith, soundtouch: stretchSoundtouch, videoflow: stretchVideoFlow };

async function clipFile() {
  const blob = await (await fetch('/clip.mp4')).blob();
  return new File([blob], 'clip.mp4', { type: 'video/mp4' });
}

async function decodeClipAudio() {
  const ctx = new OfflineAudioContext(1, 1, 44100);
  return ctx.decodeAudioData(await (await clipFile()).arrayBuffer());
}

// Offline stretch of the clip's audio (440 Hz sine, 4 s) with one method at one speed.
async function stretch(method, speed) {
  const input = await decodeClipAudio();
  const t0 = performance.now();
  const out = await METHODS[method](input, speed);
  const ms = Math.round(performance.now() - t0);
  const r = { method, speed, ms, inDuration: input.duration, outDuration: +out.duration.toFixed(4), expected: +(input.duration / speed).toFixed(4), sampleRate: out.sampleRate, channels: out.numberOfChannels, ...spectrumPeak(out.getChannelData(0), out.sampleRate) };
  r.durationOk = Math.abs(r.outDuration / r.expected - 1) <= 0.02;
  r.pitchOk = Math.abs(r.peakHz - 440) <= 5;
  log('speed', `${method} ${speed}×: ${r.outDuration}s peak ${r.peakHz} Hz purity ${r.purity} ${ms} ms`);
  return r;
}

async function exportRetimed(method, speed) {
  const file = await clipFile();
  const audio = await METHODS[method](await decodeClipAudio(), speed);
  const t0 = performance.now();
  const blob = await muxRetimed(file, audio, speed);
  return { ms: Math.round(performance.now() - t0), bytes: blob.size, base64: await blobToBase64(blob) };
}

// Preview only: the <video> element's own time-stretch. Measured live through an AnalyserNode.
async function previewPitch(rate, preservesPitch) {
  const video = Object.assign(document.createElement('video'), { src: '/clip.mp4', playsInline: true, loop: true });
  video.preservesPitch = preservesPitch;
  video.playbackRate = rate;
  const ctx = new AudioContext();
  const analyser = new AnalyserNode(ctx, { fftSize: 32768, smoothingTimeConstant: 0 });
  ctx.createMediaElementSource(video).connect(analyser).connect(new GainNode(ctx, { gain: 0 })).connect(ctx.destination);
  await ctx.resume();
  await video.play();
  await new Promise((r) => setTimeout(r, 1500));
  const db = new Float32Array(analyser.frequencyBinCount);
  analyser.getFloatFrequencyData(db);
  video.pause();
  await ctx.close();
  const binHz = ctx.sampleRate / analyser.fftSize;
  let top = 1;
  for (let k = 1; k < db.length; k++) if (k * binHz > 20 && db[k] > db[top]) top = k;
  const [a, b, c] = [db[top - 1], db[top], db[top + 1]];
  const peakHz = +((top + (a - c) / (2 * (a - 2 * b + c))) * binHz).toFixed(2);
  return { rate, preservesPitch, peakHz, pitchOk: Math.abs(peakHz - 440) <= 5 };
}

window.__spike = window.__spike ?? {};
window.__spike.stretch = stretch;
window.__spike.exportRetimed = exportRetimed;
window.__spike.previewPitch = previewPitch;

export default function SpeedLab() {
  const [rows, setRows] = useState([]);
  async function runAll() {
    const out = [];
    for (const m of Object.keys(METHODS)) for (const s of [0.5, 2]) out.push(await stretch(m, s));
    setRows(out);
  }
  return (
    <section className="lab">
      <div className="row"><button onClick={runAll}>Misura 0.5× e 2×</button></div>
      <table>
        <tbody>
          {rows.map((r) => (
            <tr key={`${r.method}${r.speed}`}><td>{r.method}</td><td>{r.speed}×</td><td>{r.outDuration} s</td><td>{r.peakHz} Hz</td><td>{r.ms} ms</td></tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
