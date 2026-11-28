# Vision large photo resize Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A photo over the vision pixel cap is resized, not refused:
- a YCbCr JPEG, through `djpeg -scale 1/N` in `aura` and `aura-ingest`;
- a PNG in the media indexer, through `photocraft-cli run`.

Everything else fails exactly as today.

**Architecture:**
- **The JPEG path.** `DownscaleForVision` keeps its in-process path under the bounds. Over the bound, an eligible JPEG goes to djpeg:
  - the scale and the output bound come from `image.DecodeConfig`;
  - djpeg runs from a stdin pipe to a bounded stdout, under `-maxmemory`, `-maxscans` and `-strict`;
  - the finish decodes the BMP with `x/image/bmp`, through the tail of today's function, extracted.

  One `decodeSlot` hold covers the exec and the finish. Every failure maps to `ErrImageTooManyPixels`, so no caller changes.
- **The PNG path.** `cmd/aura-media-index` detects a PNG over the cap with the exported `multimodal.OverPixelCap`. It resizes the PNG through PhotoCraft behind a `resizePNG` func field, and falls back to today's path on any failure.

**Tech Stack:** Go (`os/exec`, `image`, `golang.org/x/image/bmp`), libjpeg-turbo 2.1.5 `libjpeg-turbo-progs` (Debian bookworm and Ubuntu runners), PhotoCraft CLI 0.23.1, Docker, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-28-vision-large-photo-resize-design.md`

## Global Constraints

- **djpeg argv, exactly:** `-scale 1/N -bmp -maxmemory 310378 -maxscans 100 -strict`. Input on stdin, output on stdout, `cmd.Env = []string{}`, no shell.
- **djpeg constants:**
  - `djpegMaxMemoryBytes = 296 << 20` (passed as `/1000` = `310378`);
  - `djpegMaxScans = 100`;
  - `djpegTimeout = 30 * time.Second`, started after the slot is taken;
  - `cmd.WaitDelay = 2 * time.Second`;
  - stderr kept to 4 KiB.
- **Scale:** N is the largest of 8, 4, 2 with `ceil(longEdge/N) >= 1024`. The output is `ceil(w/N) x ceil(h/N)` and must be `<= maxPixelsOther` (3180²), or the image is refused before the exec.
- **Stdout bound:** `1078 + ((outW*3+3)&^3) * outH` bytes.
- **djpeg-eligible:** format `jpeg` and model `color.YCbCrModel`. `GrayModel` and `CMYKModel` are added only if Task 1 VALIDATES them.
- **Errors:** every djpeg-path failure satisfies `errors.Is(err, ErrImageTooManyPixels)`:
  - a missing binary;
  - the output bound;
  - a non-zero exit;
  - the internal timeout (`%v`, never `%w` of a deadline);
  - stdout over its bound;
  - a bad BMP.

  Only the caller's own context error is `%w`-wrapped, as today's slot wait.
- **`VisionImage.Width/Height` stay the original's size.**
- **PhotoCraft argv, exactly:** `run <tmp>/in.png --cmd image.imageSize --params {"width":1024}|{"height":1024} --out <tmp>/out.jpg --quality 85 --no-gpu`.
  - `width` when `w >= h`.
  - The timeout is 2 min.
  - Any failure hands the original bytes on, with one `slog.Warn`.
- **PhotoCraft 0.23.1 tarball pins:** the sandbox's, verbatim:
  - x86_64 `4383aef67a07cc370ad250a6ed60015205d3e6c463faf3ff4b0f67b8178ccfc8`;
  - aarch64 `fcc81c38ae62c72fa9f4fd1ec3d18396493ba261662792d6995cc35905ce71d2`.
- No new env var. No file over 600 LOC. Go tests and gates run in WSL. Tests that need djpeg, cjpeg or jpegtran take the path from `testenv.Executable`.
- Commits by pathspec. The `read_file*` files are a peer's dirty work (2026-11-28): never stage them, and cite them by symbol, not line.
- Never mutate locally, by tool or by hand: mutation is CI's. "The test can fail" is shown by the RED run.

## Review Focus

- **A portrait phone photo (6048×8064).** It must come out 768×1024, with `Width/Height` 6048×8064. Task 2 has a portrait row in the real-binary test.
- **A truncated upload**, from a phone that lost its connection mid-send. It must become reference text in chat, never the original bytes. Task 2 tests a truncated progressive file for `ErrImageTooManyPixels`; Task 3 tests it through `visionReady`.
- **A panorama whose 1/8 output is still over 10.11 MP (65535×20000).** It must be refused before any exec. Task 2's plan test.
- **A turn cancelled while another photo holds the slot.** The context error comes back and djpeg never starts. Task 2's slot test.
- **Local dev and Windows, with no djpeg.** The refusal must be today's, with "djpeg is not installed" in the text. Task 2's PATH test runs everywhere, with no binary.

## Decisions taken while writing this plan

- **Task 1 measures before any code** (PRD-first). It fills in the spec's unmeasured rows: gray, 4:2:2 and CMYK, 8160×6120 4:4:4, pipes, `-maxscans`, a header-only refusal, `-strict`, and PhotoCraft alpha and portrait. The djpeg-eligible set and the alpha rule are then data, set by Task 1's verdicts, and Task 2 copies them.
- **`exec.LookPath("djpeg")` runs on every over-cap call, uncached.** The "no djpeg" seam is then `t.Setenv("PATH", t.TempDir())`, with no package variable. The cost is a few `stat`s, beside a decode of seconds.
- **The internal timeout is a parameter of `runDjpeg`**, so a test can pass 1 ns and check the mapping. The `30 s` constant is passed by `DownscaleForVision`.
- **Existing tests stay unchanged in `internal/multimodal`.** "YCbCr one row past" expects `ErrImageTooManyPixels`: with djpeg it fails `-strict` on a header-only file, and without djpeg it is not found. Both map to the same error.
- **`TestTurnMediaLoaderDescribesAPhotoTooLargeToDecode` changes its fixture** from a 3600² JPEG to a 3200² PNG. With djpeg, that JPEG is now resized by design. The PNG is the case that still reads as text. Say so in the commit.
- **The mutation scope covers only the pure file** (`vision_djpeg_plan.go`), so mutants never exec. It rides with elicitation-boundaries, the group with the most headroom (`ci.yml:2005-2015`).
- **PhotoCraft's real exec from Go is proven by the live acceptance.** CI proves the argv, the fallback and the output check with a fake, and the binary itself in the image contract. A Go test that needs `photocraft-cli` would fail `unit-test`, where installing a 113 MB binary to test argv is not worth it.

## File structure

- `internal/multimodal/vision_djpeg_plan.go` (new, pure):
  - the constants;
  - `djpegEligible`;
  - `planDjpeg`;
  - argv;
  - the stdout bound;
  - `boundedBuffer`.
- `internal/multimodal/vision_djpeg.go` (new): `downscaleWithDjpeg` and `runDjpeg`.
- `internal/multimodal/vision_downscale.go`:
  - the routing;
  - `acquireDecodeSlot` and `prepareDecoded`, extracted;
  - `OverPixelCap`, added in Task 5;
  - the comment table, updated.
- `internal/assets/turn_media_loader.go`: the `visionReady` comment only.
- `cmd/aura-media-index/photocraft.go` (new): `photocraftArgs`, `resizeWithPhotocraft` and `fitLargePNG`. `main.go` gets the `resizePNG` field and one call in `derive`.
- `docker/aura/Dockerfile`, `docker/aura-ingest/Dockerfile`, `scripts/ingest_image_contract_test.sh`.
- `.github/workflows/ci.yml`, `scripts/critical_mutation_gate.py` and its tests.
- `spikes/vision-large-photo-downscale/` (the follow-up), `prd.md`, the spec, and `docs/superpowers/verification/`.

---

### Task 1: Measure what the spec left open, then amend

**Files:**
- Create: `spikes/vision-large-photo-downscale/35_followup.sh`, `results/35_followup.txt`
- Modify: `spikes/vision-large-photo-downscale/gen_images.py` (new fixtures), `README.md` (a "Follow-up 2026-11-xx" section with verdicts), the spec (§1 eligible set, §2 the 8160×6120 row, §6 alpha rule; dated "amended after Task 1"), `prd.md` (the paragraph "An image in the box reaches the model natively", `prd.md:3971`, after its decode-bound bullets)

**Interfaces:**
- Produces: the djpeg-eligible model set and the PhotoCraft alpha rule, which Tasks 2 and 5 copy.

- [ ] **Step 1: Add the fixtures to `gen_images.py`**
  - progressive gray 8064×6048;
  - progressive 4:2:2 8064×6048;
  - progressive Adobe CMYK 6000×6000 (Pillow writes CMYK JPEG);
  - progressive 4:4:4 8160×6120;
  - baseline and progressive 20000×700;
  - a 48 MP RGBA PNG with a transparent left half;
  - a portrait 6000×8000 PNG.

  Each image gets the spike's seed and method.
- [ ] **Step 2: Write `35_followup.sh`.** It uses the same container limits and the `rss.go` wrapper as `30_measure.sh`, with the Global Constraints argv, and emits one `ROW|` per case:
  - RSS against arrays + 2 MiB for each new JPEG, and the PSNR of the finish against Pillow LANCZOS (`40_verify.py`);
  - 8160×6120 4:4:4: does it pass, and at what RSS;
  - `djpeg < in > out` against `-outfile`, both beside the 452 MiB ballast, with the cgroup peak;
  - `jpegtran -scans` files of 100 and 101 scans at 48 MP 4:2:0: the 101-scan file is refused; the 100-scan file's wall time on 1 CPU, which must be under 30 s;
  - a header-only progressive JPEG with a complete SOS header, 8300×6300 4:4:4: is it refused with "Backing store not supported" before any scan data? (Task 2's fixture depends on it);
  - the 48 MP 4:2:0 progressive cut at 60%: does `-strict` exit non-zero;
  - PhotoCraft in `python:3.15-slim-bookworm` with `HOME=/tmp`: the transparent PNG's left-half colour, and the portrait's output size with `{"height":1024}`.
- [ ] **Step 3: Run it** from Git Bash: `timeout 900 env MSYS_NO_PATHCONV=1 wsl.exe -d Ubuntu -- bash /mnt/d/Aura/spikes/vision-large-photo-downscale/35_followup.sh > spikes/vision-large-photo-downscale/results/35_followup.txt 2>&1`.
  - Expected: one ROW per case, no harness error.
  - Run `20_build.sh` first and `60_cleanup.sh` after.
- [ ] **Step 4: Record it.**
  - The README follow-up gets a verdict per family: gray and CMYK VALIDATED only if RSS ≤ arrays + 4 MiB and PSNR within 0.5 dB of the YCbCr rows. It also says what the follow-up does not prove.
  - The spec is amended with the eligible set, the 4:4:4 edge and the alpha rule. If any RSS − arrays margin is over 4 MiB, the spec also gets a lower `djpegMaxMemoryBytes`, and Global Constraints are updated.
  - The PRD paragraph records the spike, the follow-up and the design in one bullet block, with the date and "what it does not prove".
- [ ] **Step 5: Commit** by pathspec: `spike(vision): measure the colour models, pipes and limits the resize spec left open`.

### Task 2: djpeg shrinks a JPEG over the cap

**Files:**
- Create: `internal/multimodal/vision_djpeg_plan.go`, `internal/multimodal/vision_djpeg.go`
- Modify: `internal/multimodal/vision_downscale.go`, `.github/workflows/ci.yml`:
  - `unit-test` `:210-215`;
  - `knowledge-integration-test` `:817-820`.

  Both get `libjpeg-turbo-progs` beside `poppler-utils`, with the step renamed and its comment naming the djpeg tests.
- Test: `internal/multimodal/vision_djpeg_plan_test.go`, `internal/multimodal/vision_djpeg_test.go`

**Interfaces:**
- Consumes: Task 1's eligible set.
- Produces:
  - `func djpegEligible(format string, model color.Model) bool`;
  - `type djpegPlan struct{ denom, outW, outH int }`;
  - `func planDjpeg(w, h int) (djpegPlan, error)`;
  - `func (p djpegPlan) args() []string`;
  - `func (p djpegPlan) maxBMPBytes() int`;
  - `type boundedBuffer` (an `io.Writer` that errors past `limit`);
  - `func runDjpeg(ctx context.Context, path string, raw []byte, p djpegPlan, timeout time.Duration) ([]byte, error)`;
  - `func acquireDecodeSlot(ctx context.Context) (release func(), err error)`;
  - `func prepareDecoded(img image.Image, format string, raw []byte, cfg image.Config, maxBytes int) (VisionImage, error)`, today's `:127-146`, unchanged.

- [ ] **Step 1: Write the failing pure tests** (`vision_djpeg_plan_test.go`)
  - `TestPlanDjpegPicksTheLargestScaleThatKeeps1024`:
    - 8064×6048 → 4, 2016×1512;
    - 6048×8064 → 4, 1512×2016;
    - 6000×4000 → 4, 1500×1000;
    - 3600×3600 → 2, 1800×1800;
    - 4000×3000 → 2;
    - 8192×6144 → 8, 1024×768;
    - 8193×100 → 8, 1025×13;
    - 20000×700 → 8, 2500×88.
  - `TestPlanDjpegRefusesAnOutputOverTheRGBABound`:
    - 25440×25440 → ok (3180²);
    - 25441×25440 → `ErrImageTooManyPixels`;
    - 65535×20000 → `ErrImageTooManyPixels`.
  - `TestDjpegArgs`: N=4 → `[]string{"-scale","1/4","-bmp","-maxmemory","310378","-maxscans","100","-strict"}`.
  - `TestMaxBMPBytes`: 2016×1512 → `9145654`; 1025×13 → `1078+3076*13`.
  - `TestBoundedBufferRefusesPastItsLimit`: limit 4, a write of 5 → an error, and `Len() <= 4`.
  - `TestDjpegEligible`:
    - jpeg+YCbCr → true;
    - jpeg+Gray and jpeg+CMYK → per Task 1;
    - jpeg+RGBA → false;
    - png+YCbCr → false.
- [ ] **Step 2: Write the failing real-binary tests** (`vision_djpeg_test.go`). Fixtures are made with `cjpeg` or `jpegtran` from `testenv.Executable`:
  - `TestDownscaleForVisionShrinksAJPEGOverTheCap`, table over a baseline 3600×3600 (Go-encoded), a progressive 4:4:4 3600×3600 and a progressive 4:2:0 portrait 3000×4400. Each gives `MIMEType == "image/jpeg"`, a long edge of 1024 with the right orientation, and `Width/Height` equal to the input.
  - `TestDjpegRefusalsAreTooManyPixels`, table. Each gives `errors.Is(err, ErrImageTooManyPixels)` and `!errors.Is(err, context.DeadlineExceeded)`:
    - the 3600² progressive cut at 60%;
    - a 101-scan `jpegtran -scans` file, text containing "Scan number";
    - the 8300×6300 4:4:4 header-only progressive, text containing "Backing store not supported" (per Task 1).
  - `TestDjpegTimeoutIsNotTheCallersDeadline`: `runDjpeg` with a 1 ns timeout → `ErrImageTooManyPixels`, not a deadline.
  - `TestDownscaleForVisionWithoutDjpegRefusesAsToday`: `t.Setenv("PATH", t.TempDir())`, a 3600² JPEG → `ErrImageTooManyPixels` with "djpeg is not installed". It needs no binary.
  - `TestDjpegWaitsForTheDecodeSlot`: the slot is held, with a 50 ms context → `context.DeadlineExceeded` and the text "wait for the decode slot".
- [ ] **Step 3: Run them to see them fail.** Run (WSL): `go test -race -count=1 ./internal/multimodal/ -run 'Djpeg|BoundedBuffer|MaxBMP|ShrinksAJPEGOverTheCap'`. Expected: FAIL (undefined `planDjpeg`).
- [ ] **Step 4: Implement**
  - `DownscaleForVision`: after `DecodeConfig`, an over-bound `djpegEligible` image goes to `downscaleWithDjpeg(ctx, raw, cfg, maxBytes)`. Any other over-bound image is refused as today.
  - `downscaleWithDjpeg` does, in order:
    1. `planDjpeg`;
    2. `LookPath`;
    3. the slot, via `acquireDecodeSlot`, also used by the in-process path;
    4. `runDjpeg` with `djpegTimeout`;
    5. `bmp.DecodeConfig`, whose size must equal the plan;
    6. `bmp.Decode`;
    7. `prepareDecoded(img, "bmp", raw, cfg, maxBytes)`;
    8. the `slog.Info` or `slog.Warn` from spec §4.
  - In `runDjpeg`, a non-zero exit wraps the trimmed stderr with `%v`. `runCtx.Err() != nil && ctx.Err() == nil` is the internal timeout.
  - Comments: add the djpeg rows (the formula, `-maxmemory`, the dated spike and Task 1) to the table at `vision_downscale.go:23-48`, and cite `djpeg.c:350-354` and `jmemnobs.c:66-75` @ 2.1.5 beside the constant. Add `// #nosec G204` with the reason, as `cmd/aura-media-index/main.go:143-145`.
  - CI: the two installs.
