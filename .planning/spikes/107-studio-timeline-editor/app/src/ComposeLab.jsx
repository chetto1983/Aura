import { useEffect, useRef, useState } from 'react';
import BrowserRenderer from '@videoflow/renderer-browser';
import DomRenderer from '@videoflow/renderer-dom';
import { buildAudioComposition, buildComposition, buildSplitComposition, splitLayer } from './project.js';
import { useLocalFonts } from './fonts.js';
import { log } from './log.js';
import { blobToBase64, fitDuration } from './util.js';

// Scenario "split": cut the clip at 2 s and trim 0.5 s off the head of the right half, so the
// burned-in clock must jump from 2.000 to 2.500 at timeline 2 s and the export lasts 3.5 s.
// Scenario "bias": the same composition with the video's source window nudged 0.1 ms forward, a
// candidate workaround for VideoFlow sampling a frame exactly on its start boundary (see README).
const nudge = (json) => {
  for (const l of json.layers) if (l.type === 'video') l.settings.sourceStart = (l.settings.sourceStart ?? 0) + 1e-4;
  return json;
};

// Spike 108 scenarios, all at 30 fps over two clips that carry an audio track:
//  audio            the brief's three-layer lane, verbatim (`muted` in SETTINGS, not properties)
//  audio-mute-prop  the same with `mute` set where the mixer actually reads it (properties)
//  split-<n>        the 4 s source cut into n layers, to see what the mixer's cost tracks
//  long-<n>         the same over a 60 s stereo source, the length a phone clip actually has
//  …-nudge          any of the above with the 107 workaround (sourceStart + 1e-4) applied
async function scenario108(name) {
  const [base, ...rest] = name.split('-nudge');
  const wantsNudge = rest.length > 0;
  let json;
  if (base.startsWith('split-')) json = await buildSplitComposition(Number(base.slice('split-'.length)));
  else if (base.startsWith('long-')) json = await buildSplitComposition(Number(base.slice('long-'.length)), { a: '/clip-long.mp4', seconds: 60 });
  else json = await buildAudioComposition();
  if (base === 'audio-mute-prop') json.layers.at(-1).properties.mute = true;
  return wantsNudge ? nudge(json) : json;
}

async function scenario(name) {
  if (name.startsWith('audio') || name.startsWith('split-') || name.startsWith('long-')) return scenario108(name);
  const json = await buildComposition();
  if (name === 'bias') return nudge(json);
  if (name !== 'split') return json;
  const clip = json.layers.find((l) => l.settings.name === 'clip');
  const cut = splitLayer(json, clip.id, 2);
  const right = cut.layers.find((l) => l.id === `${clip.id}-b`);
  right.settings.sourceStart += 0.5;
  right.settings.sourceDuration -= 0.5;
  for (const l of cut.layers) if (l.id !== clip.id && l.id !== right.id) l.settings.sourceDuration = 3.5;
  return fitDuration(cut);
}

// Spike 108's candidate fix, to be measured rather than argued: `decodeLayerAudio` reads
// `layer.decodedBuffer` on ANY layer, although only RuntimeAudioLayer ever sets it. Priming it with
// one buffer per distinct source should collapse N per-layer decodes to one per file. `initLayers`
// is idempotent (`elementsSetup`), so the export re-entering it costs nothing.
async function primeDecodedBuffers(renderer) {
  await renderer.initLayers();
  const ctx = new OfflineAudioContext(2, 48000, 48000);
  const bySource = new Map();
  for (const layer of renderer.layers) {
    const src = layer.json.settings.source;
    if (!src || !layer.hasAudio) continue;
    // One fetch and one decode per source; decodeAudioData detaches its input, so the promise
    // rather than the bytes is what gets shared.
    if (!bySource.has(src)) bySource.set(src, fetch(src).then((r) => r.arrayBuffer()).then((b) => ctx.decodeAudioData(b)));
    layer.decodedBuffer = await bySource.get(src);
  }
  return bySource.size;
}

// `stockFonts` is the negative control: VideoFlow's own loadFont, which goes to Google Fonts.
async function render(name, { worker = true, stockFonts = false, cache = false } = {}) {
  const json = await scenario(name);
  const renderer = stockFonts ? new BrowserRenderer(json) : useLocalFonts(new BrowserRenderer(json));
  const t0 = performance.now();
  let lastProgress = 0;
  try {
    const cachedSources = cache ? await primeDecodedBuffers(renderer) : 0;
    const blob = await renderer.exportVideo({ worker, onProgress: (p) => { lastProgress = p; } });
    const ms = Math.round(performance.now() - t0);
    log('render', `${name} worker=${worker} ${ms} ms ${blob.size} B`);
    return { ms, bytes: blob.size, lastProgress, cachedSources, duration: json.duration, layers: json.layers.map((l) => ({ id: l.id, type: l.type, settings: l.settings })), base64: await blobToBase64(blob) };
  } finally {
    renderer.destroy();
  }
}

window.__spike = window.__spike ?? {};
window.__spike.render = render;
window.__spike.scenario = scenario;

export default function ComposeLab() {
  const host = useRef(null);
  const player = useRef(null);
  const [frame, setFrame] = useState(0);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);

  useEffect(() => {
    let cancelled = false;
    const p = useLocalFonts(new DomRenderer(host.current));
    player.current = p;
    p.onFrame = setFrame;
    scenario('base').then(async (json) => {
      if (cancelled) return;
      await p.loadVideo(json);
      await p.seek(48);
      window.__spike.previewReady = true;
      log('preview', `DomRenderer loaded ${p.totalFrames} frames`);
    });
    return () => { cancelled = true; p.destroy(); };
  }, []);

  async function exportIt(name) {
    setBusy(true);
    try {
      const r = await render(name);
      const a = document.createElement('a');
      a.href = `data:video/mp4;base64,${r.base64}`;
      a.download = `spike-107-${name}.mp4`;
      a.click();
      setResult({ ms: r.ms, bytes: r.bytes });
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="lab">
      <div ref={host} className="player" data-testid="preview" />
      <div className="row">
        <button onClick={() => player.current.play()}>Play</button>
        <button onClick={() => player.current.stop()}>Stop</button>
        <span>frame {frame}</span>
        <button disabled={busy} onClick={() => exportIt('base')}>Esporta MP4</button>
        <button disabled={busy} onClick={() => exportIt('split')}>Esporta con taglio</button>
        {result && <span>{result.ms} ms · {result.bytes} B</span>}
      </div>
    </section>
  );
}
