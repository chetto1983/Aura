# Media editing — live verification (Plan A, Task 12)

Two runs against the running appliance at `https://localhost`, both on revision
`09b764cfd`. No product code, compose file, Caddy file or `.env` was changed by either.

- **First run, 2026-09-19 21:34–21:46 UTC.** Every flow. It found that no browser upload
  works on the appliance (Bug 1), so flows 1 and 3 were carried past it with an in-test
  forward to Garage.
- **Second run, 2026-09-19 22:14–22:16 UTC**, after the front door was fixed by
  `839e62efd` ("route every per-identity bucket to Garage, not only aura-assets") and Caddy
  was reloaded on the live stack. Flows 1 and 3 again, **with no forward at all**.

## Verdict

| Flow | Before `839e62efd` | After `839e62efd`, no forward | Key number |
|---|---|---|---|
| 1. Studio image → Filters → Save to library → picker | **FAIL**: "Saving failed: upload failed" | **PASS** on desktop and phone | stored WebP warm share 0.242 → 0.997 |
| 2. Agent video → trim 1–3 s → download | PASS (no upload involved) | unchanged | ffprobe 2.042 s on Chrome, phone and Firefox |
| 3. Phone MP4 + JPEG as chat attachments → Edit | **FAIL**: the attachment chip says "Failed" | **PASS** on desktop and phone | trimmed phone clip 2.000 s, rotation kept |
| 4. Firefox trim + HEVC | Trim PASS | unchanged | HEVC trim is **not** refused; HEVC crop **is**, with the app's sentence |
| 5. Rotation-only export | **Copy path** | unchanged | 52–65 ms, codec parameters identical to the source |

The editors do their part on every surface tried. Trimming, rotating, cropping and
downloading involve no upload, so they always worked. Saving and attaching did not until
`839e62efd`: Caddy routed only `/aura-assets/*` to Garage while a presigned PUT goes to the
per-identity bucket path (Bug 1). With the fix on the live stack, both flows pass end to
end with nothing intercepted but the agent run.

## What was run

| | |
|---|---|
| Revision | `docker inspect aura --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'` → `09b764cfd1f89aeba22479a1ad5b661c19be990b` (image `ghcr.io/chetto1983/aura:edge`, container started 2026-09-19T21:26:00Z) |
| Origin | `AURA_E2E_ORIGIN=https://localhost`, logged in through `web/e2e/auth.ts` (Authula, operator credentials from `.e2e.env`, session cached under `web/test-results/.auth`) |
| Harness | Playwright 1.63.0 test runner with a scratch config and spec kept outside the repo; one worker; `serviceWorkers: 'block'` |
| Chrome desktop | Branded Chrome 153.0.8010.52 (headless), 1440×900, DPR 1 |
| Phone | The same Chrome with device emulation: 390×844, DPR 3, `isMobile`, `hasTouch`, an Android user agent |
| Firefox | Playwright's Firefox 155.0 (headless), 1440×900 |
| ffprobe | `jrottenberg/ffmpeg:7.1-alpine`, run from Git Bash with `MSYS_NO_PATHCONV=1` |

### Media used

Real media already on the stack:
- The Studio image record `9778389b…`: `generated.webp`, 1024×1024, 61 994 B, from `sourceful/riverflow-v2.5-fast`.
- The Studio video record `9a26b64e…`: `generated.mp4`, from `google/veo-3.1-lite`. H.264 High L3.1, 1280×720, 24 fps, 96 frames, 4.000 s, 6 796 942 b/s, no audio, 3 408 236 B.

**Substitution:** no chat on the stack holds an agent-generated video. All three
conversations are text only; `/threads/{id}/messages` carries no media. The Veo clip above
is the only agent-generated video, so flow 2 trims it on the Studio stage.

Uploads, generated with ffmpeg for these runs:
- `phone.mp4`: coded 1920×1080 H.264 High L4.0, 30 fps, 6 s, AAC 48 kHz. Display matrix rotation 90, so it displays at 1080×1920. 2 922 497 B.
- `photo.jpg`: 1600×1200 baseline JPEG, 109 927 B.
- `hevc.mp4`: HEVC Main L3.1 (`hvc1`), 1280×720, 30 fps, 4 s, AAC. 837 693 B.

