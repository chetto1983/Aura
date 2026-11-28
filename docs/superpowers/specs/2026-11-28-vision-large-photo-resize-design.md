# Photos over the vision pixel cap are resized, not refused

Approved by the operator on 2026-11-28 ("vai"), in two parts:

1. In `aura` and in `aura-ingest`, a JPEG over the cap is resized with libjpeg-turbo's
   `djpeg -scale 1/N`.
2. In the media indexer (`aura-ingest`), a PNG over the cap is resized with `photocraft-cli run`.
   Inside `aura` it stays reference text.

The measurement this spec rests on is `spikes/vision-large-photo-downscale/README.md` (commit
e69371d4d). Its verdicts, in the vocabulary of `Skill("spike-findings-Aura")`:

| Candidate | Verdict |
|---|---|
| djpeg 1/N | **VALIDATED** for memory, JPEG only |
| PhotoCraft | **INVALIDATED** inside aura's cgroup; **PARTIAL** in aura-ingest and in the box |
| jpegn / jpegli | **PARTIAL**, baseline JPEG only |

This spec uses djpeg freely. It uses PhotoCraft only in aura-ingest, with the PARTIAL's
constraints stated (§5). It does not use jpegn, which was not approved.

## Why

`DownscaleForVision` (`internal/multimodal/vision_downscale.go:103`) refuses an image whose pixel
count is over a per-colour-model bound before decoding it. The check is at `:110`, the bounds at
`:49-54`, and the measured rationale in the comment at `:23-48`. Go's `image/jpeg` decodes at full
size and keeps every progressive coefficient, so the YCbCr bound is 12.36 MP. A 24 MP camera photo
or a 48–50 MP phone photo is therefore never resized:

- **A chat upload becomes reference text.** `visionReady` (`internal/assets/turn_media_loader.go:103`)
  clears the media type on `ErrImageTooManyPixels` (`:105`), and the model reads
  `tooLargeToShow` (`:95`) in place of the photo.
- **`read_file` refuses it** (`internal/agent/tools/read_file_image.go`, `attachImage`: "cannot be shown").
- **The indexer sends it unshrunk.** `DescribeImageForRetrieval`
  (`internal/assets/image_processor.go:83`) hands the original to the vision route (`:92`). On
  Claude, `tooLarge` refuses it before dialing (`internal/llm/anthropic/media_limits.go`: an image
  over 10 MB base64 or 8000 px per edge), and the description falls back to the OCR sidecar. On
  the lab VM there is no OCR sidecar.

## Goals

- A YCbCr JPEG over the in-process bound reaches the vision model as the same 1024 px q85 JPEG
  that a small photo gets. This holds in a chat upload, in `read_file` and in the indexer.
- In the indexer, a PNG over the bound reaches the vision route as a 1024 px JPEG.
- Every image the change does not resize fails exactly as it does today. No caller changes how it
  reacts.
- Local development and the unit tests run with neither binary installed.

## Non-goals

- PNG in `aura`. It stays `tooLargeToShow` text. No candidate streams PNG rows, and PhotoCraft
  refuses inside aura's cgroup (spike §4).
- jpegn or jpegli, WebP or GIF over the cap, EXIF orientation. Today's path ignores orientation
  too.
- New env vars. The budget, the timeout and the scan limit are measured constants, like the
  existing bounds.
- arm64 images. `publish-aura-edge.yml` builds `aura` (`:66-67`) and `aura-ingest` (`:140-141`)
  for `linux/amd64` only. The aarch64 PhotoCraft pin is written down so an ARM build does not
  silently fetch the wrong binary, but nothing builds it.

## Design

### 1. Where djpeg sits

`DownscaleForVision` keeps its signature and its in-process path. djpeg runs only when all three
of these hold:

- `image.DecodeConfig` reports format `jpeg`;
- the pixel count is over `maxVisionPixels(model)`;
- `model` is in the djpeg set.

Everything under the bound is untouched: same code, same measured numbers, no exec.

