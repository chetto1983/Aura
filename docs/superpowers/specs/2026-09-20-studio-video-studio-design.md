# Studio Video Studio — design

**Date:** 2026-09-20
**Status:** approved by the operator in conversation, ready for a plan
**Follows:** `docs/superpowers/specs/2026-09-19-studio-media-editing-design.md` (the single-clip
editors, shipped and verified live on 2026-09-20)

## Goal

A multi-track editor inside Aura's Studio: several clips and images in sequence, text and image
layers over them, exported in the browser, with nothing leaving the origin. Everything the editor
can do is a command, so the same door serves a person and, later, an agent.

## What decided this, and what it cost to find out

Nothing here is a preference. Each line is a measurement, and the file that holds it.

- **The engine is VideoFlow** (`@videoflow/core` + `@videoflow/renderer-browser`, Apache-2.0).
  `.planning/research/2026-09-19-studio-video-editor/engine-inventory.md` compared fourteen
  candidates by licence, React 19 support, export path, network behaviour and what they actually
  do. VideoFlow imports mediabunny — the library the single-clip editor already ships — so the two
  editors share one decoder and one muxer. Remotion, Twick, OpenVideo and VideoFlow's own React
  editor are not OSI for a company, and most of them phone home: Remotion posts a usage point on
  every render, OpenVideo posts the hostname.
- **VideoFlow works here, with four rules** (`.planning/spikes/107-studio-timeline-editor/`):
  a same-origin font loader (its default fetches Google Fonts — 26 off-origin requests without the
  override, zero with it), a `+0.1 ms` nudge on `sourceStart` (without it 10 frames of 96 show the
  previous frame), the per-instance render API, and a build stub for its 147 KB font list. A 4 s
  composition with a text layer and a moving image renders in 0.4–0.8 s.
- **The timeline widget is `dnd-timeline`** (MIT, 26 KB): 9 of 9 checks on desktop and on a
  390×844 touch viewport. `@xzdarcy/react-timeline-editor` (76 KB) ignores touch drag and trim
  until a `touch-action: none` line is added.
- **Undo/redo is immer patches** (MIT): one undo step per gesture, including a 30-event drag;
  `zundo` also passes but needs a pause/restore dance around every gesture.
- **Speed with pitch kept is `signalsmith-stretch`** (MIT): 8.000 s and 2.000 s out of an 8 s clip
  with the 440 Hz tone still at 440.6 / 440.3 Hz. VideoFlow's own `pitch` misses (454 / 505 Hz) and
  `soundtouchjs` (LGPL) lands 10% short. Not in v1 scope, but the answer is measured.
- **The model is multi-track**, the operator's call. The Adobe Express tour
  (`.planning/research/.../adobe-express-tour.md`) argues that scenes suit short AI clips better and
  are the only model that survives a phone; the operator chose Clideo's lane model anyway. The
  consequence is written into "The phone" below rather than argued away.

## Scope

**In, for v1:**

- several clips and images in sequence, on a video lane;
- per clip: trim at both ends, move, split at the playhead, remove a middle range, mute;
- reorder by dragging;
- text layers and image layers on their own lanes, each with its own start and end, position and
  size on the stage, and an enter/exit animation from a short list;
- export to MP4, landing in the Studio library as an ordinary asset;
- the project saved, reopened, and edited again.

**Out, for v1** (measured, planned, not built yet): speed with pitch preservation, transitions and
crossfades, colour adjustment, captions from speech, audio ducking, templates.

**Never:** a request that leaves the origin, a watermark, a paywalled export, or a stock/AI
marketplace in the rail — the three things the Clideo tour lists under "do not copy".

## Where it lives

Two entrances, because the frequent gesture and the rare one are not the same job.

- **Quick trim stays exactly as it is.** The `Edit` button on the Studio stage, on generated
  images, on chat video and on attachments keeps opening today's single-clip editor. It opens in a
  moment, it works on a phone, and its rotate-only export still takes the copy path (52–65 ms,
  measured live on 2026-09-20).
- **`Open in the editor`** is the new entrance, in the Studio, and it opens the multi-track editor
  on a project. From the quick editor there is one way forward into it, so a trim can grow into a
  montage without going back to the library.

## The project

The project is data, and only data: no live objects, no DOM handles, no blobs.

```ts
interface VideoProject {
  readonly id: string;
  readonly name: string;
  readonly size: { readonly width: number; readonly height: number };
  readonly sources: readonly ProjectSource[];   // { id, assetId, kind, duration, size }
  readonly tracks: readonly ProjectTrack[];     // { id, kind: 'video' | 'text' | 'image', items }
}

interface TrackItem {
  readonly id: string;
  readonly sourceId?: string;      // absent on a text item
  readonly start: number;          // seconds on the project timeline
  readonly end: number;
  readonly sourceStart?: number;   // where the clip begins inside its source
  readonly props: Readonly<Record<string, unknown>>; // text, colour, box, animation…
}
```

