---
name: media-generation-aura
description: "Generate or edit images and generate videos with image_generate and video_generate: a picture, photo, logo, icon, illustration, poster or flyer (locandina), banner, an edit of an uploaded or generated image (\"make it night\", \"remove the background\", \"più luminosa\"), a clip, an animation of an image. Italian triggers: \"crea/genera/fammi un'immagine\", \"disegna\", \"fammi un video\", \"anima questa foto/immagine\", \"rendi questa foto...\", \"un logo per\", \"una locandina\", \"varianti\". Not for finding existing images or videos on the web. Load it BEFORE the first image_generate or video_generate call of a request: every generation is billed, and this says which questions to ask (once, with defaults), which not to, and how to write a prompt that works the first time."
---

# Image and video generation

Every generation is billed (collecting a finished video with `job_id` is not), and a clip
usually costs many times an image. A wrong guess pays for a second generation; a needless
question wastes a turn. **Generate at once when a wrong guess is cheap to live with; ask
first when it is not.**

## Generate now, or ask first

Generate now, stating in one line the defaults you chose, when the subject and the purpose
are clear: "a dog chasing its tail" needs no interview.

Ask first — once, in the operator's language — when a wrong guess would throw the result
away:

- the image must contain **exact text** (a name, a date, a slogan) and part of it is missing;
- an **edit** where it is not obvious which image, or what must stay unchanged;
- a **video** with no hint of length or shape — *unless* the operator asked to animate the one
  image in the conversation: then take that image, the shortest duration and no sound, and
  generate;
- "animate this" with **more than one** candidate image;
- a **real person, brand or logo** whose likeness or spelling matters;
- **several variants**: each is a separate billed call, so settle style and background first;
- the **use** decides the shape (story, poster, banner, slide) and was not said.

### How to ask

One message, at most four questions, each with the default you will use if the operator
just says "ok". Never one question per turn.

> Before I generate it (each image is billed): 1) shape — square, horizontal 16:9 or
> vertical 9:16? *(default square)* 2) style — photo or illustration? *(photo)* 3) any text
> in the image? *(none)*

Only ask what is missing:

- **Image**: use (post, story, banner, print, slide); shape (1:1, 16:9, 9:16, 4:3, 3:4, 3:2,
  2:3); style (photo, illustration, 3D, flat...); the exact text and where it goes; for an
  edit, what changes and what stays; background (full scene or plain).
- **Video**: start from an image or from text; length in seconds; shape (16:9, 9:16, 1:1 and
  others); the one main movement; sound or silent (only some models have sound, and it can
  cost more). Never ask about resolution: pass the lowest that fits the use, since the price
  grows with it.

## Writing an image prompt

Open with the operation ("Create", "Edit", "Transform"), then in order: what it is and what it
is for → subject → framing and scale → style, medium and light → text → exclusions. The start
of a prompt weighs most, so the subject never comes last.

A request with several elements reads better as labelled lines; the last one is where drift is
stopped, so never leave it empty:

```
Scene: small florist storefront at blue hour, wet cobblestones
Subject: woman in a navy apron locking the door, half-turned to camera
Details: warm interior glow on the pavement, brushed brass handle, soft shadows
Use: editorial photo
Constraints: only the sign "Florista" as text, no people in the background
```