The djpeg set is `color.YCbCrModel` (3-component JPEG, any sampling). Gray (`GrayModel`) and CMYK
or YCCK (`CMYKModel`) join only if plan Task 1 measures them. The criteria are: RSS within the
formula below, decode correct, and PSNR within 0.5 dB of the YCbCr rows against a Pillow LANCZOS
reference. Until then they keep their old bounds. The reason is that djpeg's BMP writer converts
CMYK to RGB itself (`wrbmp.c:142-145` @ 2.1.5), and that conversion has never been compared with
Go's. A 3-component Adobe-RGB JPEG is reported as `RGBAModel` (`image/jpeg` `isRGB`). It stays on
its old bound because it was not measured.

### 2. The memory budget: one number, enforced by libjpeg itself

These are measured facts, and the spike's figures fit them:

- **What djpeg allocates.** With `-bmp`, djpeg builds the BMP writer with
  `use_inversion_array = TRUE` (`djpeg.c:669`). That writer requests the whole output image as a
  virtual array (`wrbmp.c:528-531`). A progressive or multi-scan JPEG adds the full coefficient
  buffer: 2 bytes per coefficient, so 3 B/px at 4:2:0 and 6 B/px at 4:4:4.
- **How `-maxmemory` is counted.** `-maxmemory N` is N×1000 bytes (`djpeg.c:350-354`). The
  no-backing-store allocator grants a virtual array only if it fits under
  `max_memory_to_use − already_allocated` (`jmemnobs.c:66-75`). Otherwise djpeg fails with
  "Backing store not supported".
- **The spike's measurement.** The refusal came in under 10 ms at 2 MiB RSS
  (`results/32_djpeg_maxmemory.txt`). Child RSS is virtual arrays + about 2.0 MiB at every measured
  point (`results/30_measure_r1.txt`). Two examples: p48_444_progressive at 1/4 has arrays of
  287.8 MiB and an RSS of 296,764 KiB (289.8 MiB); p48_baseline has arrays of 8.7 MiB and an RSS
  of 10.8 MiB.

**Decision: `-maxmemory` is the only memory gate, and it is set per image** (amended
2026-11-28, before any code).

- **The budget.** One decode gets 300 MiB over aura's heap, as the in-process bounds give it:
  `djpegBudgetBytes = 300 << 20`.
- **What the budget holds during the exec:**
  - the child, whose RSS is its arrays + `djpegBaseBytes = 2 << 20` (measured);
  - aura's stdout buffer, pre-sized to `maxBMPBytes` so that it never doubles while it grows.
- **The value passed.** `-maxmemory = (djpegBudgetBytes − djpegBaseBytes − maxBMPBytes) / 1000`,
  computed for each image.
- **The measured worst case.** For 8064×6048 at 1/4: (314,572,800 − 2,097,152 − 9,145,654) / 1000 =
  303329, that is 289.3 MiB. Its arrays measured 287.8 MiB, so it passes as long as
  `already_allocated` stays under about 1.5 MiB. Task 1 confirms this.

The code does not parse the SOF to predict the coefficient bytes. The reasons:

- `image.DecodeConfig` reports neither sampling nor progressive mode.
- The SOF scan would be a bespoke marker parser. It would duplicate libjpeg's own accounting and
  could drift from it.
- That accounting already fails closed for any sampling, 4:2:2 and multi-scan baseline included.
- An over-budget file costs one exec of under 10 ms.

This departs from the spike's recommendation, "refuse before the exec". The cost of departing is
the 10 ms exec; the gain is one source of truth.

**The second bound is the output, checked before the exec.** djpeg's output is
`ceil(w/N) × ceil(h/N)`. The finish decodes it in-process as a 24-bit BMP into RGBA. The output's
pixel count must therefore be at most `maxPixelsOther` (3180² = 10.11 MP), the bound already
measured for the costliest RGBA decode. This is what caps baseline JPEG: at N = 8, up to about
647 MP.

The bounds that follow from the budget (worked out, not measured beyond 48.8 MP). The output is
counted twice: once in djpeg's inversion array and once in aura's buffer, the latter at the 24-bit
bound.

| JPEG | Coefficient B/px | + output B/px (child + aura) | Largest photo admitted |
|---|---:|---:|---|
| baseline, single interleaved scan | 0 | 6/N² | output ≤ 10.11 MP, about 647 MP at 1/8 |
| progressive 4:2:0 | 3 | 6/16 at 1/4 | about 93 MP (about 100 MP at 1/8) |
| progressive 4:2:2 | 4 | 6/16 | about 71 MP |
| progressive 4:4:4 | 6 | 6/16 | about 49 MP. 8064×6048 (48.8 MP) passes by about 1.5 MiB. 8160×6120 works out about 5.6 MiB over and is refused: Task 1 confirms both |
| gray / CMYK progressive (if Task 1 admits them) | 2 / 8 | 4/16 / 6/16 | about 139 MP / about 37 MP |

