# `aura-video-mcp`: Aura's video editing tools (sub-project 2 of 3)

Date: 2026-09-30. Revised: 2026-10-06 after the applicability review. The original design
and the operator's 2026-10-02 amendments are recorded below. The current text consolidates
those decisions and the implementation requirements from
[Plan B's applicability corrections](../plans/2026-10-02-aura-video-mcp-plan-b.md#applicability-corrections-2026-10-06).
The affected code and shared schemas still need the listed regressions and a fresh run;
this revision is not an implementation, a PRD amendment or approval of Plan B's open limits.

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
| How a render waits | **Background job**, cancellable. As amended 2026-10-02, start returns a job id and a later status call delivers the film; there is no wake or progress card. |
| Sources | **The identity's library assets only.** The sidecar never downloads an arbitrary URL. |
| Tool scope | **Full Studio parity**, ducking (speech detection) and noise reduction included. |
| Telegram | On this MCP path, native video up to the bridge's **25 MiB** per-file cap; larger films are reached through the library and Studio (amended 2026-10-02). |
| Architecture | **A central sidecar**, a Node container, MCP-over-HTTP with Aura's OAuth. Not an MCP in each identity's box. |
| Studio export bugs the spikes found | **Fixed in this sub-project**: the sidecar renders with the same export page. |
| Who owns a render job | **The sidecar owns every job**, Aura's and external clients' alike (amended 2026-10-02). |
| E2E | **Aura's own Instagram Reels.** Claude mounts the MCP in Claude Code as `claude@aura.local` and makes them; Aura makes one from chat and from Telegram too. |
| Render in the MCP | **Yes, for all clients.** Aura sees the ordinary render tools; there is no hidden set or native wrapper (amended 2026-10-02). |
| Project versions | **Every saved version kept**, as the Studio does today. Saved projects are left out of document indexing. |
| Music in the reels | **Synthetic**, generated procedurally, like the Plan A spikes. |
| Compose network | **Default network with egress and a direct loopback publish**, chosen on 2026-10-02 and recorded in `e9d379e28`; the page and proxy still enforce an allowlist. |

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
     bounded time, provisionally 15 minutes after check completion (Plan B Q1), and the identity's
     next tool call supplies a bearer. Every authenticated request obtains the freshest bearer at
     dispatch: finalize cannot reuse the token taken before a potentially ten-minute PUT.
   - All credential waits share one deadline. A missing bearer pauses upload without discarding
     checked scratch; a new token resumes the same presigned asset, subject to verified finalize
     replay semantics. No claim in time discards unsaved scratch, and status identifies any assets
     already accepted before the deadline (see §Jobs and delivery).
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
Debian's ffmpeg/ffprobe. The operator decided on 2026-10-02: *"arm64 non supportato"*
(`391a3c014`). Publish amd64 only, with no arm64 branch, `unsupported platform` answer or
architecture check. An emulated amd64 image reports `x64`, so such a check would not establish
host support (Plan B Q4).

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
   - Launch flags come from renderer-server's (`ServerRenderer.js:98-129`), including
     `--js-flags=--max-old-space-size=4096`.
   - The page is **our export page**: `exportProject`, `withLocalFonts`, the fonts served locally,
     and the two analyses.
   - A stall watchdog fails a job that reports no progress for 120 s (renderer-server's
     `STALL_MS = 120_000`, `ServerRenderer.js:940`).
4. **Media proxy.**
   - Every request from the page is routed through Node (`page.route`).
   - It is allowed only to the signed URLs the internal API returned for this job. Plan B Q6
     provisionally streams each source once per analysis/render pass, without a tmpfs cache:
     cached bytes would count against the same measured memory limit as the browser.
   - Any non-2xx answer or network error **fails the job with the source's name**.
5. **Output check** (ffprobe plus sampled decoding), before anything is saved:
   - the duration is within one frame of the project's;
   - one H.264 video stream and one AAC audio stream, with the §Output-format properties;
   - five sampled frames are not black;
   - the audio is not silent wherever the project has an audible item.
6. **Uploads** go through Aura's internal API (§Security): the MP4, project versions and cleaned
   copies. `send_file`'s 50 MiB path is not used.

### Aura's side (Go)

As amended 2026-10-02, only the recipe, the Compose service and the internal API remain.
Plan C implements this side; there is no render-jobs table, watcher, native tool or completion route.

- **Recipe** `video` in `internal/mcp/manager/catalog.go`, next to `calendar` and `whatsapp`:
  streamable HTTP, trusted recipe, tools deferred.
  - The bridge exposes `video_render_start`, `video_render_status` and `video_render_cancel`
    normally, with the same tools and delivery path as an external MCP client.
  - The URL helper follows `PIMSidecarBaseURL()`: Compose DNS in a container, the loopback publish
    otherwise.
- **Compose service** `aura-video-mcp`, built on `aura-pim-mcp`'s block:
  - `pull_policy`;
  - `depends_on: aura`;
  - a health check;
  - the loopback port publish;
  - the OAuth metadata address.

  The service sits on the default Compose network with a direct loopback publish and internet
  egress, as `aura-pim-mcp` does. The operator chose this in `e9d379e28`: *"e chi se ne frega"*.
  No internal `aura-video` network or Caddy relay is added. On the measured Docker 29.8.1 setup,
  an internal-only network blocked both egress and host access through the published port;
  that observation is not a claim about every engine (Plan B Q7).
  The proposed 6 GiB memory limit comes from Plan B's dated ten-minute measurement and remains
  Q3's recommendation; the page and proxy allowlists apply regardless of container egress.
- **Internal API** `/internal/video/…` (§Security), and the Caddyfile answers 404 on that prefix.
- **Project indexing (M1 of the audio spec).** A Studio project file is named `<slug>.aura-video.json`,
  and its object key preserves that suffix so ingest can exclude it from RAG. Any other `.json`
  remains subject to normal indexing. Plan A's shipped ruling (2026-10-01, prd.md §12
  "Saved Studio projects and the document index") owns the one-time boot handling of old
  projects; saving another version does not change an earlier asset. Plan B reuses it.

