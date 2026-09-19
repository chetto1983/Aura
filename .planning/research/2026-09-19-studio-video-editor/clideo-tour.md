# Clideo online video editor — UX tour

Measured 2026-09-19 in Chrome driven by Playwright (`driver.mjs` in this folder). Desktop 1440×900
(`d*.png`, single-purpose tools `t*.png`), phone 390×844 with `isMobile` and `hasTouch` (`p*.png`).
Anonymous session: no login, no account, no payment. Only uploads: `clip.mp4` (4 s, with audio) and
`photo.png`. 135 screenshots, named in the text below.

**What this tour does NOT show.** Anything behind the sign-in wall (1080p/4K export, watermark
removal, the crown, "Continue in … more"). Generate (video, image, audio), Auto Subtitles, TTS
synthesis and Record: panels opened, nothing submitted. Error states for bad files, which the upload
rule excluded. Stock video and image insertion: only browsed. Phone pinch-zoom, and the codec or
bitrate of the exported file, which was not downloaded. One run each, no timing benchmarks: the
"~5 s export" figure is one observation. Where a behaviour is inferred rather than exercised, the
text says so.

---

## (a) Catalogue by area

### A1. Entry, empty state, upload
- `/video-editor` is a marketing hero with **Get started** (`d00-landing`, `p00-landing`). It opens
  `/editor/` with an empty project.
- Empty state (`d01-empty-editor`): the panel says "Click to upload or drag & drop file here" over
  blurred sample thumbnails. A source strip underneath offers Google Drive, Dropbox, Record and Text
  to Speech (tooltips). The empty timeline says "Add media to timeline to start creating video!".
  There is no onboarding tour and no coach marks.
- The file input accepts about 90 extensions (video, audio and image), `multiple`. After upload the
  preview is ready in under a second (`d02-uploading-0`), which points to client-side decoding. The
  project gets a URL, `/editor/<uuid>`.
- Dragging a file anywhere over the editor shows a full-window overlay, "Drop your files here to
  upload" (`d84-dragover-timeline`).

### A2. Editor shell (desktop) — `d03-editor-loaded`
- **Header:** logo, then the project title. It defaults to the date ("19 Sep 2026") and is an
  editable input (`d88-title-edit`). Undo and Redo, a crown button (premium: opens sign-in,
  `d75-crown-upgrade`), and a blue **Export**.
- **Left rail** (70 px): a blue **+** (Upload), then 12 categories: AI Agent, Media, Canvas, Text,
  Audio, Videos, Generate, Images, Elements, Subtitles, Record, TTS.
- **Panel** (about 440×450, top left): it shows the selected layer's tabs, or the last category when
  nothing is selected (`d06-deselected`).
- **Preview** (right): the selected layer shows a bounding box with corner, side and rotation
  handles.
- **Timeline** (full width, about 370 px tall). Toolbar left: Split, Freeze frame, Duplicate, Bring
  Forward, Send Backward, Delete. These are icon-only with tooltips (`d04-tooltip-split`). Centre:
  previous, play, next and `0:00.00 / 0:04.00`. Right: Timeline Settings (Snapping on, Ripple editing
  off, `d05-timeline-settings`), Zoom Out, Zoom In, Fit to Screen, Full Screen.
- Every icon button has a tooltip, but there are no text labels.

