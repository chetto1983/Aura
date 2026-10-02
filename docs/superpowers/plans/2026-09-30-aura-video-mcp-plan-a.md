# aura-video-mcp Plan A: a trustworthy Studio export and projects kept out of RAG — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Studio export either writes the film the editor shows, or stops and writes nothing:
- the written film has every fade, and each source is fetched once;
- a stopped export names, by its file name, the source it could not play.

A saved Studio project never becomes a searchable document, and the projects saved before this plan leave the index once.

**Revised 2026-10-01** after three reviews and the operator's rulings: the dialog names a source by its file name, and projects saved before the change are re-keyed once at boot. Every new or changed unit and integration test was run RED, then GREEN, in a scratch copy of the tree, and each task says what was measured there. The E2E specs and the lab-VM scripts were only checked statically (tsc, oxlint, prettier, `bash -n`, shellcheck, a Postgres parse of the SQL): Task 8 runs the specs RED first, and Tasks 0 and 9 run the scripts.

**Architecture:**
1. **The compile seam.** After `flow.compile()`, `withKeyframes` (new `videoflow_keyframes.ts`) moves every keyframed property from a layer's static `properties` into its `animations`, on the layer's source clock. That is the only place VideoFlow 1.3.4's renderer reads keyframes.
2. **The export seam.** Both exports run through `renderLoaded` (new `videoflow_media.ts`), which:
   - lets VideoFlow load every layer through its own `loadedMedia` cache;
   - throws `ExportSourceError` when VideoFlow disabled a layer, or when a sound that is not muted will not decode;
   - decodes each source's audio once, from the bytes that cache already holds;
   - destroys the renderer exactly once.

   The dialog turns the error into a sentence that names the source's file.
3. **Projects.**
   - a project file is named `<slug>.aura-video.json`;
   - the Go object key keeps that suffix whole;
   - the ingest's one path matcher excludes `**/*.aura-video.json`;
   - a boot pass (`StudioProjectRekey`) moves each project saved before the change to the suffixed key through the file manager's own transfer, by a rule no other JSON passes.

**Tech Stack:**
- TypeScript + React, with VideoFlow 1.3.4 (`@videoflow/core` and `@videoflow/renderer-browser`, patched by `web/patches/@videoflow+renderer-browser+1.3.4.patch`, which touches only `googleFontLoader.js`);
- vitest (jsdom) and Playwright;
- Python 3.12 with CocoIndex 1.0.24 (`services/ingest`, pinned in `docker/aura-ingest/requirements.txt:13`);
- Go (`internal/objectstore`, `internal/assets`, `cmd/aura`).

**Spec:** `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md`, sections:
- §"Fixes to the Studio export";
- §Architecture → "Project indexing (M1 of the audio spec)";
- §"Tests and E2E";
- §"What this does not prove".

The evidence is `spikes/video-mcp-render/FINDINGS.md` §S1.3 and §S1.4.

## Global Constraints

**Scope**
- Plan A covers seven things and nothing else:
  1. a source that cannot be fetched or read fails the export, naming its file;
  2. fades reach the export;
  3. each source is fetched once;
  4. projects are named `<slug>.aura-video.json`;
  5. the ingest skips `**/*.aura-video.json`;
  6. projects saved before the change are re-keyed once, at boot;
  7. the E2E, locally and on the lab VM.
- The sidecar, the render jobs and the tools belong to Plans B and C.
- `exportProject(project, urls, options?)` keeps its signature; Plan B's render page calls it unchanged.
- VideoFlow stays at 1.3.4 with the existing patch: no fork, no new patch.

**Spec rules**
- "The suffix keeps the `.json` extension the upload allowlist accepts (`internal/assets/limits.go`)". Quoted from the spec.
- "Any other `.json` the operator uploads is still indexed". Quoted from the spec.

**Operator rulings (2026-10-01)**
- The dialog names the source by its **file name**, from the asset row (`getAsset` → `file_name`, as `projectStore.ts:420-422` reads it), and falls back to the asset id only when the row gives none. The error keeps `assetId`.
- Projects saved before Plan A are **re-keyed once**, through the existing relocation path (`Browser.transfer`, `internal/assets/filemanager_ops.go:125`). The identification rule must not be able to sweep in an operator's own JSON. Task 0 counts the candidates first, read-only; Task 9 shows them leaving the index.

**Files and quality bars**
- Every new user-facing string exists in en and it, in `web/src/i18n/resources.videoStudio.ts`. Re-read that file just before editing it: other sessions edit i18n.
- No file over 600 lines. `scripts/check-file-size.sh` runs in pre-commit.
- Coverage is at least 85 % statements, branches and lines on every touched web file (vitest). `internal/assets` is a target-mode Go package (≥ 85 % across the full tag matrix).
- The Go package `internal/objectstore` keeps its exact baseline, re-pinned to the new denominator (Task 6) and never lowered.
- Mutation testing runs in CI only (Stryker, `break: 70`, `web/stryker.config.json:79`). Never run it locally. A module Stryker mutates needs its suite in `web/vitest.stryker.config.ts` `mutationTests`, in the same step.

**How to run tools**
- Never run a Windows `.exe` from Git Bash. `node`, `npx`, `go`, `python3`, `docker` and `gh` are all exes there.
- Run tools in WSL through script files: `MSYS_NO_PATHCONV=1 wsl bash <script> <args>`. Never put `$VAR` inside `wsl bash -c "…"`.
- The web tools run in WSL against the shared `web/node_modules`, which holds the Linux bindings beside the Windows ones (`@rolldown/binding-linux-x64-gnu`, `@oxlint/binding-linux-x64-gnu`, `@typescript/typescript-linux-x64`, checked 2026-10-01). If a tool stops on a missing `linux-x64` binding, stop and ask the operator.
- Never run `npm install` or `npm ci`.
- Never run `go build ./...`. The pre-push hook builds the pushed tree.

**Commits and pushes**
- Commit exact paths only, from WSL with lefthook (`$W/commit.sh`, below).
- `--no-verify` is forbidden.
- This tree is shared: never stage, reset or revert another session's files.
- There are two pushes: the code in Task 8, after a local GREEN, and the PRD's post-fix record in Task 9. CI must be green before the lab-VM E2E runs.

**The lab VM and the PRD**
- The E2E runs against the lab VM only through `$W/e2e-vm.sh`.
- Nothing else touches the VM except:
  - the read-only measurements (Task 0, Task 8 Step 8, Task 9 Step 1);
  - the controlled probe, which deletes what it creates (Task 9 Step 2);
  - the shipped image's own boot pass.
- The PRD is written first and records only what was measured, plus the rules phrased as rules (Task 0). Every sentence about how things are after the fix waits for Task 9's measurement. Every amendment says what it does not prove (CLAUDE.md "PRD-first principle").

**Path shorthand**
- `$W` means your own session's scratchpad as WSL sees it (`/mnt/c/Users/…/scratchpad`). Every helper below lives there.
- The operator's earlier helper directory (`$S` in this plan's first revision) no longer exists. Each helper is written out in full below; read each one before you first run it.
- Tool shells do not keep variables. Either write the path out in full, or start the same command line with `W=…`. Git Bash expands `$W` before `wsl` sees it, so the WSL spelling is the one to set.
- Plain Git Bash commands in this plan (`wc -l`, `git -C /d/Aura …`, `grep`) run from the repository root, `/d/Aura`. There, your scratchpad is spelled `/c/Users/…/scratchpad`, because `/mnt/c` exists only inside WSL.

## Review Focus

No task's main path exercises these five inputs, and each is the likeliest way this plan could fail a real user. Each is pinned by a test in the task that owns the code.

1. **An operator's own `.json`, attached from the cockpit to a chat that has no thread yet.** Its row looks exactly like a pre-Plan-A Studio save. The boot pass must leave it where it is. Pinned in Task 7 by "JSON that is not a project" and the content check's thirteen refusals.
2. **A clip at a speed other than 1× that fades.** The fade lasts half a second of what the viewer sees, not of source. Pinned in Task 1 by "fades a sped-up clip over half a second of film, not of source".
3. **A flipped clip.** `scale: [-1, 1]` is an array but not a keyframe list, and must stay a static value. Pinned in Task 1 by "leaves no keyframes in a layer's static properties".
4. **A source the browser cannot reach at all** (a CORS refusal or a dead network, so no HTTP status), or one whose body breaks mid-download. The export still stops as `unreachable`, by asset. Pinned in Task 2 by "a CORS refusal has no status" and "whose body fails halfway".
5. **A name that only looks like the marker** (`reel.aura-video.json.bak`, `reel-aura-video.json`), or one in capitals or with a Windows path. The key and the matcher agree: only a name that ends in `.aura-video.json` is skipped. Pinned by Task 5's inclusion cases and Task 6's table.

## What the plan stands on (verified 2026-09-30, revised 2026-10-01)

**The fade root cause, found by systematic debugging and reproduced against the real compile.**
1. `clipOpacity` (`web/src/videoStudio/videoflow.ts:58-78`) hands the builder an opacity *keyframe array* as the layer's initial property (`:195`).
2. VideoFlow's compile stores any initial property as the value of ONE step keyframe at `sourceStart` (`web/node_modules/@videoflow/core/dist/VideoFlow.js:629-638`).
3. It then serialises a one-keyframe property as a static value (`:856-864`), so the whole array lands in `properties.opacity`.
4. `BaseLayer.toJSON` would have recognised it (`core/dist/layers/BaseLayer.js:231`, `value[0]?.time !== undefined`), but the flow compile never goes through that path.
5. At render time a static property goes through `ensureUnit` (`renderer-browser/dist/layers/RuntimeBaseLayer.js:311-323`, `:472-494`). An array is read as a vector, and each keyframe object in it becomes the number NaN: `opacity` declares no unit, so `getNumUnit` falls back to `parseFloat('[object Object]')`.
6. `LayerRasterizer.js:909` sets `ctx.globalAlpha = Math.max(0, Math.min(1, Number(props.opacity ?? 1)))`, which is NaN, and a canvas ignores a NaN `globalAlpha`. The layer is drawn fully opaque.

Proof: `videoflow_keyframes.test.ts` (Task 1) builds the renderer's own runtime layers from the compiled JSON with `createBuiltinLayerTypeRegistry` and reads `applyTransitions(frame, getPropertiesAtFrame(frame)).opacity`. On today's code, 5 of 5 tests fail with `expected NaN to be close to 0.4` and `expected { fit: 'cover', mute: true, …(8) } to not have property "opacity"`.

What was **measured on a frame** is a clip's fade-out (FINDINGS.md S1.3). Title fades (the inspector writes `props.opacity` keyframes), the fade-to-black and fade-to-white washes, and the Stage preview take the same path, which the unit test shows on the renderer's own layers. They were not measured on an exported frame or on screen. The Stage compiles with the same `toVideoJSON` (`Stage.tsx:238`), and `@videoflow/renderer-dom` builds its layers with renderer-browser's `createBuiltinLayerTypeRegistry` and draws with its `LayerRasterizer` (`renderer-dom/dist/DomRenderer.js:43`). Keyframe times must be absolute source seconds (`sourceTimeAtFrame`, `RuntimeBaseLayer.js:175-186`), the rule the PRD already records for volume (prd.md §12 "VideoFlow volume (S1)").

**Media, from VideoFlow's source.**
- Every sourced layer takes its bytes from `loadedMedia`, one refcounted cache per page keyed by URL: `core/dist/MediaCache.js:45-66`, singleton at `:174`. The consumers:
  - `RuntimeVideoLayer.js:63`
  - `RuntimeAudioLayer.js:31`
  - `RuntimeImageLayer.js:16`
  - `audio/mixer.js:69`
- A failed fetch throws on `!response.ok` (`:138`) and is removed from the map, so it "does not poison the URL forever" (`:56-58`).
- `BrowserRenderer.initLayers` catches a layer that fails to initialise, logs a warning, sets `json.settings.enabled = false` and carries on (`BrowserRenderer.js:427-445`).
- Two consequences follow:
  - After `initLayers`, a disabled layer whose URL is **absent** from the cache never got its bytes (`unreachable`). One whose URL is **present** got bytes that did not play (`undecodable`).
  - The HTTP status never reaches the page; it survives only in the console warning.
- Today's `primeDecodedBuffers` fetches every source a second time (`videoflow.ts:340`) and swallows every failure (`:343`). That is the "2 GETs per source" and the black, silent 26,604 B MP4 in FINDINGS.md:237.
- The mixer never plays a muted layer: `scheduleBufferOnContext` reads `mute` first (`audio/mixer.js:245-247`), and `decodeLayerAudio` answers null for a failed decode.
- `BrowserRenderer.destroy()` destroys every layer, and each layer releases what it acquired. A layer whose acquire resolves after the destroy is never released: VideoFlow's fetch takes no signal.

