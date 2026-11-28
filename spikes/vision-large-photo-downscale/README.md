# Spike: downscaling photos over the vision pixel cap

Date: 2026-11-28. A measurement only: no production code, no Dockerfile or compose change.

`internal/multimodal/vision_downscale.go` `DownscaleForVision` shrinks an image to a 1024 px long
edge (JPEG q85) before a vision model sees it. Before decoding, it refuses any image over a pixel
cap: 12.36 MP for YCbCr JPEG, 10.11 MP for RGB(A) PNG. Go's `image/jpeg` decodes at full
resolution and keeps every progressive coefficient. The `aura` container has `mem_limit: 768m`
and `cpus: 1.0`. So a 24 MP camera photo, or a 48–50 MP phone photo, is never resized. It reaches
the model as uploaded (it can be over Claude's 5 MB per image), or it reaches it as reference
text (`internal/assets/turn_media_loader.go` `visionReady`).

The question: which decoder can bring those photos to 1024 px inside the memory aura has?

## The budget a candidate must meet

The file's bounds allow one decode **about 300 MiB** over aura's own heap. On VM .158 aura peaked
at 452 MiB with two 12 MP photos in one turn, which leaves 316 MiB of the 768 MiB. A process that
aura execs shares aura's cgroup, so that budget is also the budget for djpeg or photocraft-cli.
Two other places matter. `aura-ingest`, where `aura-media-index` runs, has no `mem_limit` in
`compose.yaml`. The identity's box, where photocraft runs today, has 2 GiB
(`internal/config/config_sandbox.go`). Two byte caps sit upstream: an uploaded image may be up to
25 MiB (`AURA_ASSET_MAX_IMAGE_BYTES`), and an image attached in a turn up to 2 MiB
(`maxAttachBytes`).

## Test images (synthetic; generated, never the operator's files)

`gen_images.py` uses Pillow 13.0.0 and numpy 2.5.5 in a throwaway uv venv. Each image is an RGB
gradient plus Gaussian noise (σ 12, seeds fixed), the method the file's own table used. The PNG
has no noise, so it fits under the 25 MiB upload cap; a PNG's decode memory depends on its pixel
count, not its byte size. The images live in `/tmp/vision-spike/img` in WSL, not in the repo.
Generation: `10_generate.sh`, then `11_regen_png.sh` for the noiseless PNG.

| file | size | pixels | kind |
|---|---:|---|---|
| p12_control.jpg | 4.93 MB | 4032x3024 (12.2 MP, under the cap) | baseline 4:2:0 q92 |
| p24_progressive.jpg | 8.63 MB | 6000x4000 (24 MP) | progressive 4:2:0 q92 |
| p48_baseline.jpg | 19.73 MB | 8064x6048 (48.8 MP) | baseline 4:2:0 q92 |
| p48_progressive.jpg | 17.54 MB | 8064x6048 | progressive 4:2:0 q92 |
| p48_444_progressive.jpg | 35.55 MB | 8064x6048 | progressive **4:4:4** q92: the file's worst family. Over the 25 MiB upload cap, so it is a memory worst case only |
| p48.png | 1.17 MB | 8000x6000 (48 MP) | 8-bit RGB PNG |

## Method

- Each run happens in a fresh container started with
  `--memory 768m --memory-swap 768m --cpus 1.0`, the limits compose gives aura, with no swap.
  A process over the limit is therefore killed, not paged.
- Every command is wrapped by `rss.go`. It reports the child's `ru_maxrss` (its VmHWM, the primary
  figure), the wall time, the exit code or signal, and the cgroup's `memory.peak`. That peak
  includes the page cache of the input file, so it is only a cross-check.
- Go harnesses are static `CGO_ENABLED=0` builds run in `debian:bookworm-slim`, the base of aura.
- photocraft-cli is the binary extracted from `ghcr.io/chetto1983/aura-sandbox:edge`
  (0.23.1, sha256 `def5bcec…875c`). It runs in `debian:bookworm-slim` too: it needs glibc 2.35, and
  bookworm has 2.36. One row reruns it in the sandbox image, with the same result.
- djpeg runs in a throwaway image: `debian:bookworm-slim` plus `libjpeg-turbo-progs` 1:2.1.5-2.
  That is the same `libjpeg62-turbo` that `aura:edge` already carries, pulled in by
  poppler and libtiff.
