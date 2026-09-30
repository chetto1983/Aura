# aura-video-mcp render sidecar — spike findings

Sub-project 2 of `docs/superpowers/specs/2026-09-27-video-studio-audio-design.md` §"The whole job":
a Node sidecar that renders Studio projects as background jobs. These spikes measure; nothing here
is product code. Run 2026-09-30.

**Where it ran.** The lab VM `192.168.101.158` (VMware on the operator's workstation: Intel
i7-11850H, the VM has 8 vCPUs and 15.4 GB), in a spike container `vmr-probe:spike`:
- `node:24-bookworm-slim` (Debian 12.15, Node 24.21.0). This is the repo's engine
  (`web/package.json` `"node": ">=24.16.0 <25"`) on a distro that both Playwright's `install-deps` and
  Google's Chrome `.deb` support.
- `--cpus=4` (the appliance's thread budget) and `--network none` unless a row says otherwise.
- `--memory=6g --memory-swap=6g`: 4 GiB OOM-killed renderer-server, see S1.3.

**Browsers** (`Dockerfile.probe`):
- **Chromium**: Playwright 1.62.1's revision 1234 is *Chrome for Testing* 151.0.7922.34
  (`playwright-core/browsers.json`). It is measured both as the full build and as the headless shell.
- **Chrome**: Google Chrome stable 154.0.8037.92, from `npx playwright install chrome`.

**Packages**:
- renderer-server side: `@videoflow/core`, `@videoflow/renderer-browser` and
  `@videoflow/renderer-server` 1.3.4, `mediabunny` 1.58.1, `esbuild` 0.24.2.
- Our export page (`build.mjs`): `web/src/videoStudio/*` bundled unchanged, resolving against
  `web/node_modules`, i.e. the cockpit's own versions **including its patch-package patch** of
  renderer-browser (S1.2).

## Fixtures

`make-fixtures.sh`, the container's own ffmpeg 5.1.9:
- Sources are H.264 High 4:2:0 1080p30 (GOP 30, CRF 20) with AAC-LC 48 kHz stereo:
  - `clip-1080p-5s.mp4`: `testsrc2`, a 440 Hz tone;
  - `clip-a/b/c.mp4`, 21 s each: `testsrc2`, `testsrc` and `gradients`. A and C carry a 0.25
    sine (−15.05 dBFS) only where the parity project listens for it; B carries a 600 Hz sine
    throughout and is muted in the project.
- `music-bed.wav` is Plan A's `music.wav` (−18.24 dBFS) looped to 50 s.
- `speech*.wav` and `speech.truth.json` are Plan A's committed fixtures.

The parity project (`lib/project.mjs`) is 60 s at 1920×1080 and 30 fps. Each Studio feature owns a
stretch of the film where its effect can be measured alone:

| When (s) | What |
|---|---|
| 0–20.5 | clip A (fade-in), whose own 1 kHz tone plays 0–5 s |
| 19.5–20.5 | 1 s crossfade A → B |
| 19.5–40 | clip B, muted |
| 21.5–29.5 | text overlay on B, the cockpit's Atkinson Hyperlegible Next |
| 39–40 | 1 s crossfade B → C |
| 39–60 | clip C (fade-out), volume 0.5, whose 800 Hz tone plays 55–60 s |
| 5–55 | music at volume 0.8: fade-in 2 s, fade-out 3 s, envelope 1 → 0.5 over 30–35 s, ducking −12 dB with 0.5 s ramps |
| from 8 | `speech.wav` at volume 0.01 (as the ducking E2E does); its speech windows are the fixture's truth |

`projectStore.loadProject` accepted it: `missing: []` in every page run.

## S1 — codecs and render parity

### S1.1 What each browser can decode and encode

**Question.** Which Linux browser build handles H.264/AAC in and out, and does a real 1080p render
come out with frames and sound?

**Method.**
- `s1-codecs.mjs` asks `isConfigSupported`
  (<https://www.w3.org/TR/webcodecs/#dom-videodecoder-isconfigsupported>), `canPlayType` (VideoFlow's
  `<video>` fallback, renderer-browser `dist/layers/RuntimeVideoLayer.js:16-21`) and whether
  `drawElementImage` exists.
- `s1-render.mjs` renders the 5 s clip with renderer-server on each binary. `VIDEOFLOW_CHROME_PATH`
  selects a binary; unset means `channel: 'chrome'`, `ServerRenderer.js:94-97`.

**Numbers.** All three builds answered **identically**:

| | H.264 High | HEVC | VP9 | AV1 | AAC | Opus | MP3 |
|---|---|---|---|---|---|---|---|
| `VideoDecoder` / `AudioDecoder` | yes | **no** | yes | yes | yes | yes | yes |
| `VideoEncoder` 1080p30 / `AudioEncoder` | yes (High and Baseline) | — | yes | yes | **no** | yes | — |

- `canPlayType('video/mp4; codecs="avc1.640028, mp4a.40.2"')` = `probably`, and
  `drawElementImage` is present in all three.
- The real render: 150/150 frames, H.264 High 1920×1080, audio **Opus** 48 kHz stereo at −15.04 dBFS
  against the source's −15.05.
- Wall time for the 5 s clip, launch included: headless shell 40.6 s, Chromium 40.4 s, Chrome 38.7 s.
- Pictures are right: the burned-in counter reads 2.500 s / frame 75. PSNR against the source over
  all 150 frames is 30.1–30.3 dB when the untagged source is read as BT.709. It is 23.2–23.4 dB when
  read as BT.601, which is ffmpeg's guess (`diag-color.sh`).
- The builds tag colour differently:
  - Chrome 154 writes full-range BT.709 (`yuvj420p`, `pc`);
  - CfT 151 (full and shell) writes limited range tagged `smpte170m`.

**Answer.**
- On Linux x86-64, Playwright's Chromium and Google Chrome are equivalent for this job: H.264 in and
  out, AAC in, **Opus out**.
- No Linux build encodes AAC. The README says so for Chrome and Chromium alike
  (`renderer-server/README.md:269`), and renderer-browser falls back to Opus
  (`BrowserRenderer.js:202-224`, AAC first, then Opus).
- Only renderer-server's `ffmpeg: true` path produced AAC (S2).
- Playwright's docs warn that "Chromium does not have all the codecs that Google Chrome … bundl[es]"
  (browsers.md, *Media codecs*). That did not hold for the CfT build Playwright 1.62.1 ships on x64.

**arm64 (read, not run).** An amd64-only answer:
- Google now publishes `google-chrome-stable_current_arm64.deb`: HEAD from the VM returned 200,
  134,430,076 B, `Last-Modified` 2026-09-30.
- But Playwright 1.62.1 cannot install it. `playwright-core/bin/reinstall_chrome_stable_linux.sh:5`
  exits "not supported on Linux Arm64", and `:38` hardcodes the amd64 `.deb`.
- Playwright's registry takes Linux x64 Chromium from Chrome for Testing
  (`cftUrl("linux64/chrome-linux64.zip")`) but Linux arm64 from its own "non-cft build",
  `builds/chromium/%s/chromium-linux-arm64.zip` (204,732,694 B; `playwright-core/lib/coreBundle.js`,
  around line 32467). That is a different build, and its codec set was **not measured**.

**What S1.1 does not show.**
- arm64 at all, and Windows or macOS.
- Hardware acceleration: every launch passes renderer-server's `--disable-gpu`, and the container
  has no GPU.
- Whether a player accepts Opus inside MP4. Telegram's players and Safari/iOS were not tried.
- Real camera footage: the sources are synthetic, untagged and 1 s GOP.

### S1.2 Does `toVideoJSON` run in Node?

**Method.** `s1-node-compile.mjs` in WSL, Node 24.18.0. `videoflow.ts` is imported four ways and
compiled on the parity project; the result is compared with the JSON the browser compiled (layer
`id`s ignored — VideoFlow draws them at random on every compile — and the port normalised).

**Numbers.**

| attempt | result |
|---|---|
| Node's own type stripping | `ERR_MODULE_NOT_FOUND` on `./project`: extensionless relative imports, `videoflow.ts:15-24` (and `:25`, `:26`) |
| esbuild bundle, packages left to Node | `ERR_IMPORT_ATTRIBUTE_MISSING`: npm renderer-browser 1.3.4 `dist/googleFontLoader.js:11` imports `./googlefonts.json` without `with { type: 'json' }` |
| esbuild bundle, packages inlined | **runs**; JSON identical to the browser's |
| renderer-browser swapped for an empty class | **runs**; identical |

The cockpit's copy of renderer-browser does not have the second blocker.
`web/patches/@videoflow+renderer-browser+1.3.4.patch` (49fedaa92, "chunk budget") replaces that JSON
registry with a two-family list. The patched package imports cleanly in plain Node, and the npm one
fails only on that line.

**Answer.**
- Yes, once bundled: no browser global is touched at import time or while compiling. The Studio
  always sets `sourceDuration`, so `compile()` never probes media (`core/dist/VideoFlow.js:288-296`).
- The only blockers are loader rules: extensionless TS imports, and the unpatched JSON import.
- `withLocalFonts` (`document`, `videoflow.ts:93-144`) and `primeDecodedBuffers`
  (`OfflineAudioContext`, `fetch`, `:328`, `:340`) need a page, but only when called.

**What S1.2 does not show.** Any other Studio module in Node: only `toVideoJSON`'s chain was
imported. It does not show that a sidecar with its own `package.json` gets the patched
renderer-browser either: patch-package runs from `web/`'s `postinstall`.

### S1.3 Parity: renderer-server as is, or our export page?

**Method.** `s1-parity.mjs <browser> <label> [ab] [capture-off]`:
- (a) `ServerRenderer.renderVideo` on the JSON our `toVideoJSON` compiled in the page;
- (b) our `exportProject` bundled unchanged (`src/studio.ts`), in the same browser binary with
  renderer-server's own launch flags (`lib/browsers.mjs`).

Both are read back with ffprobe and ffmpeg:
- `lib/measure.mjs` computes 100 ms mean-power windows exactly as
  `web/e2e/support/audioMeasure.ts` does;
- three frames are compared (PSNR and the share of pixels moved more than 16/255).

**(c), the cockpit's own Studio, was skipped.** It would upload the synthetic fixtures (three 21 s
1080p clips and the audio) into the operator's library and needs a new E2E driver. It would only show that Vite's bundle equals
esbuild's bundle of the same unchanged modules. The cockpit's ducking and envelope exports already
have their own E2E.

