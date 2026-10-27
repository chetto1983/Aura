# Image and video generation: the paid acceptance suite on the lab VM

Date: 2026-10-27. Task 13, Steps 4–5 of `docs/superpowers/plans/2026-09-14-image-video-generation.md`.
The operator approved the paid batch ("procedi pure").

**The run.**
- Stack: the lab VM `192.168.101.158`, running `9a316eaf5`, which its updater had just applied.
- Models chosen in the cockpit: `microsoft/mai-image-2.6-flash` for images, $19 per million image
  tokens, and `minimax/hailuo-3-max` for video, $0.05–0.08 per second.
- Suite: `web/e2e/media-generation-live.spec.ts`, run from WSL against `https://192.168.101.158`
  with the operator's own cockpit account, on Playwright's bundled Chromium 1243.

**Verdict:**
- The agent generated and delivered every medium the suite asks for.
- Two failures were the suite's, not the product's: a selector removed on 2026-09-28, and a count
  the product changed on 2026-09-17. Both are fixed.
- Criterion 3 passed later the same afternoon, from the cockpit (below).
- Four of the eleven acceptance criteria are not run (6, 8, 9, 10 below) and one is run only in
  part (1): they stay open.

## The runs

| Run (UTC) | Result | Why |
|---|---|---|
| 13:51 | 0/5: the first case failed, and the serial suite skipped the other four | The image was generated and shown, but the case waited 5 minutes for `[data-slot="image-preview"] img`, which `19d9239a2` removed. Fixed in `8c8b70be3`. |
| 14:04 | 4/5 passed in 2.9 min | The detached case expected the submit's frame to stay once the clip had landed. `adae9bd3b` (2026-09-17) drops it, because above the clip it is an empty second copy. Fixed in `07bc96796`. |

The detached case's remaining assertions were read from that run's persisted thread instead of
paying for another clip:
- two `video_generate` calls: the first `in_progress` with job `c78f702d…`, the second the wake's
  collect delivering `e7da807d…` (`video/mp4`);
- `AURA_VIDEO_INLINE_WAIT_SEC` is back to unset.

## What was generated, and what it cost

Read from the conversations' tool results:

| Case | Call | Asset | Cost (USD) |
|---|---|---|---|
| image (13:51 run) | `image_generate` 16:9 | `c1a0ffbe…` image/png | 0.0196 |
| image | `image_generate` 16:9 | `fdc18973…` image/png | 0.0196 |
| same-turn clip | `video_generate` 4 s requested, 720p | `00fc51c2…` video/mp4 | 0.396 |
| first-frame animation | `video_generate` 5 s, 480p, `first_frame_asset_id` = `fdc18973…` | `81528406…` video/mp4 | 0.2475 |
| detached clip | `video_generate` submit, then collect | `e7da807d…` video/mp4 | 0.396 |
| **total** | | | **1.0787** |

The two clips at $0.396 are 5 s at the 768p rate: the agent asked for 720p in one, and the other
named no resolution. The plan estimated $1.72 for six images and six clips.

## Criterion 3, from the cockpit

At 14:42 UTC the VM ran `5f1b8c72d`, which lets a chat artifact be edited and sent with the next
message. The operator asked for the edit to be Aura's, so the run went like this:
- **Attach.** In the 14:04 conversation, `fdc18973…` opened centred in the preview. Edit opened
  PhotoCraft, in Italian. File → Salva put the image in the composer as an upload, `377bc909…`
  (`source_kind` `web`), and the frame said "Allegato al messaggio."
- **Ask.** The message: "Modifica l'immagine allegata: trasforma la scena in un tramonto, con il
  cielo arancione e rosa riflesso nel lago. Lascia la barca rossa com'è."
- **Result.** Aura called `image_generate` with `reference_asset_ids` = [`377bc909…`]:
  - it returned `bd38798a…` (image/png, $0.0225365);
  - the scene was a sunset, and the red boat was unchanged.

Found in the same pass and fixed in `fix(chat): keep a user turn's files when the thread is
reloaded`: the card of the photo left the operator's message whenever the worker report refresh
replaced the thread with the server's snapshot, and came back only on a page reload.

The paid total for the day is $1.1012.

## The acceptance matrix

| # | Criterion | Status |
|---|---|---|
| 1 | Catalogs, admin save/reset, member denied | Partly: both catalogs answered (61 image models, 30 video), with the chosen models and Hailuo's $/s. Save/reset and the member refusal were not run. |
| 2 | A fresh prompt makes an image | **Passed.** `tool_search` promoted `image_generate`, the card rendered a decoded PNG (naturalWidth > 0), and the operator saw it in the cockpit. |
| 3 | Edit an uploaded photo | **Passed**, from the cockpit (above). The upload came from the preview's Edit, not from the file picker; both go through the same attachment adapter. |
| 4 | A same-turn clip | **Passed**, with playback. HTTP Range streaming passed as its own case. |
| 5 | A detached clip arrives on the wake | **Passed**, with the frame-drop count fixed. The rest was read from the thread, as above. |
| 6 | Restart mid-job | Not run. |
| 7 | Animate a generated image | **Passed.** `first_frame_asset_id` is that run's generated image. |
| 8 | Telegram | Not run: it sends real messages, and the operator has not approved that. |
| 9 | Zero-credit identity | Not run: no test identity is created on the VM. |
| 10 | Five fresh conversations | Not run. |
| 11 | Gates on the final revision | CI on the pushed head carries it. |

## What this does not show

- Criteria 1 (in part), 6, 8, 9 and 10, which stay open.
- The attachment-card fix on the VM: it was found there, but the run predates it.
- The suite after its two fixes as one green run: the fixed detached count was checked against the
  persisted thread, not re-run.
- Image quality beyond the operator's look and the decoded size. The clips were inspected as
  played in the browser, not frame by frame.