### A3. Left-rail categories
| Category | What it offers | How it reaches the timeline | Screens |
|---|---|---|---|
| AI Agent | "What do you want to create?" with chips (Generate video, Create trending TikTok video, Create voiceover, Find music, Find stock video) and a "Describe your idea" box | A chip **sends at once**. The reply streams ("Thinking…"), then shows a step card "✓ Music added" and a summary. It added a **full-length 2:09 track** on a new top row. | `d20-rail-ai-agent`, `d74-ai-agent-chip`, `d76-ai-agent-reply` |
| Media | Your uploads, with duration badges. Hovering shows a trash button. Freeze frames and used stock tracks collect here too ("clip frozen.png", mp3s). | Clicking a thumbnail adds a **new top overlay row at the playhead** (`d87`). **+ Upload** instead inserts into the **main track at the playhead**, pushing the clip later (`d34`). | `d06-deselected`, `d70-media-library`, `d71-media-thumb-hover`, `d87-library-click-adds` |
| Canvas | **Resize** dropdown named by destination: YouTube 16:9; YouTube Shorts, TikTok, Instagram Story & Reels, Spotify Canvas, Facebook Story and Snapchat Story 9:16; Instagram Post Square 1:1; Instagram Post 4:5; plus generic Widescreen, Full Portrait, Square, Landscape 4:3, Portrait 4:5, Landscape Post 5:4, Vertical 2:3, Ultrawide 21:9. **Background**: 20 swatches including gradients, plus a custom colour. | Project-level. Switching to 9:16 letterboxes the clip onto the background colour. | `d20-rail-canvas`, `d21-canvas-resize-menu`, `d23-canvas-9x16`, `d24-canvas-9x16-red-bg` |
| Text | 15 style tiles, each drawn in its own style: Title, Regular, Hand Write, Italic, Underline, UPPERCASE, Rounded (box), BLACK box, WHITE box, Classic, MEME, Spacing, Manuscript, STRICT, Cheerful | Click: a new text row at the playhead, 5 s by default | `d20-rail-text`, `d26a-text-tile-hover`, `d26-text-added` |
| Audio | Stock music: search, a "Music" category dropdown, a filter, tag chips (background music, relaxing, upbeat…). Hovering the thumbnail gives play preview. | Click the row: a **full-length** track under the main track (green waveform row). The project stretched to 2:27. | `d20-rail-audio`, `d72-audio-item-hover`, `d73-music-added` |
| Videos | Stock video grid with search, tags and duration badges | not exercised | `d20-rail-videos` |
| Generate | Video, Image and Audio tabs. Model picker: MiniMax Hailuo 2.3 / Fast, HappyHorse 1.0 I2V/R2V/T2V, Seedance 1.0 Pro Fast / 1.5 Pro, Sora 2 / Pro, Veo 3.1 / Fast / Lite, Wan 2.5 / 2.7 I2V/R2V/T2V. A "Start" frame slot, a prompt, and `6s` and `768p` chips. | not submitted | `d20-rail-generate`, `d22-generate-models` |
| Images | Stock photos with tags | not exercised | `d20-rail-images` |
| Elements | Shapes, Stickers, Emoji and GIFs rows, each with "›" to expand | not exercised | `d20-rail-elements` |
| Subtitles | Auto Subtitles (large card), Upload Subtitles File, Transcribe Manually | not exercised in the editor (see the tool in A13) | `d20-rail-subtitles` |
| Record | Audio, Camera, Screen, Screen & Camera | not exercised | `d20-rail-record` |
| TTS | Language (English US), voice (Christopher), text box | not exercised | `d20-rail-tts` |

### A4. Selected video clip: property tabs
- **Transform** (`d07-clip-transform`): Fill, Fit, Crop. Under **Flip & Rotate**: flip horizontal,
  flip vertical, rotate 90°, and an angle stepper (− 0° +).
- **Crop mode** (`d09-clip-crop`, `d10-crop-1x1`, `d11-crop-applied`): the panel becomes "Crop ✓"
  (tooltip "Done"). Presets: Freeform, Original, 16:9, 9:16, 1:1, 4:3, 4:5, 5:4, 2:3, 21:9. Also a
  fine **Rotate** slider and **Revert to original**. On the stage, a dashed box with the outside
  dimmed. The crop applies to the *layer*, so the 16:9 canvas keeps black bars (`d11`). Ctrl+Z after
  Done reopened crop mode (`d12-after-ctrl-z`).
- **Animations** (`d07-clip-animations`): an In/Out switch, then None, Fade, Blur, Zoom In, Zoom Out,
  Slide Fade, Soft Reveal and Circle as thumbnail tiles.
- **Adjust** (`d07-clip-adjust`): Opacity, Brightness, Contrast, Saturation, Hue, Blur and **Reset**.
  Each slider track is a gradient that *shows the effect*: black to white, grey to red, a rainbow for
  hue.
- **Audio** (`d07-clip-audio`): Volume %, a Noise reduction toggle, Extract audio.
- **Speed** (`d07-clip-speed`): a slider (1) plus chips 0.5, 0.75, 1.25, 1.5, 2.
- **Time** (`d07-clip-time`, `d08-clip-time-timing`). **Duration**: a −/+ stepper on `00:04.00`,
  with + disabled at the source length, and presets 1s, 2s, 3s. **Timing**: Start and End fields,
  each with a clock button. The clock presumably means "use the playhead"; it was not clicked.
