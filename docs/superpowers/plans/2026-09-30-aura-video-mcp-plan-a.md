# aura-video-mcp Plan A: a trustworthy Studio export and projects kept out of RAG — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Studio export either writes the film the editor shows (every fade included, each source fetched once) or stops, names the source it could not play, and writes nothing. A saved Studio project never becomes a searchable document.

**Architecture:** There are two seams in the cockpit's VideoFlow adapter.
1. After `flow.compile()`, `withKeyframes` (new `videoflow_keyframes.ts`) moves every keyframed property from a layer's static `properties` into its `animations`, on the layer's source clock. That is the only place VideoFlow 1.3.4's renderer reads keyframes.
2. Before `exportVideo`, `loadSources` (new `videoflow_media.ts`):
   - lets VideoFlow load every layer through its own `loadedMedia` cache;
   - throws `ExportSourceError` when VideoFlow disabled a layer;
   - pre-decodes the audio from the bytes that cache already holds.

For projects:
- a project file is named `<slug>.aura-video.json`;
- the Go object key keeps that suffix whole;
- the ingest's one path matcher excludes `**/*.aura-video.json`.

**Tech Stack:**
- TypeScript + React, with VideoFlow 1.3.4 (`@videoflow/core` and `@videoflow/renderer-browser`, patched by `web/patches/@videoflow+renderer-browser+1.3.4.patch`, which touches only `googleFontLoader.js`);
- vitest (jsdom) and Playwright;
- Python 3.12 with CocoIndex 1.0.24 (`services/ingest`, pinned in `docker/aura-ingest/requirements.txt:13`);
- Go (`internal/objectstore`).

**Spec:** `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md`, sections:
- §"Fixes to the Studio export";
- §Architecture → "Project indexing (M1 of the audio spec)";
- §"Tests and E2E";
- §"What this does not prove".

The evidence is `spikes/video-mcp-render/FINDINGS.md` §S1.3 and §S1.4.

## Global Constraints

**Scope**
- Plan A covers six things and nothing else:
  1. a source that cannot be fetched fails the export, naming it;
  2. fades reach the export;
  3. each source is fetched once;
  4. projects are named `<slug>.aura-video.json`;
  5. the ingest skips `**/*.aura-video.json`;
  6. the E2E on the lab VM.
- The sidecar, the render jobs and the tools belong to Plans B and C.
- `exportProject(project, urls, options?)` keeps its signature; Plan B's render page calls it unchanged.
- VideoFlow stays at 1.3.4 with the existing patch: no fork, no new patch.

**Spec rules**
- "The suffix keeps the `.json` extension the upload allowlist accepts (`internal/assets/limits.go`)". Quoted from the spec.
- "Any other `.json` the operator uploads is still indexed". Quoted from the spec.

**Files and quality bars**
- Every new user-facing string exists in en and it, in `web/src/i18n/resources.videoStudio.ts`. Re-read that file just before editing it: other sessions edit i18n.
- No file over 600 lines. `scripts/check-file-size.sh` runs in pre-commit.
- Coverage is at least 85 % statements, branches and lines on every touched web file (vitest).
- The Go package `internal/objectstore` keeps its exact baseline, re-pinned to the new denominator (Task 6) and never lowered.
- Mutation testing runs in CI only (Stryker, `break: 70`, `web/stryker.config.json:79`). Never run it locally.

**How to run tools**
- Never run a Windows `.exe` from Git Bash. `node`, `npx`, `go`, `python3` and `docker` are all exes there.
- Run tools in WSL through script files: `MSYS_NO_PATHCONV=1 wsl bash <script> <args>`. Never put `$VAR` inside `wsl bash -c "…"`.
- Before any WSL web command, run `MSYS_NO_PATHCONV=1 wsl bash $S/linuxbind.sh --install` and expect `missing=0`.
- Never run `npm install` or `npm ci`.
- Never run `go build ./...`. The pre-push hook builds the pushed tree.

**Commits and pushes**
- Commit exact paths only, from WSL with lefthook (`$W/commit.sh`, below).
- `--no-verify` is forbidden.
- This tree is shared: never stage, reset or revert another session's files.
- There are two pushes: the code in Task 7, and the PRD's post-fix record in Task 8. CI must be green before the lab-VM E2E runs.

**The lab VM and the PRD**
- The E2E runs against the lab VM only through `$S/e2e-vm.sh`.
- Nothing else touches the VM except the read-only measurements (Task 0) and the controlled probe (Task 8).
- The PRD is written first: it records a measurement before the code that relies on it (Task 0). Every amendment says what it does not prove (CLAUDE.md "PRD-first principle").

**Path shorthand**
- `$S` means `/mnt/c/Users/Davide/AppData/Local/Temp/claude/d--Aura/a8737a98-902e-408e-8daa-79e51d3a3ecc/scratchpad`, the operator's helper directory as WSL sees it.
- `$W` means your own session's scratchpad as WSL sees it.
- Tool shells do not keep variables. Either write the path out in full, or start the same command line with `S=… W=…`.
- Plain Git Bash commands in this plan (`wc -l`, `git -C /d/Aura …`, `find`) run from the repository root, `/d/Aura`. There, `$W` is spelled `/c/Users/…/scratchpad`: `/mnt/c` exists only inside WSL.

## Review Focus

No task's main path exercises these five inputs, and each is the likeliest way this plan could fail a real user. Each is pinned by a test in the task that owns the code.

1. **A clip at a speed other than 1× that fades.** The fade lasts half a second of what the viewer sees, not of source. Pinned in Task 1 by "fades a sped-up clip over half a second of film, not of source".
2. **A flipped clip.** `scale: [-1, 1]` is an array but not a keyframe list, and must stay a static value. Pinned in Task 1 by "leaves no keyframes in a layer's static properties".
3. **A source the browser cannot reach at all** (a CORS refusal or a dead network, so no HTTP status). The export still stops as `unreachable`, by asset id. Pinned in Task 2 by "a CORS refusal has no status".
4. **The lost bytes are a clip's cleaned copy or a picture over the film.** The error names that asset, not the original clip and not a URL. Pinned in Task 2 by the `it.each` "names … by its own asset".
5. **A name that only looks like the marker** (`reel.aura-video.json.bak`, `reel-aura-video.json`), or one in capitals or with a Windows path. The key and the matcher agree: only a name that ends in `.aura-video.json` is skipped. Pinned by Task 5's inclusion cases and Task 6's table.

## What the plan stands on (verified 2026-09-30)

**The fade root cause, found by systematic debugging and reproduced against the real compile.**
1. `clipOpacity` (`web/src/videoStudio/videoflow.ts:58-78`) hands the builder an opacity *keyframe array* as the layer's initial property (`:195`).
2. VideoFlow's compile stores any initial property as the value of ONE step keyframe at `sourceStart` (`web/node_modules/@videoflow/core/dist/VideoFlow.js:629-638`).
3. It then serialises a one-keyframe property as a static value (`:856-864`), so the whole array lands in `properties.opacity`.
4. `BaseLayer.toJSON` would have recognised it (`core/dist/layers/BaseLayer.js:231`, `value[0]?.time !== undefined`), but the flow compile never goes through that path.
5. At render time a static property goes through `ensureUnit` (`renderer-browser/dist/layers/RuntimeBaseLayer.js:311-323`, `:472-487`), which turns an array of objects into `["NaN","NaN"]`.
6. `LayerRasterizer.js:909` sets `ctx.globalAlpha = Math.max(0, Math.min(1, Number(props.opacity ?? 1)))`, which is NaN, and a canvas ignores a NaN `globalAlpha`. The layer is drawn fully opaque.

Proof: `videoflow_keyframes.test.ts` (Task 1) builds the renderer's own runtime layers from the compiled JSON with `createBuiltinLayerTypeRegistry` and reads `applyTransitions(frame, getPropertiesAtFrame(frame)).opacity`. On today's code, 5 of 5 tests fail with `expected NaN to be close to 0.4` and `expected { fit: 'cover', mute: true, …(8) } to not have property "opacity"`.

The same bug hits more than clip fades. It also hits title fades (the inspector writes `props.opacity` keyframes), the fade-to-black and fade-to-white washes, and the Stage preview. The Stage compiles with the same `toVideoJSON` (`Stage.tsx:238`), and `@videoflow/renderer-dom` builds its layers with renderer-browser's `createBuiltinLayerTypeRegistry` and draws with its `LayerRasterizer` (`renderer-dom/dist/DomRenderer.js:43`). The preview was not measured on screen. Keyframe times must be absolute source seconds (`sourceTimeAtFrame`, `RuntimeBaseLayer.js:175-186`), the rule the PRD already records for volume (prd.md §12 "VideoFlow volume (S1)").

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