A source names an Aura asset id. The bytes are fetched when the editor opens and when it renders;
the project never carries them, so it can be stored, re-read and handed to an agent.

## Commands

Every change is a command: a pure function from project to project, with its arguments as JSON.

`addClip`, `trimClip`, `moveClip`, `splitAt`, `removeRange`, `setMuted`, `addText`, `addImage`,
`setProperty`, `removeItem`, `reorderTrack`.

Three consumers share them, which is the point:

1. **The UI** calls a command on every gesture that changes the project.
2. **History** records immer patches per command; a drag is one command, applied on release, so it
   is one undo step. Undo and redo apply the inverse and the forward patch.
3. **The agent**, in the next cycle: each command is already a tool with the same argument shape
   and the same validation the UI gets, so there is no second, weaker door. Each applied command
   shows in the history as an undoable receipt, and a proposed change is previewed before it lands
   — the editor redraws from the project without rendering a file.

A command refuses rather than guesses: a trim past the source's duration, an item dropped on top of
another on the same lane, a split at a boundary. Refusals carry a translated sentence.

## Files

New module `web/src/videoStudio/`, beside — not inside — today's `web/src/mediaEdit/`.

| File | Holds |
|---|---|
| `project.ts` | the types above and the pure queries: total duration, items at a time, lane collisions |
| `commands.ts` | the commands (split per file if they grow past 600 lines) |
| `history.ts` | immer patches, the undo/redo stacks, one step per gesture |
| `videoflow.ts` | the only file that knows VideoFlow: project → composition, the render, and the spike's four rules |
| `Timeline.tsx` | lanes, ruler, playhead, zoom, drag and trim handles on `dnd-timeline` |
| `Stage.tsx` | the preview, with Konva handles for moving and scaling text and image layers |
| `Inspector.tsx` | the selected item's properties |
| `VideoStudio.tsx` | the workspace that holds the three together |

Aura's rules hold: no file over 600 lines, every user-visible string through i18next in English and
Italian, the editor's own fonts and workers served from the origin.

## Export

The browser renders, through VideoFlow's renderer, which uses the mediabunny already in the bundle.
The result is uploaded as an ordinary asset and appears in the Studio library, so it can be the
source of the next project or an attachment in a chat.

Two honest limits, from the spike:

- a composition is a **re-encode**. The copy path — the one that rotates an MP4 in 52 ms — exists
  only when nothing is drawn over the video, which is the quick editor's case, not this one;
- the project being JSON means a render can happen with nobody watching. Which way that goes —
  VideoFlow's server renderer under Playwright, or the agent writing the project and the browser
  rendering it when someone opens it — is a measurement for the cycle that needs it, not a choice
  to make here.

## The phone

Multi-track is the operator's choice and it is the harder one on a 390 px screen. The editor
therefore does on a phone what it can do honestly:

- the stage and the inspector work, and a selected item can be trimmed and moved with the fields;
- the lanes scroll horizontally with pinch zoom, and touch drag works because `dnd-timeline` was
  measured on a touch viewport;
- the quick editor remains the phone's first answer for a single clip, and the entrance says so.

Anything that cannot be made to work with a thumb is desktop-only and says that, rather than
shipping a control nobody can hit. The 44 px touch floor from the previous cycle still applies.

## Testing

- **Pure functions (vitest):** every command's edge — a split exactly on a boundary, a trim past
  the source, a range that falls between two clips, a collision on one lane, a drag that must
  collapse into one undo step. This is where the logic is proven.
- **Components (vitest + Testing Library):** a drag calls the right command with the right numbers,
  the inspector writes the property it names, the playhead follows. Contrast is checked by the same
  automated walk that fixed the photo editor's palette, not by eye.
- **End to end (Playwright):** a real project — two clips and a title — exported, then read back
  with mediabunny: duration, size, and the title present in the frames where it should be. Plus the
  case that protects the product's independence: a network log with zero off-origin requests.
- **One agent-shaped check:** three commands applied through the command layer produce the same
  project as the same three gestures through the UI.

## What this design does not settle

- Whether a headless render belongs on the server or in a browser someone opens.
- How large a project may get before the browser's memory is the limit; the spike measured 4 s
  compositions of a 320×180 clip, nothing more.
- Speed, transitions, colour and captions: measured or catalogued, deliberately unbuilt.
- Safari and iOS, untested here as in the previous cycle.
