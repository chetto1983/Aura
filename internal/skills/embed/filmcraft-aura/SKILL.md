---
name: filmcraft-aura
description: "Edit and export video with the filmcraft MCP server in your box — import clips, music and pictures, build a sequence, cut, trim, add transitions, titles and captions, mix audio, grade colour, and export an MP4 (H.264) with no human in the loop. Use it whenever the operator wants a video assembled, shortened, re-encoded, captioned or exported. Italian triggers: «monta il video», «taglia», «esporta in MP4», «unisci le clip», «metti la musica», «sottotitoli». Not for generating a clip from a prompt (that is video_generate). Load it BEFORE the first filmcraft call."
---

# FilmCraft in Aura

Your tools are `filmcraft__<tool>`. Everything is a command: `filmcraft__command_list {filter}` lists more
than 650, each with its params; `filmcraft__command_run {id, params}` runs one.

## The round trip

1. `filmcraft__media_import {paths: [absolute paths]}` — returns `items` (their ids).
2. `filmcraft__command_run {id: "file.newSequence", params: {name, fromItem: <id>}}` — or `fromItems: [...]`
   for several clips back to back; `timeline.place {item, track, seconds}` puts one where you want.
3. Look: `filmcraft__render_frame {seconds, max_side}` (the image comes back as a file under `/workspace/mcp-files/` (its path is in the result's `files`): `read_file` that path to see it); `filmcraft__sequence_inspect` for clips and times.
4. Export: `command_run {id: "file.exportMedia", params: {path: "/workspace/...mp4", format: "h264",
   width, height, bitrateMode: "crf", crf: 23, wait: true}}`. Always set `bitrateMode` and `crf`: the
   default bitrate wrote 97 MB for a 52-second 720p trailer (measured). A part only:
   `range: "custom", startSeconds, endSeconds` (measured: the first 10 s of the Sintel trailer at 720p,
   CRF 23, 10.005 s, 1.59 MB).
5. `send_file` delivers it.

## Limits (measured 2026-10-18, FilmCraft 0.9.0; the round trip re-run 2026-10-29 on 0.14.0)

- Speed at the box's 2 CPUs: about 12 frames per second for 720p, so a minute of video takes about two
  minutes; peak memory about 1 GB. On 3 CPUs, 0.14.0 rendered 24 frames per second. The call runs in the background of your turn; tell the operator.
- The server writes only under `/workspace`: anywhere else it gets "Permission denied" (Aura's landrun fence; FilmCraft has no root flags of its own).
- The `ui_*` tools need a desktop app and fail here.