- **Image clip** (photo or freeze frame): Transform, Animations, Adjust and Time. No Audio or Speed
  (`d43-image-animations`, `d43-image-adjust`, `d43-image-time`).
- **Music track**: Audio (Volume, **Fade In**, **Fade Out**, Noise reduction), Speed, Time (`d73`).

### A5. Text layer — `d27`–`d33`
- Tabs: **Text, Adjust, Background, Opacity, Time**. There is **no Animations tab** for text, no
  stroke or shadow, and no numeric position.
- **Text:** a textarea that updates the stage *and* the timeline row label live (`d28-text-edited`).
  10 colour swatches plus a custom picker with HSV area, hue, alpha, HEX and RGB
  (`d29c-text-colour-picker`). A font dropdown with search, **Recent** and **Featured** groups; each
  of the 15 names is drawn in its own face (`d29-text-font-list`). Weight from Light to Black
  (`d29b-text-weight-list`). Size − 60 +.
- **Adjust:** line height, letter spacing, align left/centre/right, underline, italic, uppercase
  (`d30-text-adjust`).
- **Background:** none plus 20 swatches, and a Rounding slider (`d30-text-background`,
  `d31-text-bg-black-rounded`).
- **Opacity** and **Time** (`d30-text-opacity`, `d30-text-time`).
- **On stage:** 4 corner and 2 side handles and a rotation handle. Dragging shows a **centre guide**
  and snaps to it (`d32-text-dragging`, `d32b-text-dragging-centre`, `d33-text-moved`). A layer can
  be dragged past the canvas edge.

### A6. Image overlay (photo.png) — `d34`–`d43`
- **+ Upload** put the photo *into the main track* at the playhead as a 5 s clip, before the video,
  with a transition marker at the join (`d34-photo-uploaded`).
- Dragging it upward shows a floating **start-time badge** and a **yellow line** where a new track
  will appear (`d38-drag-photo-to-overlay`). Dropped, it becomes its own row on top. The main track
  keeps a grey gap (`d39-photo-overlay-row`).
- Corner-dragging scales it and dragging moves it, giving a picture-in-picture (`d40-photo-resizing`,
  `d41-photo-moving`, `d42-photo-pip`). Start and End are editable (`d43-image-time`).

### A7. Transitions — `d35`–`d37`
- An "Add transition" marker sits on every join between main-track clips.
- The panel shows 23 thumbnail tiles: None, Crossfade, Fade to Black, Fade to White, Zoom In, Zoom
  Out, Slide ×4, Push ×4, Luma Fade, Blur Crossfade, Split Reveal, Circle Wipe, RGB Split, Whip Pan
  L/R, Soft Rotate, Smooth Zoom, Flash.
- Choosing one **jumps the playhead to the join and plays it**. A floating button opens a
  **Duration** slider (0.4 s by default).

### A8. Timeline
- **Rows:** overlays stack above the main track, newest on top; audio sits below. Text rows are
  grey pills with the text as label. Video rows show a filmstrip. Audio rows are a green waveform
  with the file name (`d73`, `d76`).
- **Selection:** a yellow border with trim bars at both ends (`d47-trim-handle-hover`).
- **Move:** drag. A floating start-time badge (`0.00`) and a ghost of the old position show while
  dragging. It snaps to 0, the playhead and clip edges (`d45-drag-clip-snap`, `d46-clip-moved`).
- **Trim:** drag an end. A badge shows the new time (`3.17`), **the playhead follows the edge and
  the preview shows that exact frame** (`d48-trimming-right`, `d49-trimmed`).
- **Split** (S) at the playhead. It also adds a transition marker at the cut (`d51-split`).
- **Freeze frame** inserts a 5 s still at the playhead and pushes the rest. The still is also saved
  to Media as `clip frozen.png` (`d52-freeze-frame`, `d71`).
- **Duplicate, Bring Forward, Send Backward, Delete** are enabled per context.
- **Gaps:** with ripple off, deleting a middle clip leaves a gap. The gap is selectable (outlined),
  and Delete closes it: 8.17 s became 5.50 s (`d81-delete-leaves-gap`, `d82-gap-selected`,
  `d83-gap-deleted`).