### 3. The scale

N is the largest of 8, 4 and 2 that keeps `ceil(longEdge/N) ≥ 1024`. That is the measured rule.
Every image over a JPEG bound has a long edge of at least 2910 px, so N is never 1. `-scale M/8`
with M > 1 was not measured and is not used.

### 4. Exec safety

- **No shell.** The command is `exec.CommandContext(runCtx, path, "-scale", "1/N", "-bmp",
  "-maxmemory", "<per image, §2>", "-maxscans", "100", "-strict")`. `path` comes from `exec.LookPath("djpeg")`.
  No file name and no user text reaches argv.
- **Pipes, not temp files.** The JPEG goes in on stdin (`bytes.NewReader(raw)`) and the BMP
  comes out on stdout. There is nothing to write, clean up or leave behind, and the input is never
  in page cache charged to the cgroup. The spike measured file to file only. Task 1 measures the
  pipe with a reader process inside the cgroup holding the output, and the live acceptance
  measures it again.
- **Stdout is bounded and pre-sized.** The bound is the 24-bit BMP size of the predicted output
  (`maxBMPBytes`): 1078 header bytes + the 4-byte-padded row × `outH`. The buffer is grown to that
  size before the exec, so it never re-allocates, and the bound is part of the budget (§2). One
  byte more fails the write, and djpeg dies on the broken pipe. Stderr is kept up to 4 KiB, for the
  error text.
- **Empty environment.** `cmd.Env = []string{}`. aura's environment carries API keys, and libjpeg
  reads `JSIMD_*` from it.
- **Timeout.** `djpegTimeout = 30 s`, started after the decode slot is taken. Waiting in the queue
  does not eat it. `cmd.WaitDelay = 2 s` bounds the pipe copies after a kill. The slowest measured
  run took 1.4 s, and 2.5x that on the slow repeat, so 30 s leaves room for the VM.
- **`-maxscans 100`.** libjpeg's own progressions are 10 scans for YCbCr and 2 + 4×ncomps
  otherwise (`jcparam.c:479-488`). TurboJPEG's own guard is 500 (`turbojpeg.c:128`), but our
  timeout is 30 s, so 100 leaves 10x headroom over a normal file. djpeg exits on the first scan
  over the limit (`cdjpeg.c:33-40`). Task 1 times 100 scans against the timeout.
- **`-strict`.** Any libjpeg warning is fatal (`djpeg.c:576-577`): a truncated or corrupt photo
  fails instead of reaching the model with a grey bottom. This is the same contract as the
  in-process path ("a body that is truncated … fails here"). Such a photo was refused before this
  change too, so nothing regresses. Real cameras that write extraneous bytes are an open question.
- **Read djpeg's output to the end, then decode.** `cmd.Run` returns after djpeg has exited and
  its stdout copy has finished. Only then does `bmp.DecodeConfig` check that the dimensions equal
  the predicted `outW × outH`. After that, `bmp.Decode` runs (called by name: `golang.org/x/image/bmp`,
  already in `go.mod` and already imported at `internal/documents/preview_raster.go:10`). The two
  peaks never overlap. While djpeg runs, aura's own extra memory is the stdout buffer (9 MB for a
  48 MP photo, at most 30 MB), and §2 subtracts it from `-maxmemory`.
- **BMP over PNM.** The reasons:
  - `x/image/bmp` is a dependency already present;
  - it decodes djpeg's 24-bit and 8-bit paletted output;
  - it is what the spike measured.
  PNM would need a parser of our own (CLAUDE.md, STOP BEFORE BESPOKE). The cost is the inversion
  array in djpeg, which `-maxmemory` counts. Importing the package registers the BMP format process-wide.
  `aura` already does this. In `aura-media-index` it is harmless: `.bmp` is not an image extension
  there (`services/ingest/media.py:10`).
