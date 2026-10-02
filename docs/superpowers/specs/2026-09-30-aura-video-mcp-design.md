# `aura-video-mcp`: Aura's video editing tools (sub-project 2 of 3)

Date: 2026-09-30. Status: design approved in brainstorming, section by section. This text is
awaiting review.

## Why

The operator on 2026-09-27: *"allo studio dei video manca la parte di editing audio e i tool da
esporre a aura"*. Sub-project 1 (audio in the Studio,
`docs/superpowers/specs/2026-09-27-video-studio-audio-design.md`) is shipped and tested. This
document is sub-project 2, **the tools**. Sub-project 3, the Aura panel inside the Studio, reuses
them.

The external codebase analysis of 2026-09-29 (`Aura_analisi_codebase.md` §11, table F) names the
gap exactly. No path goes from a request in chat to an MP4 the user can download:
- the Studio exports only in the browser;
- `send_file` stops at 50 MiB and may end "successful" with no downloadable file.

It proposes the acceptance chain this design adopts:
1. accessible assets;
2. montage;
3. technical check of the file;
4. storage;
5. a card with an authenticated download;
6. a verified download.

## Decisions taken with the operator (2026-09-30)

| Question | Decision |
|---|---|
| What must pass end to end | **Both, chat first.** Primary: from chat or Telegram, "make me a video with these clips and this music, lower the music under the voice". Aura builds a Studio project, renders it on the server, and delivers a downloadable card; the project also opens in the Studio. Secondary: the same tools edit an existing project. |
| How a render waits | **Background job**, the `video_generate` pattern: the tool returns straight away with a progress card, and Aura wakes the conversation when the MP4 is ready. Cancellable. *Superseded 2026-10-02: no wake; see §"Amended 2026-10-02".* |
| Sources | **The identity's library assets only.** The sidecar never downloads an arbitrary URL. |
| Tool scope | **Full Studio parity**, ducking (speech detection) and noise reduction included. |
| Telegram | **As today**: native video up to 50 MB, above that "⚠️ Il video è disponibile nel cockpit." (`internal/channels/telegram/artifact.go:33,143-149`). |
| Architecture | **A central sidecar**, a Node container, MCP-over-HTTP with Aura's OAuth. Not an MCP in each identity's box. |
| Studio export bugs the spikes found | **Fixed in this sub-project**: the sidecar renders with the same export page. |
| Who owns a render job | **Aura**, for its own conversations (native tools, job row, watcher, wake). The sidecar is the engine. *Superseded 2026-10-02: the sidecar owns every job; see §"Amended 2026-10-02".* |
| E2E | **Aura's own Instagram Reels.** Claude mounts the MCP in Claude Code as `claude@aura.local` and makes them; Aura makes one from chat and from Telegram too. |
| Render in the MCP | **Yes, for external clients too.** Aura hides the MCP's render tools from its model and keeps its native wrapper. *Superseded 2026-10-02: nothing hidden, no native wrapper.* |
| Project versions | **Every version kept**, as the Studio does today. Saved projects are left out of document indexing. |
| Music in the reels | **Synthetic**, generated procedurally, like the Plan A spikes. |

## Amended 2026-10-02: the sidecar mounts as a normal MCP

The operator on 2026-10-02, on what Aura's Go side has to build: *"si monta come un normale MCP"*.
Offered three shapes, they chose the one that keeps only the internal API. This section supersedes
every part of this document that gives Aura's side more than that, and each of those places points
here.

**Aura's side is now three things:**
- the recipe `video` in the catalog, next to `calendar` and `whatsapp`, which makes it a first-party
  sidecar whose grant Aura mints and renews (`cmd/aura/mcp_first_party_grants.go`);
- the Compose service `aura-video-mcp`;
- the internal API's four operations (§Security), unchanged.

**Gone:**
- the `aura.render_jobs` table and `internal/renderjobs` (store, client, watcher);
- the native tools `video_render` and `video_render_cancel`;
- the completion route that woke the conversation;
- the bridge's hidden tool set.

`video_render_start`, `video_render_status` and `video_render_cancel` are ordinary MCP tools. Aura's
model sees them exactly as an external client does, so every client has one delivery path.

**What this changes** (read in the code on 2026-10-02):
1. **No wake and no progress card.** A render answers with a job id and the turn goes on. Aura
   learns that a film is ready only by calling `video_render_status`. One MCP call is bounded at
   60 s (`internal/agent/mcptools/timeout.go:13`); a 60 s film rendered in about 113 s (S2).