**Why the key, not the name, must carry the marker.**
- The S3 walker matches the key relative to the prefix (CocoIndex 1.0.24 `connectors/amazon_s3/_source.py:304-317`).
- The audit that follows each cycle lists keys with `list_objects_v2` and never HEADs an object (`services/ingest/source.py:183-223`, `expected_keys`). So even a walker that read names could not make the audit agree. The key is the one thing both sides see.
- `PatternFilePathMatcher` uses globset semantics (`cocoindex/resources/file.py:227-279`), and `**/*.aura-video.json` matches at the bucket root too (measured by Task 5's pytest).
- `objectstore.AssetKey` builds `<folder><assetID><ext>` (`internal/objectstore/asset_placement.go:90-92`). The real name rides in S3 metadata (`PlaceAsset`, `:40-45`), which only a HEAD reads.
- Today `assetExtension` keeps only `path.Ext` (`:99`): `A-film.aura-video.json` would be stored as `chat/<uuid>.json`, and the ingest could not tell it from any other JSON.
- The `<uuid>` in a key is the one Presign minted for the object, and the row's `id` is the database's `gen_random_uuid()`. **The key's uuid is not the asset id** (Task 0 counts it on the VM).

**Spec rulings.** The spec is corrected here, in the PRD by Task 0, and by dated notes beside its sentences:
1. "The ingest skips that suffix" cannot hold by the file name alone, because the walker and the audit see only keys (above). Go keeps the suffix in the key (Task 6), and a parity test binds the three spellings.
2. "Naming the source": the page never learns the HTTP status (above). The error carries the **asset id** and `unreachable` | `undecodable`. The dialog shows the source's **file name**, read from the asset row (operator ruling).
3. "Projects saved before the change stay indexed until they are saved again" is wrong twice. Saving writes a **new** asset, because every version is kept. So an old `chat/<uuid>.json` project would stay indexed until deleted. Task 7 moves each one, once, at boot (operator ruling).
4. The fade bug is wider than clip fade-outs (titles, washes, preview). One fix at the compile seam covers them all.
5. A clip's fade lasts half a second of **film** (`clipTimelineDuration`), not of source. `clipOpacity` placed its fade by `clip.duration`, the clip's length in source seconds, so at 2× the fade-out's keyframes would fall past the clip's last frame on screen.
6. A sound whose bytes will not decode fails the export, because it would export as silence. Two exceptions:
   - a **muted** sound does not fail it, because the mixer never plays it;
   - a *video* whose audio will not decode is still left to the mixer, as before. VideoFlow hard-codes `RuntimeVideoLayer.hasAudio` to true, so a clip with no audio track always reaches the decoder, and such a clip exports silent by design.
7. Two existing tests are rewritten, not weakened. The commit body in Task 2 justifies both, per CLAUDE.md "NEVER MODIFY TESTS TO MAKE THEM PASS":
   - one stood for "no audio track" with a *rejected fetch*, which is the very failure this plan stops swallowing;
   - the other held a fetch this plan deletes.
8. `loadProject` reads a project by asset id and never looks at its name (`projectStore.ts:428-443`), so old `.json` projects still load. The E2E's `reopen` keeps uploading `project.json` (Task 8), and a re-keyed project keeps its id.
9. The asset still receives a `document_id`, because `DocumentProcessor` names one for every document. That is harmless: it "writes nothing and reads nothing", and the knowledge catalog asks ArcadeDB whether a document is indexed (`internal/assets/document_processor.go:11-33`).

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `prd.md` | modify §12 (after `:688`, `:714-715`, after `:726`) | record the measured bugs, the key shape and the old projects, and the rules (Task 0); the post-fix measurements (Task 9) |
| `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md` | modify (after `:160`, after `:272`) | dated notes beside the two sentences the measurements overturned (Task 0) |
| `web/src/videoStudio/videoflow_keyframes.ts` | create | `withKeyframes(json)`: keyframed properties → `animations`, on the source clock |
| `web/src/videoStudio/__tests__/videoflow_keyframes.test.ts` | create | real compile and real runtime layers: the opacity the renderer draws |
| `web/src/videoStudio/videoflow_media.ts` | create | `renderLoaded`, `ExportSourceError`, `SourceFailure`; the media load, the refusal and the once-per-source decode moved out of `videoflow.ts` |
| `web/src/videoStudio/videoflow.ts` | modify | `clipOpacity` on film time; `withKeyframes` after compile; `exportProject` through `renderLoaded`; the pre-decode removed (400 → 406 → 329 lines) |
| `web/src/videoStudio/videoflow_exportAudio.ts` | rewrite | the sound export through `renderLoaded`; its encoder in `writeWave` |
| `web/src/videoStudio/__tests__/videoflowFakes.ts` | modify | VideoFlow's real `MediaCache` behind the fake renderer; `layer()` typed like VideoFlow's layers; a decoder that can refuse |
| `web/src/videoStudio/__tests__/videoflow_export.test.ts` | rewrite | fetch counts, failure by asset, a muted sound, a broken body, the abort while media loads |
| `web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts` | create | the WAVE export (at 0 % coverage today) |
| `web/stryker.config.json` | modify `:77` | the two new modules join the CI mutation gate |
| `web/vitest.stryker.config.ts` | modify `:84-85` | their three suites join `mutationTests`, or every mutant scores `NoCoverage` |
| `web/src/videoStudio/VideoStudio_export.tsx` | modify `:1-7`, `:30-32`, `:68-75` | `failureText`: the dialog names the source by its file name |
| `web/src/i18n/resources.videoStudio.ts` | modify (en `export` near `:208`, it near `:415`) | `sourceUnreachable`, `sourceUndecodable` |
| `web/src/videoStudio/__tests__/VideoStudio_export.test.tsx` | create | the dialog's failure text through the real i18n bundle, en and it, with the name read from the asset row |
| `internal/assets/testdata/studio-project.json` | create | a real Studio project, loaded by the Studio's parser and accepted by the Go check |
| `web/src/videoStudio/projectStore.ts` | modify `:16-18`, after `:39`, `:69-71` | `PROJECT_FILE_EXTENSION`; `saveProject` names the file with it |
| `web/src/videoStudio/__tests__/projectStore.test.ts` | modify (import, tests after `:183` and `:225`) | the saved file's name; the shared fixture loads |
| `services/ingest/tests/fake_s3.py` | create | the bucket stand-in, shared instead of imported from another test's privates |
| `services/ingest/tests/test_audit.py` | modify (whole file) | uses `fake_s3` |
| `services/ingest/source.py` | modify `:46`, `:96-103`, `:183-193` | `STUDIO_PROJECT_PATTERN`, `path_matcher()`, shared by `walk` and `expected_keys` |
| `services/ingest/tests/test_studio_projects.py` | create | CocoIndex's own matcher: projects out, other JSON in |
| `internal/objectstore/asset_placement.go` | modify `:71-72`, `:88-89`, `:94-99` | `StudioProjectSuffix`; `assetExtension` keeps it whole; the key's id is not the row's |
| `internal/objectstore/studio_project_test.go` | create | the key table and the three-language parity test |
| `scripts/coverage_package_policy.json` | modify `:57` | re-pin `internal/objectstore` 456/598 → 459/601 |
| `internal/assets/studio_project_rekey.go` | create | `StudioProjectRekey`: the identification rule and the move through `Browser.transfer` |
| `internal/assets/studio_project_rekey_test.go` | create | the move, every refusal, paging, a project it cannot read, the content check |
| `internal/assets/service_test.go` | modify `:439-462` | the fake `ListRecent` honours `before_id` and the query's order |
| `internal/assets/service_delete_test.go` | modify `:64-71` | `captureLogs(t, level)` |
| `cmd/aura/studio_project_rekey_wiring.go` | create | `buildStudioProjectRekey`, `rekeyStudioProjects` |
| `cmd/aura/studio_project_rekey_wiring_test.go` | create | an unwired daemon builds nothing; a failure is a warning, never a stopped boot |
| `cmd/aura/serve.go` | modify (after `:273`) | the boot pass, once the asset service exists (566 → 567 lines) |
| `web/e2e/support/frames.ts` | create | `readFrames`, `meanLevel`: frames of an exported file, decoded in the page |
| `web/e2e/video-studio.spec.ts` | modify `:77-173` | `twinFrameDiff` reads its frames through `readFrames` (398 → 339 lines) |
| `web/e2e/video-studio-export.spec.ts` | create | fade on frames against a control, fetch count, two lost sources by name, the saved key |
| `internal/webui/dist` | rebuild | the embedded cockpit the VM runs |

`web/e2e/support/videoStudio.ts` is **not** changed: `reopen` keeps uploading a plain `project.json` (Task 8).

## Helper scripts

Create each of these in `$W` from the text below. Every one runs from WSL as `MSYS_NO_PATHCONV=1 wsl bash $W/<name>.sh …`. Read each one before you first run it. The scripts that touch the lab VM read the operator's credentials from `D:\Aura\.env.google` and never print them.

| Script | What it does |
|---|---|
| `commit.sh <message file> <paths…>` | commits exactly those paths with lefthook's pre-commit gates |
| `vt.sh <paths…>` | vitest in `web/` on those paths: every failure with its context, then the `Test Files` and `Tests` lines |
| `webcheck.sh <files…>` | `prettier --write` on the files, then `tsc`, oxlint (read `Found N warnings and N errors`: oxlint exits 0 either way), the lint contract, knip and `prettier --check .` |
| `cov.sh <include glob> <test paths…>` | vitest coverage over the paths, printing the table's rows. The text reporter cuts long names from the left (`...oflow_media.ts`). |
| `go-gates.sh <packages…>` | gofmt, vet, race tests, the unit tier's covered/total statements, and golangci-lint, per package |
| `ingest-pytest.sh <paths…>` | pytest on `services/ingest` tests in a venv with the CocoIndex the image pins |
| `build.sh` | builds the web app into `internal/webui/dist` and prints how many dist files changed |
| `push.sh <log name>` | `git push origin master` with lefthook's pre-push gates; fails if no gate ran |
| `poll-ci.sh <sha> [minutes]` | waits until no GitHub Actions run for that commit is queued or running (150 minutes by default), then prints each run's conclusion |
| `waitfix.sh <sha>` | read-only: waits until the lab VM's `aura` contains that commit, then five minutes for the edge restart |
| `e2e-local.sh <label> <playwright args…>` | builds the `aura` image from this tree, runs it on `:9080`, runs Playwright against it |
| `e2e-vm.sh <label> <playwright args…>` | Playwright against `https://192.168.101.158` as the operator's cockpit account |
| `vm-lib.sh` | sourced by the VM scripts: ssh, read-only SQL, the ingest's cycle, the ingest's own index reader |
| `vm-sql.sh <file>` | read-only SQL on the lab VM's Postgres |
| `vm-json-index.sh <out>` / `--after <file>` | read-only: the re-key candidates and whether the index holds them; after the fix, where each went |
| `vm-project-probe.sh` | the controlled probe: saves a project and an operator's JSON, reads the index two cycles later, deletes both on every exit |

`$W/commit.sh`:

```bash
#!/usr/bin/env bash
# Commits exactly the given paths with lefthook's pre-commit gates. Args: <message file> <paths…>
set -uo pipefail
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
msg="$1"; shift
cd /mnt/d/Aura && git add -N -- "$@" && \
  LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -F "$msg" -- "$@"
echo "COMMIT_RC=$?"
git log --oneline -1
```

`$W/ingest-pytest.sh`: the ingest's tests with the CocoIndex the image pins. The venv lives outside the repository. The authoritative full suite is CI's `make ingest-test` (`.github/workflows/ci.yml:963`).

```bash
#!/usr/bin/env bash
# pytest on services/ingest tests, with the CocoIndex docker/aura-ingest/requirements.txt pins.
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
V=$HOME/.cache/aura-ingest-plan-a-venv
if [ ! -x "$V/bin/python" ]; then
  UV_LINK_MODE=copy uv venv -q -p 3.12 "$V"
  uv pip install -q -p "$V/bin/python" "cocoindex[amazon_s3]==1.0.24" pytest
fi
cd /mnt/d/Aura/services || exit 1
PYTHONDONTWRITEBYTECODE=1 "$V/bin/python" -m pytest -p no:cacheprovider -q "$@"
```

`$W/vt.sh`:

```bash
#!/usr/bin/env bash
# vitest in web/ on the given paths: every failure with its context, then the Test Files and Tests lines.
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
cd /mnt/d/Aura/web || exit 1
out="$(mktemp)"
./node_modules/.bin/vitest run "$@" > "$out" 2>&1
sed 's/\x1b\[[0-9;]*m//g' "$out" | grep -E -A14 "FAIL |Error:|AssertionError|×" | grep -v '^\s*$' | head -150
sed 's/\x1b\[[0-9;]*m//g' "$out" | grep -E "Test Files|Tests  "
rm -f "$out"
```

`$W/webcheck.sh`:

```bash
#!/usr/bin/env bash
# The web gates over the given files (paths relative to web/): prettier --write on them, then tsc,
# oxlint on them, the lint contract, knip as `npm run deadcode` runs it, and prettier over the tree.
# oxlint exits 0 even with errors: read its "Found N warnings and N errors".
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
cd /mnt/d/Aura/web || exit 1
echo "== prettier write"; ./node_modules/.bin/prettier --write "$@" > /dev/null && echo written
echo "== tsc"; ./node_modules/.bin/tsc --noEmit -p tsconfig.json 2>&1 | tail -30
echo "== oxlint"; ./node_modules/.bin/oxlint --type-aware --max-warnings=0 "$@" 2>&1 | tail -30
echo "== contract"; node scripts/lint-contract.mjs 2>&1 | tail -15
echo "== knip"; ./node_modules/.bin/knip --exclude types,nsTypes,enumMembers,duplicates 2>&1 | tail -20
echo "== prettier check"; ./node_modules/.bin/prettier --check . 2>&1 | tail -8
```

`$W/cov.sh`:

```bash
#!/usr/bin/env bash
# vitest coverage in web/: cov.sh <include glob> <test paths...>. Prints the table's header and file
# rows. The text reporter cuts long names from the left (`...oflow_media.ts`).
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
cd /mnt/d/Aura/web || exit 1
include="$1"; shift
out="$(mktemp)"
./node_modules/.bin/vitest run --coverage --coverage.reportOnFailure=true --coverage.reporter=text \
  "--coverage.include=$include" --coverage.thresholds.statements=0 --coverage.thresholds.branches=0 \
  --coverage.thresholds.functions=0 --coverage.thresholds.lines=0 "$@" > "$out" 2>&1
sed 's/\x1b\[[0-9;]*m//g' "$out" | grep -E "% Stmts|\.tsx? +\||All files|Test Files|Tests  " | head -30
rm -f "$out"
```

`$W/go-gates.sh`:

```bash
#!/usr/bin/env bash
# The Go gates on the given packages (./internal/assets/ and the like): gofmt, vet, race tests,
# the unit tier's covered/total statements, and lint. Never `go build ./...` (CLAUDE.md).
set -uo pipefail
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
cd /mnt/d/Aura || exit 1
GOROOT_BIN="$(go env GOROOT)/bin"
export PATH="$GOROOT_BIN:$PATH"
for pkg in "$@"; do
  dir="${pkg#./}"; dir="${dir%/}"
  echo "######## $pkg"
  echo "== gofmt (no file listed = formatted)"; gofmt -l "$dir"
  echo "== vet"; go vet "$pkg"
  echo "== race"; go test -count=1 -race "$pkg" 2>&1 | tail -15
  profile="$(mktemp)"
  go test -count=1 -coverprofile="$profile" "$pkg" > /dev/null 2>&1 && \
    awk 'NR>1 {t+=$2; if ($3>0) c+=$2} END {print "unit tier covered/total =", c "/" t}' "$profile"
  rm -f "$profile"
  echo "== lint"; golangci-lint run "$pkg..." 2>&1 | tail -10
done
```

`$W/build.sh`:

```bash
#!/usr/bin/env bash
# Builds the web app into internal/webui/dist (vite.config.ts outDir), the bytes the aura image
# embeds and CI's web-dist-freshness gate compares, and prints how many dist files changed.
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
log="$(mktemp)"
cd /mnt/d/Aura/web && npm run build > "$log" 2>&1
echo "BUILD_RC=$?"
tail -5 "$log"
rm -f "$log"
cd /mnt/d/Aura && echo "dist files changed: $(git status --porcelain -- internal/webui/dist | wc -l)"
```

`$W/push.sh`:

```bash
#!/usr/bin/env bash
# `git push origin master` from WSL with lefthook's pre-push gates. core.hooksPath holds the Windows
# path, which WSL git cannot resolve: without the -c below, no gate runs and the push still exits 0.
# The log is kept in this scratchpad: push.sh <log name>.
set -uo pipefail
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
log="$(dirname "$0")/$1"
cd /mnt/d/Aura || exit 1
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master > "$log" 2>&1
rc=$?
tail -25 "$log"
echo "PUSH_RC=$rc"
if ! grep -q lefthook "$log"; then echo "NO LEFTHOOK BANNER: no pre-push gate ran" >&2; exit 1; fi
exit "$rc"
```

`$W/poll-ci.sh`:

```bash
#!/usr/bin/env bash
# Waits until no GitHub Actions run for <sha> is queued or running, then prints each run's status,
# conclusion and name. poll-ci.sh <sha> [minutes]: 150 by default, because a full Stryker run alone
# measured 86m34s (.github/workflows/ci.yml, web-mutation-stryker). A timeout prints what is still
# running and exits 1: poll again, never call a running job green.
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
sha="${1:?sha}"
limit=$(( ${2:-150} * 60 ))
waited=0
cd /mnt/d/Aura || exit 1
while :; do
  runs="$(gh run list --commit "$sha" --limit 50 --json status,conclusion,name \
    --jq '.[] | "\(.status)\t\(.conclusion)\t\(.name)"')"
  if [ -n "$runs" ] && ! grep -qvE '^completed' <<< "$runs"; then echo "$runs"; exit 0; fi
  if [ "$waited" -ge "$limit" ]; then
    echo "timeout after $((waited / 60)) min; not finished:"
    grep -vE '^completed' <<< "$runs"
    exit 1
  fi
  sleep 60
  waited=$((waited + 60))
done
```

`$W/waitfix.sh`:

```bash
#!/usr/bin/env bash
# READ-ONLY. Waits (up to an hour) until the lab VM's running aura contains <sha>: the commit its
# `aura version` prints (cmd/aura/version.go, stamped from VCS_REF) is <sha> or descends from it.
# The updater restarts caddy, the ingest and cloudflared about two minutes after aura flips
# (seen once, 2026-09-28), so it then waits five more and prints every container's uptime.
set -uo pipefail
. "$(dirname "$0")/vm-lib.sh"
sha="${1:?sha}"
cd /mnt/d/Aura || exit 1
running=""
for _ in $(seq 1 60); do
  running="$(vm "echo $PW | sudo -S -p '' docker exec aura aura version" 2>/dev/null | sed -n 's/^commit: //p')"
  if [ -n "$running" ] && [ "$running" != unknown ]; then
    git fetch -q origin master
    if git merge-base --is-ancestor "$sha" "$running" 2>/dev/null; then
      echo "the VM runs $running, which contains $sha; waiting 5 min for the edge restart"
      sleep 300
      vm "echo $PW | sudo -S -p '' docker ps --format '{{.Names}}\t{{.Status}}'"
      exit 0
    fi
  fi
  sleep 60
done
echo "timeout: the VM still runs ${running:-an unreadable commit}" >&2
exit 1
```

`$W/e2e-local.sh`:

```bash
#!/usr/bin/env bash
# Builds the aura image from the working tree, runs it on the local stack and runs Playwright against
# it (http://127.0.0.1:9080) with the local .env account. e2e-local.sh <label> <playwright args...>.
# AURA_PULL_POLICY=never for the up only: .env says always, and an up would pull the registry image
# over the one just built. Needs docker reachable from WSL. Output, fade-levels.json and screenshots
# as e2e-vm.sh, under C:\Users\Davide\AppData\Local\Temp\plan-a-e2e\<label>.
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
label="${1:?label, e.g. local-green}"; shift
OUT="/mnt/c/Users/Davide/AppData/Local/Temp/plan-a-e2e/$label"
if ! docker version > /dev/null 2>&1; then echo "STOP: docker is not reachable from WSL" >&2; exit 2; fi
cd /mnt/d/Aura || exit 1
docker compose build aura || exit 1
AURA_PULL_POLICY=never docker compose up -d aura || exit 1
healthy=no
for _ in $(seq 1 60); do
  if curl -fsS -o /dev/null http://127.0.0.1:9080/healthz; then healthy=yes; break; fi
  sleep 5
done
if [ "$healthy" = no ]; then echo "STOP: :9080/healthz never answered" >&2; exit 1; fi
rm -rf "$OUT"
cd /mnt/d/Aura/web || exit 1
AURA_E2E_ORIGIN=http://127.0.0.1:9080 ./node_modules/.bin/playwright test --output "$OUT" "$@"
rc=$?
echo "== results in $OUT"
find "$OUT" -name fade-levels.json -exec sh -c 'echo "$1"; cat "$1"; echo' _ {} \;
find "$OUT" -name '*.png' -exec echo "screenshot: {}" \;
exit "$rc"
```

`$W/e2e-vm.sh`:

```bash
#!/usr/bin/env bash
# Playwright against the lab VM as the operator's own cockpit account, VM_COCKPIT_EMAIL and
# VM_COCKPIT_PASSWORD from D:\Aura\.env.google (never printed). e2e-vm.sh <label> <playwright args...>
#
# web/e2e/auth.ts reads ../.env BEFORE the environment, and the repository's .env holds the local
# stack's E2E account. So Playwright runs from a directory whose parent has no .env, with the
# config and the specs read from web/. Its output goes to C:\Users\Davide\AppData\Local\Temp\
# plan-a-e2e\<label>, outside web/test-results (other sessions' runs wipe it) and readable from
# Windows; the run then prints every fade-levels.json and names every screenshot.
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
label="${1:?label, e.g. vm-red}"; shift
env_value() { grep -m1 "^$1=" /mnt/d/Aura/.env.google | cut -d= -f2- | tr -d '\r' | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
RUN="$HOME/plan-a-e2e/run"
OUT="/mnt/c/Users/Davide/AppData/Local/Temp/plan-a-e2e/$label"
mkdir -p "$RUN"
rm -rf "$OUT"
cd "$RUN" || exit 1
if [ -e "$HOME/plan-a-e2e/.env" ]; then echo "refusing: $HOME/plan-a-e2e/.env would override the account" >&2; exit 2; fi
AURA_E2E_ORIGIN=https://192.168.101.158 \
AURA_E2E_AUTHULA_EMAIL="$(env_value VM_COCKPIT_EMAIL)" \
AURA_E2E_AUTHULA_PASSWORD="$(env_value VM_COCKPIT_PASSWORD)" \
  /mnt/d/Aura/web/node_modules/.bin/playwright test --config /mnt/d/Aura/web/playwright.config.ts \
  --output "$OUT" "$@"
rc=$?
echo "== results in $OUT"
find "$OUT" -name fade-levels.json -exec sh -c 'echo "$1"; cat "$1"; echo' _ {} \;
find "$OUT" -name '*.png' -exec echo "screenshot: {}" \;
exit "$rc"
```

`$W/vm-lib.sh`:

```bash
# shellcheck shell=bash
# vm-lib.sh -- sourced by Plan A's lab-VM scripts: ssh, read-only SQL, the ingest's cycle and the
# ingest's own index witness. Nothing here writes to the VM.
VM=aura@192.168.101.158
# The ssh password never enters git: the operator keeps it in vm-ssh-password beside these helpers.
PW="$(cat "$(dirname "${BASH_SOURCE[0]}")/vm-ssh-password")" || return 1

vm() { sshpass -p "$PW" ssh -o StrictHostKeyChecking=no "$VM" "$@"; }

# vm_sql <file>: a SELECT-only file in aura-postgres, unaligned, '|' between columns.
vm_sql() {
  sshpass -p "$PW" scp -o StrictHostKeyChecking=no "$1" "$VM:/tmp/plan-a.sql"
  vm "echo $PW | sudo -S -p '' docker cp /tmp/plan-a.sql aura-postgres:/tmp/plan-a.sql && echo $PW | sudo -S -p '' docker exec aura-postgres sh -c 'psql -At -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -f /tmp/plan-a.sql; rm -f /tmp/plan-a.sql'; rm -f /tmp/plan-a.sql"
}

# ingest_interval: AURA_INGEST_INTERVAL_SEC as the running aura-ingest has it. compose.yaml
# defaults it to 60 and the VM's .env may say otherwise; empty means it could not be read.
ingest_interval() {
  vm "echo $PW | sudo -S -p '' docker inspect aura-ingest --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -E '^AURA_INGEST_INTERVAL_SEC=' | cut -d= -f2"
}

# The reader indexed_keys runs inside aura-ingest: services/ingest/arcade.py indexed_source_keys,
# the IndexedDocument row app.py writes only after a document's passages are stored.
INDEXED_KEYS_PY='import os
from ingest import arcade, identity
for key in sorted(arcade.indexed_source_keys(
        os.environ.get("ARCADE_HTTP", "http://arcadedb:2480"),
        identity.database_for(os.environ["PROBE_IDENTITY"]),
        ("root", os.environ["ARCADEDB_PASSWORD"]), 60.0)):
    print(key)'

# indexed_keys <identity id>: every object key with an IndexedDocument row in that identity's
# database, one per line.
indexed_keys() {
  case "$1" in '' | *[!0-9a-f-]*) echo "not an identity id: '$1'" >&2; return 2 ;; esac
  local py
  py="$(printf '%s' "$INDEXED_KEYS_PY" | base64 -w0)"
  vm "echo $PW | sudo -S -p '' docker exec -e PYTHONPATH=/app -e PROBE_IDENTITY=$1 aura-ingest python -c \"import base64; exec(base64.b64decode('$py'))\""
}
```

`$W/vm-sql.sh`:

```bash
#!/usr/bin/env bash
# READ-ONLY. A SELECT-only .sql file on the lab VM's Postgres, printed as psql's aligned table.
set -euo pipefail
. "$(dirname "$0")/vm-lib.sh"
sshpass -p "$PW" scp -o StrictHostKeyChecking=no "${1:?sql file}" "$VM:/tmp/plan-a.sql"
vm "echo $PW | sudo -S -p '' docker cp /tmp/plan-a.sql aura-postgres:/tmp/plan-a.sql && echo $PW | sudo -S -p '' docker exec aura-postgres sh -c 'psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -f /tmp/plan-a.sql; rm -f /tmp/plan-a.sql'; rm -f /tmp/plan-a.sql"
```

`$W/vm-json-index.sh`:

```bash
#!/usr/bin/env bash
# READ-ONLY. The Studio projects saved before the suffix, as the lab VM holds them.
#   vm-json-index.sh <out-file>      every re-key candidate by the row rule, with whether the index
#                                    holds its key; the same lines are written to <out-file>
#   vm-json-index.sh --after <file>  for every candidate an earlier run wrote: its key now, whether
#                                    the index still holds the old key, and what the daemon logged
set -euo pipefail
. "$(dirname "$0")/vm-lib.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
INTERVAL="$(ingest_interval)"
echo "ingest interval: ${INTERVAL:-unreadable} s"

if [ "${1:-}" != "--after" ]; then
  OUT="${1:?out file}"
  # internal/assets/studio_project_rekey.go savedLikeAStudioProject as SQL, over ListRecent's
  # statuses. The bytes check is the daemon's to make; this counts the rows it will read.
  cat > "$WORK/candidates.sql" <<'SQL'
SELECT identity_id, id, object_key, file_name, size_bytes FROM aura.assets
WHERE source_kind = 'web' AND thread_id = '' AND scope = 'thread'
  AND modality = 'document' AND mime_type = 'application/json'
  AND status IN ('accepted', 'processing', 'searchable', 'embedding', 'complete')
  AND deleted_at IS NULL
  AND file_name ~ '^[A-Za-z0-9-]{1,60}\.json$'
  AND object_key ~ '^chat/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.json$'
ORDER BY identity_id, created_at DESC;
SQL
  vm_sql "$WORK/candidates.sql" > "$WORK/candidates"
  : > "$OUT"
  # shellcheck disable=SC2013 # uuids split safely, and a while-read loop would lend its stdin to ssh
  for identity in $(cut -d'|' -f1 "$WORK/candidates" | sort -u); do
    indexed_keys "$identity" > "$WORK/indexed"
    echo "identity $identity: $(wc -l < "$WORK/indexed") keys indexed"
    grep "^$identity|" "$WORK/candidates" | while IFS='|' read -r _ id key name size; do
      held=no
      if grep -qxF "$key" "$WORK/indexed"; then held=yes; fi
      echo "$identity|$id|$key|$held" >> "$OUT"
      echo "  candidate $id  key $key  name $name  $size B  indexed: $held"
    done
  done
  echo "candidates: $(wc -l < "$OUT"), indexed: $(grep -c '|yes$' "$OUT" || true)"
  exit 0
fi

BEFORE="${2:?the file an earlier run wrote}"
echo "== what the daemon logged (the raw log file: docker logs can stop at a truncated line)"
vm "echo $PW | sudo -S -p '' sh -c 'grep -a -E \"studio project re-key|re-keyed a Studio project|left a JSON document\" \"\$(docker inspect -f {{.LogPath}} aura)\" | tail -40'"
if [ ! -s "$BEFORE" ]; then echo "no candidates in $BEFORE: nothing to follow"; exit 0; fi
echo "SELECT id, object_key FROM aura.assets WHERE id IN ($(cut -d'|' -f2 "$BEFORE" | sed "s/.*/'&'/" | paste -sd,));" > "$WORK/now.sql"
vm_sql "$WORK/now.sql" > "$WORK/now"
# shellcheck disable=SC2013 # uuids split safely, and a while-read loop would lend its stdin to ssh
for identity in $(cut -d'|' -f1 "$BEFORE" | sort -u); do
  indexed_keys "$identity" > "$WORK/indexed"
  grep "^$identity|" "$BEFORE" | while IFS='|' read -r _ id old was; do
    now="$(grep "^$id|" "$WORK/now" | cut -d'|' -f2 || true)"
    held=no
    if grep -qxF "$old" "$WORK/indexed"; then held=yes; fi
    new=no
    if [ -n "$now" ] && grep -qxF "$now" "$WORK/indexed"; then new=yes; fi
    echo "  $id  $old (indexed before: $was, now: $held) -> ${now:-row gone} (indexed: $new)"
  done
done
```

`$W/vm-project-probe.sh`:

```bash
#!/usr/bin/env bash
# Plan A, Task 9: save a Studio project and a plain .json on the lab VM through the cockpit's own
# upload door (presign, PUT, finalize, as projectStore.saveProject does), wait two ingest cycles,
# read which of the two keys the index holds (read-only), and delete both on every exit.
# The credentials come from .env.google and are never printed.
set -euo pipefail
shopt -s inherit_errexit
export PATH="$HOME/.local/bin:$PATH"
. "$(dirname "$0")/vm-lib.sh"
BASE=https://192.168.101.158
WORK="$(mktemp -d)"
# Every asset id a presign answered, written the moment it is known: a save that fails later, in
# its own subshell, still leaves its id here for the trap to delete.
CREATED="$WORK/created"
: > "$CREATED"
SIGNED_IN=no
cleanup() {
  if [ "$SIGNED_IN" = yes ]; then
    while read -r id; do
      api -X DELETE "$BASE/api/assets/$id" -o /dev/null -w "delete $id %{http_code}\n" || true
    done < "$CREATED"
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

INTERVAL="$(ingest_interval)"
if [ -z "$INTERVAL" ]; then echo "FAIL: AURA_INGEST_INTERVAL_SEC unreadable on aura-ingest" >&2; exit 1; fi
# Two whole cycles after the save, plus slack for the one already running.
WAIT="$(awk -v s="$INTERVAL" 'BEGIN { printf "%d", 2 * s + 30 }')"

curl() { command curl -k "$@"; }
. /mnt/d/Aura/scripts/musr_live_run_authula_helpers.sh
env_value() { grep -m1 "^$1=" /mnt/d/Aura/.env.google | cut -d= -f2- | tr -d '\r' | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
JAR="$WORK/jar"; : > "$JAR"
read -r BASE_PATH CSRF_HEADER CSRF_COOKIE CSRF_TOKEN < <(fetch_auth_config "$JAR")
BODY="$(sign_in "$JAR" "$(env_value VM_COCKPIT_EMAIL)" "$(env_value VM_COCKPIT_PASSWORD)" "$BASE_PATH" "$CSRF_HEADER" "$CSRF_COOKIE" "$CSRF_TOKEN")"
if grep -q totp_redirect "$BODY"; then echo "FAIL: account needs TOTP" >&2; exit 1; fi
api() { curl -sS -f -b "$JAR" -c "$JAR" -H "Idempotency-Key: $(cat /proc/sys/kernel/random/uuid)" -H "Origin: $BASE" -H "$CSRF_HEADER: $CSRF_TOKEN" -H 'Accept: application/json' "$@"; }
SIGNED_IN=yes
json() { python3 -c 'import json,sys; v=json.load(open(sys.argv[1]))
for k in sys.argv[2:]: v=v[k]
print(v)' "$@"; }

# Saves the bytes $2 as a thread-less JSON document named $1 and prints its asset id.
save() {
  local name="$1"
  printf '%s' "$2" > "$WORK/$name"
  api -X POST "$BASE/api/assets/presign" -H 'Content-Type: application/json' \
    -d "{\"thread_id\":\"\",\"file_name\":\"$name\",\"mime_type\":\"application/json\",\"size_bytes\":$(stat -c %s "$WORK/$name"),\"modality_hint\":\"document\"}" \
    > "$WORK/presign.json"
  local id url
  id=$(json "$WORK/presign.json" asset id)
  echo "$id" >> "$CREATED"
  url=$(json "$WORK/presign.json" upload upload_url)
  case "$url" in /*) url="$BASE$url" ;; esac
  local headers=()
  while IFS= read -r header; do headers+=(-H "$header"); done < <(python3 -c 'import json,sys
for k, v in (json.load(open(sys.argv[1]))["upload"].get("required_headers") or {}).items(): print(f"{k}: {v}")' "$WORK/presign.json")
  curl -sS -f -o /dev/null -w "put $name %{http_code}\n" -X PUT "${headers[@]}" --data-binary @"$WORK/$name" "$url" >&2
  api -X POST "$BASE/api/assets/$id/finalize" -o /dev/null -w "finalize $name %{http_code}\n" >&2
  echo "$id"
}

PROJECT="$(save plan-a-probe.aura-video.json \
  "{\"id\":\"$(cat /proc/sys/kernel/random/uuid)\",\"name\":\"plan-a probe\",\"size\":{\"width\":320,\"height\":180},\"fps\":30,\"sources\":[],\"video\":[],\"overlays\":[]}")"
# The control is an operator's own JSON, not a project: the ingest must index it, and the boot
# re-key must never take it for a project.
CONTROL="$(save plan-a-control.json '{"listino":[{"articolo":"plan-a control","prezzo":1}]}')"
if [ -z "$PROJECT" ] || [ -z "$CONTROL" ]; then echo "FAIL: a save returned no asset id" >&2; exit 1; fi
IDENTITY=$(json "$WORK/presign.json" asset identity_id)
echo "saved: project $PROJECT, control $CONTROL; ingest interval $INTERVAL s, waiting $WAIT s"
sleep "$WAIT"

echo "SELECT id, object_key FROM aura.assets WHERE id IN ('$PROJECT', '$CONTROL');" > "$WORK/rows.sql"
vm_sql "$WORK/rows.sql" > "$WORK/rows"
PROJECT_KEY="$(grep "^$PROJECT|" "$WORK/rows" | cut -d'|' -f2)"
CONTROL_KEY="$(grep "^$CONTROL|" "$WORK/rows" | cut -d'|' -f2)"
indexed_keys "$IDENTITY" > "$WORK/indexed"
in_index() { if grep -qxF "$1" "$WORK/indexed"; then echo yes; else echo no; fi; }
echo "project $PROJECT_KEY indexed: $(in_index "$PROJECT_KEY")"
echo "control $CONTROL_KEY indexed: $(in_index "$CONTROL_KEY")"
if [ "$(in_index "$CONTROL_KEY")" = no ]; then
  echo "INCONCLUSIVE: the control is not indexed yet, so the project's absence proves nothing; run again" >&2
  exit 3
fi
if [ "$(in_index "$PROJECT_KEY")" = yes ]; then
  echo "FAIL: the ingest indexed the Studio project" >&2
  exit 1
fi
echo "PASS: the ingest indexed the control and skipped the project"
```

---

### Task 0: The PRD records what was measured, and the rules, first

CLAUDE.md "PRD-first": measure, then amend, then implement. The two export bugs are measured (FINDINGS.md S1.3 and S1.4, on the lab VM). The key shape and the old projects in the index are measured here, read-only. This task writes only what those measurements show, plus the rules the next tasks implement, phrased as rules: "the export is to…". Every sentence that says how things are **after** the fix waits for Task 9, which measures it.

**Files:**
- Create (outside the repo): `$W/plan-a-json-keys.sql`, and the snapshot `$W/rekey-before.txt`
- Modify: `prd.md`:
  - a paragraph inserted after `:688`;
  - the M1 sentence at `:714-715`, shortened;
  - a paragraph inserted after `:726` (`Details: \`docs/superpowers/specs/2026-09-27-video-studio-audio-design.md\`.`).
- Modify: `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md`: dated notes after `:160` and `:272`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - two PRD paragraphs, "VideoFlow opacity and media" and "Saved Studio projects and the document index", which Task 9 completes;
  - `$W/rekey-before.txt`, one line per re-key candidate (`identity|asset id|key|indexed yes/no`), which Task 8 refreshes and Task 9 compares against.

- [ ] **Step 1: Measure the saved JSON assets' keys on the lab VM (read-only)**

Write `$W/plan-a-json-keys.sql`:

```sql
\echo '== live JSON assets, newest first (read-only)'
SELECT id, identity_id, created_at::timestamp(0) AS created, status, source_kind, scope,
       thread_id <> '' AS in_thread, object_key, file_name, document_id <> '' AS named_document,
       position(id::text IN object_key) > 0 AS key_holds_row_id
FROM aura.assets
WHERE mime_type = 'application/json' AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 15;
\echo '== keys by ending'
SELECT CASE WHEN object_key LIKE '%.aura-video.json' THEN '.aura-video.json'
            WHEN object_key LIKE '%.json' THEN '.json' ELSE 'other' END AS ending,
       count(*) AS assets,
       count(*) FILTER (WHERE document_id <> '') AS named_documents,
       count(*) FILTER (WHERE position(id::text IN object_key) > 0) AS keys_holding_row_id,
       count(*) FILTER (WHERE object_key !~ '^chat/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.json$') AS other_shapes
FROM aura.assets
WHERE mime_type = 'application/json' AND deleted_at IS NULL
GROUP BY 1;
\echo '== rows the Studio project re-key would read (internal/assets/studio_project_rekey.go, row rule only)'
SELECT id, identity_id, created_at::timestamp(0) AS created, object_key, file_name, size_bytes
FROM aura.assets
WHERE source_kind = 'web' AND thread_id = '' AND scope = 'thread'
  AND modality = 'document' AND mime_type = 'application/json'
  AND status IN ('accepted', 'processing', 'searchable', 'embedding', 'complete')
  AND deleted_at IS NULL
  AND file_name ~ '^[A-Za-z0-9-]{1,60}\.json$'
  AND object_key ~ '^chat/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.json$'
ORDER BY identity_id, created_at DESC;
```

The three statements parse under Postgres's own parser (pglast, checked in scratch). They have not been run against the VM's schema, and that is this step's job.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vm-sql.sh $W/plan-a-json-keys.sql`

Read and keep, for Step 3:
- the `.json` row of "keys by ending":
  - `assets` (⟨n⟩);
  - `named_documents` (⟨m⟩);
  - `keys_holding_row_id` (⟨k⟩);
  - `other_shapes` (⟨o⟩). These are keys the file manager named, from its own upload or a rename.
- the number of rows in the last list (⟨c⟩), the candidates by the row rule.

Expected: ⟨k⟩ is 0. The uuid in `chat/<uuid>.json` is the one Presign minted for the object, and the row's `id` is the database's `gen_random_uuid()`, so the two never coincide. If ⟨k⟩ is not 0, stop and report it: the measurement wins over this plan (CLAUDE.md), and the contract line "never read an asset id out of a key" would need rethinking.

- [ ] **Step 2: Count the old projects the index holds (read-only)**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vm-json-index.sh $W/rekey-before.txt`

The script runs the same row rule as SQL. For each identity that has a candidate, it reads the keys that identity's ArcadeDB database holds `IndexedDocument` rows for, with the ingest's own reader (`services/ingest/arcade.py` `indexed_source_keys`) inside `aura-ingest`. Nothing is written to the VM. Expected output:
- `ingest interval: <s> s`;
- one `identity <id>: <N> keys indexed` line per identity;
- one `candidate … indexed: yes|no` line per candidate;
- `candidates: ⟨c⟩, indexed: ⟨i⟩`.

Keep ⟨i⟩ for Step 3, and keep `$W/rekey-before.txt` for Task 9.

The row rule cannot read bytes, so a candidate here is not yet a project: the daemon's content check makes that call at boot. A candidate the content check refuses stays where it is, by design.

- [ ] **Step 3: Record the opacity and media findings**

In `prd.md`, after the paragraph that ends `volume curve into \`animations\`, beginning at \`sourceStart\`.` (`:688`), insert one blank line and then:

```markdown
**VideoFlow opacity and media (aura-video-mcp spikes S1.3 and S1.4, 2026-09-30).** Measured on the
lab VM: a clip's fade-out frame read RGB (233, 100, 90) where opacity 0.4 over black allows 102
(S1.3), and a source behind a refused fetch came out as a black, silent 26,604 B MP4 that the export
reported as a success, with every source fetched twice, once by VideoFlow's media cache and once by
the pre-decode (S1.4). Read in VideoFlow 1.3.4's source: handed a keyframe array as a layer's
initial property, `compile()` stores the array as the value of one step keyframe and writes it out
as a static property (core `dist/VideoFlow.js:629-638`, `:856-864`); the renderer unit-converts each
keyframe object in it to the number NaN (renderer-browser `dist/layers/RuntimeBaseLayer.js:311-323`,
`:472-494`), and a NaN opacity draws as full opacity (`dist/LayerRasterizer.js:909`). VideoFlow
disables a layer whose media fails to load, logs a warning and lets the export resolve
(`dist/BrowserRenderer.js:427-445`). The rules: the compile is to move every keyframed property into
the layer's `animations`, on its source clock, as volume already is; a clip's fade is to last half a
second of film at any speed; the export is to load each source once, through VideoFlow's
`loadedMedia` cache, and to write no file when a layer was disabled or a sound that is not muted
will not decode, naming the source by its file name and saying whether its bytes never arrived or
did not play.

This does not establish:
- that title fades and the fade-to-black and fade-to-white washes are lost on an exported frame:
  they take the same path, but that was computed on the renderer's own runtime layers in a unit
  test, not measured on a frame;
- the HTTP status of a failed fetch: VideoFlow keeps it in its console warning and the page never
  sees it;
- what an expired presigned URL or a 403 does: S1.4 exercised only a missing CORS rule;
- the Stage preview on screen: it compiles the same JSON and builds the same runtime layers
  (`@videoflow/renderer-dom` imports them from renderer-browser), but only the export was measured;
- anything about a video whose own audio will not decode: the mixer drops that audio, so such a
  clip is to export silent, with no error.
```

- [ ] **Step 4: Record the rule for saved projects**

In the "Uploads and saved projects" paragraph, replace exactly this text (`:714-715`):

```markdown
(M1): that is
reported as its own issue and is not fixed by this work.
```

with:

```markdown
(M1): that was
reported as its own issue; its rule follows this section's details.
```

Then, after the line `Details: \`docs/superpowers/specs/2026-09-27-video-studio-audio-design.md\`.` (`:726`), insert one blank line and the paragraph below. It gets a paragraph of its own: the audio lane's "This does not establish" list above belongs to the audio lane, and M1 needs its own list. Fill every `⟨…⟩` from Steps 1 and 2.

```markdown
**Saved Studio projects and the document index (M1; aura-video-mcp Plan A).** Measured ⟨date⟩ on
the lab VM, read-only: ⟨n⟩ live JSON assets with a `.json` key, ⟨m⟩ of them named as documents,
⟨o⟩ at keys the file manager named; every other key is `chat/<uuid>.json`, with no file name in it,
and in ⟨k⟩ of them the uuid is the row's own id: it is the one Presign minted for the object. ⟨c⟩
rows look like Studio saves by the row rule below, and the index holds the keys of ⟨i⟩ of them
(`IndexedDocument` rows, read with the ingest's own `arcade.indexed_source_keys`). The ingest's
matcher reads only the object key (CocoIndex 1.0.24 `connectors/amazon_s3/_source.py:304-317`), and
its audit lists keys without reading a single object's metadata (`services/ingest/source.py`
`expected_keys`), so a project can be told apart by its key and by nothing else. The rules:
- the Studio is to name a project `<slug>.aura-video.json`, and the object key is to keep that
  suffix whole (`objectstore.StudioProjectSuffix`);
- the ingest is to exclude `**/*.aura-video.json` from its walk and from its audit, and to keep
  indexing every other `.json`;
- a project saved before the change is to be moved once, at boot, to `chat/<uuid>.aura-video.json`
  through the file manager's own transfer, keeping its row and its id. Only a row that is a cockpit
  upload with no thread and thread scope, a JSON document named `<slug>.json` at `chat/<uuid>.json`
  in the file manager's bucket, at most 4 MiB, whose bytes have the Studio's typed project shape,
  is moved. Saving again is no remedy: every save is a new asset;
- no code is to read an asset id out of an object key.

This does not establish:
- that the boot pass moves exactly the Studio saves, and that the index drops them, on a real
  stack: both are measured after the fix;
- anything about a project an operator renamed in the file manager before the change: Rename
  writes the typed name into the key (`internal/assets/filemanager_ops.go` `Rename`), so its key
  fails the `chat/<uuid>.json` test, and it stays indexed until it is deleted or renamed to end in
  `.aura-video.json`;
- that the asset stops receiving a `document_id`: `DocumentProcessor` names one for every document,
  and the knowledge catalog asks ArcadeDB whether a document is indexed
  (`internal/assets/document_processor.go:11-33`).
```

- [ ] **Step 5: Date the spec sentences the measurements overturned**

The spec stays the vision; a sentence a measurement overturned gets a dated note beside it rather than a silent rewrite. In `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md`:

1. After `:160`, the line `  saved again.` that ends the "Project indexing (M1 of the audio spec)" bullet, add:

```markdown
  *Superseded 2026-10-01 (Plan A; recorded in prd.md §12 "Saved Studio projects and the document
  index"):* the ingest cannot skip a project by its name, because its matcher and its audit see only
  the object key, so the key keeps the suffix whole; and a project saved before the change is not
  fixed by saving it again, because every save is a new asset: the daemon moves it once, at boot.
```

2. After `:272`, the line `   silence. The cockpit's export dialog shows the reason.` that ends fix 1, add:

```markdown
   *Superseded 2026-10-01 (Plan A; recorded in prd.md §12 "VideoFlow opacity and media"):* the page
   never learns the HTTP status of a failed fetch, so the dialog names the source by its file name
   and says whether its bytes never arrived or did not play. The pre-decode no longer fetches at
   all: it decodes the bytes VideoFlow's cache already holds.
```

- [ ] **Step 6: Commit the PRD and the spec notes**

Write `$W/msg-t0.txt`:

```text
docs(prd): record why Studio fades and lost sources never reached the export

The aura-video-mcp spikes measured two Studio export bugs on the lab VM
(FINDINGS S1.3, S1.4): a clip's fade-out exported at full opacity, and
a source the page could not fetch became a black, silent MP4 reported
as a success, with every source fetched twice. This records the cause
read in VideoFlow 1.3.4's source and the rules the next commits
implement. Title and wash fades take the same path, but only a unit
test shows it, and the record says so.

It also records, measured read-only on the lab VM, how saved projects
sit in the store and the index: every key is chat/<uuid>.json, its uuid
is not the asset's id, and the ingest sees nothing but that key. The
rules follow: the Studio suffix .aura-video.json must survive into the
key, and projects saved before the change are moved once at boot, by a
rule that no other JSON passes. The spec gets dated notes beside the
two sentences these measurements overturned.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t0.txt prd.md docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md`

Expected: `COMMIT_RC=0` and the new subject on `git log --oneline -1`.

---

### Task 1: Fades reach the renderer

**Files:**
- Create: `web/src/videoStudio/videoflow_keyframes.ts`
- Create: `web/src/videoStudio/__tests__/videoflow_keyframes.test.ts`
- Modify: `web/src/videoStudio/videoflow.ts`:
  - `:1-5`, the header;
  - `:15-26`, the imports;
  - `:58-78`, `clipOpacity`;
  - `:193-195`, the opacity comment;
  - `:288`, the compile.
- Modify: `web/stryker.config.json:77`, `web/vitest.stryker.config.ts:84-85`

**Interfaces:**
- Consumes: `clipTimelineDuration(clip: VideoItem): number` (`web/src/videoStudio/project.ts:151`), which already exists.
- Produces: `export function withKeyframes(json: VideoJSON): VideoJSON` in `videoflow_keyframes.ts`. `toVideoJSON(project, urls)` keeps its signature, and its output now carries every fade in `layers[i].animations`, never in `layers[i].properties`.

- [ ] **Step 1: Write the failing test**

Create `web/src/videoStudio/__tests__/videoflow_keyframes.test.ts`:

```ts
import type { VideoJSON } from '@videoflow/core';
import {
  createBuiltinLayerTypeRegistry,
  type ILayerRenderer,
  type RuntimeBaseLayer,
} from '@videoflow/renderer-browser';
import { describe, expect, it } from 'vitest';
import type { VideoItem, VideoProject } from '../project';
import { toVideoJSON } from '../videoflow';

// The compile against the REAL VideoFlow, and every animated layer read back the way its renderer
// reads it before drawing: `getPropertiesAtFrame`, then the transitions — `renderFrame`'s own two
// calls (renderer-browser dist/layers/RuntimeBaseLayer.js). videoflow.test.ts mocks the builder,
// so it proves what we HAND VideoFlow; a fade handed over in a shape VideoFlow does not read passed
// there for as long as it existed (spikes/video-mcp-render/FINDINGS.md S1.3).

const urls = { assetUrl: (id: string) => `/api/assets/${id}/download` };

function film(video: VideoItem[], overlays: VideoProject['overlays'] = []): VideoProject {
  return {
    id: 'p',
    name: 'fades',
    size: { width: 320, height: 180 },
    fps: 30,
    sources: [
      { id: 'src', assetId: 'a', kind: 'video', duration: 60, size: { width: 320, height: 180 } },
    ],
    video,
    overlays,
  };
}

/** A layer of the compiled film as the renderer builds it, from VideoFlow's own registry. */
function runtimeLayer(json: VideoJSON, name: string): RuntimeBaseLayer {
  const layer = json.layers.find((candidate) => candidate.settings.name === name);
  if (layer === undefined) throw new Error(`no layer named ${name}`);
  const registry = createBuiltinLayerTypeRegistry('videoflow_keyframes.test');
  const renderer: ILayerRenderer = {
    layers: [],
    getPropertyDefinition: (type) => registry.getPropertyDefinition(type),
    loadFont: () => Promise.resolve(),
    createRuntimeLayer: (json_) =>
      registry.createRuntimeLayer(json_, json.fps, json.width, json.height, renderer),
  };
  return renderer.createRuntimeLayer(layer);
}

/** The opacity the renderer draws a layer at, `seconds` into the film. */
function opacityAt(json: VideoJSON, name: string, seconds: number): number {
  const layer = runtimeLayer(json, name);
  const frame = Math.round(seconds * json.fps);
  const props = layer.applyTransitions(frame, layer.getPropertiesAtFrame(frame));
  return Number(props.opacity);
}

describe('fades in the compiled film', () => {
  it('fades a clip out over its last half second, from wherever it starts in its source', async () => {
    // The spike's shape: a 21 s clip that crossfades in from the one before, starts 30 s into its
    // source, and fades out. It starts at 20 s, so at 40.8 s it is 0.3 s into its 0.5 s fade.
    const json = await toVideoJSON(
      film([
        { id: 'clip-b', sourceId: 'src', duration: 21, sourceStart: 0, muted: true },
        {
          id: 'clip-c',
          sourceId: 'src',
          duration: 21,
          sourceStart: 30,
          muted: true,
          fadeOut: true,
          junctionFromClipId: 'clip-b',
          junctionTransition: 'crossfade',
          junctionDuration: 1,
        },
      ]),
      urls,
    );

    expect(opacityAt(json, 'clip-c', 40.8)).toBeCloseTo(0.4, 5);
    expect(opacityAt(json, 'clip-c', 40.4)).toBeCloseTo(1, 5);
    // The crossfade into it still works: halfway through the junction it is half there.
    expect(opacityAt(json, 'clip-c', 20.5)).toBeCloseTo(0.5, 5);
  });

  it('fades a sped-up clip over half a second of film, not of source', async () => {
    // 4 s of source at 2× is 2 s on screen: the fade-out runs from 1.5 s to 2 s of film.
    const json = await toVideoJSON(
      film([
        {
          id: 'fast',
          sourceId: 'src',
          duration: 4,
          sourceStart: 1,
          muted: true,
          speed: 2,
          fadeOut: true,
        },
      ]),
      urls,
    );

    expect(opacityAt(json, 'fast', 1.8)).toBeCloseTo(0.4, 5);
    expect(opacityAt(json, 'fast', 1.2)).toBeCloseTo(1, 5);
  });

  it('fades a title in the way its inspector wrote it', async () => {
    const json = await toVideoJSON(
      film(
        [{ id: 'clip-1', sourceId: 'src', duration: 4, sourceStart: 0, muted: true }],
        [
          {
            id: 'lane-1',
            items: [
              {
                id: 'title',
                kind: 'text',
                anchor: { clipId: 'clip-1', offset: 1 },
                duration: 2,
                // Inspector.tsx `fadeFor('fadeIn', 2)`: the window's own clock, 0 at its start.
                props: {
                  text: 'AURA',
                  opacity: [
                    { time: 0, value: 0 },
                    { time: 0.5, value: 1 },
                  ],
                },
              },
            ],
          },
        ],
      ),
      urls,
    );

    // 0.2 s into its 0.5 s fade-in; every instant here is a whole frame at 30 fps.
    expect(opacityAt(json, 'title', 1.2)).toBeCloseTo(0.4, 5);
    expect(opacityAt(json, 'title', 2)).toBeCloseTo(1, 5);
  });

  it('dips to black through a fade-to-black junction', async () => {
    const json = await toVideoJSON(
      film([
        { id: 'clip-1', sourceId: 'src', duration: 4, sourceStart: 0, muted: true },
        {
          id: 'clip-2',
          sourceId: 'src',
          duration: 4,
          sourceStart: 4,
          muted: true,
          junctionFromClipId: 'clip-1',
          junctionTransition: 'fadeBlack',
          junctionDuration: 1,
        },
      ]),
      urls,
    );

    // The junction runs 3 s to 4 s; the wash rises for half of it and peaks in the middle.
    expect(opacityAt(json, 'junction-clip-2', 3.2)).toBeCloseTo(0.4, 5);
    expect(opacityAt(json, 'junction-clip-2', 3.5)).toBeCloseTo(1, 5);
  });

  it('leaves no keyframes in a layer’s static properties, where the renderer cannot read them', async () => {
    const json = await toVideoJSON(
      film([
        {
          id: 'clip-1',
          sourceId: 'src',
          duration: 4,
          sourceStart: 2,
          muted: true,
          fadeIn: true,
          fadeOut: true,
          flipX: true,
        },
      ]),
      urls,
    );
    const [clip] = json.layers;

    expect(clip?.properties).not.toHaveProperty('opacity');
    // A vector is an array too, and stays a value.
    expect(clip?.properties.scale).toEqual([-1, 1]);
    expect(clip?.animations.find((animation) => animation.property === 'opacity')).toEqual({
      property: 'opacity',
      keyframes: [
        { time: expect.closeTo(2.0001, 9) as number, value: 0 },
        { time: expect.closeTo(2.5001, 9) as number, value: 1 },
        { time: expect.closeTo(5.5001, 9) as number, value: 1 },
        { time: expect.closeTo(6.0001, 9) as number, value: 0 },
      ],
    });
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

The web tools run from WSL on the shared `web/node_modules`, which holds the Linux bindings beside the Windows ones (`@rolldown/binding-linux-x64-gnu`, `@oxlint/binding-linux-x64-gnu`, `@typescript/typescript-linux-x64`, checked 2026-10-01). If a tool stops on a missing `linux-x64` binding, stop and ask the operator: `npm install` and `npm ci` are not yours to run.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/videoflow_keyframes.test.ts`

Expected: `Tests  5 failed (5)`.
- The first four fail with `AssertionError: expected NaN to be close to 0.4, received difference is NaN, but expected 0.000005`. That NaN is the renderer's own reading of today's compiled opacity.
- The fifth fails with `AssertionError: expected { fit: 'cover', mute: true, …(8) } to not have property "opacity"`.

- [ ] **Step 3: Write the module that moves keyframes where the renderer reads them**

Create `web/src/videoStudio/videoflow_keyframes.ts`:

```ts
// videoflow_keyframes.ts — every property the Studio animates, moved to where VideoFlow's renderer
// reads keyframes: the compiled layer's `animations`, on the layer's source clock.
//
// The builder takes a layer's initial properties as values. Handed a keyframe array, `compile()`
// stores the whole array as the value of ONE step keyframe (core dist/VideoFlow.js:629-638) and
// writes a property with one keyframe out as a static value (:856-864). The renderer then reads
// that array as a vector and unit-converts each keyframe object to the number NaN — `opacity`
// declares no unit, so `parseFloat('[object Object]')` is what survives (renderer-browser
// dist/layers/RuntimeBaseLayer.js:311-323, 472-494) — and a NaN opacity draws as full opacity:
// the canvas ignores a NaN `globalAlpha` (LayerRasterizer.js:909), and CSS rejects the
// `opacity: NaN NaN` the DOM path writes (RuntimeBaseLayer.js:770). A clip's fade-out was
// measured that way on an exported frame (spikes/video-mcp-render/FINDINGS.md S1.3: RGB
// (233, 100, 90) where 0.4 over black allows 102); a title's fade and a junction's dip to black
// go through the same path, as videoflow_keyframes.test.ts shows on the renderer's own layers.
//
// The Studio writes these keyframes on the layer's own film clock: seconds since the layer came on
// screen. The renderer looks keyframes up in absolute source seconds (`sourceTimeAtFrame`,
// RuntimeBaseLayer.js:175-186), so each time is carried through the layer's own `sourceStart` and
// `speed`, cut nudge included — the numbers `withVolumes` maps a volume curve through.

import type { VideoJSON } from '@videoflow/core';

type Layer = VideoJSON['layers'][number];

interface Keyframe {
  readonly time: number;
  readonly value: unknown;
}

/** VideoFlow's own test for a keyframed property (core dist/layers/BaseLayer.js:231), made strict:
 *  a vector such as `scale: [-1, 1]` is an array too, and stays a value. */
function isKeyframes(value: unknown): value is readonly Keyframe[] {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.every(
      (frame: unknown) =>
        typeof frame === 'object' &&
        frame !== null &&
        'value' in frame &&
        'time' in frame &&
        typeof frame.time === 'number',
    )
  );
}

function animated(layer: Layer): Layer {
  const entries = Object.entries(layer.properties);
  const keyed = entries.filter((entry): entry is [string, readonly Keyframe[]] =>
    isKeyframes(entry[1]),
  );
  if (keyed.length === 0) return layer;
  const sourceStart = layer.settings.sourceStart ?? 0;
  const speed = Math.abs(Number(layer.settings.speed ?? 1)) || 1;
  return {
    ...layer,
    properties: Object.fromEntries(entries.filter(([, value]) => !isKeyframes(value))),
    animations: [
      ...layer.animations,
      ...keyed.map(([property, frames]) => ({
        property,
        keyframes: frames.map((frame) => ({
          time: sourceStart + frame.time * speed,
          value: frame.value,
        })),
      })),
    ],
  };
}

/** The compiled JSON with every keyframed property in its layer's `animations`. */
export function withKeyframes(json: VideoJSON): VideoJSON {
  return { ...json, layers: json.layers.map(animated) };
}
```

- [ ] **Step 4: Compile through it, and time a clip's fade on the film's clock**

In `web/src/videoStudio/videoflow.ts` make five edits.

1. Replace the header's last two lines (`:4-5`):

```ts
// decode per source instead of one per layer. The audio half — sounds and every volume curve —
// is videoflow_audio.ts.
```

with:

```ts
// decode per source instead of one per layer. The audio half — sounds and every volume curve —
// is videoflow_audio.ts; every fade reaches the renderer through videoflow_keyframes.ts.
```

2. In the `./project` import (`:15-24`), add `clipTimelineDuration` after `clipStarts,`:

```ts
import {
  clipStarts,
  clipTimelineDuration,
  junctionDurationAt,
```

and after `import { addAudioItems, playsCleaned, withVolumes } from './videoflow_audio';` (`:25`) add:

```ts
import { withKeyframes } from './videoflow_keyframes';
```

3. Replace `clipOpacity` (`:58-78`) whole:

```ts
/** A clip's opacity: a value, or its fades as keyframes on the film's clock — half a second of
 *  what the viewer sees, whatever the clip's speed. `withKeyframes` carries them onto the source
 *  clock the renderer reads. */
function clipOpacity(clip: VideoItem): unknown {
  const value = clip.opacity ?? 1;
  const fadeIn = clip.fadeIn === true || clip.animation === 'fadeIn';
  const fadeOut = clip.fadeOut === true || clip.animation === 'fadeOut';
  if (!fadeIn && !fadeOut) return value;
  const length = clipTimelineDuration(clip);
  const edge = Math.min(0.5, length / 2);
  return [
    ...(fadeIn
      ? [
          { time: 0, value: 0 },
          { time: edge, value },
        ]
      : []),
    ...(fadeOut
      ? [
          { time: length - edge, value },
          { time: length, value: 0 },
        ]
      : []),
  ];
}
```

4. Replace the comment above `opacity: clipOpacity(clip) as number,` (`:193-194`):

```ts
      // Runtime accepts property keyframes; the package's static property type names only the
      // scalar form. Overlay properties cross the same VideoJSON seam.
```

with:

```ts
      // Fades are keyframes, which the builder cannot take as a value: `withKeyframes` moves them
      // where the renderer reads them, and the cast is over the builder's scalar type.
```

5. Replace the last line of `toVideoJSON` (`:288`):

```ts
  return withVolumes(project, await flow.compile());
```

with:

```ts
  return withVolumes(project, withKeyframes(await flow.compile()));
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/videoflow_keyframes.test.ts src/videoStudio/__tests__/videoflow.test.ts`

Expected: `Test Files  2 passed (2)`, `Tests  17 passed (17)`. The 12 tests of `videoflow.test.ts` are unchanged: they assert what the builder is handed, and the builder is still handed the same keyframes.

Both halves of Step 4 are load-bearing. Measured in scratch with `withKeyframes` in place but `clipOpacity` still using `clip.duration`: `Tests  1 failed | 4 passed (5)`. The sped-up clip fails with `expected 1 to be close to 0.4`, because its fade-out lands past its last frame on screen.

- [ ] **Step 6: Put the new module under the CI mutation gate, with its suite**

A module Stryker mutates is only killed by the suites `web/vitest.stryker.config.ts` lists in `mutationTests`: that config's `include` is that list and nothing else (`:95`). A module whose suite is missing scores every mutant `NoCoverage`, which `critical_mutation_gate.py:63` counts as survived; the file's own comment (`:71-73`) records the run that fell to 69.37 % that way. So both files change together.

In `web/stryker.config.json`, replace:

```json
    "src/videoStudio/videoflow_audio.ts"
```

with:

```json
    "src/videoStudio/videoflow_audio.ts",
    "src/videoStudio/videoflow_keyframes.ts"
```

In `web/vitest.stryker.config.ts`, replace:

```ts
  'src/videoStudio/__tests__/videoflow_audio.test.ts',
] as const;
```

with:

```ts
  'src/videoStudio/__tests__/videoflow_audio.test.ts',
  // aura-video-mcp Plan A: the fade compile, read through the renderer's own runtime layers.
  'src/videoStudio/__tests__/videoflow_keyframes.test.ts',
] as const;
```

Both files key Stryker's incremental cache (`.github/workflows/ci.yml:1617` and `:1619`; the comment at `:1621-1625` records it), so the push in Task 8 runs the whole mutation suite rather than the incremental one: 86m34s measured (`ci.yml:1581-1582`). Task 8 plans for it.

- [ ] **Step 7: Task gates**

Run each of these and read its output:
1. `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio`. Expected: no `FAIL` line; `Test Files` and `Tests` both say `passed` only.
2. `MSYS_NO_PATHCONV=1 wsl bash $W/webcheck.sh src/videoStudio/videoflow_keyframes.ts src/videoStudio/videoflow.ts src/videoStudio/__tests__/videoflow_keyframes.test.ts stryker.config.json vitest.stryker.config.ts`. Expected:
   - `== tsc` prints no error;
   - oxlint prints `Found 0 warnings and 0 errors.`;
   - `== contract` and `== knip` report nothing against these files;
   - `== prettier check` ends `All matched files use Prettier code style!`.

   A file you did not touch that prettier flags belongs to another session: leave it.
3. `MSYS_NO_PATHCONV=1 wsl bash $W/cov.sh 'src/videoStudio/videoflow*.ts' src/videoStudio`. Expected: `% Stmts`, `% Branch` and `% Lines` are at least 85 on the `videoflow.ts` and `...w_keyframes.ts` rows. Measured in scratch with all of Plan A applied: `videoflow_keyframes.ts` 100 / 93.75 / 100, and `videoflow.ts` 96.51 / 89.04 / 98.73.
4. `wc -l web/src/videoStudio/videoflow_keyframes.ts web/src/videoStudio/videoflow.ts web/src/videoStudio/__tests__/videoflow_keyframes.test.ts`. Expected: every file ≤ 600 (74, 406, 193).

CI note: Stryker mutates `videoflow_keyframes.ts` in CI only (`break: 70`). Read its score in the `critical-mutation` artifact after Task 8's push. Never run it locally.

- [ ] **Step 8: Commit**

Write `$W/msg-t1.txt`:

```text
fix(web): fade Studio layers where VideoFlow reads keyframes

A clip's fade-out never reached the export (spikes/video-mcp-render
FINDINGS S1.3: RGB (233, 100, 90) in a frame that 0.4 opacity over black
caps at 102). VideoFlow's builder stores a keyframe array handed to it
as an initial property as the value of a single step keyframe, and the
compile writes a one-keyframe property out as a static value (core
dist/VideoFlow.js:629-638, :856-864). The renderer unit-converts each
keyframe object in that array to the number NaN, and a NaN opacity draws
at full opacity. A clip's fade was measured lost on an exported frame;
title fades and the fade-to-black and fade-to-white washes go through
the same path, as the new test shows on the renderer's own layers, and
the Stage preview compiles the same JSON.

withKeyframes moves every keyframed property into the layer's animations
on its source clock, where the renderer reads them, as the mixer already
needs for volume. A clip's fade now lasts half a second of film at any
speed: clipOpacity placed it by the clip's source length, which at 2x
puts the fade-out past the clip's last frame on screen. The new test
builds the renderer's own runtime layers from the compiled JSON and
reads the opacity they draw.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t1.txt web/src/videoStudio/videoflow_keyframes.ts web/src/videoStudio/__tests__/videoflow_keyframes.test.ts web/src/videoStudio/videoflow.ts web/stryker.config.json web/vitest.stryker.config.ts`

Expected: `COMMIT_RC=0`.

---

### Task 2: Each source is fetched once, and a source that cannot play stops the export by name

**Files:**
- Create: `web/src/videoStudio/videoflow_media.ts`
- Create: `web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts`
- Modify: `web/src/videoStudio/__tests__/videoflowFakes.ts` (the whole file, below)
- Rewrite: `web/src/videoStudio/__tests__/videoflow_export.test.ts`
- Rewrite: `web/src/videoStudio/videoflow_exportAudio.ts` (the whole file, below)
- Modify: `web/src/videoStudio/videoflow.ts`. Line numbers are as Task 1 leaves the file:
  - the header `:1-5`;
  - an import after `:27`;
  - `MIX_SAMPLE_RATE` `:57-59`;
  - the muted comment `:183-188`;
  - the pre-decode `:297-360`;
  - `ExportOptions.signal`'s doc `:363-364`;
  - `exportProject` from `:380` to its end.
- Modify: `web/stryker.config.json`, `web/vitest.stryker.config.ts` (both as Task 1 left them)

**Interfaces:**
- Consumes:
  - `toVideoJSON` and `MediaUrls` from `videoflow.ts` (Task 1's state);
  - `loadedMedia` from `@videoflow/core`, VideoFlow's page-wide `MediaCache` (`core/dist/MediaCache.js:174`).
- Produces, from `web/src/videoStudio/videoflow_media.ts`:

```ts
export type SourceFailure = 'unreachable' | 'undecodable';
export class ExportSourceError extends Error {
  readonly assetId: string;          // the project's asset id; the URL itself if no asset maps to it
  readonly failure: SourceFailure;
  constructor(assetId: string, failure: SourceFailure);
  // name === 'ExportSourceError'; message === `videoStudio: source ${assetId} is ${failure}`
}
export async function renderLoaded<T>(
  renderer: BrowserRenderer,
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
  signal: AbortSignal | undefined,
  render: () => Promise<T>,
): Promise<T>;
```

`renderLoaded` does four things, in order:
1. It loads every layer's media through VideoFlow, one fetch per source.
2. It throws `ExportSourceError` for a layer VideoFlow disabled, or for a sound that is not muted and will not decode.
3. It decodes each source's audio once.
4. It runs `render`, and destroys the renderer exactly once, however that ends.

Both exports reject with `ExportSourceError`. Their signatures are unchanged:
- `exportProject(project, urls, options?)`;
- `exportProjectAudio(project, urls, signal?)`.

`loadSources` and `primeDecodedBuffers` are private to `videoflow_media.ts`; nothing exports them any more.

- [ ] **Step 1: Put VideoFlow's real media cache behind the fake renderer**

Replace the whole of `web/src/videoStudio/__tests__/videoflowFakes.ts` with the file below. Three things change:
- the renderer loads through VideoFlow's own `MediaCache`, so a fetch count is the cache's own;
- `layer()` derives `hasAudio` from the layer's type, as VideoFlow does: `RuntimeVideoLayer` and `RuntimeAudioLayer` answer true whatever the file holds, and every other layer answers false. A still is an `image` layer;
- `layer()` can mark a layer muted (`properties.mute`, where the mixer reads it).

```ts
import { vi } from 'vitest';
import type { VideoProject } from '../project';

// videoflowFakes.ts — VideoFlow's builder and browser renderer stood in for, shared by
// videoflow.test.ts (what the compile hands the builder) and the export tests (what an export does
// with the renderer). An export test mocks both packages with the classes below, and hands out the
// media cache through a getter, so the code under test always reads this test's own cache:
//
//   vi.mock('@videoflow/core', async () => {
//     const fakes = await import('./videoflowFakes');
//     return { default: fakes.FakeVideoFlow, get loadedMedia() { return fakes.media.cache; } };
//   });
//   vi.mock('@videoflow/renderer-browser', async () => ({
//     default: (await import('./videoflowFakes')).FakeRenderer,
//   }));
//
// The builder records every call it is given; the renderer records what the export asked of it.
// The media cache is NOT a stand-in: it is VideoFlow's own `MediaCache`, a fresh one per test, so
// a hit, a miss and a failed fetch behave exactly as they do under the real renderer.

const { MediaCache } = await vi.importActual<typeof import('@videoflow/core')>('@videoflow/core');

/** The page's `loadedMedia`, replaced by a fresh cache in `installFakes`. A module mock hands it
 *  out through a getter, so the code under test always reads the current one. */
export const media = { cache: new MediaCache() };

interface Call {
  props: Record<string, unknown>;
  settings: Record<string, unknown>;
}

export const calls = {
  videos: [] as Call[],
  texts: [] as Call[],
  images: [] as Call[],
  shapes: [] as Call[],
  audios: [] as Call[],
  waits: [] as unknown[],
  project: [] as unknown[],
  compiled: 0,
};

export class FakeVideoFlow {
  constructor(settings: unknown) {
    calls.project.push(settings);
  }
  addVideo(props: Record<string, unknown>, settings: Record<string, unknown>) {
    calls.videos.push({ props, settings });
    return { animate: vi.fn() };
  }
  addText(props: Record<string, unknown>, settings: Record<string, unknown>) {
    calls.texts.push({ props, settings });
    return { animate: vi.fn() };
  }
  addImage(props: Record<string, unknown>, settings: Record<string, unknown>) {
    calls.images.push({ props, settings });
    return { animate: vi.fn() };
  }
  addShape(props: Record<string, unknown>, settings: Record<string, unknown>) {
    calls.shapes.push({ props, settings });
    return { animate: vi.fn() };
  }
  addAudio(props: Record<string, unknown>, settings: Record<string, unknown>) {
    calls.audios.push({ props, settings });
    return { animate: vi.fn() };
  }
  wait(time: unknown) {
    calls.waits.push(time);
  }
  compile() {
    calls.compiled += 1;
    return { layers: [], duration: 7 };
  }
}

export interface FakeLayer {
  json: {
    type: string;
    settings: { source?: string; enabled?: boolean };
    properties: { mute?: boolean };
  };
  hasAudio: boolean;
  decodedBuffer: unknown;
  /** Its bytes arrive but it cannot read them — an HEVC clip, a corrupt still. */
  unreadable?: true;
}

/**
 * A layer over `source`, the way VideoFlow's runtime would hold it before `initLayers`. Whether it
 * carries sound follows from its type, as in VideoFlow: `RuntimeVideoLayer` and `RuntimeAudioLayer`
 * answer `hasAudio` true whatever the file holds, and every other layer false.
 */
export function layer(
  source: string,
  options: { type?: 'video' | 'image' | 'audio'; muted?: true; unreadable?: true } = {},
): FakeLayer {
  const type = options.type ?? 'video';
  return {
    json: { type, settings: { source }, properties: options.muted ? { mute: true } : {} },
    hasAudio: type !== 'image',
    decodedBuffer: null,
    ...(options.unreadable ? { unreadable: true } : {}),
  };
}

export const renderer = {
  instances: [] as FakeRenderer[],
  layers: [] as FakeLayer[],
  stockFontRequests: [] as string[],
  initCalls: 0,
  exportOptions: [] as { worker?: boolean; signal?: AbortSignal }[],
  // Each test decides how the export behaves: a Blob by default, a hang for the abort case.
  exportImpl: null as null | ((options: { signal?: AbortSignal }) => Promise<Blob>),
  /** What `renderAudio` answers: a mix by default, `null` for a project with no sound. */
  audio: {} as object | null,
};

export class FakeRenderer {
  json: unknown;
  layers: FakeLayer[] = [];
  loadedFonts: Record<string, string> = {};
  destroyed = 0;
  private readonly held: string[] = [];
  constructor(json: unknown) {
    this.json = json;
    renderer.instances.push(this);
  }
  // VideoFlow's own loadFont fetches fonts.googleapis.com — 26 requests, measured in spike 107.
  // Anything that lands here is a request that left the origin.
  loadFont(name: string): Promise<void> {
    renderer.stockFontRequests.push(name);
    return Promise.resolve();
  }
  /** What BrowserRenderer.initLayers does (dist/BrowserRenderer.js:427-445): every layer takes its
   *  bytes from the media cache, and one that cannot load is disabled, never thrown. */
  async initLayers(): Promise<void> {
    renderer.initCalls += 1;
    this.layers = renderer.layers;
    await Promise.all(
      this.layers.map(async (held) => {
        const source = held.json.settings.source;
        if (source === undefined) return;
        try {
          await media.cache.acquire(source);
          this.held.push(source);
          if (held.unreadable) throw new Error('the layer could not read its bytes');
        } catch {
          held.json.settings.enabled = false;
        }
      }),
    );
  }
  exportVideo(options: { worker?: boolean; signal?: AbortSignal }): Promise<Blob> {
    renderer.exportOptions.push(options);
    if (renderer.exportImpl) return renderer.exportImpl(options);
    return Promise.resolve(new Blob(['mp4'], { type: 'video/mp4' }));
  }
  renderAudio(): Promise<object | null> {
    return Promise.resolve(renderer.audio);
  }
  destroy() {
    this.destroyed += 1;
    for (const source of this.held.splice(0)) media.cache.release(source);
  }
}

/** Every decode asked for, and the sources whose bytes the decoder refuses: a fetched body is its
 *  own URL (see `installFakes`), so the decoder knows which source it was handed. */
export const decoded = { calls: [] as ArrayBuffer[], refused: new Set<string>() };

export function project(): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 30,
    sources: [
      {
        id: 'src-a',
        assetId: 'asset-a',
        kind: 'video',
        duration: 10,
        size: { width: 1920, height: 1080 },
      },
      {
        id: 'src-still',
        assetId: 'asset-still',
        kind: 'image',
        duration: 0,
        size: { width: 1920, height: 1080 },
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 3, sourceStart: 4, muted: true },
    ],
    overlays: [
      {
        id: 'lane-1',
        items: [
          {
            id: 'title',
            kind: 'text',
            anchor: { clipId: 'clip-2', offset: 0.5 },
            duration: 1,
            props: { text: 'AURA', fontFamily: 'Atkinson Hyperlegible Next' },
          },
        ],
      },
    ],
  };
}