- **`decodeSlot`** (`vision_downscale.go:74`). One acquisition covers both the exec and the finish.
  djpeg shares aura's cgroup, and its memory is the decode the slot serializes. The slot is not
  re-entrant, so the finish is an inner function that assumes it is held. It is not a second
  `DownscaleForVision` call: in-process, that would deadlock. It is the tail of today's function
  (`:127-146`), extracted unchanged. `VisionImage.Width/Height` stay the original's size.
- **Logging.** A successful run logs one `slog.Info` line ("aura vision: djpeg shrank an image over
  the in-process pixel cap") with width, height, scale and `djpeg_ms`. A refusal logs `slog.Warn`
  with the reason. Errors never pass silently, and the live acceptance reads the Info line.

### 5. Failures keep today's contract

A failure must never forward the original bytes when today's code would not. In particular, the
internal timeout must not surface as `context.DeadlineExceeded`: `read_file` reads that as "the
image is fine, out of time", and `visionReady` forwards the original.

| Failure | Error | Chat upload (`visionReady`) | `read_file` | Indexer (`DescribeImageForRetrieval`) |
|---|---|---|---|---|
| djpeg not on PATH | `ErrImageTooManyPixels`, "djpeg is not installed" | reference text (today) | "cannot be shown" (today) | original bytes (today) |
| output over 10.11 MP | `ErrImageTooManyPixels` | same | same | same |
| `-maxmemory`, `-maxscans`, `-strict`, any non-zero exit | `ErrImageTooManyPixels` + djpeg's stderr, as `%v` | same | same | same |
| `djpegTimeout` while the caller's context is alive | `ErrImageTooManyPixels`, "djpeg did not finish in 30s", with no `%w` of the deadline | same | same | same |
| stdout over its bound, BMP undecodable or of the wrong size | `ErrImageTooManyPixels` | same | same | same |
| the caller's context ends (in the slot wait or during the exec) | that context error, `%w`, as today's slot wait | original bytes (today) | "ran out of time" (today) | the vision call fails on the same context |
| JPEG still over `maxBytes` after encoding | `ErrImageOverByteCap` (today) | — | refused (today) | — |

The rule: an image over the bound comes back resized, or fails exactly as it did before.
`errors.Is(err, ErrImageTooManyPixels)` holds for every path except the caller's own context.

### 6. PhotoCraft in `aura-media-index`

- **The file.** It lives in `cmd/aura-media-index/photocraft.go`. The seam is a func field on
  `mediaRuntime`, `resizePNG`, which is the `renderPDF` pattern (`cmd/aura-media-index/main.go:46-49`).
- **Where it runs.** In `derive`'s image case, before `DescribeImageForRetrieval` (`main.go:84`).
  - The indexer calls `image.DecodeConfig` itself. When the format is `png` and
    `multimodal.OverPixelCap(cfg)` holds, it calls `resizePNG`.
  - On success, the 1024 px JPEG and `image/jpeg` go on to `DescribeImageForRetrieval`, whose own
    `DownscaleForVision` passes it through.
  - `OverPixelCap(cfg image.Config) bool` is the bound check `DownscaleForVision` already makes,
    exported once with two callers, so the deadexport gate is satisfied. `DescribeImageForRetrieval`
    swallows the prepare error, which is why the indexer cannot learn "over the cap" from it.
- **The command.** `photocraft-cli run <tmp>/in.png --cmd image.imageSize --params <p> --out
  <tmp>/out.jpg --quality 85 --no-gpu`.
  - `<p>` is `{"width":1024}` for a landscape or square image and `{"height":1024}` for a portrait
    one. The command keeps the other side's ratio, so `{"width":1024}` alone would leave a portrait
    1365 px tall.
  - The input is written to a private `os.MkdirTemp` directory, as `describeScannedPDF` does. The
    extension is then always `.png`, and no CocoIndex path reaches argv.
  - The timeout is 2 min, as `renderScannedPDFPages`. The spike measured 1.4–2.1 s.
  - Output goes through `CombinedOutput`, as there. Afterwards, `out.jpg` must decode as a JPEG
    with a long edge of 1024.
- **Failure keeps today's behaviour.** A missing binary, PhotoCraft's own memory refusal, a timeout
  or a bad output all hand the original bytes to `DescribeImageForRetrieval`, with one
  `slog.Warn`. The reasons:
  - PhotoCraft's admission depends on the host's free memory at that moment;
  - a hard failure would retry the file forever on a host that never has the memory.
  A success logs one `slog.Info` ("aura ingest: photocraft resized a PNG over the pixel cap", with
  width, height and `photocraft_ms`). Both lines reach stderr, which `media.py` drops on success,
  so the acceptance reads them from a hand run (below). Whether to retry instead is an open
  question.
- **Alpha.** Task 1 measures how PhotoCraft flattens a transparent PNG. If the result is not white,
  `resizePNG` takes only PNGs with no alpha channel: `DecodeConfig` model `RGBAModel`, `GrayModel`,
  `Gray16Model` or `RGBA64Model`. A PNG with alpha keeps today's path.
- **Concurrency.** `aura-ingest` has no `mem_limit` (`compose.yaml:1011-1095`). PhotoCraft holds
  250–470 MiB per photo, and each indexer process is its own process. Several big PNGs indexed at
  once would add up on the host. This is not measured, and not serialized. See open questions.

### 7. Images

- **`docker/aura/Dockerfile`.**
  - `libjpeg-turbo-progs` joins the `poppler-utils fonts-liberation` install (`:167-169`). It is
    358 KB, and its library `libjpeg62-turbo` 2.1.5 is already in `aura:edge`
    (`results/21_djpeg_pkg.txt`).
  - The same RUN asserts `djpeg -help 2>&1 | grep -q -- -maxscans`. An image without djpeg, or with
    one too old for `-maxscans`, then fails to build. This is where djpeg-in-`aura` is asserted;
    `aura` has no image contract script.
  - The comment at `:162-166` names `pdftoppm in cmd/aura-media-index`, which does not run in this
    image (it builds only `cmd/aura`, `:78,90`). It is corrected on touch.
- **`docker/aura-ingest/Dockerfile`.**
  - `libjpeg-turbo-progs` joins the apt list (`:61-65`).
  - PhotoCraft 0.23.1 comes from a new builder stage, because the runtime stage has no curl. That
    stage copies the sandbox's `craft()` logic (`docker/aura-sandbox/Dockerfile:225-241`) for one
    app:
    - `TARGETARCH` selects x86_64 or aarch64;
    - the tarball pins are the sandbox's, x86_64 `4383aef6…ccfc8` and aarch64 `fcc81c38…71d2`
      (`:254-256`), checked with `sha256sum -c`;
    - the executable is named exactly `photocraft-cli`;
    - every `LICENSE*`, `NOTICE*`, `OFL*` and `ATTRIBUTION*` goes to
      `/usr/local/share/licenses/photocraft/`.
  - This is an unmodified official build, which the ArtCraft brand terms allow (spike §1).
  - The final stage `COPY --from`s the binary and the licences.
  - Cost: +113 MB to `aura-ingest`, a second copy on an appliance that already has one in
    `aura-sandbox`.
- **`scripts/ingest_image_contract_test.sh`.** It asserts:
  - `djpeg` with `-maxscans`;
  - `photocraft-cli` and its licence directory;
  - that PhotoCraft's version and both SHA-256 values in the ingest Dockerfile equal the sandbox
    Dockerfile's (a static check before any `docker run`, so the two pins move together);
  - one real resize: a 48 MP noiseless PNG, written with Python's stdlib `zlib`, through
    `photocraft-cli run`, with `rdjpgcom -verbose` reading a long edge of 1024.
- `scripts/security_scan/craft_osv.sh` reads only the web apps' pins (`:16-24`). The CLI in the
  sandbox is not scanned today, and the ingest copy is not either. No change here.

### 8. CI

Tests that run djpeg or jpegtran call `testenv.Executable(t, "djpeg")` (`internal/testenv/testenv.go:40`):

- locally they skip;
- under `$CI` they fail.

So every job that runs them installs `libjpeg-turbo-progs` beside its `poppler-utils`:

- `unit-test` (`ci.yml:210-218`);
- the coverage gate in `knowledge-integration-test` (`:817-838`);
- `race-db-integration-gates`, for `./internal/assets/...` (`:2633-2636`; it installs nothing today);
- `go-mutation`.

These are all the jobs that run tests in those packages. `skills.yml` and `live-outward.yml`
reach neither package, and `release.yml` runs no Go test (checked with `grep` on 2026-11-28).
WSL needs the package too (`wsl -u root apt-get install -y libjpeg-turbo-progs`). Without it, the
local RED/GREEN runs and `coverage_docker.sh` skip the tests that would exercise the path.

The pure logic is the scale, the output bound, the argv, the stdout bound and the error mapping.
It lives in its own file with daemon-free tests, and that file becomes the mutation scope
`vision_djpeg`.

## Live acceptance (lab VM .158)

The change reaches the VM through the updater only: push, every job green on the SHA,
"Publish Aura edge image" included, then the operator consents in the cockpit. The operator
drives the cockpit; no test identity is created.

1. **Chat upload.**
   - The operator uploads a real phone photo of at least 48 MP, saved as JPEG (not HEIC), in a new
     conversation, and asks what is in it.
   - The model must describe the photo, not the "too large" text, and the operator confirms that
     the description matches.
   - Meanwhile a VM script samples `memory.current` of aura's cgroup every 100 ms and reads
     `memory.events` before and after.
2. **Indexer.** First, read on the VM how the photo reaches the indexer. Whether a chat upload
   lands in the indexed bucket was never checked. If it does not, the operator adds the photo
   through the cockpit's documents. Record the route.
   - A description chunk existing does not tell a resize from the fallback: both describe
     something.
   - The observable that tells them apart is a hand run:
     `docker exec aura-ingest aura-media-index -kind image -name <name> <path>`, with stderr kept.
     Its stderr must carry the djpeg Info line. The operator agrees to it first, because it bills
     one vision call.
   - While it runs, `aura-ingest`'s cgroup is sampled the same way. The automatic run's chunk is
     then read from the identity's ArcadeDB.
3. **PNG leg.** The spike's synthetic 48 MP PNG (`gen_images.py`) takes the same route. The same
   hand run must show the PhotoCraft Info line, and the same PNG uploaded in chat must still read
   as reference text.

The dated file `docs/superpowers/verification/<date>-vision-large-photo-resize.md` shows:

- the SHA, `aura version`, and the `aura` and `aura-ingest` image digests;
- `djpeg -help | grep maxscans` and `photocraft-cli --version` in each container;
- the photo's header facts only: dimensions, progressive or baseline, sampling
  (`djpeg -verbose`), byte size;
- the Info log lines: djpeg in `aura` (scale and `djpeg_ms`), and djpeg and PhotoCraft from the
  indexer's hand runs;
- the route by which the photo reached the indexer;
- the sampled peak of each cgroup against 768 MiB or the host, and `oom_kill` unchanged;
- the route and model;
- the description chunk's length;
- PhotoCraft's wall time and outcome;
- what it does not prove.

The repo is public: no photo, no EXIF/GPS, and no description text goes in the file. The photo is
never committed.

## What this does not prove

- **The operator's file is not the population.** One photo from one phone. Which share of real
  photos is progressive, or 4:4:4, is unknown.
- **Images not measured.** CMYK, YCCK, gray and 4:2:2 stay unmeasured until Task 1, and RGB JPEGs
  and 12-bit or arithmetic JPEGs stay unmeasured here. Go's `DecodeConfig` refuses the last two
  before djpeg is reached.
- **Hostile input.** djpeg on hostile files is argued from libjpeg-turbo's fuzzing record and the
  three flags. Tests cover a truncated file, a scan-count file and an over-budget header. They do
  not cover crafted exploits.
- **Memory.** Two djpeg-path decodes cannot overlap in `aura` (one slot). In `aura-ingest`, one
  indexer process per file can overlap with another, and that is not measured.
- **PhotoCraft on real large PNGs**, and its admission under real host memory pressure.
- **arm64**, which nothing builds.

## Open questions for the operator

1. `-strict` refuses a JPEG that libjpeg warns on. Some cameras write a few extraneous bytes before
   a marker, and that photo stays refused as today. Do we keep it?
2. When PhotoCraft fails in the indexer, it falls back to sending the original, as today. Is that
   right, or should it fail and be retried next cycle?
3. PhotoCraft in `aura-ingest` runs with no `mem_limit` and no cross-process serialization. Do we
   accept that, or add a lock or a limit after the acceptance measures one burst?

## Left to the builder

- The log wording, and the exact stderr excerpt in errors.
- How the test fixtures are made: `cjpeg` from a PPM written in Go, or `jpegtran -progressive` /
  `-scans` from a Go-encoded JPEG. Both ship in `libjpeg-turbo-progs`.