2. **The caller's token can expire during a render.**
   - Aura's access tokens last 15 minutes, and without the native wrapper nothing mints §Jobs 1's
     90-minute job token.
   - The upload URL does not bridge the gap: a presign lasts 600 s (`AURA_ASSET_PRESIGN_TTL_SEC`).
   - A render that finishes after its token expired must not be lost. The checked film is kept for a
     bounded time, which the plan fixes, and the identity's next tool call saves it. A film nobody
     claims in time is discarded, and the failure says why.
3. **A film reaches the chat only under the bridge's cap.** The bridge writes a file a tool result
   carries into the turn's workspace, up to 25 MiB per file and 50 MiB per call
   (`internal/mcp/file.go:20-22`).
   - A finished status therefore carries the film as a `resource_link` when it fits, and the model
     hands it to `send_file`, on the web and on Telegram.
   - A larger film is reached through the library and the Studio, and the status says so.
   - Telegram's native video therefore stops at 25 MiB on this path, not 50 MB.

## Measured before designing (spikes, `spikes/video-mcp-render/FINDINGS.md`, bd3ac9243 + 234302bbf)

Measured on the lab VM (i7-11850H, Hyper-V, 8 vCPU) in containers limited to `--cpus=4`, with
synthetic fixtures only.

| Spike | Finding | What it does NOT show |
|---|---|---|
| S1.1 codecs | Playwright's Chromium (Chrome for Testing), its headless shell and Google Chrome have identical WebCodecs support on x64. All decode H.264, VP9, AV1, AAC, Opus and MP3, and all encode H.264. **None decodes HEVC. None encodes AAC**, so a browser export is always H.264 + Opus in MP4. | arm64: Playwright refuses Chrome there, and its arm64 Chromium is another build with unmeasured codecs. |
| S1.2 compile in Node | `toVideoJSON` runs in Node once bundled with esbuild, and its output equals the browser's (layer ids aside). Unbundled, two things block it: extensionless imports (`videoflow.ts:15-26`) and the npm renderer-browser's `googlefonts.json` import, which the cockpit's patch removes. | Every command path: only the parity project was compiled. |
| S1.3 parity | **Our export page, not renderer-server as shipped.** The film is the same to 0.00 dB, and the crossfade and fade-out frames are pixel-identical. But renderer-server fetches fonts from Google with no seam, so offline it burns a fallback font into the title. Its default element capture is about 3× slower, and at a 4 GiB limit it was killed for running out of memory. | Transitions other than crossfade, image overlays, speed ≠ 1, a cleaned clip, animated text. |
| S1.4 media | Our page fetches its media from the page itself. **With no CORS on the media origin it "succeeds" with a black, silent 26,604 B MP4** (`primeDecodedBuffers` swallows the error, `videoflow.ts:336-343`). It also fetches every source twice. renderer-server proxies through Node (`page.route` → `route.fetch()`). | Real Garage presigned URLs, expiry, 403 or 404. |
| S2 cost | Our page, Chrome, 60 s 1080p: **median 112.8 s** (112.2–113.8; 99.3 s on a quiet host), ~1 GB anon, **1,549 MiB peak**, 1.4–2.8 cores used. 720p: 108.2 s, about 260 MiB less, 24.2 MB vs 49.9 MB of file. renderer-server with ffmpeg (the only AAC path it has): 744.6 s, 3,435 MiB. | The mini-PC's CPU; real camera footage; films longer than 60 s; two renders at once. |
| S3 image | Headless shell **897 MB unpacked / 335 MB to pull** (the smallest); full Chromium 1,031 / 405; Chrome 1,369 / 509; the official Playwright image 2,571 / 949. The appliance's "7 GB" is RAM at rest, not disk. | ffmpeg, the MCP SDK and the sidecar's own code (not in the candidate). |
| S4 analyses | The Studio's `audioSpeech.ts` and `audioClean.ts`, bundled unchanged, run offline in the render page on all three browsers. They match Plan A: speech edges identical to the millisecond, noise floor −38.9 dB for 0.6 dB of speech. They need Web Audio (`OfflineAudioContext`, `AudioWorklet`), not `document`. | Music under speech, several voices, long files; whether the 500 ms worklet warm-up is enough on a slower CPU. |

The spikes also surfaced two bugs in the Studio export the cockpit uses today:
- a failed source becomes a black, silent MP4 that reports success;
- a clip's fade-out does not appear in the export. One frame was measured at RGB (233, 100, 90) where opacity 0.4 over black allows at most 102.

