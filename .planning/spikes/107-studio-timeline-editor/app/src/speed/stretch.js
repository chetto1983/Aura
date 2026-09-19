import SignalsmithStretch from 'signalsmith-stretch';
import { SimpleFilter, SoundTouch, WebAudioBufferSource } from 'soundtouchjs';
import BrowserRenderer from '@videoflow/renderer-browser';
import VideoFlow from '@videoflow/core';
import { useLocalFonts } from '../fonts.js';

const channelsOf = (buffer) => Array.from({ length: buffer.numberOfChannels }, (_, c) => buffer.getChannelData(c).slice());

// Control: a plain resample (AudioBufferSourceNode.playbackRate). Duration scales, pitch does not survive.
export async function stretchNaive(buffer, speed) {
  const ctx = new OfflineAudioContext(buffer.numberOfChannels, Math.ceil(buffer.length / speed), buffer.sampleRate);
  const src = new AudioBufferSourceNode(ctx, { buffer, playbackRate: speed });
  src.connect(ctx.destination);
  src.start();
  return ctx.startRendering();
}

// signalsmith-stretch (MIT): an AudioWorkletNode whose WASM ships inline. Offline: the input buffer
// is handed to the node, one schedule() sets the rate, the OfflineAudioContext renders 4 s / speed.
export async function stretchSignalsmith(buffer, speed) {
  const ctx = new OfflineAudioContext(buffer.numberOfChannels, Math.ceil(buffer.length / speed), buffer.sampleRate);
  const node = await SignalsmithStretch(ctx, { numberOfInputs: 0, numberOfOutputs: 1, outputChannelCount: [buffer.numberOfChannels] });
  node.connect(ctx.destination);
  await node.addBuffers(channelsOf(buffer));
  await node.schedule({ active: true, input: 0, output: 0, rate: speed });
  return ctx.startRendering();
}

// soundtouchjs (LGPL-2.1): pure JS, pulled through SimpleFilter as interleaved stereo.
export function stretchSoundtouch(buffer, speed) {
  const st = new SoundTouch();
  st.tempo = speed;
  st.pitch = 1;
  const filter = new SimpleFilter(new WebAudioBufferSource(buffer), st);
  const chunk = 4096;
  const tmp = new Float32Array(chunk * 2);
  const left = [];
  for (let n = filter.extract(tmp, chunk); n > 0; n = filter.extract(tmp, chunk)) {
    for (let i = 0; i < n; i++) left.push(tmp[i * 2]);
  }
  const out = new AudioBuffer({ numberOfChannels: 1, length: left.length, sampleRate: buffer.sampleRate });
  out.copyToChannel(Float32Array.from(left), 0);
  return out;
}

// VideoFlow's own path: speed on the layer plus pitch = 1/speed (its granular shifter), mixed by
// BrowserRenderer.renderAudio. No extra dependency.
export async function stretchVideoFlow(_buffer, speed, source = '/clip.mp4') {
  const $ = new VideoFlow({ width: 320, height: 180, fps: 24 });
  $.addAudio({ pitch: 1 / speed }, { source, speed }, { waitFor: 'finish' });
  const json = await $.compile();
  const renderer = useLocalFonts(new BrowserRenderer(json));
  try {
    return await renderer.renderAudio();
  } finally {
    renderer.destroy();
  }
}