- [ ] **Step 5: Run it** (WSL): `go vet ./internal/multimodal/ && go build ./... && go test -race -count=1 -cover ./internal/multimodal/`.
  - Expected: PASS, with coverage ≥ 85% (`scripts/coverage_package_policy.json` target).
  - Then `"$HOME/go/bin/golangci-lint" run ./internal/multimodal/` → 0 issues, and `bash scripts/deadcode_production_gate.sh` → exit 0.
- [ ] **Step 6: Commit** by pathspec: `feat(vision): shrink a JPEG over the pixel cap with djpeg`.

### Task 3: Callers keep their contract, and the mutation gate sees the plan

**Files:**
- Modify:
  - `internal/assets/turn_media_loader.go`: the comment at `:96-102` says that a JPEG over the cap is shrunk by djpeg, and that what cannot be shrunk reaches the model as text.
  - `.github/workflows/ci.yml`:
    - `race-db-integration-gates`: an install step before `:2633`;
    - `go-mutation`: an install step before `:2061`, and `vision_djpeg` added to the elicitation-boundaries `scopes` with its comment line;
  - `scripts/critical_mutation_gate.py`: `GO_SCOPES` and `REQUIRED_SCOPE_IDS`.
- Test: `internal/assets/turn_media_loader_test.go`, `scripts/critical_mutation_gate_test.py`. `git grep -n shadow_report -- scripts .github` lists every scope registry; each one gets the new scope.