- **Zoom:** − and + (keys `-` and `+`), Fit to screen (`*`). Ruler labels step from 2 s to 1 s as
  you zoom (`d53-zoomed-in`, `d54-zoom-fit`).
- **Playhead:** click the ruler or an empty track to seek (`d50-playhead-seek`).
- **Time display:** `0:00.00 / 0:04.00` in centiseconds on desktop. The phone shows whole seconds
  (`0:02 / 0:07`).
- Project length **grows to the longest item** (stock music: 2:27).
- No custom right-click menu (`d86-right-click-clip`).

### A9. Keyboard (the `hotkeysMap` in the editor bundle `DDzej5uS.js`)
- Transport: `Space` play/pause; `Home` and `End` seek to start or end; `J` and `L` skip back or
  forward; `F` fullscreen preview, exited with `Esc` or `F` (`d89-fullscreen-preview`).
- Editing: `S` split; `D` duplicate; `Delete` or `Backspace` deletes the element *or the selected
  gap*; `Mod+↑` / `Mod+↓` bring forward / send backward.
- Selection: `Shift+N` / `Shift+P` go to the next / previous element; `Esc` deselects.
- Nudge: arrow keys move the selected layer **on the stage**, not the playhead. `Shift` + arrow
  moves further.
- Undo `Mod+Z`; redo `Mod+Shift+Z` or `Mod+Y`. Zoom: `+`, `−`, `*`.
- Space and Home/End were verified live. The rest are read from the bundle.
- There is **no on-screen cheat sheet**: `?` does nothing (`d55-key-question`).

### A10. Export — `d60`–`d69`, `p40`
- **Dialog:** a panel at the top right on desktop (`d60-export-dialog`), a full-screen list with
  **Continue** pinned at the bottom on phone (`p40-export-dialog`).
- **Options:** 480p "Standard quality" (**selected by default**), 720p "Standard quality, HD",
  1080p "High quality, FHD" 👑, 2160p "High quality, 4K" 👑, and GIF "For projects up to 30
  seconds". There is no format, fps or bitrate choice.
- **Paywall placement:** the crowned tiers *can be selected* (`d61-export-1080p-click`). The wall
  only appears after **Continue**: a "Sign in to continue" modal with Google, Apple or Email
  (`d62-export-1080p-continue`). I stopped there.
- **Free path:** 480p → Continue goes to `/export-progress`. A full page with a percent ring,
  "Exporting… We are working on it, give us a minute!" and **Cancel** (`d64-export-480p-continue`).
  It took about 5 s for 8 s of video, rendered server-side.
- **Result** (`/export-result`, `d66-export-done`): the player shows a **burned-in "clideo.com"
  watermark** at the bottom right. Next to it: file size and format (388.1 KB / MP4).
  - **Remove watermark** is the *primary blue button, placed above Download*, and leads to sign-in
    (`d68-remove-watermark`).
  - **Download** is a split button (plus Google Drive, Dropbox).
  - **Edit** returns to the project intact (`d69-back-to-edit`).
  - **Continue in …** offers Video editor, Add subtitles and Compress video; "…" leads to sign-in
    (`d67-continue-in-more`).
  - A Trustpilot "Like Clideo? Rate us" card.
- **Persistence:** the anonymous project lives server-side under `/editor/<uuid>`. localStorage
  holds only flags. There is no visible "saved" state.

### A11. AI Agent (inside the editor)
- A chat panel with chips. A chip runs straight away (`d74`).
- Tool use shows as a step card ("✓ Music added") between two assistant sentences (`d76`).
- It edits the timeline directly with no preview or confirmation. The result is undoable with
  Ctrl+Z.
- It is **absent from the phone bottom bar** (`p01`).

### A12. Phone layout — `p00`–`p51`
- **Layout:** the header has back, undo, redo and an export icon. The preview is on top. A
  transport row (time, play, fullscreen) sits under it, then the timeline toolbar (settings, zoom −
  and +, fit), then the timeline. At the bottom: a blue **+** and a **horizontally scrolling
  category bar** (`p01-empty-editor`, `p30-category-bar`).
- **Selecting a clip swaps the bottom bar** for a scrolling **contextual toolbar**: ‹ back, Split,
  Adjust, Animations, Audio, Speed, Delete, Transform, Freeze, Time, Duplicate, Front, Back
  (`p02-editor-loaded`, `p20-timeline-selected`).