- **Today** is `baseline.go`, which calls the real `multimodal.DownscaleForVision`.
  **djpeg** decodes at the largest 1/N (N in 2, 4, 8) that keeps the long edge ≥ 1024 px:
  `djpeg -scale 1/N -bmp`. Then `baseline.go` finishes that BMP through the unchanged
  `DownscaleForVision`; the spike registers `x/image/bmp` for this. **jpegn** is
  `jpegnprobe/main.go`: the same 1/N rule through `jpegn.Options.ScaleDenom`, then
  `DownscaleForVision`'s own CatmullRom on white and JPEG q85. **jpegli** is
  `jpegliprobe/main.go`: `DecodingOptions.ScaleTarget` is set to the 1024 px target, and its C
  wrapper (`lib/jpegli.c`) picks the smallest M/8 that still covers that target; the finish is
  the same. **PhotoCraft** is
  `photocraft-cli run <in> --cmd image.imageSize --params '{"width":1024}' --out <out>.jpg --quality 85 --no-gpu`.
- `30_measure.sh` ran twice (r1, r2). `31_followup.sh` holds a **452 MiB ballast** in `/dev/shm`,
  which the container's memory cgroup is charged for, to reproduce aura's VM peak. It also runs
  PhotoCraft with `--approve-high-memory` and with no limit (4 GiB). `32_djpeg_maxmemory.sh` tests
  djpeg's `-maxmemory`. `33_photocraft_box.sh` runs PhotoCraft under the box's 2 GiB limit.
  `34_jpegli.sh` runs jpegli twice, alone and beside the ballast.
- `results/10_generate.txt` still lists the first PNG, a noisy one of 53 MB. `11_regen_png.sh` replaced it
  with the 1.17 MB noiseless PNG, which every run used.
- `40_verify.py` opens each output and checks its size. It then compares the output with a Pillow
  reference: a full decode, resized with LANCZOS to the same size. Run logs: `results/*.txt` (the spike `.gitignore` drops `*.log`).

## Numbers

Peak RSS is in MiB, from `ru_maxrss`. An empty Go harness process is 16 MiB. Wall time is r1 / r2
on one CPU; the host was shared, so r2 ran 1.2–2.5x slower. Every JPEG and PNG output is
1024x768, except the 24 MP one at 1024x682 (PhotoCraft: 683).

| image | today (DownscaleForVision) | djpeg 1/N (+ Go finish) | jpegn 1/N (in-process) | jpegli M/8 (wazero, in-process) | PhotoCraft CLI |
|---|---|---|---|---|---|
| p12_control (12.2 MP) | **138** MiB, 0.9 / 1.7 s, 56 KB | 1/2: **11** + 89 MiB, 0.08 + 0.17 s | **63** MiB, 0.26 / 0.44 s | 3/8: 81 MiB, 0.53 / 0.50 s | 104 MiB, 0.65 / 0.81 s |
| p24_progressive | refused (pixel cap) | 1/4: **75** + 62 MiB, 0.38 + 0.10 s | 188 MiB, 0.69 / 0.96 s | 284–293 MiB, 2.5 / 1.2 s | 190 MiB, 1.3 / 2.4 s |
| p48_baseline | refused | 1/4: **11** + 88 MiB, 0.29 + 0.18 s | **88** MiB, 0.58 / 1.30 s | 137 MiB, 2.1 / 1.0 s | refused by its own admission at 768 MiB (174 MiB RSS when it refused); no limit: 369 MiB, 2.5 s |
| p48_progressive | refused | 1/4: **150** + 88 MiB, 0.87 + 0.22 s (1/1: 281 MiB; 1/8: 144 MiB) | 363 MiB, 1.7 / 3.1 s | 574–575 MiB, 6.2 / 2.5 s | refused at 768 MiB (311 MiB RSS); no limit: 367 MiB, 2.2 s |
| p48_444_progressive | refused | 1/4: **290** + 89 MiB, 1.4 + 0.16 s | **677** MiB, 2.7 / 3.5 s | **OOM-killed** at 768 MiB, both runs (770 MiB RSS) | refused at 768 MiB (467 MiB RSS); no limit: 467 MiB, 2.9 s |
| p48.png | refused | n/a (JPEG only) | n/a (JPEG only) | n/a (JPEG only) | 252 MiB, 1.6 / 2.1 s |

**Beside aura's 452 MiB peak** (`results/31_followup_r1.txt`, `results/33_photocraft_box.txt`), in the 316 MiB
left of 768:

| candidate | p48_baseline | p48_progressive | p48_444_progressive | smaller images |
|---|---|---|---|---|
| djpeg 1/4 | ok (cgroup peak 474 MiB) | ok (614 MiB) | ok (753 of 768 MiB, page cache of the 35 MB input included) | — |
| jpegn | ok (543 MiB) | **OOM-killed** | **OOM-killed** | — |
| jpegli (`results/34_jpegli.txt`, two runs) | ok (588 MiB) | **OOM-killed** | **OOM-killed** | — |
| PhotoCraft | — | — | — | **refuses** p12_control, p24_progressive and p48.png ("not enough memory to open"), `--approve-high-memory` or not |

