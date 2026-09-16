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
- a **video** with no hint of length or shape — *unless* it animates the one image already
  in the conversation: then take that image, the shortest duration and no sound, and generate;
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

Order: what it is and what it is for → subject → framing and scale → style, medium and
light → text → exclusions.

- Say "photorealistic" when that is the goal; mood words alone ("epic") do nothing.
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

One readable shot: **camera + subject + visible action + setting + style and light + sound
(only if wanted)**.

- **One camera move** per clip: static, slow push-in, tracking, orbit, crane or handheld.
  Two moves in one prompt is the most common reason a clip goes wrong.
- **Action verbs**, not adjectives; add what moves in the environment (water, leaves, hair,
  light). A clip that barely moves had too many static descriptions.
- **Sound**, when wanted: dialogue in quotes ("She says: 'We're late.'"), then effects and
  ambience.

**Animating an image** (`first_frame_asset_id`): the image already carries subject, style and
light. Describe only **what changes over time** — the subject's motion, the camera's, the
environment's, the pace and how it ends. Re-describing the picture makes the model stop
moving it.

## Tool rules

- **The operator chooses the model.** Never pick one, name one you were not given, or promise
  a price the result did not report.
- Duration, resolution and ratio are requests: the model's nearest supported value is used,
  and `adjustments` says what changed — tell the operator.
- If `adjustments` says reference images were omitted, or `used.reference_asset_ids` is empty
  after you passed some, **the edit did not happen**: the image is a fresh generation. Say so,
  suggest an image model that accepts references, and do not retry.
- `unsupported` "cannot start from an image": the video model has no image-to-video. Stop and
  say so; never resubmit the same request as text-only.
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