export const urls = {
  assetUrl: (assetId: string) => `/api/assets/${encodeURIComponent(assetId)}/download`,
};

/** Empties every record and stands in the browser pieces the adapter reaches: fetch, the
 *  decoder, the font set and the stylesheet links it waits on. For a `beforeEach`. */
export function installFakes(): void {
  calls.videos.length = 0;
  calls.texts.length = 0;
  calls.images.length = 0;
  calls.shapes.length = 0;
  calls.audios.length = 0;
  calls.waits.length = 0;
  calls.project.length = 0;
  calls.compiled = 0;
  renderer.instances.length = 0;
  renderer.layers = [];
  renderer.stockFontRequests.length = 0;
  renderer.initCalls = 0;
  renderer.exportOptions.length = 0;
  renderer.exportImpl = null;
  renderer.audio = {};
  decoded.calls.length = 0;
  decoded.refused.clear();
  media.cache = new MediaCache();

  // Each source's body is its own URL, so a decode can tell which source it was handed.
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => Promise.resolve(new Response(url))),
  );
  vi.stubGlobal(
    'OfflineAudioContext',
    class {
      decodeAudioData(bytes: ArrayBuffer) {
        decoded.calls.push(bytes);
        if (decoded.refused.has(new TextDecoder().decode(bytes))) {
          return Promise.reject(new DOMException('Unable to decode audio data', 'EncodingError'));
        }
        return Promise.resolve({ id: decoded.calls.length });
      }
    },
  );
  Object.defineProperty(document, 'fonts', {
    configurable: true,
    value: { load: vi.fn(() => Promise.resolve([])) },
  });
  vi.spyOn(document.head, 'appendChild').mockImplementation(<T extends Node>(node: T): T => {
    Node.prototype.appendChild.call(document.head, node);
    queueMicrotask(() => node.dispatchEvent(new Event('load')));
    return node;
  });
}

/** Undoes `installFakes`. For an `afterEach`. */
export function removeFakes(): void {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.head.querySelectorAll('link[data-local-font]').forEach((link) => {
    link.remove();
  });
}
```

- [ ] **Step 2: Write the failing export tests**

Replace the whole of `web/src/videoStudio/__tests__/videoflow_export.test.ts` with the file below. Two of today's tests are **rewritten**, and the commit (Step 10) says why:
- "leaves a source it cannot decode to the mixer" stood for a clip with no audio track by making `fetch` *reject*. That is exactly the failure the export must now report. It becomes "leaves a video whose sound will not decode to the mixer", with bytes that arrive and do not decode.
- "stops the pre-decode when the abort lands before the export starts" held open the pre-decode's own `fetch`, which this task deletes. It becomes "decodes nothing and exports nothing when the abort lands while the media loads". VideoFlow's fetch takes no signal, so its bytes land after the abort. The test then reads the page-wide cache's reference count, to prove that the bytes the layer acquired after the close were released.

The new cases:
- the error's `name` and `message`, which are what survive where the class does not;
- a body that fails halfway through the download;
- an undecodable sound named by its own asset (`asset-b`);
- a muted sound whose bytes will not decode. It exports, because the mixer never plays a muted layer (`scheduleBufferOnContext` reads `mute` first, `renderer-browser/dist/audio/mixer.js:245-247`).

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { VideoProject } from '../project';
import { exportProject } from '../videoflow';
import { ExportSourceError } from '../videoflow_media';
import {
  calls,
  decoded,
  installFakes,
  layer,
  media,
  project,
  removeFakes,
  renderer,
  urls,
} from './videoflowFakes';

// What the export does with VideoFlow's browser renderer: the media it loads, the pre-decode, the
// local fonts, the renderer's lifetime and the abort. The compile it starts from is
// videoflow.test.ts's subject.

vi.mock('@videoflow/core', async () => {
  const fakes = await import('./videoflowFakes');
  return {
    default: fakes.FakeVideoFlow,
    get loadedMedia() {
      return fakes.media.cache;
    },
  };
});
vi.mock('@videoflow/renderer-browser', async () => ({
  default: (await import('./videoflowFakes')).FakeRenderer,
}));

beforeEach(installFakes);
afterEach(removeFakes);

const A = '/api/assets/asset-a/download';
const B = '/api/assets/asset-b/download';
const C = '/api/assets/asset-c/download';

/** How many times the page fetched each URL. */
function fetchesOf(url: string): number {
  const stub = globalThis.fetch as unknown as { mock: { calls: [string][] } };
  return stub.mock.calls.filter(([fetched]) => fetched === url).length;
}

/** The demo project with a sound of its own, `asset-b`, under the film. */
function withSound(): VideoProject {
  const base = project();
  return {
    ...base,
    sources: [
      ...base.sources,
      {
        id: 'src-b',
        assetId: 'asset-b',
        kind: 'audio',
        duration: 5,
        size: { width: 0, height: 0 },
      },
    ],
  };
}

/** A fetch that answers `status` for one URL and serves every other. */
function answering(url: string, status: number): void {
  vi.stubGlobal(
    'fetch',
    vi.fn((fetched: string) =>
      Promise.resolve(new Response(fetched, { status: fetched === url ? status : 200 })),
    ),
  );
}

describe('exportProject', () => {
  it('fetches each source once and decodes it once, however many clips were cut from it', async () => {
    renderer.layers = [layer(A), layer(A), layer(B), layer(C, { type: 'image' })];

    const blob = await exportProject(project(), urls);

    expect(blob.type).toBe('video/mp4');
    // S1.4 measured two GETs per source: VideoFlow's own, then the pre-decode's.
    expect([fetchesOf(A), fetchesOf(B), fetchesOf(C)]).toEqual([1, 1, 1]);
    expect(decoded.calls).toHaveLength(2);
    expect(renderer.layers[0]?.decodedBuffer).toBe(renderer.layers[1]?.decodedBuffer);
    expect(renderer.layers[2]?.decodedBuffer).not.toBe(renderer.layers[0]?.decodedBuffer);
    expect(renderer.layers[3]?.decodedBuffer).toBeNull();
    expect(renderer.exportOptions[0]).toMatchObject({ worker: true });
  });

  it('stops on a source the server refuses, naming its asset, and exports nothing', async () => {
    answering(A, 404);
    renderer.layers = [layer(A)];

    const failure = exportProject(project(), urls);

    await expect(failure).rejects.toBeInstanceOf(ExportSourceError);
    // The name and the message are what survive where the class does not: a log line, or a page
    // handing the failure on as plain data.
    await expect(failure).rejects.toMatchObject({
      name: 'ExportSourceError',
      message: 'videoStudio: source asset-a is unreachable',
      assetId: 'asset-a',
      failure: 'unreachable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('stops on a source whose body fails halfway, as one that never arrived', async () => {
    // The server answered 200 and the stream broke: VideoFlow's cache drops the entry when the
    // body rejects (core dist/MediaCache.js `fetchAndStore`, then `acquire`), so it reads as lost.
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            new ReadableStream({
              start: (stream) => {
                stream.error(new TypeError('network error'));
              },
            }),
          ),
        ),
      ),
    );
    renderer.layers = [layer(A)];

    await expect(exportProject(project(), urls)).rejects.toMatchObject({
      assetId: 'asset-a',
      failure: 'unreachable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it('stops on a source the page cannot reach at all: a CORS refusal has no status', async () => {
    // S1.4's case: the media origin sent no CORS header, and the export resolved with black.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    );
    renderer.layers = [layer(A)];

    await expect(exportProject(project(), urls)).rejects.toMatchObject({
      assetId: 'asset-a',
      failure: 'unreachable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it.each([
    ['the cleaned copy of a clip', 'asset-a-clean'],
    ['a picture laid over the film', 'asset-logo'],
  ])('names %s by its own asset when its bytes are gone', async (_what, assetId) => {
    const url = `/api/assets/${assetId}/download`;
    answering(url, 404);
    renderer.layers = [layer(A), layer(url)];
    const base = project();

    await expect(
      exportProject(
        {
          ...base,
          sources: base.sources.map((source) =>
            source.id === 'src-a' ? { ...source, denoisedAssetId: 'asset-a-clean' } : source,
          ),
          overlays: [
            ...base.overlays,
            {
              id: 'lane-2',
              items: [
                {
                  id: 'logo',
                  kind: 'image',
                  anchor: { clipId: 'clip-1', offset: 0 },
                  duration: 1,
                  props: { assetId: 'asset-logo' },
                },
              ],
            },
          ],
        },
        urls,
      ),
    ).rejects.toMatchObject({ assetId, failure: 'unreachable' });
  });

  it('stops on a source whose bytes arrived but that the renderer could not read', async () => {
    renderer.layers = [
      layer(A),
      layer('/api/assets/asset-still/download', { type: 'image', unreadable: true }),
    ];

    await expect(exportProject(project(), urls)).rejects.toMatchObject({
      assetId: 'asset-still',
      failure: 'undecodable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it('stops on a sound whose bytes will not decode, rather than exporting its silence', async () => {
    decoded.refused.add(B);
    renderer.layers = [layer(A), layer(B, { type: 'audio' })];

    await expect(exportProject(withSound(), urls)).rejects.toMatchObject({
      assetId: 'asset-b',
      failure: 'undecodable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it('exports past a muted sound whose bytes will not decode: the mixer never plays it', async () => {
    decoded.refused.add(B);
    renderer.layers = [layer(A), layer(B, { type: 'audio', muted: true })];

    const blob = await exportProject(withSound(), urls);

    expect(blob.type).toBe('video/mp4');
    expect(renderer.layers[1]?.decodedBuffer).toBeNull();
  });

  it('leaves a video whose sound will not decode to the mixer, rather than failing the export', async () => {
    // A clip with no audio track still reaches the decoder — VideoFlow hard-codes
    // `RuntimeVideoLayer.hasAudio` true — and the mixer catches that failure itself.
    decoded.refused.add(A);
    renderer.layers = [layer(A)];

    const blob = await exportProject(project(), urls);

    expect(blob.type).toBe('video/mp4');
    expect(renderer.layers[0]?.decodedBuffer).toBeNull();
  });

  it('hands the renderer the local fonts before it renders a single frame', async () => {
    await exportProject(project(), urls);

    const instance = renderer.instances[0];
    await instance?.loadFont('Atkinson Hyperlegible Next');
    expect(renderer.stockFontRequests).toEqual([]);
    expect(instance?.loadedFonts['Atkinson Hyperlegible Next']).toBe('/fonts/atkinson.css');
  });

  it('destroys the renderer when the export finishes', async () => {
    await exportProject(project(), urls);

    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('destroys the renderer once when the export throws', async () => {
    renderer.exportImpl = () => Promise.reject(new Error('encoder gone'));

    await expect(exportProject(project(), urls)).rejects.toThrow('encoder gone');
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('destroys the renderer the moment its editor closes', async () => {
    const controller = new AbortController();
    renderer.exportImpl = (options) =>
      new Promise((_resolve, reject) => {
        options.signal?.addEventListener(
          'abort',
          () => {
            reject(new Error('aborted'));
          },
          { once: true },
        );
      });

    const running = exportProject(project(), urls, { signal: controller.signal });
    await vi.waitFor(() => {
      expect(renderer.exportOptions).toHaveLength(1);
    });
    controller.abort();

    await expect(running).rejects.toThrow();
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('constructs no renderer when the abort lands while the project is being compiled', async () => {
    const controller = new AbortController();

    const running = exportProject(project(), urls, { signal: controller.signal });
    // Synchronously after the call: `exportProject` is suspended inside `toVideoJSON`, past the
    // first check and before any listener could exist.
    controller.abort();

    await expect(running).rejects.toThrow();
    expect(renderer.instances).toHaveLength(0);
  });

  it('decodes nothing and exports nothing when the abort lands while the media loads', async () => {
    const controller = new AbortController();
    let arrive: (() => void) | undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn(
        (url: string) =>
          new Promise<Response>((resolve) => {
            arrive = () => {
              resolve(new Response(url));
            };
          }),
      ),
    );
    renderer.layers = [layer(A)];

    const running = exportProject(project(), urls, { signal: controller.signal });
    await vi.waitFor(() => {
      expect(arrive).toBeDefined();
    });
    controller.abort();
    // VideoFlow's fetch takes no signal; its bytes land after the editor has closed.
    arrive?.();

    await expect(running).rejects.toThrow();
    expect(decoded.calls).toHaveLength(0);
    expect(renderer.exportOptions).toHaveLength(0);
    expect(renderer.instances[0]?.destroyed).toBe(1);
    // What the layer acquired after the close is released too: the cache is the page's, and the
    // Stage reads it. One acquire now is the only reference left, so the count reads 1.
    const entry = await media.cache.acquire(A);
    expect(entry.refCount).toBe(1);
    media.cache.release(A);
  });

  it('renders nothing at all when its signal is already aborted', async () => {
    const controller = new AbortController();
    controller.abort();

    await expect(exportProject(project(), urls, { signal: controller.signal })).rejects.toThrow();
    expect(renderer.instances).toHaveLength(0);
    expect(calls.compiled).toBe(0);
  });

  it('reports progress the way its caller asked to hear it', async () => {
    const onProgress = vi.fn();
    renderer.exportImpl = (options) => {
      (options as { onProgress?: (p: number) => void }).onProgress?.(0.5);
      return Promise.resolve(new Blob(['mp4'], { type: 'video/mp4' }));
    };

    await exportProject(project(), urls, { onProgress });

    expect(onProgress).toHaveBeenCalledWith(0.5);
  });
});
```