**Interfaces:**
- Consumes: Task 2's `DownscaleForVision` behaviour.
- Produces: mutation scope `vision_djpeg` → `internal/multimodal/vision_djpeg_plan.go`.

- [ ] **Step 1: Write the failing tests**
  - `TestTurnMediaLoaderDescribesAPhotoTooLargeToDecode`: the fixture becomes a 3200×3200 RGBA PNG. The assertions are unchanged (no media type, no bytes, "open the file").
  - `TestTurnMediaLoaderShrinksAJPEGOverTheCap` (`testenv.Executable(t, "djpeg")`): `jpegOf(t, 3600, 3600)` → an `image/jpeg` part with a long edge of 1024.
  - `TestTurnMediaLoaderDescribesAJPEGItCannotShrink`: `t.Setenv("PATH", t.TempDir())` with the same 3600² JPEG → reference text, never the original bytes.
  - `critical_mutation_gate_test.py`: the expected scope map includes `vision_djpeg`.
- [ ] **Step 2: Run them to see them fail.**
  - Run (WSL): `go test -race -count=1 ./internal/assets/ -run 'TurnMediaLoader'`. Expected: `ShrinksAJPEGOverTheCap` passes already, from Task 2, which confirms the wiring. The renamed fixture passes too.
  - Run: `PYTHONPATH=scripts python3 -m unittest scripts/critical_mutation_gate_test.py scripts/mutation_workflow_test.py`. Expected: FAIL (scope missing).