## Tools

The command application is pure and fast in Node. Asset probing, TTS and saving involve I/O;
an edit adding many sources can exceed Aura's 60 s MCP call timeout
(`internal/agent/mcptools/timeout.go:13`). Plan C measures those cases and adds sources in
bounded batches. Rendering and analyses run in a job.

| Tool (all MCP) | Input | Output |
|---|---|---|
| `video_project_create` | name, format (`16:9`, `9:16`, `1:1` or width × height), optional clips and sounds by asset id | version 1 saved; the timeline summary; the project asset id |
| `video_project_open` | project asset id | the timeline summary: item ids, start and end, source, volume, fades, ducking, transitions, overlays, and which analyses are still missing |
| `video_project_edit` | project asset id; a list of operations | **Project version all or nothing.** A new version, one `before => after` line per operation (`volume: 1 => 0.3`), the new timeline summary and asset id; any already synthesized speech asset is reported on refusal |
| `video_project_list` | — | the identity's projects, latest version of each |
| `video_render_start` / `_status` / `_cancel` | project asset id, quality `1080p` or `720p` / job id / job id | job id; state, queue position, progress; success has the film asset id and a signed download link valid for 2 hours. Failed/cancelled status also identifies any assets already accepted during upload |

**Operations** are exactly the Studio's commands, named after them:
- clips: `add_clip`, `trim_clip`, `split_at`, `remove_range`, `move_clip`, `set_muted`,
  `set_clip_presentation`, `set_junction_transition`;
- project: `set_frame_size`;
- overlays and items: `add_overlay`, `set_property`, `remove_item`;
- audio: `add_audio`, `extract_audio`, `move_audio`, `trim_audio`, `split_audio`,
  `set_audio_properties` (volume, fades, speed, ducking, `denoise`), `set_envelope`;
- **`add_speech`** {text, voice, language, time} (*corrected 2026-10-02: `time`, the field
  `add_audio` already takes, `commands_audio.ts:121`*): Aura's TTS through the internal API. The voice
  becomes an audio asset and then an `add_audio`.

Adding a source probes it with ffprobe for duration, size and whether it has audio. HEVC is refused:
"HEVC is not supported by the renderer: convert the clip to H.264 first".