**PhotoCraft under the box's 2 GiB limit** (`results/33_photocraft_box.txt`): all four 48 MP images are
resized. The JPEGs take 368–467 MiB and 3.3–5.1 s; the PNG takes 252 MiB and 1.4 s.

**djpeg `-maxmemory`** (`results/32_djpeg_maxmemory.txt`): libjpeg-turbo ships no backing store
(`jmemnobs`). A decode that needs more than N KB fails with "Backing store not supported" in under
10 ms and at 2 MiB RSS, before any big allocation. At 300000 KB it refused the 4:4:4 progressive
image (which needs 290 MiB) and passed the other two. The cap fails closed and never OOMs.

**Fidelity** (`results/41_verify.txt`), as PSNR against the Pillow LANCZOS reference:
- p12_control: today 39.6 dB, djpeg 39.5 dB, jpegn 39.6 dB, jpegli 39.6 dB, PhotoCraft 39.5 dB;
- every 48 MP JPEG output that was produced: 43.8–44.2 dB for every candidate;
- p24: 42.0 dB for djpeg, jpegn and jpegli, 41.9 dB for PhotoCraft;
- the PNG through PhotoCraft: 50.6 dB.

The 1/N decode loses nothing measurable here against a full decode.

## Candidates and verdicts

### 1. PhotoCraft `photocraft-cli` 0.23.1 — **INVALIDATED** inside aura's cgroup; **PARTIAL** in aura-ingest and in the box

- **A one-shot mode exists; no MCP is needed.** The CLI's `--help` lists `convert`, `info`, `run`,
  `batch`, `droplet`, `serve` and `mcp`. `run <file> --cmd image.imageSize --params {...} --out x.jpg --quality 85`
  is a single call. The parameters of `image.imageSize` come from `commands --json`:
  `{width, height, units, percent, resolution, resample=bicubic|lanczos|…}`. One side keeps the ratio.
- **It decodes at full size.**
  - Measured: it holds 367–467 MiB for a 48 MP JPEG, more than the 300 MiB budget. Even `info`
    hits the same memory refusal at 311 MiB RSS, so opening the file is already the full decode.
  - The source agrees. PhotoCraft decodes JPEG through `zune-jpeg` (craft-libs
    `crates/image-codecs/src/codecs/jpeg.rs` @ `3eb5b528`). craft-jpeg has a `decode_reduced`
    (1/2, 1/4, 1/8 IDCT), but a code search finds callers only in `crates/raw`, for DNG previews.
- **Its own admission check refuses first** (`crates/io/src/memory.rs` @ v0.23.1).
  - It reads its cgroup's free memory, and its automatic room is "half of free, leaving 1 GiB, or
    a quarter of free".
  - At 768 MiB it refuses every 48 MP JPEG, yet it accepts the 48 MP PNG. Beside aura's 452 MiB
    it refuses even the 12 MP control.
  - `--approve-high-memory` changed nothing on the JPEGs it was tried on; its `--help` scopes the
    flag to PSD/PSB.
  - The refusal is clean, an error rather than an OOM. But the image is not resized.
- **Licence.** MIT OR Apache-2.0, plus the ArtCraft brand terms (`docs/brand/LICENSE-brand.txt`).
  An unmodified official build may be shipped with its NOTICE, licences and OFL font files, which
  is what `docker/aura-sandbox/Dockerfile` already does. Shipping it in aura-ingest would raise no
  licensing problem.
- **Size and architectures.** The binary is 113 MB (`aura:edge` is 1.6 GB). Releases exist for
  x86_64 and aarch64; both SHA-256 are pinned in the sandbox Dockerfile. It links only libc, libm
  and libgcc_s.
- **Where it works:** wherever 370–470 MiB is free beside it.
  - In the box (2 GiB) and with no limit (aura-ingest has no `mem_limit`), it resized all four
    48 MP images, PNG included.
  - So it is PARTIAL there, with these constraints:
    - a 113 MB binary;
    - 370–470 MiB per 48 MP decode;
    - a refusal that depends on the host's or cgroup's free memory at that moment;
    - 2–5 s per photo on one CPU.
  - The box route is the existing `photocraft__*` MCP. It needs a running box, and it was not
    measured through the live mount.