- [ ] **Step 3: Implement** the registry, the matrix, the installs and the comment.
- [ ] **Step 4: Run it.**
  - Run: `go test -race -count=1 ./internal/assets/...`, then the unittest line from Step 2. Expected: PASS.
  - Run: `bash scripts/coverage_docker.sh` (disposable `aura_cov` only). Expected: the aggregate and the multimodal and assets floors hold.
- [ ] **Step 5: Commit** by pathspec: `test(vision): callers keep their contract over the cap; mutate the djpeg plan`. The body states why the loader fixture changed.

### Task 4: djpeg and PhotoCraft in the images

**Files:**
- Modify:
  - `docker/aura/Dockerfile:162-169`: add `libjpeg-turbo-progs` and `djpeg -help 2>&1 | grep -q -- -maxscans` in the same RUN, and correct the stale `pdftoppm in cmd/aura-media-index` comment;
  - `docker/aura-ingest/Dockerfile:61-65`: add `libjpeg-turbo-progs`, a `FROM alpine:3.24 AS photocraft` stage with `ARG TARGETARCH` and the sandbox's `craft()` logic for one app (`docker/aura-sandbox/Dockerfile:225-241`, pins `:254-256`), and two `COPY --from=photocraft` lines (binary and licences);
  - `scripts/ingest_image_contract_test.sh`.