**Numbers, Chrome 154, offline.** Both pipelines give the same levels:

| window | expected | (a) | (b) |
|---|---|---|---|
| clip A tone 1–4 s | −15.05 dBFS | −15.01 | −15.01 |
| music up | −23.18: the mono bed exports at −21.24 (`web/e2e/video-studio-envelope.spec.ts:96`), × 0.8 | −23.20 | −23.20 |
| ducking depth | 12 dB | **11.98** | **11.98** |
| envelope drop | 6.02 dB | 6.01 | 6.01 |
| clip C at volume 0.5 | −21.07 dBFS | −21.13 | −21.13 |
| music fade-in, 5–6 → 6–7 s | rising | −34.08 → −25.53 | same |
| music fade-out, 52–53 → 54–55 s | falling | −30.69 → −43.43 | same |

Where they differ:
- Audio: a vs b over all 600 windows: **max difference 0.00 dB**.
- Crossfade (20.0 s) and fade-out (59.8 s) frames: **identical**, 0 pixels differ.
- Title frame (25.0 s): 1.2 % of pixels differ, PSNR 23.6 dB. Looking at the frames:
  - (a) offline burns a **fallback sans** into the title; its page requested fonts.googleapis.com
    for Noto Sans and Atkinson and both failed;
  - (b) renders **Atkinson Hyperlegible Next** from the image's own files (`withLocalFonts`),
    and made no external request.