**Why the key, not the name, must carry the marker.**
- The S3 walker matches the key relative to the prefix (CocoIndex 1.0.24 `connectors/amazon_s3/_source.py:304-317`).
- `PatternFilePathMatcher` uses globset semantics (`cocoindex/resources/file.py:227-279`), and `**/*.aura-video.json` matches at the bucket root too (measured by Task 5's pytest).
- `objectstore.AssetKey` builds `<folder><assetID><ext>` (`internal/objectstore/asset_placement.go:90-92`). The real name rides in S3 metadata (`PlaceAsset`, `:40-45`), which only a HEAD reads.
- Today `assetExtension` keeps only `path.Ext` (`:99`): `A-film.aura-video.json` would be stored as `chat/<id>.json`, and the ingest could not tell it from any other JSON.

**Spec rulings** (the spec is corrected here, and in the PRD by Task 0):
1. "The ingest skips that suffix" cannot hold by the file name alone, because keys carry no names. Go keeps the suffix in the key (Task 6), and a parity test binds the three spellings.
2. "Naming the source": the page never learns the HTTP status (above). The error carries the **asset id** and `unreachable` | `undecodable`. The dialog shows the asset id, the one name the project file holds for a source.
3. "Projects saved before the change stay indexed until they are saved again" is inaccurate. Saving writes a **new** asset (every version is kept), so an old `chat/<id>.json` project stays indexed until it is deleted.
4. The fade bug is wider than clip fade-outs (titles, washes, preview). One fix at the compile seam covers them all.
5. A clip's fade lasts half a second of **film** (`clipTimelineDuration`), not of source. `clipOpacity` placed its fade by `clip.duration`, the clip's length in source seconds, so at 2× the fade-out's keyframes would fall past the clip's last frame on screen.
6. A sound whose bytes will not decode fails the export (it would export as silence). A *video* whose audio will not decode is still left to the mixer, as before: VideoFlow hard-codes `RuntimeVideoLayer.hasAudio` to true, so a clip with no audio track always reaches the decoder.
7. Two existing tests are rewritten, not weakened. The commit body in Task 2 justifies both, per CLAUDE.md "NEVER MODIFY TESTS TO MAKE THEM PASS":
   - one stood for "no audio track" with a *rejected fetch*, which is the very failure this plan stops swallowing;
   - the other held a fetch this plan deletes.
8. `loadProject` reads a project by asset id and never looks at its name (`projectStore.ts:428-443`), so old `.json` projects still load. Nothing is added to prove it: a test would be a tautology.
9. The asset still receives a `document_id`, because `DocumentProcessor` names one for every document. That is harmless. It "writes nothing and reads nothing", and the knowledge catalog asks ArcadeDB whether a document is indexed (`internal/assets/document_processor.go:11-33`).

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `prd.md` | modify §12 (after `:688`, and `:714-715`) | record the measured bugs and the rules (Task 0); the post-fix measurement (Task 8) |
| `web/src/videoStudio/videoflow_keyframes.ts` | create | `withKeyframes(json)`: keyframed properties → `animations`, on the source clock |
| `web/src/videoStudio/__tests__/videoflow_keyframes.test.ts` | create | real compile and real runtime layers: the opacity the renderer draws |
| `web/src/videoStudio/videoflow_media.ts` | create | `loadSources`, `ExportSourceError`, `SourceFailure`, and the pre-decode moved out of `videoflow.ts` |
| `web/src/videoStudio/videoflow.ts` | modify | `clipOpacity` on film time; `withKeyframes` after compile; `loadSources` in `exportProject`; the pre-decode removed (400 → 341 lines) |
| `web/src/videoStudio/videoflow_exportAudio.ts` | modify `:4`, `:27` | `loadSources` instead of `primeDecodedBuffers` |
| `web/src/videoStudio/__tests__/videoflowFakes.ts` | modify | VideoFlow's real `MediaCache` behind the fake renderer; `layer()`; a decoder that can refuse |
| `web/src/videoStudio/__tests__/videoflow_export.test.ts` | rewrite | fetch counts, failure by name, abort while media loads |
| `web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts` | create | the WAVE export (at 0 % coverage today) |
| `web/src/videoStudio/VideoStudio_export.tsx` | modify `:1-9`, `:30-32`, `:67-75` | `failureText`: the dialog names the source |
| `web/src/i18n/resources.videoStudio.ts` | modify (en `export` near `:208`, it near `:415`) | `sourceUnreachable`, `sourceUndecodable` |
| `web/src/videoStudio/__tests__/VideoStudio_export.test.tsx` | create | the dialog's failure text through the real i18n bundle, en and it |
| `web/stryker.config.json` | modify `:74-77` | the two new modules join the CI mutation gate |
| `web/src/videoStudio/projectStore.ts` | modify `:16-18`, `:39-40`, `:68-71` | `PROJECT_FILE_EXTENSION`; `saveProject` names the file with it |
| `web/src/videoStudio/__tests__/projectStore.test.ts` | modify (import, and a test after `:182`) | the saved file's name |
| `services/ingest/source.py` | modify `:46`, `:96-103`, `:183-193` | `STUDIO_PROJECT_PATTERN`, `path_matcher()`, shared by `walk` and `expected_keys` |
| `services/ingest/tests/test_studio_projects.py` | create | CocoIndex's own matcher: projects out, other JSON in |
| `internal/objectstore/asset_placement.go` | modify `:94-100` | `StudioProjectSuffix`; `assetExtension` keeps it whole |
| `internal/objectstore/studio_project_test.go` | create | the key table and the three-language parity test |
| `scripts/coverage_package_policy.json` | modify `:57` | re-pin `internal/objectstore` 456/598 → 459/601 |
| `web/e2e/support/frames.ts` | create | `readFrames`, `meanLevel`: frames of an exported file, decoded in the page |
| `web/e2e/video-studio.spec.ts` | modify `:78-173` | `twinFrameDiff` reads its frames through `readFrames` (398 → 339 lines) |
| `web/e2e/support/videoStudio.ts` | modify `:161-168` | `reopen` uploads `project.aura-video.json` |
| `web/e2e/video-studio-export.spec.ts` | create | fade measured on frames, lost source by name, the saved key |
| `internal/webui/dist` | rebuild | the embedded cockpit the VM runs |

## Helper scripts

The operator's helpers in `$S`. Read each one before you first use it.

| Script | What it does |
|---|---|
| `linuxbind.sh --install` | adds the Linux bindings beside the Windows `node_modules`; expect `missing=0` |
| `vtf.sh <paths…>` | vitest in `/mnt/d/Aura/web` on those paths: failures, then the `Test Files` and `Tests` lines |
| `webcheck.sh <files…>` | `prettier --write` on the files, then `tsc`, oxlint (read `Found N warnings and N errors`), `lint-contract`, knip and `prettier --check .` |
| `covre.sh <fragments> <paths…>` | vitest coverage over the paths, printing the rows whose file cell matches any comma-separated fragment. The text reporter truncates long names from the left (`...oflow_media.ts`), so pass name **tails**. |
| `build.sh` | builds the web app into `internal/webui/dist` and prints how many dist files changed |
| `push.sh <log name>` | `git push origin master` with lefthook's pre-push gates; the log goes into `$S` |
| `poll-ci.sh <sha>` | run from **Git Bash** as `bash /c/Users/Davide/AppData/Local/Temp/claude/d--Aura/a8737a98-902e-408e-8daa-79e51d3a3ecc/scratchpad/poll-ci.sh <sha>`; waits until no GitHub Actions run for that commit is queued or running, then prints each run's conclusion |
| `waitfix.sh <sha>` | waits (up to an hour) until the lab VM's `aura` image contains that commit |
| `e2e-vm.sh <playwright args…>` | Playwright from `/mnt/d/Aura/web` against `https://192.168.101.158` as the operator's cockpit account |
| `vmsql.sh <file in $S>` | read-only SQL on the lab VM's Postgres |

Create these in `$W`. Every one runs from WSL as `MSYS_NO_PATHCONV=1 wsl bash $W/<name>.sh …`.

`$W/commit.sh`: commits exactly the given paths, with lefthook's pre-commit gates.

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

`$W/go-objectstore.sh`: the Go gates for the one Go package this plan touches.

```bash
#!/usr/bin/env bash
# gofmt, vet, race tests, the unit statement count and lint on internal/objectstore.
set -uo pipefail
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
cd /mnt/d/Aura
export PATH="$(go env GOROOT)/bin:$PATH"
echo "== gofmt (no file listed = formatted)"; gofmt -l internal/objectstore
echo "== vet"; go vet ./internal/objectstore/
echo "== race"; go test -count=1 -race ./internal/objectstore/
echo "== statements"
go test -count=1 -coverprofile=/tmp/objectstore.cover ./internal/objectstore/ > /dev/null && \
  awk 'NR>1 {t+=$2; if ($3>0) c+=$2} END {print "unit tier covered/total =", c "/" t}' /tmp/objectstore.cover
echo "== lint"; golangci-lint run ./internal/objectstore/...
```

`$W/ingest-pytest.sh`: the ingest's tests with the CocoIndex the image pins. The venv lives outside the repository; the authoritative full suite is CI's `make ingest-test` (`.github/workflows/ci.yml:963`).

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
cd /mnt/d/Aura/services
PYTHONDONTWRITEBYTECODE=1 "$V/bin/python" -m pytest -p no:cacheprovider -q "$@"
```

---

### Task 0: The PRD records the measurements and the rules first

CLAUDE.md "PRD-first": measure, then amend, then implement. The bugs are measured (FINDINGS.md S1.3 and S1.4, on the lab VM). The key shape is measured here, read-only. The post-fix numbers come in Task 8.

**Files:**
- Modify: `prd.md`: insert a paragraph after `:688`, and amend the M1 sentence at `:714-715`.
- Create (outside the repo): `$S/plan-a-json-keys.sql`.

**Interfaces:**
- Consumes: nothing.
- Produces: the PRD paragraph "VideoFlow opacity and media". Task 8 appends its measurement to the amended M1 sentence.

- [ ] **Step 1: Measure the saved JSON assets' keys on the lab VM (read-only)**

Write `$S/plan-a-json-keys.sql`:

```sql
\echo '== live JSON assets, newest first (read-only)'
SELECT created_at::timestamp(0) AS created, status, object_key, file_name,
       NULLIF(document_id::text, '') IS NOT NULL AS named_document
FROM aura.assets
WHERE mime_type = 'application/json' AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 15;
\echo '== keys by ending'
SELECT CASE WHEN object_key LIKE '%.aura-video.json' THEN '.aura-video.json'
            WHEN object_key LIKE '%.json' THEN '.json' ELSE 'other' END AS ending,
       count(*) AS assets,
       count(*) FILTER (WHERE NULLIF(document_id::text, '') IS NOT NULL) AS named_documents
FROM aura.assets
WHERE mime_type = 'application/json' AND deleted_at IS NULL
GROUP BY 1;
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vmsql.sh plan-a-json-keys.sql`

Expected:
- every `object_key` is `chat/<uuid>.json`, with the file's name nowhere in it;
- the `.aura-video.json` row is absent;
- the names appear only in the `file_name` column (a Studio save looks like `<slug>.json`).

Keep the counts and today's date for Step 3. If any key carries a name or another shape, stop and report it: the measurement wins over this plan (CLAUDE.md).

- [ ] **Step 2: Record the opacity and media findings**

In `prd.md`, after the paragraph that ends `volume curve into \`animations\`, beginning at \`sourceStart\`.` (`:688`), insert one blank line and then:

```markdown
**VideoFlow opacity and media (aura-video-mcp spikes S1.3 and S1.4, 2026-09-30).** The same rule
binds every keyframed property, and the compile broke it for opacity. VideoFlow's builder takes a
layer's initial properties as values: handed a keyframe array, `compile()` stores it as the value
of one step keyframe and writes it out as a static property (core `dist/VideoFlow.js:629-638`,
`:856-864`). The renderer reads that array as a vector and unit-converts each keyframe to `NaN`
(renderer-browser `dist/layers/RuntimeBaseLayer.js:311-323`), and a NaN opacity draws as full
opacity (`dist/LayerRasterizer.js:909`). Every clip fade, title fade and fade-to-black or
fade-to-white wash therefore exported at full opacity: S1.3 read RGB (233, 100, 90) in a fade-out
frame where opacity 0.4 over black allows 102. The compile now moves every keyframed property into
`animations`, on the layer's source clock, and a clip's fade lasts half a second of film at any
speed. VideoFlow disables a layer whose media fails to load, logs a warning and lets the export
resolve (`dist/BrowserRenderer.js:427-445`): S1.4 measured a black, silent 26,604 B MP4 reported
as a success, and every source fetched twice, once by VideoFlow's media cache and once by the
pre-decode. The export now loads every source once, through that cache (`loadedMedia`), and
refuses to write a file when a layer was disabled, naming the source's asset and whether its bytes
never arrived or did not play.

This does not establish:
- the HTTP status of a failed fetch: VideoFlow keeps it in its console warning and the page never
  sees it;
- what an expired presigned URL or a 403 does: S1.4 exercised only a missing CORS rule;
- the Stage preview on screen: it compiles the same JSON and builds the same runtime layers
  (`@videoflow/renderer-dom` imports them from renderer-browser), but only the export was measured.
```

- [ ] **Step 3: Record the rule for saved projects**

In the same section, replace exactly this text (`:714-715`):

```markdown
(M1): that is
reported as its own issue and is not fixed by this work.
```

with the text below. Fill in the three `⟨…⟩` slots from Step 1's output.

```markdown
(M1): that was
reported as its own issue. aura-video-mcp Plan A (2026-09-30) keeps saved projects out of the index
by path, because a path is all the ingest sees: its matcher reads the object key (CocoIndex 1.0.24
`connectors/amazon_s3/_source.py:304-317`), and the key carries an extension but never the name,
which rides in S3 metadata (`internal/objectstore/asset_placement.go`). The Studio names a project
`<slug>.aura-video.json`, the key keeps that suffix whole, and the ingest excludes
`**/*.aura-video.json`. Measured ⟨date⟩ on the lab VM, read-only: ⟨n⟩ live JSON assets, every key
`chat/<id>.json` with no name in it, ⟨m⟩ of them named as documents. A project saved before the
change keeps its `.json` key and stays indexed until it is deleted: saving again writes a new asset.
```

- [ ] **Step 4: Commit the PRD alone**

Write `$W/msg-t0.txt`:

```text
docs(prd): record why Studio fades and lost sources never reached the export

The aura-video-mcp spikes measured two Studio export bugs on the lab VM
(FINDINGS S1.3, S1.4): every opacity keyframe exported at full opacity,
and a source the page could not fetch became a black, silent MP4
reported as a success, with every source fetched twice. This records the
cause read in VideoFlow 1.3.4's source and the rules the next commits
implement: keyframed properties compile into animations on the source
clock, and the export loads each source once through loadedMedia and
refuses to write a file when a layer was disabled.

It also records how a saved project leaves the index: the ingest's
matcher reads only the object key, and the key carries no name, so the
Studio suffix .aura-video.json must survive into the key. The key shape
was measured read-only on the lab VM before the change.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t0.txt prd.md`

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
- Modify: `web/stryker.config.json:77`

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

Run `MSYS_NO_PATHCONV=1 wsl bash $S/linuxbind.sh --install` and expect `missing=0`. Then:

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio/__tests__/videoflow_keyframes.test.ts`

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
// that array as a vector and unit-converts each keyframe object to "NaN" (renderer-browser
// dist/layers/RuntimeBaseLayer.js:311-323, 472-487), and a NaN opacity draws as full opacity: the
// canvas ignores a NaN `globalAlpha` (LayerRasterizer.js:909), and CSS rejects the
// `opacity: NaN NaN` the DOM path writes (RuntimeBaseLayer.js:770).
// So every fade the Studio wrote — a clip's, a title's, a junction's dip to black — exported at
// full opacity: spikes/video-mcp-render/FINDINGS.md S1.3 measured RGB (233, 100, 90) where 0.4
// over black allows 102.
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

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio/__tests__/videoflow_keyframes.test.ts src/videoStudio/__tests__/videoflow.test.ts`

Expected: `Test Files  2 passed (2)`, `Tests  17 passed (17)`. The 12 tests of `videoflow.test.ts` are unchanged: they assert what the builder is handed, and the builder is still handed the same keyframes.

Both halves of Step 4 are load-bearing. Measured in scratch with `withKeyframes` in place but `clipOpacity` still using `clip.duration`: `Tests  1 failed | 4 passed (5)`. The sped-up clip fails with `expected 1 to be close to 0.4`, because its fade-out lands past its last frame on screen.

- [ ] **Step 6: Put the new module under the CI mutation gate**

In `web/stryker.config.json`, replace:

```json
    "src/videoStudio/videoflow_audio.ts"
```

with:

```json
    "src/videoStudio/videoflow_audio.ts",
    "src/videoStudio/videoflow_keyframes.ts"
```

- [ ] **Step 7: Task gates**

Run each of these and read its output:
1. `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio`. Expected: no `FAIL` line; `Test Files` and `Tests` both say `passed` only.
2. `MSYS_NO_PATHCONV=1 wsl bash $S/webcheck.sh src/videoStudio/videoflow_keyframes.ts src/videoStudio/videoflow.ts src/videoStudio/__tests__/videoflow_keyframes.test.ts stryker.config.json`. Expected:
   - `== tsc` prints no error;
   - oxlint prints `Found 0 warnings and 0 errors.`;
   - `== contract` and `== knip` report nothing against these files;
   - `== prettier check` ends `All matched files use Prettier code style!`.

   A file you did not touch that prettier flags belongs to another session: leave it.
3. `MSYS_NO_PATHCONV=1 wsl bash $S/covre.sh w_keyframes.ts,videoflow.ts src/videoStudio`. Expected: `% Stmts`, `% Branch` and `% Lines` are at least 85 on both rows. With all of Plan A applied in scratch, `videoflow_keyframes.ts` measured 100 % statements and 93.75 % branches, and `videoflow.ts` 92.78 % and 86.66 %.
4. `wc -l web/src/videoStudio/videoflow_keyframes.ts web/src/videoStudio/videoflow.ts web/src/videoStudio/__tests__/videoflow_keyframes.test.ts`. Expected: every file ≤ 600 (73, 406, 193).

CI note: Stryker mutates `videoflow_keyframes.ts` in CI only (`break: 70`). Read its score in the `critical-mutation` artifact after Task 7's push. Never run it locally.

- [ ] **Step 8: Commit**

Write `$W/msg-t1.txt`:

```text
fix(web): fade Studio layers where VideoFlow reads keyframes

A clip's fade-out never reached the export (spikes/video-mcp-render
FINDINGS S1.3: RGB (233, 100, 90) in a frame that 0.4 opacity over black
caps at 102). VideoFlow's builder stores a keyframe array handed to it
as an initial property as the value of a single step keyframe, and the
compile writes a one-keyframe property out as a static value (core
dist/VideoFlow.js:629-638, :856-864). The renderer unit-converts that
array to "NaN", and a NaN opacity draws at full opacity, so clip fades,
title fades and the fade-to-black and fade-to-white washes were all lost
in the export; the Stage preview compiles the same JSON.

withKeyframes moves every keyframed property into the layer's animations
on its source clock, where the renderer reads them, as the mixer already
needs for volume. A clip's fade now lasts half a second of film at any
speed: clipOpacity placed it by the clip's source length, which at 2x
puts the fade-out past the clip's last frame on screen. The new test
builds the renderer's own runtime layers from the compiled JSON and
reads the opacity they draw.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t1.txt web/src/videoStudio/videoflow_keyframes.ts web/src/videoStudio/__tests__/videoflow_keyframes.test.ts web/src/videoStudio/videoflow.ts web/stryker.config.json`

Expected: `COMMIT_RC=0`.

---

### Task 2: Each source is fetched once, and a source that cannot play stops the export by name

**Files:**
- Create: `web/src/videoStudio/videoflow_media.ts`
- Create: `web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts`
- Modify: `web/src/videoStudio/__tests__/videoflowFakes.ts` (the whole file, below)
- Rewrite: `web/src/videoStudio/__tests__/videoflow_export.test.ts`
- Modify: `web/src/videoStudio/videoflow.ts`. Line numbers are as Task 1 leaves the file:
  - the header `:1-5`;
  - an import after `:27`;
  - `MIX_SAMPLE_RATE` `:57-58`;
  - the pre-decode `:297-359`;
  - `exportProject` `:393`.
- Modify: `web/src/videoStudio/videoflow_exportAudio.ts:4`, `:27`
- Modify: `web/stryker.config.json`

**Interfaces:**
- Consumes: `toVideoJSON` and `MediaUrls` from `videoflow.ts` (Task 1's state).
- Produces, from `web/src/videoStudio/videoflow_media.ts`:

```ts
export type SourceFailure = 'unreachable' | 'undecodable';
export class ExportSourceError extends Error {
  readonly assetId: string;          // the project's asset id; the URL itself if no asset maps to it
  readonly failure: SourceFailure;
  constructor(assetId: string, failure: SourceFailure);
  // name === 'ExportSourceError'; message === `videoStudio: source ${assetId} is ${failure}`
}
export async function loadSources(
  renderer: BrowserRenderer,
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
  signal?: AbortSignal,
): Promise<void>;
```

Both exports reject with `ExportSourceError`:
- `exportProject(project, urls, options?)` keeps its signature;
- so does `exportProjectAudio(project, urls, signal?)`.

`primeDecodedBuffers` is no longer exported from anywhere.

- [ ] **Step 1: Put VideoFlow's real media cache behind the fake renderer**

Replace the whole of `web/src/videoStudio/__tests__/videoflowFakes.ts` with:

```ts
import { vi } from 'vitest';
import type { VideoProject } from '../project';

// videoflowFakes.ts — VideoFlow's builder and browser renderer stood in for, shared by
// videoflow.test.ts (what the compile hands the builder) and videoflow_export.test.ts (what the
// export does with the renderer). Each file mocks both packages with the classes below:
//
//   vi.mock('@videoflow/core', async () => ({ default: (await import('./videoflowFakes')).FakeVideoFlow }));
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
  json: { type: string; settings: { source?: string; enabled?: boolean } };
  hasAudio: boolean;
  decodedBuffer: unknown;
  /** Its bytes arrive but it cannot read them — an HEVC clip, a corrupt still. */
  unreadable?: true;
}