**Interfaces:**
- Produces: `/usr/bin/djpeg` in both images; `/usr/local/bin/photocraft-cli` and `/usr/local/share/licenses/photocraft/` in `aura-ingest`.

- [ ] **Step 1: Write the failing contract.** Extend `ingest_image_contract_test.sh`:
  - a static check, before `docker run`, that the PhotoCraft version and both SHA-256 values in the ingest Dockerfile equal the sandbox Dockerfile's;
  - `command -v djpeg photocraft-cli`;
  - `djpeg -help 2>&1 | grep -q -- -maxscans`;
  - a non-empty `/usr/local/share/licenses/photocraft`;
  - a 48 MP noiseless PNG written with stdlib `zlib`, put through the Global Constraints PhotoCraft argv, after which `rdjpgcom -verbose` prints `1024w`.
- [ ] **Step 2: Run it against today's image to see it fail** (WSL): `docker build -f docker/aura-ingest/Dockerfile -t aura-ingest:local . && bash scripts/ingest_image_contract_test.sh`. Expected: FAIL ("djpeg" missing).
  - Record `docker image inspect -f '{{.Size}}' aura-ingest:local` first.
- [ ] **Step 3: Implement both Dockerfiles.**
- [ ] **Step 4: Run it.**
  - Rebuild `aura-ingest:local`, then run the contract test. Expected: `ok: ingest image contract`.
  - `docker build -f docker/aura/Dockerfile -t aura:local .` → success.
  - Record both image sizes. Expected: ingest about +113 MB, aura about +0.4 MB.
  - Inside `aura-ingest:local`, run djpeg on p48_444_progressive and PhotoCraft on p48.png with the spike's `rss.go`. Append the rows to the follow-up as "inside aura-ingest", since the spike could not measure there.