### 2. libjpeg-turbo `djpeg -scale 1/N` — **VALIDATED** that it fits the memory, for JPEG only

- Worst case: 290 MiB for a 48 MP 4:4:4 progressive JPEG. A 48 MP 4:2:0 progressive JPEG takes
  150 MiB, and a baseline JPEG of any size about 11 MiB. All three ran beside the 452 MiB ballast
  without a kill. The Go finish on the 1/4 BMP adds about 72 MiB, it is unchanged code, and the
  output quality is equal to today's.
- What sets the memory is the coefficient buffer, about 2 bytes per coefficient: 3 B/px at 4:2:0
  and 6 B/px at 4:4:4. A progressive image needs the whole buffer, so 1/8 saves almost nothing
  over 1/4 (144 vs 150 MiB). A baseline image streams. The figure can be computed from the SOF
  header before any exec, and `-maxmemory` enforces it fail-closed.
- Cost: `libjpeg-turbo-progs`, 358 KB installed. Its library, `libjpeg62-turbo` 2.1.5, is already
  in `aura:edge`, pulled in by `libpoppler126` (`poppler-utils`) and `libtiff6`.
  - `docker/aura-ingest/Dockerfile` installs `poppler-utils` from the same bookworm, so the library
    is very likely there too. That image was not inspected.
  - arm64 availability is the Debian archive's, and it was not checked.
- **Open design points the spec must settle:**
  - Read djpeg's output to the end, and let djpeg exit, before the Go decode. Run at the same
    time, the 290 MiB and the 72 MiB add up.
  - Pass `-maxmemory`, set above the decode's RSS. At 300000 KB it refused the 4:4:4 image, whose
    RSS was 290 MiB. Pass `-maxscans` too, against a scan-count CPU bomb.
  - Choose N from the header: the largest of 2, 4 and 8 that keeps the long edge ≥ 1024. djpeg
    also accepts any M/N, such as `3/8`.
  - The output is BMP or PNM, and `DownscaleForVision` registers neither today.
  - It is an exec of a C decoder on untrusted input. libjpeg-turbo is heavily fuzzed (OSS-Fuzz),
    but this is an attack surface aura does not have today.
  - It does nothing for PNG.

### 3. Pure Go / wazero with a scaled decode — **PARTIAL**, baseline JPEG only (jpegn better than jpegli)

- **Inventory.** `go.mod` has only `golang.org/x/image` v0.48.0, which has no scaled decode.
  Two real options:
  - `gen2brain/jpegn` v0.7.1: MIT, no dependencies, SIMD for amd64, arm64 and riscv64.
    `Options.ScaleDenom` scales by 1/2, 1/4 or 1/8 inside the IDCT. Its README says that a
    progressive image "is decoded whole first".
  - `gen2brain/jpegli` v0.4.2: jpegli compiled to WASM and run on wazero v1.12.0. On arm64 it
    becomes pure Go through wasm2go. `DecodingOptions.ScaleTarget` maps to libjpeg's
    `scale_num/8` in `lib/jpegli.c`.
- **Measured.**
  - jpegn:
    - a 48 MP baseline JPEG takes 88 MiB, in-process with no exec, and it survived the ballast;
    - a progressive JPEG takes 363 MiB at 4:2:0 and 677 MiB at 4:4:4, worse than libjpeg-turbo;
      both were OOM-killed beside the ballast.
  - jpegli is worse still:
    - 137 MiB for the baseline JPEG, which survived the ballast;
    - 574 MiB for the 4:2:0 progressive;
    - the 4:4:4 progressive was OOM-killed even alone at 768 MiB;
    - it is 2–4x slower than jpegn.
  - Output quality equals the other candidates.
- **Constraints.**
  - Either library would need its own cap for progressive frames. That cap sits about where
    today's does, so progressive gains nothing.
  - jpegn's `init()` calls `image.RegisterFormat("jpeg", …)`. Imported in the same binary as
    `image/jpeg`, it makes `image.Decode`'s choice of decoder depend on init order. Call it by
    name and do not blank-import it.
  - jpegn is young: 11 stars, created 2025-08, last commit 2026-11-18.

### 4. PNG over the cap

No candidate has a reduced-resolution PNG decode, since PNG has no DCT. djpeg, jpegn and jpegli
do not read PNG at all.

PhotoCraft resizes the 48 MP PNG at 252 MiB, but only where memory is free: in the box, or in
aura-ingest with no limit. Beside aura's heap it refuses.

Today's Go path decodes a PNG to 4 B/px RGBA, and its cap is 10.11 MP. Lifting that cap inside
aura needs a decoder that streams rows into the resampler. No candidate here does that, and none
was inventoried further.