/** A layer over `source`, the way VideoFlow's runtime would hold it before `initLayers`. */
export function layer(
  source: string,
  options: { type?: string; hasAudio?: boolean; unreadable?: true } = {},
): FakeLayer {
  return {
    json: { type: options.type ?? 'video', settings: { source } },
    hasAudio: options.hasAudio ?? true,
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

Replace the whole of `web/src/videoStudio/__tests__/videoflow_export.test.ts` with the file below. Two of today's tests are **rewritten**; the commit (Step 10) says why.
- "leaves a source it cannot decode to the mixer" stood for a clip with no audio track by making `fetch` *reject*. That is exactly the failure the export must now report. It becomes "leaves a video whose sound will not decode to the mixer", with bytes that arrive and do not decode.
- "stops the pre-decode when the abort lands before the export starts" held open the pre-decode's own `fetch`, which this task deletes. It becomes "decodes nothing and exports nothing when the abort lands while the media loads". VideoFlow's fetch takes no signal, so its bytes land after the abort.

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { exportProject } from '../videoflow';
import { ExportSourceError } from '../videoflow_media';
import {
  calls,
  decoded,
  installFakes,
  layer,
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
    renderer.layers = [layer(A), layer(A), layer(B), layer(C, { hasAudio: false })];

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
    await expect(failure).rejects.toMatchObject({ assetId: 'asset-a', failure: 'unreachable' });
    expect(renderer.exportOptions).toHaveLength(0);
    expect(renderer.instances[0]?.destroyed).toBe(1);
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
    renderer.layers = [layer(A), layer('/api/assets/asset-still/download', { unreadable: true })];

    await expect(exportProject(project(), urls)).rejects.toMatchObject({
      assetId: 'asset-still',
      failure: 'undecodable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it('stops on a sound whose bytes will not decode, rather than exporting its silence', async () => {
    decoded.refused.add(B);
    renderer.layers = [layer(A), layer(B, { type: 'audio' })];

    await expect(exportProject(project(), urls)).rejects.toMatchObject({
      failure: 'undecodable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
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

`videoflow_exportAudio.ts` has no test today (0 % coverage). Create `web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts`:

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
      if (!wav.empty) this.target.buffer = new ArrayBuffer(44);
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
    expect(blob.size).toBe(44);
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

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio/__tests__/videoflow_export.test.ts src/videoStudio/__tests__/videoflow_exportAudio.test.ts`

Expected: `Tests  8 failed | 14 passed (22)`.
- One failure is `AssertionError: expected [ 2, 2, 1 ] to deeply equal [ 1, 1, 1 ]`: every audible source is fetched twice today.
- Seven fail with `AssertionError: promise resolved "Blob{ …(1) }" instead of rejecting`:
  - the refused source;
  - the CORS refusal;
  - the cleaned copy;
  - the picture;
  - the unreadable still;
  - the undecodable sound;
  - the sound export's refused source.

  Today each of them "succeeds".

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

/** The rate BrowserRenderer mixes at (`renderAudio`, `sampleRate: 48000`) — primed buffers match. */
const MIX_SAMPLE_RATE = 48000;

/** What the export touches on a live renderer. `initLayers` and the layers are not in
 *  BrowserRenderer's public type — see `primeDecodedBuffers` for why we reach for them anyway, and
 *  spike 108 §8 for the measurement that says it is safe. */
interface PrimableLayer {
  readonly json: {
    readonly type: string;
    readonly settings: { readonly source?: string; readonly enabled?: boolean };
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
 * it would export as silence, so it stops the export.
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
    else if (layer.json.type === 'audio')
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
export async function loadSources(
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
```

- [ ] **Step 7: Export through it**

In `web/src/videoStudio/videoflow.ts` make five edits.

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

2. After `import { withKeyframes } from './videoflow_keyframes';` add:

```ts
import { loadSources } from './videoflow_media';
```

3. Delete these three lines, which sit just above `clipOpacity`'s doc comment:

```ts
/** The rate BrowserRenderer mixes at (`renderAudio`, `sampleRate: 48000`) — primed buffers match. */
const MIX_SAMPLE_RATE = 48000;

```

4. Delete everything from `/** What the decode cache touches on a live renderer.` through the closing `}` of `export async function primeDecodedBuffers(…)`. That is the `PrimableLayer` interface, the `PrimableRenderer` interface and the whole function (`:297-359` once Task 1 has landed). All of it now lives in `videoflow_media.ts`. What follows `toVideoJSON`'s closing `}` is then the blank line and `export interface ExportOptions {`.

5. In `exportProject`, replace:

```ts
    await primeDecodedBuffers(renderer, signal);
```

with:

```ts
    await loadSources(renderer, project, urls, signal);
```

In `web/src/videoStudio/videoflow_exportAudio.ts` make two edits.

1. Replace `:4`:

```ts
import { primeDecodedBuffers, toVideoJSON, type MediaUrls } from './videoflow';
```

with:

```ts
import { toVideoJSON, type MediaUrls } from './videoflow';
import { loadSources } from './videoflow_media';
```

2. Replace `:27`:

```ts
    await primeDecodedBuffers(renderer, signal);
```

with:

```ts
    await loadSources(renderer, project, urls, signal);
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio/__tests__/videoflow_export.test.ts src/videoStudio/__tests__/videoflow_exportAudio.test.ts src/videoStudio/__tests__/videoflow.test.ts src/videoStudio/__tests__/videoflow_keyframes.test.ts`

Expected: `Test Files  4 passed (4)`, `Tests  39 passed (39)`, made up of 16 + 6 + 12 + 5.

- [ ] **Step 9: Gates**

1. In `web/stryker.config.json`, replace:

   ```json
       "src/videoStudio/videoflow_keyframes.ts"
   ```

   with:

   ```json
       "src/videoStudio/videoflow_keyframes.ts",
       "src/videoStudio/videoflow_media.ts"
   ```

2. `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio`. Expected: no `FAIL` line.
3. `MSYS_NO_PATHCONV=1 wsl bash $S/webcheck.sh src/videoStudio/videoflow_media.ts src/videoStudio/videoflow.ts src/videoStudio/videoflow_exportAudio.ts src/videoStudio/__tests__/videoflowFakes.ts src/videoStudio/__tests__/videoflow_export.test.ts src/videoStudio/__tests__/videoflow_exportAudio.test.ts stryker.config.json`. Expected:
   - `tsc` clean;
   - `Found 0 warnings and 0 errors.`;
   - knip reports no unused export: `ExportSourceError` is used by the tests, and by the dialog after Task 3; `SourceFailure` is used in its own file.
   - prettier: all matched files formatted.
4. `MSYS_NO_PATHCONV=1 wsl bash $S/covre.sh oflow_media.ts,exportAudio.ts,videoflow.ts src/videoStudio`. Expected: at least 85 in `% Stmts`, `% Branch` and `% Lines` on all three rows. Measured in scratch:
   - `videoflow_media.ts`: 100 / 100 / 100;
   - `videoflow_exportAudio.ts`: 100 / 100 / 100;
   - `videoflow.ts`: 92.78 / 86.66.
5. `wc -l web/src/videoStudio/videoflow_media.ts web/src/videoStudio/videoflow.ts web/src/videoStudio/videoflow_exportAudio.ts web/src/videoStudio/__tests__/videoflowFakes.ts web/src/videoStudio/__tests__/videoflow_export.test.ts web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts`. Expected: all ≤ 600 (165, 341, 61, 259, 274, 151).

CI note: Stryker mutates `videoflow_media.ts` in CI only (`break: 70`). Read its score in the `critical-mutation` artifact after Task 7's push. If it is under 70 %, read the surviving mutants first: kill them with a test that asserts a behaviour, never by weakening an assertion.

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

loadSources lets VideoFlow load every layer through its own loadedMedia
cache, throws ExportSourceError naming the source's asset when a layer
was disabled -- unreachable when the cache forgot the URL, undecodable
when it holds bytes that did not play -- and pre-decodes the audio from
the bytes the cache already holds. A sound that will not decode now
fails the export instead of exporting silence; a video whose audio will
not decode is still left to the mixer, as VideoFlow hard-codes
RuntimeVideoLayer.hasAudio to true.

Two export tests are rewritten, not weakened. "leaves a source it cannot
decode to the mixer" stood for a clip with no audio track with a fetch
that REJECTED -- the very failure this commit stops swallowing -- and
now uses bytes that arrive and do not decode. "stops the pre-decode
when the abort lands before the export starts" held the pre-decode's own
fetch open; that fetch no longer exists, so the abort now lands while
VideoFlow's media loads, and still nothing is decoded or exported. The
fake renderer now loads through VideoFlow's real MediaCache, so the
fetch counts are the cache's own. exportProjectAudio gains its first
tests.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t2.txt web/src/videoStudio/videoflow_media.ts web/src/videoStudio/videoflow.ts web/src/videoStudio/videoflow_exportAudio.ts web/src/videoStudio/__tests__/videoflowFakes.ts web/src/videoStudio/__tests__/videoflow_export.test.ts web/src/videoStudio/__tests__/videoflow_exportAudio.test.ts web/stryker.config.json`

Expected: `COMMIT_RC=0`.

---

### Task 3: The export dialog names the source that stopped it

**Files:**
- Create: `web/src/videoStudio/__tests__/VideoStudio_export.test.tsx`
- Modify: `web/src/i18n/resources.videoStudio.ts`, the en `export` block (`failed` at `:208`) and the it block (`failed` at `:415`)
- Modify: `web/src/videoStudio/VideoStudio_export.tsx`: imports `:1-7`, `reason` `:30-32`, and the `catch` `:67-75`

**Interfaces:**
- Consumes:
  - `ExportSourceError` from `./videoflow_media` (Task 2);
  - `NoProjectAudioError` from `./videoflow_exportAudio`, which already exists.
- Produces two i18n keys, both taking one value, `{ source: string }`:
  - `videoStudio.export.sourceUnreachable`;
  - `videoStudio.export.sourceUndecodable`.

  The en texts:
  - "The export stopped: source {{source}} could not be fetched, so no file was written."
  - "The export stopped: the renderer could not read source {{source}}, so no file was written."

- [ ] **Step 1: Write the failing test**

Create `web/src/videoStudio/__tests__/VideoStudio_export.test.tsx`:

```tsx
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
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

const project: VideoProject = {
  id: 'p',
  name: 'demo',
  size: { width: 320, height: 180 },
  fps: 30,
  sources: [],
  video: [],
  overlays: [],
};

function press(key: string): void {
  render(
    <ExportPanel
      project={project}
      fileName="demo.mp4"
      audioFileName="demo.wav"
      urls={{ assetUrl: (id) => `/api/assets/${id}/download` }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: i18n.t(key) }));
}

beforeEach(() => {
  flow.exportProject.mockReset();
  flow.exportProjectAudio.mockReset();
  downloadBlob.mockReset();
});

afterEach(async () => {
  await i18n.changeLanguage('en');
});

describe('ExportPanel failures', () => {
  it.each([
    ['en', 'The export stopped: source asset-b could not be fetched, so no file was written.'],
    [
      'it',
      'Esportazione interrotta: non è stato possibile recuperare la sorgente asset-b, quindi non è stato scritto alcun file.',
    ],
  ])('names the source it could not fetch (%s), and downloads nothing', async (language, text) => {
    await i18n.changeLanguage(language);
    flow.exportProject.mockRejectedValue(new ExportSourceError('asset-b', 'unreachable'));

    press('videoStudio.export.action');

    expect((await screen.findByRole('alert')).textContent).toBe(text);
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('names a source the renderer could not read, in the sound export too', async () => {
    flow.exportProjectAudio.mockRejectedValue(new ExportSourceError('asset-c', 'undecodable'));

    press('videoStudio.export.audioAction');

    expect((await screen.findByRole('alert')).textContent).toBe(
      'The export stopped: the renderer could not read source asset-c, so no file was written.',
    );
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('says a project with no sound has none to export', async () => {
    flow.exportProjectAudio.mockRejectedValue(new NoProjectAudioError());

    press('videoStudio.export.audioAction');

    expect((await screen.findByRole('alert')).textContent).toBe(
      i18n.t('videoStudio.export.noAudio'),
    );
  });

  it('reports any other failure with its own reason', async () => {
    flow.exportProject.mockRejectedValue(new Error('encoder gone'));

    press('videoStudio.export.action');

    expect((await screen.findByRole('alert')).textContent).toBe('The export failed: encoder gone');
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio/__tests__/VideoStudio_export.test.tsx`

Expected: `Tests  3 failed | 2 passed (5)`. Each failure shows today's dialog passing the internal message through:
- `Received: "The export failed: videoStudio: source asset-b is unreachable"`;
- `Received: "Esportazione non riuscita: videoStudio: source asset-b is unreachable"`;
- `Received: "The export failed: videoStudio: source asset-c is undecodable"`.

- [ ] **Step 3: Add the two sentences, in both languages**

Re-read `web/src/i18n/resources.videoStudio.ts` now: other sessions edit i18n, and the anchors below must still be there.

In the en block, after:

```ts
      failed: 'The export failed: {{reason}}',
```

add:

```ts
      sourceUnreachable:
        'The export stopped: source {{source}} could not be fetched, so no file was written.',
      sourceUndecodable:
        'The export stopped: the renderer could not read source {{source}}, so no file was written.',
```

In the it block, after:

```ts
      failed: 'Esportazione non riuscita: {{reason}}',
```

add:

```ts
      sourceUnreachable:
        'Esportazione interrotta: non è stato possibile recuperare la sorgente {{source}}, quindi non è stato scritto alcun file.',
      sourceUndecodable:
        'Esportazione interrotta: il renderer non è riuscito a leggere la sorgente {{source}}, quindi non è stato scritto alcun file.',
```

- [ ] **Step 4: Pick the sentence in the dialog**

In `web/src/videoStudio/VideoStudio_export.tsx` make three edits.

1. Replace the import block's first line and the `videoflow_exportAudio` import:

```ts
import { useEffect, useRef, useState } from 'react';
```

becomes:

```ts
import type { TFunction } from 'i18next';
import { useEffect, useRef, useState } from 'react';
```

and:

```ts
import { exportProjectAudio, NoProjectAudioError } from './videoflow_exportAudio';
```

becomes:

```ts
import { exportProjectAudio, NoProjectAudioError } from './videoflow_exportAudio';
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
/** What the operator reads when an export fails: the source that stopped it, by its asset id —
 *  the one name the project file holds for it — or the sound export's empty mix, or the error's
 *  own reason. */
function failureText(t: TFunction, error: unknown): string {
  if (error instanceof ExportSourceError) {
    return error.failure === 'unreachable'
      ? t('videoStudio.export.sourceUnreachable', { source: error.assetId })
      : t('videoStudio.export.sourceUndecodable', { source: error.assetId });
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
      // An abort is the operator's own decision, not a failure to report back to them.
      if (!controller.signal.aborted) setFailure(failureText(t, error));
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio/__tests__/VideoStudio_export.test.tsx src/i18n`

Expected: `Test Files  5 passed (5)`, `Tests  17 passed (17)`. The i18n suites include the en/it parity check and the static-key usage gate, which read the new `t('videoStudio.export.source…')` literals.

- [ ] **Step 6: Task gates**

1. `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio src/i18n`. Expected: no `FAIL` line. `VideoStudio.test.tsx` renders this panel and is untouched.
2. `MSYS_NO_PATHCONV=1 wsl bash $S/webcheck.sh src/videoStudio/VideoStudio_export.tsx src/videoStudio/__tests__/VideoStudio_export.test.tsx src/i18n/resources.videoStudio.ts`. Expected: `tsc` clean, `Found 0 warnings and 0 errors.`, prettier formatted.
3. `MSYS_NO_PATHCONV=1 wsl bash $S/covre.sh udio_export.tsx src/videoStudio`. Expected: at least 85 on every column. Scratch measured 94.28 statements, 96.15 branches and 93.54 lines, with this test and `VideoStudio.test.tsx`.
4. `wc -l web/src/videoStudio/VideoStudio_export.tsx web/src/i18n/resources.videoStudio.ts web/src/videoStudio/__tests__/VideoStudio_export.test.tsx`. Expected: all ≤ 600 (151, 434, 103, if no other session has added keys meanwhile).

CI note: this task adds nothing to Stryker; the dialog's choice is pinned by exact-text tests.

- [ ] **Step 7: Commit**

Write `$W/msg-t3.txt`:

```text
fix(web): name the source that stopped a Studio export

When a source could not play, the dialog showed the error's internal
message ("The export failed: videoStudio: source asset-b is
unreachable"), and before the previous commit it showed nothing at all,
because a black MP4 downloaded as a success.

failureText turns ExportSourceError into one sentence per failure, in
English and Italian, naming the source by its asset id -- the one name
the project file holds for it -- and saying that no file was written.
The page never learns the HTTP status, so the sentence does not claim
one. The new test reads the texts through the real i18n bundle.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t3.txt web/src/videoStudio/VideoStudio_export.tsx web/src/videoStudio/__tests__/VideoStudio_export.test.tsx web/src/i18n/resources.videoStudio.ts`

Expected: `COMMIT_RC=0`.

---

### Task 4: The Studio names its project files `<slug>.aura-video.json`

**Files:**
- Modify: `web/src/videoStudio/projectStore.ts`: header `:16-18`, a constant after `:39`, and `saveProject` `:69-71`
- Modify: `web/src/videoStudio/__tests__/projectStore.test.ts`: the import `:4-10`, and a test after `:183`

**Interfaces:**
- Consumes: `projectFileName(project, extension, name?)` (`projectStore.ts:54-65`), unchanged. It returns `` `${slug}.${extension}` ``.
- Produces `export const PROJECT_FILE_EXTENSION = 'aura-video.json'` from `web/src/videoStudio/projectStore.ts`. It has no leading dot, because `projectFileName` adds one. Plan B's sidecar imports it. `saveProject(project, name?)` keeps its signature; its file name becomes `<slug>.aura-video.json`. `loadProject` is untouched and still loads a `.json` project by asset id.

- [ ] **Step 1: Write the failing test**

In `web/src/videoStudio/__tests__/projectStore.test.ts`, add `PROJECT_FILE_EXTENSION` to the import:

```ts
import {
  lastSavedProject,
  loadProject,
  PROJECT_FILE_EXTENSION,
  projectFileName,
  rememberSavedProject,
  saveProject,
} from '../projectStore';
```

In `describe('saveProject', …)`, after the test `presigns a .json document, uploads it and finalizes` (it ends `expect(api.finalizeAsset).toHaveBeenCalledWith('file-1');` and `});`), add:

```ts
  it('marks the file as a Studio project, which keeps it out of the document index', async () => {
    await saveProject(project());

    const request = api.presignAsset.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(request.file_name).toBe('A-film.aura-video.json');
    expect(uploaded.files[0]?.name).toBe('A-film.aura-video.json');
    expect(PROJECT_FILE_EXTENSION).toBe('aura-video.json');
  });
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio/__tests__/projectStore.test.ts`

Expected: `Tests  1 failed | 59 passed (60)`, and the failure is:

```
Expected: "A-film.aura-video.json"
Received: "A-film.json"
```

- [ ] **Step 3: Name the file with the marker**

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

2. Replace (`:39-40`):

```ts
const PROJECT_MIME = 'application/json';
/** Long enough to recognise the project, short enough that no store has to think about it. */
```

with:

```ts
const PROJECT_MIME = 'application/json';

/**
 * What a project file's name ends in: the Studio's marker, never a separate format. It still ends
 * in `.json`, the extension the upload allowlist accepts (internal/assets/limits.go), and the
 * object key keeps it whole (internal/objectstore `StudioProjectSuffix`), which is what the
 * ingest skips (services/ingest/source.py `STUDIO_PROJECT_PATTERN`): a project is the editor's
 * state, not a document anyone searches. A project saved before the marker is plain `.json` and
 * still loads — `loadProject` reads by asset id and never looks at a name.
 */
export const PROJECT_FILE_EXTENSION = 'aura-video.json';

/** Long enough to recognise the project, short enough that no store has to think about it. */
```

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

- [ ] **Step 4: Run the test to verify it passes**

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio/__tests__/projectStore.test.ts`

Expected: `Tests  60 passed (60)`. The older `presigns a .json document` test still passes: `/\.json$/` matches the new name.

- [ ] **Step 5: Task gates**

1. `MSYS_NO_PATHCONV=1 wsl bash $S/vtf.sh src/videoStudio`. Expected: no `FAIL` line.
2. `MSYS_NO_PATHCONV=1 wsl bash $S/webcheck.sh src/videoStudio/projectStore.ts src/videoStudio/__tests__/projectStore.test.ts`. Expected:
   - `tsc` clean;
   - `Found 0 warnings and 0 errors.`;
   - knip silent: `PROJECT_FILE_EXTENSION` is used in its own file, and knip runs with `ignoreExportsUsedInFile`;
   - prettier formatted.
3. `MSYS_NO_PATHCONV=1 wsl bash $S/covre.sh projectStore.ts src/videoStudio`. Expected: at least 85 on every column. Scratch measured 96.34 / 91.42 / 98.68.
4. `wc -l web/src/videoStudio/projectStore.ts web/src/videoStudio/__tests__/projectStore.test.ts`. Expected: 456 and 536, both ≤ 600.

- [ ] **Step 6: Commit**

Write `$W/msg-t4.txt`:

```text
feat(web): name Studio project files <slug>.aura-video.json

A saved project is the editor's state, not a document anyone searches,
yet it is uploaded as a plain .json and indexed like one (PRD M1). The
Studio now marks the file with the suffix aura-video.json, exported as
PROJECT_FILE_EXTENSION for the render sidecar to share. The name still
ends in .json, which the upload allowlist accepts. Old projects keep
loading: loadProject reads by asset id and never looks at the name.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t4.txt web/src/videoStudio/projectStore.ts web/src/videoStudio/__tests__/projectStore.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 5: The ingest skips `**/*.aura-video.json`

**Files:**
- Modify: `services/ingest/source.py`: a constant after `:46`, `path_matcher()` and `walk` at `:96-102`, and `expected_keys` at `:183-193`
- Create: `services/ingest/tests/test_studio_projects.py`

**Interfaces:**
- Consumes: nothing from earlier tasks. The suffix spelling must match Task 4's `PROJECT_FILE_EXTENSION`, and Task 6's parity test enforces that.
- Produces, in `services/ingest/source.py`:
  - `STUDIO_PROJECT_PATTERN = "**/*.aura-video.json"`, on a line of its own, which Task 6's parity test reads with a regex;
  - `def path_matcher() -> PatternFilePathMatcher`, which `walk` and `expected_keys` both use.

- [ ] **Step 1: Write the failing test**

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
from ingest.tests.test_audit import _config, _install_fake_s3


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
    source.walk(object(), _config())

    assert not seen["matcher"].is_file_included(pathlib.PurePosixPath("chat/x.aura-video.json"))
    assert seen["matcher"].is_file_included(pathlib.PurePosixPath("chat/x.json"))


def test_the_audit_does_not_expect_a_project(monkeypatch):
    # The audit compares the bucket with the index: expecting a row for a project it never
    # indexes would print a MISSING line on every cycle that can never clear.
    _install_fake_s3(monkeypatch, [{"Contents": [
        {"Key": "chat/a.aura-video.json"},
        {"Key": "chat/b.json"},
    ]}])

    assert source.expected_keys(_config()) == {"chat/b.json"}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ingest-pytest.sh ingest/tests/test_studio_projects.py`

The first run creates the venv outside the repository. Expected: `10 failed`.
- Eight fail with `AttributeError: module 'ingest.source' has no attribute 'path_matcher'`.
- The walker test fails because today's matcher includes `chat/x.aura-video.json`.
- The audit test fails because `expected_keys` still returns `chat/a.aura-video.json`.

- [ ] **Step 3: One matcher, excluding Studio projects**

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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ingest-pytest.sh ingest/tests/test_studio_projects.py ingest/tests/test_audit.py ingest/tests/test_source_file_name.py`

Expected: `36 passed`. `test_audit.py` and `test_source_file_name.py` exercise `walk`, `expected_keys` and the metadata name path, so they show the refactor changes nothing else.

- [ ] **Step 5: Task gates**

1. `wc -l services/ingest/source.py services/ingest/tests/test_studio_projects.py`. Expected: 241 and 73.
2. `git -C /d/Aura status --short services/ingest` from Git Bash. Expected: only the two files. The pytest script writes no bytecode and no cache into the tree.

CI note: CI's `make ingest-test` job (`.github/workflows/ci.yml:963`) runs the whole ingest suite inside the `aura-ingest` image, with the same CocoIndex 1.0.24. There is no mutation gate for Python.

- [ ] **Step 6: Commit**

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

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t5.txt services/ingest/source.py services/ingest/tests/test_studio_projects.py`

Expected: `COMMIT_RC=0`.

---

### Task 6: The object key keeps the Studio suffix whole

**Files:**
- Modify: `internal/objectstore/asset_placement.go`: a constant before `:94`, and `assetExtension` at `:98-99`
- Create: `internal/objectstore/studio_project_test.go`
- Modify: `scripts/coverage_package_policy.json:57`

**Interfaces:**
- Consumes (the parity test reads both from disk):
  - `PROJECT_FILE_EXTENSION` from Task 4;
  - `STUDIO_PROJECT_PATTERN` from Task 5.
- Produces `const StudioProjectSuffix = ".aura-video.json"` in package `objectstore`, with a leading dot. `AssetKey(id, "x.aura-video.json", FolderChat)` returns `"chat/" + id + ".aura-video.json"`. Every other name keeps today's key.

- [ ] **Step 1: Write the failing test**

Create `internal/objectstore/studio_project_test.go`:

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
			pattern: `(?m)^export const PROJECT_FILE_EXTENSION = '([^']*)';$`,
			dot:     ".",
		},
		{
			file:    filepath.Join("..", "..", "services", "ingest", "source.py"),
			pattern: `(?m)^STUDIO_PROJECT_PATTERN = "\*\*/\*([^"]*)"$`,
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

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-objectstore.sh`

Expected: `vet` fails with `vet: internal/objectstore/studio_project_test.go:58:53: undefined: StudioProjectSuffix`, and `race` fails to compile for the same reason.

- [ ] **Step 3: Keep the suffix in the key**

In `internal/objectstore/asset_placement.go`, replace:

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

// assetExtension returns a lowercase ".ext", or "" when the name has none.
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

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-objectstore.sh`

Expected:
- `gofmt` lists no file;
- `vet` is silent;
- `race` prints `ok  	github.com/chetto1983/aura/internal/objectstore`;
- `unit tier covered/total = 419/601`;
- `golangci-lint` prints `0 issues.`

Also prove that the parity test can fail. Temporarily change the Python line to `STUDIO_PROJECT_PATTERN = "**/*.aura-video.jsn"` and run again. Expected: `source.py spells the suffix ".aura-video.jsn"; StudioProjectSuffix is ".aura-video.json"`. Restore the line, then confirm `git -C /d/Aura diff --stat services/ingest/source.py` from Git Bash shows no change left over from the check.

- [ ] **Step 5: Re-pin the package's coverage baseline**

`internal/objectstore` is a baseline-mode package: 456 of 598 statements covered. `scripts/coverage_package_gate.py:152-160` fails when the denominator changes. The change adds 3 statements, all covered by the new test. Measured in scratch: unit tier 416/598 → 419/601, and the file `asset_placement.go` went from 34/36 to 37/39.

In `scripts/coverage_package_policy.json`, replace:

```json
    "github.com/chetto1983/aura/internal/objectstore": {"mode": "baseline", "covered_statements": 456, "total_statements": 598},
```

with:

```json
    "github.com/chetto1983/aura/internal/objectstore": {"mode": "baseline", "covered_statements": 459, "total_statements": 601},
```

The CI coverage gate measures the full tag matrix. If it reports `statement denominator changed from 601 to N (now C/N …)`, another commit changed this package meanwhile. Re-pin to exactly the `C/N` it prints, and name that commit in the message.

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
if either drifts. internal/objectstore's coverage baseline is re-pinned
from 456/598 to 459/601: three new statements, all covered.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t6.txt internal/objectstore/asset_placement.go internal/objectstore/studio_project_test.go scripts/coverage_package_policy.json`

Expected: `COMMIT_RC=0`. The pre-commit gofmt, vet and lint hooks run on the staged Go files.

---

### Task 7: The E2E on the lab VM — a fade that darkens, a lost source by name, the saved key

The specs below were checked with `prettier`, `tsc` and oxlint (0 warnings, 0 errors), and the frame reader ran in WSL Chromium on `web/e2e/fixtures/video-studio/clip-a.mp4`: frames at media time 1, 2.5 and 3.8 s, each 230,400 bytes (320 × 180 × 4), mean level 123.7–125.0. The specs themselves have **not** run against the VM. This task runs them, RED first.

**Files:**
- Create: `web/e2e/support/frames.ts`
- Modify: `web/e2e/video-studio.spec.ts`: an import after `:4`, and `twinFrameDiff` with its doc comment at `:78-173`
- Modify: `web/e2e/support/videoStudio.ts:161-168` (`reopen`)
- Create: `web/e2e/video-studio-export.spec.ts`
- Rebuild: `internal/webui/dist`

**Interfaces:**
- Consumes:
  - Task 1: a fade-out's last half second draws at `(end − t) / 0.5`;
  - Task 3: the en sentence `The export stopped: source ${assetId} could not be fetched, so no file was written.`;
  - Tasks 4 and 6: the saved key is `chat/<assetId>.aura-video.json`.
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

Then replace everything from the doc comment that opens `/**` / ` * How much of the picture differs between two instants that show the same source frame, and how` (`:78`) through the closing `}` of `async function twinFrameDiff(…)` (`:173`, just above `test.describe('the multi-track video editor', () => {`) with:

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

- [ ] **Step 3: A test project is named the way the Studio names one**

In `web/e2e/support/videoStudio.ts`, replace:

```ts
/** Puts a project file in the library and opens the Studio on it, the way a reload does. */
export async function reopen(page: Page, project: object, clipAsset: string): Promise<Locator> {
  const fileId = await uploadBytes(
    page,
    Buffer.from(JSON.stringify(project)),
    'project.json',
```

with:

```ts
/** Puts a project file in the library and opens the Studio on it, the way a reload does. Named
 *  as the Studio names one, so the ingest leaves it alone as it would a saved project. */
export async function reopen(page: Page, project: object, clipAsset: string): Promise<Locator> {
  const fileId = await uploadBytes(
    page,
    Buffer.from(JSON.stringify(project)),
    'project.aura-video.json',
```

- [ ] **Step 4: Write the export spec**

Create `web/e2e/video-studio-export.spec.ts`:

```ts
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, readAsset, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { meanLevel, readFrames } from './support/frames';
import {
  editorOnSeededClip,
  exportTo,
  FIXTURES,
  reopen,
  silentFilm,
  uploadClip,
} from './support/videoStudio';

// video-studio-export.spec.ts — what the Studio's files must never do quietly, each found by the
// render spikes (spikes/video-mcp-render/FINDINGS.md): drop a clip's fade (S1.3), turn a source it
// could not fetch into black frames that download as a success (S1.4), or become a searchable
// document when saved (spec §Project indexing). The file and the key are the same at every width,
// so each is measured once, on the desktop project.

const PHONE =
  'the exported file and the saved key do not depend on the width; the desktop run measures them';

test('a clip that fades out darkens in the exported file, as far as its fade has gone', async ({
  page,
}, info) => {
  test.skip(info.project.name.startsWith('mobile'), PHONE);
  test.setTimeout(10 * 60_000);
  const clip = await uploadClip(page);
  // Two muted 4 s clips of one source: the second replays the first frame for frame, so the film
  // at t + 4 s shows the source frame it showed at t, and only the fade can tell them apart.
  const film = silentFilm(clip, 'fade check');
  const editor = await reopen(
    page,
    {
      ...film,
      video: film.video.map((item, index) => (index === 1 ? { ...item, fadeOut: true } : item)),
    },
    clip,
  );
  await expect(editor.getByRole('button', { name: 'Clip 2' })).toBeVisible({ timeout: 60_000 });

  const mp4 = readFileSync(await exportTo(page, editor, info, 'fade.mp4'));
  // Before the fade (7.2 s) and 0.3 s into its last half second (7.8 s), each beside its twin.
  const [early, earlyTwin, late, lateTwin] = await readFrames(page, mp4, [3.2, 7.2, 3.8, 7.8]);
  if (
    early === undefined ||
    earlyTwin === undefined ||
    late === undefined ||
    lateTwin === undefined
  ) {
    throw new Error('the export answered fewer frames than it was asked for');
  }
  // The opacity the renderer drew the presented frame at: 1 until 7.5 s, then down to 0 at 8 s.
  const expected = Math.min(1, (8 - lateTwin.mediaTime) / 0.5);
  const measured = {
    beforeFade: meanLevel(earlyTwin) / meanLevel(early),
    inFade: meanLevel(lateTwin) / meanLevel(late),
    expected,
    presentedAt: lateTwin.mediaTime,
  };
  // Written beside the exported file, so a run with a private `--output` keeps both to read.
  const levels = info.outputPath('fade-levels.json');
  writeFileSync(levels, JSON.stringify(measured, null, 2));
  await info.attach('fade-levels', { contentType: 'application/json', path: levels });

  expect(measured.beforeFade).toBeGreaterThan(0.9);
  // S1.3's frame at this point of its fade read RGB (233, 100, 90), where 0.4 over black allows 102.
  expect(Math.abs(measured.inFade - expected)).toBeLessThan(0.1);
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
  // The library still lists the second clip and its row is complete, so the editor opens and the
  // export is allowed to start (projectStore `sourceIsGone` reads the row, not the bytes); only
  // the bytes stop answering.
  await page.route(`**/api/assets/${lost}/download`, (route) =>
    route.fulfill({ status: 404, body: '' }),
  );
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
  const downloads: string[] = [];
  page.on('download', (download) => {
    downloads.push(download.suggestedFilename());
  });

  await editor.getByRole('button', { name: 'Export', exact: true }).click();

  // S1.4 measured the old answer: a black, silent MP4 in 4.9 s, reported as a success.
  await expect(
    editor.getByText(
      `The export stopped: source ${lost} could not be fetched, so no file was written.`,
    ),
  ).toBeVisible({ timeout: 120_000 });
  // Kept beside the run for a person to look at: the sentence where the operator reads it.
  await page.screenshot({ path: info.outputPath('lost-source.png') });
  expect(downloads).toEqual([]);
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
  // be in it, and the name — which rides in the object's metadata — must not.
  expect((await readAsset(page, saved)).object_key).toBe(`chat/${saved}.aura-video.json`);
});
```

Every asset these tests create is presigned through the page, so the `assetCleanup` fixture deletes it when the test ends (`web/e2e/support/assetCleanup.ts:17-60`).

The measurements:
- **Fade:** the ratio of a frame's mean level to its twin's, 4 s earlier.
  - Before the fade (7.2 s against 3.2 s), it must stay above 0.9.
  - 0.3 s into the last half second (7.8 s against 3.8 s), it must be within 0.1 of the opacity due at the presented frame, `(8 − mediaTime) / 0.5` (0.4 at 7.8 s).
- **Lost source:** the exact sentence, and no `download` event.
- **Project:** the key `chat/<id>.aura-video.json`.

- [ ] **Step 5: Static gates, then commit the tests**

1. `MSYS_NO_PATHCONV=1 wsl bash $S/webcheck.sh e2e/support/frames.ts e2e/video-studio.spec.ts e2e/support/videoStudio.ts e2e/video-studio-export.spec.ts`. Expected: `tsc` clean, `Found 0 warnings and 0 errors.`, knip silent, prettier formatted.
2. `wc -l web/e2e/support/frames.ts web/e2e/video-studio.spec.ts web/e2e/support/videoStudio.ts web/e2e/video-studio-export.spec.ts`. Expected: 90, 339, 325 and 142.

Write `$W/msg-t7.txt`:

```text
test(web): measure Studio fades, lost sources and saved keys end to end

video-studio-export.spec.ts proves on a real export what the unit tests
prove on the compile: a clip's fade-out darkens its frames as far as the
fade has gone (the level ratio against the same source frame 4 s
earlier, within 0.1 of the opacity due), a source whose bytes answer 404
stops the export with the sentence that names it and downloads nothing,
and a saved project's object key keeps the .aura-video.json suffix the
ingest skips.

The in-page frame reader moves out of video-studio.spec.ts into
support/frames.ts, so twinFrameDiff and the fade test share it; its
pixel arithmetic is unchanged. reopen names its project file the way the
Studio now does.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t7.txt web/e2e/support/frames.ts web/e2e/video-studio.spec.ts web/e2e/support/videoStudio.ts web/e2e/video-studio-export.spec.ts`

- [ ] **Step 6: RED on the image the lab VM runs now**

Nothing is pushed yet, so the VM still runs the old cockpit. Run `MSYS_NO_PATHCONV=1 wsl bash $S/linuxbind.sh --install` and expect `missing=0`. Then:

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/e2e-vm.sh video-studio-export.spec.ts video-studio.spec.ts --project=chrome --reporter=line --output=$W/pw-red`

Expected in `video-studio-export.spec.ts`: all three tests fail, each for its own reason.
- **Fade:** `expect(received).toBeLessThan(expected)`, with `Expected: < 0.1` and a received value near 0.6. The `fade-levels.json` under `pw-red` in your scratchpad shows `inFade` near 1.0 against `expected` near 0.4: the old cockpit draws the fading clip at full opacity.
- **Lost source:** `Timed out 120000ms waiting for expect(locator).toBeVisible()` on the "The export stopped: …" sentence. The old export downloads a black clip instead.
- **Saved key:** `Expected: "chat/<id>.aura-video.json"` and `Received: "chat/<id>.json"`.

Every chrome test in `video-studio.spec.ts` passes: `twinFrameDiff` was refactored, not changed.

If any of the three export tests passes here, stop. It does not measure what it claims, and it must be fixed before anything is pushed.

- [ ] **Step 7: Rebuild the embedded cockpit and commit it**

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/build.sh`. Expected: the build's last lines, then a count of changed dist files greater than 0.

Write `$W/msg-t7-dist.txt`:

```text
build(web): rebuild the embedded dist for the trustworthy Studio export

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t7-dist.txt internal/webui/dist`. Note the new commit's SHA: it is the **dist SHA** below.

- [ ] **Step 8: Push, then wait for CI**

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/push.sh plan-a`. Expected:
- `PUSH_RC=0`;
- `$S/plan-a.log` shows the lefthook banner, which is the proof that the pre-push gates ran. No banner means no gate.

If it prints `PUSH_RC=held`, another session holds the push: wait for it and ask. Do not force it.

Then, from **Git Bash**: `bash /c/Users/Davide/AppData/Local/Temp/claude/d--Aura/a8737a98-902e-408e-8daa-79e51d3a3ecc/scratchpad/poll-ci.sh <dist SHA>`

Expected: every run `completed | success`. These are the jobs this plan touches:
- the web unit and coverage job;
- the Stryker critical-mutation job. Read `videoflow_keyframes.ts` and `videoflow_media.ts` in its artifact: each must be ≥ 70 % killed.
- `make ingest-test`;
- the Go coverage gate, which checks `internal/objectstore` at 459/601;
- `web-e2e`, which runs the new spec against CI's own stack on the chrome project.

A red job is fixed before anything else. A red job owned by a parallel session is theirs: wait for their push.

- [ ] **Step 9: GREEN on the lab VM**

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/waitfix.sh <dist SHA>`. Expected: `VM runs <sha>, which contains <dist SHA>`. The updater restarts the edge after `aura`. If the first E2E attempt cannot reach the cockpit, wait a minute and run it again.

Run: `MSYS_NO_PATHCONV=1 wsl bash $S/e2e-vm.sh video-studio-export.spec.ts video-studio.spec.ts --reporter=line --output=$W/pw-green`

Expected:
- every test passes on both projects;
- the three export tests are reported as skipped on `mobile-chrome`.

- [ ] **Step 10: Read the measurements and look at the evidence**

1. Read `fade-levels.json`: from Git Bash, `find /c/Users/…/scratchpad/pw-green -name fade-levels.json` (your scratchpad's Windows spelling; `/mnt/c` exists only in WSL). Expected:
   - `beforeFade` > 0.9;
   - `|inFade − expected|` < 0.1, with `expected` near 0.4 and `presentedAt` near 7.8.

   Keep the four numbers for Task 8.
2. Find `lost-source.png` the same way and open it with the Read tool. Expected: the editor with the English sentence naming the lost asset, and no progress bar still running.
3. `fade.mp4`, the exported film, sits beside them for anyone who wants to look at it.

---

### Task 8: Measure the landed fix on the lab VM and complete the PRD record

The E2E proves the key. This task proves the ingest's behaviour on the real stack. It uses a controlled probe:
- save one Studio project and one plain `.json` through the cockpit's own upload door;
- let the ingest run at least two cycles;
- read what it extracted;
- read the two rows (read-only);
- delete both.

`AURA_INGEST_INTERVAL_SEC` is 60 (`services/ingest/app.py:40`). In live mode the ingest prints `[extract] <file name>` for each extraction (`app.py:236`) and `[audit] MISSING <key>` only for an object missing across two cycles (`app.py:410-427`).

**Files:**
- Create (outside the repo): `$W/vm-project-probe.sh`
- Modify: `prd.md`: the "VideoFlow opacity and media" paragraph and the M1 text written in Task 0

**Interfaces:**
- Consumes: the deployed image from Task 7, and the fade numbers from Task 7 Step 10.
- Produces: the PRD's post-fix record.

- [ ] **Step 1: Write the probe**

Create `$W/vm-project-probe.sh`. It uses the same sign-in and upload calls as the M1 probe of 2026-09-27, and the same helpers (`scripts/musr_live_run_authula_helpers.sh` `fetch_auth_config` and `sign_in`):

```bash
#!/usr/bin/env bash
# Plan A, Task 8: save a Studio project and a plain .json on the lab VM through the cockpit's own
# upload door (presign, PUT, finalize, as projectStore.saveProject does), let the ingest run at
# least two cycles, read what it extracted and what the rows say (read-only), then delete both.
# The credentials come from .env.google and are never printed.
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
BASE=https://192.168.101.158
VM=aura@192.168.101.158
PW=aura
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
curl() { command curl -k "$@"; }
. /mnt/d/Aura/scripts/musr_live_run_authula_helpers.sh
env_value() { grep -m1 "^$1=" /mnt/d/Aura/.env.google | cut -d= -f2- | tr -d '\r' | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"; }
JAR="$WORK/jar"; : > "$JAR"
read -r BASE_PATH CSRF_HEADER CSRF_COOKIE CSRF_TOKEN < <(fetch_auth_config "$JAR")
BODY="$(sign_in "$JAR" "$(env_value VM_COCKPIT_EMAIL)" "$(env_value VM_COCKPIT_PASSWORD)" "$BASE_PATH" "$CSRF_HEADER" "$CSRF_COOKIE" "$CSRF_TOKEN")"
if grep -q totp_redirect "$BODY"; then echo "FAIL: account needs TOTP" >&2; exit 1; fi
api() { curl -sS -b "$JAR" -c "$JAR" -H "Idempotency-Key: $(cat /proc/sys/kernel/random/uuid)" -H "Origin: $BASE" -H "$CSRF_HEADER: $CSRF_TOKEN" -H 'Accept: application/json' "$@"; }
json() { python3 -c 'import json,sys; v=json.load(open(sys.argv[1]))
for k in sys.argv[2:]: v=v[k]
print(v)' "$@"; }

# Saves one project-shaped JSON under $1 and prints its asset id.
save() {
  local name="$1"
  printf '{"id":"%s","name":"plan-a probe","size":{"width":320,"height":180},"fps":30,"sources":[],"video":[],"overlays":[]}' \
    "$(cat /proc/sys/kernel/random/uuid)" > "$WORK/$name"
  api -X POST "$BASE/api/assets/presign" -H 'Content-Type: application/json' \
    -d "{\"thread_id\":\"\",\"file_name\":\"$name\",\"mime_type\":\"application/json\",\"size_bytes\":$(stat -c %s "$WORK/$name"),\"modality_hint\":\"document\"}" \
    > "$WORK/presign.json"
  local id url
  id=$(json "$WORK/presign.json" asset id)
  url=$(json "$WORK/presign.json" upload upload_url)
  case "$url" in /*) url="$BASE$url" ;; esac
  local headers=()
  while IFS= read -r header; do headers+=(-H "$header"); done < <(python3 -c 'import json,sys
for k, v in (json.load(open(sys.argv[1]))["upload"].get("required_headers") or {}).items(): print(f"{k}: {v}")' "$WORK/presign.json")
  curl -sS -o /dev/null -w "put $name %{http_code}\n" -X PUT "${headers[@]}" --data-binary @"$WORK/$name" "$url" >&2
  api -X POST "$BASE/api/assets/$id/finalize" -o /dev/null -w "finalize $name %{http_code}\n" >&2
  echo "$id"
}

SINCE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
PROJECT=$(save plan-a-probe.aura-video.json)
CONTROL=$(save plan-a-control.json)
echo "saved at $SINCE: project $PROJECT, control $CONTROL"
# AURA_INGEST_INTERVAL_SEC is 60 (services/ingest/app.py:40): 150 s covers two whole cycles.
sleep 150

vm() { sshpass -p "$PW" ssh -o StrictHostKeyChecking=no "$VM" "$@"; }
echo "== the ingest since $SINCE"
vm "echo $PW | sudo -S -p '' docker logs --since '$SINCE' aura-ingest 2>&1 | grep -E '^\[(extract|audit)\]' | tail -40"
echo "== the rows"
cat > "$WORK/rows.sql" <<SQL
SELECT id, file_name, object_key, status, NULLIF(document_id::text, '') IS NOT NULL AS named_document
FROM aura.assets WHERE id IN ('$PROJECT', '$CONTROL');
SQL
sshpass -p "$PW" scp -o StrictHostKeyChecking=no "$WORK/rows.sql" "$VM:/tmp/plan-a-rows.sql"
vm "echo $PW | sudo -S -p '' docker cp /tmp/plan-a-rows.sql aura-postgres:/tmp/plan-a-rows.sql && echo $PW | sudo -S -p '' docker exec aura-postgres sh -c 'psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -f /tmp/plan-a-rows.sql; rm -f /tmp/plan-a-rows.sql'; rm -f /tmp/plan-a-rows.sql"

for id in "$PROJECT" "$CONTROL"; do
  api -X DELETE "$BASE/api/assets/$id" -o /dev/null -w "delete $id %{http_code}\n"
done
```

Check it before it touches the VM: `MSYS_NO_PATHCONV=1 wsl bash -n $W/vm-project-probe.sh` must print nothing. The script has been through `bash -n`, and its JSON helpers ran against a sample presign answer. Its sign-in and upload calls follow the M1 probe that ran on this VM on 2026-09-27.

- [ ] **Step 2: Run it**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vm-project-probe.sh`

Expected:
1. `put` and `finalize` answer 2xx for both files.
2. `== the ingest since …` contains `[extract] plan-a-control.json`, and **no** line containing `aura-video.json`: neither an `[extract]` nor an `[audit] MISSING`.
3. `== the rows` shows two keys:
   - the project's `object_key` is `chat/<its id>.aura-video.json`;
   - the control's is `chat/<its id>.json`.
4. `delete` answers 2xx twice.

If `[extract] plan-a-control.json` is missing, no cycle reached the files within 150 s. The run proves nothing either way, so run it again. Do not read the project's absence as a pass.

- [ ] **Step 3: Complete the PRD record**

In `prd.md`, at the end of the "VideoFlow opacity and media" paragraph from Task 0 (after `…never arrived or did not play.`, before that paragraph's own `This does not establish:` list), add this sentence. Fill every `⟨…⟩` from Task 7 Step 10 and the dist SHA:

```markdown
Measured after the fix on ⟨date⟩ on the lab VM (image ⟨dist SHA⟩, `web/e2e/video-studio-export.spec.ts`):
a clip's fade-out frame at ⟨presentedAt⟩ s kept ⟨inFade⟩ of its twin's level where ⟨expected⟩ was
due, the frame before the fade kept ⟨beforeFade⟩, and a source answering 404 stopped the export
with the sentence naming its asset and no download.
```

After the M1 text from Task 0, which ends `saving again writes a new asset.`, append this. Fill every `⟨…⟩` from Step 2:

```markdown
Measured after the fix on ⟨date⟩ on the lab VM (image ⟨dist SHA⟩): a project saved through the
Studio's upload door as `plan-a-probe.aura-video.json` got the key `chat/<id>.aura-video.json`,
and in the 150 s that followed the ingest extracted the plain `plan-a-control.json` saved
beside it but never named the project, as an extraction or as `[audit] MISSING`; both were deleted
afterwards. This does not establish that a project indexed before the change leaves the index, or
that the asset loses its `document_id`: `DocumentProcessor` still names one, and the knowledge
catalog asks ArcadeDB whether a document is indexed (`internal/assets/document_processor.go:11-33`).
```

- [ ] **Step 4: Commit and push the record**

Write `$W/msg-t8.txt`:

```text
docs(prd): record the Studio export and project fixes measured on the VM

After aura-video-mcp Plan A reached the lab VM: the exported fade-out
frame kept the level the fade was due, a source answering 404 stopped
the export by name with nothing downloaded, and a project saved as
.aura-video.json kept that suffix in its key and was never extracted
by the ingest, while a plain .json saved beside it was.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t8.txt prd.md`, then `MSYS_NO_PATHCONV=1 wsl bash $S/push.sh plan-a-prd`, then the Git Bash `poll-ci.sh <sha>` from Task 7. Expected: `PUSH_RC=0` and every run `completed | success`.

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

`TestStudioProjectSuffixMatchesTheCockpitAndTheIngest` fails if any of the three spellings drifts. A fourth spelling (the sidecar's) goes into that test.

**Export errors**
- `ExportSourceError` from `web/src/videoStudio/videoflow_media.ts`.
  - `name === 'ExportSourceError'`.
  - `message === 'videoStudio: source <assetId> is <failure>'`.
  - Fields: `assetId: string` and `failure: 'unreachable' | 'undecodable'` (the type `SourceFailure`).
- `assetId` is the project's asset id. It covers a source's `assetId`, its `denoisedAssetId`, or an overlay's `props.assetId`. It falls back to the URL itself only when no project asset maps to it.
- `unreachable`: the bytes never arrived (any non-2xx, a CORS refusal, a dead network). The HTTP status is **not** available.
- `undecodable`: the bytes arrived and a layer could not play them, or a sound's audio would not decode.

**Export functions**
- `exportProject(project, urls, options?)`: **the signature is unchanged**. `options` is `{ onProgress?, signal? }`.
  - It resolves only with a complete MP4 `Blob`.
  - It rejects with `ExportSourceError` before `exportVideo` is called.
  - It rejects with the signal's reason on abort, and with anything VideoFlow's encoder throws.
- `exportProjectAudio(project, urls, signal?)`: unchanged. It rejects with `ExportSourceError` or `NoProjectAudioError`.
- `loadSources(renderer, project, urls, signal?)` is exported for those two exports to share. A render page does not call it: it calls `exportProject`.
- `toVideoJSON(project, urls)` is unchanged in signature. Its output now carries every fade in `animations` on the source clock (`withKeyframes`), and the Stage preview shares that output.

**Facts the sidecar must know**
- Fetch-once is not a URL map. It works because every layer and the mixer take their bytes from VideoFlow's per-page `loadedMedia`, keyed by the exact URL `urls.assetUrl(assetId)` returns.
  - The sidecar's page must hand `exportProject` one stable URL per asset.
  - Its proxy must serve those URLs (S1.4's CORS finding still applies).
  - A URL that changes between layers of one source, such as a fresh signature per call, would be fetched once per distinct URL.
- The failure is decided after `initLayers`. The sidecar can map `ExportSourceError.assetId` straight to the job's "failed, naming the source" state. No black or silent file is ever handed to its output check by this path.
- The export's codecs are unchanged: H.264 + Opus in MP4 (spec S1.1). Plan A changes nothing about encoding.
- Projects saved before Plan A carry no marker: their names and keys end in `.json`. A listing "by suffix" misses them. A listing that must include old projects has to read the content, as `loadProject` does with its `isProject` check (`projectStore.ts:436`, not exported today), not the name.
- The E2E helpers `readFrames` and `meanLevel` (`web/e2e/support/frames.ts`) are there for Plan C's reel checks (black frames, sampled frames).

## Spec coverage

| Spec requirement | Where |
|---|---|
| Fix 1: a failed source fails the export, naming it; the dialog shows the reason | Tasks 2, 3; E2E Task 7 test 2 |
| Fix 2: fade-out appears, confirmed on a rendered frame | Task 1 (runtime layers); Task 7 test 1 (exported frames) |
| Fix 3: each source fetched once | Task 2 (`[1, 1, 1]` fetch count against VideoFlow's real `MediaCache`) |
| M1: `<slug>.aura-video.json`; `projectFileName` writes it | Task 4; the key: Task 6 (spec ruling 1) |
| M1: the ingest skips the suffix; other `.json` still indexed | Task 5; measured on the VM in Task 8 |
| M1: projects saved before the change | Ruling 3 (they stay indexed until deleted); recorded in the PRD, Task 0 |
| Tests and E2E: failing test first, E2E on the lab VM | every task's Step 2; Task 7 RED then GREEN |
| Mutation only in CI | Tasks 1 and 2 add the new modules to `stryker.config.json`; read after the push |
| What this does not prove | Task 0 and Task 8 PRD text |