- [ ] **Step 5: Commit** by pathspec: `build(images): ship djpeg in aura and aura-ingest, PhotoCraft in aura-ingest`. The body gives the size cost.

### Task 5: The indexer resizes a PNG over the cap with PhotoCraft

**Files:**
- Create: `cmd/aura-media-index/photocraft.go`
- Modify: `cmd/aura-media-index/main.go` (`mediaRuntime.resizePNG`, set in `newMediaRuntime`; one `fitLargePNG` call before `DescribeImageForRetrieval` at `:84`), `internal/multimodal/vision_downscale.go` (`OverPixelCap`, which `DownscaleForVision`'s own check at `:110` now calls)
- Test: `cmd/aura-media-index/photocraft_test.go`, `internal/multimodal/vision_downscale_test.go`

**Interfaces:**
- Consumes: Task 1's alpha rule.
- Produces:
  - `func OverPixelCap(cfg image.Config) bool` (package `multimodal`);
  - `func photocraftArgs(in, out string, w, h int) []string`;
  - `func resizeWithPhotocraft(ctx context.Context, raw []byte, w, h int) ([]byte, error)`;
  - `func (r mediaRuntime) fitLargePNG(ctx context.Context, raw []byte, mimeType string) ([]byte, string)`;
  - field `resizePNG func(context.Context, []byte, int, int) ([]byte, error)`.

- [ ] **Step 1: Write the failing tests**
  - `TestOverPixelCapMatchesTheBounds`:
    - YCbCr 3515×3516 → true, 3515×3515 → false;
    - RGBA 3180×3181 → true;
    - gray 4772×4772 → false.
  - `TestPhotocraftArgsFollowTheLongEdge`:
    - 8000×6000 → the exact argv with `{"width":1024}`;
    - 6000×8000 → `{"height":1024}`;
    - 4000×4000 → `width`.
  - `TestFitLargePNGResizesOnlyAPNGOverTheCap`, with a fake `resizePNG`:
    - an opaque (colour type 2) 3200×3200 PNG header → called once with 3200×3200, returning the fake JPEG and `image/jpeg`;
    - a small PNG → not called;
    - an over-cap JPEG header → not called;
    - a colour type 6 (alpha) 3200×3200 header → called, or not called if Task 1's alpha rule applies.

    The indexer's tests need their own header helper: `pngHeader` lives in `internal/multimodal`'s tests and always writes colour type 6.
  - `TestFitLargePNGFallsBackWhenPhotocraftFails`: the fake returns an error → the original bytes and type.
  - `TestResizeWithPhotocraftWithoutTheBinary`: `t.Setenv("PATH", t.TempDir())` → an error containing "photocraft-cli is not installed".
  - `TestResizeWithPhotocraftRejectsAWrongOutput`: the output check, a helper `checkResizedJPEG(b []byte) error`. A PNG → error; a 2000×1500 JPEG → error; a 1024×768 JPEG → nil.
  - Through `derive`, with the existing fake sidecar of `main_test.go`: an over-cap PNG reaches the describer as `image/jpeg`, carrying the fake's bytes.
- [ ] **Step 2: Run them to see them fail.** Run (WSL): `go test -race -count=1 ./cmd/aura-media-index/ ./internal/multimodal/ -run 'OverPixelCap|Photocraft|FitLargePNG'`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement** per spec §6:
  - a private `os.MkdirTemp` directory, `in.png` with mode 0600, `exec.CommandContext` with a 2 min timeout and `CombinedOutput`, the `#nosec G204` reason as `main.go:143-145`;
  - `checkResizedJPEG` on `out.jpg`;
  - on every failure, `fitLargePNG` logs `slog.Warn` and returns the input.
- [ ] **Step 4: Run it** (WSL): `go vet ./cmd/aura-media-index/ ./internal/multimodal/ && go build ./... && go test -race -count=1 ./cmd/aura-media-index/ ./internal/multimodal/`, then `golangci-lint run` on both, then `bash scripts/deadcode_production_gate.sh`. Expected: PASS, 0 issues, exit 0.
- [ ] **Step 5: Commit** by pathspec: `feat(ingest): resize a PNG over the vision pixel cap with PhotoCraft`.

### Task 6: Ship, measure on the lab VM, record

**Files:**
- Create: `docs/superpowers/verification/<run date>-vision-large-photo-resize.md`
- Modify: `prd.md` (the same paragraph: the live figures), this plan (a status line)

- [ ] **Step 1: Ship.**
  - Fetch and compare migrations (none here).
  - Push with the WSL recipe from CLAUDE.md.
  - Wait until every job on the SHA is green: `unit-test`, the coverage gate, `race-db-integration-gates`, `go-mutation` (`vision_djpeg` ≥ 70%), `ingest-sidecar-test` and "Publish Aura edge image".
  - The operator consents to the update in the cockpit. The VM gets it through the updater only.
- [ ] **Step 2: Check the VM before the run.** Record:
  - `aura version` = the SHA;
  - the image digests of `aura` and `aura-ingest`;
  - `djpeg -help 2>&1 | grep -c maxscans` in both;
  - `photocraft-cli --version` in `aura-ingest`.
- [ ] **Step 3: Chat leg** (the operator drives it).
  - The operator uploads a real phone JPEG of at least 48 MP in a new conversation and asks what is in it.
  - Meanwhile a VM script samples aura's cgroup `memory.current` every 100 ms and reads `memory.events` before and after.
  - Expected:
    - the Info log line, with scale and `djpeg_ms`;
    - a description the operator confirms;
    - a peak under 768 MiB;
    - `oom_kill` unchanged.
- [ ] **Step 4: Indexer legs.**
  - The same photo indexes, and its description chunk is present in the identity's ArcadeDB.
  - The spike's synthetic 48 MP PNG indexes with a description.
  - `aura-ingest`'s cgroup is sampled for both.
  - The same PNG in chat still reads as reference text.
- [ ] **Step 5: Write the verification file** per spec §Live acceptance.
  - It holds header facts and counts only: no photo, no EXIF/GPS, no description text (the repo is public). It also says what the run does not prove.
  - Add the live figures to the PRD paragraph, and a status line to this plan.
  - Commit by pathspec: `docs(verification): photos over the vision pixel cap resized on the lab VM`.