- **Online** (bridge network, element capture off), (a) fetched both families from Google in 22
  requests. Its film was then **pixel-identical** to (b) on all three frames, with identical audio.
- Chromium CfT 151 and the headless shell give the same picture: audio 0.00 dB, only the title
  differs. Our page on Chrome against our page on Chromium: 0.07 dB max, frames 43–47 dB PSNR (the
  colour tags above).

**Time and memory, same run, same JSON:**

| | Chrome | Chromium CfT | headless shell |
|---|---|---|---|
| (a) default: element capture on | **390.6 s** | 423.5 s | — |
| (a) `elementCapture: false` | 127.9 s | — | 160.4 s |
| (b) our page | **104.8 s** | 124.6 s | 125.5 s |

renderer-server asks the page for element capture unless `elementCapture: false`
(`ServerRenderer.js:867-879`). The page feature-detects `drawElementImage`, which every build here
has (S1.1), so every default run logged "Compositing via element capture". The README pitches it
for speed and requires Chrome 149+ (README:253). On this CPU-only container it is ~3× **slower**.

At `--memory=4g` the Chrome (a) run was **OOM-killed** (`OOMKilled=true`, "Target crashed")
after its log reported "Encoding 80%". S2 has its peak.

**Answer. Our export page, not renderer-server as is.** As shipped, renderer-server:
- renders the wrong font offline;
- chooses its slowest and heaviest path on current Chrome.

