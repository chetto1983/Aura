import { useEffect, useState } from 'react';
import { capabilities, describe, trim } from './trim.js';
import { log } from './log.js';

const SAMPLES = [
  'clip-h264-aac.mp4',
  'clip-h264-silent.mp4',
  'clip-vp9-opus.webm',
  'clip-hevc-aac.mp4',
  'clip-rot90.mp4',
  'clip-long-1080p.mp4',
];

async function fetchSample(name) {
  const res = await fetch(`/${name}`);
  const blob = await res.blob();
  return new File([blob], name, { type: blob.type || (name.endsWith('.webm') ? 'video/webm' : 'video/mp4') });
}

function toBase64(blob) {
  return new Promise((resolve) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).split(',')[1]);
    r.readAsDataURL(blob);
  });
}

// Playwright drives the same code path the buttons do.
window.__spike = window.__spike ?? {};
window.__spike.capabilities = capabilities;
window.__spike.describeSample = async (name) => describe(await fetchSample(name));
window.__spike.trimSample = async (name, opts) => {
  const { blob, result } = await trim(await fetchSample(name), opts);
  return { result, base64: await toBase64(blob) };
};

export default function VideoLab() {
  const [file, setFile] = useState(null);
  const [info, setInfo] = useState(null);
  const [caps, setCaps] = useState(null);
  const [opts, setOpts] = useState({ start: 2.5, end: 6, mode: 'exact', rotate: 0, flip: false, mute: false, width: '' });
  const [progress, setProgress] = useState(0);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);

  useEffect(() => {
    capabilities().then((c) => {
      setCaps(c);
      log('video', 'encoder capabilities', c);
    });
  }, []);

  async function choose(f) {
    setFile(f);
    setResult(null);
    const d = await describe(f);
    setInfo(d);
    setOpts((o) => ({ ...o, end: Math.min(o.end, Number(d.duration.toFixed(2))) }));
    log('video', `loaded ${f.name}`, d);
  }

  async function run() {
    setBusy(true);
    setProgress(0);
    try {
      const { blob, result: r } = await trim(file, {
        ...opts,
        start: Number(opts.start),
        end: Number(opts.end),
        rotate: Number(opts.rotate) || undefined,
        width: Number(opts.width) || undefined,
        onProgress: setProgress,
      });
      setResult({ url: URL.createObjectURL(blob), name: `trim-${file.name}`, meta: r });
    } catch (e) {
      log('error', `trim failed: ${e.message}`);
      setResult({ error: e.message });
    } finally {
      setBusy(false);
    }
  }

  const set = (k) => (e) => setOpts((o) => ({ ...o, [k]: e.target.type === 'checkbox' ? e.target.checked : e.target.value }));

  return (
    <section className="lab">
      <div className="row">
        {SAMPLES.map((s) => (
          <button key={s} onClick={async () => choose(await fetchSample(s))}>{s}</button>
        ))}
        <label className="file">
          Il tuo video… <input type="file" accept="video/*" onChange={(e) => e.target.files[0] && choose(e.target.files[0])} />
        </label>
      </div>
      {caps && (
        <p className="muted">
          Encoder in questo browser — video: {caps.encodableVideo.join(', ') || 'nessuno'} · audio:{' '}
          {caps.encodableAudio.join(', ') || 'nessuno'}
        </p>
      )}
      {file && (
        <div className="split">
          <div>
            <h3>Originale · {file.name}</h3>
            <video src={URL.createObjectURL(file)} controls muted />
            <pre>{JSON.stringify(info, null, 1)}</pre>
          </div>
          <div>
            <h3>Taglio</h3>
            <div className="form">
              <label>Inizio (s) <input type="number" step="0.1" value={opts.start} onChange={set('start')} /></label>
              <label>Fine (s) <input type="number" step="0.1" value={opts.end} onChange={set('end')} /></label>
              <label>
                Modalità
                <select value={opts.mode} onChange={set('mode')}>
                  <option value="exact">esatto (ricodifica)</option>
                  <option value="expand">veloce · allarga al keyframe</option>
                  <option value="shrink">veloce · stringe al keyframe</option>
                </select>
              </label>
              <label>
                Ruota
                <select value={opts.rotate} onChange={set('rotate')}>
                  {[0, 90, 180, 270].map((r) => <option key={r} value={r}>{r}°</option>)}
                </select>
              </label>
              <label>Larghezza <input type="number" placeholder="originale" value={opts.width} onChange={set('width')} /></label>
              <label><input type="checkbox" checked={opts.flip} onChange={set('flip')} /> Specchia</label>
              <label><input type="checkbox" checked={opts.mute} onChange={set('mute')} /> Togli audio</label>
              <button className="primary" disabled={busy} onClick={run}>{busy ? `${Math.round(progress * 100)}%` : 'Taglia'}</button>
            </div>
            {result?.error && <p className="err">{result.error}</p>}
            {result?.url && (
              <>
                <video src={result.url} controls />
                <a href={result.url} download={result.name}>Scarica {result.name}</a>
                <pre>{JSON.stringify(result.meta, null, 1)}</pre>
              </>
            )}
          </div>
        </div>
      )}
    </section>
  );
}