- [ ] **Step 3: Write the sound export's tests**

`videoflow_exportAudio.ts` has no test today (0 % coverage). Create `web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts`. The encoder stand-in leaves known RIFF/WAVE bytes in its target, and the first test asserts that the export hands those exact bytes back.

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { exportProjectAudio, NoProjectAudioError } from '../videoflow_exportAudio';
import { ExportSourceError } from '../videoflow_media';
import { installFakes, layer, project, removeFakes, renderer, urls } from './videoflowFakes';

// The sound-only export: the same media and pre-decode as the film (videoflow_export.test.ts),
// then the mix written as a 16-bit WAVE by Mediabunny, which is stood in for here — jsdom has no
// AudioBuffer to encode. What is proven is what this module decides: when there is no file to
// write, when the file came out empty, and that closing the editor stops the encoder.

vi.mock('@videoflow/core', async () => {
  const fakes = await import('./videoflowFakes');
  return {
    default: fakes.FakeVideoFlow,
    get loadedMedia() {
      return fakes.media.cache;
    },
  };
});
vi.mock('@videoflow/renderer-browser', async () => ({
  default: (await import('./videoflowFakes')).FakeRenderer,
}));

const wav = vi.hoisted(() => ({
  outputs: [] as { state: string }[],
  /** The bytes the encoder leaves in its target: the file the export must hand back unchanged. */
  file: new Uint8Array([0x52, 0x49, 0x46, 0x46, 0x24, 0x00, 0x00, 0x00, 0x57, 0x41, 0x56, 0x45]),
  /** The encoder finishes without a file. */
  empty: false,
  /** When set, `start()` waits on it: the moment an abort can land mid-write. */
  started: null as Promise<void> | null,
  /** The encoder refuses the mix. */
  refuses: false,
}));

vi.mock('mediabunny', () => {
  class BufferTarget {
    buffer: ArrayBuffer | null = null;
  }
  class Output {
    state = 'pending';
    private readonly target: BufferTarget;
    constructor(options: { target: BufferTarget }) {
      this.target = options.target;
      wav.outputs.push(this);
    }
    addAudioTrack(): void {
      // The track is the source below; nothing to record.
    }
    async start(): Promise<void> {
      this.state = 'started';
      await wav.started;
    }
    finalize(): Promise<void> {
      this.state = 'finalized';
      if (!wav.empty) this.target.buffer = wav.file.slice().buffer;
      return Promise.resolve();
    }
    cancel(): Promise<void> {
      this.state = 'canceled';
      return Promise.resolve();
    }
  }
  return {
    BufferTarget,
    Output,
    WavOutputFormat: class {
      readonly container = 'wav';
    },
    AudioBufferSource: class {
      add(): Promise<void> {
        return wav.refuses ? Promise.reject(new Error('encoder refused')) : Promise.resolve();
      }
    },
  };
});

beforeEach(() => {
  installFakes();
  wav.outputs.length = 0;
  wav.empty = false;
  wav.started = null;
  wav.refuses = false;
});
afterEach(removeFakes);

describe('exportProjectAudio', () => {
  it('writes the mix as a WAVE file and destroys the renderer', async () => {
    renderer.layers = [layer('/api/assets/asset-a/download')];

    const blob = await exportProjectAudio(project(), urls);

    expect(blob.type).toBe('audio/wav');
    expect(new Uint8Array(await blob.arrayBuffer())).toEqual(wav.file);
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('says there is nothing to write when the project has no sound', async () => {
    renderer.audio = null;

    await expect(exportProjectAudio(project(), urls)).rejects.toBeInstanceOf(NoProjectAudioError);
    expect(wav.outputs).toHaveLength(0);
  });

  it('stops on a source it cannot fetch, naming its asset, and writes nothing', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string) => Promise.resolve(new Response(url, { status: 404 }))),
    );
    renderer.layers = [layer('/api/assets/asset-a/download', { type: 'audio' })];

    const failure = exportProjectAudio(project(), urls);

    await expect(failure).rejects.toBeInstanceOf(ExportSourceError);
    await expect(failure).rejects.toMatchObject({ assetId: 'asset-a', failure: 'unreachable' });
    expect(wav.outputs).toHaveLength(0);
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('refuses an encoder that finished without a file', async () => {
    wav.empty = true;

    await expect(exportProjectAudio(project(), urls)).rejects.toThrow(
      'WAVE encoder produced no file',
    );
  });

  it('cancels the encoder when writing the mix fails, so no half file is left open', async () => {
    wav.refuses = true;

    await expect(exportProjectAudio(project(), urls)).rejects.toThrow('encoder refused');
    expect(wav.outputs[0]?.state).toBe('canceled');
  });

  it('cancels the encoder when the editor closes while it writes', async () => {
    const controller = new AbortController();
    let resume: (() => void) | undefined;
    wav.started = new Promise((resolve) => {
      resume = resolve;
    });

    const running = exportProjectAudio(project(), urls, controller.signal);
    await vi.waitFor(() => {
      expect(wav.outputs[0]?.state).toBe('started');
    });
    controller.abort();
    resume?.();

    await expect(running).rejects.toThrow();
    expect(wav.outputs[0]?.state).toBe('canceled');
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });
});
```

- [ ] **Step 4: Give the tests their vocabulary, and nothing else yet**

Create `web/src/videoStudio/videoflow_media.ts` with only the error type, so the tests compile and fail on behaviour:

```ts
/** Why a source stopped the export: its bytes never arrived, or they arrived and did not play. */
export type SourceFailure = 'unreachable' | 'undecodable';

/** A source the export could not play. The asset id names it; the export wrote nothing. */
export class ExportSourceError extends Error {
  readonly assetId: string;
  readonly failure: SourceFailure;

  constructor(assetId: string, failure: SourceFailure) {
    super(`videoStudio: source ${assetId} is ${failure}`);
    this.name = 'ExportSourceError';
    this.assetId = assetId;
    this.failure = failure;
  }
}
```

- [ ] **Step 5: Run the tests to verify they fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/videoflow_export.test.ts src/videoStudio/__tests__/videoflow_exportAudio.test.ts`

Expected: `Tests  10 failed | 14 passed (24)`, measured in scratch:
- `AssertionError: expected [ 2, 2, 1 ] to deeply equal [ 1, 1, 1 ]`: every audible source is fetched twice today.
- Eight fail with `AssertionError: promise resolved "Blob{ …(1) }" instead of rejecting`. Each of these "succeeds" today:
  - the refused source;
  - the body that fails halfway;
  - the CORS refusal;
  - the cleaned copy;
  - the picture;
  - the unreadable still;
  - the undecodable sound;
  - the sound export's refused source.
- The abort while the media loads fails with `AssertionError: expected 2 to be 1`. The layer's acquire lands after the close and nothing ever releases it, so the page-wide cache keeps the source pinned.

- [ ] **Step 6: Write the media module**

Replace the whole of `web/src/videoStudio/videoflow_media.ts` with:

```ts
// videoflow_media.ts — the bytes an export plays: each source fetched once, and a source the
// renderer could not read stopping the export by name instead of becoming black or silence.
//
// VideoFlow's layers and its mixer all take their bytes from `loadedMedia`, one refcounted cache
// per page keyed by URL (core dist/MediaCache.js; renderer-browser dist/layers/RuntimeVideoLayer.js:63,
// RuntimeAudioLayer.js:31, RuntimeImageLayer.js:16, audio/mixer.js:69). Its `initLayers` catches
// a layer that fails to load, logs a warning and disables the layer (BrowserRenderer.js:427-445),
// and the export still resolves: measured in spikes/video-mcp-render/FINDINGS.md S1.4, a source
// behind a refused fetch came out as a black, silent 26,604 B MP4. The same run fetched every
// source twice, because the pre-decode fetched on its own instead of reading that cache.

import { loadedMedia } from '@videoflow/core';
import type BrowserRenderer from '@videoflow/renderer-browser';
import type { AssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import type { VideoProject } from './project';

/** Why a source stopped the export: its bytes never arrived, or they arrived and did not play. */
export type SourceFailure = 'unreachable' | 'undecodable';

/** A source the export could not play. The asset id names it; the export wrote nothing. A page
 *  that hands the failure on to another process catches it here and passes `{ assetId, failure }`
 *  as plain data: a class instance does not survive a structured clone. */
export class ExportSourceError extends Error {
  readonly assetId: string;
  readonly failure: SourceFailure;

  constructor(assetId: string, failure: SourceFailure) {
    super(`videoStudio: source ${assetId} is ${failure}`);
    this.name = 'ExportSourceError';
    this.assetId = assetId;
    this.failure = failure;
  }
}

/** The rate BrowserRenderer mixes at (`renderAudio`, `sampleRate: 48000`) — primed buffers match. */
const MIX_SAMPLE_RATE = 48000;

/** What the export touches on a live renderer. `initLayers` and the layers are not in
 *  BrowserRenderer's public type — see `primeDecodedBuffers` for why we reach for them anyway, and
 *  spike 108 §8 for the measurement that says it is safe. */
interface PrimableLayer {
  readonly json: {
    readonly type: string;
    readonly settings: { readonly source?: string; readonly enabled?: boolean };
    readonly properties?: { readonly mute?: unknown };
  };
  readonly hasAudio: boolean;
  decodedBuffer: AudioBuffer | null;
}

interface PrimableRenderer {
  readonly layers: readonly PrimableLayer[];
  initLayers(): Promise<void>;
}

/** Every asset the project plays, by the URL its layers fetch it from. */
function assetsByUrl(
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
): ReadonlyMap<string, string> {
  const ids = [
    ...project.sources.flatMap((source) =>
      source.denoisedAssetId === undefined
        ? [source.assetId]
        : [source.assetId, source.denoisedAssetId],
    ),
    ...project.overlays
      .flatMap((lane) => lane.items)
      .flatMap((item) => (typeof item.props.assetId === 'string' ? [item.props.assetId] : [])),
  ];
  return new Map(ids.map((id) => [urls.assetUrl(id), id]));
}

/** A source's bytes out of VideoFlow's cache, decoded. Its layers already hold them, so this is
 *  never a fetch; `decodeAudioData` detaches its input, and `arrayBuffer()` is a fresh copy. */
async function decodeHeld(
  source: string,
  audioCtx: OfflineAudioContext,
): Promise<AudioBuffer | null> {
  const entry = await loadedMedia.acquire(source);
  try {
    return await audioCtx.decodeAudioData(await entry.blob.arrayBuffer());
  } catch {
    return null;
  } finally {
    loadedMedia.release(source);
  }
}

/**
 * One decoded buffer per SOURCE, because VideoFlow decodes once per LAYER.
 *
 * Measured, spike 108: 3 layers over 2 sources cost 3 decodes; 16 layers over 1 source cost 16,
 * and every decode reads the WHOLE file whatever `sourceDuration` says — 8 layers of a 60 s clip
 * hold 175.8 MB of PCM alive and spend 1.85 s, 32.7 % of the render, decoding. A cut is the
 * commonest gesture in this editor, so the pathological case is the normal case.
 *
 * The seam is VideoFlow's own and needs no fork: `decodeLayerAudio` reads `layer.decodedBuffer`
 * off ANY layer although only `RuntimeAudioLayer` ever writes it, and `initLayers()` is idempotent
 * (`elementsSetup`), so the export re-entering it costs nothing. `scheduleBufferOnContext` still
 * applies `sourceStart` / `sourceDuration` / `speed` / `mute` per layer. Measured: 16 decodes → 1,
 * 8 → 1, decode time 1 847 → 234 ms on the 60 s case, audio identical sample for sample over
 * 2 880 512 samples. The render-time saving is the noisy figure (−22 % and −40 % in two runs); the
 * decode collapse is the exact one.
 *
 * A video that will not decode — a clip with no audio track, which VideoFlow hands to the decoder
 * anyway because `RuntimeVideoLayer.hasAudio` is hard-coded true — is left to the mixer's own
 * path, which catches the same failure. A SOUND that will not decode has nothing else to play:
 * it would export as silence, so it stops the export — unless the operator muted it, since the
 * mixer drops a muted layer whatever its bytes (`scheduleBufferOnContext` reads `mute` first,
 * audio/mixer.js:245-247).
 */
async function primeDecodedBuffers(
  layers: readonly PrimableLayer[],
  nameOf: (source: string) => string,
  signal: AbortSignal | undefined,
): Promise<void> {
  const audioCtx = new OfflineAudioContext(2, MIX_SAMPLE_RATE, MIX_SAMPLE_RATE);
  const bySource = new Map<string, Promise<AudioBuffer | null>>();
  for (const layer of layers) {
    const source = layer.json.settings.source;
    if (source === undefined || !layer.hasAudio) continue;
    let decoding = bySource.get(source);
    if (decoding === undefined) {
      // The promise rather than the buffer is shared, so two layers of one source wait on one
      // decode instead of racing to start two.
      decoding = decodeHeld(source, audioCtx);
      bySource.set(source, decoding);
    }
    const buffer = await decoding;
    // `decodeAudioData` has no cancellation of its own: a decode already running finishes, and
    // nothing after it is ever scheduled.
    signal?.throwIfAborted();
    if (buffer !== null) layer.decodedBuffer = buffer;
    else if (layer.json.type === 'audio' && layer.json.properties?.mute !== true)
      throw new ExportSourceError(nameOf(source), 'undecodable');
  }
}

/**
 * Load every layer's media through VideoFlow — one fetch per source, into its cache — refuse the
 * export if any layer could not load, and pre-decode the audio from the bytes already held.
 *
 * A layer VideoFlow disabled is told apart by the cache: `acquire` forgets a URL whose fetch
 * failed ("a failed fetch does not poison the URL forever"), so a source still in the cache is one
 * whose bytes arrived and could not be read. VideoFlow keeps the HTTP status in its console warning
 * only; the page never learns it, and neither does this error.
 */
async function loadSources(
  renderer: BrowserRenderer,
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
  signal?: AbortSignal,
): Promise<void> {
  const primable = renderer as unknown as PrimableRenderer;
  await primable.initLayers();
  signal?.throwIfAborted();
  const assets = assetsByUrl(project, urls);
  const nameOf = (source: string): string => assets.get(source) ?? source;
  const lost = primable.layers
    .map((layer) => layer.json.settings)
    .find((settings) => settings.enabled === false && settings.source !== undefined)?.source;
  if (lost !== undefined) {
    throw new ExportSourceError(
      nameOf(lost),
      loadedMedia.has(lost) ? 'undecodable' : 'unreachable',
    );
  }
  await primeDecodedBuffers(primable.layers, nameOf, signal);
}

/**
 * Load the renderer's media, run `render` on it, and destroy it exactly once, however the export
 * ends. The abort reaches `render` through a listener that exists only while it runs. While the
 * media loads there is none, on purpose: VideoFlow's fetches take no signal, so a renderer
 * destroyed under them would see its layers acquire their bytes afterwards and keep them pinned in
 * `loadedMedia`, the page-wide cache the Stage reads too. `loadSources` checks the signal after
 * every await instead, and the `finally` releases what the layers acquired once the loads settle.
 */
export async function renderLoaded<T>(
  renderer: BrowserRenderer,
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
  signal: AbortSignal | undefined,
  render: () => Promise<T>,
): Promise<T> {
  let closed = false;
  const close = (): void => {
    if (closed) return;
    closed = true;
    renderer.destroy();
  };
  try {
    await loadSources(renderer, project, urls, signal);
    signal?.addEventListener('abort', close, { once: true });
    return await render();
  } finally {
    signal?.removeEventListener('abort', close);
    close();
  }
}
```

- [ ] **Step 7: Export through it**

In `web/src/videoStudio/videoflow.ts` make seven edits.

1. Replace the header's first five lines (`:1-5`, as Task 1 left them):

```ts
// videoflow.ts — the only file that composes VideoFlow. It turns the editor's project into a
// VideoJSON, exports it in the browser, and owns the four behaviours the spikes measured:
// fonts from our own origin, the cut nudge, mute and volume where the mixer reads them, and one
// decode per source instead of one per layer. The audio half — sounds and every volume curve —
// is videoflow_audio.ts; every fade reaches the renderer through videoflow_keyframes.ts.
```

with:

```ts
// videoflow.ts — the only file that composes VideoFlow. It turns the editor's project into a
// VideoJSON and exports it in the browser, with the behaviours the spikes measured: fonts from our
// own origin, the cut nudge, and mute and volume where the mixer reads them. The audio half —
// sounds and every volume curve — is videoflow_audio.ts; every fade reaches the renderer through
// videoflow_keyframes.ts; the media an export reads, once per source and never silently lost, is
// videoflow_media.ts.
```

2. After `import { withKeyframes } from './videoflow_keyframes';` (`:27`) add:

```ts
import { renderLoaded } from './videoflow_media';
```

3. Delete these three lines, which sit just above `clipOpacity`'s doc comment (`:57-59`):

```ts
/** The rate BrowserRenderer mixes at (`renderAudio`, `sampleRate: 48000`) — primed buffers match. */
const MIX_SAMPLE_RATE = 48000;

```

4. The comment above the properties `flow.addVideo` is handed (`:183-188`) points at "the cache below", which this task moves out. Replace:

```ts
    // `muted` in a layer's SETTINGS is a no-op — the mixer reads `mute` in its PROPERTIES
    // (spike 108 §6: a clip carrying `settings.muted` played at full volume). Muting does not
    // save the decode either, which is one more reason the cache below belongs to us. A static
    // `volume` here would be ignored as well: the mixer reads it only from the compiled
    // `animations` (S1), which `withVolumes` writes. A cleaned clip is muted too: its sound plays
    // from the cleaned copy's own layer (videoflow_audio.ts).
```

with:

```ts
    // `muted` in a layer's SETTINGS is a no-op — the mixer reads `mute` in its PROPERTIES
    // (spike 108 §6: a clip carrying `settings.muted` played at full volume). Muting does not
    // save the decode either, which is one more reason the export decodes once per source
    // (videoflow_media.ts `primeDecodedBuffers`). A static `volume` here would be ignored as
    // well: the mixer reads it only from the compiled `animations` (S1), which `withVolumes`
    // writes. A cleaned clip is muted too: its sound plays from the cleaned copy's own layer
    // (videoflow_audio.ts).
```

5. Delete everything from `/** What the decode cache touches on a live renderer.` through the closing `}` of `export async function primeDecodedBuffers(…)` and the blank line after it (`:297-360`). That is the `PrimableLayer` interface, the `PrimableRenderer` interface and the whole function; all of it now lives in `videoflow_media.ts`. After the deletion, `toVideoJSON`'s closing `}` is followed by one blank line and then `export interface ExportOptions {`.

6. Replace `ExportOptions.signal`'s doc comment:

```ts
  /** Aborting destroys the renderer. The previous cycle shipped an editor that kept transcoding
   *  after its dialog closed; this one cannot. */
```

with:

```ts
  /** Aborting stops the export and destroys the renderer: at once while it encodes, and as soon as
   *  its media has settled while that loads (videoflow_media.ts `renderLoaded`). The previous
   *  cycle shipped an editor that kept transcoding after its dialog closed; this one cannot. */
```

7. In `exportProject`, replace everything from the comment `// Rechecked after every await, because the listener below cannot cover what happens before it` to the function's closing `}`:

```ts
  // Rechecked after every await, because the listener below cannot cover what happens before it
  // exists: an abort landing while the project compiles would otherwise construct a renderer that
  // nothing ever destroys.
  signal?.throwIfAborted();
  const renderer = withLocalFonts(new BrowserRenderer(json));
  let closed = false;
  const close = (): void => {
    if (closed) return;
    closed = true;
    renderer.destroy();
  };
  signal?.addEventListener('abort', close, { once: true });
  try {
    await primeDecodedBuffers(renderer, signal);
    // The priming loop reaches its own check only when there is audio to prime; a lane of stills
    // would otherwise fall straight through into an export the editor has already closed.
    signal?.throwIfAborted();
    return await renderer.exportVideo({
      worker: true,
      ...(options.onProgress ? { onProgress: options.onProgress } : {}),
      ...(signal ? { signal } : {}),
    });
  } finally {
    signal?.removeEventListener('abort', close);
    close();
  }
}
```

with:

```ts
  // Rechecked after every await, because the abort listener `renderLoaded` adds cannot cover what
  // happens before it exists: an abort landing while the project compiles would otherwise
  // construct a renderer that nothing ever destroys.
  signal?.throwIfAborted();
  const renderer = withLocalFonts(new BrowserRenderer(json));
  return await renderLoaded(renderer, project, urls, signal, () =>
    renderer.exportVideo({
      worker: true,
      ...(options.onProgress ? { onProgress: options.onProgress } : {}),
      ...(signal ? { signal } : {}),
    }),
  );
}
```

The `throwIfAborted` that followed the pre-decode is not lost. `loadSources` checks the signal right after `initLayers` and after every decode, and nothing awaits between its last check and `render()`, so a lane of stills cannot fall through into an export the editor has already closed.

Replace the whole of `web/src/videoStudio/videoflow_exportAudio.ts` with the file below. The encoder body moves, unchanged, into `writeWave`; `renderLoaded` owns the renderer's lifetime.

```ts
import BrowserRenderer from '@videoflow/renderer-browser';
import { AudioBufferSource, BufferTarget, Output, WavOutputFormat } from 'mediabunny';
import type { VideoProject } from './project';
import { toVideoJSON, type MediaUrls } from './videoflow';
import { renderLoaded } from './videoflow_media';

/** The renderer found no sound layers; the UI translates this separately from a failed encode. */
export class NoProjectAudioError extends Error {}

/** Render the edited project's sound mix to a standalone 16-bit PCM WAVE file. */
export async function exportProjectAudio(
  project: VideoProject,
  urls: MediaUrls,
  signal?: AbortSignal,
): Promise<Blob> {
  signal?.throwIfAborted();
  const json = await toVideoJSON(project, urls);
  signal?.throwIfAborted();
  const renderer = new BrowserRenderer(json);
  return await renderLoaded(renderer, project, urls, signal, async () => {
    const audio = await renderer.renderAudio();
    signal?.throwIfAborted();
    if (audio === null) throw new NoProjectAudioError();
    return await writeWave(audio, signal);
  });
}

/** The mix as a WAVE file. Closing the editor cancels the encoder, so no half file is left open. */
async function writeWave(audio: AudioBuffer, signal: AbortSignal | undefined): Promise<Blob> {
  const target = new BufferTarget();
  const output = new Output({ format: new WavOutputFormat(), target });
  const source = new AudioBufferSource({ codec: 'pcm-s16' });
  output.addAudioTrack(source);
  let canceling: Promise<void> | undefined;
  const cancel = () => {
    canceling = output.cancel();
  };
  signal?.addEventListener('abort', cancel, { once: true });
  try {
    await output.start();
    signal?.throwIfAborted();
    await source.add(audio);
    signal?.throwIfAborted();
    await output.finalize();
    signal?.throwIfAborted();
    if (target.buffer === null) throw new Error('WAVE encoder produced no file');
    return new Blob([target.buffer], { type: 'audio/wav' });
  } finally {
    signal?.removeEventListener('abort', cancel);
    if (canceling !== undefined) await canceling;
    else if (output.state !== 'finalized' && output.state !== 'canceled') await output.cancel();
  }
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/videoflow_export.test.ts src/videoStudio/__tests__/videoflow_exportAudio.test.ts src/videoStudio/__tests__/videoflow.test.ts src/videoStudio/__tests__/videoflow_keyframes.test.ts src/videoStudio/__tests__/videoflow_audio.test.ts`

Expected: `Test Files  5 passed (5)`, `Tests  64 passed (64)`, made up of 18 + 6 + 12 + 5 + 23. Measured in scratch.

Two of the new tests are load-bearing against this plan's previous revision. That revision threw for any undecodable sound, and destroyed the renderer the moment the abort landed. Run against that code, both tests fail:
- "exports past a muted sound" fails with `ExportSourceError: videoStudio: source asset-b is undecodable`;
- "decodes nothing and exports nothing when the abort lands while the media loads" fails with `expected 2 to be 1`.

- [ ] **Step 9: Gates**

1. Put the new module under the CI mutation gate, with its two suites. In `web/stryker.config.json`, as Task 1 left it, replace:

   ```json
       "src/videoStudio/videoflow_keyframes.ts"
   ```

   with:

   ```json
       "src/videoStudio/videoflow_keyframes.ts",
       "src/videoStudio/videoflow_media.ts"
   ```

   In `web/vitest.stryker.config.ts`, as Task 1 left it, replace:

   ```ts
     'src/videoStudio/__tests__/videoflow_keyframes.test.ts',
   ] as const;
   ```

   with:

   ```ts
     'src/videoStudio/__tests__/videoflow_keyframes.test.ts',
     // aura-video-mcp Plan A: an export's media, once per source, and the sound-only export.
     'src/videoStudio/__tests__/videoflow_export.test.ts',
     'src/videoStudio/__tests__/videoflow_exportAudio.test.ts',
   ] as const;
   ```

   Without these suites, every mutant in `videoflow_media.ts` would score `NoCoverage`, and the gate counts that as survived (Task 1 Step 6). Measured in scratch, under `vitest.stryker.config.ts` with the three Plan A entries added: the `videoflow_audio`, `videoflow_keyframes`, `videoflow_export` and `videoflow_exportAudio` suites pass, 52 tests of 52. This edit goes out in the same push as Task 1's, so it does not cost a second full Stryker run.
2. `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio`. Expected: no `FAIL` line.
3. `MSYS_NO_PATHCONV=1 wsl bash $W/webcheck.sh src/videoStudio/videoflow_media.ts src/videoStudio/videoflow.ts src/videoStudio/videoflow_exportAudio.ts src/videoStudio/__tests__/videoflowFakes.ts src/videoStudio/__tests__/videoflow_export.test.ts src/videoStudio/__tests__/videoflow_exportAudio.test.ts stryker.config.json vitest.stryker.config.ts`. Expected:
   - `tsc` is clean;
   - oxlint prints `Found 0 warnings and 0 errors.`;
   - knip reports no unused export:
     - `ExportSourceError` is used by the tests now, and by the dialog after Task 3;
     - `renderLoaded` is used by both exports;
     - `SourceFailure` is used in its own file (`ignoreExportsUsedInFile`);
   - prettier: all matched files formatted.
4. `MSYS_NO_PATHCONV=1 wsl bash $W/cov.sh 'src/videoStudio/videoflow*.ts' src/videoStudio`. Expected: at least 85 in `% Stmts`, `% Branch` and `% Lines` on every row. Measured in scratch with all of Plan A applied:

   | File | % Stmts | % Branch | % Lines |
   |---|---|---|---|
   | `videoflow.ts` | 96.51 | 89.04 | 98.73 |
   | `videoflow_media.ts` | 100 | 96.15 | 100 |
   | `videoflow_exportAudio.ts` | 100 | 100 | 100 |
   | `videoflow_keyframes.ts` | 100 | 93.75 | 100 |

5. `wc -l web/src/videoStudio/videoflow_media.ts web/src/videoStudio/videoflow.ts web/src/videoStudio/videoflow_exportAudio.ts web/src/videoStudio/__tests__/videoflowFakes.ts web/src/videoStudio/__tests__/videoflow_export.test.ts web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts`. Expected: all ≤ 600 (201, 329, 53, 275, 346, 153).

