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

## 3b. `google/gemma-4-31b-it` on the image that broke GLM-OCR

Same call shape as `_llm_caption.py` builds — a `data:` URI beside the text prompt — so
what comes back is literally what markitdown would put in the alt text.

The image is a screenshot of **ABB RobotStudio**: a 3D robot cell plus a left-hand tree
panel of dozens of near-identical rows. That repetition is what sent a 0.5B OCR decoder
into a loop.

| | GLM-OCR (0.9B, local) | gemma-4-31b-it (OpenRouter) |
|---|---|---|
| output | 18,144 chars, `Layers` × 2,585 | **814 chars** |
| latency | ~1.4 s | 5.9 s |
| tokens | — | 305 prompt / 206 completion |
| content | a repetition loop | scene description **and** UI transcription |

It read the window title, the tab bar, both side panels and the status bar, and it opened
with what the picture actually *is*: *"A screenshot of ABB RobotStudio software showing a
3D simulation of an orange industrial robotic arm on a grey pedestal next to a conveyor
belt system with brown blocks."*

The other regime, the photograph GLM-OCR reduced to `SZGH` (3.1 s, 284 chars): gemma names
the robot and the control cabinet, keeps the `SZGH` watermark, and transcribes the
cabinet's bilingual safety labels — `CAUTION / 运转中 / Running / 报警状态 / Alarm Lamp`.

And the background logo, which GLM-OCR read as the single letter `S` (1.8 s, 287 chars):
*"A logo featuring a green stylized 'S' … next to the text 'SACCHI' … Below, 'A Sonepar
Company'."* That is the document's provenance, and nothing else in the pipeline recovers it.

**Caveat, and it matters: gemma's transcription is not pixel-faithful.** Two runs of the
same screenshot returned `Job: Part Flow` and `Job: Part Transfer`. It reads small text by
inference. Treat its output as description that happens to contain text, never as OCR.

Cost is a non-issue: **$0.005 for the whole deck** at $0.10/$0.34 per Mtok.

## 3c. The real defect is neither model — markitdown captions SLOTS, not images

```
picture slots markitdown will caption : 72
DISTINCT images behind them           : 32
redundant calls                       : 40  (56%)
   36x  ebf3e4e56cc9   <- the SACCHI logo, on nearly every slide
```

**markitdown has no deduplication.** It calls the model once per picture *placement*, so
the same blob is captioned 36 times and 36 identical paragraphs are inserted into the
output. With gemma's ~290-char logo description that is ~10,400 characters — **40% of the
original document, all the same sentence about a logo.**

This is the same failure as GLM-OCR's blowup arriving by a different road, and it is the
more dangerous of the two because the result looks entirely plausible.

### The fix, implemented and measured

It goes in the **client**, not in markitdown. The picture reaches the model as a `data:`
URI inside the messages (`_llm_caption.py` builds it that way), so a shim in front of
`chat.completions.create` can hash it — and that shim has to exist regardless, because the
shipped server passes no client at all. No fork, and it covers every converter at once.

Key = sha256 of (model, prompt text, image data URI). Same run, same file, everything else
unchanged:

```
vision_calls_billed     : 32
calls_avoided_by_cache  : 40
dedup_saving_pct        : 55.6
distinct_images         : 32     <- matches the independent blob-hash count exactly
```

**$0.0019 for the whole deck** (9,660 prompt + 2,859 completion tokens), 150 s.

The same shim carries the two guards the GLM-OCR run showed were needed: collapse any
token repeated more than twice, THEN cap at 1,200 characters — order matters, since a cap
alone still lets a loop spend the whole budget on one word. On this deck exactly one
caption hit the cap. Alt-text length went to max 1,252 / median 232 / **zero empty**.

And the screenshot that produced 18,144 characters of `Layers` now reads:

> *A screenshot of the ABB RobotStudio software displaying a 3D simulation of an industrial
> robotic arm. An orange robotic arm is positioned to pick up a brown box from a conveyor
> belt. The interface includes various toolbars at the top and control panels on the left
> and right sides. **Transcribed Text:** …*

A cross-document cache keyed the same way is the obvious next step: a corporate corpus
repeats one letterhead across hundreds of files, and it would be captioned once, ever.

## 4. What this means for us

- The official `mcp/markitdown` image is fine as a **format reader** and useless for
  images. Adopting it means adopting the 15-line vision server too, or accepting no
  captions.
- **Deduplicate by blob hash before anything else.** It is the largest single defect
  found, it is free to fix, and it cuts the work 56% on this file.
- **A general VLM beats the specialist OCR here, and by a wide margin.** GLM-OCR is
  attractive — local, free, 1.4 s/image — but it is a document parser with a closed prompt
  set, and on a slide deck it returns one letter for a logo and a repetition loop for a
  screenshot. gemma-4-31b-it costs half a cent for the whole deck and returns something
  useful for every image class. Reconsider GLM-OCR for the Normattiva scans and the
  830-page manual, which ARE document-shaped, and measure it there rather than here.
- **Cap output per image and guard against repetition** regardless of model. It happened
  on the first file tested.
- **Never treat a VLM caption as OCR.** Two runs of the same screenshot disagreed on a
  window title. It is description that happens to contain text.
- The deck's own `descr` alt text is free signal already present in 30 of 72 pictures and
  costs nothing. Take it before paying any model.

## 5. Housekeeping found on the way

`aura-markitdown` is still **running** on `127.0.0.1:8083` but is no longer declared in
`compose.yaml` — an orphan left when the old sidecar was removed. Its `/` answers 404.