**Refusals.** A refused operation list leaves the project version untouched and returns a result
(not a tool failure) naming the operation, the reason and what would be accepted, e.g.
`trim_clip c3: start 12 s is past the clip's 8 s`. `add_speech` can already have synthesized an
audio asset before a later operation refuses: the response names that retained asset. Project
atomicity does not imply rollback of a TTS asset.

**Analyses.** Turning ducking on, or `denoise: true`, only records the choice. The job computes what
is missing before rendering:
- the speech windows of the sources ducking has not heard;
- the cleaned copies that do not exist yet.

The job computes them in scratch and renders with them. Only after the film passes its output
check does it save cleaned copies and a new analysis version, the way the Studio records an
automatic analysis (`recordAnalysis`, Plan A ruling). A later render reuses what was saved.

## Jobs and delivery

Every client, Aura included, uses the same ordinary MCP tools under its own bearer. The
sidecar owns the queue and jobs; delivery is the status call's. The following requirements
include the four regressions from Plan B's 2026-10-05 applicability review.

1. **Start.**
   - `video_render_start` reads the project and resolves its actual playback and missing-analysis
     dependencies before queuing. Signed source links last two hours; compute needs no bearer.
   - Dependencies come from timeline clips/audio, image overlays and analyses of sources that are
     actually used. Originals needed for analysis are included even when a cleaned stream plays;
     unused sources and unselected cleaned copies do not block a render. A missing used dependency
     refuses start, naming it. A source deleted after start can fail through the object store.
   - Start returns the job id immediately, with instructions to tell the user it started and ask
     for status on a later turn. No job token, native wrapper, progress card or conversation wake.
2. **Execution.** The queue position is reported. Then, in order:
   1. the missing analyses, held in scratch and recorded in the in-memory project;
   2. the compile in Node;
   3. the render in the page;
   4. ffmpeg (AAC, faststart, no edit list);
   5. the output check;
   6. the upload: cleaned copies, then the analysis version with real asset ids, then the checked
      film and its public download link.

   Progress runs from 0 to 1 across these steps.
3. **Bounds and credentials.**
   - The sidecar has a 120 s stall watchdog and a 60-minute age limit, queue time included.
     Credential waiting pauses those clocks; it has its own single deadline, provisionally
     15 minutes after check completion (Plan B Q1). Repeated waits do not extend that deadline.
   - Every authenticated request obtains the current caller bearer just before dispatch. In
     particular, finalize after a potentially ten-minute signed PUT and public resolve after film
     acceptance must not reuse a bearer captured before upload. The PUT itself uses only the signed
     URL and required headers, with no Aura Authorization header.
   - Keep checked scratch and the current upload's asset id/PUT outcome during credential waits.
     A refresh resumes that upload; it does not presign and upload a duplicate. Retrying finalize
     requires Plan C to verify replay semantics, including a response lost after acceptance.
   - The deadline also bounds upload/link recovery and its requests; respect both the remaining
     time and signed PUT expiry. A rejected credential waits for a different verified caller
     bearer, rather than retrying that same token in a loop.
   - Finished status is retained for two hours. Another identity's job, an absent/expired job and
     every job after a sidecar restart answer `unknown`; there is no silent render retry.
4. **Delivery.**
   - `video_render_status` reports the job. Success requires an accepted checked film and a usable
     signed public link valid for two hours.
   - Up to 25 MiB, success also carries `aura-video://film/<assetId>` as a `resource_link`. The
     bridge reads it under the caller's bearer into the turn's workspace for `send_file`, on web
     and Telegram. Larger films are reached through the library/Studio, named in the response.
   - If obtaining the public link fails after acceptance, recover the link for that same film
     within the deadline. On final failure, identify the saved film; never render or upload it
     again merely to obtain its link. No conversation is woken automatically.
5. **Cancellation.** `video_render_cancel` stops a queued or running job. The runner checks its
   signal before credential acquisition and every write, and propagates it through HTTP, signed
   PUT, browser and transcode operations alongside their timeouts. Cancel after the first accepted
   copy must not start the project or film upload. A request already in flight may have committed:
   report the known or uncertain outcome instead of implying rollback. SIGTERM uses the same path;
   active-upload shutdown must be measured against the Compose stop grace.