- **Facts, not praise.** "stunning", "epic", "masterpiece", "8k" and style tags ("luxury
  minimalist") degrade the result; write what they look like — materials, light, surface wear,
  lens feel ("overcast daylight, brushed aluminium, chipped paint, 50mm feel"). Say
  "photorealistic" when that is the goal.
- **Exact colours** as hex: "#0d3d2d deep emerald", not "dark green".
- Put required text in quotes, say where it goes and "exactly once, no other text"; spell
  unusual names letter by letter.
- Phrase exclusions as what is there: "an empty beach with no people", not "no people".

**Edits** go in `reference_asset_ids`. The id is the `asset_id` of an attachment (modality
image) or of an earlier result — nothing else is an id. Say "Change only X", then list what
stays (face and identity, pose, framing, lighting, background, text), and repeat that list
on every follow-up edit or the image drifts. With several references, number them and give
each a role ("image 1 is the person, image 2 the style"). Change one thing per call: a small
edit of a good image is cheaper than a new one.

## Writing a video prompt

One readable shot: **subject and camera first, then the visible action, the setting and the
light; style and quality words last; sound only if wanted**. The first third of the prompt
carries the most weight.

- **One camera move** per clip: static, slow push-in, tracking, orbit, crane or handheld.
  Two moves in one prompt is the most common reason a clip goes wrong.
- **Action verbs**, not adjectives; add what moves in the environment (water, leaves, hair,
  light). A clip that barely moves had too many static descriptions.
- **Name the last frame**: "ends on her face lit by the fridge light" beats "she looks sad".
  The ending is what the model steers toward.
- **Fit the action to the seconds**: about one state change per five seconds. A longer story is
  several clips, not one crowded prompt.
- **No contradictions**: "still pond" with "flowing water", or "close-up" with "wide
  landscape", produce artifacts, because the model follows the strongest signal.
- **Same character in several clips**: repeat the same identity line word for word in every
  prompt (face, hair, clothes, one distinctive item). Clips share no memory.
- **Sound**, when wanted: dialogue in quotes ("She says: 'We're late.'"), then effects and
  ambience.

**Animating an image** (`first_frame_asset_id`) happens only when the operator asks to animate
an image or to start from it. "A video of a boat" is a new clip, even with a boat picture
above it. The image already carries subject, style and light. Describe only **what changes over time** — the subject's motion, the camera's, the
environment's, the pace and how it ends. Re-describing the picture makes the model stop
moving it; one line such as "keep the subject from the first frame" protects the identity.

## Tool rules

- **The operator chooses the model.** Never pick one, name one you were not given, or promise
  a price the result did not report.
- Duration, resolution and ratio are requests: the model's nearest supported value is used,
  and `adjustments` says what changed — tell the operator.
- `last_frame_asset_id` makes the clip end on an image; it needs `first_frame_asset_id`, and a
  model without end frames refuses it (nothing billed).
- `unsupported` about reference images: the model cannot take them, or not that many.
  Nothing was generated or billed. Call again with at most the number it names, keeping the
  ones that matter, or tell the operator to choose a model that accepts references. Never
  drop the references just to get an image out: that is a different image, and it is billed.
- `unsupported` "cannot start from an image": the video model has no image-to-video. Nothing
  was generated. Say so; never resubmit the same request as text-only.
- `outcome_unknown`: the provider gave no usable answer, so the request may have been accepted
  and billed. **Never send it again**; tell the operator what happened.
- `job_failed` saying nothing was sent to the provider: nothing was billed, and one more try is
  fine. If it fails again, stop and tell the operator.
- A result with `delivered` is **already shown**. Do not send it again, search the workspace
  for it or download it; use its `asset_id` only as a reference for the next edit or animation.
- `cost_usd` is the real price of that call — mention it when the operator is deciding whether
  to go on. `null` means unknown, not free.
- `{"status":"in_progress","job_id":...}`: say the video is on its way and **end the turn**.
  Do not poll or submit again; the runtime notice tells you when to collect.
- Never regenerate unasked. Variants are one `image_generate` call each, each billed.
- Real people and brands: only edits the person shown would accept — never sexualised, never
  a minor, never placed somewhere they were not. Describe other companies' logos in words
  rather than reproducing them. If the provider refuses, report it; do not reword around it.

---

Parts of the prompt guidance above (labelled image lines, facts over praise, hex colours, the
weight of the opening, the last frame, action per second, contradictions, the identity line)
are adapted and rewritten from *Visual Skills* by Serge Shima
(https://github.com/smixs/visual-skills), licensed CC BY 4.0
(https://creativecommons.org/licenses/by/4.0/).
