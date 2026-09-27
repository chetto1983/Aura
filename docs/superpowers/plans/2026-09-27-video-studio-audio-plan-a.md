# Video Studio audio — Plan A (spikes + foundation) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Measure the four unknowns the audio design rests on, record them in the PRD, and land the foundation every later audio task builds on: editor sources that are never transcribed, audio filed under `media/`, the audio model in the project file, and a rail and E2E helpers ready to grow.

**Architecture:** Plan A is the first half of sub-project 1. Tasks 1–6 are throwaway probes in `spikes/video-studio-audio/`, run in a real Chromium, whose numbers become the PRD amendment and the thresholds of Plan B (T2–T7, written after this plan runs). Tasks 7–8 are product code: a `FinalizeMedia` door on the asset service, the audio model and its loader, the rail extracted from the 598-line workspace, and the Studio E2E helpers moved into `e2e/support/`. Each product task closes with an E2E on the lab VM.

**Tech Stack:** Go 1.26 (asset service, AG-UI handlers), TypeScript/React 19 + Vite 8 (cockpit), VideoFlow 1.3.4, wavesurfer.js 8.0.1, `@ricky0123/vad-web` 0.0.31, `@echogarden/fvad-wasm` 0.2.0, `@sapphi-red/web-noise-suppressor` 0.4.1, mediabunny 1.58.1, Playwright 1.62.1, vitest.

**Spec:** `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md`

## Global Constraints

- **Never run a Windows executable.** In Git Bash, `node`, `npm`, `npx`, `python3` and `go` are Windows exes. Every such command below runs **in WSL**: write it to a script file and run `MSYS_NO_PATHCONV=1 wsl bash /mnt/c/.../script.sh` (a `wsl bash -c '…'` one-liner expands variables too early). In WSL, prepend `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`.
- **Never `go build ./...` locally** (about an hour on `/mnt/d`); vet, build and test only the touched packages.
- **Shared index:** another session works in this tree. Stage and commit explicit paths only (`git commit -- <paths>`), never `git add -A`, never `git reset` the shared HEAD. **Never `--no-verify`.**
- **Files ≤ 600 lines.** Refactor on touch: a file you edit gets its dead code removed and stays under 600.
- **The pure core stays pure:** `project.ts`, `commands*.ts`, `projectStore.ts` validation and the compile step import no DOM, Web Audio or `fetch` beyond what they already do.
- Comments only for a non-obvious why. Every user-facing string in English and Italian (`web/src/i18n/resources.videoStudio.ts`).
- Envelope gain 0–1; item volume 0–2; fades 0–5 s (spec §Model).
- Unit gates: vitest coverage ≥ 85 % on every touched web file; Go `vet`, `test`, `test -race` green on touched packages.
- **Per-task E2E protocol (Tasks 7–8), from the spec:** build + unit + lint + format + knip green in WSL and the committed dist rebuilt (`npm run build`); commit on `master`; push; CI green including *Publish Aura edge image*; wait until the VM's `aura` container reports the commit (`org.opencontainers.image.revision`); run the task's Playwright spec from WSL against `https://192.168.101.158`; inspect screenshots and measured values, not only PASS; delete every asset the run created on the operator's account.
- The VM account is the operator's own (`.env.google` `VM_COCKPIT_EMAIL`/`VM_COCKPIT_PASSWORD`, identical by hash to `.env` `AURA_E2E_AUTHULA_*`, so `gotoAuthenticated` works unchanged). Never print them. Never create an identity.
- A probe that fails is a finding, not a problem to engineer around: on a failed pass condition, write it into FINDINGS.md and stop for the operator (spec §Spikes, "STOP BEFORE BESPOKE").

## Review Focus

These are the failure modes the spec implies that no happy-path test would reach. Each has its test in the task that owns the code.

1. **A project file whose audio item names a source that is missing, or is a still image.** The load refuses it with "not a project", before it can reach the compile. (Task 8, `projectStore.test.ts`.)
2. **An audio item anchored to a clip the file does not hold, or extracted from one.** The load refuses it instead of drawing a ghost on a lane. (Task 8.)
3. **A number that JSON parses to `Infinity` (`1e999`) in an audio field or a speech window.** The load refuses it; `typeof Infinity === 'number'`, and it would reach `clipStarts` as `NaN`. (Task 8.)
4. **`finalize?use=media` on a document.** The call is refused with 400, and the PDF is neither accepted as media nor queued for processing. (Task 7, Go test and E2E.)
5. **`finalize?use=` with any value other than `media`.** The call is refused with 400; it never falls through to the processing default. (Task 7, handler test.)

---

### Task 1: Spike workspace and audio fixtures

