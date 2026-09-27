import VideoFlow from '@videoflow/core';
import BrowserRenderer from '@videoflow/renderer-browser';

interface Case {
  readonly name: string;
  readonly sourceStart: number;
  readonly sourceDuration: number;
  readonly speed: number;
  readonly volume: number | readonly { time: number; value: number }[];
}

// Full volume, then a quarter from keyframe time 3. The drop's TIMELINE position names the domain.
const STEP = [
  { time: 0, value: 1 },
  { time: 3, value: 1 },
  { time: 3.001, value: 0.25 },
  { time: 60, value: 0.25 },
];
const CASES: readonly Case[] = [
  { name: 'static-1', sourceStart: 0, sourceDuration: 6, speed: 1, volume: 1 },
  { name: 'static-0.5', sourceStart: 0, sourceDuration: 6, speed: 1, volume: 0.5 },
  { name: 'step-untrimmed', sourceStart: 0, sourceDuration: 6, speed: 1, volume: STEP },
  { name: 'step-trimmed-2s', sourceStart: 2, sourceDuration: 6, speed: 1, volume: STEP },
  { name: 'step-trimmed-2s-speed-2', sourceStart: 2, sourceDuration: 6, speed: 2, volume: STEP },
];

function levels(buffer: AudioBuffer): number[] {
  const data = buffer.getChannelData(0);
  const hop = Math.round(buffer.sampleRate / 10);
  const out: number[] = [];
  for (let at = 0; at + hop <= data.length; at += hop) {
    let sum = 0;
    for (let i = at; i < at + hop; i += 1) sum += (data[i] ?? 0) ** 2;
    out.push(Number((10 * Math.log10(sum / hop + 1e-20)).toFixed(2)));
  }
  return out;
}

async function mix(item: Case) {
  const flow = new VideoFlow({ name: item.name, width: 320, height: 180, fps: 30, backgroundColor: '#000000' });
  flow.addAudio(
    // The runtime reads keyframes from a property; the static type names only the scalar.
    { volume: item.volume as unknown as number },
    {
      source: '/fixtures/music.wav',
      startTime: 0,
      sourceStart: item.sourceStart,
      sourceDuration: item.sourceDuration,
      speed: item.speed,
    },
  );
  flow.wait(item.sourceDuration / item.speed);
  return measure(item.name, await flow.compile());
}

async function measure(name: string, json: Awaited<ReturnType<VideoFlow['compile']>>) {
  const { properties, animations } = json.layers[0] as unknown as { properties: unknown; animations: unknown };
  const renderer = new BrowserRenderer(json);
  try {
    const buffer = await renderer.renderAudio();
    if (buffer === null) throw new Error(`${name}: renderAudio returned nothing`);
    const db = levels(buffer);
    const reference = db[5] ?? 0;
    const drop = db.findIndex((level, i) => i > 5 && level <= reference - 9);
    return {
      name,
      dropAtTimeline: drop === -1 ? null : drop / 10,
      firstSecondDb: reference,
      compiled: { properties, animations },
      levels: db,
    };
  } finally {
    renderer.destroy();
  }
}

// Round two, after round one showed neither a static `volume` nor a keyframe array in the
// properties reaching the mix: the mixer reads only the compiled `animations`
// (renderer-browser/dist/audio/mixer.js, applyAudioKeyframes), so these cases write them into
// the VideoJSON directly, in absolute source seconds as compile's own comment says.
interface JsonCase {
  readonly name: string;
  readonly kind: 'audio' | 'video';
  readonly sourceStart: number;
  readonly speed: number;
  readonly staticVolume?: number;
  readonly keyframes?: readonly { time: number; value: number }[];
}
const JSON_CASES: readonly JsonCase[] = [
  { name: 'json-static-0.5', kind: 'audio', sourceStart: 0, speed: 1, keyframes: [{ time: 0, value: 0.5 }] },
  { name: 'json-step-untrimmed', kind: 'audio', sourceStart: 0, speed: 1, keyframes: STEP },
  { name: 'json-step-trimmed-2s', kind: 'audio', sourceStart: 2, speed: 1, keyframes: STEP },
  { name: 'json-step-trimmed-2s-speed-2', kind: 'audio', sourceStart: 2, speed: 2, keyframes: STEP },
  {
    name: 'json-two-point-ramp-0-to-1-over-2s',
    kind: 'audio',
    sourceStart: 0,
    speed: 1,
    keyframes: [
      { time: 1, value: 0 },
      { time: 3, value: 1 },
    ],
  },
  { name: 'video-static-property-0.5', kind: 'video', sourceStart: 0, speed: 1, staticVolume: 0.5 },
  { name: 'video-static-property-1', kind: 'video', sourceStart: 0, speed: 1, staticVolume: 1 },
  { name: 'video-json-0.5', kind: 'video', sourceStart: 0, speed: 1, keyframes: [{ time: 0, value: 0.5 }] },
];

async function mixJson(item: JsonCase) {
  const seconds = item.kind === 'video' ? 4 : 6;
  const flow = new VideoFlow({ name: item.name, width: 320, height: 180, fps: 30, backgroundColor: '#000000' });
  const settings = {
    source: item.kind === 'video' ? '/fixtures/clip-a.mp4' : '/fixtures/music.wav',
    startTime: 0,
    sourceStart: item.sourceStart,
    sourceDuration: seconds - item.sourceStart,
    speed: item.speed,
  };
  const props = item.staticVolume === undefined ? {} : { volume: item.staticVolume };
  if (item.kind === 'video') flow.addVideo(props, settings);
  else flow.addAudio(props, settings);
  flow.wait((seconds - item.sourceStart) / item.speed);
  const json = await flow.compile();
  const layer = json.layers[0] as unknown as { animations: unknown[]; properties: Record<string, unknown> };
  if (item.keyframes !== undefined) {
    layer.animations = [{ property: 'volume', keyframes: item.keyframes }];
    delete layer.properties.volume;
  }
  return measure(item.name, json);
}

async function main() {
  const results = [];
  for (const item of CASES) results.push(await mix(item));
  for (const item of JSON_CASES) results.push(await mixJson(item));
  Object.assign(window, { s1Results: results });
}
void main();