CI note: Stryker mutates `videoflow_media.ts` in CI only (`break: 70`). Read its score in the `critical-mutation` artifact after Task 8's push. If it is under 70 %, read the surviving mutants first. Kill them with a test that asserts a behaviour, never by weakening an assertion.

- [ ] **Step 10: Commit**

Write `$W/msg-t2.txt`:

```text
fix(web): stop a Studio export on a source it cannot play, fetched once

A source the page could not fetch became a black, silent MP4 that the
export reported as a success (spikes/video-mcp-render FINDINGS S1.4:
26,604 B in 4.9 s), and every audible source was fetched twice.
VideoFlow's initLayers disables a layer whose media fails and lets the
export resolve (dist/BrowserRenderer.js:427-445), and the pre-decode
fetched every source again on its own and swallowed its failures.

renderLoaded lets VideoFlow load every layer through its own
loadedMedia cache, throws ExportSourceError naming the source's asset
when a layer was disabled -- unreachable when the cache forgot the URL,
undecodable when it holds bytes that did not play -- and decodes each
source's audio once from the bytes the cache already holds. A sound that
will not decode now fails the export instead of exporting silence,
unless it is muted: the mixer never plays a muted layer. A video whose
audio will not decode is still left to the mixer, as VideoFlow
hard-codes RuntimeVideoLayer.hasAudio to true. The renderer is destroyed
once; an abort while the media loads waits for VideoFlow's fetches,
which take no signal, so what the layers acquired is released rather
than pinned in the page-wide cache.

Two export tests are rewritten, not weakened. "leaves a source it cannot
decode to the mixer" stood for a clip with no audio track with a fetch
that REJECTED -- the very failure this commit stops swallowing -- and
now uses bytes that arrive and do not decode. "stops the pre-decode
when the abort lands before the export starts" held the pre-decode's own
fetch open; that fetch no longer exists, so the abort now lands while
VideoFlow's media loads, and still nothing is decoded or exported. The
fake renderer now loads through VideoFlow's real MediaCache, so the
fetch counts are the cache's own. exportProjectAudio gains its first
tests. Both suites join vitest.stryker.config.ts with the module.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t2.txt web/src/videoStudio/videoflow_media.ts web/src/videoStudio/videoflow.ts web/src/videoStudio/videoflow_exportAudio.ts web/src/videoStudio/__tests__/videoflowFakes.ts web/src/videoStudio/__tests__/videoflow_export.test.ts web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts web/stryker.config.json web/vitest.stryker.config.ts`

Expected: `COMMIT_RC=0`.

---

### Task 3: The export dialog names the source that stopped it, by its file name

**Files:**
- Create: `web/src/videoStudio/__tests__/VideoStudio_export.test.tsx`
- Modify: `web/src/i18n/resources.videoStudio.ts`, the en `export` block (`failed` at `:208`) and the it block (`failed` at `:415`)
- Modify: `web/src/videoStudio/VideoStudio_export.tsx`:
  - the imports `:1-7`;
  - `reason` `:30-32`;
  - the `catch` `:68-75`.

**Interfaces:**
- Consumes:
  - `ExportSourceError` from `./videoflow_media` (Task 2);
  - `NoProjectAudioError` from `./videoflow_exportAudio`, which already exists;
  - `getAsset(id: string): Promise<Asset>` from `web/src/chat/attachments/api.ts:36`, which already exists. `Asset.file_name` is the name the library lists (`chat/attachments/types.ts:33`).
- Produces two i18n keys. Each takes one value, `{ source: string }`: the source's file name, or its asset id when the asset row cannot give one.
  - `videoStudio.export.sourceUnreachable`;
  - `videoStudio.export.sourceUndecodable`.

  The en texts:
  - "The export stopped: {{source}} could not be fetched, so no file was written."
  - "The export stopped: the renderer could not read {{source}}, so no file was written."

**Why the file name, read after the failure.** The project file holds only the asset id for a source. Nobody recognises `5f0c…`, and the operator ruled (2026-10-01) that the dialog names the source by its file name. That name lives on the asset row, which is the same row `projectStore.ts:420-422` reads through `getAsset`. The dialog reads it once, after the failure: a source added since the project loaded, or a clip's cleaned copy, has a row the load never read. If the row does not answer, or answers with an empty name, the dialog shows the asset id. A source whose bytes are gone may have lost its row too.

A cancel can land while the name is being read. The dialog checks the signal again after the read, so a cancelled export still says nothing.

- [ ] **Step 1: Write the failing test**

Create `web/src/videoStudio/__tests__/VideoStudio_export.test.tsx`:

```tsx
import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Asset } from '../../chat/attachments/types';
import i18n from '../../i18n/i18n';
import type { VideoProject } from '../project';
import { NoProjectAudioError } from '../videoflow_exportAudio';
import { ExportSourceError } from '../videoflow_media';
import { ExportPanel } from '../VideoStudio_export';

// The export bar's failures, read through the REAL bundle in both languages: a failure reaches
// the operator only as the sentence this panel picks, and a key that resolves to nothing would
// ship as an empty alert. The exports themselves are videoflow_export.test.ts's subject.

const flow = vi.hoisted(() => ({ exportProject: vi.fn(), exportProjectAudio: vi.fn() }));
vi.mock('../videoflow', async (original) => ({
  ...(await original<typeof import('../videoflow')>()),
  exportProject: flow.exportProject,
}));
vi.mock('../videoflow_exportAudio', async (original) => ({
  ...(await original<typeof import('../videoflow_exportAudio')>()),
  exportProjectAudio: flow.exportProjectAudio,
}));

const downloadBlob = vi.hoisted(() => vi.fn());
vi.mock('../../mediaEdit/download', () => ({ downloadBlob }));

// The asset row the library lists a source by: what the failure is named with.
const api = vi.hoisted(() => ({ getAsset: vi.fn() }));
vi.mock('../../chat/attachments/api', () => api);

function row(id: string, fileName: string): Asset {
  return {
    id,
    status: 'complete',
    modality: 'video',
    file_name: fileName,
    mime_type: 'video/mp4',
    declared_size_bytes: 1,
    size_bytes: 1,
  };
}

const project: VideoProject = {
  id: 'p',
  name: 'demo',
  size: { width: 320, height: 180 },
  fps: 30,
  sources: [],
  video: [],
  overlays: [],
};

/** Render the panel and press `key`'s button; the export fails, and the panel settles, inside act. */
async function press(key: string): Promise<void> {
  render(
    <ExportPanel
      project={project}
      fileName="demo.mp4"
      audioFileName="demo.wav"
      urls={{ assetUrl: (id) => `/api/assets/${id}/download` }}
    />,
  );
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: i18n.t(key) }));
    await turn();
  });
}

/** One macrotask: every promise the panel chained — the failure, the name read, the reset — has
 *  settled by then, so its state updates all land inside the surrounding act. */
function turn(): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, 0);
  });
}

function alertText(): string | null | undefined {
  return screen.queryByRole('alert')?.textContent;
}

beforeEach(() => {
  flow.exportProject.mockReset();
  flow.exportProjectAudio.mockReset();
  downloadBlob.mockReset();
  api.getAsset.mockReset();
  api.getAsset.mockImplementation((id: string) =>
    Promise.resolve(row(id, id === 'asset-b' ? 'clip-b.mp4' : 'voce.m4a')),
  );
});

afterEach(async () => {
  // The panel is still mounted here, and a language change re-renders it.
  await act(async () => {
    await i18n.changeLanguage('en');
  });
});

describe('ExportPanel failures', () => {
  it.each([
    ['en', 'The export stopped: clip-b.mp4 could not be fetched, so no file was written.'],
    [
      'it',
      'Esportazione interrotta: non è stato possibile recuperare clip-b.mp4, quindi non è stato scritto alcun file.',
    ],
  ])(
    'names the source it could not fetch by its file (%s), and downloads nothing',
    async (language, text) => {
      await i18n.changeLanguage(language);
      flow.exportProject.mockRejectedValue(new ExportSourceError('asset-b', 'unreachable'));

      await press('videoStudio.export.action');

      expect(alertText()).toBe(text);
      expect(api.getAsset).toHaveBeenCalledWith('asset-b');
      expect(downloadBlob).not.toHaveBeenCalled();
    },
  );

  it('names a source the renderer could not read, in the sound export too', async () => {
    flow.exportProjectAudio.mockRejectedValue(new ExportSourceError('asset-c', 'undecodable'));

    await press('videoStudio.export.audioAction');

    expect(alertText()).toBe(
      'The export stopped: the renderer could not read voce.m4a, so no file was written.',
    );
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it.each([
    ['whose row is gone too', () => Promise.reject(new Error('asset not found'))],
    ['whose row has no name', () => Promise.resolve(row('asset-b', ''))],
  ])('falls back to the asset id for a source %s', async (_why, answer) => {
    api.getAsset.mockImplementation(answer);
    flow.exportProject.mockRejectedValue(new ExportSourceError('asset-b', 'unreachable'));

    await press('videoStudio.export.action');

    expect(alertText()).toBe(
      'The export stopped: asset-b could not be fetched, so no file was written.',
    );
  });

  it('says nothing when the operator cancels while the name is read', async () => {
    let answer: ((asset: Asset) => void) | undefined;
    api.getAsset.mockImplementation(
      () =>
        new Promise<Asset>((resolve) => {
          answer = resolve;
        }),
    );
    flow.exportProject.mockRejectedValue(new ExportSourceError('asset-b', 'unreachable'));

    await press('videoStudio.export.action');
    expect(answer).toBeDefined();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.export.cancel') }));
      answer?.(row('asset-b', 'clip-b.mp4'));
      await turn();
    });

    expect(screen.getByRole('button', { name: i18n.t('videoStudio.export.action') })).toBeTruthy();
    expect(alertText()).toBeUndefined();
  });

  it('says a project with no sound has none to export', async () => {
    flow.exportProjectAudio.mockRejectedValue(new NoProjectAudioError());

    await press('videoStudio.export.audioAction');

    expect(alertText()).toBe(i18n.t('videoStudio.export.noAudio'));
  });

  it('reports any other failure with its own reason', async () => {
    flow.exportProject.mockRejectedValue(new Error('encoder gone'));

    await press('videoStudio.export.action');

    expect(alertText()).toBe('The export failed: encoder gone');
    expect(api.getAsset).not.toHaveBeenCalled();
  });
});
```

The `afterEach` wraps the language reset in `act`: the panel is still mounted there, a language change re-renders it, and outside `act` React logs a warning on every test. This is the pattern the cockpit's other i18n tests use.

- [ ] **Step 2: Run the test to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/VideoStudio_export.test.tsx`

Expected: `Tests  6 failed | 2 passed (8)`, measured in scratch against today's dialog and today's i18n:
- the four text failures show today's dialog passing the internal message through:
  - `Received: "The export failed: videoStudio: source asset-b is unreachable"` (en, and both fallbacks);
  - `Received: "Esportazione non riuscita: videoStudio: source asset-b is unreachable"` (it);
  - `Received: "The export failed: videoStudio: source asset-c is undecodable"`;
- the cancel case fails with `AssertionError: expected undefined to be defined`: today's dialog never reads a name.

The two that pass are the empty-mix sentence and the generic reason, which this task keeps.

- [ ] **Step 3: Add the two sentences, in both languages**

Re-read `web/src/i18n/resources.videoStudio.ts` now: other sessions edit i18n, and the anchors below must still be there.

In the en block, after:

```ts
      failed: 'The export failed: {{reason}}',
```

add:

```ts
      sourceUnreachable:
        'The export stopped: {{source}} could not be fetched, so no file was written.',
      sourceUndecodable:
        'The export stopped: the renderer could not read {{source}}, so no file was written.',
```

In the it block, after:

```ts
      failed: 'Esportazione non riuscita: {{reason}}',
```

add:

```ts
      sourceUnreachable:
        'Esportazione interrotta: non è stato possibile recuperare {{source}}, quindi non è stato scritto alcun file.',
      sourceUndecodable:
        'Esportazione interrotta: il renderer non è riuscito a leggere {{source}}, quindi non è stato scritto alcun file.',