Made equal (`elementCapture: false`, fonts reachable), it produces the same film as our page, so the
pixels are not what separates them. What separates them is where fonts and media come from, and the
page's own export path. Our page already carries `withLocalFonts` and `primeDecodedBuffers`.

**What S1.3 does not show.**
- A live cockpit export (c).
- Any transition other than a crossfade, image overlays, speed ≠ 1, a denoised clip, or a project
  longer than 60 s.
- Why the clip fade-out does not show. Plan A suspected `clipOpacity` (its FINDINGS, S1).
  - Clip C's JSON carries the fade, `opacity: [{time: 20.5, value: 1}, {time: 21, value: 0}]`, so
    at 59.8 s it should be at 0.4.
  - In both pipelines, on Chrome and Chromium, the frame at 59.8 s has a brightest pixel of
    RGB (233, 100, 90) and a mean R of 164.5. At 0.4 over black no channel could exceed 102.
  - So the fade-out is not visible. This is one frame, and clip C also carries a junction
    crossfade, so which of the two drops the keyframes was not isolated.
- A project in which two layers share a source, where `primeDecodedBuffers` saves most.

### S1.4 How each pipeline reaches its media

**Method.** `s1-media.mjs` renders the 5 s clip plus 5 s of music with the media on a **second
origin**, behind a presigned-style query (`?X-Amz-Algorithm=…&X-Amz-Signature=…`). That origin runs
once without `Access-Control-Allow-Origin` and once with it.

**Numbers.**

| | no CORS | with CORS |
|---|---|---|
| (a) renderer-server | picture OK, −14.8 dBFS, 1 GET per source | OK, 1 GET per source |
| (b) our page | **resolves "successfully" in 4.9 s: a 26,604 B file, BLACK picture (mean RGB 0,0,0), NO audio track** | OK, 9.1 s, −14.8 dBFS, **2 GETs per source** |

How each gets there:
- **(a)**: every page request goes through `page.route('**/*')`. Node fetches it with
  `route.fetch()` and rewrites the CORS headers (`ServerRenderer.js:633-655`).
- **(a), local paths**: these are rewritten to `https://videoflow.local/file/<uuid>` and read whole
  into memory (`:441-470`, `:605`).
- **(b)**: the page fetches the media itself. With the CORS rule missing:
  - `primeDecodedBuffers` swallows the failure by design (`videoflow.ts:340-343`,
    `.catch(() => null)`);
  - VideoFlow disables the video layer. Its only trace is a console warning, `layer "…" (video)
    failed to initialise — disabling it for this render. Failed to fetch`, and the export
    promise still resolves.
- **(b) fetches every source twice**: once for VideoFlow's media cache, and again at
  `videoflow.ts:340`. In the parity runs that was 2 GETs for each of the five sources.
- Query strings passed through untouched in both.

**Answer (design hint).**
- For Garage presigned GETs, (a)'s mechanism works whatever the bucket's CORS: the fetch happens in
  Node, not in the page.
- (b) works only with a CORS rule for the page's origin, or with the page's media routed through
  Playwright the way renderer-server does it.
- Either way a job must check its own output. (b) turns a media failure into a black, silent MP4
  and no error.

**What S1.4 does not show.** Real Garage, real expiry, a 403 or 404, a 50 MB source, or Range-only
servers.

## S2 — render time and memory at the appliance's budget

