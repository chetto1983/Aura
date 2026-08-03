# markitdown-mcp on `Robot.pptx` — what it does, measured

**Date:** 2026-08-03 · **Corpus:** `D:/tmp/baseline-corpus/Robot.pptx` (21 MB, 47 distinct
media files, 72 picture slots) · **Probe:** `D:/tmp/markitdown-probe/`

Run in an isolated container. markitdown-mcp is never mounted into an agent session and
never runs on the host: its own README states it has no authentication and that
`convert_to_markdown` "can be used to read any file that the server's user has access to,
or any data from the network". Only the read-only corpus was visible to it.

---

## 1. The official image cannot describe an image. There is no setting for it.

Verified **inside** `mcp/markitdown:latest`, not from the GitHub tree:

```
markitdown 0.1.6
return MarkItDown(enable_plugins=check_plugins_enabled()).convert_uri(uri).markdown
Installed MarkItDown 3rd-party Plugins:  * No 3rd-party plugins installed.
```

`llm_client` and `llm_model` are the ONLY way markitdown captions an image
(`converters/_llm_caption.py`, `_image_converter.py:69-77`, `_pptx_converter.py:110-144`).
The shipped server passes neither, and exposes no flag and no environment variable for
them. The image sets `MARKITDOWN_ENABLE_PLUGINS=True`, but ships zero plugins, so that
variable does nothing at all.

**To configure vision you must replace the tool.** `MarkItDown.__init__` takes
`llm_client` / `llm_model` / `llm_prompt` through `**kwargs` (`_markitdown.py:149-151`)
and `convert_uri` injects them into each converter (`:588-595`). That is a ~15-line
server keeping the identical one-tool surface — `server_vision.py` in the probe.

Baseline, official image, zero cost: **0.34 s, 25,720 chars, 72 images, 42 of them with
completely empty alt text.** The other 30 carry the deck's own `descr` — which here are
the titles of the web pages the author copied the pictures from ("CNC General Robot
Controller 6 Industrial Robot Arm Similar Kuka…"). Not descriptions of content, but real
routing signal, and free.

## 2. Two upstream defects worth knowing

- `alt_text = "\n".join([llm_description, alt_text]) or shape.name` — joining two empty
  strings yields `"\n"`, which is **truthy**, so the `or shape.name` fallback is
  unreachable. An undescribed picture emits `![](file.jpg)`, never `![Picture 3]`.
- `llm_caption` is wrapped in `except Exception: pass`. **A failing vision call is
  silent**: wrong key, refused image, model down — the document comes out looking fine
  with no description in it and no error anywhere. The markdown cannot tell you whether
  vision ran. Only a call counter can, which is why the probe meters the client.

## 3. GLM-OCR, local — fast, free, and doing a different job than you think

`ggml-org/GLM-OCR-GGUF` on the llama.cpp CUDA server already in the stack: 906 MB + a
mandatory 461 MB mmproj, ~2.7 GB VRAM on the 4 GB A2000, alongside the embedder.

**72 images, 103 s, ~1.4 s/image, ~113 tok/s, €0.00.** Every image got something: empty
alts went 42 → 0.

But its model card is explicit under **Prompt Limited**: it supports exactly two prompt
classes — Document Parsing (`Text Recognition:`, `Formula Recognition:`, `Table
Recognition:`) and Information Extraction against a strict JSON schema. It is a 0.9B
document-OCR model, **not a captioner**. markitdown's default free-form "describe this
image" prompt is outside its documented envelope, so the probe used `Text Recognition:`.

What that produced, over the 24,875 characters it added:

| contribution | images |
|---|---|
| 1–5 chars | **43** (36 of them the single letter `S` — the background logo on every slide) |
| 6–20 chars | 5 |
| 21–80 chars | 13 |
| >80 chars | 11 |

The good half is genuinely good, and it is content no other path recovers — text that
exists only as pixels inside a diagram:

```
Keeping humans safe from the machines, and the machines safe from humans
ABB Remote Control System 1.23 Bits (24MB) Guard Step Request ID of ID …
Flip NONFLIP Orientation of flange surface 4th axis of rotation
Coordinates: X, Y, Z, A, B, C
a) Joint motion. b) Linear motion. c) Arch motion.
```

**And one image blew up.** A dense technical diagram produced **18,144 characters** in a
repetition loop — the word `Layers` 2,585 times. That single image is **73% of everything
GLM-OCR added to the document.** Unguarded, that one picture would dominate the file's
tsvector and its embedding.

## 4. What this means for us

- The official `mcp/markitdown` image is fine as a **format reader** and useless for
  images. Adopting it means adopting the 15-line vision server too, or accepting no
  captions.
- GLM-OCR is the right model for **document-shaped images** — scans, screenshots,
  diagrams with labels — and the wrong one for photographs, where it returns a brand
  watermark. That matches the corpus: the Normattiva decrees and the 830-page manual are
  document-shaped; `Robot.pptx` is a deck of web photos.
- Any adoption needs a **per-image output cap and a repetition guard** before the text
  reaches an index. This is not hypothetical: it happened on the first file tested.
- The deck's own `descr` alt text is free signal already present in 30 of 72 pictures and
  costs nothing. Take it before paying any model.

## 5. Housekeeping found on the way

`aura-markitdown` is still **running** on `127.0.0.1:8083` but is no longer declared in
`compose.yaml` — an orphan left when the old sidecar was removed. Its `/` answers 404.