```

- [ ] **Step 4: Pick the sentence in the dialog**

In `web/src/videoStudio/VideoStudio_export.tsx` make three edits.

1. The imports. Before `import { useEffect, useRef, useState } from 'react';` add:

```ts
import type { TFunction } from 'i18next';
```

After `import { useTranslation } from 'react-i18next';` add:

```ts
import { getAsset } from '../chat/attachments/api';
```

After `import { exportProjectAudio, NoProjectAudioError } from './videoflow_exportAudio';` add:

```ts
import { ExportSourceError } from './videoflow_media';
```

2. Replace `reason` (`:30-32`):

```ts
function reason(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
```

with:

```ts
/**
 * What a source is called where the operator sees it: its asset row's file name, the name the
 * library lists. The project file holds only the asset id, so the row is read here, once, after
 * the failure — a source added since the project loaded, or a clip's cleaned copy, has a row the
 * load never read. A row that does not answer, or answers with no name, leaves the id: a source
 * whose bytes are gone may have lost its row too.
 */
async function sourceName(assetId: string): Promise<string> {
  try {
    const name = (await getAsset(assetId)).file_name;
    return name === '' ? assetId : name;
  } catch {
    return assetId;
  }
}

/** What the operator reads when an export fails: the source that stopped it, by name, or the
 *  sound export's empty mix, or the error's own reason. */
async function failureText(t: TFunction, error: unknown): Promise<string> {
  if (error instanceof ExportSourceError) {
    const source = await sourceName(error.assetId);
    return error.failure === 'unreachable'
      ? t('videoStudio.export.sourceUnreachable', { source })
      : t('videoStudio.export.sourceUndecodable', { source });
  }
  if (error instanceof NoProjectAudioError) return t('videoStudio.export.noAudio');
  return t('videoStudio.export.failed', {
    reason: error instanceof Error ? error.message : String(error),
  });
}
```

3. Replace the `catch` body (`:68-75`):

```ts
      // An abort is the operator's own decision, not a failure to report back to them.
      if (!controller.signal.aborted) {
        setFailure(
          error instanceof NoProjectAudioError
            ? t('videoStudio.export.noAudio')
            : t('videoStudio.export.failed', { reason: reason(error) }),
        );
      }
```

with:

```ts
      // An abort is the operator's own decision, not a failure to report back to them — and it
      // can land while the failing source's name is read, so it is asked again after.
      const text = controller.signal.aborted ? undefined : await failureText(t, error);
      if (text !== undefined && !controller.signal.aborted) setFailure(text);
```

Both checks are load-bearing. With the second `!controller.signal.aborted` removed, "says nothing when the operator cancels while the name is read" fails (measured in scratch).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/VideoStudio_export.test.tsx src/i18n`

Expected: `Test Files  5 passed (5)`, `Tests  20 passed (20)`: this file's 8, plus the i18n suites, which include the en/it parity check and the static-key usage gate (both read the new `t('videoStudio.export.source…')` literals). Measured in scratch.

- [ ] **Step 6: Task gates**

1. `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio src/i18n`. Expected: no `FAIL` line. `VideoStudio.test.tsx` renders this panel and is untouched.
2. `MSYS_NO_PATHCONV=1 wsl bash $W/webcheck.sh src/videoStudio/VideoStudio_export.tsx src/videoStudio/__tests__/VideoStudio_export.test.tsx src/i18n/resources.videoStudio.ts`. Expected: `tsc` clean, `Found 0 warnings and 0 errors.`, prettier formatted.
3. `MSYS_NO_PATHCONV=1 wsl bash $W/cov.sh src/videoStudio/VideoStudio_export.tsx src/videoStudio/__tests__/VideoStudio_export.test.tsx src/videoStudio/__tests__/VideoStudio.test.tsx`. Expected: at least 85 in `% Stmts`, `% Branch` and `% Lines`. Measured in scratch: 95.12 / 96.87 / 94.59.
4. `wc -l web/src/videoStudio/VideoStudio_export.tsx web/src/i18n/resources.videoStudio.ts web/src/videoStudio/__tests__/VideoStudio_export.test.tsx`. Expected: all ≤ 600 (170, 434, 182, if no other session has added keys meanwhile).

CI note: this task adds nothing to Stryker. The dialog's choice is pinned by exact-text tests.

- [ ] **Step 7: Commit**

Write `$W/msg-t3.txt`:

```text
fix(web): name the source that stopped a Studio export by its file

When a source could not play, the dialog showed the error's internal
message ("The export failed: videoStudio: source asset-b is
unreachable"), and before the previous commit it showed nothing at all,
because a black MP4 downloaded as a success.

failureText turns ExportSourceError into one sentence per failure, in
English and Italian, naming the source by its file name and saying that
no file was written. The project file holds only the asset id, so the
name is read from the asset row once, after the failure; a row that does
not answer, or has no name, leaves the asset id. A cancel that lands
while the name is read still reports nothing. The page never learns the
HTTP status, so the sentence does not claim one. The new test reads the
texts through the real i18n bundle.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t3.txt web/src/videoStudio/VideoStudio_export.tsx web/src/videoStudio/__tests__/VideoStudio_export.test.tsx web/src/i18n/resources.videoStudio.ts`

Expected: `COMMIT_RC=0`.

---

### Task 4: The Studio names its project files `<slug>.aura-video.json`

**Files:**
- Create: `internal/assets/testdata/studio-project.json`. This is a real Studio project, shared by this task's test and Task 7's re-key tests.
- Modify: `web/src/videoStudio/projectStore.ts`:
  - the header `:16-18`;
  - a constant after `:39`;
  - `saveProject` `:69-71`.
- Modify: `web/src/videoStudio/__tests__/projectStore.test.ts`:
  - a fixture import after `:2`;
  - a test after `:183`;
  - a test after `:225`.

**Interfaces:**
- Consumes: `projectFileName(project, extension, name?)` (`projectStore.ts:54-65`), unchanged. It returns `` `${slug}.${extension}` ``.
- Produces:
  - `export const PROJECT_FILE_EXTENSION = 'aura-video.json'` from `web/src/videoStudio/projectStore.ts`. It has no leading dot, because `projectFileName` adds one. Plan B's sidecar imports it.
  - `saveProject(project, name?)` keeps its signature. Its file name becomes `<slug>.aura-video.json`.
  - `loadProject` is untouched, and still loads a `.json` project by asset id.
  - `internal/assets/testdata/studio-project.json`: a project the Studio's own parser loads with nothing missing. Task 7's Go check is held to it.

- [ ] **Step 1: Write the shared fixture**

Create `internal/assets/testdata/studio-project.json`. It is a full Studio project:
- a video, a still and a music source;
- a clip with both fades;
- a muted still joined to it by a crossfade;
- a title;
- a sound on a music lane.

It lives under the Go package that will read it (Task 7). The web test imports it across the tree, the way `src/chat/__tests__/sseAdapter.test.ts:9` already imports `internal/agui/testdata/golden-events.json`.

```json
{
  "id": "6f1c2a9e-4b7d-4e2a-9c51-0d3e8f7a1b24",
  "name": "Reel di Aura",
  "size": { "width": 1920, "height": 1080 },
  "fps": 30,
  "sources": [
    {
      "id": "src-clip",
      "assetId": "9b2f0c1e-7a34-4d5e-8f61-2c3b4a5d6e7f",
      "kind": "video",
      "duration": 12.5,
      "hasAudio": true,
      "size": { "width": 1920, "height": 1080 }
    },
    {
      "id": "src-still",
      "assetId": "1d4e5f60-8a9b-4c0d-9e1f-2a3b4c5d6e70",
      "kind": "image",
      "duration": 0,
      "size": { "width": 800, "height": 600 }
    },
    {
      "id": "src-music",
      "assetId": "3c5d7e9f-1a2b-4c3d-8e4f-5a6b7c8d9e0f",
      "kind": "audio",
      "duration": 95.2,
      "hasAudio": true,
      "size": { "width": 0, "height": 0 }
    }
  ],
  "video": [
    {
      "id": "clip-1",
      "sourceId": "src-clip",
      "duration": 4,
      "sourceStart": 1.25,
      "muted": false,
      "volume": 1,
      "fadeIn": true,
      "fadeOut": true
    },
    {
      "id": "clip-2",
      "sourceId": "src-still",
      "duration": 3,
      "sourceStart": 0,
      "muted": true,
      "junctionFromClipId": "clip-1",
      "junctionTransition": "crossfade",
      "junctionDuration": 0.5
    }
  ],
  "overlays": [
    {
      "id": "lane-1",
      "items": [
        {
          "id": "title-1",
          "kind": "text",
          "anchor": { "clipId": "clip-1", "offset": 0.5 },
          "duration": 2,
          "props": { "text": "Ciao", "color": "#FFFFFF", "position": [0.5, 0.25] }
        }
      ]
    }
  ],
  "audio": [
    {
      "id": "music",
      "items": [
        {
          "id": "sound-1",
          "sourceId": "src-music",
          "anchor": { "clipId": "clip-1", "offset": 0 },
          "sourceStart": 0,
          "duration": 7,
          "volume": 0.6,
          "muted": false,
          "fadeIn": 1,
          "fadeOut": 1
        }
      ]
    }
  ]
}
```

- [ ] **Step 2: Write the failing tests**

In `web/src/videoStudio/__tests__/projectStore.test.ts`, after `import type { Asset, PresignResponse } from '../../chat/attachments/types';` add:

```ts
import savedStudioProject from '../../../../internal/assets/testdata/studio-project.json';
```

In `describe('saveProject', …)`, after the test `presigns a .json document, uploads it and finalizes` (it ends `expect(api.finalizeAsset).toHaveBeenCalledWith('file-1');` and `});`), add:

```ts
  it('marks the file as a Studio project, which keeps it out of the document index', async () => {
    await saveProject(project());

    const request = api.presignAsset.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(request.file_name).toBe('A-film.aura-video.json');
    expect(uploaded.files[0]?.name).toBe('A-film.aura-video.json');
  });
```

The test asserts the literal name and not `PROJECT_FILE_EXTENSION`: a test that compares the constant with itself proves nothing.

In `describe('loadProject', …)`, after the test `round-trips a project exactly` (it ends `expect(loaded.missing).toEqual([]);` and `});`), add:

```ts
  it('loads the project file the server re-keys out of the document index', async () => {
    // internal/assets/studio_project_rekey.go moves a project saved before the Studio suffix only
    // when its own check accepts this file. The Studio's parser accepting the same file is what
    // keeps that check describing a project rather than a guess at one.
    serve(JSON.stringify(savedStudioProject));

    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.project.name).toBe('Reel di Aura');
    expect(loaded.missing).toEqual([]);
  });
```

- [ ] **Step 3: Run the tests to verify the right one fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/projectStore.test.ts`

Expected: `Tests  1 failed | 60 passed (61)`, measured in scratch. The failure is `AssertionError: expected 'A-film.json' to be 'A-film.aura-video.json'`.

The fixture test passes already, as it should: today's parser loads that project. That test can fail too. Measured in scratch with `"muted": true` removed from `clip-2`, the still clip, it fails with `Error: videoStudio: those bytes are not a project`, because the Studio refuses a still that is not muted. Task 7's Go check does not look at `muted`, so this test is what keeps the fixture a project the Studio really loads.

- [ ] **Step 4: Name the file with the marker**

In `web/src/videoStudio/projectStore.ts` make three edits.

1. Replace the header's first three lines (`:16-18`):

```ts
// projectStore.ts — the project as a file. It is a `.json` DOCUMENT uploaded through the same
// presign the chat attachments use, which is what puts it under `chat/`: the server files by
// modality (internal/assets/service.go `folderFor`), and `media/` is where the sources live.
```

with:

```ts
// projectStore.ts — the project as a file. It is a `.aura-video.json` DOCUMENT uploaded through
// the same presign the chat attachments use, which is what puts it under `chat/`: the server files
// by modality (internal/assets/service.go `folderFor`), and `media/` is where the sources live.
```

2. After `const PROJECT_MIME = 'application/json';` (`:39`) add:

```ts

/**
 * What a project file's name ends in: the Studio's marker, never a separate format. It still ends
 * in `.json`, the extension the upload allowlist accepts (internal/assets/limits.go), and the
 * object key keeps it whole (internal/objectstore `StudioProjectSuffix`), which is what the
 * ingest skips (services/ingest/source.py `STUDIO_PROJECT_PATTERN`): a project is the editor's
 * state, not a document anyone searches. A project saved before the marker was plain `.json`: the
 * server moves it to the marker once, at boot (internal/assets/studio_project_rekey.go), and it
 * loads either way, because `loadProject` reads by asset id and never looks at a name.
 */
export const PROJECT_FILE_EXTENSION = 'aura-video.json';

```

The block starts and ends with a blank line, so one blank line separates the new constant from `PROJECT_MIME` above it and from `/** Long enough to recognise the project…` (the line that was `:40`) below it.

3. In `saveProject`, replace (`:69-71`):

```ts
  const file = new File([JSON.stringify(project)], projectFileName(project, 'json', name), {
    type: PROJECT_MIME,
  });
```

with:

```ts
  const file = new File(
    [JSON.stringify(project)],
    projectFileName(project, PROJECT_FILE_EXTENSION, name),
    { type: PROJECT_MIME },
  );
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/projectStore.test.ts`

Expected: `Tests  61 passed (61)`, measured in scratch. The older `presigns a .json document` test still passes, because `/\.json$/` matches the new name.

- [ ] **Step 6: Task gates**

1. `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio`. Expected: no `FAIL` line.
2. `MSYS_NO_PATHCONV=1 wsl bash $W/webcheck.sh src/videoStudio/projectStore.ts src/videoStudio/__tests__/projectStore.test.ts`. Expected:
   - `tsc` is clean. A JSON import outside `src` type-checks the way the `sseAdapter.test.ts` precedent does;
   - `Found 0 warnings and 0 errors.`;
   - knip is silent: `PROJECT_FILE_EXTENSION` is used in its own file, and knip runs with `ignoreExportsUsedInFile`;
   - prettier formatted.
3. `MSYS_NO_PATHCONV=1 wsl bash $W/cov.sh src/videoStudio/projectStore.ts src/videoStudio/__tests__/projectStore.test.ts`. Expected: at least 85 in `% Stmts`, `% Branch` and `% Lines`. Measured in scratch: 96.34 / 93.14 / 98.68.
4. `wc -l web/src/videoStudio/projectStore.ts web/src/videoStudio/__tests__/projectStore.test.ts`. Expected: 457 and 546, both ≤ 600.

- [ ] **Step 7: Commit**

Write `$W/msg-t4.txt`:

```text
feat(web): name Studio project files <slug>.aura-video.json

A saved project is the editor's state, not a document anyone searches,
yet it is uploaded as a plain .json and indexed like one (PRD M1). The
Studio now marks the file with the suffix aura-video.json, exported as
PROJECT_FILE_EXTENSION for the render sidecar to share. The name still
ends in .json, which the upload allowlist accepts. Old projects keep
loading: loadProject reads by asset id and never looks at the name.

internal/assets/testdata/studio-project.json is a real Studio project,
loaded here through the Studio's own parser with nothing missing. The
server's re-key of old projects is held to the same file, so its check
cannot drift from what the Studio loads.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t4.txt internal/assets/testdata/studio-project.json web/src/videoStudio/projectStore.ts web/src/videoStudio/__tests__/projectStore.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 5: The ingest skips `**/*.aura-video.json`

**Files:**
- Create: `services/ingest/tests/fake_s3.py`, the bucket stand-in that `test_audit.py` held privately, now shared
- Modify: `services/ingest/tests/test_audit.py` (the whole file, below): it uses `fake_s3`
- Modify: `services/ingest/source.py`:
  - a constant after `:46`;
  - `path_matcher()` and `walk` at `:96-102`;
  - `expected_keys` at `:183-193`.
- Create: `services/ingest/tests/test_studio_projects.py`

**Interfaces:**
- Consumes: nothing from earlier tasks. The suffix spelling must match Task 4's `PROJECT_FILE_EXTENSION`, and Task 6's parity test enforces that.
- Produces, in `services/ingest/source.py`:
  - `STUDIO_PROJECT_PATTERN = "**/*.aura-video.json"`, on a line of its own, which Task 6's parity test reads with a regex;
  - `def path_matcher() -> PatternFilePathMatcher`, which `walk` and `expected_keys` both use.
- Produces, in `services/ingest/tests/fake_s3.py`:
  - `config(prefix="") -> source.S3Config`;
  - `install(monkeypatch, pages) -> None`.

- [ ] **Step 1: Share the bucket stand-in instead of importing another test's privates**

The new test needs the paginator stand-in that `test_audit.py` keeps as private helpers (`_config`, `_install_fake_s3`). A test module importing another test module's underscored names couples the two, so the stand-in moves into a module of its own. Its unused `requested` attribute is dropped: nothing reads it.

Create `services/ingest/tests/fake_s3.py`:

```python
"""A stand-in for the bucket listing the walker and the audit read, shared by their tests.

`install` answers `pages` to the paginator source.py asks its S3 session for; `config` is the
bucket binding the listing is read under.
"""

from ingest import source


class _FakePaginator:
    def __init__(self, pages):
        self._pages = pages

    def paginate(self, **_kwargs):
        return self._pages


class _FakeS3:
    def __init__(self, pages):
        self._pages = pages

    def get_paginator(self, _name):
        return _FakePaginator(self._pages)


def config(prefix=""):
    return source.S3Config(
        identity_id="11111111-1111-1111-1111-111111111111",
        endpoint="http://garage:3900", bucket="aura-bench",
        access_key="k", secret_key="s", region="garage", prefix=prefix,
    )


def install(monkeypatch, pages):
    client = _FakeS3(pages)
    monkeypatch.setattr(
        "ingest.source.sync_get_session", lambda: type("S", (), {"create_client": lambda *a, **k: client})()
    )
```

Replace the whole of `services/ingest/tests/test_audit.py` with the file below. The tests are unchanged; only their helpers come from `fake_s3`.

```python
"""The reconciliation that turns a lost document into a failure instead of a silence.

An ingest pass that drops a file still exits 0: CocoIndex catches the component failure,
prints "component build failed" and carries on. On 2026-08-09 two separate defects did
exactly that on the same 130-document corpus and both were found by comparing the bucket
against the index BY HAND. These tests cover that comparison without a daemon, because
the parts that can be wrong are pure: which keys count as expected, and how the index's
answer is parsed.
"""

import pytest

from ingest import arcade, source
from ingest.tests import fake_s3


def test_expected_keys_excludes_the_prefixes_the_walker_excludes(monkeypatch):
    # RESERVED_PATTERNS keeps Aura's own layout out of the pipeline, so counting those
    # objects here would invent a discrepancy on every single pass: they are never
    # supposed to produce a document row.
    fake_s3.install(monkeypatch, [{"Contents": [
        {"Key": "laws/statute.pdf"},
        {"Key": "identity/abcd/original"},
        {"Key": "share/token/thing.pdf"},
        {"Key": "laws/table.csv"},
    ]}])

    assert source.expected_keys(fake_s3.config()) == {"laws/statute.pdf", "laws/table.csv"}


def test_expected_keys_skips_folder_markers(monkeypatch):
    # A zero-byte key ending in "/" is what a UI writes so an empty prefix appears in a
    # listing. The walker never offers one to the pipeline, so expecting a row for it
    # invents a discrepancy that can never clear. MEASURED 2026-08-16: one "test/" marker
    # made every catch-up run exit 1.
    fake_s3.install(monkeypatch, [{"Contents": [
        {"Key": "test/"},
        {"Key": "test/statute.pdf"},
    ]}])

    assert source.expected_keys(fake_s3.config()) == {"test/statute.pdf"}


def test_expected_keys_still_counts_an_empty_file(monkeypatch):
    # The test is the trailing slash, not the size: an empty file a person uploaded is a
    # document, gets a row, and must still be audited.
    fake_s3.install(monkeypatch, [{"Contents": [{"Key": "vuoto.txt", "Size": 0}]}])

    assert source.expected_keys(fake_s3.config()) == {"vuoto.txt"}


def test_expected_keys_spans_pages(monkeypatch):
    fake_s3.install(monkeypatch, [
        {"Contents": [{"Key": "a.pdf"}]},
        {"Contents": [{"Key": "b.pdf"}]},
        {},
    ])

    assert source.expected_keys(fake_s3.config()) == {"a.pdf", "b.pdf"}


def test_indexed_source_keys_reads_the_rows(monkeypatch):
    captured = {}

    def fake_post(base_url, path, payload, auth, timeout_s):
        captured.update(base_url=base_url, path=path, payload=payload)
        return {"result": [{"source_key": "laws/a.pdf"}, {"source_key": "laws/b.csv"}]}

    monkeypatch.setattr("ingest.arcade._post", fake_post)

    keys = arcade.indexed_source_keys("http://arcadedb:2480", "mem_x", ("root", "pw"), 60.0)

    assert keys == {"laws/a.pdf", "laws/b.csv"}
    assert captured["path"] == "/api/v1/query/mem_x"
    assert arcade.DOCUMENT_TYPE in captured["payload"]["command"]


@pytest.mark.parametrize("body", [{}, {"result": []}, {"result": [{"source_key": None}]}])
def test_indexed_source_keys_treats_an_empty_index_as_empty_not_as_success(monkeypatch, body):
    # An empty answer must read as "nothing is indexed", never as "nothing is missing".
    # Returning a full set here would make the audit pass loudest exactly when the whole
    # pass has failed.
    monkeypatch.setattr("ingest.arcade._post", lambda *a, **k: body)

    assert arcade.indexed_source_keys("http://x", "mem_x", ("root", "pw"), 1.0) == set()


def test_the_audit_reports_the_objects_with_no_row(monkeypatch):
    # The whole point, expressed as the set difference it is: an object present in the
    # bucket and absent from the index is a document the pass lost.
    expected = {"laws/a.pdf", "laws/b.csv", "laws/c.pdf"}
    indexed = {"laws/a.pdf", "laws/c.pdf"}

    assert sorted(expected - indexed) == ["laws/b.csv"]
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ingest-pytest.sh ingest/tests/test_audit.py`

The first run creates the venv outside the repository. Expected: `9 passed`, against today's `source.py`. Measured in scratch.

`grep -rn "_install_fake_s3\|_config\b" services/ingest/tests` from Git Bash then prints nothing: no other test used those helpers.

- [ ] **Step 2: Write the failing test**

Create `services/ingest/tests/test_studio_projects.py`:

```python
"""A Video Studio project is never a document.

The Studio saves its editor state as `<slug>.aura-video.json`; the object key keeps that suffix
whole (Go objectstore.StudioProjectSuffix). The key is all the walker's matcher sees -- the real
filename rides in metadata only a HEAD reads -- so the suffix in the key is what keeps a project
out of the index, while any other `.json` the operator uploads is still a document.

These run CocoIndex's own PatternFilePathMatcher (globset semantics), so `**/` at the bucket
root is measured here rather than assumed.
"""

import pathlib

import pytest

from ingest import source
from ingest.tests import fake_s3


@pytest.mark.parametrize(
    "key",
    [
        "chat/019f8a2b-0000-7000-8000-000000000001.aura-video.json",
        "reel.aura-video.json",  # the bucket root: `**/` matches zero folders too
        "progetti/2026/reel.aura-video.json",
    ],
)
def test_a_studio_project_is_not_offered_to_the_pipeline(key):
    assert not source.path_matcher().is_file_included(pathlib.PurePosixPath(key))


@pytest.mark.parametrize(
    "key",
    [
        "chat/019f8a2b-0000-7000-8000-000000000002.json",
        "dati/listino.json",
        "reel.aura-video.json.bak",  # the marker must END the name
        "reel-aura-video.json",
    ],
)
def test_any_other_json_is_still_a_document(key):
    assert source.path_matcher().is_file_included(pathlib.PurePosixPath(key))


def test_the_reserved_prefixes_stay_excluded():
    matcher = source.path_matcher()
    assert not matcher.is_file_included(pathlib.PurePosixPath("identity/abcd/original"))
    assert not matcher.is_file_included(pathlib.PurePosixPath("share/token/thing.pdf"))


def test_the_walker_filters_with_the_same_matcher(monkeypatch):
    seen = {}

    def list_objects(client, bucket, *, prefix, path_matcher):
        seen["matcher"] = path_matcher
        return iter(())

    monkeypatch.setattr(source.amazon_s3, "list_objects", list_objects)
    source.walk(object(), fake_s3.config())

    assert not seen["matcher"].is_file_included(pathlib.PurePosixPath("chat/x.aura-video.json"))
    assert seen["matcher"].is_file_included(pathlib.PurePosixPath("chat/x.json"))


def test_the_audit_does_not_expect_a_project(monkeypatch):
    # The audit compares the bucket with the index: expecting a row for a project it never
    # indexes would print a MISSING line on every cycle that can never clear.
    fake_s3.install(monkeypatch, [{"Contents": [
        {"Key": "chat/a.aura-video.json"},
        {"Key": "chat/b.json"},
    ]}])

    assert source.expected_keys(fake_s3.config()) == {"chat/b.json"}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ingest-pytest.sh ingest/tests/test_studio_projects.py`

Expected: `10 failed`, measured in scratch:
- Eight fail with `AttributeError: module 'ingest.source' has no attribute 'path_matcher'`.
- The walker test fails with `AssertionError: assert not True`: today's matcher includes `chat/x.aura-video.json`.
- The audit test fails with `assert {'chat/a.aura...'chat/b.json'} == {'chat/b.json'}`: `expected_keys` still returns `chat/a.aura-video.json`.

- [ ] **Step 4: One matcher, excluding Studio projects**

In `services/ingest/source.py` make three edits.

1. Replace:

```python
RESERVED_PATTERNS = ("identity/**", "share/**")


@dataclasses.dataclass(frozen=True, slots=True)
```

with:

```python
RESERVED_PATTERNS = ("identity/**", "share/**")

# A Video Studio project: the editor's state, not a document anyone searches. It belongs to the
# person -- so it is NOT a reserved prefix, and the file manager still lists it -- but it is never
# indexed. The Studio names it `<slug>.aura-video.json` and the object key keeps that suffix whole
# (Go objectstore.StudioProjectSuffix, which TestStudioProjectSuffixMatchesTheCockpitAndTheIngest
# reads from this line): the filename itself rides in metadata this matcher never sees. `**/`
# matches at the bucket root too (globset semantics, CocoIndex PatternFilePathMatcher).
STUDIO_PROJECT_PATTERN = "**/*.aura-video.json"


@dataclasses.dataclass(frozen=True, slots=True)
```

2. Replace:

```python
def walk(client: object, config: S3Config) -> amazon_s3.S3Walker:
    return amazon_s3.list_objects(
        client,
        config.bucket,
        prefix=config.prefix,
        path_matcher=PatternFilePathMatcher(excluded_patterns=list(RESERVED_PATTERNS)),
    )
```

with:

```python
def path_matcher() -> PatternFilePathMatcher:
    """What the pipeline reads: everything but Aura's own layout and Studio projects.

    One matcher for walk() and expected_keys(): the audit compares against exactly what the
    walker fed the pipeline, and two filters would manufacture a discrepancy or hide a real one.
    """
    return PatternFilePathMatcher(
        excluded_patterns=[*RESERVED_PATTERNS, STUDIO_PROJECT_PATTERN]
    )


def walk(client: object, config: S3Config) -> amazon_s3.S3Walker:
    return amazon_s3.list_objects(
        client,
        config.bucket,
        prefix=config.prefix,
        path_matcher=path_matcher(),
    )
```

3. In `expected_keys`, replace:

```python
    Same bucket, same prefix and the same reserved-prefix exclusion as walk(), because
    the point is to compare against what walk() fed the pipeline -- a different filter
    here would manufacture a discrepancy or hide a real one.
```

with:

```python
    Same bucket, same prefix and the same path_matcher() as walk(), because the point is
    to compare against what walk() fed the pipeline.
```

and replace:

```python
    matcher = PatternFilePathMatcher(excluded_patterns=list(RESERVED_PATTERNS))
```

with:

```python
    matcher = path_matcher()
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ingest-pytest.sh ingest/tests/test_studio_projects.py ingest/tests/test_audit.py ingest/tests/test_source_file_name.py`

Expected: `36 passed`, measured in scratch. `test_audit.py` and `test_source_file_name.py` exercise `walk`, `expected_keys` and the metadata name path, so they show the refactor changes nothing else.

The rest of `services/ingest/tests` does not run in this venv. Its modules need what only the image carries: `cocoindex[neo4j]`, `iscc_tika`, and `AURA_EMBED_SPACE` (measured in scratch: 10 collection errors, all of those three kinds). CI's `make ingest-test` job runs the whole suite inside the `aura-ingest` image, with the same CocoIndex 1.0.24 (`.github/workflows/ci.yml:963`). There is no mutation gate for Python.

- [ ] **Step 6: Task gates**

1. `wc -l services/ingest/source.py services/ingest/tests/test_studio_projects.py services/ingest/tests/fake_s3.py services/ingest/tests/test_audit.py`. Expected: 241, 73, 38 and 94.
2. `git -C /d/Aura status --short services/ingest` from Git Bash. Expected: only these four files. The pytest script writes no bytecode and no cache into the tree.

- [ ] **Step 7: Commit**

Write `$W/msg-t5.txt`:

```text
fix(ingest): skip Studio project files by their key

A saved Video Studio project was walked and indexed like any other JSON
(PRD M1). The walker's matcher reads only the object key -- the real
name rides in S3 metadata -- so the exclusion has to be on the key's
suffix: **/*.aura-video.json, which CocoIndex's globset matcher applies
at the bucket root too (measured by the new test). walk() and
expected_keys() now share one path_matcher(), so the audit never
expects a row for a project it skips. Any other .json is still indexed.

The bucket stand-in test_audit.py kept private moves to
tests/fake_s3.py, so the new test does not import another test's
underscored helpers.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t5.txt services/ingest/source.py services/ingest/tests/test_studio_projects.py services/ingest/tests/fake_s3.py services/ingest/tests/test_audit.py`

Expected: `COMMIT_RC=0`.

---

### Task 6: The object key keeps the Studio suffix whole

**Files:**
- Modify: `internal/objectstore/asset_placement.go`:
  - `AssetKey`'s doc, at `:71-72` and `:88-89`;
  - a constant before `:94`;
  - `assetExtension`'s doc and first line `:94-99`.
- Create: `internal/objectstore/studio_project_test.go`
- Modify: `scripts/coverage_package_policy.json:57`

**Interfaces:**
- Consumes (the parity test reads both from disk):
  - `PROJECT_FILE_EXTENSION` from Task 4;
  - `STUDIO_PROJECT_PATTERN` from Task 5.
- Produces `const StudioProjectSuffix = ".aura-video.json"` in package `objectstore`, with a leading dot.
  - `AssetKey(id, "x.aura-video.json", FolderChat)` returns `"chat/" + id + ".aura-video.json"`.
  - Every other name keeps today's key.
  - The `id` in a key is whatever the caller minted (Presign mints a fresh uuid), never the asset row's id. Nothing may read an asset id back out of a key: Task 9 measures the two differing on the lab VM.

- [ ] **Step 1: Write the failing test**

Create `internal/objectstore/studio_project_test.go`. Both parity patterns end in `\s*$` rather than a bare `$`. In Go's multiline mode `$` matches only before `\n`, so a line that ends in a stray space, or in a CRLF from a Windows checkout, would otherwise read as "no longer declares the suffix".

```go
package objectstore

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// A Video Studio project is the editor's state, not a document anyone searches. The ingest
// sidecar can tell it apart by path alone -- its matcher sees keys, never the metadata that
// carries the name -- so the key keeps the Studio's suffix whole, while every other name still
// yields one short extension and never the name itself.
func TestAssetKeyKeepsTheStudioProjectSuffixWhole(t *testing.T) {
	for name, want := range map[string]string{
		"Reel di Aura.aura-video.json":       "chat/asset-2.aura-video.json",
		"REEL.AURA-VIDEO.JSON":               "chat/asset-2.aura-video.json",
		`C:\progetti\reel.aura-video.json`:   "chat/asset-2.aura-video.json",
		"reel.json":                          "chat/asset-2.json",
		"reel.aura-video.json.bak":           "chat/asset-2.bak",
		"reel-aura-video.json":               "chat/asset-2.json",
		"Contratto riservato.aura-video.pdf": "chat/asset-2.pdf",
	} {
		if got := AssetKey("asset-2", name, FolderChat); got != want {
			t.Errorf("AssetKey(%q) = %q, want %q", name, got, want)
		}
	}
}

// The suffix is spelled three times in three languages: here, in the cockpit that names the
// file, and in the ingest pattern that skips it. A drift in any one of them silently indexes
// every project saved after it, so the other two are read and compared, the way
// internal/embeddings/prefix_parity_test.go holds the ingest's document prefix to Go's.
func TestStudioProjectSuffixMatchesTheCockpitAndTheIngest(t *testing.T) {
	for _, spelling := range []struct {
		file    string
		pattern string
		dot     string
	}{
		{
			file:    filepath.Join("..", "..", "web", "src", "videoStudio", "projectStore.ts"),
			pattern: `(?m)^export const PROJECT_FILE_EXTENSION = '([^']*)';\s*$`,
			dot:     ".",
		},
		{
			file:    filepath.Join("..", "..", "services", "ingest", "source.py"),
			pattern: `(?m)^STUDIO_PROJECT_PATTERN = "\*\*/\*([^"]*)"\s*$`,
		},
	} {
		source, err := os.ReadFile(spelling.file)
		if err != nil {
			t.Fatalf("read %s: %v", spelling.file, err)
		}
		match := regexp.MustCompile(spelling.pattern).FindSubmatch(source)
		if match == nil {
			t.Fatalf("%s no longer declares the suffix on one line matching %s", spelling.file, spelling.pattern)
		}
		if got := spelling.dot + string(match[1]); got != StudioProjectSuffix {
			t.Errorf("%s spells the suffix %q; StudioProjectSuffix is %q", spelling.file, got, StudioProjectSuffix)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./internal/objectstore/`

Expected: `vet` fails with `vet: internal/objectstore/studio_project_test.go:58:53: undefined: StudioProjectSuffix`, and `race` fails to compile for the same reason. Measured in scratch.

- [ ] **Step 3: Keep the suffix in the key**

In `internal/objectstore/asset_placement.go` make three edits.

1. `AssetKey`'s doc says what the id in a key is, so that no caller mistakes it for the row's id. Replace:

```go
// it: "<folder>/<assetID><.ext>" in the identity's own bucket — media/ for pictures and
// clips, chat/ for documents (see AssetFolder).
```

with:

```go
// it: "<folder>/<assetID><ext>" in the identity's own bucket — media/ for pictures and
// clips, chat/ for documents (see AssetFolder). assetID is whatever the caller minted for the
// object: Presign mints a fresh uuid, and the asset row's own id is the database's, so the two
// differ and an asset id is never read back out of a key.
```

2. Replace:

```go
// extractor routes on. The name rides in metadata instead — see PlaceAsset, which is what
// callers should use.
```

with:

```go
// extractor routes on, and the Studio's ".aura-video.json" is what the ingest's matcher skips.
// The name rides in metadata instead — see PlaceAsset, which is what callers should use.
```

3. Replace:

```go
// assetExtension returns a lowercase ".ext", or "" when the name has none.
//
// Bounded and character-checked because it is caller-supplied: a chat client or a Telegram
// message can send any name, and this fragment becomes part of an object key.
func assetExtension(name string) string {
	ext := strings.ToLower(path.Ext(baseName(name)))
```

with:

```go
// StudioProjectSuffix ends the name of a Video Studio project file
// (web/src/videoStudio/projectStore.ts PROJECT_FILE_EXTENSION). It is the one compound
// extension a key keeps whole: a project is the editor's state, not a document anyone
// searches, and the ingest sidecar skips it by path (services/ingest/source.py
// STUDIO_PROJECT_PATTERN) because the key is all its matcher sees. Like ".pdf", it names a
// kind of file and never the file.
const StudioProjectSuffix = ".aura-video.json"

// assetExtension returns the lowercase extension a key keeps: StudioProjectSuffix whole for a
// Studio project, otherwise the last ".ext", or "" when the name has none.
//
// Bounded and character-checked because it is caller-supplied: a chat client or a Telegram
// message can send any name, and this fragment becomes part of an object key.
func assetExtension(name string) string {
	base := strings.ToLower(baseName(name))
	if strings.HasSuffix(base, StudioProjectSuffix) {
		return StudioProjectSuffix
	}
	ext := path.Ext(base)
```

The rest of the function (the length bound and the character check) is unchanged. The whole suffix is a constant, so it cannot carry a caller's characters.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./internal/objectstore/`

Expected, measured in scratch:
- `gofmt` lists no file;
- `vet` is silent;
- `race` prints `ok  	github.com/chetto1983/aura/internal/objectstore`;
- `unit tier covered/total = 419/601`;
- `golangci-lint` prints `0 issues.`

Then prove the parity test can fail. Temporarily change the cockpit's line to `export const PROJECT_FILE_EXTENSION = 'aura-video.jsn';` and run the gates again. Expected, measured in scratch: `--- FAIL: TestStudioProjectSuffixMatchesTheCockpitAndTheIngest` with `../../web/src/videoStudio/projectStore.ts spells the suffix ".aura-video.jsn"; StudioProjectSuffix is ".aura-video.json"`. Restore the line, then confirm from Git Bash that `git -C /d/Aura diff --stat web/src/videoStudio/projectStore.ts` shows only Task 4's change.

- [ ] **Step 5: Re-pin the package's coverage baseline**

`internal/objectstore` is a baseline-mode package: 456 of 598 statements covered. `scripts/coverage_package_gate.py:152-160` fails when the denominator changes. The change adds 3 statements, all covered by the new test. Measured in scratch, the unit tier went from 416/598 to 419/601, and the file `asset_placement.go` from 34/36 to 37/39.

In `scripts/coverage_package_policy.json`, replace:

```json
    "github.com/chetto1983/aura/internal/objectstore": {"mode": "baseline", "covered_statements": 456, "total_statements": 598},
```

with:

```json
    "github.com/chetto1983/aura/internal/objectstore": {"mode": "baseline", "covered_statements": 459, "total_statements": 601},
```

The CI coverage gate measures the full tag matrix. If it reports `statement denominator changed from 601 to N (now C/N …)`, another commit has changed this package in the meantime. Re-pin to exactly the `C/N` it prints, and name that commit in the message.

- [ ] **Step 6: Commit**

Write `$W/msg-t6.txt`:

```text
fix(objectstore): keep the Studio project suffix whole in an object key

The ingest's matcher sees only the object key, and the key carries the
file's extension but never its name. assetExtension kept path.Ext, so
"reel.aura-video.json" was stored as chat/<id>.json and the ingest could
not tell a Studio project from any other JSON document.

StudioProjectSuffix is the one compound extension a key keeps whole; it
names a kind of file, never the file. A parity test reads the cockpit's
PROJECT_FILE_EXTENSION and the ingest's STUDIO_PROJECT_PATTERN and fails
if either drifts. AssetKey's doc now says the id in a key is the one the
caller minted, not the asset row's. internal/objectstore's coverage
baseline is re-pinned from 456/598 to 459/601: three new statements,
all covered.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t6.txt internal/objectstore/asset_placement.go internal/objectstore/studio_project_test.go scripts/coverage_package_policy.json`

Expected: `COMMIT_RC=0`. The pre-commit gofmt, vet and lint hooks run on the staged Go files.

---

### Task 7: Projects saved before Plan A leave the document index once, at boot

**Files:**
- Create: `internal/assets/studio_project_rekey.go`
- Create: `internal/assets/studio_project_rekey_test.go`
- Modify: `internal/assets/service_test.go:439-462`. The fake `ListRecent` honours `beforeID`, as the query does.
- Modify: `internal/assets/service_delete_test.go:64-71`. `captureWarnings` becomes `captureLogs(t, level)`.
- Create: `cmd/aura/studio_project_rekey_wiring.go`
- Create: `cmd/aura/studio_project_rekey_wiring_test.go`
- Modify: `cmd/aura/serve.go:273`: one line after it

**Interfaces:**
- Consumes:
  - `objectstore.StudioProjectSuffix` (Task 6);
  - `internal/assets/testdata/studio-project.json` (Task 4);
  - `(*Browser).transfer(ctx, identityID, source, destination string, removeSource bool, name string) (string, error)` (`internal/assets/filemanager_ops.go:125`): the file manager's own copy → `Rows.Relocate` → delete, which Rename and Move already use;
  - `(*Browser).resolveStore(ctx, identityID) (objectstore.Store, string, error)` (`internal/assets/browser.go:103`);
  - `Service.Store.ListRecent(ctx, identityID, beforeID, modalities, limit)` (`internal/assets/store.go:120`). It is called on the store, not the service, because `Service.ListRecent` clamps a page to 48;
  - `IdentityLister` (`internal/assets/delete_sweep.go:21`);
  - in `cmd/aura`: `identityRoster` (`serve_memory_backfill.go:28`) and `buildFileBrowser` (`file_browser_wiring.go:19`).
- Produces:
  - `type StudioProjectRekey struct { Assets *Service; Files *Browser; Identities IdentityLister }`;
  - `func (r StudioProjectRekey) Run(ctx context.Context) (int, error)`: how many projects it moved, and a joined error naming every asset it could not move;
  - in `cmd/aura`: `buildStudioProjectRekey(chat *chatEnv, objects objectstore.Store) *assets.StudioProjectRekey` and `rekeyStudioProjects(ctx context.Context, rekey *assets.StudioProjectRekey)`.

**Why this task exists.** Tasks 4–6 keep every project saved from now on out of the index. A project saved before them sits at `chat/<uuid>.json`, and the ingest indexes it as a document. Saving it again does not help, because every save is a new asset (spec ruling 3). The operator ruled (2026-10-01) that these projects are re-keyed once, through the existing relocation path. When the old key vanishes from the bucket, the ingest drops it the way it drops any deleted document. Task 0 counts the candidates on the lab VM first, read-only. Task 9 proves on the VM that they left the index.

**The identification rule.** A row is touched only when it passes every one of the conjuncts below.

The row conjuncts, which come from `aura.assets` and cost no read:
1. `source_kind = web`: the cockpit's upload door. Telegram and the tools upload through other doors.
2. `thread_id = ''`: `saveProject` presigns with no thread.
3. `scope = thread`: Presign's default (`service.go:114-116`). A file filed in the library is the operator's.
4. `modality = document`: `ListRecent` is asked for documents only.
5. `mime_type = application/json`.
6. `file_name` matches `^[A-Za-z0-9-]{1,60}\.json$`: what `projectFileName` writes, a slug or the project's uuid.
7. `object_key` matches `^chat/<uuid>\.json$`: Presign's minted uuid, then the one extension `assetExtension` kept. A name the file manager gave (`chat/Reel.json`) or a `media/` key does not match.

The object conjuncts, which cost one bounded read:

8. The object lives in the bucket the file manager moves in (its per-identity resolver).
9. The object is at most 4 MiB. A project is well under a kilobyte per clip.
10. The bytes have the Studio's saved shape, typed: `id`, `name`, a positive `fps`, a `size` of positive width and height, and `sources`, `video` and `overlays` lists. Every source has an `id`, an `assetId` and a kind of video, image or audio. Every clip has an `id`, a positive `duration`, and a `sourceId` naming a video or image source.

No one conjunct is enough, and the row conjuncts are not enough together either. An operator can attach `listino.json` from the same cockpit to a new chat that has no thread yet, and that row passes 1–7. Only the content check (10) tells it apart, and no other JSON passes it.

The content check alone would not be enough either. A project file the operator uploaded on purpose, to the library or from Telegram, is the operator's document to index. The row conjuncts limit the move to what `saveProject` wrote.

What this rule cannot do: the Go check (10) is a narrower port of the Studio's own parser (`projectStore.ts` `hasProjectShape` and `referencesHold`). It does not look at `muted` on a still, for example. So it can accept a file the Studio would refuse. That is harmless: such a row still passed 1–7, so it is a broken Studio save, and moving a broken save out of the index loses nothing. It cannot refuse a file the Studio loads. Task 4's test loads `testdata/studio-project.json` through the Studio's parser, and this task's tests accept the same file.

**Placement, idempotence, logging.**
- **Placement:** at boot, right after the asset service is built, the way `reconcileArcadeMemoryTenants` (`serve.go:243`) repairs what an older build left behind. Boot is the one place every install passes, and the appliance is updated by its updater only, with no operator running one-off commands. The nightly sweeper (`DeleteSweep`, 02:30) was rejected, because this pass runs once and not every night.
- **Not fatal:** a failure is a warning, and the boot continues. A project left indexed costs search noise, not data, and the next start tries again.
- **Idempotent:** a moved row's key ends in `.aura-video.json` and no longer matches conjunct 7, so a second run moves nothing. The test runs it twice.
- **Logged:** one Info line per move (`asset_id`, `from`, `to`). One Info line per refusal after the row rule passed (`asset_id`, `key`, `reason`). One boot summary: Info `aura serve: studio project re-key done moved=N`, or Warn `…left projects in the index` with the error.

**A known edge, not fixed here.** `transfer` copies, relocates the row, then deletes the old object. If that last delete fails, the row already points at the new key. The old object stays in the bucket, still indexed, and the error names it. The next start will not retry it, because the row no longer matches. The operator can delete the orphan from the file manager, which lists it by key. Task 9 reads the daemon's log for any `remove chat/…` error.

- [ ] **Step 1: Let the fakes answer what the store answers**

The re-key reads every document page by page, and today's fake `ListRecent` ignores `beforeID`. A paging bug would pass against it. In `internal/assets/service_test.go`, replace the fake's `ListRecent` (`:439-462`):

```go
func (s *fakeAssetStore) ListRecent(
	_ context.Context, identityID, beforeID string, modalities []Modality, limit int,
) ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRecentLimit = limit
	s.lastRecentModalities = modalities
	s.lastRecentBefore = beforeID
	var out []Asset
	for _, asset := range s.assets {
		if asset.IdentityID != identityID || !slices.Contains(modalities, asset.Modality) ||
			!usableAssetStatuses[asset.Status] || !asset.DeletedAt.IsZero() {
			continue
		}
		out = append(out, asset)
	}
	// The query orders by created_at DESC and only then applies the LIMIT; a map's iteration
	// order would otherwise make this fake answer a different question than the store does.
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
```

with the following. It is the query's `(created_at, id) DESC` order and its `before_id` rule (`internal/db/queries/assets.sql:47-62`): a `before_id` the owner does not hold yields an empty page.

```go
func (s *fakeAssetStore) ListRecent(
	_ context.Context, identityID, beforeID string, modalities []Modality, limit int,
) ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRecentLimit = limit
	s.lastRecentModalities = modalities
	s.lastRecentBefore = beforeID
	// A before_id the owner does not hold compares as NULL in the query, so the page is empty.
	before, paged := s.assets[beforeID]
	if beforeID != "" && (!paged || before.IdentityID != identityID) {
		return nil, nil
	}
	var out []Asset
	for _, asset := range s.assets {
		if asset.IdentityID != identityID || !slices.Contains(modalities, asset.Modality) ||
			!usableAssetStatuses[asset.Status] || !asset.DeletedAt.IsZero() ||
			(paged && !newerFirst(before, asset)) {
			continue
		}
		out = append(out, asset)
	}
	// The query orders by (created_at, id) DESC and only then applies the LIMIT; a map's
	// iteration order would otherwise make this fake answer a different question than the store.
	sort.Slice(out, func(i, j int) bool { return newerFirst(out[i], out[j]) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// newerFirst is the query's (created_at, id) DESC order: whether a comes before b.
func newerFirst(a, b Asset) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}
```

In `internal/assets/service_delete_test.go`, the re-key's tests read Info lines, and today's helper captures warnings only. Replace `captureWarnings` (`:64-71`):

```go
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}
```

with:

```go
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	return captureLogs(t, slog.LevelWarn)
}

// captureLogs sends the default logger's records at level and above to the returned buffer for
// the rest of the test.
func captureLogs(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/assets/studio_project_rekey_test.go`. `identityList` and `failingIdentities` already exist (`delete_sweep_test.go:13-17`), and so do `requireRowAt` (`filemanager_move_test.go:51`) and `newAssetServiceTestRig` (`service_test.go:306`).

```go
package assets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

// The cockpit's own loadProject accepts this file (web/src/videoStudio/__tests__/projectStore.test.ts,
// "loads the project file the server re-keys"), so the Go check and the Studio's parser agree on
// a real project, not on two copies of an assumption.
const studioProjectFixture = "testdata/studio-project.json"

const (
	rekeyOwner     = "owner-1"
	legacyKey      = "chat/0b7c5e1a-2f3d-4c8e-9a6b-1d2e3f4a5b6c.json"
	rekeyedKey     = "chat/0b7c5e1a-2f3d-4c8e-9a6b-1d2e3f4a5b6c.aura-video.json"
	legacyFileName = "Reel-di-Aura.json"
)

func studioProjectBytes(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(studioProjectFixture)
	if err != nil {
		t.Fatalf("read %s: %v", studioProjectFixture, err)
	}
	return body
}

type rekeyRig struct {
	rekey   StudioProjectRekey
	rows    *fakeAssetStore
	objects objectstore.Store
}

func newRekeyRig(t *testing.T) rekeyRig {
	t.Helper()
	svc, rows := newAssetServiceTestRig(t, Limits{})
	return rekeyRig{
		rekey: StudioProjectRekey{
			Assets:     svc,
			Files:      &Browser{Objects: svc.Objects, SharedBucket: svc.Bucket, Rows: rows},
			Identities: identityList{rekeyOwner},
		},
		rows:    rows,
		objects: svc.Objects,
	}
}

// studioSave is the row projectStore.ts saveProject left before aura-video-mcp Plan A: a JSON
// document from the cockpit with no thread, named <slug>.json, at chat/<the presign's uuid>.json.
func studioSave(id string, created time.Time) Asset {
	return Asset{
		ID: id, IdentityID: rekeyOwner, SourceKind: SourceWeb, Scope: ScopeThread,
		Modality: ModalityDocument, Status: StatusComplete, MIMEType: "application/json",
		FileName: legacyFileName, ObjectBucket: "asset-test", ObjectKey: legacyKey, CreatedAt: created,
	}
}

// store puts the row and, unless body is nil, its bytes, the way a finalized upload leaves them.
func (r rekeyRig) store(t *testing.T, asset Asset, body []byte) {
	t.Helper()
	if body != nil {
		asset.SizeBytes = int64(len(body))
		if _, err := r.objects.Put(context.Background(), assetRef(asset),
			bytes.NewReader(body), objectstore.PutOptions{Size: asset.SizeBytes}); err != nil {
			t.Fatalf("Put %s: %v", asset.ObjectKey, err)
		}
	}
	r.rows.mu.Lock()
	r.rows.assets[asset.ID] = asset
	r.rows.mu.Unlock()
}

func (r rekeyRig) bytesAt(t *testing.T, bucket, key string) []byte {
	t.Helper()
	body, _, err := r.objects.Get(context.Background(), objectstore.ObjectRef{Bucket: bucket, Key: key})
	if objectstore.IsNotFound(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("Get %s: %v", key, err)
	}
	defer func() { _ = body.Close() }()
	read, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return read
}

// A project saved before Plan A sits at chat/<uuid>.json, where the ingest indexes it as a
// document; saving it again only adds a new asset. Moved once to the Studio's suffix, the old key
// vanishes from the bucket, which is how the ingest drops a document. The row keeps its id, so
// every link to it still loads, and takes the name a save would give it today.
func TestStudioProjectRekeyMovesAnOldStudioProjectOutOfTheIndex(t *testing.T) {
	logs := captureLogs(t, slog.LevelInfo)
	rig := newRekeyRig(t)
	project := studioProjectBytes(t)
	rig.store(t, studioSave("asset-project", time.Now()), project)

	moved, err := rig.rekey.Run(context.Background())
	if err != nil || moved != 1 {
		t.Fatalf("Run = %d, %v; want 1, nil", moved, err)
	}
	requireRowAt(t, rig.rows, "asset-project", rekeyedKey, "Reel-di-Aura.aura-video.json")
	if got := rig.bytesAt(t, "asset-test", rekeyedKey); !bytes.Equal(got, project) {
		t.Fatalf("bytes at %s = %q, want the project", rekeyedKey, got)
	}
	if got := rig.bytesAt(t, "asset-test", legacyKey); got != nil {
		t.Fatalf("the old key still holds %d bytes: the ingest would keep it", len(got))
	}
	for _, want := range []string{"re-keyed a Studio project", "asset_id=asset-project", "to=" + rekeyedKey} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log lacks %q:\n%s", want, logs.String())
		}
	}

	moved, err = rig.rekey.Run(context.Background())
	if err != nil || moved != 0 {
		t.Fatalf("second Run = %d, %v; want 0, nil: the re-key is once", moved, err)
	}
	requireRowAt(t, rig.rows, "asset-project", rekeyedKey, "Reel-di-Aura.aura-video.json")
}

// An operator's own JSON is never touched. Each row below differs from a pre-Plan-A Studio save
// in exactly one respect, so each conjunct of the rule is shown to hold on its own.
func TestStudioProjectRekeyLeavesEveryOtherJSONWhereItIs(t *testing.T) {
	notAProject := []byte(`{"id":"settings","name":"export settings","fps":30,"size":{"width":1920,"height":1080}}`)
	for _, tc := range []struct {
		name  string
		edit  func(*Asset)
		bytes func(project []byte) []byte
		// logged: the row looked like a Studio save, so leaving it is a decision worth a line.
		logged bool
	}{
		{name: "attached in a chat thread", edit: func(a *Asset) { a.ThreadID = "thread-1" }},
		{name: "sent from Telegram", edit: func(a *Asset) { a.SourceKind = SourceTelegram }},
		{name: "filed in the library", edit: func(a *Asset) { a.Scope = ScopeLibrary }},
		{name: "not a document", edit: func(a *Asset) { a.Modality = ModalityImage }},
		{name: "not JSON by its type", edit: func(a *Asset) { a.MIMEType = "text/plain" }},
		{name: "a name the Studio never writes", edit: func(a *Asset) { a.FileName = "Reel di Aura.json" }},
		{name: "a key the file manager named", edit: func(a *Asset) { a.ObjectKey = "chat/Reel-di-Aura.json" }},
		{name: "under media/", edit: func(a *Asset) { a.ObjectKey = "media/0b7c5e1a-2f3d-4c8e-9a6b-1d2e3f4a5b6c.json" }},
		{name: "in another bucket", edit: func(a *Asset) { a.ObjectBucket = "someone-elses" }, logged: true},
		{name: "JSON that is not a project", bytes: func([]byte) []byte { return notAProject }, logged: true},
		{name: "larger than any project", bytes: func(project []byte) []byte {
			return append(project, bytes.Repeat([]byte(" "), studioProjectMaxBytes)...)
		}, logged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t, slog.LevelInfo)
			rig := newRekeyRig(t)
			asset := studioSave("asset-json", time.Now())
			if tc.edit != nil {
				tc.edit(&asset)
			}
			body := studioProjectBytes(t)
			if tc.bytes != nil {
				body = tc.bytes(body)
			}
			rig.store(t, asset, body)

			moved, err := rig.rekey.Run(context.Background())
			if err != nil || moved != 0 {
				t.Fatalf("Run = %d, %v; want 0, nil", moved, err)
			}
			requireRowAt(t, rig.rows, "asset-json", asset.ObjectKey, asset.FileName)
			if got := rig.bytesAt(t, asset.ObjectBucket, asset.ObjectKey); !bytes.Equal(got, body) {
				t.Fatalf("the object at %s changed", asset.ObjectKey)
			}
			if left := strings.Contains(logs.String(), "left a JSON document where it is"); left != tc.logged {
				t.Fatalf("logged the decision = %v, want %v:\n%s", left, tc.logged, logs.String())
			}
		})
	}
}

// The identity's documents are read a page at a time, newest first, and an old project lies
// behind every newer document; stopping at the first page would leave exactly the oldest ones
// indexed.
func TestStudioProjectRekeyReadsPastTheFirstPage(t *testing.T) {
	rig := newRekeyRig(t)
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	rig.store(t, studioSave("asset-project", start), studioProjectBytes(t))
	for i := range 2*rekeyPage + 1 {
		rig.store(t, Asset{
			ID: fmt.Sprintf("asset-pdf-%03d", i), IdentityID: rekeyOwner, SourceKind: SourceWeb,
			Modality: ModalityDocument, Status: StatusComplete, MIMEType: "application/pdf",
			FileName: "fattura.pdf", ObjectBucket: "asset-test", ObjectKey: fmt.Sprintf("chat/fattura-%03d.pdf", i),
			CreatedAt: start.Add(time.Duration(i+1) * time.Minute),
		}, nil)
	}

	moved, err := rig.rekey.Run(context.Background())
	if err != nil || moved != 1 {
		t.Fatalf("Run = %d, %v; want 1, nil", moved, err)
	}
	requireRowAt(t, rig.rows, "asset-project", rekeyedKey, "Reel-di-Aura.aura-video.json")
}

// A project whose bytes cannot be read is named in the error and retried at the next start; the
// projects after it are still moved.
func TestStudioProjectRekeyGoesOnPastAProjectItCannotRead(t *testing.T) {
	rig := newRekeyRig(t)
	lost := studioSave("asset-lost", time.Now())
	lost.ObjectKey = "chat/7e8f9a0b-1c2d-4e3f-8a4b-5c6d7e8f9a0b.json"
	lost.SizeBytes = 512
	rig.store(t, lost, nil)
	rig.store(t, studioSave("asset-project", time.Now().Add(-time.Hour)), studioProjectBytes(t))

	moved, err := rig.rekey.Run(context.Background())
	if moved != 1 || err == nil || !strings.Contains(err.Error(), "asset-lost") {
		t.Fatalf("Run = %d, %v; want 1 and an error naming asset-lost", moved, err)
	}
	requireRowAt(t, rig.rows, "asset-project", rekeyedKey, "Reel-di-Aura.aura-video.json")
	requireRowAt(t, rig.rows, "asset-lost", lost.ObjectKey, legacyFileName)

	rig.rekey.Identities = failingIdentities{err: errors.New("identity store down")}
	if _, err := rig.rekey.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "identity store down") {
		t.Fatalf("Run with no identities = %v, want the lister's error", err)
	}
}

// The content check is the conjunct no other JSON passes: the Studio's container shape, typed,
// with every clip naming a source it can draw. A file the Studio's own parser would refuse is not
// moved either way; refused here, it simply stays where it was.
func TestIsStudioProjectRecognisesOnlyTheStudiosShape(t *testing.T) {
	project := string(studioProjectBytes(t))
	if !isStudioProject([]byte(project)) {
		t.Fatal("the Studio's own project was refused")
	}
	for name, body := range map[string]string{
		"not JSON":        `{"id":`,
		"an array":        `[` + project + `]`,
		"an empty object": `{}`,
		"a numeric id":    strings.Replace(project, `"id": "6f1c2a9e-4b7d-4e2a-9c51-0d3e8f7a1b24"`, `"id": 7`, 1),
		"no frame rate":   strings.Replace(project, `"fps": 30`, `"fps": 0`, 1),
		// The project's own frame is the first size in the file, ahead of every source's.
		"a frame with no height":     strings.Replace(project, `"width": 1920, "height": 1080`, `"width": 1920`, 1),
		"sources that are null":      strings.Replace(project, `"sources": [`, `"sources": null, "unused": [`, 1),
		"no overlays lane list":      strings.Replace(project, `"overlays": [`, `"lanes": [`, 1),
		"a source of no known kind":  strings.Replace(project, `"kind": "audio"`, `"kind": "midi"`, 1),
		"a clip of a missing source": strings.Replace(project, `"sourceId": "src-still"`, `"sourceId": "src-gone"`, 1),
		"a clip playing a sound":     strings.Replace(project, `"sourceId": "src-still"`, `"sourceId": "src-music"`, 1),
		"a clip of no length":        strings.Replace(project, `"duration": 4,`, `"duration": 0,`, 1),
		"a clip with no id":          strings.Replace(project, `"id": "clip-2",`, ``, 1),
	} {
		if body == project {
			t.Fatalf("%s: the fixture no longer contains the text this case rewrites", name)
		}
		if isStudioProject([]byte(body)) {
			t.Errorf("%s: accepted as a Studio project", name)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./internal/assets/`

Expected: `vet` fails with `vet: internal/assets/studio_project_rekey_test.go:40:10: undefined: StudioProjectRekey`, and `race` fails to compile for the same reason. Measured in scratch.

- [ ] **Step 4: Write the re-key**

Create `internal/assets/studio_project_rekey.go`:

```go
package assets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/objectstore"
)

// A Video Studio project saved before aura-video-mcp Plan A sits at chat/<uuid>.json, and the
// ingest sidecar indexes it as a document: its matcher reads only the object key, and that key
// carried no sign of the Studio until objectstore.StudioProjectSuffix. Saving again does not
// help, because every save is a new asset. StudioProjectRekey moves each such project once to
// chat/<uuid>.aura-video.json through the file manager's own relocation (Browser.transfer), and
// the ingest then drops it as a key that vanished.
//
// Only a row that passes every test below is touched, and no one test would do on its own: an
// operator can attach a .json from the same cockpit to a chat that has no thread yet. The bytes
// are what no other file passes -- the Studio's saved shape, typed, with every clip naming a
// source it can draw.

const (
	rekeyPage = 100
	// studioProjectMaxBytes bounds the one read the check makes. A project is the editor's state,
	// well under a kilobyte per clip, so a JSON document past 4 MiB is not one.
	studioProjectMaxBytes = 4 << 20
)

var (
	// legacyStudioProjectKey is the key Presign gave a project: chat/, then the uuid it minted
	// for the key (not the row's id), then the one extension assetExtension kept.
	legacyStudioProjectKey = regexp.MustCompile(
		`^chat/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.json$`)
	// legacyStudioProjectName is what projectFileName wrote: a slug of letters, digits and
	// hyphens, at most 60 of them, or the project's own uuid, then .json.
	legacyStudioProjectName = regexp.MustCompile(`^[A-Za-z0-9-]{1,60}\.json$`)
)

// StudioProjectRekey moves the Studio projects saved before Plan A out of the document index.
type StudioProjectRekey struct {
	Assets     *Service
	Files      *Browser
	Identities IdentityLister
}

// Run re-keys every identity's old Studio projects and returns how many it moved. What it could
// not move is named in the joined error and tried again on the next run; a moved project no
// longer matches, so a second run moves nothing.
func (r StudioProjectRekey) Run(ctx context.Context) (int, error) {
	identities, err := r.Identities.IdentityIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("studio project re-key: list identities: %w", err)
	}
	moved := 0
	var errs []error
	for _, identityID := range identities {
		n, err := r.rekeyIdentity(ctx, identityID)
		moved += n
		if err != nil {
			errs = append(errs, fmt.Errorf("studio project re-key of %s: %w", identityID, err))
		}
	}
	return moved, errors.Join(errs...)
}

func (r StudioProjectRekey) rekeyIdentity(ctx context.Context, identityID string) (int, error) {
	moved := 0
	var errs []error
	for before := ""; ; {
		page, err := r.Assets.Store.ListRecent(ctx, identityID, before, []Modality{ModalityDocument}, rekeyPage)
		if err != nil {
			return moved, errors.Join(append(errs, fmt.Errorf("list documents: %w", err))...)
		}
		for _, asset := range page {
			if !savedLikeAStudioProject(asset) {
				continue
			}
			ok, err := r.rekey(ctx, identityID, asset)
			if err != nil {
				errs = append(errs, fmt.Errorf("asset %s at %s: %w", asset.ID, asset.ObjectKey, err))
			}
			if ok {
				moved++
			}
		}
		if len(page) < rekeyPage {
			return moved, errors.Join(errs...)
		}
		before = page[len(page)-1].ID
	}
}

// savedLikeAStudioProject is what web/src/videoStudio/projectStore.ts saveProject left before
// Plan A: a JSON document from the cockpit (ListRecent is asked for documents only), with no
// thread and no library scope, named <slug>.json, at a key of the presign's own uuid.
func savedLikeAStudioProject(asset Asset) bool {
	return asset.SourceKind == SourceWeb && asset.ThreadID == "" && asset.Scope == ScopeThread &&
		asset.MIMEType == "application/json" &&
		legacyStudioProjectName.MatchString(asset.FileName) &&
		legacyStudioProjectKey.MatchString(asset.ObjectKey)
}

// rekey moves one candidate when its bucket, size and bytes say it is a Studio project, and logs
// the decision either way.
func (r StudioProjectRekey) rekey(ctx context.Context, identityID string, asset Asset) (bool, error) {
	store, bucket, err := r.Files.resolveStore(ctx, identityID)
	if err != nil {
		return false, err
	}
	refusal := ""
	switch {
	case asset.ObjectBucket != bucket:
		refusal = "it lives in another bucket than the file manager's"
	case asset.SizeBytes > studioProjectMaxBytes:
		refusal = "it is larger than any Studio project"
	default:
		project, err := readAtMost(ctx, store, objectstore.ObjectRef{Bucket: bucket, Key: asset.ObjectKey})
		if err != nil {
			return false, err
		}
		if !isStudioProject(project) {
			refusal = "its bytes are not a Studio project"
		}
	}
	if refusal != "" {
		slog.Info("aura assets: left a JSON document where it is", "asset_id", asset.ID, "key", asset.ObjectKey, "reason", refusal)
		return false, nil
	}
	to := strings.TrimSuffix(asset.ObjectKey, ".json") + objectstore.StudioProjectSuffix
	name := strings.TrimSuffix(asset.FileName, ".json") + objectstore.StudioProjectSuffix
	if _, err := r.Files.transfer(ctx, identityID, asset.ObjectKey, to, true, name); err != nil {
		return false, err
	}
	slog.Info("aura assets: re-keyed a Studio project out of the document index", "asset_id", asset.ID, "from", asset.ObjectKey, "to", to)
	return true, nil
}

func readAtMost(ctx context.Context, store objectstore.Store, ref objectstore.ObjectRef) ([]byte, error) {
	body, _, err := store.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	return io.ReadAll(io.LimitReader(body, studioProjectMaxBytes+1))
}

// studioProjectFile is the part of the Studio's saved shape (projectStore.ts hasProjectShape and
// referencesHold) that tells a project from any other JSON. A pointer or a slice pointer is
// required: nil means the field is missing or null, and a field of the wrong JSON type fails the
// decode.
type studioProjectFile struct {
	ID       *string            `json:"id"`
	Name     *string            `json:"name"`
	FPS      *float64           `json:"fps"`
	Size     *studioFrame       `json:"size"`
	Sources  *[]studioSource    `json:"sources"`
	Video    *[]studioClip      `json:"video"`
	Overlays *[]json.RawMessage `json:"overlays"`
}

type studioFrame struct {
	Width  *float64 `json:"width"`
	Height *float64 `json:"height"`
}

type studioSource struct {
	ID      *string `json:"id"`
	AssetID *string `json:"assetId"`
	Kind    string  `json:"kind"`
}

type studioClip struct {
	ID       *string  `json:"id"`
	SourceID *string  `json:"sourceId"`
	Duration *float64 `json:"duration"`
}

func isStudioProject(raw []byte) bool {
	var file studioProjectFile
	if json.Unmarshal(raw, &file) != nil {
		return false
	}
	if file.ID == nil || file.Name == nil || !positive(file.FPS) || file.Size == nil ||
		!positive(file.Size.Width) || !positive(file.Size.Height) ||
		file.Sources == nil || file.Video == nil || file.Overlays == nil {
		return false
	}
	kinds := make(map[string]string, len(*file.Sources))
	for _, source := range *file.Sources {
		if source.ID == nil || source.AssetID == nil ||
			(source.Kind != "video" && source.Kind != "image" && source.Kind != "audio") {
			return false
		}
		kinds[*source.ID] = source.Kind
	}
	for _, clip := range *file.Video {
		if clip.ID == nil || clip.SourceID == nil || !positive(clip.Duration) {
			return false
		}
		if kind := kinds[*clip.SourceID]; kind != "video" && kind != "image" {
			return false
		}
	}
	return true
}

func positive(value *float64) bool {
	return value != nil && *value > 0
}
```

- [ ] **Step 5: Run the tests to verify they pass, and that each conjunct is load-bearing**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./internal/assets/`

Expected, measured in scratch:
- `gofmt` lists no file;
- `vet` is silent;
- `race` prints `ok  	github.com/chetto1983/aura/internal/assets`;
- `unit tier covered/total = 907/1151`;
- `golangci-lint` prints `0 issues.`

The five new tests pass:
- `TestStudioProjectRekeyMovesAnOldStudioProjectOutOfTheIndex`;
- `TestStudioProjectRekeyLeavesEveryOtherJSONWhereItIs` (eleven subtests);
- `TestStudioProjectRekeyReadsPastTheFirstPage`;
- `TestStudioProjectRekeyGoesOnPastAProjectItCannotRead`;
- `TestIsStudioProjectRecognisesOnlyTheStudiosShape`.

`studio_project_rekey.go` itself covers 70 of 73 statements (`go tool cover -func`: `Run` 100 %, `rekeyIdentity` 94.1 %, `savedLikeAStudioProject` 100 %, `rekey` 90.5 %, `readAtMost` 100 %, `isStudioProject` 100 %, `positive` 100 %). `internal/assets` is a target-mode package (`scripts/coverage_package_policy.json:19`): the full tag matrix in CI must stay at or above 85 %, and a new file at 95.9 % does not lower it.

Each conjunct was removed in turn in scratch, and every removal failed at least one test:

| Removed | Fails |
|---|---|
| the destination (`to := asset.ObjectKey`) | the move, the paging and the unreadable-project tests: the row stays at `chat/….json` |
| paging (`return` after the first page) | `ReadsPastTheFirstPage`: `Run = 0, <nil>; want 1, nil` |
| `ThreadID == ""` | `attached in a chat thread`: `Run = 1, <nil>; want 0, nil` |
| `SourceKind == SourceWeb` | `sent from Telegram` |
| `Scope == ScopeThread` | `filed in the library` |
| the MIME test | `not JSON by its type` |
| the name pattern | `a name the Studio never writes` |
| the key pattern | `a key the file manager named` and `under media/` |
| the bucket test | `in another bucket`: the read fails in the file manager's bucket |
| the size bound | `larger than any project` |
| the content check | `JSON that is not a project` |
| the clip-to-source rule | `a clip playing a sound`, `a clip of a missing source` |
| the source kinds | `a source of no known kind` |
| the frame | `a frame with no height` |

- [ ] **Step 6: Write the boot wiring's failing test**

Create `cmd/aura/studio_project_rekey_wiring_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/objectstore"
)

// The boot moves old Studio projects only when the daemon has rows to read them from; an
// unwired daemon gets nil, which the boot skips, never a pass that panics on a nil store.
func TestBuildStudioProjectRekey(t *testing.T) {
	if rekey := buildStudioProjectRekey(&chatEnv{}, objectstore.NewFake()); rekey != nil {
		t.Fatalf("an unwired daemon built %#v", rekey)
	}
	pool := newLazyPool(t)
	svc := &assets.Service{}
	rekey := buildStudioProjectRekey(&chatEnv{
		cfg: &config.Config{ObjectStoreBucket: "aura-assets"}, pool: pool, assets: svc, identity: identity.New(pool),
	}, objectstore.NewFake())
	if rekey == nil || rekey.Assets != svc || rekey.Files == nil || rekey.Files.Rows == nil || rekey.Identities == nil {
		t.Fatalf("rekey = %#v, want the daemon's asset service, a file manager with rows, every identity", rekey)
	}
	if rekey.Files.SharedBucket != "aura-assets" {
		t.Fatalf("SharedBucket = %q, want the configured bucket", rekey.Files.SharedBucket)
	}
}

type rekeyIdentities struct {
	ids []string
	err error
}

func (r rekeyIdentities) IdentityIDs(context.Context) ([]string, error) { return r.ids, r.err }

// The boot logs what the pass did, and a failure is a warning, never a stopped boot.
func TestRekeyStudioProjectsLogsAndNeverFails(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	rekeyStudioProjects(context.Background(), nil)
	rekeyStudioProjects(context.Background(), &assets.StudioProjectRekey{Identities: rekeyIdentities{}})
	rekeyStudioProjects(context.Background(), &assets.StudioProjectRekey{
		Identities: rekeyIdentities{err: errors.New("identity store down")},
	})

	for _, want := range []string{
		`level=INFO msg="aura serve: studio project re-key done" moved=0`,
		`level=WARN msg="aura serve: studio project re-key left projects in the index" moved=0`,
		"identity store down",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log lacks %q:\n%s", want, logs.String())
		}
	}
}
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./cmd/aura/`

Expected: `vet` fails with `vet: cmd/aura/studio_project_rekey_wiring_test.go:20:14: undefined: buildStudioProjectRekey`. Measured in scratch.

- [ ] **Step 7: Wire it into the boot**

Create `cmd/aura/studio_project_rekey_wiring.go`:

```go
package main

import (
	"context"
	"log/slog"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/objectstore"
)

// buildStudioProjectRekey gives the boot the pass that moves the Video Studio projects saved
// before aura-video-mcp Plan A out of the document index, through the file manager's own
// relocation. Without an asset service, an identity store or a pool there are no rows to read,
// and the boot gets nil.
func buildStudioProjectRekey(chat *chatEnv, objects objectstore.Store) *assets.StudioProjectRekey {
	if chat.assets == nil || chat.identity == nil || chat.pool == nil {
		return nil
	}
	return &assets.StudioProjectRekey{
		Assets:     chat.assets,
		Files:      buildFileBrowser(chat.cfg, chat.pool, objects),
		Identities: identityRoster{store: chat.identity},
	}
}

// rekeyStudioProjects runs that pass at boot, as reconcileArcadeMemoryTenants does: both repair
// what an older build left behind and both are idempotent, so the first start does the work and
// every later one finds nothing. A failure is logged and never stops the boot -- a project left
// indexed costs search noise, not data -- and the next start tries it again.
func rekeyStudioProjects(ctx context.Context, rekey *assets.StudioProjectRekey) {
	if rekey == nil {
		return
	}
	moved, err := rekey.Run(ctx)
	if err != nil {
		slog.Warn("aura serve: studio project re-key left projects in the index", "moved", moved, "err", err)
		return
	}
	slog.Info("aura serve: studio project re-key done", "moved", moved)
}
```

In `cmd/aura/serve.go`, after:

```go
	chat.assets = buildAssetService(chat.cfg, chat.pool, objectStore)
```

(`:273`) add:

```go
	rekeyStudioProjects(ctx, buildStudioProjectRekey(chat, objectStore))
```

The pass runs once the asset service exists, and before the HTTP server serves a request. `serve.go` grows from 566 to 567 lines.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./cmd/aura/`

Expected, measured in scratch:
- `gofmt` lists no file;
- `vet` is silent;
- `race` ends `ok  	github.com/chetto1983/aura/cmd/aura`. In scratch, `-run 'StudioProjectRekey|RekeyStudioProjects|BuildAssetDeleteSweep|ReconcileArcadeMemoryTenants'` passed all four, beside the two neighbouring boot passes;
- `golangci-lint` prints `0 issues.`

`cmd/aura` is glue outside the coverage denominator (CLAUDE.md, coverage gate). Its tests still run in the gate.

- [ ] **Step 8: Task gates**

1. `wc -l internal/assets/studio_project_rekey.go internal/assets/studio_project_rekey_test.go internal/assets/service_test.go internal/assets/service_delete_test.go cmd/aura/studio_project_rekey_wiring.go cmd/aura/studio_project_rekey_wiring_test.go cmd/aura/serve.go`. Expected: all ≤ 600 (215, 265, 589, 193, 40, 65, 567).
2. `git -C /d/Aura status --short internal/assets cmd/aura` from Git Bash. Expected: only this task's seven files.

CI note: the Go mutation gate does not cover `internal/assets` (no scope for it in `scripts/critical_mutation_gate.py` `REQUIRED_SCOPE_IDS`). The table in Step 5 is this task's evidence that its tests catch each conjunct.

- [ ] **Step 9: Commit**

Write `$W/msg-t7.txt`:

```text
fix(assets): move Studio projects saved before the suffix out of the index

A Video Studio project saved before objectstore.StudioProjectSuffix sits
at chat/<uuid>.json, and the ingest indexes it as a document; saving it
again only adds a new asset. StudioProjectRekey moves each such project
once to chat/<uuid>.aura-video.json through the file manager's own
transfer (copy, relocate the row, delete), so the old key vanishes from
the bucket and the ingest drops it like any deleted document. The row
keeps its id, so every link still loads it.

Only a row that passes every test is touched: a cockpit upload with no
thread and no library scope, a JSON document named <slug>.json at
chat/<the presign's uuid>.json, in the file manager's bucket, at most
4 MiB, whose bytes have the Studio's typed shape with every clip naming
a source it can draw. An operator's own JSON attached to a fresh chat
passes the row tests and fails the bytes; each test was removed in turn
and a test failed every time. The Studio's own parser loads the shared
fixture (web projectStore.test.ts), so the check describes a project.

It runs at boot, after the asset service is built, the way
reconcileArcadeMemoryTenants repairs what an older build left: once
moved, a row no longer matches, so later starts move nothing. A failure
is a warning, never a stopped boot, and the next start retries.

The fake ListRecent now honours before_id and the query's (created_at,
id) DESC order; it ignored both, so a paging bug would have passed.
captureWarnings becomes captureLogs(t, level) so a test can read Info.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t7.txt internal/assets/studio_project_rekey.go internal/assets/studio_project_rekey_test.go internal/assets/service_test.go internal/assets/service_delete_test.go cmd/aura/studio_project_rekey_wiring.go cmd/aura/studio_project_rekey_wiring_test.go cmd/aura/serve.go`

Expected: `COMMIT_RC=0`.

---

### Task 8: The E2E — a fade that darkens, sources lost by name, one fetch per source, the saved key

**What has run, and what has not.** The specs below are clean under `prettier`, `tsc` and oxlint (0 warnings, 0 errors), checked in scratch on 2026-10-01. The frame reader ran in WSL Chromium on `web/e2e/fixtures/video-studio/clip-a.mp4` (2026-09-30): frames at media time 1, 2.5 and 3.8 s, each 230,400 bytes (320 × 180 × 4), with mean levels 123.7–125.0. The specs themselves have **not** run against any cockpit: no stack was reachable from the scratch (`127.0.0.1:9080` answered nothing, and docker was unreachable from WSL). This task runs them in three stages:
1. RED on the image the lab VM runs now;
2. GREEN on a local build of this branch, before anything is pushed;
3. GREEN on the VM once CI has shipped the image.

**Files:**
- Create: `web/e2e/support/frames.ts`
- Modify: `web/e2e/video-studio.spec.ts`: an import after `:4`, and `twinFrameDiff` with its doc comment at `:77-173`
- Create: `web/e2e/video-studio-export.spec.ts`
- Rebuild: `internal/webui/dist`

Not changed: `web/e2e/support/videoStudio.ts`. `reopen` keeps uploading `project.json`, a plain `.json` project in the pre-Plan-A shape. So every Studio E2E that reopens a project keeps proving that such a project still opens (spec ruling 8), and a project Task 7 re-keys keeps its asset id, which is what `reopen` loads by.

**Interfaces:**
- Consumes:
  - Task 1: a fade-out's last half second draws at `(end − t) / 0.5`, and a static opacity draws as it always did;
  - Task 2: an export reads a source the Stage already holds from the page's media cache, without a GET of its own;
  - Task 3: the en sentences `The export stopped: ${fileName} could not be fetched, so no file was written.` and `The export stopped: the renderer could not read ${fileName}, so no file was written.`;
  - Tasks 4 and 6: the saved key is `chat/<uuid>.aura-video.json`, where the uuid is the one Presign minted, never the asset id.
- Produces, from `web/e2e/support/frames.ts`, for Plan C's reel checks to reuse:

```ts
export interface Frame { readonly mediaTime: number; readonly rgba: Buffer }
export async function readFrames(page: Page, mp4: Buffer, times: readonly number[]): Promise<readonly Frame[]>;
export function meanLevel(frame: Frame): number; // mean of R, G and B over every pixel, 0–255
```

- [ ] **Step 1: Extract the in-page frame reader**

Create `web/e2e/support/frames.ts`:

```ts
import type { Page } from '@playwright/test';

// frames.ts — frames of an exported file, read at chosen instants. They are decoded in the page:
// Node has no WebCodecs, and a `<video>` fed the exported bytes is also the bluntest available
// proof that what came out is playable.

export interface Frame {
  /** Where the frame sits in the file: the one Chrome presented for the seek, within 0.1 s of the
   *  instant asked for. */
  readonly mediaTime: number;
  /** RGBA, row by row, `videoWidth × videoHeight × 4` bytes. */
  readonly rgba: Buffer;
}

/** The frames Chrome presents at `times`, in that order. */
export async function readFrames(
  page: Page,
  mp4: Buffer,
  times: readonly number[],
): Promise<readonly Frame[]> {
  const read = await page.evaluate(
    async ({ base64, times }) => {
      const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
      const url = URL.createObjectURL(new Blob([bytes], { type: 'video/mp4' }));
      const video = document.createElement('video');
      video.muted = true;
      video.preload = 'auto';
      video.src = url;
      try {
        await new Promise<void>((resolve, reject) => {
          video.onloadeddata = () => {
            resolve();
          };
          video.onerror = () => {
            reject(new Error('the exported file did not decode in the browser'));
          };
        });
        const canvas = new OffscreenCanvas(video.videoWidth, video.videoHeight);
        const context = canvas.getContext('2d', { willReadFrequently: true });
        if (context === null) throw new Error('no 2d context');
        const frames: { mediaTime: number; rgba: string }[] = [];
        for (const time of times) {
          // `seeked` can fire before the decoded frame reaches the compositor under CI load.
          // Read only after Chrome reports the requested frame as presented.
          const mediaTime = await new Promise<number>((resolve, reject) => {
            const timeout = window.setTimeout(() => {
              reject(new Error(`the exported video did not present frame at ${String(time)}s`));
            }, 10_000);
            const onFrame: VideoFrameRequestCallback = (_now, metadata) => {
              if (Math.abs(metadata.mediaTime - time) > 0.1) {
                video.requestVideoFrameCallback(onFrame);
                return;
              }
              window.clearTimeout(timeout);
              resolve(metadata.mediaTime);
            };
            video.requestVideoFrameCallback(onFrame);
            video.currentTime = time;
          });
          context.drawImage(video, 0, 0);
          const data = context.getImageData(0, 0, canvas.width, canvas.height).data;
          // A typed array does not cross `evaluate`; base64 does, built in 32 KiB slices so
          // `fromCharCode` is never handed more arguments than a call can take.
          let binary = '';
          for (let at = 0; at < data.length; at += 0x8000) {
            binary += String.fromCharCode(...data.subarray(at, at + 0x8000));
          }
          frames.push({ mediaTime, rgba: btoa(binary) });
        }
        return frames;
      } finally {
        URL.revokeObjectURL(url);
      }
    },
    { base64: mp4.toString('base64'), times: [...times] },
  );
  return read.map((frame) => ({
    mediaTime: frame.mediaTime,
    rgba: Buffer.from(frame.rgba, 'base64'),
  }));
}

/** How bright a frame is: the mean of R, G and B over every pixel, 0–255. */
export function meanLevel(frame: Frame): number {
  let sum = 0;
  for (let at = 0; at < frame.rgba.length; at += 4) {
    sum += (frame.rgba[at] ?? 0) + (frame.rgba[at + 1] ?? 0) + (frame.rgba[at + 2] ?? 0);
  }
  return sum / ((frame.rgba.length / 4) * 3);
}
```

- [ ] **Step 2: `twinFrameDiff` reads its frames through it**

The fade spec needs the same in-page decode that `twinFrameDiff` holds inline. CLAUDE.md "REUSABLE CODE" says to extract it, not copy it.

In `web/e2e/video-studio.spec.ts`, after `import { expect, test } from './support/assetCleanup';` add:

```ts
import { readFrames } from './support/frames';
```

Then replace everything from the doc comment that opens `/**` / ` * How much of the picture differs between two instants that show the same source frame, and how` (`:77-78`) through the closing `}` of `async function twinFrameDiff(…)` (`:173`, just above `test.describe('the multi-track video editor', () => {`) with:

```ts
/**
 * How much of the picture differs between two instants that show the same source frame, and how
 * much of that difference is the title's own colour. The frames are read by `readFrames`.
 */
async function twinFrameDiff(
  page: Page,
  mp4: Buffer,
  pairs: readonly (readonly [number, number])[],
  ink: string,
): Promise<readonly FrameDiff[]> {
  const frames = await readFrames(page, mp4, pairs.flat());
  const target = [1, 3, 5].map((at) => parseInt(ink.slice(at, at + 2), 16));
  const isInk = (frame: Buffer, at: number) =>
    Math.max(
      Math.abs((frame[at] ?? 0) - (target[0] ?? 0)),
      Math.abs((frame[at + 1] ?? 0) - (target[1] ?? 0)),
      Math.abs((frame[at + 2] ?? 0) - (target[2] ?? 0)),
    ) <= INK_DISTANCE;
  return pairs.map((_pair, index) => {
    const a = frames[2 * index]?.rgba;
    const b = frames[2 * index + 1]?.rgba;
    if (a === undefined || b === undefined) throw new Error('a twin frame was not read');
    let changed = 0;
    let inked = 0;
    for (let i = 0; i < a.length; i += 4) {
      const moved = Math.max(
        Math.abs((a[i] ?? 0) - (b[i] ?? 0)),
        Math.abs((a[i + 1] ?? 0) - (b[i + 1] ?? 0)),
        Math.abs((a[i + 2] ?? 0) - (b[i + 2] ?? 0)),
      );
      if (moved < CHANGED_CHANNEL) continue;
      changed += 1;
      if (isInk(a, i) && !isInk(b, i)) inked += 1;
    }
    return { changed, inked };
  });
}
```

The pixel arithmetic is the same as before. Only where it runs changes: Node, over the bytes `readFrames` returns in the order asked for, instead of the page. The file goes from 398 to 339 lines.

- [ ] **Step 3: Write the export spec**

Create `web/e2e/video-studio-export.spec.ts`:

```ts
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { expect, readAsset, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { meanLevel, readFrames } from './support/frames';
import {
  containerFacts,
  editorOnSeededClip,
  exportTo,
  FIXTURES,
  reopen,
  silentFilm,
  uploadClip,
} from './support/videoStudio';

// video-studio-export.spec.ts — what the Studio's files must never do quietly, each found by the
// render spikes (spikes/video-mcp-render/FINDINGS.md): drop a clip's fade (S1.3), turn a source it
// could not fetch or read into black frames that download as a success (S1.4), fetch a source
// twice (S1.4), or become a searchable document when saved (spec §Project indexing). The file and
// the key are the same at every width, so each is measured once, on the desktop project.

const PHONE =
  'the exported file and the saved key do not depend on the width; the desktop run measures them';

/** Presses Export and answers with what happened first: a download, or `sentence` on screen. */
async function exportOutcome(
  page: Page,
  editor: Locator,
  sentence: string,
): Promise<'downloaded' | 'refused'> {
  const downloaded = page
    .waitForEvent('download', { timeout: 120_000 })
    .then(() => 'downloaded' as const);
  const refused = editor
    .getByText(sentence)
    .waitFor({ timeout: 120_000 })
    .then(() => 'refused' as const);
  await editor.getByRole('button', { name: 'Export', exact: true }).click();
  return await Promise.race([downloaded, refused]);
}

test('a clip that fades out darkens in the exported file, as far as its fade has gone', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  // Three muted 4 s clips of one source: each replays the first frame for frame, so the film at
  // t + 4 s shows the source frame it showed at t. Clip 2 fades out; clip 3 is the control, drawn
  // at a static opacity of 0.4, which the renderer honoured before and after the fix: whatever the
  // decode does to brightness, it does to both.
  const film = silentFilm(clip, 'fade check', 3);
  const editor = await reopen(
    page,
    {
      ...film,
      video: film.video.map((item, index) =>
        index === 1 ? { ...item, fadeOut: true } : index === 2 ? { ...item, opacity: 0.4 } : item,
      ),
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Clip 3' })).toBeVisible({ timeout: 60_000 });

  // The clip's bytes, fetched by the export itself: the context sees a worker's requests too. The
  // Stage already holds them in the page's media cache, so a fixed export reads them from there.
  let fetchedByExport = 0;
  let exporting = false;
  page.context().on('request', (request) => {
    if (
      exporting &&
      request.method() === 'GET' &&
      request.url().includes(`/api/assets/${clip}/download`)
    ) {
      fetchedByExport += 1;
    }
  });
  exporting = true;
  const mp4 = readFileSync(await exportTo(page, editor, info, 'fade.mp4'));
  exporting = false;
  // Before the fade (7.2 s), 0.3 s into its last half second (7.8 s), and the control (11.8 s),
  // each beside its twin.
  const [early, earlyTwin, late, lateTwin, control] = await readFrames(
    page,
    mp4,
    [3.2, 7.2, 3.8, 7.8, 11.8],
  );
  if (
    early === undefined ||
    earlyTwin === undefined ||
    late === undefined ||
    lateTwin === undefined ||
    control === undefined
  ) {
    throw new Error('the export answered fewer frames than it was asked for');
  }
  // The opacity the renderer drew the presented frame at: 1 until 7.5 s, then down to 0 at 8 s.
  const expected = Math.min(1, (8 - lateTwin.mediaTime) / 0.5);
  const measured = {
    beforeFade: meanLevel(earlyTwin) / meanLevel(early),
    inFade: meanLevel(lateTwin) / meanLevel(late),
    control: meanLevel(control) / meanLevel(late),
    expected,
    presentedAt: lateTwin.mediaTime,
    fetchedByExport,
  };
  // Written beside the exported file, so a run with a private `--output` keeps both to read.
  const levels = info.outputPath('fade-levels.json');
  writeFileSync(levels, JSON.stringify(measured, null, 2));
  await info.attach('fade-levels', { contentType: 'application/json', path: levels });

  expect(measured.beforeFade).toBeGreaterThan(0.9);
  expect(measured.control).toBeLessThan(0.7);
  // S1.3's frame at this point of its fade read RGB (233, 100, 90), where 0.4 over black allows
  // 102. Measured against the control, scaled to the fade's own opacity at the presented frame.
  expect(Math.abs(measured.inFade - (measured.control * expected) / 0.4)).toBeLessThan(0.1);
  // S1.4 measured two GETs per source: VideoFlow's own, then the pre-decode's.
  expect(fetchedByExport).toBe(0);
});

test('a source the export cannot fetch stops it by name, and nothing downloads', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  test.setTimeout(5 * 60_000);
  const clip = await uploadClip(page);
  const lost = await uploadAsset(page, resolve(FIXTURES, 'clip-b.mp4'), 'clip-b.mp4', 'video/mp4', {
    use: 'media',
  });
  const film = silentFilm(clip, 'lost source check');
  // A Playwright stub, not the server: the library still lists the second clip and its row is
  // complete, so the editor opens and the export is allowed to start (projectStore `sourceIsGone`
  // reads the row, not the bytes); only the bytes stop answering.
  let refused = 0;
  await page.route(`**/api/assets/${lost}/download`, (route) => {
    refused += 1;
    return route.fulfill({ status: 404, body: '' });
  });
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        {
          id: 'src-b',
          assetId: lost,
          kind: 'video',
          duration: 4,
          size: { width: 320, height: 180 },
          hasAudio: true,
        },
      ],
      video: film.video.map((item, index) => (index === 1 ? { ...item, sourceId: 'src-b' } : item)),
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });

  // S1.4 measured the old answer: a black, silent MP4 in 4.9 s, reported as a success. The
  // sentence names the file as the library lists it, read from the asset row.
  const outcome = await exportOutcome(
    page,
    editor,
    'The export stopped: clip-b.mp4 could not be fetched, so no file was written.',
  );
  // The stub really answered: the sentence is about the bytes it refused, not a route it missed.
  expect(refused).toBeGreaterThan(0);
  // Kept beside the run for a person to look at: the sentence where the operator reads it.
  await page.screenshot({ path: info.outputPath('lost-source.png') });
  expect(outcome).toBe('refused');
});

test('a source the renderer cannot read stops the export by name, and nothing downloads', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  test.setTimeout(5 * 60_000);
  const clip = await uploadClip(page);
  // MPEG-4 Part 2 in an MP4: it parses, and no browser holds a decoder for it
  // (video-studio.spec.ts proves the editor refuses it at the picker). Uploaded past the picker,
  // the way an agent's project can name any asset.
  const path = resolve(FIXTURES, 'unplayable.mp4');
  const facts = await containerFacts(path);
  const unplayable = await uploadAsset(page, path, 'unplayable.mp4', 'video/mp4', { use: 'media' });
  const film = silentFilm(clip, 'undecodable source check');
  const editor = await reopen(
    page,
    {
      ...film,
      sources: [
        ...film.sources,
        {
          id: 'src-u',
          assetId: unplayable,
          kind: 'video',
          duration: facts.duration,
          size: { width: facts.width, height: facts.height },
          hasAudio: facts.hasAudio,
        },
      ],
      video: film.video.map((item, index) =>
        index === 1 ? { ...item, sourceId: 'src-u', duration: facts.duration } : item,
      ),
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });

  const outcome = await exportOutcome(
    page,
    editor,
    'The export stopped: the renderer could not read unplayable.mp4, so no file was written.',
  );
  await page.screenshot({ path: info.outputPath('undecodable-source.png') });
  expect(outcome).toBe('refused');
});

test('a saved project keeps its Studio suffix in the store, where the ingest skips it', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  const editor = await editorOnSeededClip(page, 'saved project check');

  await editor.getByRole('button', { name: 'Save the project' }).click();
  await expect(editor.getByText('Project saved.')).toBeVisible({ timeout: 60_000 });

  const saved = await page.evaluate(() =>
    window.localStorage.getItem('aura.videoStudio.lastSavedProject'),
  );
  if (saved === null) throw new Error('the save remembered no project');
  // The key is all the ingest's path matcher sees (services/ingest/source.py): the suffix has to
  // be in it, and the name — which rides in the object's metadata — must not. Its uuid is the one
  // Presign minted for the key, never the asset's own id (internal/assets/service.go `Presign`).
  expect((await readAsset(page, saved)).object_key).toMatch(
    /^chat\/[0-9a-f-]{36}\.aura-video\.json$/,
  );
});
```

Every asset these tests create is presigned through the page, so the `assetCleanup` fixture deletes it when the test ends (`web/e2e/support/assetCleanup.ts:17-60`).

What each test measures:
- **Fade.** Three muted 4 s clips of one source. Clip 2 fades out. Clip 3 is the control: it is drawn at a static opacity of 0.4, which the renderer honoured before the fix too, because a static value never went through the keyframe path. Each ratio is a frame's mean level against its twin's, the same source frame 4 s earlier:
  - `beforeFade` (7.2 s against 3.2 s) must stay above 0.9;
  - `control` (11.8 s against 3.8 s) must be under 0.7: the control really darkened, so the decode is not hiding a fade;
  - `inFade` (7.8 s against 3.8 s) must be within 0.1 of `control × expected / 0.4`, where `expected = (8 − presentedAt) / 0.5`, the opacity due at the frame Chrome presented. Scaling by the control instead of comparing with 0.4 directly means the decode's own gamma and range apply equally to both;
  - `fetchedByExport` counts the GETs of the clip's download URL while the export runs, on the browser context, so a worker's requests count too. The Stage already holds the clip in the page's cache, so a fixed export fetches it 0 times. The old pre-decode fetched it once more.

  All six numbers are written to `fade-levels.json` **before** any assertion, so a RED run still records them.
- **Lost source.** The 404 is a **Playwright stub**, not the server. The library still lists the second clip and its row is complete, so the editor opens and the export starts; only the bytes stop answering. `refused > 0` proves the stub actually served. The outcome must be the sentence, and no download. The sentence names `clip-b.mp4`, read from the asset row.
- **Undecodable source.** `unplayable.mp4` is MPEG-4 Part 2 in an MP4: 2 s, no audio, 320 × 180, as `containerFacts` reads it. It is uploaded past the picker, the way an agent's project can name any asset. The renderer can fetch it and cannot read it. The sentence names `unplayable.mp4`.
- **Saved key.** `^chat\/[0-9a-f-]{36}\.aura-video\.json$`, with no slug and no asset id: the uuid in the key is the one Presign minted (`internal/assets/service.go` `Presign`), and Task 9 measures it differing from the row's id.

- [ ] **Step 4: Static gates, then commit the tests**

1. `MSYS_NO_PATHCONV=1 wsl bash $W/webcheck.sh e2e/support/frames.ts e2e/video-studio.spec.ts e2e/video-studio-export.spec.ts`. Expected: `tsc` clean, `Found 0 warnings and 0 errors.`, knip silent, prettier formatted.
2. `wc -l web/e2e/support/frames.ts web/e2e/video-studio.spec.ts web/e2e/video-studio-export.spec.ts`. Expected: 90, 339 and 239.

Write `$W/msg-t8.txt`:

```text
test(web): measure Studio fades, lost sources and saved keys end to end

video-studio-export.spec.ts proves on a real export what the unit tests
prove on the compile and the media load. A clip's fade-out darkens its
frames as far as the fade has gone: the level ratio against the same
source frame 4 s earlier, scaled by a control clip drawn at a static 0.4
the renderer always honoured, within 0.1 of the opacity due. The export
fetches no source the Stage already holds. A source whose bytes a
Playwright stub refuses, and a source the renderer cannot read, each
stop the export with the sentence that names the file, and nothing
downloads. A saved project's object key is chat/<uuid>.aura-video.json,
the suffix the ingest skips.

The in-page frame reader moves out of video-studio.spec.ts into
support/frames.ts, so twinFrameDiff and the fade test share it; its
pixel arithmetic is unchanged.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t8.txt web/e2e/support/frames.ts web/e2e/video-studio.spec.ts web/e2e/video-studio-export.spec.ts`

- [ ] **Step 5: RED on the image the lab VM runs now**

Nothing is pushed yet, so the VM still runs the old cockpit.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/e2e-vm.sh vm-red video-studio-export.spec.ts video-studio.spec.ts --project=chrome --reporter=line`

Expected in `video-studio-export.spec.ts`: all four tests fail, each for its own reason.
- **Fade:** `expect(received).toBeLessThan(expected)` with `Expected: < 0.1` on the `inFade` line. The `fade-levels.json` the script prints should show `inFade` near 1.0, `control` well under 0.7 and `expected` near 0.4: the old cockpit draws the fading clip at full opacity and the static control at 0.4. Write down `fetchedByExport`. The reviewers' expectation is 1 (the pre-decode's own GET), and Task 9 records whatever this run shows.
- **Lost source:** `Expected: "refused"` and `Received: "downloaded"`. The old export downloads a black clip.
- **Undecodable source:** the same, `Received: "downloaded"`.
- **Saved key:** `toMatch` fails on `chat/<uuid>.json`.

Every chrome test in `video-studio.spec.ts` passes: `twinFrameDiff` was refactored, not changed.

If any of the four export tests passes here, stop. It does not measure what it claims, and it must be fixed before anything is pushed. If the fade test fails on `beforeFade` or `control` instead of `inFade`, stop as well: the measurement itself is wrong, and the fix cannot be judged by it.

- [ ] **Step 6: Rebuild the embedded cockpit and commit it**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/build.sh`. Expected: `BUILD_RC=0`, the build's last lines, then a count of changed dist files greater than 0.

Write `$W/msg-t8-dist.txt`:

```text
build(web): rebuild the embedded dist for the trustworthy Studio export

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t8-dist.txt internal/webui/dist`. Note the new commit's SHA: it is the **dist SHA** below.

- [ ] **Step 7: GREEN on a local build, before anything is pushed**

Docker Desktop must be running. `e2e-local.sh` starts `aura` with `docker compose up -d aura`, and compose starts what `aura` depends on. If the script stops with `docker is not reachable from WSL`, the WSL integration has lost its socket: restore Docker Desktop's WSL integration, or ask the operator. Never run `docker.exe` from Git Bash instead.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/e2e-local.sh local-green video-studio-export.spec.ts video-studio.spec.ts --project=chrome --reporter=line`

The script builds the `aura` image from this working tree, starts it on `:9080`, and runs the specs against it with the local `.env` account. Expected:
- every test passes;
- the printed `fade-levels.json` has `beforeFade` > 0.9, `control` < 0.7, `|inFade − control × expected / 0.4|` < 0.1, and `fetchedByExport` 0.

Two side effects to know about:
- the image holds whatever else is dirty in this shared tree;
- this boot runs Task 7's re-key against the local stack. Read what it did in the `aura` container's log, lines with `studio project re-key`, `re-keyed a Studio project` and `left a JSON document`. It is the rule's first real reading.

If anything fails here, fix it here: nothing is pushed until this run is green.

- [ ] **Step 8: Push, then wait for CI**

First take the last reading of the old projects before the new image boots and re-keys them. It is read-only:

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vm-json-index.sh $W/rekey-before.txt`

It overwrites Task 0's snapshot. If its counts differ from Task 0's, the operator saved or deleted projects in between: Task 9 compares against this newer file, and records both readings.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/push.sh plan-a-push.log`. Expected:
- `PUSH_RC=0`;
- the script exits 0. It exits 1 with `NO LEFTHOOK BANNER` when no pre-push gate ran, and that is a failed push, whatever git says.

If another session's push is in flight, wait for it and ask. Never force.

Then run: `MSYS_NO_PATHCONV=1 wsl bash $W/poll-ci.sh <dist SHA>`

**This push runs the full Stryker suite**, not the incremental one. Tasks 1 and 2 changed `web/stryker.config.json` and `web/vitest.stryker.config.ts`, and both files key Stryker's incremental cache (`.github/workflows/ci.yml:1617`, `:1619`). A cache miss measured 86m34s (`ci.yml:1581-1582`); the job's own budget is 120 minutes. `poll-ci.sh` waits 150 minutes by default. If it times out, it prints what is still running and exits 1. Run it again; never read a running job as green.

Expected: every run `completed | success`. These are the jobs this plan touches:
- the web unit and coverage job;
- the Stryker critical-mutation job. Read `videoflow_keyframes.ts` and `videoflow_media.ts` in its artifact: each must be ≥ 70 % killed;
- `make ingest-test`;
- the Go coverage gate, which checks `internal/objectstore` at 459/601 and `internal/assets` at ≥ 85 %;
- `web-e2e`, which runs the new spec on CI's own stack, on the chrome project.

A red job is fixed before anything else. A red job owned by a parallel session is theirs: wait for their push.

- [ ] **Step 9: GREEN on the lab VM**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/waitfix.sh <dist SHA>`. Expected: `the VM runs <sha>, which contains <dist SHA>`, then five minutes for the edge restart, then the container list.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/e2e-vm.sh vm-green video-studio-export.spec.ts video-studio.spec.ts --reporter=line`

Expected:
- every test passes on `chrome`;
- the four export tests are reported as skipped on `mobile-chrome`.

If the first attempt cannot reach the cockpit, the updater is still restarting the edge: wait a minute and run it again.

- [ ] **Step 10: Read the measurements and look at the evidence**

1. The `fade-levels.json` the script printed. Expected: `beforeFade` > 0.9, `control` < 0.7, `|inFade − control × expected / 0.4|` < 0.1 with `expected` near 0.4 and `presentedAt` near 7.8, and `fetchedByExport` 0. Keep all six numbers for Task 9, with the RED run's.
2. `lost-source.png` and `undecodable-source.png`: the script printed their paths under `C:\Users\Davide\AppData\Local\Temp\plan-a-e2e\vm-green`. Open each with the Read tool. Expected: the editor with the English sentence naming `clip-b.mp4` or `unplayable.mp4`, and no progress bar still running.
3. `fade.mp4`, the exported film, sits beside them for anyone who wants to look at it.

---

### Task 9: Measure the landed fix on the lab VM and complete the PRD record

The E2E proves the export and the key. This task measures the rest on the real stack, read-only except for one controlled probe:
- the boot pass moved the old projects, and the index let go of them;
- a project saved now is skipped by the ingest while an operator's own JSON beside it is indexed;
- the key's uuid is not the asset id.

Then it writes the PRD's post-fix record: every sentence that says how things are **now**.

The ingest's cycle is `AURA_INGEST_INTERVAL_SEC`: 60 s by default (`services/ingest/app.py:40`, `compose.yaml` `aura-ingest`), but the VM's `.env` may set it, so every script here reads it from the running container. The index is read through its `IndexedDocument` rows, not through log lines. `app.py` declares that row only after a document's passages are stored (`arcade.py` `indexed_source_keys`), and CocoIndex retracts the declarations of a key that left the bucket: a deleted document's 249 passages left ArcadeDB in the 2026-08-06 spike, with no code of Aura's. The audit's `[audit] MISSING` line is no evidence either way. `expected_keys` no longer lists a project key (Task 5's `test_the_audit_does_not_expect_a_project`), so nothing here asserts on it.

**Files:**
- Uses (outside the repo): `$W/vm-json-index.sh`, `$W/vm-project-probe.sh`, `$W/rekey-before.txt` (Task 8 Step 8)
- Modify: `prd.md`: the two paragraphs Task 0 wrote

**Interfaces:**
- Consumes:
  - the image Task 8 shipped (the dist SHA);
  - the fade numbers from Task 8 Steps 5 and 10, RED and GREEN;
  - the snapshot `$W/rekey-before.txt`.
- Produces: the PRD's post-fix record.

- [ ] **Step 1: The old projects left the index**

The boot pass ran when the VM's `aura` restarted on the new image. Before reading the index, let at least two ingest cycles pass after that restart: `waitfix.sh` printed the restart, and `vm-json-index.sh` prints the interval.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vm-json-index.sh --after $W/rekey-before.txt`

Expected:
- `== what the daemon logged` shows exactly one `aura serve: studio project re-key done moved=<M>`. If it shows `…left projects in the index` instead, read its `err`: a `remove chat/…` error is the orphan case Task 7 describes, and that key is still indexed;
- one `re-keyed a Studio project out of the document index` line per moved asset, with `asset_id`, `from=chat/<uuid>.json` and `to=chat/<uuid>.aura-video.json`;
- one `left a JSON document where it is` line, with its `reason`, per candidate whose bucket, size or bytes said no;
- for every moved candidate: `<id>  chat/<uuid>.json (indexed before: yes, now: no) -> chat/<uuid>.aura-video.json (indexed: no)`;
- for every refused candidate: the same key before and after, and its index state unchanged.

If the snapshot had no candidate, the script says `no candidates … nothing to follow`. Then the record says the VM had none, and Step 2's probe is the only real-stack evidence for the skip.

Write down ⟨M⟩ moved, ⟨R⟩ refused with their reasons, and how many of the moved keys were indexed before (⟨I⟩) and after (⟨I′⟩, expected 0).

- [ ] **Step 2: A project saved now is skipped, an operator's JSON is not**

`$W/vm-project-probe.sh` (Helper scripts) signs in as the operator's cockpit account and saves two thread-less JSON documents through the cockpit's own upload door (presign, PUT, finalize, as `projectStore.saveProject` does):
- `plan-a-probe.aura-video.json`, a project;
- `plan-a-control.json`, an operator's price list.

It waits two whole ingest cycles plus 30 s, reads both rows and the index, and on every exit deletes whatever it created, whether the run passed or failed.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vm-project-probe.sh`

Expected:
1. `put` and `finalize` answer 2xx for both files;
2. `saved: project <P>, control <C>; ingest interval <s> s, waiting <w> s`;
3. `project chat/<uuid>.aura-video.json indexed: no`, where `<uuid>` is **not** `<P>`. This is the asset-id measurement, so write both down;
4. `control chat/<uuid>.json indexed: yes`;
5. `PASS: the ingest indexed the control and skipped the project`;
6. two `delete <id> 2xx` lines from the exit trap.

Exit 3 with `INCONCLUSIVE` means the control was not indexed yet, so the project's absence proves nothing: run it again. Never read a project's absence as a pass without the control's presence.

- [ ] **Step 3: Complete the PRD record**

In `prd.md`, at the end of the "VideoFlow opacity and media" paragraph Task 0 wrote (after `…never arrived or did not play.`, before that paragraph's `This does not establish:` list), add the text below. Fill every `⟨…⟩` from Task 8 Steps 5 and 10, and the dist SHA:

```markdown
Measured after the fix on ⟨date⟩ on the lab VM (image ⟨dist SHA⟩,
`web/e2e/video-studio-export.spec.ts`): against its twin 4 s earlier, a clip's fade-out frame at
⟨presentedAt⟩ s now keeps ⟨inFade⟩ of the level, where a control clip drawn at a static 0.4 keeps
⟨control⟩ and the fade is due ⟨expected⟩ (the image before the fix kept ⟨inFade RED⟩); the frame
before the fade keeps ⟨beforeFade⟩. The export now fetches a source the Stage already holds
⟨fetchedByExport⟩ times, where the image before the fix fetched it ⟨fetchedByExport RED⟩ times.
When a Playwright stub answers 404 for a source's bytes, the export now stops with the sentence
that names the source's file and downloads nothing, and so does a source whose bytes the renderer
cannot read (`unplayable.mp4`).
```

At the end of the "Saved Studio projects and the document index" paragraph Task 0 wrote (after `- no code is to read an asset id out of an object key.`, before its `This does not establish:` list), add the text below. Fill every `⟨…⟩` from Steps 1 and 2:

```markdown

Measured after the fix on ⟨date⟩ on the lab VM (image ⟨dist SHA⟩): at its first start the daemon
moved ⟨M⟩ of the ⟨c′⟩ candidates and left ⟨R⟩ (⟨reasons⟩); ⟨I⟩ of the moved keys were indexed
before and ⟨I′⟩ are now. A project saved through the cockpit's upload door as
`plan-a-probe.aura-video.json` now gets the key `chat/⟨uuid⟩.aura-video.json` while its asset id is
⟨P⟩, and two ingest cycles later the index holds no row for it, while the operator's
`plan-a-control.json` saved beside it is indexed; both were deleted afterwards.
```

Then replace that paragraph's first `This does not establish:` bullet, which said the boot pass and the index would be measured after the fix, with:

```markdown
- that the boot pass would refuse every JSON an operator saved that is not a project: on the VM it
  met the ⟨c′⟩ candidates above, and the other shapes were shown by the unit tests;
```

- [ ] **Step 4: Commit and push the record**

Write `$W/msg-t9.txt`:

```text
docs(prd): record the Studio export and project fixes measured on the VM

After aura-video-mcp Plan A reached the lab VM: the exported fade-out
frame kept the level the fade was due, against a control clip at a
static opacity; the export fetched no source the Stage already held; a
source a stub refused and a source the renderer could not read each
stopped the export by name with nothing downloaded. The boot pass moved
the projects saved before the suffix, and the index let go of them; a
project saved now kept the suffix in a key whose uuid is not its asset
id, and was never indexed, while a plain .json saved beside it was.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t9.txt prd.md`, then `MSYS_NO_PATHCONV=1 wsl bash $W/push.sh plan-a-prd.log`, then `MSYS_NO_PATHCONV=1 wsl bash $W/poll-ci.sh <sha>`. Expected: `PUSH_RC=0`, and every run `completed | success`. A PRD-only push does not touch the Stryker cache key, so CI is short this time.

---

## Contract for Plans B and C

What Plan A lands and later plans may rely on. These are the exact names.

**Imports**
- `PROJECT_FILE_EXTENSION` from `web/src/videoStudio/projectStore.ts`.
  - Value: `'aura-video.json'`, with **no leading dot**.
  - A project file is named `projectFileName(project, PROJECT_FILE_EXTENSION, name?)`, which yields `<slug>.aura-video.json`.
  - The sidecar names every project file it saves this way.
- `StudioProjectSuffix` from `internal/objectstore`.
  - Value: `".aura-video.json"`, **with** the dot.
  - `objectstore.AssetKey(id, name, folder)` keeps it whole in the key.
  - Go code that finds projects by key uses this constant.
- `STUDIO_PROJECT_PATTERN` and `path_matcher()` in `services/ingest/source.py`.
  - The pattern is `"**/*.aura-video.json"`.
  - `path_matcher()` is the one matcher both the walker and the audit use.

`TestStudioProjectSuffixMatchesTheCockpitAndTheIngest` fails if any of the three spellings drifts. If the sidecar imports `PROJECT_FILE_EXTENSION`, there is no fourth spelling. Only if it declares the suffix again in its own code does that line join the test.

**Keys and ids**
- The uuid in an object key is the one Presign minted for the object, never the asset row's id. Task 0 counts the two coinciding on the lab VM and Task 9 measures one pair. **No plan derives an asset id from a key**, or a key from an asset id: the row holds both.
- A file-manager **Rename** writes the typed name into the key (`internal/assets/filemanager_ops.go` `Rename`). A project renamed to a name without the suffix gets a key without it, and the ingest indexes it from then on. A **Move** keeps the key's base name, and so keeps the suffix.

**Projects saved before Plan A**
- `StudioProjectRekey` (`internal/assets`) runs once at every boot and moves each pre-Plan-A project that passes its rule to `chat/<uuid>.aura-video.json`. The row and its id stay the same.
- A project that fails the rule keeps its `.json` key and stays indexed: one renamed in the file manager, one attached in a thread, one over 4 MiB, one whose bytes the content check refuses. A listing "by suffix" misses those. A listing that must include them has to read the content, as `loadProject` does with its `isProject` check (`projectStore.ts:409`, not exported today), not the name.

**Export errors**
- `ExportSourceError` from `web/src/videoStudio/videoflow_media.ts`.
  - `name === 'ExportSourceError'`.
  - `message === 'videoStudio: source <assetId> is <failure>'`.
  - Fields: `assetId: string` and `failure: 'unreachable' | 'undecodable'` (the type `SourceFailure`).
- **Across a page boundary it travels as plain data.** A class instance does not survive a structured clone, a `postMessage` or JSON, and `instanceof` fails on the far side. Plan B's render page catches `ExportSourceError` in the page and hands on `{ assetId, failure }`; the sidecar matches on those two fields, never on the class.
- `assetId` is the project's asset id: a source's `assetId`, its `denoisedAssetId`, or an overlay's `props.assetId`. It falls back to the URL itself only when no project asset maps to that URL.
- **What a person reads is the file name.** The cockpit's dialog resolves `assetId` to the asset row's `file_name` (`getAsset`), and falls back to the id when the row has none. Anything shown to a person does the same. `assetId` stays the machine field.
- `unreachable`: the bytes never arrived (any non-2xx, a CORS refusal, a dead network, a body that broke mid-download). The HTTP status is **not** available.
- `undecodable`: the bytes arrived and a layer could not play them, or a sound that is not muted would not decode.

**Export functions**
- `exportProject(project, urls, options?)`: **the signature is unchanged**. `options` is `{ onProgress?, signal? }`.
  - It resolves only with the `Blob` VideoFlow's encoder produced.
  - It rejects with `ExportSourceError` before `exportVideo` is called.
  - On abort while the media loads, it rejects with the signal's reason once VideoFlow's fetches have settled. VideoFlow's fetch takes no signal, and releasing what they acquired matters more than a few milliseconds.
  - On abort while it encodes, **the encoder's error wins**: the rejection is whatever VideoFlow's `exportVideo` throws when its signal fires, not necessarily the signal's reason. A caller tells an abort apart by its own `signal.aborted`, as the cockpit's dialog does.
- `exportProjectAudio(project, urls, signal?)`: unchanged. It rejects with `ExportSourceError` or `NoProjectAudioError`.
- `renderLoaded<T>(renderer, project, urls, signal, render): Promise<T>` from `videoflow_media.ts` is what both exports share. It loads, refuses or decodes, runs `render`, and destroys the renderer once. A render page does not call it: it calls `exportProject`.
- `toVideoJSON(project, urls)` is unchanged in signature. Its output now carries every fade in `animations` on the source clock (`withKeyframes`), and the Stage preview shares that output.

**What Plan A does not guarantee**
- **A video's own audio that will not decode still exports silent**, with no error. VideoFlow hard-codes `RuntimeVideoLayer.hasAudio` to true, so a clip with no audio track always reaches the decoder, and the mixer drops it by design. A sound layer that will not decode does stop the export.
- So **the sidecar's output check stays mandatory**: black, silent, too short, a missing stream (spec §Tests). `ExportSourceError` covers a source that could not load. It says nothing about what a loaded source contained.
- Encoding is unchanged: H.264 + Opus in MP4 (spec S1.1).

**Facts the sidecar must know**
- Fetch-once is not a URL map. It works because every layer and the mixer take their bytes from VideoFlow's per-page `loadedMedia`, keyed by the exact URL `urls.assetUrl(assetId)` returns.
  - The sidecar's page must hand `exportProject` one stable URL per asset.
  - Its proxy must serve those URLs (S1.4's CORS finding still applies).
  - A URL that changes between layers of one source, such as a fresh signature per call, would be fetched once per distinct URL.
- **`assetUrl` must not carry a credential.** When no project asset maps to a URL, `ExportSourceError.assetId`, and so its `message`, is that URL. A presigned URL would put its signature into the error, the job's state and every log line that prints it. The sidecar's `assetUrl` returns its proxy's credential-free route, one per asset id.
- The failure is decided after `initLayers`. The sidecar can map `{ assetId, failure }` straight to the job's "failed, naming the source" state. This path never hands a black or silent file to its output check.
- The E2E helpers `readFrames` and `meanLevel` (`web/e2e/support/frames.ts`) are there for Plan C's reel checks (black frames, sampled frames).

**Spec sentences superseded.** Task 0 dates two spec sentences that the measurements overturned (spec `:160` and `:276`). Plans B and C read the notes beside them, not the original sentences.

## Spec coverage

| Spec requirement | Where |
|---|---|
| Fix 1: a failed source fails the export, naming it; the dialog shows the reason | Tasks 2 and 3 (by file name, ruled 2026-10-01); Task 8 tests 2 and 3 on the lab VM |
| Fix 2: fade-out appears, confirmed on a rendered frame | Task 1 (runtime layers); Task 8 test 1 (exported frames, against a static control) |
| Fix 3: each source fetched once | Task 2 (`[1, 1, 1]` against VideoFlow's real `MediaCache`); Task 8 test 1 (`fetchedByExport` on the real page) |
| M1: `<slug>.aura-video.json`; `projectFileName` writes it | Task 4; the key keeps it: Task 6 (spec ruling 1) |
| M1: the ingest skips the suffix; other `.json` still indexed | Task 5; measured on the VM in Task 9 Step 2 |
| M1: projects saved before the change | Task 7 (moved once at boot, ruled 2026-10-01); counted in Task 0; measured leaving the index in Task 9 Step 1 |
| Tests and E2E: failing test first, E2E on the lab VM | every task's RED step; Task 8 RED on the VM, GREEN locally before the push, GREEN on the VM |
| Mutation only in CI | Tasks 1 and 2 add the new modules to `stryker.config.json` and their suites to `vitest.stryker.config.ts`; read after Task 8's push |
| What this does not prove | Task 0's two lists; Task 9 completes them |