The four spikes need a real browser, the pinned packages and three audio fixtures with a known ground truth. The fixtures are product test data (Plan B's E2E measures against them), so they are committed with their provenance.

**Files:**
- Create: `spikes/video-studio-audio/package.json`
- Create: `spikes/video-studio-audio/.gitignore`
- Create: `spikes/video-studio-audio/vite.config.ts`
- Create: `spikes/video-studio-audio/setup.sh`
- Create: `spikes/video-studio-audio/lib/browser.mjs`
- Create: `spikes/video-studio-audio/src/wav.ts`
- Create: `spikes/video-studio-audio/src/fixtures.ts`
- Create: `spikes/video-studio-audio/fixtures.html`
- Create: `spikes/video-studio-audio/tts-phrases.sh`
- Create: `spikes/video-studio-audio/make-music.mjs`
- Create: `spikes/video-studio-audio/make-fixtures.mjs`
- Create: `spikes/video-studio-audio/verify-fixtures.mjs`
- Create: `spikes/video-studio-audio/FINDINGS.md`
- Create: `web/e2e/fixtures/video-studio/audio/README.md`
- Output (committed): `web/e2e/fixtures/video-studio/audio/{music.wav,speech.wav,speech-noisy.wav,speech.truth.json}`

**Interfaces:**
- Produces: `openProbe(pagePath, { mobile? }) → { page, assets, close }` in `lib/browser.mjs`, where `assets` is the list of `{ url, bytes }` the page fetched. Produces `encodeWav`, `decodeMono`, `rmsDb`, `toBase64`, `fromBase64` in `src/wav.ts`. Produces the fixtures, 16 kHz mono PCM16. `speech.truth.json` holds `{ sampleRate, snrDb, speechRmsDb, windows: [[start, end], …] }` in seconds.

- [ ] **Step 1: Write the spike package and config**

`spikes/video-studio-audio/package.json`:

```json
{
  "name": "video-studio-audio-spikes",
  "private": true,
  "type": "module",
  "dependencies": {
    "@echogarden/fvad-wasm": "0.2.0",
    "@ricky0123/vad-web": "0.0.31",
    "@sapphi-red/web-noise-suppressor": "0.4.1",
    "@videoflow/core": "1.3.4",
    "@videoflow/renderer-browser": "1.3.4",
    "dnd-timeline": "3.1.1",
    "react": "19.3.0",
    "react-dom": "19.3.0",
    "wavesurfer.js": "8.0.1"
  },
  "devDependencies": {
    "@vitejs/plugin-react": "6.1.1",
    "playwright": "1.62.1",
    "vite": "8.3.0"
  }
}
```

`spikes/video-studio-audio/.gitignore`:

```
node_modules/
public/
out/
```

`spikes/video-studio-audio/vite.config.ts`:

```ts
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react()],
  server: { host: '127.0.0.1', port: 5199, strictPort: true },
});
```

`spikes/video-studio-audio/setup.sh`:

```bash
#!/usr/bin/env bash
# Installs the probe's pinned packages and stages what the probe pages fetch: the fixtures, the
# VAD models and the ONNX runtime files, all served from the probe's own origin.
set -euo pipefail
cd "$(dirname "$0")"
export PATH="$HOME/.local/bin:$PATH"
npm install --no-audit --no-fund
npx playwright install chromium
mkdir -p public/fixtures public/vad public/ort out
cp ../../web/e2e/fixtures/video-studio/clip-a.mp4 public/fixtures/
cp ../../web/e2e/fixtures/video-studio/audio/*.wav ../../web/e2e/fixtures/video-studio/audio/*.json public/fixtures/ 2>/dev/null || true
cp node_modules/@ricky0123/vad-web/dist/silero_vad_legacy.onnx node_modules/@ricky0123/vad-web/dist/silero_vad_v5.onnx public/vad/
cp node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded.wasm node_modules/onnxruntime-web/dist/ort-wasm-simd-threaded.mjs public/ort/
```

- [ ] **Step 2: Write the browser helper and the WAV helpers**

`spikes/video-studio-audio/lib/browser.mjs`:

```js
// Starts the probe's Vite server and a Chromium on it. Every response the page receives is
// recorded with its size, so a probe can say exactly which bytes a package pulled in.
import { chromium, devices } from 'playwright';
import { createServer } from 'vite';
import { fileURLToPath } from 'node:url';

const ROOT = fileURLToPath(new URL('..', import.meta.url));

export async function openProbe(pagePath, { mobile = false } = {}) {
  const server = await createServer({ root: ROOT, configFile: `${ROOT}vite.config.ts` });
  await server.listen();
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext(
    mobile ? { ...devices['Pixel 5'] } : { viewport: { width: 1280, height: 800 } },
  );
  const page = await context.newPage();
  const assets = [];
  page.on('response', async (response) => {
    try {
      assets.push({ url: response.url(), bytes: (await response.body()).length });
    } catch {
      // A redirect or an aborted request has no body to count.
    }
  });
  page.on('console', (message) => console.log(`[page] ${message.text()}`));
  page.on('pageerror', (error) => console.log(`[page error] ${error.message}`));
  await page.goto(`http://127.0.0.1:5199/${pagePath}`);
  return {
    page,
    assets,
    close: async () => {
      await browser.close();
      await server.close();
    },
  };
}
```

`spikes/video-studio-audio/src/wav.ts`:

```ts
export function encodeWav(samples: Float32Array, sampleRate: number): Uint8Array {
  const buffer = new ArrayBuffer(44 + samples.length * 2);
  const view = new DataView(buffer);
  const text = (offset: number, value: string) => {
    for (let i = 0; i < value.length; i += 1) view.setUint8(offset + i, value.charCodeAt(i));
  };
  text(0, 'RIFF');
  view.setUint32(4, 36 + samples.length * 2, true);
  text(8, 'WAVE');
  text(12, 'fmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, sampleRate, true);
  view.setUint32(28, sampleRate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  text(36, 'data');
  view.setUint32(40, samples.length * 2, true);
  samples.forEach((sample, i) => {
    view.setInt16(44 + i * 2, Math.round(Math.max(-1, Math.min(1, sample)) * 32767), true);
  });
  return new Uint8Array(buffer);
}

export function toBase64(bytes: Uint8Array): string {
  let text = '';
  for (let i = 0; i < bytes.length; i += 0x8000) {
    text += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(text);
}

export function fromBase64(base64: string): Uint8Array {
  return Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
}

/** Decodes any container Chromium can read and renders it to mono at `sampleRate`. */
export async function decodeMono(bytes: Uint8Array, sampleRate: number): Promise<Float32Array> {
  const probe = new OfflineAudioContext(1, 1, sampleRate);
  const decoded = await probe.decodeAudioData(bytes.slice().buffer);
  const offline = new OfflineAudioContext(1, Math.ceil(decoded.duration * sampleRate), sampleRate);
  const source = offline.createBufferSource();
  source.buffer = decoded;
  source.connect(offline.destination);
  source.start();
  return (await offline.startRendering()).getChannelData(0);
}

export function rmsDb(samples: Float32Array, from = 0, to = samples.length): number {
  let sum = 0;
  for (let i = from; i < to; i += 1) sum += (samples[i] ?? 0) ** 2;
  return 10 * Math.log10(sum / Math.max(1, to - from) + 1e-20);
}
```

- [ ] **Step 3: Write the fixture page**

`spikes/video-studio-audio/fixtures.html`:

```html
<!doctype html>
<html>
  <body>
    <script type="module" src="/src/fixtures.ts"></script>
  </body>
</html>
```

`spikes/video-studio-audio/src/fixtures.ts`:

```ts
// Builds the speech fixtures from three TTS phrases: silence, phrase, gap, phrase, gap, phrase,
// silence. The windows are known to the sample, so they are the ground truth a VAD is scored on.
import { decodeMono, encodeWav, fromBase64, toBase64 } from './wav';

const RATE = 16000;
const LEAD = 1.0;
const GAP = 1.5;
const TAIL = 1.0;
const SNR_DB = 10;
const TRIM_DB = -50;

function trimSilence(samples: Float32Array): Float32Array {
  const floor = 10 ** (TRIM_DB / 20);
  let start = 0;
  while (start < samples.length && Math.abs(samples[start] ?? 0) < floor) start += 1;
  let end = samples.length;
  while (end > start && Math.abs(samples[end - 1] ?? 0) < floor) end -= 1;
  return samples.slice(start, end);
}

function mulberry32(seed: number): () => number {
  let state = seed;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let t = Math.imul(state ^ (state >>> 15), 1 | state);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** Paul Kellet's economy pink filter over seeded white noise: reproducible, speech-like spectrum. */
function pinkNoise(length: number, seed: number): Float32Array {
  const random = mulberry32(seed);
  const out = new Float32Array(length);
  let b0 = 0;
  let b1 = 0;
  let b2 = 0;
  for (let i = 0; i < length; i += 1) {
    const white = random() * 2 - 1;
    b0 = 0.99765 * b0 + white * 0.099046;
    b1 = 0.963 * b1 + white * 0.2965164;
    b2 = 0.57 * b2 + white * 1.0526913;
    out[i] = b0 + b1 + b2 + white * 0.1848;
  }
  return out;
}

function rms(samples: Float32Array, from: number, to: number): number {
  let sum = 0;
  for (let i = from; i < to; i += 1) sum += (samples[i] ?? 0) ** 2;
  return Math.sqrt(sum / Math.max(1, to - from));
}

async function build(phrases: readonly string[]) {
  const voiced = await Promise.all(
    phrases.map(async (base64) => trimSilence(await decodeMono(fromBase64(base64), RATE))),
  );
  const silence = Math.round((LEAD + TAIL + GAP * (voiced.length - 1)) * RATE);
  const speech = new Float32Array(silence + voiced.reduce((n, v) => n + v.length, 0));
  const windows: [number, number][] = [];
  let at = Math.round(LEAD * RATE);
  voiced.forEach((phrase, index) => {
    speech.set(phrase, at);
    windows.push([at, at + phrase.length]);
    at += phrase.length + (index < voiced.length - 1 ? Math.round(GAP * RATE) : 0);
  });
  const peak = speech.reduce((max, s) => Math.max(max, Math.abs(s)), 0);
  const gain = 10 ** (-3 / 20) / peak;
  for (let i = 0; i < speech.length; i += 1) speech[i] = (speech[i] ?? 0) * gain;

  let sum = 0;
  let count = 0;
  for (const [start, end] of windows) {
    for (let i = start; i < end; i += 1) sum += (speech[i] ?? 0) ** 2;
    count += end - start;
  }
  const speechRms = Math.sqrt(sum / count);
  const noise = pinkNoise(speech.length, 20260927);
  const scale = speechRms / 10 ** (SNR_DB / 20) / rms(noise, 0, noise.length);
  const noisy = speech.map((s, i) => s + (noise[i] ?? 0) * scale);
  const noisyPeak = noisy.reduce((max, s) => Math.max(max, Math.abs(s)), 0);
  if (noisyPeak > 0.99) {
    for (let i = 0; i < noisy.length; i += 1) noisy[i] = ((noisy[i] ?? 0) * 0.99) / noisyPeak;
  }
  return {
    speech: toBase64(encodeWav(speech, RATE)),
    noisy: toBase64(encodeWav(noisy, RATE)),
    truth: {
      sampleRate: RATE,
      snrDb: SNR_DB,
      speechRmsDb: Number((20 * Math.log10(speechRms)).toFixed(2)),
      windows: windows.map(([s, e]) => [Number((s / RATE).toFixed(3)), Number((e / RATE).toFixed(3))]),
    },
  };
}

Object.assign(window, { buildFixtures: build });
```

- [ ] **Step 4: Write the three generators and the verifier**

`spikes/video-studio-audio/tts-phrases.sh`:

```bash
#!/usr/bin/env bash
# Asks the lab VM's own TTS for the three phrases the speech fixtures are built from. Signs in
# with the operator's cockpit account from .env.google (never printed) through the repo's
# Authula helpers. Output: out/phrase-{1,2,3}.bin, exactly the bytes /api/tts answered.
set -euo pipefail
cd "$(dirname "$0")"
export BASE=https://192.168.101.158
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
printf 'insecure\n' > "$WORK/.curlrc"
export CURL_HOME="$WORK"
envval() { grep "^$1=" ../../.env.google | head -1 | cut -d= -f2- | sed -e 's/^["'\'']//' -e 's/["'\'']$//' | tr -d '\r'; }
# shellcheck source=../../scripts/musr_live_run_authula_helpers.sh
source ../../scripts/musr_live_run_authula_helpers.sh
JAR="$WORK/jar"
: > "$JAR"
read -r base_path csrf_header csrf_cookie csrf_token < <(fetch_auth_config "$JAR")
sign_in "$JAR" "$(envval VM_COCKPIT_EMAIL)" "$(envval VM_COCKPIT_PASSWORD)" \
  "$base_path" "$csrf_header" "$csrf_cookie" "$csrf_token"
COOKIE="$(cookie_header_from_jar "$JAR")"
caps="$(curl -fsS "$BASE/api/voice/capabilities" -H "Cookie: $COOKIE")"
echo "capabilities: $caps"
grep -q '"tts":true' <<<"$caps" || { echo "the VM has no TTS configured: stop and ask the operator" >&2; exit 1; }
mkdir -p out
phrases=(
  "The river runs past the old mill every morning."
  "Nobody expected the concert to start so early."
  "Please keep the second box beside the window."
)
for i in 0 1 2; do
  curl -fsS -X POST "$BASE/api/tts" -H "Cookie: $COOKIE" -H "Origin: $BASE" \
    -H "Content-Type: application/json" -H "Idempotency-Key: $(cat /proc/sys/kernel/random/uuid)" \
    -H "$csrf_header: $csrf_token" \
    -d "$(printf '{"text":"%s"}' "${phrases[$i]}")" -o "out/phrase-$((i + 1)).bin"
  echo "phrase $((i + 1)): $(wc -c < "out/phrase-$((i + 1)).bin") bytes, $(file -b "out/phrase-$((i + 1)).bin")"
done
```

`spikes/video-studio-audio/make-music.mjs`:

```js
// The music bed: 8 s of an A-minor progression at a CONSTANT level, 16 kHz mono PCM16. Three
// equal sines of different frequency have an RMS of amp·sqrt(3/2): 0.1225, i.e. -18.24 dBFS.
// A constant level is the point: a volume or a fade is then read straight off the RMS.
import { writeFileSync } from 'node:fs';

const RATE = 16000;
const SECONDS = 8;
const AMP = 0.1;
const CHORDS = [
  [220, 261.63, 329.63],
  [174.61, 220, 261.63],
  [196, 246.94, 293.66],
  [164.81, 207.65, 246.94],
];
const n = RATE * SECONDS;
const wav = Buffer.alloc(44 + n * 2);
wav.write('RIFF', 0);
wav.writeUInt32LE(36 + n * 2, 4);
wav.write('WAVEfmt ', 8);
wav.writeUInt32LE(16, 16);
wav.writeUInt16LE(1, 20);
wav.writeUInt16LE(1, 22);
wav.writeUInt32LE(RATE, 24);
wav.writeUInt32LE(RATE * 2, 28);
wav.writeUInt16LE(2, 32);
wav.writeUInt16LE(16, 34);
wav.write('data', 36);
wav.writeUInt32LE(n * 2, 40);
const phase = [0, 0, 0];
for (let i = 0; i < n; i += 1) {
  const chord = CHORDS[Math.floor(i / (2 * RATE)) % CHORDS.length];
  let value = 0;
  for (let k = 0; k < 3; k += 1) {
    phase[k] += (2 * Math.PI * chord[k]) / RATE;
    value += AMP * Math.sin(phase[k]);
  }
  wav.writeInt16LE(Math.round(value * 32767), 44 + i * 2);
}
writeFileSync(new URL('../../web/e2e/fixtures/video-studio/audio/music.wav', import.meta.url), wav);
console.log(`music.wav: ${wav.length} bytes`);
```

`spikes/video-studio-audio/make-fixtures.mjs`:

```js
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const OUT = new URL('../../web/e2e/fixtures/video-studio/audio/', import.meta.url);
const phrases = [1, 2, 3].map((i) =>
  readFileSync(new URL(`out/phrase-${i}.bin`, import.meta.url)).toString('base64'),
);
const probe = await openProbe('fixtures.html');
try {
  await probe.page.waitForFunction(() => 'buildFixtures' in window);
  const built = await probe.page.evaluate((p) => window.buildFixtures(p), phrases);
  mkdirSync(OUT, { recursive: true });
  writeFileSync(new URL('speech.wav', OUT), Buffer.from(built.speech, 'base64'));
  writeFileSync(new URL('speech-noisy.wav', OUT), Buffer.from(built.noisy, 'base64'));
  writeFileSync(new URL('speech.truth.json', OUT), `${JSON.stringify(built.truth, null, 2)}\n`);
  console.log(JSON.stringify(built.truth));
} finally {
  await probe.close();
}
```

`spikes/video-studio-audio/verify-fixtures.mjs`:

```js
// Reads the committed fixtures back and prints the levels the README promises.
import { readFileSync } from 'node:fs';

const DIR = new URL('../../web/e2e/fixtures/video-studio/audio/', import.meta.url);
const pcm = (name) => {
  const b = readFileSync(new URL(name, DIR));
  const n = (b.length - 44) / 2;
  return Float32Array.from({ length: n }, (_, i) => b.readInt16LE(44 + i * 2) / 32768);
};
const db = (x, a, b) => {
  let s = 0;
  for (let i = a; i < b; i += 1) s += x[i] ** 2;
  return (10 * Math.log10(s / Math.max(1, b - a) + 1e-20)).toFixed(2);
};
const truth = JSON.parse(readFileSync(new URL('speech.truth.json', DIR), 'utf8'));
const rate = truth.sampleRate;
const music = pcm('music.wav');
console.log(`music RMS ${db(music, 0, music.length)} dBFS (want -18.24)`);
for (const name of ['speech.wav', 'speech-noisy.wav']) {
  const x = pcm(name);
  const windows = truth.windows.map(([s, e]) => db(x, Math.round(s * rate), Math.round(e * rate)));
  const gaps = [];
  let from = 0;
  for (const [s, e] of truth.windows) {
    gaps.push(db(x, Math.round((from + 0.15) * rate), Math.round((s - 0.15) * rate)));
    from = e;
  }
  console.log(`${name}: speech windows ${windows.join(', ')} dBFS; gaps ${gaps.join(', ')} dBFS`);
}
```

- [ ] **Step 5: Run the generators (in WSL, one script file)**

```bash
cd /mnt/d/Aura/spikes/video-studio-audio
bash setup.sh
bash tts-phrases.sh
node make-music.mjs
node make-fixtures.mjs
bash setup.sh   # stages the fresh fixtures into public/
node verify-fixtures.mjs
```

Expected: `capabilities: {"tts":true,…}`, three phrase files of a few kB each; `music RMS -18.24 dBFS`; `speech.wav` gaps below -100 dBFS (trimmed TTS silence is digital zero); `speech-noisy.wav` gaps about 10 dB under its speech windows. If the VM answers `"tts":false`, stop and report: the speech fixtures cannot be made without TTS.

- [ ] **Step 6: Write the fixture provenance**

`web/e2e/fixtures/video-studio/audio/README.md`:

```markdown
# Audio fixtures for the Video Studio

16 kHz, mono, 16-bit PCM WAV. Made by `spikes/video-studio-audio/` on 2026-09-27.

| File | What it is | How it was made |
|---|---|---|
| `music.wav` | 8 s A-minor progression at a constant level: RMS −18.24 dBFS | `make-music.mjs`, three equal sines per chord, amplitude 0.1 |
| `speech.wav` | Three English phrases: 1.0 s lead silence, 1.5 s gaps, 1.0 s tail | The lab VM's own `POST /api/tts` (`tts-phrases.sh`), trimmed at −50 dBFS, peak-normalised to −3 dBFS (`make-fixtures.mjs`) |
| `speech-noisy.wav` | `speech.wav` plus seeded pink noise at 10 dB SNR | Paul Kellet's pink filter, seed 20260927, SNR measured over the speech windows |
| `speech.truth.json` | The speech windows in seconds, exact to the sample | Written by the same run; `windows` is the ground truth a VAD is scored on |

`verify-fixtures.mjs` prints the levels above from the committed bytes.
```

Also create `spikes/video-studio-audio/FINDINGS.md` with the header, filled by Tasks 2–6:

```markdown
# Video Studio audio — spike findings

Spec: `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md`. Probes run in headless
Chromium (Playwright 1.62.1, WSL) against the probe's own Vite server.

## Fixtures

(Task 1: paste the `verify-fixtures.mjs` output here.)
```

- [ ] **Step 7: Commit**

```bash
git add spikes/video-studio-audio/package.json spikes/video-studio-audio/.gitignore \
  spikes/video-studio-audio/vite.config.ts spikes/video-studio-audio/setup.sh \
  spikes/video-studio-audio/lib/browser.mjs spikes/video-studio-audio/src/wav.ts \
  spikes/video-studio-audio/src/fixtures.ts spikes/video-studio-audio/fixtures.html \
  spikes/video-studio-audio/tts-phrases.sh spikes/video-studio-audio/make-music.mjs \
  spikes/video-studio-audio/make-fixtures.mjs spikes/video-studio-audio/verify-fixtures.mjs \
  spikes/video-studio-audio/FINDINGS.md spikes/video-studio-audio/package-lock.json \
  web/e2e/fixtures/video-studio/audio
git commit -m "test(video-studio): add audio fixtures with a known speech ground truth" -- \
  spikes/video-studio-audio web/e2e/fixtures/video-studio/audio
```

The commit body says why: the E2E measures exported audio against these, and the VAD is scored against `speech.truth.json`. End with the `Co-Authored-By` trailer.

---

### Task 2: S1 — does VideoFlow apply animated volume, and in which time domain?

The docs say keyframe `time` values live in source media time (`node_modules/@videoflow/core/dist/types.d.ts:87-91`). But `clipOpacity` writes its keyframes from 0 to `clip.duration`, which is segment time (`web/src/videoStudio/videoflow.ts:75-94`). For a trimmed clip, one of the two is wrong, and the volume curve inherits whichever is right.

**Files:**
- Create: `spikes/video-studio-audio/s1.html`
- Create: `spikes/video-studio-audio/src/s1.ts`
- Create: `spikes/video-studio-audio/s1.mjs`
- Modify: `spikes/video-studio-audio/FINDINGS.md`

- [ ] **Step 1: Write the probe**

`s1.html`: the same shape as `fixtures.html`, loading `/src/s1.ts`.

`src/s1.ts`:

```ts
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
  const renderer = new BrowserRenderer(await flow.compile());
  try {
    const buffer = await renderer.renderAudio();
    if (buffer === null) throw new Error(`${item.name}: renderAudio returned nothing`);
    const db = levels(buffer);
    const reference = db[5] ?? 0;
    const drop = db.findIndex((level, i) => i > 5 && level <= reference - 9);
    return { name: item.name, dropAtTimeline: drop === -1 ? null : drop / 10, firstSecondDb: reference, levels: db };
  } finally {
    renderer.destroy();
  }
}

async function main() {
  const results = [];
  for (const item of CASES) results.push(await mix(item));
  Object.assign(window, { s1Results: results });
}
void main();
```

`s1.mjs`:

```js
import { writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const probe = await openProbe('s1.html');
try {
  await probe.page.waitForFunction(() => 's1Results' in window, null, { timeout: 120_000 });
  const results = await probe.page.evaluate(() => window.s1Results);
  writeFileSync(new URL('out/s1.json', import.meta.url), JSON.stringify(results, null, 2));
  for (const r of results) {
    console.log(`${r.name}: level ${r.firstSecondDb} dBFS, drop at timeline ${r.dropAtTimeline ?? 'none'} s`);
  }
} finally {
  await probe.close();
}
```

- [ ] **Step 2: Run it (WSL)**

```bash
cd /mnt/d/Aura/spikes/video-studio-audio && node s1.mjs
```

How to read the result:
- `static-0.5` should sit 6.02 dB (±0.2) under `static-1`.
- `step-untrimmed` should drop at 3.0 s.
- `step-trimmed-2s`: a drop at **1.0 s** means keyframe times are absolute source time (as the docs say); a drop at **3.0 s** means segment time (as `clipOpacity` assumes).
- `step-trimmed-2s-speed-2`: **0.5 s** means absolute source, **1.5 s** means segment source seconds, **3.0 s** means timeline seconds.

A result matching none of these is a finding: record it and stop.

- [ ] **Step 3: Record and commit**

Add to `FINDINGS.md` an `## S1` section: the five printed lines, the domain the numbers prove, and one consequence for Plan B. If the domain is absolute source time, `clipOpacity`'s fades are shifted on every trimmed clip. That is a live bug, to be fixed on touch in Plan B's T2 with its own test.

```bash
git commit -m "spike(video-studio): measure VideoFlow's keyframe time domain for volume" -- \
  spikes/video-studio-audio/s1.html spikes/video-studio-audio/src/s1.ts \
  spikes/video-studio-audio/s1.mjs spikes/video-studio-audio/FINDINGS.md
```

(`git add` the three new files first.)

---

### Task 3: S2 — wavesurfer inside a dnd-timeline item

The audio lane draws its waveform with wavesurfer from cached peaks and edits the envelope with wavesurfer's plugin. Both live inside a dnd-timeline item whose outer div carries dnd-kit's pointer listeners (`web/src/videoStudio/Timeline_items.tsx:226-240`), so an envelope drag may also start an item drag. This probe measures that, with and without a guard, plus the render time from peaks.

**Files:**
- Create: `spikes/video-studio-audio/s2.html`
- Create: `spikes/video-studio-audio/src/s2.tsx`
- Create: `spikes/video-studio-audio/s2.mjs`
- Modify: `spikes/video-studio-audio/FINDINGS.md`

- [ ] **Step 1: Write the probe page**

`s2.html`:

```html
<!doctype html>
<html>
  <body style="margin:0;background:#111;color:#eee;font-family:sans-serif">
    <div id="root"></div>
    <script type="module" src="/src/s2.tsx"></script>
  </body>
</html>
```

`src/s2.tsx`:

```tsx
import { TimelineContext, useItem, useRow, useTimelineContext, type Span } from 'dnd-timeline';
import { useEffect, useRef, useState, type PointerEvent } from 'react';
import { createRoot } from 'react-dom/client';
import WaveSurfer from 'wavesurfer.js';
import EnvelopePlugin from 'wavesurfer.js/plugins/envelope';

const SECONDS = 300;
const guarded = new URLSearchParams(location.search).get('guard') === '1';
const log: Record<string, unknown>[] = [];
Object.assign(window, { s2Log: log });

function peaks(count: number): Float32Array {
  const out = new Float32Array(count);
  for (let i = 0; i < count; i += 1) out[i] = 0.2 + 0.6 * Math.abs(Math.sin(i / 37));
  return out;
}

function Waveform({ duration }: { duration: number }) {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (host.current === null) return undefined;
    const started = performance.now();
    const envelope = EnvelopePlugin.create({
      points: [
        { time: 60, volume: 1 },
        { time: 120, volume: 0.3 },
        { time: 240, volume: 0.3 },
      ],
      lineColor: '#f5b041',
      dragPointSize: 14,
    });
    const ws = WaveSurfer.create({
      container: host.current,
      height: 48,
      peaks: [peaks(4000)],
      duration,
      interact: false,
      cursorWidth: 0,
      waveColor: '#2e9e8f',
      normalize: true,
      plugins: [envelope],
    });
    ws.on('redrawcomplete', () => log.push({ event: 'redrawcomplete', ms: performance.now() - started }));
    envelope.on('points-change', (points) =>
      log.push({ event: 'points-change', points: points.map((p) => [Math.round(p.time), Number(p.volume.toFixed(2))]) }),
    );
    return () => ws.destroy();
  }, [duration]);
  // The guard under test: a press that lands on the envelope's SVG never reaches dnd-kit.
  const stop = (event: PointerEvent) => {
    if (guarded && (event.target as Element).closest('svg') !== null) event.stopPropagation();
  };
  return <div ref={host} data-testid="waveform" onPointerDown={stop} style={{ width: '100%' }} />;
}

function AudioItem({ span }: { span: Span }) {
  const { setNodeRef, attributes, listeners, itemStyle, itemContentStyle } = useItem({
    id: 'music',
    span,
    resizeHandleWidth: 44,
  });
  return (
    <div ref={setNodeRef} style={itemStyle} onPointerDown={listeners.onPointerDown} onPointerMove={listeners.onPointerMove}>
      <div style={itemContentStyle}>
        <div {...attributes} data-testid="item" style={{ width: '100%', background: '#1d2b33', borderRadius: 6 }}>
          <Waveform duration={span.end - span.start} />
        </div>
      </div>
    </div>
  );
}

function Lane({ children }: { children: React.ReactNode }) {
  const { setNodeRef, rowWrapperStyle, rowStyle } = useRow({ id: 'audio-lane' });
  return (
    <div style={{ ...rowWrapperStyle, width: '100%' }}>
      <div ref={setNodeRef} style={{ ...rowStyle, minHeight: 64 }}>
        {children}
      </div>
    </div>
  );
}

function Lanes({ span }: { span: Span }) {
  const { style, setTimelineRef } = useTimelineContext();
  return (
    <div ref={setTimelineRef} style={{ ...style, width: 1100 }}>
      <Lane>
        <AudioItem span={span} />
      </Lane>
    </div>
  );
}

function App() {
  const [span, setSpan] = useState<Span>({ start: 20, end: 20 + SECONDS });
  const range = { start: 0, end: 400 };
  return (
    <TimelineContext
      range={range}
      sidebarWidth={0}
      onRangeChanged={() => undefined}
      onDragEnd={(event) => {
        const next = event.active.data.current.getSpanFromDragEvent?.(event);
        log.push({ event: 'item-drag-end', start: next?.start ?? null });
        if (next !== null && next !== undefined) setSpan(next);
      }}
    >
      <Lanes span={span} />
    </TimelineContext>
  );
}

createRoot(document.getElementById('root') as HTMLElement).render(<App />);
```

- [ ] **Step 2: Write the runner**

`s2.mjs`:

```js
import { writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

async function run(guard, mobile) {
  const probe = await openProbe(`s2.html?guard=${guard ? 1 : 0}`, { mobile });
  const { page } = probe;
  try {
    await page.waitForFunction(() => window.s2Log.some((e) => e.event === 'redrawcomplete'), null, { timeout: 30_000 });
    // The second envelope point sits at t=120 of a 300 s item that starts at 20 on a 0–400 range.
    const circle = page.locator('[data-testid="waveform"] svg circle').nth(1);
    const box = await circle.boundingBox();
    if (box === null) throw new Error('no envelope point drawn');
    const x = box.x + box.width / 2;
    const y = box.y + box.height / 2;
    await page.mouse.move(x, y);
    await page.mouse.down();
    for (const dy of [4, 8, 12, 16]) await page.mouse.move(x, y + dy, { steps: 3 });
    await page.mouse.up();
    await page.waitForTimeout(300);
    const afterPoint = await page.evaluate(() => window.s2Log.slice());
    // Then a drag on the item body, away from any point: this one MUST move the item.
    const item = await page.locator('[data-testid="item"]').boundingBox();
    const bx = item.x + 30;
    const by = item.y + 6;
    await page.mouse.move(bx, by);
    await page.mouse.down();
    for (const dx of [10, 30, 60, 90]) await page.mouse.move(bx + dx, by, { steps: 3 });
    await page.mouse.up();
    await page.waitForTimeout(300);
    const all = await page.evaluate(() => window.s2Log.slice());
    return {
      guard,
      mobile,
      renderMs: all.find((e) => e.event === 'redrawcomplete')?.ms,
      pointDragMovedPoint: afterPoint.some((e) => e.event === 'points-change'),
      pointDragMovedItem: afterPoint.some((e) => e.event === 'item-drag-end'),
      bodyDragMovedItem: all.slice(afterPoint.length).some((e) => e.event === 'item-drag-end'),
    };
  } finally {
    await probe.close();
  }
}

const results = [];
for (const mobile of [false, true]) for (const guard of [false, true]) results.push(await run(guard, mobile));
writeFileSync(new URL('out/s2.json', import.meta.url), JSON.stringify(results, null, 2));
console.table(results);
```

- [ ] **Step 3: Run it (WSL) and read the table**

```bash
cd /mnt/d/Aura/spikes/video-studio-audio && node s2.mjs
```

Pass condition (spec S2) on the guarded rows, desktop and mobile: `pointDragMovedPoint` true, `pointDragMovedItem` false, `bodyDragMovedItem` true, and `renderMs` under 200. Record the unguarded rows too: they say whether the guard is needed at all. If no configuration passes, write that down and stop for the operator; do not write a waveform component.

- [ ] **Step 4: Record and commit**

`FINDINGS.md` `## S2`: the table and the verdict. Commit `s2.html`, `src/s2.tsx`, `s2.mjs` and `FINDINGS.md` with `spike(video-studio): measure wavesurfer envelope inside a dnd-timeline item`.

---

### Task 4: S3 — which noise suppressor, and how much does it clean?

**Files:**
- Create: `spikes/video-studio-audio/s3.html`
- Create: `spikes/video-studio-audio/src/s3.ts`
- Create: `spikes/video-studio-audio/s3.mjs`
- Modify: `spikes/video-studio-audio/FINDINGS.md`

- [ ] **Step 1: Write the probe**

`s3.html`: the `fixtures.html` shape, loading `/src/s3.ts`.

`src/s3.ts`:

```ts
import {
  GtcrnWorkletNode,
  loadGtcrn,
  loadRnnoise,
  loadSpeex,
  RnnoiseWorkletNode,
  SpeexWorkletNode,
} from '@sapphi-red/web-noise-suppressor';
import gtcrnWasm from '@sapphi-red/web-noise-suppressor/gtcrn.wasm?url';
import gtcrnWorklet from '@sapphi-red/web-noise-suppressor/gtcrnWorklet.js?url';
import rnnoiseWasm from '@sapphi-red/web-noise-suppressor/rnnoise.wasm?url';
import rnnoiseSimdWasm from '@sapphi-red/web-noise-suppressor/rnnoise_simd.wasm?url';
import rnnoiseWorklet from '@sapphi-red/web-noise-suppressor/rnnoiseWorklet.js?url';
import speexWasm from '@sapphi-red/web-noise-suppressor/speex.wasm?url';
import speexWorklet from '@sapphi-red/web-noise-suppressor/speexWorklet.js?url';
import { decodeMono, rmsDb } from './wav';

// RNNoise is a 48 kHz model; the context runs there and upsamples the 16 kHz fixture itself.
const RATE = 48000;
const MARGIN = 0.15;

type Maker = (ctx: OfflineAudioContext) => Promise<AudioNode | null>;
const MAKERS: Record<string, Maker> = {
  none: () => Promise.resolve(null),
  rnnoise: async (ctx) => {
    const wasmBinary = await loadRnnoise({ url: rnnoiseWasm, simdUrl: rnnoiseSimdWasm });
    await ctx.audioWorklet.addModule(rnnoiseWorklet);
    return new RnnoiseWorkletNode(ctx, { wasmBinary, maxChannels: 1 });
  },
  speex: async (ctx) => {
    const wasmBinary = await loadSpeex({ url: speexWasm });
    await ctx.audioWorklet.addModule(speexWorklet);
    return new SpeexWorkletNode(ctx, { wasmBinary, maxChannels: 1 });
  },
  gtcrn: async (ctx) => {
    const wasmBinary = await loadGtcrn({ url: gtcrnWasm });
    await ctx.audioWorklet.addModule(gtcrnWorklet);
    return new GtcrnWorkletNode(ctx, { wasmBinary, maxChannels: 1 });
  },
};

async function fixture(name: string): Promise<Float32Array> {
  const bytes = new Uint8Array(await (await fetch(`/fixtures/${name}`)).arrayBuffer());
  return decodeMono(bytes, RATE);
}

function score(out: Float32Array, windows: readonly [number, number][]) {
  const at = (seconds: number) => Math.round(seconds * RATE);
  const speech = windows.map(([s, e]) => rmsDb(out, at(s + 0.05), at(e - 0.05)));
  const gaps: number[] = [];
  let from = 0;
  for (const [s, e] of windows) {
    gaps.push(rmsDb(out, at(from + MARGIN), at(s - MARGIN)));
    from = e;
  }
  const mean = (xs: number[]) => xs.reduce((a, b) => a + b, 0) / xs.length;
  return { speechDb: Number(mean(speech).toFixed(2)), gapDb: Number(mean(gaps).toFixed(2)) };
}

async function process(file: string, maker: Maker, windows: readonly [number, number][]) {
  const input = await fixture(file);
  const ctx = new OfflineAudioContext(1, input.length, RATE);
  const buffer = ctx.createBuffer(1, input.length, RATE);
  buffer.copyToChannel(input, 0);
  const source = ctx.createBufferSource();
  source.buffer = buffer;
  const node = await maker(ctx);
  if (node === null) source.connect(ctx.destination);
  else {
    source.connect(node);
    node.connect(ctx.destination);
  }
  source.start();
  const started = performance.now();
  const out = (await ctx.startRendering()).getChannelData(0);
  const seconds = (performance.now() - started) / 1000;
  return { ...score(out, windows), realtime: Number((input.length / RATE / seconds).toFixed(1)) };
}

async function main() {
  const truth = (await (await fetch('/fixtures/speech.truth.json')).json()) as { windows: [number, number][] };
  const results = [];
  for (const [name, maker] of Object.entries(MAKERS)) {
    results.push({
      name,
      noisy: await process('speech-noisy.wav', maker, truth.windows),
      clean: await process('speech.wav', maker, truth.windows),
    });
  }
  Object.assign(window, { s3Results: results });
}
void main();
```

`s3.mjs`:

```js
import { writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const probe = await openProbe('s3.html');
try {
  await probe.page.waitForFunction(() => 's3Results' in window, null, { timeout: 300_000 });
  const results = await probe.page.evaluate(() => window.s3Results);
  const base = results.find((r) => r.name === 'none');
  const rows = results.map((r) => ({
    suppressor: r.name,
    'noise floor drop (dB)': (base.noisy.gapDb - r.noisy.gapDb).toFixed(1),
    'speech loss on noisy (dB)': (base.noisy.speechDb - r.noisy.speechDb).toFixed(1),
    'speech loss on clean (dB)': (base.clean.speechDb - r.clean.speechDb).toFixed(1),
    '× real time': r.noisy.realtime,
  }));
  const wasm = probe.assets.filter((a) => a.url.endsWith('.wasm'));
  writeFileSync(new URL('out/s3.json', import.meta.url), JSON.stringify({ results, wasm }, null, 2));
  console.table(rows);
  console.table(wasm);
} finally {
  await probe.close();
}
```

- [ ] **Step 2: Run it (WSL)**

```bash
cd /mnt/d/Aura/spikes/video-studio-audio && node s3.mjs
```

Pass condition (spec S3): at least one suppressor gives a noise-floor drop well above 0 dB, costs under 3 dB of speech on both files, and runs faster than real time (`× real time` > 1). Pick the one with the largest floor drop among those. Its measured drop, minus 3 dB of headroom, becomes Task T6's E2E threshold in Plan B. If none passes, record it and stop.

- [ ] **Step 3: Record and commit**

`FINDINGS.md` `## S3`: both tables, the choice, the T6 threshold, and each `.wasm` size (the bytes the committed dist gains). Commit `s3.html`, `src/s3.ts`, `s3.mjs` and `FINDINGS.md` with `spike(video-studio): compare RNNoise, Speex and GTCRN offline on noisy speech`.

---

### Task 5: S4 — Silero or WebRTC VAD?

**Files:**
- Create: `spikes/video-studio-audio/s4.html`
- Create: `spikes/video-studio-audio/src/s4.ts`
- Create: `spikes/video-studio-audio/s4.mjs`
- Modify: `spikes/video-studio-audio/FINDINGS.md`

- [ ] **Step 1: Write the probe**

`s4.html`: the `fixtures.html` shape, loading `/src/s4.ts`.

`src/s4.ts`:

```ts
import fvadInit from '@echogarden/fvad-wasm';
import fvadWasm from '@echogarden/fvad-wasm/fvad.wasm?url';
import { NonRealTimeVAD } from '@ricky0123/vad-web';
import { decodeMono } from './wav';

const RATE = 16000;
// Both detectors get the same smoothing so their edges are comparable: a pause shorter than
// MERGE_MS joins its neighbours, a window shorter than MIN_MS is dropped.
const MERGE_MS = 300;
const MIN_MS = 250;
type Window = [number, number];

function smooth(windows: Window[]): Window[] {
  const merged: Window[] = [];
  for (const w of windows) {
    const last = merged.at(-1);
    if (last !== undefined && w[0] - last[1] < MERGE_MS / 1000) last[1] = w[1];
    else merged.push([w[0], w[1]]);
  }
  return merged.filter(([s, e]) => e - s >= MIN_MS / 1000);
}

async function silero(samples: Float32Array): Promise<Window[]> {
  const vad = await NonRealTimeVAD.new({
    modelURL: '/vad/silero_vad_legacy.onnx',
    ortConfig: (ort) => {
      ort.env.wasm.wasmPaths = '/ort/';
      ort.env.wasm.numThreads = 1;
    },
    redemptionMs: MERGE_MS,
    minSpeechMs: MIN_MS,
    preSpeechPadMs: 0,
  });
  const out: Window[] = [];
  for await (const { start, end } of vad.run(samples, RATE)) out.push([start / 1000, end / 1000]);
  return smooth(out);
}

async function webrtc(samples: Float32Array, mode: 0 | 1 | 2 | 3): Promise<Window[]> {
  const m = await fvadInit({ locateFile: () => fvadWasm });
  const handle = m._fvad_new();
  if (m._fvad_set_sample_rate(handle, RATE) !== 0) throw new Error('fvad: sample rate refused');
  if (m._fvad_set_mode(handle, mode) !== 0) throw new Error('fvad: mode refused');
  const frame = (RATE * 30) / 1000;
  const pointer = m._malloc(frame * 2);
  // The Emscripten heap view; its name is what this probe checks the build actually exports.
  const heap: Int16Array = m.HEAP16 ?? new Int16Array(m.HEAPU8.buffer);
  const windows: Window[] = [];
  for (let at = 0; at + frame <= samples.length; at += frame) {
    for (let i = 0; i < frame; i += 1) {
      heap[pointer / 2 + i] = Math.round(Math.max(-1, Math.min(1, samples[at + i] ?? 0)) * 32767);
    }
    if (m._fvad_process(handle, pointer, frame) === 1) windows.push([at / RATE, (at + frame) / RATE]);
  }
  m._free(pointer);
  m._fvad_free(handle);
  return smooth(windows);
}

function edges(found: Window[], truth: Window[]) {
  const errors = truth.map(([ts, te]) => {
    const hit = found.find(([s, e]) => s < te && e > ts);
    return hit === undefined ? null : [Math.round((hit[0] - ts) * 1000), Math.round((hit[1] - te) * 1000)];
  });
  const falseWindows = found.filter(([s, e]) => !truth.some(([ts, te]) => s < te && e > ts)).length;
  return { errorsMs: errors, misses: errors.filter((e) => e === null).length, falseWindows };
}

async function main() {
  const truth = ((await (await fetch('/fixtures/speech.truth.json')).json()) as { windows: Window[] }).windows;
  const results = [];
  for (const file of ['speech.wav', 'speech-noisy.wav']) {
    const bytes = new Uint8Array(await (await fetch(`/fixtures/${file}`)).arrayBuffer());
    const samples = await decodeMono(bytes, RATE);
    results.push({ file, detector: 'silero', ...edges(await silero(samples), truth) });
    for (const mode of [0, 1, 2, 3] as const) {
      results.push({ file, detector: `webrtc-mode-${mode}`, ...edges(await webrtc(samples, mode), truth) });
    }
  }
  Object.assign(window, { s4Results: results });
}
void main();
```

`s4.mjs`:

```js
import { writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const probe = await openProbe('s4.html');
try {
  await probe.page.waitForFunction(() => 's4Results' in window, null, { timeout: 300_000 });
  const results = await probe.page.evaluate(() => window.s4Results);
  const heavy = probe.assets.filter((a) => /\.(wasm|onnx|mjs)(\?|$)/.test(a.url));
  const origins = [...new Set(probe.assets.map((a) => new URL(a.url).origin))];
  writeFileSync(new URL('out/s4.json', import.meta.url), JSON.stringify({ results, heavy, origins }, null, 2));
  console.table(results.map((r) => ({ ...r, errorsMs: JSON.stringify(r.errorsMs) })));
  console.table(heavy);
  console.log('origins:', origins);
} finally {
  await probe.close();
}
```

- [ ] **Step 2: Run it (WSL)**

```bash
cd /mnt/d/Aura/spikes/video-studio-audio && node s4.mjs
```

`origins` must list only `http://127.0.0.1:5199`: anything else means a package reached a CDN despite the options, and that is a finding. If the probe throws on `m.HEAP16`/`m.HEAPU8`, print `Object.keys(m)` from the page and record which heap export the build has. That is the question this line exists to answer.

Decision rule (spec S4): take the best WebRTC mode and compare it with Silero. The lighter detector (by summed `heavy` bytes) wins unless its edges miss by more than 150 ms, or it misses a window, where the other does not.

- [ ] **Step 3: Record and commit**

`FINDINGS.md` `## S4`: both tables, the origins, the bytes each detector adds to the dist, and the choice. Commit `s4.html`, `src/s4.ts`, `s4.mjs` and `FINDINGS.md` with `spike(video-studio): compare Silero and WebRTC VAD on the speech fixtures`.

---

### Task 6: M1, the PRD amendment, and the spec's numbers

**Files:**
- Modify: `spikes/video-studio-audio/FINDINGS.md`
- Modify: `prd.md` (after the video editor paragraphs, currently ending near line 578)
- Modify: `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md` (the Spikes table: add the measured result to each row)

- [ ] **Step 1: Measure M1 on the VM (read-only)**

Write `~/aura-vm/m1.sh` in WSL. It lives outside the repo because it runs with the VM's ssh password (given by the operator for this lab VM) from `VM_SSH_PASSWORD`. `sudo -S` eats stdin, so the SQL travels as a file: scp to the host, then `docker cp` into the container.

```bash
#!/usr/bin/env bash
set -euo pipefail
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cat > "$WORK/m1.sql" <<'SQL'
\d aura.assets
SELECT status,
       count(*) AS assets,
       count(*) FILTER (WHERE NULLIF(document_id::text, '') IS NOT NULL) AS with_document
FROM aura.assets
WHERE mime_type = 'application/json' AND file_name LIKE '%.json'
GROUP BY status
ORDER BY status;
SQL
VM=aura@192.168.101.158
SSH=(sshpass -p "$VM_SSH_PASSWORD" ssh -o StrictHostKeyChecking=no "$VM")
sshpass -p "$VM_SSH_PASSWORD" scp -o StrictHostKeyChecking=no "$WORK/m1.sql" "$VM:/tmp/m1.sql"
# $VM_SSH_PASSWORD expands here; \$POSTGRES_* expands inside the container.
"${SSH[@]}" "echo '$VM_SSH_PASSWORD' | sudo -S -p '' docker cp /tmp/m1.sql aura-postgres:/tmp/m1.sql && \
  echo '$VM_SSH_PASSWORD' | sudo -S -p '' docker exec aura-postgres sh -c 'psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -f /tmp/m1.sql'; \
  rm -f /tmp/m1.sql"
```

The `\d` output comes first. If `document_id`, `mime_type` or `file_name` is not in it, rewrite the query from the real column list; do not guess. Record the rows in `FINDINGS.md` `## M1`. JSON assets with `document_id` set, or in `searchable`/`complete`, mean saved projects are indexed. That goes to the operator as its own item; it is **not** fixed here.

- [ ] **Step 2: Write the PRD amendment**

Append after the video editor paragraphs in `prd.md` one paragraph, in the style of its neighbours:

- measured 2026-09-27;
- the keyframe time domain (S1);
- the chosen suppressor with its floor drop and speed (S3);
- the chosen VAD with its edge error and bytes (S4);
- the wavesurfer verdict (S2);
- the `FinalizeMedia` rule;
- a closing sentence on what the measurement does NOT establish, copied from the spec's §What this does not prove plus anything the spikes added.

Every number comes from `FINDINGS.md`.

- [ ] **Step 3: Update the spec's Spikes table with the results, then commit**

```bash
git commit -m "docs(video-studio): record the audio spike measurements" -- \
  prd.md docs/superpowers/specs/2026-09-27-video-studio-audio-design.md \
  spikes/video-studio-audio/FINDINGS.md
```

Stop here and report to the operator: the four verdicts, M1, and the Plan B thresholds. Plan B is written from them.

---

### Task 7: T1a — editor sources are finalized without processing, and audio is filed under `media/`

**Files:**
- Create: `internal/assets/service_accept.go` (moved from `service.go`: `Finalize`, `ErrWrongModality`, `FinalizeUnprocessed`, `storedSizeMatchesUpload`, `accept`; new `FinalizeMedia`)
- Modify: `internal/assets/service.go:149-169,197-209,219-258` (the moved code goes; `folderFor` gains audio)
- Modify: `internal/assets/service_accept_test.go`
- Modify: `internal/agui/asset_service.go` (interface gains `FinalizeMedia`)
- Modify: `internal/agui/assets_api.go:118-126` (`handleAssetFinalize` reads `use`)
- Modify: `internal/agui/assets_api_test.go` (fake gains `FinalizeMedia` and counters; new test)
- Modify: `web/src/chat/attachments/api.ts` (`finalizeMediaAsset`)
- Modify: `web/src/chat/attachments/__tests__/api.test.ts`
- Modify: `web/src/videoStudio/VideoStudio_sources.ts:1,186` (`uploadSource` uses `finalizeMediaAsset`)
- Modify: `web/src/videoStudio/__tests__/VideoStudio.test.tsx:62,156,226`
- Modify: `web/e2e/support/assetUpload.ts` (`uploadBytes`, `use` option)
- Create: `web/e2e/support/assetCleanup.ts`
- Create: `web/e2e/video-studio-audio.spec.ts`

**Interfaces:**
- Produces (Go): `func (s *Service) FinalizeMedia(ctx context.Context, identityID, assetID string) (Asset, error)`. It accepts image, video or audio, and enqueues nothing. For any other modality it returns `ErrWrongModality`.
- Produces (HTTP): `POST /api/assets/{id}/finalize?use=media`. An unknown `use` answers 400.
- Produces (web): `finalizeMediaAsset(id: string): Promise<Asset>`.
- Produces (e2e): `uploadBytes(page, bytes, fileName, mimeType, { use? })`, `uploadAsset(page, path, fileName, mimeType, { use? })`, `trackCreatedAssets(page) → { ids(): string[] }`, `deleteAssets(page, ids)`, `readAsset(page, id) → { status, modality, object_key, summary }`.

- [ ] **Step 1: Write the failing Go service tests**

Append to `internal/assets/service_accept_test.go` (add `"errors"` to the imports):

```go
// presignAndStore presigns fileName and puts body where the presign said, the way a browser does.
func presignAndStore(t *testing.T, svc *Service, fileName, mimeType, body string) Asset {
	t.Helper()
	resp, err := svc.Presign(context.Background(), PresignRequest{
		IdentityID:        serviceIdentityID,
		SourceKind:        SourceWeb,
		ThreadID:          "thread-1",
		FileName:          fileName,
		MIMEType:          mimeType,
		DeclaredSizeBytes: int64(len(body)),
	})
	if err != nil {
		t.Fatalf("Presign() error = %v", err)
	}
	ref := objectstore.ObjectRef{Bucket: resp.Asset.ObjectBucket, Key: resp.Asset.ObjectKey}
	if _, err := svc.Objects.Put(context.Background(), ref, strings.NewReader(body), objectstore.PutOptions{
		MIMEType: mimeType,
		Size:     int64(len(body)),
	}); err != nil {
		t.Fatalf("Put object: %v", err)
	}
	return resp.Asset
}

// An editor source is accepted and left alone. The plain finalize would run the modality's
// processor, and for audio that is speech-to-text over a music bed.
func TestServiceFinalizeMediaAcceptsMediaWithoutProcessing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fileName string
		mimeType string
		want     Modality
	}{
		{name: "a sound", fileName: "bed.wav", mimeType: "audio/wav", want: ModalityAudio},
		{name: "a clip", fileName: "clip.mp4", mimeType: "video/mp4", want: ModalityVideo},
		{name: "a picture", fileName: "panel.png", mimeType: "image/png", want: ModalityImage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newAssetServiceTestRig(t, Limits{
				MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100, MaxVideoBytes: 100,
			})
			queue := &recordingProcessingQueue{}
			svc.ProcessingJobs = queue
			presigned := presignAndStore(t, svc, tc.fileName, tc.mimeType, "RIFFxxxx")

			accepted, err := svc.FinalizeMedia(context.Background(), serviceIdentityID, presigned.ID)
			if err != nil {
				t.Fatalf("FinalizeMedia() error = %v", err)
			}
			if accepted.Status != StatusAccepted || accepted.Modality != tc.want {
				t.Fatalf("accepted = status %q modality %q, want %q %q", accepted.Status, accepted.Modality, StatusAccepted, tc.want)
			}
			if queue.calls != 0 {
				t.Fatalf("processing enqueued %d times, want none", queue.calls)
			}
		})
	}
}

// A document through the editor's door is refused before a byte is read, and nothing is queued:
// the door must not become a way to file a PDF without the index knowing.
func TestServiceFinalizeMediaRefusesADocument(t *testing.T) {
	svc, store := newAssetServiceTestRig(t, Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100})
	queue := &recordingProcessingQueue{}
	svc.ProcessingJobs = queue
	presigned := presignAndStore(t, svc, "manual.pdf", "application/pdf", "%PDF test")

	_, err := svc.FinalizeMedia(context.Background(), serviceIdentityID, presigned.ID)
	if !errors.Is(err, ErrWrongModality) {
		t.Fatalf("FinalizeMedia() error = %v, want ErrWrongModality", err)
	}
	if got := store.assets[presigned.ID].Status; got == StatusAccepted {
		t.Fatalf("status = %q, want the document left unaccepted", got)
	}
	if queue.calls != 0 {
		t.Fatalf("processing enqueued %d times, want none", queue.calls)
	}
}
```

Extend `TestServicePresignPutsMediaInItsOwnFolder` (`service_accept_test.go:104-106`) with one row:

```go
		{name: "a sound", fileName: "bed.wav", mimeType: "audio/wav", want: "media/"},
```

- [ ] **Step 2: Run them and watch them fail (WSL)**

```bash
cd /mnt/d/Aura && go test ./internal/assets/ -run 'TestServiceFinalizeMedia|TestServicePresignPutsMediaInItsOwnFolder' -count=1
```

Expected: a compile failure, `svc.FinalizeMedia undefined`. Once the method exists as a stub, the "a sound" presign row fails with `ObjectKey = "chat/…", want the "media/" folder`.

- [ ] **Step 3: Move the accept code and implement `FinalizeMedia`**

Create `internal/assets/service_accept.go` holding the moved `Finalize`, `ErrWrongModality`, `FinalizeUnprocessed`, `storedSizeMatchesUpload` and `accept`, with `accept` taking the modalities it allows:

```go
package assets

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/identityctx"
)

func (s *Service) Finalize(ctx context.Context, identityID, assetID string) (Asset, error) {
	asset, err := s.accept(ctx, identityID, assetID)
	if err != nil {
		return asset, err
	}
	if err := s.enqueueProcessing(ctx, asset); err != nil {
		updated, _ := s.Store.SetStatus(ctx, asset.ID, identityID, StatusFailed, "processing_enqueue_failed", err.Error())
		return updated, err
	}
	return asset, nil
}

// ErrWrongModality refuses an asset finalized for a use that needs another modality.
var ErrWrongModality = errors.New("assets: the asset is not of the expected modality")

// FinalizeUnprocessed accepts an upload that is the input of one generation rather than
// knowledge: the same checks as Finalize, and no processing, so no vision summary is paid for
// and nothing is filed for the index. The asset must be of modality.
func (s *Service) FinalizeUnprocessed(ctx context.Context, identityID, assetID string, modality Modality) (Asset, error) {
	return s.accept(ctx, identityID, assetID, modality)
}

// FinalizeMedia accepts an editor source: a picture, a clip or a sound the operator will cut.
// Same checks as Finalize and no processing. For audio the processor is speech-to-text, and a
// music bed transcribed on the appliance's CPU on every upload says nothing to anyone.
func (s *Service) FinalizeMedia(ctx context.Context, identityID, assetID string) (Asset, error) {
	return s.accept(ctx, identityID, assetID, ModalityImage, ModalityVideo, ModalityAudio)
}
```

Move `storedSizeMatchesUpload` unchanged. Move `accept`, changing only its signature, its doc sentence and the modality check:

```go
// accept takes an uploaded object through every check that makes it an asset — it exists, it
// is within the limits, its bytes are what its name claims — and stops at accepted. No allowed
// modality accepts any; otherwise a mismatch is refused before a byte is read.
func (s *Service) accept(ctx context.Context, identityID, assetID string, allowed ...Modality) (Asset, error) {
	if s.Store == nil || s.Objects == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	asset, err := s.Store.GetForIdentity(ctx, assetID, identityID)
	if err != nil {
		return Asset{}, err
	}
	if len(allowed) > 0 && !slices.Contains(allowed, asset.Modality) {
		return Asset{}, ErrWrongModality
	}
	// … the rest of the body exactly as it is today (service.go:233-257).
}
```

In `service.go`, delete the moved blocks and change `folderFor`:

```go
func folderFor(modality Modality) objectstore.AssetFolder {
	switch modality {
	case ModalityImage, ModalityVideo, ModalityAudio:
		return objectstore.FolderMedia
	default:
		return objectstore.FolderChat
	}
}
```

Update its comment to "pictures, clips and sounds". Also update the `FolderMedia` comment in `internal/objectstore/asset_placement.go:56`, which says "pictures and clips". Then run `goimports -w internal/assets/service.go internal/assets/service_accept.go`.

- [ ] **Step 4: Run the service tests green (WSL)**

```bash
cd /mnt/d/Aura && go test ./internal/assets/ -count=1 && go vet ./internal/assets/ ./internal/objectstore/
```

Expected: PASS, including every existing Finalize/FinalizeUnprocessed test. `wc -l internal/assets/service.go` is now under 520.

- [ ] **Step 5: Write the failing handler test**

In `internal/agui/assets_api_test.go`, give the fake counters and the new method:

```go
func (f *fakeAssetService) Finalize(context.Context, string, string) (assets.Asset, error) {
	f.finalizeCalls++
	return f.getResp, f.getErr
}

func (f *fakeAssetService) FinalizeMedia(context.Context, string, string) (assets.Asset, error) {
	f.finalizeMediaCalls++
	return f.getResp, f.getErr
}
```

(add `finalizeCalls, finalizeMediaCalls int` to the struct), and the test:

```go
// The editor's finalize is a query on the same route, so it keeps the route's capability gate
// and idempotency metadata. A use nobody defined is refused rather than read as the default,
// which would quietly queue speech-to-text.
func TestAssetFinalizeRoutesTheMediaUse(t *testing.T) {
	for _, tc := range []struct {
		name      string
		target    string
		wantCode  int
		wantPlain int
		wantMedia int
	}{
		{name: "plain finalize processes", target: "/api/assets/asset-1/finalize", wantCode: http.StatusOK, wantPlain: 1},
		{name: "media finalize does not", target: "/api/assets/asset-1/finalize?use=media", wantCode: http.StatusOK, wantMedia: 1},
		{name: "an unknown use is refused", target: "/api/assets/asset-1/finalize?use=everything", wantCode: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assetSvc := &fakeAssetService{getResp: assets.Asset{ID: "asset-1"}}
			s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
			s.SetAssetService(assetSvc)
			req := withPrincipal(httptest.NewRequest(http.MethodPost, tc.target, nil), assetAPIIdentityID)
			rec := httptest.NewRecorder()

			s.Mux().ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if assetSvc.finalizeCalls != tc.wantPlain || assetSvc.finalizeMediaCalls != tc.wantMedia {
				t.Fatalf("Finalize calls = %d, FinalizeMedia calls = %d; want %d and %d",
					assetSvc.finalizeCalls, assetSvc.finalizeMediaCalls, tc.wantPlain, tc.wantMedia)
			}
		})
	}
}
```

Run `go test ./internal/agui/ -run TestAssetFinalizeRoutesTheMediaUse -count=1`: it fails to compile until the interface has `FinalizeMedia`, then fails on the media row until the handler reads `use`.

- [ ] **Step 6: Implement the handler and the interface**

`internal/agui/asset_service.go`, after `Finalize`:

```go
	// FinalizeMedia accepts an editor source (picture, clip, sound) without running its
	// processor: `POST /api/assets/{id}/finalize?use=media`.
	FinalizeMedia(context.Context, string, string) (assets.Asset, error)
```

`internal/agui/assets_api.go`:

```go
// handleAssetFinalize accepts an upload. `?use=media` is the editor's door: a source the operator
// will cut is accepted and left alone, where the plain call runs the modality's processor.
func (s *Server) handleAssetFinalize(w http.ResponseWriter, r *http.Request) {
	use := r.URL.Query().Get("use")
	asset, ok := s.callAssetMutation(w, r, func(ctx context.Context, identityID, id string) (assets.Asset, error) {
		switch use {
		case "":
			return s.assets.Finalize(ctx, identityID, id)
		case "media":
			return s.assets.FinalizeMedia(ctx, identityID, id)
		default:
			return assets.Asset{}, fmt.Errorf("unknown finalize use %q", use)
		}
	})
	if !ok {
		return
	}
	writeJSON(w, asset)
}
```

Add `"fmt"` to the imports if missing. Check that `sanitizeErr` passes the refusal text through; if it rewrites it, keep whatever it returns. The status code is what the test pins.

- [ ] **Step 7: Run AG-UI and race green (WSL)**

```bash
cd /mnt/d/Aura && go test ./internal/agui/ -count=1 && go vet ./internal/agui/ && \
  CGO_ENABLED=1 go test -race ./internal/assets/ ./internal/agui/ -count=1 && go build ./cmd/aura/
```

Expected: PASS. `cmd/aura` builds, because `*assets.Service` now satisfies the widened interface.

- [ ] **Step 8: Write the failing web tests**

`web/src/chat/attachments/__tests__/api.test.ts`: import `finalizeMediaAsset` and, in `encodes ids for asset routes`, call it right after `finalizeAsset('asset/1')`. Then expect `'/api/assets/asset%2F1/finalize?use=media'` second in the URL list.

`web/src/videoStudio/__tests__/VideoStudio.test.tsx`:
- line 62 becomes `const assets = vi.hoisted(() => ({ presignAsset: vi.fn(), finalizeAsset: vi.fn(), finalizeMediaAsset: vi.fn() }));`;
- line 156 becomes `assets.finalizeMediaAsset.mockResolvedValue({ id: 'asset-new' });`;
- line 226 becomes `expect(assets.finalizeMediaAsset).toHaveBeenCalledWith('asset-new');`;
- add `expect(assets.finalizeAsset).not.toHaveBeenCalled();`.

Run in WSL:

```bash
cd /mnt/d/Aura/web && ./node_modules/.bin/vitest run src/chat/attachments/__tests__/api.test.ts src/videoStudio/__tests__/VideoStudio.test.tsx
```

Expected: FAIL (`finalizeMediaAsset` is not exported; the picked clip is finalized the plain way).

- [ ] **Step 9: Implement the client**

`web/src/chat/attachments/api.ts`, replacing `finalizeAsset`:

```ts
async function postFinalize(id: string, query: string): Promise<Asset> {
  const res = await fetch(`${assetURL(id)}/finalize${query}`, {
    method: 'POST',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readJSON<Asset>(res);
}

export async function finalizeAsset(id: string): Promise<Asset> {
  return postFinalize(id, '');
}

/** Finalizes an editor source: accepted and left alone, never transcribed or summarized. */
export async function finalizeMediaAsset(id: string): Promise<Asset> {
  return postFinalize(id, '?use=media');
}
```

`web/src/videoStudio/VideoStudio_sources.ts`: import `finalizeMediaAsset` instead of `finalizeAsset`, and line 186 becomes `const finalized = await finalizeMediaAsset(presign.asset.id);`. Update the doc comment above `uploadSource` ("Presign, PUT, finalize-as-media …").

Re-run the two vitest files: PASS. Then run the whole `src/videoStudio` and `src/chat/attachments` suites.

- [ ] **Step 10: Write the E2E helpers**

`web/e2e/support/assetUpload.ts`, restructured so bytes and paths share one path:

```ts
import { readFileSync } from 'node:fs';
import type { Page } from '@playwright/test';

// assetUpload.ts — putting fixture bytes into the identity's library the way the cockpit does:
// presign, PUT to the object store, finalize. It runs INSIDE the page so every call rides the
// session cookie the test already has, and so the PUT leaves the browser rather than Node — a
// seeded asset that arrived by another route would not prove the browser can reach the store.

export interface UploadOptions {
  /** `media` finalizes the way the editor does: accepted, never processed. */
  readonly use?: 'media';
}

/** Uploads bytes through the real asset routes and answers with the asset id. */
export async function uploadBytes(
  page: Page,
  bytes: Buffer,
  fileName: string,
  mimeType: string,
  options: UploadOptions = {},
): Promise<string> {
  return page.evaluate(
    async ({ base64, file, mimeType, query }) => {
      const body = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
      const presign = await fetch('/api/assets/presign', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          thread_id: '',
          file_name: file,
          mime_type: mimeType,
          size_bytes: body.byteLength,
          modality_hint: 'unknown',
        }),
      });
      if (!presign.ok) throw new Error(`presign: HTTP ${String(presign.status)}`);
      const { asset, upload } = (await presign.json()) as {
        asset: { id: string };
        upload: { upload_url: string; required_headers?: Record<string, string> };
      };
      const put = await fetch(upload.upload_url, {
        method: 'PUT',
        headers: upload.required_headers ?? {},
        body,
      });
      if (!put.ok) throw new Error(`put: HTTP ${String(put.status)}`);
      const done = await fetch(`/api/assets/${asset.id}/finalize${query}`, { method: 'POST' });
      if (!done.ok) throw new Error(`finalize: HTTP ${String(done.status)}`);
      return asset.id;
    },
    {
      base64: bytes.toString('base64'),
      file: fileName,
      mimeType,
      query: options.use === 'media' ? '?use=media' : '',
    },
  );
}

/** Uploads a fixture file through the real asset routes and answers with its asset id. */
export async function uploadAsset(
  page: Page,
  path: string,
  fileName: string,
  mimeType: string,
  options: UploadOptions = {},
): Promise<string> {
  return uploadBytes(page, readFileSync(path), fileName, mimeType, options);
}
```

`web/e2e/support/assetCleanup.ts`:

```ts
import type { Page } from '@playwright/test';

// assetCleanup.ts — these specs also run against the lab VM, signed in as the operator. Every
// asset a run presigns is recorded here and deleted when the test ends, so a run leaves the
// operator's library the way it found it.

export interface CreatedAssets {
  ids(): readonly string[];
}

/** Records the id of every asset this page presigns, whoever presigned it (test or editor). */
export function trackCreatedAssets(page: Page): CreatedAssets {
  const seen = new Set<string>();
  page.on('response', (response) => {
    if (!response.url().includes('/api/assets/presign') || !response.ok()) return;
    void response
      .json()
      .then((body: { asset?: { id?: unknown } }) => {
        if (typeof body.asset?.id === 'string') seen.add(body.asset.id);
      })
      .catch(() => undefined);
  });
  return { ids: () => [...seen] };
}

export async function deleteAssets(page: Page, ids: readonly string[]): Promise<void> {
  await page.evaluate(async (all) => {
    await Promise.all(all.map((id) => fetch(`/api/assets/${id}`, { method: 'DELETE' })));
  }, ids);
}

export interface AssetRow {
  readonly status: string;
  readonly modality: string;
  readonly object_key: string;
  readonly summary: string;
}

export async function readAsset(page: Page, id: string): Promise<AssetRow> {
  return page.evaluate(async (assetId) => {
    const res = await fetch(`/api/assets/${assetId}`);
    if (!res.ok) throw new Error(`asset ${assetId}: HTTP ${String(res.status)}`);
    return (await res.json()) as AssetRow;
  }, id);
}
```

- [ ] **Step 11: Write the T1a E2E**

`web/e2e/video-studio-audio.spec.ts`:

```ts
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { deleteAssets, readAsset, trackCreatedAssets } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';

// video-studio-audio.spec.ts — the audio half of the editor against a running Aura: here, that a
// sound the editor stores is filed with the media and never sent to speech-to-text. The claim is
// only worth something against a control, so the same file also goes through the plain finalize,
// and the test waits until that one has visibly been processed.

const AUDIO = resolve(dirname(fileURLToPath(import.meta.url)), 'fixtures/video-studio/audio');

test.describe('audio sources in the library', () => {
  test('an editor sound is filed under media and never transcribed', async ({ page }) => {
    test.setTimeout(5 * 60_000);
    const created = trackCreatedAssets(page);
    await gotoAuthenticated(page, '/');
    try {
      const control = await uploadAsset(page, resolve(AUDIO, 'music.wav'), 'music.wav', 'audio/wav');
      const media = await uploadAsset(page, resolve(AUDIO, 'music.wav'), 'music.wav', 'audio/wav', { use: 'media' });

      // The worker demonstrably runs: the plain upload leaves `accepted` for processing and beyond.
      await expect
        .poll(async () => (await readAsset(page, control)).status, { timeout: 180_000, intervals: [2_000] })
        .not.toMatch(/^(accepted|processing)$/);

      const row = await readAsset(page, media);
      expect(row.modality).toBe('audio');
      expect(row.object_key.startsWith('media/'), row.object_key).toBe(true);
      expect(row.status).toBe('accepted');
      expect(row.summary).toBe('');

      const refused = await page.evaluate(async () => {
        const presign = await fetch('/api/assets/presign', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ thread_id: '', file_name: 'manual.pdf', mime_type: 'application/pdf', size_bytes: 9, modality_hint: 'unknown' }),
        });
        const { asset, upload } = (await presign.json()) as {
          asset: { id: string };
          upload: { upload_url: string; required_headers?: Record<string, string> };
        };
        await fetch(upload.upload_url, { method: 'PUT', headers: upload.required_headers ?? {}, body: '%PDF test' });
        return (await fetch(`/api/assets/${asset.id}/finalize?use=media`, { method: 'POST' })).status;
      });
      expect(refused).toBe(400);
    } finally {
      await deleteAssets(page, created.ids());
    }
  });
});
```

(Task 8 adds `uploadBytes` to this import when it first uses it.)

- [ ] **Step 12: Local gates (WSL), then commit**

```bash
cd /mnt/d/Aura/web && ./node_modules/.bin/tsc -b && ./node_modules/.bin/eslint src e2e && \
  ./node_modules/.bin/prettier --check src e2e && ./node_modules/.bin/knip && \
  ./node_modules/.bin/vitest run --coverage src/videoStudio src/chat/attachments && npm run build
```

Expected: all green, coverage ≥ 85 % on the touched files, `internal/webui/dist` rebuilt.

```bash
git add internal/assets/service_accept.go web/e2e/support/assetCleanup.ts web/e2e/video-studio-audio.spec.ts
git commit -m "feat(assets): finalize editor sources without processing and file audio as media" -- \
  internal/assets/service.go internal/assets/service_accept.go internal/assets/service_accept_test.go \
  internal/objectstore/asset_placement.go internal/agui/asset_service.go internal/agui/assets_api.go \
  internal/agui/assets_api_test.go web/src/chat/attachments/api.ts \
  web/src/chat/attachments/__tests__/api.test.ts web/src/videoStudio/VideoStudio_sources.ts \
  web/src/videoStudio/__tests__/VideoStudio.test.tsx web/e2e/support/assetUpload.ts \
  web/e2e/support/assetCleanup.ts web/e2e/video-studio-audio.spec.ts internal/webui/dist
```

The body explains the transcription cost and the query-on-the-same-route choice. End with the trailer.

- [ ] **Step 13: Push, wait for the VM, run the E2E**

```bash
git push
gh run list --commit "$(git rev-parse HEAD)" --json name,status,conclusion
```

Wait for every run green, *Publish Aura edge image* included. A red job is fixed before anything else (fix ALL red CI).

Then, in WSL, `~/aura-vm/wait-revision.sh "$(git rev-parse HEAD)"`, created once:

```bash
#!/usr/bin/env bash
# Waits until the lab VM's aura container runs the image built from commit $1. The updater
# applies an image only after about 15 minutes without chat activity, so this polls each minute
# for up to an hour. The ssh password stays in the WSL session's VM_SSH_PASSWORD, not in the repo.
set -euo pipefail
want="$1"
for _ in $(seq 60); do
  # $VM_SSH_PASSWORD expands HERE, before ssh sends the line: the VM has no such variable.
  have="$(sshpass -p "$VM_SSH_PASSWORD" ssh -o StrictHostKeyChecking=no aura@192.168.101.158 \
    "echo '$VM_SSH_PASSWORD' | sudo -S -p '' docker inspect aura --format '{{index .Config.Labels \"org.opencontainers.image.revision\"}}'" 2>/dev/null || true)"
  [ "$have" = "$want" ] && { echo "VM runs $want"; exit 0; }
  echo "VM runs ${have:-unknown}; waiting for $want"
  sleep 60
done
echo "the VM did not reach $want within an hour" >&2
exit 1
```

Then run the E2E from WSL:

```bash
cd /mnt/d/Aura/web && AURA_E2E_ORIGIN=https://192.168.101.158 AURA_E2E_USE_BUNDLED_CHROMIUM=true \
  ./node_modules/.bin/playwright test e2e/video-studio-audio.spec.ts --reporter=list
```

Expected: PASS on `chrome` and `mobile-chrome`. Report the control's final status and the media row's `object_key`. If the control never leaves `accepted`, the VM may have no STT configured. Read `/api/voice/capabilities` and report it; do not delete the control assertion.

---

### Task 8: T1b — the audio model, its loader, the rail split and the shared E2E helpers

**Files:**
- Modify: `web/src/videoStudio/project.ts` (types + `audioTracks`)
- Modify: `web/src/videoStudio/projectStore.ts:139-259` (validation)
- Modify: `web/src/videoStudio/__tests__/projectStore.test.ts`
- Create: `web/src/videoStudio/VideoStudio_rail.tsx`
- Modify: `web/src/videoStudio/VideoStudio.tsx:1-13,362-429`
- Create: `web/e2e/support/videoStudio.ts` (helpers moved out of `video-studio.spec.ts`)
- Modify: `web/e2e/video-studio.spec.ts` (imports the helpers; tracks and deletes created assets)
- Modify: `web/e2e/video-studio-audio.spec.ts` (T1b test)

**Interfaces:**
- Consumes: `uploadBytes`, `trackCreatedAssets`, `deleteAssets` (Task 7).
- Produces: the types `EnvelopePoint { time: number; gain: number }`, `AudioDucking { amountDb: number; ramp: number }`, `AudioItem`, `AudioTrack`; `VideoProject.audio?: readonly AudioTrack[]`; `ProjectSource.kind: 'video' | 'image' | 'audio'`; `ProjectSource.speech?: readonly (readonly [number, number])[]`; `ProjectSource.denoisedAssetId?: string`; `VideoItem.denoise?: boolean`; `audioTracks(project: VideoProject): readonly AudioTrack[]`. Also `StudioRail` with the props below, and the helpers `openStudioWith`, `editorOnSeededClip`, `pressAddAction`, `addClip`, `setField`, `seconds`, `boxOf`, `dragBy`, `containerFacts`, `watchNetwork`, `expectNothingLeftTheAppliance`, `FIXTURES`, exported from `e2e/support/videoStudio.ts`.

- [ ] **Step 1: Write the failing loader tests**

In `web/src/videoStudio/__tests__/projectStore.test.ts`, add after the existing `it.each` refusal block:

```ts
const MUSIC_SOURCE = {
  id: 'src-music',
  assetId: 'asset-music',
  kind: 'audio',
  duration: 8,
  size: { width: 0, height: 0 },
  speech: [[1, 2.5]],
};

function audioItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 'bed-1',
    sourceId: 'src-music',
    anchor: { clipId: 'clip-1', offset: 0 },
    sourceStart: 0,
    duration: 6,
    volume: 0.8,
    muted: false,
    fadeIn: 1,
    fadeOut: 2,
    envelope: [
      { time: 0, gain: 1 },
      { time: 3, gain: 0.4 },
    ],
    ducking: { amountDb: -12, ramp: 0.4 },
    denoise: true,
    label: 'Bed',
    ...overrides,
  };
}

function withAudio(item: Record<string, unknown>, sources: unknown[] = [MUSIC_SOURCE]) {
  const base = project();
  return { ...base, sources: [...base.sources, ...sources], audio: [{ id: 'audio-1', items: [item] }] };
}

describe('loadProject with audio lanes', () => {
  it('round-trips a project that carries an audio lane', async () => {
    const original = withAudio(audioItem());
    serve(JSON.stringify(original));
    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.project).toEqual(original);
  });

  it('still reads a project saved before audio existed', async () => {
    const legacy = project();
    serve(JSON.stringify(legacy));
    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.project.audio).toBeUndefined();
    expect(loaded.project.video).toEqual(legacy.video);
  });

  it.each([
    ['an audio item naming a source the file does not hold', audioItem({ sourceId: 'src-gone' }), undefined],
    ['an audio item playing a still image', audioItem({ sourceId: 'src-b' }), undefined],
    ['an audio item anchored to a clip the file does not hold', audioItem({ anchor: { clipId: 'clip-gone', offset: 0 } }), undefined],
    ['an audio item extracted from a clip the file does not hold', audioItem({ extractedFrom: 'clip-gone' }), undefined],
    ['an audio volume JSON reads as Infinity', '__INFINITE_VOLUME__', undefined],
    ['an envelope point with no gain', audioItem({ envelope: [{ time: 0 }] }), undefined],
    ['ducking that is not an object', audioItem({ ducking: -12 }), undefined],
    ['a speech window that runs backwards', audioItem(), [{ ...MUSIC_SOURCE, speech: [[3, 1]] }]],
    ['a speech window that is not a pair', audioItem(), [{ ...MUSIC_SOURCE, speech: [[1]] }]],
  ])('refuses a saved file carrying %s', async (_case, item, sources) => {
    const body =
      item === '__INFINITE_VOLUME__'
        ? JSON.stringify(withAudio(audioItem())).replace('"volume":0.8', '"volume":1e999')
        : JSON.stringify(withAudio(item as Record<string, unknown>, sources));
    serve(body);
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow(/not a project/);
  });

  it('refuses a clip on the video lane that plays an audio source', async () => {
    const base = withAudio(audioItem());
    const broken = { ...base, video: [{ ...base.video[0], sourceId: 'src-music' }] };
    serve(JSON.stringify(broken));
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow(/not a project/);
  });
});
```

Run (WSL): `cd /mnt/d/Aura/web && ./node_modules/.bin/vitest run src/videoStudio/__tests__/projectStore.test.ts`. Expected: the round-trip test fails (the audio source's `kind` is refused, so the file is "not a project"), and most refusal rows pass for the wrong reason. Also run `tsc -b`: `audio` does not exist on `VideoProject`.

- [ ] **Step 2: Add the model to `project.ts`**

`ProjectSource`:

```ts
export interface ProjectSource {
  readonly id: string;
  readonly assetId: string;
  readonly kind: 'video' | 'image' | 'audio';
  readonly duration: number; // seconds; 0 for an image, which takes the duration its item asks for
  readonly size: ProjectSize; // 0×0 for a sound
  readonly hasAudio?: boolean;
  /** Where the VAD heard speech, in source seconds. Written once per source by the analysis. */
  readonly speech?: readonly (readonly [number, number])[];
  /** The same source with its noise removed, computed once when noise reduction is first asked. */
  readonly denoisedAssetId?: string;
}
```

`VideoItem`: add `readonly denoise?: boolean;` after `volume`.

After `OverlayTrack`:

```ts
export interface EnvelopePoint {
  readonly time: number; // seconds from the item's start
  readonly gain: number; // 0–1: the envelope only attenuates; the item's volume boosts
}

export interface AudioDucking {
  readonly amountDb: number; // −24 to −3
  readonly ramp: number; // seconds, 0.1 to 2
}

/**
 * A sound on an audio lane. It hangs off a clip like an overlay and rides the ripple with it, but
 * unlike an overlay it may run past its clip: a music bed covers many. It is clipped at the
 * project's end and never lengthens the project.
 */
export interface AudioItem {
  readonly id: string;
  readonly sourceId: string;
  readonly anchor: OverlayAnchor;
  readonly sourceStart: number;
  readonly duration: number; // source seconds
  readonly volume: number; // 0–2
  readonly muted: boolean;
  readonly fadeIn?: number;
  readonly fadeOut?: number;
  readonly speed?: number;
  readonly envelope?: readonly EnvelopePoint[];
  readonly ducking?: AudioDucking;
  readonly denoise?: boolean;
  /** The clip this sound was extracted from; removing that clip removes it. */
  readonly extractedFrom?: string;
  readonly label?: string;
}

export interface AudioTrack {
  readonly id: string;
  readonly items: readonly AudioItem[];
}
```

`VideoProject`: add `readonly audio?: readonly AudioTrack[];` after `overlays`, and below `sourceOf`:

```ts
/** The audio lanes; a project saved before audio existed has none. */
export function audioTracks(project: VideoProject): readonly AudioTrack[] {
  return project.audio ?? [];
}
```

- [ ] **Step 3: Extend the loader's validation**

In `projectStore.ts`, add the finite-number guard next to `bagOf` and use it for every new numeric field:

```ts
/** `typeof Infinity === 'number'`, and JSON reads `1e999` as exactly that. */
function finite(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

function optionalFinite(value: unknown): boolean {
  return value === undefined || finite(value);
}

function isSpeech(value: unknown): boolean {
  return (
    Array.isArray(value) &&
    value.every(
      (window) =>
        Array.isArray(window) &&
        window.length === 2 &&
        finite(window[0]) &&
        finite(window[1]) &&
        window[0] <= window[1],
    )
  );
}
```

`isSource`:

```ts
    (source.kind === 'video' || source.kind === 'image' || source.kind === 'audio') &&
    …
    (source.speech === undefined || isSpeech(source.speech)) &&
    (source.denoisedAssetId === undefined || typeof source.denoisedAssetId === 'string') &&
```

`isClip`: add `(clip.denoise === undefined || typeof clip.denoise === 'boolean') &&`.

New guards after `isOverlayTrack`:

```ts
function isEnvelopePoint(value: unknown): boolean {
  const point = bagOf(value);
  return point !== undefined && finite(point.time) && finite(point.gain);
}

function isDucking(value: unknown): boolean {
  const ducking = bagOf(value);
  return ducking !== undefined && finite(ducking.amountDb) && finite(ducking.ramp);
}

function isAudioItem(value: unknown): value is AudioItem {
  const item = bagOf(value);
  if (item === undefined) return false;
  const anchor = bagOf(item.anchor);
  return (
    typeof item.id === 'string' &&
    typeof item.sourceId === 'string' &&
    anchor !== undefined &&
    typeof anchor.clipId === 'string' &&
    finite(anchor.offset) &&
    finite(item.sourceStart) &&
    finite(item.duration) &&
    finite(item.volume) &&
    typeof item.muted === 'boolean' &&
    optionalFinite(item.fadeIn) &&
    optionalFinite(item.fadeOut) &&
    optionalFinite(item.speed) &&
    (item.envelope === undefined ||
      (Array.isArray(item.envelope) && item.envelope.every(isEnvelopePoint))) &&
    (item.ducking === undefined || isDucking(item.ducking)) &&
    (item.denoise === undefined || typeof item.denoise === 'boolean') &&
    (item.extractedFrom === undefined || typeof item.extractedFrom === 'string') &&
    (item.label === undefined || typeof item.label === 'string')
  );
}

function isAudioTrack(value: unknown): value is AudioTrack {
  const track = bagOf(value);
  return (
    track !== undefined &&
    typeof track.id === 'string' &&
    Array.isArray(track.items) &&
    track.items.every(isAudioItem)
  );
}
```

`hasProjectShape`: add `(project.audio === undefined || (Array.isArray(project.audio) && project.audio.every(isAudioTrack))) &&`.

`referencesHold`: replace its body with a kind-aware version:

```ts
function referencesHold(project: VideoProject): boolean {
  const kinds = new Map(project.sources.map((source) => [source.id, source.kind]));
  const clips = new Set(project.video.map((clip) => clip.id));
  const plays = (sourceId: string, allowed: readonly ProjectSource['kind'][]) => {
    const kind = kinds.get(sourceId);
    return kind !== undefined && allowed.includes(kind);
  };
  return (
    project.video.every((clip) => plays(clip.sourceId, ['video', 'image'])) &&
    project.video.every(
      (clip, index) =>
        clip.junctionFromClipId === undefined ||
        project.video[index - 1]?.id === clip.junctionFromClipId,
    ) &&
    project.overlays.every((lane) => lane.items.every((item) => clips.has(item.anchor.clipId))) &&
    audioTracks(project).every((lane) =>
      lane.items.every(
        (item) =>
          plays(item.sourceId, ['audio', 'video']) &&
          clips.has(item.anchor.clipId) &&
          (item.extractedFrom === undefined || clips.has(item.extractedFrom)),
      ),
    )
  );
}
```

Update the doc comment above it to name the two new ways of failing: a lane playing a source of the wrong kind, and a sound hanging off a missing clip. Import `audioTracks`, `AudioItem`, `AudioTrack` and `ProjectSource` from `./project`.

- [ ] **Step 4: Run the loader tests green (WSL)**

```bash
cd /mnt/d/Aura/web && ./node_modules/.bin/vitest run src/videoStudio && ./node_modules/.bin/tsc -b
```

Expected: PASS. `wc -l src/videoStudio/projectStore.ts` stays under 600 (about 390).

- [ ] **Step 5: Extract the rail**

`web/src/videoStudio/VideoStudio_rail.tsx`:

```tsx
import { Plus, Scissors, SlidersHorizontal, Trash2, Type } from 'lucide-react';
import { useTranslation } from 'react-i18next';

interface StudioRailProps {
  readonly canAddTitle: boolean;
  readonly canRemove: boolean;
  readonly onAddSource: () => void;
  readonly onAddTitle: () => void;
  readonly onSplit: () => void;
  readonly onShowProperties: () => void;
  readonly onRemove: () => void;
}

/** The desktop tool rail. The narrow layout has its own bar (VideoStudio_mobile.tsx); both drive
 *  the handlers the workspace owns. */
export function StudioRail({
  canAddTitle,
  canRemove,
  onAddSource,
  onAddTitle,
  onSplit,
  onShowProperties,
  onRemove,
}: StudioRailProps) {
  const { t } = useTranslation();
  return (
    <div role="toolbar" aria-label={t('videoStudio.commands')} className="video-studio-rail">
      <button type="button" className="video-studio-rail-button" data-primary="true" onClick={onAddSource}>
        <Plus aria-hidden="true" />
        <span>{t('videoStudio.command.addSource')}</span>
      </button>
      <button type="button" className="video-studio-rail-button" disabled={!canAddTitle} onClick={onAddTitle}>
        <Type aria-hidden="true" />
        <span>{t('videoStudio.command.addText')}</span>
      </button>
      <button type="button" className="video-studio-rail-button" onClick={onSplit}>
        <Scissors aria-hidden="true" />
        <span>{t('videoStudio.command.split')}</span>
      </button>
      <button type="button" className="video-studio-rail-button" onClick={onShowProperties}>
        <SlidersHorizontal aria-hidden="true" />
        <span>{t('videoStudio.inspector.label')}</span>
      </button>
      <button type="button" className="video-studio-rail-button" disabled={!canRemove} onClick={onRemove}>
        <Trash2 aria-hidden="true" />
        <span>{t('videoStudio.command.remove')}</span>
      </button>
    </div>
  );
}
```

In `VideoStudio.tsx`:
- replace the `<div role="toolbar" …>` block (lines 362–429) with `<StudioRail … />`, passing:
  - `canAddTitle={underPlayhead !== undefined}`;
  - `canRemove={selectedId !== undefined}`;
  - `onAddSource={() => fileInput.current?.click()}`;
  - `onAddTitle={addTitle}`;
  - `onSplit={() => { run((current) => splitAt(current, { time: playhead })); }}`;
  - `onShowProperties={() => propertiesRef.current?.focus()}`;
  - `onRemove={() => { if (selectedId !== undefined) run((current) => removeItem(current, { itemId: selectedId })); }}`;
- follow it with the hidden `<input ref={fileInput} …>`, unchanged but outside the rail;
- drop `Plus`, `Scissors`, `SlidersHorizontal`, `Trash2`, `Type` from the lucide import and import `StudioRail`.

Run in WSL: `./node_modules/.bin/vitest run src/videoStudio`. PASS, with no test changes; the rail's role, label and button names are identical. `wc -l src/videoStudio/VideoStudio.tsx` is now about 550.

- [ ] **Step 6: Move the Studio E2E helpers into `e2e/support/videoStudio.ts`**

Move the following from `web/e2e/video-studio.spec.ts` into the new file, unchanged, adding `export`:
- `FIXTURES`;
- `videoRecord`;
- `NetworkLog` and `watchNetwork`;
- `expectNothingLeftTheAppliance`;
- `openStudioWith`;
- `editorOnSeededClip`;
- `pressAddAction`;
- `addClip`;
- `setField`;
- `seconds`;
- `boxOf`;
- `dragBy`;
- `containerFacts`.

`FIXTURES` resolves from `e2e/support`, so it becomes `resolve(dirname(fileURLToPath(import.meta.url)), '../fixtures/video-studio')`. `twinFrameDiff`, `FrameDiff` and the title constants stay in the spec. In `video-studio.spec.ts`, import the helpers. In each test, add `const created = trackCreatedAssets(page);` first and wrap the body in `try { … } finally { await deleteAssets(page, created.ids()); }`, so a VM run leaves no clip behind.

- [ ] **Step 7: Write the T1b E2E**

Append to `web/e2e/video-studio-audio.spec.ts`:

```ts
import { randomUUID } from 'node:crypto';
import { FIXTURES, openStudioWith } from './support/videoStudio';

/** A project in the shape saved before audio existed: no `audio` key at all. */
function legacyProject(clipAsset: string, name: string) {
  return {
    id: randomUUID(),
    name,
    size: { width: 320, height: 180 },
    fps: 30,
    sources: [
      { id: 'src-a', assetId: clipAsset, kind: 'video', duration: 4, size: { width: 320, height: 180 }, hasAudio: true },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false }],
    overlays: [],
  };
}

/** Puts a project file in the library and opens the Studio on it, the way a reload does. */
async function reopen(page: import('@playwright/test').Page, project: object, clipAsset: string) {
  const fileId = await uploadBytes(page, Buffer.from(JSON.stringify(project)), 'project.json', 'application/json');
  await page.addInitScript((id) => {
    window.localStorage.setItem('aura.videoStudio.lastSavedProject', id);
  }, fileId);
  await openStudioWith(page, clipAsset, 'audio lane check');
  await page.getByRole('button', { name: 'Reopen the last project you saved here' }).click();
  return page.getByRole('dialog', { name: 'Video editor' });
}

test.describe('the project file with audio lanes', () => {
  test('a project saved before audio existed opens unchanged', async ({ page }) => {
    test.setTimeout(3 * 60_000);
    const created = trackCreatedAssets(page);
    await gotoAuthenticated(page, '/');
    try {
      const clip = await uploadAsset(page, resolve(FIXTURES, 'clip-a.mp4'), 'clip-a.mp4', 'video/mp4', { use: 'media' });
      const editor = await reopen(page, legacyProject(clip, 'legacy audio check'), clip);
      await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 60_000 });
      await expect(editor.getByRole('heading', { name: 'legacy audio check', includeHidden: true })).toBeAttached();
      await expect(editor.getByRole('alert')).toHaveCount(0);
    } finally {
      await deleteAssets(page, created.ids());
    }
  });

  test('an audio lane naming a missing source is refused at load', async ({ page }) => {
    test.setTimeout(3 * 60_000);
    const created = trackCreatedAssets(page);
    await gotoAuthenticated(page, '/');
    try {
      const clip = await uploadAsset(page, resolve(FIXTURES, 'clip-a.mp4'), 'clip-a.mp4', 'video/mp4', { use: 'media' });
      const broken = {
        ...legacyProject(clip, 'broken audio check'),
        audio: [
          {
            id: 'audio-1',
            items: [
              { id: 'bed', sourceId: 'src-gone', anchor: { clipId: 'clip-1', offset: 0 }, sourceStart: 0, duration: 2, volume: 1, muted: false },
            ],
          },
        ],
      };
      const editor = await reopen(page, broken, clip);
      await expect(editor.getByRole('alert')).toBeVisible({ timeout: 60_000 });
      await expect(editor.getByRole('button', { name: 'Clip 1' })).toHaveCount(0);
    } finally {
      await deleteAssets(page, created.ids());
    }
  });
});
```

Keep all imports at the top of the file (merge them with Task 7's). Before running, read `web/src/studio/StudioWorkspace.tsx:240-260` to confirm the reopen button renders when `lastSavedProject()` is set and the Studio surface is open. If its gate differs, follow the component rather than this plan's assumption.

- [ ] **Step 8: Local gates, commit, push, VM E2E**

Run the same local gates as Task 7 Step 12, with `src/videoStudio` for vitest. Commit:

```bash
git add web/src/videoStudio/VideoStudio_rail.tsx web/e2e/support/videoStudio.ts
git commit -m "feat(video-studio): carry audio lanes in the project file" -- \
  web/src/videoStudio/project.ts web/src/videoStudio/projectStore.ts \
  web/src/videoStudio/__tests__/projectStore.test.ts web/src/videoStudio/VideoStudio.tsx \
  web/src/videoStudio/VideoStudio_rail.tsx web/e2e/support/videoStudio.ts \
  web/e2e/video-studio.spec.ts web/e2e/video-studio-audio.spec.ts internal/webui/dist
```

Push, wait for green CI and `~/aura-vm/wait-revision.sh`, then run **both** Studio specs against the VM (the extraction must not have broken the old one):

```bash
cd /mnt/d/Aura/web && AURA_E2E_ORIGIN=https://192.168.101.158 AURA_E2E_USE_BUNDLED_CHROMIUM=true \
  ./node_modules/.bin/playwright test e2e/video-studio-audio.spec.ts e2e/video-studio.spec.ts --reporter=list
```

Expected: PASS on `chrome` and `mobile-chrome`. Read the failure screenshots, if any, before touching code. Afterwards, list the operator's recent assets and confirm the run left none of its own behind.

Plan A ends here. Report to the operator, then write Plan B (T2–T7) from `FINDINGS.md`.