- **Panels follow two rules.**
  - Tools that change the preview (Adjust, Speed, Transform, Time, Animations, Audio, Canvas) open
    **in place of the timeline**, with a title and a ⌄ collapse. **The preview stays visible**
    (`p1x-tool-adjust`, `p1x-tool-speed`, `p1x-tool-transform`, `p1x-tool-time`,
    `p1x-tool-animations`, `p1x-tool-audio`, `p51-cat-canvas`).
  - **Asset libraries** such as the Text presets open **full screen** with a ✕ (`p31-cat-text`).
- Selecting a text layer opens its settings straight away, with icon tabs: text, adjust sliders,
  background bucket, opacity (`p33-text-selected`).
- **Touch trim** works. You drag the end bar and get the same time badge (`2.45`), the playhead
  follows and the preview shows the out-point frame (`p21-touch-trimming`, `p22-touch-trimmed`).
  The bars are thin: about 10 px visible, 18 px hit area.
- New text lands at the playhead, **even past the end of the video** (`p32-text-added`).

### A13. Tools menu and single-purpose tools — `t00`–`t53`
- **Nav "Tools" menu** (`t00-tools-menu`): Compress video, Add subtitles, Video Translator, Text to
  Speech, Screen recorder, Resize video, GIF maker, Video Converter, All tools.
- **`/tools` (about 45 pages):**
  - Editing: Video editor, Add subtitles, Compress, Resize, Cut, Meme maker, Crop, Merge, Speed,
    Video maker, Slideshow maker, GIF maker, Rotate, Add music, Loop, Split, Flip, Reverse, Mute,
    Stop motion, Filter, Adjust, GIF editor.
  - Recorders: Audio, Screen, Presentation, Camera.
  - AI and conversion: Auto subtitle generator, DPI converter, Cut audio, Merge audio, Video, Audio
    and Image Converter, Video and Audio Translator.
  - Overlays: Add Text, Stickers, Emojis, Shapes, Photo and Image to Video; Text to Speech.
- **The shared single-purpose shell** is simpler than the full editor: back arrow, preview with
  play, one settings column on the right, a filmstrip at the bottom, and a bottom bar with
  **"Final output — <length>"**, **"Format — MP4"** (26 containers) and **Export**.
- **Cut** (`t12-cut-editor`, `t13-cut-delete-selected`): a full-width filmstrip with a yellow
  **selection window**, pre-set to the middle (1.20–2.80 s); the outside is dimmed.
  - A radio for **Extract Selected / Delete Selected**. Delete *flips the dimming* so the kept parts
    are bright.
  - "Cut from, sec: [00:01.20] to [00:02.80]".
  - **Fade in / Fade out** (extract) or **Crossfade** (delete).
  - **Final output** updates live: 00:01 for extract, 00:02 for delete.
- **Merge** (`t22-merge-editor`, `t23-merge-item-hover`): the items as filmstrip blocks with a name
  chip and a hover ✕, plus an **"Add more videos"** dashed drop zone. Options:
  - Crop: **"Fit with border"**, with 1:1, 16:9, 9:16, 5:4.
  - **"Image Duration: 2s — Applies to all images"**, **Crossfade**, **Add audio**.
  - Bottom bar: Final output 00:06, **Video size 320×180**, Format.
- **Speed** (`t30-speed-editor`): a slider plus a 0.25×–2× grid, starting at 1.25×. **"Mute video"
  is ticked by default.** Final output is recomputed (00:03).
- **Add subtitles** (`t50`–`t53`): first a choice screen with **Auto subtitles / Add manually /
  Upload .SRT**. The manual editor has:
  - A list of cues, each with start and end fields, text and ⊕ insert-after.
  - Add subtitle, **Translate**, TXT and SRT download, and delete all.
  - A cue lane on a small timeline.
  - A **Styles** tab with 6 preset caption looks, plus font, size, colour, background and alignment.
- **Add Text to Video** (`t40-add-text-landing`): an SEO page whose "Get started" just opens the
  full editor. There is no dedicated flow.

### A14. Other states
- **Loading:** the preview is instant after upload; the agent shows "Thinking…"; export shows a
  percent ring.
- **Fullscreen preview** (`F`): full-bleed with a thin transport bar (`d89`).
- **Undo** covers canvas, crop and transitions (`d12`, `d25-undo-back-16x9`).
- Tooltips on every icon button.
- **Error states:** not exercised (see the perimeter at the top).

