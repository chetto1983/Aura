---
name: media-generation-aura
description: "Generating or editing images and generating videos with image_generate and video_generate — a picture, a photo, a logo, an illustration, a poster, an edit of an uploaded photo (\"make it night\", \"remove the background\"), a clip, an animation, \"animate this image\", \"make a video of\". Load this BEFORE the first image_generate or video_generate call of a request: every call spends real money, and this is how to ask only what is missing, in one message, and write a prompt the model follows the first time instead of paying for a second try."
---

# Image and video generation

Every `image_generate` and `video_generate` call is billed, and a clip usually costs many
times an image: the video price is per second. A wrong guess costs a whole second generation; a question costs
a few tokens. But a question the request already answered is waste too. The rule is:
**generate at once when a wrong guess is cheap to live with, ask first when it is not.**

## Generate now, or ask first

Generate now, stating the defaults you chose in one line, when the subject and the purpose
are clear. "A dog chasing its tail" needs no interview.

Ask first — once, in the operator's language — when a wrong guess would throw the result
away:

- the image must contain **exact text** (a name, a slogan, a date) and it was not given;
- an **edit**: which image, and what must stay unchanged, is not obvious;
- a **video** with no hint of length or shape, or "animate this" with more than one
  candidate image in the conversation;
- a **real person, brand or logo** whose likeness or spelling matters;
- the **use** decides the shape (story, poster, banner, slide) and was not said.

### How to ask

One message, at most four questions, each with the default you will use if the operator
just says "ok". Never a questionnaire, never one question per turn.

> Before I generate it (each image is billed): 1) shape — square, horizontal 16:9 or
> vertical 9:16? *(default 16:9)* 2) style — photo or illustration? *(photo)* 3) any text in
> the image? *(none)*

### Image checklist — pick only the missing items

| Ask about | Why it changes the result |
|---|---|
| use (post, story, banner, print, slide) | sets the shape and the polish |
| shape: 1:1, 16:9, 9:16, 4:3, 3:4, 3:2, 2:3 | cannot be fixed after generation |
| style: photo, illustration, 3D, flat, watercolor... | the biggest single choice |
| exact text and where it goes | text is the most common failure |
| what to change and what to keep (edits) | an edit without "keep" redraws everything |
| background: full scene or plain | changes the whole composition |

### Video checklist — pick only the missing items

| Ask about | Why it changes the result |
|---|---|
| start from an image, or from text only | image-to-video needs `first_frame_asset_id` |
| length in seconds | the price is per second |
| shape: 16:9, 9:16, 1:1 | cannot be fixed after generation |
| the one main movement (subject and/or camera) | a clip reads as one shot, not a story |
| sound or silent | sound costs more, and not every model has it |

Resolution is rarely worth a question: use the lowest the request allows, because the price
grows with it.

## Writing an image prompt

Describe the picture, not your intention to make one. In this order:

1. **What it is and what it is for** — "a product photo of...", "a flat illustration for a
   blog header".
2. **Subject** — who or what, with the concrete details that identify it.
3. **Composition** — framing and scale: "medium close-up at eye level", "full body, feet
   visible", "subject on the left third".
4. **Style and light** — medium, materials, colors, lighting. Say "photorealistic" when
   that is the goal; mood words alone ("epic", "beautiful") do nothing.
5. **Text** — the exact words in quotes, where they go, and "exactly once, no other text".
   Spell unusual names letter by letter.
6. **Constraints** — what must not appear, phrased concretely: "an empty beach with no
   people" works better than "no people".

**Edits** (`reference_asset_ids`): say "Change only X", then list what stays: the face and
identity, pose, framing, lighting, background, text. Repeat that list on every follow-up
edit, or the image drifts.

**Several references**: number them and give each a role — "Image 1 is the person, image 2
the style, image 3 the background" — then say how they combine.

**Iterating**: change one thing per call, pass the previous result as the reference, and
restate what must stay. A small edit of a good image is cheaper than a new image.

## Writing a video prompt

One readable shot:

**camera + subject + visible action + setting + style and light + sound (only if wanted)**

- **One camera move** per clip: static, slow push-in, tracking, orbit, crane or handheld.
  Two moves in one prompt is the most common reason a clip goes wrong.
- **Action verbs**, not adjectives: a clip that barely moves had too many static
  descriptions and too few verbs. Add what moves in the environment too — water, leaves,
  hair, light.
- **Sound**, when wanted, gets its own words: dialogue in quotes ("She says: 'We're
  late.'"), then effects and ambience.

**Animating an image** (`first_frame_asset_id`): the image already carries the subject, the
style and the light. Describe only **what changes over time** — the subject's motion, the
camera's motion, the environment's motion, the pace and how it ends. Re-describing the
picture makes the model stop moving it.

## Tool rules

- **The operator chooses the model.** Never pick one, name one you were not given, or
  promise a price the result did not report.
- Options are requests: the model's nearest supported value is used. Read `adjustments`
  in the result and tell the operator what changed, such as a different duration or ratio.
- A result with `delivered` is **already shown** to the operator. Do not send it again,
  do not search the workspace for it, do not download it. Use its `asset_id` only as a
  reference for the next edit or animation.
- `cost_usd` is the real price of that call; mention it when the operator is choosing
  whether to go on. `null` means unknown, not free.
- A video that returns `{"status":"in_progress","job_id":...}` is still being made: say
  so and **end the turn**. Do not poll and do not submit again. The runtime wakes the
  conversation when it finishes; then call `video_generate` once with only that `job_id`.
- Never regenerate unasked, and never generate several variants unless asked for them —
  each one is billed.