## Recommendation

1. **JPEG (the 24–50 MP camera case): libjpeg-turbo DCT scaling in aura, and in aura-ingest.**
   - Add `libjpeg-turbo-progs` (358 KB; its library is already present).
   - Before the exec, read the SOF header, pick N, and compute the coefficient bytes. Refuse what
     is over about 300 MiB.
   - Run `djpeg -scale 1/N -maxmemory <budget> -maxscans <n>` from stdin to a buffered stdout,
     then let the unchanged `DownscaleForVision` finish at about 3 MP. This needs BMP or PNM
     registration; the spike used `x/image/bmp`.
   - The cap rises: baseline JPEG becomes effectively unbounded, progressive 4:2:0 reaches about
     100 MP, and progressive 4:4:4 about 50 MP. These limits are worked out from 3 and 6 B/px at
     300 MiB, not measured beyond 48.8 MP.
   - Among the five candidates, only this one resized every 48 MP JPEG beside aura's 452 MiB peak.
2. **Optional, in-process:** jpegn for *baseline* JPEG only. It costs no exec and 88 MiB at 48 MP,
   with djpeg kept for progressive. It is a judgment call: one more young dependency to save an
   exec on the common case. jpegli has no advantage over jpegn here.
3. **PNG over the cap:**
   - Inside aura, keep today's "too large to show" text.
   - In aura-ingest (no `mem_limit`), photocraft-cli `run` would resize it in 1.4–2.1 s and
     252 MiB, at the price of a 113 MB binary and an admission check tied to free memory.
   - In chat, the agent already has the box's `photocraft__*` MCP.
   - Do not bring photocraft-cli into aura's cgroup.
4. Per CLAUDE.md, the spec comes next. It records this measurement, and then a live E2E on the lab
   VM checks it.

## What this measurement does NOT prove

- **Real camera files.** These are synthetic images of gradients plus noise. EXIF orientation,
  embedded thumbnails, ICC profiles, MPF/HDR gain maps and restart markers were not exercised.
  Real 48 MP photos may compress differently. Which share of real phone photos is baseline (that
  is, cheap) is not measured.
- **CMYK, YCCK, gray, 4:2:2 and 12-bit JPEG were not measured.** Neither were arithmetic coding
  and WebP or GIF over the cap.
- **Not measured inside a running aura.** The ballast stands in for aura's heap as shmem, not as a
  Go heap with GC behaviour. A real exec from aura, its pipe buffering and the decode slot's
  serialization were not exercised. Neither were two decodes at once, which the slot forbids
  today for the in-process path only.
- **Wall times are on a shared 16-core host limited to 1 CPU.** They varied up to 2.5x between
  runs, and they are not the VM's.
- **The aura-ingest image was not inspected**, because it is not on this host. The libjpeg-turbo
  library there is inferred from its `poppler-utils`.
- **PhotoCraft in the live box was not measured** through the `photocraft__*` MCP mount, only as
  the CLI under a 2 GiB limit.
- **djpeg's security on hostile input** is argued from libjpeg-turbo's fuzzing record, not tested.
- **Fidelity is PSNR against a LANCZOS reference** on noisy synthetic content. No model read the
  outputs.

## Files

- Scripts, in the order they ran: `00_inventory.sh`, `01_photocraft_help.sh`, `02_probe_env.sh`,
  `10_generate.sh`, `11_regen_png.sh`, `20_build.sh`, `21_djpeg_pkg.sh`, `22_build_jpegli.sh`,
  `30_measure.sh` (twice: `results/30_measure_r1.txt`, `results/30_measure_r2.txt`), `31_followup.sh`,
  `32_djpeg_maxmemory.sh`, `33_photocraft_box.sh`, `34_jpegli.sh`, `41_verify.sh` (which runs
  `40_verify.py`), `50_gates.sh` and `60_cleanup.sh` (which removes the djpeg image).
- Run them from Git Bash with
  `timeout 900 env MSYS_NO_PATHCONV=1 wsl.exe -d Ubuntu -- bash /mnt/d/Aura/spikes/vision-large-photo-downscale/<script>.sh > results/<script>.txt 2>&1`.
  To re-run, run `20_build.sh` again first: `60_cleanup.sh` has removed the djpeg image.
- Harnesses: `rss.go`, `baseline.go`, `jpegnprobe/main.go` and `jpegliprobe/main.go`, all
  `//go:build ignore` and built by file name. jpegn and jpegli are built in their own modules under
  `/tmp`, so the repo's `go.mod` is unchanged. Image generator: `gen_images.py`.
