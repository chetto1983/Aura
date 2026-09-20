---
spike: 108
idea: studio-media-editing
name: video-studio-audio
type: standard
validates: "Given a video lane of three layers over two source clips — the shape the multi-track editor produces and the one spike 107 never rendered — when VideoFlow's browser renderer exports it, then the two numbers the design rests on are measured rather than assumed: whether the audio mixer decodes once per source or once per layer, and whether the 0.1 ms `sourceStart` nudge proven at 24 fps still holds at 30, measured on the SAME composition at both frame rates so that frame rate and composition are not confounded"
verdict: VALIDATED
related: [105, 107]
tags: [studio, web, videoflow, audio, decode, mixer, frame-timing, 30fps]
---

# Spike 108: What a multi-clip video lane costs VideoFlow

## What This Validates

The multi-track video studio design (`docs/superpowers/specs/2026-09-20-studio-video-studio-design.md`)
rests on two numbers spike 107 never measured. 107 composed **one** video layer, so multi-clip audio
had never been rendered at all, and its `+0.1 ms` frame-sampling workaround was proven on a **24 fps**
source while a phone clip is 30. An adversarial review caught both. This spike measures them.

**Overall: VALIDATED — both answers are the unwelcome one, and the fix for the first is measured too.**

| Q | Question | Answer |
|---|---|---|
| 1 | Does the mixer decode once per source, or once per layer? | **Once per LAYER.** 3 layers / 2 sources → 3 decodes. 16 layers / 1 source → 16 decodes. Each decodes the WHOLE file, whatever `sourceDuration` says |
| 2 | Does the `+0.1 ms` nudge still hold at 30 fps? | **Still required, and it fixes every configuration completely.** Same composition, no nudge: 49/240 at 30 fps, 26/192 at 24 fps, 6/192 at 24 fps over a 30 fps source. With the nudge: **0 in all three** |
| 3 | (found on the way) Does `muted` silence a layer? | **No.** `muted` in *settings* is ignored; the mixer reads `mute` in *properties*. And neither saves the decode |
| 4 | Can a per-source cache be built without forking VideoFlow? | **Yes, measured.** Priming `layer.decodedBuffer` collapses 16 decodes to 1 and 8 to 1, for **bit-identical** audio |

## How to Run

The harness is spike 107's app — VideoFlow installed, the same-origin `loadFont` override, the
ComposeLab. This spike adds scenarios to it (`app/src/project.js`, `app/src/ComposeLab.jsx`); it does
not build a second app.

```bash
# 1. Media. Two 4 s clips differing only in their sine (440 / 880 Hz) at 30 fps, the same pair at
#    24 fps, and one 60 s stereo clip. Vite serves ../media as its publicDir, so they go there too.
cd .planning/spikes/108-video-studio-audio
for spec in "a:440:30" "b:880:30" "a24:440:24" "b24:880:24"; do
  IFS=: read -r name hz fps <<< "$spec"
  MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/out:/m" jrottenberg/ffmpeg:7.1-alpine \
    -hide_banner -loglevel error -y -f lavfi -i "testsrc2=s=320x180:r=$fps:d=4" -f lavfi -i "sine=f=$hz:d=4" \
    -c:v libx264 -pix_fmt yuv420p -c:a aac -shortest "/m/clip-$name.mp4"
done
MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W)/out:/m" jrottenberg/ffmpeg:7.1-alpine \
  -hide_banner -loglevel error -y -f lavfi -i testsrc2=s=320x180:r=30:d=60 \
  -f lavfi -i sine=f=440:d=60 -f lavfi -i sine=f=660:d=60 \
  -filter_complex "[1:a][2:a]join=inputs=2:channel_layout=stereo[a]" -map 0:v -map "[a]" \
  -c:v libx264 -pix_fmt yuv420p -c:a aac -shortest /m/clip-long.mp4
cp out/clip-a.mp4 out/clip-b.mp4 out/clip-a24.mp4 out/clip-b24.mp4 out/clip-long.mp4 \
   ../107-studio-timeline-editor/media/

# 2. Harness up (npm install first if node_modules/ is gone).
cd ../107-studio-timeline-editor/app && npm run dev      # http://localhost:5207

# 3. Measure: decode count, wall time, heap → out/audio-report.json + the MP4s.
cd ../../108-video-studio-audio && node run-audio.mjs 3

# 4. Read the results back with ffmpeg, independent of VideoFlow → out/probe-report.json.
node probe-audio.mjs
```

