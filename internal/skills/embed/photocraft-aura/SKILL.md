---
name: photocraft-aura
description: "Edit photos and images (JPEG, PNG, TIFF, WebP, PSD/PSB with layers) with the photocraft MCP server in your box — resize, crop, rotate, levels, curves, brightness/contrast, hue/saturation, filters, layers and text, export to any format keeping EXIF. Use it to prepare a photo for a document or a site, fix exposure, crop, or read and convert a layered PSD. Italian triggers: «ridimensiona la foto», «ritaglia», «schiarisci», «più contrasto», «converti il PSD», «esporta in JPG». Not for generating images (that is image_generate). Load it BEFORE the first photocraft call."
---

# PhotoCraft in Aura

Your tools are `photocraft__<tool>`. Files: `/workspace` only. Everything is an engine command.

## The round trip

1. `photocraft__doc_open {path}` — returns size and layers.
2. Find the command: `photocraft__command_list {filter}` (each entry has its `params` signature), then
   `photocraft__command_run {id, params}`. The filter matches ids, labels and keywords, not synonyms:
   "resize" finds nothing, "size" finds `image.imageSize`. Measured ids:
   - `image.imageSize {width?, height?, percent?}` — one side keeps the ratio;
   - `image.crop {x, y, width, height}`;
   - `image.adjustments.brightnessContrast {brightness: -150..150, contrast: -50..100}`;
   - `layer.newAdjustmentLayer.levels {...}` for a non-destructive adjustment.
3. `photocraft__render_preview {max_side}` and look at it. The image comes back as a file under `/workspace/mcp-files/` (its path is in the result's `files`): `read_file` that path to see it.
4. `photocraft__doc_export {path, quality}` — format from the extension; EXIF is kept (measured; Pillow drops
   it). `.pcraft` keeps layers. Then `send_file`.

A Smart Object's contents: `photocraft__doc_replace_smart_object {path, layer?}` swaps them for a file under
`/workspace`, keeping its transform and filters.

## Limits (measured 2026-10-18, PhotoCraft 0.10.0, early alpha; the round trip re-run 2026-10-29 on 0.17.0)

- About 1 s for a resize and an adjustment of a 19-megapixel photo, about 330 MB of memory.
- The `ui_*` and `control_call` tools need a desktop app and fail here.