## Method

Neither run started a paid generation or a model turn.
- **No Studio Generate.** The Studio was only read and edited.
- **The run was held in the browser.** `**/agent/run` was routed to a handler that never
  answers, so the request never left Chromium or Firefox. Server evidence:
  - Every conversation the sends created read `TotalInputTokens 0`, `TotalOutputTokens 0`, `TotalCostUSD 0`.
  - The aura log holds only MCP grant and embed-backfill lines across both windows
    (21:30–21:46 and 22:10–22:25 UTC), with no run.
- **No vision summary.** A chat image's regular finalize (`internal/assets/service.go:147`)
  queues the image processor. Its vision call goes to the primary model whenever that
  model takes images (`internal/multimodal/vision.go:145`), which can be a paid OpenRouter
  call. So for **the JPEG only**, the test routed the composer's
  `POST /api/assets/{id}/finalize` to the Studio's `POST /api/studio/uploads/{id}/finalize`.
  That route is `FinalizeUnprocessed` (`service.go:165`): the same acceptance checks, no
  processing. The MP4 and HEVC files took the regular finalize, because no processor is
  wired for video.

**The Garage forward — first run only.** This was the substitution that let flows 1 and 3
continue past Bug 1 before it was fixed. **The second run used none of it**: the only route
left in place was the held agent run and the JPEG's finalize above.
- The browser's PUT to `https://172.31.131.222/aura-<identity>/…` was carried to Garage on
  `127.0.0.1:3900`. It kept its signed `Host` and its presigned query, and got a CORS
  answer.
- Nothing else changed: presign, signature, finalize, Studio library and download were all
  the running server's.
- The chat composer uploads a `File` by XHR, and Playwright's interception exposes no body
  for that. So for those PUTs the forward sent the attached file itself, named by the
  signed `x-amz-meta-filename` header. In the second run the composer's own body went to
  Garage, because nothing intercepted it.
- Checked after both runs: every stored object reads back through `/api/assets/{id}/download`
  with the local file's SHA-256 (`phone.mp4` `ba65bbfd…`, `photo.jpg` `3fa80881…`).

## Flow 1 — Studio image → Filters → Save to library → library picker

Steps: Studio → select the image record → Edit → Filters (on the phone behind Filerobot's ☰
menu) → Sepia → Save to library. Then close the editor and open Start frame → Your images.

**Before `839e62efd`: FAIL (desktop).**
- `POST /api/assets/presign` returned 200.
- The `PUT https://172.31.131.222/aura-214f9d28-…/chat/52396265-….webp` failed with
  `net::ERR_FAILED`.
- The editor shows "Saving failed: upload failed" with Retry.
- The asset row `c5265375…` stayed `presigned` with 0 bytes, and nothing reached the library.

![Save to library fails before the Caddy fix](media-editing-2026-09-19/desktop-studio-save-upload-failed.png)

**After `839e62efd`, nothing forwarded: PASS on desktop and phone.** The presigned PUT goes
straight to `https://172.31.131.222/aura-214f9d28-…/chat/<uuid>.webp` and Caddy hands it to
Garage.

| | desktop | phone |
|---|---|---|
| Save to library → "Saved to the Studio library" | **438 ms** | **461 ms** |
| New library asset | `87fb0223…` `generated-edited.webp`, image/webp, 82 932 B | `5bbe58d5…` `generated-edited.webp`, 73 016 B |
| Stored image | 1024×1024 | 1024×1024 |
| Warm share (R ≥ G ≥ B), source 0.242 | **0.9972** | **0.9965** |
| Mean R−B, source −2.4 | **+38.9** | **+38.9** |
| `/api/studio/library` count | 10 → 11 | 12 → 13 |
| First tile in "Your images" | `generated-edited.webp` | `generated-edited.webp` |

The warm share and R−B are measured on the bytes read back from the download route, not on
the canvas. The library counts start high because the first run's items were still there;
everything this verification created has since been deleted (see Leftovers).