6. **Save guarantees and recovery.** No output of a render enters the library before its film
   passes the output check. The subsequent saves are separate operations, with no rollback API.
   Record each accepted asset and expose that record in failed/cancelled status, including copies,
   versions and films; keep uncertain outcomes explicit. If the deadline expires before any save,
   nothing was accepted and the scratch is discarded. If it expires after a save, identify what
   remains in the library. A null success result does not imply that nothing was saved. Plan B
   Tasks 14 and 18 must amend the shared status schema and its tests together before execution.

**Limits of this version:**

| Limit | Value | Reason |
|---|---|---|
| Film length | **10 minutes proposed**, 6 GiB container limit proposed | Plan B Task 13 records a 5,769 MiB peak with its real sources; Q3 remains open and source size matters. |
| Resolution | 1080p or 720p, at most 1920 px wide | Instagram's cap; measured file size depends on bitrate and duration. |
| Frame rate | the project's; 23–60 accepted | Instagram's range. |
| Video bitrate | **6 Mbps at 1080p / 3 Mbps at 720p proposed** | Plan B Task 11 records its 2026-10-02 real-footage sweep; replacement footage needs a fresh run, not reuse of those scores. |
| Concurrency | 1 job | S2 used 1.4–2.8 of 4 cores; two at once was not measured. |

Plan B Q1, Q2, Q3, Q5 and Q6 remain provisional. Its dated measurements support the
recommendations but do not close those questions. Q4 (amd64 only) and Q7 (direct publish
with egress) are operator decisions. The documentation revision does not record new PRD caps.

## Security and the internal API

- **Tokens.** Aura's OAuth server issues them for the sidecar's resource, as it does for
  `aura-pim-mcp` (`compose.yaml` `CALENDAR_MCP_OAuth__Resource`).
  - Aura's bridge obtains one per identity.
  - Claude Code obtains one by signing in as `claude@aura.local` through the tunnel already used for
    the memory MCP.
- **Internal API**, reachable only from the docker network: Caddy answers 404 on `/internal/video/`.
  Every call carries the bearer, and Aura checks its audience and identity. It has four operations:
  - `resolve`: asset ids → signed GET links on the internal object-store endpoint, valid 2 hours.
    Under Plan B Q5's provisional interpretation, upload-finished means status `accepted`,
    `processing` or `complete`, owned by the caller and not deleted. Everything else is "not
    found", without revealing whether it exists. `searchable` and `embedding` were removed by
    migration `0136_assets_drop_searchable.up.sql`; an unfinalized `uploaded` row is not eligible.
  - `presign` + `finalize`: a new asset through `assets.Service` (`handleAssetPresign`'s path, a new
    source kind `video_mcp` for the sidecar). Plan C adds `assets.SourceVideoMCP` and widens the
    database `assets_source_kind_check`, which currently admits `web`, `telegram`, `cli` and
    `agent` (`0035_assets_source_kind_agent.up.sql`). Choose the next free migration number when
    landing. Finalize verifies both ownership and this source kind; foreign assets answer 404.
    Q2 provisionally raises this handler's video limit to 300,000,000 bytes; source kind alone
    does not change `assets.Service`'s configured limits. Prove other upload paths retain their
    limits (50 MiB default for video) and define/test repeat-finalize semantics before retries.
  - `tts`: Aura's TTS (`internal/multimodal/tts.go`), saved as an audio asset.
  - `projects`: the identity's saved projects.
- **Inside the sidecar:**
  - it runs as a non-root user on a read-only filesystem;
  - scratch space is a tmpfs, retained during bounded credential/link recovery and emptied when
    the job finally ends; cleanup does not delete accepted library assets;
  - the page reaches only its own origin and the proxy, and the proxy only the job's allowlist;
  - the container has internet egress by the operator's Q7 decision. The page/proxy allowlists
    restrict rendering; the isolated synthetic CI tier can still run with `--network none`.
