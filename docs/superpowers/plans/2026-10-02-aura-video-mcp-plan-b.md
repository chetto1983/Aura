# aura-video-mcp Plan B: the render sidecar `services/video-mcp/` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Aura's video sidecar. It edits a Video Studio project with the Studio's own commands and renders it to an Instagram-ready MP4, over MCP and under the caller's own Aura identity. No render output enters the library before the film passes its check. A failure names its reason and any assets already accepted during the final upload step; those sequential writes are not a transaction.

**Revised 2026-10-06 after the applicability review.** The contract corrections in
§Applicability corrections below supersede the affected 2026-10-02 code blocks, interfaces,
test expectations and recorded outputs. Tasks 2, 3, 14, 17 and 18 must implement and test
those corrections before their original blocks can be used. The corrections are requirements
for implementation, not newly tested service code. Q1, Q2, Q3, Q5 and Q6 remain provisional;
Q4 and Q7 are already decided. This documentation update does not close Gate 1 or amend the PRD.

**Original provenance, 2026-10-02.** Every original source and test file below was written and run in a scratch clone of `master`, moved last to `391a3c014`, master's head when that version of the plan was finished. A generator copied the scratch files byte for byte into the original document. This revised document changes contracts and execution requirements; that byte-for-byte and test claim applies only to the original version. Of the files the original plan edited by hand, master changed under it only `prd.md` (new §12 paragraphs; the anchor line is re-read in Task 11) and the spec (three commits, below). `internal/webui/dist`, which Task 10 regenerates, was rebuilt in between too.

**The spec changed three times while this plan was written**, each time on the operator's word, and the plan follows all three:
- **`698d37234`, "mount aura-video-mcp as a normal MCP"** (spec §"Amended 2026-10-02"). Aura's side is only the recipe, the Compose service and the internal API. There is no render-jobs table, watcher, native tool, job token, wake, progress card or hidden tool set: Aura's model calls `video_render_*` as any client does. For the sidecar this means three things:
  - **a job's compute never needs a token, and its writes wait for one.** `video_render_start` reads everything the job will read. The job keeps what it makes in its scratch until a token of its caller's can save it, for at most 15 minutes (Q1; Tasks 2, 13, 14, 16, 17, 18);
  - **delivery is the status call's.** A finished status carries the film as a `resource_link` up to the bridge's 25 MiB, served with `resources/read`, and points a larger film at the library and the Studio (Task 18);
  - **`video_render_start` tells the model** that the render runs in the background, to tell the user it started, and not to poll within a turn (Task 18).
- **`8d79a0e6f`**: on this path Telegram's native video stops at the bridge's 25 MiB. Task 11's PRD paragraph measures a reel against that cap.
- **`391a3c014`**, the operator: *"arm64 non supportato"*. The image is amd64 only, with no platform branch anywhere (Q4, now decided).

**One more problem turned up while bringing the plan onto them, and the sidecar fixes it.** Aura's bridge reads a tool with no `openWorldHint` as open-world, and grades its non-idempotent writes destructive, which stops the turn for the operator's approval. The spec wants no approval gate. The four writing tools now say they are closed-world (Tasks 9, 18; Review Focus 4).

Historical results from the original scratch clone (2026-10-02):
- the unit tier: 260 tests in 23 files, typecheck clean, coverage 98.88 % statements / 92.22 % branches / 99.54 % functions / 99.4 % lines;
- B1 alone (only Tasks 1–10's files, B1's seam and B1's `package.json`, freshly installed): 118 tests in 11 files, typecheck clean, 99.06 / 90.9 / 100 / 99.57;
- each of Tasks 14–19 at its own point in the sequence: its tests RED before it and GREEN after it, with the typecheck clean on exactly the files that exist by then;
- each rule the redirect added, broken on purpose and restored, its test failing each time:
  - the age clock running through a wait for the caller;
  - a token taken before the compute;
  - a cleaned copy saved as soon as it is made;
  - the browser's film kept beside the delivered one;
  - the start leaving the reads to the job;
  - a kept copy fetched like a source;
  - the closed-world hint removed;
- the render tier inside the built image, read-only root, uid 10001, no network: 14 tests, the three render-tier modules at 100 % on every metric. It was shown to fail when the transcode writes an edit list, and a real out-of-memory kill was shown to produce the job's sentence;
- what a film waiting for its caller costs: 300 MB held on the job's tmpfs, the cgroup read with an idle Node and with the sidecar's own image (Tasks 13, 20);
- whether a Compose network with `internal: true` keeps a loopback publish and drops egress, on Docker Engine 29.8.1 (Q7);
- the bundles, with the drift gate shown to pass and to fail;
- the web and Go edits, RED then GREEN, on the scratch clone's first base `ce204e7ef` (master has not touched those files since);
- the mutation gate's Python tests, RED (2 failures, 17 errors in 70) then GREEN (70);
- the CI backstop on real commit ranges, both outcomes; `ci.yml` as each of Tasks 10, 19, 20, 21 and 22 leaves it, rebuilt from master and the plan's blocks: actionlint reports no new finding, and the final one is byte-identical to the scratch tree's;
- the CI smoke step's exact commands against the image;
- the embedded cockpit rebuilt with the plan's `build.sh`, on `ce204e7ef` too.

Everything else above ran on `391a3c014`. The following were checked only statically and run first in CI or in Plan C: the three CI jobs, the Stryker run (its config was validated against Stryker's own schema, and its two suites were run, but no mutant was), the publish step, and everything §Contract for Plan C asks of Aura's Go side and Caddy. Each task says what was measured.

**Architecture:**
1. **One Node 24 process, an MCP server over streamable HTTP** (`@modelcontextprotocol/sdk` 1.31.0, stateless).
   - It verifies Aura's bearer the way `cmd/arcadedb-mcp/auth.go` does. The identity is the token's `sub` from Aura's issuer.
   - Every tool acts under that identity's token.
   - Projects live in the identity's own library, reached only through Aura's internal API, `/internal/video/*`. Plan C writes the Go side of that API; this plan writes its contract, `contract/internal-api.json`, and a fake that both sides test against.
   - Aura's model calls the tools as any MCP client does (spec §"Amended 2026-10-02"). A finished status links its film as `aura-video://film/<assetId>` when it fits the bridge's 25 MiB file cap, and the sidecar serves that link with `resources/read` under the reader's own token.
   - Every tool that writes says it is closed-world, so Aura's gateway asks no approval for it.
2. **The Studio core is the cockpit's own.**
   - `src/studio.ts` (Node) and `page/render.ts` (the browser) are the only modules that import `web/src/videoStudio`.
   - esbuild bundles both against `web/node_modules`, which carries the patched renderer-browser.
   - `drift.ts` reads esbuild's metafiles and refuses a build that took any of it from elsewhere.
3. **Command application is pure and fast.** `video_project_edit` applies the Studio's commands all or nothing for the project version. Asset probing, TTS and saving involve I/O. A refused list leaves the project version untouched and answers with the reason and what would be accepted; it names any speech asset already synthesized before the refusal (Task 8).
4. **Renders are jobs.** A FIFO queue runs one job at a time. Each job gets:
   - its own Chrome Headless Shell, launched with renderer-server's flags;
   - its own loopback origin, which serves the page, streams the job's sources from their signed URLs, plays back what the job made, and receives the film;
   - a stall watchdog (120 s) and an age limit (60 min, not counting a wait for its caller);
   - six steps that carry progress from 0 to 1: analyses, compile, render, transcode, check, upload.

   ffmpeg copies the H.264 stream, makes the sound AAC, puts the moov atom first and writes no edit list. An output check then verifies every Instagram row on the file, and only then does anything enter the library.

   **A job's compute never needs a token.** `video_render_start` reads the project and a signed two-hour link to every asset it plays, with the start call's own token. What the job makes stays in its scratch until its last step: the cleaned copies, the speech windows and the checked film. That step takes the freshest token the caller has shown, or waits up to 15 minutes for their next call (Q1).
5. **The image:** Node 24, Playwright's headless shell (pinned by Playwright 1.63.0) and Debian's ffmpeg, for linux/amd64. It runs as uid 10001 on a read-only root, on the default Compose network with a direct loopback publish and internet egress (Q7, decided). The render page and media proxy still enforce the job's allowlist.

**Tech Stack:**
- Node 24.16+ with TypeScript 7.0.2: `node build.ts` runs on Node's type stripping.
- Runtime dependencies: `@modelcontextprotocol/sdk` 1.31.0, jose 6.2.12, zod 4.6.5, Playwright 1.63.0.
- Build and test: esbuild 0.28.2, vitest and `@vitest/coverage-v8` 5.0.3 (web's own), ajv 8.20.0, Stryker 10.0.0.
- VideoFlow 1.3.4, patched, from `web/node_modules`.
- Debian bookworm's ffmpeg 5.1.9.
- Docker buildx and GitHub Actions; Python 3 for the mutation gate.

**Spec:** [the current design](../specs/2026-09-30-aura-video-mcp-design.md), revised 2026-10-06. The original plan used `391a3c014`; the network decision was subsequently recorded in `e9d379e28`. Sections:
- §"Amended 2026-10-02: the sidecar mounts as a normal MCP";
- §Architecture, "The sidecar";
- §Tools;
- §"Jobs and delivery";
- §"Security and the internal API";
- §"Tests and E2E", Gate 2;
- §"Environment variables";
- §"What this does not prove".

The evidence base is `spikes/video-mcp-render/FINDINGS.md` (S1.1–S4) plus the three measurements this plan records (Tasks 11–13). Plan A (`docs/superpowers/plans/2026-09-30-aura-video-mcp-plan-a.md`) shipped the export the render page calls, and its rulings hold here.

## Decided with the operator

- **The sidecar mounts as a normal MCP.** 2026-10-02, the operator: *"si monta come un normale MCP"*; recorded in the spec by `698d37234`.
- **Q4, arm64: not supported.** 2026-10-02, the operator: *"arm64 non supportato"*; recorded in the spec by `391a3c014` (§Architecture, "The sidecar"). The image is published for linux/amd64 only. The sidecar carries no arm64 branch: no architecture check, no `unsupported platform` answer, no platform stage in the Dockerfile, no second platform in the publish, and no test of any of these.
- **Q7, direct loopback publish with egress.** Recorded in the spec by `e9d379e28`, the operator: *"e chi se ne frega"*. Use the default Compose network and publish the service's own port on loopback, as `aura-pim-mcp` does. No internal `aura-video` network or Caddy relay is required. Caddy still returns 404 on `/internal/video/`.

## Questions for the operator

Q1, Q2, Q3, Q5 and Q6 are written on their recommendations, provisionally. A different answer changes the named tasks and their contracts. Editing these documents does not constitute an operator decision on those numbers or options. Q4 and Q7 are recorded above as decided.

**Q1. How long a checked film waits for its caller.** The amendment decides the behaviour and leaves this number to the plan: "The checked film is kept for a bounded time, which the plan fixes, and the identity's next tool call saves it. A film nobody claims in time is discarded, and the failure says why."
- The facts:
  - Aura's access tokens last 15 minutes: Authula v1.46.0 `plugins/jwt/types/config.go:25`, applied by `internal/webauth/mcp_token_plugin.go:98`.
  - Aura's bridge re-reads its grant before every call, and the keeper re-mints a grant with less than 7 minutes left, on a 5-minute tick (`cmd/aura/mcp_first_party_grants.go:44-45`). Each call Aura makes therefore brings a token with 5 to 15 minutes left. Nothing calls between turns.
  - A presign lasts 600 s (`AURA_ASSET_PRESIGN_TTL_SEC`, prd.md); a `resolve` link two hours.
  - The 10-minute film rendered in 1,358 s here (Task 13); a 60 s film in about 113 s on the lab VM (S2).
  - Every MCP call records its bearer before its tool runs (`http.ts`), so any call of that identity, a `video_render_status` included, ends a wait.
- **The shape built here: a job's compute never needs a token, and every write it makes waits for one.**
  - `video_render_start` loads the project and resolves every asset it plays, with the start call's own token (Task 18). The links last two hours, and a job reads only within its first 60 minutes. A played asset the library no longer holds refuses the start, naming it.
  - What the job makes stays in its scratch until the last step. The cleaned copies are played to the page from there under a local id (Task 16), the speech windows stay in memory, and the checked film waits there. The browser's own film is deleted after the transcode.
  - The last step makes every write: the cleaned copies, then the analyses version with their ids, then the film and its link. Each authenticated HTTP request obtains the freshest bearer at dispatch, including `finalize` after the potentially ten-minute PUT and the public `resolve` after it. A minute's margin bounds a short authenticated request; it does not authorize reuse across a PUT (correction R1).
  - When none is fresh, the job waits for the caller's next call, with the stall watchdog and the age limit stopped only during that wait (Task 14). All credential waits share one deadline, provisionally check completion plus `CLAIM_MS`; a later wait never starts another 15-minute allowance.
  - If the claim expires before any write, the checked film is discarded and the failure says why, with no accepted copy, version or film. If writes have already been accepted, the failure lists their asset ids; it must not say that nothing was saved (correction R3). Scratch cleanup waits until recovery succeeds, cancellation arrives or the deadline expires.
  - In practice, a film that renders within the 5 to 15 minutes the start call's token has left is saved at once. A longer one is saved by any call its caller makes within 15 minutes of its check, an "is it ready?" turn included.
- The claim deadline also bounds upload/link recovery: request timeouts must not run past its remaining time or the signed PUT's expiry. Cancellation or deadline expiry stops waiting and in-flight requests. A rejected credential cannot be retried in a loop; wait for a different verified caller bearer and retry only where commit/replay semantics are established.
- **Where the kept bytes live, and what they cost**, measured (Tasks 13, 20):
  - they live on the job's `/scratch` tmpfs, charged to the container's memory as shmem, byte for byte, and swap is off, so nothing reclaims them. The largest film the check passes (300,000,000 bytes) beside a 12 MB copy read 312,004,608 bytes of shmem;
  - with the browser gone, the sidecar at rest is 114 to 118 MiB, and 412 to 416 MiB of its 6 GiB while it holds them (two runs; the shmem is the same in both).
- Options:
  - **(a) Recommended.** Wait 15 minutes (`CLAIM_MS`, `src/jobs/limits.ts`, one token's life), and keep the queue's one slot meanwhile. A held film never shares the memory limit with a browser. The cost is the queue: the next render waits at most 15 minutes.
  - (b) Wait 60 minutes, keeping the slot. A caller who comes back within the hour keeps the film, but every render behind it waits up to an hour.
  - (c) Release the slot while a film waits, and keep the film up to the status's two-hour life. The next render then runs beside the held bytes: a 10-minute render's 5,769 MiB plus 297.6 MiB held leaves about 77 MiB of 6 GiB, and a second held film would not fit. It would need a cap on held bytes or a larger limit, and the two were never measured together.
- Owner: Task 2 (`Credentials.next`), Task 13 (the held bytes), Task 14 (`waitFor` stopping both clocks), Task 16 (the kept copies), Task 17 (the job's last step, `CLAIM_MS`) and Task 18 (the start's reads).

**Q2. A video presign may need 300 MB; Aura's video ceiling is 50 MiB.**
- `AURA_ASSET_MAX_VIDEO_BYTES` defaults to 52,428,800 (`internal/mediagen/ports.go:30`).
- A 10-minute film is held under Instagram's 300 MB row (Task 11), and a 3-minute 1080p reel at 6 Mbps is about 85 MB.
- Options:
  - **(a) Recommended.** The internal API's `presign` gives the sidecar's own source kind a 300,000,000-byte video ceiling. The contract says so: `presign` summary, "A video may be as large as Instagram's 300 MB row", and the output check refuses anything larger before upload.
  - (b) Raise the global default.
  - (c) Keep 50 MiB: a film over 52,428,800 bytes renders and is then refused at upload.
- Owner: Plan C's handler. This plan's contract text.

**Q3. Film length against memory.** Measured in Task 13: a 10-minute 1080p film peaked at 5,769 MiB under a 6 GiB limit at VideoFlow's default bitrate. At 6 Mbps under 5 GiB it completed only by page-cache reclaim, at the limit.
- **(a) Recommended.** 10 minutes, as the spec says. The container gets `mem_limit: 6g` with swap off. A kill is named: "the renderer ran out of memory (the renderer may use 6 GiB): render a shorter film, or use smaller source files". The limit is a ceiling, not a reservation: at rest only Node runs (114 to 118 MiB, Task 20), because the browser exists only during a job.
- (b) 10 minutes at 5 GiB. Measured to complete, with no headroom for larger sources.
- (c) 3 minutes at 3 GiB. Not measured; the fitted line puts it near 2.0 GiB of anonymous memory with these sources.
- Owner: Task 13 (`FILM_MAX_SECONDS`, the PRD rule) and Plan C's Compose block (§Contract for Plan C).

**Q5. "Complete" in the spec's `resolve`.**
- The spec says `resolve` answers "only for assets the identity owns and that are complete".
- A film or cleaned copy is accepted through `FinalizeMedia`, which never processes it (`internal/assets/service_accept.go:38-43`), so its status stays `accepted` and would never resolve.
- A project version the sidecar just saved goes through the document path and is not `complete` until the ingest is done with it.
- **(a) Recommended.** "Complete" means the upload finished. The contract's `x-bytes-exist` lists `accepted`, `processing` and `complete`. Migration `0136_assets_drop_searchable.up.sql` removed `searchable` and `embedding`; they are not valid current asset statuses. `uploaded` is not sufficient: the upload must have been finalized.
- (b) The literal `complete` status only: a film could never be downloaded, and a project could not be opened right after it was saved.
- Owner: Task 3 (contract) and Plan C.

**Q6. "Each source is fetched once and cached for the job."**
- Measured (Task 13): the page asks for each source once per pass, whole, with no Range request.
- A copy on the job's tmpfs would be charged to the same memory limit as the browser: 0.81 MiB of anonymous memory per MiB of source is already there, and a 300 MB cache would come on top.
- **(a) Recommended.** Stream each source from its signed URL as the page asks (`src/jobs/pageServer.ts`). A job that analyses and renders asks for a source at most once per pass.
- (b) Cache on the tmpfs, as written. The limit in Q3 would then have to grow by the job's source bytes.
- Owner: Task 16.

**Q7. Decided: direct loopback publish with egress (Plan C's Compose block).** The original spec asked for both no egress and a loopback publish. Measured on Docker Engine 29.8.1:
- a container whose only network is `internal: true` has no egress (`fetch('https://example.com')` fails with `EAI_AGAIN`) and still reaches a peer on that network;
- but its published port answers nothing on the host;
- the same container with an ordinary network as well answers on its port, and reaches the internet (200).

That measurement does not establish a rule for every Docker engine. On this measured setup,
the operator chose the default network and the service's own loopback publish, recorded in
spec commit `e9d379e28`. The container has internet egress; the page route and Node media
proxy restrict a render to Aura's returned URLs. Plan C measures host access and the proxy
allowlist on the target stack. Caddy's only video change is the internal-prefix 404.
- Owner: Plan C's Compose block and Caddyfile (§Contract for Plan C, 3–4).

## Applicability corrections (2026-10-06)

The 2026-10-05 review extracted the original UTF-8 code blocks into a scratch tree on
`d605357e9a8084ca068ffa78d2c3b56856e290e0`. The original 260 unit tests and typecheck
passed. Four added job regressions failed, with 260 passes out of 264; the typecheck still
passed. The mutation/workflow/release contract checks passed (70 tests). These results
describe the original code, not the corrections below. Docker renders, current-stack E2E
and Stryker were not rerun in that review. Subsequent scheduler commits through `372fe8d48`
did not change the reviewed inputs. Repository assumptions below were re-read on
2026-10-06 at `d2f078572`; the entire extracted suite was not rerun there.

| Correction | Measured defect | Required change and acceptance | Owner |
|---|---|---|---|
| R1: credentials during upload | A fresh bearer arriving during the signed PUT is ignored by `finalize`, which uses the earlier token and fails with 401. | Obtain the current caller bearer immediately before each authenticated request. Preserve the presigned asset id, PUT completion and checked scratch while waiting for a fresh bearer. Resume the same finalize, then resolve with a newly selected bearer, within one claim deadline. Verify no second presign, no duplicate accepted asset, and no premature scratch cleanup. Retry finalize only after Plan C proves repeat-finalize semantics; never blindly retry a write whose outcome is unknown. | Tasks 2, 3, 17; Plan C API tests |
| R2: cancellation during saves | Cancel after the first cleaned-copy upload; the original runner still uploads the project and film (three writes instead of one). | Check the job signal before credential acquisition and every subsequent write; propagate it through API requests and the signed PUT alongside each request's timeout. A cancelled job starts no later write. A write already in flight can have committed: report that outcome under R3. Verify cancellation during PUT, between saves and during credential waits, plus SIGTERM with an active upload. | Tasks 3, 14, 17, 18 |
| R3: partial saves and delivery | When public `resolve` omits an already accepted film, the job fails but the film remains in the library. The API has no rollback operation. | Guarantee no render output is saved before the output check, not a transaction across sequential uploads. Retain accepted asset ids as each save completes. Failure and cancellation status name saved copies, versions and films; uncertain write outcomes are described as uncertain. Retry an interrupted public resolve for the same film under a fresh bearer within the deadline; never render or upload that film again merely to obtain its link. Success still requires a usable delivery link. | Tasks 3, 14, 17, 18; spec §Jobs and delivery |
| R4: unused sources | A deleted still source, absent from the timeline, prevents a valid render from starting. | Derive dependencies from timeline video and audio items, image overlays and `analysisPlan`, using the Studio's source/audio helpers and compile graph. Resolve originals needed for missing analyses, and cleaned assets only when the selected playback needs them. Ignore unused sources and unused cleaned copies. Replace the test expecting every source in `project.sources`: that expectation is the bug. | Task 17 `played.ts`, job tests; Task 18 start tests |

Implementation acceptance, required in addition to the original suites:

- [ ] R1: token expires during PUT, a new call supplies a token, and the same upload finishes and delivers once.
- [ ] R1: a 401 before commit can wait for refresh; a lost response after commit cannot cause a duplicate upload. Plan C verifies finalize replay for the same owned `video_mcp` asset.
- [ ] R1: repeated credential waits do not extend the deadline fixed when the output check completes.
- [ ] R2: cancel after the first accepted copy; no project or film write follows, and status names the copy.
- [ ] R2: cancel during PUT or a credential wait; the operation settles, the queue becomes idle and SIGTERM exits within the measured Compose stop grace.
- [ ] R3: public resolve fails after film acceptance; status identifies that existing film, and a retry uses its asset id without another upload.
- [ ] R3: a later copy or project upload fails; status names all earlier accepted writes and never claims an empty library.
- [ ] R4: delete an unused source or leave an unused `denoisedAssetId`; start succeeds. Delete a played source or an original needed by a missing analysis; start refuses with its name.
- [ ] R4: used picture/audio/overlay dependencies are included, denoise on/off chooses the correct playback asset, and asset ids are deduplicated.
- [ ] Plan C: migrate the `assets_source_kind_check` for `video_mcp`, verify ownership on finalize, and exercise accepted/processing/complete resolution against the real handlers.
- [ ] Task 11: supply existing real camera and screen files explicitly, record hashes and use a fresh measurement output directory. Historical numbers are reference results, not expected values for replacement footage.

**Execution rule.** The Task 3 client/fake, Task 14 queue/status, Task 17 `played.ts` and
`job.ts`, and Task 18 tool schemas/tests below are the pre-review baseline. Their code
blocks must not be copied as a finished implementation or used to waive the acceptance
above. The affected tasks add failing regressions first, correct their implementation and
shared schemas together, and record fresh results. Other original measurements retain
their dated scope. This avoids treating a documentation amendment as tested source code.

## B1 and B2

There are 22 tasks, so this plan proposes two halves, each closed and pushed on its own.
- **B1, the edit half (Tasks 1–10).**
  - Delivers: the sidecar's MCP server, its bearer check, the internal API's contract, client and fake, the Studio core moved so a second consumer can import it, the edit engine, the four project tools, and the unit tier's own CI job, so B1's push is tested in CI.
  - Proven: in-process, over real MCP against the contract-checked fake.
  - Not included: no image and no process entry yet. `src/main.ts` would be dark code until there is a bundle to run it from, so it lands in B2's Task 19.
  - The run behind it: B1's files alone, with B1's `package.json`, were installed and run (118 tests, 99.06 / 90.9 / 100 / 99.57).
- **B2, the render half (Tasks 11–22).**
  - The three measurements, each recorded in the PRD before the code it justifies.
  - The queue, the output check, the page and its origin, the job runner, the render tools.
  - The bundles and the drift gate, the image and the render tier with its CI job, the mutation gate and the publish.

If the operator keeps one plan, the tasks run in order unchanged.

## Global Constraints

**Scope**
- Plan B is the sidecar `services/video-mcp/`, its image `docker/aura-video-mcp/Dockerfile`, its CI and publish, the internal API's contract (`contract/internal-api.json`), and the Studio edits a second consumer needs. Nothing in Go beyond one test path and two comments (Task 4).
- Plan C writes Aura's side, which the amendment of 2026-10-02 reduced to three things plus the proof:
  - the `video` recipe in the catalog, which makes the sidecar a first-party server whose grant Aura mints and renews (`cmd/aura/mcp_first_party_grants.go`);
  - the Compose service;
  - the internal API's four operations, with the Caddy 404 on their prefix;
  - the E2E.

  There is no render-jobs table, watcher, native tool, job token, completion route, wake, progress card or hidden tool set. The sidecar is not a capability Aura requires. §Contract for Plan C is the boundary.
- The image is built and published for linux/amd64 only, with no platform branch in the code, the image or CI (decided 2026-10-02).
- `exportProject(project, urls, options?)` keeps its signature (Plan A). Plan B adds one optional field, `videoBitrate`, unset in the cockpit.
- `StudioProjectRekey` (`internal/assets/studio_project_rekey.go`) is not changed beyond a comment path: its rule reads the parser that moves to `projectFile.ts` in Task 4, and Task 4's Go parity test keeps the three spellings bound.
- VideoFlow stays 1.3.4 with the existing patch.

**Spec rules, quoted**
- "HEVC is not supported by the renderer: convert the clip to H.264 first".
- "Every request from the page is routed through Node", "allowed only to the signed URLs the internal API returned for this job", and "Any non-2xx answer or network error **fails the job with the source's name**".
- "documented constants in code, not env vars" (§Environment variables).
- "Under `$CI` it fails if anything is missing; it never skips" (§Tests and E2E).
- "No approval gate: edits add versions, and render and cancel create or stop jobs; nothing is destroyed" (§Security, "Gateway").
- "A render that finishes after its token expired must not be lost" and "A film nobody claims in time is discarded, and the failure says why" (§"Amended 2026-10-02" 2).

**Plan A carry-overs (all honoured)**
- `hasAudio` is filled from ffprobe, and the Studio refuses sound over a silent video (`web/src/videoStudio/commands_audio.ts:136,171`). Task 6 fills it; Task 8 pins both refusals.
- `ExportSourceError` crosses the page boundary as plain data: `{assetId, failure, message}` in `PageFailure` (Task 17).
- No code derives an asset id from an object key: every id comes from the internal API's rows.
- `PROJECT_FILE_EXTENSION` is imported, never respelled (Tasks 4, 9).

**Files and quality bars**
- No `.go`, `.ts` or `.tsx` file over 600 lines (`scripts/check-file-size.sh`, pre-commit).
- Coverage has two tiers, each with its own 85 % floor on statements, branches, functions and lines, never averaged:
  - the unit tier (`vitest.config.ts`);
  - the render tier (`vitest.integration.config.ts`), over `src/jobs/browser.ts`, `outputProbe.ts` and `transcode.ts`, the modules only a real browser and ffmpeg run.

  `src/jobs/inPage.ts` runs inside the page, so no Node tier can see it. It is four one-line functions, out of both tiers by name.
- Mutation testing runs in CI only (Stryker, `break: 70`). Never run it locally.
- Web files keep web's gates: prettier, tsc, oxlint at zero, knip.
- Every new user-visible sentence is English. The sidecar's tool answers are English; the Studio's bundle sentences come from `videoStudioEn`.

**How to run tools**
- Never run a Windows `.exe` from Git Bash. `node`, `npm`, `npx`, `go`, `python3`, `docker` and `gh` are exes there.
- Run tools in WSL through script files: `MSYS_NO_PATHCONV=1 wsl bash <script> <args>`. Never put `$VAR` inside `wsl bash -c "…"`.
- `npm ci` and `npm install` run only in `services/video-mcp/` (its own `node_modules`, ignored by git). Never in `web/`: its `node_modules` is shared with Windows and already carries the Linux bindings.
- Never run `go build ./...`. The pre-push hook builds the pushed tree.
- No paid API call of any kind. Do not touch the lab VM or the appliance.

**Commits and pushes**
- Commit exact paths only, from WSL, with lefthook (`$W/commit.sh`). `--no-verify` is forbidden.
- The tree is shared: never stage, reset or revert another session's files.
- PRD first (CLAUDE.md): a measurement is committed as its PRD paragraph before the code it justifies (Tasks 11–13).
- Push at the end of each half: B1 in Task 10, B2 in Task 22. CI must be green on that SHA, every job (Task 22 says how to read it).

**Path shorthand**
- `$W` means your own session's scratchpad as WSL sees it (`/mnt/c/Users/…/scratchpad`). Every helper below lives there.
- Plain Git Bash commands (`wc -l`, `grep`, `git -C /d/Aura …`) run from `/d/Aura`.
- `$M` is the measurement work directory, `~/planb-scratch/measure` in WSL (`MEASURE_DIR` overrides it). It holds 650 MB of fixtures, outside the repository.

## Review Focus

No task's main path exercises these five inputs, and each is the likeliest way this plan could fail a real person. Each is pinned by a test in the task that owns the code.

1. **A render that outlives the caller's token** (Q1; the amendment's point 2). The job must compute with no token at all, must save nothing before a token lets it save everything, must not lose a film while a call can still save it, and must not wait forever. Pinned:
   - in Task 2 by `credentials.test.ts` "must outlive the next call by a minute", "ends with the first call that brings a fresh one, and not with another identity’s or a stale one", "gives up after its time with nothing, so the caller can say why" and "stops at once with the reason its job was stopped for";
   - in Task 14 by `queue.test.ts` "not while it waits for its caller, and by the watchdog again once that wait is over" and "when it is older than 60 minutes, not counting the time it waited for its caller";
   - in Task 16 by `proxy.test.ts` "plays a copy the job made and has not saved from scratch, under the local id it was given";
   - in Task 17 by `job.test.ts` "computes with no credential at all, and makes every write with the one its caller’s next call brings" and "when no call of its caller’s came in time to save what it made, saving none of it".
2. **A source deleted, or its bytes broken, after the project was saved** (spec E2E 5). The render fails naming the file and saves no film. Pinned:
   - in Task 16 by `proxy.test.ts`: "records a source the object store will not give, by its file name and never by its URL", "records a source whose store cannot be reached at all", and "records a source whose body stops halfway, after its status said yes". The last was added for this list: it covers the page server's one line no test reached, and it was shown to fail with the guard removed;
   - in Task 17 by `job.test.ts` "refuses a project whose played asset the library no longer holds, naming it", and in Task 18 by `renderTools.test.ts` "creates no job for a project whose source the library no longer holds, and names it";
   - in Task 20 by the render tier's "fails naming a source the object store will not give, and delivers no film", "is refused at its start, naming a source the library no longer holds" and "fails naming a source whose bytes the renderer cannot play".
3. **A page that asks for anything off its allowlist**: `../`, an encoded slash, another origin, another asset id. It gets a 403 or a 404 and never a byte. Pinned:
   - in Task 16 by `proxy.test.ts`'s "what the page may reach" table (another port, host, scheme, userinfo, Google Fonts, the object store itself) and its "what the proxy serves" table (another asset id, `/media/<id>/extra`, a malformed escape);
   - by "serves the page and its fonts, and nothing above them": `fonts%2F..%2F..%2F..%2Fetc%2Fpasswd` answers 404 with an empty body;
   - in Task 20 by the render tier's "reaches nothing but its own origin", in a real browser.
4. **A write that Aura's gateway would stop for approval.** Aura's bridge reads a missing `openWorldHint` as open-world. It grades a write that is neither read-only nor idempotent, in an open world, as destructive (`internal/agent/mcptools/bridge_risk.go:231`, `:270-289`), and a destructive tool stops the turn for the operator's approval (`internal/gateway/classify.go:63`). Every create, every edit and every render would wait for a click, though the spec wants no gate. Pinned:
   - in Task 9 by `projectTools.test.ts` "declares its two writes closed-world, so Aura’s bridge grades them reversible and asks no approval";
   - in Task 18 by "advertises the render tools exactly as contract/render-tools.json records them for Aura", whose file now carries each tool's annotations.

   Both were shown to fail with `openWorldHint` taken out of `LIBRARY_WRITE`.
5. **A cancel or a crash while the browser is up.** No Chromium is left running. Pinned in Task 20 by "stops when cancelled, and leaves no browser behind" (a `/proc` scan that first proves a browser was running) and "fails with the browser's own words when its page crashes, and leaves no browser behind".

## What the plan stands on (verified 2026-10-02)

**The SDK, read in the installed 1.31.0.**
- `createMcpExpressApp` (`server/express.js`) is used with an allowed-hosts list.
- `requireBearerAuth` answers a missing or bad bearer with 401 and `WWW-Authenticate: Bearer … resource_metadata="…"`; measured: `401 Bearer error="invalid_token", error_description="Missing Authorization header", scope="mcp:tools", resource_metadata="http://127.0.0.1:18097/.well-known/oauth-protected-resource/mcp"`.
- `metadataHandler` serves RFC 9728 metadata.
- A `StreamableHTTPServerTransport` without `sessionIdGenerator` is stateless (`webStandardStreamableHttp.js:738`).
- A thrown tool error becomes `{isError: true}`. An input that fails the zod schema becomes an `isError` result reading "Input validation error". A tool with an `outputSchema` must answer `structuredContent`.
- The SDK's types are not `exactOptionalPropertyTypes`-safe: a transport is passed `as Transport`, and the stateless option is omitted rather than set to `undefined`.

**The renderer.**
- renderer-server's launch flags are `ServerRenderer.js:98-129` and its stall rule `:940` (`STALL_MS = 120_000`), in `spikes/video-mcp-render/node_modules/@videoflow/renderer-server/dist/`. The spec's `:100-110` covers only part of the flag list.
- WebCodecs in the headless shell encodes H.264 High and Opus. It has no AAC encoder: every render logs `Using Opus audio (128 kbps) — AAC not available on this platform`.
- mediabunny's `computeVideoBitrate` gives 3 Mbps × (pixels / 1080p)^0.95 × `qualityToBitrateFactor`; VideoFlow's default quality is about 12 Mbps at 1080p.
- The headless shell's H.264 has no B-frames, so every GOP is closed. Its keyframes came up to 5 s apart in reel-30 (8 in 29 s).

**Aura.**
- The asset service has `PresignPut` only (`internal/objectstore/s3.go:72`). There is no `PresignGet`, so `resolve` needs one: Plan C adds it.
- Access tokens last 15 minutes, and a presign 600 s. The grant keeper re-mints a grant with less than 7 minutes left on a 5-minute tick, so each call Aura makes brings 5 to 15 minutes (Q1).
- The video ceiling defaults to 50 MiB (Q2).
- `FinalizeMedia` accepts an editor source with no processor (Q5).
- Aura's bridge, which the amendment makes the sidecar's only Aura-side client:
  - it carries a file into a chat up to 25 MiB per file, exactly 25 MiB included, and 50 MiB per call (`internal/mcp/file.go:20-22`, `file_test.go:85`);
  - it reads a `resource_link` with `resources/read` on the session that made the call, and never fetches the URI itself (`internal/agent/mcptools/bridge_links.go`);
  - one MCP call is bounded at 60 s by default (`internal/agent/mcptools/timeout.go:13`, `AURA_MCP_CALL_TIMEOUT_SEC`). The render tools answer at once; a `video_project_edit` that probes many new assets (20 s each at most, Task 6) could pass it;
  - a structured result whose `error` is a non-empty string is read as a failed call (`internal/mcp/domain_outcome.go`), so a failed job's status reaches Aura's model as an error result that carries the reason;
  - it grades a recipe's tool by the server's annotations when the recipe has no action table, and reads a missing `openWorldHint` as true (`internal/agent/mcptools/bridge_risk.go:196-289`). Only a destructive tool stops the turn for approval (`internal/gateway/classify.go:63`);
  - under a strict profile it dials a private address only for a built-in HTTP recipe it knows (`internal/mcp/egress_policy.go:66-77`), which Plan C extends to `recipe:video`.
- Only the memory capability is required at boot (`cmd/aura/serve_memory_readiness.go:71`). A default-on recipe whose sidecar is missing is a WARN at mount (`internal/mcp/manager/runtimeset.go:43-44`).

**The Studio.**
- `projectStore.ts`'s parser lived beside the cockpit's React save path, so bundling the parser pulled React and the chat attachment client into the sidecar; Task 4 moves the pure half to `projectFile.ts`.
- `sourceEdit` sat in `VideoStudio_sources.ts` with the upload UI's imports; Task 4 moves it to `sourceEdit.ts`.
- The Studio's default junction transition lasts 1 s.
- `add_speech` places its sound at `time`, the field name `add_audio` already uses. The spec wrote "where".

**Measured on this host for this plan.** WSL2 Docker on the operator's Windows 11 desktop, `--cpus=4`, Playwright 1.63.0's Chrome Headless Shell 153.0.8010.12, Node 24.21.0, Debian ffmpeg 5.1.9.
- The page fetches each source once, whole, with no Range request.
- Bitrate per resolution (Task 11).
- The stream copy against the Instagram rows (Task 12).
- Memory at 10 minutes, the OOM sentence, and what a film waiting for its caller costs (Tasks 13, 20).
- Docker Engine 29.8.1's `internal: true` network: no egress, and no loopback publish (Q7).

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `services/video-mcp/package.json`, `package-lock.json` | create (T1), grow (T17, T19, T20, T21) | pins; scripts `typecheck`, `test`, `build`, `test:integration`, `mutation` |
| `services/video-mcp/tsconfig.json` | create (T1), modify (T17) | web's strict options plus `allowImportingTsExtensions` |
| `services/video-mcp/vitest.config.ts` | create (T1) | the unit tier; `RENDER_TIER` and `IN_PAGE` named once |
| `services/video-mcp/.gitignore` | create (T1) | `dist/`, `coverage/`, `.stryker-tmp/`, `reports/` |
| `services/video-mcp/src/config.ts` | create (T1) | `PORT` 8097, `SCOPE`, paths, `configFromEnv` |
| `services/video-mcp/src/log.ts` | create (T2) | JSON lines on stdout |
| `services/video-mcp/src/auth.ts` | create (T2) | `tokenVerifier`, `callerOf` |
| `services/video-mcp/src/credentials.ts` | create (T2) | the freshest token per identity, and the wait for its next call (Q1) |
| `services/video-mcp/contract/internal-api.json` | create (T3) | the internal API, one JSON Schema both sides test against |
| `services/video-mcp/src/internalApi.ts` | create (T3) | the client: resolve, upload (presign + PUT + finalize), tts, projects |
| `web/src/videoStudio/projectFile.ts` | create (T4) | the project file's name and parser, pure |
| `web/src/videoStudio/projectStore.ts` | rewrite (T4) | the cockpit's save and load, importing `projectFile.ts` (457 → 125 lines) |
| `web/src/videoStudio/sourceEdit.ts` | create (T4) | `sourceFrom`, `sourceEdit`, `IMAGE_SECONDS`, `STARTING_*` |
| `web/src/videoStudio/VideoStudio_sources.ts`, `VideoStudio.tsx` | modify (T4) | import the moved halves |
| `web/src/videoStudio/videoflow_audio.ts` | modify (T4) | `clipSound` folds three copies; `uncleanedSources` |
| `web/src/videoStudio/videoflow.ts` | modify (T4) | `ExportOptions.videoBitrate` |
| `internal/objectstore/studio_project_test.go`, `asset_placement.go`, `internal/assets/studio_project_rekey.go` | modify (T4) | the parity test and two comments point at `projectFile.ts` |
| `internal/webui/dist/**` | regenerate (T10) | the embedded cockpit, rebuilt from Task 4's sources |
| `services/video-mcp/src/studio.ts` | create (T5) | the one Node door into the Studio |
| `services/video-mcp/src/refusal.ts`, `src/sentences.ts` | create (T5) | a refusal, and the Studio's bundle sentence plus what would be accepted |
| `services/video-mcp/src/probe.ts`, `src/sources.ts` | create (T6) | ffprobe facts, HEVC refused; a source added once, by asset id |
| `services/video-mcp/src/http.ts` | create (T7) | `/health`, the metadata, `/mcp` behind the bearer |
| `services/video-mcp/src/operations.ts`, `src/changes.ts`, `src/summary.ts` | create (T8) | the 20 operations, all or nothing; `before => after`; the timeline summary |
| `services/video-mcp/src/projects.ts`, `src/tools/projectTools.ts` | create (T9) | load, save, list; the four project tools; `LIBRARY_WRITE`, the closed-world annotation of every writing tool |
| `spikes/video-mcp-render/plan-b/*` | create (T11) | the measurement harness, `held.sh` included (run in T13 and T20) |
| `prd.md` | modify (T11, T12, T13) | "Render bitrate", "Instagram rows", "Render memory" in §12 |
| `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md` | modify (T11, T13) | dated notes beside the sentences the measurements answered |
| `services/video-mcp/src/jobs/limits.ts` | create (T11), modify (T13, T17) | quality, bitrate, frame, film length, ages, the caller wait |
| `services/video-mcp/src/jobs/transcode.ts` | create (T12) | ffmpeg's arguments and run |
| `services/video-mcp/src/jobs/memory.ts` | create (T13) | the cgroup's OOM kills and the sentence |
| `services/video-mcp/src/jobs/queue.ts` | create (T14) | `JobQueue`, `STEPS`, the request with what the job reads, the watchdog, the age limit, and both stopped while a job waits for its caller |
| `services/video-mcp/src/jobs/outputCheck.ts`, `mp4Facts.ts`, `samples.ts`, `outputProbe.ts` | create (T15) | the rows, the file's facts, where to sample, how to read them |
| `services/video-mcp/src/jobs/proxy.ts`, `pageServer.ts` | create (T16) | the allowlist (mutated in CI); the job's origin, which also plays what the job made from its scratch |
| `services/video-mcp/page/render.ts`, `types/url-imports.d.ts` | create (T17) | `window.aura`: detectSpeech, clean, render |
| `services/video-mcp/src/jobs/played.ts` | create (T17) | what a render reads, resolved at its start: every played asset, or a refusal naming the one that is gone |
| `services/video-mcp/src/jobs/inPage.ts`, `browser.ts`, `analyses.ts`, `job.ts` | create (T17) | the browser and the six steps; every write in the last |
| `services/video-mcp/src/tools/renderTools.ts`, `src/tools/filmResource.ts`, `contract/render-tools.json` | create (T18) | the three render tools and the start's reads, the film link and its `resources/read`, the published descriptions, annotations and schemas |
| `services/video-mcp/src/main.ts`, `services/video-mcp/drift.ts`, `services/video-mcp/build.ts` | create (T19) | the process; the drift gate; the two bundles |
| `services/video-mcp/vitest.integration.config.ts`, `test/integration/*` | create (T20) | the render tier |
| `docker/aura-video-mcp/Dockerfile`, `.dockerignore` | create, modify (T20) | the image, linux/amd64: the runtime and integration targets |
| `.github/workflows/ci.yml`, `scripts/web_filter_backstop.sh` | modify (T10, T19, T20, T21) | three jobs and the aggregate's inputs; the backstop takes the job's pathspecs |
| `services/video-mcp/stryker.config.json`, `vitest.stryker.config.ts` | create (T21) | the mutation run |
| `scripts/critical_mutation_gate.py`, `_test.py`, `scripts/mutation_workflow_test.py`, `Makefile` | modify (T21) | the `video_mcp` scope |
| `.github/workflows/publish-aura-edge.yml` | modify (T22) | `ghcr.io/chetto1983/aura-video-mcp`, linux/amd64 |

Every `test/` file sits beside its task and is listed there.

## Helper scripts

Create each of these in `$W` from the text below and read it before you first run it. Each runs from WSL as `MSYS_NO_PATHCONV=1 wsl bash $W/<name>.sh …`.

| Script | What it does |
|---|---|
| `commit.sh <message file> <paths…>` | commits exactly those paths with lefthook's pre-commit gates |
| `svc.sh <tests…>` / `types` / `all` | the sidecar's unit tier: some files, the typecheck, or both plus coverage and its floors |
| `svc-npm.sh <npm args…>` | npm in `services/video-mcp/` only |
| `vt.sh <paths…>` | vitest in `web/` |
| `webcheck.sh <files…>` | web's prettier, tsc, oxlint, lint contract, knip |
| `go-gates.sh <packages…>` | gofmt, vet, race, coverage, lint |
| `image.sh` | builds both image targets from the repository |
| `render-tier.sh [command…]` | the render tier and the runtime smoke as CI runs them |
| `pygate.sh` | the mutation gate's Python tests |
| `build.sh` | rebuilds `internal/webui/dist` |
| `push.sh <log name>` | `git push origin master` with lefthook's pre-push gates; fails if no gate ran |
| `poll-ci.sh <sha> [minutes]` | waits for every run on that commit, then prints each conclusion |

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

`$W/svc.sh`:

```bash
#!/usr/bin/env bash
# The sidecar's unit tier in services/video-mcp.
#   svc.sh <test files…>   vitest on those files: every failure with its context, then the totals
#   svc.sh types           the typecheck alone (tsc --noEmit over src, page, test and the configs)
#   svc.sh all             the typecheck, then the whole tier with coverage against its 85 % floors
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
cd /mnt/d/Aura/services/video-mcp || exit 1
out="$(mktemp)"
case "${1:-}" in
  types)
    ./node_modules/.bin/tsc --noEmit -p tsconfig.json > "$out" 2>&1; echo "TSC_RC=$?" >> "$out"; rc=0 ;;
  all)
    ./node_modules/.bin/tsc --noEmit -p tsconfig.json > "$out" 2>&1; echo "TSC_RC=$?" >> "$out"
    ./node_modules/.bin/vitest run --coverage >> "$out" 2>&1; rc=$? ;;
  *)
    ./node_modules/.bin/vitest run "$@" > "$out" 2>&1; rc=$? ;;
esac
sed 's/\x1b\[[0-9;]*m//g' "$out" | grep -E -A12 "FAIL |AssertionError|Error:|error TS|×|⎯⎯ Unhandled" | grep -v '^\s*$' | head -120
sed 's/\x1b\[[0-9;]*m//g' "$out" | grep -E "TSC_RC|Test Files|Tests  |All files|ERROR: Coverage"
echo "VITEST_RC=$rc"
rm -f "$out"
```

`$W/svc-npm.sh`:

```bash
#!/usr/bin/env bash
# npm in services/video-mcp, with WSL's Node: svc-npm.sh install | ci | run build | install --save-exact …
# The sidecar's node_modules is its own (git-ignored). Never run npm in web/.
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
cd /mnt/d/Aura/services/video-mcp || exit 1
npm --no-audit --no-fund "$@" 2>&1 | tail -15
echo "NPM_RC=${PIPESTATUS[0]}"
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

`$W/image.sh`:

```bash
#!/usr/bin/env bash
# Builds docker/aura-video-mcp/Dockerfile from the repository for amd64, both targets:
# aura-video-mcp:local (the runtime Compose will run) and aura-video-mcp:integration (the render tier).
set -uo pipefail
cd /mnt/d/Aura || exit 1
for target in runtime integration; do
  tag=local; [ "$target" = integration ] && tag=integration
  log="$(mktemp)"
  start=$(date +%s)
  docker build -f docker/aura-video-mcp/Dockerfile --target "$target" -t "aura-video-mcp:$tag" . > "$log" 2>&1
  rc=$?
  tail -4 "$log"; rm -f "$log"
  echo "$target build exit: $rc after $(( $(date +%s) - start )) s"
done
docker image ls --format '{{.Repository}}:{{.Tag}} {{.Size}}' | grep '^aura-video-mcp:'
```

`$W/render-tier.sh`:

```bash
#!/usr/bin/env bash
# The render tier and the runtime smoke as ci.yml video-mcp-render runs them, against the image
# image.sh built: read-only root, uid 10001, no network, a tmpfs wherever vitest writes.
# render-tier.sh [command…]: a command replaces the image's own (e.g. one test with -t).
set -uo pipefail
out="$(mktemp)"
docker run --rm --read-only --memory=4g --memory-swap=4g --network none \
  --tmpfs /tmp:size=2g,mode=1777 \
  --tmpfs /repo/services/video-mcp/node_modules/.vite:uid=10001,mode=0700 \
  --tmpfs /repo/services/video-mcp/node_modules/.vite-temp:uid=10001,mode=0700 \
  --tmpfs /repo/services/video-mcp/coverage:uid=10001,mode=0700 \
  aura-video-mcp:integration "$@" > "$out" 2>&1
echo "RENDER_TIER_RC=$?"
sed 's/\x1b\[[0-9;]*m//g' "$out" | grep -E -A14 "FAIL |AssertionError|×|✓|Test Files|Tests  |All files|\.ts +\||ERROR" | grep -v '^\s*$' | head -80
rm -f "$out"
docker rm -f video-mcp-smoke > /dev/null 2>&1
docker run -d --name video-mcp-smoke --read-only --tmpfs /tmp:mode=1777 \
  --tmpfs /scratch:size=700m,uid=10001,mode=0700 -p 127.0.0.1:18097:8097 \
  -w /app aura-video-mcp:integration node server.mjs > /dev/null
for _ in $(seq 1 30); do curl -fsS http://127.0.0.1:18097/health > /dev/null 2>&1 && break; sleep 1; done
echo "health: $(curl -s http://127.0.0.1:18097/health)"
echo "metadata: $(curl -s http://127.0.0.1:18097/.well-known/oauth-protected-resource/mcp)"
echo "no bearer: $(curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:18097/mcp)"
echo "user $(docker exec video-mcp-smoke id -u), node $(docker exec video-mcp-smoke node --version), $(docker exec video-mcp-smoke ffmpeg -version | head -1 | cut -d' ' -f1-3)"
docker logs video-mcp-smoke 2>&1 | head -3
docker rm -f video-mcp-smoke > /dev/null
```

`$W/pygate.sh`:

```bash
#!/usr/bin/env bash
# The mutation gate's own tests, the workflow contract tests and release readiness's (which imports
# the scope roster), with WSL's python3. CI's web-mutation job runs the first two files.
set -uo pipefail
cd /mnt/d/Aura || exit 1
PYTHONPATH=scripts python3 -m unittest scripts/critical_mutation_gate_test.py scripts/mutation_workflow_test.py \
  scripts/release_readiness_gate_test.py 2>&1 | grep -E "^(FAIL|ERROR):|Error:|^Ran |^OK|^FAILED" | head -40
```

`$W/build.sh`:

```bash
#!/usr/bin/env bash
# Builds the web app into internal/webui/dist (vite.config.ts outDir), the bytes the aura image
# embeds, and prints how many dist files changed.
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

---

# B1 — the edit half

### Task 1: The sidecar's skeleton and its settings

The project is laid out the way web/ is, with web's strict compiler options. Two choices are easy to undo by accident:
- **The install is the sidecar's own.** `npm` runs only in `services/video-mcp/`, never in `web/` (Global Constraints).
- **The unit tier names the render-only modules now.** `vitest.config.ts` lists them (`RENDER_TIER`, `IN_PAGE`) before Tasks 12, 15 and 17 create them. That way no intermediate run counts a module only a browser or ffmpeg can execute against the unit floor. `drift.ts` (Task 19) is in its coverage `include` for the same reason.

B1's `package.json` carries only what B1 runs. Playwright arrives in Task 17, esbuild in Task 19, Stryker in Task 21.

**Files:**
- Create: `services/video-mcp/package.json`, `services/video-mcp/package-lock.json` (written by npm), `services/video-mcp/tsconfig.json`, `services/video-mcp/vitest.config.ts`, `services/video-mcp/.gitignore`
- Create: `services/video-mcp/src/config.ts`
- Test: `services/video-mcp/test/unit/config.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces (`src/config.ts`):
  - `PORT = 8097`, `SCOPE = 'mcp:tools'`, `MCP_PATH = '/mcp'`, `METADATA_PATH = '/.well-known/oauth-protected-resource/mcp'`;
  - `interface Config { issuer: string; jwksUrl: string; resources: readonly string[]; internalApiUrl: string }`;
  - `configFromEnv(env: NodeJS.ProcessEnv): Config`.
- Produces (`vitest.config.ts`): `RENDER_TIER` and `IN_PAGE` (path lists). `vitest.integration.config.ts` imports them in Task 20.

- [ ] **Step 1: Write the project files**

`services/video-mcp/package.json`:

```json
{
  "name": "aura-video-mcp",
  "private": true,
  "version": "0.0.0",
  "description": "Aura's video sidecar: Studio projects edited and rendered over MCP",
  "type": "module",
  "engines": {
    "node": ">=24.16.0 <25"
  },
  "scripts": {
    "typecheck": "tsc --noEmit -p tsconfig.json",
    "test": "vitest run --coverage"
  },
  "dependencies": {
    "@modelcontextprotocol/sdk": "1.31.0",
    "jose": "6.2.12",
    "zod": "4.6.5"
  },
  "devDependencies": {
    "@types/express": "5.0.6",
    "@types/node": "24.19.1",
    "@vitest/coverage-v8": "5.0.3",
    "ajv": "8.20.0",
    "typescript": "7.0.2",
    "vitest": "5.0.3"
  }
}
```

`services/video-mcp/tsconfig.json`: web's compiler options plus `allowImportingTsExtensions`, which `node build.ts` needs in Task 19 because Node's type stripping imports `./drift.ts` by its extension:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2023", "DOM", "DOM.Iterable"],
    "types": ["node"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "resolveJsonModule": true,
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "noUncheckedSideEffectImports": true,
    "noUncheckedIndexedAccess": true,
    "exactOptionalPropertyTypes": true,
    "verbatimModuleSyntax": true,
    "isolatedModules": true,
    "skipLibCheck": true,
    "noEmit": true,
    "allowImportingTsExtensions": true
  },
  "include": [
    "src",
    "page",
    "test",
    "types",
    "*.ts"
  ]
}
```

`services/video-mcp/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config';

/** The modules only a real browser and ffmpeg can run: the render tier measures them
 *  (vitest.integration.config.ts), each tier against its own 85 % floor, never one average. */
export const RENDER_TIER = ['src/jobs/browser.ts', 'src/jobs/outputProbe.ts', 'src/jobs/transcode.ts'];
/** Functions page.evaluate runs inside the browser (src/jobs/inPage.ts): no Node tier sees them run. */
export const IN_PAGE = ['src/jobs/inPage.ts'];

// The unit tier: Node only, no ffmpeg, no browser.
export default defineConfig({
  test: {
    environment: 'node',
    include: ['test/unit/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.ts', 'drift.ts'],
      // main.ts is the process entry (env -> listen), the shape create-aura excludes bin.ts for.
      exclude: ['src/main.ts', ...RENDER_TIER, ...IN_PAGE],
      thresholds: { statements: 85, branches: 85, functions: 85, lines: 85 },
    },
  },
});
```

`services/video-mcp/.gitignore`:

```text
# Build and test byproducts (local only): the bundles build.ts writes, vitest's coverage, and
# Stryker's sandbox and report.
dist/
coverage/
.stryker-tmp/
reports/
```

- [ ] **Step 2: Install**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc-npm.sh install`

Expected: `added 154 packages` (measured 2026-10-02; a later day may resolve a different transitive count) and `NPM_RC=0`. `services/video-mcp/package-lock.json` exists, and `git -C /d/Aura status --porcelain services/video-mcp` lists no `node_modules`.

- [ ] **Step 3: Write the failing test**

`services/video-mcp/test/unit/config.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { configFromEnv } from '../../src/config';

describe('the sidecar’s settings', () => {
  it('default to the Compose deployment: loopback name first, then the Compose name', () => {
    expect(configFromEnv({})).toEqual({
      issuer: 'http://127.0.0.1:9080',
      jwksUrl: 'http://aura:9080/oauth/jwks',
      resources: ['http://127.0.0.1:8097/mcp', 'http://aura-video-mcp:8097/mcp'],
      internalApiUrl: 'http://aura:9080/internal/video',
    });
  });

  it('take each AURA_VIDEO_MCP_* value, trimmed, without a trailing slash where one would double', () => {
    expect(
      configFromEnv({
        AURA_VIDEO_MCP_OAUTH_ISSUER: ' https://aura.example/ ',
        AURA_VIDEO_MCP_OAUTH_JWKS_URL: 'http://aura:9080/oauth/jwks',
        AURA_VIDEO_MCP_OAUTH_RESOURCE: 'https://video.example/mcp, ,http://aura-video-mcp:8097/mcp',
        AURA_VIDEO_MCP_INTERNAL_API_URL: 'http://aura:9080/internal/video/',
      }),
    ).toEqual({
      issuer: 'https://aura.example',
      jwksUrl: 'http://aura:9080/oauth/jwks',
      resources: ['https://video.example/mcp', 'http://aura-video-mcp:8097/mcp'],
      internalApiUrl: 'http://aura:9080/internal/video',
    });
  });

  it('treat a blank value as unset rather than as an empty address', () => {
    expect(configFromEnv({ AURA_VIDEO_MCP_OAUTH_ISSUER: '  ' }).issuer).toBe('http://127.0.0.1:9080');
  });
});
```

- [ ] **Step 4: Run it to see it fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/config.test.ts`

Expected:
```text
 FAIL  test/unit/config.test.ts [ test/unit/config.test.ts ]
Error: Cannot find module '../../src/config' imported from /mnt/d/Aura/services/video-mcp/test/unit/config.test.ts
 Test Files  1 failed (1)
VITEST_RC=1
```

- [ ] **Step 5: Write the settings**

`services/video-mcp/src/config.ts`:

```ts
// config.ts — the sidecar's deployment settings, read once from the environment. Limits are not
// here: they are documented constants beside the code they bound (spec §Environment variables).

/** The port the sidecar listens on inside its container; Compose publishes it on loopback. */
export const PORT = 8097;
/** The one scope Aura's authorization server grants an MCP client (internal/mcp AuraOAuthToolsScope). */
export const SCOPE = 'mcp:tools';
/** Where the MCP endpoint and its protected-resource metadata live. */
export const MCP_PATH = '/mcp';
export const METADATA_PATH = '/.well-known/oauth-protected-resource/mcp';

export interface Config {
  /** Aura's authorization server, exactly as its tokens name it in `iss`. */
  readonly issuer: string;
  /** Where its signing keys are read: the Compose name, which a container can reach. */
  readonly jwksUrl: string;
  /** Every name a token may carry in `aud`, canonical first. The canonical one is advertised, and
   *  an MCP client treats it as the server's address, so it must be the URL a host client dials;
   *  the Compose name is the one Aura's own grant is pinned to (cmd/aura/mcp_first_party_grants.go
   *  ResourceURL: server.URL). The arcadedb-mcp measurement of 2026-09-02 behind both halves is in
   *  compose.yaml beside MCP_OAUTH_RESOURCE. */
  readonly resources: readonly string[];
  /** Aura's internal API for this sidecar, on the Compose network only. */
  readonly internalApiUrl: string;
}

function setting(env: NodeJS.ProcessEnv, name: string, fallback: string): string {
  const value = env[name]?.trim();
  return value === undefined || value === '' ? fallback : value;
}

export function configFromEnv(env: NodeJS.ProcessEnv): Config {
  const resources = setting(
    env,
    'AURA_VIDEO_MCP_OAUTH_RESOURCE',
    `http://127.0.0.1:${PORT}${MCP_PATH},http://aura-video-mcp:${PORT}${MCP_PATH}`,
  )
    .split(',')
    .map((name) => name.trim())
    .filter((name) => name !== '');
  return {
    issuer: setting(env, 'AURA_VIDEO_MCP_OAUTH_ISSUER', 'http://127.0.0.1:9080').replace(/\/+$/, ''),
    jwksUrl: setting(env, 'AURA_VIDEO_MCP_OAUTH_JWKS_URL', 'http://aura:9080/oauth/jwks'),
    resources,
    internalApiUrl: setting(
      env,
      'AURA_VIDEO_MCP_INTERNAL_API_URL',
      'http://aura:9080/internal/video',
    ).replace(/\/+$/, ''),
  };
}
```

- [ ] **Step 6: Run it to see it pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/config.test.ts`

Expected: `Test Files  1 passed (1)`, `Tests  3 passed (3)`, `VITEST_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 7: Commit**

Write `$W/msg-t1.txt`:

```text
feat(video-mcp): lay out the video sidecar and read its settings

services/video-mcp is Aura's video sidecar (spec 2026-09-30, Plan B).
This is its skeleton: web's strict compiler options, a unit tier with
an 85 % floor on every metric, and the deployment settings read once
from AURA_VIDEO_MCP_*. The unit tier names now the modules only a
browser or ffmpeg can run, so no run ever counts them against it.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t1.txt services/video-mcp/package.json services/video-mcp/package-lock.json services/video-mcp/tsconfig.json services/video-mcp/vitest.config.ts services/video-mcp/.gitignore services/video-mcp/src/config.ts services/video-mcp/test/unit/config.test.ts`

Expected: `COMMIT_RC=0` and the new subject on `git log --oneline -1`.

---

### Task 2: The bearer, verified as `cmd/arcadedb-mcp/auth.go` verifies it

**Review correction R1.** The one-minute margin below is for a short authenticated
HTTP request, not the whole presign/PUT/finalize sequence. Credential acquisition must
also check cancellation even when a current token is available. Task 17 owns one claim
deadline, and passes only its remaining time to `Credentials.next`.

A token is accepted only when all of these hold:
- it is EdDSA-signed by a key in Aura's JWKS;
- its `iss` is Aura's issuer;
- its `aud` holds one of the sidecar's resource names;
- it carries `sub`, `scope` and `exp`.

Re-reading an unknown `kid` is `createRemoteJWKSet`'s own behaviour (jose 6.2.12). An unreachable JWKS is a server error, not a bad token. It is logged once per minute per reason, as arcadedb-mcp's `logJWKSFailure` does. `callerOf` gives every tool `{identity, token, expiresAt}`.

`Credentials` answers the spec's §"Amended 2026-10-02" point 2, a render that outlives its caller's token. It keeps the freshest token each identity has shown and offers none with less than a minute left. `next` waits for the identity's next call to bring a fresh one, and gives up after the time its job allows. `http.ts` (Task 7) remembers every call's bearer before its tool runs, so any call of that identity ends the wait. Task 17's job is the one that waits, and only to write: everything it reads was taken when it started (Q1).

**Files:**
- Create: `services/video-mcp/src/log.ts`, `services/video-mcp/src/auth.ts`, `services/video-mcp/src/credentials.ts`
- Test: `services/video-mcp/test/support/tokens.ts`, `services/video-mcp/test/unit/auth.test.ts`, `services/video-mcp/test/unit/credentials.test.ts`

**Interfaces:**
- Consumes: `Config` (Task 1).
- Produces:
  - `log(level: 'info' | 'warn' | 'error', message: string, fields?: LogFields): void`: one JSON line on stdout, `{time, level, message, …fields}`;
  - `interface Caller { identity: string; token: string; expiresAt: number }` (seconds since the epoch);
  - `tokenVerifier(config: Config, keys?: JWTVerifyGetKey, now?: () => number): OAuthTokenVerifier`;
  - `callerOf(info: AuthInfo | undefined): Caller`, which throws when a tool runs without a verified caller;
  - `class Credentials(now?: () => number)` with `remember(caller: Caller): void`, `current(identity: string): string | undefined` and `next(identity: string, ms: number, signal: AbortSignal): Promise<string | undefined>`. `next` resolves `undefined` when no call brings a fresh token within `ms`, and rejects with the signal's reason when its job is stopped.
- Test support (`test/support/tokens.ts`):
  - `IDENTITY`, `OTHER_IDENTITY`, `RESOURCES`, `config(over?)`;
  - `signingKey(kid)`, `localKeys(...keys)`;
  - `token(key, claims?)`, where `Claims` can omit any claim;
  - `jwksServer(initial)`.

- [ ] **Step 1: Write the failing tests and their support**

`services/video-mcp/test/support/tokens.ts`:

```ts
// Aura's MCP tokens, minted the way Authula signs them (EdDSA, `kid` on the header) with claims
// mcpTokenClaims writes (internal/webauth/mcp_oauth_server.go): iss, aud, scope, client_id, sub.
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createLocalJWKSet, exportJWK, generateKeyPair, SignJWT, type JWK, type JWTPayload } from 'jose';
import type { Config } from '../../src/config';

const ISSUER = 'http://127.0.0.1:9080';
export const IDENTITY = '0b6c9f5e-4f61-4f5c-9a43-6a2d7f8a1c11';
export const OTHER_IDENTITY = '7d1e2a33-5b8c-4e0f-8a71-3c9d0e4f5a22';
export const RESOURCES = ['http://127.0.0.1:8097/mcp', 'http://aura-video-mcp:8097/mcp'];

export function config(over: Partial<Config> = {}): Config {
  return {
    issuer: ISSUER,
    jwksUrl: 'http://aura:9080/oauth/jwks',
    resources: RESOURCES,
    internalApiUrl: 'http://aura:9080/internal/video',
    ...over,
  };
}

export interface SigningKey {
  readonly kid: string;
  readonly privateKey: CryptoKey;
  readonly jwk: JWK;
}

export async function signingKey(kid: string): Promise<SigningKey> {
  const { privateKey, publicKey } = await generateKeyPair('EdDSA', { extractable: true });
  return { kid, privateKey, jwk: { ...(await exportJWK(publicKey)), kid, alg: 'EdDSA', use: 'sig' } };
}

export function localKeys(...keys: SigningKey[]) {
  return createLocalJWKSet({ keys: keys.map((key) => key.jwk) });
}

export interface Claims {
  readonly sub?: string;
  readonly aud?: string | string[];
  readonly iss?: string;
  readonly scope?: string;
  /** Seconds from now; negative is already expired. */
  readonly expiresIn?: number;
  /** Claims left out of the token altogether. */
  readonly omit?: readonly ('sub' | 'scope')[];
}

export async function token(key: SigningKey, claims: Claims = {}): Promise<string> {
  const payload: JWTPayload = { client_id: 'aura-mcp' };
  if (!claims.omit?.includes('scope')) payload.scope = claims.scope ?? 'mcp:tools';
  const jwt = new SignJWT(payload)
    .setProtectedHeader({ alg: 'EdDSA', kid: key.kid })
    .setIssuer(claims.iss ?? ISSUER)
    .setAudience(claims.aud ?? RESOURCES[0] ?? '')
    .setIssuedAt()
    .setExpirationTime(Math.floor(Date.now() / 1000) + (claims.expiresIn ?? 900));
  if (!claims.omit?.includes('sub')) jwt.setSubject(claims.sub ?? IDENTITY);
  return jwt.sign(key.privateKey);
}

/** A JWKS endpoint whose key set the test swaps, counting how often it is read. */
export async function jwksServer(initial: SigningKey[]): Promise<{
  readonly url: string;
  readonly reads: () => number;
  publish(keys: SigningKey[]): void;
  close(): Promise<void>;
}> {
  let keys = initial;
  let reads = 0;
  const server: Server = createServer((_request, response) => {
    reads += 1;
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ keys: keys.map((key) => key.jwk) }));
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  return {
    url: `http://127.0.0.1:${String((server.address() as AddressInfo).port)}/oauth/jwks`,
    reads: () => reads,
    publish(next) {
      keys = next;
    },
    close: () => new Promise((resolve) => server.close(() => resolve())),
  };
}
```

`services/video-mcp/test/unit/auth.test.ts`:

```ts
import { InvalidTokenError, ServerError } from '@modelcontextprotocol/sdk/server/auth/errors.js';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { callerOf, tokenVerifier } from '../../src/auth';
import {
  config,
  IDENTITY,
  jwksServer,
  localKeys,
  RESOURCES,
  signingKey,
  token,
  type SigningKey,
} from '../support/tokens';

let key: SigningKey;
let stranger: SigningKey;

beforeAll(async () => {
  key = await signingKey('k1');
  stranger = await signingKey('k1');
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('a bearer Aura issued for this server', () => {
  it('is accepted under either of its names, and names the identity by its subject', async () => {
    const verifier = tokenVerifier(config(), localKeys(key));
    for (const aud of RESOURCES) {
      const info = await verifier.verifyAccessToken(await token(key, { aud }));
      expect(info.extra).toEqual({ identity: IDENTITY });
      expect(info.scopes).toEqual(['mcp:tools']);
      expect(info.clientId).toBe('aura-mcp');
      expect(info.expiresAt).toBeGreaterThan(Date.now() / 1000);
      expect(callerOf(info)).toEqual({
        identity: IDENTITY,
        token: info.token,
        expiresAt: info.expiresAt,
      });
    }
  });

  it('is accepted when it names several audiences and one of them is this server', async () => {
    const verifier = tokenVerifier(config(), localKeys(key));
    const info = await verifier.verifyAccessToken(
      await token(key, { aud: ['http://aura-pim-mcp:8080', RESOURCES[1] ?? ''] }),
    );
    expect(info.extra).toEqual({ identity: IDENTITY });
  });
});

describe('a bearer this server refuses as invalid', () => {
  const cases: [string, () => Promise<string>][] = [
    ['another issuer', () => token(key, { iss: 'http://evil.example' })],
    ['another server', () => token(key, { aud: 'http://aura-arcadedb-mcp:8096/mcp' })],
    ['an expired one', () => token(key, { expiresIn: -60 })],
    ['one with no subject', () => token(key, { omit: ['sub'] })],
    ['one with no scope claim', () => token(key, { omit: ['scope'] })],
    ['one signed by a key Aura never published', () => token(stranger)],
    ['not a JWT at all', () => Promise.resolve('not-a-jwt')],
  ];
  for (const [name, mint] of cases) {
    it(`refuses ${name}`, async () => {
      const verifier = tokenVerifier(config(), localKeys(key));
      await expect(verifier.verifyAccessToken(await mint())).rejects.toBeInstanceOf(
        InvalidTokenError,
      );
    });
  }

  it('refuses a token whose subject is blank', async () => {
    const verifier = tokenVerifier(config(), localKeys(key));
    await expect(verifier.verifyAccessToken(await token(key, { sub: '   ' }))).rejects.toThrow(
      'bearer token has no subject',
    );
  });
});

describe("Aura's signing keys", () => {
  it('are read again when a token names a key published after the last read', async () => {
    vi.useFakeTimers({ toFake: ['Date'] });
    const jwks = await jwksServer([key]);
    try {
      const verifier = tokenVerifier(config({ jwksUrl: jwks.url }));
      await verifier.verifyAccessToken(await token(key));
      const rotated = await signingKey('k2');
      jwks.publish([key, rotated]);
      // jose's cooldown: within 30 s of a read an unknown key is refused without another read.
      await expect(verifier.verifyAccessToken(await token(rotated))).rejects.toBeInstanceOf(
        InvalidTokenError,
      );
      expect(jwks.reads()).toBe(1);
      vi.setSystemTime(Date.now() + 31_000);
      const info = await verifier.verifyAccessToken(await token(rotated));
      expect(info.extra).toEqual({ identity: IDENTITY });
      expect(jwks.reads()).toBe(2);
    } finally {
      await jwks.close();
    }
  });

  it('that cannot be read are an outage: a server error, said once a minute', async () => {
    const lines: string[] = [];
    vi.spyOn(process.stdout, 'write').mockImplementation((line) => {
      lines.push(String(line));
      return true;
    });
    let clock = 1_000_000;
    const verifier = tokenVerifier(
      config({ jwksUrl: 'http://127.0.0.1:9/oauth/jwks' }),
      () => Promise.reject(new TypeError('fetch failed')),
      () => clock,
    );
    const bearer = await token(key);
    await expect(verifier.verifyAccessToken(bearer)).rejects.toBeInstanceOf(ServerError);
    await expect(verifier.verifyAccessToken(bearer)).rejects.toBeInstanceOf(ServerError);
    expect(lines).toHaveLength(1);
    expect(JSON.parse(lines[0] ?? '{}')).toMatchObject({
      level: 'error',
      jwks_url: 'http://127.0.0.1:9/oauth/jwks',
      reason: 'fetch failed',
    });
    expect(lines[0]).not.toContain(bearer);
    clock += 60_000;
    await expect(verifier.verifyAccessToken(bearer)).rejects.toBeInstanceOf(ServerError);
    expect(lines).toHaveLength(2);
  });
});

describe('callerOf', () => {
  it('is loud about a tool reached without a verified token', () => {
    expect(() => callerOf(undefined)).toThrow('without a verified caller');
    expect(() =>
      callerOf({ token: 't', clientId: '', scopes: [], extra: { identity: IDENTITY } }),
    ).toThrow('without a verified caller');
  });
});
```

`services/video-mcp/test/unit/credentials.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { Credentials } from '../../src/credentials';
import { IDENTITY, OTHER_IDENTITY } from '../support/tokens';

describe('the freshest credential of each identity', () => {
  it('is the one that expires last, whatever order the calls arrived in', () => {
    const credentials = new Credentials(() => 1000);
    credentials.remember({ identity: IDENTITY, token: 'later', expiresAt: 3000 });
    credentials.remember({ identity: IDENTITY, token: 'earlier', expiresAt: 2000 });
    expect(credentials.current(IDENTITY)).toBe('later');
  });

  it('is never another identity’s', () => {
    const credentials = new Credentials(() => 1000);
    credentials.remember({ identity: OTHER_IDENTITY, token: 'theirs', expiresAt: 3000 });
    expect(credentials.current(IDENTITY)).toBeUndefined();
  });

  it('must outlive the next call by a minute', () => {
    let now = 1000;
    const credentials = new Credentials(() => now);
    credentials.remember({ identity: IDENTITY, token: 't', expiresAt: 1061 });
    expect(credentials.current(IDENTITY)).toBe('t');
    now = 1001.5;
    expect(credentials.current(IDENTITY)).toBeUndefined();
  });

  it('forgets an expired one when another call arrives', () => {
    let now = 1000;
    const credentials = new Credentials(() => now);
    credentials.remember({ identity: IDENTITY, token: 'old', expiresAt: 1100 });
    now = 2000;
    credentials.remember({ identity: OTHER_IDENTITY, token: 'x', expiresAt: 3000 });
    credentials.remember({ identity: IDENTITY, token: 'new', expiresAt: 1500 });
    expect(credentials.current(IDENTITY)).toBeUndefined();
    credentials.remember({ identity: IDENTITY, token: 'fresh', expiresAt: 2900 });
    expect(credentials.current(IDENTITY)).toBe('fresh');
  });
});

describe('waiting for the next call of an identity', () => {
  const never = new AbortController().signal;

  it('is no wait when a fresh credential is already there', async () => {
    const credentials = new Credentials(() => 1000);
    credentials.remember({ identity: IDENTITY, token: 'here', expiresAt: 1900 });
    await expect(credentials.next(IDENTITY, 50, never)).resolves.toBe('here');
  });

  it('ends with the first call that brings a fresh one, and not with another identity’s or a stale one', async () => {
    const credentials = new Credentials(() => 1000);
    const waiting = [credentials.next(IDENTITY, 5_000, never), credentials.next(IDENTITY, 5_000, never)];
    credentials.remember({ identity: OTHER_IDENTITY, token: 'theirs', expiresAt: 1900 });
    credentials.remember({ identity: IDENTITY, token: 'too short', expiresAt: 1030 });
    credentials.remember({ identity: IDENTITY, token: 'fresh', expiresAt: 1900 });
    await expect(Promise.all(waiting)).resolves.toEqual(['fresh', 'fresh']);
  });

  it('gives up after its time with nothing, so the caller can say why', async () => {
    const credentials = new Credentials(() => 1000);
    await expect(credentials.next(IDENTITY, 20, never)).resolves.toBeUndefined();
    credentials.remember({ identity: IDENTITY, token: 'late', expiresAt: 1900 });
    expect(credentials.current(IDENTITY)).toBe('late');
  });

  it('stops at once with the reason its job was stopped for', async () => {
    const credentials = new Credentials(() => 1000);
    const job = new AbortController();
    const waiting = credentials.next(IDENTITY, 5_000, job.signal);
    job.abort(new Error('cancelled'));
    await expect(waiting).rejects.toThrow('cancelled');
    await expect(credentials.next(IDENTITY, 5_000, job.signal)).rejects.toThrow('cancelled');
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/auth.test.ts test/unit/credentials.test.ts`

Expected: both files `FAIL`, with `Error: Cannot find module '../../src/auth' imported from …/test/unit/auth.test.ts` and `Error: Cannot find module '../../src/credentials' imported from …/test/unit/credentials.test.ts`; `Test Files  2 failed (2)`, `VITEST_RC=1`.

- [ ] **Step 3: Write the log, the verifier and the credentials**

`services/video-mcp/src/log.ts`:

```ts
// log.ts — one JSON object per line on stdout, which `docker logs` keeps. Callers pass ids, names,
// sizes and reasons; a bearer token or a signed URL never reaches a field (their tests say so).

export type LogFields = Readonly<Record<string, string | number | boolean | undefined>>;

export function log(level: 'info' | 'warn' | 'error', message: string, fields: LogFields = {}): void {
  process.stdout.write(
    `${JSON.stringify({ time: new Date().toISOString(), level, message, ...fields })}\n`,
  );
}
```

`services/video-mcp/src/auth.ts`:

```ts
// auth.ts — Aura's bearer, verified the way cmd/arcadedb-mcp/auth.go verifies it: the issuer,
// the audience (any of this server's names), the expiry, the `mcp:tools` scope, and the signature
// against Aura's JWKS, read again once when a token names a key the cached set does not hold.
// jose does that last part itself: createRemoteJWKSet reloads on an unknown `kid` outside a 30 s
// cooldown (jose 6.2.12 dist/webapi/jwks/remote.js), and its audience check passes when ANY
// accepted name matches (lib/jwt_claims_set.js checkAudiencePresence) — the semantics
// arcadedb-mcp had to write by hand because its library ANDs repeated audiences.

import { InvalidTokenError, ServerError } from '@modelcontextprotocol/sdk/server/auth/errors.js';
import type { OAuthTokenVerifier } from '@modelcontextprotocol/sdk/server/auth/provider.js';
import type { AuthInfo } from '@modelcontextprotocol/sdk/server/auth/types.js';
import { createRemoteJWKSet, jwtVerify, type JWTVerifyGetKey } from 'jose';
import type { Config } from './config';
import { log } from './log';

/** arcadedb-mcp's arcadeJWKSCacheTTL: a rotated key is picked up within five minutes anyway. */
const KEYS_MAX_AGE_MS = 5 * 60_000;
/** An unchanged JWKS failure is said again after a minute, never once per request. */
const JWKS_FAILURE_RESTATE_MS = 60_000;
/** Authula signs Aura's MCP tokens with Ed25519 (authula v1.46.0 plugins/jwt/services/token_service.go:53). */
const ALGORITHMS = ['EdDSA'];
/** What jose throws for a token it refuses, as opposed to keys it could not read. */
const TOKEN_ERROR_CODES = new Set([
  'ERR_JWT_EXPIRED',
  'ERR_JWT_CLAIM_VALIDATION_FAILED',
  'ERR_JWT_INVALID',
  'ERR_JWS_INVALID',
  'ERR_JWS_SIGNATURE_VERIFICATION_FAILED',
  'ERR_JWKS_NO_MATCHING_KEY',
  'ERR_JWKS_MULTIPLE_MATCHING_KEYS',
  'ERR_JOSE_ALG_NOT_ALLOWED',
  'ERR_JOSE_NOT_SUPPORTED',
]);

/** Who called. The issuer is pinned to Aura's own, whose subjects ARE identity ids, so issuer and
 *  subject name the identity by the subject alone (cmd/arcadedb-mcp/auth_issuers.go
 *  tenantIdentity, home issuer). */
export interface Caller {
  readonly identity: string;
  readonly token: string;
  /** Seconds since the epoch. */
  readonly expiresAt: number;
}

function errorCode(error: unknown): string | undefined {
  return typeof error === 'object' && error !== null && 'code' in error
    ? String(error.code)
    : undefined;
}

export function tokenVerifier(
  config: Config,
  keys: JWTVerifyGetKey = createRemoteJWKSet(new URL(config.jwksUrl), {
    cacheMaxAge: KEYS_MAX_AGE_MS,
  }),
  now: () => number = Date.now,
): OAuthTokenVerifier {
  let lastFailure: { reason: string; at: number } | undefined;
  return {
    async verifyAccessToken(token: string): Promise<AuthInfo> {
      let payload;
      try {
        ({ payload } = await jwtVerify(token, keys, {
          issuer: config.issuer,
          audience: [...config.resources],
          algorithms: ALGORITHMS,
          requiredClaims: ['sub', 'scope', 'exp'],
        }));
      } catch (error) {
        const code = errorCode(error);
        if (code !== undefined && TOKEN_ERROR_CODES.has(code)) {
          throw new InvalidTokenError(`bearer token refused (${code})`);
        }
        // Keys this server cannot read are an outage, not a bad token: a 401 would send the
        // client to sign in again, forever, against a server that cannot verify anything.
        const reason = error instanceof Error ? error.message : String(error);
        if (
          lastFailure?.reason !== reason ||
          now() - lastFailure.at >= JWKS_FAILURE_RESTATE_MS
        ) {
          lastFailure = { reason, at: now() };
          log('error', "Aura's JWKS is unreachable, so every bearer token is refused", {
            jwks_url: config.jwksUrl,
            reason,
          });
        }
        throw new ServerError("Aura's signing keys are unreachable");
      }
      lastFailure = undefined;
      const subject = typeof payload.sub === 'string' ? payload.sub.trim() : '';
      if (subject === '') throw new InvalidTokenError('bearer token has no subject');
      return {
        token,
        clientId: typeof payload.client_id === 'string' ? payload.client_id : '',
        scopes: typeof payload.scope === 'string' ? payload.scope.split(' ').filter(Boolean) : [],
        ...(payload.exp === undefined ? {} : { expiresAt: payload.exp }),
        extra: { identity: subject },
      };
    },
  };
}

/** The caller a verified request carries; requireBearerAuth has already refused anything else. */
export function callerOf(info: AuthInfo | undefined): Caller {
  const identity = info?.extra?.identity;
  if (info === undefined || typeof identity !== 'string' || info.expiresAt === undefined) {
    throw new Error('aura-video-mcp: a tool ran without a verified caller');
  }
  return { identity, token: info.token, expiresAt: info.expiresAt };
}
```

`services/video-mcp/src/credentials.ts`:

```ts
// credentials.ts — the freshest bearer each identity has shown this sidecar. A render outlives
// the call that started it, and Aura's MCP tokens live 15 minutes (Authula's JWT default,
// internal/webauth/mcp_token_plugin.go; the refresh grant answers expires_in 900): a job saves
// its analyses and its film with the newest token its caller has presented since, never with one
// minted for it. When none is fresh enough, the job waits for the caller's next call, which
// brings one (spec §"Amended 2026-10-02" 2): http.ts remembers every call's bearer before its tool
// runs, so a video_render_status call is enough.

import type { Caller } from './auth';

/** A token must outlive the call it is spent on: presign, finalize and resolve take seconds. */
const CALL_MARGIN_SECONDS = 60;

export class Credentials {
  private readonly latest = new Map<string, Caller>();
  private readonly waiting = new Map<string, Set<() => void>>();

  constructor(private readonly now: () => number = () => Date.now() / 1000) {}

  remember(caller: Caller): void {
    for (const [identity, known] of this.latest) {
      if (known.expiresAt <= this.now()) this.latest.delete(identity);
    }
    const known = this.latest.get(caller.identity);
    if (known === undefined || caller.expiresAt > known.expiresAt) {
      this.latest.set(caller.identity, caller);
    }
    for (const wake of this.waiting.get(caller.identity) ?? []) wake();
  }

  /** A bearer of `identity` that is still valid a minute from now. */
  current(identity: string): string | undefined {
    const known = this.latest.get(identity);
    return known === undefined || known.expiresAt - this.now() < CALL_MARGIN_SECONDS ? undefined : known.token;
  }

  /** The current bearer of `identity`, or the first fresh one a call brings within `ms`; undefined
   *  when none does. A stopped job stops waiting with its signal's reason. */
  async next(identity: string, ms: number, signal: AbortSignal): Promise<string | undefined> {
    signal.throwIfAborted();
    const token = this.current(identity);
    if (token !== undefined) return token;
    return new Promise((resolve, reject) => {
      const wakers = this.waiting.get(identity) ?? new Set<() => void>();
      this.waiting.set(identity, wakers);
      const settle = (): void => {
        clearTimeout(timer);
        signal.removeEventListener('abort', stopped);
        wakers.delete(wake);
        if (wakers.size === 0) this.waiting.delete(identity);
      };
      const wake = (): void => {
        const fresh = this.current(identity);
        if (fresh === undefined) return;
        settle();
        resolve(fresh);
      };
      const stopped = (): void => {
        settle();
        reject(signal.reason as Error);
      };
      const timer = setTimeout(() => {
        settle();
        resolve(undefined);
      }, ms);
      signal.addEventListener('abort', stopped, { once: true });
      wakers.add(wake);
    });
  }
}
```

- [ ] **Step 4: Run them to see them pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/auth.test.ts test/unit/credentials.test.ts`

Expected: `Test Files  2 passed (2)`, `Tests  21 passed (21)`, `VITEST_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 5: Commit**

Write `$W/msg-t2.txt`:

```text
feat(video-mcp): verify Aura's bearer and keep each identity's freshest

The sidecar accepts a token exactly as cmd/arcadedb-mcp does: EdDSA
against Aura's JWKS, Aura's issuer, one of the sidecar's own resource
names as audience, and sub, scope and exp present. An unreachable JWKS
is a server error logged once a minute, never a bad token.

Aura's access tokens last 15 minutes and a ten-minute film renders for
over twenty, so a job saves its film with the freshest token its
identity has shown. Every tool call refreshes it, and a job that finds
none with a minute left can wait for that identity's next call to
bring one (the spec's 2026-10-02 amendment).

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t2.txt services/video-mcp/src/log.ts services/video-mcp/src/auth.ts services/video-mcp/src/credentials.ts services/video-mcp/test/support/tokens.ts services/video-mcp/test/unit/auth.test.ts services/video-mcp/test/unit/credentials.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 3: The internal API: one contract, its client, and a fake that cannot disagree with it

**Review corrections R1–R3.** The client and its upload signature below are the original
baseline. Before using them, make authenticated request dispatch acquire a current bearer
and carry the job's cancellation signal; a token captured before PUT cannot be reused for
finalize or public resolve. Combine cancellation with timeouts using Node's native
[`AbortSignal.any`](https://nodejs.org/api/globals.html#static-method-abortsignalanysignals),
and preserve upload progress while waiting for credentials. Update the fake, interfaces,
ProjectStore save path and tests together. The client alone cannot assume finalize is
safe to retry: Plan C's real handler tests must prove that first.

`contract/internal-api.json` is the internal API's specification, and both sides test against it. It is a JSON Schema 2020-12 document:
- `$defs` holds every request and answer;
- `x-operations` names each operation's method, path, request schema, answer schema per status, and worked examples;
- `x-auth` is the bearer rule;
- `x-bytes-exist` is the list of statuses that mean "the bytes are there" (Q5).

Here, `test/support/fakeInternalApi.ts` validates with ajv every request the client sends and every answer it gives itself, and records a violation instead of answering. In Plan C, the Go handlers replay every example and validate their real answers against the same `$defs` (§Contract for Plan C).

The client never builds a URL to an object, and never reads an asset id out of a key (Plan A's rule): every id and every link comes from a row this API returned.

**Files:**
- Create: `services/video-mcp/contract/internal-api.json`, `services/video-mcp/src/internalApi.ts`
- Test: `services/video-mcp/test/support/contract.ts`, `services/video-mcp/test/support/fakeInternalApi.ts`, `services/video-mcp/test/unit/internalApi.test.ts`

**Interfaces:**
- Consumes: nothing from earlier tasks at run time; the tests use `test/support/tokens.ts` (Task 2).
- Produces (`src/internalApi.ts`):
  - `interface ResolvedAsset { assetId; url; fileName; mimeType; sizeBytes; expiresAt }`;
  - `interface Resolution { found: ReadonlyMap<string, ResolvedAsset>; notFound: readonly string[] }`;
  - `interface AssetRow { assetId; fileName; mimeType; sizeBytes; status }` and `interface ProjectRow { assetId; fileName; sizeBytes; createdAt }`;
  - `interface NewFile { fileName; mimeType; modality: 'video' | 'audio' | 'document'; use: 'media' | 'document'; body: Blob }` and `interface SpeechRequest { text; voice?; language? }`;
  - `interface InternalApi { resolve(token, assetIds, endpoint?: 'internal' | 'public'): Promise<Resolution>; upload(token, file: NewFile): Promise<AssetRow>; tts(token, request: SpeechRequest): Promise<AssetRow>; projects(token): Promise<readonly ProjectRow[]> }`;
  - `internalApi(baseUrl: string, fetchImpl?: typeof fetch): InternalApi`;
  - `class InternalApiError extends Error { operation; status; reason }`.

  `upload` is presign, then the PUT with the presign's headers, then finalize. `resolve` sends at most 64 ids per call. The timeouts are 30 s per call, 120 s for `tts`, and 10 minutes for the PUT.
- Test support:
  - `test/support/contract.ts`: `OPERATIONS`, `BYTES_EXIST`, and `violation(pointer, value)`, which uses ajv;
  - `test/support/fakeInternalApi.ts`: `class FakeInternalApi { url; start(); close(); trust(token, identity); seed(identity, Seed): FakeAsset; objectFailures; reads; violations; ttsFailure; … }`, which issues its own signed URLs (`signedUrl(asset, endpoint)`) and a monotonic `stamp()`.

- [ ] **Step 1: Write the contract**

`services/video-mcp/contract/internal-api.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://aura.local/internal/video/contract.json",
  "$comment": "Aura's internal API for aura-video-mcp: the one contract both sides test against. services/video-mcp builds its fake from this file (test/support/fakeInternalApi.ts) and validates every request its client sends and every answer the fake gives. Plan C's Go handlers validate their real answers against the same $defs and replay every example under x-operations. One document, valid JSON Schema 2020-12: the schemas live in $defs, and x-operations, an annotation every validator ignores, says which schema each request and answer must satisfy. Change it here, in one commit, with both sides.",
  "x-auth": "Every call carries `Authorization: Bearer <token>`: an access token from Aura's authorization server with `iss` = Aura's issuer, `aud` containing one of aura-video-mcp's resource names (AURA_VIDEO_MCP_OAUTH_RESOURCE), scope `mcp:tools`, `sub` = the identity id, unexpired. Anything else is a 401 with an `error` body. Every operation acts for that identity and no other: an asset another identity owns, and one that does not exist, are answered exactly alike.",
  "x-bytes-exist": ["accepted", "processing", "complete"],
  "x-operations": {
    "resolve": {
      "method": "POST",
      "path": "/internal/video/resolve",
      "summary": "Asset ids to signed GET links valid two hours: on the internal object-store endpoint (AURA_OBJECTSTORE_ENDPOINT) by default, for the sidecar to read; on the public one (AURA_OBJECTSTORE_PUBLIC_ENDPOINT) with endpoint public, for a caller to download a film. Answers only for assets the identity owns, not deleted, whose status is one of x-bytes-exist; every other id is in not_found, which never says why. The object store has no PresignGet today (internal/objectstore/s3.go has PresignPut only): Plan C adds it.",
      "request": "#/$defs/resolveRequest",
      "responses": { "200": "#/$defs/resolveResponse", "400": "#/$defs/error", "401": "#/$defs/error" },
      "examples": [
        {
          "name": "one owned clip and one id the identity does not own",
          "request": {
            "asset_ids": ["3f1d2c4b-5a69-4e70-8f81-92a3b4c5d6e7", "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"]
          },
          "status": 200,
          "response": {
            "assets": [
              {
                "asset_id": "3f1d2c4b-5a69-4e70-8f81-92a3b4c5d6e7",
                "url": "http://garage:3900/aura-0b6c9f5e/media/5c2e7a90-1b3d-4f6e-9a8c-7d6e5f4a3b2c.mp4?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=7200&X-Amz-Signature=0f3a",
                "file_name": "beach.mp4",
                "mime_type": "video/mp4",
                "size_bytes": 18337001,
                "expires_at": "2026-10-02T14:00:00Z"
              }
            ],
            "not_found": ["9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"]
          }
        },
        {
          "name": "a rendered film's download link, signed on the public endpoint",
          "request": { "asset_ids": ["6e5d4c3b-2a19-4807-b6a5-948372615041"], "endpoint": "public" },
          "status": 200,
          "response": {
            "assets": [
              {
                "asset_id": "6e5d4c3b-2a19-4807-b6a5-948372615041",
                "url": "http://127.0.0.1:3900/aura-0b6c9f5e/media/1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d.mp4?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=7200&X-Amz-Signature=7c1b",
                "file_name": "Aura-memory.mp4",
                "mime_type": "video/mp4",
                "size_bytes": 24876543,
                "expires_at": "2026-10-02T14:00:00Z"
              }
            ],
            "not_found": []
          }
        },
        {
          "name": "no ids at all",
          "request": { "asset_ids": [] },
          "status": 400,
          "response": { "error": "asset_ids: at least one asset id" }
        }
      ]
    },
    "presign": {
      "method": "POST",
      "path": "/internal/video/presign",
      "summary": "A new asset of the identity, with no thread, through assets.Service.Presign (handleAssetPresign's path) under the sidecar's own source kind, and a signed PUT on the internal object-store endpoint. A video may be as large as Instagram's 300 MB row; every other modality keeps Aura's own ceiling. A refused name, type or size is a 400 that says why.",
      "request": "#/$defs/presignRequest",
      "responses": { "200": "#/$defs/presignResponse", "400": "#/$defs/error", "401": "#/$defs/error" },
      "examples": [
        {
          "name": "a rendered film",
          "request": { "file_name": "reel.mp4", "mime_type": "video/mp4", "size_bytes": 24876543, "modality_hint": "video" },
          "status": 200,
          "response": {
            "asset_id": "6e5d4c3b-2a19-4807-b6a5-948372615041",
            "upload": {
              "upload_url": "http://garage:3900/aura-0b6c9f5e/media/1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d.mp4?X-Amz-Signature=9e1d",
              "method": "PUT",
              "required_headers": { "Content-Type": "video/mp4", "x-amz-meta-filename": "reel.mp4" },
              "expires_at": "2026-10-02T12:15:00Z"
            }
          }
        },
        {
          "name": "a file type the library refuses",
          "request": { "file_name": "reel.mov", "mime_type": "video/quicktime", "size_bytes": 1000, "modality_hint": "video" },
          "status": 400,
          "response": { "error": "asset type not supported: video type \".mov\"" }
        }
      ]
    },
    "finalize": {
      "method": "POST",
      "path": "/internal/video/assets/{asset_id}/finalize",
      "summary": "Accepts an upload this sidecar presigned. `media` is the editor's door (assets.Service.FinalizeMedia: accepted and left alone, no processor): films and cleaned copies. `document` is the plain finalize the Studio's saveProject calls: project versions. An asset that is not the identity's, or that this sidecar's source kind did not presign, is a 404; bytes that never arrived are a 400.",
      "request": "#/$defs/finalizeRequest",
      "responses": { "200": "#/$defs/asset", "400": "#/$defs/error", "401": "#/$defs/error", "404": "#/$defs/error" },
      "examples": [
        {
          "name": "a rendered film, accepted as media",
          "pathParameters": { "asset_id": "6e5d4c3b-2a19-4807-b6a5-948372615041" },
          "request": { "use": "media" },
          "status": 200,
          "response": {
            "asset_id": "6e5d4c3b-2a19-4807-b6a5-948372615041",
            "file_name": "reel.mp4",
            "mime_type": "video/mp4",
            "size_bytes": 24876543,
            "status": "accepted"
          }
        },
        {
          "name": "an asset the identity does not own",
          "pathParameters": { "asset_id": "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d" },
          "request": { "use": "media" },
          "status": 404,
          "response": { "error": "asset not found" }
        }
      ]
    },
    "tts": {
      "method": "POST",
      "path": "/internal/video/tts",
      "summary": "Aura's speech (internal/multimodal/tts.go) saved as an audio asset of the identity, accepted as media. `voice` and `language` are optional; omitted, Aura's configured voice speaks. One it cannot honour is a 400 that names it; a synthesizer that is not configured or does not answer is a 503.",
      "request": "#/$defs/ttsRequest",
      "responses": { "200": "#/$defs/asset", "400": "#/$defs/error", "401": "#/$defs/error", "503": "#/$defs/error" },
      "examples": [
        {
          "name": "a line of voiceover",
          "request": { "text": "Aura ricorda quello che conta.", "language": "it" },
          "status": 200,
          "response": {
            "asset_id": "2b3c4d5e-6f70-4812-9a3b-4c5d6e7f8091",
            "file_name": "speech.ogg",
            "mime_type": "audio/ogg",
            "size_bytes": 41213,
            "status": "accepted"
          }
        },
        {
          "name": "a voice the synthesizer does not have",
          "request": { "text": "ciao", "voice": "no_such_voice" },
          "status": 400,
          "response": { "error": "voice \"no_such_voice\" is not available" }
        }
      ]
    },
    "projects": {
      "method": "GET",
      "path": "/internal/video/projects",
      "summary": "The identity's saved Studio project versions, newest first, at most 200: every asset whose object key ends in objectstore.StudioProjectSuffix (\".aura-video.json\") and whose status is one of x-bytes-exist. A project saved before Plan A that the boot re-key left on a plain .json key is not listed.",
      "responses": { "200": "#/$defs/projectsResponse", "401": "#/$defs/error" },
      "examples": [
        {
          "name": "two versions of one project",
          "status": 200,
          "response": {
            "projects": [
              { "asset_id": "8c7b6a59-4837-4261-9504-a3b2c1d0e9f8", "file_name": "Aura-memory.aura-video.json", "size_bytes": 3120, "created_at": "2026-10-02T11:58:00Z" },
              { "asset_id": "1f2e3d4c-5b6a-4798-8a7b-6c5d4e3f2a1b", "file_name": "Aura-memory.aura-video.json", "size_bytes": 2894, "created_at": "2026-10-02T11:40:00Z" }
            ]
          }
        }
      ]
    }
  },
  "$defs": {
    "assetId": {
      "type": "string",
      "pattern": "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
    },
    "timestamp": { "type": "string", "pattern": "^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}(\\.\\d+)?(Z|[+-]\\d{2}:\\d{2})$" },
    "error": {
      "type": "object",
      "additionalProperties": false,
      "required": ["error"],
      "properties": { "error": { "type": "string", "minLength": 1 } }
    },
    "asset": {
      "type": "object",
      "additionalProperties": false,
      "required": ["asset_id", "file_name", "mime_type", "size_bytes", "status"],
      "properties": {
        "asset_id": { "$ref": "#/$defs/assetId" },
        "file_name": { "type": "string" },
        "mime_type": { "type": "string" },
        "size_bytes": { "type": "integer", "minimum": 0 },
        "status": { "type": "string", "minLength": 1 }
      }
    },
    "resolveRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": ["asset_ids"],
      "properties": {
        "asset_ids": { "type": "array", "minItems": 1, "maxItems": 64, "uniqueItems": true, "items": { "$ref": "#/$defs/assetId" } },
        "endpoint": { "enum": ["internal", "public"] }
      }
    },
    "resolveResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": ["assets", "not_found"],
      "properties": {
        "assets": {
          "type": "array",
          "items": {
            "type": "object",
            "additionalProperties": false,
            "required": ["asset_id", "url", "file_name", "mime_type", "size_bytes", "expires_at"],
            "properties": {
              "asset_id": { "$ref": "#/$defs/assetId" },
              "url": { "type": "string", "minLength": 1 },
              "file_name": { "type": "string" },
              "mime_type": { "type": "string" },
              "size_bytes": { "type": "integer", "minimum": 0 },
              "expires_at": { "$ref": "#/$defs/timestamp" }
            }
          }
        },
        "not_found": { "type": "array", "items": { "$ref": "#/$defs/assetId" } }
      }
    },
    "presignRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": ["file_name", "mime_type", "size_bytes", "modality_hint"],
      "properties": {
        "file_name": { "type": "string", "minLength": 1, "maxLength": 255 },
        "mime_type": { "type": "string", "minLength": 1 },
        "size_bytes": { "type": "integer", "minimum": 1 },
        "modality_hint": { "enum": ["video", "audio", "document"] }
      }
    },
    "presignResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": ["asset_id", "upload"],
      "properties": {
        "asset_id": { "$ref": "#/$defs/assetId" },
        "upload": {
          "type": "object",
          "additionalProperties": false,
          "required": ["upload_url", "method", "required_headers", "expires_at"],
          "properties": {
            "upload_url": { "type": "string", "minLength": 1 },
            "method": { "const": "PUT" },
            "required_headers": { "type": "object", "additionalProperties": { "type": "string" } },
            "expires_at": { "$ref": "#/$defs/timestamp" }
          }
        }
      }
    },
    "finalizeRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": ["use"],
      "properties": { "use": { "enum": ["media", "document"] } }
    },
    "ttsRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": ["text"],
      "properties": {
        "text": { "type": "string", "minLength": 1, "maxLength": 4000 },
        "voice": { "type": "string", "minLength": 1 },
        "language": { "type": "string", "minLength": 2 }
      }
    },
    "projectsResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": ["projects"],
      "properties": {
        "projects": {
          "type": "array",
          "maxItems": 200,
          "items": {
            "type": "object",
            "additionalProperties": false,
            "required": ["asset_id", "file_name", "size_bytes", "created_at"],
            "properties": {
              "asset_id": { "$ref": "#/$defs/assetId" },
              "file_name": { "type": "string" },
              "size_bytes": { "type": "integer", "minimum": 0 },
              "created_at": { "$ref": "#/$defs/timestamp" }
            }
          }
        }
      }
    }
  }
}
```

- [ ] **Step 2: Write the failing tests, the contract reader and the fake**

`services/video-mcp/test/support/contract.ts`:

```ts
// The internal API contract as validators: one ajv instance over contract/internal-api.json, the
// file Plan C's Go handlers validate against too.
import { Ajv2020 } from 'ajv/dist/2020.js';
import contract from '../../contract/internal-api.json' with { type: 'json' };

export interface Example {
  readonly name: string;
  readonly pathParameters?: Readonly<Record<string, string>>;
  readonly request?: unknown;
  readonly status: number;
  readonly response: unknown;
}

export interface Operation {
  readonly method: 'GET' | 'POST';
  readonly path: string;
  readonly request?: string;
  readonly responses: Readonly<Record<string, string>>;
  readonly examples: readonly Example[];
}

export type OperationName = 'resolve' | 'presign' | 'finalize' | 'tts' | 'projects';

export const OPERATIONS = contract['x-operations'] as unknown as Readonly<
  Record<OperationName, Operation>
>;
export const BYTES_EXIST: readonly string[] = contract['x-bytes-exist'];

const ajv = new Ajv2020({ allErrors: true, strict: true });
ajv.addVocabulary(['x-auth', 'x-bytes-exist', 'x-operations']);
ajv.addSchema(contract);

/** Why `value` breaks the schema at `pointer` (`#/$defs/...`), or undefined when it holds. */
export function violation(pointer: string, value: unknown): string | undefined {
  const validate = ajv.getSchema(`${contract.$id}${pointer}`);
  if (validate === undefined) throw new Error(`the contract has no schema at ${pointer}`);
  return validate(value) ? undefined : ajv.errorsText(validate.errors);
}
```

`services/video-mcp/test/support/fakeInternalApi.ts`:

```ts
// Aura's internal API and object store, faked from the contract (contract/internal-api.json):
// each route is an x-operations entry, every request body is checked against its schema (a
// refused one is answered 400 and recorded as a violation), and every answer the fake gives is
// checked against the schema of its status before it is sent (a fake that drifts throws). The
// object store answers the signed URLs the fake handed out, and nothing without a signature.
import { randomUUID } from 'node:crypto';
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { BYTES_EXIST, OPERATIONS, violation, type OperationName } from './contract';

/** Instagram's size row: the one video ceiling the contract gives the sidecar. */
const VIDEO_MAX_BYTES = 300 * 1000 * 1000;
const STUDIO_PROJECT_SUFFIX = '.aura-video.json';

export interface FakeAsset {
  readonly id: string;
  readonly identity: string;
  readonly fileName: string;
  readonly mimeType: string;
  readonly key: string;
  readonly createdAt: string;
  /** Who made it: the cockpit's own uploads are `web`; this sidecar's are `video_mcp`. */
  readonly sourceKind: 'web' | 'video_mcp';
  bytes: Buffer | undefined;
  status: string;
}

export interface Seed {
  readonly fileName: string;
  readonly mimeType: string;
  readonly bytes: Buffer;
  readonly status?: string;
  readonly createdAt?: string;
}

export interface Speech {
  readonly fileName: string;
  readonly mimeType: string;
  readonly bytes: Buffer;
}

function keyFor(fileName: string, mimeType: string): string {
  const lower = fileName.toLowerCase();
  const extension = lower.endsWith(STUDIO_PROJECT_SUFFIX)
    ? STUDIO_PROJECT_SUFFIX
    : (/\.[a-z0-9]+$/.exec(lower)?.[0] ?? '');
  const folder = /^(video|audio|image)\//.test(mimeType) ? 'media' : 'chat';
  return `${folder}/${randomUUID()}${extension}`;
}

async function bodyOf(request: IncomingMessage): Promise<Buffer> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(chunk as Buffer);
  return Buffer.concat(chunks);
}

export class FakeInternalApi {
  readonly assets = new Map<string, FakeAsset>();
  /** Requests whose body the contract refuses. */
  readonly violations: string[] = [];
  /** Every operation called, with the identity its bearer named. */
  readonly calls: { readonly operation: OperationName; readonly identity: string }[] = [];
  /** How many times each object key was read. */
  readonly reads = new Map<string, number>();
  /** Answers the object store gives instead of the bytes, by asset id. */
  readonly objectFailures = new Map<string, number>();
  /** What the next tts call answers instead of speech. */
  ttsFailure: { readonly status: number; readonly error: string } | undefined;
  speech: (text: string) => Speech = () => ({
    fileName: 'speech.wav',
    mimeType: 'audio/wav',
    bytes: Buffer.from('RIFF'),
  });

  private readonly tokens = new Map<string, string>();
  private lastStamp = 0;
  private server: Server | undefined;
  private origin = '';

  /** Aura's internal API base, as AURA_VIDEO_MCP_INTERNAL_API_URL names it. */
  get url(): string {
    return `${this.origin}/internal/video`;
  }

  /** A bearer this fake verifies as `identity`'s. */
  trust(token: string, identity: string): void {
    this.tokens.set(token, identity);
  }

  seed(identity: string, seed: Seed): FakeAsset {
    const asset: FakeAsset = {
      id: randomUUID(),
      identity,
      fileName: seed.fileName,
      mimeType: seed.mimeType,
      key: keyFor(seed.fileName, seed.mimeType),
      createdAt: seed.createdAt ?? this.stamp(),
      sourceKind: 'web',
      bytes: seed.bytes,
      status: seed.status ?? 'accepted',
    };
    this.assets.set(asset.id, asset);
    return asset;
  }

  async start(): Promise<void> {
    this.server = createServer((request, response) => {
      this.route(request, response).catch((error: unknown) => {
        response.writeHead(500).end(String(error));
      });
    });
    await new Promise<void>((resolve) => this.server?.listen(0, '127.0.0.1', resolve));
    this.origin = `http://127.0.0.1:${String((this.server.address() as AddressInfo).port)}`;
  }

  async close(): Promise<void> {
    await new Promise((resolve) => this.server?.close(resolve));
  }

  /** Postgres stamps rows in microseconds; two saves inside one millisecond must still order. */
  private stamp(): string {
    this.lastStamp = Math.max(Date.now(), this.lastStamp + 1);
    return new Date(this.lastStamp).toISOString();
  }

  /** The internal endpoint is this fake on 127.0.0.1; the public one is the same fake reached as
   *  localhost, so a test can tell which one Aura was asked to sign for. */
  private signedUrl(asset: FakeAsset, endpoint: string = 'internal'): string {
    const origin = endpoint === 'public' ? this.origin.replace('127.0.0.1', 'localhost') : this.origin;
    return `${origin}/objects/${asset.key}?X-Amz-Signature=${randomUUID()}`;
  }

  private send(
    response: ServerResponse,
    operation: OperationName,
    status: number,
    body: unknown,
  ): void {
    const pointer = OPERATIONS[operation].responses[String(status)];
    if (pointer === undefined) throw new Error(`the contract gives ${operation} no ${status}`);
    const broken = violation(pointer, body);
    if (broken !== undefined) throw new Error(`the fake answered ${operation} off contract: ${broken}`);
    response.writeHead(status, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify(body));
  }

  private async route(request: IncomingMessage, response: ServerResponse): Promise<void> {
    const url = new URL(request.url ?? '/', this.origin);
    if (url.pathname.startsWith('/objects/')) {
      await this.object(request, response, url);
      return;
    }
    for (const [name, operation] of Object.entries(OPERATIONS) as [OperationName, (typeof OPERATIONS)[OperationName]][]) {
      const pattern = new RegExp(`^${operation.path.replace('{asset_id}', '([^/]+)')}$`);
      const match = pattern.exec(url.pathname);
      if (match === null || request.method !== operation.method) continue;
      const identity = this.tokens.get(
        (request.headers.authorization ?? '').replace(/^Bearer /, ''),
      );
      if (identity === undefined) {
        this.send(response, name, 401, { error: 'bearer token refused' });
        return;
      }
      this.calls.push({ operation: name, identity });
      let body: unknown;
      if (operation.request !== undefined) {
        body = JSON.parse((await bodyOf(request)).toString() || 'null');
        const broken = violation(operation.request, body);
        if (broken !== undefined) {
          this.violations.push(`${name}: ${broken}`);
          this.send(response, name, 400, { error: broken });
          return;
        }
      }
      this.answer(name, identity, body, decodeURIComponent(match[1] ?? ''), response);
      return;
    }
    response.writeHead(404).end();
  }

  private owned(identity: string, id: string): FakeAsset | undefined {
    const asset = this.assets.get(id);
    return asset?.identity === identity ? asset : undefined;
  }

  private row(asset: FakeAsset) {
    return {
      asset_id: asset.id,
      file_name: asset.fileName,
      mime_type: asset.mimeType,
      size_bytes: asset.bytes?.length ?? 0,
      status: asset.status,
    };
  }

  private answer(
    name: OperationName,
    identity: string,
    body: unknown,
    pathId: string,
    response: ServerResponse,
  ): void {
    switch (name) {
      case 'resolve': {
        const { asset_ids: ids, endpoint } = body as { asset_ids: string[]; endpoint?: string };
        const found = ids
          .map((id) => this.owned(identity, id))
          .filter((asset): asset is FakeAsset => asset !== undefined && BYTES_EXIST.includes(asset.status));
        this.send(response, name, 200, {
          assets: found.map((asset) => ({
            asset_id: asset.id,
            url: this.signedUrl(asset, endpoint),
            file_name: asset.fileName,
            mime_type: asset.mimeType,
            size_bytes: asset.bytes?.length ?? 0,
            expires_at: new Date(Date.now() + 2 * 3600_000).toISOString(),
          })),
          not_found: ids.filter((id) => !found.some((asset) => asset.id === id)),
        });
        return;
      }
      case 'presign': {
        const file = body as { file_name: string; mime_type: string; size_bytes: number; modality_hint: string };
        if (file.modality_hint === 'video' && !/\.(mp4|webm)$/i.test(file.file_name)) {
          this.send(response, name, 400, { error: `asset type not supported: video type "${file.file_name}"` });
          return;
        }
        if (file.modality_hint === 'video' && file.size_bytes > VIDEO_MAX_BYTES) {
          this.send(response, name, 400, { error: `asset too large: video exceeds ${String(VIDEO_MAX_BYTES)} bytes` });
          return;
        }
        const asset: FakeAsset = {
          id: randomUUID(),
          identity,
          fileName: file.file_name,
          mimeType: file.mime_type,
          key: keyFor(file.file_name, file.mime_type),
          createdAt: this.stamp(),
          sourceKind: 'video_mcp',
          bytes: undefined,
          status: 'presigned',
        };
        this.assets.set(asset.id, asset);
        this.send(response, name, 200, {
          asset_id: asset.id,
          upload: {
            upload_url: this.signedUrl(asset),
            method: 'PUT',
            required_headers: { 'Content-Type': asset.mimeType, 'x-amz-meta-filename': asset.fileName },
            expires_at: new Date(Date.now() + 900_000).toISOString(),
          },
        });
        return;
      }
      case 'finalize': {
        const asset = this.owned(identity, pathId);
        if (asset === undefined || asset.sourceKind !== 'video_mcp') {
          this.send(response, name, 404, { error: 'asset not found' });
          return;
        }
        if (asset.bytes === undefined) {
          this.send(response, name, 400, { error: 'the upload never arrived' });
          return;
        }
        asset.status = (body as { use: string }).use === 'media' ? 'accepted' : 'processing';
        this.send(response, name, 200, this.row(asset));
        return;
      }
      case 'tts': {
        if (this.ttsFailure !== undefined) {
          this.send(response, name, this.ttsFailure.status, { error: this.ttsFailure.error });
          return;
        }
        const speech = this.speech((body as { text: string }).text);
        const asset = this.seed(identity, { ...speech, status: 'accepted' });
        this.send(response, name, 200, this.row(asset));
        return;
      }
      case 'projects': {
        const projects = [...this.assets.values()]
          .filter(
            (asset) =>
              asset.identity === identity &&
              asset.key.endsWith(STUDIO_PROJECT_SUFFIX) &&
              BYTES_EXIST.includes(asset.status),
          )
          .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
          .slice(0, 200);
        this.send(response, name, 200, {
          projects: projects.map((asset) => ({
            asset_id: asset.id,
            file_name: asset.fileName,
            size_bytes: asset.bytes?.length ?? 0,
            created_at: asset.createdAt,
          })),
        });
        return;
      }
    }
  }

  private async object(request: IncomingMessage, response: ServerResponse, url: URL): Promise<void> {
    const key = url.pathname.slice('/objects/'.length);
    const asset = [...this.assets.values()].find((candidate) => candidate.key === key);
    if (asset === undefined || !url.searchParams.has('X-Amz-Signature')) {
      response.writeHead(403).end();
      return;
    }
    if (request.method === 'PUT') {
      if (request.headers['content-type'] !== asset.mimeType) {
        response.writeHead(403).end('SignatureDoesNotMatch');
        return;
      }
      asset.bytes = await bodyOf(request);
      asset.status = 'uploaded';
      response.writeHead(200).end();
      return;
    }
    this.reads.set(key, (this.reads.get(key) ?? 0) + 1);
    const failure = this.objectFailures.get(asset.id);
    if (failure !== undefined || asset.bytes === undefined) {
      response.writeHead(failure ?? 404).end();
      return;
    }
    response.writeHead(200, { 'Content-Type': asset.mimeType, 'Content-Length': asset.bytes.length });
    response.end(asset.bytes);
  }
}
```

`services/video-mcp/test/unit/internalApi.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { internalApi, InternalApiError, type InternalApi } from '../../src/internalApi';
import { OPERATIONS, violation } from '../support/contract';
import { FakeInternalApi } from '../support/fakeInternalApi';
import { IDENTITY, OTHER_IDENTITY } from '../support/tokens';

let fake: FakeInternalApi;
let api: InternalApi;

beforeEach(async () => {
  fake = new FakeInternalApi();
  await fake.start();
  fake.trust('mine', IDENTITY);
  fake.trust('theirs', OTHER_IDENTITY);
  api = internalApi(fake.url);
});

afterEach(async () => {
  expect(fake.violations).toEqual([]);
  await fake.close();
});

const clip = (over: { status?: string; createdAt?: string; fileName?: string } = {}) => ({
  fileName: over.fileName ?? 'beach.mp4',
  mimeType: 'video/mp4',
  bytes: Buffer.from('mp4 bytes'),
  ...(over.status === undefined ? {} : { status: over.status }),
  ...(over.createdAt === undefined ? {} : { createdAt: over.createdAt }),
});

describe('the contract file', () => {
  it('holds every example to its own schemas', () => {
    for (const [name, operation] of Object.entries(OPERATIONS)) {
      for (const example of operation.examples) {
        const pointer = operation.responses[String(example.status)];
        expect(pointer, `${name} / ${example.name}`).toBeDefined();
        expect(violation(pointer ?? '', example.response), `${name} / ${example.name}`).toBeUndefined();
        if (example.status === 200 && operation.request !== undefined) {
          expect(violation(operation.request, example.request), `${name} / ${example.name}`).toBeUndefined();
        }
      }
    }
  });
});

describe('resolve', () => {
  it('answers signed links for the identity’s complete assets and not_found for the rest', async () => {
    const mine = fake.seed(IDENTITY, clip());
    const uploading = fake.seed(IDENTITY, clip({ status: 'presigned' }));
    const theirs = fake.seed(OTHER_IDENTITY, clip());
    const resolution = await api.resolve('mine', [mine.id, uploading.id, theirs.id]);
    expect([...resolution.found.keys()]).toEqual([mine.id]);
    expect(resolution.found.get(mine.id)).toMatchObject({
      fileName: 'beach.mp4',
      mimeType: 'video/mp4',
      sizeBytes: 9,
    });
    expect(resolution.found.get(mine.id)?.url).toContain('X-Amz-Signature=');
    expect(resolution.notFound).toEqual([uploading.id, theirs.id]);
  });

  it('asks in batches of the contract’s 64, and asks for an id once', async () => {
    const ids = Array.from({ length: 70 }, () => fake.seed(IDENTITY, clip()).id);
    const resolution = await api.resolve('mine', [...ids, ids[0] ?? '']);
    expect(resolution.found.size).toBe(70);
    expect(fake.calls.filter((call) => call.operation === 'resolve')).toHaveLength(2);
  });
});

describe('upload', () => {
  it('presigns, puts the bytes with the headers the signature covers, and finalizes', async () => {
    const film = await api.upload('mine', {
      fileName: 'reel.mp4',
      mimeType: 'video/mp4',
      modality: 'video',
      use: 'media',
      body: new Blob([Buffer.from('film')]),
    });
    expect(film).toMatchObject({ fileName: 'reel.mp4', sizeBytes: 4, status: 'accepted' });
    const stored = fake.assets.get(film.assetId);
    expect(stored?.identity).toBe(IDENTITY);
    expect(stored?.sourceKind).toBe('video_mcp');
    expect(stored?.bytes?.toString()).toBe('film');
  });

  it('finalizes a project version as a document', async () => {
    const version = await api.upload('mine', {
      fileName: 'reel.aura-video.json',
      mimeType: 'application/json',
      modality: 'document',
      use: 'document',
      body: new Blob(['{}']),
    });
    expect(version.status).toBe('processing');
    expect(fake.assets.get(version.assetId)?.key).toMatch(/^chat\/[0-9a-f-]{36}\.aura-video\.json$/);
  });

  it('names the object store when the PUT is refused', async () => {
    const refusing = internalApi(fake.url, (input, init) =>
      init?.method === 'PUT'
        ? Promise.resolve(new Response('SignatureDoesNotMatch', { status: 403 }))
        : fetch(input, init),
    );
    const refused = refusing.upload('mine', {
      fileName: 'reel.mp4',
      mimeType: 'video/mp4',
      modality: 'video',
      use: 'media',
      body: new Blob(['x']),
    });
    await expect(refused).rejects.toMatchObject({
      operation: 'upload',
      status: 403,
      reason: 'SignatureDoesNotMatch',
    });
  });

  it('says why Aura refused the file', async () => {
    const refused = api.upload('mine', {
      fileName: 'reel.mov',
      mimeType: 'video/quicktime',
      modality: 'video',
      use: 'media',
      body: new Blob(['x']),
    });
    await expect(refused).rejects.toMatchObject({
      operation: 'presign',
      status: 400,
      reason: 'asset type not supported: video type "reel.mov"',
    });
  });
});

describe('tts', () => {
  it('answers the voice Aura saved', async () => {
    fake.speech = () => ({ fileName: 'speech.ogg', mimeType: 'audio/ogg', bytes: Buffer.from('ogg') });
    const voice = await api.tts('mine', { text: 'ciao', language: 'it' });
    expect(voice).toMatchObject({ fileName: 'speech.ogg', mimeType: 'audio/ogg', status: 'accepted' });
    expect(fake.assets.get(voice.assetId)?.identity).toBe(IDENTITY);
  });

  it('carries the synthesizer’s refusal', async () => {
    fake.ttsFailure = { status: 503, error: 'speech synthesis is not configured' };
    await expect(api.tts('mine', { text: 'ciao' })).rejects.toBeInstanceOf(InternalApiError);
    await expect(api.tts('mine', { text: 'ciao' })).rejects.toThrow(
      "Aura's tts answered 503: speech synthesis is not configured",
    );
  });
});

describe('projects', () => {
  it('lists the identity’s project versions, newest first, and nothing else', async () => {
    const older = fake.seed(IDENTITY, { fileName: 'a.aura-video.json', mimeType: 'application/json', bytes: Buffer.from('{}'), createdAt: '2026-10-01T10:00:00Z' });
    const newer = fake.seed(IDENTITY, { fileName: 'a.aura-video.json', mimeType: 'application/json', bytes: Buffer.from('{}'), createdAt: '2026-10-02T10:00:00Z' });
    fake.seed(IDENTITY, { fileName: 'notes.json', mimeType: 'application/json', bytes: Buffer.from('{}') });
    fake.seed(OTHER_IDENTITY, { fileName: 'b.aura-video.json', mimeType: 'application/json', bytes: Buffer.from('{}') });
    const rows = await api.projects('mine');
    expect(rows.map((row) => row.assetId)).toEqual([newer.id, older.id]);
  });
});

describe('an answer that is not success', () => {
  it('is an InternalApiError with the status and Aura’s own reason', async () => {
    await expect(api.projects('a token nobody issued')).rejects.toMatchObject({
      operation: 'projects',
      status: 401,
      reason: 'bearer token refused',
    });
  });

  it('keeps a body that is not the contract’s as the reason', async () => {
    const broken = internalApi('http://unused', () =>
      Promise.resolve(new Response('<html>bad gateway</html>', { status: 502 })),
    );
    await expect(broken.projects('mine')).rejects.toMatchObject({
      status: 502,
      reason: '<html>bad gateway</html>',
    });
  });
});
```

- [ ] **Step 3: Run it to see it fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/internalApi.test.ts`

Expected: `FAIL`, `Error: Cannot find module '../../src/internalApi' imported from …/test/unit/internalApi.test.ts`, `VITEST_RC=1`.

- [ ] **Step 4: Write the client**

`services/video-mcp/src/internalApi.ts`:

```ts
// internalApi.ts — the client of Aura's internal API for this sidecar (contract/internal-api.json,
// the file Aura's handlers are tested against too). Every call carries a bearer of the identity it
// acts for; the sidecar holds no credential of its own and no storage key.

/** The contract's own limit on one resolve. */
const RESOLVE_BATCH = 64;
/** Aura's own calls answer in milliseconds; a stuck one must not hold an edit past Aura's 60 s
 *  MCP call timeout (internal/agent/mcptools/timeout.go:13). */
const CALL_TIMEOUT_MS = 30_000;
/** Speech is synthesized before it is answered: a paragraph on the local Kokoro takes seconds. */
const TTS_TIMEOUT_MS = 120_000;
/** A 300 MB film over the Compose network, with room to spare. */
const PUT_TIMEOUT_MS = 10 * 60_000;

export type Modality = 'video' | 'audio' | 'document';
export type Endpoint = 'internal' | 'public';
/** `media` accepts a source and leaves it alone; `document` is the plain finalize a project takes. */
export type FinalizeUse = 'media' | 'document';

export interface ResolvedAsset {
  readonly assetId: string;
  /** A signed GET on the object store. A credential: it never reaches a log, a page or a result. */
  readonly url: string;
  readonly fileName: string;
  readonly mimeType: string;
  readonly sizeBytes: number;
  readonly expiresAt: string;
}

export interface Resolution {
  readonly found: ReadonlyMap<string, ResolvedAsset>;
  /** Not the identity's, not complete, or not there: the contract never says which. */
  readonly notFound: readonly string[];
}

export interface AssetRow {
  readonly assetId: string;
  readonly fileName: string;
  readonly mimeType: string;
  readonly sizeBytes: number;
  readonly status: string;
}

export interface ProjectRow {
  readonly assetId: string;
  readonly fileName: string;
  readonly sizeBytes: number;
  readonly createdAt: string;
}

export interface NewFile {
  readonly fileName: string;
  readonly mimeType: string;
  readonly modality: Modality;
  readonly use: FinalizeUse;
  readonly body: Blob;
}

export interface SpeechRequest {
  readonly text: string;
  readonly voice?: string | undefined;
  readonly language?: string | undefined;
}

export interface InternalApi {
  /** `internal` links are for this sidecar to read; `public` ones are for the caller to download
   *  (Aura signs them on its public object-store endpoint). */
  resolve(token: string, assetIds: readonly string[], endpoint?: Endpoint): Promise<Resolution>;
  /** Presign, PUT, finalize: a new asset of the token's identity. */
  upload(token: string, file: NewFile): Promise<AssetRow>;
  tts(token: string, request: SpeechRequest): Promise<AssetRow>;
  projects(token: string): Promise<readonly ProjectRow[]>;
}

/** Aura answered the operation with something other than success; `reason` is its `error`. */
export class InternalApiError extends Error {
  constructor(
    readonly operation: string,
    readonly status: number,
    readonly reason: string,
  ) {
    super(`Aura's ${operation} answered ${String(status)}: ${reason}`);
    this.name = 'InternalApiError';
  }
}

interface WireAsset {
  asset_id: string;
  file_name: string;
  mime_type: string;
  size_bytes: number;
  status: string;
}

const assetRow = (wire: WireAsset): AssetRow => ({
  assetId: wire.asset_id,
  fileName: wire.file_name,
  mimeType: wire.mime_type,
  sizeBytes: wire.size_bytes,
  status: wire.status,
});

export function internalApi(baseUrl: string, fetchImpl: typeof fetch = fetch): InternalApi {
  async function call<T>(
    operation: string,
    token: string,
    path: string,
    body?: unknown,
    timeoutMs = CALL_TIMEOUT_MS,
  ): Promise<T> {
    const response = await fetchImpl(`${baseUrl}${path}`, {
      method: body === undefined ? 'GET' : 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      signal: AbortSignal.timeout(timeoutMs),
    });
    const text = await response.text();
    if (!response.ok) {
      let reason = text.trim();
      try {
        const parsed = JSON.parse(text) as { error?: unknown };
        if (typeof parsed.error === 'string') reason = parsed.error;
      } catch {
        // Not the contract's error body (a proxy page, a crash): the raw text is the reason.
      }
      throw new InternalApiError(operation, response.status, reason);
    }
    return JSON.parse(text) as T;
  }

  return {
    async resolve(token, assetIds, endpoint = 'internal') {
      const found = new Map<string, ResolvedAsset>();
      const notFound: string[] = [];
      const unique = [...new Set(assetIds)];
      for (let at = 0; at < unique.length; at += RESOLVE_BATCH) {
        const answer = await call<{
          assets: {
            asset_id: string;
            url: string;
            file_name: string;
            mime_type: string;
            size_bytes: number;
            expires_at: string;
          }[];
          not_found: string[];
        }>('resolve', token, '/resolve', {
          asset_ids: unique.slice(at, at + RESOLVE_BATCH),
          ...(endpoint === 'public' ? { endpoint } : {}),
        });
        for (const wire of answer.assets) {
          found.set(wire.asset_id, {
            assetId: wire.asset_id,
            url: wire.url,
            fileName: wire.file_name,
            mimeType: wire.mime_type,
            sizeBytes: wire.size_bytes,
            expiresAt: wire.expires_at,
          });
        }
        notFound.push(...answer.not_found);
      }
      return { found, notFound };
    },

    async upload(token, file) {
      const presigned = await call<{
        asset_id: string;
        upload: { upload_url: string; required_headers: Record<string, string> };
      }>('presign', token, '/presign', {
        file_name: file.fileName,
        mime_type: file.mimeType,
        size_bytes: file.body.size,
        modality_hint: file.modality,
      });
      const put = await fetchImpl(presigned.upload.upload_url, {
        method: 'PUT',
        headers: presigned.upload.required_headers,
        body: file.body,
        signal: AbortSignal.timeout(PUT_TIMEOUT_MS),
      });
      if (!put.ok) {
        throw new InternalApiError('upload', put.status, (await put.text()).trim().slice(0, 200));
      }
      return assetRow(
        await call<WireAsset>(
          'finalize',
          token,
          `/assets/${encodeURIComponent(presigned.asset_id)}/finalize`,
          { use: file.use },
        ),
      );
    },

    async tts(token, request) {
      return assetRow(
        await call<WireAsset>(
          'tts',
          token,
          '/tts',
          {
            text: request.text,
            ...(request.voice === undefined ? {} : { voice: request.voice }),
            ...(request.language === undefined ? {} : { language: request.language }),
          },
          TTS_TIMEOUT_MS,
        ),
      );
    },

    async projects(token) {
      const answer = await call<{
        projects: { asset_id: string; file_name: string; size_bytes: number; created_at: string }[];
      }>('projects', token, '/projects');
      return answer.projects.map((wire) => ({
        assetId: wire.asset_id,
        fileName: wire.file_name,
        sizeBytes: wire.size_bytes,
        createdAt: wire.created_at,
      }));
    },
  };
}
```

- [ ] **Step 5: Run it to see it pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/internalApi.test.ts`

Expected: `Tests  12 passed (12)`, `VITEST_RC=0`. Among them, "holds every example to its own schemas" proves the contract is consistent with itself, and the fake's `violations` stay empty in every test.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 6: Commit**

Write `$W/msg-t3.txt`:

```text
feat(video-mcp): write the internal API's contract, its client and fake

Aura's internal API for the sidecar is specified once, as JSON Schema
2020-12 with worked examples: resolve, presign, finalize, tts and
projects, the bearer rule, and which statuses mean the bytes exist.
The sidecar's fake answers only what the contract allows and checks
every request the client sends against it; Plan C's Go handlers replay
the same examples, so the two sides cannot drift apart unnoticed.

The client never builds an object URL and never reads an asset id out
of a key: every id and link comes from a row the API returned.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t3.txt services/video-mcp/contract/internal-api.json services/video-mcp/src/internalApi.ts services/video-mcp/test/support/contract.ts services/video-mcp/test/support/fakeInternalApi.ts services/video-mcp/test/unit/internalApi.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 4: The Studio core, ready for a second consumer

The sidecar must import the Studio's pure core and nothing else. Two modules mixed that core with the cockpit's browser code:
- `projectStore.ts` held the project file's name and parser beside the cockpit's React save path and its attachment client;
- `VideoStudio_sources.ts` held `sourceEdit` beside the upload UI.

Bundled as they were, the parser pulled React and the chat client into a Node server. They split, with no change to what any function does. Re-read each file just before you edit it: other sessions edit `web/src/videoStudio`.

Two additions serve the render:
- `uncleanedSources`, the twin of the existing `unheardByDucking`, so a render can make the cleaned copies noise reduction still lacks. It folds the "does this clip sound" test, written three times in `videoflow_audio.ts`, into one `clipSound`.
- `ExportOptions.videoBitrate`. The cockpit leaves it unset and keeps VideoFlow's default.

Go keeps binding the three spellings of the project suffix (`TestStudioProjectSuffixMatchesTheCockpitAndTheIngest`), now read from `projectFile.ts`.

`StudioProjectRekey` (`internal/assets/studio_project_rekey.go`) is not changed: it implements in Go the same shape test the parser does. Only its comment's file reference moves. Its behaviour is pinned by its own tests (Plan A, Task 7).

**Files:**
- Create: `web/src/videoStudio/projectFile.ts`, `web/src/videoStudio/sourceEdit.ts`
- Modify: `web/src/videoStudio/projectStore.ts` (rewritten: 457 → 125 lines)
- Modify: `web/src/videoStudio/VideoStudio_sources.ts` (332 → 235 lines), `web/src/videoStudio/VideoStudio.tsx` (imports)
- Modify: `web/src/videoStudio/videoflow_audio.ts`, `web/src/videoStudio/videoflow.ts`
- Modify (tests): `web/src/videoStudio/__tests__/projectStore.test.ts`, `__tests__/VideoStudio_sources.test.ts`, `__tests__/videoflow_audio.test.ts`, `__tests__/videoflow_export.test.ts`
- Modify (Go): `internal/objectstore/studio_project_test.go:41`, plus comments at `internal/objectstore/asset_placement.go:97` and `internal/assets/studio_project_rekey.go:157`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `projectFile.ts`: `PROJECT_FILE_EXTENSION = 'aura-video.json'` (the spelling Go's parity test reads), `projectFileName(project: VideoProject, extension: string, name = project.name): string`, and `isProject(value: unknown): value is VideoProject`. Its shape checks stay private to the file.
  - `sourceEdit.ts`: `IMAGE_SECONDS`, `STARTING_SIZE`, `STARTING_FPS`, `interface ProbedSource`, `sourceFrom(probed: ProbedSource, assetId: string): ProjectSource`, `interface SourcePlacement`, and `sourceEdit(probed: ProbedSource, assetId: string, placement: SourcePlacement = { time: 0 }): Edit`, all exactly as they were in `VideoStudio_sources.ts`.
  - `videoflow_audio.ts`: `uncleanedSources(project: VideoProject): ProjectSource[]`.
  - `videoflow.ts`: `ExportOptions.videoBitrate?: number`, handed to `renderer.exportVideo` only when set.

- [ ] **Step 1: Point Go's parity test at the new file, and see it fail**

Apply to `internal/objectstore/studio_project_test.go` (and the two comment lines, which carry no behaviour):

```diff
diff --git a/internal/assets/studio_project_rekey.go b/internal/assets/studio_project_rekey.go
index 706af2c78..a5a90120b 100644
--- a/internal/assets/studio_project_rekey.go
+++ b/internal/assets/studio_project_rekey.go
@@ -154,7 +154,7 @@ func readAtMost(ctx context.Context, store objectstore.Store, ref objectstore.Ob
 	return io.ReadAll(io.LimitReader(body, studioProjectMaxBytes))
 }
 
-// studioProjectFile is the part of the Studio's saved shape (projectStore.ts hasProjectShape and
+// studioProjectFile is the part of the Studio's saved shape (projectFile.ts hasProjectShape and
 // referencesHold) that tells a project from any other JSON. A pointer or a slice pointer is
 // required: nil means the field is missing or null, and a field of the wrong JSON type fails the
 // decode.
diff --git a/internal/objectstore/asset_placement.go b/internal/objectstore/asset_placement.go
index b7021a741..fce589022 100644
--- a/internal/objectstore/asset_placement.go
+++ b/internal/objectstore/asset_placement.go
@@ -94,7 +94,7 @@ func AssetKey(assetID, fileName string, folder AssetFolder) string {
 }
 
 // StudioProjectSuffix ends the name of a Video Studio project file
-// (web/src/videoStudio/projectStore.ts PROJECT_FILE_EXTENSION). It is the one compound
+// (web/src/videoStudio/projectFile.ts PROJECT_FILE_EXTENSION). It is the one compound
 // extension a key keeps whole: a project is the editor's state, not a document anyone
 // searches, and the ingest sidecar skips it by path (services/ingest/source.py
 // STUDIO_PROJECT_PATTERN) because the key is all its matcher sees. Like ".pdf", it names a
diff --git a/internal/objectstore/studio_project_test.go b/internal/objectstore/studio_project_test.go
index ccdc39d4a..33df6fbbd 100644
--- a/internal/objectstore/studio_project_test.go
+++ b/internal/objectstore/studio_project_test.go
@@ -38,7 +38,7 @@ func TestStudioProjectSuffixMatchesTheCockpitAndTheIngest(t *testing.T) {
 		dot     string
 	}{
 		{
-			file:    filepath.Join("..", "..", "web", "src", "videoStudio", "projectStore.ts"),
+			file:    filepath.Join("..", "..", "web", "src", "videoStudio", "projectFile.ts"),
 			pattern: `(?m)^export const PROJECT_FILE_EXTENSION = '([^']*)';\s*$`,
 			dot:     ".",
 		},
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./internal/objectstore/`

Expected: the race run fails with `studio_project_test.go:52: read ../../web/src/videoStudio/projectFile.ts: open ../../web/src/videoStudio/projectFile.ts: no such file or directory` and `FAIL github.com/chetto1983/aura/internal/objectstore`.

- [ ] **Step 2: Move the project file's name and parser into `projectFile.ts`**

`web/src/videoStudio/projectFile.ts`:

```ts
import {
  audioTracks,
  type AudioItem,
  type AudioTrack,
  type OverlayItem,
  type OverlayTrack,
  type ProjectSource,
  type VideoItem,
  type VideoProject,
} from './project';

// projectFile.ts — the project as a file format: what its name ends in, how a project's name
// becomes a file name, and whether bytes read back are a project. Pure, because two processes read
// one format from here: the cockpit's store (projectStore.ts) and the video sidecar
// (services/video-mcp), which bundles this module into Node, where nothing may reach the DOM, a
// fetch or React.

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

/** Long enough to recognise the project, short enough that no store has to think about it. */
const NAME_MAX = 60;

/**
 * What the project is called on disk, saved or exported. Its name is operator input — a Studio
 * prompt, in the commonest case — so it is reduced to a slug rather than used: the server builds
 * its object key from the asset id, but the name also travels through headers, file cards and a
 * download attribute, and `../` in any of them is nobody's idea of a title.
 *
 * `name` is passed separately because an unnamed project reads as `videoStudio.untitled` on
 * screen and must read the same on disk: the fallback is a translated string the CALLER resolves,
 * never a word written here. A name that survives slugging as nothing at all falls back to the
 * project's own id, which is an identifier rather than prose and so needs no language.
 */
export function projectFileName(
  project: VideoProject,
  extension: string,
  name = project.name,
): string {
  const slug = name
    .normalize('NFKD')
    .replace(/[^a-zA-Z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, NAME_MAX);
  return `${slug === '' ? project.id : slug}.${extension}`;
}

type Bag = Record<string, unknown>;
const CLIP_TRANSITIONS = new Set([
  'none',
  'fade',
  'blurResolve',
  'zoom',
  'slideUp',
  'slideDown',
  'slideLeft',
  'slideRight',
  'overshootPop',
  'glitchResolve',
  'wipeReveal',
  'lightSweepReveal',
]);
const JUNCTION_TRANSITIONS = new Set([
  'none',
  'crossfade',
  'fadeBlack',
  'fadeWhite',
  'zoom',
  'blur',
]);

function bagOf(value: unknown): Bag | undefined {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as Bag)
    : undefined;
}

/**
 * A number inside the range the editor itself can produce. `typeof Infinity === 'number'`, JSON
 * reads `1e999` as exactly that, and a speed of 0 is finite but divided by: either reaches
 * `clipStarts` as NaN. The bounds are the commands' own (`setClipPresentation`,
 * `setAudioProperties`), so a file the editor wrote always loads.
 */
function between(value: unknown, min: number, max: number): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= min && value <= max;
}

function optionalBetween(value: unknown, min: number, max: number): boolean {
  return value === undefined || between(value, min, max);
}

/** Past zero: a length, a rate or a ramp that is 0 is a division waiting to happen. */
function positive(value: unknown): value is number {
  return between(value, Number.MIN_VALUE, Number.MAX_VALUE);
}

function nonNegative(value: unknown): value is number {
  return between(value, 0, Number.MAX_VALUE);
}

function optionalPositive(value: unknown): boolean {
  return value === undefined || positive(value);
}

const ANY = Number.MAX_VALUE;
const SPEED = [0.25, 4] as const;

function isSpeech(value: unknown): boolean {
  return (
    Array.isArray(value) &&
    value.every(
      (window) =>
        Array.isArray(window) &&
        window.length === 2 &&
        nonNegative(window[0]) &&
        nonNegative(window[1]) &&
        window[0] <= window[1],
    )
  );
}

/** A source's pixel size: 0 × 0 is a sound's. */
function isSize(value: unknown): boolean {
  const size = bagOf(value);
  return size !== undefined && nonNegative(size.width) && nonNegative(size.height);
}

/** The project's own frame, which a render divides by. */
function isFrame(value: unknown): boolean {
  const size = bagOf(value);
  return size !== undefined && positive(size.width) && positive(size.height);
}

function isSource(value: unknown): value is ProjectSource {
  const source = bagOf(value);
  return (
    source !== undefined &&
    typeof source.id === 'string' &&
    typeof source.assetId === 'string' &&
    (source.kind === 'video' || source.kind === 'image' || source.kind === 'audio') &&
    nonNegative(source.duration) &&
    (source.hasAudio === undefined || typeof source.hasAudio === 'boolean') &&
    (source.speech === undefined || isSpeech(source.speech)) &&
    (source.denoisedAssetId === undefined || typeof source.denoisedAssetId === 'string') &&
    // No `fps`: nothing ever measured a source's frame rate — `probeVideo` does not report one —
    // and a file saved while the field existed still loads, with the number simply ignored.
    isSize(source.size)
  );
}

function isClip(value: unknown): value is VideoItem {
  const clip = bagOf(value);
  return (
    clip !== undefined &&
    typeof clip.id === 'string' &&
    typeof clip.sourceId === 'string' &&
    positive(clip.duration) &&
    nonNegative(clip.sourceStart) &&
    typeof clip.muted === 'boolean' &&
    optionalBetween(clip.volume, 0, 2) &&
    (clip.denoise === undefined || typeof clip.denoise === 'boolean') &&
    (clip.rotation === undefined || [0, 90, 180, 270].includes(clip.rotation as number)) &&
    (clip.fit === undefined || clip.fit === 'contain' || clip.fit === 'cover') &&
    (clip.flipX === undefined || typeof clip.flipX === 'boolean') &&
    (clip.flipY === undefined || typeof clip.flipY === 'boolean') &&
    optionalBetween(clip.brightness, 0, ANY) &&
    optionalBetween(clip.contrast, 0, ANY) &&
    optionalBetween(clip.saturation, 0, ANY) &&
    optionalBetween(clip.hue, -ANY, ANY) &&
    optionalBetween(clip.blur, 0, ANY) &&
    optionalBetween(clip.opacity, 0, 1) &&
    (clip.animation === undefined ||
      clip.animation === 'none' ||
      clip.animation === 'fadeIn' ||
      clip.animation === 'fadeOut') &&
    (clip.fadeIn === undefined || typeof clip.fadeIn === 'boolean') &&
    (clip.fadeOut === undefined || typeof clip.fadeOut === 'boolean') &&
    optionalBetween(clip.speed, ...SPEED) &&
    (clip.transitionIn === undefined ||
      (typeof clip.transitionIn === 'string' && CLIP_TRANSITIONS.has(clip.transitionIn))) &&
    (clip.transitionOut === undefined ||
      (typeof clip.transitionOut === 'string' && CLIP_TRANSITIONS.has(clip.transitionOut))) &&
    optionalPositive(clip.transitionInDuration) &&
    optionalPositive(clip.transitionOutDuration) &&
    (clip.junctionFromClipId === undefined || typeof clip.junctionFromClipId === 'string') &&
    (clip.junctionTransition === undefined ||
      (typeof clip.junctionTransition === 'string' &&
        JUNCTION_TRANSITIONS.has(clip.junctionTransition))) &&
    optionalPositive(clip.junctionDuration)
  );
}

function isOverlayItem(value: unknown): value is OverlayItem {
  const item = bagOf(value);
  if (item === undefined) return false;
  const anchor = bagOf(item.anchor);
  return (
    typeof item.id === 'string' &&
    (item.kind === 'text' || item.kind === 'image') &&
    nonNegative(item.duration) &&
    anchor !== undefined &&
    typeof anchor.clipId === 'string' &&
    nonNegative(anchor.offset) &&
    // `props` is VideoFlow's own untyped bag by design — what is IN it is checked where it is
    // read (Stage clamps a position, Inspector refuses a size it cannot parse). That it is a bag
    // and not an array or a string is the part this guard can answer.
    bagOf(item.props) !== undefined
  );
}

function isOverlayTrack(value: unknown): value is OverlayTrack {
  const track = bagOf(value);
  return (
    track !== undefined &&
    typeof track.id === 'string' &&
    Array.isArray(track.items) &&
    track.items.every(isOverlayItem)
  );
}

function isEnvelopePoint(value: unknown): boolean {
  const point = bagOf(value);
  return point !== undefined && nonNegative(point.time) && between(point.gain, 0, 1);
}

function isDucking(value: unknown): boolean {
  const ducking = bagOf(value);
  return (
    ducking !== undefined && between(ducking.amountDb, -24, -3) && between(ducking.ramp, 0.1, 2)
  );
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
    nonNegative(anchor.offset) &&
    nonNegative(item.sourceStart) &&
    positive(item.duration) &&
    between(item.volume, 0, 2) &&
    typeof item.muted === 'boolean' &&
    optionalBetween(item.fadeIn, 0, 5) &&
    optionalBetween(item.fadeOut, 0, 5) &&
    optionalBetween(item.speed, ...SPEED) &&
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

/**
 * Whether every id the project points with has something to point at, and at the right kind of
 * thing. A shape check cannot see this, and every way of failing it is known: a clip naming a
 * source the file does not hold reaches `videoflow.ts`, which throws an untranslated internal
 * sentence into the alert; an overlay anchored to a clip that is not there becomes a zero-length
 * ghost on a lane; a lane playing a source of the wrong kind (a clip over a sound, a sound over a
 * still) hands the renderer media it cannot play; and a sound hanging off a missing clip has no
 * project time at all. None of it belongs on the far side of the load.
 */
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

function hasProjectShape(value: unknown): value is VideoProject {
  const project = bagOf(value);
  return (
    project !== undefined &&
    typeof project.id === 'string' &&
    typeof project.name === 'string' &&
    positive(project.fps) &&
    isFrame(project.size) &&
    Array.isArray(project.sources) &&
    project.sources.every(isSource) &&
    Array.isArray(project.video) &&
    project.video.every(isClip) &&
    Array.isArray(project.overlays) &&
    project.overlays.every(isOverlayTrack) &&
    (project.audio === undefined ||
      (Array.isArray(project.audio) && project.audio.every(isAudioTrack)))
  );
}

/**
 * Whether the bytes are a project. A saved project file is EXTERNAL INPUT — it round-trips
 * through a store anyone with the asset id can overwrite — so the whole persisted shape is
 * checked, not only the containers: a lane holding a string, or a clip with no `sourceStart`,
 * reaches `clipStarts` as `NaN` and takes the timeline with it. And the shape is only half of
 * it: the ids have to point somewhere too.
 */
export function isProject(value: unknown): value is VideoProject {
  return hasProjectShape(value) && referencesHold(value);
}
```

`web/src/videoStudio/projectStore.ts`, whole (the parser and its shape checks left; the save, load and remember stay):

```ts
import { IDENTITY_SCOPED } from '../chat/artifacts/renderers/assetSourceContext';
import { finalizeAsset, getAsset, presignAsset } from '../chat/attachments/api';
import { isTerminalAsset, putWithProgress } from '../chat/attachments/upload';
import { assetIsGone } from './assetStatus';
import type { VideoProject } from './project';
import { isProject, PROJECT_FILE_EXTENSION, projectFileName } from './projectFile';

// projectStore.ts — the project as a file. It is a `.aura-video.json` DOCUMENT uploaded through
// the same presign the chat attachments use, which is what puts it under `chat/`: the server files
// by modality (internal/assets/service.go `folderFor`), and `media/` is where the sources live.
// No table, no route and no migration of its own — a project is bytes with an asset id.
//
// A load answers with the project AND with the sources whose bytes are gone, because a saved
// project outlives the clips it names: the library sweeps, and an asset id in a file is a claim
// about the past. Only a source that is really gone is reported gone — an expired session or a
// 500 is a different sentence, and telling an operator their file was permanently deleted
// because a proxy hiccuped is the worse of the two wrong answers.

/** The asset route a load reads through — the `useAssetSource` seam, as a value. */
export interface ProjectAssetSource {
  readonly assetUrl: (assetId: string) => string;
  readonly credentials: RequestCredentials;
}

export interface LoadedProject {
  readonly project: VideoProject;
  /** `ProjectSource.id` of every source whose asset the library no longer holds. */
  readonly missing: readonly string[];
}

const PROJECT_MIME = 'application/json';

/** Save the project and answer with the asset id it now lives at. */
export async function saveProject(project: VideoProject, name = project.name): Promise<string> {
  const file = new File(
    [JSON.stringify(project)],
    projectFileName(project, PROJECT_FILE_EXTENSION, name),
    { type: PROJECT_MIME },
  );
  const presign = await presignAsset({
    // No thread: a project belongs to the identity, the way a Studio frame does.
    thread_id: '',
    file_name: file.name,
    mime_type: PROJECT_MIME,
    size_bytes: file.size,
    modality_hint: 'document',
  });
  await putWithProgress(
    presign.upload.upload_url,
    file,
    presign.upload.required_headers,
    // The file is a few kilobytes: there is no progress worth drawing, and inventing a bar for
    // it would be a lie about how long the save takes.
    () => undefined,
  );
  const saved = await finalizeAsset(presign.asset.id);
  return saved.id;
}

/** Where the last project saved on this browser lives, so the Studio can offer it back after a
 *  reload. A listing of every saved project needs the library door, which is cycle 2's. */
const LAST_SAVED_KEY = 'aura.videoStudio.lastSavedProject';

export function rememberSavedProject(assetId: string): void {
  try {
    localStorage.setItem(LAST_SAVED_KEY, assetId);
  } catch {
    // A browser refusing storage is one where the entrance simply does not appear after a
    // reload. Explicitly silenced: it must never fail the save it followed.
  }
}

export function lastSavedProject(): string | undefined {
  try {
    return localStorage.getItem(LAST_SAVED_KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

/**
 * Whether the bytes are gone, asked of the route the renderer will really fetch. `HEAD` because
 * the answer wanted is the status and not the file; Go's ServeMux matches a `GET` pattern for
 * `HEAD` too, so this is the download route answering about itself. What the status MEANS is
 * `assetIsGone`'s, which is the same reading the editor's own fetch uses.
 */
async function bytesAreGone(assetId: string, source: ProjectAssetSource): Promise<boolean> {
  return assetIsGone(
    assetId,
    await fetch(source.assetUrl(assetId), { method: 'HEAD', credentials: source.credentials }),
  );
}

/**
 * Whether the library still holds this source. One metadata read on the happy path: a row the
 * retention sweeper marked terminal is gone even while its bytes linger.
 *
 * `getAsset` throws an Error that does not carry the status it came from, so a rejection is
 * ambiguous — 404, 401 and 500 arrive identically. The bytes route is asked to break the tie
 * rather than assuming the worst, which is what made every failure read as "deleted".
 */
async function sourceIsGone(assetId: string, source: ProjectAssetSource): Promise<boolean> {
  try {
    return isTerminalAsset(await getAsset(assetId));
  } catch {
    return await bytesAreGone(assetId, source);
  }
}

export async function loadProject(
  assetId: string,
  source: ProjectAssetSource = IDENTITY_SCOPED,
): Promise<LoadedProject> {
  const response = await fetch(source.assetUrl(assetId), { credentials: source.credentials });
  if (!response.ok)
    throw new Error(`videoStudio: the project file answered ${String(response.status)}`);
  const value: unknown = await response.json();
  if (!isProject(value)) throw new Error('videoStudio: those bytes are not a project');
  const checked = await Promise.all(
    value.sources.map(async (item) =>
      (await sourceIsGone(item.assetId, source)) ? item.id : undefined,
    ),
  );
  return { project: value, missing: checked.filter((id) => id !== undefined) };
}
```

Apply to `web/src/videoStudio/__tests__/projectStore.test.ts`:

```diff
diff --git a/web/src/videoStudio/__tests__/projectStore.test.ts b/web/src/videoStudio/__tests__/projectStore.test.ts
index 7d06d4d6c..129d2d9ae 100644
--- a/web/src/videoStudio/__tests__/projectStore.test.ts
+++ b/web/src/videoStudio/__tests__/projectStore.test.ts
@@ -2,13 +2,8 @@ import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
 import type { Asset, PresignResponse } from '../../chat/attachments/types';
 import savedStudioProject from '../../../../internal/assets/testdata/studio-project.json';
 import type { VideoProject } from '../project';
-import {
-  lastSavedProject,
-  loadProject,
-  projectFileName,
-  rememberSavedProject,
-  saveProject,
-} from '../projectStore';
+import { projectFileName } from '../projectFile';
+import { lastSavedProject, loadProject, rememberSavedProject, saveProject } from '../projectStore';
 
 // The project file is an ASSET, so the only thing worth testing here is the round trip through
 // that route and what a load says about a source whose bytes are gone. The transport itself —
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./internal/objectstore/`

Expected: no file listed under gofmt; vet silent; `ok  github.com/chetto1983/aura/internal/objectstore`; lint `0 issues.`

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/projectStore.test.ts`

Expected: `Test Files  1 passed (1)` with every test passing.

- [ ] **Step 3: Move `sourceEdit` into `sourceEdit.ts`**

Apply to `web/src/videoStudio/__tests__/VideoStudio_sources.test.ts`:

```diff
diff --git a/web/src/videoStudio/__tests__/VideoStudio_sources.test.ts b/web/src/videoStudio/__tests__/VideoStudio_sources.test.ts
index f194981a9..832b4576e 100644
--- a/web/src/videoStudio/__tests__/VideoStudio_sources.test.ts
+++ b/web/src/videoStudio/__tests__/VideoStudio_sources.test.ts
@@ -11,10 +11,10 @@ import {
   REFUSAL_UNDECODABLE,
   REFUSAL_UNDECODABLE_SOUND,
   SOURCE_ACCEPT,
-  sourceEdit,
   takesAsSource,
   uploadSource,
 } from '../VideoStudio_sources';
+import { sourceEdit } from '../sourceEdit';
 
 // The async half of the workspace, tested without a renderer: what the probe refuses, what the
 // first source is allowed to do to the project's frame, and which failures are allowed to be
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/VideoStudio_sources.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../sourceEdit' imported from …/VideoStudio_sources.test.ts`.

`web/src/videoStudio/sourceEdit.ts`:

```ts
import { addClip } from './commands';
import { addAudio } from './commands_audio';
import type { ProjectSource, VideoProject } from './project';

// sourceEdit.ts — how a probed source enters a project: the source, and a clip of its whole length
// or a sound on a free lane. Pure, so both doors share it: the Studio's own (VideoStudio_sources.ts,
// which probes in the browser) and the video sidecar (services/video-mcp, which probes with
// ffprobe in Node). The sidecar takes `sourceFrom` and `IMAGE_SECONDS` only: its projects are
// framed when they are created, never by their first clip.

/**
 * How long a still is on screen when it is added. A number this module CHOOSES rather than
 * measures — an image has no length of its own — and the item is what carries it, so a trim
 * handle or the inspector changes it like any other clip's.
 */
export const IMAGE_SECONDS = 5;

/** A frame before there is a clip to measure. A project still wearing EXACTLY this, with no
 *  source in it, is one nobody has chosen a frame for — which is the only project `sourceEdit`
 *  is allowed to re-frame. */
export const STARTING_SIZE = { width: 1920, height: 1080 };
export const STARTING_FPS = 30;

/** Any pure project-to-project function — `history.ts`'s `Edit`, restated so this module does
 *  not depend on the history to describe what it returns. */
type Edit = (project: VideoProject) => VideoProject;

/** What the probe found: what kind of source it is, the numbers it needs, and nothing about the
 *  file. A still's `duration` is 0 — the model's own convention for a source with no length. */
export interface ProbedSource {
  readonly kind: ProjectSource['kind'];
  readonly duration: number;
  readonly width: number;
  readonly height: number;
  readonly hasAudio?: boolean;
}

/**
 * Whether this project's frame is still the one nobody picked: no source in it, and the default
 * size untouched. A project built from a clip already carries that clip's frame, and a saved one
 * carries whatever it was saved with — neither is re-framed by what is added to it next.
 */
function framedByDefault(project: VideoProject): boolean {
  return (
    project.sources.length === 0 &&
    project.size.width === STARTING_SIZE.width &&
    project.size.height === STARTING_SIZE.height
  );
}

/** A probed asset as a project source, under a fresh id. */
export function sourceFrom(probed: ProbedSource, assetId: string): ProjectSource {
  return {
    id: crypto.randomUUID(),
    assetId,
    kind: probed.kind,
    duration: probed.duration,
    size: { width: probed.width, height: probed.height },
    ...(probed.hasAudio === undefined ? {} : { hasAudio: probed.hasAudio }),
  };
}

/** Where a probed SOUND goes: the project time it starts at and the name its item shows. A clip
 *  or a still needs neither — it joins the end of the video lane. */
export interface SourcePlacement {
  readonly time: number;
  readonly label?: string | undefined;
}

/**
 * The edit that puts a probed source in the project, with a clip of its whole length.
 *
 * The first source of an UNFRAMED project also sets its frame. VideoFlow does not letterbox a
 * clip that does not fit, it crops it (`fit: 'cover'`), so a portrait clip in a project still
 * wearing the 1920×1080 default would lose its sides with nothing said — and silent cropping is
 * the defect class this cycle keeps refusing. The narrowing is what keeps the cure from becoming
 * the same disease: a SECOND source never re-frames the project, and neither does the first
 * source of a project whose frame came from somewhere (a clip, a saved file). The workspace
 * watches the size across the commit and says so when it changes.
 *
 * A sound goes on the first free audio lane at `placement.time` instead, hung on the clip there,
 * and never frames anything: it has no picture to give.
 */
export function sourceEdit(
  probed: ProbedSource,
  assetId: string,
  placement: SourcePlacement = { time: 0 },
): Edit {
  return (project) => {
    const source = sourceFrom(probed, assetId);
    const withSource: VideoProject = {
      ...project,
      size: probed.kind !== 'audio' && framedByDefault(project) ? source.size : project.size,
      sources: [...project.sources, source],
    };
    if (probed.kind === 'audio') {
      return addAudio(withSource, {
        sourceId: source.id,
        time: placement.time,
        label: placement.label,
      });
    }
    return addClip(
      withSource,
      // A still lasts as long as its ITEM says: the source's own duration is zero, and `addClip`
      // refuses a clip of no length.
      { sourceId: source.id, duration: probed.kind === 'image' ? IMAGE_SECONDS : probed.duration },
    );
  };
}
```

Apply to `web/src/videoStudio/VideoStudio_sources.ts` (the moved block leaves; nothing else changes) and to `web/src/videoStudio/VideoStudio.tsx` (imports only):

```diff
diff --git a/web/src/videoStudio/VideoStudio.tsx b/web/src/videoStudio/VideoStudio.tsx
index c22d583ce..302aee325 100644
--- a/web/src/videoStudio/VideoStudio.tsx
+++ b/web/src/videoStudio/VideoStudio.tsx
@@ -19,7 +19,8 @@ import {
   type ClipJunction,
   type VideoProject,
 } from './project';
-import { projectFileName, rememberSavedProject, saveProject } from './projectStore';
+import { projectFileName } from './projectFile';
+import { rememberSavedProject, saveProject } from './projectStore';
 import { Stage } from './Stage';
 import { Timeline } from './Timeline';
 import { VideoStudioTransport } from './VideoStudioTransport';
@@ -45,13 +46,11 @@ import {
   probeAsset,
   probeSource,
   REFUSAL_MISSING_ASSET,
-  sourceEdit,
   SOURCE_ACCEPT,
   uploadSource,
-  type ProbedSource,
-  type SourcePlacement,
   type StudioOpen,
 } from './VideoStudio_sources';
+import { sourceEdit, type ProbedSource, type SourcePlacement } from './sourceEdit';
 import { Button } from '@/components/ui/button';
 import { ConfirmDialog } from '@/components/ui/confirm-dialog';
 
diff --git a/web/src/videoStudio/VideoStudio_sources.ts b/web/src/videoStudio/VideoStudio_sources.ts
index 7624f4caa..9d9270abf 100644
--- a/web/src/videoStudio/VideoStudio_sources.ts
+++ b/web/src/videoStudio/VideoStudio_sources.ts
@@ -3,9 +3,9 @@ import { putWithProgress } from '../chat/attachments/upload';
 import { probeAudio, probeVideo, type VideoInfo } from '../mediaEdit/videoMedia';
 import { assetIsGone } from './assetStatus';
 import { addClip, CommandRefusal } from './commands';
-import { addAudio } from './commands_audio';
 import { emptyProject, type ProjectSource, type VideoProject } from './project';
 import { loadProject, type LoadedProject, type ProjectAssetSource } from './projectStore';
+import { sourceEdit, STARTING_FPS, STARTING_SIZE, type ProbedSource } from './sourceEdit';
 
 // VideoStudio_sources.ts — the door a source comes in through, and the three ways a project is
 // opened. Nothing here draws anything: it is the async half of the workspace, kept out of the
@@ -53,13 +53,6 @@ export function takesAsSource(mimeType: string): boolean {
  */
 export const SOURCE_ACCEPT = `video/mp4,video/webm,${STILL_TYPES.join(',')},${AUDIO_ACCEPT}`;
 
-/**
- * How long a still is on screen when it is added. A number this module CHOOSES rather than
- * measures — an image has no length of its own — and the item is what carries it, so a trim
- * handle or the inspector changes it like any other clip's.
- */
-const IMAGE_SECONDS = 5;
-
 /** What a project is opened on. Three shapes because there are three doors: the quick editor
  *  hands over a project it built from the clip it was trimming, the Studio hands over one
  *  generated asset, and a saved project is a file to read back. */
@@ -68,26 +61,6 @@ export type StudioOpen =
   | { readonly kind: 'source'; readonly assetId: string; readonly name: string }
   | { readonly kind: 'saved'; readonly assetId: string };
 
-/** A frame before there is a clip to measure. A project still wearing EXACTLY this, with no
- *  source in it, is one nobody has chosen a frame for — which is the only project `sourceEdit`
- *  is allowed to re-frame. */
-const STARTING_SIZE = { width: 1920, height: 1080 };
-const STARTING_FPS = 30;
-
-/** Any pure project-to-project function — `history.ts`'s `Edit`, restated so this module does
- *  not depend on the history to describe what it returns. */
-type Edit = (project: VideoProject) => VideoProject;
-
-/** What the probe found: what kind of source it is, the numbers it needs, and nothing about the
- *  file. A still's `duration` is 0 — the model's own convention for a source with no length. */
-export interface ProbedSource {
-  readonly kind: ProjectSource['kind'];
-  readonly duration: number;
-  readonly width: number;
-  readonly height: number;
-  readonly hasAudio?: boolean;
-}
-
 /**
  * Read a still, or refuse it. `createImageBitmap` IS the decode — it is to an image what
  * `canDecode` is to a video track — so a file the browser cannot turn into pixels is refused at
@@ -156,76 +129,6 @@ export async function probeSource(bytes: Blob): Promise<ProbedSource> {
   };
 }
 
-/**
- * Whether this project's frame is still the one nobody picked: no source in it, and the default
- * size untouched. A project built from a clip already carries that clip's frame, and a saved one
- * carries whatever it was saved with — neither is re-framed by what is added to it next.
- */
-function framedByDefault(project: VideoProject): boolean {
-  return (
-    project.sources.length === 0 &&
-    project.size.width === STARTING_SIZE.width &&
-    project.size.height === STARTING_SIZE.height
-  );
-}
-
-/** Where a probed SOUND goes: the project time it starts at and the name its item shows. A clip
- *  or a still needs neither — it joins the end of the video lane. */
-export interface SourcePlacement {
-  readonly time: number;
-  readonly label?: string | undefined;
-}
-
-/**
- * The edit that puts a probed source in the project, with a clip of its whole length.
- *
- * The first source of an UNFRAMED project also sets its frame. VideoFlow does not letterbox a
- * clip that does not fit, it crops it (`fit: 'cover'`), so a portrait clip in a project still
- * wearing the 1920×1080 default would lose its sides with nothing said — and silent cropping is
- * the defect class this cycle keeps refusing. The narrowing is what keeps the cure from becoming
- * the same disease: a SECOND source never re-frames the project, and neither does the first
- * source of a project whose frame came from somewhere (a clip, a saved file). The workspace
- * watches the size across the commit and says so when it changes.
- *
- * A sound goes on the first free audio lane at `placement.time` instead, hung on the clip there,
- * and never frames anything: it has no picture to give.
- */
-export function sourceEdit(
-  probed: ProbedSource,
-  assetId: string,
-  placement: SourcePlacement = { time: 0 },
-): Edit {
-  const size = { width: probed.width, height: probed.height };
-  return (project) => {
-    const source: ProjectSource = {
-      id: crypto.randomUUID(),
-      assetId,
-      kind: probed.kind,
-      duration: probed.duration,
-      size,
-      ...(probed.hasAudio === undefined ? {} : { hasAudio: probed.hasAudio }),
-    };
-    const withSource: VideoProject = {
-      ...project,
-      size: probed.kind !== 'audio' && framedByDefault(project) ? size : project.size,
-      sources: [...project.sources, source],
-    };
-    if (probed.kind === 'audio') {
-      return addAudio(withSource, {
-        sourceId: source.id,
-        time: placement.time,
-        label: placement.label,
-      });
-    }
-    return addClip(
-      withSource,
-      // A still lasts as long as its ITEM says: the source's own duration is zero, and `addClip`
-      // refuses a clip of no length.
-      { sourceId: source.id, duration: probed.kind === 'image' ? IMAGE_SECONDS : probed.duration },
-    );
-  };
-}
-
 /**
  * Presign, PUT, finalize-as-media — the attachments' own path, through the editor's door — and
  * answer with the asset id. A source is accepted and left alone: the plain finalize would run the
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/VideoStudio_sources.test.ts src/videoStudio/__tests__/projectStore.test.ts`

Expected: both files pass.

- [ ] **Step 4: `uncleanedSources`, test first**

Apply to `web/src/videoStudio/__tests__/videoflow_audio.test.ts`:

```diff
diff --git a/web/src/videoStudio/__tests__/videoflow_audio.test.ts b/web/src/videoStudio/__tests__/videoflow_audio.test.ts
index c38fefd23..3947b1576 100644
--- a/web/src/videoStudio/__tests__/videoflow_audio.test.ts
+++ b/web/src/videoStudio/__tests__/videoflow_audio.test.ts
@@ -5,6 +5,7 @@ import type { AudioItem, VideoItem, VideoProject } from '../project';
 import {
   addAudioItems,
   playsCleaned,
+  uncleanedSources,
   unheardByDucking,
   unheardSources,
   withVolumes,
@@ -400,3 +401,38 @@ describe('what ducking has still to hear', () => {
     expect(ids(withVoice(ducked({ muted: true })))).toEqual([]);
   });
 });
+
+describe('what noise reduction has still to clean', () => {
+  const ids = (next: VideoProject) => uncleanedSources(next).map((source) => source.id);
+
+  it('is the source of a sound or a clip with noise reduction on and no cleaned copy, once each', () => {
+    expect(ids(project([sound({ denoise: true })]))).toEqual(['src-m']);
+    expect(ids(project([], { denoise: true }))).toEqual(['src-a']);
+    expect(
+      ids(
+        project([sound({ denoise: true }), sound({ id: 'bed-2', denoise: true })], {
+          denoise: true,
+        }),
+      ),
+    ).toEqual(['src-a', 'src-m']);
+  });
+
+  it('is nothing once the copy exists, or while noise reduction is off', () => {
+    expect(ids(cleaned([sound({ denoise: true })], { denoise: true }))).toEqual([]);
+    expect(ids(project([sound()]))).toEqual([]);
+  });
+
+  it('skips what plays nothing: a muted sound, a muted clip, a clip with no audio track', () => {
+    expect(ids(project([sound({ denoise: true, muted: true })]))).toEqual([]);
+    expect(ids(project([], { denoise: true, muted: true }))).toEqual([]);
+    const silent = project([], { denoise: true });
+    expect(
+      ids({
+        ...silent,
+        sources: silent.sources.map((source) =>
+          source.id === 'src-a' ? { ...source, hasAudio: false } : source,
+        ),
+      }),
+    ).toEqual([]);
+  });
+});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/videoflow_audio.test.ts`

Expected: the three new tests fail with `TypeError: uncleanedSources is not a function`; every older test in the file still passes.

Apply to `web/src/videoStudio/videoflow_audio.ts`:

```diff
diff --git a/web/src/videoStudio/videoflow_audio.ts b/web/src/videoStudio/videoflow_audio.ts
index dc13ab4e5..87317354d 100644
--- a/web/src/videoStudio/videoflow_audio.ts
+++ b/web/src/videoStudio/videoflow_audio.ts
@@ -45,18 +45,41 @@ function cleanedAsset(
   return denoise === true ? sourceOf(project, sourceId)?.denoisedAssetId : undefined;
 }
 
+/** The source a clip's sound comes from, or nothing for a clip that sounds of nothing: a muted one,
+ *  a still, or a video with no audio track. */
+function clipSound(project: VideoProject, clip: VideoItem): ProjectSource | undefined {
+  const source = sourceOf(project, clip.sourceId);
+  return !clip.muted && source?.kind === 'video' && source.hasAudio !== false ? source : undefined;
+}
+
 /** Whether a clip's sound comes from its cleaned copy. VideoFlow's video layer can only play its
  *  own file's audio, so a cleaned clip is its muted picture plus a sound layer of its own. */
 export function playsCleaned(project: VideoProject, clip: VideoItem): boolean {
-  const source = sourceOf(project, clip.sourceId);
   return (
-    !clip.muted &&
-    source?.kind === 'video' &&
-    source.hasAudio !== false &&
+    clipSound(project, clip) !== undefined &&
     cleanedAsset(project, clip.sourceId, clip.denoise) !== undefined
   );
 }
 
+/** What noise reduction has still to clean before the film it compiles is right: the source of
+ *  every clip and sound that plays with noise reduction on and has no cleaned copy yet. Until it
+ *  has one, the export plays the source as it is (`cleanedAsset`). */
+export function uncleanedSources(project: VideoProject): ProjectSource[] {
+  const uncleaned = new Map<string, ProjectSource>();
+  const want = (source: ProjectSource | undefined): void => {
+    if (source !== undefined && source.denoisedAssetId === undefined) {
+      uncleaned.set(source.id, source);
+    }
+  };
+  for (const clip of project.video) {
+    if (clip.denoise === true) want(clipSound(project, clip));
+  }
+  for (const item of audioTracks(project).flatMap((track) => track.items)) {
+    if (item.denoise === true && !item.muted) want(sourceOf(project, item.sourceId));
+  }
+  return [...uncleaned.values()];
+}
+
 /** Every sound as a VideoFlow AudioLayer, at the window its anchor gives it. */
 export function addAudioItems(
   flow: VideoFlow,
@@ -127,8 +150,8 @@ interface Player {
 function playersBesides(project: VideoProject, itemId: string): Player[] {
   const starts = clipStarts(project);
   const clips = project.video.flatMap((clip, index): Player[] => {
-    const source = sourceOf(project, clip.sourceId);
-    if (clip.muted || source?.kind !== 'video' || source.hasAudio === false) return [];
+    const source = clipSound(project, clip);
+    if (source === undefined) return [];
     const start = starts[index] ?? 0;
     const end = start + clipTimelineDuration(clip);
     return [{ source, start, end, sourceStart: clip.sourceStart, speed: clip.speed ?? 1 }];
@@ -203,8 +226,7 @@ function duckingOf(project: VideoProject, item: AudioItem): Ducking | undefined
 function loudnessByLayer(project: VideoProject): ReadonlyMap<string, Loudness> {
   const byName = new Map<string, Loudness>();
   for (const clip of project.video) {
-    const source = sourceOf(project, clip.sourceId);
-    if (!clip.muted && source?.kind === 'video' && source.hasAudio !== false) {
+    if (clipSound(project, clip) !== undefined) {
       byName.set(playsCleaned(project, clip) ? `${clip.id}${CLEANED}` : clip.id, {
         volume: clip.volume ?? 1,
       });
```

Run the same command. Expected: every test passes.

- [ ] **Step 5: `videoBitrate`, test first**

Apply to `web/src/videoStudio/__tests__/videoflow_export.test.ts`:

```diff
diff --git a/web/src/videoStudio/__tests__/videoflow_export.test.ts b/web/src/videoStudio/__tests__/videoflow_export.test.ts
index 71995fabe..84a10f8f0 100644
--- a/web/src/videoStudio/__tests__/videoflow_export.test.ts
+++ b/web/src/videoStudio/__tests__/videoflow_export.test.ts
@@ -378,4 +378,12 @@ describe('exportProject', () => {
 
     expect(onProgress).toHaveBeenCalledWith(0.5);
   });
+
+  it('hands the renderer the video bitrate it was given, and none when it was given none', async () => {
+    await exportProject(project(), urls, { videoBitrate: 6_000_000 });
+    await exportProject(project(), urls);
+
+    expect(renderer.exportOptions[0]).toMatchObject({ videoBitrate: 6_000_000 });
+    expect(renderer.exportOptions[1]).not.toHaveProperty('videoBitrate');
+  });
 });
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio/__tests__/videoflow_export.test.ts`

Expected: `AssertionError: expected { worker: true } to match object { videoBitrate: 6000000 }`; `Tests  1 failed | 19 passed (20)`.

Apply to `web/src/videoStudio/videoflow.ts`:

```diff
diff --git a/web/src/videoStudio/videoflow.ts b/web/src/videoStudio/videoflow.ts
index dac38c0fa..d6f3233eb 100644
--- a/web/src/videoStudio/videoflow.ts
+++ b/web/src/videoStudio/videoflow.ts
@@ -300,6 +300,9 @@ export interface ExportOptions {
    *  its media has settled while that loads (videoflow_media.ts `renderLoaded`). The previous
    *  cycle shipped an editor that kept transcoding after its dialog closed; this one cannot. */
   readonly signal?: AbortSignal;
+  /** The H.264 track's target, in bits per second. Unset, VideoFlow's own default (12 Mbps at
+   *  1080p, renderer-browser videoQuality.d.ts). */
+  readonly videoBitrate?: number;
 }
 
 /**
@@ -323,6 +326,7 @@ export async function exportProject(
     renderer.exportVideo({
       worker: true,
       ...(options.onProgress ? { onProgress: options.onProgress } : {}),
+      ...(options.videoBitrate === undefined ? {} : { videoBitrate: options.videoBitrate }),
       ...(signal ? { signal } : {}),
     }),
   );
```

Run the same command. Expected: `Tests  20 passed (20)`.

- [ ] **Step 6: The whole Studio suite, and web's gates**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/vt.sh src/videoStudio`

Expected: every file passes. The five touched suites alone are 138 tests (measured in scratch: 118 in the first three, 20 in the export suite).

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/webcheck.sh src/videoStudio/projectFile.ts src/videoStudio/sourceEdit.ts src/videoStudio/projectStore.ts src/videoStudio/VideoStudio_sources.ts src/videoStudio/VideoStudio.tsx src/videoStudio/videoflow.ts src/videoStudio/videoflow_audio.ts src/videoStudio/__tests__/projectStore.test.ts src/videoStudio/__tests__/VideoStudio_sources.test.ts src/videoStudio/__tests__/videoflow_audio.test.ts src/videoStudio/__tests__/videoflow_export.test.ts`

Expected:
- tsc prints nothing;
- oxlint prints `Found 0 warnings and 0 errors.`;
- the contract passes;
- knip lists nothing;
- `All matched files use Prettier code style!`

Then `git -C /d/Aura diff --stat` changes nothing beyond the files above. Prettier already formatted them in scratch.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/go-gates.sh ./internal/assets/`

Expected: gofmt lists nothing; `ok  github.com/chetto1983/aura/internal/assets`; lint `0 issues.` Only a comment changed.

- [ ] **Step 7: Commit**

Write `$W/msg-t4.txt`:

```text
feat(studio): give the Studio core a second consumer

The video sidecar imports the Studio's pure core, and two modules mixed
it with the cockpit's browser code: projectStore.ts held the project
file's name and parser beside the React save path and the attachment
client, and VideoStudio_sources.ts held sourceEdit beside the upload
UI. Bundled as they were, the parser pulled React and the chat client
into a Node server. projectFile.ts and sourceEdit.ts take the pure
halves; no function changes.

uncleanedSources is unheardByDucking's twin: what noise reduction has
still to clean before a render, so the sidecar can make those copies.
The "does this clip sound" test, written three times in
videoflow_audio.ts, becomes clipSound. ExportOptions.videoBitrate lets
the sidecar set the H.264 target; unset, the cockpit keeps VideoFlow's
default. Go's suffix parity test reads the constant from its new file.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t4.txt web/src/videoStudio/projectFile.ts web/src/videoStudio/sourceEdit.ts web/src/videoStudio/projectStore.ts web/src/videoStudio/VideoStudio_sources.ts web/src/videoStudio/VideoStudio.tsx web/src/videoStudio/videoflow.ts web/src/videoStudio/videoflow_audio.ts web/src/videoStudio/__tests__/projectStore.test.ts web/src/videoStudio/__tests__/VideoStudio_sources.test.ts web/src/videoStudio/__tests__/videoflow_audio.test.ts web/src/videoStudio/__tests__/videoflow_export.test.ts internal/objectstore/studio_project_test.go internal/objectstore/asset_placement.go internal/assets/studio_project_rekey.go`

Expected: `COMMIT_RC=0`. The pre-commit gofmt, vet, lint and file-size gates ran.

---

### Task 5: The seam into the Studio, and its refusals in words

`src/studio.ts` is the one Node module that imports `web/src/videoStudio`; every other module imports the seam. B1's seam exports what B1 uses. Tasks 11, 15 and 17 add `ProjectSize`, `junctionDurationAt`, `recordAnalysis` and `toVideoJSON` when their first user arrives. Task 19's drift gate then holds every bundle to this rule.

A Studio command refuses with an error whose message is an i18n key (`videoStudio: …`). The cockpit translates the key; the sidecar must say it in words. `refusalSentence` turns it into the English bundle sentence plus what would be accepted, from the operation's own target. For a trim past its source, the test expects: "A clip cannot play more than its source holds. Accepted: start ≥ -2 s and end ≤ 18 s, measured from where it starts in its 20 s source." An error that is not a Studio refusal answers `undefined`: it is a bug, and the caller lets it fail the call.

**Files:**
- Create: `services/video-mcp/src/studio.ts`, `services/video-mcp/src/refusal.ts`, `services/video-mcp/src/sentences.ts`
- Test: `services/video-mcp/test/support/projects.ts`, `services/video-mcp/test/unit/sentences.test.ts`

**Interfaces:**
- Consumes: the Studio core (Task 4).
- Produces:
  - `src/studio.ts` re-exports:
    - from `commands`: `addClip`, `addOverlay`, `CommandRefusal`, `moveClip`, `removeItem`, `removeRange`, `setClipPresentation`, `setFrameSize`, `setJunctionTransition`, `setMuted`, `setProperty`, `splitAt`, `trimClip`;
    - from `commands_audio`: `addAudio`, `extractAudio`, `moveAudio`, `setAudioProperties`, `setEnvelope`, `splitAudio`, `trimAudio`;
    - from `audioLane`: `audioWindow`, `findAudioItem`;
    - from `project`: `audioTracks`, `clipStarts`, `clipTimelineDuration`, `emptyProject`, `overlayWindow`, `projectDuration`, `sourceOf` and the types `AudioItem`, `OverlayItem`, `ProjectSource`, `VideoItem`, `VideoProject`;
    - from `projectFile`: `isProject`, `PROJECT_FILE_EXTENSION`, `projectFileName`;
    - from `sourceEdit`: `IMAGE_SECONDS`, `sourceFrom`, `STARTING_FPS` and the type `ProbedSource`;
    - from `videoflow_audio`: `uncleanedSources`, `unheardByDucking`;
    - `videoStudioEn`.
  - `class Refusal extends Error`: a refusal the caller reads as an answer, never a tool failure.
  - `bundleSentence(key: string): string`.
  - `refusalSentence(error: unknown, operation: OperationFields, project: VideoProject): string | undefined`.
- Test support (`test/support/projects.ts`): `CAMERA`, `MUSIC` and `STILL` (`ProjectSource`), and `sampleProject(): VideoProject`. In that project:
  - `c1` plays 8 s of a 20 s camera source with sound;
  - `c2` plays 6 s of a 12 s screen recording with none;
  - a title `t1` and a music bed `m1` each sit on a lane of their own.

- [ ] **Step 1: Write the failing test and its fixture**

`services/video-mcp/test/support/projects.ts`:

```ts
// A small project as the Studio saves it (web/src/videoStudio/project.ts): two clips of a 20 s
// camera source with sound, a 12 s screen recording without, a 30 s music bed and a still.
import type { ProjectSource, VideoProject } from '../../src/studio';

export const CAMERA: ProjectSource = {
  id: 'src-camera',
  assetId: '3f1d2c4b-5a69-4e70-8f81-92a3b4c5d6e7',
  kind: 'video',
  duration: 20,
  size: { width: 1920, height: 1080 },
  hasAudio: true,
};
const SCREEN: ProjectSource = {
  id: 'src-screen',
  assetId: '4a2e3d5c-6b7a-4f81-9a92-a3b4c5d6e7f8',
  kind: 'video',
  duration: 12,
  size: { width: 1916, height: 878 },
  hasAudio: false,
};
export const MUSIC: ProjectSource = {
  id: 'src-music',
  assetId: '5b3f4e6d-7c8b-4a92-8ba3-b4c5d6e7f809',
  kind: 'audio',
  duration: 30,
  size: { width: 0, height: 0 },
};
export const STILL: ProjectSource = {
  id: 'src-still',
  assetId: '6c4a5f7e-8d9c-4ba3-9cb4-c5d6e7f8091a',
  kind: 'image',
  duration: 0,
  size: { width: 1080, height: 1920 },
};

/** c1 0–8 s (camera from 2 s), c2 8–14 s (screen), a title on c1, the music under both. */
export function sampleProject(): VideoProject {
  return {
    id: 'a1b2c3d4-e5f6-4789-8abc-def012345678',
    name: 'Aura memory',
    size: { width: 1080, height: 1920 },
    fps: 30,
    sources: [CAMERA, SCREEN, MUSIC, STILL],
    video: [
      { id: 'c1', sourceId: CAMERA.id, duration: 8, sourceStart: 2, muted: false },
      { id: 'c2', sourceId: SCREEN.id, duration: 6, sourceStart: 0, muted: true },
    ],
    overlays: [
      {
        id: 'titles',
        items: [
          {
            id: 't1',
            kind: 'text',
            anchor: { clipId: 'c1', offset: 1 },
            duration: 3,
            props: { text: 'Aura remembers', fontSize: 6 },
          },
        ],
      },
    ],
    audio: [
      {
        id: 'music-lane',
        items: [
          {
            id: 'm1',
            sourceId: MUSIC.id,
            anchor: { clipId: 'c1', offset: 0 },
            sourceStart: 0,
            duration: 30,
            volume: 0.8,
            muted: false,
          },
        ],
      },
    ],
  };
}
```

`services/video-mcp/test/unit/sentences.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { Refusal } from '../../src/refusal';
import { bundleSentence, refusalSentence } from '../../src/sentences';
import {
  addAudio,
  addClip,
  addOverlay,
  CommandRefusal,
  extractAudio,
  moveAudio,
  setAudioProperties,
  splitAt,
  splitAudio,
  trimAudio,
  trimClip,
  type AudioItem,
  type VideoProject,
} from '../../src/studio';
import { sampleProject } from '../support/projects';

function refusedBy(run: () => unknown): unknown {
  try {
    run();
  } catch (error) {
    return error;
  }
  throw new Error('the command was not refused');
}

describe('the cockpit’s own words', () => {
  it('are read from its English bundle by the refusal’s key', () => {
    expect(bundleSentence('videoStudio.refusal.trimPastSource')).toBe(
      'A clip cannot play more than its source holds.',
    );
    expect(bundleSentence('videoStudio.audio.refusal.overlap')).not.toBe(
      'videoStudio.audio.refusal.overlap',
    );
  });

  it('fall back to the key itself when the bundle has no sentence for it', () => {
    expect(bundleSentence('videoStudio.refusal.noSuchRefusal')).toBe(
      'videoStudio.refusal.noSuchRefusal',
    );
  });
});

describe('a refused operation', () => {
  const project = sampleProject();

  it('says what a trim could have asked, against the source it plays', () => {
    const operation = { op: 'trim_clip', clipId: 'c1', start: 0, end: 30 };
    const error = refusedBy(() => trimClip(project, operation));
    expect(refusalSentence(error, operation, project)).toBe(
      'A clip cannot play more than its source holds. Accepted: start ≥ -2 s and end ≤ 18 s, measured from where it starts in its 20 s source.',
    );
  });

  it('lists every clip’s window when a cut lands on an edge', () => {
    const operation = { op: 'split_at', time: 8 };
    const error = refusedBy(() => splitAt(project, operation));
    expect(refusalSentence(error, operation, project)).toContain(
      'Accepted: a time strictly inside a clip: c1 0–8 s, c2 8–14 s.',
    );
  });

  it('gives a sound’s window, as the film’s end cuts it, when its cut lands on an edge', () => {
    const operation = { op: 'split_audio', itemId: 'm1', time: 0 };
    const error = refusedBy(() => splitAudio(project, operation));
    expect(refusalSentence(error, operation, project)).toContain(
      'Accepted: a time strictly inside 0–14 s.',
    );
  });

  it('carries a refusal the sidecar raised itself unchanged', () => {
    const operation = { op: 'add_clip' };
    expect(refusalSentence(new Refusal('beach.mp4 could not be read as media'), operation, project)).toBe(
      'beach.mp4 could not be read as media',
    );
  });

  it('turns a command’s loud range check into a sentence', () => {
    const operation = { op: 'set_audio_properties', itemId: 'm1', volume: 3 };
    const error = refusedBy(() => setAudioProperties(project, operation));
    expect(refusalSentence(error, operation, project)).toBe(
      'audio volume must be between 0 and 2.',
    );
  });

  it('is no sentence at all for an error that is a bug', () => {
    expect(refusalSentence(new TypeError('x is undefined'), { op: 'trim_clip' }, project)).toBeUndefined();
    expect(
      refusalSentence(new CommandRefusal('videoStudio.refusal.emptyRange'), { op: 'remove_range' }, project),
    ).toBe('That stretch of the timeline is empty. Accepted: a range that overlaps the film; the film runs 0–14 s.');
  });
});

describe('what each refusal says would be accepted', () => {
  const project = sampleProject();
  // A short sound starting 5 s into its source, and a second lane holding a voice at 9–11 s.
  const busy: VideoProject = {
    ...project,
    audio: [
      { id: 'music-lane', items: [{ ...musicItem(project), sourceStart: 5, duration: 6 }] },
      { id: 'voice-lane', items: [{ ...musicItem(project), id: 'v1', anchor: { clipId: 'c2', offset: 1 }, duration: 2 }] },
    ],
  };
  const cases: [string, Record<string, unknown>, (p: VideoProject) => unknown, string | undefined][] = [
    ['a sound trimmed past its source', { op: 'trim_audio', itemId: 'm1', start: 0, end: 40 },
      (p) => trimAudio(p, { itemId: 'm1', start: 0, end: 40 }),
      'start ≥ -5 s and end ≤ 25 s, measured from where it starts in its 30 s source'],
    ['a clip longer than the source it is added from', { op: 'add_clip', sourceId: 'src-screen', duration: 20 },
      (p) => addClip(p, { sourceId: 'src-screen', duration: 20 }),
      "sourceStart + duration within the source's 12 s"],
    ['an overlay with nothing left of its window', { op: 'add_overlay', anchor: { clipId: 'c1', offset: 9 } },
      (p) => addOverlay(p, { kind: 'text', anchor: { clipId: 'c1', offset: 9 }, duration: 2, props: {} }),
      'an offset inside the clip and a positive duration; the clips are c1 0–8 s, c2 8–14 s'],
    ['an overlay on a busy lane', { op: 'add_overlay', anchor: { clipId: 'c1', offset: 0 }, trackId: 'titles' },
      (p) => addOverlay(p, { kind: 'text', anchor: { clipId: 'c1', offset: 0 }, duration: 2, props: {}, trackId: 'titles' }),
      'another lane, or no trackId to let the editor choose a free one'],
    ['a sound moved onto a busy lane', { op: 'move_audio', itemId: 'm1', start: 9, trackId: 'voice-lane' },
      (p) => moveAudio(p, { itemId: 'm1', start: 9, trackId: 'voice-lane' }),
      'another lane, or no trackId to let the editor choose a free one'],
    ['a sound placed past the film', { op: 'add_audio', sourceId: 'src-music', time: 20 },
      (p) => addAudio(p, { sourceId: 'src-music', time: 20 }), 'a time inside the film; the film runs 0–14 s'],
    ['a sound moved to the film’s end', { op: 'move_audio', itemId: 'm1', start: 14 },
      (p) => moveAudio(p, { itemId: 'm1', start: 14 }), 'a time inside the film; the film runs 0–14 s'],
    ['a sound whose trim would start it before the film', { op: 'trim_audio', itemId: 'm1', start: -2, end: 6 },
      (p) => trimAudio(p, { itemId: 'm1', start: -2, end: 6 }), 'a start at or after 0 s'],
    ['a clip of a source the project lacks', { op: 'add_clip', sourceId: 'src-gone' },
      (p) => addClip(p, { sourceId: 'src-gone', duration: 1 }),
      "the project's sources are src-camera, src-screen, src-music, src-still"],
    ['a still put on an audio lane', { op: 'add_audio', sourceId: 'src-still', time: 0 },
      (p) => addAudio(p, { sourceId: 'src-still', time: 0 }), 'a sound, or a clip whose source has an audio track'],
    ['fades longer than the sound', { op: 'set_audio_properties', itemId: 'm1', fadeIn: 4, fadeOut: 4 },
      (p) => setAudioProperties(p, { itemId: 'm1', fadeIn: 4, fadeOut: 4 }), 'fadeIn + fadeOut no longer than the sound'],
    ['a clip’s sound extracted twice', { op: 'extract_audio', clipId: 'c1' },
      (p) => extractAudio(extractAudio(p, { clipId: 'c1' }), { clipId: 'c1' }), undefined],
  ];

  it.each(cases)('%s', (_name, operation, run, hint) => {
    const error = refusedBy(() => run(busy));
    expect(error).toBeInstanceOf(CommandRefusal);
    const sentence = refusalSentence(error, operation as { op: string }, busy);
    const key = (error as CommandRefusal).reasonKey;
    expect(sentence).toBe(hint === undefined ? bundleSentence(key) : `${bundleSentence(key)} Accepted: ${hint}.`);
  });
});

function musicItem(project: VideoProject): AudioItem {
  const item = project.audio?.[0]?.items[0];
  if (item === undefined) throw new Error('the sample project has its music');
  return item;
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/sentences.test.ts`

Expected: `FAIL`, `Error: Cannot find module '../../src/refusal' imported from …/test/unit/sentences.test.ts`, `VITEST_RC=1`.

- [ ] **Step 3: Write the seam, the refusal and the sentences**

`services/video-mcp/src/studio.ts` (B1's exports):

```ts
// studio.ts — the one door from the sidecar into the Studio's core. Every module here reaches
// web/src/videoStudio through this file, and the drift check (drift.ts) holds every bundle to it:
// the cockpit's own files, resolved against web/node_modules (its patched renderer-browser
// included), and no other part of web/src.
export {
  addClip,
  addOverlay,
  CommandRefusal,
  moveClip,
  removeItem,
  removeRange,
  setClipPresentation,
  setFrameSize,
  setJunctionTransition,
  setMuted,
  setProperty,
  splitAt,
  trimClip,
} from '../../../web/src/videoStudio/commands';
export {
  addAudio,
  extractAudio,
  moveAudio,
  setAudioProperties,
  setEnvelope,
  splitAudio,
  trimAudio,
} from '../../../web/src/videoStudio/commands_audio';
export { audioWindow, findAudioItem } from '../../../web/src/videoStudio/audioLane';
export {
  audioTracks,
  clipStarts,
  clipTimelineDuration,
  emptyProject,
  overlayWindow,
  projectDuration,
  sourceOf,
  type AudioItem,
  type OverlayItem,
  type ProjectSource,
  type VideoItem,
  type VideoProject,
} from '../../../web/src/videoStudio/project';
export {
  isProject,
  PROJECT_FILE_EXTENSION,
  projectFileName,
} from '../../../web/src/videoStudio/projectFile';
export {
  IMAGE_SECONDS,
  sourceFrom,
  STARTING_FPS,
  type ProbedSource,
} from '../../../web/src/videoStudio/sourceEdit';
export { uncleanedSources, unheardByDucking } from '../../../web/src/videoStudio/videoflow_audio';
export { videoStudioEn } from '../../../web/src/i18n/resources.videoStudio';
```

`services/video-mcp/src/refusal.ts`:

```ts
// refusal.ts — a request the sidecar declines, in one sentence for the caller. A tool answers it
// as a result, never as a failure: the caller asked for something that cannot be, and the
// sentence says what would be accepted (spec §Tools, "Refusals").
export class Refusal extends Error {
  constructor(sentence: string) {
    super(sentence);
    this.name = 'Refusal';
  }
}
```

`services/video-mcp/src/sentences.ts`:

```ts
// sentences.ts — what a refused operation says. The Studio's commands refuse by i18n key
// (commands.ts CommandRefusal), and the cockpit's English bundle already words each key; the
// sidecar reads that bundle rather than spelling the sentences again, and adds what would be
// accepted, computed from the operation and the project as they were when it was refused.

import { Refusal } from './refusal';
import {
  audioWindow,
  clipStarts,
  clipTimelineDuration,
  CommandRefusal,
  findAudioItem,
  projectDuration,
  sourceOf,
  videoStudioEn,
  type VideoProject,
} from './studio';

/** The fields of an operation a hint reads; every operation carries its own subset. */
export interface OperationFields {
  readonly op: string;
  readonly clipId?: string | undefined;
  readonly itemId?: string | undefined;
  readonly sourceId?: string | undefined;
  readonly trackId?: string | undefined;
  readonly anchor?: { readonly clipId: string; readonly offset: number } | undefined;
}

const round = (value: number): string => String(Math.round(value * 1000) / 1000);

/** The cockpit's English sentence for a refusal key such as `videoStudio.audio.refusal.noClip`. */
export function bundleSentence(key: string): string {
  let node: unknown = videoStudioEn;
  for (const part of key.split('.')) {
    node = typeof node === 'object' && node !== null ? (node as Record<string, unknown>)[part] : undefined;
  }
  return typeof node === 'string' ? node : key;
}

function clipWindows(project: VideoProject): string {
  const starts = clipStarts(project);
  return project.video
    .map((clip, index) => {
      const start = starts[index] ?? 0;
      return `${clip.id} ${round(start)}–${round(start + clipTimelineDuration(clip))} s`;
    })
    .join(', ');
}

/** What the refused operation could have asked instead, or nothing when the sentence says it. */
function acceptedHint(key: string, operation: OperationFields, project: VideoProject): string | undefined {
  const film = `the film runs 0–${round(projectDuration(project))} s`;
  const clip = project.video.find((candidate) => candidate.id === operation.clipId);
  const sound = operation.itemId === undefined ? undefined : findAudioItem(project, operation.itemId);
  switch (key.split('.').at(-1)) {
    case 'trimPastSource': {
      const playing = clip ?? sound;
      const source = playing === undefined ? sourceOf(project, operation.sourceId ?? '') : sourceOf(project, playing.sourceId);
      if (source === undefined) return undefined;
      if (playing === undefined) {
        return `sourceStart + duration within the source's ${round(source.duration)} s`;
      }
      return `start ≥ ${round(-playing.sourceStart)} s and end ≤ ${round(source.duration - playing.sourceStart)} s, measured from where it starts in its ${round(source.duration)} s source`;
    }
    case 'splitOnBoundary':
      return sound === undefined
        ? `a time strictly inside a clip: ${clipWindows(project)}`
        : `a time strictly inside ${round(audioWindow(project, sound).start)}–${round(audioWindow(project, sound).end)} s`;
    case 'emptyRange':
      return operation.anchor === undefined
        ? `a range that overlaps the film; ${film}`
        : `an offset inside the clip and a positive duration; the clips are ${clipWindows(project)}`;
    case 'overlayOverlap':
    case 'overlap':
      return 'another lane, or no trackId to let the editor choose a free one';
    case 'noClip':
    case 'pastEnd':
      return `a time inside the film; ${film}`;
    case 'beforeStart':
      return 'a start at or after 0 s';
    case 'sourceMissing':
      return `the project's sources are ${project.sources.map((source) => source.id).join(', ') || 'none'}`;
    case 'notSound':
      return 'a sound, or a clip whose source has an audio track';
    case 'fadesTooLong':
      return sound === undefined ? undefined : 'fadeIn + fadeOut no longer than the sound';
    default:
      return undefined;
  }
}

/** The sentence a refused operation answers with, or undefined for an error that is a bug. */
export function refusalSentence(
  error: unknown,
  operation: OperationFields,
  project: VideoProject,
): string | undefined {
  if (error instanceof Refusal) return error.message;
  if (error instanceof CommandRefusal) {
    const hint = acceptedHint(error.reasonKey, operation, project);
    return hint === undefined
      ? bundleSentence(error.reasonKey)
      : `${bundleSentence(error.reasonKey)} Accepted: ${hint}.`;
  }
  // The commands are loud about a caller out of step with the model ("no clip named …", a volume
  // out of range): for a tool that is a refusal too, worded by the command itself.
  if (error instanceof Error && error.message.startsWith('videoStudio: ')) {
    return `${error.message.slice('videoStudio: '.length)}.`;
  }
  return undefined;
}
```

- [ ] **Step 4: Run it to see it pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/sentences.test.ts`

Expected: `Tests  20 passed (20)`, `VITEST_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`. The typecheck now walks every Studio module the seam reaches.

- [ ] **Step 5: Commit**

Write `$W/msg-t5.txt`:

```text
feat(video-mcp): one door into the Studio, and its refusals in words

src/studio.ts is the only module of the sidecar that imports
web/src/videoStudio; everything else imports it, so the Studio's own
commands, project shape and parser are the ones a project is edited
with. A Studio command refuses with an i18n key the cockpit
translates; the sidecar turns it into the English sentence plus what
would be accepted, read from the operation's own target. An error that
is no refusal stays an error: it is a bug, not an answer.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t5.txt services/video-mcp/src/studio.ts services/video-mcp/src/refusal.ts services/video-mcp/src/sentences.ts services/video-mcp/test/support/projects.ts services/video-mcp/test/unit/sentences.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 6: A source named by asset id: resolved, probed once, HEVC refused

Adding a clip or a sound by asset id resolves it through the internal API, under the caller's token, then probes its signed URL with ffprobe. The probe reads:
- the kind, from the MIME type and the streams;
- the duration and the size;
- `hasAudio`, Plan A's carry-over: the Studio refuses sound over a silent video (`commands_audio.ts:136,171`), and only ffprobe can say a video is silent.

HEVC is refused with the spec's sentence. A source the project already holds is never probed again. An asset the library does not hold (another identity's, deleted, or not finished uploading) is one sentence that never says which: "asset X is not in your library, or its upload has not finished".

`ffprobe` is restricted to `-protocol_whitelist http,https,tcp,tls` with a 20 s timeout. A probe that fails is "<file> could not be read as media".

**Files:**
- Create: `services/video-mcp/src/probe.ts`, `services/video-mcp/src/sources.ts`
- Test: `services/video-mcp/test/unit/probe.test.ts`, `services/video-mcp/test/unit/sources.test.ts`

**Interfaces:**
- Consumes: `InternalApi`, `SpeechRequest` (Task 3); `Refusal`, `sourceFrom`, `ProbedSource`, `ProjectSource`, `VideoProject` (Task 5).
- Produces:
  - `HEVC_REFUSAL`; `interface FfprobeStream`, `interface FfprobeOutput { streams?; format?: { duration? } }`;
  - `probedFrom(fileName: string, mimeType: string, report: FfprobeOutput): ProbedSource`;
  - `ffprobe(url: string): Promise<FfprobeOutput>`;
  - `type Probe = (url: string, fileName: string, mimeType: string) => Promise<ProbedSource>` and `probeAsset: Probe`;
  - `interface SourceDoor { source(project, assetId): Promise<{ project; source }>; picture(assetId): Promise<void>; speech(request: SpeechRequest): Promise<string>; readonly spoken: readonly string[] }`;
  - `notInLibrary(assetId: string): Refusal`;
  - `sourceDoor(api: InternalApi, token: string, probe: Probe): SourceDoor`.

- [ ] **Step 1: Write the failing tests**

`services/video-mcp/test/unit/probe.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { HEVC_REFUSAL, probeAsset, probedFrom, type FfprobeOutput } from '../../src/probe';
import { Refusal } from '../../src/refusal';

// ffprobe's own JSON shapes (`-show_streams -show_format -of json`), trimmed to the fields read.
const h264 = { codec_type: 'video', codec_name: 'h264', width: 1920, height: 1080, duration: '8.000000' };
const aac = { codec_type: 'audio', codec_name: 'aac', duration: '8.010000' };

describe('a clip', () => {
  it('is a video with its length, its size, and whether it has an audio track', () => {
    expect(probedFrom('a.mp4', 'video/mp4', { streams: [h264, aac] })).toEqual({
      kind: 'video',
      duration: 8,
      width: 1920,
      height: 1080,
      hasAudio: true,
    });
    expect(probedFrom('a.mp4', 'video/mp4', { streams: [h264] })).toMatchObject({ hasAudio: false });
  });

  it('takes its length from the container when the stream does not say', () => {
    const report: FfprobeOutput = {
      streams: [{ ...h264, duration: 'N/A' }],
      format: { duration: '12.5' },
    };
    expect(probedFrom('a.webm', 'video/webm', report).duration).toBe(12.5);
  });

  it('is measured as a viewer sees it when a phone stored it with a quarter turn', () => {
    const turned = { ...h264, side_data_list: [{ rotation: -90 }] };
    expect(probedFrom('a.mp4', 'video/mp4', { streams: [turned] })).toMatchObject({
      width: 1080,
      height: 1920,
    });
  });

  it('is refused in HEVC, with the spec’s sentence', () => {
    expect(() =>
      probedFrom('a.mp4', 'video/mp4', { streams: [{ ...h264, codec_name: 'hevc' }, aac] }),
    ).toThrow(new Refusal(HEVC_REFUSAL));
  });

  it('is refused when it holds no picture, or a cover picture only', () => {
    expect(() => probedFrom('a.mp4', 'video/mp4', { streams: [aac] })).toThrow(
      'a.mp4 has no picture the renderer can read',
    );
    const cover = { ...h264, codec_name: 'mjpeg', disposition: { attached_pic: 1 } };
    expect(() => probedFrom('a.mp4', 'video/mp4', { streams: [cover, aac] })).toThrow(Refusal);
  });

  it('is refused with no length', () => {
    expect(() =>
      probedFrom('a.mp4', 'video/mp4', { streams: [{ ...h264, duration: '0' }], format: {} }),
    ).toThrow('a.mp4 has no picture');
  });
});

describe('a still', () => {
  it('is a PNG, a JPEG or a WebP, with no length of its own', () => {
    const png = { codec_type: 'video', codec_name: 'png', width: 1080, height: 1920 };
    expect(probedFrom('s.png', 'image/png', { streams: [png] })).toEqual({
      kind: 'image',
      duration: 0,
      width: 1080,
      height: 1920,
    });
  });

  it('is refused as a GIF, or when its bytes are not the picture its type claims', () => {
    const gif = { codec_type: 'video', codec_name: 'gif', width: 10, height: 10 };
    expect(() => probedFrom('s.gif', 'image/gif', { streams: [gif] })).toThrow(
      's.gif is image/gif: only a PNG, JPEG or WebP picture can be a clip',
    );
    expect(() => probedFrom('s.png', 'image/png', { streams: [gif] })).toThrow(Refusal);
  });
});

describe('a sound', () => {
  it('is audio with its length and no frame', () => {
    expect(probedFrom('m.wav', 'audio/wav', { streams: [{ ...aac, duration: '50' }] })).toEqual({
      kind: 'audio',
      duration: 50,
      width: 0,
      height: 0,
    });
  });

  it('is refused when it holds no audio', () => {
    expect(() => probedFrom('m.mp3', 'audio/mpeg', { streams: [] })).toThrow(
      'm.mp3 holds no sound the renderer can read',
    );
  });
});

it('refuses a type that is no source at all', () => {
  expect(() => probedFrom('notes.pdf', 'application/pdf', { streams: [] })).toThrow(
    'notes.pdf is application/pdf: a source is a clip, a picture or a sound',
  );
});

it('names the file, never the signed URL, when ffprobe cannot read it', async () => {
  const url = 'http://127.0.0.1:9/objects/x.mp4?X-Amz-Signature=secret';
  const failure = probeAsset(url, 'beach.mp4', 'video/mp4');
  await expect(failure).rejects.toThrow(new Refusal('beach.mp4 could not be read as media'));
  await expect(failure).rejects.not.toThrow(/secret/);
});
```

`services/video-mcp/test/unit/sources.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import type { InternalApi, ResolvedAsset } from '../../src/internalApi';
import type { Probe } from '../../src/probe';
import { Refusal } from '../../src/refusal';
import { sourceDoor } from '../../src/sources';
import { sampleProject, CAMERA } from '../support/projects';

const BEACH = '8e6c7b90-af1e-4dc5-9ed6-e7f8091a2b3c';
const POSTER = '9f7d8ca1-b02f-4ed6-8fe7-f8091a2b3c4d';

function asset(assetId: string, fileName: string, mimeType: string): ResolvedAsset {
  return { assetId, url: `http://garage:3900/b/${fileName}?X-Amz-Signature=s`, fileName, mimeType, sizeBytes: 10, expiresAt: '2026-10-02T14:00:00Z' };
}

/** Aura's internal API with only what the door asks of it, recording what it was asked. */
function library(...assets: ResolvedAsset[]) {
  const resolved: string[][] = [];
  const api: InternalApi = {
    async resolve(_token, ids) {
      resolved.push([...ids]);
      const found = new Map(assets.filter((known) => ids.includes(known.assetId)).map((known) => [known.assetId, known]));
      return { found, notFound: ids.filter((id) => !found.has(id)) };
    },
    upload: () => Promise.reject(new Error('not used')),
    tts: async () => ({ assetId: 'b8d4c2a0-1f3e-4d5c-9b7a-6e5f4d3c2b1a', fileName: 'speech.ogg', mimeType: 'audio/ogg', sizeBytes: 1, status: 'accepted' }),
    projects: () => Promise.reject(new Error('not used')),
  };
  return { api, resolved };
}

const probe: Probe = async (url, fileName, mimeType) => {
  expect(url).toContain('X-Amz-Signature');
  return mimeType.startsWith('image/')
    ? { kind: 'image', duration: 0, width: 800, height: 600 }
    : { kind: 'video', duration: 9, width: 1920, height: 1080, hasAudio: fileName !== 'silent.mp4' };
};

describe('the source door', () => {
  it('adds a newly named asset as a probed source under a fresh id, keeping the asset id it was given', async () => {
    const { api } = library(asset(BEACH, 'beach.mp4', 'video/mp4'));
    const { project, source } = await sourceDoor(api, 't', probe).source(sampleProject(), BEACH);
    expect(source).toMatchObject({ assetId: BEACH, kind: 'video', duration: 9, size: { width: 1920, height: 1080 }, hasAudio: true });
    expect(source.id).not.toBe(BEACH);
    expect(project.sources.at(-1)).toEqual(source);
  });

  it('reuses a source the project already plays, without asking Aura', async () => {
    const { api, resolved } = library();
    const { project, source } = await sourceDoor(api, 't', probe).source(sampleProject(), CAMERA.assetId);
    expect(source).toEqual(CAMERA);
    expect(project).toEqual(sampleProject());
    expect(resolved).toEqual([]);
  });

  it('refuses an asset Aura does not answer for', async () => {
    const { api } = library();
    await expect(sourceDoor(api, 't', probe).source(sampleProject(), BEACH)).rejects.toThrow(
      new Refusal(`asset ${BEACH} is not in your library, or its upload has not finished`),
    );
  });

  it('takes only a picture as an image overlay', async () => {
    const { api } = library(asset(POSTER, 'poster.png', 'image/png'), asset(BEACH, 'beach.mp4', 'video/mp4'));
    const door = sourceDoor(api, 't', probe);
    await expect(door.picture(POSTER)).resolves.toBeUndefined();
    await expect(door.picture(BEACH)).rejects.toThrow(
      `asset ${BEACH} is not a picture: an image overlay shows a PNG, JPEG or WebP`,
    );
  });

  it('remembers every speech it had synthesized', async () => {
    const door = sourceDoor(library().api, 't', probe);
    const spoken = await door.speech({ text: 'hello', voice: undefined, language: 'en' });
    expect(door.spoken).toEqual([spoken]);
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/probe.test.ts test/unit/sources.test.ts`

Expected: both `FAIL` with `Error: Cannot find module '../../src/probe' imported from …`; `VITEST_RC=1`.

- [ ] **Step 3: Write the probe and the source door**

`services/video-mcp/src/probe.ts`:

```ts
// probe.ts — the sidecar's door for a source: ffprobe reads what an asset holds before it becomes
// a layer, the way the Studio's probeSource does in the browser (VideoStudio_sources.ts). It fills
// `hasAudio` on every clip, so a sound is refused over a video with no audio track at edit time
// (commands_audio.ts addAudio, extractAudio) instead of stopping the export with "could not read"
// for a file that plays (Plan A Close-out). HEVC is refused here: no browser build the renderer can
// run decodes it (spikes/video-mcp-render/FINDINGS.md S1.1).

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { Refusal } from './refusal';
import type { ProbedSource } from './studio';

const run = promisify(execFile);

/** A URL that never answers must not hold an edit past Aura's 60 s MCP call timeout. */
const PROBE_TIMEOUT_MS = 20_000;
/** The stills the Studio takes as a clip (VideoStudio_sources.ts STILL_TYPES). */
const STILL_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp']);
const STILL_CODECS = new Set(['png', 'mjpeg', 'webp']);

export const HEVC_REFUSAL =
  'HEVC is not supported by the renderer: convert the clip to H.264 first';

export interface FfprobeStream {
  readonly codec_type?: string;
  readonly codec_name?: string;
  readonly width?: number;
  readonly height?: number;
  readonly duration?: string;
  readonly disposition?: { readonly attached_pic?: number };
  readonly side_data_list?: readonly { readonly rotation?: number }[];
}

export interface FfprobeOutput {
  readonly streams?: readonly FfprobeStream[];
  readonly format?: { readonly duration?: string };
}

function seconds(...candidates: (string | undefined)[]): number {
  for (const candidate of candidates) {
    const value = Number(candidate);
    if (Number.isFinite(value) && value > 0) return value;
  }
  return 0;
}

/** The size a viewer sees: a phone's portrait clip is stored landscape with a quarter turn. */
function displayed(stream: FfprobeStream): { width: number; height: number } {
  const width = stream.width ?? 0;
  const height = stream.height ?? 0;
  const turn = Math.abs(stream.side_data_list?.find((data) => data.rotation !== undefined)?.rotation ?? 0);
  return turn % 180 === 90 ? { width: height, height: width } : { width, height };
}

/** What ffprobe's report says the asset is, or the refusal that names why it cannot be one. */
export function probedFrom(fileName: string, mimeType: string, report: FfprobeOutput): ProbedSource {
  const streams = report.streams ?? [];
  const picture = streams.find(
    (stream) => stream.codec_type === 'video' && stream.disposition?.attached_pic !== 1,
  );
  const sound = streams.find((stream) => stream.codec_type === 'audio');
  if (mimeType.startsWith('image/')) {
    if (!STILL_TYPES.has(mimeType) || !STILL_CODECS.has(picture?.codec_name ?? '')) {
      throw new Refusal(
        `${fileName} is ${mimeType}: only a PNG, JPEG or WebP picture can be a clip; convert anything else to MP4 first`,
      );
    }
    return { kind: 'image', duration: 0, ...displayed(picture ?? {}) };
  }
  if (mimeType.startsWith('audio/')) {
    const duration = seconds(sound?.duration, report.format?.duration);
    if (sound === undefined || duration === 0) {
      throw new Refusal(`${fileName} holds no sound the renderer can read`);
    }
    return { kind: 'audio', duration, width: 0, height: 0 };
  }
  if (!mimeType.startsWith('video/')) {
    throw new Refusal(`${fileName} is ${mimeType}: a source is a clip, a picture or a sound`);
  }
  if (picture?.codec_name === 'hevc') throw new Refusal(HEVC_REFUSAL);
  const duration = seconds(picture?.duration, report.format?.duration);
  if (picture === undefined || duration === 0) {
    throw new Refusal(`${fileName} has no picture the renderer can read: convert it to H.264 MP4`);
  }
  return { kind: 'video', duration, ...displayed(picture), hasAudio: sound !== undefined };
}

/** ffprobe's report on a signed URL. Only HTTP reaches ffprobe: a playlist that names a local
 *  file is refused by the protocol whitelist rather than read. */
export async function ffprobe(url: string): Promise<FfprobeOutput> {
  const { stdout } = await run(
    'ffprobe',
    [
      '-v', 'error',
      '-protocol_whitelist', 'http,https,tcp,tls',
      '-show_streams', '-show_format',
      '-of', 'json',
      url,
    ],
    { timeout: PROBE_TIMEOUT_MS, maxBuffer: 4 << 20 },
  );
  return JSON.parse(stdout) as FfprobeOutput;
}

export type Probe = (url: string, fileName: string, mimeType: string) => Promise<ProbedSource>;

export const probeAsset: Probe = async (url, fileName, mimeType) => {
  let report: FfprobeOutput;
  try {
    report = await ffprobe(url);
  } catch {
    // ffprobe's own words carry the signed URL; the caller gets the file's name instead.
    throw new Refusal(`${fileName} could not be read as media`);
  }
  return probedFrom(fileName, mimeType, report);
};
```

`services/video-mcp/src/sources.ts`:

```ts
// sources.ts — where an asset becomes something a project can play: resolved through Aura's
// internal API (which answers only for the caller's own assets whose bytes exist), probed with
// ffprobe, and added as a project source the way the Studio adds one (sourceEdit.ts sourceFrom).
// The asset id is what the caller named and what resolve answered for, never one read back out of
// an object key (Plan A Close-out).

import type { InternalApi, SpeechRequest } from './internalApi';
import type { Probe } from './probe';
import { Refusal } from './refusal';
import { sourceFrom, type ProjectSource, type VideoProject } from './studio';

export interface SourceDoor {
  /** The project with a source playing `assetId`: the one it already has, or a probed new one. */
  source(project: VideoProject, assetId: string): Promise<{ project: VideoProject; source: ProjectSource }>;
  /** Checks that `assetId` is a picture of the caller's an image overlay can show. */
  picture(assetId: string): Promise<void>;
  /** Aura speaks `request`; the answer is the new audio asset's id. */
  speech(request: SpeechRequest): Promise<string>;
  /** The speech assets this door has had synthesized so far. */
  readonly spoken: readonly string[];
}

export function notInLibrary(assetId: string): Refusal {
  return new Refusal(`asset ${assetId} is not in your library, or its upload has not finished`);
}

export function sourceDoor(api: InternalApi, token: string, probe: Probe): SourceDoor {
  const spoken: string[] = [];

  async function probed(assetId: string) {
    const { found } = await api.resolve(token, [assetId]);
    const asset = found.get(assetId);
    if (asset === undefined) throw notInLibrary(assetId);
    return await probe(asset.url, asset.fileName, asset.mimeType);
  }

  return {
    spoken,
    async source(project, assetId) {
      const known = project.sources.find((candidate) => candidate.assetId === assetId);
      if (known !== undefined) return { project, source: known };
      const source = sourceFrom(await probed(assetId), assetId);
      return { project: { ...project, sources: [...project.sources, source] }, source };
    },
    async picture(assetId) {
      if ((await probed(assetId)).kind !== 'image') {
        throw new Refusal(`asset ${assetId} is not a picture: an image overlay shows a PNG, JPEG or WebP`);
      }
    },
    async speech(request) {
      const row = await api.tts(token, request);
      spoken.push(row.assetId);
      return row.assetId;
    },
  };
}
```

- [ ] **Step 4: Run them to see them pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/probe.test.ts test/unit/sources.test.ts`

Expected: `Tests  17 passed (17)`, `VITEST_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 5: Commit**

Write `$W/msg-t6.txt`:

```text
feat(video-mcp): probe a source by asset id, once, and refuse HEVC

A clip or sound named by asset id is resolved under the caller's token
and probed with ffprobe on its signed link: kind, duration, size, and
whether it has audio, which the Studio needs to refuse sound over a
silent video and only ffprobe can tell. HEVC is refused with the
spec's sentence. A source the project already holds is not probed
again, and an asset the library does not hold is one sentence that
never says why.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t6.txt services/video-mcp/src/probe.ts services/video-mcp/src/sources.ts services/video-mcp/test/unit/probe.test.ts services/video-mcp/test/unit/sources.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 7: The HTTP surface: health, metadata, and MCP behind the bearer

Everything here comes from the MCP SDK, 1.31.0:
- `createMcpExpressApp` with a Host allow-list;
- `requireBearerAuth` for the token;
- `metadataHandler` for RFC 9728 protected-resource metadata;
- a stateless streamable-HTTP transport per request, with JSON answers.

`/health` answers `{"status":"ok","version":…}` and needs no token: Compose's health check calls it (§Contract for Plan C).

The metadata names the canonical resource, and its `resource_metadata` URL follows the Host the client dialled, so both the loopback name and the Compose name point at an address their client can reach. `GET` and `DELETE /mcp` answer 405: there is no session. A tool registration or a request that throws is logged and answered 500, never left hanging.

**Files:**
- Create: `services/video-mcp/src/http.ts`
- Test: `services/video-mcp/test/support/sidecar.ts`, `services/video-mcp/test/unit/http.test.ts`

**Interfaces:**
- Consumes: `tokenVerifier`, `callerOf`, `Caller` (Task 2); `Credentials` (Task 2); `Config`, `PORT`, `SCOPE`, `MCP_PATH`, `METADATA_PATH` (Task 1); `log` (Task 2).
- Produces:
  - `interface HttpParts { config; verifier; credentials; register: (server: McpServer) => void; version: string }`;
  - `createApp(parts: HttpParts): Express`, which calls `credentials.remember(caller)` on every MCP call.
- Test support (`test/support/sidecar.ts`):
  - `startSidecar(wire: (api: FakeInternalApi, credentials: Credentials) => (server: McpServer) => void): Promise<Running>`;
  - `interface Running { api; credentials; base; bearer(identity, claims?); call(tool, args, identity?, claims?): Promise<ToolAnswer>; close() }`;
  - `interface ToolAnswer { text; isError; structured }`, where `text` joins the result's text items only, so a link Task 18 adds is not read as an empty line;
  - `mcpClient(base, bearer)`;
  - `stubProbe(library)`.

- [ ] **Step 1: Write the failing test and the in-process sidecar**

`services/video-mcp/test/support/sidecar.ts`:

```ts
// The sidecar as a client meets it: the HTTP app (src/http.ts) with real token verification
// against a local signing key, in front of the contract-checked fake of Aura's internal API, and
// an MCP client per identity. `register` receives the fake so a test wires the tools exactly as
// main.ts does, with only the probe (ffprobe) stubbed in the unit tier.
import type { Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js';
import { tokenVerifier } from '../../src/auth';
import { Credentials } from '../../src/credentials';
import { createApp } from '../../src/http';
import type { Probe } from '../../src/probe';
import { Refusal } from '../../src/refusal';
import type { ProbedSource } from '../../src/studio';
import { FakeInternalApi } from './fakeInternalApi';
import { config, IDENTITY, localKeys, signingKey, token, type Claims } from './tokens';

export interface Running {
  readonly api: FakeInternalApi;
  readonly credentials: Credentials;
  /** The sidecar's own address. */
  readonly base: string;
  /** A fresh bearer of `identity`, which the fake internal API trusts too. */
  bearer(identity: string, claims?: Claims): Promise<string>;
  /** Calls `tool` as `identity` with a fresh token; answers the text of its result. */
  call(tool: string, args: Record<string, unknown>, identity?: string, claims?: Claims): Promise<ToolAnswer>;
  close(): Promise<void>;
}

export interface ToolAnswer {
  readonly text: string;
  readonly isError: boolean;
  readonly structured: unknown;
}

/** An MCP client connected to the sidecar at `base` with `bearer`. The cast is http.ts's: the
 *  SDK's transports are typed without exactOptionalPropertyTypes. */
export async function mcpClient(base: string, bearer: string): Promise<Client> {
  const client = new Client({ name: 'test', version: '1' });
  const transport = new StreamableHTTPClientTransport(new URL(`${base}/mcp`), {
    requestInit: { headers: { Authorization: `Bearer ${bearer}` } },
  });
  await client.connect(transport as Transport);
  return client;
}

/** ffprobe's answer per file name; a name it does not know cannot be read as media. */
export function stubProbe(library: Readonly<Record<string, ProbedSource>>): Probe {
  return (_url, fileName) => {
    const probed = library[fileName];
    return probed === undefined
      ? Promise.reject(new Refusal(`${fileName} could not be read as media`))
      : Promise.resolve(probed);
  };
}

export async function startSidecar(
  wire: (api: FakeInternalApi, credentials: Credentials) => (server: McpServer) => void,
): Promise<Running> {
  const api = new FakeInternalApi();
  await api.start();
  const key = await signingKey('k1');
  const credentials = new Credentials();
  const app = createApp({
    config: config({ internalApiUrl: api.url }),
    verifier: tokenVerifier(config(), localKeys(key)),
    credentials,
    register: wire(api, credentials),
    version: 'test',
  });
  const server: Server = app.listen(0, '127.0.0.1');
  await new Promise((resolve) => server.once('listening', resolve));
  const base = `http://127.0.0.1:${String((server.address() as AddressInfo).port)}`;

  const bearer = async (identity: string, claims: Claims = {}): Promise<string> => {
    const minted = await token(key, { sub: identity, ...claims });
    api.trust(minted, identity);
    return minted;
  };

  return {
    api,
    credentials,
    base,
    bearer,
    async call(tool, args, identity = IDENTITY, claims = {}) {
      const client = await mcpClient(base, await bearer(identity, claims));
      try {
        const result = await client.callTool({ name: tool, arguments: args });
        const content = result.content as { type: string; text?: string }[];
        return {
          text: content.flatMap((part) => (part.type === 'text' ? [part.text ?? ''] : [])).join('\n'),
          isError: result.isError === true,
          structured: result.structuredContent,
        };
      } finally {
        await client.close();
      }
    },
    async close() {
      await new Promise((resolve) => server.close(resolve));
      await api.close();
    },
  };
}
```

`services/video-mcp/test/unit/http.test.ts`:

```ts
import { request, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { z } from 'zod';
import { callerOf, tokenVerifier } from '../../src/auth';
import { Credentials } from '../../src/credentials';
import { createApp } from '../../src/http';
import { mcpClient } from '../support/sidecar';
import { config, IDENTITY, localKeys, signingKey, token, type SigningKey } from '../support/tokens';

let key: SigningKey;
let server: Server | undefined;
let credentials: Credentials;

beforeAll(async () => {
  key = await signingKey('k1');
});

afterEach(async () => {
  await new Promise((resolve) => server?.close(resolve));
  server = undefined;
});

/** A tool that answers with the identity it was called by. */
function whoami(mcp: McpServer): void {
  mcp.registerTool(
    'whoami',
    { description: 'the caller', inputSchema: { echo: z.string() } },
    ({ echo }, extra) => ({
      content: [{ type: 'text', text: `${callerOf(extra.authInfo).identity} ${echo}` }],
    }),
  );
}

async function start(register: (mcp: McpServer) => void = whoami): Promise<string> {
  credentials = new Credentials();
  const app = createApp({
    config: config(),
    verifier: tokenVerifier(config(), localKeys(key)),
    credentials,
    register,
    version: 'test',
  });
  server = app.listen(0, '127.0.0.1');
  await new Promise((resolve) => server?.once('listening', resolve));
  return `http://127.0.0.1:${String((server?.address() as AddressInfo).port)}`;
}

describe('the health check', () => {
  it('answers ok with the version, and needs no token', async () => {
    const base = await start();
    const response = await fetch(`${base}/health`);
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ status: 'ok', version: 'test' });
  });
});

describe('the protected-resource metadata', () => {
  it('advertises the canonical name, Aura as the authorization server, and the scope', async () => {
    const base = await start();
    const response = await fetch(`${base}/.well-known/oauth-protected-resource/mcp`);
    expect(await response.json()).toEqual({
      resource: 'http://127.0.0.1:8097/mcp',
      authorization_servers: ['http://127.0.0.1:9080'],
      scopes_supported: ['mcp:tools'],
      resource_name: 'aura-video-mcp',
    });
  });
});

describe('the MCP endpoint', () => {
  it('sends a caller with no token to the metadata of the name it dialled', async () => {
    const base = await start();
    const response = await fetch(`${base}/mcp`, { method: 'POST', body: '{}' });
    expect(response.status).toBe(401);
    expect(response.headers.get('www-authenticate')).toContain(
      `resource_metadata="${base}/.well-known/oauth-protected-resource/mcp"`,
    );
  });

  it('refuses a Host that is none of its names', async () => {
    const base = new URL(await start());
    // fetch drops a Host header (a forbidden header name in the Fetch standard); node:http sends it.
    const status = await new Promise<number | undefined>((resolve, reject) => {
      request(
        { hostname: base.hostname, port: base.port, path: '/health', headers: { Host: 'evil.example:8097' } },
        (response) => {
          response.resume();
          resolve(response.statusCode);
        },
      )
        .on('error', reject)
        .end();
    });
    expect(status).toBe(403);
  });

  it('runs a tool for a verified caller, and remembers that caller’s credential', async () => {
    const base = await start();
    const bearer = await token(key);
    const client = await mcpClient(base, bearer);
    const tools = await client.listTools();
    expect(tools.tools.map((tool) => tool.name)).toEqual(['whoami']);
    const result = await client.callTool({ name: 'whoami', arguments: { echo: 'hi' } });
    expect(result.content).toEqual([{ type: 'text', text: `${IDENTITY} hi` }]);
    expect(credentials.current(IDENTITY)).toBe(bearer);
    await client.close();
  });

  it('answers 500 and logs the reason when a call cannot be served, instead of leaving it hanging', async () => {
    const base = await start(() => {
      throw new Error('a tool registered twice');
    });
    const written: string[] = [];
    const spy = vi.spyOn(process.stdout, 'write').mockImplementation((chunk) => {
      written.push(String(chunk));
      return true;
    });
    try {
      const response = await fetch(`${base}/mcp`, {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${await token(key)}`,
          'Content-Type': 'application/json',
          Accept: 'application/json, text/event-stream',
        },
        body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list' }),
      });
      expect(response.status).toBe(500);
    } finally {
      spy.mockRestore();
    }
    expect(written.map((line) => JSON.parse(line) as Record<string, unknown>)).toContainEqual(
      expect.objectContaining({ level: 'error', message: 'an MCP request failed', identity: IDENTITY, reason: 'a tool registered twice' }),
    );
  });

  it('keeps no session to stream to or to end', async () => {
    const base = await start();
    expect((await fetch(`${base}/mcp`)).status).toBe(405);
    expect((await fetch(`${base}/mcp`, { method: 'DELETE' })).status).toBe(405);
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/http.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../src/http' imported from …`; `VITEST_RC=1`.

- [ ] **Step 3: Write the app**

`services/video-mcp/src/http.ts`:

```ts
// http.ts — the sidecar's HTTP surface, all of it from the MCP SDK (1.31.0): createMcpExpressApp
// with its Host allow-list, requireBearerAuth for the token, metadataHandler for RFC 9728
// protected-resource metadata, and a stateless streamable-HTTP transport per request
// (webStandardStreamableHttp.js:738: no `sessionIdGenerator`, no sessions). Stateless because
// every call is authenticated on its own and nothing lives in a session: projects live in Aura's
// library and jobs in the queue, both keyed by identity.

import { requireBearerAuth } from '@modelcontextprotocol/sdk/server/auth/middleware/bearerAuth.js';
import { metadataHandler } from '@modelcontextprotocol/sdk/server/auth/handlers/metadata.js';
import type { OAuthTokenVerifier } from '@modelcontextprotocol/sdk/server/auth/provider.js';
import { createMcpExpressApp } from '@modelcontextprotocol/sdk/server/express.js';
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js';
import type { Express, Request } from 'express';
import { callerOf, type Caller } from './auth';
import { MCP_PATH, METADATA_PATH, SCOPE, type Config } from './config';
import type { Credentials } from './credentials';
import { log } from './log';

const SERVER_NAME = 'aura-video-mcp';

export interface HttpParts {
  readonly config: Config;
  readonly verifier: OAuthTokenVerifier;
  readonly credentials: Credentials;
  /** Registers every tool on a fresh server; `caller` is read per call from its `authInfo`. */
  readonly register: (server: McpServer) => void;
  readonly version: string;
}

/** The URL this request reached the metadata under, so a client that dialled the loopback name
 *  and one that dialled the Compose name are each pointed at an address they can reach. */
function metadataUrl(request: Request): string {
  return `${request.protocol}://${request.get('host') ?? ''}${METADATA_PATH}`;
}

export function createApp(parts: HttpParts): Express {
  const { config, verifier, credentials } = parts;
  const hosts = [...new Set(config.resources.map((resource) => new URL(resource).hostname))];
  const app = createMcpExpressApp({ host: '0.0.0.0', allowedHosts: [...hosts, 'localhost'] });
  app.get('/health', (_request, response) => {
    response.json({ status: 'ok', version: parts.version });
  });
  app.use(
    METADATA_PATH,
    metadataHandler({
      resource: config.resources[0] ?? '',
      authorization_servers: [config.issuer],
      scopes_supported: [SCOPE],
      resource_name: SERVER_NAME,
    }),
  );
  app.post(
    MCP_PATH,
    (request, response, next) => {
      requireBearerAuth({
        verifier,
        requiredScopes: [SCOPE],
        resourceMetadataUrl: metadataUrl(request),
      })(request, response, next);
    },
    async (request, response) => {
      const caller: Caller = callerOf(request.auth);
      credentials.remember(caller);
      const server = new McpServer({ name: SERVER_NAME, version: parts.version });
      // No sessionIdGenerator: stateless. The SDK types the option `?: () => string` and its own
      // docs pass `undefined`, which exactOptionalPropertyTypes refuses; leaving it out is the same.
      const transport = new StreamableHTTPServerTransport({ enableJsonResponse: true });
      response.on('close', () => {
        void transport.close();
        void server.close();
      });
      try {
        parts.register(server);
        // The SDK declares its transports without exactOptionalPropertyTypes (`onclose?: () => void`
        // assigned `undefined`); the object is the Transport it is, under the stricter reading.
        await server.connect(transport as Transport);
        await transport.handleRequest(request, response, request.body);
      } catch (error) {
        log('error', 'an MCP request failed', {
          identity: caller.identity,
          reason: error instanceof Error ? error.message : String(error),
        });
        if (!response.headersSent) response.status(500).end();
      }
    },
  );
  // Stateless: there is no session to stream to or to end.
  app.get(MCP_PATH, (_request, response) => {
    response.status(405).set('Allow', 'POST').end();
  });
  app.delete(MCP_PATH, (_request, response) => {
    response.status(405).set('Allow', 'POST').end();
  });
  return app;
}
```

- [ ] **Step 4: Run it to see it pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/http.test.ts`

Expected: `Tests  7 passed (7)`, `VITEST_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 5: Commit**

Write `$W/msg-t7.txt`:

```text
feat(video-mcp): serve MCP behind Aura's bearer, with health and metadata

The HTTP surface is the MCP SDK's own: an Express app with a Host
allow-list, the bearer middleware, RFC 9728 protected-resource
metadata, and a stateless streamable-HTTP transport per request,
since every call is authenticated alone and nothing lives in a
session. /health says ok with the version, without a token. A request
that throws is logged and answered 500.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t7.txt services/video-mcp/src/http.ts services/video-mcp/test/support/sidecar.ts services/video-mcp/test/unit/http.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 8: The edit engine: the Studio's commands, all or nothing

`OPERATION` is a zod discriminated union of the 20 operations the spec names. Each is the Studio command of the same name, with the command's own arguments:
- clips: `add_clip`, `trim_clip`, `split_at`, `remove_range`, `move_clip`, `set_muted`, `set_clip_presentation`, `set_junction_transition`;
- the frame: `set_frame_size`;
- overlays and items: `add_overlay`, `set_property`, `remove_item`;
- sound: `add_audio`, `extract_audio`, `move_audio`, `trim_audio`, `split_audio`, `set_audio_properties`, `set_envelope`;
- `add_speech`.

`add_clip` and `add_audio` take a source by exactly one of `assetId` (resolved and probed once, Task 6) or `sourceId` (already in the project). `add_speech {text, voice?, language?, time, label?}` asks Aura's TTS through the internal API and places the voice with `add_audio`. It is `time`, as `add_audio` names it, not the spec's "where".

`applyOperations` applies the list in order, each against what the one before left.
- On success it answers one line per operation: `<op> <target>: <before => after>` from `describeChange`.
- On the first refusal it answers `{refused}` and nothing changes: "operation i of n, <op> <target>: <sentence>". When speech was already synthesized, the answer says which assets stay in the library.

`argsOf` strips `op` and `undefined` fields before a command sees them. Without it, zod's `| undefined` would reach the Studio under `exactOptionalPropertyTypes`, and `set_clip_presentation` would have written `op` onto the clip.

`timelineSummary` is what `video_project_open` answers:
- the frame;
- every source;
- every clip, overlay and sound, with its id, window, source, volume, fades, ducking and transition;
- the analyses the next render will make.

**Files:**
- Create: `services/video-mcp/src/operations.ts`, `services/video-mcp/src/changes.ts`, `services/video-mcp/src/summary.ts`
- Test: `services/video-mcp/test/support/door.ts`, `services/video-mcp/test/unit/operations.test.ts`, `services/video-mcp/test/unit/summary.test.ts`

**Interfaces:**
- Consumes: the seam (Task 5); `SourceDoor`, `notInLibrary` (Task 6); `Refusal`, `refusalSentence` (Task 5).
- Produces:
  - `MAX_OPERATIONS = 50`, `OPERATION` (the zod union), `type Operation`;
  - `type Edit = { refused: undefined; project: VideoProject; lines: readonly string[] } | { refused: string }`;
  - `applyOperations(project, operations: readonly Operation[], door: SourceDoor): Promise<Edit>`;
  - `describeChange(before: VideoProject, after: VideoProject): string`;
  - `timelineSummary(project: VideoProject): string` and `missingAnalyses(project: VideoProject): string[]`.
- Test support (`test/support/door.ts`): `fakeDoor(library): FakeDoor` and `VOICE_ASSET`.

- [ ] **Step 1: Write the failing tests and the fake door**

`services/video-mcp/test/support/door.ts`:

```ts
// A SourceDoor over a fixed library, for the edit engine's tests: each asset id answers the probe
// it was given, an unknown one is not in the library, and every probe and synthesis is counted.
import { Refusal } from '../../src/refusal';
import { notInLibrary, type SourceDoor } from '../../src/sources';
import { sourceFrom, type ProbedSource } from '../../src/studio';

export interface FakeDoor extends SourceDoor {
  readonly probed: string[];
  readonly said: string[];
}

export const VOICE_ASSET = '7d5b6a8f-9e0d-4cb4-8dc5-d6e7f8091a2b';

export function fakeDoor(library: Readonly<Record<string, ProbedSource | Refusal>>): FakeDoor {
  const probed: string[] = [];
  const said: string[] = [];
  const spoken: string[] = [];
  const probe = (assetId: string): ProbedSource => {
    probed.push(assetId);
    const answer = library[assetId];
    if (answer === undefined) throw notInLibrary(assetId);
    if (answer instanceof Refusal) throw answer;
    return answer;
  };
  return {
    probed,
    said,
    spoken,
    async source(project, assetId) {
      const known = project.sources.find((candidate) => candidate.assetId === assetId);
      if (known !== undefined) return { project, source: known };
      const source = sourceFrom(probe(assetId), assetId);
      return { project: { ...project, sources: [...project.sources, source] }, source };
    },
    async picture(assetId) {
      if (probe(assetId).kind !== 'image') throw new Refusal(`asset ${assetId} is not a picture`);
    },
    async speech(request) {
      said.push(request.text);
      spoken.push(VOICE_ASSET);
      return VOICE_ASSET;
    },
  };
}
```

`services/video-mcp/test/unit/operations.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { applyOperations, OPERATION, type Operation } from '../../src/operations';
import { HEVC_REFUSAL } from '../../src/probe';
import { Refusal } from '../../src/refusal';
import { audioTracks, IMAGE_SECONDS } from '../../src/studio';
import { fakeDoor, VOICE_ASSET } from '../support/door';
import { MUSIC, sampleProject } from '../support/projects';

const BEACH = '8e6c7b90-af1e-4dc5-9ed6-e7f8091a2b3c';
const POSTER = '9f7d8ca1-b02f-4ed6-8fe7-f8091a2b3c4d';
const SONG = 'a08e9db2-c13a-4fe7-9af8-091a2b3c4d5e';
const SILENT = 'b19faec3-d24b-4af8-8b09-1a2b3c4d5e6f';
const PHONE = 'c2a0bfd4-e35c-4b09-9c1a-2b3c4d5e6f70';

const library = {
  [BEACH]: { kind: 'video', duration: 12, width: 1080, height: 1920, hasAudio: true },
  [POSTER]: { kind: 'image', duration: 0, width: 1080, height: 1920 },
  [SONG]: { kind: 'audio', duration: 40, width: 0, height: 0 },
  [SILENT]: { kind: 'video', duration: 6, width: 1920, height: 1080, hasAudio: false },
  [PHONE]: new Refusal(HEVC_REFUSAL),
} as const;

const ops = (...list: unknown[]): Operation[] => list.map((item) => OPERATION.parse(item));

describe('an edit', () => {
  it('applies every operation in order and says what each one did', async () => {
    const edit = await applyOperations(
      sampleProject(),
      ops(
        { op: 'trim_clip', clipId: 'c1', start: 0, end: 6 },
        { op: 'set_audio_properties', itemId: 'm1', volume: 0.3 },
      ),
      fakeDoor(library),
    );
    expect(edit.refused).toBeUndefined();
    if (edit.refused !== undefined) return;
    expect(edit.lines).toEqual([
      'trim_clip c1: clip c1 duration: 8 => 6',
      'set_audio_properties m1: sound m1 volume: 0.8 => 0.3',
    ]);
    expect(edit.project.video.map((clip) => clip.duration)).toEqual([6, 6]);
  });

  it('is all or nothing: a refusal leaves the project as it was and names the operation', async () => {
    const project = sampleProject();
    const edit = await applyOperations(
      project,
      ops(
        { op: 'set_audio_properties', itemId: 'm1', volume: 0.3 },
        { op: 'trim_clip', clipId: 'c1', start: 0, end: 30 },
      ),
      fakeDoor(library),
    );
    expect(edit).toEqual({
      refused:
        'operation 2 of 2, trim_clip c1: A clip cannot play more than its source holds. Accepted: start ≥ -2 s and end ≤ 18 s, measured from where it starts in its 20 s source.',
    });
    expect(project).toEqual(sampleProject());
  });

  it('runs every Studio command it names, each against what the one before left', async () => {
    const list = ops(
      { op: 'set_audio_properties', itemId: 'm1', volume: 0.5 },
      { op: 'split_audio', itemId: 'm1', time: 3 },
      { op: 'trim_audio', itemId: 'm1', start: 0, end: 2 },
      { op: 'set_envelope', itemId: 'm1', points: [{ time: 0, gain: 1 }, { time: 1, gain: 0.5 }] },
      { op: 'move_audio', itemId: 'm1', start: 0.5 },
      { op: 'extract_audio', clipId: 'c1' },
      { op: 'set_muted', clipId: 'c2', muted: false },
      { op: 'set_clip_presentation', clipId: 'c1', opacity: 0.8, fadeOut: true },
      { op: 'move_clip', clipId: 'c2', toIndex: 0 },
      { op: 'set_junction_transition', fromClipId: 'c2', toClipId: 'c1', transition: 'crossfade', duration: 0.5 },
      { op: 'set_frame_size', width: 1080, height: 1350 },
      { op: 'set_property', itemId: 't1', key: 'text', value: 'Aura' },
      { op: 'split_at', time: 2 },
      { op: 'remove_range', from: 0, to: 1 },
      { op: 'remove_item', itemId: 't1' },
    );
    const edit = await applyOperations(sampleProject(), list, fakeDoor(library));
    if (edit.refused !== undefined) throw new Error(edit.refused);
    expect(edit.lines.map((line) => line.split(':')[0])).toEqual([
      'set_audio_properties m1',
      'split_audio m1',
      'trim_audio m1',
      'set_envelope m1',
      'move_audio m1',
      'extract_audio c1',
      'set_muted c2',
      'set_clip_presentation c1',
      'move_clip c2',
      'set_junction_transition',
      'set_frame_size',
      'set_property t1',
      'split_at',
      'remove_range',
      'remove_item t1',
    ]);
    expect(edit.lines).not.toContain(expect.stringMatching(/: no change$/));
    expect(edit.lines[10]).toBe('set_frame_size: size: 1080×1920 => 1080×1350');
    expect(edit.project.video[0]?.sourceId).toBe('src-screen');
  });

  it('names a lone refused operation without counting it', async () => {
    const edit = await applyOperations(sampleProject(), ops({ op: 'remove_item', itemId: 'nope' }), fakeDoor(library));
    expect(edit).toEqual({ refused: 'remove_item nope: no overlay item named nope.' });
  });

  it('lets an error that is not a refusal fail the call, as the bug it is', async () => {
    const door = fakeDoor(library);
    door.source = () => Promise.reject(new TypeError('cannot read properties of undefined'));
    await expect(
      applyOperations(sampleProject(), ops({ op: 'add_clip', assetId: BEACH }), door),
    ).rejects.toThrow(TypeError);
  });
});

describe('a source named by asset id', () => {
  it('is probed once, joins the project, and plays its whole length by default', async () => {
    const door = fakeDoor(library);
    const edit = await applyOperations(
      sampleProject(),
      ops({ op: 'add_clip', assetId: BEACH }, { op: 'add_clip', assetId: BEACH, sourceStart: 2, duration: 3 }),
      door,
    );
    if (edit.refused !== undefined) throw new Error(edit.refused);
    expect(door.probed).toEqual([BEACH]);
    const added = edit.project.sources.filter((source) => source.assetId === BEACH);
    expect(added).toHaveLength(1);
    expect(edit.project.video.slice(2).map((clip) => [clip.duration, clip.sourceStart])).toEqual([
      [12, 0],
      [3, 2],
    ]);
    expect(edit.lines[0]).toMatch(/^add_clip 8e6c7b90-af1e-4dc5-9ed6-e7f8091a2b3c: \+ source .+ \(asset 8e6c7b90-af1e-4dc5-9ed6-e7f8091a2b3c, video\); \+ clip .+ \(source .+, 12 s\)$/);
  });

  it('is a still for the Studio’s five seconds unless the operation says otherwise', async () => {
    const edit = await applyOperations(sampleProject(), ops({ op: 'add_clip', assetId: POSTER }), fakeDoor(library));
    if (edit.refused !== undefined) throw new Error(edit.refused);
    expect(edit.project.video.at(-1)?.duration).toBe(IMAGE_SECONDS);
  });

  it('is refused as a clip when it is a sound, and named', async () => {
    const edit = await applyOperations(sampleProject(), ops({ op: 'add_clip', assetId: SONG }), fakeDoor(library));
    expect(edit).toEqual({ refused: `add_clip ${SONG}: ${SONG} is a sound: put it on an audio lane with add_audio` });
  });

  it('is refused in HEVC with the spec’s sentence', async () => {
    const edit = await applyOperations(sampleProject(), ops({ op: 'add_clip', assetId: PHONE }), fakeDoor(library));
    expect(edit).toEqual({ refused: `add_clip ${PHONE}: ${HEVC_REFUSAL}` });
  });

  it('is refused when the library does not have it', async () => {
    const missing = 'd3b1c0e5-f46d-4c1a-8d2b-3c4d5e6f7081';
    const edit = await applyOperations(sampleProject(), ops({ op: 'add_audio', assetId: missing, time: 0 }), fakeDoor(library));
    expect(edit).toEqual({
      refused: `add_audio ${missing}: asset ${missing} is not in your library, or its upload has not finished`,
    });
  });

  it('cannot lend a sound it does not have: ffprobe’s hasAudio refuses it at edit time', async () => {
    const door = fakeDoor(library);
    const edit = await applyOperations(
      sampleProject(),
      ops({ op: 'add_clip', assetId: SILENT }, { op: 'add_audio', assetId: SILENT, time: 1 }),
      door,
    );
    expect(edit.refused).toMatch(
      /^operation 2 of 2, add_audio b19faec3-d24b-4af8-8b09-1a2b3c4d5e6f: .+ Accepted: a sound, or a clip whose source has an audio track\.$/,
    );
  });

  it('is named by its project id too, and an id the project lacks lists the ones it has', async () => {
    const played = await applyOperations(sampleProject(), ops({ op: 'add_audio', sourceId: MUSIC.id, time: 9 }), fakeDoor(library));
    if (played.refused !== undefined) throw new Error(played.refused);
    expect(audioTracks(played.project).flatMap((lane) => lane.items)).toHaveLength(2);
    const unknown = await applyOperations(sampleProject(), ops({ op: 'add_clip', sourceId: 'src-x' }), fakeDoor(library));
    expect(unknown).toEqual({
      refused: 'add_clip src-x: the project has no source src-x; its sources are src-camera, src-screen, src-music, src-still',
    });
  });
});

describe('speech', () => {
  it('is Aura’s voice put on a free lane, labelled with its words', async () => {
    const door = fakeDoor({ ...library, [VOICE_ASSET]: { kind: 'audio', duration: 3.2, width: 0, height: 0 } });
    const edit = await applyOperations(
      sampleProject(),
      ops({ op: 'add_speech', text: 'Aura remembers what matters.', language: 'en', time: 1 }),
      door,
    );
    if (edit.refused !== undefined) throw new Error(edit.refused);
    expect(door.said).toEqual(['Aura remembers what matters.']);
    const voice = audioTracks(edit.project).flatMap((lane) => lane.items).find((item) => item.label !== undefined);
    expect(voice).toMatchObject({ label: 'Aura remembers what matters.', duration: 3.2 });
  });

  it('already synthesized stays in the library when a later operation is refused, and says so', async () => {
    const door = fakeDoor({ ...library, [VOICE_ASSET]: { kind: 'audio', duration: 3.2, width: 0, height: 0 } });
    const edit = await applyOperations(
      sampleProject(),
      ops({ op: 'add_speech', text: 'hello', time: 1 }, { op: 'split_at', time: 0 }),
      door,
    );
    expect(edit.refused).toMatch(
      new RegExp(`^operation 2 of 2, split_at: .+ The speech already synthesized stays in your library: ${VOICE_ASSET}\\.$`),
    );
  });
});

describe('an image overlay', () => {
  it('shows a picture of the caller’s library', async () => {
    const edit = await applyOperations(
      sampleProject(),
      ops({ op: 'add_overlay', kind: 'image', anchor: { clipId: 'c2', offset: 0 }, duration: 2, props: { assetId: POSTER, scale: 0.5 } }),
      fakeDoor(library),
    );
    expect(edit.refused).toBeUndefined();
  });

  it('is refused without a picture, or with one that is not a picture', async () => {
    const none = await applyOperations(
      sampleProject(),
      ops({ op: 'add_overlay', kind: 'image', anchor: { clipId: 'c2', offset: 0 }, duration: 2, props: {} }),
      fakeDoor(library),
    );
    expect(none).toEqual({ refused: 'add_overlay: an image overlay names its picture in props.assetId' });
    const song = await applyOperations(
      sampleProject(),
      ops({ op: 'add_overlay', kind: 'image', anchor: { clipId: 'c2', offset: 0 }, duration: 2, props: { assetId: SONG } }),
      fakeDoor(library),
    );
    expect(song).toEqual({ refused: `add_overlay: asset ${SONG} is not a picture` });
  });
});

describe('the operation schema', () => {
  it('takes a source by exactly one of its two names', () => {
    expect(OPERATION.safeParse({ op: 'add_clip', assetId: BEACH, sourceId: 'src-camera' }).success).toBe(false);
    expect(OPERATION.safeParse({ op: 'add_clip' }).success).toBe(false);
  });

  it('refuses an argument no command has, and an op that is not one', () => {
    expect(OPERATION.safeParse({ op: 'trim_clip', clipId: 'c1', start: 0, end: 2, ripple: false }).success).toBe(false);
    expect(OPERATION.safeParse({ op: 'delete_project' }).success).toBe(false);
  });

  it('takes every Studio command by its name', () => {
    const every = [
      { op: 'add_clip', sourceId: 'src-camera' },
      { op: 'trim_clip', clipId: 'c1', start: 0, end: 1 },
      { op: 'split_at', time: 1 },
      { op: 'remove_range', from: 0, to: 1 },
      { op: 'move_clip', clipId: 'c1', toIndex: 1 },
      { op: 'set_muted', clipId: 'c1', muted: true },
      { op: 'set_clip_presentation', clipId: 'c1', opacity: 0.5, transitionIn: 'fade' },
      { op: 'set_junction_transition', fromClipId: 'c1', toClipId: 'c2', transition: 'crossfade', duration: 0.5 },
      { op: 'set_frame_size', width: 1080, height: 1920 },
      { op: 'add_overlay', kind: 'text', anchor: { clipId: 'c1', offset: 0 }, duration: 2, props: { text: 'hi' } },
      { op: 'set_property', itemId: 't1', key: 'text', value: 'hello' },
      { op: 'remove_item', itemId: 't1' },
      { op: 'add_audio', sourceId: 'src-music', time: 0 },
      { op: 'extract_audio', clipId: 'c1' },
      { op: 'move_audio', itemId: 'm1', start: 1 },
      { op: 'trim_audio', itemId: 'm1', start: 0, end: 5 },
      { op: 'split_audio', itemId: 'm1', time: 3 },
      { op: 'set_audio_properties', itemId: 'm1', ducking: { amountDb: -12, ramp: 0.5 }, denoise: true },
      { op: 'set_envelope', itemId: 'm1', points: [{ time: 0, gain: 1 }] },
      { op: 'add_speech', text: 'hi', time: 0 },
    ];
    expect(every.map((operation) => OPERATION.safeParse(operation).success)).toEqual(every.map(() => true));
  });
});
```

`services/video-mcp/test/unit/summary.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { describeChange } from '../../src/changes';
import {
  addAudio,
  addOverlay,
  extractAudio,
  removeItem,
  setAudioProperties,
  setClipPresentation,
  setEnvelope,
  setJunctionTransition,
  splitAt,
} from '../../src/studio';
import { missingAnalyses, timelineSummary } from '../../src/summary';
import { sampleProject, STILL } from '../support/projects';

describe('the timeline summary', () => {
  it('gives every item its id, its window in the film, its source and how it plays', () => {
    expect(timelineSummary(sampleProject())).toBe(
      [
        '"Aura memory" 1080×1920 at 30 fps, 14 s',
        'sources:',
        '  src-camera  video 20 s 1920×1080, with sound  asset 3f1d2c4b-5a69-4e70-8f81-92a3b4c5d6e7',
        '  src-screen  video 12 s 1916×878, no sound  asset 4a2e3d5c-6b7a-4f81-9a92-a3b4c5d6e7f8',
        '  src-music  audio 30 s  asset 5b3f4e6d-7c8b-4a92-8ba3-b4c5d6e7f809',
        '  src-still  image 1080×1920  asset 6c4a5f7e-8d9c-4ba3-9cb4-c5d6e7f8091a',
        'clips, in order:',
        '  c1  0–8 s  src-camera from 2 s',
        '  c2  8–14 s  src-screen from 0 s  muted',
        'overlays:',
        '  t1  1–4 s  lane titles  text on c1 +1 s  "Aura remembers"',
        'sounds:',
        '  m1  0–14 s  lane music-lane  src-music from 0 s  volume 0.8',
        'analyses: none missing',
      ].join('\n'),
    );
  });

  it('names the analyses a render still has to make, and what asked for each', () => {
    let project = setAudioProperties(sampleProject(), { itemId: 'm1', ducking: { amountDb: -12, ramp: 0.5 }, fadeIn: 1 });
    project = setClipPresentation(project, { clipId: 'c1', denoise: true });
    expect(missingAnalyses(project)).toEqual([
      'speech windows of src-camera (ducking)',
      'cleaned copy of src-camera (noise reduction)',
    ]);
    const summary = timelineSummary(project);
    expect(summary).toContain('  c1  0–8 s  src-camera from 2 s  noise reduction');
    expect(summary).toContain('volume 0.8  fade in 1 s  ducks -12 dB over 0.5 s under speech');
    expect(summary).toContain(
      'analyses the next render makes first: speech windows of src-camera (ducking); cleaned copy of src-camera (noise reduction)',
    );
  });

  it('says how each clip and sound plays when it is not plainly', () => {
    let project = setClipPresentation(sampleProject(), { clipId: 'c1', volume: 1.5, speed: 2, fadeIn: true, fadeOut: true });
    project = setJunctionTransition(project, { fromClipId: 'c1', toClipId: 'c2', transition: 'fadeBlack', duration: 0.5 });
    project = setAudioProperties(project, { itemId: 'm1', muted: true, speed: 0.5, fadeOut: 2 });
    project = setEnvelope(project, { itemId: 'm1', points: [{ time: 0, gain: 1 }, { time: 2, gain: 0.5 }] });
    project = addOverlay(project, { kind: 'image', anchor: { clipId: 'c2', offset: 0 }, duration: 1, props: { assetId: STILL.assetId } });
    project = addAudio(project, { sourceId: 'src-camera', time: 1, label: 'room tone' });
    const summary = timelineSummary(project);
    expect(summary).toContain('  c1  0–4 s  src-camera from 2 s  volume 1.5  speed 2  fades in  fades out');
    expect(summary).toContain('src-screen from 0 s  muted  fadeBlack 0.5 s from c1');
    expect(summary).toMatch(/m1 .+ muted {2}fade out 2 s {2}speed 0\.5 {2}envelope of 2 points/);
    expect(summary).toMatch(/ {2}image on c2 \+0 s {2}asset 6c4a5f7e-8d9c-4ba3-9cb4-c5d6e7f8091a/);
    expect(summary).toContain('"room tone"');
    const extracted = timelineSummary(extractAudio(sampleProject(), { clipId: 'c1' }));
    expect(extracted).toMatch(/extracted from c1/);
  });

  it('says none for an empty lane rather than leaving the reader to guess', () => {
    const empty = { ...sampleProject(), video: [], overlays: [], audio: [] };
    expect(timelineSummary(empty)).toContain('clips, in order:\n  none\noverlays:\n  none\nsounds:\n  none');
  });
});

describe('what an operation changed', () => {
  it('is each changed field as before => after, item by item', () => {
    const before = sampleProject();
    expect(describeChange(before, setAudioProperties(before, { itemId: 'm1', volume: 0.3, fadeOut: 2 }))).toBe(
      'sound m1 volume: 0.8 => 0.3; sound m1 fadeOut: unset => 2',
    );
  });

  it('names what an operation added, with the id the next operation needs, and what it took away', () => {
    const before = sampleProject();
    const split = splitAt(before, { time: 4 });
    const added = split.video.find((clip) => clip.id !== 'c1' && clip.id !== 'c2');
    expect(describeChange(before, split)).toBe(
      `clip c1 duration: 8 => 4; + clip ${String(added?.id)} (source src-camera, 4 s)`,
    );
    expect(describeChange(before, removeItem(before, { itemId: 'c1' }))).toContain('- clip c1; - overlay t1');
  });

  it('is no change when nothing changed', () => {
    expect(describeChange(sampleProject(), sampleProject())).toBe('no change');
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/operations.test.ts test/unit/summary.test.ts`

Expected:
- `operations.test.ts` fails with `Error: Cannot find module '../../src/operations' imported from …`;
- `summary.test.ts` fails with `Error: Cannot find module '../../src/changes' imported from …`;
- `VITEST_RC=1`.

- [ ] **Step 3: Write the engine, the change lines and the summary**

`services/video-mcp/src/changes.ts`:

```ts
// changes.ts — what one operation did, as `before => after` (spec §Tools: `volume: 1 => 0.3`).
// It reads the two projects rather than the operation, so the line says what the Studio's command
// really changed: a trim's clamped fades, a removal that took its overlays with it, a split's new
// item and its id, which the caller needs for the next operation.

import { audioTracks, type VideoProject } from './studio';

type Fields = Record<string, unknown>;

interface Entity {
  readonly brief: string;
  readonly fields: Fields;
}

function shown(value: unknown): string {
  if (value === undefined) return 'unset';
  if (typeof value === 'number') return String(Math.round(value * 1000) / 1000);
  if (typeof value === 'string' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
}

/** Every item the project holds, keyed by what a line calls it. */
function entities(project: VideoProject): Map<string, Entity> {
  const all = new Map<string, Entity>();
  all.set('frame', {
    brief: '',
    fields: { size: `${String(project.size.width)}×${String(project.size.height)}`, fps: project.fps, name: project.name },
  });
  for (const source of project.sources) {
    const { id, ...fields } = source;
    all.set(`source ${id}`, { brief: `asset ${source.assetId}, ${source.kind}`, fields });
  }
  for (const clip of project.video) {
    const { id, ...fields } = clip;
    all.set(`clip ${id}`, { brief: `source ${clip.sourceId}, ${shown(clip.duration)} s`, fields });
  }
  for (const lane of project.overlays) {
    for (const item of lane.items) {
      const { id, props, ...rest } = item;
      const fields: Fields = { lane: lane.id, ...rest };
      for (const [key, value] of Object.entries(props)) fields[`props.${key}`] = value;
      all.set(`overlay ${id}`, { brief: `${item.kind} on ${item.anchor.clipId}`, fields });
    }
  }
  for (const lane of audioTracks(project)) {
    for (const item of lane.items) {
      const { id, ...fields } = item;
      all.set(`sound ${id}`, { brief: `source ${item.sourceId}, lane ${lane.id}`, fields: { lane: lane.id, ...fields } });
    }
  }
  return all;
}

/** One line of what changed between two projects, item by item, or `no change`. */
export function describeChange(before: VideoProject, after: VideoProject): string {
  const was = entities(before);
  const now = entities(after);
  const parts: string[] = [];
  for (const [name, entity] of now) {
    const old = was.get(name);
    if (old === undefined) {
      parts.push(`+ ${name} (${entity.brief})`);
      continue;
    }
    const keys = new Set([...Object.keys(old.fields), ...Object.keys(entity.fields)]);
    for (const key of keys) {
      const from = shown(old.fields[key]);
      const to = shown(entity.fields[key]);
      if (from !== to) parts.push(`${name === 'frame' ? '' : `${name} `}${key}: ${from} => ${to}`);
    }
  }
  for (const name of was.keys()) if (!now.has(name)) parts.push(`- ${name}`);
  return parts.length === 0 ? 'no change' : parts.join('; ');
}
```

`services/video-mcp/src/summary.ts`:

```ts
// summary.ts — the timeline as a caller reads it (spec §Tools, video_project_open): every item's
// id, its start and end in the film, its source, volume, fades, ducking, transitions and overlays,
// and the analyses a render still has to make. Times are the film's, computed by the Studio's own
// rules (clipStarts, overlayWindow, audioWindow), so they are the ones the export plays.

import {
  audioTracks,
  audioWindow,
  clipStarts,
  clipTimelineDuration,
  overlayWindow,
  projectDuration,
  uncleanedSources,
  unheardByDucking,
  type AudioItem,
  type ProjectSource,
  type VideoItem,
  type VideoProject,
} from './studio';

const s = (value: number): string => `${String(Math.round(value * 1000) / 1000)} s`;
const span = (start: number, end: number): string =>
  `${String(Math.round(start * 1000) / 1000)}–${String(Math.round(end * 1000) / 1000)} s`;

function sourceLine(source: ProjectSource): string {
  const size = source.kind === 'audio' ? '' : ` ${String(source.size.width)}×${String(source.size.height)}`;
  const sound = source.kind !== 'video' ? '' : source.hasAudio === false ? ', no sound' : ', with sound';
  const length = source.kind === 'image' ? '' : ` ${s(source.duration)}`;
  return `  ${source.id}  ${source.kind}${length}${size}${sound}  asset ${source.assetId}`;
}

function clipLine(clip: VideoItem, start: number): string {
  const parts = [
    `  ${clip.id}  ${span(start, start + clipTimelineDuration(clip))}`,
    `${clip.sourceId} from ${s(clip.sourceStart)}`,
  ];
  if (clip.muted) parts.push('muted');
  else if (clip.volume !== undefined) parts.push(`volume ${String(clip.volume)}`);
  if (clip.speed !== undefined) parts.push(`speed ${String(clip.speed)}`);
  if (clip.fadeIn === true) parts.push('fades in');
  if (clip.fadeOut === true) parts.push('fades out');
  if (clip.junctionTransition !== undefined && clip.junctionTransition !== 'none') {
    parts.push(`${clip.junctionTransition} ${s(clip.junctionDuration ?? 1)} from ${String(clip.junctionFromClipId)}`);
  }
  if (clip.denoise === true) parts.push('noise reduction');
  return parts.join('  ');
}

function soundLine(project: VideoProject, lane: string, item: AudioItem): string {
  const window = audioWindow(project, item);
  const parts = [`  ${item.id}  ${span(window.start, window.end)}`, `lane ${lane}`, `${item.sourceId} from ${s(item.sourceStart)}`];
  parts.push(item.muted ? 'muted' : `volume ${String(item.volume)}`);
  if (item.fadeIn !== undefined) parts.push(`fade in ${s(item.fadeIn)}`);
  if (item.fadeOut !== undefined) parts.push(`fade out ${s(item.fadeOut)}`);
  if (item.ducking !== undefined) {
    parts.push(`ducks ${String(item.ducking.amountDb)} dB over ${s(item.ducking.ramp)} under speech`);
  }
  if (item.speed !== undefined) parts.push(`speed ${String(item.speed)}`);
  if (item.envelope !== undefined) parts.push(`envelope of ${String(item.envelope.length)} points`);
  if (item.denoise === true) parts.push('noise reduction');
  if (item.extractedFrom !== undefined) parts.push(`extracted from ${item.extractedFrom}`);
  if (item.label !== undefined) parts.push(`"${item.label}"`);
  return parts.join('  ');
}

/** What a render still has to compute before it can play the project as asked. */
export function missingAnalyses(project: VideoProject): string[] {
  return [
    ...unheardByDucking(project).map((source) => `speech windows of ${source.id} (ducking)`),
    ...uncleanedSources(project).map((source) => `cleaned copy of ${source.id} (noise reduction)`),
  ];
}

export function timelineSummary(project: VideoProject): string {
  const lines = [
    `"${project.name}" ${String(project.size.width)}×${String(project.size.height)} at ${String(project.fps)} fps, ${s(projectDuration(project))}`,
    'sources:',
    ...(project.sources.length === 0 ? ['  none'] : project.sources.map(sourceLine)),
    'clips, in order:',
  ];
  const starts = clipStarts(project);
  lines.push(...(project.video.length === 0 ? ['  none'] : project.video.map((clip, index) => clipLine(clip, starts[index] ?? 0))));
  lines.push('overlays:');
  const overlays = project.overlays.flatMap((lane) =>
    lane.items.map((item) => {
      const window = overlayWindow(project, item.anchor, item.duration);
      const text = typeof item.props.text === 'string' ? `  "${item.props.text}"` : '';
      const picture = typeof item.props.assetId === 'string' ? `  asset ${item.props.assetId}` : '';
      return `  ${item.id}  ${span(window.start, window.end)}  lane ${lane.id}  ${item.kind} on ${item.anchor.clipId} +${s(item.anchor.offset)}${text}${picture}`;
    }),
  );
  lines.push(...(overlays.length === 0 ? ['  none'] : overlays));
  lines.push('sounds:');
  const sounds = audioTracks(project).flatMap((lane) => lane.items.map((item) => soundLine(project, lane.id, item)));
  lines.push(...(sounds.length === 0 ? ['  none'] : sounds));
  const missing = missingAnalyses(project);
  lines.push(
    missing.length === 0
      ? 'analyses: none missing'
      : `analyses the next render makes first: ${missing.join('; ')}`,
  );
  return lines.join('\n');
}
```

`services/video-mcp/src/operations.ts`:

```ts
// operations.ts — `video_project_edit`'s operations: exactly the Studio's commands, named after
// them, with the commands' own arguments (web/src/videoStudio/commands*.ts), plus `add_speech`.
// A source enters by asset id: the sidecar probes it once and adds it to the project the way the
// Studio's library does (sourceEdit.ts sourceFrom), and an asset the project already plays is
// reused rather than probed again. The list is all or nothing: every operation runs against the
// project the previous one left, and a refusal anywhere returns the project untouched.

import { z } from 'zod';
import { describeChange } from './changes';
import { Refusal } from './refusal';
import { refusalSentence } from './sentences';
import type { SourceDoor } from './sources';
import {
  addAudio,
  addClip,
  addOverlay,
  extractAudio,
  IMAGE_SECONDS,
  moveAudio,
  moveClip,
  removeItem,
  removeRange,
  setAudioProperties,
  setClipPresentation,
  setEnvelope,
  setFrameSize,
  setJunctionTransition,
  setMuted,
  setProperty,
  splitAt,
  splitAudio,
  trimAudio,
  trimClip,
  type ProjectSource,
  type VideoProject,
} from './studio';

/** One edit's reach: a whole reel's worth of changes in one call, and no more. */
export const MAX_OPERATIONS = 50;

const seconds = z.number().finite();
const id = z.string().min(1).max(200);
const assetId = z.uuid();
const CLIP_TRANSITIONS = [
  'none', 'fade', 'blurResolve', 'zoom', 'slideUp', 'slideDown', 'slideLeft', 'slideRight',
  'overshootPop', 'glitchResolve', 'wipeReveal', 'lightSweepReveal',
] as const;
const JUNCTIONS = ['none', 'crossfade', 'fadeBlack', 'fadeWhite', 'zoom', 'blur'] as const;

/** A source named by the asset it plays (probed and added) or by its id in the project. */
const sourceRef = { assetId: assetId.optional(), sourceId: id.optional() };
const oneSource = (value: { assetId?: string | undefined; sourceId?: string | undefined }) =>
  (value.assetId === undefined) !== (value.sourceId === undefined);
const ONE_SOURCE = { message: 'name the source by exactly one of assetId and sourceId' };

export const OPERATION = z.discriminatedUnion('op', [
  z
    .strictObject({
      op: z.literal('add_clip'),
      ...sourceRef,
      duration: z.number().positive().optional(),
      sourceStart: z.number().min(0).optional(),
      atIndex: z.number().int().min(0).optional(),
    })
    .refine(oneSource, ONE_SOURCE),
  z.strictObject({ op: z.literal('trim_clip'), clipId: id, start: seconds, end: seconds }),
  z.strictObject({ op: z.literal('split_at'), time: seconds }),
  z.strictObject({ op: z.literal('remove_range'), from: seconds, to: seconds }),
  z.strictObject({ op: z.literal('move_clip'), clipId: id, toIndex: z.number().int().min(0) }),
  z.strictObject({ op: z.literal('set_muted'), clipId: id, muted: z.boolean() }),
  z.strictObject({
    op: z.literal('set_clip_presentation'),
    clipId: id,
    volume: z.number().optional(),
    denoise: z.boolean().optional(),
    rotation: z.union([z.literal(0), z.literal(90), z.literal(180), z.literal(270)]).optional(),
    fit: z.enum(['contain', 'cover']).optional(),
    flipX: z.boolean().optional(),
    flipY: z.boolean().optional(),
    brightness: z.number().optional(),
    contrast: z.number().optional(),
    saturation: z.number().optional(),
    hue: z.number().optional(),
    blur: z.number().optional(),
    opacity: z.number().optional(),
    fadeIn: z.boolean().optional(),
    fadeOut: z.boolean().optional(),
    speed: z.number().optional(),
    transitionIn: z.enum(CLIP_TRANSITIONS).optional(),
    transitionOut: z.enum(CLIP_TRANSITIONS).optional(),
    transitionInDuration: z.number().optional(),
    transitionOutDuration: z.number().optional(),
  }),
  z.strictObject({
    op: z.literal('set_junction_transition'),
    fromClipId: id,
    toClipId: id,
    transition: z.enum(JUNCTIONS),
    duration: z.number().optional(),
  }),
  z.strictObject({
    op: z.literal('set_frame_size'),
    width: z.number().int().min(16).max(3840),
    height: z.number().int().min(16).max(3840),
  }),
  z.strictObject({
    op: z.literal('add_overlay'),
    kind: z.enum(['text', 'image']),
    anchor: z.strictObject({ clipId: id, offset: z.number().min(0) }),
    duration: z.number().positive(),
    props: z.record(z.string(), z.unknown()),
    trackId: id.optional(),
  }),
  z.strictObject({ op: z.literal('set_property'), itemId: id, key: z.string().min(1), value: z.unknown() }),
  z.strictObject({ op: z.literal('remove_item'), itemId: id }),
  z
    .strictObject({
      op: z.literal('add_audio'),
      ...sourceRef,
      time: z.number().min(0),
      label: z.string().max(200).optional(),
    })
    .refine(oneSource, ONE_SOURCE),
  z.strictObject({ op: z.literal('extract_audio'), clipId: id }),
  z.strictObject({ op: z.literal('move_audio'), itemId: id, start: seconds, trackId: id.optional() }),
  z.strictObject({ op: z.literal('trim_audio'), itemId: id, start: seconds, end: seconds }),
  z.strictObject({ op: z.literal('split_audio'), itemId: id, time: seconds }),
  z.strictObject({
    op: z.literal('set_audio_properties'),
    itemId: id,
    volume: z.number().optional(),
    muted: z.boolean().optional(),
    fadeIn: z.number().optional(),
    fadeOut: z.number().optional(),
    speed: z.number().optional(),
    denoise: z.boolean().optional(),
    ducking: z.strictObject({ amountDb: z.number(), ramp: z.number() }).nullable().optional(),
  }),
  z.strictObject({
    op: z.literal('set_envelope'),
    itemId: id,
    points: z.array(z.strictObject({ time: seconds, gain: seconds })).max(500),
  }),
  z.strictObject({
    op: z.literal('add_speech'),
    text: z.string().min(1).max(4000),
    voice: z.string().min(1).optional(),
    language: z.string().min(2).optional(),
    /** The spec's `where`: project seconds, as `add_audio` places a sound. */
    time: z.number().min(0),
    label: z.string().max(200).optional(),
  }),
]);

export type Operation = z.infer<typeof OPERATION>;

export type Edit =
  | { readonly refused: undefined; readonly project: VideoProject; readonly lines: readonly string[] }
  | { readonly refused: string };

/** The words an operation is named by in a line: its op and the item it acts on. */
function operationLabel(operation: Operation): string {
  const fields = operation as Partial<Record<'clipId' | 'itemId' | 'assetId' | 'sourceId', string>>;
  const target = fields.clipId ?? fields.itemId ?? fields.assetId ?? fields.sourceId;
  return target === undefined ? operation.op : `${operation.op} ${target}`;
}

type Defined<T> = { [K in keyof T]: Exclude<T[K], undefined> };

/**
 * A command's arguments out of its operation: without `op` (setClipPresentation writes every key
 * it is given onto the clip), and without the keys zod leaves undefined, because the commands are
 * typed under exactOptionalPropertyTypes and an absent value there is a missing key.
 */
function argsOf<T extends { op: string }>(operation: T): Defined<Omit<T, 'op'>> {
  const { op: _op, ...rest } = operation;
  return Object.fromEntries(
    Object.entries(rest).filter(([, value]) => value !== undefined),
  ) as Defined<Omit<T, 'op'>>;
}

/** The project a sourced operation plays from, and that source. */
async function withSource(
  project: VideoProject,
  ref: { assetId?: string | undefined; sourceId?: string | undefined },
  door: SourceDoor,
): Promise<{ project: VideoProject; source: ProjectSource }> {
  if (ref.assetId !== undefined) return await door.source(project, ref.assetId);
  const source = project.sources.find((candidate) => candidate.id === ref.sourceId);
  if (source === undefined) {
    throw new Refusal(
      `the project has no source ${String(ref.sourceId)}; its sources are ${project.sources.map((known) => known.id).join(', ') || 'none'}`,
    );
  }
  return { project, source };
}

async function applyOne(project: VideoProject, operation: Operation, door: SourceDoor): Promise<VideoProject> {
  switch (operation.op) {
    case 'add_clip': {
      const placed = await withSource(project, operation, door);
      if (placed.source.kind === 'audio') {
        throw new Refusal(`${placed.source.assetId} is a sound: put it on an audio lane with add_audio`);
      }
      const sourceStart = operation.sourceStart ?? 0;
      const duration =
        operation.duration ??
        (placed.source.kind === 'image' ? IMAGE_SECONDS : placed.source.duration - sourceStart);
      return addClip(placed.project, {
        sourceId: placed.source.id,
        duration,
        sourceStart,
        atIndex: operation.atIndex,
      });
    }
    case 'add_audio': {
      const placed = await withSource(project, operation, door);
      return addAudio(placed.project, { sourceId: placed.source.id, time: operation.time, label: operation.label });
    }
    case 'add_speech': {
      const spoken = await door.speech({ text: operation.text, voice: operation.voice, language: operation.language });
      const placed = await door.source(project, spoken);
      return addAudio(placed.project, {
        sourceId: placed.source.id,
        time: operation.time,
        label: operation.label ?? operation.text.slice(0, 60),
      });
    }
    case 'add_overlay': {
      if (operation.kind === 'image') {
        const picture = operation.props.assetId;
        if (typeof picture !== 'string') {
          throw new Refusal('an image overlay names its picture in props.assetId');
        }
        await door.picture(picture);
      }
      return addOverlay(project, argsOf(operation));
    }
    case 'trim_clip':
      return trimClip(project, operation);
    case 'split_at':
      return splitAt(project, operation);
    case 'remove_range':
      return removeRange(project, operation);
    case 'move_clip':
      return moveClip(project, operation);
    case 'set_muted':
      return setMuted(project, operation);
    case 'set_clip_presentation':
      return setClipPresentation(project, argsOf(operation));
    case 'set_junction_transition':
      return setJunctionTransition(project, argsOf(operation));
    case 'set_frame_size':
      return setFrameSize(project, operation);
    case 'set_property':
      return setProperty(project, operation);
    case 'remove_item':
      return removeItem(project, operation);
    case 'extract_audio':
      return extractAudio(project, operation);
    case 'move_audio':
      return moveAudio(project, operation);
    case 'trim_audio':
      return trimAudio(project, operation);
    case 'split_audio':
      return splitAudio(project, operation);
    case 'set_audio_properties':
      return setAudioProperties(project, argsOf(operation));
    case 'set_envelope':
      return setEnvelope(project, operation);
  }
}

/**
 * Every operation in order, each against what the one before left. The first refusal ends the
 * edit and names its operation; the caller saves nothing. An error that is not a refusal is a bug
 * and propagates as one.
 */
export async function applyOperations(
  project: VideoProject,
  operations: readonly Operation[],
  door: SourceDoor,
): Promise<Edit> {
  let current = project;
  const lines: string[] = [];
  for (const [index, operation] of operations.entries()) {
    const label = operationLabel(operation);
    let next: VideoProject;
    try {
      next = await applyOne(current, operation, door);
    } catch (error) {
      const sentence = refusalSentence(error, operation, current);
      if (sentence === undefined) throw error;
      const position = operations.length === 1 ? '' : `operation ${String(index + 1)} of ${String(operations.length)}, `;
      // Speech is synthesized when its operation is reached, so only after every earlier one
      // passed; a later refusal cannot take it back, and says where it is.
      const kept =
        door.spoken.length === 0
          ? ''
          : ` The speech already synthesized stays in your library: ${door.spoken.join(', ')}.`;
      return { refused: `${position}${label}: ${sentence}${kept}` };
    }
    lines.push(`${label}: ${describeChange(current, next)}`);
    current = next;
  }
  return { refused: undefined, project: current, lines };
}
```

- [ ] **Step 4: Run them to see them pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/operations.test.ts test/unit/summary.test.ts`

Expected: `Tests  26 passed (26)`, `VITEST_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 5: Commit**

Write `$W/msg-t8.txt`:

```text
feat(video-mcp): edit a project with the Studio's commands, all or nothing

The twenty operations are the Studio's commands by name, with their
own arguments, plus add_speech, which asks Aura's voice and places it
as add_audio does. A list applies in order, each operation against
what the one before left, and either saves with one before => after
line per operation or changes nothing and names the operation that
refused, why, and what would be accepted. The summary is what open
answers: every item with its id, window, source and settings, and the
analyses the next render will make.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t8.txt services/video-mcp/src/operations.ts services/video-mcp/src/changes.ts services/video-mcp/src/summary.ts services/video-mcp/test/support/door.ts services/video-mcp/test/unit/operations.test.ts services/video-mcp/test/unit/summary.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 9: Projects in the library, and the four project tools

A project version is an asset of the identity, saved as Aura's cockpit saves one:
- named `projectFileName(project, PROJECT_FILE_EXTENSION)`;
- `application/json`, modality `document`, finalized as a document;
- one new asset per save, and every version kept.

`load` resolves the id, reads at most 5 MiB and parses it with the Studio's own `isProject`. Anything else is "X (id) is not a Video Studio project".

`list` reads the identity's project versions (the internal API's `projects`), reads them 8 at a time, groups them by the project id inside each file, and answers the newest of each with its count.

The tools:
- `video_project_create`;
- `video_project_open`;
- `video_project_edit` (all or nothing, Task 8);
- `video_project_list`.

A refusal is an answer the caller reads, not an `isError`. An error that is not a refusal (Aura down, a bug) stays an error.

**The two writing tools say they are closed-world** (`LIBRARY_WRITE`: `destructiveHint: false`, `idempotentHint: false`, `openWorldHint: false`). They write only into the caller's own library, and the spec wants no approval gate on them (§Security, "Gateway"). Aura's bridge reads an absent `openWorldHint` as true, and grades a write that is neither read-only nor idempotent, in an open world, as destructive (`internal/agent/mcptools/bridge_risk.go:231` and `:270-289`). A destructive tool stops the turn for the operator's approval (`internal/gateway/classify.go:63`). Without the hint, every create and every edit would wait for a click. The render tools take the same constant in Task 18.

**Files:**
- Create: `services/video-mcp/src/projects.ts`, `services/video-mcp/src/tools/projectTools.ts`
- Test: `services/video-mcp/test/unit/projectTools.test.ts`

**Interfaces:**
- Consumes: `InternalApi` (Task 3); `applyOperations`, `OPERATION`, `MAX_OPERATIONS` (Task 8); `sourceDoor`, `Probe` (Task 6); `timelineSummary` (Task 8); the seam (Task 5); `callerOf` (Task 2).
- Produces:
  - `interface ProjectStore { load(token, assetId): Promise<VideoProject>; save(token, project): Promise<string>; list(token): Promise<readonly ProjectListing[]> }`;
  - `interface ProjectListing { projectId; name; assetId; savedAt; versions }`;
  - `projectStore(api: InternalApi, fetchImpl?: typeof fetch): ProjectStore`;
  - `interface ProjectToolParts { api; store; probe }` and `registerProjectTools(server: McpServer, parts: ProjectToolParts): void`;
  - `LIBRARY_WRITE = { destructiveHint: false, idempotentHint: false, openWorldHint: false } as const`;
  - `text(body): CallToolResult` and `answer(work): Promise<CallToolResult>`.

- [ ] **Step 1: Write the failing test**

`services/video-mcp/test/unit/projectTools.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { internalApi } from '../../src/internalApi';
import { projectStore } from '../../src/projects';
import { isProject } from '../../src/studio';
import { registerProjectTools } from '../../src/tools/projectTools';
import { mcpClient, startSidecar, stubProbe, type Running } from '../support/sidecar';
import { IDENTITY, OTHER_IDENTITY } from '../support/tokens';

const PROBES = {
  'beach.mp4': { kind: 'video', duration: 12, width: 1080, height: 1920, hasAudio: true },
  'song.wav': { kind: 'audio', duration: 40, width: 0, height: 0 },
} as const;

let sidecar: Running;
let beach: string;
let song: string;

beforeEach(async () => {
  sidecar = await startSidecar((fake) => (server) => {
    const api = internalApi(fake.url);
    registerProjectTools(server, { api, store: projectStore(api), probe: stubProbe(PROBES) });
  });
  beach = sidecar.api.seed(IDENTITY, { fileName: 'beach.mp4', mimeType: 'video/mp4', bytes: Buffer.from('mp4') }).id;
  song = sidecar.api.seed(IDENTITY, { fileName: 'song.wav', mimeType: 'audio/wav', bytes: Buffer.from('wav') }).id;
});

afterEach(async () => {
  expect(sidecar.api.violations).toEqual([]);
  await sidecar.close();
});

const assetIdIn = (text: string): string => /asset ([0-9a-f-]{36})/.exec(text)?.[1] ?? '';

async function create(): Promise<string> {
  const created = await sidecar.call('video_project_create', {
    name: 'Aura memory',
    format: '9:16',
    clips: [{ assetId: beach }],
    sounds: [{ assetId: song }],
  });
  return assetIdIn(created.text);
}

it('declares its two writes closed-world, so Aura’s bridge grades them reversible and asks no approval', async () => {
  const client = await mcpClient(sidecar.base, await sidecar.bearer(IDENTITY));
  try {
    const annotations = Object.fromEntries((await client.listTools()).tools.map((tool) => [tool.name, tool.annotations]));
    expect(annotations).toEqual({
      video_project_create: { destructiveHint: false, idempotentHint: false, openWorldHint: false },
      video_project_open: { readOnlyHint: true },
      video_project_edit: { destructiveHint: false, idempotentHint: false, openWorldHint: false },
      video_project_list: { readOnlyHint: true },
    });
  } finally {
    await client.close();
  }
});

describe('video_project_create', () => {
  it('saves version 1 in the caller’s library as a Studio project document, and answers its timeline', async () => {
    const created = await sidecar.call('video_project_create', {
      name: 'Aura memory',
      format: '9:16',
      clips: [{ assetId: beach }],
      sounds: [{ assetId: song }],
    });
    expect(created.isError).toBe(false);
    expect(created.text).toMatch(/^version 1 saved as asset [0-9a-f-]{36}\n/);
    expect(created.text).toContain('"Aura memory" 1080×1920 at 30 fps, 12 s');
    const saved = sidecar.api.assets.get(assetIdIn(created.text));
    expect(saved).toMatchObject({ identity: IDENTITY, fileName: 'Aura-memory.aura-video.json', mimeType: 'application/json', status: 'processing' });
    expect(saved?.key).toMatch(/^chat\/[0-9a-f-]{36}\.aura-video\.json$/);
    expect(isProject(JSON.parse(saved?.bytes?.toString() ?? 'null'))).toBe(true);
  });

  it('creates nothing when a named source is refused, and says which', async () => {
    const before = sidecar.api.assets.size;
    const refused = await sidecar.call('video_project_create', { name: 'x', format: '1:1', clips: [{ assetId: song }] });
    expect(refused).toMatchObject({ isError: false, text: `nothing was created: add_clip ${song}: ${song} is a sound: put it on an audio lane with add_audio` });
    expect(sidecar.api.assets.size).toBe(before);
  });
});

describe('video_project_open', () => {
  it('answers the timeline of a project the caller owns', async () => {
    const id = await create();
    const opened = await sidecar.call('video_project_open', { assetId: id });
    expect(opened.text).toMatch(new RegExp(`^asset ${id}\\n"Aura memory" 1080×1920 at 30 fps, 12 s\\n`));
  });

  it('cannot tell another identity’s project from one that does not exist', async () => {
    const id = await create();
    const theirs = await sidecar.call('video_project_open', { assetId: id }, OTHER_IDENTITY);
    const nobodys = await sidecar.call('video_project_open', { assetId: '00000000-0000-4000-8000-000000000000' }, OTHER_IDENTITY);
    expect(theirs).toEqual({ ...nobodys, text: nobodys.text.replace('00000000-0000-4000-8000-000000000000', id) });
    expect(theirs.text).toBe(`asset ${id} is not in your library, or its upload has not finished`);
  });

  it('refuses an asset that is not a project: another file, bytes that are not JSON, or too many to be one', async () => {
    const opened = await sidecar.call('video_project_open', { assetId: beach });
    expect(opened.text).toBe(`beach.mp4 (${beach}) is not a Video Studio project`);
    const notes = sidecar.api.seed(IDENTITY, { fileName: 'notes.json', mimeType: 'application/json', bytes: Buffer.from('{"id":') }).id;
    expect((await sidecar.call('video_project_open', { assetId: notes })).text).toBe(`notes.json (${notes}) is not a Video Studio project`);
    const huge = sidecar.api.seed(IDENTITY, { fileName: 'huge.aura-video.json', mimeType: 'application/json', bytes: Buffer.alloc(6 * 1024 * 1024, 32) }).id;
    expect((await sidecar.call('video_project_open', { assetId: huge })).text).toBe(`huge.aura-video.json (${huge}) is not a Video Studio project`);
    expect(sidecar.api.reads.get(sidecar.api.assets.get(huge)?.key ?? '')).toBeUndefined();
  });

  it('fails the call, naming the file, when the object store will not give the project back', async () => {
    const id = await create();
    sidecar.api.objectFailures.set(id, 500);
    const opened = await sidecar.call('video_project_open', { assetId: id });
    expect(opened).toMatchObject({ isError: true, text: 'the object store answered 500 for Aura-memory.aura-video.json' });
  });
});

describe('video_project_edit', () => {
  it('saves a new version with one before => after line per operation', async () => {
    const first = await create();
    const edited = await sidecar.call('video_project_edit', {
      assetId: first,
      operations: [{ op: 'remove_range', from: 0, to: 2 }],
    });
    expect(edited.text).toMatch(/^saved as a new version: asset [0-9a-f-]{36}\nremove_range: clip [0-9a-f-]{36} duration: 12 => 10; clip [0-9a-f-]{36} sourceStart: 0 => 2/);
    expect(assetIdIn(edited.text)).not.toBe(first);
    expect(sidecar.api.assets.get(first)?.bytes?.toString()).toContain('"duration":12');
  });

  it('saves nothing when one operation is refused', async () => {
    const first = await create();
    const before = sidecar.api.assets.size;
    const refused = await sidecar.call('video_project_edit', {
      assetId: first,
      operations: [
        { op: 'remove_range', from: 0, to: 2 },
        { op: 'split_at', time: 50 },
      ],
    });
    expect(refused.text).toMatch(/^nothing changed: operation 2 of 2, split_at: /);
    expect(sidecar.api.assets.size).toBe(before);
  });

  it('answers an argument the schema refuses as a failed call, naming it', async () => {
    const first = await create();
    const wrong = await sidecar.call('video_project_edit', { assetId: first, operations: [{ op: 'trim_clip', clipId: 'c1' }] });
    expect(wrong.isError).toBe(true);
    expect(wrong.text).toContain('Input validation error');
  });
});

describe('video_project_list', () => {
  it('answers each project once, at its newest version, with how many versions it has', async () => {
    const first = await create();
    const edited = await sidecar.call('video_project_edit', { assetId: first, operations: [{ op: 'remove_range', from: 0, to: 1 }] });
    await sidecar.call('video_project_create', { name: 'Telegram', format: '16:9' });
    const listed = await sidecar.call('video_project_list', {});
    const lines = listed.text.split('\n');
    expect(lines).toHaveLength(2);
    expect(lines[0]).toMatch(/^asset [0-9a-f-]{36}  "Telegram"  saved .+  1 version  project /);
    expect(lines[1]).toMatch(new RegExp(`^asset ${assetIdIn(edited.text)}  "Aura memory"  saved .+  2 versions  project `));
  });

  it('says so when there is nothing to list', async () => {
    expect((await sidecar.call('video_project_list', {}, OTHER_IDENTITY)).text).toBe('no projects yet');
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/projectTools.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../src/projects' imported from …`; `VITEST_RC=1`.

- [ ] **Step 3: Write the store and the tools**

`services/video-mcp/src/projects.ts`:

```ts
// projects.ts — a project as the Studio keeps it: a `.aura-video.json` document in the caller's
// library, one immutable asset per version (projectStore.ts saveProject: modality `document`, the
// plain finalize). Reading one goes through resolve, which answers only for the caller's own
// assets, and through the Studio's own parser (projectFile.ts isProject).

import type { InternalApi, ResolvedAsset } from './internalApi';
import { Refusal } from './refusal';
import { notInLibrary } from './sources';
import { isProject, PROJECT_FILE_EXTENSION, projectFileName, type VideoProject } from './studio';

/** A project file is kilobytes; anything near this is not one, and is not read into memory. */
const PROJECT_MAX_BYTES = 5 * 1024 * 1024;
/** The listing reads every version to group them by project; this many at a time. */
const READ_CONCURRENCY = 8;
const PROJECT_MIME = 'application/json';

export interface ProjectVersion {
  readonly assetId: string;
  readonly project: VideoProject;
}

export interface ProjectListing {
  readonly projectId: string;
  readonly name: string;
  /** The newest version: the one to open, edit or render. */
  readonly assetId: string;
  readonly savedAt: string;
  readonly versions: number;
}

export interface ProjectStore {
  load(token: string, assetId: string): Promise<VideoProject>;
  /** Saves `project` as a new version and answers with its asset id. */
  save(token: string, project: VideoProject): Promise<string>;
  list(token: string): Promise<readonly ProjectListing[]>;
}

async function read(asset: ResolvedAsset, fetchImpl: typeof fetch): Promise<VideoProject | undefined> {
  if (asset.sizeBytes > PROJECT_MAX_BYTES) return undefined;
  const response = await fetchImpl(asset.url, { signal: AbortSignal.timeout(30_000) });
  if (!response.ok) {
    throw new Error(`the object store answered ${String(response.status)} for ${asset.fileName}`);
  }
  try {
    const value: unknown = JSON.parse(await response.text());
    return isProject(value) ? value : undefined;
  } catch {
    return undefined;
  }
}

async function eachLimited<T, R>(items: readonly T[], limit: number, run: (item: T) => Promise<R>): Promise<R[]> {
  const results: R[] = new Array<R>(items.length);
  let next = 0;
  const worker = async (): Promise<void> => {
    while (next < items.length) {
      const index = next;
      next += 1;
      results[index] = await run(items[index] as T);
    }
  };
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker));
  return results;
}

export function projectStore(api: InternalApi, fetchImpl: typeof fetch = fetch): ProjectStore {
  return {
    async load(token, assetId) {
      const { found } = await api.resolve(token, [assetId]);
      const asset = found.get(assetId);
      if (asset === undefined) throw notInLibrary(assetId);
      const project = await read(asset, fetchImpl);
      if (project === undefined) {
        throw new Refusal(`${asset.fileName} (${assetId}) is not a Video Studio project`);
      }
      return project;
    },

    async save(token, project) {
      const body = new Blob([JSON.stringify(project)], { type: PROJECT_MIME });
      const row = await api.upload(token, {
        fileName: projectFileName(project, PROJECT_FILE_EXTENSION),
        mimeType: PROJECT_MIME,
        modality: 'document',
        use: 'document',
        body,
      });
      return row.assetId;
    },

    async list(token) {
      const rows = await api.projects(token);
      if (rows.length === 0) return [];
      const { found } = await api.resolve(token, rows.map((row) => row.assetId));
      const parsed = await eachLimited(rows, READ_CONCURRENCY, async (row) => {
        const asset = found.get(row.assetId);
        return asset === undefined ? undefined : await read(asset, fetchImpl);
      });
      // Newest first, as Aura answers: the first version seen of a project is its latest.
      const byProject = new Map<string, { listing: ProjectListing; versions: number }>();
      rows.forEach((row, index) => {
        const project = parsed[index];
        if (project === undefined) return;
        const known = byProject.get(project.id);
        if (known !== undefined) {
          known.versions += 1;
          return;
        }
        byProject.set(project.id, {
          listing: { projectId: project.id, name: project.name, assetId: row.assetId, savedAt: row.createdAt, versions: 1 },
          versions: 1,
        });
      });
      return [...byProject.values()].map(({ listing, versions }) => ({ ...listing, versions }));
    },
  };
}
```

`services/video-mcp/src/tools/projectTools.ts`:

```ts
// projectTools.ts — the four project tools (spec §Tools). An edit is pure and fast: the only
// waits are Aura's internal API and ffprobe on a newly named source, far inside Aura's 60 s MCP
// call timeout. A refusal is a result the caller reads, never a tool failure; an error that is not
// a refusal (Aura down, a bug) is one, and the SDK answers it with `isError`.

import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import type { CallToolResult } from '@modelcontextprotocol/sdk/types.js';
import { z } from 'zod';
import { callerOf } from '../auth';
import type { InternalApi } from '../internalApi';
import { applyOperations, MAX_OPERATIONS, OPERATION, type Operation } from '../operations';
import type { Probe } from '../probe';
import type { ProjectStore } from '../projects';
import { Refusal } from '../refusal';
import { sourceDoor } from '../sources';
import { emptyProject, STARTING_FPS } from '../studio';
import { timelineSummary } from '../summary';

export interface ProjectToolParts {
  readonly api: InternalApi;
  readonly store: ProjectStore;
  readonly probe: Probe;
}

/** The three frames a reel, a landscape film and a square post are made in. */
const FORMATS = {
  '16:9': { width: 1920, height: 1080 },
  '9:16': { width: 1080, height: 1920 },
  '1:1': { width: 1080, height: 1080 },
} as const;

const assetId = z.uuid();

/** A tool that writes only into the caller's own library, where nothing is destroyed (spec
 *  §Security, "Gateway": no approval gate). The closed world is said out loud: Aura's bridge reads
 *  an absent openWorldHint as true and grades a non-idempotent open-world write as destructive,
 *  which stops the turn for the operator's approval (internal/agent/mcptools/bridge_risk.go,
 *  unsafeToRepeatBeyondAura; internal/gateway/classify.go). */
export const LIBRARY_WRITE = { destructiveHint: false, idempotentHint: false, openWorldHint: false } as const;

export function text(body: string): CallToolResult {
  return { content: [{ type: 'text', text: body }] };
}

/** Runs a tool's work; a refusal becomes its answer, anything else stays an error. */
export async function answer(work: () => Promise<string>): Promise<CallToolResult> {
  try {
    return text(await work());
  } catch (error) {
    if (error instanceof Refusal) return text(error.message);
    throw error;
  }
}

export function registerProjectTools(server: McpServer, parts: ProjectToolParts): void {
  const { api, store, probe } = parts;

  server.registerTool(
    'video_project_create',
    {
      description:
        'Create a Video Studio project in your library: a name, a frame (16:9, 9:16, 1:1, or width and height), and optionally clips and sounds by asset id, in order. Answers with version 1\'s asset id and the timeline.',
      inputSchema: {
        name: z.string().min(1).max(200),
        format: z.union([
          z.enum(['16:9', '9:16', '1:1']),
          z.strictObject({ width: z.number().int().min(16).max(3840), height: z.number().int().min(16).max(3840) }),
        ]),
        fps: z.number().int().min(23).max(60).optional(),
        clips: z
          .array(z.strictObject({ assetId, duration: z.number().positive().optional(), sourceStart: z.number().min(0).optional() }))
          .max(MAX_OPERATIONS)
          .optional(),
        sounds: z.array(z.strictObject({ assetId, time: z.number().min(0).optional() })).max(20).optional(),
      },
      annotations: LIBRARY_WRITE,
    },
    async (args, extra) =>
      answer(async () => {
        const { token } = callerOf(extra.authInfo);
        const size = typeof args.format === 'string' ? FORMATS[args.format] : args.format;
        const operations: Operation[] = [
          ...(args.clips ?? []).map((clip): Operation => ({ op: 'add_clip', ...clip })),
          ...(args.sounds ?? []).map((sound): Operation => ({ op: 'add_audio', assetId: sound.assetId, time: sound.time ?? 0 })),
        ];
        const edit = await applyOperations(
          emptyProject(args.name, size, args.fps ?? STARTING_FPS),
          operations,
          sourceDoor(api, token, probe),
        );
        if (edit.refused !== undefined) return `nothing was created: ${edit.refused}`;
        const saved = await store.save(token, edit.project);
        return [`version 1 saved as asset ${saved}`, ...edit.lines, '', timelineSummary(edit.project)].join('\n');
      }),
  );

  server.registerTool(
    'video_project_open',
    {
      description:
        'Read a Video Studio project by its asset id: every clip, overlay and sound with its id, start and end, source, volume, fades, ducking and transitions, and the analyses the next render will make.',
      inputSchema: { assetId },
      annotations: { readOnlyHint: true },
    },
    async (args, extra) =>
      answer(async () => {
        const project = await store.load(callerOf(extra.authInfo).token, args.assetId);
        return `asset ${args.assetId}\n${timelineSummary(project)}`;
      }),
  );

  server.registerTool(
    'video_project_edit',
    {
      description:
        'Edit a Video Studio project with the Studio\'s own commands, all or nothing. Saves a new version and answers with its asset id, one "before => after" line per operation, and the new timeline; a refused operation changes nothing and says what would be accepted.',
      inputSchema: { assetId, operations: z.array(OPERATION).min(1).max(MAX_OPERATIONS) },
      annotations: LIBRARY_WRITE,
    },
    async (args, extra) =>
      answer(async () => {
        const { token } = callerOf(extra.authInfo);
        const project = await store.load(token, args.assetId);
        const edit = await applyOperations(project, args.operations, sourceDoor(api, token, probe));
        if (edit.refused !== undefined) return `nothing changed: ${edit.refused}`;
        const saved = await store.save(token, edit.project);
        return [`saved as a new version: asset ${saved}`, ...edit.lines, '', timelineSummary(edit.project)].join('\n');
      }),
  );

  server.registerTool(
    'video_project_list',
    {
      description: 'Your Video Studio projects, newest first: the latest version of each, by asset id.',
      inputSchema: {},
      annotations: { readOnlyHint: true },
    },
    async (_args, extra) =>
      answer(async () => {
        const listed = await store.list(callerOf(extra.authInfo).token);
        if (listed.length === 0) return 'no projects yet';
        return listed
          .map(
            (entry) =>
              `asset ${entry.assetId}  "${entry.name}"  saved ${entry.savedAt}  ${String(entry.versions)} version${entry.versions === 1 ? '' : 's'}  project ${entry.projectId}`,
          )
          .join('\n');
      }),
  );
}
```

- [ ] **Step 4: Run it to see it pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/projectTools.test.ts`

Expected: `Tests  12 passed (12)`, `VITEST_RC=0`. Among them, "declares its two writes closed-world, so Aura’s bridge grades them reversible and asks no approval" reads the four tools' annotations as `tools/list` advertises them. With `openWorldHint` taken out of `LIBRARY_WRITE`, it fails (measured in scratch).

- [ ] **Step 5: B1's whole unit tier, with its floors**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh all`

Expected:
```text
TSC_RC=0
 Test Files  11 passed (11)
      Tests  118 passed (118)
All files         |   99.06 |     90.9 |     100 |   99.57 |
VITEST_RC=0
```
Those are the numbers B1 alone measured in scratch: statements, branches, functions, lines, each over 85.

- [ ] **Step 6: Commit**

Write `$W/msg-t9.txt`:

```text
feat(video-mcp): create, open, edit and list Studio projects over MCP

A project version is an asset of the caller, saved as the cockpit
saves one: <slug>.aura-video.json, a document, one new asset per save.
A load reads it through the internal API and parses it with the
Studio's own check. The four project tools answer refusals as text the
caller reads and keep errors as errors. The two that write say they
are closed-world: Aura's bridge reads a missing openWorldHint as open,
grades such a write destructive, and would stop every edit for the
operator's approval.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t9.txt services/video-mcp/src/projects.ts services/video-mcp/src/tools/projectTools.ts services/video-mcp/test/unit/projectTools.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 10: The unit tier in CI, and B1's push

The sidecar's unit tier joins CI as `video-mcp-test`. A `dorny/paths-filter` job runs it when one of these changes:
- the sidecar;
- the Studio core, the two Studio i18n resources, web's install and its patches, the fonts;
- the speech fixture the render tier reads;
- the image's files;
- `ci.yml`.

When the filter says nothing changed, the no-skip-as-green backstop must agree on the event's real diff. Its script takes the job's own pathspecs now; web's two jobs keep the default and are unchanged. The filter text is a YAML anchor, so Task 20's render job reuses it word for word.

**Files:**
- Modify: `.github/workflows/ci.yml` (one job inserted before `  # Mutation evidence arrives from parallel jobs`, line 1584)
- Modify: `scripts/web_filter_backstop.sh` (rewritten: takes pathspecs)

**Interfaces:**
- Consumes: `npm run typecheck`, `npm test` (Task 1's scripts).
- Produces:
  - job `video-mcp-test`, with steps "Detect video MCP changes" (`id: changes`, output `video_mcp`), "Typecheck and the unit tier with coverage", and the backstop;
  - the YAML anchors `&video-mcp-filter` and `&video-mcp-backstop`, which Task 20 aliases;
  - `web_filter_backstop.sh <base> <head> [pathspec…]`.

- [ ] **Step 1: Teach the backstop the job's pathspecs, and show it can pass and fail**

`scripts/web_filter_backstop.sh`, whole:

```bash
#!/usr/bin/env bash
# web_filter_backstop.sh — no-skip-as-green backstop for the path-filtered CI jobs
# (WR-05): web-lint/web-test, and the aura-video-mcp jobs. Each gates its real work
# on `dorny/paths-filter` reporting a change; when it reports none the job would
# otherwise report SUCCESS having run nothing. This step runs in exactly that
# branch and FAILS if the diff for the event range actually touched a watched path —
# i.e. the filter misfired (a glob edit, base-ref quirk, or rename dodging the globs)
# and a change is about to merge with its gates silently skipped.
#
# It is deliberately fail-OPEN on an uncomputable range (shallow clone with the
# base SHA unfetchable, a brand-new branch's first push where BEFORE is all-zeros,
# etc.): the goal is to catch a CONFIRMED filter miss without ever red-falsing the
# pipeline on a range it cannot trust. Fail-CLOSED only on a verified watched-path diff.
#
# Args:
#   $1 — base SHA (PR base.sha or push `before`)
#   $2 — head SHA (PR head.sha or push `sha`)
#   $3… — the git pathspecs the job's filter watches; web's own when none are given
#
# Exit codes:
#   0 — no watched path changed in the (computable) range, or the range is untrustworthy
#   1 — the filter reported no change but a watched path changed

set -euo pipefail

base="${1:-}"
head="${2:-}"
shift $(( $# < 2 ? $# : 2 ))
if [ "$#" -eq 0 ]; then
  set -- 'web/**' 'internal/webui/dist/**'
fi
zero="0000000000000000000000000000000000000000"

if [ -z "${base}" ] || [ -z "${head}" ] || [ "${base}" = "${zero}" ]; then
  echo "web-filter-backstop: no trustworthy diff range (base='${base}' head='${head}') — skipping cross-check."
  exit 0
fi

# Best-effort: the filtered jobs use a shallow checkout, so the base/head objects may be
# absent. Fetch them; if the fetch fails, fail-open (do not break the pipeline).
if ! git cat-file -e "${base}^{commit}" 2>/dev/null; then
  git fetch --no-tags --depth=1 origin "${base}" 2>/dev/null || true
fi
if ! git cat-file -e "${head}^{commit}" 2>/dev/null; then
  git fetch --no-tags --depth=1 origin "${head}" 2>/dev/null || true
fi
if ! git cat-file -e "${base}^{commit}" 2>/dev/null || ! git cat-file -e "${head}^{commit}" 2>/dev/null; then
  echo "web-filter-backstop: base/head objects unfetchable in this shallow clone — skipping cross-check."
  exit 0
fi

changed="$(git diff --name-only "${base}" "${head}" -- "$@" || true)"
if [ -n "${changed}" ]; then
  echo "NO-SKIP-AS-GREEN VIOLATION: paths-filter reported no change, but these watched paths changed:" >&2
  echo "${changed}" >&2
  echo "The quality gates would merge SKIPPED. Fix the dorny/paths-filter globs." >&2
  exit 1
fi

echo "web-filter-backstop: confirmed no change under $* in the range — skip is genuine."
```

Write `$W/backstop-check.sh`:

```bash
#!/usr/bin/env bash
# The backstop on real ranges of master: Task 4's commit touched web/src/videoStudio, a webauth fix did not.
set -uo pipefail
cd /mnt/d/Aura || exit 1
studio=$(git log --format=%H -1 -- web/src/videoStudio)
goonly=$(git log --format=%H -1 -- internal/webauth)
bash scripts/web_filter_backstop.sh "$studio^" "$studio" 'services/video-mcp/**' 'web/src/videoStudio/**'; echo "rc=$? (want 1)"
bash scripts/web_filter_backstop.sh "$goonly^" "$goonly" 'services/video-mcp/**' 'web/src/videoStudio/**'; echo "rc=$? (want 0)"
bash scripts/web_filter_backstop.sh "$studio^" "$studio" > /dev/null 2>&1; echo "web defaults: rc=$? (want 1)"
bash -n scripts/web_filter_backstop.sh && echo "bash -n ok"
command -v shellcheck > /dev/null && shellcheck scripts/web_filter_backstop.sh && echo "shellcheck ok"
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/backstop-check.sh`

Expected (measured on the scratch clone's history):
- `NO-SKIP-AS-GREEN VIOLATION: paths-filter reported no change, but these watched paths changed:`, the Studio files, and `rc=1 (want 1)`;
- `web-filter-backstop: confirmed no change under services/video-mcp/** web/src/videoStudio/** in the range — skip is genuine.` and `rc=0 (want 0)`;
- `web defaults: rc=1 (want 1)`;
- `bash -n ok`, `shellcheck ok`.

- [ ] **Step 2: Insert the job**

In `.github/workflows/ci.yml`, insert this block immediately before the line that begins `  # Mutation evidence arrives from parallel jobs and is judged once.` (`:1584`). The block ends with one blank line:

```yaml
  # ── aura-video-mcp (services/video-mcp) ─────────────────────────────────────
  # The sidecar takes the Studio core from web/src/videoStudio against web's own patched
  # install, so a change there is a change here. Its unit tier runs here, with its 85 % floor.
  video-mcp-test:
    name: Video MCP unit tier (typecheck + vitest + coverage)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Detect video MCP changes
        uses: dorny/paths-filter@ceb8a2b8f2d89434be7ff52d3de7ec3738c5cc9d # v4
        id: changes
        with:
          # Keep in step with the backstop's pathspecs below.
          filters: &video-mcp-filter |
            video_mcp:
              - 'services/video-mcp/**'
              - 'web/src/videoStudio/**'
              - 'web/src/i18n/resources.videoStudio*.ts'
              - 'web/package.json'
              - 'web/package-lock.json'
              - 'web/patches/**'
              - 'web/public/fonts/**'
              - 'web/e2e/fixtures/video-studio/audio/speech.wav'
              - 'docker/aura-video-mcp/**'
              - '.dockerignore'
              - '.github/workflows/ci.yml'

      - name: Set up Node
        if: steps.changes.outputs.video_mcp == 'true'
        uses: actions/setup-node@820762786026740c76f36085b0efc47a31fe5020 # v7
        with:
          node-version: ${{ env.AURA_CI_NODE_VERSION }}
          cache: npm
          cache-dependency-path: |
            web/package-lock.json
            services/video-mcp/package-lock.json

      # web first: the Studio core resolves its packages in web/node_modules, patched by web's
      # postinstall.
      - name: Typecheck and the unit tier with coverage
        if: steps.changes.outputs.video_mcp == 'true'
        run: |
          cd web && npm ci
          cd ../services/video-mcp && npm ci
          npm run typecheck
          npm test

      - &video-mcp-backstop
        name: No-skip-as-green backstop (filter must not have misfired)
        if: steps.changes.outputs.video_mcp != 'true'
        run: >-
          bash scripts/web_filter_backstop.sh
          "${{ github.event.pull_request.base.sha || github.event.before }}"
          "${{ github.event.pull_request.head.sha || github.sha }}"
          'services/video-mcp/**' 'web/src/videoStudio/**' 'web/src/i18n/resources.videoStudio*.ts'
          'web/package.json' 'web/package-lock.json' 'web/patches/**' 'web/public/fonts/**'
          'web/e2e/fixtures/video-studio/audio/speech.wav' 'docker/aura-video-mcp/**'
          '.dockerignore' '.github/workflows/ci.yml'

```

- [ ] **Step 3: Check the workflow statically**

Write `$W/ci-check.sh`:

```bash
#!/usr/bin/env bash
# The workflow, statically: YAML parses, the video-mcp jobs' steps, and actionlint's count against master's 16.
set -uo pipefail
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
cd /mnt/d/Aura || exit 1
python3 - <<'PY'
import yaml
jobs = yaml.safe_load(open('.github/workflows/ci.yml'))['jobs']
for name in ('video-mcp-test', 'video-mcp-render', 'video-mcp-mutation'):
    if name in jobs:
        print(name, [step.get('name') for step in jobs[name]['steps']])
print('web-mutation needs', jobs['web-mutation']['needs'])
PY
echo "actionlint findings: $(actionlint .github/workflows/ci.yml .github/workflows/publish-aura-edge.yml 2>&1 | grep -c 'shellcheck reported')"
PYTHONPATH=scripts python3 -m unittest scripts/mutation_workflow_test.py 2>&1 | tail -1
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ci-check.sh`

Expected:
- `video-mcp-test ['Checkout', 'Detect video MCP changes', 'Set up Node', 'Typecheck and the unit tier with coverage', 'No-skip-as-green backstop (filter must not have misfired)']`;
- `web-mutation needs ['web-mutation-stryker', 'go-mutation']`;
- `actionlint findings: 16`: master's own 16 findings (SC2046 and SC2034 in older jobs), none new;
- `OK`.

The job itself runs first in CI, on the push below.

- [ ] **Step 4: Commit**

Write `$W/msg-t10.txt`:

```text
ci: run the video sidecar's unit tier, and let the backstop watch it

video-mcp-test runs the sidecar's typecheck and unit tier, with its
85 % floor, whenever the sidecar, the Studio core it imports, web's
install and patches, or the image changes. A path filter that says
nothing changed must agree with the event's real diff: the
no-skip-as-green backstop now takes each job's own pathspecs, and
web's two jobs keep its defaults.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t10.txt .github/workflows/ci.yml scripts/web_filter_backstop.sh`

Expected: `COMMIT_RC=0`.

- [ ] **Step 5: Rebuild the embedded cockpit**

Task 4 changed the cockpit's sources. The aura image embeds the built cockpit, `internal/webui/dist`, and this repository rebuilds it in a commit of its own whenever web changes (as `b71a33dac` and `ea613fa65` did). B2 changes nothing in web, so B1 is the half that does it.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/build.sh`

Expected:
- `BUILD_RC=0`;
- vite's `✓ built in …`;
- `dist files changed:` with a count above 0, because the hashed chunks that hold the Studio change.

The count depends on how far master's last rebuild is from yours. The scratch clone, based on a commit before master's own rebuild, counted 352; that number says nothing about yours.

Write `$W/msg-t10-dist.txt`:

```text
build(web): rebuild the embedded dist for the Studio core split

The cockpit's Studio now imports its project file and source edit from
modules of their own, so a second consumer can take them without React
or the chat attachment client; the embedded bundle is rebuilt from
those sources.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t10-dist.txt internal/webui/dist`

Expected: `COMMIT_RC=0`. `git -C /d/Aura status --porcelain -- internal/webui/dist` then prints nothing: removed chunks are staged as deletions by the pathspec.

- [ ] **Step 6: Push B1 and read CI on that commit**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/push.sh push-b1.log`

Expected: the lefthook banner, the pre-push gates (build, tagged-tier-compile, deadcode, web-deadcode) passing, and `PUSH_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/poll-ci.sh "$(git -C /mnt/d/Aura rev-parse HEAD)"`

Expected: every run `completed	success`. Among them, "Video MCP unit tier (typecheck + vitest + coverage)" ran its steps rather than taking the backstop branch: its log shows `Tests  118 passed (118)`. "Web unit tests (Vitest + coverage)" ran too, because Task 4 changed web. A red job is fixed before B2 starts, whoever's it is (CLAUDE.md); never re-push only to re-read a hook.

---

# B2 — the render half

The three measurements come first, each as its own task (CLAUDE.md "PRD-first": measure, then amend, then implement). They were taken while this plan was written, on 2026-10-02:
- **The host:** the operator's Windows 11 desktop, Docker under WSL2, `--cpus=4`, swap off, no network.
- **The software:** Playwright 1.63.0's Chrome Headless Shell 153.0.8010.12, Node 24, Debian ffmpeg 5.1.9.
- **The harness:** `spikes/video-mcp-render/plan-b/`, which Task 11 commits.
- **The raw outputs:** on that desktop in `~/planb-scratch/measure/out`.

Each task re-runs its measurement where you execute it, then records your numbers: the PRD records what was measured, not what this plan predicted. If your numbers move a rule (a different bitrate, a different film length, a row that fails), stop and tell the operator before writing code: the measurement wins over this plan.

The films are cut from two real clips that the operator owns. They stay outside the repository:
- `camera.mp4`: 268 s of 1080p camera footage, 305 MB, the video stream of "Video Industry_corsi.mov";
- `screen.mp4`: a 1080p screen recording with sound, 16 MB.

`projects.mjs` writes the three films as the Studio saves projects:
- **reel-30:** 29 s at 1080×1920 and 30 fps. Camera, screen recording, camera, with two crossfades, a title, and a music bed ducked under a voice.
- **reel-30-720:** the same film at 720×1280.
- **film-600:** 10 minutes. Forty 15 s clips alternating camera and screen, a crossfade at every fourth junction, a fade to black every tenth, a title a minute, a 600 s music bed with fades and ducking, and the voice every 30 s.

### Task 11: Measure the bitrate per resolution, record it, then the limits

**Files:**
- Create: `spikes/video-mcp-render/plan-b/` with `Dockerfile`, `prepare.sh`, `projects.mjs`, `build.mjs`, `server.mjs`, `render.mjs`, `check.mjs`, `run.sh`, `sweep.sh`, `sources.sh`, `limit.sh`, `rows.sh` and `held.sh`
- Modify: `prd.md` (a paragraph inserted before `Measured 2026-09-08 using an agent-generated weather demo`, `:844` on master `391a3c014`)
- Modify: `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md` (dated notes after `:296` and `:403` on `391a3c014`)
- Create: `services/video-mcp/src/jobs/limits.ts`; modify `services/video-mcp/src/studio.ts` (`type ProjectSize`)
- Test: `services/video-mcp/test/unit/limits.test.ts`

**Interfaces:**
- Consumes: the Studio's `exportProject` with Task 4's `videoBitrate`, bundled by the harness from this repository's `web/`.
- Produces (`src/jobs/limits.ts`):
  - `type Quality = '1080p' | '720p'`;
  - `FPS_MIN = 23`, `FPS_MAX = 60`, `STALL_MS = 120_000`, `JOB_MAX_AGE_MS` (60 min), `FINISHED_JOB_TTL_MS` (2 h);
  - `renderSize(size: ProjectSize, quality: Quality): ProjectSize`: the short side at 1080 or 720, never wider than 1920, both sides even;
  - `videoBitrate(quality: Quality, seconds: number): number`: 6,000,000 or 3,000,000, unless the 300 MB row needs less. Then it is 85 % of the byte budget less the audio's 128 kbps, and never under 250,000.

  `FILM_MAX_SECONDS` waits for Task 13's measurement.
- Produces (`src/studio.ts`): `type ProjectSize`, from `project`.

- [ ] **Step 1: Write the harness**

Each file goes in `spikes/video-mcp-render/plan-b/`. They run from `$M`, where `prepare.sh` copies them. Nothing they write lands in the repository.

`Dockerfile`:

```dockerfile
# Plan B measurement image: the sidecar's candidate base (node:24-bookworm-slim, Debian ffmpeg) with
# Playwright 1.63.0's Chrome Headless Shell, the build web/ resolves today. Root is fine here: it
# measures, it does not ship.
FROM node:24-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
WORKDIR /m
RUN echo '{"name":"m","private":true,"type":"module"}' > package.json \
 && npm i --no-audit --no-fund playwright@1.63.0 \
 && npx playwright install --with-deps --only-shell chromium \
 && rm -rf /var/lib/apt/lists/* /root/.npm
```

`prepare.sh`:

```bash
#!/usr/bin/env bash
# prepare.sh <camera file> <screen recording>: Plan B's measurement harness, made ready. Copies the
# harness into $MEASURE_DIR (default ~/planb-scratch/measure, outside the repository: the fixtures
# weigh 300 MB), remuxes the two real clips into it read-only (nothing is written back), writes the
# projects, bundles the page against this repository's web/ and builds the measurement image.
# Then: sweep.sh (bitrate), sources.sh (memory per source byte), limit.sh and run.sh (a film at a
# memory limit), rows.sh (the Instagram rows).
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
[ "$#" -eq 2 ] || { echo "usage: prepare.sh <camera file> <screen recording>" >&2; exit 2; }
SRC="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$SRC/../../.." && pwd)"
M=${MEASURE_DIR:-$HOME/planb-scratch/measure}
mkdir -p "$M/fixtures" "$M/out"
cp "$SRC"/Dockerfile "$SRC"/*.mjs "$SRC"/*.sh "$M"/
cd "$M"
[ -f package.json ] || echo '{"name":"measure","private":true,"type":"module"}' > package.json
[ -d node_modules/esbuild ] || npm i --no-audit --no-fund esbuild@0.28.2 > /dev/null
A="$REPO/web/e2e/fixtures/video-studio/audio"
[ -f fixtures/camera.mp4 ] || ffmpeg -v error -y -i "$1" -map 0:v:0 -c copy -movflags +faststart fixtures/camera.mp4
[ -f fixtures/screen.mp4 ] || ffmpeg -v error -y -i "$2" -map 0:v:0 -map 0:a:0 -c copy -movflags +faststart fixtures/screen.mp4
[ -f fixtures/music-600.wav ] || ffmpeg -v error -y -stream_loop -1 -i "$A/music.wav" -t 600 -c:a pcm_s16le fixtures/music-600.wav
cp "$A/speech.wav" fixtures/speech.wav
node projects.mjs
AURA_WEB="$REPO/web" node build.mjs
docker build -q -t planb-measure:1.63.0 . | tail -1
docker run --rm planb-measure:1.63.0 sh -c 'ffmpeg -version | head -1; node --version; ls /root/.cache/ms-playwright; ffmpeg -hide_banner -filters 2>/dev/null | grep -E " (libvmaf|ssim|psnr) "'
ls -la fixtures
```

`projects.mjs`:

```js
// The measurement projects, as the Studio saves them (web/src/videoStudio/project.ts).
//   reel-30   30 s at 1080×1920/30: camera, screen recording, camera, two crossfades, a title,
//             a music bed ducked under a voice. The Instagram-rows and bitrate film.
//   reel-30-720  the same at 720×1280.
//   film-600  10 minutes at 1080×1920/30: 40 clips of 15 s alternating camera and screen
//             recording, a crossfade every 4th junction, a fade to black every 10th, a title
//             a minute, a 600 s music bed with fades and ducking, and the voice every 30 s.
import { writeFileSync } from 'node:fs';

const SPEECH_TRUTH = [
  [1, 3.662],
  [5.162, 7.857],
  [9.357, 11.905],
];
const sources = [
  { id: 'src-camera', assetId: 'camera.mp4', kind: 'video', duration: 268.1, size: { width: 1920, height: 1080 }, hasAudio: false },
  { id: 'src-screen', assetId: 'screen.mp4', kind: 'video', duration: 33.6, size: { width: 1916, height: 878 }, hasAudio: true },
  { id: 'src-music', assetId: 'music-600.wav', kind: 'audio', duration: 600, size: { width: 0, height: 0 } },
  { id: 'src-voice', assetId: 'speech.wav', kind: 'audio', duration: 12.9, size: { width: 0, height: 0 }, speech: SPEECH_TRUTH },
];
const title = (id, clipId, text) => ({
  id,
  kind: 'text',
  anchor: { clipId, offset: 1 },
  duration: 5,
  props: { text, fontFamily: 'Atkinson Hyperlegible Next', fontSize: 6, fontWeight: 700, color: '#ffffff' },
});
const voice = (id, clipId, offset) => ({ id, sourceId: 'src-voice', anchor: { clipId, offset }, sourceStart: 0, duration: 12.9, volume: 1, muted: false });

function reel30(size) {
  const clip = (id, sourceId, sourceStart, extra = {}) => ({ id, sourceId, duration: 10, sourceStart, muted: true, ...extra });
  const cross = (from) => ({ junctionFromClipId: from, junctionTransition: 'crossfade', junctionDuration: 0.5 });
  return {
    id: 'reel-30',
    name: 'reel 30',
    size,
    fps: 30,
    sources,
    video: [clip('c1', 'src-camera', 60, { fadeIn: true }), clip('c2', 'src-screen', 5, cross('c1')), clip('c3', 'src-camera', 150, { ...cross('c2'), fadeOut: true })],
    overlays: [{ id: 'titles', items: [title('t1', 'c2', 'Aura — the Video Studio')] }],
    audio: [
      { id: 'music-lane', items: [{ id: 'music', sourceId: 'src-music', anchor: { clipId: 'c1', offset: 0 }, sourceStart: 0, duration: 29, volume: 0.8, muted: false, fadeIn: 1, fadeOut: 2, ducking: { amountDb: -12, ramp: 0.5 } }] },
      { id: 'voice-lane', items: [voice('v1', 'c1', 8)] },
    ],
  };
}

function film600() {
  const video = [];
  for (let i = 0; i < 40; i += 1) {
    const camera = i % 2 === 0;
    const id = `c${i}`;
    const junction =
      i === 0 ? {} : i % 10 === 0 ? { junctionFromClipId: `c${i - 1}`, junctionTransition: 'fadeBlack', junctionDuration: 1 } : i % 4 === 0 ? { junctionFromClipId: `c${i - 1}`, junctionTransition: 'crossfade', junctionDuration: 0.5 } : {};
    video.push({ id, sourceId: camera ? 'src-camera' : 'src-screen', duration: 15.17, sourceStart: camera ? (i * 13) % 250 : (i * 7) % 18, muted: true, ...junction });
  }
  return {
    id: 'film-600',
    name: 'film 600',
    size: { width: 1080, height: 1920 },
    fps: 30,
    sources,
    video,
    overlays: [{ id: 'titles', items: Array.from({ length: 10 }, (_, m) => title(`t${m}`, `c${m * 4}`, `Minute ${m + 1}`)) }],
    audio: [
      { id: 'music-lane', items: [{ id: 'music', sourceId: 'src-music', anchor: { clipId: 'c0', offset: 0 }, sourceStart: 0, duration: 599, volume: 0.8, muted: false, fadeIn: 2, fadeOut: 3, ducking: { amountDb: -12, ramp: 0.5 } }] },
      { id: 'voice-lane', items: Array.from({ length: 20 }, (_, k) => voice(`v${k}`, `c${k * 2}`, 2)) },
    ],
  };
}

writeFileSync('fixtures/reel-30.json', JSON.stringify(reel30({ width: 1080, height: 1920 })));
writeFileSync('fixtures/reel-30-720.json', JSON.stringify(reel30({ width: 720, height: 1280 })));
writeFileSync('fixtures/film-600.json', JSON.stringify(film600()));
console.log('projects written');
```

`build.mjs`:

```js
// The export page for the measurements: web/src/videoStudio bundled unchanged, with Plan B's
// videoBitrate option, resolving against web/node_modules, as the spike's build.mjs does.
// AURA_WEB is the repository's web/ (prepare.sh sets it).
import { build } from 'esbuild';
import { copyFileSync, cpSync, mkdirSync, writeFileSync } from 'node:fs';
import { basename } from 'node:path';

const WEB = process.env.AURA_WEB;
if (!WEB) throw new Error('AURA_WEB must name the repository web/ directory');
const DIST = new URL('./dist', import.meta.url).pathname;
mkdirSync(`${DIST}/assets`, { recursive: true });
const urlImports = {
  name: 'url-imports',
  setup(b) {
    b.onResolve({ filter: /\?url$/ }, async (args) => {
      const r = await b.resolve(args.path.slice(0, -4), { resolveDir: args.resolveDir, kind: 'import-statement' });
      if (r.errors.length > 0) return { errors: r.errors };
      return { path: r.path, namespace: 'url-import' };
    });
    b.onLoad({ filter: /.*/, namespace: 'url-import' }, (args) => {
      const name = basename(args.path);
      copyFileSync(args.path, `${DIST}/assets/${name}`);
      return { contents: `export default ${JSON.stringify(`/dist/assets/${name}`)};`, loader: 'js' };
    });
  },
};
writeFileSync(
  `${DIST}/entry.ts`,
  `import { exportProject } from '${WEB}/src/videoStudio/videoflow';\nObject.assign(window, { studio: { exportProject } });\n`,
);
await build({
  entryPoints: [`${DIST}/entry.ts`],
  outfile: `${DIST}/studio.js`,
  bundle: true,
  format: 'esm',
  platform: 'browser',
  target: 'es2022',
  external: ['module', 'fs', 'path', 'url'],
  plugins: [urlImports],
  logLevel: 'warning',
});
cpSync(`${WEB}/public/fonts`, `${DIST}/fonts`, { recursive: true });
writeFileSync(
  `${DIST}/studio.html`,
  '<!doctype html><html><head><meta charset="utf-8"></head><body><script type="module" src="/dist/studio.js"></script></body></html>\n',
);
console.log('built', DIST);
```

`server.mjs`:

```js
// Static server: /dist (page), /fonts (LOCAL_FONTS), /media/<file> (fixtures, Range-capable),
// POST /upload/<name> writes the page's MP4 to out/.
import { createServer } from 'node:http';
import { appendFileSync, createReadStream, createWriteStream, statSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';

const ROOT = new URL('.', import.meta.url).pathname;
const TYPES = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.wasm': 'application/wasm',
  '.woff2': 'font/woff2',
  '.mp4': 'video/mp4',
  '.wav': 'audio/wav',
  '.ogg': 'audio/ogg',
};
const MOUNTS = { dist: 'dist', fonts: 'dist/fonts', media: 'fixtures' };

export function startServer() {
  const server = createServer((req, res) => {
    const url = new URL(req.url, 'http://x');
    if (process.env.REQUEST_LOG && url.pathname.startsWith('/media/')) {
      appendFileSync(process.env.REQUEST_LOG, `${req.method} ${url.pathname} range=${req.headers.range ?? 'none'}\n`);
    }
    if (req.method === 'POST' && url.pathname.startsWith('/upload/')) {
      const out = createWriteStream(join(process.env.UPLOAD_DIR ?? join(ROOT, 'out'), url.pathname.slice(8).replace(/[^\w.-]/g, '_')));
      req.pipe(out);
      out.on('finish', () => res.writeHead(200).end());
      return;
    }
    const [, mount, ...rest] = url.pathname.split('/');
    const base = MOUNTS[mount];
    const rel = normalize(decodeURIComponent(rest.join('/')));
    if (base === undefined || rel.startsWith('..')) return void res.writeHead(404).end();
    const path = join(ROOT, base, rel);
    let size;
    try {
      size = statSync(path).size;
    } catch {
      return void res.writeHead(404).end();
    }
    const headers = { 'Content-Type': TYPES[extname(path)] ?? 'application/octet-stream', 'Accept-Ranges': 'bytes' };
    const range = /^bytes=(\d*)-(\d*)$/.exec(req.headers.range ?? '');
    if (range) {
      const start = range[1] === '' ? size - Number(range[2]) : Number(range[1]);
      const end = range[1] !== '' && range[2] !== '' ? Number(range[2]) : size - 1;
      res.writeHead(206, { ...headers, 'Content-Range': `bytes ${start}-${end}/${size}`, 'Content-Length': end - start + 1 });
      return void createReadStream(path, { start, end }).pipe(res);
    }
    res.writeHead(200, { ...headers, 'Content-Length': size });
    createReadStream(path).pipe(res);
  });
  return new Promise((resolve) =>
    server.listen(0, '127.0.0.1', () =>
      resolve({ origin: `http://127.0.0.1:${server.address().port}`, close: () => new Promise((r) => server.close(r)) }),
    ),
  );
}
```

`render.mjs`:

```js
// node render.mjs <project.json> <out.mp4> [videoBitrate]: one export in the headless shell with
// renderer-server's launch flags (the spike's RENDER_ARGS), cgroup v2 memory sampled every 500 ms
// (memory.current, memory.stat anon) and memory.peak at the end, as s2-bench.mjs does.
import { readFileSync, writeFileSync } from 'node:fs';
import { chromium } from 'playwright';
import { startServer } from './server.mjs';

const [projectFile, outName, bitrate] = process.argv.slice(2);
const ARGS = [
  '--no-sandbox',
  '--js-flags=--max-old-space-size=4096',
  '--enable-blink-features=CanvasDrawElement',
  '--disable-frame-rate-limit',
  '--disable-gpu-vsync',
  '--no-zygote',
  '--disable-gpu',
  '--disable-dev-shm-usage',
  '--disable-background-timer-throttling',
  '--disable-renderer-backgrounding',
  '--disable-features=site-per-process',
  '--disable-extensions',
];
const CG = '/sys/fs/cgroup';
const read = (f) => {
  try {
    return readFileSync(`${CG}/${f}`, 'utf8').trim();
  } catch {
    return '';
  }
};
const samples = [];
const sampler = setInterval(
  () =>
    samples.push({
      current: Number(read('memory.current')),
      anon: Number(/^anon (\d+)$/m.exec(read('memory.stat'))?.[1] ?? 0),
    }),
  500,
);
const project = JSON.parse(readFileSync(projectFile, 'utf8'));
const server = await startServer();
const started = performance.now();
const browser = await chromium.launch({ headless: true, args: ARGS });
const logs = [];
const progressAt = [];
try {
  const page = await browser.newPage({ viewport: { width: 640, height: 640 } });
  page.on('console', (m) => logs.push(`${m.type()}: ${m.text()}`.slice(0, 300)));
  page.on('pageerror', (e) => logs.push(`pageerror: ${e.message}`.slice(0, 300)));
  await page.exposeFunction('progress', (p) =>
    progressAt.push([Number(((performance.now() - started) / 1000).toFixed(1)), Number(p.toFixed(3))]),
  );
  await page.goto(`${server.origin}/dist/studio.html`);
  await page.waitForFunction(() => 'studio' in window);
  const result = await page.evaluate(
    async ({ project, outName, bitrate }) => {
      let last = -1;
      const blob = await window.studio.exportProject(
        project,
        { assetUrl: (id) => `/media/${encodeURIComponent(id)}` },
        {
          ...(bitrate ? { videoBitrate: Number(bitrate) } : {}),
          onProgress: (p) => {
            if (p - last >= 0.1) {
              last = p;
              window.progress(p);
            }
          },
        },
      );
      const r = await fetch(`/upload/${outName}`, { method: 'POST', body: blob });
      return { bytes: blob.size, uploaded: r.status };
    },
    { project, outName, bitrate },
  );
  const wall = (performance.now() - started) / 1000;
  const record = {
    projectFile,
    outName,
    bitrate: bitrate ?? 'default',
    browser: browser.version(),
    wallSeconds: Number(wall.toFixed(1)),
    ...result,
    memoryPeakMiB: Math.round(Number(read('memory.peak')) / 2 ** 20),
    currentMaxMiB: Math.round(Math.max(...samples.map((s) => s.current)) / 2 ** 20),
    anonMaxMiB: Math.round(Math.max(...samples.map((s) => s.anon)) / 2 ** 20),
    memoryMax: read('memory.max'),
    cpuMax: read('cpu.max'),
    progressAt,
    logs: logs.filter((l) => !/favicon/.test(l)).slice(0, 12),
  };
  writeFileSync(`out/${outName}.json`, JSON.stringify(record, null, 2));
  console.log(JSON.stringify(record, null, 2));
} finally {
  clearInterval(sampler);
  await browser.close();
  await server.close();
}
```

`check.mjs`:

```js
// node check.mjs <file.mp4>: every Instagram Reels row (Graph API "Reel specifications", read in
// the spec 2026-09-30) as the file states it, read with ffprobe/ffmpeg and a box walk.
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';

const file = process.argv[2];
const ffprobe = (...args) => JSON.parse(execFileSync('ffprobe', ['-v', 'error', ...args, '-of', 'json', file], { maxBuffer: 1 << 28 }).toString());

function boxes(buf, start, end, depth, path, out) {
  let at = start;
  while (at + 8 <= end) {
    let size = buf.readUInt32BE(at);
    const type = buf.toString('latin1', at + 4, at + 8);
    let header = 8;
    if (size === 1) {
      size = Number(buf.readBigUInt64BE(at + 8));
      header = 16;
    } else if (size === 0) size = end - at;
    out.push({ path: [...path, type].join('/'), at, size });
    if (['moov', 'trak', 'edts', 'mdia', 'minf', 'stbl'].includes(type)) boxes(buf, at + header, at + size, depth + 1, [...path, type], out);
    if (type === 'elst') {
      const version = buf.readUInt8(at + header);
      const count = buf.readUInt32BE(at + header + 4);
      const entries = [];
      let e = at + header + 8;
      for (let i = 0; i < count; i += 1) {
        if (version === 1) {
          entries.push({ segmentDuration: Number(buf.readBigUInt64BE(e)), mediaTime: Number(buf.readBigInt64BE(e + 8)) });
          e += 20;
        } else {
          entries.push({ segmentDuration: buf.readUInt32BE(e), mediaTime: buf.readInt32BE(e + 4) });
          e += 12;
        }
      }
      out.at(-1).entries = entries;
    }
    at += size;
  }
  return out;
}

const all = boxes(readFileSync(file), 0, readFileSync(file).length, 0, [], []);
const top = all.filter((b) => !b.path.includes('/')).map((b) => b.path);
const streams = ffprobe('-show_streams', '-show_format').streams;
const v = streams.find((s) => s.codec_type === 'video');
const a = streams.find((s) => s.codec_type === 'audio');
const frames = ffprobe('-select_streams', 'v:0', '-show_entries', 'frame=key_frame,pict_type,pts_time,pkt_size').frames;
const keys = frames.filter((f) => f.key_frame === 1).map((f) => Number(f.pts_time));
const gops = keys.slice(1).map((t, i) => Number((t - keys[i]).toFixed(3)));
const types = {};
for (const f of frames) types[f.pict_type] = (types[f.pict_type] ?? 0) + 1;
const perSecond = new Map();
for (const f of frames) {
  const s = Math.floor(Number(f.pts_time));
  perSecond.set(s, (perSecond.get(s) ?? 0) + Number(f.pkt_size) * 8);
}
const traceText = execFileSync('sh', ['-c', `ffmpeg -v trace -i "${file}" -map 0:v:0 -c copy -bsf:v trace_headers -frames:v 400 -f null - 2>&1 | grep -oE 'nal_unit_type +[0-9]+ += +[0-9]+' | awk '{print $NF}' | sort | uniq -c`]).toString();
const firstPts = ffprobe('-show_entries', 'packet=pts_time,stream_index', '-read_intervals', '%+#4').packets;
console.log(
  JSON.stringify(
    {
      file,
      container: { format: v && ffprobe('-show_format').format.format_name, topLevelBoxes: top, moovBeforeMdat: top.indexOf('moov') < top.indexOf('mdat'), editLists: all.filter((b) => b.path.endsWith('elst')).map((b) => ({ path: b.path, entries: b.entries })) },
      video: v && {
        codec: v.codec_name,
        profile: v.profile,
        level: v.level,
        size: `${v.width}x${v.height}`,
        pixFmt: v.pix_fmt,
        colorRange: v.color_range,
        colorSpace: v.color_space,
        colorTransfer: v.color_transfer,
        colorPrimaries: v.color_primaries,
        fieldOrder: v.field_order,
        rFrameRate: v.r_frame_rate,
        avgFrameRate: v.avg_frame_rate,
        hasBFrames: v.has_b_frames,
        bitRate: Number(v.bit_rate),
        duration: Number(v.duration),
        frames: frames.length,
        pictTypes: types,
        keyframes: keys.length,
        gopSeconds: [...new Set(gops)],
        maxOneSecondMbps: Number((Math.max(...perSecond.values()) / 1e6).toFixed(2)),
        nalUnitTypes: traceText.trim().split('\n'),
      },
      audio: a && { codec: a.codec_name, profile: a.profile, sampleRate: Number(a.sample_rate), channels: a.channels, bitRate: Number(a.bit_rate), duration: Number(a.duration), startTime: Number(a.start_time) },
      firstPackets: firstPts,
      bytes: Number(ffprobe('-show_format').format.size),
      formatDuration: Number(ffprobe('-show_format').format.duration),
    },
    null,
    1,
  ),
);
```

`run.sh`:

```bash
#!/usr/bin/env bash
# run.sh <project.json> <out name> [videoBitrate]: one render in a fresh container at the appliance's
# budget (--cpus=4), offline, with the memory limit S1.3 needed (6 GiB) so memory.peak is the
# render's own. The harness is mounted at /m/h, so `playwright` resolves from the image's
# /m/node_modules by Node's walk up the directory tree.
set -uo pipefail
M=${MEASURE_DIR:-$HOME/planb-scratch/measure}
docker run --rm --cpus=4 --memory=6g --memory-swap=6g --network none \
  -v "$M":/m/h -w /m/h planb-measure:1.63.0 node render.mjs "fixtures/$1" "$2" ${3:-} 2>&1 | tail -60
```

`sweep.sh`:

```bash
#!/usr/bin/env bash
# sweep.sh: the bitrate sweep. reel-30 (real camera + screen footage) rendered at 1080x1920 and
# 720x1280 at a near-transparent reference bitrate and at candidate targets, each in a fresh
# container (run.sh), then every candidate scored against its resolution's reference with ffmpeg's
# ssim and psnr filters, and its actual bitrate and worst one-second peak read by ffprobe.
set -uo pipefail
H="$(dirname "$0")"
M=${MEASURE_DIR:-$HOME/planb-scratch/measure}
cp "$H"/*.mjs "$H"/*.sh "$M/" 2>/dev/null || true
render() { # project out bitrate
  [ -s "$M/out/$2" ] || bash "$H/run.sh" "$1" "$2" "$3" | grep -E '"(wallSeconds|bytes|memoryPeakMiB)"'
}
render reel-30.json r1080-ref.mp4 20000000
for b in 2500000 3000000 3500000 4000000 6000000; do render reel-30.json "r1080-$b.mp4" "$b"; done
render reel-30-720.json r720-ref.mp4 12000000
for b in 1500000 2000000 2500000 3000000; do render reel-30-720.json "r720-$b.mp4" "$b"; done
render reel-30-720.json r720-default.mp4 ""
docker run --rm -v "$M":/m/h -w /m/h planb-measure:1.63.0 sh -c '
  score() { # candidate reference
    rate=$(ffprobe -v error -select_streams v:0 -show_entries stream=bit_rate -of csv=p=0 "$1")
    peak=$(ffprobe -v error -select_streams v:0 -show_entries packet=pts_time,size -of csv=p=0 "$1" \
      | awk -F, "{s[int(\$1)]+=\$2} END {m=0; for (k in s) if (s[k]>m) m=s[k]; printf \"%.2f\", m*8/1e6}")
    q=$(ffmpeg -v info -i "$1" -i "$2" -lavfi "[0:v]split[a0][a1];[1:v]split[b0][b1];[a0][b0]ssim;[a1][b1]psnr" -f null - 2>&1 \
      | grep -oE "(SSIM Y:[0-9.]+|All:[0-9.]+ \([0-9.]+\)|average:[0-9.]+)" | tr "\n" " ")
    printf "%-22s %6.2f Mbps  peak %6s Mbps  %8d B  %s\n" "$(basename "$1")" "$(echo "$rate" | awk "{print \$1/1e6}")" "$peak" "$(stat -c %s "$1")" "$q"
  }
  for f in out/r1080-*.mp4 out/reel-30-default.mp4; do score "$f" out/r1080-ref.mp4; done
  for f in out/r720-*.mp4; do score "$f" out/r720-ref.mp4; done
' | tee "$M/out/sweep.txt"
```

`sources.sh`:

```bash
#!/usr/bin/env bash
# sources.sh: does a render's memory grow with the bytes of its sources, and how often does the
# page ask for each one? reel-30 as measured, then the same reel with its third clip on a byte-for-
# byte copy of the camera file under another name (one more 305 MB source, same pictures), both
# with every request the page makes logged by the harness server (REQUEST_LOG).
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
H="$(dirname "$0")"
M=${MEASURE_DIR:-$HOME/planb-scratch/measure}
cp "$H"/*.mjs "$H"/*.sh "$M/" 2>/dev/null || true
cd "$M" || exit 1
[ -f fixtures/camera-b.mp4 ] || cp fixtures/camera.mp4 fixtures/camera-b.mp4
node -e '
const fs = require("fs");
const p = JSON.parse(fs.readFileSync("fixtures/reel-30.json", "utf8"));
p.id = "reel-30-two-cameras";
p.sources = [...p.sources, { ...p.sources[0], id: "src-camera-b", assetId: "camera-b.mp4" }];
p.video = p.video.map((c) => (c.id === "c3" ? { ...c, sourceId: "src-camera-b" } : c));
fs.writeFileSync("fixtures/reel-30-two-cameras.json", JSON.stringify(p));
'
for name in reel-30 reel-30-two-cameras; do
  docker run --rm --cpus=4 --memory=6g --memory-swap=6g --network none -e REQUEST_LOG="out/$name.requests" \
    -v "$M":/m/h -w /m/h planb-measure:1.63.0 node render.mjs "fixtures/$name.json" "$name-src.mp4" 2>&1 \
    | grep -E '"(wallSeconds|bytes|memoryPeakMiB|anonMaxMiB|currentMaxMiB)"'
  echo "requests by $name:"; sort "out/$name.requests" | uniq -c
done
# Is a 720p render the 1080p one, smaller? The 1080p reference scaled to 720x1280 against the
# 720p reference: a project's geometry is relative, or titles and fits would differ.
docker run --rm -v "$M":/m/h -w /m/h planb-measure:1.63.0 sh -c '
  ffmpeg -v info -i out/r1080-ref.mp4 -i out/r720-ref.mp4 -lavfi "[0:v]scale=720:1280:flags=bicubic[a];[a][1:v]ssim" -f null - 2>&1 | grep -oE "SSIM Y:[0-9.]+ .*All:[0-9.]+ \([0-9.]+\)"'
```

`limit.sh`:

```bash
#!/usr/bin/env bash
# limit.sh <project> <out name> <videoBitrate> <memory>: one render at a memory limit, shaped as the
# sidecar runs: the film written to a tmpfs (/scratch, 700m, charged to the container's memory as
# the sidecar's scratch is), swap off, network none. Answers whether it completes and memory.peak.
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
H="$(dirname "$0")"
M=${MEASURE_DIR:-$HOME/planb-scratch/measure}
cp "$H"/*.mjs "$H"/*.sh "$M/" 2>/dev/null || true
docker run --rm --cpus=4 --memory="$4" --memory-swap="$4" --network none \
  --tmpfs /scratch:size=700m -e UPLOAD_DIR=/scratch \
  -v "$M":/m/h -w /m/h planb-measure:1.63.0 sh -c "node render.mjs fixtures/$1 $2 $3; echo exit=\$?; ls -la /scratch" 2>&1 \
  | grep -E '"(wallSeconds|bytes|memoryPeakMiB|anonMaxMiB|currentMaxMiB|memoryMax)"|exit=|scratch|Killed|Error|error' | head -20
```

`rows.sh`:

```bash
#!/usr/bin/env bash
# rows.sh <out name>: the browser's file, then two ffmpeg remuxes of it (video copied, Opus -> AAC-LC
# 48 kHz stereo 128 kbps, faststart), with and without -use_editlist 0, each through check.mjs.
set -uo pipefail
M=${MEASURE_DIR:-$HOME/planb-scratch/measure}
cp "$(dirname "$0")/check.mjs" "$M/" 2>/dev/null || true
docker run --rm -v "$M":/m/h -w /m/h planb-measure:1.63.0 sh -c "
  set -e
  in=out/$1
  ffmpeg -v error -y -i \$in -map 0:v:0 -map 0:a:0 -c:v copy -c:a aac -b:a 128k -ar 48000 -ac 2 -movflags +faststart out/\$(basename \$in .mp4)-aac.mp4
  ffmpeg -v error -y -i \$in -map 0:v:0 -map 0:a:0 -c:v copy -c:a aac -b:a 128k -ar 48000 -ac 2 -movflags +faststart -use_editlist 0 out/\$(basename \$in .mp4)-aac-noelst.mp4
  for f in \$in out/\$(basename \$in .mp4)-aac.mp4 out/\$(basename \$in .mp4)-aac-noelst.mp4; do node check.mjs \$f; done
"
```

`held.sh` (Task 13 runs it):

```bash
#!/usr/bin/env bash
# held.sh <image> [idle]: what a render's kept bytes cost once its browser has exited. The image
# runs as the sidecar does (read-only root, a 700 MiB /scratch tmpfs, 6 GiB with swap off, network
# none), with its own command, or an idle Node when `idle` is given; its cgroup is read at rest,
# while /scratch holds the largest film the output check passes (300,000,000 bytes) beside a 12 MB
# cleaned copy, and after that scratch is emptied.
set -uo pipefail
IMAGE=${1:?usage: held.sh <image> [idle]}
COMMAND=()
[ "${2:-}" = idle ] && COMMAND=(node -e 'setInterval(() => {}, 2 ** 30)')
NAME=planb-held
docker rm -f "$NAME" > /dev/null 2>&1
docker run -d --name "$NAME" --read-only --network none --tmpfs /tmp:mode=1777 \
  --tmpfs /scratch:size=700m,uid=10001,mode=0700 --memory=6g --memory-swap=6g "$IMAGE" "${COMMAND[@]}" > /dev/null
sleep 5
cgroup() {
  docker exec "$NAME" sh -c 'echo "memory.current $(cat /sys/fs/cgroup/memory.current)"; grep -E "^(anon|file|shmem) " /sys/fs/cgroup/memory.stat'
}
echo "== at rest"; cgroup
docker exec "$NAME" sh -c 'mkdir /scratch/job-held \
  && head -c 300000000 /dev/urandom > /scratch/job-held/delivered.mp4 \
  && head -c 12000000 /dev/urandom > /scratch/job-held/cleaned-src-voice.ogg \
  && ls -l /scratch/job-held'
echo "== holding them"; cgroup
docker exec "$NAME" rm -rf /scratch/job-held
echo "== the scratch emptied"; cgroup
docker rm -f "$NAME" > /dev/null
```

Write `$W/shellcheck-harness.sh`:

```bash
#!/usr/bin/env bash
export PATH="$HOME/.local/bin:$PATH"
cd /mnt/d/Aura/spikes/video-mcp-render/plan-b || exit 1
bash -n ./*.sh && echo "bash -n ok"
shellcheck -S warning ./*.sh && echo "shellcheck ok"
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/shellcheck-harness.sh`

Expected: `bash -n ok`, `shellcheck ok`.

- [ ] **Step 2: Prepare: the clips, the projects, the page, the image**

Write `$W/measure-prepare.sh`:

```bash
#!/usr/bin/env bash
# measure-prepare.sh <camera file> <screen recording>: both explicit inputs are read-only.
set -euo pipefail
[ "$#" -eq 2 ] || { echo "usage: measure-prepare.sh <camera file> <screen recording>" >&2; exit 2; }
for input in "$@"; do
  [ -f "$input" ] && [ -r "$input" ] || { echo "missing or unreadable input: $input" >&2; exit 2; }
done
camera_input=$(realpath -- "$1")
screen_input=$(realpath -- "$2")
# A fresh directory prevents prepare.sh/sweep.sh from reusing an earlier fixture or render.
export MEASURE_DIR
MEASURE_DIR=$(mktemp -d "$HOME/aura-video-measure-XXXXXXXX")
printf 'camera=%s\nscreen=%s\n' "$camera_input" "$screen_input" > "$MEASURE_DIR/inputs.txt"
sha256sum -- "$camera_input" "$screen_input" >> "$MEASURE_DIR/inputs.txt"
printf 'measurement directory: %s\n' "$MEASURE_DIR"
bash /mnt/d/Aura/spikes/video-mcp-render/plan-b/prepare.sh "$camera_input" "$screen_input"
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/measure-prepare.sh <existing-camera-file> <existing-screen-file>`,
using WSL paths and quoting each argument. Use the printed measurement directory as `$M`
for every subsequent Task 11–13 command; it replaces the historical `$M` default for this run.
The original camera path, `/mnt/c/Users/Davide/Desktop/Video Industry_corsi.mov`, did not
exist at the 2026-10-05 review and is still absent on 2026-10-06. The original screen
recording exists. Do not substitute footage without recording its provenance and rerunning
the measurements; do not overwrite the 2026-10-02 numbers with predictions.

Expected:
- `built …/measure/dist`;
- the image's `ffmpeg version 5.1.9-0+deb12u1`, `v24.…`, `chromium_headless_shell-…` and the `ssim` and `psnr` filters (Debian's ffmpeg has no `libvmaf`);
- `fixtures/` listing `camera.mp4`, `screen.mp4`, `music-600.wav`, `speech.wav` and the three project files, with `inputs.txt` recording the supplied paths and SHA-256 hashes. The historical camera/screen sizes were 304,744,327 B / 16,160,824 B; replacement footage is not expected to have those sizes.

- [ ] **Step 3: Run the sweep**

Run: `MSYS_NO_PATHCONV=1 wsl bash $M/sweep.sh`, where `$M` is the fresh directory printed by Step 2. The wrapper prevents reuse of old fixtures and renders. It took about 12 minutes with the historical inputs.

Expected, as measured on 2026-10-02 (`$M/out/sweep.txt`). The third column is the busiest one-second rate:
```text
r1080-2500000.mp4        1.80 Mbps  peak   5.38 Mbps   6991909 B  average:41.504373 SSIM Y:0.982564 …
r1080-3000000.mp4        2.08 Mbps  peak   5.37 Mbps   8007407 B  average:42.732390 SSIM Y:0.985550 …
r1080-3500000.mp4        2.34 Mbps  peak   5.38 Mbps   8968026 B  average:43.442905 SSIM Y:0.987363 …
r1080-4000000.mp4        2.63 Mbps  peak   5.37 Mbps  10005352 B  average:44.177655 SSIM Y:0.989026 …
r1080-6000000.mp4        3.74 Mbps  peak   6.01 Mbps  14045434 B  average:47.052589 SSIM Y:0.992629 …
r1080-ref.mp4            9.94 Mbps  peak  19.64 Mbps  36505122 B  SSIM Y:1.000000
reel-30-default.mp4      6.99 Mbps  peak  11.85 Mbps  25812147 B  average:53.126862 SSIM Y:0.996796 …
r720-1500000.mp4         1.06 Mbps  peak   2.98 Mbps   4332983 B  average:40.740821 SSIM Y:0.979696 …
r720-2000000.mp4         1.34 Mbps  peak   2.98 Mbps   5337727 B  average:42.168933 SSIM Y:0.984811 …
r720-2500000.mp4         1.61 Mbps  peak   2.97 Mbps   6323602 B  average:43.645396 SSIM Y:0.987731 …
r720-3000000.mp4         1.89 Mbps  peak   3.03 Mbps   7334692 B  average:45.008854 SSIM Y:0.990044 …
r720-default.mp4         3.31 Mbps  peak   5.54 Mbps  12469858 B  average:50.195152 SSIM Y:0.995159 …
r720-ref.mp4             5.78 Mbps  peak  11.67 Mbps  21424825 B  SSIM Y:1.000000
```
Encoder output is not byte-reproducible: a re-run of the 6 Mbps render, from the harness as committed, had the same 14,045,434 bytes and differed from byte 51, inside the file header. Small differences in a fresh sweep are expected; a different conclusion is a stop.

- [ ] **Step 4: Record it in the PRD**

In `prd.md`, re-read the current anchor `Measured 2026-09-08 using an agent-generated weather demo: the accepted HTML asset`. Insert the paragraph below before it, followed by one blank line. It is a historical example: use your actual measurement date, input provenance/hashes, stack versions and measured numbers. Do not paste the 2026-10-02 date or scores as evidence for a replacement-footage run.

```markdown
**Render bitrate (aura-video-mcp Plan B, 2026-10-02).** Measured on the operator's desktop (Docker
under WSL2, `--cpus=4`) in Playwright 1.63.0's Chrome Headless Shell 153.0.8010.12 with Debian's
ffmpeg 5.1.9 (`spikes/video-mcp-render/plan-b/sweep.sh`), on reel-30: 29 s at 1080×1920 and 30 fps
cut from two real clips, a 1080p camera recording and a screen recording, with crossfades, a title
and a ducked music bed under a voice. Each H.264 track is scored with ffmpeg's SSIM (luma) and PSNR
against a reference rendered at a 20 Mbps target, which itself averaged 9.94 Mbps:
- at 1080×1920, targets of 2.5, 3, 3.5, 4 and 6 Mbps averaged 1.80, 2.08, 2.34, 2.63 and 3.74 Mbps,
  with SSIM 0.9826, 0.9856, 0.9874, 0.9890 and 0.9926 and PSNR 41.5, 42.7, 43.4, 44.2 and 47.1 dB;
  VideoFlow's default (about 12 Mbps) averaged 6.99 Mbps, SSIM 0.9968, 53.1 dB;
- at 720×1280, targets of 1.5, 2, 2.5 and 3 Mbps averaged 1.06, 1.34, 1.61 and 1.89 Mbps, SSIM
  0.9797, 0.9848, 0.9877 and 0.9900; VideoFlow's default averaged 3.31 Mbps, SSIM 0.9952;
- the busiest second never passed 6.01 Mbps below the default, and 11.85 Mbps at it;
- the 720p render is the 1080p one made smaller: the 1080p reference scaled to 720×1280 scores SSIM
  0.9946 against the 720p reference, so a project's geometry scales and no layout changes.

The rules: a render is to target 6 Mbps with the short side at 1080 and 3 Mbps at 720, never wider
than 1920 px; a film long enough that its target would pass Instagram's 300 MB row is to get 85 % of
that budget less the audio's 128 kbps (3,272,000 b/s for 10 minutes at 1080). At 6 Mbps a 60 s reel
is about 29 MB, over the 25 MiB Aura's bridge carries into a chat (`internal/mcp/file.go:20`), which
takes about 54 s of such a reel; at 3 Mbps a 60 s 720p reel is about 15 MB.

This does not establish:
- how the films look to a person: SSIM and PSNR against a reference from the same encoder, on one
  29 s film; no one watched them, and Debian's ffmpeg has no VMAF;
- the rates on other footage: one camera clip and one screen recording; fast motion, grain or
  60 fps were not rendered;
- what Instagram re-encodes the upload to;
- another encoder build: one headless shell, 153.0.8010.12, which stays about 35 % under every
  target it was given.
```

- [ ] **Step 5: Date the spec's limits**

In `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md`:

1. After the §Limits table, whose last row is `| Concurrency | 1 job | S2 used 1.4–2.8 of 4 cores; two at once was not measured. |` (`:296`), add one blank line and:

```markdown
*Measured 2026-10-02 (Plan B; prd.md §12 "Render bitrate"):* the video bitrate is 6 Mbps with the
short side at 1080 and 3 Mbps at 720; a film whose target would pass the 300 MB row gets less.
```

2. After `  measurement behind each.` (`:403`, the end of §Environment variables' "Limits" bullet), add:

```markdown
  *Corrected 2026-10-02 (Plan B):* prd.md has no caps table; each measured limit is recorded in
  §12's Plan B paragraphs and beside its constant in `services/video-mcp/src/jobs/limits.ts`.
```

- [ ] **Step 6: Commit the measurement**

Write `$W/msg-t11a.txt`:

```text
docs(prd): measure the render bitrate per resolution on real clips

The spec left the H.264 target to a measurement on real clips. The
harness (spikes/video-mcp-render/plan-b) renders a 29 s reel cut from
a camera recording and a screen recording in the headless shell the
sidecar ships, at targets from 1.5 to 6 Mbps and at VideoFlow's
default, and scores each against a near-transparent reference. 6 Mbps
at 1080 and 3 Mbps at 720 hold SSIM at 0.9926 and 0.9900 and keep a
60 s reel near 29 MB; a long film gives way to the 300 MB row. The
record says what SSIM against the same encoder does not show.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t11a.txt prd.md docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md spikes/video-mcp-render/plan-b`

Expected: `COMMIT_RC=0`.

- [ ] **Step 7: Write the failing test**

`services/video-mcp/test/unit/limits.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { renderSize, videoBitrate } from '../../src/jobs/limits';

describe('the frame a quality renders', () => {
  it.each([
    [{ width: 1080, height: 1920 }, '1080p', { width: 1080, height: 1920 }],
    [{ width: 1080, height: 1920 }, '720p', { width: 720, height: 1280 }],
    [{ width: 1920, height: 1080 }, '1080p', { width: 1920, height: 1080 }],
    [{ width: 1920, height: 1080 }, '720p', { width: 1280, height: 720 }],
    [{ width: 1080, height: 1080 }, '720p', { width: 720, height: 720 }],
    [{ width: 3840, height: 2160 }, '1080p', { width: 1920, height: 1080 }],
    [{ width: 2560, height: 1080 }, '1080p', { width: 1920, height: 810 }],
    [{ width: 1081, height: 1921 }, '720p', { width: 720, height: 1280 }],
  ] as const)('%o at %s is %o', (size, quality, expected) => {
    expect(renderSize(size, quality)).toEqual(expected);
  });
});

describe('the video bitrate', () => {
  it('is the quality’s measured target for a reel', () => {
    expect(videoBitrate('1080p', 60)).toBe(6_000_000);
    expect(videoBitrate('720p', 60)).toBe(3_000_000);
  });

  it('gives way to the 300 MB row on a long film, with room for the sound and a margin', () => {
    expect(videoBitrate('1080p', 600)).toBe(3_272_000);
    expect((videoBitrate('1080p', 600) + 128_000) * 600 / 8).toBeLessThan(300_000_000);
    expect(videoBitrate('720p', 600)).toBe(3_000_000);
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/limits.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../src/jobs/limits' imported from …`; `VITEST_RC=1`.

- [ ] **Step 8: Write the limits**

In `services/video-mcp/src/studio.ts`, in the `export { … } from '../../../web/src/videoStudio/project';` list, add `type ProjectSize,` between `type OverlayItem,` and `type ProjectSource,`.

`services/video-mcp/src/jobs/limits.ts`:

```ts
// limits.ts — the render's limits, as documented constants beside the code they bound (spec
// §Environment variables: "documented constants in code, not env vars"). The measured ones are
// recorded in prd.md §12 with the measurement behind each.

import type { ProjectSize } from '../studio';

export type Quality = '1080p' | '720p';

/** Instagram's frame-rate row. */
export const FPS_MIN = 23;
export const FPS_MAX = 60;
/** renderer-server's STALL_MS (ServerRenderer.js:940). */
export const STALL_MS = 120_000;
/** Spec §Jobs: a job older than this, counted from its submission and not counting a wait for its
 *  caller (queue.ts waitFor), is failed. */
export const JOB_MAX_AGE_MS = 60 * 60_000;
/** A finished job is answered as long as its download link lives (resolve signs for two hours). */
export const FINISHED_JOB_TTL_MS = 2 * 60 * 60_000;
/** Instagram's size and audio rows. */
const FILM_MAX_BYTES = 300_000_000;
const AUDIO_BITRATE = 128_000;
/** Instagram's horizontal-pixels row. */
const MAX_WIDTH = 1920;
const SHORT_SIDE: Readonly<Record<Quality, number>> = { '1080p': 1080, '720p': 720 };

/**
 * The H.264 target per quality, measured 2026-10-02 on real camera and screen footage (prd.md
 * §12 "Render bitrate"): at 6 Mbps a 1080p reel scores SSIM 0.9926 against a 20 Mbps reference
 * and averages 3.7 Mbps, about 29 MB a minute, so Aura's bridge (25 MiB a file) carries some 54 s
 * of it into a chat; 720p at 3 Mbps scores 0.9900 and averages 1.9 Mbps.
 */
const VIDEO_BITRATE: Readonly<Record<Quality, number>> = { '1080p': 6_000_000, '720p': 3_000_000 };
/** A target is an average the encoder may reach on hard footage; this much of the size budget is
 *  given to it, the rest is margin. */
const SIZE_BUDGET_SHARE = 0.85;

/** The frame a quality renders: the project's shape with the short side at 1080 or 720, never
 *  wider than 1920, both sides even (H.264 4:2:0). */
export function renderSize(size: ProjectSize, quality: Quality): ProjectSize {
  const scale = Math.min(SHORT_SIDE[quality] / Math.min(size.width, size.height), MAX_WIDTH / size.width);
  const even = (side: number): number => Math.max(2, 2 * Math.round((side * scale) / 2));
  return { width: even(size.width), height: even(size.height) };
}

/** The video target for a film this long: the quality's own, unless the 300 MB row needs less. */
export function videoBitrate(quality: Quality, seconds: number): number {
  const budget = (SIZE_BUDGET_SHARE * (FILM_MAX_BYTES * 8)) / seconds - AUDIO_BITRATE;
  return Math.max(250_000, Math.min(VIDEO_BITRATE[quality], Math.floor(budget)));
}
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/limits.test.ts`

Expected: `Tests  10 passed (10)`, `VITEST_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 9: Commit the code**

Write `$W/msg-t11b.txt`:

```text
feat(video-mcp): render at the measured bitrate and frame per quality

A render's frame keeps the project's shape with the short side at 1080
or 720, never wider than Instagram's 1920 px, both sides even for
4:2:0. Its H.264 target is the one measured on real clips: 6 Mbps or
3 Mbps, unless the film is long enough that the 300 MB row needs less,
when it gets 85 % of that budget less the audio's 128 kbps.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t11b.txt services/video-mcp/src/jobs/limits.ts services/video-mcp/src/studio.ts services/video-mcp/test/unit/limits.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 12: Measure the stream copy against Instagram's rows, record it, then the transcode

WebCodecs in the headless shell encodes H.264 and Opus; it has no AAC encoder. Instagram wants AAC, the moov atom at the front, and no edit list. The question this task measures: can ffmpeg copy the video stream untouched and still pass every row?

**Files:**
- Modify: `prd.md` (a paragraph inserted before `Measured 2026-09-08 using an agent-generated weather demo`)
- Create: `services/video-mcp/src/jobs/transcode.ts`
- Test: `services/video-mcp/test/unit/transcode.test.ts`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `transcodeArgs(input: string, output: string, hasAudio: boolean): string[]`;
  - `transcode(input: string, output: string, signal: AbortSignal): Promise<void>`, which probes for an audio stream first.

- [ ] **Step 1: Run the rows**

Run: `MSYS_NO_PATHCONV=1 wsl bash $M/rows.sh reel-30-default.mp4`. The film is from Task 11's sweep.

Expected, three reports from `check.mjs`, as measured on 2026-10-02 (`$M/out/rows-reel-30-default.txt`):
1. **The browser's own file**, `reel-30-default.mp4`:
   - `moovBeforeMdat: true`, `editLists: []`;
   - H.264 `High`, level 40, `1080x1920`, `yuvj420p` (`colorRange: pc`, transfer `iec61966-2-1`, primaries `bt709`), `progressive`, `30/1`, `hasBFrames: 0`;
   - audio `opus`, 48000 Hz, 2 channels;
   - 25,812,147 B.
2. **Remuxed** (video copied, Opus to AAC-LC 48 kHz stereo 128 kbps, `+faststart`), `reel-30-default-aac.mp4`: `editLists` with two entries, one per track. ffmpeg writes them to hide AAC's priming samples, and an edit list fails the container row. 25,822,211 B.
3. **Remuxed with `-use_editlist 0` as well**, `reel-30-default-aac-noelst.mp4`:
   - `editLists: []`, moov first;
   - AAC `LC`, 48000 Hz, 2 channels, 126,901 b/s;
   - the same H.264, but its average frame rate reads `1113600/37139` (29.985) and its duration 29.014844 s;
   - audio 29.034833 s;
   - 25,822,147 B.

- [ ] **Step 2: Record it in the PRD**

In `prd.md`, insert before the line `Measured 2026-09-08 using an agent-generated weather demo: the accepted HTML asset`, followed by one blank line:

```markdown
**Instagram rows (aura-video-mcp Plan B, 2026-10-02).** Measured on the same host and film as
"Render bitrate" (`spikes/video-mcp-render/plan-b/rows.sh`, `check.mjs`). The headless shell writes
H.264 High at level 4.0, 1080×1920, `yuvj420p` (full range, BT.709 primaries, sRGB transfer),
progressive, 30 fps, with no B-frames, so every GOP is closed; its audio is Opus, because WebCodecs
here has no AAC encoder (VideoFlow logs "Using Opus audio (128 kbps) — AAC not available on this
platform"); moov comes first and there is no edit list. Remuxed by ffmpeg 5.1.9 with the video
stream copied and the sound made AAC-LC 48 kHz stereo 128 kbps with `+faststart`, the file passes
every row the spec lists but one: ffmpeg writes two edit lists, one per track, to hide AAC's priming
samples. With `-use_editlist 0` it writes none and every row passes: MP4 with moov first and no edit
list; AAC-LC, 48 kHz, 2 channels, 126,901 b/s; H.264, progressive, closed GOP, 4:2:0; 1080 px wide;
the busiest second 11.85 Mbps; 29.03 s; 25,822,147 bytes. The price is in the video stream's
timing: its average frame rate reads 29.985 instead of 30 and its duration 29.015 s instead of
29 s, because the priming is no longer hidden. The rules: the transcode is to copy the video
stream, make the sound AAC-LC 48 kHz stereo 128 kbps, put moov first and write no edit list; a
film with no sound at all is to get a silent AAC track; and the output check is to verify every
row on the delivered file before anything is saved, with the duration within one frame.

This does not establish:
- that Instagram accepts the file: no upload was made; the rows are the Graph API's "Reel
  specifications" as the spec quotes them;
- that full-range `yuvj420p` with an sRGB transfer looks the same on Instagram's players as
  limited-range BT.709: no row names range or transfer, and nothing was viewed there;
- loudness: the AAC was not compared with the Opus it came from;
- another browser build's encoder: one headless shell, 153.0.8010.12.
```

- [ ] **Step 3: Commit the measurement**

Write `$W/msg-t12a.txt`:

```text
docs(prd): measure the stream copy against Instagram's rows

The headless shell encodes H.264 and Opus only. Remuxed by ffmpeg with
the picture copied and the sound made AAC-LC, the browser's film
passes every row the spec lists except one: ffmpeg writes an edit list
per track to hide AAC's priming. With -use_editlist 0 it writes none
and every row passes, at the price of a video timing that reads 29.985
fps. The record says what was not checked: an actual upload, how full
range plays there, and loudness.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t12a.txt prd.md`

Expected: `COMMIT_RC=0`.

- [ ] **Step 4: Write the failing test**

`services/video-mcp/test/unit/transcode.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { transcodeArgs } from '../../src/jobs/transcode';

describe('the transcode', () => {
  it('copies the picture, makes the sound AAC-LC 48 kHz stereo 128 kbps, front moov, no edit list', () => {
    expect(transcodeArgs('in.mp4', 'out.mp4', true)).toEqual([
      '-v', 'error', '-nostdin', '-y', '-i', 'in.mp4',
      '-map', '0:v:0', '-map', '0:a:0', '-c:v', 'copy', '-c:a', 'aac', '-b:a', '128k', '-ar', '48000', '-ac', '2',
      '-movflags', '+faststart', '-use_editlist', '0', 'out.mp4',
    ]);
  });

  it('gives a film with no sound a silent AAC track as long as its picture', () => {
    const args = transcodeArgs('in.mp4', 'out.mp4', false);
    expect(args.slice(6, 10)).toEqual(['-f', 'lavfi', '-i', 'anullsrc=channel_layout=stereo:sample_rate=48000']);
    expect(args).toContain('-shortest');
    expect(args.slice(args.indexOf('-map'), args.indexOf('-map') + 4)).toEqual(['-map', '0:v:0', '-map', '1:a:0']);
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/transcode.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../src/jobs/transcode' imported from …`; `VITEST_RC=1`.

- [ ] **Step 5: Write the transcode**

`services/video-mcp/src/jobs/transcode.ts`:

```ts
// transcode.ts — the browser's MP4 made into Instagram's (spec §Output format): the H.264 stream
// copied untouched, which passes every video row as the headless shell encodes it (measured
// 2026-10-02, prd.md §12 "Instagram rows"); the Opus track, the only audio WebCodecs encodes here,
// made AAC-LC 48 kHz stereo 128 kbps; the moov atom moved to the front; and no edit list, which
// ffmpeg otherwise writes to hide AAC's priming samples. A film with no sound at all gets a silent
// AAC track, because the check and Instagram's audio row both want one.

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const run = promisify(execFile);

export function transcodeArgs(input: string, output: string, hasAudio: boolean): string[] {
  return [
    '-v', 'error',
    '-nostdin',
    '-y',
    '-i', input,
    ...(hasAudio ? [] : ['-f', 'lavfi', '-i', 'anullsrc=channel_layout=stereo:sample_rate=48000']),
    '-map', '0:v:0',
    '-map', hasAudio ? '0:a:0' : '1:a:0',
    '-c:v', 'copy',
    '-c:a', 'aac',
    '-b:a', '128k',
    '-ar', '48000',
    '-ac', '2',
    ...(hasAudio ? [] : ['-shortest']),
    '-movflags', '+faststart',
    '-use_editlist', '0',
    output,
  ];
}

/** Whether the file has an audio stream at all. */
async function hasAudioStream(path: string, signal: AbortSignal): Promise<boolean> {
  const { stdout } = await run(
    'ffprobe',
    ['-v', 'error', '-select_streams', 'a', '-show_entries', 'stream=index', '-of', 'csv=p=0', path],
    { signal },
  );
  return stdout.trim() !== '';
}

export async function transcode(input: string, output: string, signal: AbortSignal): Promise<void> {
  await run('ffmpeg', transcodeArgs(input, output, await hasAudioStream(input, signal)), {
    signal,
    maxBuffer: 1 << 20,
  });
}
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/transcode.test.ts`

Expected: `Tests  2 passed (2)`, `VITEST_RC=0`. `transcode` itself runs ffmpeg, so the render tier measures it (Task 20, `RENDER_TIER`), and Task 20 shows that tier failing when `-use_editlist 0` is removed.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 6: Commit the code**

Write `$W/msg-t12b.txt`:

```text
feat(video-mcp): make the browser's film Instagram's with one ffmpeg pass

The H.264 stream is copied untouched, which passes every video row as
measured; the Opus becomes AAC-LC 48 kHz stereo 128 kbps, the moov
atom moves to the front, and no edit list is written. A film with no
sound at all gets a silent AAC track as long as its picture, because
Instagram and the output check both want one.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t12b.txt services/video-mcp/src/jobs/transcode.ts services/video-mcp/test/unit/transcode.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 13: Measure a 10-minute film's memory, record it, then the length and the out-of-memory sentence

The spec fixed 10 minutes but measured memory only at 60 s (S2: 1,549 MiB peak), and asked the plan to measure 10 minutes before fixing the container's limit. The amendment of 2026-10-02 adds a fourth question, Q1's: a checked film may wait for its caller's next call before it is saved. Four questions:
- Does memory grow with the sources' bytes?
- Does the page fetch a source more than once?
- What does a 10-minute film need?
- What does a film waiting to be saved cost, once the browser has exited?

**Files:**
- Modify: `prd.md` (a paragraph inserted before `Measured 2026-09-08 using an agent-generated weather demo`)
- Modify: `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md` (dated notes after `:162`, `:192` and the Limits table)
- Modify: `services/video-mcp/src/jobs/limits.ts` (`FILM_MAX_SECONDS`)
- Create: `services/video-mcp/src/jobs/memory.ts`
- Test: `services/video-mcp/test/unit/memory.test.ts`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `FILM_MAX_SECONDS = 600` (`limits.ts`);
  - `oomKillsIn(events: string): number | undefined` and `limitIn(max: string): number | undefined`;
  - `oomKills(): Promise<number>`, read from `/sys/fs/cgroup/memory.events`;
  - `outOfMemory(): Promise<string>`: "the renderer ran out of memory (the renderer may use N GiB): render a shorter film, or use smaller source files", the limit read from `memory.max`.

- [ ] **Step 1: Memory against source bytes, and the fetches**

Run: `MSYS_NO_PATHCONV=1 wsl bash $M/sources.sh`

Expected, as measured on 2026-10-02:
- reel-30: `memoryPeakMiB` 1869, `anonMaxMiB` 1266, `wallSeconds` 42.7. Requests: one `GET` each for `camera.mp4`, `music-600.wav`, `screen.mp4` and `speech.wav`, all `range=none`.
- reel-30-two-cameras (one more 305 MB source, the same pictures): 2110, 1501 and 52.4. One `GET` per source again.
- the geometry check: `SSIM Y:0.994593`.

- [ ] **Step 2: A 10-minute film, twice**

These two renders take about 25 minutes each here. Run them one at a time.

Run: `MSYS_NO_PATHCONV=1 wsl bash $M/run.sh film-600.json film-600-default.mp4`. That is VideoFlow's default bitrate, at the 6 GiB limit `run.sh` sets.

Expected: `wallSeconds` 1499.6, `bytes` 373384681 (over the 300 MB row), `memoryPeakMiB` 5769, `anonMaxMiB` 4963.

Run: `MSYS_NO_PATHCONV=1 wsl bash $M/limit.sh film-600.json film-600-6m-5g.mp4 6000000 5g`. That is 6 Mbps under 5 GiB, with the film written to a 700 MiB tmpfs charged to the same limit, as the sidecar's scratch will be.

Expected: `exit=0`, `wallSeconds` 1358.2, `bytes` 214304995, `memoryPeakMiB` 5120 (the limit itself: the kernel reclaimed page cache to finish), `anonMaxMiB` 4355.

- [ ] **Step 3: What a film waiting to be saved costs**

A job keeps its checked film and its cleaned copies in its scratch until a token of its caller's lets it save them (Q1). That scratch is the `/scratch` tmpfs. `held.sh` puts the largest film the output check passes, 300,000,000 bytes, and a 12 MB copy there, in a container shaped as the sidecar's, with no browser running. It reads the cgroup before, during and after.

Run: `MSYS_NO_PATHCONV=1 wsl bash /mnt/d/Aura/spikes/video-mcp-render/plan-b/held.sh planb-measure:1.63.0 idle`

Expected, as measured on 2026-10-02 (an idle Node; the sidecar's own image is read again in Task 20):
```text
== at rest
memory.current 16166912
anon 12439552
file 684032
shmem 0
…
== holding them
memory.current 328630272
anon 12455936
file 312705024
shmem 312004608
== the scratch emptied
memory.current 15355904
anon 12447744
file 708608
shmem 0
```
`shmem` is the held bytes to the page: 312,000,000 bytes of files read 312,004,608. With swap off, nothing can reclaim them. They leave when the files do.

- [ ] **Step 4: Record it in the PRD**

In `prd.md`, insert before the line `Measured 2026-09-08 using an agent-generated weather demo: the accepted HTML asset`, followed by one blank line:

```markdown
**Render memory (aura-video-mcp Plan B, 2026-10-02).** Measured on the same host as "Render bitrate"
(`spikes/video-mcp-render/plan-b/sources.sh`, `run.sh`, `limit.sh`), the container's cgroup v2
counters read every 500 ms. reel-30 at VideoFlow's default bitrate peaked at 1,869 MiB with at most
1,266 MiB of anonymous memory; the same reel with one more 305 MB source peaked at 2,110 and 1,501
MiB, about 0.81 MiB of anonymous memory per MiB of source. The page asked for each source once,
whole, with no Range request. film-600, ten minutes at 1080×1920 and 30 fps from the same clips,
completed at the default bitrate under a 6 GiB limit in 1,499.6 s, peaking at 5,769 MiB with 4,963
MiB anonymous, in a 373,384,681-byte file, over Instagram's 300 MB row; at a 6 Mbps target under a
5 GiB limit, its film written to a 700 MiB tmpfs inside that limit, it completed in 1,358.2 s at the
limit itself (5,120 MiB, the kernel reclaiming page cache), with 4,355 MiB anonymous and 214,304,995
bytes. Read through these renders, anonymous memory is about 847 MiB + 0.81 × the sources' MiB +
5.41 MiB per second of film. A film waiting to be saved lives on that tmpfs, and the cgroup charges
it byte for byte as shmem: a 300,000,000-byte film beside a 12 MB copy read 312,004,608 bytes of
shmem with no browser running (`held.sh`), which with swap off nothing reclaims, and 0 once the
files went. Beside a 10-minute render's 5,769 MiB it would leave about 77 MiB of 6 GiB. The rules:
a film may last 10 minutes; the sidecar's container is to get 6 GiB with swap off, its job scratch
a 700 MiB tmpfs inside that limit; the proxy is to stream each source from its signed link as the
page asks, keeping no copy, which would be charged to the same limit; a job whose film waits for
its caller is to keep the queue's one slot meanwhile, so a held film never shares the limit with
another render; and a render the kernel kills for memory is to fail saying so, with the limit it
had.

This does not establish:
- the lab VM or the mini-PC: one desktop under WSL2 with 4 CPUs; seconds and page cache differ
  elsewhere;
- that 6 GiB always suffices: the line has three unknowns and passes through three renders by
  construction, so it is a reading, not a model; it also leaves out the encoder's rate, which moved
  anonymous memory by 608 MiB between the two 10-minute films. Past roughly 2 GB more source bytes
  than these films used, a 10-minute film can be killed, and then fails with that sentence;
- whether the browser needs renderer-server's 4,096 MB V8 heap flag: not varied;
- two renders at once: the queue runs one;
- a held film beside a running render: the 77 MiB is arithmetic on two separate readings, and a
  render's own peak includes page cache the kernel could have reclaimed.
```

- [ ] **Step 5: Date the spec's memory sentences**

In `docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md`:

1. After `     fetched once and cached for the job.` (`:162`), add:

```markdown
     *Superseded 2026-10-02 (Plan B; prd.md §12 "Render memory"):* the page asks for each source
     once per pass, whole, so the proxy streams it from its signed link and keeps no copy: a copy
     on the job's tmpfs would be charged to the renderer's own memory limit (operator question 6).
```

2. After `  (1,549 MiB peak at 60 s): the plan measures a 10-minute film before fixing the container limit.` (`:192`), add:

```markdown
  *Measured 2026-10-02 (Plan B; prd.md §12 "Render memory"):* a 10-minute film peaked at 5,769 MiB
  at VideoFlow's default bitrate; the container gets 6 GiB (operator question 3).
```

3. After the note Task 11 added below the §Limits table, add one line:

```markdown
*Measured 2026-10-02 (prd.md §12 "Render memory"):* the film length stays 10 minutes.
```

- [ ] **Step 6: Commit the measurement**

Write `$W/msg-t13a.txt`:

```text
docs(prd): measure a ten-minute film's memory before fixing the limit

The spec kept films to ten minutes but had measured memory at 60 s
only. A ten-minute film from real clips peaked at 5,769 MiB at
VideoFlow's default bitrate under 6 GiB, and completed at 6 Mbps under
5 GiB only by reclaiming page cache. Memory grows with the sources'
bytes, and the page asks for each source once, whole, so the proxy
streams instead of keeping a copy charged to the same limit. A film
waiting for its caller to save it is charged too, byte for byte, as
shmem nothing reclaims, so its job keeps the queue's one slot. The
rule is 6 GiB with the scratch tmpfs inside it, and a kill that says
so; the record says the fitted line is a reading of three renders,
not a model.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t13a.txt prd.md docs/superpowers/specs/2026-09-30-aura-video-mcp-design.md`

Expected: `COMMIT_RC=0`.

- [ ] **Step 7: Write the failing test**

`services/video-mcp/test/unit/memory.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { limitIn, oomKills, oomKillsIn, outOfMemory } from '../../src/jobs/memory';

describe('the container’s memory counters', () => {
  it('read the kills cgroup v2 records', () => {
    expect(oomKillsIn('low 0\nhigh 0\nmax 12\noom 2\noom_kill 1\noom_group_kill 0\n')).toBe(1);
    expect(oomKillsIn('')).toBeUndefined();
  });

  it('read a limit, and no limit when there is none', () => {
    expect(limitIn('6442450944\n')).toBe(6_442_450_944);
    expect(limitIn('max\n')).toBeUndefined();
    expect(limitIn('')).toBeUndefined();
  });

  it('read this container’s own counters, and word a kill with the limit when there is one', async () => {
    expect(await oomKills()).toBeGreaterThanOrEqual(0);
    expect(await outOfMemory()).toMatch(/^the renderer ran out of memory( \(the renderer may use [\d.]+ GiB\))?: render a shorter film, or use smaller source files$/);
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/memory.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../src/jobs/memory' imported from …`; `VITEST_RC=1`.

- [ ] **Step 8: Write the memory reader and the film length**

`services/video-mcp/src/jobs/memory.ts`:

```ts
// memory.ts — whether the kernel killed part of the render for memory. A render's memory grows
// with the film and with its sources (prd.md §12 "Render memory": measured 2026-10-02, about
// 850 MiB + 0.81 × source MiB + 5.4 MiB per second of film, unreclaimable), and the container's
// limit is fixed by Compose. When the browser dies, the container's own cgroup v2 counters say
// whether it was the limit, so the job can say so instead of "Target crashed".

import { readFile } from 'node:fs/promises';

const CGROUP = '/sys/fs/cgroup';

/** `oom_kill` from cgroup v2's memory.events, or undefined when the text has none. */
export function oomKillsIn(events: string): number | undefined {
  const count = /^oom_kill (\d+)$/m.exec(events)?.[1];
  return count === undefined ? undefined : Number(count);
}

/** memory.max in bytes, or undefined when it is `max` (no limit) or unreadable. */
export function limitIn(max: string): number | undefined {
  const bytes = Number(max.trim());
  return Number.isFinite(bytes) && bytes > 0 ? bytes : undefined;
}

async function read(file: string): Promise<string> {
  try {
    return await readFile(`${CGROUP}/${file}`, 'utf8');
  } catch {
    return '';
  }
}

export async function oomKills(): Promise<number> {
  return oomKillsIn(await read('memory.events')) ?? 0;
}

/** The sentence a render killed for memory fails with. */
export async function outOfMemory(): Promise<string> {
  const limit = limitIn(await read('memory.max'));
  const allowed = limit === undefined ? '' : ` (the renderer may use ${String(Math.round((limit / 2 ** 30) * 10) / 10)} GiB)`;
  return `the renderer ran out of memory${allowed}: render a shorter film, or use smaller source files`;
}
```

In `services/video-mcp/src/jobs/limits.ts`, replace:

```ts
/** Instagram's frame-rate row. */
export const FPS_MIN = 23;
```

with:

```ts
/** Spec §Limits, kept after the 10-minute film was measured (prd.md §12 "Render memory"). */
export const FILM_MAX_SECONDS = 600;
/** Instagram's frame-rate row. */
export const FPS_MIN = 23;
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/memory.test.ts test/unit/limits.test.ts`

Expected: `Tests  13 passed (13)`, `VITEST_RC=0`. On a host with no cgroup v2 limit, the live sentence has no "(the renderer may use … GiB)", and the test accepts both forms.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 9: Commit the code**

Write `$W/msg-t13b.txt`:

```text
feat(video-mcp): keep films to ten minutes and name a kill for memory

A render's memory grows with the film and with its sources, and the
container's limit is Compose's. When the browser dies, the container's
own cgroup v2 counters say whether the kernel killed it for memory, so
the job can say "the renderer ran out of memory", with the limit it
had, instead of "Target crashed".

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t13b.txt services/video-mcp/src/jobs/memory.ts services/video-mcp/src/jobs/limits.ts services/video-mcp/test/unit/memory.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 14: The job queue: one at a time, the watchdog, the age limit

**Review corrections R1–R3.** `waitFor` pauses the queue's clocks only while waiting for
credentials; all such waits share Task 17's fixed claim deadline. Cancellation must abort
an active upload as well as browser work. The revised queue/status contract must preserve
and expose known accepted assets even when the terminal state is `failed` or `cancelled`.
The original `Delivery`/`JobView` and tests below do not yet provide that record; extend
them and Task 18's published schema in the same change, with an identity-isolation test.

Renders run one at a time, first in first out (spec §Architecture 3: S2 used 1.4–2.8 of 4 cores, and two at once was never measured). A job belongs to the identity that submitted it, and another identity's job id is answered exactly as one that does not exist.

The queue owns the three ways a running job stops early, and says which:
- cancelled;
- stalled: "the render stalled: no progress for 120 s", renderer-server's own watchdog (`ServerRenderer.js:940`);
- too old: "the job is older than 60 minutes", counted from when it was submitted, queue time included, and not counting a wait for its caller.

The runner only has to honour the `AbortSignal` it is given. Progress runs from 0 to 1 across the six steps of spec §Jobs 2, never moves back, and every report feeds the watchdog.

A job whose film waits for its caller's next call (Q1, Task 17) waits inside `controls.waitFor`. That stops both clocks for the wait and starts them again after it. A job waiting on its caller has not stalled. Its age stops too, so the 15 minutes Q1 grants are never cut short by a long render, and the wait itself is bounded by `CLAIM_MS`.

A request carries what the job will read, taken by `video_render_start` (Task 18): the project, and a signed link to every asset it plays. A finished job is forgotten after `FINISHED_JOB_TTL_MS` (2 h), the life of its download link.

**Files:**
- Create: `services/video-mcp/src/jobs/queue.ts`
- Test: `services/video-mcp/test/unit/queue.test.ts`

**Interfaces:**
- Consumes: `FINISHED_JOB_TTL_MS`, `JOB_MAX_AGE_MS`, `STALL_MS`, `Quality` (Task 11); `ResolvedAsset` (Task 3); `VideoProject` (Task 5); `IDENTITY`, `OTHER_IDENTITY` (Task 2's `test/support/tokens.ts`); `sampleProject` (Task 5's `test/support/projects.ts`).
- Produces:
  - `type JobState = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'`;
  - `STEPS` (`analyses`, `compile`, `render`, `transcode`, `check`, `upload`, each `{start, share}`) and `type Step`;
  - `interface JobRequest { identity; projectAssetId; quality: Quality; project: VideoProject; sources: ReadonlyMap<string, ResolvedAsset> }`;
  - `interface Delivery { filmAssetId; fileName; projectAssetId; bytes; seconds; width; height; downloadUrl; downloadExpiresAt }`;
  - `interface JobView { jobId; state; queuePosition: number | null; progress; step: Step | null; projectAssetId; quality; createdAt; finishedAt: string | null; result: Delivery | null; error: string | null }`;
  - `interface RunControls { signal: AbortSignal; progress(step: Step, fraction: number): void; waitFor<T>(work: Promise<T>): Promise<T> }` and `type Runner = (request, controls) => Promise<Delivery>`;
  - `class JobQueue(runner: Runner, now?: () => number)` with `submit(request): JobView`, `view(identity, jobId): JobView | undefined`, `cancel(identity, jobId): JobView | undefined`, `cancelAll(): void` and `idle(): Promise<void>`.

- [ ] **Step 1: Write the failing test**

`services/video-mcp/test/unit/queue.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { JobQueue, type Delivery, type JobRequest, type RunControls } from '../../src/jobs/queue';
import { sampleProject } from '../support/projects';
import { IDENTITY, OTHER_IDENTITY } from '../support/tokens';

const PROJECT = '8c7b6a59-4837-4261-9504-a3b2c1d0e9f8';
const request: JobRequest = { identity: IDENTITY, projectAssetId: PROJECT, quality: '1080p', project: sampleProject(), sources: new Map() };
const delivery: Delivery = {
  filmAssetId: '6e5d4c3b-2a19-4807-b6a5-948372615041',
  fileName: 'Aura-memory.mp4',
  projectAssetId: PROJECT,
  bytes: 24_876_543,
  seconds: 30,
  width: 1080,
  height: 1920,
  downloadUrl: 'http://127.0.0.1:3900/b/k.mp4?X-Amz-Signature=s',
  downloadExpiresAt: '2026-10-02T14:00:00.000Z',
};

/** A runner each test drives by hand: it reports progress, finishes or fails when told to, and
 *  rejects with the signal's reason when the queue aborts it, as a real job must. */
function manualRunner() {
  const started: { request: JobRequest; controls: RunControls; finish: (result: Delivery) => void; fail: (error: Error) => void }[] = [];
  const runner = (job: JobRequest, controls: RunControls) =>
    new Promise<Delivery>((resolve, reject) => {
      controls.signal.addEventListener('abort', () => reject(controls.signal.reason as Error));
      started.push({ request: job, controls, finish: resolve, fail: reject });
    });
  return { runner, started };
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'Date'] });
  vi.setSystemTime(new Date('2026-10-02T12:00:00Z'));
});

afterEach(() => {
  vi.useRealTimers();
});

describe('the queue', () => {
  it('runs one job at a time, in order, and says where each waiting job stands', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const first = queue.submit(request);
    const second = queue.submit(request);
    const third = queue.submit({ ...request, quality: '720p' });
    expect([first.state, first.queuePosition]).toEqual(['running', null]);
    expect([second.state, second.queuePosition]).toEqual(['queued', 1]);
    expect(queue.view(IDENTITY, third.jobId)?.queuePosition).toBe(2);
    expect(started).toHaveLength(1);
    started[0]?.finish(delivery);
    await vi.waitFor(() => expect(started).toHaveLength(2));
    expect(queue.view(IDENTITY, first.jobId)).toMatchObject({ state: 'succeeded', progress: 1, result: delivery, error: null });
    expect(queue.view(IDENTITY, third.jobId)?.queuePosition).toBe(1);
  });

  it('fills the bar step by step and never moves it back', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const job = queue.submit(request);
    const { controls } = started[0] ?? { controls: undefined };
    controls?.progress('analyses', 1);
    controls?.progress('render', 0.5);
    expect(queue.view(IDENTITY, job.jobId)).toMatchObject({ step: 'render', progress: 0.47 });
    controls?.progress('render', 0.2);
    expect(queue.view(IDENTITY, job.jobId)?.progress).toBe(0.47);
    controls?.progress('upload', 2);
    expect(queue.view(IDENTITY, job.jobId)?.progress).toBe(1);
  });

  it('answers another identity’s job exactly as one that does not exist, and will not cancel it', () => {
    const { runner } = manualRunner();
    const queue = new JobQueue(runner);
    const job = queue.submit(request);
    expect(queue.view(OTHER_IDENTITY, job.jobId)).toBeUndefined();
    expect(queue.view(OTHER_IDENTITY, 'no-such-job')).toBeUndefined();
    expect(queue.cancel(OTHER_IDENTITY, job.jobId)).toBeUndefined();
    expect(queue.view(IDENTITY, job.jobId)?.state).toBe('running');
  });
});

describe('a job stops early', () => {
  it('when cancelled while it waits, without ever running', () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    queue.submit(request);
    const waiting = queue.submit(request);
    expect(queue.cancel(IDENTITY, waiting.jobId)).toMatchObject({ state: 'cancelled', queuePosition: null, error: null });
    expect(started).toHaveLength(1);
  });

  it('when cancelled while it runs: its signal aborts, and the next job starts', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const running = queue.submit(request);
    queue.submit(request);
    queue.cancel(IDENTITY, running.jobId);
    expect(started[0]?.controls.signal.aborted).toBe(true);
    await vi.waitFor(() => expect(started).toHaveLength(2));
    expect(queue.view(IDENTITY, running.jobId)).toMatchObject({ state: 'cancelled', error: null });
  });

  it('when it reports no progress for 120 s, as renderer-server’s watchdog fails it', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const job = queue.submit(request);
    vi.advanceTimersByTime(100_000);
    started[0]?.controls.progress('render', 0.1);
    vi.advanceTimersByTime(100_000);
    expect(started[0]?.controls.signal.aborted).toBe(false);
    vi.advanceTimersByTime(20_001);
    await queue.idle();
    expect(queue.view(IDENTITY, job.jobId)).toMatchObject({ state: 'failed', error: 'the render stalled: no progress for 120 s' });
  });

  it('not while it waits for its caller, and by the watchdog again once that wait is over', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const job = queue.submit(request);
    let answer: (token: string) => void = () => undefined;
    const waited = started[0]?.controls.waitFor(new Promise<string>((resolve) => (answer = resolve)));
    vi.advanceTimersByTime(10 * 60_000);
    expect(started[0]?.controls.signal.aborted).toBe(false);
    answer('fresh');
    await expect(waited).resolves.toBe('fresh');
    vi.advanceTimersByTime(120_001);
    await queue.idle();
    expect(queue.view(IDENTITY, job.jobId)).toMatchObject({ state: 'failed', error: 'the render stalled: no progress for 120 s' });
  });

  it('when it is older than 60 minutes, counted from when it was submitted', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const first = queue.submit(request);
    const second = queue.submit(request);
    for (let minute = 0; minute < 61; minute += 1) {
      vi.advanceTimersByTime(60_000);
      started[0]?.controls.progress('render', minute / 61);
    }
    await queue.idle();
    expect(queue.view(IDENTITY, first.jobId)).toMatchObject({ state: 'failed', error: 'the job is older than 60 minutes' });
    expect(queue.view(IDENTITY, second.jobId)).toMatchObject({ state: 'failed', error: 'the job is older than 60 minutes' });
    expect(started).toHaveLength(1);
  });

  it('when it is older than 60 minutes, not counting the time it waited for its caller', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const job = queue.submit(request);
    const minutes = (count: number): void => {
      for (let minute = 0; minute < count; minute += 1) {
        vi.advanceTimersByTime(60_000);
        started[0]?.controls.progress('render', 0.5);
      }
    };
    minutes(50);
    let answer: (token: string) => void = () => undefined;
    const waited = started[0]?.controls.waitFor(new Promise<string>((resolve) => (answer = resolve)));
    vi.advanceTimersByTime(15 * 60_000);
    answer('fresh');
    await waited;
    minutes(9);
    expect(started[0]?.controls.signal.aborted).toBe(false);
    minutes(2);
    await queue.idle();
    expect(queue.view(IDENTITY, job.jobId)).toMatchObject({ state: 'failed', error: 'the job is older than 60 minutes' });
  });

  it('when its runner fails, with the runner’s own reason', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const job = queue.submit(request);
    started[0]?.fail(new Error('beach.mp4: the object store answered 404'));
    await queue.idle();
    expect(queue.view(IDENTITY, job.jobId)).toMatchObject({ state: 'failed', error: 'beach.mp4: the object store answered 404', result: null });
  });

  it('all at once when the process stops: the running one aborted, the waiting ones never run', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const running = queue.submit(request);
    const waiting = queue.submit(request);
    queue.cancelAll();
    await queue.idle();
    expect([queue.view(IDENTITY, running.jobId)?.state, queue.view(IDENTITY, waiting.jobId)?.state]).toEqual(['cancelled', 'cancelled']);
    expect(started).toHaveLength(1);
  });

  it('and a finished one cancelled is answered as it finished', async () => {
    const { runner, started } = manualRunner();
    const queue = new JobQueue(runner);
    const job = queue.submit(request);
    started[0]?.finish(delivery);
    await queue.idle();
    expect(queue.cancel(IDENTITY, job.jobId)?.state).toBe('succeeded');
  });
});

it('forgets a finished job once its download link has expired', async () => {
  const { runner, started } = manualRunner();
  const queue = new JobQueue(runner);
  const job = queue.submit(request);
  started[0]?.finish(delivery);
  await queue.idle();
  vi.advanceTimersByTime(2 * 60 * 60_000);
  expect(queue.view(IDENTITY, job.jobId)?.state).toBe('succeeded');
  vi.advanceTimersByTime(1);
  expect(queue.view(IDENTITY, job.jobId)).toBeUndefined();
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/queue.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../src/jobs/queue' imported from …/test/unit/queue.test.ts`; `VITEST_RC=1`.

- [ ] **Step 3: Write the queue**

`services/video-mcp/src/jobs/queue.ts`:

```ts
// queue.ts — render jobs, one at a time, first in first out (spec §Architecture 3: S2 used 1.4–2.8
// of 4 cores, and two at once was never measured). A job belongs to the identity that submitted it:
// another identity's job id is answered exactly as one that does not exist. The queue owns the
// three ways a running job stops early — cancelled, stalled, too old — and says which; the runner
// only has to honour the signal it is given.

import { randomUUID } from 'node:crypto';
import type { ResolvedAsset } from '../internalApi';
import type { VideoProject } from '../studio';
import { FINISHED_JOB_TTL_MS, JOB_MAX_AGE_MS, STALL_MS, type Quality } from './limits';

export type JobState = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled';

/** The steps of spec §Jobs 2, with the share of the bar each one fills. Rendering dominates. */
export const STEPS = {
  analyses: { start: 0, share: 0.1 },
  compile: { start: 0.1, share: 0.02 },
  render: { start: 0.12, share: 0.7 },
  transcode: { start: 0.82, share: 0.08 },
  check: { start: 0.9, share: 0.05 },
  upload: { start: 0.95, share: 0.05 },
} as const;
export type Step = keyof typeof STEPS;

export interface JobRequest {
  readonly identity: string;
  readonly projectAssetId: string;
  readonly quality: Quality;
  /** Everything the job reads, taken by video_render_start with its caller's token (played.ts):
   *  the project, and a signed link to every asset it plays. */
  readonly project: VideoProject;
  readonly sources: ReadonlyMap<string, ResolvedAsset>;
}

export interface Delivery {
  readonly filmAssetId: string;
  readonly fileName: string;
  /** The version rendered: the one submitted, or the one the analyses saved. */
  readonly projectAssetId: string;
  readonly bytes: number;
  readonly seconds: number;
  readonly width: number;
  readonly height: number;
  readonly downloadUrl: string;
  readonly downloadExpiresAt: string;
}

export interface JobView {
  readonly jobId: string;
  readonly state: JobState;
  /** 1 for the next job to run; null once it runs. */
  readonly queuePosition: number | null;
  readonly progress: number;
  readonly step: Step | null;
  readonly projectAssetId: string;
  readonly quality: Quality;
  readonly createdAt: string;
  readonly finishedAt: string | null;
  readonly result: Delivery | null;
  readonly error: string | null;
}

export interface RunControls {
  readonly signal: AbortSignal;
  /** Reports `fraction` (0–1) of `step` done; every report feeds the stall watchdog. */
  progress(step: Step, fraction: number): void;
  /** Waits for `work` with both clocks stopped, the stall watchdog and the age limit, because the
   *  job is waiting on its caller and not rendering; both run on when the wait is over. */
  waitFor<T>(work: Promise<T>): Promise<T>;
}

export type Runner = (request: JobRequest, controls: RunControls) => Promise<Delivery>;

interface Job {
  readonly id: string;
  readonly request: JobRequest;
  readonly createdAt: number;
  state: JobState;
  progress: number;
  step: Step | null;
  finishedAt: number | null;
  result: Delivery | null;
  error: string | null;
  controller: AbortController | null;
}

export class JobQueue {
  private readonly jobs = new Map<string, Job>();
  private readonly waiting: Job[] = [];
  private running: Promise<void> | null = null;

  constructor(
    private readonly runner: Runner,
    private readonly now: () => number = Date.now,
  ) {}

  submit(request: JobRequest): JobView {
    this.forgetOld();
    const job: Job = {
      id: randomUUID(),
      request,
      createdAt: this.now(),
      state: 'queued',
      progress: 0,
      step: null,
      finishedAt: null,
      result: null,
      error: null,
      controller: null,
    };
    this.jobs.set(job.id, job);
    this.waiting.push(job);
    this.drain();
    return this.viewOf(job);
  }

  view(identity: string, jobId: string): JobView | undefined {
    this.forgetOld();
    const job = this.owned(identity, jobId);
    return job === undefined ? undefined : this.viewOf(job);
  }

  /** Stops a queued or running job; a finished one is answered as it finished. */
  cancel(identity: string, jobId: string): JobView | undefined {
    const job = this.owned(identity, jobId);
    if (job === undefined) return undefined;
    if (job.state === 'queued') {
      this.waiting.splice(this.waiting.indexOf(job), 1);
      this.finish(job, 'cancelled', null);
    } else if (job.state === 'running') {
      job.controller?.abort(new JobStopped('cancelled', 'cancelled'));
    }
    return this.viewOf(job);
  }

  /** Stops every queued and running job: the process is going away. */
  cancelAll(): void {
    for (const job of this.waiting.splice(0)) this.finish(job, 'cancelled', null);
    for (const job of this.jobs.values()) {
      if (job.state === 'running') job.controller?.abort(new JobStopped('cancelled', 'cancelled'));
    }
  }

  /** Resolves when nothing is running or waiting. */
  async idle(): Promise<void> {
    while (this.running !== null) await this.running;
  }

  private owned(identity: string, jobId: string): Job | undefined {
    const job = this.jobs.get(jobId);
    return job?.request.identity === identity ? job : undefined;
  }

  private viewOf(job: Job): JobView {
    const position = this.waiting.indexOf(job);
    return {
      jobId: job.id,
      state: job.state,
      queuePosition: position === -1 ? null : position + 1,
      progress: Math.round(job.progress * 1000) / 1000,
      step: job.step,
      projectAssetId: job.request.projectAssetId,
      quality: job.request.quality,
      createdAt: new Date(job.createdAt).toISOString(),
      finishedAt: job.finishedAt === null ? null : new Date(job.finishedAt).toISOString(),
      result: job.result,
      error: job.error,
    };
  }

  private finish(job: Job, state: JobState, error: string | null, result: Delivery | null = null): void {
    job.state = state;
    job.error = error;
    job.result = result;
    job.finishedAt = this.now();
    job.controller = null;
    if (state === 'succeeded') job.progress = 1;
  }

  private forgetOld(): void {
    for (const [id, job] of this.jobs) {
      if (job.finishedAt !== null && this.now() - job.finishedAt > FINISHED_JOB_TTL_MS) this.jobs.delete(id);
    }
  }

  private drain(): void {
    if (this.running !== null) return;
    const next = this.waiting.shift();
    if (next === undefined) return;
    this.running = this.run(next).finally(() => {
      this.running = null;
      this.drain();
    });
  }

  private async run(job: Job): Promise<void> {
    let deadline = job.createdAt + JOB_MAX_AGE_MS;
    if (deadline <= this.now()) {
      this.finish(job, 'failed', TOO_OLD);
      return;
    }
    const controller = new AbortController();
    job.controller = controller;
    job.state = 'running';
    let stall: ReturnType<typeof setTimeout> | undefined;
    let old: ReturnType<typeof setTimeout> | undefined;
    const watch = (): void => {
      clearTimeout(stall);
      stall = setTimeout(() => controller.abort(new JobStopped('failed', STALLED)), STALL_MS);
    };
    const age = (): void => {
      clearTimeout(old);
      old = setTimeout(() => controller.abort(new JobStopped('failed', TOO_OLD)), deadline - this.now());
    };
    watch();
    age();
    try {
      const result = await this.runner(job.request, {
        signal: controller.signal,
        progress: (step, fraction) => {
          watch();
          const { start, share } = STEPS[step];
          job.step = step;
          job.progress = Math.max(job.progress, start + share * Math.min(1, Math.max(0, fraction)));
        },
        waitFor: async (work) => {
          clearTimeout(stall);
          clearTimeout(old);
          const since = this.now();
          try {
            return await work;
          } finally {
            deadline += this.now() - since;
            watch();
            age();
          }
        },
      });
      controller.signal.throwIfAborted();
      this.finish(job, 'succeeded', null, result);
    } catch (error) {
      const stopped = controller.signal.aborted ? controller.signal.reason : error;
      if (stopped instanceof JobStopped) this.finish(job, stopped.state, stopped.state === 'cancelled' ? null : stopped.message);
      else this.finish(job, 'failed', error instanceof Error ? error.message : String(error));
    } finally {
      clearTimeout(stall);
      clearTimeout(old);
    }
  }
}

const STALLED = `the render stalled: no progress for ${String(STALL_MS / 1000)} s`;
const TOO_OLD = `the job is older than ${String(JOB_MAX_AGE_MS / 60_000)} minutes`;

/** Why the queue stopped a job; the runner sees it as its signal's reason. */
class JobStopped extends Error {
  constructor(
    readonly state: 'failed' | 'cancelled',
    message: string,
  ) {
    super(message);
    this.name = 'JobStopped';
  }
}
```

- [ ] **Step 4: Run it to see it pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/queue.test.ts`

Expected: `Tests  13 passed (13)`, `VITEST_RC=0`. Among them, "when it is older than 60 minutes, not counting the time it waited for its caller" runs 50 minutes, waits 15 for its caller, runs 9 more and is still alive, then fails once 60 counted minutes have passed. With the line `deadline += this.now() - since;` taken out, it fails (measured in scratch: `AssertionError: expected true to be false`).

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 5: Commit**

Write `$W/msg-t14.txt`:

```text
feat(video-mcp): queue renders one at a time, with a watchdog and an age

Two renders at once were never measured, so the queue runs one, first
in first out. It owns the three ways a running job stops early and
says which: cancelled, stalled for 120 s as renderer-server's own
watchdog judges, or older than 60 minutes from submission. A job whose
film waits for its caller's next call does it with both clocks
stopped: it is waiting on a person, not on the renderer. A request
carries everything its job will read. Another identity's job is
answered exactly as one that does not exist, and a finished job is
forgotten when its download link would have expired.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t14.txt services/video-mcp/src/jobs/queue.ts services/video-mcp/test/unit/queue.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 15: The output check: every Instagram row, on the delivered file

A film enters the library only after every rule below holds on the file ffmpeg wrote (spec §Architecture 5, §Output format). Each broken rule is one sentence, and they are all said at once.

1. **Instagram's Reels rows** (Graph API "Reel specifications", as Task 12 measured them):
   - MP4 with moov before mdat, and no edit list;
   - at most 300,000,000 bytes, 3 s to 15 minutes, and no second over 25 Mbps;
   - one H.264 stream: progressive, no B-frames, 4:2:0, 23–60 fps, at most 1920 px wide;
   - one AAC-LC stream: 48 kHz, one or two channels, at most 128 kbps plus 5 % (126.9 kbps was measured).
2. **The render asked for:** exactly the frame size `renderSize` gave, and the project's length within one frame.
3. **What was rendered:**
   - five frames, one in each fifth of the film, each moved inside its clip past any fade, junction or transition, whose brightest luma must reach 32. A failed source renders black (FINDINGS S1.4), and a dark scene still has a highlight.
   - half a second inside every stretch where something audible plays, whose peak must reach -60 dB.

The rules are judged on plain data (`FilmFacts`), so the unit tier holds every one, and Task 21 mutates them. Measuring is split in two:
- `mp4Facts.ts` reads what ffprobe and ffmpeg print, and the MP4's own boxes. It is pure and unit-tested.
- `outputProbe.ts` runs the tools. It is in the render tier only (`RENDER_TIER`, Task 1), where Task 20 measures it on real films.

**Files:**
- Create: `services/video-mcp/src/jobs/outputCheck.ts`, `services/video-mcp/src/jobs/mp4Facts.ts`, `services/video-mcp/src/jobs/samples.ts`, `services/video-mcp/src/jobs/outputProbe.ts`
- Modify: `services/video-mcp/src/studio.ts` (`junctionDurationAt`)
- Test: `services/video-mcp/test/unit/outputCheck.test.ts`, `services/video-mcp/test/unit/mp4Facts.test.ts`, `services/video-mcp/test/unit/samples.test.ts`

**Interfaces:**
- Consumes: the seam (Task 5) plus `junctionDurationAt`; `sampleProject` (Task 5's `test/support/projects.ts`).
- Produces:
  - `interface VideoFacts { codec; profile; width; height; pixFmt; fieldOrder; fps; hasBFrames; seconds }`;
  - `interface AudioFacts { codec; profile; sampleRate; channels; bitRate }`;
  - `interface FilmFacts { bytes; formatName; topLevelBoxes; editLists; formatSeconds; video; audio; maxBitsPerSecond; frames: {at, lumaMax}[]; sound: {from, to, peakDb}[] }`;
  - `interface Expected { seconds; fps; width; height }` and `outputProblems(facts: FilmFacts, expected: Expected): string[]`;
  - `interface SamplePlan { frames: number[]; sound: {from, to}[] }` and `samplePlan(project: VideoProject): SamplePlan`;
  - `mp4Facts.ts`: `interface ProbeStream`, `videoFacts`, `audioFacts`, `countBoxes(bytes, type)`, `boxes(path)`, `interface Measured`, `factsFrom(measured)`, `busiestSecond(packets)`, `lumaMaxIn(log)` and `peakDbIn(log)`;
  - `filmFacts(path: string, plan: SamplePlan): Promise<FilmFacts>` (`outputProbe.ts`).

- [ ] **Step 1: Write the failing tests**

`services/video-mcp/test/unit/outputCheck.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { outputProblems, type FilmFacts } from '../../src/jobs/outputCheck';

// The facts of a real film: reel-30 rendered by the headless shell and remuxed by ffmpeg (video
// copied, Opus -> AAC-LC, +faststart, -use_editlist 0), as measured on 2026-10-02.
const measured: FilmFacts = {
  bytes: 25_822_147,
  formatName: 'mov,mp4,m4a,3gp,3g2,mj2',
  topLevelBoxes: ['ftyp', 'moov', 'free', 'mdat'],
  editLists: 0,
  formatSeconds: 29.035,
  video: [{ codec: 'h264', profile: 'High', width: 1080, height: 1920, pixFmt: 'yuvj420p', fieldOrder: 'progressive', fps: 30, hasBFrames: 0, seconds: 29.014844 }],
  audio: [{ codec: 'aac', profile: 'LC', sampleRate: 48_000, channels: 2, bitRate: 126_901 }],
  maxBitsPerSecond: 11_850_000,
  frames: [6, 10, 15, 20, 25].map((at) => ({ at, lumaMax: 235 })),
  sound: [{ from: 8.5, to: 9, peakDb: -9.4 }],
};
const expected = { seconds: 29, fps: 30, width: 1080, height: 1920 };
const video = measured.video[0] as FilmFacts['video'][number];
const audio = measured.audio[0] as FilmFacts['audio'][number];

it('delivers the film the measured pipeline makes', () => {
  expect(outputProblems(measured, expected)).toEqual([]);
});

describe('each Instagram row, at its edge and just past it', () => {
  const cases: [string, Partial<FilmFacts>, string | undefined][] = [
    ['300 MB exactly', { bytes: 300_000_000 }, undefined],
    ['a byte over 300 MB', { bytes: 300_000_001 }, 'the file is 300000001 bytes, over 300 MB'],
    ['3 s exactly', { formatSeconds: 3 }, undefined],
    ['under 3 s', { formatSeconds: 2.999 }, 'the film lasts 2.999 s, outside 3 s to 15 minutes'],
    ['15 minutes exactly', { formatSeconds: 900 }, undefined],
    ['over 15 minutes', { formatSeconds: 900.001 }, 'the film lasts 900.001 s, outside 3 s to 15 minutes'],
    ['25 Mbps in its busiest second', { maxBitsPerSecond: 25_000_000 }, undefined],
    ['over 25 Mbps in one second', { maxBitsPerSecond: 25_000_001 }, 'one second of the film carries 25 Mbps, over 25'],
    ['the moov atom behind the media', { topLevelBoxes: ['ftyp', 'mdat', 'moov'] }, 'the moov atom is not in front of the media data'],
    ['no moov atom', { topLevelBoxes: ['ftyp', 'mdat'] }, 'the moov atom is not in front of the media data'],
    ['an edit list', { editLists: 1 }, 'the file has 1 edit lists'],
    ['not an MP4', { formatName: 'matroska,webm' }, 'the file is matroska,webm, not MP4'],
  ];
  it.each(cases)('%s', (_name, change, problem) => {
    expect(outputProblems({ ...measured, ...change }, expected)).toEqual(problem === undefined ? [] : [problem]);
  });
});

describe('the picture', () => {
  const cases: [string, Partial<FilmFacts['video'][number]>, string][] = [
    ['in another codec', { codec: 'hevc' }, 'the video is hevc, not H.264'],
    ['interlaced', { fieldOrder: 'tt' }, 'the video is not progressive (tt)'],
    ['with B-frames', { hasBFrames: 2 }, 'the video has B-frames, so its GOPs are not guaranteed closed'],
    ['in 4:4:4', { pixFmt: 'yuv444p' }, 'the video is yuv444p, not 4:2:0'],
    ['at 22 fps', { fps: 22 }, 'the video runs at 22 fps, outside 23–60'],
    ['at 61 fps', { fps: 61 }, 'the video runs at 61 fps, outside 23–60'],
    ['a frame short and a hair', { seconds: 29 - 1 / 30 - 0.001 }, "the video lasts 28.966 s, not the project's 29 s"],
  ];
  it.each(cases)('is refused %s', (_name, change, problem) => {
    expect(outputProblems({ ...measured, video: [{ ...video, ...change }] }, expected)).toEqual([problem]);
  });

  it('is delivered one frame off the project’s length, which the AAC priming shift costs', () => {
    expect(outputProblems({ ...measured, video: [{ ...video, seconds: 29 + 1 / 30 }] }, expected)).toEqual([]);
  });

  it('is refused at another size than the one asked, and wider than 1920', () => {
    expect(outputProblems({ ...measured, video: [{ ...video, width: 720, height: 1280 }] }, expected)).toEqual([
      'the video is 720×1280, not the 1080×1920 asked',
    ]);
    expect(outputProblems({ ...measured, video: [{ ...video, width: 2560, height: 1440 }] }, { ...expected, width: 2560, height: 1440 })).toEqual([
      'the video is 2560 px wide, over 1920',
    ]);
  });

  it('is refused missing, or twice', () => {
    expect(outputProblems({ ...measured, video: [] }, expected)).toEqual(['the file has 0 video streams, not one']);
    expect(outputProblems({ ...measured, video: [video, video] }, expected)).toEqual(['the file has 2 video streams, not one']);
  });
});

describe('the sound', () => {
  const cases: [string, Partial<FilmFacts['audio'][number]>, string][] = [
    ['Opus, as the browser encodes it', { codec: 'opus', profile: 'unknown' }, 'the audio is opus unknown, not AAC-LC'],
    ['AAC but HE', { profile: 'HE-AAC' }, 'the audio is aac HE-AAC, not AAC-LC'],
    ['at 44.1 kHz', { sampleRate: 44_100 }, 'the audio is at 44100 Hz, not 48 kHz'],
    ['in six channels', { channels: 6 }, 'the audio has 6 channels'],
    ['at 192 kbps', { bitRate: 192_000 }, 'the audio runs at 192 kbps, over 128'],
  ];
  it.each(cases)('is refused %s', (_name, change, problem) => {
    expect(outputProblems({ ...measured, audio: [{ ...audio, ...change }] }, expected)).toEqual([problem]);
  });

  it('is delivered mono, and a few percent over 128 kbps as an encoder lands', () => {
    expect(outputProblems({ ...measured, audio: [{ ...audio, channels: 1, bitRate: 134_400 }] }, expected)).toEqual([]);
  });

  it('is refused missing: a film is AAC-less when the transcode did not run', () => {
    expect(outputProblems({ ...measured, audio: [] }, expected)).toEqual(['the file has 0 audio streams, not one']);
  });
});

describe('what was rendered', () => {
  it('refuses a black frame, and takes a dark one with a highlight', () => {
    const frames = [{ at: 6, lumaMax: 31 }, { at: 10, lumaMax: 32 }];
    expect(outputProblems({ ...measured, frames }, expected)).toEqual(['the frame at 6 s is black']);
  });

  it('refuses silence where the project has sound, and takes a quiet passage', () => {
    const sound = [{ from: 8.5, to: 9, peakDb: -60.1 }, { from: 20, to: 20.5, peakDb: -60 }];
    expect(outputProblems({ ...measured, sound }, expected)).toEqual(['the film is silent at 8.5 s–9 s, where the project has sound']);
  });

  it('says every broken rule at once, so one render tells the whole story', () => {
    expect(outputProblems({ ...measured, editLists: 2, audio: [] }, expected)).toEqual([
      'the file has 2 edit lists',
      'the file has 0 audio streams, not one',
    ]);
  });
});
```

`services/video-mcp/test/unit/mp4Facts.test.ts`:

```ts
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { audioFacts, boxes, busiestSecond, countBoxes, factsFrom, lumaMaxIn, peakDbIn, videoFacts } from '../../src/jobs/mp4Facts';

describe('reading the delivered file', () => {
  const box = (type: string, body: Buffer = Buffer.alloc(0)): Buffer => {
    const header = Buffer.alloc(8);
    header.writeUInt32BE(8 + body.length, 0);
    header.write(type, 4, 'latin1');
    return Buffer.concat([header, body]);
  };

  it('counts edit lists in every track, and stops at a box that lies about its size', () => {
    const track = (withEdit: boolean) => box('trak', Buffer.concat([withEdit ? box('edts', box('elst', Buffer.alloc(16))) : Buffer.alloc(0), box('mdia')]));
    expect(countBoxes(Buffer.concat([box('mvhd'), track(true), track(true)]), 'elst')).toBe(2);
    expect(countBoxes(Buffer.concat([box('mvhd'), track(false)]), 'elst')).toBe(0);
    const liar = Buffer.from(box('trak', box('edts', box('elst'))));
    liar.writeUInt32BE(4096, 0);
    expect(countBoxes(liar, 'elst')).toBe(0);
  });

  it('takes ffprobe’s stream fields as the check names them', () => {
    expect(videoFacts({ codec_name: 'h264', profile: 'High', width: 1080, height: 1920, pix_fmt: 'yuvj420p', field_order: 'progressive', r_frame_rate: '30000/1001', has_b_frames: 0, duration: '29.014844' })).toEqual({
      codec: 'h264', profile: 'High', width: 1080, height: 1920, pixFmt: 'yuvj420p', fieldOrder: 'progressive', fps: 30000 / 1001, hasBFrames: 0, seconds: 29.014844,
    });
    expect(videoFacts({}).fps).toBe(0);
    expect(videoFacts({ r_frame_rate: '30/0' }).fps).toBe(0);
    expect(audioFacts({ codec_name: 'aac', profile: 'LC', sample_rate: '48000', channels: 2, bit_rate: '126901' })).toEqual({
      codec: 'aac', profile: 'LC', sampleRate: 48_000, channels: 2, bitRate: 126_901,
    });
    expect(audioFacts({})).toEqual({ codec: 'unknown', profile: 'unknown', sampleRate: 0, channels: 0, bitRate: 0 });
  });
});

describe('what ffmpeg and the file say', () => {
  it('is the busiest second of the packets ffprobe lists', () => {
    expect(busiestSecond('0.000000,1000\n0.033333,2000\n1.000000,500\n\n')).toBe(24_000);
    expect(busiestSecond('')).toBe(0);
  });

  it('is the brightest luma signalstats printed, and 0 when it printed none', () => {
    expect(lumaMaxIn('[Parsed_metadata_1 @ 0x5] frame:0 pts:0\n[Parsed_metadata_1 @ 0x5] lavfi.signalstats.YMAX=235\n')).toBe(235);
    expect(lumaMaxIn('Output file is empty, nothing was encoded')).toBe(0);
  });

  it('is the loudest sample volumedetect heard, and silence when it heard nothing', () => {
    expect(peakDbIn('[Parsed_volumedetect_0 @ 0x5] max_volume: -9.4 dB\n')).toBe(-9.4);
    expect(peakDbIn('[Parsed_volumedetect_0 @ 0x5] max_volume: -inf dB\n')).toBe(-Infinity);
    expect(peakDbIn('Stream map matches no streams.')).toBe(-Infinity);
  });

  it('is the boxes a file holds, in order, with a 64-bit box and one that runs to the end', async () => {
    const dir = await mkdtemp(join(tmpdir(), 'boxes-'));
    const header = (size: number, type: string) => {
      const bytes = Buffer.alloc(8);
      bytes.writeUInt32BE(size, 0);
      bytes.write(type, 4, 'latin1');
      return bytes;
    };
    const elst = Buffer.concat([header(16, 'elst'), Buffer.alloc(8)]);
    const edts = Buffer.concat([header(8 + elst.length, 'edts'), elst]);
    const trak = Buffer.concat([header(8 + edts.length, 'trak'), edts]);
    const moov = Buffer.concat([header(8 + trak.length, 'moov'), trak]);
    const large = Buffer.alloc(16);
    large.writeUInt32BE(1, 0);
    large.write('free', 4, 'latin1');
    large.writeBigUInt64BE(24n, 8);
    const file = Buffer.concat([header(16, 'ftyp'), Buffer.alloc(8), moov, large, Buffer.alloc(8), header(0, 'mdat'), Buffer.alloc(32)]);
    try {
      await writeFile(join(dir, 'film.mp4'), file);
      expect(await boxes(join(dir, 'film.mp4'))).toEqual({ topLevel: ['ftyp', 'moov', 'free', 'mdat'], editLists: 1 });
      await writeFile(join(dir, 'broken.mp4'), Buffer.concat([header(16, 'ftyp'), Buffer.alloc(8), header(4, 'junk')]));
      expect(await boxes(join(dir, 'broken.mp4'))).toEqual({ topLevel: ['ftyp'], editLists: 0 });
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });

  it('reads ffprobe’s report into facts, and an empty report into facts that fail every rule', () => {
    const facts = factsFrom({
      bytes: 10,
      report: { streams: [{ codec_type: 'video', codec_name: 'h264' }, { codec_type: 'audio', codec_name: 'aac' }, { codec_type: 'data' }], format: { format_name: 'mov,mp4', duration: '4.5' } },
      packets: '0.1,100\n',
      boxes: { topLevel: ['ftyp', 'moov', 'mdat'], editLists: 0 },
      frames: [],
      sound: [],
    });
    expect([facts.video.length, facts.audio.length, facts.formatSeconds, facts.maxBitsPerSecond]).toEqual([1, 1, 4.5, 800]);
    expect(factsFrom({ bytes: 0, report: {}, packets: '', boxes: { topLevel: [], editLists: 0 }, frames: [], sound: [] })).toMatchObject({
      formatName: 'unknown',
      formatSeconds: 0,
      video: [],
      audio: [],
    });
  });
});
```

`services/video-mcp/test/unit/samples.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { samplePlan } from '../../src/jobs/samples';
import { setClipPresentation, setJunctionTransition, setAudioProperties, type VideoProject } from '../../src/studio';
import { sampleProject } from '../support/projects';

const rounded = (values: readonly number[]) => values.map((value) => Math.round(value * 1000) / 1000);

describe('the sampled frames', () => {
  it('are five, one in each fifth of the film, where nothing fades', () => {
    // c1 0–8 s, c2 8–14 s: the fifths' middles are 1.4, 4.2, 7, 9.8 and 12.6 s.
    expect(rounded(samplePlan(sampleProject()).frames)).toEqual([1.4, 4.2, 7, 9.8, 12.6]);
  });

  it('move inside their clip, past its fade and past a junction', () => {
    let project = setClipPresentation(sampleProject(), { clipId: 'c1', fadeIn: true });
    project = setJunctionTransition(project, { fromClipId: 'c1', toClipId: 'c2', transition: 'fadeBlack', duration: 2 });
    // c1 0–8 s and c2 6–12 s wash into each other over 6–8 s; the third fifth's middle, 6 s, opens
    // c2 inside that wash and moves to 8.05 s. The first, 1.2 s, is already past c1's fade.
    expect(rounded(samplePlan(project).frames)).toEqual([1.2, 3.6, 8.05, 8.4, 10.8]);
  });

  it('keep clear of a clip transition by its own length, one second when it names none', () => {
    const project = setClipPresentation(sampleProject(), { clipId: 'c2', transitionOut: 'slideUp' });
    expect(rounded(samplePlan(project).frames).at(-1)).toBe(12.6);
    const long = setClipPresentation(sampleProject(), { clipId: 'c2', transitionIn: 'zoom', transitionInDuration: 2 });
    expect(rounded(samplePlan(long).frames)[3]).toBe(10.05);
  });

  it('fall in the middle of a clip too short to have an inside', () => {
    const tiny: VideoProject = {
      ...sampleProject(),
      video: [{ id: 'c1', sourceId: 'src-camera', duration: 0.6, sourceStart: 0, muted: false, fadeIn: true, fadeOut: true }],
      overlays: [],
      audio: [],
    };
    expect(rounded(samplePlan(tiny).frames)).toEqual([0.3, 0.3, 0.3, 0.3, 0.3]);
  });
});

describe('the sampled sound', () => {
  it('is half a second in the middle of every stretch something audible plays', () => {
    // c1 plays the camera's sound 0–8 s; c2 is muted; the music plays 0–14 s.
    expect(samplePlan(sampleProject()).sound).toEqual([
      { from: 3.75, to: 4.25 },
      { from: 6.75, to: 7.25 },
    ]);
  });

  it('skips what nobody can hear, and keeps clear of a sound’s fades', () => {
    let project = setClipPresentation(sampleProject(), { clipId: 'c1', volume: 0 });
    project = setAudioProperties(project, { itemId: 'm1', fadeIn: 4 });
    expect(samplePlan(project).sound).toEqual([{ from: 8.75, to: 9.25 }]);
    expect(samplePlan(setAudioProperties(project, { itemId: 'm1', muted: true })).sound).toEqual([]);
  });

  it('does not count on a clip whose sound was never measured', () => {
    const project = sampleProject();
    const unknown: VideoProject = {
      ...project,
      sources: project.sources.map((source) => (source.id === 'src-camera' ? { ...source, hasAudio: undefined } : source)),
      audio: [],
    } as VideoProject;
    expect(samplePlan(unknown).sound).toEqual([]);
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/outputCheck.test.ts test/unit/mp4Facts.test.ts test/unit/samples.test.ts`

Expected: three `FAIL`s, each `Error: Cannot find module '../../src/jobs/outputCheck'` (then `mp4Facts`, `samples`) `imported from …`; `VITEST_RC=1`.

- [ ] **Step 3: Write the check, the readers, the sampling and the probe**

In `services/video-mcp/src/studio.ts`, in the `export { … } from '../../../web/src/videoStudio/project';` list, add `junctionDurationAt,` between `emptyProject,` and `overlayWindow,`.

`services/video-mcp/src/jobs/outputCheck.ts`:

```ts
// outputCheck.ts — whether a film may be delivered (spec §Architecture 5, §Output format). It
// judges facts outputProbe.ts measured with ffprobe and ffmpeg, so every rule is tested on plain
// data: each Instagram Reels row (Graph API "Reel specifications", read 2026-09-30), the duration
// against the project's, five sampled frames that must not be black, and sound wherever the
// project has something audible. Every broken rule is one sentence; none at all means deliver.

export interface VideoFacts {
  readonly codec: string;
  readonly profile: string;
  readonly width: number;
  readonly height: number;
  readonly pixFmt: string;
  readonly fieldOrder: string;
  readonly fps: number;
  readonly hasBFrames: number;
  readonly seconds: number;
}

export interface AudioFacts {
  readonly codec: string;
  readonly profile: string;
  readonly sampleRate: number;
  readonly channels: number;
  readonly bitRate: number;
}

export interface FilmFacts {
  readonly bytes: number;
  readonly formatName: string;
  /** The file's top-level boxes, in order. */
  readonly topLevelBoxes: readonly string[];
  readonly editLists: number;
  readonly formatSeconds: number;
  readonly video: readonly VideoFacts[];
  readonly audio: readonly AudioFacts[];
  /** The most bits any one second of the file carries. */
  readonly maxBitsPerSecond: number;
  /** The brightest luma of each sampled frame, 0–255. */
  readonly frames: readonly { readonly at: number; readonly lumaMax: number }[];
  /** The loudest sample of each window the project has something audible in, in dBFS. */
  readonly sound: readonly { readonly from: number; readonly to: number; readonly peakDb: number }[];
}

export interface Expected {
  readonly seconds: number;
  readonly fps: number;
  readonly width: number;
  readonly height: number;
}

const INSTAGRAM = {
  maxBytes: 300_000_000,
  minSeconds: 3,
  maxSeconds: 15 * 60,
  maxBitsPerSecond: 25_000_000,
  maxWidth: 1920,
  minFps: 23,
  maxFps: 60,
  sampleRate: 48_000,
  audioBitRate: 128_000,
} as const;
/** An encoder lands a few percent either side of its audio target (126.9 kbps measured). */
const AUDIO_BITRATE_TOLERANCE = 1.05;
/** A frame whose brightest pixel is this dark is black: a failed source renders RGB 0, and a dark
 *  scene still has a highlight above it. */
const BLACK_LUMA = 32;
/** Below this peak a window the project fills with sound came out silent. */
const SILENT_DB = -60;
const PLANAR_420 = new Set(['yuv420p', 'yuvj420p']);

const s = (value: number): string => `${String(Math.round(value * 1000) / 1000)} s`;

function videoProblems(video: VideoFacts, expected: Expected): string[] {
  const problems: string[] = [];
  if (video.codec !== 'h264') problems.push(`the video is ${video.codec}, not H.264`);
  if (video.fieldOrder !== 'progressive') problems.push(`the video is not progressive (${video.fieldOrder})`);
  if (video.hasBFrames !== 0) problems.push('the video has B-frames, so its GOPs are not guaranteed closed');
  if (!PLANAR_420.has(video.pixFmt)) problems.push(`the video is ${video.pixFmt}, not 4:2:0`);
  if (video.fps < INSTAGRAM.minFps || video.fps > INSTAGRAM.maxFps) {
    problems.push(`the video runs at ${String(video.fps)} fps, outside 23–60`);
  }
  if (video.width > INSTAGRAM.maxWidth) problems.push(`the video is ${String(video.width)} px wide, over 1920`);
  if (video.width !== expected.width || video.height !== expected.height) {
    problems.push(
      `the video is ${String(video.width)}×${String(video.height)}, not the ${String(expected.width)}×${String(expected.height)} asked`,
    );
  }
  // "Within one frame" inclusive: 29 + 1/30 - 29 is a hair over 1/30 in floating point, and
  // ffprobe reports microseconds.
  if (Math.abs(video.seconds - expected.seconds) > 1 / expected.fps + 1e-6) {
    problems.push(`the video lasts ${s(video.seconds)}, not the project's ${s(expected.seconds)}`);
  }
  return problems;
}

function audioProblems(audio: AudioFacts): string[] {
  const problems: string[] = [];
  if (audio.codec !== 'aac' || audio.profile !== 'LC') {
    problems.push(`the audio is ${audio.codec} ${audio.profile}, not AAC-LC`);
  }
  if (audio.sampleRate !== INSTAGRAM.sampleRate) problems.push(`the audio is at ${String(audio.sampleRate)} Hz, not 48 kHz`);
  if (audio.channels < 1 || audio.channels > 2) problems.push(`the audio has ${String(audio.channels)} channels`);
  if (audio.bitRate > INSTAGRAM.audioBitRate * AUDIO_BITRATE_TOLERANCE) {
    problems.push(`the audio runs at ${String(Math.round(audio.bitRate / 1000))} kbps, over 128`);
  }
  return problems;
}

/** Every rule the film breaks, one sentence each; an empty list delivers it. */
export function outputProblems(facts: FilmFacts, expected: Expected): string[] {
  const problems: string[] = [];
  if (!facts.formatName.split(',').includes('mp4')) problems.push(`the file is ${facts.formatName}, not MP4`);
  const moov = facts.topLevelBoxes.indexOf('moov');
  const mdat = facts.topLevelBoxes.indexOf('mdat');
  if (moov === -1 || mdat === -1 || moov > mdat) problems.push('the moov atom is not in front of the media data');
  if (facts.editLists > 0) problems.push(`the file has ${String(facts.editLists)} edit lists`);
  if (facts.bytes > INSTAGRAM.maxBytes) problems.push(`the file is ${String(facts.bytes)} bytes, over 300 MB`);
  if (facts.formatSeconds < INSTAGRAM.minSeconds || facts.formatSeconds > INSTAGRAM.maxSeconds) {
    problems.push(`the film lasts ${s(facts.formatSeconds)}, outside 3 s to 15 minutes`);
  }
  if (facts.maxBitsPerSecond > INSTAGRAM.maxBitsPerSecond) {
    problems.push(`one second of the film carries ${String(Math.round(facts.maxBitsPerSecond / 1e5) / 10)} Mbps, over 25`);
  }
  const [video, ...extraVideo] = facts.video;
  if (video === undefined || extraVideo.length > 0) problems.push(`the file has ${String(facts.video.length)} video streams, not one`);
  else problems.push(...videoProblems(video, expected));
  const [audio, ...extraAudio] = facts.audio;
  if (audio === undefined || extraAudio.length > 0) problems.push(`the file has ${String(facts.audio.length)} audio streams, not one`);
  else problems.push(...audioProblems(audio));
  for (const frame of facts.frames) {
    if (frame.lumaMax < BLACK_LUMA) problems.push(`the frame at ${s(frame.at)} is black`);
  }
  for (const window of facts.sound) {
    if (window.peakDb < SILENT_DB) {
      problems.push(`the film is silent at ${s(window.from)}–${s(window.to)}, where the project has sound`);
    }
  }
  return problems;
}
```

`services/video-mcp/src/jobs/mp4Facts.ts`:

```ts
// mp4Facts.ts — reading what ffprobe and ffmpeg print, and the MP4's own boxes, into the facts
// outputCheck.ts judges. Pure readers of text and bytes, so each is held by the unit tier;
// outputProbe.ts runs the tools that produce their input.

import { open } from 'node:fs/promises';
import type { AudioFacts, FilmFacts, VideoFacts } from './outputCheck';

/** The fields of one `ffprobe -show_streams -of json` stream the check reads. */
export interface ProbeStream {
  readonly codec_type?: string;
  readonly codec_name?: string;
  readonly profile?: string;
  readonly width?: number;
  readonly height?: number;
  readonly pix_fmt?: string;
  readonly field_order?: string;
  readonly r_frame_rate?: string;
  readonly has_b_frames?: number;
  readonly duration?: string;
  readonly sample_rate?: string;
  readonly channels?: number;
  readonly bit_rate?: string;
}

const rate = (fraction: string | undefined): number => {
  const [top, bottom] = (fraction ?? '0/1').split('/').map(Number);
  return bottom === undefined || bottom === 0 ? 0 : (top ?? 0) / bottom;
};

export function videoFacts(stream: ProbeStream): VideoFacts {
  return {
    codec: stream.codec_name ?? 'unknown',
    profile: stream.profile ?? 'unknown',
    width: stream.width ?? 0,
    height: stream.height ?? 0,
    pixFmt: stream.pix_fmt ?? 'unknown',
    fieldOrder: stream.field_order ?? 'unknown',
    fps: rate(stream.r_frame_rate),
    hasBFrames: stream.has_b_frames ?? 0,
    seconds: Number(stream.duration ?? 0),
  };
}

export function audioFacts(stream: ProbeStream): AudioFacts {
  return {
    codec: stream.codec_name ?? 'unknown',
    profile: stream.profile ?? 'unknown',
    sampleRate: Number(stream.sample_rate ?? 0),
    channels: stream.channels ?? 0,
    bitRate: Number(stream.bit_rate ?? 0),
  };
}

/** How many `type` boxes the box tree in `bytes` holds, walking the containers a track has. */
export function countBoxes(bytes: Buffer, type: string): number {
  const containers = new Set(['moov', 'trak', 'edts', 'mdia', 'minf', 'stbl']);
  let count = 0;
  let at = 0;
  while (at + 8 <= bytes.length) {
    const length = bytes.readUInt32BE(at);
    const name = bytes.toString('latin1', at + 4, at + 8);
    if (length < 8 || at + length > bytes.length) break;
    if (name === type) count += 1;
    if (containers.has(name)) count += countBoxes(bytes.subarray(at + 8, at + length), type);
    at += length;
  }
  return count;
}

/** The top-level boxes in order and the edit lists inside moov, read from the file's own bytes:
 *  ffprobe reports neither box order nor edit lists. */
export async function boxes(path: string): Promise<{ topLevel: string[]; editLists: number }> {
  const file = await open(path, 'r');
  try {
    const { size } = await file.stat();
    const header = Buffer.alloc(16);
    const topLevel: string[] = [];
    let editLists = 0;
    let at = 0;
    while (at + 8 <= size) {
      await file.read(header, 0, 16, at);
      let length = header.readUInt32BE(0);
      const headerLength = length === 1 ? 16 : 8;
      if (length === 1) length = Number(header.readBigUInt64BE(8));
      else if (length === 0) length = size - at;
      if (length < headerLength) break;
      const type = header.toString('latin1', 4, 8);
      topLevel.push(type);
      if (type === 'moov') {
        const moov = Buffer.alloc(length - headerLength);
        await file.read(moov, 0, moov.length, at + headerLength);
        editLists += countBoxes(moov, 'elst');
      }
      at += length;
    }
    return { topLevel, editLists };
  } finally {
    await file.close();
  }
}

/** What the tools said about one file, before it is read into facts. */
export interface Measured {
  readonly bytes: number;
  /** `ffprobe -show_streams -show_format -of json`. */
  readonly report: { readonly streams?: readonly ProbeStream[]; readonly format?: { readonly format_name?: string; readonly duration?: string } };
  /** `ffprobe -show_entries packet=pts_time,size -of csv=p=0`. */
  readonly packets: string;
  readonly boxes: { readonly topLevel: readonly string[]; readonly editLists: number };
  readonly frames: FilmFacts['frames'];
  readonly sound: FilmFacts['sound'];
}

export function factsFrom(measured: Measured): FilmFacts {
  const streams = measured.report.streams ?? [];
  return {
    bytes: measured.bytes,
    formatName: measured.report.format?.format_name ?? 'unknown',
    topLevelBoxes: measured.boxes.topLevel,
    editLists: measured.boxes.editLists,
    formatSeconds: Number(measured.report.format?.duration ?? 0),
    video: streams.filter((stream) => stream.codec_type === 'video').map(videoFacts),
    audio: streams.filter((stream) => stream.codec_type === 'audio').map(audioFacts),
    maxBitsPerSecond: busiestSecond(measured.packets),
    frames: measured.frames,
    sound: measured.sound,
  };
}

/** The most bits one second carries, from `ffprobe -show_entries packet=pts_time,size -of csv`. */
export function busiestSecond(packets: string): number {
  const bySecond = new Map<number, number>();
  for (const line of packets.split('\n')) {
    const [time, bytes] = line.split(',');
    const second = Math.floor(Number(time));
    if (time !== undefined && time !== '' && Number.isFinite(second)) {
      bySecond.set(second, (bySecond.get(second) ?? 0) + Number(bytes) * 8);
    }
  }
  return Math.max(0, ...bySecond.values());
}

/** YMAX from ffmpeg's `signalstats,metadata=print` log; 0 when no frame was printed. */
export function lumaMaxIn(log: string): number {
  const value = /lavfi\.signalstats\.YMAX=(\d+(?:\.\d+)?)/.exec(log)?.[1];
  return value === undefined ? 0 : Number(value);
}

/** max_volume from ffmpeg's `volumedetect` log; -Infinity for silence or no sound at all. */
export function peakDbIn(log: string): number {
  const value = /max_volume: (-?\d+(?:\.\d+)?) dB/.exec(log)?.[1];
  return value === undefined ? -Infinity : Number(value);
}
```

`services/video-mcp/src/jobs/samples.ts`:

```ts
// samples.ts — where the output check looks (spec §Architecture 5): five frames spread over the
// film, each moved inside its clip past any fade, junction or transition, where the picture is
// meant to be fully there; and half a second inside every stretch the project fills with sound.
// A failed source renders black and silent (spikes/video-mcp-render FINDINGS S1.4): these are the
// places that cannot be black or silent unless something failed.

import {
  audioTracks,
  audioWindow,
  clipStarts,
  clipTimelineDuration,
  junctionDurationAt,
  projectDuration,
  sourceOf,
  type VideoItem,
  type VideoProject,
} from '../studio';

const FRAME_SAMPLES = 5;
/** clipOpacity's fade (videoflow.ts): half a second of the film, or half a short clip. */
const FADE_SECONDS = 0.5;
const SOUND_WINDOW = 0.5;
/** Clear of the frame on either side of a boundary. */
const GUARD = 0.05;

export interface SamplePlan {
  readonly frames: readonly number[];
  readonly sound: readonly { readonly from: number; readonly to: number }[];
}

/** How long a clip transition plays: one second unless the clip says (videoflow_transitions.ts
 *  ownEdge). */
function transitionLength(kind: string | undefined, duration: number | undefined): number {
  return kind === undefined || kind === 'none' ? 0 : (duration ?? 1);
}

/** How far into and out of a clip the picture is still arriving or leaving. */
function edges(clip: VideoItem, junctionIn: number, junctionOut: number): { into: number; outOf: number } {
  const fade = Math.min(FADE_SECONDS, clipTimelineDuration(clip) / 2);
  const into = Math.max(
    junctionIn,
    clip.fadeIn === true ? fade : 0,
    transitionLength(clip.transitionIn, clip.transitionInDuration),
  );
  const outOf = Math.max(
    junctionOut,
    clip.fadeOut === true ? fade : 0,
    transitionLength(clip.transitionOut, clip.transitionOutDuration),
  );
  return { into: into + GUARD, outOf: outOf + GUARD };
}

/** Five instants, one per fifth of the film, each pulled inside the clip it falls in. */
function frameTimes(project: VideoProject): number[] {
  const duration = projectDuration(project);
  const starts = clipStarts(project);
  const times: number[] = [];
  for (let k = 0; k < FRAME_SAMPLES; k += 1) {
    const wanted = (duration * (2 * k + 1)) / (2 * FRAME_SAMPLES);
    let index = project.video.length - 1;
    while (index > 0 && (starts[index] ?? 0) > wanted) index -= 1;
    const clip = project.video[index];
    if (clip === undefined) continue;
    const start = starts[index] ?? 0;
    const end = start + clipTimelineDuration(clip);
    const { into, outOf } = edges(clip, junctionDurationAt(project, index), junctionDurationAt(project, index + 1));
    const low = start + into;
    const high = end - outOf;
    times.push(low <= high ? Math.min(Math.max(wanted, low), high) : (start + end) / 2);
  }
  return times;
}

/** Half a second in the middle of every stretch something audible plays, clear of its fades. */
function soundWindows(project: VideoProject): { from: number; to: number }[] {
  const spans: { from: number; to: number }[] = [];
  const starts = clipStarts(project);
  project.video.forEach((clip, index) => {
    const source = sourceOf(project, clip.sourceId);
    if (clip.muted || (clip.volume ?? 1) === 0 || source?.kind !== 'video' || source.hasAudio !== true) return;
    const start = starts[index] ?? 0;
    spans.push({ from: start, to: start + clipTimelineDuration(clip) });
  });
  for (const item of audioTracks(project).flatMap((track) => track.items)) {
    if (item.muted || item.volume === 0) continue;
    const window = audioWindow(project, item);
    spans.push({ from: window.start + (item.fadeIn ?? 0), to: window.end - (item.fadeOut ?? 0) });
  }
  return spans
    .filter((span) => span.to - span.from >= SOUND_WINDOW + 2 * GUARD)
    .map((span) => {
      const middle = (span.from + span.to) / 2;
      return { from: middle - SOUND_WINDOW / 2, to: middle + SOUND_WINDOW / 2 };
    });
}

export function samplePlan(project: VideoProject): SamplePlan {
  return { frames: frameTimes(project), sound: soundWindows(project) };
}
```

`services/video-mcp/src/jobs/outputProbe.ts`:

```ts
// outputProbe.ts — the facts outputCheck.ts judges, measured on the delivered file with the
// image's ffprobe and ffmpeg: streams and format, every packet's size, the brightest luma of each
// sampled frame (signalstats) and the loudest sample of each sound window (volumedetect). It runs
// the tools; mp4Facts.ts reads what they print.

import { execFile } from 'node:child_process';
import { stat } from 'node:fs/promises';
import { promisify } from 'node:util';
import { boxes, factsFrom, lumaMaxIn, peakDbIn, type Measured } from './mp4Facts';
import type { FilmFacts } from './outputCheck';
import type { SamplePlan } from './samples';

const run = promisify(execFile);
const LOG = { maxBuffer: 64 << 20 };

async function ffmpegLog(...args: string[]): Promise<string> {
  return (await run('ffmpeg', ['-v', 'info', '-nostdin', ...args, '-f', 'null', '-'], LOG)).stderr;
}

export async function filmFacts(path: string, plan: SamplePlan): Promise<FilmFacts> {
  const probe = await run('ffprobe', ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', path], LOG);
  const packets = await run('ffprobe', ['-v', 'error', '-show_entries', 'packet=pts_time,size', '-of', 'csv=p=0', path], LOG);
  const frames = [];
  for (const at of plan.frames) {
    const log = await ffmpegLog('-ss', String(at), '-i', path, '-frames:v', '1', '-vf', 'signalstats,metadata=print:key=lavfi.signalstats.YMAX');
    frames.push({ at, lumaMax: lumaMaxIn(log) });
  }
  const sound = [];
  for (const window of plan.sound) {
    const log = await ffmpegLog('-ss', String(window.from), '-t', String(window.to - window.from), '-i', path, '-vn', '-af', 'volumedetect');
    sound.push({ ...window, peakDb: peakDbIn(log) });
  }
  const measured: Measured = {
    bytes: (await stat(path)).size,
    report: JSON.parse(probe.stdout) as Measured['report'],
    packets: packets.stdout,
    boxes: await boxes(path),
    frames,
    sound,
  };
  return factsFrom(measured);
}
```

- [ ] **Step 4: Run them to see them pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/outputCheck.test.ts test/unit/mp4Facts.test.ts test/unit/samples.test.ts`

Expected: `Test Files  3 passed (3)`, `Tests  47 passed (47)`, `VITEST_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 5: Commit**

Write `$W/msg-t15.txt`:

```text
feat(video-mcp): check every Instagram row on the delivered file

A film is delivered only when the file ffmpeg wrote holds every Reels
row (moov first, no edit list, 300 MB, H.264 progressive 4:2:0 with
closed GOPs, AAC-LC 48 kHz at 128 kbps, 1920 px, 23-60 fps, 25 Mbps
in any second), has the size asked and the project's length within a
frame, shows no black frame where a clip is fully on screen, and is
not silent where the project has sound. The rules judge plain facts so
the unit tier and mutation testing hold them; running ffprobe and
ffmpeg is the render tier's.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t15.txt services/video-mcp/src/jobs/outputCheck.ts services/video-mcp/src/jobs/mp4Facts.ts services/video-mcp/src/jobs/samples.ts services/video-mcp/src/jobs/outputProbe.ts services/video-mcp/src/studio.ts services/video-mcp/test/unit/outputCheck.test.ts services/video-mcp/test/unit/mp4Facts.test.ts services/video-mcp/test/unit/samples.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 16: The job's own origin, and the proxy's allowlist

Each job gets one loopback HTTP server (`pageServer.ts`). It serves:
- the export page, its WASM, worklet and fonts, from the page directory and nothing above it;
- `GET /media/<assetId>`: a source the job resolved, streamed from its signed URL as the page asks. No copy is kept (operator question 6, Task 13's measurement);
- `GET /media/<localId>`: a copy the job made and has not saved yet, a cleaned sound, served from its path in the job's scratch under the local id Task 17 gives it. It is never fetched, so the job needs no token to play what it made (Q1);
- `POST /output`: the film, written to the job's scratch;
- `POST /cleaned/<sourceId>`: a cleaned copy, only for a source the job asked to clean.

Bytes travel over HTTP, never through the DevTools protocol, where a `route.fulfill` body would be base64 and a 300 MB source would sit in memory three times.

The two judgments in `proxy.ts` are pure, so Stryker can hold them (Task 21):
- `pageMayFetch`: a request may leave the browser for the job's own origin only, with no userinfo. Task 17's browser routes every request through it.
- `allowedMedia` and `allowedCleaned`: a path names an asset or a source the job allowed, or nothing. Neither `..`, an encoded slash, nor a malformed escape gets through.

A source the store will not give (a non-2xx status, an unreachable store, or a body that stops halfway after a 200) is recorded by its file name, never by its signed URL, and the job fails naming it (spec §Security: "fails the job with the source's name").

**Files:**
- Create: `services/video-mcp/src/jobs/proxy.ts`, `services/video-mcp/src/jobs/pageServer.ts`
- Test: `services/video-mcp/test/unit/proxy.test.ts`

**Interfaces:**
- Consumes: `ResolvedAsset`, `internalApi` (Task 3); `FakeInternalApi` (Task 3's `test/support/fakeInternalApi.ts`); `IDENTITY` (Task 2).
- Produces:
  - `pageMayFetch(url: string, origin: string): boolean`;
  - `allowedMedia(pathname: string, allowlist: ReadonlySet<string>): string | undefined`;
  - `allowedCleaned(pathname: string, expected: ReadonlySet<string>): string | undefined`;
  - `interface SourceFailure { assetId; fileName; reason }`;
  - `interface PageServer { origin; failures; refused; fetched: ReadonlyMap<string, number>; filmPath; cleanedPath(sourceId): string; close(): Promise<void> }`;
  - `interface PageServerOptions { pageDir; scratch; sources: ReadonlyMap<string, ResolvedAsset>; cleaning?: ReadonlySet<string>; kept?: ReadonlyMap<string, string>; fetchImpl?: typeof fetch }`, where `kept` maps a local id to a path in scratch;
  - `startPageServer(options: PageServerOptions): Promise<PageServer>`.

- [ ] **Step 1: Write the failing test**

`services/video-mcp/test/unit/proxy.test.ts`:

```ts
import { mkdtemp, readFile, rm, writeFile, mkdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { internalApi, type ResolvedAsset } from '../../src/internalApi';
import { startPageServer, type PageServer } from '../../src/jobs/pageServer';
import { allowedCleaned, allowedMedia, pageMayFetch } from '../../src/jobs/proxy';
import { FakeInternalApi } from '../support/fakeInternalApi';
import { IDENTITY } from '../support/tokens';

const ORIGIN = 'http://127.0.0.1:41234';
const CLIP = '3f1d2c4b-5a69-4e70-8f81-92a3b4c5d6e7';

describe('what the page may reach', () => {
  it.each([
    [`${ORIGIN}/render.js`, true],
    [`${ORIGIN}/media/${CLIP}`, true],
    ['http://127.0.0.1:41235/render.js', false],
    ['http://localhost:41234/render.js', false],
    ['https://127.0.0.1:41234/render.js', false],
    ['http://user:pass@127.0.0.1:41234/render.js', false],
    ['https://fonts.googleapis.com/css2?family=Noto+Sans', false],
    ['http://garage:3900/aura/media/x.mp4?X-Amz-Signature=s', false],
    ['not a url', false],
  ])('%s → %s', (url, allowed) => {
    expect(pageMayFetch(url, ORIGIN)).toBe(allowed);
  });
});

describe('what the proxy serves', () => {
  const allowlist = new Set([CLIP]);
  it.each([
    [`/media/${CLIP}`, CLIP],
    [`/media/${encodeURIComponent(CLIP)}`, CLIP],
    ['/media/9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d', undefined],
    [`/media/${CLIP}/extra`, undefined],
    [`/medias/${CLIP}`, undefined],
    ['/media/%E0%A4%A', undefined],
    ['/media/', undefined],
  ])('%s → %s', (path, assetId) => {
    expect(allowedMedia(path, allowlist)).toBe(assetId);
  });

  it('takes a cleaned copy only for a source the job asked to clean', () => {
    expect(allowedCleaned('/cleaned/src-camera', new Set(['src-camera']))).toBe('src-camera');
    expect(allowedCleaned('/cleaned/src-screen', new Set(['src-camera']))).toBeUndefined();
    expect(allowedCleaned('/cleaned/%E0%A4%A', new Set(['src-camera']))).toBeUndefined();
    expect(allowedCleaned('/output', new Set(['src-camera']))).toBeUndefined();
  });
});

describe('the job’s page server', () => {
  let fake: FakeInternalApi;
  let scratch: string;
  let pageDir: string;
  let server: PageServer | undefined;
  let clip: ResolvedAsset;
  let gone: ResolvedAsset;

  beforeEach(async () => {
    fake = new FakeInternalApi();
    await fake.start();
    scratch = await mkdtemp(join(tmpdir(), 'video-mcp-scratch-'));
    pageDir = await mkdtemp(join(tmpdir(), 'video-mcp-page-'));
    await writeFile(join(pageDir, 'render.html'), '<!doctype html>');
    await mkdir(join(pageDir, 'fonts'));
    await writeFile(join(pageDir, 'fonts', 'atkinson.css'), '@font-face{}');
    const token = 'bearer';
    fake.trust(token, IDENTITY);
    const ids = [
      fake.seed(IDENTITY, { fileName: 'beach.mp4', mimeType: 'video/mp4', bytes: Buffer.from('the beach') }).id,
      fake.seed(IDENTITY, { fileName: 'gone.mp4', mimeType: 'video/mp4', bytes: Buffer.from('x') }).id,
    ];
    const { found } = await internalApi(fake.url).resolve(token, ids);
    clip = found.get(ids[0] ?? '') as ResolvedAsset;
    gone = found.get(ids[1] ?? '') as ResolvedAsset;
    fake.objectFailures.set(gone.assetId, 404);
  });

  afterEach(async () => {
    await server?.close();
    await fake.close();
    await rm(scratch, { recursive: true, force: true });
    await rm(pageDir, { recursive: true, force: true });
  });

  async function start(): Promise<PageServer> {
    server = await startPageServer({
      pageDir,
      scratch,
      sources: new Map([[clip.assetId, clip], [gone.assetId, gone]]),
      cleaning: new Set(['src-camera']),
    });
    return server;
  }

  it('serves the page and its fonts, and nothing above them', async () => {
    const page = await start();
    expect(await (await fetch(`${page.origin}/render.html`)).text()).toBe('<!doctype html>');
    expect((await fetch(`${page.origin}/fonts/atkinson.css`)).headers.get('content-type')).toBe('text/css');
    // A dotted segment is resolved by the URL parser before any server sees it; an encoded slash
    // is not, and decodes into one. Either way the path stays inside the page directory, so the
    // host's /etc/passwd, which exists, is answered as a page file that does not.
    const climbing = await fetch(`${page.origin}/fonts%2F..%2F..%2F..%2Fetc%2Fpasswd`);
    expect([climbing.status, await climbing.text()]).toEqual([404, '']);
    expect((await fetch(`${page.origin}/%E0%A4%A`)).status).toBe(403);
    expect((await fetch(`${page.origin}/`)).status).toBe(403);
    expect((await fetch(`${page.origin}/missing.js`)).status).toBe(404);
  });

  it('streams an allowed source from its signed URL, counting each fetch', async () => {
    const page = await start();
    const response = await fetch(`${page.origin}/media/${clip.assetId}`);
    expect(await response.text()).toBe('the beach');
    expect(response.headers.get('content-type')).toBe('video/mp4');
    expect(page.fetched.get(clip.assetId)).toBe(1);
    expect(page.failures).toEqual([]);
  });

  it('refuses a source the job did not resolve, and records the asking', async () => {
    const page = await start();
    const other = '9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d';
    expect((await fetch(`${page.origin}/media/${other}`)).status).toBe(403);
    expect(page.refused).toEqual([`GET /media/${other}`]);
  });

  it('records a source the object store will not give, by its file name and never by its URL', async () => {
    const page = await start();
    expect((await fetch(`${page.origin}/media/${gone.assetId}`)).status).toBe(502);
    expect(page.failures).toEqual([{ assetId: gone.assetId, fileName: 'gone.mp4', reason: 'the object store answered 404' }]);
    expect(JSON.stringify(page.failures)).not.toContain('Signature');
  });

  it('records a source whose store cannot be reached at all', async () => {
    server = await startPageServer({
      pageDir,
      scratch,
      sources: new Map([[clip.assetId, { ...clip, url: 'http://127.0.0.1:9/x.mp4?X-Amz-Signature=s' }]]),
    });
    expect((await fetch(`${server.origin}/media/${clip.assetId}`)).status).toBe(502);
    expect(server.failures).toEqual([{ assetId: clip.assetId, fileName: 'beach.mp4', reason: 'the object store could not be reached' }]);
  });

  it('records a source whose body stops halfway, after its status said yes', async () => {
    const halfway = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('the be'));
        controller.error(new Error('connection reset'));
      },
    });
    const running = await startPageServer({
      pageDir,
      scratch,
      sources: new Map([[clip.assetId, clip]]),
      fetchImpl: () => Promise.resolve(new Response(halfway, { status: 200, headers: { 'content-type': 'video/mp4' } })),
    });
    server = running;
    await fetch(`${running.origin}/media/${clip.assetId}`).then((response) => response.text()).catch(() => '');
    await expect.poll(() => running.failures).toEqual([
      { assetId: clip.assetId, fileName: 'beach.mp4', reason: 'the object store stopped sending it' },
    ]);
  });

  it('plays a copy the job made and has not saved from scratch, under the local id it was given', async () => {
    const copy = join(scratch, 'cleaned-src-camera.ogg');
    await writeFile(copy, 'the cleaned voice');
    server = await startPageServer({ pageDir, scratch, sources: new Map([[clip.assetId, clip]]), kept: new Map([['kept-src-camera', copy]]) });
    const response = await fetch(`${server.origin}/media/kept-src-camera`);
    expect([response.status, response.headers.get('content-type'), await response.text()]).toEqual([200, 'audio/ogg', 'the cleaned voice']);
    expect((await fetch(`${server.origin}/media/kept-src-screen`)).status).toBe(403);
    expect(server.fetched.size).toBe(0);
  });

  it('takes the film and an asked-for cleaned copy into scratch, and nothing else', async () => {
    const page = await start();
    expect((await fetch(`${page.origin}/output`, { method: 'POST', body: 'mp4 bytes' })).status).toBe(204);
    expect(await readFile(page.filmPath, 'utf8')).toBe('mp4 bytes');
    expect((await fetch(`${page.origin}/cleaned/src-camera`, { method: 'POST', body: 'ogg' })).status).toBe(204);
    expect(await readFile(page.cleanedPath('src-camera'), 'utf8')).toBe('ogg');
    expect((await fetch(`${page.origin}/cleaned/src-screen`, { method: 'POST', body: 'ogg' })).status).toBe(403);
    expect((await fetch(`${page.origin}/render.html`, { method: 'POST', body: 'x' })).status).toBe(403);
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/proxy.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../src/jobs/pageServer' imported from …/test/unit/proxy.test.ts` (the first missing import in the file's order); `VITEST_RC=1`.

- [ ] **Step 3: Write the allowlist and the page server**

`services/video-mcp/src/jobs/proxy.ts`:

```ts
// proxy.ts — what the render page may reach (spec §Security, "the page reaches only its own origin
// and the proxy, and the proxy only the job's allowlist"). Two judgments, pure so mutation testing
// can hold them: the browser lets a request out only to the job's own origin (browser.ts routes
// every request through this), and the job's server proxies a source only when the job resolved
// it (pageServer.ts). The signed URLs themselves never reach the page.

/** Whether the page may send `url` anywhere: its own origin only. `data:` and `blob:` never leave
 *  the browser and are not routed; every other scheme and host is refused. */
export function pageMayFetch(url: string, origin: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return false;
  }
  return parsed.origin === origin && parsed.username === '' && parsed.password === '';
}

/** The asset a `/media/<assetId>` path asks for, when the job resolved that asset. */
export function allowedMedia(pathname: string, allowlist: ReadonlySet<string>): string | undefined {
  const match = /^\/media\/([^/]+)$/.exec(pathname);
  if (match === null) return undefined;
  let assetId: string;
  try {
    assetId = decodeURIComponent(match[1] ?? '');
  } catch {
    return undefined;
  }
  return allowlist.has(assetId) ? assetId : undefined;
}

/** The project source a `/cleaned/<sourceId>` upload is for, when the job asked for that copy. */
export function allowedCleaned(pathname: string, expected: ReadonlySet<string>): string | undefined {
  const match = /^\/cleaned\/([^/]+)$/.exec(pathname);
  if (match === null) return undefined;
  let sourceId: string;
  try {
    sourceId = decodeURIComponent(match[1] ?? '');
  } catch {
    return undefined;
  }
  return expected.has(sourceId) ? sourceId : undefined;
}
```

`services/video-mcp/src/jobs/pageServer.ts`:

```ts
// pageServer.ts — one job's own origin on loopback: the export page, its WASM, worklet and fonts;
// the media proxy; and the two doors the page sends its results through. Bytes travel over HTTP,
// never through the DevTools protocol (a route.fulfill body is base64 over CDP: a 300 MB source
// would sit in memory three times). Each source is streamed from its signed URL as the page asks,
// and the page asks once per pass: measured 2026-10-02, one whole-file GET per source and no Range
// request for a full export (prd.md §12 "Render memory"). A copy the job made and has not saved
// yet is played from the job's scratch, under the local id the job gave it.

import { createReadStream, createWriteStream } from 'node:fs';
import { stat } from 'node:fs/promises';
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { extname, join, posix } from 'node:path';
import { Readable } from 'node:stream';
import type { ReadableStream as NodeReadableStream } from 'node:stream/web';
import { pipeline } from 'node:stream/promises';
import type { ResolvedAsset } from '../internalApi';
import { allowedCleaned, allowedMedia } from './proxy';

const TYPES: Readonly<Record<string, string>> = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.wasm': 'application/wasm',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.ogg': 'audio/ogg',
};

/** A source the proxy could not deliver: what the job fails with, by the source's file name. */
export interface SourceFailure {
  readonly assetId: string;
  readonly fileName: string;
  readonly reason: string;
}

export interface PageServer {
  readonly origin: string;
  readonly failures: readonly SourceFailure[];
  /** Paths the page asked for that are neither the page nor an allowed source. */
  readonly refused: readonly string[];
  /** GETs per asset id, for the record and the tests. */
  readonly fetched: ReadonlyMap<string, number>;
  /** Where the film the page sent is, once it has. */
  readonly filmPath: string;
  cleanedPath(sourceId: string): string;
  close(): Promise<void>;
}

export interface PageServerOptions {
  readonly pageDir: string;
  readonly scratch: string;
  readonly sources: ReadonlyMap<string, ResolvedAsset>;
  /** Project source ids whose cleaned copy the page will send. */
  readonly cleaning?: ReadonlySet<string>;
  /** Copies the job made and has not saved yet, by the local id the project plays them under:
   *  served from their path in scratch, never fetched. */
  readonly kept?: ReadonlyMap<string, string>;
  readonly fetchImpl?: typeof fetch;
}

/** The page file a request path names. An absolute path normalizes at its root (`/a/../../x` is
 *  `/x`), so no decoded path, `%2F` included, climbs out of the page directory. */
function ownFile(root: string, pathname: string): string | undefined {
  let decoded: string;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    return undefined;
  }
  const inside = posix.normalize(`/${decoded}`);
  return inside === '/' ? undefined : join(root, inside);
}

export async function startPageServer(options: PageServerOptions): Promise<PageServer> {
  const fetchImpl = options.fetchImpl ?? fetch;
  const kept = options.kept ?? new Map<string, string>();
  const allowlist = new Set([...options.sources.keys(), ...kept.keys()]);
  const cleaning = options.cleaning ?? new Set<string>();
  const failures: SourceFailure[] = [];
  const refused: string[] = [];
  const fetched = new Map<string, number>();
  const filmPath = join(options.scratch, 'film.mp4');
  const cleanedPath = (sourceId: string): string => join(options.scratch, `cleaned-${encodeURIComponent(sourceId)}.ogg`);

  async function proxy(assetId: string, response: ServerResponse): Promise<void> {
    const asset = options.sources.get(assetId) as ResolvedAsset;
    fetched.set(assetId, (fetched.get(assetId) ?? 0) + 1);
    const fail = (reason: string): void => {
      failures.push({ assetId, fileName: asset.fileName, reason });
      if (!response.headersSent) response.writeHead(502);
      response.end();
    };
    let upstream: Response;
    try {
      upstream = await fetchImpl(asset.url);
    } catch {
      // A fetch error's cause can carry the request, and so the signature: the reason is ours.
      fail('the object store could not be reached');
      return;
    }
    if (!upstream.ok || upstream.body === null) {
      fail(`the object store answered ${String(upstream.status)}`);
      return;
    }
    response.writeHead(200, {
      'Content-Type': upstream.headers.get('content-type') ?? asset.mimeType,
      ...(upstream.headers.get('content-length') === null ? {} : { 'Content-Length': upstream.headers.get('content-length') ?? '' }),
    });
    try {
      // fetch is typed by the DOM lib and Readable.fromWeb by node:stream/web: the same object.
      await pipeline(Readable.fromWeb(upstream.body as NodeReadableStream<Uint8Array>), response);
    } catch {
      fail('the object store stopped sending it');
    }
  }

  async function receive(request: IncomingMessage, response: ServerResponse, path: string): Promise<void> {
    await pipeline(request, createWriteStream(path));
    response.writeHead(204).end();
  }

  async function serveFile(path: string, response: ServerResponse): Promise<void> {
    try {
      const info = await stat(path);
      response.writeHead(200, { 'Content-Type': TYPES[extname(path)] ?? 'application/octet-stream', 'Content-Length': info.size });
      await pipeline(createReadStream(path), response);
    } catch {
      if (!response.headersSent) response.writeHead(404);
      response.end();
    }
  }

  async function route(request: IncomingMessage, response: ServerResponse): Promise<void> {
    const { pathname } = new URL(request.url ?? '/', 'http://page');
    if (request.method === 'GET') {
      const assetId = allowedMedia(pathname, allowlist);
      const keptPath = assetId === undefined ? undefined : kept.get(assetId);
      if (keptPath !== undefined) return await serveFile(keptPath, response);
      if (assetId !== undefined) return await proxy(assetId, response);
      const file = pathname.startsWith('/media/') ? undefined : ownFile(options.pageDir, pathname);
      if (file !== undefined) return await serveFile(file, response);
    }
    if (request.method === 'POST') {
      if (pathname === '/output') return await receive(request, response, filmPath);
      const sourceId = allowedCleaned(pathname, cleaning);
      if (sourceId !== undefined) return await receive(request, response, cleanedPath(sourceId));
    }
    refused.push(`${request.method ?? '?'} ${pathname}`);
    response.writeHead(403).end();
  }

  const server = createServer((request, response) => {
    route(request, response).catch(() => {
      if (!response.headersSent) response.writeHead(500);
      response.end();
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address() as AddressInfo;
  return {
    origin: `http://127.0.0.1:${String(port)}`,
    failures,
    refused,
    fetched,
    filmPath,
    cleanedPath,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
}
```

- [ ] **Step 4: Run it to see it pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/proxy.test.ts`

Expected: `Tests  25 passed (25)`, `VITEST_RC=0`. Among them, "plays a copy the job made and has not saved from scratch, under the local id it was given" reads the copy back as `audio/ogg`, and finds that another local id is refused and nothing was fetched.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 5: Show the halfway guard bites**

"records a source whose body stops halfway, after its status said yes" holds the one line in `pageServer.ts` that records a stream failing after its 200: `fail('the object store stopped sending it');`, in the `catch` around the `pipeline`. Check that the test fails without it.

1. Put `// ` in front of that line.
2. Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/proxy.test.ts`

   Expected (measured):
   - `× records a source whose body stops halfway, after its status said yes`;
   - `AssertionError: expected [] to deeply equal [ { …(3) } ]`;
   - `Tests  1 failed | 24 passed (25)`, `VITEST_RC=1`.
3. Remove the `// ` again, and re-run the same command.

   Expected: `Tests  25 passed (25)`, `VITEST_RC=0`.

- [ ] **Step 6: Commit**

Write `$W/msg-t16.txt`:

```text
feat(video-mcp): give each job its own origin and a source allowlist

The render page reaches one loopback origin: its own files, the media
proxy, and the two doors its results leave by. The proxy streams a
source from its signed link only when the job resolved that asset,
keeps no copy (a copy would be charged to the renderer's own memory
limit), and records a source the store will not give, or stops giving
halfway, by its file name and never by its URL. A copy the job made
and has not saved yet is played from its scratch, so playing it needs
no token. The two judgments are pure functions, so mutation testing
can hold them.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t16.txt services/video-mcp/src/jobs/proxy.ts services/video-mcp/src/jobs/pageServer.ts services/video-mcp/test/unit/proxy.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 17: The render page, the browser, and the job's six steps

**Mandatory review corrections R1–R4.** `played.ts`, `job.ts` and their unit tests below
are the pre-review baseline, not an implementation of the revised contract. Add all four
regressions from §Applicability corrections before rewriting them. The rest of this task
describes the intended pipeline; its upload/cancellation and dependency selection must
follow that section.

**A job's compute never needs a token, and its writes wait for one** (spec §"Amended 2026-10-02" 2, Q1). Aura's tokens last 15 minutes, and a 10-minute film renders for over 20 (Task 13). So the job is cut in two:
- **What it reads was taken at its start.** `video_render_start` (Task 18) loads the project and resolves every asset it plays with the start call's own token (`played.ts`). The request hands the job those signed links, which last two hours, while a job reads only within its first 60 minutes. A source the library no longer holds refuses the start, naming it; nothing is queued.
- **What it makes stays in its scratch until the end.** The cleaned copies are played from scratch under a local id, `kept-<source id>` (Task 16), and the speech windows stay in memory. The film is transcoded and checked there too. Every write to Aura happens in the last step, and nothing is saved before it.

The steps of spec §Jobs 2, in order, each reported to the queue:

1. **analyses.** The speech windows that ducking has not heard and the cleaned copies that noise reduction has not made.
   - They run in the page, because the Studio runs them in the browser.
   - They enter the project the way the Studio records an automatic analysis (`recordAnalysis`), with each cleaned copy under its local id, and the job renders that project.
2. **compile.** `toVideoJSON` in Node, so a project the Studio cannot compile fails before a browser starts.
3. **render.** The page's `exportProject` at Task 11's `videoBitrate` and `renderSize`. Progress arrives once per percent.
4. **transcode.** Task 12. The browser's own film is deleted as soon as the delivered one exists: the tmpfs is memory (Task 13).
5. **check.** Task 15, on the delivered file.
6. **upload**, every write, in this order:
   1. each cleaned copy, as a new audio asset;
   2. the analyses, recorded again with the copies' real asset ids and saved as a new project version, so a later render reuses them;
   3. the film, as a new video asset, and a `resolve` of it for the two-hour download link.

The rules the job keeps:
- Each authenticated request selects the freshest caller bearer at dispatch, including finalize after PUT and public resolve after film acceptance. Credential acquisition checks cancellation on both the immediate and waiting paths. All waits use the time remaining to one deadline, fixed at output-check completion plus `CLAIM_MS` (provisionally 15 minutes, Q1). `controls.waitFor` stops the stall watchdog and age limit only while waiting for credentials (Task 14). The job keeps the queue's slot meanwhile, so a held film never shares the memory limit with another render (Task 13: it costs its own bytes, as shmem).
- Every byte the page reads comes through the job's own origin (Task 16).
- Scratch is retained while credential refresh or link recovery is pending within the claim deadline. It is emptied when the job succeeds, finally fails, is cancelled or expires; deleting scratch does not roll back accepted library assets.
- Each failure is one sentence that names what failed:
  - a source by its file name: "screen.mp4 could not be played by the renderer", "camera.mp4 never reached the renderer";
  - a check: "the film failed its check: …";
  - a kill for memory: Task 13's sentence, when `oomKills()` rose during the render;
  - Aura not signing the film it just accepted, naming that film's asset id and any earlier accepted writes; a link retry uses that same asset;
  - no fresh caller credential by the claim deadline. When this happens before any save, the checked scratch is discarded and status says no output was saved. When it happens after an accepted write, status lists the saved assets and identifies the remaining unsaved output. It never claims that an accepted film was discarded from the library.

**The page** (`page/render.ts`) is the page's one door into `web/src/videoStudio`, as `src/studio.ts` is the server's. It sets `window.aura` to `{detectSpeech, clean, render}`. A failure comes back as plain data, `PageFailure {failed: {assetId, failure, message}}`: an `Error` thrown inside `page.evaluate` reaches Node as a string, and an `ExportSourceError`'s asset id would be lost (Plan A's carry-over).

**The browser** (`browser.ts`) is one Chrome Headless Shell per job, launched with renderer-server's flags (`ServerRenderer.js:98-129`) and closed when the job ends, however it ends. Every request any of its pages makes passes the context's route and leaves only for the job's own origin (`pageMayFetch`). What it blocked is kept for the record.

`inPage.ts` holds the four functions `page.evaluate` sends into the browser. No Node tier sees them run, so they are out of both coverage tiers (`IN_PAGE`).

**Files:**
- Modify: `services/video-mcp/package.json` and `package-lock.json` (Playwright 1.63.0), `services/video-mcp/tsconfig.json` (the fvad declaration), `services/video-mcp/src/studio.ts` (`recordAnalysis`, `toVideoJSON`), `services/video-mcp/src/jobs/limits.ts` (`CLAIM_MS`)
- Create: `services/video-mcp/page/render.ts`, `services/video-mcp/types/url-imports.d.ts`
- Create: `services/video-mcp/src/jobs/inPage.ts`, `browser.ts`, `analyses.ts`, `played.ts`, `job.ts`
- Test: `services/video-mcp/test/unit/analyses.test.ts`, `services/video-mcp/test/unit/job.test.ts`

**Interfaces:**
- Consumes:
  - `InternalApi`, `ResolvedAsset` (Task 3); `Credentials` with `current` and `next` (Task 2); `ProjectStore`, `projectStore` (Task 9); `Refusal` (Task 5);
  - `renderSize`, `videoBitrate` (Task 11); `transcode` (Task 12); `oomKills`, `outOfMemory` (Task 13);
  - `Delivery`, `JobRequest` with `project` and `sources`, `Runner`, `Step` and `RunControls.waitFor` (Task 14); `outputProblems`, `FilmFacts`, `filmFacts`, `samplePlan`, `SamplePlan` (Task 15);
  - `startPageServer` with `kept`, `PageServer`, `pageMayFetch` (Task 16);
  - `FakeInternalApi` (Task 3), `sampleProject` (Task 5), `IDENTITY` (Task 2).
- Produces:
  - `page/render.ts`: `interface PageFailure { failed: { assetId: string | null; failure: 'unreachable' | 'undecodable' | 'error'; message } }`, `interface PageApi { detectSpeech; clean; render(project, videoBitrate) }`, and `window.aura`, `window.reportProgress`;
  - `inPage.ts`: `pageReady`, `detectSpeechIn`, `cleanIn`, `renderIn`;
  - `browser.ts`: `interface RenderPage { page; blocked; close() }`, `type OpenPage = (origin, signal, onProgress?) => Promise<RenderPage>` and `openRenderPage: OpenPage`;
  - `analyses.ts`: `interface AnalysisPlan { speech; clean }`, `interface AnalysisResults { speech; cleaned }`, `analysisPlan(project)`, `analysisCount(plan)` and `withAnalyses(project, results)`;
  - `played.ts`: `playedAssets(project): Map<string, string>` (asset id → what plays it) and `resolvePlayed(api: InternalApi, token: string, project: VideoProject): Promise<Map<string, ResolvedAsset>>`, which throws a `Refusal` naming the first played asset the library no longer holds. Task 18's start calls it;
  - `job.ts`: `interface JobParts { api; store; credentials; pageDir; scratchRoot; openPage?; transcode?; facts?; oomKills?; claimMs? }` and `renderRunner(parts: JobParts): Runner`. A delivery carries the film's `fileName` (the internal API's row);
  - `limits.ts`: `CLAIM_MS = 15 * 60_000`.

- [ ] **Step 1: Add Playwright**

Replace `services/video-mcp/package.json` with:

```json
{
  "name": "aura-video-mcp",
  "private": true,
  "version": "0.0.0",
  "description": "Aura's video sidecar: Studio projects edited and rendered over MCP",
  "type": "module",
  "engines": {
    "node": ">=24.16.0 <25"
  },
  "scripts": {
    "typecheck": "tsc --noEmit -p tsconfig.json",
    "test": "vitest run --coverage"
  },
  "dependencies": {
    "@modelcontextprotocol/sdk": "1.31.0",
    "jose": "6.2.12",
    "playwright": "1.63.0",
    "zod": "4.6.5"
  },
  "devDependencies": {
    "@types/express": "5.0.6",
    "@types/node": "24.19.1",
    "@vitest/coverage-v8": "5.0.3",
    "ajv": "8.20.0",
    "typescript": "7.0.2",
    "vitest": "5.0.3"
  }
}
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc-npm.sh install`

Expected: `NPM_RC=0`. Playwright's package downloads no browser on install; the image installs the headless shell (Task 20).

- [ ] **Step 2: Write the failing tests**

`services/video-mcp/test/unit/analyses.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { analysisCount, analysisPlan, withAnalyses } from '../../src/jobs/analyses';
import { setAudioProperties, setClipPresentation } from '../../src/studio';
import { sampleProject } from '../support/projects';

describe('what a render analyses first', () => {
  it('is nothing for a project with no ducking and no noise reduction', () => {
    expect(analysisCount(analysisPlan(sampleProject()))).toBe(0);
  });

  it('is the speech ducking has not heard and the copies noise reduction has not made, recorded the Studio’s way', () => {
    let project = setAudioProperties(sampleProject(), { itemId: 'm1', ducking: { amountDb: -12, ramp: 0.5 } });
    project = setClipPresentation(project, { clipId: 'c1', denoise: true });
    const plan = analysisPlan(project);
    expect([plan.speech.map((source) => source.id), plan.clean.map((source) => source.id)]).toEqual([['src-camera'], ['src-camera']]);
    const analysed = withAnalyses(project, {
      speech: new Map([['src-camera', [[1, 3.5], [3.2, 4]] as const]]),
      cleaned: new Map([['src-camera', 'b8d4c2a0-1f3e-4d5c-9b7a-6e5f4d3c2b1a']]),
    });
    expect(analysed.sources.find((source) => source.id === 'src-camera')).toMatchObject({
      speech: [[1, 4]],
      denoisedAssetId: 'b8d4c2a0-1f3e-4d5c-9b7a-6e5f4d3c2b1a',
    });
    expect(analysisCount(analysisPlan(analysed))).toBe(0);
  });
});
```

`services/video-mcp/test/unit/job.test.ts`:

```ts
// The job's own logic, every branch, with the browser and ffmpeg replaced: the page is scripted
// (it answers each in-page call and sends its bytes to the job's real page server, as the export
// page does), the transcode copies, and the probe answers facts. The render tier proves the real
// tools (test/integration/render.test.ts).
import { copyFile, mkdtemp, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { Page } from 'playwright';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { PageFailure } from '../../page/render';
import { Credentials } from '../../src/credentials';
import { internalApi, type InternalApi } from '../../src/internalApi';
import type { OpenPage } from '../../src/jobs/browser';
import { cleanIn, detectSpeechIn, renderIn } from '../../src/jobs/inPage';
import { renderRunner, type JobParts } from '../../src/jobs/job';
import type { FilmFacts } from '../../src/jobs/outputCheck';
import { playedAssets, resolvePlayed } from '../../src/jobs/played';
import type { Delivery, JobRequest, Step } from '../../src/jobs/queue';
import { projectStore, type ProjectStore } from '../../src/projects';
import { Refusal } from '../../src/refusal';
import { setAudioProperties, setClipPresentation, type VideoProject } from '../../src/studio';
import { FakeInternalApi } from '../support/fakeInternalApi';
import { sampleProject } from '../support/projects';
import { IDENTITY } from '../support/tokens';

interface Script {
  speech?: (assetId: string) => { speech: [number, number][] } | PageFailure;
  clean?: (assetId: string) => PageFailure | undefined;
  render?: () => PageFailure | undefined;
  /** Rejects the render call, as page.evaluate does when the page dies. */
  crash?: Error;
}

let fake: FakeInternalApi;
let api: InternalApi;
let store: ProjectStore;
let credentials: Credentials;
let scratchRoot: string;
let pageDir: string;
let ids: Record<string, string>;
let rendered: { project: VideoProject; bitrate: number }[];

beforeEach(async () => {
  fake = new FakeInternalApi();
  await fake.start();
  fake.trust('bearer', IDENTITY);
  api = internalApi(fake.url);
  store = projectStore(api);
  credentials = new Credentials();
  credentials.remember({ identity: IDENTITY, token: 'bearer', expiresAt: Date.now() / 1000 + 900 });
  scratchRoot = await mkdtemp(join(tmpdir(), 'job-scratch-'));
  pageDir = await mkdtemp(join(tmpdir(), 'job-page-'));
  rendered = [];
  ids = {};
  const seeds: [string, string][] = [['camera.mp4', 'video/mp4'], ['screen.mp4', 'video/mp4'], ['music.wav', 'audio/wav'], ['still.png', 'image/png']];
  for (const [fileName, mimeType] of seeds) {
    ids[fileName] = fake.seed(IDENTITY, { fileName, mimeType, bytes: Buffer.from(`${fileName} bytes`) }).id;
  }
});

afterEach(async () => {
  await fake.close();
  await rm(scratchRoot, { recursive: true, force: true });
  await rm(pageDir, { recursive: true, force: true });
});

/** The sample project on the seeded assets, saved; its asset id. */
async function saved(change: (project: VideoProject) => VideoProject = (project) => project): Promise<string> {
  const byId: Record<string, string> = { 'src-camera': 'camera.mp4', 'src-screen': 'screen.mp4', 'src-music': 'music.wav', 'src-still': 'still.png' };
  const project = sampleProject();
  return await store.save('bearer', change({ ...project, sources: project.sources.map((source) => ({ ...source, assetId: ids[byId[source.id] ?? ''] ?? '' })) }));
}

/** A page that answers each in-page call from `script`, fetching and sending bytes through the
 *  job's own origin the way page/render.ts does. */
function scriptedPages(script: Script): OpenPage {
  return async (origin, signal, onProgress) => {
    const page = {
      evaluate: async (fn: unknown, argument: unknown) => {
        if (fn === detectSpeechIn) return script.speech?.(argument as string) ?? { speech: [[1, 2]] };
        if (fn === cleanIn) {
          const [assetId, sourceId] = argument as [string, string, string];
          const failure = script.clean?.(assetId);
          if (failure !== undefined) return failure;
          await fetch(`${origin}/cleaned/${sourceId}`, { method: 'POST', body: 'ogg' });
          return { bytes: 3, name: 'camera.clean.ogg' };
        }
        if (fn === renderIn) {
          const [project, bitrate] = argument as [VideoProject, number];
          rendered.push({ project, bitrate });
          if (script.crash !== undefined) throw script.crash;
          for (const source of project.sources) await (await fetch(`${origin}/media/${source.assetId}`)).arrayBuffer();
          onProgress?.(0.5);
          await new Promise<void>((resolve, reject) => {
            if (signal.aborted) reject(signal.reason as Error);
            signal.addEventListener('abort', () => reject(signal.reason as Error));
            setTimeout(resolve, 5);
          });
          const failure = script.render?.();
          if (failure !== undefined) return failure;
          await fetch(`${origin}/output`, { method: 'POST', body: 'film' });
          return { bytes: 4 };
        }
        throw new Error('the job called something the page does not have');
      },
    } as unknown as Page;
    return { page, blocked: [], close: async () => undefined };
  };
}

function goodFacts(width = 1080, height = 1920, seconds = 14): FilmFacts {
  return {
    bytes: 4,
    formatName: 'mov,mp4,m4a,3gp,3g2,mj2',
    topLevelBoxes: ['ftyp', 'moov', 'mdat'],
    editLists: 0,
    formatSeconds: seconds,
    video: [{ codec: 'h264', profile: 'High', width, height, pixFmt: 'yuvj420p', fieldOrder: 'progressive', fps: 30, hasBFrames: 0, seconds }],
    audio: [{ codec: 'aac', profile: 'LC', sampleRate: 48_000, channels: 2, bitRate: 128_000 }],
    maxBitsPerSecond: 5_000_000,
    frames: [{ at: 1.4, lumaMax: 235 }],
    sound: [],
  };
}

function runner(script: Script = {}, over: Partial<JobParts> = {}) {
  return renderRunner({
    api,
    store,
    credentials,
    pageDir,
    scratchRoot,
    openPage: scriptedPages(script),
    transcode: (input, output) => copyFile(input, output),
    facts: async () => goodFacts(),
    oomKills: async () => 0,
    ...over,
  });
}

/** What video_render_start hands the queue: the project and its played assets, read with the
 *  start call's bearer. */
async function started(projectAssetId: string, quality: JobRequest['quality'] = '1080p'): Promise<JobRequest> {
  const project = await store.load('bearer', projectAssetId);
  return { identity: IDENTITY, projectAssetId, quality, project, sources: await resolvePlayed(api, 'bearer', project) };
}

/** The file names in every job's scratch directory, now. */
async function held(): Promise<string[]> {
  return (await readdir(scratchRoot, { recursive: true })).filter((path) => path.includes('/')).map((path) => path.split('/')[1] ?? '').sort();
}

const ducked = (sample: VideoProject): VideoProject =>
  setClipPresentation(setAudioProperties(sample, { itemId: 'm1', ducking: { amountDb: -12, ramp: 0.5 } }), { clipId: 'c1', denoise: true });

/** Runs `job` as the queue would. `onWait` runs when the job starts waiting for its caller. */
async function run(job: ReturnType<typeof runner>, projectAssetId: string, signal = new AbortController().signal, onWait = () => {}) {
  const steps: [Step, number][] = [];
  const request = await started(projectAssetId);
  const delivery = await job(request, {
    signal,
    progress: (step, fraction) => steps.push([step, fraction]),
    waitFor: (work) => {
      steps.push(['upload', -1]);
      onWait();
      return work;
    },
  });
  return { delivery, steps };
}

describe('a render job', () => {
  it('renders, checks and delivers the film, step by step, and empties its scratch', async () => {
    const project = await saved();
    const { delivery, steps } = await run(runner(), project);
    expect(steps).toEqual([['analyses', 0], ['analyses', 1], ['compile', 1], ['render', 0.5], ['render', 1], ['transcode', 1], ['check', 1], ['upload', 1]]);
    expect(delivery).toMatchObject<Partial<Delivery>>({ fileName: 'Aura-memory.mp4', projectAssetId: project, bytes: 4, seconds: 14, width: 1080, height: 1920 });
    expect(delivery.downloadUrl).toMatch(/^http:\/\/localhost:\d+\/objects\/media\/.+\.mp4\?X-Amz-Signature=/);
    expect(fake.assets.get(delivery.filmAssetId)).toMatchObject({ fileName: 'Aura-memory.mp4', mimeType: 'video/mp4', status: 'accepted', sourceKind: 'video_mcp' });
    expect(rendered[0]?.bitrate).toBe(6_000_000);
    expect(await readdir(scratchRoot)).toEqual([]);
  });

  it('makes the missing analyses first, renders with them, and saves them as a version', async () => {
    const project = await saved(ducked);
    const { delivery, steps } = await run(runner(), project);
    expect(steps.slice(0, 4)).toEqual([['analyses', 0], ['analyses', 0.5], ['analyses', 1], ['analyses', 1]]);
    expect(rendered[0]?.project.sources.find((source) => source.id === 'src-camera')).toMatchObject({ speech: [[1, 2]], denoisedAssetId: 'kept-src-camera' });
    expect(delivery.projectAssetId).not.toBe(project);
    const version = (await store.load('bearer', delivery.projectAssetId)).sources.find((source) => source.id === 'src-camera');
    expect(version).toMatchObject({ speech: [[1, 2]] });
    expect(fake.assets.get(version?.denoisedAssetId ?? '')).toMatchObject({ fileName: 'camera.clean.ogg', mimeType: 'audio/ogg', status: 'accepted' });
  });

  it('computes with no credential at all, and makes every write with the one its caller’s next call brings', async () => {
    const project = await saved(ducked);
    const before = fake.assets.size;
    credentials = new Credentials();
    let waiting: { files: Promise<string[]>; assets: number } | undefined;
    const call = () => {
      waiting = { files: held(), assets: fake.assets.size };
      setTimeout(() => credentials.remember({ identity: IDENTITY, token: 'bearer', expiresAt: Date.now() / 1000 + 900 }), 5);
    };
    const { delivery, steps } = await run(runner(), project, undefined, call);
    expect(steps.slice(-3)).toEqual([['check', 1], ['upload', -1], ['upload', 1]]);
    expect(await waiting?.files).toEqual(['cleaned-src-camera.ogg', 'delivered.mp4']);
    expect(waiting?.assets).toBe(before);
    expect(fake.assets.size).toBe(before + 3);
    expect(fake.assets.get(delivery.filmAssetId)).toMatchObject({ mimeType: 'video/mp4', status: 'accepted', sourceKind: 'video_mcp' });
    expect((await store.load('bearer', delivery.projectAssetId)).sources.find((source) => source.id === 'src-camera')?.denoisedAssetId).not.toBe('kept-src-camera');
  });

  it('renders at the size its quality asks, with the bitrate a long film can afford', async () => {
    const project = await saved((sample) => ({ ...sample, video: sample.video.map((clip) => (clip.id === 'c1' ? { ...clip, duration: 18 } : clip)) }));
    const job = runner({}, { facts: async () => goodFacts(720, 1280, 24) });
    const result = await job(await started(project, '720p'), { signal: new AbortController().signal, progress: () => undefined, waitFor: (work) => work });
    expect([result.width, result.height]).toEqual([720, 1280]);
    expect(rendered.at(-1)?.project.size).toEqual({ width: 720, height: 1280 });
  });
});

describe('a render job fails, naming what failed, and delivers nothing', () => {
  const films = () => [...fake.assets.values()].filter((asset) => asset.mimeType === 'video/mp4' && asset.sourceKind === 'video_mcp');

  it.each<[string, Script, (project: VideoProject) => VideoProject, RegExp]>([
    ['when a speech analysis cannot read its sound', { speech: () => ({ failed: { assetId: null, failure: 'error', message: 'Unable to decode audio data' } }) },
      (sample) => setAudioProperties(sample, { itemId: 'm1', ducking: { amountDb: -12, ramp: 0.5 } }), /^listening for speech in camera\.mp4 failed: Unable to decode audio data$/],
    ['when a cleaned copy’s source never arrives', { clean: (assetId) => ({ failed: { assetId, failure: 'unreachable', message: 'x' } }) },
      (sample) => setClipPresentation(sample, { clipId: 'c1', denoise: true }), /^camera\.mp4 never reached the renderer$/],
    ['when the renderer cannot play a source', { render: () => ({ failed: { assetId: ids['screen.mp4'] ?? '', failure: 'undecodable', message: 'x' } }) },
      (sample) => sample, /^screen\.mp4 could not be played by the renderer$/],
    ['when the render itself fails', { render: () => ({ failed: { assetId: null, failure: 'error', message: 'out of canvas' } }) },
      (sample) => sample, /^the render failed: out of canvas$/],
    ['when the page dies', { crash: new Error('page.evaluate: Target crashed') }, (sample) => sample, /^page\.evaluate: Target crashed$/],
  ])('%s', async (_name, script, change, error) => {
    const project = await saved(change);
    await expect(run(runner(script), project)).rejects.toThrow(error);
    expect(films()).toEqual([]);
    expect(await readdir(scratchRoot)).toEqual([]);
  });

  it('when the object store will not give a source, by the store’s answer', async () => {
    const project = await saved();
    fake.objectFailures.set(ids['screen.mp4'] ?? '', 404);
    await expect(run(runner(), project)).rejects.toThrow('screen.mp4: the object store answered 404');
  });

  it('when the kernel killed the page for memory, saying so', async () => {
    const project = await saved();
    let kills = 0;
    const counted = runner({ crash: new Error('page.evaluate: Target crashed') }, { oomKills: async () => kills++ });
    await expect(run(counted, project)).rejects.toThrow(/^the renderer ran out of memory( \(the renderer may use [\d.]+ GiB\))?: render a shorter film, or use smaller source files$/);
  });

  it('when the film fails its check', async () => {
    const project = await saved();
    const black = runner({}, { facts: async () => ({ ...goodFacts(), frames: [{ at: 1.4, lumaMax: 0 }] }) });
    await expect(run(black, project)).rejects.toThrow('the film failed its check: the frame at 1.4 s is black');
    expect(films()).toEqual([]);
  });

  it('when it is cancelled while it renders, with the queue’s reason', async () => {
    const project = await saved();
    const controller = new AbortController();
    const running = run(runner(), project, controller.signal);
    setTimeout(() => controller.abort(new Error('cancelled')), 1);
    await expect(running).rejects.toThrow('cancelled');
    expect(films()).toEqual([]);
  });

  it('when no call of its caller’s came in time to save what it made, saving none of it', async () => {
    const project = await saved(ducked);
    const before = fake.assets.size;
    let clock = Date.now() / 1000;
    credentials = new Credentials(() => clock);
    credentials.remember({ identity: IDENTITY, token: 'bearer', expiresAt: clock + 900 });
    const job = runner({ render: () => void (clock += 900) }, { claimMs: 20 });
    await expect(run(job, project)).rejects.toThrow(
      'the film was rendered and passed its check, but no call of yours came within 15 minutes with a fresh credential to save it, so it was discarded: start the render again',
    );
    expect(fake.assets.size).toBe(before);
    expect(await readdir(scratchRoot)).toEqual([]);
  });

  it('when Aura accepts the film but will not sign a link to it', async () => {
    const project = await saved();
    const linkless: InternalApi = {
      ...api,
      resolve: async (token, assetIds, endpoint) => (endpoint === 'public' ? { found: new Map(), notFound: [...assetIds] } : await api.resolve(token, assetIds, endpoint)),
    };
    const job = renderRunner({ api: linkless, store, credentials, pageDir, scratchRoot, openPage: scriptedPages({}), transcode: (input, output) => copyFile(input, output), facts: async () => goodFacts(), oomKills: async () => 0 });
    await expect(run(job, project)).rejects.toThrow(/^Aura did not answer for the film it just accepted \([0-9a-f-]{36}\)$/);
  });
});

it('refuses a project whose played asset the library no longer holds, naming it', async () => {
  const project = await store.load('bearer', await saved());
  const gone = fake.assets.get(ids['screen.mp4'] ?? '');
  if (gone !== undefined) gone.status = 'deleted';
  const refused = resolvePlayed(api, 'bearer', project);
  await expect(refused).rejects.toBeInstanceOf(Refusal);
  await expect(refused).rejects.toThrow(`video source src-screen (asset ${String(gone?.id)}) is no longer in your library`);
});

it('plays every source, every cleaned copy and every picture a project names', () => {
  const project: VideoProject = {
    ...sampleProject(),
    sources: sampleProject().sources.map((source) => (source.id === 'src-camera' ? { ...source, denoisedAssetId: 'clean-1' } : source)),
    overlays: [{ id: 'pics', items: [{ id: 'p1', kind: 'image', anchor: { clipId: 'c1', offset: 0 }, duration: 1, props: { assetId: 'poster-1' } }] }],
  };
  expect([...playedAssets(project).entries()]).toEqual([
    [sampleProject().sources[0]?.assetId, 'video source src-camera'],
    ['clean-1', 'the cleaned copy of src-camera'],
    [sampleProject().sources[1]?.assetId, 'video source src-screen'],
    [sampleProject().sources[2]?.assetId, 'audio source src-music'],
    [sampleProject().sources[3]?.assetId, 'image source src-still'],
    ['poster-1', 'the picture of overlay p1'],
  ]);
});
```

- [ ] **Step 3: Run them to see them fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/analyses.test.ts test/unit/job.test.ts`

Expected:
- `analyses.test.ts` fails with `Error: Cannot find module '../../src/jobs/analyses' imported from …`;
- `job.test.ts` fails with `Error: Cannot find module '../../src/jobs/inPage' imported from …` (its type-only imports are erased, and `inPage` is the first value import that is missing);
- `VITEST_RC=1`.

- [ ] **Step 4: Write the page and its declarations**

In `services/video-mcp/src/studio.ts`:
1. in the `export { … } from '../../../web/src/videoStudio/commands_audio';` list, add `recordAnalysis,` between `moveAudio,` and `setAudioProperties,`;
2. add this line immediately before the line `export { uncleanedSources, unheardByDucking } from '../../../web/src/videoStudio/videoflow_audio';`:

```ts
export { toVideoJSON } from '../../../web/src/videoStudio/videoflow';
```

In `services/video-mcp/tsconfig.json`, replace:

```text
    "*.ts"
  ]
```

with:

```text
    "*.ts",
    "../../web/src/videoStudio/fvad-wasm.d.ts"
  ]
```

The page reaches `audioSpeech.ts`, whose libfvad import is declared only in that web file. Measured without it: `audioSpeech.ts(1,22): error TS7016: Could not find a declaration file for module '@echogarden/fvad-wasm'`.

`services/video-mcp/types/url-imports.d.ts`:

```ts
// The Studio's analyses import their WASM and worklet as `?url` (Vite's explicit URL import,
// https://vite.dev/guide/assets#explicit-url-imports); build.ts gives them the same meaning.
declare module '*?url' {
  const url: string;
  export default url;
}
```

`services/video-mcp/page/render.ts`:

```ts
// render.ts — the export page a render job opens (spec §Architecture 3): the Studio's own
// exportProject, which serves its fonts locally (withLocalFonts), and the two analyses the Studio
// runs in the browser, speech detection and noise reduction. It is the page's one door into
// web/src/videoStudio, as src/studio.ts is the server's (drift.ts holds both to it).
//
// Every byte arrives from the page's own origin: a source at /media/<assetId>, served by the job's
// proxy. What the page makes leaves the same way, by POST: the film to /output, a cleaned copy to
// /cleaned/<sourceId>. A failure comes back as plain data, because an Error thrown inside
// page.evaluate reaches Node as a string and an ExportSourceError's asset id would be lost.

import { cleanedFile } from '../../../web/src/videoStudio/audioClean';
import { detectSpeech } from '../../../web/src/videoStudio/audioSpeech';
import type { VideoProject } from '../../../web/src/videoStudio/project';
import { exportProject } from '../../../web/src/videoStudio/videoflow';
import { ExportSourceError } from '../../../web/src/videoStudio/videoflow_media';

export interface PageFailure {
  readonly failed: {
    /** The source that stopped the export, when one did. */
    readonly assetId: string | null;
    readonly failure: 'unreachable' | 'undecodable' | 'error';
    readonly message: string;
  };
}

export interface PageApi {
  detectSpeech(assetId: string): Promise<{ readonly speech: readonly (readonly [number, number])[] } | PageFailure>;
  clean(assetId: string, sourceId: string, label: string): Promise<{ readonly bytes: number; readonly name: string } | PageFailure>;
  render(project: VideoProject, videoBitrate: number): Promise<{ readonly bytes: number } | PageFailure>;
}

declare global {
  interface Window {
    aura: PageApi;
    /** Exposed by the job (page.exposeFunction): every report feeds its stall watchdog. */
    reportProgress(fraction: number): Promise<void>;
  }
}

const mediaUrl = (assetId: string): string => `/media/${encodeURIComponent(assetId)}`;

function failed(error: unknown): PageFailure {
  if (error instanceof ExportSourceError) {
    return { failed: { assetId: error.assetId, failure: error.failure, message: error.message } };
  }
  return { failed: { assetId: null, failure: 'error', message: error instanceof Error ? error.message : String(error) } };
}

async function send(path: string, body: Blob): Promise<number> {
  const response = await fetch(path, { method: 'POST', body });
  if (!response.ok) throw new Error(`the job refused ${path}: ${String(response.status)}`);
  return body.size;
}

window.aura = {
  async detectSpeech(assetId) {
    try {
      return { speech: await detectSpeech(mediaUrl(assetId)) };
    } catch (error) {
      return failed(error);
    }
  },

  async clean(assetId, sourceId, label) {
    try {
      const file = await cleanedFile(mediaUrl(assetId), label);
      return { bytes: await send(`/cleaned/${encodeURIComponent(sourceId)}`, file), name: file.name };
    } catch (error) {
      return failed(error);
    }
  },

  async render(project, videoBitrate) {
    try {
      let reported = -1;
      const film = await exportProject(
        project,
        { assetUrl: mediaUrl },
        {
          videoBitrate,
          onProgress: (fraction) => {
            // One report per percent: each crosses into Node, and a frame-by-frame stream of them
            // is thousands of calls for nothing the bar can show.
            if (fraction - reported >= 0.01 || fraction === 1) {
              reported = fraction;
              void window.reportProgress(fraction);
            }
          },
        },
      );
      return { bytes: await send('/output', film) };
    } catch (error) {
      return failed(error);
    }
  },
};
```

- [ ] **Step 5: Write the browser, the analyses and the job**

In `services/video-mcp/src/jobs/limits.ts`, immediately after the line `export const JOB_MAX_AGE_MS = 60 * 60_000;`, add:

```ts
/** How long a checked film waits for its caller's next call when no bearer of theirs is fresh
 *  enough to save it (spec §"Amended 2026-10-02" 2): one token's life. The job keeps the queue's
 *  slot meanwhile, so what it holds on its tmpfs never shares the memory limit with the next
 *  render's browser (prd.md §12 "Render memory"). */
export const CLAIM_MS = 15 * 60_000;
```

`services/video-mcp/src/jobs/inPage.ts`:

```ts
// inPage.ts — the functions the job runs inside the render page. page.evaluate serializes each one
// into the browser, so it runs there and never in Node: Node's coverage cannot see it run, which
// is the one reason this file is out of both coverage tiers (vitest.config.ts IN_PAGE). What they
// call is page/render.ts.

import type { VideoProject } from '../studio';

export const pageReady = (): boolean => 'aura' in window;

export const detectSpeechIn = (assetId: string) => window.aura.detectSpeech(assetId);

export const cleanIn = ([assetId, sourceId, label]: readonly [string, string, string]) =>
  window.aura.clean(assetId, sourceId, label);

export const renderIn = ([project, bitrate]: readonly [VideoProject, number]) => window.aura.render(project, bitrate);
```

`services/video-mcp/src/jobs/browser.ts`:

```ts
// browser.ts — one headless shell per job, closed when the job ends however it ends, so the only
// thing resident at rest is Node (spec §Architecture 3). Every request any page of it makes passes
// through the context's route and leaves only for the job's own origin (proxy.ts pageMayFetch).

import { chromium, type Browser, type Page } from 'playwright';
import { log } from '../log';
import { pageReady } from './inPage';
import { pageMayFetch } from './proxy';

/** renderer-server 1.3.4's own launch flags (ServerRenderer.js:98-129, copied by the spike in
 *  spikes/video-mcp-render/lib/browsers.mjs), so the export runs as the measured renders did. Its
 *  comment there is why `--single-process` is absent: one V8 heap cap would cover the page, the
 *  compositor and the growing output, and a long export dies at the same percentage every time. */
const RENDER_ARGS = [
  '--no-sandbox',
  '--js-flags=--max-old-space-size=4096',
  '--enable-blink-features=CanvasDrawElement',
  '--disable-frame-rate-limit',
  '--disable-gpu-vsync',
  '--no-zygote',
  '--disable-gpu',
  '--disable-dev-shm-usage',
  '--disable-background-timer-throttling',
  '--disable-renderer-backgrounding',
  '--disable-features=site-per-process',
  '--disable-extensions',
] as const;

/** A page wider than 640 px buys nothing: the export draws to its own canvas at the film's size. */
const VIEWPORT = { width: 640, height: 640 };

export interface RenderPage {
  readonly page: Page;
  /** URLs the page tried to reach outside its origin, query strings dropped. */
  readonly blocked: readonly string[];
  /** Closes the browser; safe to call twice. */
  close(): Promise<void>;
}

/** Opens a page of the job's origin in a browser of its own. `onProgress` hears the export's
 *  reports; the analyses make none. */
export type OpenPage = (origin: string, signal: AbortSignal, onProgress?: (fraction: number) => void) => Promise<RenderPage>;

export const openRenderPage: OpenPage = async (origin, signal, onProgress) => {
  signal.throwIfAborted();
  const browser: Browser = await chromium.launch({ headless: true, args: [...RENDER_ARGS] });
  let closed: Promise<void> | undefined;
  const close = (): Promise<void> => (closed ??= browser.close());
  signal.addEventListener('abort', () => void close(), { once: true });
  const blocked: string[] = [];
  try {
    const context = await browser.newContext({ viewport: VIEWPORT });
    await context.route('**/*', async (route) => {
      const url = route.request().url();
      if (pageMayFetch(url, origin)) return await route.continue();
      const { origin: elsewhere, pathname } = new URL(url);
      blocked.push(`${elsewhere}${pathname}`);
      await route.abort('blockedbyclient');
    });
    const page = await context.newPage();
    page.on('pageerror', (error) => log('warn', 'the render page raised an error', { reason: error.message.slice(0, 300) }));
    await page.exposeFunction('reportProgress', (fraction: number) => onProgress?.(fraction));
    const loaded = await page.goto(`${origin}/render.html`);
    if (loaded?.ok() !== true) throw new Error(`the render page did not load (${String(loaded?.status())})`);
    await page.waitForFunction(pageReady);
    return { page, blocked, close };
  } catch (error) {
    await close();
    throw error;
  }
};
```

`services/video-mcp/src/jobs/analyses.ts`:

```ts
// analyses.ts — what a render computes before it can play the project as asked (spec §Tools,
// "Analyses"): the speech windows ducking has not heard, and the cleaned copies noise reduction
// has not made. They enter the project the way the Studio records an automatic analysis
// (History.annotate around recordAnalysis, commands_audio.ts), and the job saves the result as a
// new version, so a later render reuses them.

import { recordAnalysis, uncleanedSources, unheardByDucking, type ProjectSource, type VideoProject } from '../studio';

export interface AnalysisPlan {
  readonly speech: readonly ProjectSource[];
  readonly clean: readonly ProjectSource[];
}

export interface AnalysisResults {
  readonly speech: ReadonlyMap<string, readonly (readonly [number, number])[]>;
  /** Project source id → the cleaned copy's asset id. */
  readonly cleaned: ReadonlyMap<string, string>;
}

export function analysisPlan(project: VideoProject): AnalysisPlan {
  return { speech: unheardByDucking(project), clean: uncleanedSources(project) };
}

export function analysisCount(plan: AnalysisPlan): number {
  return plan.speech.length + plan.clean.length;
}

/** The project with every result recorded through the Studio's own door. */
export function withAnalyses(project: VideoProject, results: AnalysisResults): VideoProject {
  let next = project;
  for (const [sourceId, speech] of results.speech) next = recordAnalysis(next, { sourceId, speech });
  for (const [sourceId, denoisedAssetId] of results.cleaned) next = recordAnalysis(next, { sourceId, denoisedAssetId });
  return next;
}
```

`services/video-mcp/src/jobs/played.ts`:

```ts
// played.ts — what a render reads from Aura, taken when it starts, with the token the start call
// brought (spec §"Amended 2026-10-02" 2): a signed link to every asset the project plays. The
// links last two hours and a job reads them within its first hour (limits.ts JOB_MAX_AGE_MS), so
// a job's compute never needs a token; only its writes do (job.ts).

import type { InternalApi, ResolvedAsset } from '../internalApi';
import { Refusal } from '../refusal';
import type { VideoProject } from '../studio';

/** Every asset the project plays: its sources, their cleaned copies and its pictures. */
export function playedAssets(project: VideoProject): Map<string, string> {
  const played = new Map<string, string>();
  for (const source of project.sources) {
    played.set(source.assetId, `${source.kind} source ${source.id}`);
    if (source.denoisedAssetId !== undefined) played.set(source.denoisedAssetId, `the cleaned copy of ${source.id}`);
  }
  for (const item of project.overlays.flatMap((lane) => lane.items)) {
    if (typeof item.props.assetId === 'string') played.set(item.props.assetId, `the picture of overlay ${item.id}`);
  }
  return played;
}

/** A signed link to every asset `project` plays, or a refusal naming the first one the caller's
 *  library no longer holds. */
export async function resolvePlayed(api: InternalApi, token: string, project: VideoProject): Promise<Map<string, ResolvedAsset>> {
  const played = playedAssets(project);
  const { found, notFound } = await api.resolve(token, [...played.keys()]);
  const missing = notFound[0];
  if (missing !== undefined) {
    throw new Refusal(`${String(played.get(missing))} (asset ${missing}) is no longer in your library`);
  }
  return new Map(found);
}
```

`services/video-mcp/src/jobs/job.ts`:

```ts
// job.ts — one render, the steps of spec §Jobs 2 in order: the missing analyses, the compile in
// Node, the render in the page, ffmpeg, the output check, and then every write the job makes. What
// it reads was taken when it started (played.ts), so its compute never needs a token. What it
// makes, the cleaned copies and the film, waits in its scratch for the save step, which takes the
// freshest bearer the caller has shown (credentials.ts) and, when none is fresh, waits for their
// next call to bring one (spec §"Amended 2026-10-02" 2). Every byte the page reads comes through
// the job's own origin (pageServer.ts), and the scratch directory is emptied however the job ends.

import { openAsBlob } from 'node:fs';
import { mkdtemp, rm } from 'node:fs/promises';
import { join } from 'node:path';
import type { PageFailure } from '../../page/render';
import type { Credentials } from '../credentials';
import type { InternalApi } from '../internalApi';
import type { ProjectStore } from '../projects';
import { projectDuration, projectFileName, toVideoJSON, type ProjectSource, type VideoProject } from '../studio';
import { analysisCount, analysisPlan, withAnalyses } from './analyses';
import { openRenderPage, type OpenPage, type RenderPage } from './browser';
import { cleanIn, detectSpeechIn, renderIn } from './inPage';
import { outputProblems, type FilmFacts } from './outputCheck';
import { filmFacts } from './outputProbe';
import { startPageServer, type PageServer } from './pageServer';
import type { Delivery, Runner } from './queue';
import { CLAIM_MS, renderSize, videoBitrate } from './limits';
import { oomKills, outOfMemory } from './memory';
import { samplePlan, type SamplePlan } from './samples';
import { transcode } from './transcode';

export interface JobParts {
  readonly api: InternalApi;
  readonly store: ProjectStore;
  readonly credentials: Credentials;
  /** dist/page: render.html, render.js, assets, fonts. */
  readonly pageDir: string;
  /** The tmpfs every job's scratch directory is made in. */
  readonly scratchRoot: string;
  readonly openPage?: OpenPage;
  readonly transcode?: (input: string, output: string, signal: AbortSignal) => Promise<void>;
  readonly facts?: (path: string, plan: SamplePlan) => Promise<FilmFacts>;
  /** The container's OOM-kill count (memory.ts), read around the render. */
  readonly oomKills?: () => Promise<number>;
  /** How long the save step waits for its caller's next call (limits.ts CLAIM_MS). */
  readonly claimMs?: number;
}

const DISCARDED = `the film was rendered and passed its check, but no call of yours came within ${String(CLAIM_MS / 60_000)} minutes with a fresh credential to save it, so it was discarded: start the render again`;

const mediaUrl = (assetId: string): string => `/media/${encodeURIComponent(assetId)}`;

/** The id a cleaned copy is played under until the save step gives it an asset id. */
const keptId = (sourceId: string): string => `kept-${sourceId}`;

interface KeptCopy {
  readonly path: string;
  readonly fileName: string;
}

function pageFailure(answer: PageFailure, names: ReadonlyMap<string, string>, doing: string): Error {
  const { assetId, failure, message } = answer.failed;
  const name = assetId === null ? undefined : (names.get(assetId) ?? assetId);
  if (name !== undefined && failure === 'unreachable') return new Error(`${name} never reached the renderer`);
  if (name !== undefined && failure === 'undecodable') return new Error(`${name} could not be played by the renderer`);
  return new Error(`${doing} failed: ${message}`);
}

export function renderRunner(parts: JobParts): Runner {
  const open = parts.openPage ?? openRenderPage;
  const toDelivered = parts.transcode ?? transcode;
  const measure = parts.facts ?? filmFacts;
  const killed = parts.oomKills ?? oomKills;
  const claimMs = parts.claimMs ?? CLAIM_MS;

  return async (request, { signal, progress, waitFor }) => {
    /** The caller's freshest bearer; with none fresh, the one their next call brings. */
    const bearer = async (): Promise<string> => {
      const token =
        parts.credentials.current(request.identity) ??
        (await waitFor(parts.credentials.next(request.identity, claimMs, signal)));
      if (token === undefined) throw new Error(DISCARDED);
      return token;
    };
    const names = new Map([...request.sources].map(([assetId, asset]): [string, string] => [assetId, asset.fileName]));
    const scratch = await mkdtemp(join(parts.scratchRoot, 'job-'));
    let server: PageServer | undefined;
    let browser: RenderPage | undefined;
    const stop = async (): Promise<void> => {
      await browser?.close();
      await server?.close();
      browser = undefined;
      server = undefined;
    };
    try {
      const plan = analysisPlan(request.project);
      const total = analysisCount(plan);
      const speech = new Map<string, readonly (readonly [number, number])[]>();
      const copies = new Map<string, KeptCopy>();
      progress('analyses', 0);
      if (total > 0) {
        server = await startPageServer({ pageDir: parts.pageDir, scratch, sources: request.sources, cleaning: new Set(plan.clean.map((source) => source.id)) });
        browser = await open(server.origin, signal);
        const { page } = browser;
        let done = 0;
        const named = (source: ProjectSource): string => names.get(source.assetId) ?? source.id;
        for (const source of plan.speech) {
          const answer = await page.evaluate(detectSpeechIn, source.assetId);
          if ('failed' in answer) throw pageFailure(answer, names, `listening for speech in ${named(source)}`);
          speech.set(source.id, answer.speech);
          progress('analyses', ++done / total);
        }
        for (const source of plan.clean) {
          const answer = await page.evaluate(cleanIn, [source.assetId, source.id, named(source)] as const);
          if ('failed' in answer) throw pageFailure(answer, names, `cleaning ${named(source)}`);
          copies.set(source.id, { path: server.cleanedPath(source.id), fileName: answer.name });
          names.set(keptId(source.id), answer.name);
          progress('analyses', ++done / total);
        }
        await stop();
      }
      progress('analyses', 1);
      const played = withAnalyses(request.project, { speech, cleaned: new Map([...copies.keys()].map((id): [string, string] => [id, keptId(id)])) });

      const seconds = projectDuration(played);
      const size = renderSize(played.size, request.quality);
      const film: VideoProject = { ...played, size };
      await toVideoJSON(film, { assetUrl: mediaUrl });
      progress('compile', 1);

      const kept = new Map([...copies].map(([sourceId, copy]): [string, string] => [keptId(sourceId), copy.path]));
      server = await startPageServer({ pageDir: parts.pageDir, scratch, sources: request.sources, kept });
      browser = await open(server.origin, signal, (fraction) => progress('render', fraction));
      const killedBefore = await killed();
      let rendered: Awaited<ReturnType<Window['aura']['render']>>;
      try {
        rendered = await browser.page.evaluate(renderIn, [film, videoBitrate(request.quality, seconds)] as const);
      } catch (error) {
        // A cancel closes the browser under the page: the queue's reason is the one to report.
        signal.throwIfAborted();
        if ((await killed()) > killedBefore) throw new Error(await outOfMemory());
        throw error;
      }
      const failed = server.failures[0];
      const filmPath = server.filmPath;
      await stop();
      // The proxy saw the object store's answer; the page only saw that the bytes did not come.
      if (failed !== undefined) throw new Error(`${failed.fileName}: ${failed.reason}`);
      if ('failed' in rendered) throw pageFailure(rendered, names, 'the render');
      progress('render', 1);

      const delivered = join(scratch, 'delivered.mp4');
      await toDelivered(filmPath, delivered, signal);
      // The tmpfs is memory: only the film that may be delivered is kept.
      await rm(filmPath, { force: true });
      progress('transcode', 1);

      const problems = outputProblems(await measure(delivered, samplePlan(film)), { seconds, fps: played.fps, ...size });
      if (problems.length > 0) throw new Error(`the film failed its check: ${problems.join('; ')}`);
      progress('check', 1);

      signal.throwIfAborted();
      let projectAssetId = request.projectAssetId;
      if (total > 0) {
        const cleaned = new Map<string, string>();
        for (const [sourceId, copy] of copies) {
          const row = await parts.api.upload(await bearer(), {
            fileName: copy.fileName,
            mimeType: 'audio/ogg',
            modality: 'audio',
            use: 'media',
            body: await openAsBlob(copy.path, { type: 'audio/ogg' }),
          });
          cleaned.set(sourceId, row.assetId);
        }
        projectAssetId = await parts.store.save(await bearer(), withAnalyses(request.project, { speech, cleaned }));
      }
      const owner = await bearer();
      const row = await parts.api.upload(owner, {
        fileName: projectFileName(played, 'mp4'),
        mimeType: 'video/mp4',
        modality: 'video',
        use: 'media',
        body: await openAsBlob(delivered, { type: 'video/mp4' }),
      });
      const link = (await parts.api.resolve(owner, [row.assetId], 'public')).found.get(row.assetId);
      if (link === undefined) throw new Error(`Aura did not answer for the film it just accepted (${row.assetId})`);
      progress('upload', 1);
      const delivery: Delivery = {
        filmAssetId: row.assetId,
        fileName: row.fileName,
        projectAssetId,
        bytes: row.sizeBytes,
        seconds,
        width: size.width,
        height: size.height,
        downloadUrl: link.url,
        downloadExpiresAt: link.expiresAt,
      };
      return delivery;
    } finally {
      await stop();
      await rm(scratch, { recursive: true, force: true });
    }
  };
}
```

- [ ] **Step 6: Run them to see them pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/analyses.test.ts test/unit/job.test.ts`

Expected: `Test Files  2 passed (2)`, `Tests  19 passed (19)`, `VITEST_RC=0`. The job tests drive the runner with a scripted page in place of the browser: the real browser, ffmpeg and ffprobe are the render tier's (Task 20). Three of them hold the redirect's rule:
- "computes with no credential at all, and makes every write with the one its caller’s next call brings" runs a project that needs both analyses with an empty `Credentials`. When the job starts to wait, its scratch holds exactly `cleaned-src-camera.ogg` and `delivered.mp4`, and the library has not gained one asset. After the call: three, the copy, the version and the film;
- "when no call of its caller’s came in time to save what it made, saving none of it" lets the token run out during the render. No call comes within the test's 20 ms claim: the sentence above, the library unchanged, the scratch empty;
- "refuses a project whose played asset the library no longer holds, naming it" is `resolvePlayed`'s `Refusal`, which Task 18's start answers.

Each was shown to bite in scratch, the file restored after each: taking a token before the analyses, uploading a cleaned copy as soon as it is made, or keeping the browser's film beside the delivered one, fails "computes with no credential at all…".

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 7: Commit**

Write `$W/msg-t17.txt`:

```text
feat(video-mcp): render a project in its own browser, step by step

A job makes the analyses the project still needs in the page, as the
Studio does, and records them the Studio's way; compiles in Node so a
project the Studio cannot compile fails before a browser starts;
renders with the Studio's own exportProject at the measured bitrate;
transcodes and checks. Each job has its own headless shell with
renderer-server's flags, closed however the job ends, whose every
request leaves only for the job's origin.

Aura's tokens last 15 minutes and a long film renders for more, so a
job's compute needs none: what it reads was resolved when it started,
and what it makes, the cleaned copies and the film, waits in its
scratch. Each authenticated upload request takes a current bearer,
and credential waits share one deadline after the output check.
Cancellation stops subsequent writes and aborts an active request.
A failure names the source, memory kill, check or missing credential,
and identifies any copies, versions or film already accepted; a
missing public link is recovered for that same film. The page hands failures back as plain
data so a source's asset id survives page.evaluate.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t17.txt services/video-mcp/package.json services/video-mcp/package-lock.json services/video-mcp/tsconfig.json services/video-mcp/src/studio.ts services/video-mcp/src/jobs/limits.ts services/video-mcp/page/render.ts services/video-mcp/types/url-imports.d.ts services/video-mcp/src/jobs/inPage.ts services/video-mcp/src/jobs/browser.ts services/video-mcp/src/jobs/analyses.ts services/video-mcp/src/jobs/played.ts services/video-mcp/src/jobs/job.ts services/video-mcp/test/unit/analyses.test.ts services/video-mcp/test/unit/job.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 18: The three render tools, the film link, and their published schema

**Review corrections R2–R4.** Update `render-tools.json`, live output schemas and status
tests together with Task 14's accepted-asset record. Failure/cancellation text must report
that record without exposing signed source URLs or another identity's assets. A cancelled
state alone does not prove writes stopped. Start resolves only Task 17's actual playback
and analysis dependencies, not every source retained in a project file. The original
schemas and tests below are baseline material until these requirements are implemented.

`video_render_start {assetId, quality}`, `video_render_status {jobId}` and `video_render_cancel {jobId}` (spec §Tools) are ordinary MCP tools: Aura's model calls them as any client does (spec §"Amended 2026-10-02"). Each answers a `JobStatus` as structured content, plus a sentence for a reader (`statusSentence`):
- "job J is waiting, number 2 in the queue";
- "job J is running: render, 41% done";
- "job J finished: 29 s, 1080×1920, 25.8 MB, asset A; download until T: URL";
- "job J failed: beach.mp4: the object store answered 404";
- "job J was cancelled".

An id this identity does not own answers `state: "unknown"`, exactly as one that never existed: "there is no job J here: it finished more than two hours ago, it is not yours, or the renderer restarted".

**The film reaches a chat as a link** (the amendment's point 3). Aura's bridge carries a file into a chat only when a tool result holds it inline or links it, at most 25 MiB per file and 50 MiB per call (`internal/mcp/file.go:20-22`). It reads a link with `resources/read` on the same session, as the identity that asked, and never fetches the URI itself (`internal/agent/mcptools/bridge_links.go`).
- A finished status whose film is at most 25 MiB adds a `resource_link`: `aura-video://film/<assetId>`, named by the film's file name, `video/mp4`, with its size. The model hands the file the bridge wrote to `send_file`, on the web and on Telegram.
- A larger film is not linked, and the sentence ends "; at 26.2 MB it is over the 25 MiB a chat can carry: open it from the library or the Studio".
- `filmResource.ts` serves `aura-video://film/{assetId}` for `resources/read`. It resolves the asset through the internal API under the reader's own bearer, so a reader gets only their own films. It refuses anything that is not a `video/mp4` the reader owns ("film X is not in your library") or is over 25 MiB, before fetching a byte. It answers the bytes as a base64 `blob`.

**A start reads everything its job will read** (Q1). With the start call's own token, it loads the project and resolves every asset the project plays (`resolvePlayed`, Task 17), and queues the job with both. The job then needs a token only to write.

A start the renderer cannot honour creates no job, and answers why as an error result:
- a project the caller cannot open;
- no clip;
- under 3 s or over `FILM_MAX_SECONDS`;
- a frame rate outside 23–60;
- a played asset the library no longer holds: "video source <source id> (asset <asset id>) is no longer in your library". A source deleted after the start fails the job instead, by the object store's answer (Task 16).

**The descriptions tell a model what to do with a background job**, Aura's included, in as few words as the project tools use:
- `video_render_start`: "Render a Video Studio project to an Instagram-ready MP4, 1080p or 720p. The render runs in the background: this answers a job id at once, so tell the user it has started. video_render_status delivers the film later; do not call it repeatedly within one turn." A model that loops on status inside its turn would hold the turn for the whole render, one 60 s MCP call at a time.
- `video_render_status` says what a finished job carries: the asset id and the download link, the film itself up to 25 MiB "to send to the user", and a larger one "in the library and the Studio".

**The two writing tools are closed-world**, as the project tools are (`LIBRARY_WRITE`, Task 9): start is `LIBRARY_WRITE`, and cancel is `LIBRARY_WRITE` with `idempotentHint: true`. Without the hint, Aura's bridge would grade `video_render_start` destructive and stop every render for the operator's approval. The spec wants no gate (§Security, "Gateway").

`contract/render-tools.json` is the three tools as `tools/list` advertises them: for each name, its description, its annotations and its input and output JSON Schemas. Plan C and any client are written against it. The unit tier compares the live listing with this file (`toMatchFileSnapshot`). Vitest 5's snapshot mode is `none` under `$CI` and `new` elsewhere (`updateSnapshot` in its runner), so it never overwrites a file that differs, and a schema change that does not update the file fails, locally and in CI.

**Files:**
- Create: `services/video-mcp/src/tools/filmResource.ts`, `services/video-mcp/src/tools/renderTools.ts`, `services/video-mcp/contract/render-tools.json`
- Test: `services/video-mcp/test/unit/renderTools.test.ts`

**Interfaces:**
- Consumes:
  - `JobQueue`, `JobView`, `STEPS` (Task 14); `FILM_MAX_SECONDS`, `FPS_MIN`, `FPS_MAX`, `Quality` (Tasks 11, 13);
  - `InternalApi` (Task 3); `ProjectStore`, `LIBRARY_WRITE` (Task 9); `Refusal`, `projectDuration` (Task 5); `callerOf` (Task 2); `resolvePlayed` (Task 17);
  - `startSidecar`, `mcpClient`, `stubProbe` (Task 7's `test/support/sidecar.ts`); the fake's `objectFailures` (Task 3).
- Produces:
  - `filmResource.ts`:
    - `CHAT_FILE_MAX_BYTES = 25 << 20`, `filmUri(assetId)`, `megabytes(bytes)` and `OVER_CHAT_CAP`;
    - `registerFilmResource(server: McpServer, api: InternalApi, fetchImpl?: typeof fetch): void`;
  - `renderTools.ts`:
    - `type JobStatus`: `JobView` plus `state: 'unknown'`, with nullable `projectAssetId`, `quality` and `createdAt`; its `result` carries `fileName`;
    - `interface RenderToolParts { queue: JobQueue; store: ProjectStore; api: InternalApi }`;
    - `registerRenderTools(server: McpServer, parts: RenderToolParts): void`;
    - `renderRefusal(project: VideoProject): string | undefined`;
    - `statusSentence(status: JobStatus): string`.

- [ ] **Step 1: Write the failing test**

`services/video-mcp/test/unit/renderTools.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { internalApi } from '../../src/internalApi';
import { JobQueue, type Delivery, type JobRequest, type RunControls } from '../../src/jobs/queue';
import { projectStore } from '../../src/projects';
import { registerProjectTools } from '../../src/tools/projectTools';
import { CHAT_FILE_MAX_BYTES, registerFilmResource } from '../../src/tools/filmResource';
import { registerRenderTools, renderRefusal, statusSentence, type JobStatus } from '../../src/tools/renderTools';
import { mcpClient, startSidecar, stubProbe, type Running } from '../support/sidecar';
import { sampleProject } from '../support/projects';
import { IDENTITY, OTHER_IDENTITY } from '../support/tokens';

const PROBES = { 'beach.mp4': { kind: 'video', duration: 12, width: 1080, height: 1920, hasAudio: true } } as const;
const film: Delivery = {
  filmAssetId: '6e5d4c3b-2a19-4807-b6a5-948372615041',
  fileName: 'Aura-memory.mp4',
  projectAssetId: '8c7b6a59-4837-4261-9504-a3b2c1d0e9f8',
  bytes: 24_876_543,
  seconds: 12,
  width: 1080,
  height: 1920,
  downloadUrl: 'http://localhost:3900/b/k.mp4?X-Amz-Signature=s',
  downloadExpiresAt: '2026-10-02T14:00:00.000Z',
};

let sidecar: Running;
let jobs: { request: JobRequest; controls: RunControls; finish: (result: Delivery) => void }[];
let beach: string;
let project: string;

beforeEach(async () => {
  jobs = [];
  sidecar = await startSidecar((fake) => {
    const api = internalApi(fake.url);
    const store = projectStore(api);
    const queue = new JobQueue(
      (request, controls) =>
        new Promise<Delivery>((resolve, reject) => {
          controls.signal.addEventListener('abort', () => reject(controls.signal.reason as Error));
          jobs.push({ request, controls, finish: resolve });
        }),
    );
    return (server) => {
      registerProjectTools(server, { api, store, probe: stubProbe(PROBES) });
      registerRenderTools(server, { queue, store, api });
      registerFilmResource(server, api);
    };
  });
  beach = sidecar.api.seed(IDENTITY, { fileName: 'beach.mp4', mimeType: 'video/mp4', bytes: Buffer.from('mp4') }).id;
  const created = await sidecar.call('video_project_create', { name: 'Aura memory', format: '9:16', clips: [{ assetId: beach }] });
  project = /asset ([0-9a-f-]{36})/.exec(created.text)?.[1] ?? '';
});

afterEach(async () => {
  expect(sidecar.api.violations).toEqual([]);
  await sidecar.close();
});

describe('video_render_start', () => {
  it('queues the caller’s project with everything its job will read, and answers the job as structured content', async () => {
    const started = await sidecar.call('video_render_start', { assetId: project, quality: '1080p' });
    expect(started.isError).toBe(false);
    expect(started.structured).toMatchObject({ state: 'running', projectAssetId: project, quality: '1080p', progress: 0, result: null });
    const request = jobs[0]?.request;
    expect(request).toMatchObject({ identity: IDENTITY, projectAssetId: project, quality: '1080p' });
    expect(request?.project.sources.map((source) => source.assetId)).toEqual([beach]);
    expect(request?.sources.get(beach)).toMatchObject({ fileName: 'beach.mp4', url: expect.stringContaining('X-Amz-Signature=') as unknown });
  });

  it('creates no job for a project the caller cannot open, and says why', async () => {
    const theirs = await sidecar.call('video_render_start', { assetId: project, quality: '720p' }, OTHER_IDENTITY);
    expect(theirs).toMatchObject({ isError: true, text: `asset ${project} is not in your library, or its upload has not finished` });
    expect(jobs).toHaveLength(0);
  });

  it('creates no job for a project whose source the library no longer holds, and names it', async () => {
    const gone = sidecar.api.assets.get(beach);
    if (gone !== undefined) gone.status = 'deleted';
    const refused = await sidecar.call('video_render_start', { assetId: project, quality: '1080p' });
    expect(refused).toMatchObject({ isError: true, text: expect.stringMatching(new RegExp(`^video source [0-9a-f-]{36} \\(asset ${beach}\\) is no longer in your library$`)) as unknown });
    expect(jobs).toHaveLength(0);
  });
});

describe('a project the renderer refuses', () => {
  it.each([
    ['with no clip', { video: [] }, 'the project has no clip to render'],
    ['under 3 s', { video: [{ id: 'c1', sourceId: 'src-camera', duration: 2.9, sourceStart: 0, muted: false }] }, 'the film lasts 2.9 s; Instagram takes 3 s or more'],
    ['over 10 minutes', { sources: [{ ...sampleProject().sources[0], duration: 700 }], video: [{ id: 'c1', sourceId: 'src-camera', duration: 600.5, sourceStart: 0, muted: false }] }, 'the film lasts 600.5 s; this renderer takes films up to 10 minutes'],
    ['at 20 fps', { fps: 20 }, 'the project runs at 20 fps; Instagram takes 23–60'],
    ['at 61 fps', { fps: 61 }, 'the project runs at 61 fps; Instagram takes 23–60'],
  ])('%s', (_name, change, sentence) => {
    expect(renderRefusal({ ...sampleProject(), ...change } as ReturnType<typeof sampleProject>)).toBe(sentence);
  });

  it('is none at the edges: 3 s, 10 minutes, 23 and 60 fps', () => {
    const at = (duration: number, fps: number) => ({
      ...sampleProject(),
      fps,
      sources: [{ ...sampleProject().sources[0], duration: 700 }],
      video: [{ id: 'c1', sourceId: 'src-camera', duration, sourceStart: 0, muted: false }],
    }) as ReturnType<typeof sampleProject>;
    expect([renderRefusal(at(3, 23)), renderRefusal(at(600, 60))]).toEqual([undefined, undefined]);
  });
});

describe('video_render_status and _cancel', () => {
  it('follow the caller’s own job to its film', async () => {
    const { jobId } = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
    jobs[0]?.controls.progress('render', 0.5);
    const running = await sidecar.call('video_render_status', { jobId });
    expect(running.text).toBe(`job ${jobId} is running: render, 47% done`);
    jobs[0]?.finish(film);
    await new Promise((resolve) => setTimeout(resolve, 10));
    const done = await sidecar.call('video_render_status', { jobId });
    expect(done.structured).toMatchObject({ state: 'succeeded', result: film });
    expect(done.text).toBe(`job ${jobId} finished: 12 s, 1080×1920, 24.9 MB, asset ${film.filmAssetId}; download until ${film.downloadExpiresAt}: ${film.downloadUrl}`);
  });

  it('link a finished film a chat can carry, which only its owner can read back', async () => {
    const reel = sidecar.api.seed(IDENTITY, { fileName: 'Aura-memory.mp4', mimeType: 'video/mp4', bytes: Buffer.from('the film') }).id;
    const { jobId } = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
    jobs[0]?.finish({ ...film, filmAssetId: reel, bytes: 8 });
    await new Promise((resolve) => setTimeout(resolve, 10));
    const mine = await mcpClient(sidecar.base, await sidecar.bearer(IDENTITY));
    const theirs = await mcpClient(sidecar.base, await sidecar.bearer(OTHER_IDENTITY));
    try {
      const status = await mine.callTool({ name: 'video_render_status', arguments: { jobId } });
      const link = { type: 'resource_link', uri: `aura-video://film/${reel}`, name: 'Aura-memory.mp4', mimeType: 'video/mp4', size: 8 };
      expect(status.content).toContainEqual(link);
      const read = await mine.readResource({ uri: link.uri });
      expect(read.contents).toEqual([{ uri: link.uri, mimeType: 'video/mp4', blob: Buffer.from('the film').toString('base64') }]);
      await expect(theirs.readResource({ uri: link.uri })).rejects.toThrow(`film ${reel} is not in your library`);
      sidecar.api.objectFailures.set(reel, 503);
      await expect(mine.readResource({ uri: link.uri })).rejects.toThrow(`film ${reel} could not be read: the object store answered 503`);
    } finally {
      await mine.close();
      await theirs.close();
    }
  });

  it('link a film of exactly 25 MiB, the most Aura’s bridge carries, and read it back whole', async () => {
    const edge = sidecar.api.seed(IDENTITY, { fileName: 'edge.mp4', mimeType: 'video/mp4', bytes: Buffer.alloc(CHAT_FILE_MAX_BYTES, 1) }).id;
    const { jobId } = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
    jobs[0]?.finish({ ...film, filmAssetId: edge, bytes: CHAT_FILE_MAX_BYTES });
    await new Promise((resolve) => setTimeout(resolve, 10));
    const client = await mcpClient(sidecar.base, await sidecar.bearer(IDENTITY));
    try {
      const status = await client.callTool({ name: 'video_render_status', arguments: { jobId } });
      expect(status.content).toContainEqual(expect.objectContaining({ type: 'resource_link', uri: `aura-video://film/${edge}`, size: CHAT_FILE_MAX_BYTES }));
      const read = await client.readResource({ uri: `aura-video://film/${edge}` });
      expect(Buffer.from((read.contents[0] as { blob: string }).blob, 'base64').length).toBe(CHAT_FILE_MAX_BYTES);
    } finally {
      await client.close();
    }
  });

  it('send a film one byte over 25 MiB to the library and the Studio, and will not read it back', async () => {
    const big = sidecar.api.seed(IDENTITY, { fileName: 'long.mp4', mimeType: 'video/mp4', bytes: Buffer.alloc(CHAT_FILE_MAX_BYTES + 1) }).id;
    const { jobId } = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
    jobs[0]?.finish({ ...film, filmAssetId: big, bytes: CHAT_FILE_MAX_BYTES + 1 });
    await new Promise((resolve) => setTimeout(resolve, 10));
    const client = await mcpClient(sidecar.base, await sidecar.bearer(IDENTITY));
    try {
      const status = await client.callTool({ name: 'video_render_status', arguments: { jobId } });
      expect(status.content).toEqual([
        { type: 'text', text: expect.stringMatching(/; at 26\.2 MB it is over the 25 MiB a chat can carry: open it from the library or the Studio$/) as unknown },
      ]);
      await expect(client.readResource({ uri: `aura-video://film/${big}` })).rejects.toThrow(
        `film ${big} is 26.2 MB, over the 25 MiB a chat can carry: open it from the library or the Studio`,
      );
    } finally {
      await client.close();
    }
  });

  it('answer another identity’s job as unknown, as they answer one that never was', async () => {
    const { jobId } = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
    const theirs = await sidecar.call('video_render_status', { jobId }, OTHER_IDENTITY);
    expect(theirs.structured).toMatchObject({ jobId, state: 'unknown', result: null });
    expect(theirs.text).toBe(`there is no job ${jobId} here: it finished more than two hours ago, it is not yours, or the renderer restarted`);
    expect((await sidecar.call('video_render_cancel', { jobId }, OTHER_IDENTITY)).structured).toMatchObject({ state: 'unknown' });
    expect(jobs[0]?.controls.signal.aborted).toBe(false);
  });

  it('cancel a waiting job before it runs and a running one as it runs', async () => {
    const first = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
    const second = (await sidecar.call('video_render_start', { assetId: project, quality: '720p' })).structured as JobStatus;
    expect((await sidecar.call('video_render_status', { jobId: second.jobId })).text).toBe(`job ${second.jobId} is waiting, number 1 in the queue`);
    expect((await sidecar.call('video_render_cancel', { jobId: second.jobId })).text).toBe(`job ${second.jobId} was cancelled`);
    await sidecar.call('video_render_cancel', { jobId: first.jobId });
    expect(jobs[0]?.controls.signal.aborted).toBe(true);
  });
});

it('says a failed job’s reason', () => {
  const failed: JobStatus = { jobId: 'j1', state: 'failed', queuePosition: null, progress: 0.4, step: 'render', projectAssetId: null, quality: '1080p', createdAt: null, finishedAt: null, result: null, error: 'beach.mp4: the object store answered 404' };
  expect(statusSentence(failed)).toBe('job j1 failed: beach.mp4: the object store answered 404');
});

it('advertises the render tools exactly as contract/render-tools.json records them for Aura', async () => {
  const client = await mcpClient(sidecar.base, await sidecar.bearer(IDENTITY));
  try {
    const tools = (await client.listTools()).tools.filter((tool) => tool.name.startsWith('video_render_'));
    const contract = Object.fromEntries(
      tools.map((tool) => [tool.name, { description: tool.description, annotations: tool.annotations, inputSchema: tool.inputSchema, outputSchema: tool.outputSchema }]),
    );
    await expect(`${JSON.stringify(contract, null, 2)}\n`).toMatchFileSnapshot('../../contract/render-tools.json');
  } finally {
    await client.close();
  }
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/renderTools.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../src/tools/filmResource' imported from …` (the first missing import in the file's order); `VITEST_RC=1`.

- [ ] **Step 3: Write the film resource, the tools and their schema file**

`services/video-mcp/src/tools/filmResource.ts`:

```ts
// filmResource.ts — a finished film read back over MCP (spec §"Amended 2026-10-02" 3). A finished
// status links its film as aura-video://film/<assetId>; Aura's bridge reads the link with
// resources/read on its own session, as the identity that asked, and writes the bytes into the
// turn's workspace for send_file (internal/agent/mcptools/bridge_links.go). The film is resolved
// through the internal API under the reader's own bearer, so a reader gets only their own films,
// and only up to the bridge's file cap; a larger film is opened from the library or the Studio.

import { ResourceTemplate, type McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { callerOf } from '../auth';
import type { InternalApi } from '../internalApi';

/** What Aura's bridge carries into a chat per file (internal/mcp/file.go MaxFileBytes). */
export const CHAT_FILE_MAX_BYTES = 25 << 20;

export const filmUri = (assetId: string): string => `aura-video://film/${assetId}`;

export const megabytes = (bytes: number): string => `${String(Math.round(bytes / 1e5) / 10)} MB`;

export const OVER_CHAT_CAP = 'over the 25 MiB a chat can carry: open it from the library or the Studio';

export function registerFilmResource(server: McpServer, api: InternalApi, fetchImpl: typeof fetch = fetch): void {
  server.registerResource(
    'film',
    new ResourceTemplate('aura-video://film/{assetId}', { list: undefined }),
    { mimeType: 'video/mp4', description: 'A film a render delivered, read back to carry it into a chat: 25 MiB at most.' },
    async (uri, { assetId }, extra) => {
      const id = String(assetId);
      const film = (await api.resolve(callerOf(extra.authInfo).token, [id])).found.get(id);
      if (film === undefined || film.mimeType !== 'video/mp4') throw new Error(`film ${id} is not in your library`);
      if (film.sizeBytes > CHAT_FILE_MAX_BYTES) throw new Error(`film ${id} is ${megabytes(film.sizeBytes)}, ${OVER_CHAT_CAP}`);
      const response = await fetchImpl(film.url);
      if (!response.ok) throw new Error(`film ${id} could not be read: the object store answered ${String(response.status)}`);
      const blob = Buffer.from(await response.arrayBuffer()).toString('base64');
      return { contents: [{ uri: uri.href, mimeType: 'video/mp4', blob }] };
    },
  );
}
```

`services/video-mcp/src/tools/renderTools.ts`:

```ts
// renderTools.ts — video_render_start, _status and _cancel (spec §Tools), ordinary MCP tools that
// Aura's model calls as any client does (spec §"Amended 2026-10-02"). Each answers a JobStatus as
// structured content (contract/render-tools.json, checked against JOB_STATUS by the unit tier) and
// a sentence for a reader; a finished film a chat can carry is linked too (filmResource.ts). A
// start reads, with its caller's token, everything the job will read (played.ts), so the job's
// compute needs no token. A start the renderer cannot honour creates no job and answers why, as
// an error result: there is no job to report on.

import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import type { CallToolResult } from '@modelcontextprotocol/sdk/types.js';
import { z } from 'zod';
import { callerOf } from '../auth';
import type { InternalApi } from '../internalApi';
import { FILM_MAX_SECONDS, FPS_MAX, FPS_MIN, type Quality } from '../jobs/limits';
import { resolvePlayed } from '../jobs/played';
import { STEPS, type JobQueue, type JobView } from '../jobs/queue';
import type { ProjectStore } from '../projects';
import { Refusal } from '../refusal';
import { projectDuration, type VideoProject } from '../studio';
import { CHAT_FILE_MAX_BYTES, filmUri, megabytes, OVER_CHAT_CAP } from './filmResource';
import { LIBRARY_WRITE } from './projectTools';

const FILM_MIN_SECONDS = 3;

const JOB_STATUS = z.object({
  jobId: z.string(),
  state: z.enum(['queued', 'running', 'succeeded', 'failed', 'cancelled', 'unknown']),
  queuePosition: z.number().int().min(1).nullable(),
  progress: z.number().min(0).max(1),
  step: z.enum(Object.keys(STEPS) as [keyof typeof STEPS, ...(keyof typeof STEPS)[]]).nullable(),
  projectAssetId: z.string().nullable(),
  quality: z.enum(['1080p', '720p']).nullable(),
  createdAt: z.string().nullable(),
  finishedAt: z.string().nullable(),
  result: z
    .object({
      filmAssetId: z.string(),
      fileName: z.string(),
      projectAssetId: z.string(),
      bytes: z.number().int(),
      seconds: z.number(),
      width: z.number().int(),
      height: z.number().int(),
      downloadUrl: z.string(),
      downloadExpiresAt: z.string(),
    })
    .nullable(),
  error: z.string().nullable(),
});
export type JobStatus = z.infer<typeof JOB_STATUS>;

export interface RenderToolParts {
  readonly queue: JobQueue;
  readonly store: ProjectStore;
  readonly api: InternalApi;
}

function unknownJob(jobId: string): JobStatus {
  return {
    jobId,
    state: 'unknown',
    queuePosition: null,
    progress: 0,
    step: null,
    projectAssetId: null,
    quality: null,
    createdAt: null,
    finishedAt: null,
    result: null,
    error: null,
  };
}

const inSeconds = (seconds: number): string => `${String(Math.round(seconds * 10) / 10)} s`;

export function statusSentence(status: JobStatus): string {
  switch (status.state) {
    case 'queued':
      return `job ${status.jobId} is waiting, number ${String(status.queuePosition)} in the queue`;
    case 'running':
      return `job ${status.jobId} is running: ${String(status.step)}, ${String(Math.round(status.progress * 100))}% done`;
    case 'succeeded': {
      const result = status.result;
      if (result === null) return `job ${status.jobId} finished`;
      const finished = `job ${status.jobId} finished: ${inSeconds(result.seconds)}, ${String(result.width)}×${String(result.height)}, ${megabytes(result.bytes)}, asset ${result.filmAssetId}; download until ${result.downloadExpiresAt}: ${result.downloadUrl}`;
      return result.bytes > CHAT_FILE_MAX_BYTES ? `${finished}; at ${megabytes(result.bytes)} it is ${OVER_CHAT_CAP}` : finished;
    }
    case 'failed':
      return `job ${status.jobId} failed: ${String(status.error)}`;
    case 'cancelled':
      return `job ${status.jobId} was cancelled`;
    case 'unknown':
      return `there is no job ${status.jobId} here: it finished more than two hours ago, it is not yours, or the renderer restarted`;
  }
}

/** The status as structured content and a sentence; a finished film a chat can carry is linked, so
 *  Aura's bridge reads it into the turn for send_file (spec §"Amended 2026-10-02" 3). */
function answered(status: JobStatus): CallToolResult {
  const content: CallToolResult['content'] = [{ type: 'text', text: statusSentence(status) }];
  const film = status.result;
  if (film !== null && film.bytes <= CHAT_FILE_MAX_BYTES) {
    content.push({ type: 'resource_link', uri: filmUri(film.filmAssetId), name: film.fileName, mimeType: 'video/mp4', size: film.bytes });
  }
  return { content, structuredContent: status };
}

/** Why this project cannot be rendered, or nothing when it can. */
export function renderRefusal(project: VideoProject): string | undefined {
  if (project.video.length === 0) return 'the project has no clip to render';
  const seconds = projectDuration(project);
  if (seconds < FILM_MIN_SECONDS) return `the film lasts ${inSeconds(seconds)}; Instagram takes 3 s or more`;
  if (seconds > FILM_MAX_SECONDS) {
    return `the film lasts ${inSeconds(seconds)}; this renderer takes films up to ${String(FILM_MAX_SECONDS / 60)} minutes`;
  }
  if (project.fps < FPS_MIN || project.fps > FPS_MAX) return `the project runs at ${String(project.fps)} fps; Instagram takes 23–60`;
  return undefined;
}

const refused = (sentence: string): CallToolResult => ({ content: [{ type: 'text', text: sentence }], isError: true });

export function registerRenderTools(server: McpServer, parts: RenderToolParts): void {
  const { queue, store, api } = parts;
  const view = (status: JobView): JobStatus => status;

  server.registerTool(
    'video_render_start',
    {
      description:
        'Render a Video Studio project to an Instagram-ready MP4, 1080p or 720p. The render runs in the background: this answers a job id at once, so tell the user it has started. video_render_status delivers the film later; do not call it repeatedly within one turn.',
      inputSchema: { assetId: z.uuid(), quality: z.enum(['1080p', '720p']) },
      outputSchema: JOB_STATUS.shape,
      annotations: LIBRARY_WRITE,
    },
    async (args, extra) => {
      const caller = callerOf(extra.authInfo);
      try {
        const project = await store.load(caller.token, args.assetId);
        const refusal = renderRefusal(project);
        if (refusal !== undefined) return refused(refusal);
        const sources = await resolvePlayed(api, caller.token, project);
        return answered(view(queue.submit({ identity: caller.identity, projectAssetId: args.assetId, quality: args.quality as Quality, project, sources })));
      } catch (error) {
        if (error instanceof Refusal) return refused(error.message);
        throw error;
      }
    },
  );

  server.registerTool(
    'video_render_status',
    {
      description:
        'Where a render job stands: queued, running, failed with its reason, or finished with the film\'s asset id and a download link valid two hours. A finished film up to 25 MiB comes with the result, to send to the user; a larger one is in the library and the Studio.',
      inputSchema: { jobId: z.string().min(1).max(100) },
      outputSchema: JOB_STATUS.shape,
      annotations: { readOnlyHint: true },
    },
    async (args, extra) => answered(queue.view(callerOf(extra.authInfo).identity, args.jobId) ?? unknownJob(args.jobId)),
  );

  server.registerTool(
    'video_render_cancel',
    {
      description: 'Stop a queued or running render job. A finished job is answered as it finished.',
      inputSchema: { jobId: z.string().min(1).max(100) },
      outputSchema: JOB_STATUS.shape,
      annotations: { ...LIBRARY_WRITE, idempotentHint: true },
    },
    async (args, extra) => answered(queue.cancel(callerOf(extra.authInfo).identity, args.jobId) ?? unknownJob(args.jobId)),
  );
}
```

`services/video-mcp/contract/render-tools.json`:

```json
{
  "video_render_start": {
    "description": "Render a Video Studio project to an Instagram-ready MP4, 1080p or 720p. The render runs in the background: this answers a job id at once, so tell the user it has started. video_render_status delivers the film later; do not call it repeatedly within one turn.",
    "annotations": {
      "destructiveHint": false,
      "idempotentHint": false,
      "openWorldHint": false
    },
    "inputSchema": {
      "type": "object",
      "properties": {
        "assetId": {
          "type": "string",
          "format": "uuid",
          "pattern": "^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$"
        },
        "quality": {
          "type": "string",
          "enum": [
            "1080p",
            "720p"
          ]
        }
      },
      "required": [
        "assetId",
        "quality"
      ],
      "$schema": "http://json-schema.org/draft-07/schema#"
    },
    "outputSchema": {
      "type": "object",
      "properties": {
        "jobId": {
          "type": "string"
        },
        "state": {
          "type": "string",
          "enum": [
            "queued",
            "running",
            "succeeded",
            "failed",
            "cancelled",
            "unknown"
          ]
        },
        "queuePosition": {
          "anyOf": [
            {
              "type": "integer",
              "minimum": 1,
              "maximum": 9007199254740991
            },
            {
              "type": "null"
            }
          ]
        },
        "progress": {
          "type": "number",
          "minimum": 0,
          "maximum": 1
        },
        "step": {
          "anyOf": [
            {
              "type": "string",
              "enum": [
                "analyses",
                "compile",
                "render",
                "transcode",
                "check",
                "upload"
              ]
            },
            {
              "type": "null"
            }
          ]
        },
        "projectAssetId": {
          "type": [
            "string",
            "null"
          ]
        },
        "quality": {
          "anyOf": [
            {
              "type": "string",
              "enum": [
                "1080p",
                "720p"
              ]
            },
            {
              "type": "null"
            }
          ]
        },
        "createdAt": {
          "type": [
            "string",
            "null"
          ]
        },
        "finishedAt": {
          "type": [
            "string",
            "null"
          ]
        },
        "result": {
          "anyOf": [
            {
              "type": "object",
              "properties": {
                "filmAssetId": {
                  "type": "string"
                },
                "fileName": {
                  "type": "string"
                },
                "projectAssetId": {
                  "type": "string"
                },
                "bytes": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "seconds": {
                  "type": "number"
                },
                "width": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "height": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "downloadUrl": {
                  "type": "string"
                },
                "downloadExpiresAt": {
                  "type": "string"
                }
              },
              "required": [
                "filmAssetId",
                "fileName",
                "projectAssetId",
                "bytes",
                "seconds",
                "width",
                "height",
                "downloadUrl",
                "downloadExpiresAt"
              ],
              "additionalProperties": false
            },
            {
              "type": "null"
            }
          ]
        },
        "error": {
          "type": [
            "string",
            "null"
          ]
        }
      },
      "required": [
        "jobId",
        "state",
        "queuePosition",
        "progress",
        "step",
        "projectAssetId",
        "quality",
        "createdAt",
        "finishedAt",
        "result",
        "error"
      ],
      "$schema": "http://json-schema.org/draft-07/schema#",
      "additionalProperties": false
    }
  },
  "video_render_status": {
    "description": "Where a render job stands: queued, running, failed with its reason, or finished with the film's asset id and a download link valid two hours. A finished film up to 25 MiB comes with the result, to send to the user; a larger one is in the library and the Studio.",
    "annotations": {
      "readOnlyHint": true
    },
    "inputSchema": {
      "type": "object",
      "properties": {
        "jobId": {
          "type": "string",
          "minLength": 1,
          "maxLength": 100
        }
      },
      "required": [
        "jobId"
      ],
      "$schema": "http://json-schema.org/draft-07/schema#"
    },
    "outputSchema": {
      "type": "object",
      "properties": {
        "jobId": {
          "type": "string"
        },
        "state": {
          "type": "string",
          "enum": [
            "queued",
            "running",
            "succeeded",
            "failed",
            "cancelled",
            "unknown"
          ]
        },
        "queuePosition": {
          "anyOf": [
            {
              "type": "integer",
              "minimum": 1,
              "maximum": 9007199254740991
            },
            {
              "type": "null"
            }
          ]
        },
        "progress": {
          "type": "number",
          "minimum": 0,
          "maximum": 1
        },
        "step": {
          "anyOf": [
            {
              "type": "string",
              "enum": [
                "analyses",
                "compile",
                "render",
                "transcode",
                "check",
                "upload"
              ]
            },
            {
              "type": "null"
            }
          ]
        },
        "projectAssetId": {
          "type": [
            "string",
            "null"
          ]
        },
        "quality": {
          "anyOf": [
            {
              "type": "string",
              "enum": [
                "1080p",
                "720p"
              ]
            },
            {
              "type": "null"
            }
          ]
        },
        "createdAt": {
          "type": [
            "string",
            "null"
          ]
        },
        "finishedAt": {
          "type": [
            "string",
            "null"
          ]
        },
        "result": {
          "anyOf": [
            {
              "type": "object",
              "properties": {
                "filmAssetId": {
                  "type": "string"
                },
                "fileName": {
                  "type": "string"
                },
                "projectAssetId": {
                  "type": "string"
                },
                "bytes": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "seconds": {
                  "type": "number"
                },
                "width": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "height": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "downloadUrl": {
                  "type": "string"
                },
                "downloadExpiresAt": {
                  "type": "string"
                }
              },
              "required": [
                "filmAssetId",
                "fileName",
                "projectAssetId",
                "bytes",
                "seconds",
                "width",
                "height",
                "downloadUrl",
                "downloadExpiresAt"
              ],
              "additionalProperties": false
            },
            {
              "type": "null"
            }
          ]
        },
        "error": {
          "type": [
            "string",
            "null"
          ]
        }
      },
      "required": [
        "jobId",
        "state",
        "queuePosition",
        "progress",
        "step",
        "projectAssetId",
        "quality",
        "createdAt",
        "finishedAt",
        "result",
        "error"
      ],
      "$schema": "http://json-schema.org/draft-07/schema#",
      "additionalProperties": false
    }
  },
  "video_render_cancel": {
    "description": "Stop a queued or running render job. A finished job is answered as it finished.",
    "annotations": {
      "destructiveHint": false,
      "idempotentHint": true,
      "openWorldHint": false
    },
    "inputSchema": {
      "type": "object",
      "properties": {
        "jobId": {
          "type": "string",
          "minLength": 1,
          "maxLength": 100
        }
      },
      "required": [
        "jobId"
      ],
      "$schema": "http://json-schema.org/draft-07/schema#"
    },
    "outputSchema": {
      "type": "object",
      "properties": {
        "jobId": {
          "type": "string"
        },
        "state": {
          "type": "string",
          "enum": [
            "queued",
            "running",
            "succeeded",
            "failed",
            "cancelled",
            "unknown"
          ]
        },
        "queuePosition": {
          "anyOf": [
            {
              "type": "integer",
              "minimum": 1,
              "maximum": 9007199254740991
            },
            {
              "type": "null"
            }
          ]
        },
        "progress": {
          "type": "number",
          "minimum": 0,
          "maximum": 1
        },
        "step": {
          "anyOf": [
            {
              "type": "string",
              "enum": [
                "analyses",
                "compile",
                "render",
                "transcode",
                "check",
                "upload"
              ]
            },
            {
              "type": "null"
            }
          ]
        },
        "projectAssetId": {
          "type": [
            "string",
            "null"
          ]
        },
        "quality": {
          "anyOf": [
            {
              "type": "string",
              "enum": [
                "1080p",
                "720p"
              ]
            },
            {
              "type": "null"
            }
          ]
        },
        "createdAt": {
          "type": [
            "string",
            "null"
          ]
        },
        "finishedAt": {
          "type": [
            "string",
            "null"
          ]
        },
        "result": {
          "anyOf": [
            {
              "type": "object",
              "properties": {
                "filmAssetId": {
                  "type": "string"
                },
                "fileName": {
                  "type": "string"
                },
                "projectAssetId": {
                  "type": "string"
                },
                "bytes": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "seconds": {
                  "type": "number"
                },
                "width": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "height": {
                  "type": "integer",
                  "minimum": -9007199254740991,
                  "maximum": 9007199254740991
                },
                "downloadUrl": {
                  "type": "string"
                },
                "downloadExpiresAt": {
                  "type": "string"
                }
              },
              "required": [
                "filmAssetId",
                "fileName",
                "projectAssetId",
                "bytes",
                "seconds",
                "width",
                "height",
                "downloadUrl",
                "downloadExpiresAt"
              ],
              "additionalProperties": false
            },
            {
              "type": "null"
            }
          ]
        },
        "error": {
          "type": [
            "string",
            "null"
          ]
        }
      },
      "required": [
        "jobId",
        "state",
        "queuePosition",
        "progress",
        "step",
        "projectAssetId",
        "quality",
        "createdAt",
        "finishedAt",
        "result",
        "error"
      ],
      "$schema": "http://json-schema.org/draft-07/schema#",
      "additionalProperties": false
    }
  }
}
```

- [ ] **Step 4: Run it to see it pass, and typecheck**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/renderTools.test.ts`

Expected: `Tests  17 passed (17)`, `VITEST_RC=0`. Among them:
- "advertises the render tools exactly as contract/render-tools.json records them for Aura": the file you wrote is the live listing, byte for byte, annotations included;
- "queues the caller’s project with everything its job will read, …" and "creates no job for a project whose source the library no longer holds, and names it": the start's reads. Replacing the start's `resolvePlayed` with an empty map fails both (measured in scratch);
- "link a finished film a chat can carry, which only its owner can read back": the link, the bytes read back, another identity refused, and a store that answers 503;
- both sides of the bridge's cap, which accepts a file of exactly 25 MiB (`internal/mcp/file_test.go:85`): "link a film of exactly 25 MiB, the most Aura’s bridge carries, and read it back whole" reads 26,214,400 bytes back through `resources/read`; "send a film one byte over 25 MiB to the library and the Studio, and will not read it back".

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 5: Commit**

Write `$W/msg-t18.txt`:

```text
feat(video-mcp): start, follow and cancel renders, and link the film

The three render tools are ordinary MCP tools that Aura's model calls
like any client. A start reads, with its caller's token, everything
the job will read, and refuses a project whose source is gone; its
description tells a model the render runs in the background, to tell
the user so, and not to poll within a turn. Each tool answers a job
status as structured content and a sentence; a job of another
identity is unknown, exactly as one that never was. A finished film a
chat can carry, 25 MiB as Aura's bridge counts it, is linked as
aura-video://film/<id>, and resources/read serves it under the
reader's own bearer, so the bridge can hand it to send_file; a larger
film is pointed at the library and the Studio. Start and cancel say
they are closed-world, so Aura asks no approval for them. The schemas
are published in contract/render-tools.json, which the unit tier
holds to the live listing.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t18.txt services/video-mcp/src/tools/filmResource.ts services/video-mcp/src/tools/renderTools.ts services/video-mcp/contract/render-tools.json services/video-mcp/test/unit/renderTools.test.ts`

Expected: `COMMIT_RC=0`.

---

### Task 19: The process, the two bundles, and the drift gate

`src/main.ts` is the process:
- it reads its settings from the environment (Task 1's `configFromEnv`);
- it wires the queue, the runner, the store, both tool sets and the film resource;
- it listens on 8097;
- a SIGTERM (Compose stopping or replacing the container) cancels every job, which closes its browser, and then closes the server.

A job a restart interrupts is gone, and its status then answers "there is no job J here: … or the renderer restarted". Nothing in Aura tracks jobs since the amendment, so that sentence is the whole of it. `main.ts` is the process entry, so it is out of the unit tier's denominator (`vitest.config.ts`). Task 20's smoke runs it in the image.

`build.ts` (run as `node build.ts`, on Node 24's type stripping) makes the two bundles:
- `dist/server.mjs`: `src/main.ts` for Node, everything inside it except Playwright, which finds its browser at run time;
- `dist/page/`: `page/render.ts` for the headless shell, plus:
  - the WASM and worklet the Studio imports with Vite's `?url`, copied beside the page;
  - the fonts it serves locally;
  - `render.html`.

Both resolve the Studio core against `web/node_modules`, the cockpit's patched install.

`drift.ts` reads both bundles' esbuild metafiles, so it judges what was really bundled, not what an import says (spec §Architecture 2). It refuses:
- any file of `web/` outside the Studio core (`web/src/videoStudio`, its two i18n resources, `web/node_modules`);
- a Studio package (VideoFlow, mediabunny, fvad, the noise suppressor) taken from anywhere but `web/node_modules`;
- an unpatched renderer-browser (its 1.5 MB `googlefonts.json` in the bundle);
- a sidecar module other than `src/studio.ts` and `page/render.ts` that reaches into `web/src`.

The CI unit job now also builds, so the drift gate runs on every change the filter sees.

**Files:**
- Modify: `services/video-mcp/package.json` and `package-lock.json` (esbuild 0.28.2, `build`, `allowScripts`)
- Create: `services/video-mcp/src/main.ts`, `services/video-mcp/drift.ts`, `services/video-mcp/build.ts`
- Modify: `.github/workflows/ci.yml` (`video-mcp-test`: its name, its step's name, `npm run build`)
- Test: `services/video-mcp/test/unit/drift.test.ts`

**Interfaces:**
- Consumes: everything the process wires: `tokenVerifier` (Task 2), `configFromEnv`, `PORT` (Task 1), `Credentials` (Task 2), `createApp` (Task 7), `internalApi` (Task 3), `renderRunner` (Task 17), `JobQueue` (Task 14), `log` (Task 2), `probeAsset` (Task 6), `projectStore`, `registerProjectTools` (Task 9), `registerRenderTools`, `registerFilmResource` (Task 18).
- Produces:
  - `drift.ts`: `interface Metafile { inputs }`, `interface Places { here; web; cwd }`, `inputFile(input: string, cwd: string): string` and `driftProblems(bundles: Record<string, Metafile>, places: Places): string[]`;
  - `npm run build`, which writes `dist/server.mjs` and `dist/page/{render.html, render.js, assets/, fonts/}` and prints `built …/dist: server N inputs, page M inputs, no drift`, or exits 1 listing every problem;
  - the process, which logs `aura-video-mcp is listening` with `{port, issuer}`.

- [ ] **Step 1: Add esbuild and the build script**

Replace `services/video-mcp/package.json` with:

```json
{
  "name": "aura-video-mcp",
  "private": true,
  "version": "0.0.0",
  "description": "Aura's video sidecar: Studio projects edited and rendered over MCP",
  "type": "module",
  "engines": {
    "node": ">=24.16.0 <25"
  },
  "scripts": {
    "build": "node build.ts",
    "typecheck": "tsc --noEmit -p tsconfig.json",
    "test": "vitest run --coverage"
  },
  "dependencies": {
    "@modelcontextprotocol/sdk": "1.31.0",
    "jose": "6.2.12",
    "playwright": "1.63.0",
    "zod": "4.6.5"
  },
  "devDependencies": {
    "@types/express": "5.0.6",
    "@types/node": "24.19.1",
    "@vitest/coverage-v8": "5.0.3",
    "ajv": "8.20.0",
    "esbuild": "0.28.2",
    "typescript": "7.0.2",
    "vitest": "5.0.3"
  },
  "allowScripts": {
    "esbuild@0.28.2": true
  }
}
```

`allowScripts` lets esbuild's own install script, and nothing else, run.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc-npm.sh install`

Expected: `NPM_RC=0`.

- [ ] **Step 2: Write the failing test**

`services/video-mcp/test/unit/drift.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { driftProblems, inputFile, type Metafile } from '../../drift';

const places = { here: '/repo/services/video-mcp', web: '/repo/web', cwd: '/repo/services/video-mcp' };
const bundle = (inputs: Record<string, string[]>): Metafile => ({
  inputs: Object.fromEntries(Object.entries(inputs).map(([input, imports]) => [input, { imports: imports.map((path) => ({ path })) }])),
});

describe('the drift check', () => {
  it('passes a bundle that takes the Studio from the cockpit through its two doors', () => {
    const server = bundle({
      'src/main.ts': ['src/studio.ts'],
      'src/studio.ts': ['../../web/src/videoStudio/commands.ts'],
      '../../web/src/videoStudio/commands.ts': [],
      '../../web/src/i18n/resources.videoStudio.ts': [],
      '../../web/node_modules/@videoflow/core/dist/index.js': [],
      '../../web/node_modules/@videoflow/renderer-browser/dist/googleFontLoader.js': [],
      'node_modules/zod/index.js': [],
    });
    const page = bundle({
      'page/render.ts': ['../../web/src/videoStudio/videoflow.ts'],
      'url-import:/repo/web/node_modules/@echogarden/fvad-wasm/fvad.wasm': [],
      '(disabled):../../web/node_modules/mediabunny/dist/modules/src/node.js': [],
    });
    expect(driftProblems({ server, page }, places)).toEqual([]);
  });

  it('refuses a cockpit file outside the Studio core, such as the chat it once pulled in', () => {
    const server = bundle({ '../../web/src/chat/attachments/api.ts': [] });
    expect(driftProblems({ server }, places)).toEqual(['server: web/src/chat/attachments/api.ts is in the bundle, and it is not the Studio core']);
  });

  it('refuses a second copy of a Studio package', () => {
    const server = bundle({ 'node_modules/@videoflow/core/dist/index.js': [] });
    expect(driftProblems({ server }, places)).toEqual([
      "server: @videoflow/core comes from services/video-mcp/node_modules/@videoflow/core/dist/index.js, not from the cockpit's web/node_modules",
    ]);
  });

  it('refuses an unpatched renderer-browser', () => {
    const page = bundle({ '../../web/node_modules/@videoflow/renderer-browser/dist/googlefonts.json': [] });
    expect(driftProblems({ page }, places)).toEqual(['page: renderer-browser is unpatched (googlefonts.json is bundled); run npm ci in web/']);
  });

  it('refuses a sidecar module that reaches into web/src past the doors', () => {
    const server = bundle({ 'src/summary.ts': ['../../web/src/videoStudio/project.ts'] });
    expect(driftProblems({ server }, places)).toEqual([
      'server: services/video-mcp/src/summary.ts imports web/src/videoStudio/project.ts; only src/studio.ts and page/render.ts may reach web/src',
    ]);
  });

  it('reads a namespaced input as the file it names', () => {
    expect(inputFile('url-import:/repo/web/x.wasm', places.cwd)).toBe('/repo/web/x.wasm');
    expect(inputFile('(disabled):../../web/y.js', places.cwd)).toBe('/repo/web/y.js');
    expect(inputFile('src/main.ts', places.cwd)).toBe('/repo/services/video-mcp/src/main.ts');
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/drift.test.ts`

Expected: `FAIL` with `Error: Cannot find module '../../drift' imported from …/test/unit/drift.test.ts`; `VITEST_RC=1`.

- [ ] **Step 3: Write the drift check**

`services/video-mcp/drift.ts`:

```ts
// drift.ts — the drift test (spec §Architecture 2): the sidecar takes the Studio core from the
// cockpit's own files, never from a copy, and with the cockpit's patched renderer. It reads the two
// bundles' esbuild metafiles, so it judges what was really bundled, not what an import says.

import { resolve, sep } from 'node:path';

export interface Metafile {
  readonly inputs: Readonly<Record<string, { readonly imports: readonly { readonly path: string }[] }>>;
}

export interface Places {
  /** services/video-mcp, absolute. */
  readonly here: string;
  /** web/, absolute. */
  readonly web: string;
  /** Where esbuild ran: metafile paths are relative to it. */
  readonly cwd: string;
}

/** The packages the Studio core is built from; each must be the cockpit's installed copy. */
const STUDIO_PACKAGES = [
  '@videoflow/core',
  '@videoflow/renderer-browser',
  '@videoflow/renderer-dom',
  'mediabunny',
  '@echogarden/fvad-wasm',
  '@sapphi-red/web-noise-suppressor',
];
/** What only an UNPATCHED renderer-browser bundles (web/patches/@videoflow+renderer-browser+1.3.4.patch
 *  replaces this 1.5 MB font list with the two families the Studio ships). */
const UNPATCHED_MARKER = `${sep}@videoflow${sep}renderer-browser${sep}dist${sep}googlefonts.json`;

/** The file a metafile input names. esbuild prefixes a plugin's input with its namespace
 *  (`url-import:/abs/path`) and a module a package's `browser` field turns off with
 *  `(disabled):`; the file is what follows. */
export function inputFile(input: string, cwd: string): string {
  return resolve(cwd, input.replace(/^(\(disabled\)|[\w-]+):(?=[./])/, ''));
}

export function driftProblems(bundles: Readonly<Record<string, Metafile>>, places: Places): string[] {
  const web = (...parts: string[]): string => resolve(places.web, ...parts);
  const inside = (path: string, dir: string): boolean => path.startsWith(dir + sep);
  const allowed = (path: string): boolean =>
    inside(path, web('src', 'videoStudio')) ||
    inside(path, web('node_modules')) ||
    path === web('src', 'i18n', 'resources.videoStudio.ts') ||
    path === web('src', 'i18n', 'resources.videoStudioAudio.ts');
  const seams = new Set([resolve(places.here, 'src', 'studio.ts'), resolve(places.here, 'page', 'render.ts')]);
  const shown = (path: string): string => path.replace(resolve(places.here, '..', '..') + sep, '');
  const problems: string[] = [];
  for (const [name, metafile] of Object.entries(bundles)) {
    for (const [input, { imports }] of Object.entries(metafile.inputs)) {
      const path = inputFile(input, places.cwd);
      if (inside(path, places.web) && !allowed(path)) {
        problems.push(`${name}: ${shown(path)} is in the bundle, and it is not the Studio core`);
      }
      const studioPackage = STUDIO_PACKAGES.find((pkg) => path.includes(`${sep}node_modules${sep}${pkg.split('/').join(sep)}${sep}`));
      if (studioPackage !== undefined && !inside(path, web('node_modules'))) {
        problems.push(`${name}: ${studioPackage} comes from ${shown(path)}, not from the cockpit's web/node_modules`);
      }
      if (path.endsWith(UNPATCHED_MARKER)) {
        problems.push(`${name}: renderer-browser is unpatched (googlefonts.json is bundled); run npm ci in web/`);
      }
      if (inside(path, places.here) && !seams.has(path)) {
        for (const imported of imports) {
          if (inside(inputFile(imported.path, places.cwd), web('src'))) {
            problems.push(`${name}: ${shown(path)} imports ${shown(inputFile(imported.path, places.cwd))}; only src/studio.ts and page/render.ts may reach web/src`);
          }
        }
      }
    }
  }
  return problems;
}
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh test/unit/drift.test.ts`

Expected: `Tests  6 passed (6)`, `VITEST_RC=0`.

- [ ] **Step 4: Write the process and the build**

`services/video-mcp/src/main.ts`:

```ts
// main.ts — the process: settings from the environment, the pieces wired, the port opened. A
// SIGTERM (Compose stopping or replacing the container) cancels every job, which closes its
// browser, and then closes the server. A job a restart interrupts is gone: its status answers
// that there is no such job, "or the renderer restarted".

import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { tokenVerifier } from './auth';
import { configFromEnv, PORT } from './config';
import { Credentials } from './credentials';
import { createApp } from './http';
import { internalApi } from './internalApi';
import { renderRunner } from './jobs/job';
import { JobQueue } from './jobs/queue';
import { log } from './log';
import { probeAsset } from './probe';
import { projectStore } from './projects';
import { registerProjectTools } from './tools/projectTools';
import { registerFilmResource } from './tools/filmResource';
import { registerRenderTools } from './tools/renderTools';

/** Where the image keeps the export page (Dockerfile: dist/page beside dist/server.mjs). */
const PAGE_DIR = join(dirname(fileURLToPath(import.meta.url)), 'page');
/** The tmpfs Compose mounts for job scratch (compose.yaml aura-video-mcp tmpfs). */
const SCRATCH_ROOT = '/scratch';

const config = configFromEnv(process.env);
const credentials = new Credentials();
const api = internalApi(config.internalApiUrl);
const store = projectStore(api);
const queue = new JobQueue(renderRunner({ api, store, credentials, pageDir: PAGE_DIR, scratchRoot: SCRATCH_ROOT }));

const app = createApp({
  config,
  verifier: tokenVerifier(config),
  credentials,
  register: (server) => {
    registerProjectTools(server, { api, store, probe: probeAsset });
    registerRenderTools(server, { queue, store, api });
    registerFilmResource(server, api);
  },
  version: process.env.AURA_VIDEO_MCP_VERSION ?? 'dev',
});

const server = app.listen(PORT, '0.0.0.0', () => {
  log('info', 'aura-video-mcp is listening', { port: PORT, issuer: config.issuer });
});

process.once('SIGTERM', () => {
  log('info', 'stopping: running and queued jobs are cancelled');
  queue.cancelAll();
  void queue.idle().finally(() => server.close(() => process.exit(0)));
});
```

`services/video-mcp/build.ts`:

```ts
// build.ts — the sidecar's two bundles, run by Node 24's own type stripping (`node build.ts`):
//   dist/server.mjs   src/main.ts for Node: every dependency inside it except Playwright, which
//                     finds its browser at run time and is installed beside it in the image;
//   dist/page/        page/render.ts for the headless shell, the WASM and worklet files the
//                     Studio imports with Vite's `?url`, the fonts it serves locally, render.html.
// Both resolve the Studio core against web/node_modules, the cockpit's patched install, and the
// drift check (drift.ts) refuses the build when they do not.

import { build, type Plugin } from 'esbuild';
import { copyFileSync, cpSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { basename, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { driftProblems } from './drift.ts';

const HERE = dirname(fileURLToPath(import.meta.url));
const WEB = join(HERE, '..', '..', 'web');
const DIST = join(HERE, 'dist');
const ASSETS = join(DIST, 'page', 'assets');

/** Vite's `?url` import: the file copied next to the page, its URL the module's value. */
const urlImports: Plugin = {
  name: 'url-imports',
  setup(context) {
    context.onResolve({ filter: /\?url$/ }, async (args) => {
      const found = await context.resolve(args.path.slice(0, -'?url'.length), {
        resolveDir: args.resolveDir,
        kind: 'import-statement',
      });
      return found.errors.length > 0 ? { errors: found.errors } : { path: found.path, namespace: 'url-import' };
    });
    context.onLoad({ filter: /.*/, namespace: 'url-import' }, (args) => {
      copyFileSync(args.path, join(ASSETS, basename(args.path)));
      return { contents: `export default ${JSON.stringify(`/assets/${basename(args.path)}`)};`, loader: 'js' };
    });
  },
};

rmSync(DIST, { recursive: true, force: true });
mkdirSync(ASSETS, { recursive: true });

const server = await build({
  entryPoints: [join(HERE, 'src', 'main.ts')],
  outfile: join(DIST, 'server.mjs'),
  bundle: true,
  format: 'esm',
  platform: 'node',
  target: 'node24',
  external: ['playwright', 'playwright-core'],
  // Express and its CommonJS dependencies require Node's built-ins; an ES module has no `require`.
  banner: { js: "import { createRequire } from 'node:module'; const require = createRequire(import.meta.url);" },
  metafile: true,
  logLevel: 'warning',
});

const page = await build({
  entryPoints: [join(HERE, 'page', 'render.ts')],
  outfile: join(DIST, 'page', 'render.js'),
  bundle: true,
  format: 'esm',
  platform: 'browser',
  target: 'es2022',
  // renderer-browser's Node-only branches, never taken in a page (the spike's build.mjs).
  external: ['module', 'fs', 'path', 'url'],
  plugins: [urlImports],
  metafile: true,
  logLevel: 'warning',
});

cpSync(join(WEB, 'public', 'fonts'), join(DIST, 'page', 'fonts'), { recursive: true });
writeFileSync(
  join(DIST, 'page', 'render.html'),
  '<!doctype html>\n<html><head><meta charset="utf-8"><title>aura-video-mcp</title></head>' +
    '<body><script type="module" src="/render.js"></script></body></html>\n',
);

const problems = driftProblems(
  { server: server.metafile, page: page.metafile },
  { here: HERE, web: WEB, cwd: process.cwd() },
);
if (problems.length > 0) {
  console.error(`drift: the sidecar does not take the Studio core from the cockpit\n${problems.join('\n')}`);
  process.exit(1);
}
console.log(`built ${DIST}: server ${Object.keys(server.metafile.inputs).length} inputs, page ${Object.keys(page.metafile.inputs).length} inputs, no drift`);
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc-npm.sh run build`

Expected (measured): `built /mnt/d/Aura/services/video-mcp/dist: server 611 inputs, page 138 inputs, no drift`, `NPM_RC=0`. The input counts move with the dependencies; `no drift` and `NPM_RC=0` are what matter.

- [ ] **Step 5: Show the drift gate fails**

Append this line to `services/video-mcp/src/studio.ts`. It re-exports the cockpit's save path, which reaches the chat attachment client, exactly the drift Task 4 removed:

```ts
export { saveProject } from '../../../web/src/videoStudio/projectStore';
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc-npm.sh run build`

Expected (measured):
```text
drift: the sidecar does not take the Studio core from the cockpit
server: web/src/chat/artifacts/renderers/assetSourceContext.ts is in the bundle, and it is not the Studio core
server: web/src/chat/http.ts is in the bundle, and it is not the Studio core
server: web/src/chat/attachments/api.ts is in the bundle, and it is not the Studio core
server: web/src/chat/attachments/upload.ts is in the bundle, and it is not the Studio core
```
and `NPM_RC=1`.

Remove the line again, and re-run the build. Expected: `… no drift`, `NPM_RC=0`.

- [ ] **Step 6: The whole unit tier, with its floors**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh all`

Expected (measured on the finished scratch tree):
```text
TSC_RC=0
 Test Files  23 passed (23)
      Tests  260 passed (260)
All files          |   98.88 |    92.22 |   99.54 |    99.4 |
VITEST_RC=0
```
Those are statements, branches, functions and lines, each over the 85 % floor. `browser.ts`, `outputProbe.ts`, `transcode.ts` (the render tier's) and `inPage.ts` are not in this denominator; `main.ts` is the process entry.

- [ ] **Step 7: Build in CI too**

In `.github/workflows/ci.yml`, in the `video-mcp-test` job:
1. replace `    name: Video MCP unit tier (typecheck + vitest + coverage)` with `    name: Video MCP unit tier (typecheck + vitest + coverage + bundle drift)`;
2. replace `      - name: Typecheck and the unit tier with coverage` with `      - name: Typecheck, unit tier with coverage, bundles with the drift check`;
3. after the line `          npm test` in that step's `run: |` block, add a line with the same indentation: `          npm run build`.

The job now reads, whole:

```yaml
  # ── aura-video-mcp (services/video-mcp) ─────────────────────────────────────
  # The sidecar takes the Studio core from web/src/videoStudio against web's own patched
  # install, so a change there is a change here. Its unit tier runs here, with its 85 % floor.
  video-mcp-test:
    name: Video MCP unit tier (typecheck + vitest + coverage + bundle drift)
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Detect video MCP changes
        uses: dorny/paths-filter@ceb8a2b8f2d89434be7ff52d3de7ec3738c5cc9d # v4
        id: changes
        with:
          # Keep in step with the backstop's pathspecs below.
          filters: &video-mcp-filter |
            video_mcp:
              - 'services/video-mcp/**'
              - 'web/src/videoStudio/**'
              - 'web/src/i18n/resources.videoStudio*.ts'
              - 'web/package.json'
              - 'web/package-lock.json'
              - 'web/patches/**'
              - 'web/public/fonts/**'
              - 'web/e2e/fixtures/video-studio/audio/speech.wav'
              - 'docker/aura-video-mcp/**'
              - '.dockerignore'
              - '.github/workflows/ci.yml'

      - name: Set up Node
        if: steps.changes.outputs.video_mcp == 'true'
        uses: actions/setup-node@820762786026740c76f36085b0efc47a31fe5020 # v7
        with:
          node-version: ${{ env.AURA_CI_NODE_VERSION }}
          cache: npm
          cache-dependency-path: |
            web/package-lock.json
            services/video-mcp/package-lock.json

      # web first: the Studio core resolves its packages in web/node_modules, patched by web's
      # postinstall.
      - name: Typecheck, unit tier with coverage, bundles with the drift check
        if: steps.changes.outputs.video_mcp == 'true'
        run: |
          cd web && npm ci
          cd ../services/video-mcp && npm ci
          npm run typecheck
          npm test
          npm run build

      - &video-mcp-backstop
        name: No-skip-as-green backstop (filter must not have misfired)
        if: steps.changes.outputs.video_mcp != 'true'
        run: >-
          bash scripts/web_filter_backstop.sh
          "${{ github.event.pull_request.base.sha || github.event.before }}"
          "${{ github.event.pull_request.head.sha || github.sha }}"
          'services/video-mcp/**' 'web/src/videoStudio/**' 'web/src/i18n/resources.videoStudio*.ts'
          'web/package.json' 'web/package-lock.json' 'web/patches/**' 'web/public/fonts/**'
          'web/e2e/fixtures/video-studio/audio/speech.wav' 'docker/aura-video-mcp/**'
          '.dockerignore' '.github/workflows/ci.yml'

```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ci-check.sh`

Expected:
- `video-mcp-test ['Checkout', 'Detect video MCP changes', 'Set up Node', 'Typecheck, unit tier with coverage, bundles with the drift check', 'No-skip-as-green backstop (filter must not have misfired)']`;
- `web-mutation needs ['web-mutation-stryker', 'go-mutation']`;
- `actionlint findings: 16`;
- `OK`.

- [ ] **Step 8: Commit**

Write `$W/msg-t19.txt`:

```text
feat(video-mcp): bundle the sidecar and its page, and refuse a drifted core

build.ts makes two bundles: the server for Node, and the render page
for the headless shell with the Studio's WASM, worklet and fonts. Both
take the Studio core from the cockpit's own files against web's
patched install, and drift.ts reads esbuild's metafiles to refuse a
build that bundled any other part of web/, a second copy of a Studio
package, an unpatched renderer, or a sidecar module reaching past the
two doors. main.ts wires the process and stops every job on SIGTERM.
CI's unit job builds too, so the drift gate runs whenever the Studio
changes.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t19.txt services/video-mcp/package.json services/video-mcp/package-lock.json services/video-mcp/src/main.ts services/video-mcp/drift.ts services/video-mcp/build.ts services/video-mcp/test/unit/drift.test.ts .github/workflows/ci.yml`

Expected: `COMMIT_RC=0`.

---

### Task 20: The image, and the render tier inside it

`docker/aura-video-mcp/Dockerfile` has four stages:
1. `webdeps`: `npm ci` in `web/`, whose postinstall applies `web/patches`.
2. `build`: the sidecar's `npm ci`, then `node build.ts` against that install. The drift gate runs here too.
3. `runtime`: `node:24-bookworm-slim` with:
   - Debian's ffmpeg;
   - Playwright 1.63.0 and its Chrome Headless Shell, installed by Playwright's own CLI, so the browser is the one the package pins;
   - the two bundles;
   - user 10001, port 8097, `node server.mjs`.
4. `integration`: the runtime plus the built repository, run as uid 10001 with `CI=true`. Its command is the render tier with coverage.

The image is built for linux/amd64 only, where the renderer was measured (decided with the operator on 2026-10-02, `391a3c014`). It has no platform branch.

The render tier (`vitest.integration.config.ts`, `test/integration/`) renders real films, in that image, with the image's own ffmpeg making every fixture: two clips (one with a tone, one silent and portrait), a still, a music bed, a clip in HEVC, and films that break the output check one rule at a time. The speech is the cockpit's E2E fixture (`web/e2e/fixtures/video-studio/audio/speech.wav`), because ducking needs a voice a VAD hears. Its 14 tests:
- **delivery:** a 1080×1920 delivery that saves its analyses as a new version; the same film at 720×1280; a cleaned copy made and rendered with; a film with no audio layer given a silent AAC track;
- **failures:** a source the store will not give, a crash of the page, a sound the speech analysis cannot read, a source the renderer cannot play, and a start refused for a source the library no longer holds;
- **the page:** a cancel, which leaves no browser behind; the page reaching nothing but its origin; a browser that cannot open;
- **refusals:** HEVC refused at the edit;
- **the output check** on films that break it.

It measures `browser.ts`, `outputProbe.ts` and `transcode.ts` against their own 85 % floor. It runs under `$CI` with nothing to skip: a missing binary fails it.

CI's `video-mcp-render` job builds the integration target and runs it the way Compose runs the sidecar: read-only root, uid 10001, no network, a tmpfs wherever vitest writes. It then starts the runtime read-only and checks `/health`, the metadata and a 401 for a missing bearer. It reuses the unit job's filter and backstop through their YAML anchors.

**Files:**
- Create: `docker/aura-video-mcp/Dockerfile`
- Modify: `.dockerignore` (the sidecar in, its build output out)
- Modify: `services/video-mcp/package.json` (`test:integration`)
- Create: `services/video-mcp/vitest.integration.config.ts`, `services/video-mcp/test/integration/fixtures.ts`, `services/video-mcp/test/integration/render.test.ts`
- Modify: `.github/workflows/ci.yml` (job `video-mcp-render`)

**Interfaces:**
- Consumes: the whole sidecar; `RENDER_TIER` (Task 1); `startSidecar` (Task 7); `FakeInternalApi` (Task 3); `IDENTITY` (Task 2); the anchors `*video-mcp-filter` and `*video-mcp-backstop` (Task 10).
- Produces:
  - the images `aura-video-mcp:local` (target `runtime`) and `aura-video-mcp:integration` (target `integration`), built by `$W/image.sh`;
  - `makeFixtures(dir): Promise<{ path(name): string }>` (`test/integration/fixtures.ts`);
  - job `video-mcp-render`, with steps "Detect video MCP changes", "Set up Docker Buildx", "Build the image's integration target", "Render tier (…)", "The runtime starts read-only and answers health, metadata and a missing bearer", and the backstop.

- [ ] **Step 1: Write the image and let it see the sidecar**

`docker/aura-video-mcp/Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1.7
# aura-video-mcp: Aura's video sidecar (services/video-mcp). The Studio core is bundled from the
# cockpit's own web/src/videoStudio against the cockpit's own patched install (npm ci in web/,
# whose postinstall applies web/patches), and build.ts refuses the build if it drifted. The page
# renders in Playwright's Chrome Headless Shell, pinned by the Playwright version in
# services/video-mcp/package.json; ffmpeg and ffprobe are Debian's.
#
# linux/amd64 only, where the renderer was measured (spikes/video-mcp-render FINDINGS S1.1), as the
# operator decided on 2026-10-02 (spec §Architecture, "The sidecar").
ARG NODE_IMAGE=node:24-bookworm-slim

FROM ${NODE_IMAGE} AS webdeps
WORKDIR /repo/web
COPY web/package.json web/package-lock.json ./
COPY web/patches ./patches
RUN npm ci --no-audit --no-fund

FROM ${NODE_IMAGE} AS build
WORKDIR /repo/services/video-mcp
COPY services/video-mcp/package.json services/video-mcp/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY --from=webdeps /repo/web/node_modules /repo/web/node_modules
COPY web/src/videoStudio /repo/web/src/videoStudio
COPY web/src/i18n/resources.videoStudio.ts web/src/i18n/resources.videoStudioAudio.ts /repo/web/src/i18n/
COPY web/public/fonts /repo/web/public/fonts
COPY services/video-mcp ./
RUN node build.ts

FROM ${NODE_IMAGE} AS runtime
ARG VCS_REF=dev
ENV NODE_ENV=production \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    AURA_VIDEO_MCP_VERSION=${VCS_REF}
WORKDIR /app
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /repo/services/video-mcp/node_modules/playwright ./node_modules/playwright
COPY --from=build /repo/services/video-mcp/node_modules/playwright-core ./node_modules/playwright-core
RUN node node_modules/playwright/cli.js install --with-deps --only-shell chromium \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /repo/services/video-mcp/dist ./
# Debian already has a `video` group (for /dev/video*): the user is named for the service.
RUN useradd --system --uid 10001 --user-group --home-dir /nonexistent --shell /usr/sbin/nologin aura-video
USER 10001
EXPOSE 8097
CMD ["node", "server.mjs"]

# The render tier (vitest.integration.config.ts) in this very image: same Node, same ffmpeg, same
# headless shell, same user. CI runs it with the root filesystem read-only, as Compose does.
FROM runtime AS integration
USER root
COPY --from=build /repo /repo
COPY web/e2e/fixtures/video-studio/audio/speech.wav /repo/web/e2e/fixtures/video-studio/audio/speech.wav
RUN chown -R 10001 /repo/services/video-mcp
WORKDIR /repo/services/video-mcp
USER 10001
# Vitest 5 writes an API token under $XDG_DATA_HOME/vitest before it runs a test (its
# resolveApiToken); /tmp is the one place a read-only root leaves writable.
ENV CI=true \
    XDG_DATA_HOME=/tmp
CMD ["./node_modules/.bin/vitest", "run", "--config", "vitest.integration.config.ts", "--coverage"]
```

In `.dockerignore`, apply:

```diff
diff --git a/.dockerignore b/.dockerignore
index dc37705e7..225964670 100644
--- a/.dockerignore
+++ b/.dockerignore
@@ -25,6 +25,8 @@
 !services/
 !services/ingest/
 !services/ingest/**
+!services/video-mcp/
+!services/video-mcp/**
 # The appliance payload the aura image carries (docker/aura/Dockerfile, payload stage). Its
 # file list is scripts/payload_manifest.txt: a payload file left out here fails that build.
 !scripts/
@@ -54,3 +56,8 @@ web/.stryker-tmp
 web/coverage
 web/test-results
 web/playwright-report
+services/video-mcp/node_modules
+services/video-mcp/dist
+services/video-mcp/coverage
+services/video-mcp/.stryker-tmp
+services/video-mcp/reports
```

None of these paths is in `scripts/payload_manifest.txt`, so the payload manifest does not change (verified: `grep -c video-mcp scripts/payload_manifest.txt` prints `0`).

- [ ] **Step 2: Write the render tier**

Replace `services/video-mcp/package.json` with:

```json
{
  "name": "aura-video-mcp",
  "private": true,
  "version": "0.0.0",
  "description": "Aura's video sidecar: Studio projects edited and rendered over MCP",
  "type": "module",
  "engines": {
    "node": ">=24.16.0 <25"
  },
  "scripts": {
    "build": "node build.ts",
    "typecheck": "tsc --noEmit -p tsconfig.json",
    "test": "vitest run --coverage",
    "test:integration": "vitest run --config vitest.integration.config.ts"
  },
  "dependencies": {
    "@modelcontextprotocol/sdk": "1.31.0",
    "jose": "6.2.12",
    "playwright": "1.63.0",
    "zod": "4.6.5"
  },
  "devDependencies": {
    "@types/express": "5.0.6",
    "@types/node": "24.19.1",
    "@vitest/coverage-v8": "5.0.3",
    "ajv": "8.20.0",
    "esbuild": "0.28.2",
    "typescript": "7.0.2",
    "vitest": "5.0.3"
  },
  "allowScripts": {
    "esbuild@0.28.2": true
  }
}
```

`services/video-mcp/vitest.integration.config.ts`:

```ts
import { defineConfig } from 'vitest/config';
import { RENDER_TIER } from './vitest.config';

// The render tier: real renders with Chrome Headless Shell and Debian's ffmpeg, in the image
// (docker/aura-video-mcp/Dockerfile, target `integration`). Nothing here skips: a missing binary
// fails the tier, under $CI and everywhere else.
export default defineConfig({
  test: {
    environment: 'node',
    include: ['test/integration/**/*.test.ts'],
    fileParallelism: false,
    testTimeout: 300_000,
    hookTimeout: 180_000,
    coverage: {
      provider: 'v8',
      include: RENDER_TIER,
      reportsDirectory: 'coverage/integration',
      thresholds: { statements: 85, branches: 85, functions: 85, lines: 85 },
    },
  },
});
```

`services/video-mcp/test/integration/fixtures.ts`:

```ts
// Synthetic sources made by the image's own ffmpeg, so the integration tier needs no binary in
// the repository: two clips (one with a tone, one silent and portrait), a still, a music bed, a
// clip in HEVC, and films that break the output check one rule at a time. The speech is the
// cockpit's own E2E fixture (web/e2e/fixtures/video-studio/audio/speech.wav, 12.9 s, its speech
// windows in speech.truth.json), because ducking needs a voice a VAD hears.
import { execFile } from 'node:child_process';
import { copyFile, mkdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

const run = promisify(execFile);
const SPEECH = join(dirname(fileURLToPath(import.meta.url)), '../../../../web/e2e/fixtures/video-studio/audio/speech.wav');

const ffmpeg = (...args: string[]) => run('ffmpeg', ['-v', 'error', '-nostdin', '-y', ...args]);

export interface Fixtures {
  readonly dir: string;
  readonly path: (name: string) => string;
}

export async function makeFixtures(dir: string): Promise<Fixtures> {
  await mkdir(dir, { recursive: true });
  const path = (name: string): string => join(dir, name);
  await ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=30:duration=8', '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=8',
    '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-c:a', 'aac', '-shortest', '-movflags', '+faststart', path('landscape.mp4'));
  await ffmpeg('-f', 'lavfi', '-i', 'testsrc=size=720x1280:rate=30:duration=6',
    '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', path('portrait.mp4'));
  await ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=1080x1920', '-frames:v', '1', path('poster.png'));
  await ffmpeg('-f', 'lavfi', '-i', 'sine=frequency=220:sample_rate=48000:duration=30', '-ac', '2', path('music.wav'));
  await ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=640x360:rate=30:duration=2', '-c:v', 'libx265', '-tag:v', 'hvc1', path('phone-hevc.mp4'));
  await copyFile(SPEECH, path('speech.wav'));
  return { dir, path };
}

/** Films that break the output check one way each, encoded as the transcode delivers a good one. */
export async function brokenFilms(fixtures: Fixtures): Promise<Record<'black' | 'silent' | 'short' | 'noAudio' | 'opus', string>> {
  const { path } = fixtures;
  const delivered = ['-c:v', 'libx264', '-profile:v', 'high', '-pix_fmt', 'yuv420p', '-bf', '0', '-movflags', '+faststart', '-use_editlist', '0'];
  const aac = ['-c:a', 'aac', '-b:a', '128k', '-ar', '48000', '-ac', '2'];
  await ffmpeg('-f', 'lavfi', '-i', 'color=black:size=1080x1920:rate=30:duration=4', '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=4', ...delivered, ...aac, '-shortest', path('black.mp4'));
  await ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=1080x1920:rate=30:duration=4', '-f', 'lavfi', '-i', 'anullsrc=channel_layout=stereo:sample_rate=48000', ...delivered, ...aac, '-shortest', path('silent.mp4'));
  await ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=1080x1920:rate=30:duration=2', '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=2', ...delivered, ...aac, '-shortest', path('short.mp4'));
  await ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=1080x1920:rate=30:duration=4', ...delivered, path('no-audio.mp4'));
  await ffmpeg('-f', 'lavfi', '-i', 'testsrc2=size=1080x1920:rate=30:duration=4', '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=4', ...delivered, '-c:a', 'libopus', '-b:a', '128k', '-shortest', path('opus.mp4'));
  return { black: path('black.mp4'), silent: path('silent.mp4'), short: path('short.mp4'), noAudio: path('no-audio.mp4'), opus: path('opus.mp4') };
}
```

`services/video-mcp/test/integration/render.test.ts`:

```ts
// The render tier: real renders in the image, Chrome Headless Shell and Debian's ffmpeg, driven
// through the MCP tools as a client drives them, against the contract-checked fake of Aura's
// internal API. It runs only where both binaries are, and it never skips: a missing one fails it.
import { execFile } from 'node:child_process';
import { mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest';
import { internalApi } from '../../src/internalApi';
import { openRenderPage, type OpenPage, type RenderPage } from '../../src/jobs/browser';
import { renderRunner } from '../../src/jobs/job';
import { outputProblems } from '../../src/jobs/outputCheck';
import { boxes } from '../../src/jobs/mp4Facts';
import { filmFacts } from '../../src/jobs/outputProbe';
import { startPageServer } from '../../src/jobs/pageServer';
import { JobQueue } from '../../src/jobs/queue';
import { probeAsset } from '../../src/probe';
import { projectStore } from '../../src/projects';
import { registerProjectTools } from '../../src/tools/projectTools';
import type { JobStatus } from '../../src/tools/renderTools';
import { registerRenderTools } from '../../src/tools/renderTools';
import { startSidecar, type Running } from '../support/sidecar';
import { IDENTITY } from '../support/tokens';
import { brokenFilms, makeFixtures, type Fixtures } from './fixtures';

const run = promisify(execFile);
const PAGE_DIR = join(dirname(fileURLToPath(import.meta.url)), '../../dist/page');

let sidecar: Running;
let fixtures: Fixtures;
let scratch: string;
const assets: Record<string, string> = {};
/** Every page a job opened, newest last: the crash test reaches one through DevTools. */
const pages: RenderPage[] = [];
const recordingPages: OpenPage = async (origin, signal, onProgress) => {
  const opened = await openRenderPage(origin, signal, onProgress);
  pages.push(opened);
  return opened;
};

beforeAll(async () => {
  await run('ffmpeg', ['-version']);
  scratch = await mkdtemp(join(tmpdir(), 'video-mcp-it-'));
  fixtures = await makeFixtures(join(scratch, 'fixtures'));
  sidecar = await startSidecar((fake, credentials) => {
    const api = internalApi(fake.url);
    const store = projectStore(api);
    const queue = new JobQueue(renderRunner({ api, store, credentials, pageDir: PAGE_DIR, scratchRoot: scratch, openPage: recordingPages }));
    return (server) => {
      registerProjectTools(server, { api, store, probe: probeAsset });
      registerRenderTools(server, { queue, store, api });
    };
  });
  const seeds: [string, string][] = [
    ['landscape.mp4', 'video/mp4'],
    ['portrait.mp4', 'video/mp4'],
    ['poster.png', 'image/png'],
    ['music.wav', 'audio/wav'],
    ['speech.wav', 'audio/wav'],
    ['phone-hevc.mp4', 'video/mp4'],
  ];
  for (const [fileName, mimeType] of seeds) {
    assets[fileName] = sidecar.api.seed(IDENTITY, { fileName, mimeType, bytes: await readFile(fixtures.path(fileName)) }).id;
  }
}, 180_000);

afterAll(async () => {
  await sidecar.close();
  await rm(scratch, { recursive: true, force: true });
});

const assetIdIn = (text: string): string => /asset ([0-9a-f-]{36})/.exec(text)?.[1] ?? '';

async function finished(jobId: string): Promise<JobStatus> {
  for (;;) {
    const status = (await sidecar.call('video_render_status', { jobId })).structured as JobStatus;
    if (status.state !== 'queued' && status.state !== 'running') return status;
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
}

/** A reel the way a client builds one: two clips crossfaded, a title, a still, the music ducked
 *  under the speech, which only the job's analyses can hear. */
async function reel(name: string): Promise<string> {
  const created = await sidecar.call('video_project_create', {
    name,
    format: '9:16',
    clips: [{ assetId: assets['landscape.mp4'] }, { assetId: assets['portrait.mp4'] }, { assetId: assets['poster.png'], duration: 3 }],
  });
  const project = assetIdIn(created.text);
  const opened = (await sidecar.call('video_project_open', { assetId: project })).text;
  const clips = [...opened.matchAll(/^ {2}([0-9a-f-]{36}) {2}\d/gm)].map((match) => match[1]);
  const edited = await sidecar.call('video_project_edit', {
    assetId: project,
    operations: [
      { op: 'set_junction_transition', fromClipId: clips[0], toClipId: clips[1], transition: 'crossfade', duration: 0.5 },
      { op: 'add_overlay', kind: 'text', anchor: { clipId: clips[0], offset: 1 }, duration: 3, props: { text: 'Aura', fontFamily: 'Atkinson Hyperlegible Next', fontSize: 6, color: '#ffffff' } },
      { op: 'add_audio', assetId: assets['music.wav'], time: 0 },
      { op: 'add_audio', assetId: assets['speech.wav'], time: 1 },
    ],
  });
  expect(edited.text).toMatch(/^saved as a new version/);
  const summary = (await sidecar.call('video_project_open', { assetId: assetIdIn(edited.text) })).text;
  const withDucking = await sidecar.call('video_project_edit', {
    assetId: assetIdIn(edited.text),
    operations: [{ op: 'set_audio_properties', itemId: musicItem(summary), volume: 0.8, ducking: { amountDb: -12, ramp: 0.5 } }],
  });
  expect(withDucking.text).toContain('analyses the next render makes first: speech windows of');
  return assetIdIn(withDucking.text);
}

/** Headless shell processes alive now, read from /proc: the slim image has no procps. */
async function shellsRunning(): Promise<number> {
  let count = 0;
  for (const pid of (await readdir('/proc')).filter((name) => /^\d+$/.test(name))) {
    try {
      if ((await readFile(`/proc/${pid}/cmdline`, 'utf8')).includes('chrome-headless-shell')) count += 1;
    } catch {
      // The process ended between the listing and the read.
    }
  }
  return count;
}

/** The music's item id in a summary: the sound whose source is 30 s long. */
function musicItem(summary: string): string {
  const source = /^ {2}([0-9a-f-]{36}) {2}audio 30 s/m.exec(summary)?.[1];
  const item = new RegExp(`^ {2}([0-9a-f-]{36}) {2}\\S+ s {2}lane \\S+ {2}${String(source)} from`, 'm').exec(summary)?.[1];
  if (item === undefined) throw new Error(`no music in\n${summary}`);
  return item;
}

describe('a render', () => {
  it('delivers a 1080×1920 Instagram MP4 and saves the analyses it made as a new version', async () => {
    const project = await reel('Aura reel');
    const started = await sidecar.call('video_render_start', { assetId: project, quality: '1080p' });
    expect(started.isError).toBe(false);
    const done = await finished((started.structured as JobStatus).jobId);
    expect(done).toMatchObject({ state: 'succeeded', progress: 1, error: null });
    const result = done.result;
    if (result === null) throw new Error('no result');
    expect(result.projectAssetId).not.toBe(project);
    expect((await sidecar.call('video_project_open', { assetId: result.projectAssetId })).text).toContain('analyses: none missing');
    expect(result.downloadUrl).toMatch(/^http:\/\/localhost:\d+\/objects\/media\//);

    const film = join(scratch, 'downloaded.mp4');
    await writeFile(film, Buffer.from(await (await fetch(result.downloadUrl)).arrayBuffer()));
    const facts = await filmFacts(film, { frames: [], sound: [] });
    expect(facts.video).toEqual([expect.objectContaining({ codec: 'h264', profile: 'High', width: 1080, height: 1920, fps: 30 })]);
    expect(facts.audio).toEqual([expect.objectContaining({ codec: 'aac', profile: 'LC', sampleRate: 48_000, channels: 2 })]);
    expect(facts.topLevelBoxes.indexOf('moov')).toBeLessThan(facts.topLevelBoxes.indexOf('mdat'));
    expect(facts.editLists).toBe(0);
    expect(Math.abs((facts.video[0]?.seconds ?? 0) - result.seconds)).toBeLessThanOrEqual(1 / 30 + 1e-6);
    expect(sidecar.api.assets.get(result.filmAssetId)).toMatchObject({ mimeType: 'video/mp4', status: 'accepted', sourceKind: 'video_mcp' });
  });

  it('at 720p is the same film at 720×1280', async () => {
    const project = assetIdIn((await sidecar.call('video_project_create', { name: 'small', format: '9:16', clips: [{ assetId: assets['landscape.mp4'] }] })).text);
    const done = await finished(((await sidecar.call('video_render_start', { assetId: project, quality: '720p' })).structured as JobStatus).jobId);
    expect(done.result).toMatchObject({ width: 720, height: 1280 });
  });

  it('fails naming a source the object store will not give, and delivers no film', async () => {
    const project = assetIdIn((await sidecar.call('video_project_create', { name: 'gone', format: '9:16', clips: [{ assetId: assets['portrait.mp4'] }] })).text);
    const before = [...sidecar.api.assets.values()].filter((asset) => asset.mimeType === 'video/mp4').length;
    sidecar.api.objectFailures.set(assets['portrait.mp4'] ?? '', 404);
    try {
      const done = await finished(((await sidecar.call('video_render_start', { assetId: project, quality: '720p' })).structured as JobStatus).jobId);
      expect(done).toMatchObject({ state: 'failed', error: 'portrait.mp4: the object store answered 404', result: null });
      expect([...sidecar.api.assets.values()].filter((asset) => asset.mimeType === 'video/mp4')).toHaveLength(before);
    } finally {
      sidecar.api.objectFailures.delete(assets['portrait.mp4'] ?? '');
    }
  });

  it('stops when cancelled, and leaves no browser behind', async () => {
    const project = assetIdIn((await sidecar.call('video_project_create', { name: 'cancel', format: '9:16', clips: [{ assetId: assets['landscape.mp4'] }] })).text);
    const { jobId } = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
    for (;;) {
      const status = (await sidecar.call('video_render_status', { jobId })).structured as JobStatus;
      if (status.step === 'render') break;
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
    expect(await shellsRunning()).toBeGreaterThan(0);
    expect(((await sidecar.call('video_render_cancel', { jobId })).structured as JobStatus).jobId).toBe(jobId);
    expect(await finished(jobId)).toMatchObject({ state: 'cancelled' });
    expect(await shellsRunning()).toBe(0);
  });
});

describe('a render that has to clean and to make sound', () => {
  it('makes the cleaned copy noise reduction asks for, records it, and renders with it', async () => {
    const created = await sidecar.call('video_project_create', { name: 'clean', format: '9:16', clips: [{ assetId: assets['landscape.mp4'] }] });
    const opened = (await sidecar.call('video_project_open', { assetId: assetIdIn(created.text) })).text;
    const clip = /^clips, in order:\n {2}([0-9a-f-]{36})/m.exec(opened)?.[1];
    const edited = await sidecar.call('video_project_edit', { assetId: assetIdIn(created.text), operations: [{ op: 'set_clip_presentation', clipId: clip, denoise: true }] });
    expect(edited.text).toContain('cleaned copy of');
    const done = await finished(((await sidecar.call('video_render_start', { assetId: assetIdIn(edited.text), quality: '720p' })).structured as JobStatus).jobId);
    expect(done.state).toBe('succeeded');
    const copies = [...sidecar.api.assets.values()].filter((asset) => asset.mimeType === 'audio/ogg');
    expect(copies).toEqual([expect.objectContaining({ fileName: 'landscape.clean.ogg', status: 'accepted', sourceKind: 'video_mcp' })]);
    expect((await sidecar.call('video_project_open', { assetId: done.result?.projectAssetId })).text).toContain('analyses: none missing');
  });

  it('gives a film with no audio layer at all a silent AAC track, and delivers it', async () => {
    // A still is no audio source: the renderer writes no audio track (BrowserRenderer.js:1316,
    // numberOfChannels 0), and only the transcode's silent track satisfies Instagram's audio row.
    const project = assetIdIn((await sidecar.call('video_project_create', { name: 'mute', format: '9:16', clips: [{ assetId: assets['poster.png'], duration: 4 }] })).text);
    const done = await finished(((await sidecar.call('video_render_start', { assetId: project, quality: '720p' })).structured as JobStatus).jobId);
    expect(done).toMatchObject({ state: 'succeeded', error: null });
  });
});

describe('a render that cannot be made', () => {
  it('is refused at its start, naming a source the library no longer holds', async () => {
    const project = assetIdIn((await sidecar.call('video_project_create', { name: 'deleted', format: '9:16', clips: [{ assetId: assets['landscape.mp4'] }] })).text);
    const asset = sidecar.api.assets.get(assets['landscape.mp4'] ?? '');
    if (asset === undefined) throw new Error('no landscape asset');
    asset.status = 'deleted';
    try {
      const refused = await sidecar.call('video_render_start', { assetId: project, quality: '720p' });
      expect(refused.isError).toBe(true);
      expect(refused.text).toMatch(new RegExp(`^video source [0-9a-f-]{36} \\(asset ${asset.id}\\) is no longer in your library$`));
    } finally {
      asset.status = 'accepted';
    }
  });

  it('fails with the browser’s own words when its page crashes, and leaves no browser behind', async () => {
    const project = assetIdIn((await sidecar.call('video_project_create', { name: 'crash', format: '9:16', clips: [{ assetId: assets['landscape.mp4'] }] })).text);
    const { jobId } = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
    for (;;) {
      if (((await sidecar.call('video_render_status', { jobId })).structured as JobStatus).step === 'render') break;
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
    const { page } = pages.at(-1) as RenderPage;
    // DevTools' Page.crash kills the renderer process the way the kernel's OOM killer does; the
    // call itself never answers, because the page that would answer is gone.
    void (await page.context().newCDPSession(page)).send('Page.crash').catch(() => undefined);
    const done = await finished(jobId);
    expect(done.state).toBe('failed');
    expect(done.error).toMatch(/crashed/i);
    expect(await shellsRunning()).toBe(0);
  });

  it('fails naming the sound a speech analysis could not read', async () => {
    const voice = sidecar.api.seed(IDENTITY, { fileName: 'voice.wav', mimeType: 'audio/wav', bytes: await readFile(fixtures.path('speech.wav')) });
    const created = await sidecar.call('video_project_create', {
      name: 'deaf',
      format: '9:16',
      clips: [{ assetId: assets['portrait.mp4'] }],
      sounds: [{ assetId: assets['music.wav'] }, { assetId: voice.id, time: 1 }],
    });
    const summary = (await sidecar.call('video_project_open', { assetId: assetIdIn(created.text) })).text;
    const ducked = await sidecar.call('video_project_edit', {
      assetId: assetIdIn(created.text),
      operations: [{ op: 'set_audio_properties', itemId: musicItem(summary), ducking: { amountDb: -12, ramp: 0.5 } }],
    });
    voice.bytes = Buffer.from('RIFF and nothing a decoder can read');
    const done = await finished(((await sidecar.call('video_render_start', { assetId: assetIdIn(ducked.text), quality: '720p' })).structured as JobStatus).jobId);
    expect(done.state).toBe('failed');
    expect(done.error).toMatch(/^listening for speech in voice\.wav failed: /);
  });

  it('fails naming a source whose bytes the renderer cannot play', async () => {
    const broken = sidecar.api.seed(IDENTITY, { fileName: 'broken.mp4', mimeType: 'video/mp4', bytes: await readFile(fixtures.path('landscape.mp4')) });
    const project = assetIdIn((await sidecar.call('video_project_create', { name: 'broken', format: '9:16', clips: [{ assetId: broken.id }] })).text);
    broken.bytes = Buffer.from('these are not the bytes ffprobe read');
    const done = await finished(((await sidecar.call('video_render_start', { assetId: project, quality: '720p' })).structured as JobStatus).jobId);
    expect(done).toMatchObject({ state: 'failed', error: 'broken.mp4 could not be played by the renderer' });
  });
});

describe('the render page', () => {
  it('reaches nothing but its own origin', async () => {
    const page = await startPageServer({ pageDir: PAGE_DIR, scratch, sources: new Map() });
    const browser = await openRenderPage(page.origin, new AbortController().signal);
    try {
      const reached = await browser.page.evaluate(async () => {
        try {
          await fetch('http://example.com/beacon');
          return true;
        } catch {
          return false;
        }
      });
      expect(reached).toBe(false);
      expect(browser.blocked).toEqual(['http://example.com/beacon']);
      const written: string[] = [];
      const spy = vi.spyOn(process.stdout, 'write').mockImplementation((chunk) => {
        written.push(String(chunk));
        return true;
      });
      try {
        await browser.page.evaluate(() => setTimeout(() => {
          throw new Error('a layer failed to draw');
        }));
        await vi.waitFor(() => expect(written.join('')).toContain('a layer failed to draw'));
      } finally {
        spy.mockRestore();
      }
    } finally {
      await browser.close();
      await page.close();
    }
  });

  it('closes its browser when it cannot be opened', async () => {
    const page = await startPageServer({ pageDir: join(scratch, 'no-such-page'), scratch, sources: new Map() });
    try {
      await expect(openRenderPage(page.origin, new AbortController().signal)).rejects.toThrow(
        'the render page did not load (404)',
      );
      expect(await shellsRunning()).toBe(0);
    } finally {
      await page.close();
    }
  });
});

describe('a source', () => {
  it('in HEVC is refused at the edit, with the spec’s sentence', async () => {
    const created = await sidecar.call('video_project_create', { name: 'phone', format: '9:16', clips: [{ assetId: assets['phone-hevc.mp4'] }] });
    expect(created.text).toBe(`nothing was created: add_clip ${String(assets['phone-hevc.mp4'])}: HEVC is not supported by the renderer: convert the clip to H.264 first`);
  });
});

describe('the output check, on films that break it', () => {
  const expected = { seconds: 4, fps: 30, width: 1080, height: 1920 };
  const plan = { frames: [1, 2, 3], sound: [{ from: 1.75, to: 2.25 }] };

  it('refuses black frames, silence, a short film, a missing audio stream and Opus', async () => {
    const films = await brokenFilms(fixtures);
    expect(outputProblems(await filmFacts(films.black, plan), expected)).toEqual([
      'the frame at 1 s is black',
      'the frame at 2 s is black',
      'the frame at 3 s is black',
    ]);
    expect(outputProblems(await filmFacts(films.silent, plan), expected)).toEqual(['the film is silent at 1.75 s–2.25 s, where the project has sound']);
    expect(outputProblems(await filmFacts(films.short, { frames: [1], sound: [] }), expected)).toEqual([
      expect.stringMatching(/^the film lasts 2(\.\d+)? s, outside 3 s to 15 minutes$/),
      expect.stringMatching(/^the video lasts 2(\.\d+)? s, not the project's 4 s$/),
    ]);
    expect(outputProblems(await filmFacts(films.noAudio, { frames: [1], sound: [] }), expected)).toEqual(['the file has 0 audio streams, not one']);
    // libopus runs a pure tone over its 128 kbps target: the check names both rules it breaks.
    expect(outputProblems(await filmFacts(films.opus, { frames: [1], sound: [] }), expected)).toEqual([
      'the audio is opus unknown, not AAC-LC',
      expect.stringMatching(/^the audio runs at \d+ kbps, over 128$/),
    ]);
    expect((await boxes(films.black)).editLists).toBe(0);
  });
});
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc-npm.sh install`

Expected: `NPM_RC=0`. Nothing new is installed; the lockfile is unchanged.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh types`

Expected: `TSC_RC=0`.

- [ ] **Step 3: Build both targets**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/image.sh`

Expected: `runtime build exit: 0` and `integration build exit: 0`. The `build` stage's log line `built /repo/services/video-mcp/dist: … no drift` is inside the build. Then the two images, `aura-video-mcp:local 1.52GB` and `aura-video-mcp:integration 2.79GB` as measured. A cold build downloads the headless shell and Debian's ffmpeg and takes several minutes; later builds reuse the layers.

- [ ] **Step 4: Run the render tier, and the runtime smoke**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/render-tier.sh`

Expected (measured; the times are this host's):
```text
RENDER_TIER_RC=0
 ✓ test/integration/render.test.ts (14 tests) 64030ms
   ✓ a render (4)
     ✓ delivers a 1080×1920 Instagram MP4 and saves the analyses it made as a new version 23092ms
     …
 Test Files  1 passed (1)
      Tests  14 passed (14)
All files       |     100 |      100 |     100 |     100 |
 browser.ts     |     100 |      100 |     100 |     100 |
 outputProbe.ts |     100 |      100 |     100 |     100 |
 transcode.ts   |     100 |      100 |     100 |     100 |
health: {"status":"ok","version":"dev"}
metadata: {"resource":"http://127.0.0.1:8097/mcp","authorization_servers":["http://127.0.0.1:9080"],"scopes_supported":["mcp:tools"],"resource_name":"aura-video-mcp"}
no bearer: 401
user 10001, node v24.21.0, ffmpeg version 5.1.9-0+deb12u1
{"time":"…","level":"info","message":"aura-video-mcp is listening","port":8097,"issuer":"http://127.0.0.1:9080"}
```
Vitest names only the tests slower than 300 ms, so one or two of the 14 are counted and not listed. `"version":"dev"` is right here: `image.sh` passes no `VCS_REF`, and the publish does (Task 22).

- [ ] **Step 5: What a held film costs in this image**

Task 13 measured a held film with an idle Node. Read it again with the sidecar's own server at rest, the way Compose will run it (Q1).

Run: `MSYS_NO_PATHCONV=1 wsl bash /mnt/d/Aura/spikes/video-mcp-render/plan-b/held.sh aura-video-mcp:local`

Expected (the second of two runs on 2026-10-02):
```text
== at rest
memory.current 119451648
anon 113070080
file 8192
shmem 0
…
== holding them
memory.current 432246784
anon 113070080
file 312029184
shmem 312004608
== the scratch emptied
memory.current 119283712
anon 112783360
file 32768
shmem 0
```
The `shmem` lines are exact: 0, 312004608, 0. The others move a few MB from run to run with the page cache (the first run read 123846656 at rest and 436486144 holding), so read them as ranges, not as a regression.
The sidecar at rest is 114 to 118 MiB. With the largest film the check passes and a cleaned copy waiting for their caller, it is 412 to 416 MiB of the 6 GiB, and nothing else runs meanwhile, because the waiting job keeps the queue's slot.

- [ ] **Step 6: Show the render tier bites**

The film must fail its check when the transcode writes an edit list.
1. In `services/video-mcp/src/jobs/transcode.ts`, delete the line `    '-use_editlist', '0',`.
2. Run: `MSYS_NO_PATHCONV=1 wsl bash $W/image.sh`. Expected: both builds `exit: 0`.
3. Run: `MSYS_NO_PATHCONV=1 wsl bash $W/render-tier.sh ./node_modules/.bin/vitest run --config vitest.integration.config.ts -t "delivers a 1080"`

   Expected (measured):
   ```text
   RENDER_TIER_RC=1
        × delivers a 1080×1920 Instagram MP4 and saves the analyses it made as a new version
        ↓ at 720p is the same film at 720×1280
        …
    FAIL  test/integration/render.test.ts > a render > delivers a 1080×1920 Instagram MP4 and saves the analyses it made as a new version
   AssertionError: expected { …(11) } to match object { state: 'succeeded', …(2) }
   -   "error": null,
   -   "progress": 1,
   -   "state": "succeeded",
   +   "error": "the film failed its check: the file has 2 edit lists",
   +   "progress": 0.9,
   +   "state": "failed",
   ```
   The film was rendered, transcoded and refused by the output check before anything was saved.
4. Put the line back exactly where it was (after `'-movflags', '+faststart',`), and rebuild with `MSYS_NO_PATHCONV=1 wsl bash $W/image.sh`. Expected: both builds `exit: 0`.

- [ ] **Step 7: The out-of-memory sentence, end to end**

Task 13 tested the sentence on the cgroup's text. This step measures it on a real kill. Write `$W/oom.probe.test.ts`. It stays in the scratchpad, outside the repository:

```ts
// A one-off probe, kept outside the repository: one 1080p render under a container limit too small
// for Chrome, printing what the job says. oom.sh mounts it into the integration image's
// test/integration, so its imports resolve as the render tier's do.
import { mkdtemp, readFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { it } from 'vitest';
import { internalApi } from '../../src/internalApi';
import { renderRunner } from '../../src/jobs/job';
import { JobQueue } from '../../src/jobs/queue';
import { probeAsset } from '../../src/probe';
import { projectStore } from '../../src/projects';
import { registerProjectTools } from '../../src/tools/projectTools';
import { registerRenderTools, type JobStatus } from '../../src/tools/renderTools';
import { startSidecar } from '../support/sidecar';
import { IDENTITY } from '../support/tokens';
import { makeFixtures } from './fixtures';

it('prints what a job killed for memory says', async () => {
  const scratch = await mkdtemp(join(tmpdir(), 'oom-'));
  const fixtures = await makeFixtures(join(scratch, 'fixtures'));
  const pageDir = join(dirname(fileURLToPath(import.meta.url)), '../../dist/page');
  const sidecar = await startSidecar((fake, credentials) => {
    const api = internalApi(fake.url);
    const store = projectStore(api);
    const queue = new JobQueue(renderRunner({ api, store, credentials, pageDir, scratchRoot: scratch }));
    return (server) => {
      registerProjectTools(server, { api, store, probe: probeAsset });
      registerRenderTools(server, { queue, store, api });
    };
  });
  const clip = sidecar.api.seed(IDENTITY, { fileName: 'landscape.mp4', mimeType: 'video/mp4', bytes: await readFile(fixtures.path('landscape.mp4')) }).id;
  const created = await sidecar.call('video_project_create', { name: 'oom', format: '9:16', clips: [{ assetId: clip }] });
  const project = /asset ([0-9a-f-]{36})/.exec(created.text)?.[1] ?? '';
  const { jobId } = (await sidecar.call('video_render_start', { assetId: project, quality: '1080p' })).structured as JobStatus;
  for (;;) {
    const status = await sidecar.call('video_render_status', { jobId });
    const state = (status.structured as JobStatus).state;
    if (state !== 'queued' && state !== 'running') {
      console.log(`PROBE ${status.text}`);
      break;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  await sidecar.close();
}, 300_000);
```

Write `$W/oom.sh`:

```bash
#!/usr/bin/env bash
# oom.sh <memory>: one 1080p render in the integration image under a limit too small for Chrome
# (oom.probe.test.ts beside this script, mounted read-only), then the container's own cgroup
# counters. Measures the job's sentence when the kernel kills the page, end to end.
set -uo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
out="$(mktemp)"
docker run --rm --read-only --memory="$1" --memory-swap="$1" --network none \
  --tmpfs /tmp:size=1g,mode=1777 \
  --tmpfs /repo/services/video-mcp/node_modules/.vite:uid=10001,mode=0700 \
  --tmpfs /repo/services/video-mcp/node_modules/.vite-temp:uid=10001,mode=0700 \
  -v "$here/oom.probe.test.ts:/repo/services/video-mcp/test/integration/oom.probe.test.ts:ro" \
  aura-video-mcp:integration \
  sh -c './node_modules/.bin/vitest run --config vitest.integration.config.ts test/integration/oom.probe.test.ts; cat /sys/fs/cgroup/memory.events' \
  > "$out" 2>&1
sed 's/\x1b\[[0-9;]*m//g' "$out" | grep -E "PROBE|✓|×|^oom|Tests  |Error" | head -20
rm -f "$out"
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/oom.sh 1g`

Expected (measured):
```text
PROBE job … failed: the renderer ran out of memory (the renderer may use 1 GiB): render a shorter film, or use smaller source files
 ✓ test/integration/oom.probe.test.ts (1 test) …
      Tests  1 passed (1)
oom 1
oom_kill 1
oom_group_kill 0
```
The kernel killed the browser (`oom_kill 1`), and the job said so with the limit it had. Delete the two scratchpad files afterwards if you like: nothing in the repository refers to them.

- [ ] **Step 8: The render tier in CI**

In `.github/workflows/ci.yml`, insert this block immediately before the line that begins `  # Mutation evidence arrives from parallel jobs and is judged once.`, that is, right after the `video-mcp-test` job. The block ends with one blank line:

```yaml
  # The render tier: a real film rendered inside the image, where the headless shell and ffmpeg
  # are, against its own 85 % floor, never averaged with the unit tier's.
  video-mcp-render:
    name: Video MCP render tier (a real film rendered in the image)
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Detect video MCP changes
        uses: dorny/paths-filter@ceb8a2b8f2d89434be7ff52d3de7ec3738c5cc9d # v4
        id: changes
        with:
          filters: *video-mcp-filter

      - name: Set up Docker Buildx
        if: steps.changes.outputs.video_mcp == 'true'
        uses: docker/setup-buildx-action@f87e5991a6d7451dcb8d9637bfbc97413f497069 # v4

      - name: Build the image's integration target
        if: steps.changes.outputs.video_mcp == 'true'
        uses: docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7
        with:
          context: .
          file: docker/aura-video-mcp/Dockerfile
          target: integration
          platforms: linux/amd64
          load: true
          tags: aura-video-mcp:integration
          cache-from: type=gha,scope=aura-video-mcp-integration
          cache-to: type=gha,mode=max,scope=aura-video-mcp-integration

      # As Compose runs the sidecar: read-only root, uid 10001, no network. The image sets
      # CI=true, so nothing in the tier can skip; each tmpfs is a directory vitest writes to.
      - name: Render tier (H.264 High, AAC 48 kHz stereo, faststart, no edit list, 1080×1920 at 30)
        if: steps.changes.outputs.video_mcp == 'true'
        run: |
          docker run --rm --read-only --memory=4g --memory-swap=4g --network none \
            --tmpfs /tmp:size=2g,mode=1777 \
            --tmpfs /repo/services/video-mcp/node_modules/.vite:uid=10001,mode=0700 \
            --tmpfs /repo/services/video-mcp/node_modules/.vite-temp:uid=10001,mode=0700 \
            --tmpfs /repo/services/video-mcp/coverage:uid=10001,mode=0700 \
            aura-video-mcp:integration

      # The integration image is the runtime plus the repository, so its /app is the runtime's.
      - name: The runtime starts read-only and answers health, metadata and a missing bearer
        if: steps.changes.outputs.video_mcp == 'true'
        run: |
          docker run -d --name video-mcp-smoke --read-only --tmpfs /tmp:mode=1777 \
            --tmpfs /scratch:size=700m,uid=10001,mode=0700 -p 127.0.0.1:18097:8097 \
            -w /app aura-video-mcp:integration node server.mjs
          for _ in $(seq 1 30); do curl -fsS http://127.0.0.1:18097/health > /dev/null && break; sleep 1; done
          curl -fsS http://127.0.0.1:18097/health | grep -F '"status":"ok"'
          curl -fsS http://127.0.0.1:18097/.well-known/oauth-protected-resource/mcp | grep -F '"resource_name":"aura-video-mcp"'
          test "$(curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:18097/mcp)" = 401
          docker logs video-mcp-smoke
          docker rm -f video-mcp-smoke

      - *video-mcp-backstop

```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ci-check.sh`

Expected:
- `video-mcp-test [… as in Task 19 …]`;
- `video-mcp-render ['Checkout', 'Detect video MCP changes', 'Set up Docker Buildx', "Build the image's integration target", 'Render tier (H.264 High, AAC 48 kHz stereo, faststart, no edit list, 1080×1920 at 30)', 'The runtime starts read-only and answers health, metadata and a missing bearer', 'No-skip-as-green backstop (filter must not have misfired)']`;
- `web-mutation needs ['web-mutation-stryker', 'go-mutation']`;
- `actionlint findings: 16`;
- `OK`.

The smoke step's commands are the ones `render-tier.sh` ran in Step 4.

- [ ] **Step 9: Commit**

Write `$W/msg-t20.txt`:

```text
feat(video-mcp): ship the sidecar as an image, and render in it in CI

The image is Node 24, Playwright's Chrome Headless Shell pinned by
Playwright 1.63.0, and Debian's ffmpeg, running as uid 10001; the
Studio core is bundled against web's patched install inside the
build, where the drift gate refuses anything else. It is built for
linux/amd64 only. The render tier renders real films in that same image,
read-only and with no network: deliveries at 1080 and 720, cleaning
and silence, every way a source can fail, a crash and a cancel that
leave no browser, a page that reaches only its origin, and the output
check on broken films; it fails when the transcode writes an edit
list. CI runs it and smokes the runtime on every change the filter
sees.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t20.txt docker/aura-video-mcp/Dockerfile .dockerignore services/video-mcp/package.json services/video-mcp/vitest.integration.config.ts services/video-mcp/test/integration/fixtures.ts services/video-mcp/test/integration/render.test.ts .github/workflows/ci.yml`

Expected: `COMMIT_RC=0`.

---

### Task 21: Mutation testing on the allowlist and the output check

Two of the sidecar's files decide what is safe:
- `src/jobs/proxy.ts` is the only thing between the render page and the object store;
- `src/jobs/outputCheck.ts` is the only thing between a broken film and the operator's library.

Stryker mutates exactly these two, in CI only. `break: 70` fails the run below 70 % killed. `scripts/critical_mutation_gate.py` reads the report as one more scope, `video_mcp`, scored on its own denominator and never averaged into the frontend's.

The run is `video-mcp-mutation`, a job of its own like the other producers. It is not path-filtered, because the aggregate (`web-mutation`) needs this report on every run and fails closed without it.

Stryker copies the sidecar into `.stryker-tmp/sandbox-*`, where `../../web` does not resolve. So `vitest.stryker.config.ts` runs only the two suites that kill these mutants, `proxy.test.ts` and `outputCheck.test.ts`, neither of which loads the Studio. The job then needs no web install.

**Files:**
- Create: `services/video-mcp/stryker.config.json`, `services/video-mcp/vitest.stryker.config.ts`
- Modify: `services/video-mcp/package.json` and `package-lock.json` (Stryker 10.0.0, `mutation`)
- Modify: `scripts/critical_mutation_gate.py`, `scripts/critical_mutation_gate_test.py`, `scripts/mutation_workflow_test.py`
- Modify: `.github/workflows/ci.yml` (job `video-mcp-mutation`; the aggregate reads its report)
- Modify: `Makefile` (`video-mcp-mutation`)

**Interfaces:**
- Consumes: `proxy.test.ts` (Task 16), `outputCheck.test.ts` (Task 15).
- Produces:
  - `npm run mutation`, writing `services/video-mcp/reports/mutation/mutation.json`;
  - the scope `video_mcp` (files `services/video-mcp/src/jobs/proxy.ts`, `services/video-mcp/src/jobs/outputCheck.ts`), in `REQUIRED_SCOPE_IDS`;
  - `VIDEO_MCP_FILES` and `STRYKER_SCOPE_IDS`, the latter replacing `FRONTEND_SCOPE_IDS`;
  - `--video-mcp-report <path>`;
  - job `video-mcp-mutation`, artifact `video-mcp-mutation`; `web-mutation` `needs: [web-mutation-stryker, video-mcp-mutation, go-mutation]`;
  - `make video-mcp-mutation`.

- [ ] **Step 1: Write the failing tests for the gate and the workflow**

In `scripts/critical_mutation_gate_test.py`, apply:

```diff
diff --git a/scripts/critical_mutation_gate_test.py b/scripts/critical_mutation_gate_test.py
index c107c88ae..4061ea9df 100644
--- a/scripts/critical_mutation_gate_test.py
+++ b/scripts/critical_mutation_gate_test.py
@@ -78,7 +78,7 @@ class GoMutationParserTest(unittest.TestCase):
         # GO_SCOPES must fail the suite here rather than quietly delete its own requirement.
         self.assertEqual(
             critical_mutation_gate.REQUIRED_SCOPE_IDS
-            - critical_mutation_gate.FRONTEND_SCOPE_IDS,
+            - critical_mutation_gate.STRYKER_SCOPE_IDS,
             set(critical_mutation_gate.GO_SCOPES),
         )
         self.assertTrue(
@@ -266,6 +266,25 @@ class MediaFrontendScopeTest(unittest.TestCase):
         )
 
 
+class VideoMcpScopeTest(unittest.TestCase):
+    def test_stryker_mutates_exactly_the_video_mcp_files(self) -> None:
+        service = REPO / "services/video-mcp"
+        config = json.loads((service / "stryker.config.json").read_text(encoding="utf-8"))
+        self.assertEqual(config["mutate"], list(critical_mutation_gate.VIDEO_MCP_FILES))
+        self.assertIn("json", config["reporters"])
+        self.assertEqual(config["jsonReporter"]["fileName"], "reports/mutation/mutation.json")
+        for relative in critical_mutation_gate.VIDEO_MCP_FILES:
+            self.assertTrue((service / relative).is_file(), relative)
+
+    def test_the_mutation_run_includes_their_suites(self) -> None:
+        # A suite renamed away from vitest.stryker.config.ts fails here, not as a silent NoCoverage.
+        service = REPO / "services/video-mcp"
+        include = (service / "vitest.stryker.config.ts").read_text(encoding="utf-8")
+        for suite in ("test/unit/proxy.test.ts", "test/unit/outputCheck.test.ts"):
+            self.assertIn(f"'{suite}'", include)
+            self.assertTrue((service / suite).is_file(), suite)
+
+
 class FrontendMutationParserTest(unittest.TestCase):
     def test_stryker_config_emits_the_machine_readable_report(self) -> None:
         config = json.loads(
@@ -368,10 +387,29 @@ class GateModesTest(unittest.TestCase):
             ),
             encoding="utf-8",
         )
+        self.video_mcp = self.root / "video-mcp-mutation.json"
+        self.write_video_mcp(killed=1, survived=0)
 
     def tearDown(self) -> None:
         self._tmp.cleanup()
 
+    def write_video_mcp(self, killed: int, survived: int) -> None:
+        mutants = [{"id": "k", "status": "Killed"}] * killed + [
+            {"id": "s", "status": "Survived"}
+        ] * survived
+        self.video_mcp.write_text(
+            json.dumps(
+                {
+                    "schemaVersion": "1.0",
+                    "files": {
+                        name: {"mutants": mutants}
+                        for name in critical_mutation_gate.VIDEO_MCP_FILES
+                    },
+                }
+            ),
+            encoding="utf-8",
+        )
+
     def mutate(self, relative_path: str) -> tuple[int, str]:
         self.mutated.append(relative_path)
         return self.outcomes.get(relative_path, (0, mutesting_log()))
@@ -410,6 +448,7 @@ class GateModesTest(unittest.TestCase):
             [
                 "--go-mutesting", "fake",
                 "--frontend-report", str(self.frontend),
+                "--video-mcp-report", str(self.video_mcp),
                 "--output", str(self.root / "report.json"),
                 "--log-dir", str(self.root / "logs"),
                 "--go-cache-dir", str(self.cache_dir),
@@ -473,6 +512,31 @@ class GateModesTest(unittest.TestCase):
             self.aggregate()
         self.assertEqual(self.mutated, [])
 
+    def test_the_aggregate_scores_the_video_mcp_files_on_their_own_report(self) -> None:
+        for scope_id in GO_SCOPES:
+            self.store(scope_id)
+        scopes = {scope["id"]: scope for scope in self.aggregate()["scopes"]}  # type: ignore[union-attr,index]
+        self.assertEqual(
+            scopes["video_mcp"]["files"],
+            ["services/video-mcp/src/jobs/proxy.ts", "services/video-mcp/src/jobs/outputCheck.ts"],
+        )
+        self.assertEqual(scopes["video_mcp"]["killed"], 2)
+
+    def test_the_aggregate_fails_on_a_below_floor_video_mcp_report(self) -> None:
+        for scope_id in GO_SCOPES:
+            self.store(scope_id)
+        self.write_video_mcp(killed=3, survived=2)
+        with self.assertRaisesRegex(RuntimeError, "video_mcp=60.00%"):
+            self.aggregate()
+
+    def test_the_aggregate_fails_closed_without_the_video_mcp_report(self) -> None:
+        for scope_id in GO_SCOPES:
+            self.store(scope_id)
+        self.video_mcp.unlink()
+        with self.assertRaisesRegex(ValueError, "cannot stat Stryker report"):
+            self.aggregate()
+        self.assertIs(self.written_report()["passed"], False)
+
     def test_the_aggregate_fails_on_a_below_floor_group_result(self) -> None:
         for scope_id in GO_SCOPES:
             self.store(scope_id)
```

In `scripts/mutation_workflow_test.py`, apply:

```diff
diff --git a/scripts/mutation_workflow_test.py b/scripts/mutation_workflow_test.py
index 67ec64b6f..5b542684f 100644
--- a/scripts/mutation_workflow_test.py
+++ b/scripts/mutation_workflow_test.py
@@ -11,6 +11,8 @@ import critical_mutation_gate
 REPO = pathlib.Path(__file__).resolve().parents[1]
 GO_CACHE = "artifacts/go-mutation-cache"
 SKILLS_CACHE = "artifacts/skills-mutation-cache"
+# critical_mutation_gate.py --video-mcp-report reads mutation.json from here by default.
+VIDEO_MCP_REPORTS = "services/video-mcp/reports/mutation"
 
 
 def workflow_text(workflow: str) -> str:
@@ -59,6 +61,31 @@ class StrykerJobTest(unittest.TestCase):
         self.assertLess(step_index(steps, "npm run mutation"), step_index(steps, "actions/upload-artifact@"))
 
 
+class VideoMcpStrykerJobTest(unittest.TestCase):
+    def setUp(self) -> None:
+        self.body = job_body("ci.yml", "video-mcp-mutation")
+        self.steps = job_steps("ci.yml", "video-mcp-mutation")
+
+    def test_it_runs_on_every_push_like_the_other_producers(self) -> None:
+        # The aggregate needs this report on every run: a path filter here would leave it
+        # missing on a Go-only push, and the gate would fail closed for nothing.
+        self.assertNotIn("dorny/paths-filter", self.body)
+
+    def test_it_mutates_from_the_sidecar_with_its_own_config(self) -> None:
+        run = self.steps[step_index(self.steps, "npm run mutation")]
+        self.assertIn("cd services/video-mcp && npm ci", run)
+        config = json.loads(
+            (REPO / "services/video-mcp/stryker.config.json").read_text(encoding="utf-8")
+        )
+        self.assertEqual(config["vitest"]["configFile"], "vitest.stryker.config.ts")
+
+    def test_it_hands_its_report_to_the_aggregate(self) -> None:
+        upload = step_index(self.steps, "actions/upload-artifact@", "name: video-mcp-mutation")
+        self.assertIn(f"{VIDEO_MCP_REPORTS}/mutation.json", self.steps[upload])
+        self.assertIn("if: always()", self.steps[upload])
+        self.assertLess(step_index(self.steps, "npm run mutation"), upload)
+
+
 class GoGroupJobTest(unittest.TestCase):
     def setUp(self) -> None:
         self.body = job_body("ci.yml", "go-mutation")
@@ -97,7 +124,9 @@ class AggregateJobTest(unittest.TestCase):
 
     def test_the_aggregate_keeps_its_name_and_runs_after_both_producers(self) -> None:
         self.assertIn("name: Critical mutation testing (each boundary >= 70% killed)", self.body)
-        self.assertRegex(self.body, r"needs: \[web-mutation-stryker, go-mutation\]")
+        self.assertRegex(
+            self.body, r"needs: \[web-mutation-stryker, video-mcp-mutation, go-mutation\]"
+        )
         # A skipped aggregate would read as a green check; it must run and fail closed.
         self.assertIn("if: ${{ !cancelled() }}", self.body)
 
@@ -107,10 +136,12 @@ class AggregateJobTest(unittest.TestCase):
         self.assertIn(f"path: {GO_CACHE}", self.steps[go])
         stryker = step_index(self.steps, "actions/download-artifact@", "name: stryker-mutation")
         self.assertIn("path: web/reports", self.steps[stryker])
+        video = step_index(self.steps, "actions/download-artifact@", "name: video-mcp-mutation")
+        self.assertIn(f"path: {VIDEO_MCP_REPORTS}", self.steps[video])
         run = step_index(self.steps, "python3 scripts/critical_mutation_gate.py")
         self.assertIn("--require-measured", self.steps[run])
         self.assertIn(f"--go-cache-dir {GO_CACHE}", self.steps[run])
-        self.assertLess(max(go, stryker), run)
+        self.assertLess(max(go, stryker, video), run)
 
     def test_the_aggregate_saves_the_merged_cache_once(self) -> None:
         lookup = step_index(self.steps, "actions/cache/restore@", "lookup-only: true")
```

- [ ] **Step 2: Run them to see them fail**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/pygate.sh`

Expected (measured):
- `AttributeError: module 'critical_mutation_gate' has no attribute 'VIDEO_MCP_FILES'`, 13 times;
- `AttributeError: module 'critical_mutation_gate' has no attribute 'STRYKER_SCOPE_IDS'`, once;
- three `IndexError: list index out of range` from `VideoMcpStrykerJobTest` (no such job yet);
- `AssertionError: expected one step with ('actions/download-artifact@', 'name: video-mcp-mutation'), found 0`;
- `AssertionError: Regex didn't match: 'needs: \\[web-mutation-stryker, video-mcp-mutation, go-mutation\\]'`;
- `Ran 70 tests`, `FAILED (failures=2, errors=17)`.

- [ ] **Step 3: Write the Stryker run**

Replace `services/video-mcp/package.json` with:

```json
{
  "name": "aura-video-mcp",
  "private": true,
  "version": "0.0.0",
  "description": "Aura's video sidecar: Studio projects edited and rendered over MCP",
  "type": "module",
  "engines": {
    "node": ">=24.16.0 <25"
  },
  "scripts": {
    "build": "node build.ts",
    "typecheck": "tsc --noEmit -p tsconfig.json",
    "test": "vitest run --coverage",
    "test:integration": "vitest run --config vitest.integration.config.ts",
    "mutation": "stryker run"
  },
  "dependencies": {
    "@modelcontextprotocol/sdk": "1.31.0",
    "jose": "6.2.12",
    "playwright": "1.63.0",
    "zod": "4.6.5"
  },
  "devDependencies": {
    "@stryker-mutator/core": "10.0.0",
    "@stryker-mutator/vitest-runner": "10.0.0",
    "@types/express": "5.0.6",
    "@types/node": "24.19.1",
    "@vitest/coverage-v8": "5.0.3",
    "ajv": "8.20.0",
    "esbuild": "0.28.2",
    "typescript": "7.0.2",
    "vitest": "5.0.3"
  },
  "allowScripts": {
    "esbuild@0.28.2": true
  }
}
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc-npm.sh install`

Expected: `NPM_RC=0`.

`services/video-mcp/stryker.config.json`:

```json
{
  "$schema": "./node_modules/@stryker-mutator/core/schema/stryker-schema.json",
  "packageManager": "npm",
  "testRunner": "vitest",
  "coverageAnalysis": "perTest",
  "mutate": ["src/jobs/proxy.ts", "src/jobs/outputCheck.ts"],
  "thresholds": { "high": 85, "low": 70, "break": 70 },
  "vitest": { "configFile": "vitest.stryker.config.ts" },
  "concurrency": 4,
  "reporters": ["clear-text", "progress", "json"],
  "jsonReporter": { "fileName": "reports/mutation/mutation.json" },
  "tempDirName": ".stryker-tmp",
  "cleanTempDir": true
}
```

`services/video-mcp/vitest.stryker.config.ts`:

```ts
import { defineConfig } from 'vitest/config';

// Stryker's run (stryker.config.json): the suites written for the two files it mutates. Neither
// loads src/studio.ts, so nothing reaches ../../web, which Stryker's copy of this directory
// (.stryker-tmp/sandbox-*/) could not resolve.
export default defineConfig({
  test: {
    environment: 'node',
    include: ['test/unit/proxy.test.ts', 'test/unit/outputCheck.test.ts'],
  },
});
```

Never run `npm run mutation` here: it runs in CI only. Check its two inputs without a mutant. Write `$W/stryker-check.sh`:

```bash
#!/usr/bin/env bash
# Stryker's two inputs, without a mutant: the suites vitest.stryker.config.ts runs, and
# stryker.config.json against Stryker's own JSON schema (with ajv, already a devDependency).
set -uo pipefail
export PATH="$HOME/.local/bin:$PATH"
cd /mnt/d/Aura/services/video-mcp || exit 1
./node_modules/.bin/vitest run --config vitest.stryker.config.ts 2>&1 | sed 's/\x1b\[[0-9;]*m//g' | grep -E "✓|×|Test Files|Tests  "
node --input-type=module -e "
import Ajv from 'ajv';
import { readFileSync } from 'node:fs';
const schema = JSON.parse(readFileSync('node_modules/@stryker-mutator/core/schema/stryker-schema.json', 'utf8'));
const config = JSON.parse(readFileSync('stryker.config.json', 'utf8'));
const ajv = new Ajv({ strict: false, allErrors: true });
const valid = ajv.validate(schema, config);
console.log('stryker.config.json valid against its schema:', valid, valid ? '' : JSON.stringify(ajv.errors));
"
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/stryker-check.sh`

Expected (measured):
```text
 ✓ test/unit/outputCheck.test.ts (33 tests) …
 ✓ test/unit/proxy.test.ts (25 tests) …
 Test Files  2 passed (2)
      Tests  58 passed (58)
stryker.config.json valid against its schema: true
```

- [ ] **Step 4: Teach the gate the new scope**

In `scripts/critical_mutation_gate.py`, apply:

```diff
diff --git a/scripts/critical_mutation_gate.py b/scripts/critical_mutation_gate.py
index c716d0ac3..365dce6d9 100644
--- a/scripts/critical_mutation_gate.py
+++ b/scripts/critical_mutation_gate.py
@@ -39,7 +39,12 @@ MEDIA_FRONTEND_FILES = (
     "src/components/image-generation.tsx",
     "src/components/image.tsx",
 )
-FRONTEND_SCOPE_IDS = frozenset({"frontend", "media_frontend"})
+# aura-video-mcp's two critical files (services/video-mcp/stryker.config.json): the proxy's
+# allowlist, the only thing between the render page and the object store, and the output check,
+# the only thing between a broken film and the operator's library. Scored on their own Stryker
+# run and their own denominator.
+VIDEO_MCP_FILES = ("src/jobs/proxy.ts", "src/jobs/outputCheck.ts")
+STRYKER_SCOPE_IDS = frozenset({"frontend", "media_frontend", "video_mcp"})
 # The scopes this plan added (2026-09-16). A mutation report written before them is stale
 # evidence, not a passing one — release readiness says so by name.
 MEDIA_SCOPE_IDS = frozenset({"media_clamp", "media_watcher", "media_frontend"})
@@ -57,6 +62,7 @@ REQUIRED_SCOPE_IDS = frozenset(
         "elicitation_route",
         "frontend",
         "media_frontend",
+        "video_mcp",
     }
 )
 KILLED_STATUSES = ("Killed", "Timeout")
@@ -259,6 +265,15 @@ def run(args: argparse.Namespace, make_measurer: Callable[..., Any] = measurer)
                 ),
             )
         )
+        report["scopes"].append(
+            scope(
+                "video_mcp",
+                [f"services/video-mcp/{name}" for name in VIDEO_MCP_FILES],
+                parse_frontend_report(
+                    args.video_mcp_report.resolve(), args.frontend_max_age_hours, VIDEO_MCP_FILES
+                ),
+            )
+        )
         failures = scope_failures(report["scopes"], args.minimum)
         report["passed"] = not failures
         if failures:
@@ -291,6 +306,11 @@ def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
         type=pathlib.Path,
         default=pathlib.Path("web/reports/mutation/mutation.json"),
     )
+    parser.add_argument(
+        "--video-mcp-report",
+        type=pathlib.Path,
+        default=pathlib.Path("services/video-mcp/reports/mutation/mutation.json"),
+    )
     parser.add_argument(
         "--output",
         type=pathlib.Path,
```

- [ ] **Step 5: The job, the aggregate and the Make target**

In `.github/workflows/ci.yml`:
1. insert this block immediately before the line `  go-mutation:`. The block ends with one blank line:

```yaml
  # aura-video-mcp's two critical files on their own Stryker run: the proxy's allowlist and the
  # output check (services/video-mcp/stryker.config.json). Two small files, so no incremental
  # cache. Not path-filtered: the aggregate needs this report on every run.
  video-mcp-mutation:
    name: Stryker mutation (video MCP proxy allowlist + output check)
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7

      - name: Set up Node
        uses: actions/setup-node@820762786026740c76f36085b0efc47a31fe5020 # v7
        with:
          node-version: ${{ env.AURA_CI_NODE_VERSION }}
          cache: npm
          cache-dependency-path: services/video-mcp/package-lock.json

      # No web install: the two suites Stryker runs (vitest.stryker.config.ts) never load the
      # Studio. GITHUB_STEP_SUMMARY is unset for the reason web-mutation-stryker gives.
      - name: Stryker mutation run with fresh JSON evidence
        run: cd services/video-mcp && npm ci && env -u GITHUB_STEP_SUMMARY npm run mutation

      - name: Hand the video MCP report to the mutation gate
        if: always()
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: video-mcp-mutation
          path: services/video-mcp/reports/mutation/mutation.json
          if-no-files-found: error

```

2. in the `web-mutation` job, apply:

```diff
@@ -1884,15 +1884,16 @@
   web-mutation:
     name: Critical mutation testing (each boundary >= 70% killed)
     runs-on: ubuntu-latest
-    needs: [web-mutation-stryker, go-mutation]
+    needs: [web-mutation-stryker, video-mcp-mutation, go-mutation]
     # A failed producer must not skip this job: a skipped check reads as green. It runs, finds
     # the measurement that is missing and fails closed.
     if: ${{ !cancelled() }}
     timeout-minutes: 10
-    # REL-02: this is deliberately unconditional. The ten independently-scored
-    # boundaries — eight Go files, the frontend aggregate and the media frontend on its own
-    # denominator — publish one candidate-bound report on every run; no strong scope can
-    # average away a weak one and a Go-only change cannot reuse stale web evidence.
+    # REL-02: this is deliberately unconditional. The eleven independently-scored
+    # boundaries — eight Go files, the frontend aggregate, the media frontend and the video
+    # MCP's two files, each on its own denominator — publish one candidate-bound report on
+    # every run; no strong scope can average away a weak one and a Go-only change cannot reuse
+    # stale web evidence.
     # A Go scope is re-measured only when its input closure changed (2026-09-28): the result
     # measured on a byte-identical closure (scripts/go_mutation_cache.py fingerprints the
     # file, every main-module input of its package's test closure, go.mod/go.sum, the go env
@@ -1933,6 +1934,12 @@
           name: stryker-mutation
           path: web/reports
 
+      - name: Download the video MCP Stryker report
+        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
+        with:
+          name: video-mcp-mutation
+          path: services/video-mcp/reports/mutation
+
       # lookup-only reports an exact per-commit hit (a re-run) without downloading over the
       # merged measurements.
       - name: Look up this commit's Go mutation cache
@@ -1967,6 +1974,7 @@
             artifacts/production-readiness/mutation-logs/
             web/reports/mutation/mutation.json
             web/reports/stryker-incremental.json
+            services/video-mcp/reports/mutation/mutation.json
           if-no-files-found: error
 
   web-e2e:
```

The hunk line numbers are the ones measured in the finished scratch tree; match the hunks by their text.

In `Makefile`, apply:

```diff
diff --git a/Makefile b/Makefile
index 3b9db8093..294f3c87e 100644
--- a/Makefile
+++ b/Makefile
@@ -6,7 +6,7 @@
 # sqlc CLI: install with `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`
 # (v1.27.0 panics on Windows hosts via wazero out-of-bounds; v1.31.1 verified clean).
 
-.PHONY: help tools sqlc memory-up-core lint vet deadcode vuln coverage coverage-docker quality quality-full test test-race tagged-tier-compile file-size embedding-model-contract llm-model-contract capability-declaration web-lint web-test web-mutation web-quality evidence-contracts agent-memory-eval-contract agent-memory-eval agent-memory-eval-running-aura critical-mutation observability-check observability-evidence release-readiness db-up db-migrate db-status db-reset memory-up sandbox-images installer-artifact payload-manifest arcadedb-integration ingest-image ingest-test extractor-matrix ingest-reconcile sandbox-image-contract restore-drill load-chaos musr-e2e
+.PHONY: help tools sqlc memory-up-core lint vet deadcode vuln coverage coverage-docker quality quality-full test test-race tagged-tier-compile file-size embedding-model-contract llm-model-contract capability-declaration web-lint web-test web-mutation video-mcp-mutation web-quality evidence-contracts agent-memory-eval-contract agent-memory-eval agent-memory-eval-running-aura critical-mutation observability-check observability-evidence release-readiness db-up db-migrate db-status db-reset memory-up sandbox-images installer-artifact payload-manifest arcadedb-integration ingest-image ingest-test extractor-matrix ingest-reconcile sandbox-image-contract restore-drill load-chaos musr-e2e
 
 # Resolve go-installed tool binaries even when $GOPATH/bin is not on PATH
 # (common in a fresh WSL login shell). Falls back to a bare name on PATH.
@@ -35,6 +35,7 @@ help:
 	@echo "make web-test      — vitest run --coverage (>=85% thresholds enforced in vitest.config.ts)"
 	@echo "make web-mutation  — Stryker mutation run (break=70: fails below 70% killed)"
 	@echo "make web-quality   — full frontend gate: web-lint + web-test + web-mutation"
+	@echo "make video-mcp-mutation — Stryker on the video MCP's proxy allowlist + output check (break=70)"
 	@echo "make evidence-contracts — self-test every candidate-bound release report parser"
 	@echo "make agent-memory-eval-contract — deterministic evaluator; never claims a live MRS"
 	@echo "make agent-memory-eval — blocking MRS over the already-running live memory stack"
@@ -179,6 +180,10 @@ web-test:
 web-mutation:
 	cd web && npm run mutation
 
+# The video sidecar's critical files (CI video-mcp-mutation); critical-mutation reads its report.
+video-mcp-mutation:
+	cd services/video-mcp && npm run mutation
+
 # Full frontend gate: static checks + unit/coverage + mutation. Mirrors the CI
 # web-lint + web-test + web-mutation jobs (the heavy tiers stay out of the git
 # pre-push hook, which runs only the fast web-lint trio via lefthook).
```

- [ ] **Step 6: Run them to see them pass**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/pygate.sh`

Expected: `Ran 70 tests`, `OK`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ci-check.sh`

Expected:
- `video-mcp-test […]` and `video-mcp-render […]` as in Tasks 19 and 20;
- `video-mcp-mutation ['Checkout', 'Set up Node', 'Stryker mutation run with fresh JSON evidence', 'Hand the video MCP report to the mutation gate']`;
- `web-mutation needs ['web-mutation-stryker', 'video-mcp-mutation', 'go-mutation']`;
- `actionlint findings: 16`;
- `OK`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/svc.sh all`

Expected: as in Task 19 Step 6 (`Tests  260 passed (260)`, the same four coverage figures). The new `vitest.stryker.config.ts` is typechecked with the rest (`TSC_RC=0`).

- [ ] **Step 7: Commit**

Write `$W/msg-t21.txt`:

```text
test(video-mcp): mutate the proxy allowlist and the output check in CI

The allowlist is all that stands between the render page and the
object store, and the output check is all that stands between a broken
film and the library. Stryker mutates exactly those two files on a job
of its own, with break 70, running only the two suites that kill their
mutants, since Stryker's sandbox copy cannot reach web/. The critical
mutation gate scores them as the video_mcp scope on their own
denominator and fails closed when the report is missing or stale.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t21.txt services/video-mcp/package.json services/video-mcp/package-lock.json services/video-mcp/stryker.config.json services/video-mcp/vitest.stryker.config.ts scripts/critical_mutation_gate.py scripts/critical_mutation_gate_test.py scripts/mutation_workflow_test.py .github/workflows/ci.yml Makefile`

Expected: `COMMIT_RC=0`.

---

### Task 22: Publish the image, push B2, read CI

The edge workflow builds and pushes `ghcr.io/chetto1983/aura-video-mcp:master-<sha>` for linux/amd64 only (decided 2026-10-02, `391a3c014`), from the `runtime` target with `VCS_REF` set to the commit, so `/health` answers that version. It then moves the `edge` tag with the others, `aura` last, as it does for every image of the commit.

CI's publish job took about 6 minutes before this image. The first run adds a cold build of this one, which downloads the headless shell and Debian's ffmpeg; here, a rebuild of the runtime after a source change took 131 s. The cache scope `aura-video-mcp-edge` keeps later runs short.

The image is published, not deployed: Compose and the release manifest are Plan C's (§Contract for Plan C).

**Files:**
- Modify: `.github/workflows/publish-aura-edge.yml`

**Interfaces:**
- Consumes: `docker/aura-video-mcp/Dockerfile` (Task 20).
- Produces: `ghcr.io/chetto1983/aura-video-mcp:master-<sha>` and `:edge`, linux/amd64.

- [ ] **Step 1: Publish it**

In `.github/workflows/publish-aura-edge.yml`, apply:

```diff
diff --git a/.github/workflows/publish-aura-edge.yml b/.github/workflows/publish-aura-edge.yml
index ab693054a..3ca852b46 100644
--- a/.github/workflows/publish-aura-edge.yml
+++ b/.github/workflows/publish-aura-edge.yml
@@ -74,7 +74,7 @@ jobs:
       # dies on the missing build context (measured 2026-08-31: `lstat
       # /opt/aura/docker: no such file or directory` killed the first clean-host
       # E2E). Appliances pull — they never build — so the edge channel carries
-      # aura, caddy, cloudflared, ingest, and the two box images below.
+      # aura, caddy, cloudflared, ingest, the two box images and the video sidecar below.
       - name: Build and push caddy
         uses: docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7
         with:
@@ -180,6 +180,26 @@ jobs:
           cache-from: type=gha,scope=aura-egress-edge
           cache-to: type=gha,mode=max,scope=aura-egress-edge
 
+      # The video sidecar (services/video-mcp): linux/amd64 only, where its renderer was
+      # measured, as the operator decided on 2026-10-02 (spec §Architecture, "The sidecar").
+      - name: Build and push the video sidecar
+        uses: docker/build-push-action@c3c9e263c25d99ce0380d002d59b67737d91b0dc # v7
+        with:
+          context: .
+          file: docker/aura-video-mcp/Dockerfile
+          target: runtime
+          platforms: linux/amd64
+          push: true
+          build-args: |
+            VCS_REF=${{ github.sha }}
+          tags: |
+            ghcr.io/${{ github.repository_owner }}/aura-video-mcp:master-${{ github.sha }}
+          labels: |
+            org.opencontainers.image.source=${{ github.server_url }}/${{ github.repository }}
+            org.opencontainers.image.revision=${{ github.sha }}
+          cache-from: type=gha,scope=aura-video-mcp-edge
+          cache-to: type=gha,mode=max,scope=aura-video-mcp-edge
+
       # The edge tags move only here, together and aura last, once every image of this commit is
       # in the registry. They used to move image by image as each build finished, aura first, so
       # a run cancelled by the next push (concurrency above) or failing half way left aura:edge
@@ -192,7 +212,7 @@ jobs:
           REGISTRY: ghcr.io/${{ github.repository_owner }}
           SHA: ${{ github.sha }}
         run: |
-          for image in aura-caddy aura-cloudflared aura-ingest aura-sandbox aura-egress aura; do
+          for image in aura-caddy aura-cloudflared aura-ingest aura-sandbox aura-egress aura-video-mcp aura; do
             docker buildx imagetools create --prefer-index=false \
               --tag "${REGISTRY}/${image}:edge" "${REGISTRY}/${image}:master-${SHA}"
           done
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/ci-check.sh`

Expected: as in Task 21 Step 6, with `actionlint findings: 16`. actionlint reads `publish-aura-edge.yml` too, and the new step adds no finding.

- [ ] **Step 2: Commit**

Write `$W/msg-t22.txt`:

```text
ci: publish the video sidecar's image with the edge channel

ghcr.io/chetto1983/aura-video-mcp is built from the runtime target
for linux/amd64 only, where the renderer was measured, as the
operator decided on 2026-10-02. Its edge tag moves with the others,
aura last.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
```

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/commit.sh $W/msg-t22.txt .github/workflows/publish-aura-edge.yml`

Expected: `COMMIT_RC=0`.

- [ ] **Step 3: Push B2, and read every job on that commit**

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/push.sh push-b2.log`

Expected: the lefthook banner, every pre-push gate passing, `PUSH_RC=0`.

Run: `MSYS_NO_PATHCONV=1 wsl bash $W/poll-ci.sh "$(git -C /mnt/d/Aura rev-parse HEAD)"`

Expected: every run `completed	success`. Read these in their logs rather than trusting the colour:
- "Video MCP unit tier (typecheck + vitest + coverage + bundle drift)": `Tests  260 passed (260)`, the coverage table, and `built …/dist: … no drift`;
- "Video MCP render tier (a real film rendered in the image)": `Tests  14 passed (14)`, the 100 % table, and the smoke's `"status":"ok"`, `"resource_name":"aura-video-mcp"` and the 401;
- "Stryker mutation (video MCP proxy allowlist + output check)": its mutation score, at least 70 % (`break: 70`). It ran first here: the score was never measured locally;
- "Critical mutation testing (each boundary >= 70% killed)": the `video_mcp` scope among the eleven, each at 70 % or more;
- the edge publish: `aura-video-mcp:master-<sha>` pushed and `aura-video-mcp:edge` moved.

A surviving mutant that takes `video_mcp` under 70 % is fixed with a test in `proxy.test.ts` or `outputCheck.test.ts` that kills it, never by lowering `break`. Commit that test alone, push, and poll again. A red job is fixed before B2 is called done, whoever's it is (CLAUDE.md); never re-push only to re-read a hook.

---

## Contract for Plan C

Plan C writes Aura's side. Since the spec's amendment of 2026-10-02 (`698d37234`), the sidecar mounts as a normal MCP, and that side has four parts:
- the recipe `video` in the catalog (§1);
- the Compose service `aura-video-mcp` (§3);
- the internal API's four operations, with Caddy's 404 on their prefix (§4);
- the E2E.

Aura's model reaches the sidecar through its ordinary MCP bridge, as any client does. Everything below is what Plan B's sidecar expects of Aura and offers to it, as it stands after Task 22. A change to any of it is made in the same commit on both sides. The image is published for linux/amd64 only (decided 2026-10-02, `391a3c014`).

### 1. The recipe `video`

Six files, every change named. The model is `calendar`: a first-party HTTP sidecar shipped in the same Compose file, whose grant Aura mints and renews.

1. **`internal/mcp/manager/catalog.go`: a URL helper and an entry.**
   - The helper follows `PIMSidecarBaseURL()` (`:47-56`): Compose DNS in the appliance, the loopback publish for a native Aura process, and WR-01's port check. The path is `/mcp`, with no trailing slash, because the grant's audience is the URL itself (§2):

     ```go
     // videoRecipeURL is the video sidecar's MCP endpoint. The appliance uses Compose DNS;
     // a native Aura process uses the loopback-only host publish. WR-01 keeps an invalid
     // host-side port from retargeting the URL through userinfo syntax.
     func videoRecipeURL() string {
     	if os.Getenv("AURA_IN_CONTAINER") == "1" {
     		return "http://aura-video-mcp:8097/mcp"
     	}
     	port := strings.TrimSpace(os.Getenv("AURA_VIDEO_MCP_PORT"))
     	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
     		port = "8097"
     	}
     	return fmt.Sprintf("http://127.0.0.1:%s/mcp", port)
     }
     ```
   - The entry sits in `BuiltInCatalog()` beside `calendar` and `whatsapp` (`:115-139`), with the same shape:

     ```go
     		{
     			// Aura's video sidecar (services/video-mcp): Video Studio projects edited with
     			// the Studio's own commands, and renders to Instagram-ready MP4s as background
     			// jobs. A first-party HTTP recipe like calendar; not a required capability.
     			Name:       "video",
     			Summary:    "Aura video sidecar: Video Studio projects and Instagram-ready renders over streamable-HTTP",
     			Source:     "recipe:video",
     			TrustClass: mcp.TrustTrustedRecipe,
     			Runtime:    "local",
     			Server: mcp.ManagedServer{
     				Type:   mcp.ServerTypeStreamableHTTP,
     				URL:    videoRecipeURL(),
     				Source: "recipe:video",
     				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
     			},
     		},
     ```
   - `BuiltInCatalog`'s comment, "(calendar, memory, whatsapp)", names `video` too. The tools mount deferred and namespaced through the existing `MountManagedServerWithOptions`, as `calendar`'s do; nothing new is needed for that.
2. **`internal/mcp/manager/first_party.go`: comment only.** `FirstPartyRecipe` walks `BuiltInCatalog()` and matches source and URL together (`:31-44`), so the entry alone makes the server first-party, at exactly the address this deployment resolves. Its comment's list of sidecars gains `video`.
3. **`cmd/aura/mcp_first_party_grants.go`: comment only.** `firstPartyMCPServers` keeps a server that is `FirstPartyRecipe` and `UsesOAuth` (`:99-116`). `UsesOAuth` holds for streamable HTTP with no static bearer and OAuth not disabled (`internal/mcp/oauth_config.go:198-213`), which the recipe is. The keeper then mints, for every active human identity, a token whose audience is `server.URL`. It re-mints when less than 7 minutes are left, on a 5-minute tick (`:44-45`), and the bridge re-reads the grant before every call. So every call Aura makes brings a token with 5 to 15 minutes left. The header comment's "calendar, memory, whatsapp" gains `video`.
4. **`internal/mcp/manager/runtimeset.go`: default-on in the appliance.** Add `videoRecipeName = "video"` beside `calendarRecipeName` and put it in `containerDefaultOnRecipes` (`:48`). Its sidecar ships in the same Compose file, as `calendar`'s and `whatsapp`'s do, and the comment at `:36-44` gives the reason: a recipe nobody installs is a sidecar whose tools never reach the model. An explicit `aura mcp disable video` still wins.
5. **`internal/mcp/egress_policy.go`: let the bridge dial it under a strict profile.** `isTrustedBuiltInHTTPRecipe` (`:66-77`) allows a private address only for the memory, calendar and whatsapp sources. Appliance installs enforce `AURA_PROFILE=single_user_hardened` (`compose.yaml:312-316`), a strict profile, which enforces the policy (`RuntimeEgressPolicy`, `:62-64`). Without `recipe:video` in that switch, the bridge refuses to dial `aura-video-mcp:8097`. Add a `sourceRecipeVideo = "recipe:video"` constant beside the other two (`:15-18`).
6. **`internal/agent/mcptools/bridge_risk.go`: no entry.** The sidecar's tools grade themselves through their annotations. The three that only read are `readOnlyHint: true`. The four that write declare `destructiveHint: false` and `openWorldHint: false`, and cancel also `idempotentHint: true` (Tasks 9, 18). The fallback in `classifyToolRisk` (`:196-235`) therefore grades the writes mutating and not destructive, and none reaches the approval gate (`internal/gateway/classify.go:63`), as the spec wants (§Security, "Gateway"). A `recipe:video` row in `trustedRecipeActions` would replace the hints: if Plan C adds one, it grades exactly that.

The tests that pin these are Plan C's: `catalog_test.go` (the names and the URL helper's three cases), `default_on_test.go`, the egress policy's and `FirstPartyRecipe`'s own tests.

### 2. Not a required capability

Aura must start and run with the sidecar down.
- **Only memory is required.** `cmd/aura/serve_memory_readiness.go:71` is the one capability readiness waits on. Plan C adds no readiness check for `video`, `aura` does not `depends_on` the sidecar, and nothing waits on its health.
- **A sidecar that is not up fails soft.** A default-on recipe whose sidecar is missing is a WARN at mount (`runtimeset.go:43-44`). The boot mount budget retries a sidecar still starting (`mcp_live_mount.go`, at `requestMountTimeout`'s comment: 180 s and 40 attempts in Compose). After that its tools are absent until the next boot or `aura mcp enable video`.
- **Plan C measures it**: Aura booting and answering a chat with `aura-video-mcp` stopped.

### 3. The Compose service

```yaml
  aura-video-mcp:
    image: ${AURA_VIDEO_MCP_IMAGE:-ghcr.io/chetto1983/aura-video-mcp:edge}
    pull_policy: ${AURA_VIDEO_MCP_PULL_POLICY:-always}
    container_name: aura-video-mcp
    restart: unless-stopped
    depends_on:
      aura:
        condition: service_started
    read_only: true
    tmpfs:
      - /tmp:mode=1777
      - /scratch:size=700m,uid=10001,mode=0700
    mem_limit: 6g
    memswap_limit: 6g
    environment:
      AURA_VIDEO_MCP_OAUTH_ISSUER: http://127.0.0.1:9080
      AURA_VIDEO_MCP_OAUTH_JWKS_URL: http://aura:9080/oauth/jwks
      AURA_VIDEO_MCP_OAUTH_RESOURCE: ${AURA_VIDEO_MCP_OAUTH_RESOURCE:-http://127.0.0.1:8097/mcp,http://127.0.0.1:8097/mcp/,http://aura-video-mcp:8097/mcp,http://aura-video-mcp:8097/mcp/}
      AURA_VIDEO_MCP_INTERNAL_API_URL: http://aura:9080/internal/video
    ports:
      - "127.0.0.1:${AURA_VIDEO_MCP_PORT:-8097}:8097"
    healthcheck:
      test: ["CMD", "node", "-e", "fetch('http://127.0.0.1:8097/health').then((r) => process.exit(r.ok ? 0 : 1), () => process.exit(1))"]
      interval: 10s
      timeout: 5s
      retries: 6
      start_period: 10s
```

The service uses the existing default Compose network, alongside `aura` and `garage`.
No additional network or Caddy relay is added. The service itself publishes its port on
loopback, following the operator's Q7 decision in spec commit `e9d379e28`.

Measured or decided for each line:
- **The image, the pull policy and `depends_on`** follow `aura-pim-mcp` (`compose.yaml:1261-1273`). `always` is safe for an image nothing builds locally. `service_started` is enough: the sidecar reads Aura's JWKS only when a call arrives.
- **The image runs as uid 10001** by itself (`USER 10001`); no `user:` is needed.
- **The root is read-only.** The render tier and the smoke ran that way. The job scratch is the `/scratch` tmpfs (`src/main.ts` `SCRATCH_ROOT`), owned by uid 10001.
- **Memory.** `mem_limit: 6g` with `memswap_limit` equal to it is Q3's limit with swap off. Measured: the container then reads `memory.max 6442450944` and `memory.swap.max 0`. The `/scratch` tmpfs is charged to the same limit (Task 13). A film waiting for its caller is charged byte for byte as shmem: the sidecar at rest is 114 to 118 MiB, and 412 to 416 MiB while it holds the largest film the check passes (Task 20).
- **The OAuth resource names.** The canonical one comes first and is the loopback name a host client dials. The Compose name is the one Aura's grant is pinned to (`ResourceURL: server.URL`, §1, 3). Both are listed with and without the trailing slash, as the three other sidecars list theirs (`compose.yaml:814`, `:1052`, `:1307`). The sidecar's own default lists the two slash-less forms (`src/config.ts`). If `AURA_VIDEO_MCP_PORT` changes, this variable changes with it, as `AURA_PIM_MCP_OAUTH_RESOURCE` does.
- **The issuer** is `http://127.0.0.1:9080`, as for the other sidecars. E2E 1's Claude Code reaches both Aura and the sidecar through an SSH tunnel to the VM's loopback, so its tokens carry that issuer too.
- **The health check.** There is no curl or wget in the image, so it uses Node's `fetch`. Measured: it exits 0 against a healthy sidecar and 1 when nothing answers. `/health` needs no token.
- **Network and loopback publish (Q7, decided).** The default network has internet egress, as the operator accepted. `ports` binds only to `127.0.0.1`; host clients and the E2E SSH tunnel dial that publish directly. The browser and media proxy allowlists remain enforced in code. Plan C verifies host `/health` and MCP access on the target stack, together with Caddy's `/internal/video/` 404. The isolated render tier may still use `--network none`, because its fixtures are local; that is not the deployment network policy.
- **CPU.** The measurements ran with `--cpus=4`, and the spec's concurrency of one job is based on that. The block sets no CPU cap; Plan C chooses one per host, and a different cap moves the render times Tasks 11 and 13 recorded.
- **Stopping.** A SIGTERM cancels every job, aborts active API/PUT requests, closes each browser and exits. Plan C measures an active-upload shutdown against Compose's default 10 s grace after R2; the original independent ten-minute PUT timeout does not prove that grace is enough.

The Compose-level variables follow `AURA_PIM_MCP_*`:
- `AURA_VIDEO_MCP_IMAGE`, default `ghcr.io/chetto1983/aura-video-mcp:edge`, the tag Task 22 moves;
- `AURA_VIDEO_MCP_PULL_POLICY`, default `always`;
- `AURA_VIDEO_MCP_PORT`, default 8097, the service's direct loopback publish and the recipe's native URL;
- `AURA_VIDEO_MCP_OAUTH_RESOURCE`, the audience list above.

### 4. The internal API

Base URL: `AURA_VIDEO_MCP_INTERNAL_API_URL`, default `http://aura:9080/internal/video`. It is reachable only on the Compose networks: Caddy answers 404 on `/internal/video/`, and Plan C adds that rule.

The full schemas and one example per outcome are `services/video-mcp/contract/internal-api.json` (§8). Every request carries `Authorization: Bearer <token>`. Every error body is `{"error": "<a sentence>"}`. A missing or refused bearer is 401.

"Has bytes" (`x-bytes-exist`) means status `accepted`, `processing` or `complete` (Q5's provisional upload-finished interpretation). `searchable` and `embedding` were removed by migration `0136_assets_drop_searchable.up.sql`; `uploaded` is excluded until finalized.

| Operation | Method and path | Request | 200 | Errors |
|---|---|---|---|---|
| `resolve` | `POST /internal/video/resolve` | `{"asset_ids": [uuid, …] (1–64, unique), "endpoint"?: "internal" \| "public"}` | `{"assets": [{"asset_id", "url", "file_name", "mime_type", "size_bytes", "expires_at"}], "not_found": [uuid, …]}` | 400 (`asset_ids: at least one asset id`), 401 |
| `presign` | `POST /internal/video/presign` | `{"file_name" (1–255), "mime_type", "size_bytes" (≥ 1), "modality_hint": "video" \| "audio" \| "document"}` | `{"asset_id", "upload": {"upload_url", "method": "PUT", "required_headers": {…}, "expires_at"}}` | 400 with the reason (type, name or size refused), 401 |
| `finalize` | `POST /internal/video/assets/{asset_id}/finalize` | `{"use": "media" \| "document"}` | `{"asset_id", "file_name", "mime_type", "size_bytes", "status"}` | 400 (the bytes never arrived), 401, 404 |
| `tts` | `POST /internal/video/tts` | `{"text" (1–4000), "voice"?, "language"? (≥ 2 chars)}` | the new audio asset, as `finalize` answers it, `status: "accepted"` | 400 (a voice or language it cannot honour, named), 401, 503 (no synthesizer, or it did not answer) |
| `projects` | `GET /internal/video/projects` | none | `{"projects": [{"asset_id", "file_name", "size_bytes", "created_at"}]}`, newest first, at most 200 | 401 |

Four operations, as the spec counts them: `presign` and `finalize` are one, the upload, in two calls.

What each operation must do:
- **Every handler verifies the bearer** exactly as the sidecar does (§5), and acts for its `sub` alone. The sidecar always sends the bearer its caller presented, never one minted for the sidecar (`x-auth`).
- **`resolve` signs two-hour GET links.**
  - It signs on the internal object-store endpoint (`AURA_OBJECTSTORE_ENDPOINT`, `http://garage:3900`) by default, for the sidecar to read. With `"endpoint": "public"` it signs on the public one (`AURA_OBJECTSTORE_PUBLIC_ENDPOINT`), for a person to download a film.
  - It answers only for assets the identity owns, not deleted, that have bytes. Every other id goes in `not_found`, and nothing says why: another identity's asset and one that never existed are answered alike.
  - The two hours matter: `video_render_start` resolves every asset a job will read, and the job reads them within its first hour (Task 17).
  - The object store has no `PresignGet` today (`internal/objectstore/s3.go:72` has `PresignPut` only): Plan C adds it.
- **`presign` creates a new asset of the identity.**
  - It has no thread, goes through `assets.Service.Presign` (`handleAssetPresign`'s path), and is signed for PUT on the internal endpoint.
  - It uses a source kind of the sidecar's own, `video_mcp`. Plan C adds `assets.SourceVideoMCP` and a migration widening `aura.assets`'s `assets_source_kind_check`; the current CHECK in `0035_assets_source_kind_agent.up.sql` admits only `web`, `telegram`, `cli` and `agent`. Choose the next free migration number from `internal/db/migrations/` when landing, per CLAUDE.md; do not pin it in this plan. Cover both migration directions using the repository's existing policy for populated rows, without dropping existing kinds or silently rewriting provenance.
  - Provisionally under Q2, a video may be as large as Instagram's 300,000,000-byte row. `assets.Service.Presign` validates through its configured `Limits`; adding a source kind alone does not alter that limit. Plan C must scope the 300 MB video limit to this handler's service/configuration, and prove the existing web/other upload paths retain their configured limits (50 MiB by default). Every other modality keeps Aura's ceiling.
  - The sidecar PUTs the bytes to `upload_url` with exactly `required_headers`, within `expires_at` (a presign lasts 600 s, `AURA_ASSET_PRESIGN_TTL_SEC`).
- **`finalize` accepts the upload.**
  - `media` is the editor's door: `assets.Service.FinalizeMedia`, accepted and never processed. It is used for films and cleaned copies.
  - `document` is the plain finalize the Studio's `saveProject` calls. It is used for project versions, named `<slug>.aura-video.json`.
  - An asset that is not the identity's, or that this source kind did not presign, is a 404. Plan C must establish and test repeat-finalize behaviour for the same owned asset, including a response lost after acceptance, before Task 3 enables that retry. The contract's examples must record the measured result; the current client/fake cannot establish Go handler idempotence.
- **`tts`** is `internal/multimodal/tts.go`'s speech, saved as an audio asset of the identity, accepted as media.
- **`projects`** lists every asset of the identity whose object key ends in `objectstore.StudioProjectSuffix` (`.aura-video.json`) and that has bytes. A pre-Plan-A project the boot re-key left on a plain `.json` key is not listed (`StudioProjectRekey`, which Plan B does not change).

When the sidecar writes: every write a job makes, the cleaned copies, the analyses version and the film, comes after the output check. Each authenticated request obtains a current caller bearer, including finalize after PUT and public resolve after acceptance. Credential waits share one deadline, provisionally 15 minutes after check completion (Q1/R1). API calls and the signed PUT honour job cancellation and their own timeouts. Accepted writes are retained in failure/cancellation status (R3); there is no rollback operation. A burst of `presign`, `finalize` and `resolve` calls at the end of a render is normal.

### 5. The MCP surface

| | |
|---|---|
| Port | 8097 in the container (`src/config.ts` `PORT`) |
| `POST /mcp` | MCP over streamable HTTP, stateless, JSON answers; every request is authenticated alone |
| `GET`, `DELETE /mcp` | 405: there is no session |
| `/health` | 200 `{"status":"ok","version":"<AURA_VIDEO_MCP_VERSION>"}`, no token |
| Metadata (RFC 9728) | `GET /.well-known/oauth-protected-resource/mcp` → `{"resource":"http://127.0.0.1:8097/mcp","authorization_servers":["http://127.0.0.1:9080"],"scopes_supported":["mcp:tools"],"resource_name":"aura-video-mcp"}` (measured in the smoke, Task 20) |
| 401 | `WWW-Authenticate: Bearer error="invalid_token", …, scope="mcp:tools", resource_metadata="<the metadata URL on the Host the client dialled>"` |
| A request that throws | 500, logged |

**The bearer**, as the sidecar verifies it (`src/auth.ts`):

| Claim or rule | Value |
|---|---|
| Algorithm, keys | EdDSA, a key in Aura's JWKS (`AURA_VIDEO_MCP_OAUTH_JWKS_URL`). An unknown `kid` makes jose's `createRemoteJWKSet` fetch the set again, as a rotated key needs. |
| `iss` | exactly `AURA_VIDEO_MCP_OAUTH_ISSUER` (trailing `/` stripped) |
| `aud` | contains one of `AURA_VIDEO_MCP_OAUTH_RESOURCE` |
| `scope` | contains `mcp:tools` |
| `sub` | the identity id, non-empty |
| `exp` | present and in the future; Aura's tokens last 15 minutes |

The tools (`tools/list`):

| Tool | Input | Annotations |
|---|---|---|
| `video_project_create` | `{name, format: "16:9" \| "9:16" \| "1:1" \| {width, height}, fps?, clips?: [{assetId, duration?, sourceStart?}], sounds?: [{assetId, time?}]}` | `destructiveHint: false`, `idempotentHint: false`, `openWorldHint: false` |
| `video_project_open` | `{assetId}` | `readOnlyHint: true` |
| `video_project_edit` | `{assetId, operations: [1–50 of the 20 operations]}` | as create |
| `video_project_list` | `{}` | `readOnlyHint: true` |
| `video_render_start` | `{assetId (uuid), quality: "1080p" \| "720p"}` | as create |
| `video_render_status` | `{jobId (1–100 chars)}` | `readOnlyHint: true` |
| `video_render_cancel` | `{jobId}` | as create, with `idempotentHint: true` |

The project tools answer text. A refusal is an answer, not an `isError`.

**The render tools' descriptions, annotations, input and output schemas** are `services/video-mcp/contract/render-tools.json`, held to the live listing by the unit tier (Task 18). `video_render_start`'s description tells a model that the render runs in the background, to tell the user it has started, that `video_render_status` delivers the film later, and not to call it repeatedly within one turn. Each render tool answers a `JobStatus` as `structuredContent`:

| Field | Type |
|---|---|
| `jobId` | string |
| `state` | `queued`, `running`, `succeeded`, `failed`, `cancelled` or `unknown` |
| `queuePosition` | integer ≥ 1, or null once it runs |
| `progress` | 0–1 |
| `step` | `analyses`, `compile`, `render`, `transcode`, `check`, `upload`, or null |
| `projectAssetId`, `quality`, `createdAt`, `finishedAt` | string or null |
| `result` | `{filmAssetId, fileName, projectAssetId, bytes, seconds, width, height, downloadUrl, downloadExpiresAt}` or null; success requires a resolved delivery link |
| `error` | string or null |

**R3 amendment:** this original field list is incomplete for partial saves. Task 14/18
must add the accepted-asset record to `JobStatus`, update `render-tools.json` and the live
listing together, and test it on failed and cancelled jobs. Until that schema is written
and validated, clients must not infer that `result: null` means nothing was saved.

What the render tools' answers mean:
- **The sentence.** Each answer also carries one text item, the status in words (Task 18).
- **A start reads.** `video_render_start` loads the project and resolves every asset it plays under the caller's token before queuing (Task 18). A played asset the library no longer holds refuses the start, naming it. A source deleted after the start fails the job by the object store's answer.
- **`unknown`** answers a job id this identity does not own, one that never was, one forgotten two hours after it finished, and every job after a restart.
- **The film link.**
  - A `succeeded` status whose film is at most 25 MiB adds one `resource_link`: `uri` `aura-video://film/<filmAssetId>`, `name` the film's file name, `mimeType` `video/mp4`, `size` its bytes. Exactly 25 MiB is linked, as the bridge accepts it (`internal/mcp/file_test.go:85`).
  - Aura's bridge reads it with `resources/read` on the same session (`internal/agent/mcptools/bridge_links.go`) and writes it into the turn's workspace for `send_file`.
  - A larger film is not linked, and the sentence says to open it from the library or the Studio. On this path Telegram's native video stops at 25 MiB (spec, amended in `8d79a0e6f`). A 1080p reel at the measured 6 Mbps target carries about 54 s under that cap; a 720p one carries well over a minute (Task 11).
- **A failed job** carries `error`. Aura's bridge reads a non-empty `error` as a failed call (`internal/mcp/domain_outcome.go`), so the model sees an error result whose text is the reason. A `failed` answer from `video_render_status` is that, and is expected.
- **Every call is quick.** One MCP call is bounded at 60 s in Aura (`internal/agent/mcptools/timeout.go:13`). The render tools answer at once. `video_project_edit` probes each new asset with ffprobe (20 s at most each, Task 6), so an edit that adds many large assets in one call can pass 60 s: Plan C's E2E should add sources a few at a time.

Resources (`resources/templates/list`):
- `aura-video://film/{assetId}`, `video/mp4`. `resources/read` resolves the asset under the reader's bearer and answers `{contents: [{uri, mimeType: "video/mp4", blob: <base64>}]}`.
- It answers a JSON-RPC error for an asset that is not a `video/mp4` the reader owns ("film X is not in your library"), one over 25 MiB ("film X is N MB, over the 25 MiB a chat can carry: …"), or a store that refuses ("film X could not be read: the object store answered N").

### 6. The sidecar's environment

| Variable | Default | Meaning |
|---|---|---|
| `AURA_VIDEO_MCP_OAUTH_ISSUER` | `http://127.0.0.1:9080` | the `iss` Aura's tokens carry |
| `AURA_VIDEO_MCP_OAUTH_JWKS_URL` | `http://aura:9080/oauth/jwks` | where the signing keys are read, on the Compose network |
| `AURA_VIDEO_MCP_OAUTH_RESOURCE` | `http://127.0.0.1:8097/mcp,http://aura-video-mcp:8097/mcp` | every accepted audience, canonical first (Compose sets four, §3) |
| `AURA_VIDEO_MCP_INTERNAL_API_URL` | `http://aura:9080/internal/video` | the internal API |
| `AURA_VIDEO_MCP_VERSION` | the image's `VCS_REF` (the commit; `dev` in a local build) | what `/health` reports |

Limits are documented constants, not env vars (spec §Environment variables): film length, bitrate, frame, job age, stall, claim time and concurrency are in `src/jobs/limits.ts`, and the measured ones are recorded in prd.md §12.

### 7. Logs

One JSON object per line on stdout: `{"time", "level", "message", …fields}` (`src/log.ts`). The lines the sidecar writes:

| Level | Message | Fields |
|---|---|---|
| info | `aura-video-mcp is listening` | `port`, `issuer` |
| info | `stopping: running and queued jobs are cancelled` | none |
| error | `Aura's JWKS is unreachable, so every bearer token is refused` | `jwks_url`, `reason`; once a minute per reason |
| error | `an MCP request failed` | `identity`, `reason` |
| warn | `the render page raised an error` | `reason` (first 300 characters) |

No field ever holds a bearer or a signed URL. Jobs are not logged: a job's outcome is its status, kept two hours after it finishes. Plan C's E2E reads outcomes through `video_render_status`, never from `docker logs`.

### 8. The shared contracts, and how they keep both sides in agreement

- **`services/video-mcp/contract/internal-api.json`**: the internal API as one JSON Schema 2020-12 document.
  - The schemas are in `$defs`. `x-operations` names each operation's method, path, request schema and response schema per status, with one worked example per outcome. `x-auth` is the bearer rule, and `x-bytes-exist` the statuses that mean the bytes are there.
  - **Plan B's side:** the fake (`test/support/fakeInternalApi.ts`) validates every request the client sends and every answer it gives against those `$defs`. It records a violation instead of answering, and every test that uses it asserts `violations` is empty. "holds every example to its own schemas" checks the document against itself (Task 3).
  - **Plan C's side:** a Go test replays every `x-operations` example against the real handlers and validates each real answer against the same `$defs`. It reads the file from `services/video-mcp/contract/`, as Task 4's parity test reads `projectFile.ts` from `web/`, so there is one copy and no generated twin.
  - **The result:** a change to an operation changes the file, and then the fake's tests and the Go replay both judge it in the same commit. Go already depends on `github.com/google/jsonschema-go` v0.4.3 (`internal/elicit`). Check how it handles `$ref` into `$defs` under 2020-12 before choosing it, and add nothing new without that check (CLAUDE.md, "INVENTORY BEFORE INVENTION").
- **`services/video-mcp/contract/render-tools.json`**: the three render tools as `tools/list` advertises them: description, annotations, input and output schema. It is what any client of `video_render_*`, Aura's bridge included, can rely on. The unit tier fails when the live listing and the file differ.

### 9. What Plan C does not build

The amendment of 2026-10-02 removed these, and nothing in Plan B expects them:
- the `aura.render_jobs` table, its migration, and `internal/renderjobs` (store, sidecar client, watcher);
- the native tools `video_render` and `video_render_cancel`;
- a completion route in `cmd/aura/background_completion.go`, a wake of the conversation, and a progress card;
- a hidden tool set in the bridge (`bridge_policy.go`): Aura's model sees `video_render_*` as any client does;
- a token minted for a job (the spec's former 90-minute one), or any credential of the sidecar's own: every call carries the caller's bearer;
- a `recipe:video` row in `trustedRecipeActions`, unless it grades exactly as the annotations do (§1, 6).

Plan C builds §1 to §4 and the E2E, and with them:
- `PresignGet` (§4);
- the 300 MB video ceiling for the sidecar's source kind (Q2);
- the `video_mcp` source-kind constant and CHECK migration (§4), distinct from the removed render-jobs migration;
- the Caddy 404 and the service's direct loopback publish on the default Compose network (Q7, decided);
- the appliance's side: `.env` and the updater, which pull `aura-video-mcp:edge` like the other sidecars, and the release manifest that pins images by digest. None of Plan B's files is in `scripts/payload_manifest.txt`.

Two renders at once (spec §Limits) is neither plan's: it needs a measurement first.

## Spec coverage

| Spec | Where |
|---|---|
| §Architecture 1, the sidecar, MCP over streamable HTTP with Aura's OAuth | Tasks 1, 2, 7 |
| §Architecture 2, one Studio core, the drift test | Tasks 4, 5, 19 |
| §Architecture 3, renders as jobs, a browser per job, renderer-server's flags, the watchdog | Tasks 14, 16, 17 |
| §Architecture 4, ffmpeg: AAC-LC 48 kHz stereo 128 kbps, `+faststart`, no edit list, video copied | Tasks 12, 20 |
| §Architecture 5, the output check | Tasks 15, 20 |
| §Architecture 6, uploads through the internal API: the MP4, project versions and cleaned copies | Task 17 (the job's last step), Task 3 |
| §Tools, the project tools and the 20 operations, `add_speech` through the internal TTS | Tasks 6, 8, 9 |
| §Tools, the render tools | Task 18 |
| §Tools, "Analyses" saved as a new version | Task 17 |
| HEVC refused with the spec's sentence | Task 6 (unit), Task 20 (in the image) |
| §Jobs and delivery, as amended: the sidecar owns every job, delivery is the status call's | Tasks 14, 17, 18 |
| §"Amended 2026-10-02" 1, no wake, a bounded MCP call | Task 18 (the tools answer at once, and the description says not to poll within a turn); §Contract 5 |
| §"Amended 2026-10-02" 2, a render that outlives its token: compute without one, every write waiting for one | Tasks 2, 13, 14, 16, 17, 18 (Q1) |
| §"Amended 2026-10-02" 3, a film into a chat under the bridge's cap | Task 18 |
| §Security, the proxy allowlist, read-only, non-root, tmpfs | Tasks 16, 17, 20; §Contract 3 |
| §Security, default-network egress with page/proxy allowlists | Tasks 16, 17; §Contract 3; Q7 decided in `e9d379e28` |
| §Security, "Gateway": no approval gate | Tasks 9, 18 (closed-world annotations); §Contract 1, 6 |
| §Security, the internal API and its not-found semantics | Task 3 (contract and fake); §Contract 4 |
| §Limits: length, bitrate, frame, concurrency, measured | Tasks 11, 13, 14 |
| §Tests and E2E, Gate 2, the sidecar's tiers; the CI integration that never skips | Tasks 1–21 (unit), 20 (render tier), 10, 19, 20, 21 (CI) |
| Mutation in CI only, on the proxy allowlist and the output check | Task 21 |
| §Environment variables | §Contract 3, 6 |
| The image published as `ghcr.io/chetto1983/aura-video-mcp`, amd64 only (amended `391a3c014`) | Tasks 20, 22 |
| The decisions table's Telegram row and E2E 3, at the bridge's 25 MiB (amended `8d79a0e6f`) | Task 18; Task 11's PRD paragraph; §Contract 5 |
| Plan A's carry-overs | Global Constraints, "Plan A carry-overs" |
| Aura's side: the recipe, the Compose service, the internal API, the E2E | Plan C (§Contract for Plan C) |

## Self-review

**Historical self-review, 2026-10-02.** The claims below describe the original scratch
version, not the revised execution requirements. The 2026-10-05 applicability review
found four missing regressions despite those green tests. §Applicability corrections is
the current acceptance list; the affected interfaces and code blocks remain baseline
until Tasks 2, 3, 14, 17 and 18 implement and verify it.

- **Placeholders.** The generator that wrote this document from its parts refuses an unknown directive and lists any `TBD`, `TODO`, `FIXME`, `fill in` or `implement later` it finds; it found none. Every code step carries the file itself: the generator copied it from the scratch clone where it was run, so no step says "similar to" another.
- **Names and types.** Every name an Interfaces block lists is the one the code exports. Checked:
  - `Credentials.next` (Task 2) is what `bearer()` calls in the job's last step (Task 17);
  - `RunControls.waitFor` (Task 14) is what the job calls and its tests stub; it stops both clocks;
  - `JobRequest.project` and `.sources` (Task 14) are filled by `resolvePlayed` at the start (Tasks 17, 18) and read by the job;
  - `PageServerOptions.kept` (Task 16) is what the job passes for the cleaned copies under `kept-<source id>` (Task 17);
  - `LIBRARY_WRITE` (Task 9) is what the render tools' start and cancel use (Task 18);
  - `Delivery.fileName` (Task 14) is what the job fills (Task 17) and the link names (Task 18);
  - `CHAT_FILE_MAX_BYTES`, `filmUri`, `megabytes` and `OVER_CHAT_CAP` (Task 18) are used by `renderTools.ts` alone, besides `filmResource.ts`;
  - `RENDER_TIER` and `IN_PAGE` (Task 1) are read by Task 20's config;
  - the seam's late exports (`ProjectSize`, `junctionDurationAt`, `recordAnalysis`, `toVideoJSON`) land in Tasks 11, 15 and 17, each with its first user.

  The typecheck passed at every one of Tasks 14–19 on exactly the files that exist by then.
- **Review Focus.** Each of its five lines names the tests that pin it, and every one of those tests is in the files the tasks write.
- **Against the amended spec.** The plan was checked against the spec at `391a3c014`:
  - the normal-MCP amendment (`698d37234`): no native wrapper, job token, watcher, hidden tools, wake or card anywhere in the plan; reads at the start and every write waiting for a token (Q1); delivery through the status call;
  - the 25 MiB Telegram correction (`8d79a0e6f`): Task 11's PRD paragraph and §Contract 5 measure a reel against it;
  - arm64 not supported (`391a3c014`): no platform branch in the code, the image or the publish, and no test of one.