## Output format: Instagram Reels, read at the source

Instagram Graph API, IG User Media, "Reel specifications" (page version v25.0, read 2026-09-30):

| Property | Requirement |
|---|---|
| Container | MOV or MP4, **no edit lists, moov atom at the front** |
| Audio | **AAC**, 48 kHz max, 1 or 2 channels, 128 kbps |
| Video | HEVC or H.264, progressive, **closed GOP**, 4:2:0 |
| Frame rate | 23–60 fps |
| Picture | at most 1920 horizontal pixels; 9:16 recommended |
| Bitrate | VBR, 25 Mbps max |
| Duration | 3 s to 15 min |
| Size | 300 MB max |

So **every MP4 the sidecar delivers goes through ffmpeg**:
- the video stream is copied untouched;
- the Opus audio becomes AAC-LC 48 kHz stereo 128 kbps;
- `-movflags +faststart` puts the moov atom first;
- no edit list is written.

The output check verifies each of these properties on the delivered file (§Jobs, step 2).

## Architecture

### The sidecar, `services/video-mcp/`

A Node 24 project in the repository, published as `ghcr.io/chetto1983/aura-video-mcp` (amd64). The
image holds Node, Playwright's **Chrome Headless Shell** (pinned by the Playwright version) and
Debian's ffmpeg/ffprobe. On arm64 it answers `unsupported platform` until an S1.1 run there
measures otherwise.

1. **MCP server** over streamable HTTP (`@modelcontextprotocol/sdk`).
   - It verifies Aura's bearer the way `cmd/arcadedb-mcp/auth.go` does: issuer, audience (the
     sidecar's own resource name), expiry, and the signature against Aura's JWKS, re-read once on an
     unknown key.
   - The identity is issuer + subject, mapped to the Aura identity.
2. **Studio core**, bundled with esbuild from `web/src/videoStudio/` at image build: `project.ts`,
   `commands*.ts`, `projectStore.ts`'s parser, and `toVideoJSON` with its compile modules.
   - It carries the cockpit's renderer-browser patch (`web/patches/`).
   - Validating, editing and compiling happen in Node; no browser starts for them.
   - A drift test fails the build if the sidecar bundle and the cockpit do not take the core from the
     same files.
3. **Job engine**: one job at a time, in a FIFO queue.
   - A job launches its own headless shell and closes it at the end, so the only thing resident at
     rest is Node.
   - Launch flags come from renderer-server's (`ServerRenderer.js:100-110`), including
     `--js-flags=--max-old-space-size=4096`.
   - The page is **our export page**: `exportProject`, `withLocalFonts`, the fonts served locally,
     and the two analyses.
   - A stall watchdog fails a job that reports no progress for 120 s (renderer-server's
     `STALL_MS = 120_000`, `ServerRenderer.js:940`).
4. **Media proxy.**
   - Every request from the page is routed through Node (`page.route`).
   - It is allowed only to the signed URLs the internal API returned for this job, and each source is
     fetched once and cached for the job.
   - Any non-2xx answer or network error **fails the job with the source's name**.
5. **Output check** (ffprobe plus sampled decoding), before anything is saved:
   - the duration is within one frame of the project's;
   - one H.264 video stream and one AAC audio stream, with the §Output-format properties;
   - five sampled frames are not black;
   - the audio is not silent wherever the project has an audible item.
6. **Uploads** go through Aura's internal API (§Security): the MP4, project versions and cleaned
   copies. `send_file`'s 50 MiB path is not used.

### Aura's side (Go)

*Amended 2026-10-02 (§"Amended 2026-10-02"): only the recipe, the Compose service and the internal
API remain. The hidden tool set, render jobs, native tools and completion route below are gone.*

- **Recipe** `video` in `internal/mcp/manager/catalog.go`, next to `calendar` and `whatsapp`:
  streamable HTTP, trusted recipe, tools deferred.
  - The recipe declares a **hidden tool set**, `video_render_start`, `video_render_status` and
    `video_render_cancel`, which the bridge does not register. The bridge has no such field today
    (`bridge_policy.go`); it is added.
  - The URL helper follows `PIMSidecarBaseURL()`: Compose DNS in a container, the loopback publish
    otherwise.