The first run, through the forward, measured the same picture: 443 ms and 364 ms, the same
byte counts, warm 0.997, and the new item first in the picker.

![Photo editor, Sepia, desktop](media-editing-2026-09-19/desktop-studio-photo-sepia.png)
![The new photo first in the Studio picker, desktop](media-editing-2026-09-19/desktop-studio-library-picker.png)

<img src="media-editing-2026-09-19/phone-studio-photo-sepia.png" alt="Photo editor, Sepia, phone" width="260"> <img src="media-editing-2026-09-19/phone-studio-library-picker.png" alt="The Studio picker, phone" width="260">

Both screenshots are from the second run. The picker also holds the earlier sepia copies and
the colour-bar `photo.jpg` chat attachments of flow 3: "Your images" lists usable images
from any thread by design (`service.go:31`). All of them were deleted afterwards.

## Flow 2 — agent-generated video → trim 1–3 s → download

Steps: Studio → select the Veo record → Edit → Start `1` → End `3` → Save.

| Browser | Export (click → download) | File | ffprobe |
|---|---|---|---|
| Chrome desktop | 61 ms, 56 ms (two runs) | `generated-edited.mp4`, 2 558 958 B | **2.041667 s**; H.264 High L3.1 1280×720, 24 fps |
| Phone | 58 ms | same size | **2.041667 s** |
| Firefox | 165 ms | same size | **2.041667 s** |

- **It is a copy.** Profile, level and resolution are the source's.
- **The stream holds 73 frames.** That is the key frame before 1 s onward: the `expand`
  policy keeps it behind an edit list. The edit list presents 2.04 s, one 24 fps frame
  over 2 s.
- The filmstrip drew 10 frames in all three browsers.

![Video editor, trim 1–3 s, desktop](media-editing-2026-09-19/desktop-studio-video-trim.png)

<img src="media-editing-2026-09-19/phone-studio-video-trim.png" alt="Video editor, trim, phone" width="260">

## Flow 3 — phone MP4 and JPEG as chat attachments

Steps: New chat → Add files (`phone.mp4` + `photo.jpg` together) → text → Send, with the
run held. Then the attachment card's Edit (pencil).

**The composer cannot open Edit before sending.** The pending chip (`AttachmentChip.tsx`)
offers only Remove. The Edit button lives on the sent message's `AttachmentCard`. So the
message was sent with `/agent/run` held.

**Before `839e62efd`: FAIL.**
- The chip reads **Failed**.
- The console reports the CORS block on `https://172.31.131.222/aura-214f9d28-…/chat/….mp4`,
  and the PUT fails with `net::ERR_FAILED`.
- Asset `1fa1ff9f…` stayed `presigned`, 0 bytes.

![Chat attachment fails before the Caddy fix](media-editing-2026-09-19/desktop-chat-upload-failed.png)

**After `839e62efd`, nothing forwarded: PASS on desktop and phone.**

Requests the browser made, in order:
1. `presign` ×2, then `PUT` ×2 straight to `https://172.31.131.222/aura-214f9d28-…/chat/<uuid>.<ext>`.
2. `finalize` ×2: the MP4's regular, the JPEG's the Studio's unprocessed one (see Method).
3. `POST /api/conversations` 201.
4. `POST /agent/run`, held.

Both uploads were stored whole, with the composer's own XHR body:

| Asset | desktop | phone |
|---|---|---|
| `phone.mp4`, 2 922 497 B, SHA-256 `ba65bbfd…` | `f0260e9d…`, `complete` | `7ef7b529…`, `complete` |
| `photo.jpg`, 109 927 B, SHA-256 `3fa80881…` | `4a7740e3…`, `accepted` | `8a921a5d…`, `accepted` |

Each was read back through `/api/assets/{id}/download`: the same byte count and the same
SHA-256 as the local file.

The cards show a pencil named "Edit phone.mp4" and "Edit photo.jpg".