- **Gateway.** No approval gate: edits add versions, and render and cancel create or stop jobs;
  nothing is destroyed. Under the current policy only destructive actions reach the gate.
  *Amended 2026-10-02:* this holds only if each write tool says so. The bridge grades a tool with
  no `destructiveHint`, or a non-idempotent write that does not declare itself closed-world, as
  destructive (`internal/agent/mcptools/bridge_risk.go:225-234`, `:265-292`). So the sidecar's
  write tools carry explicit annotations: not destructive, closed-world.

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
  - every operation and its refusal, project-version atomicity and retained TTS assets,
    `before => after` lines;
  - the queue, cancellation between writes/during PUT, the stall watchdog and bounded SIGTERM;
  - bearer expiry during PUT, refresh before finalize/public resolve, a single claim deadline,
    scratch retention during recovery and no duplicate upload;
  - public-link failure and later-upload failure after earlier assets were accepted, with those
    ids retained in failed/cancelled status and no false "nothing saved" claim;
  - playback/analysis dependencies, including deleted unused sources, selected cleaned copies,
    used overlays/audio and missing originals needed for analysis;
  - the proxy refusing anything off its allowlist;
  - the output check on fixtures that are black, silent, too short, missing a stream, or AAC-less;
  - token verification against fake published keys;
  - the drift test.
- **Sidecar integration in CI:** a real render in the image (headless shell + ffmpeg) on synthetic
  fixtures, asserting H.264 High, AAC 48 kHz stereo, faststart, no edit list, 1080×1920 at 30 fps and
  the duration. Under `$CI` it fails if anything is missing; it never skips.
- **Aura (Go)**, the internal API and recipe:
  - audience, identity, "not found" semantics and accepted/processing/complete eligibility;
  - the source-kind migration, owned finalize and measured repeat-finalize behaviour;
  - the video-specific upload ceiling without widening other paths;
  - recipe/default-on/first-party grant and strict egress policy integration;
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
3. **Aura on Telegram (CDP harness):** the same request; *amended 2026-10-02:* a native video when the
   film comes through under the bridge's 25 MiB cap, and the library otherwise. Measured by Plan B
   (Task 11): a 1080p reel fits under 25 MiB up to about 54 s and a 60 s 720p reel is about 15 MB,
   so the native-video run uses a reel inside those bounds.
4. **Editing an existing project from chat:**
   - "accorcia l'intro di 2 s e abbassa la musica";
   - a new version, a new render, and the difference measured.
5. **A job that must fail:** a source deleted after the project was made. The job fails naming it,
   and no black video is produced.
   *Amended 2026-10-02:* under the normal-MCP shape a render reads every source it plays at its
   start, so a source deleted before the start makes `video_render_start` refuse, naming it. A job
   still fails, naming the source, when the source is deleted after the start or its bytes are
   broken. The E2E covers both.

Assets created only for tests are deleted from the operator's library afterwards. Nothing paid is
called (no `video_generate`) unless the operator asks.

## Environment variables

- **Compose:** `AURA_VIDEO_MCP_IMAGE`, `AURA_VIDEO_MCP_PULL_POLICY`, `AURA_VIDEO_MCP_PORT`, following
  `AURA_PIM_MCP_*`.
- **Limits** (film length, job age, concurrency, bitrate): documented constants in code, not env
  vars, until a deployment needs to change them. They are recorded in the PRD's caps table with the
  measurement behind each.

## What this does not prove

- Anything on arm64, which is not supported (amended 2026-10-02), or render times on the mini-PC's
  CPU. S2's seconds are the lab VM's.
- The original spikes used test patterns; Plan B's 2026-10-02 harness subsequently measured its
  stated camera/screen clips. It does not establish limits for arbitrary camera/4K footage.
  The original camera input is now missing; repeat measurements require explicit existing
  input paths, hashes and a fresh output directory (Plan B Task 11).
- Two renders at once.
- Whether the Opus → AAC transcode changes loudness. The E2E measures it; the spikes did not.
- Sub-project 3 (the panel), which reuses these tools and is designed separately.

## For review

- The internal API uses the caller's bearer, whose audience includes the sidecar. No bearer is
  minted for a job and no storage key bypasses ownership. Freshness is checked per authenticated
  request, not once for an upload that may outlive the credential.
- Aura and external clients use one visible MCP tool surface and status-based delivery.
- The 2026-10-05 applicability review found four missing regressions despite the original
  260 green tests. Plan B's correction checklist is required; historical coverage/typecheck
  evidence does not prove the revised implementation, Docker rendering or current-stack E2E.