- **Compose service** `aura-video-mcp`, built on `aura-pim-mcp`'s block:
  - `pull_policy`;
  - `depends_on: aura`;
  - a health check;
  - the loopback port publish;
  - the OAuth metadata address.

  It sits on the internal network with no internet egress. Chromium's memory limit comes from S2
  (1,549 MiB peak at 60 s): the plan measures a 10-minute film before fixing the container limit.
- **Internal API** `/internal/video/…` (§Security), and the Caddyfile answers 404 on that prefix.
- **Render jobs**: the new package `internal/renderjobs`:
  - a store (table `aura.render_jobs`; the migration takes the next free number when it lands, per
    CLAUDE.md);
  - a sidecar client;
  - a watcher shaped like `internal/mediagen/watcher.go`, resuming from the table on boot.
- **Native tools** `video_render` and `video_render_cancel` (`internal/agent/tools/`, deferred).
- **Completion**: a third route in `cmd/aura/background_completion.go`, next to shell and media
  (`NotifyMedia`, :89).
- **Project indexing (M1 of the audio spec).** A Studio project file is named `<slug>.aura-video.json`,
  and the ingest skips that suffix, so it never becomes a RAG document. Any other `.json` the operator
  uploads is still indexed. The suffix keeps the `.json` extension the upload allowlist accepts
  (`internal/assets/limits.go`). The cockpit's `projectFileName` writes the same suffix; this is the
  one change to how the Studio saves. Projects saved before the change stay indexed until they are
  saved again.
  *Superseded 2026-10-01 (Plan A; recorded in prd.md §12 "Saved Studio projects and the document
  index"):* the ingest cannot skip a project by its name, because its matcher and its audit see only
  the object key, so the key keeps the suffix whole; and a project saved before the change is not
  fixed by saving it again, because every save is a new asset: the daemon is to move it once, at boot.

## Tools

The rule: an **edit is pure and fast** (Node, well under a second, far inside Aura's 60 s MCP call
timeout, `internal/agent/mcptools/timeout.go:13`). **Everything heavy runs in a job.**

| Tool (MCP unless noted) | Input | Output |
|---|---|---|
| `video_project_create` | name, format (`16:9`, `9:16`, `1:1` or width × height), optional clips and sounds by asset id | version 1 saved; the timeline summary; the project asset id |
| `video_project_open` | project asset id | the timeline summary: item ids, start and end, source, volume, fades, ducking, transitions, overlays, and which analyses are still missing |
| `video_project_edit` | project asset id; a list of operations | **all or nothing.** A new version, one `before => after` line per operation (`volume: 1 => 0.3`), the new timeline summary and asset id |
| `video_project_list` | — | the identity's projects, latest version of each |
| `video_render_start` / `_status` / `_cancel` | project asset id, quality `1080p` or `720p` / job id / job id | job id; state, queue position, progress; at the end the asset id and a signed download link valid for 2 hours. **Hidden from Aura's model.** |
| `video_render` (**native**, Aura) | project asset id, quality | job id and a progress card; the turn ends |
| `video_render_cancel` (**native**, Aura) | job id | cancelled or already finished |

**Operations** are exactly the Studio's commands, named after them:
- clips: `add_clip`, `trim_clip`, `split_at`, `remove_range`, `move_clip`, `set_muted`,
  `set_clip_presentation`, `set_junction_transition`;
- project: `set_frame_size`;
- overlays and items: `add_overlay`, `set_property`, `remove_item`;
- audio: `add_audio`, `extract_audio`, `move_audio`, `trim_audio`, `split_audio`,
  `set_audio_properties` (volume, fades, speed, ducking, `denoise`), `set_envelope`;
- **`add_speech`** {text, voice, language, where}: Aura's TTS through the internal API. The voice
  becomes an audio asset and then an `add_audio`.

Adding a source probes it with ffprobe for duration, size and whether it has audio. HEVC is refused:
"HEVC is not supported by the renderer: convert the clip to H.264 first".

**Refusals.** Every operation is checked before anything changes. A refused list leaves the project
untouched and returns a result (not a tool failure) naming the operation, the reason and what would
be accepted, e.g. `trim_clip c3: start 12 s is past the clip's 8 s`.

**Analyses.** Turning ducking on, or `denoise: true`, only records the choice. The job computes what
is missing before rendering:
- the speech windows of the sources ducking has not heard;
- the cleaned copies that do not exist yet.

It saves them as a new version, the way the Studio records an automatic analysis
(`History.annotate`, plan C ruling). A later render reuses what is saved.

## Jobs and delivery

*Amended 2026-10-02 (§"Amended 2026-10-02"): every client, Aura included, submits through
`video_render_start` under its own token. Supervision (3), the wake and the card (4) and the native
cancel (5) are gone; delivery is the status call's.*

1. **Start.**
   - From Aura, `video_render` writes the row (identity, conversation, project, quality, status
     `queued`).
   - It issues a short-lived token (90 min) for that identity and the sidecar audience, and submits
     the job to the sidecar.
   - From an external MCP client, `video_render_start` submits it directly under the caller's token.
2. **Execution.** The queue position is reported. Then, in order:
   1. the missing analyses, saved as a new version;
   2. the compile in Node;
   3. the render in the page;
   4. ffmpeg (AAC, faststart, no edit list);
   5. the output check;
   6. the upload.

   Progress runs from 0 to 1 across these steps.
3. **Supervision** (Aura's jobs only).
   - The watcher polls the sidecar's status and resumes after an Aura restart.
   - A job the sidecar no longer knows (a sidecar restart) fails with "the renderer restarted". There
     is no silent retry.
   - Jobs expire after **60 minutes**. S2 measured about 1.9× real time on the lab VM, and the mini-PC
     is slower.
4. **Delivery.**
   - **Web:** the card becomes the video, with an authenticated download and "open in the Studio".
   - **Telegram:** as today.
   - **The conversation is woken** with "render finished: 58 s, 1080p, 47 MB, asset …", or the failure
     and its reason.
5. **Cancellation.** The native tool, the card's button or `video_render_cancel` stops the queued or
   running job and destroys its browser.

**Limits of this version:**

| Limit | Value | Reason |
|---|---|---|
| Film length | **10 minutes** | Memory was measured only at 60 s; the plan measures 10 minutes before fixing it. |
| Resolution | 1080p or 720p, at most 1920 px wide | Instagram's cap; 720p halves the file. |
| Frame rate | the project's; 23–60 accepted | Instagram's range. |
| Video bitrate | chosen per resolution **after measuring it on real clips** in the plan | the Studio's default is about 12 Mbps (49.9 MB per synthetic minute); Instagram's cap is 25 Mbps |
| Concurrency | 1 job | S2 used 1.4–2.8 of 4 cores; two at once was not measured. |

## Security and the internal API

- **Tokens.** Aura's OAuth server issues them for the sidecar's resource, as it does for
  `aura-pim-mcp` (`compose.yaml` `CALENDAR_MCP_OAuth__Resource`).
  - Aura's bridge obtains one per identity.
  - Claude Code obtains one by signing in as `claude@aura.local` through the tunnel already used for
    the memory MCP.
- **Internal API**, reachable only from the docker network: Caddy answers 404 on `/internal/video/`.
  Every call carries the bearer, and Aura checks its audience and identity. It has four operations:
  - `resolve`: asset ids → signed GET links on the internal object-store endpoint, valid 2 hours.
    It answers only for assets the identity owns and that are complete; anything else is "not
    found", which does not reveal whether it exists.
  - `presign` + `finalize`: a new asset through `assets.Service` (`handleAssetPresign`'s path, a new
    source kind for the sidecar).
  - `tts`: Aura's TTS (`internal/multimodal/tts.go`), saved as an audio asset.
  - `projects`: the identity's saved projects.
- **Inside the sidecar:**
  - it runs as a non-root user on a read-only filesystem;
  - scratch space is a tmpfs, emptied after every job;
  - the page reaches only its own origin and the proxy, and the proxy only the job's allowlist;
  - there is no internet egress.
- **Gateway.** No approval gate: edits add versions, and render and cancel create or stop jobs;
  nothing is destroyed. Under the current policy only destructive actions reach the gate.

## Fixes to the Studio export (the cockpit benefits too)

Each fix has a failing test first:
1. **A source that cannot be fetched fails the export, naming the source.** `primeDecodedBuffers`
   stops swallowing a failed fetch, and a layer VideoFlow could not load is treated as an error, not
   silence. The cockpit's export dialog shows the reason.
   *Superseded 2026-10-01 (Plan A; recorded in prd.md §12 "VideoFlow opacity and media"):* the page
   never learns the HTTP status of a failed fetch, so the dialog names the source by its file name
   and says whether its bytes never arrived or did not play. The pre-decode is to decode the
   bytes VideoFlow's cache already holds, and to fetch nothing.
2. **A clip's fade-out appears in the export**: `clipOpacity`, `videoflow.ts:58`, confirmed on a
   rendered frame against the expected opacity.
3. **Each source is fetched once**: `primeDecodedBuffers` reuses the bytes VideoFlow's cache already
   holds.

## Tests and E2E

**Gate 2.**
- **Sidecar (vitest):**
  - every operation and its refusal, atomicity, `before => after` lines;
  - the queue, cancellation and the stall watchdog;
  - the proxy refusing anything off its allowlist;
  - the output check on fixtures that are black, silent, too short, missing a stream, or AAC-less;
  - token verification against fake published keys;
  - the drift test.
- **Sidecar integration in CI:** a real render in the image (headless shell + ffmpeg) on synthetic
  fixtures, asserting H.264 High, AAC 48 kHz stereo, faststart, no edit list, 1080×1920 at 30 fps and
  the duration. Under `$CI` it fails if anything is missing; it never skips.
- **Aura (Go)** *(amended 2026-10-02: the internal API and the recipe only; the store, watcher,
  native tools, completion route and hidden tool set are gone)*:
  - the internal API: audience, identity, "not found" semantics;
  - the `render_jobs` store (db_integration);
  - the watcher against a fake sidecar;
  - the native tools;
  - the completion route;
  - the bridge's hidden tool set;
  - race + goleak, coverage ≥ 85 %.
- **Mutation in CI only.** The critical files join the gate: the proxy's allowlist, the output check,
  and the internal API's authentication.

**E2E on the lab VM (Definition of Done).**
1. **Aura's Instagram Reels through the MCP.** Claude mounts `aura-video-mcp` in Claude Code as
   `claude@aura.local` and makes **2–3 reels about Aura** (e.g. its memory, the Video Studio,
   Telegram).
   - **Format:** 9:16 at 1080×1920, 20–60 s.
   - **Sources:**
     - screen recordings of the cockpit shot with Playwright in Claude's own identity;
     - screenshots;
     - a TTS voiceover;
     - a synthetic music bed with ducking;
     - text overlays and transitions.
   - **Checks:**
     - ffprobe against every row of §Output-format;
     - ducking depth measured with the Studio's method (10–14 dB, ramp ≥ 0.3 s);
     - no black frames;
     - Claude looks at sampled frames;
     - the project opens and plays in the Studio.

   The reels stay in Claude's library for the operator to judge.
2. **Aura from chat, with the operator's login:**
   - "fammi un reel di Aura con queste clip e questa musica, abbassa la musica sotto la voce";
   - *amended 2026-10-02:* Aura starts the render and reports the job; a later status delivers the
     film in the chat when it is under the bridge's cap, and in the library otherwise;
   - the downloaded MP4 gets the same checks.
3. **Aura on Telegram (CDP harness):** the same request; a native video if ≤ 50 MB, otherwise the
   cockpit notice.
4. **Editing an existing project from chat:**
   - "accorcia l'intro di 2 s e abbassa la musica";
   - a new version, a new render, and the difference measured.
5. **A job that must fail:** a source deleted after the project was made. The job fails naming it,
   and no black video is produced.

Assets created only for tests are deleted from the operator's library afterwards. Nothing paid is
called (no `video_generate`) unless the operator asks.

## Environment variables

- **Compose:** `AURA_VIDEO_MCP_IMAGE`, `AURA_VIDEO_MCP_PULL_POLICY`, `AURA_VIDEO_MCP_PORT`, following
  `AURA_PIM_MCP_*`.
- **Limits** (film length, job age, concurrency, bitrate): documented constants in code, not env
  vars, until a deployment needs to change them. They are recorded in the PRD's caps table with the
  measurement behind each.

## What this does not prove

- Anything on arm64, or render times on the mini-PC's CPU. S2's seconds are the lab VM's.
- Camera footage and 4K: the spikes used test patterns.
- Two renders at once.
- Whether the Opus → AAC transcode changes loudness. The E2E measures it; the spikes did not.
- Sub-project 3 (the panel), which reuses these tools and is designed separately.

## For review

- The internal API uses the sidecar's own bearer, whose audience is the sidecar. It is the narrowest
  credential that carries the identity. The alternative, a storage key in the sidecar, would bypass
  asset ownership, and was rejected.
- Hiding the MCP's render tools from Aura's model, rather than teaching the bridge to track jobs,
  keeps one delivery path per client. *Superseded 2026-10-02: nothing is hidden, and Aura uses the
  MCP's render tools like any client (§"Amended 2026-10-02").*