`out/` is git-ignored: no media and no render is committed. Every number below is from one run,
`measuredAt 2026-09-20T17:24:22Z`, Chrome 153 headless on Windows.

## Investigation Trail

1. **Read the mixer first.** `renderer-browser/dist/audio/mixer.js` — `renderMixedAudio` loops over
   enabled layers and calls `scheduleLayerOnContext` per layer, which calls `decodeLayerAudio` per
   layer. The only decoded-buffer cache, `layer.decodedBuffer`, is a field of `RuntimeAudioLayer`;
   `RuntimeVideoLayer` has no such field and hard-codes `get hasAudio() { return true; }`. What *is*
   shared is `loadedMedia.acquire(sourceUrl)` — the fetched **bytes**, not the decoded PCM. Reading
   says per-layer; the rest of this file measures it.

2. **The counter goes on `BaseAudioContext.prototype`, not `AudioContext.prototype`.** The mixer
   decodes on an `OfflineAudioContext`, and in Chrome neither subclass owns `decodeAudioData` — both
   inherit it. A patch on `AudioContext.prototype` would have counted zero and read as "no decodes".

3. **Decodes track layers, never sources.** Twenty-one renders over eighteen scenarios:

   | scenario | layers | distinct sources | `decodeAudioData` calls | PCM decoded |
   |---|---|---|---|---|
   | `audio` (the brief's lane) | 3 | **2** | **3** | 2.2 MB |
   | `split-1` | 1 | 1 | 1 | 0.7 MB |
   | `split-2` | 2 | 1 | 2 | 1.5 MB |
   | `split-4` | 4 | 1 | **4** | 2.9 MB |
   | `split-8` | 8 | 1 | 8 | 5.9 MB |
   | `split-16` | 16 | **1** | **16** | 11.7 MB |
   | `long-1` (60 s stereo) | 1 | 1 | 1 | 22.0 MB |
   | `long-4` | 4 | 1 | 4 | 87.9 MB |
   | `long-8` | 8 | **1** | **8** | 175.8 MB |

   The review's prediction was exact: 3 decodes for 2 sources. A clip split in four costs four full
   decodes of one file. Every decode reads the **whole** source — each `split-16` layer covers 0.25 s
   of a 4 s clip and still yields a 4.000 s buffer (`{"seconds":4,"channels":1,"sampleRate":48000,
   "pcmBytes":768000}`, straight off the decoded `AudioBuffer`). `sourceDuration` narrows the
   *scheduling* (`bufferSource.start(when, offset, duration)`), never the decode.

4. **What it costs.** Decode time per call is tight — 8–11 ms for the 4 s mono clip over 61 calls,
   229–236 ms for the 60 s stereo one over 13. Render wall time is not: the same scenarios moved by
   up to ±40 % between runs on this machine, so read the last column as an order of magnitude.

   | scenario | decode time | share of the render | render |
   |---|---|---|---|
   | `audio`, 3 layers of 4 s mono | 26 ms over 3 calls | 3.2 % | 691 / 715 / 821 ms |
   | `split-8`, 4 s mono | 72 ms over 8 calls | 13.4 % | 539 ms |
   | `split-16`, 4 s mono | 140 ms over 16 calls | 20.4 % | 687 ms |
   | `long-1`, 60 s stereo | 236 ms | 5.9 % | 3 973 ms |
   | `long-4`, 60 s stereo | 925 ms over 4 calls | 20.2 % | 4 568 ms |
   | `long-8`, 60 s stereo | **1 847 ms** over 8 calls | **32.7 %** | 5 644 ms |

   Splitting one 60 s clip into eight adds **1.61 s of pure decode** and holds **175.8 MB of PCM
   alive at once** — the mixer connects every decoded buffer to the `OfflineAudioContext`'s
   destination and only releases them when `startRendering()` returns.

5. **`performance.memory` cannot see any of that, measured.** The brief asked for `usedJSHeapSize`
   before and after. It is the wrong instrument and the control proves it: holding 1…8 decoded 60 s
   stereo buffers alive, forcing GC between each, `usedJSHeapSize` moved by at most **4 226 bytes**
   at any step — +0.0 MB to a tenth — all the way to 175.8 MB of PCM. AudioBuffer backing stores are
   not on the V8 heap. The heap figures the renders do show (25.1–35.5 MB for the 4 s scenarios,
   26.9 → 80.6 MB for `long-8`) are the fetched `ArrayBuffer`s and the encoder's own allocations, not
   the audio. Process-level probes did not close the gap either — see *What This Does NOT Prove*.

6. **`muted` in settings does nothing; `mute` in properties does.** The brief's third layer carries
   `muted: true` in its **settings**. `readMute` reads `layer.json.properties.mute`, so the layer
   plays. Measured per second of the export (RMS and Goertzel energy at 440 Hz = clip-a, 880 Hz =
   clip-b):

   | second | 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 |
   |---|---|---|---|---|---|---|---|---|
   | brief's lane, RMS | 0.088 | 0.088 | 0.088 | 0.088 | 0.089 | 0.088 | **0.088** | **0.088** |
   | with `properties.mute`, RMS | 0.088 | 0.088 | 0.088 | 0.088 | 0.089 | 0.088 | **0.00004** | **0** |
   | dominant tone | 440 | 440 | 440 | 440 | 880 | 880 | 880 | 880 |

   The lane itself is right: seconds 0–3 are clip-a's 440 Hz, seconds 4–7 clip-b's 880 Hz, with the
   off-tone energy at 0.00000–0.00003. And the mute costs nothing to *decode*: `audio-mute-prop`
   still made **3** `decodeAudioData` calls. `scheduleBufferOnContext` checks `readMute` **after**
   `decodeLayerAudio` has already run.

7. **The nudge, measured as a controlled comparison.** The first version of this spike compared its
   own 49/240 at 30 fps against 107's 10/96 at 24 and concluded the nudge was "worse at 30 fps".
   That was not a measurement: 107 rendered **one** layer over a 24 fps source, this spike renders
   **three** over a 30 fps source, so frame rate, sampling ratio and composition all moved at once.
   The same composition is therefore rendered in three configurations, with and without the nudge.
   Both clips of each pair are the same `testsrc2` video, which the probe asserts rather than assumes
   (worst per-channel frame difference 0.0000 for both pairs), so every output frame is matched by
   argmin of the full-frame difference against one clip's frames.

   | project fps | source fps | output frames | frames on a source boundary | mismatches, no nudge | with the nudge |
   |---|---|---|---|---|---|
   | 30 | 30 | 240 | 240 (100 %) | **49** (20.4 %) | **0** |
   | 24 | 30 | 192 | 48 (25 %) | **6** (3.1 %) | **0** |
   | 24 | 24 | 192 | 192 (100 %) | **26** (13.5 %) | **0** |

   Four things follow, and only these four:

   - **The nudge is required in every configuration and fixes every one completely.** That is the
     load-bearing result, and it is now proven at two frame rates and two sampling ratios rather
     than at one.
   - **Like for like — same composition, same 1:1 sampling ratio — 30 fps does mismatch more than
     24: 20.4 % against 13.5 %.** This is the claim the review challenged, now measured directly
     instead of inferred across two spikes.
   - **The sampling ratio dominates the frame rate.** A frame can only hit the defect when its
     timeline second lands exactly on a source frame boundary, i.e. when `n × srcFps` is divisible
     by `projectFps`. At 1:1 every frame does; at 24 fps over a 30 fps source only one in four does,
     and the count drops to 6 of 192. Dropping the project to 24 fps at the same 1:1 ratio helps
     modestly (20.4 % → 13.5 %); a non-integer ratio helps far more (→ 3.1 %).
   - **Composition matters as much as frame rate.** Within a single run, with everything else held:

     | layer | 30 fps / 30 fps | 24 fps / 30 fps | 24 fps / 24 fps |
     |---|---|---|---|
     | one (0–4 s, `sourceStart` 0) | 14/120 = 11.7 % | 2/96 = 2.1 % | **10/96 = 10.4 %** |
     | two (4–6 s, `sourceStart` 0) | 27/60 = **45.0 %** | 4/48 = 8.3 % | 16/48 = **33.3 %** |
     | three (6–8 s, `sourceStart` 2) | 8/60 = 13.3 % | 0/48 = 0 % | 0/48 = 0 % |

   **107's number reproduces to the frame.** 107 rendered one layer at 24 fps over a 24 fps source
   and measured 10/96. This spike's layer one at 24 fps / 24 fps — the same shape — is **10/96**.
   So 107's figure was neither wrong nor a lucky best case; it was the single-layer, origin-aligned
   case, and setting it beside a whole three-layer lane's 49/240 compared two different things. The
   like-for-like comparison is layer one to layer one: 10.4 % at 24 fps, 11.7 % at 30.

   Every one of the 81 mismatches across the three configurations is off by exactly **−1** (the
   previous source frame), matching 107's diagnosis in `VideoFrameSource.holds(t)`. The six counts
   reproduced **exactly** — 49 / 6 / 26 and 0 / 0 / 0 — in two independent runs.

8. **The fix needs no fork, and it was measured rather than argued.** `decodeLayerAudio` reads
   `layer.decodedBuffer` off **any** layer object, although only `RuntimeAudioLayer` ever writes it,
   and `initLayers()` is idempotent (guarded by `elementsSetup`). So a caller can build the runtime
   layers, prime `decodedBuffer` with one buffer per distinct `source`, and then export
   (`primeDecodedBuffers` in `ComposeLab.jsx`: ~12 lines, no patched prototype, no fork):

   | scenario | decodes | PCM decoded | decode time | render |
   |---|---|---|---|---|
   | `audio-mute-prop` per-layer | 3 | 2.2 MB | 26 ms | 661 ms |
   | `audio-mute-prop` per-source | **2** | 1.5 MB | 18 ms | 674 ms (+2 %) |
   | `split-16` per-layer | 16 | 11.7 MB | 140 ms | 687 ms |
   | `split-16` per-source | **1** | 0.7 MB | 10 ms | 523 ms (−24 %) |
   | `long-8` per-layer | 8 | 175.8 MB | 1 847 ms | 5 644 ms |
   | `long-8` per-source | **1** | 22.0 MB | 234 ms | 4 396 ms (**−22 %**) |

   The decode collapse is exact and reproduced identically in both runs; the render-time saving is
   the noisy part, measured at −22 % and −40 % for `long-8` in the two runs. At three layers of a
   4 s clip it saves 8 ms, which is inside the noise — the cache pays where there is something to
   save. The output is not merely the same size: the exported MP4s are byte-for-byte the same length
   and their decoded audio is **identical sample for sample**, worst delta `0` over 384 000, 194 560
   and 2 880 512 samples.

9. **The lane is otherwise valid.** ffprobe of the export: `8.000000 s`, `622066 B`, one video stream
   (`h264 320×180 @ 30/1, 240 frames`) and **one** audio stream (`aac 2ch @ 48000`) — the two mono
   44.1 kHz sources come out as a single stereo 48 kHz mix. Worker and main thread give identical
   per-second audio to five decimals (the main-thread MP4 is larger, 881 791 B, as in 107). All
   three frame-rate configurations export a `duration` of 8 s. 63 requests, **0 off-origin**, 0
   console errors.

## Results

**VALIDATED.** The numbers Task 5 branches on:

- **Decodes = number of video layers, independent of distinct sources.** 3 for 2 sources; 16 for 1.
- **Every decode is the whole source**, regardless of `sourceStart` / `sourceDuration`:
  768 000 B of PCM for a 4 s mono clip, 23 040 000 B for a 60 s stereo one.
- **All of them are held simultaneously** until `startRendering()` returns: 175.8 MB for eight layers
  of a 60 s stereo clip.
- **Cost:** 8–11 ms per decode for a 4 s mono clip, **229–236 ms** for a 60 s stereo one — 1.85 s,
  **32.7 %** of a 5.6 s render, for eight layers of one file.
- **A per-source cache needs no fork and changes nothing audible:** 16 decodes → 1, 8 → 1, decode
  time 140 → 10 ms and 1 847 → 234 ms, audio identical sample for sample.
- **The nudge (`sourceStart + 1e-4`) is required at 24 fps and at 30, and fixes both completely:**
  49/240, 26/192 and 6/192 mismatches without it, **0** in all three with it.
- **At an identical 1:1 sampling ratio the same composition mismatches more at 30 fps than at 24**
  (20.4 % vs 13.5 %), but the ratio between project and source frame rate matters more than either
  rate, and the three layers of one lane spread far more than the two rates do: layer two
  (start 4 s, `sourceStart` 0) mismatched 3–4× as often as layer one at both rates.
- **`muted` in settings is a no-op**; `mute` belongs in `properties`, and it does not save the decode.

### The decision for Task 5

**One decoded buffer per source, cached by the adapter — not per layer as VideoFlow does it.**

The cost grows with the edit, not with the footage: a 60 s clip cut into eight pieces costs 8 full
decodes, 1.85 s of the render and 176 MB of live PCM to produce audio that one 22 MB buffer already
contains. A cut is the commonest gesture in the editor, so the pathological case is the normal case.

The seam is already in VideoFlow and needs no fork. After `renderer.initLayers()`, group
`renderer.layers` by `settings.source`, decode each distinct source once on an `OfflineAudioContext`
at 48 000 Hz, assign the same `AudioBuffer` to every layer's `decodedBuffer`, then export —
`initLayers()` is idempotent, and `decodeLayerAudio` returns the primed buffer before it fetches
anything. `scheduleBufferOnContext` still applies `sourceStart` / `sourceDuration` / `speed` /
`mute` per layer, so no per-layer semantics change; measured at 8 decodes → 1 on `long-8` for
bit-identical audio.

Two riders, both measured:

- **The nudge stays, on EVERY video layer, including the ones whose `sourceStart` is already 0.**
  It is needed at 24 fps and at 30, and it is worth an upstream issue. Two traps the measurement
  closes: do not size the workaround to a frame rate (what drives the rate is the project/source
  ratio and where the layer starts, not the project fps), and **do not gate it on
  `sourceStart > 0`** — layer two of this lane has `sourceStart: 0` and is the **worst** layer
  measured, 27/60 = 45.0 % at 30 fps and 16/48 = 33.3 % at 24. What this run shows is only that
  `sourceStart > 0` does not predict vulnerability; it does **not** isolate what does. All three
  layers have an integer `startTime` (0, 4, 6 s) and still differ 11.7 / 45.0 / 13.3 %, so nudge
  every video layer unconditionally rather than reasoning about which ones need it.
- **Mute must be written to `properties.mute`.** A `muted` setting silences nothing, and muting a
  layer never avoids its decode — which is another reason the cache belongs in the adapter.

## What This Does NOT Prove

- **Where the PCM actually lives.** That `usedJSHeapSize` is blind to it is measured (at most
  4 226 B of movement for 175.8 MB of buffers, eight steps). Where it *does* live is not: both
  Windows counters tried, `WorkingSetSize` and `PrivatePageCount` summed over the headless Chrome
  processes, moved by less than Chrome's own page-load allocations were still settling by — in one
  run **downward** while the buffers were held. `run-audio.mjs` still prints that line, labelled
  inconclusive, so nobody repeats it. The 175.8 MB is arithmetic over the real `AudioBuffer`s
  (`length × channels × 4`), not an observed process footprint. A cross-origin-isolated
  `performance.measureUserAgentSpecificMemory()` would settle it and was not set up.
- **Three frame-rate configurations, not a model of the defect.** 30/30, 24/30 and 24/24 were
  measured on one composition. 25, 29.97, 50 and 60 fps, VFR sources, and every other
  project/source ratio are untested, and nothing here predicts a count for them — the "frames on a
  source boundary" column explains the three measured counts, it is not a fitted law. Whether the
  nudge shifts audio sync was not measured at any rate, and no value smaller than `1e-4` s was
  bisected.
- **The 30 vs 24 difference rests on one composition.** 20.4 % against 13.5 % is one three-layer
  lane at each rate, not a distribution over compositions. The per-layer table says the spread
  *within* a single lane (11.7 % to 45.0 %) is wider than the gap *between* the two rates, so a
  different lane could plausibly reverse the ordering.
- **The cache under the editor's real lifecycle.** It was measured on a fresh renderer per export.
  Nothing here says when to evict, what a re-export after an edit costs, or whether the same
  `AudioBuffer` can be shared with `DomRenderer`'s live preview context at the same time.
- **One browser.** Chrome 153 headless on Windows only. No Edge, Firefox or Safari/iOS — and Safari's
  `OfflineAudioContext` is exactly where a per-layer decode would hurt most.
- **Synthetic media.** `testsrc2` + sine, 320×180, CFR H.264, mono 44.1 kHz (and one 60 s stereo).
  No phone clip, no VFR, no HEVC, no 1080p, no silent source. `RuntimeVideoLayer.hasAudio` is
  hard-coded `true`, so a video with **no** audio track was not measured here — it would still be
  fetched and handed to `decodeAudioData`, which returns null on failure.
- **Wall times are not a benchmark.** One run per scenario, on a machine also running Docker and a
  Vite dev server; the same scenario moved by up to ±40 % between runs, and the cache's render-time
  saving on `long-8` read −22 % in one run and −40 % in the other. Only the decode counts and the
  frame-mismatch counts are exact integers, identical across every run.
- **Not the whole mixer.** Groups, transitions, `pitch`, `speed`, `pan` and volume keyframes were not
  exercised. `renderGroupAudio` opens a **nested** `OfflineAudioContext` per group, so a grouped
  timeline may multiply the cost measured here; that is unmeasured, and so is whether the
  `decodedBuffer` seam survives a group sub-mix.
- **Nothing about the editor.** No UI, no model, no integration into `web/`. This is the render path
  alone, driven by scripted scenarios.