![The sent message's attachment cards](media-editing-2026-09-19/desktop-chat-cards.png)

The "Untitled" rows in that sidebar are the conversations the held sends opened; they have
since been deleted. The `photo.jpg` card's preview had not finished loading when the shot
was taken; the phone run shows it drawn.

| | desktop | phone |
|---|---|---|
| `phone.mp4` editor size label | **1080 × 1920** (file rotation applied) | **1080 × 1920** |
| Trim 1–3 s → download `phone-edited.mp4` | **59 ms** (58 ms in the first run) | **63 ms** (74 ms) |
| ffprobe of the cut | video **2.000 s**, 60 frames, H.264 High L4.0, coded 1920×1080, **display matrix rotation 90 kept**; AAC 2.008 s; 977 093 B | identical |
| `photo.jpg` → Filters → Inkwell → Download | `photo-edited.jpeg`, 65 280 B, `ffd8ff`, 1600×1200 | 51 346 B, `ffd8ff`, 1600×1200 |
| Grey share (\|R−G\|, \|G−B\| ≤ 6), source 0.00003 | **1.000** | **1.000** |

![Phone MP4 in the video editor, desktop](media-editing-2026-09-19/desktop-chat-phone-mp4-trim.png)
![JPEG in the photo editor, Inkwell, desktop](media-editing-2026-09-19/desktop-chat-jpeg-inkwell.png)

<img src="media-editing-2026-09-19/phone-chat-phone-mp4-trim.png" alt="Phone MP4 editor, phone" width="260"> <img src="media-editing-2026-09-19/phone-chat-jpeg-inkwell.png" alt="JPEG editor, phone" width="260">

## Flow 4 — Firefox, and an HEVC source

**Firefox trim of the Veo clip: PASS.** 165 ms, 2.041667 s, the same 2 558 958 B file
Chrome writes. The filmstrip drew 10 frames.

![Firefox, trim](media-editing-2026-09-19/firefox-studio-video-trim.png)

**HEVC, uploaded as a chat attachment** (first run, through the forward; Firefox's own PUT
body reached it, no substitution needed).

| | Firefox 155 | Chrome 153 on this host |
|---|---|---|
| Editor opens | yes; preview black, filmstrip **0** frames | yes; filmstrip 10 frames |
| Trim 1–3 s | **not refused**: 136 ms, 623 564 B, HEVC Main 2.033 s + AAC 2.008 s | 50–55 ms, the same 623 564 B file |
| Crop 1:1 | **refused** in 383 ms: "This browser cannot process the video track (hevc). Try Chrome or Edge." No file. | transcoded in 616 ms: H.264 High L3.1 **720×720**, `has_b_frames=0`, 120 frames, 4.000 s, 2 890 784 b/s + AAC |

- **A trim or rotation never refuses HEVC.** It is a copy, and a copy needs no decoder, so
  Firefox writes a valid HEVC file it cannot itself play.
- **The refusal appears only when a transcode is needed.** A crop is the case measured.
- **The sentence is the app's own.** It is `mediaEdit.video.blocked` in English, not a raw
  Mediabunny error. The Italian string was not exercised live.

![Firefox refuses the HEVC crop](media-editing-2026-09-19/firefox-hevc-crop.png)

## Flow 5 — rotation only (90°, no trim change, no crop)

Chrome desktop: Reset → Rotate → "Rotate 90° right" → Save.

| Source | Size label | Export | Result (ffprobe) |
|---|---|---|---|
| `phone.mp4`, matrix +90, shown 1080×1920 | 1920 × 1080 | **65 ms** | H.264 High L4.0, coded 1920×1080, 180 frames, 6.000 s, **3 758 957 b/s = source**; AAC 126 759 b/s = source; **no display matrix** (+90 and −90 cancel); 2 919 342 B (source 2 922 497 B) |
| Veo `generated.mp4`, no matrix, 1280×720 | 720 × 1280 | **52 ms** (both runs) | H.264 High L3.1, coded 1280×720, 96 frames, 4.000 s, **6 796 942 b/s = source**; **display matrix rotation −90** (90° clockwise); 3 400 536 B (source 3 408 236 B) |

**Verdict: copy path.**
- Profile, level, `has_b_frames`, frame count and stream bit rate are identical to the source.
- Only the display matrix changes.
- The export finishes in tens of milliseconds.

A real transcode shows new encoder parameters. The HEVC crop above is one: a new codec,
`has_b_frames=0`, a new size and a new bit rate. Its time was 616 ms, not seconds, because
the clip is 4 s of 720p and Chrome encodes in hardware. On a short clip the time does not
separate the two paths; the codec parameters do.

![Rotation only, 90° right, phone MP4](media-editing-2026-09-19/desktop-chat-phone-mp4-rot90.png)

## Bugs found

### Bug 1 — every browser upload failed on the appliance (blocker) — FIXED by `839e62efd`

The presigned PUT and Caddy did not match.
- `AURA_OBJECTSTORE_PUBLIC_ENDPOINT=https://172.31.131.222`, and per-identity isolation
  names the bucket `aura-<identity>`. The presigned URL is therefore
  `https://172.31.131.222/aura-214f9d28-a020-4627-8611-4ee85ab4e1dc/chat/<uuid>.<ext>`.
- `caddy/Caddyfile:33` sends only `/aura-assets/*` to `garage:3900`. Everything else goes
  to `aura:9080`, which answers the preflight with 401 and no CORS headers.

Preflight measured with `curl -k -X OPTIONS -H 'Origin: https://localhost' -H 'Access-Control-Request-Method: PUT'`, before and after the fix:

| Path | before, via `https://localhost` | before, via `https://172.31.131.222` | after, via `https://172.31.131.222` |
|---|---|---|---|
| `/aura-assets/x` | 200 | 200 | 200, `Access-Control-Allow-Origin: *` |
| `/aura-214f9d28-…/chat/x.webp` | **401** | **401** | **200**, `Access-Control-Allow-Origin: *` |
| `/aura-probe-identity/x` | — | — | 200, `Access-Control-Allow-Origin: *` |

Effects seen before the fix:
- Save to library: "Saving failed: upload failed".
- Chat attachments: the chip says "Failed".
- The Studio's own "Upload a file" frame upload takes the same presign route
  (`frameUpload.ts` calls `presignAsset`), so it failed too. That was not exercised directly.
- Failed attempts left `presigned` 0-byte asset rows (`c5265375…`, `1fa1ff9f…`).
- The committed E2E (`web/e2e/media-edit.spec.ts`) cannot catch this. It runs against a
  side stack with a shared bucket and no Caddy in front.

**The fix**, `839e62efd`, replaces the `/aura-assets/*` handle with
`@objectstore path_regexp ^/aura-[A-Za-z0-9][A-Za-z0-9_.-]{0,63}/` in `caddy/Caddyfile` and
`caddy/Caddyfile.domain`, and was applied to the live `/opt/aura/caddy/Caddyfile` with a
Caddy reload. Flows 1 and 3 were then re-run with no forward at all and pass, on desktop and
phone: see the two flow sections above.

### Bug 2 — finalize accepts an object shorter than declared (minor, server)

`accept` (`internal/assets/service.go:183-217`) checks the stored size only against the
modality limit. It never compares it with `declared_size_bytes` and does not refuse 0 bytes.

Still open. The first run's first forward attempt stored empty objects, and the server
accepted them:
- `ea7a6a19…` `phone.mp4`: declared 2 922 497 B, stored 0 B, status `complete`.
- `9395b01f…` `photo.jpg`: declared 109 927 B, stored 0 B, status `accepted`.
- Both have the content hash of the empty string (`e3b0c442…`).

The empty JPEG then showed in the Studio picker as a broken tile. The editor itself handled
the empty clip correctly ("The file could not be opened."). The empty objects came from the
test harness, not from a browser, but the server accepted them. Both were deleted after the
measurement (`DELETE /api/assets/{id}` → 200, now `deleting`).

### Bug 3 — a stray chevron on the desktop video tool row (cosmetic, Plan A)

- `VideoEditor.tsx:225` passes `className="sm:hidden"` to `NativeSelect`, which applies it
  to the inner `<select>`.
- The wrapper, which turns `w-full` through `has-[select.w-full]` (`native-select.tsx:13`),
  stays, and so does its `ChevronDownIcon`.
- So at ≥ 640 px a lone chevron sits at the right end of the Trim / Crop / Rotate / Audio
  row. It is visible in every desktop and Firefox video-editor screenshot above.

### Bug 4 — portrait filmstrip frames are stretched (cosmetic, Plan A)

- `VideoTimeline.tsx` `Frame` draws every thumbnail with
  `drawImage(source, 0, 0, 160, 90)`, whatever the frame's shape.
- A portrait clip's frames (about 51×90) come out stretched about 3× wide.
- The phone MP4 filmstrips above show horizontal bars smeared across each slot, while the
  preview above them is correct.

## Leftovers — all removed

After the second run everything the two runs created was deleted, and the stack reads as it
did before them: 6 images in the Studio library (`generated.webp` and five `generated.png`,
all from 2026-09-16/17) and 3 conversations.

Deleted:
- **20 assets**, `DELETE /api/assets/{id}` → 200 each: four `generated-edited.webp`
  (`be8db27e…`, `3d56b2ec…`, `87fb0223…`, `5bbe58d5…`), four `phone.mp4`, four `photo.jpg`,
  four `hevc.mp4`, the two `presigned` 0-byte rows (`c5265375…`, `1fa1ff9f…`) and the two
  empty ones of Bug 2 (`ea7a6a19…`, `9395b01f…`, deleted right after that measurement).
- **9 empty conversations**, `DELETE /api/conversations/{id}` → 204 each. Send opens the
  conversation (`POST /api/conversations` 201) before the held run, so each send left one.

Kept, because they are not mine: the two Studio-generated assets (`generated.webp`,
`generated.mp4`), the older library images, and the three real conversations. The delete
filter matched only the four test file names, created at or after 2026-09-19 21:30 UTC, with
`source_kind: web`; conversations only when created in that window with an empty title and
zero tokens and cost.

## What this does not prove

- **Safari and iOS.** WebKit was not run (the `mobile-safari` project was not enabled), and
  no real iPhone was used.
- **A real phone.** "Phone" is desktop Chrome's device emulation on Windows, not Android
  Chrome's codec stack or memory limits.
- **Very large clips.** The largest source was 3.4 MB and 6 s. Neither the 500 MB "Open
  anyway" gate nor memory under a long 4K clip was exercised.
- **Uploads beyond what the second run touched.**
  - The upload path is proven only for this host's origin, `https://localhost` with the
    object store at `https://172.31.131.222`. `caddy/Caddyfile.domain` carries the same fix
    but was not exercised.
  - The harness ignores HTTPS errors, so whether a real browser trusts Caddy's internal
    certificate for `172.31.131.222` is still not measured.
  - Only two uploads per viewport, 2.9 MB and 110 KB, and no resumable or interrupted
    upload.
  - The Studio's own "Upload a file" frame picker was never used; it shares the presign
    route, so the fix should cover it, but that is inference, not measurement.
- **The regular chat image path.** The JPEG took the Studio's unprocessed finalize, so the
  vision summary and document naming of a chat image did not run.
- **A persisted chat message.** With the run held, the cards are the client's optimistic
  message. That they survive a reload of the thread was not checked.
- **The other Edit entry points.** Edit on an agent-delivered video or image inside a chat
  (`LocalArtifactDisplay`, `GeneratedImagePreview`, `PreviewModal`) was not exercised: no
  such chat exists, and making one needs a model turn.
- **Other tools and editor states.**
  - Photo editor: only the Filters tab was used (no crop, rotate, annotate or resize).
  - Video editor: mute, and a crop of an H.264 source, were not exercised.
  - The Italian locale was not used live.
- **Other hosts.** Firefox is Playwright's 155.0 build, headless. Chrome's HEVC decode
  comes from this Windows host; on a host without it Chrome would refuse the HEVC crop as
  Firefox did, but that was not measured.