---

## (b) Best things to take (ranked)

**Aura today**, read from `web/src/mediaEdit/` on 2026-09-19:
- One clip at a time. Tools: Trim, Crop (ratio presets), Rotate 90°, Audio (mute).
- A filmstrip with two handles: 44 px hit areas, keyboard sliders at ±0.1 s and ±1 s. Start and End
  fields, "Play the selection", an output-size readout, and Save → Mediabunny `Conversion` →
  download.
- **Missing:** a playhead on the filmstrip, a preview that follows the handle while dragging, and an
  output-length readout.
- Photo editor: Filerobot, with Save to Library through `uploadStudioFrame`.

**Engine**, from reading `node_modules/mediabunny/dist/mediabunny.d.ts` (v1.58.1), not from running
it:
- `Conversion` takes `trim`, `crop`, `rotate`, `frameRate`, audio `discard`.
- It also takes per-sample **`process`** hooks for video (d.ts ~L1336–1349: "overlays, colour
  transformations, or timestamp modifications") and for audio.
- So single-clip compositing looks reachable through `process`. Joining several inputs is not a
  `Conversion` option, since it takes one `Input`.

The ideas are ranked by value to an operator cutting short AI generations (5–10 s shots with bad
edges, glitches in the middle, and the wrong aspect for the destination).

1. **The preview follows the trim handle, with a time badge on the handle and a playhead on the
   filmstrip.** Seeing the exact frame you cut at is the whole job with AI clips, whose first and
   last frames often morph. Clideo: `d48-trimming-right`, `p21-touch-trimming`. **[UX pattern
   only]**
2. **"Set Start/End from the playhead", click-to-seek on the filmstrip, and transport keys**
   (`Space`, `Home`/`End`, `J`/`L`, plus `I`/`O` for in and out), with a visible shortcut hint,
   which Clideo lacks. Clideo: clock buttons in `d08-clip-time-timing`, `d50-playhead-seek`, the
   bundle hotkey map. **[UX pattern only]**
3. **An always-visible output summary beside Save: final length · px size · format**, updating live
   as you trim or crop. Aura shows only the size. Clideo: `t12-cut-editor`, `t22-merge-editor`.
   **[UX pattern only]**
4. **Save the current frame as an image to the Library.** Clideo's Freeze frame leaves a PNG in the
   media library. For Aura it serves as a thumbnail, a start or end frame for the next I2V
   generation, or a hand-off to the photo editor. `uploadStudioFrame` already exists; the frame
   comes from the `<video>` or a `CanvasSink`. Clideo: `d52-freeze-frame`,
   `d71-media-thumb-hover`. **[UX pattern only]**
5. **Cut out a middle segment ("Delete selected")**, with inverted dimming so the kept parts read as
   kept, and an optional crossfade over the join. This is the most common fix for a mid-clip AI
   glitch. Clideo: `t13-cut-delete-selected`. **[needs new engine capability: multi-segment
   concat (two ranges into one output)]**
6. **Aspect presets named by destination** (TikTok / Reels 9:16, Instagram Post 4:5, YouTube 16:9),
   with **Fit-with-background** (pad onto a colour or blur) as well as **Fill** (crop). Aura's crop
   can only cut away pixels. Clideo: `d21-canvas-resize-menu`, `d24-canvas-9x16-red-bg`, Merge's
   "Fit with border" in `t22`. **[needs new engine capability: compositing (pad onto a canvas via
   `process`)]**
7. **A title or caption layer**: 4–6 style presets (Title, black box, white box, rounded), direct
   manipulation on the stage (drag, corner scale, rotation handle, centre snap guide) and a
   start/end timing. The tile-per-style picker is the fast part; skip the 15-font sprawl. Clideo:
   `d20-rail-text`, `d27-text-selected`, `d32b-text-dragging-centre`,
   `d31-text-bg-black-rounded`. **[needs new engine capability: text rendering + compositing]**
8. **One-tap fade in and out**: video from or to black, audio gain ramps. Clideo:
   `d07-clip-animations`, `d73-music-added` (Fade In/Out), `t12-cut-editor` checkboxes. **[needs
   new engine capability: compositing + audio `process` gain]**
9. **Merge several clips**: an ordered filmstrip with name chips, "Add more", ✕ remove,
   fit-with-border when sizes differ, one crossfade toggle, and one duration for stills. AI work
   produces many 5–8 s shots that belong together. Clideo: `t22-merge-editor`,
   `t23-merge-item-hover`. **[needs new engine capability: multi-clip concat + scaling]**
10. **Speed presets (0.5×–2×) with the output length recomputed, and an explicit audio choice**
    (keep with pitch correction, or drop). Clideo: `d07-clip-speed`, `t30-speed-editor`. **[needs
    new engine capability: speed (timestamp retiming; audio time-stretch or drop)]**
11. **An image or logo overlay** (watermark, picture-in-picture) placed on the stage with handles.
    Clideo: `d38`–`d42`. **[needs new engine capability: compositing]**
12. **Adjust sliders whose tracks show the effect** (brightness black to white, saturation grey to
    colour, hue rainbow), plus Reset. Clideo: `d07-clip-adjust`. **[needs new engine capability:
    colour filters via `process`]**
13. **Phone rules.** Selecting something swaps in a contextual tool bar. Tools that change the
    picture open *in place under the preview*. Libraries open full screen. Export is a full-screen
    list with a pinned CTA. Aura's 44 px handles already beat Clideo's 18 px ones; keep them.
    Clideo: `p02-editor-loaded`, `p1x-tool-adjust`, `p31-cat-text`, `p40-export-dialog`. **[UX
    pattern only]**
14. **Next steps after Save** (Clideo's "Continue in…"): Save to Library, Send to chat, Edit again,
    Open frame in photo editor. Add a whole-window drop overlay for adding media. Clideo:
    `d66-export-done`, `d84-dragover-timeline`. **[UX pattern only]**
15. **Agent-driven edits with visible receipts.** Aura is agent-first: expose trim, crop, aspect and
    caption as agent tools, and show each applied step as an undoable card ("✓ Trimmed to
    0.4–3.8 s"). Unlike Clideo, show a preview or confirm before the change lands. Clideo:
    `d76-ai-agent-reply`. **[needs new engine capability: editor operations as an agent tool
    surface]**

## (c) Do not copy

- **A watermark on the free export, and "Remove watermark" as the primary blue button above
  Download.** The main call to action is the upsell, not the file (`d66-export-done`,
  `d68-remove-watermark`).
- **Premium options that can be selected and are only blocked after Continue.** 1080p and 4K carry a
  small crown, are selectable, and only then hit a sign-in wall (`d61`, `d62`). The header crown and
  "Continue in … more" lead to the same wall (`d67`, `d75`).
- **The lowest quality (480p) pre-selected.** Aura has no tiers: default to the source resolution.
- **A Trustpilot "Rate us" card** on the result page (`d66`).
- **Clutter.** Half the rail is stock or AI marketplaces (Videos, Images, Elements/GIFs/Stickers,
  Generate with 18 models, TTS, Record) that have nothing to do with fixing the clip in hand. On top
  of that: 15 text presets, 23 transitions, 8 animations, 45 near-duplicate tool pages. Some tool
  pages are only SEO shells, e.g. "Add Text to Video" just opens the editor (`t40`).
- **Inconsistent insertion that silently changes the output length.**
  - "+ Upload" inserts into the main track and pushes the clip (`d34`).
  - A library click adds an overlay instead (`d87`).
  - Text lands at the playhead even past the end of the video (`p32`).
  - Stock music and the AI Agent add full-length tracks that stretch 8 s to 2:27 or 2:09 (`d73`,
    `d76`).
- **A "crop" that does not change the output frame.** Clideo's crop affects the layer, so the fixed
  16:9 canvas gets black bars (`d11`). Undo after Done reopened crop mode (`d12`).
- **AI chips that fire instantly** and edit the timeline with no preview (`d74`, `d76`).
- **Pre-ticked destructive defaults**: "Mute video" is on by default in Speed (`t30`).
- **Icon-only toolbars with no shortcut sheet** (`?` does nothing, `d55`). **Thin touch handles**
  (18 px hit area, `p20`).
- **Whole-second time on phone** (`0:02 / 0:07`, `p22`). That is too coarse for frame-accurate AI
  trims.
- **An anonymous server-side project with no saved indicator**, named by the date (`d03`, `d88`).