**Method.** `s2-bench.mjs`: one render per fresh container, `--cpus=4 --memory=6g`, offline. It
samples the container's cgroup v2 counters every 500 ms
(<https://docs.kernel.org/admin-guide/cgroup-v2.html>): `cpu.stat` for usage and throttling,
`memory.current`, `memory.stat` `anon`, and `memory.peak` at the end.
- `wallSeconds` covers compile, browser launch, render and the upload back to Node.
- RTF is film seconds ÷ wall seconds.
- `memory.peak` includes the page cache of the files read and written; `anon` is the processes'
  own memory.

**Numbers: runs with no other spike load on the VM or on its host:**

| run (60 s film) | wall s | RTF | mean cores (of 4) | samples ≥ 90 % of limit | anon max MiB | memory.peak MiB |
|---|---|---|---|---|---|---|
| (b) Chrome 1080p #1 | 112.8 | 0.532 | 2.65 | 7/225 | 986 | 1540 |
| (b) Chrome 1080p #2 | 112.2 | 0.535 | 2.66 | 6/223 | 941 | 1545 |
| (b) Chrome 1080p #3 | 113.8 | 0.528 | 2.65 | 7/227 | 949 | 1536 |
| (b) Chrome 1080p, later | 99.3 | 0.604 | 1.96 | 3/198 | 1041 | 1549 |
| (b) Chrome **720p** | 108.2 | 0.555 | 1.74 | 2/215 | 780 | 1383 |
| (b) headless shell 1080p | 104.8 | 0.573 | 1.81 | 1/209 | 696 | 1305 |
| (a) `elementCapture: false` | 111.0 | 0.541 | 2.77 | 9/219 | 1320 | 1987 |
| (a) default (element capture) | 326.1 | 0.184 | 1.42 | 2/648 | **2746** | **3398** |
| (a) `ffmpeg: true` | 744.6 | 0.081 | 1.52 | 6/1355 | 2663 | 3435 |

**Summary of the numbers:**
- **(b) Chrome 1080p, the three back-to-back runs: median 112.8 s**, spread 112.2–113.8 s (1.4 %),
  i.e. 1.9× the film's duration. Across all four clean runs: 99.3–113.8 s.
- 720p saves no time (108.2 s); resolution is not the bottleneck. It saves ~260 MiB anon and halves
  the file (24.2 MB against 49.9 MB).
- No run saturated its 4 CPUs. At most 9 of 219 samples (4.1 %) reached 90 % of the limit, and
  throttling was 0.1–6.6 s per run.
- Outputs:
  - every browser-export run: H.264 with Opus;
  - `ffmpeg: true`: **H.264 + AAC**, 31.4 MB;
  - 1080p at the default quality: (b) 49,931,583 B and renderer-server's default path
    49,933,869 B for the minute.

**Runs excluded, and why.**
- The VM was **paused for 3 h 11 min** mid-run. At 17:29 UTC `uptime` read 1 h 47 min, while the
  kernel's boot log is stamped 12:31 UTC. One 720p run straddled the pause (146.3 s).
- The VM runs on the operator's workstation, whose 16 threads it shares. Later runs overlapped
  another session driving the same scripts, or Docker builds on the host: 134.4, 114.8, 105.7,
  137.5, 138.5 and 136.9 s.
- Under that contention, (b) 1080p took up to **139 s** (2.3×).
- The host-CPU sampler for the last pair failed (localized performance-counter names), so host
  load is known only as "no spike work running", not as a number.

**What S2 does not transfer.**
- The seconds are this i7-11850H's, shared through VMware. The reference appliance's CPU (a
  GEEKOM Mini Air12) was not measured, and its per-core speed differs.
- The core counts (≤ 2.8 of 4) and the memory figures are properties of the render, and should
  carry over; the seconds should not.
- Sources B and C are low-entropy, so decode was cheap. Real footage decodes slower.
- A single 60 s project. Nothing longer, or with more layers.

## S3 — image footprint

**Method.** `Dockerfile.candidate`:
- `node:24-bookworm-slim`;
- `npm ci --omit=dev` of the spike's `package.json`: the three `@videoflow` packages, `mediabunny`,
  fvad, noise-suppressor, `playwright` and `esbuild`, 45 MB of `node_modules`;
- ONE browser via `playwright install --with-deps`.

There is no Debian ffmpeg. Playwright's own `ffmpeg-1011` screencast helper comes with every
install.

It was built on the workstation's Docker 29.8.1, the same engine and containerd image store as the
VM. The operator moved S3 off the VM to save time: image size does not depend on the host.
- That store's `Size` is unpacked + compressed.
- `docker save` exports the compressed blobs. Check: the Playwright image's save is 949,433,856 B,
  and its registry manifest sums to 949,411,114 B for amd64.
- Cross-check on the VM, before S3 moved: two candidates built there from the same Dockerfile.
  - Chromium (CfT): store `Size` 1,435,629,189 B, `docker save | gzip -6` 401,828,416 B.
  - Headless shell: 1,231,756,855 B and 331,985,516 B, with the same 648 MB install layer.

  Both equal the workstation's unpacked + compressed within 1 %.

**Numbers:**

| image | unpacked | compressed (pull) | install layer (history) | `du` inside |
|---|---|---|---|---|
| `node:24-bookworm-slim` (base) | 246 MB | 83 MB | — | — |
| candidate + **headless shell** | **897 MB** | **335 MB** | 648 MB, built in 70 s | ms-playwright 267M, `/usr/lib/x86_64-linux-gnu` 268M |
| candidate + Chromium (CfT) | 1,031 MB | 405 MB | 782 MB, 64 s | ms-playwright 394M, libs 268M |
| candidate + Google Chrome | 1,369 MB | 509 MB | 1.12 GB, 106 s | `/opt/google` 443M, libs 375M |
| `mcr.microsoft.com/playwright:v1.62.1-noble` | 2,571 MB | 949 MB (arm64: 960 MB) | — | Chromium, shell, Firefox, WebKit; Node 24.18.1, Ubuntu 24.04.4 |

The library directory includes the base image's own libraries. For scale, the VM's store reported
the spike's probe image (three browsers + Debian ffmpeg) at 3,077,976,487 B, unpacked +
compressed.

**Answer.**
- The smallest candidate is ~0.9 GB on disk and 335 MB to pull.
- Google Chrome costs +470 MB on disk.
- The Playwright image is 2.9× the smallest candidate, for browsers the sidecar does not use.

**The 7 GB budget.** The "7 GB" is **RAM at rest** for the whole default stack, not disk
(measured 2026-09-02 on the operator's 16 GB mini-PC, STT and TTS running). Against it, from S2:
- one (b) render adds 696–1,041 MiB anon, 1,305–1,549 MiB with page cache;
- renderer-server's default path adds 2,746 MiB anon, 3,398 MiB with page cache.

**What S3 does not show.** An image with ffmpeg. Any arm64 image. The runtime disk used by fixtures
and outputs.

## S4 — the two audio analyses in the render browser

**Method.** `s4-analyses.mjs <browser>` runs, in the export page, offline (`--network none`):
- the Studio's `detectSpeech` (`audioSpeech.ts`), `denoiseSamples` and `cleanedFile`
  (`audioClean.ts`), bundled unchanged;
- on Plan A's `speech.wav` and `speech-noisy.wav`;
- scored with Plan A's own code (`s3.ts` `score`, `s4.ts` `edges`).

**Numbers: identical on Chrome 154, Chromium 151 and the headless shell:**

| file | speech edges (ms) vs truth | misses / false | floor drop | speech loss | cleaned `.ogg` |
|---|---|---|---|---|---|
| `speech.wav` | [−10,58] [−2,63] [3,95] | 0 / 0 | — (gaps are digital silence) | 0.0 dB | 143,576 B Opus (Chrome 143,584) |
| `speech-noisy.wav` | [20,−62] [−2,63] [3,65] | 0 / 0 | **38.9 dB** | **0.6 dB** | 212,992 B; decoded back: 38.9 / 0.6 dB |

**Comparison with Plan A** (`spikes/video-studio-audio/FINDINGS.md`):
- The edges equal Plan A's "rnnoise + webrtc-3" row to the millisecond.
- Plan A's S3 measured 37.7 dB floor drop for 0.5 dB loss, **without** the 992-sample delay trim
  that `audioClean.ts` now applies. Here it is 38.9 dB for 0.6 dB, with the trim.

**Timing and network:**
- Each 12.9 s file takes 1.7–2.5 s to detect, 1.5–2.1 s to denoise and 2.0–2.7 s for the `.ogg`.
  Each includes the 500 ms worklet warm-up.
- Every request went to the page's own origin. `fvad.wasm`, `rnnoise_simd.wasm` and the RNNoise
  worklet were served from local files; no CDN.

**DOM assumptions.** None of these modules touch `document`. They need a Window's audio stack, which
Node lacks:
- `OfflineAudioContext`: `audioClean.ts:39`, `audioSpeech.ts:23`, `audioDecode.ts:31`;
- `audioWorklet.addModule`: `audioClean.ts:45`;
- `AudioBuffer`: `:80`;
- `File`: `:94`;
- `fetch(url, {credentials: 'same-origin'})`: `audioDecode.ts:27`. Cross-origin, the CORS
  caveat of S1.4 applies.

The one console 404 in every page was `/favicon.ico`.

**What S4 does not show.** Longer or real-world audio, and the analyses running alongside a render
in the same page.

## Decisions this forces (inputs for the design; the operator decides)

1. **Pipeline: our export page, bundled from the Studio's modules, driven by Playwright. Not
   renderer-server as is.**
   - As shipped, renderer-server burns a fallback font offline, and its automatic element capture is
     3× slower and 2.8× the memory here.
   - Made equal it gives the same film, so keeping it would mean overriding both its fonts and its
     capture choice.
2. **Media access:** fetch sources outside the page (Playwright routing to Node, as renderer-server
   does) or give Garage a CORS rule for the render page's origin. The first does not depend on
   bucket configuration.
3. **Verify every output before it becomes an asset:** check for non-black frames, an audio track
   and the duration.
   - A media failure in our page yields a black, silent MP4 with no error (S1.4).
   - The page's console does carry VideoFlow's "failed to initialise — disabling it for this
     render" warning, which a job can also treat as fatal.
4. **Browser:**
   - On x86-64, Chromium (CfT), its headless shell and Chrome are equivalent in codecs and output.
   - The headless shell is the smallest (335 MB compressed) and was the fastest and leanest here.
   - **arm64 is open**: Playwright will not install Chrome there, its registry has no CfT build
     there, and its own arm64 Chromium has an unmeasured codec set. It must be measured before an
     arm64 (DGX-Spark-class) target is promised.
5. **Audio codec:** Linux browsers write **Opus in MP4**; there is no AAC encoder. Either:
   - verify Opus-in-MP4 on Telegram and iOS, or
   - add an audio-only AAC pass with ffmpeg (renderer-server's ffmpeg path proves the AAC encoder
     works in the image).

   Neither is measured.
6. **Concurrency 1 per 4-thread budget.** One render uses 1.4–2.8 cores and never saturates 4, but
   two would need ~5.
7. **Timeout from S2:**
   - (b) 1080p took 1.65–1.9× the film's duration on a quiet host, and up to 2.3× under
     contention, on this CPU. The appliance's CPU is unmeasured.
   - A bound of ~3× duration + 60 s, plus a progress-stall watchdog, would cover what was seen.
     renderer-server uses a 120 s stall at `ServerRenderer.js:940`.
8. **Memory limit:** ≥ 2 GiB for (b), whose peak was 1,549 MiB with page cache. renderer-server's
   default path needs > 4 GiB.
9. **Size vs Telegram's 50 MB:**
   - A 60 s 1080p render of these synthetic sources was 49.9 MB at the default quality; 720p was
     24.2 MB, at no time saving.
   - The default is `QUALITY_VERY_HIGH`, "12 Mbps at 1080p / H.264" (renderer-browser
     `dist/videoQuality.d.ts`). Real footage should land nearer that, ~90 MB a minute. Not measured.
   - `RenderOptions` takes `videoQuality`/`videoBitrate`, but `exportProject` passes neither
     (`videoflow.ts:391-395`).
10. **Compile where the Studio does.** `toVideoJSON` runs in Node once bundled, but the export needs
    the page anyway. The sidecar must carry the cockpit's renderer-browser patch if it imports the
    package unbundled.

## Reproducing

```
node build.mjs                                   # WSL: bundles web/src/videoStudio into dist/
docker build -t vmr-probe:spike -f Dockerfile.probe .
docker run --rm -v $PWD/fixtures:/spike/fixtures vmr-probe:spike bash make-fixtures.sh   # needs fixtures-src/ (Plan A's four audio files)
docker run --rm --cpus=4 --memory=6g --memory-swap=6g --network none \
  -v $PWD/out:/spike/out -v $PWD/fixtures:/spike/fixtures vmr-probe:spike \
  node s1-parity.mjs chrome offline        # or s1-codecs / s1-render / s1-media / s2-bench / s4-analyses
node s1-node-compile.mjs out/vm/s1-videojson-chrome-offline.json   # WSL, next to web/
```
