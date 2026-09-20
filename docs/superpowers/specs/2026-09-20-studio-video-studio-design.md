# Studio Video Studio — design

**Date:** 2026-09-20 (revised the same day, after an adversarial review)
**Status:** approved by the operator in conversation; revised against the review below
**Follows:** `docs/superpowers/specs/2026-09-19-studio-media-editing-design.md` (the single-clip
editors, shipped and verified live on 2026-09-20)

## Goal

A multi-track editor inside Aura's Studio: several clips and images in sequence, text and image
layers over them, exported in the browser, with nothing leaving the origin. Every change is a
command, so the same door serves a person and, in a later cycle, an agent.

## What decided this

Nothing here is a preference. Each line names the file that measured it.

- **The engine is VideoFlow** (`@videoflow/core` + `@videoflow/renderer-browser`, Apache-2.0).
  `.planning/research/2026-09-19-studio-video-editor/engine-inventory.md` compared fourteen
  candidates by licence, React 19 support, export path and network behaviour. VideoFlow imports
  mediabunny — the library the single-clip editor already ships — so both editors share one
  decoder and one muxer. Remotion, Twick, OpenVideo and VideoFlow's own React editor are not OSI
  for a company, and most phone home: Remotion posts a usage point on every render, OpenVideo posts
  the hostname.
- **VideoFlow works here, with four rules** (`.planning/spikes/107-studio-timeline-editor/`): a
  same-origin font loader (26 off-origin requests without it, zero with it), a `+0.1 ms` nudge on
  `sourceStart` (without it 10 frames of 96 repeat the previous one), the per-instance render API,
  and a build stub for its 147 KB font list. A 4 s composition with a text layer and a moving image
  renders in 0.4–0.8 s.
- **The timeline widget is `dnd-timeline`** (MIT, 26 KB): 9 of 9 checks on desktop and on a 390×844
  touch viewport, emulated through CDP — not a real phone.
- **Undo/redo is immer patches** (MIT): one undo step per gesture, including a 30-event drag.
- **The model is multi-track**, the operator's call. The Adobe tour argues scenes suit short AI
  clips better and survive a phone; the cost of lanes is paid in "Lane semantics" below, which is
  where it actually lands, not in a paragraph about screen width.

### What is NOT measured, and is measured first

The review (`scratchpad/fable-spec-review.md`, Fable 5.1, 2026-09-20) found the spike proves less
than this design assumed. Before any of this is built:

- **Several clips with audio were never rendered.** Spike 107 composed ONE video layer. VideoFlow's
  mixer decodes each layer's whole source to PCM (`renderer-browser/audio/mixer.ts:89-104`), and the
  DOM preview re-mixes the entire project on every Play (`DomRenderer.ts:969-982`). A clip split in
  four is four full decodes of one file. **Task 1 of the plan measures it**: two clips plus a split
  plus a muted one, rendered and probed, with the memory watched.
- **The `+0.1 ms` nudge is proven at 24 fps only**, and a phone clip is 30. Same task, same run.
- **`VideoJSON` requires an fps**, which the model below now carries rather than inventing at render
  time.

## Scope

This is **cycle 1 of three**. Drawing it any wider is drawing three cycles and calling them one.

**Cycle 1 — the editor and the file:**

- several clips and images in sequence on the video lane;
- per clip: trim both ends, split at the playhead, remove a middle range, mute, reorder;
- text layers and image layers with their own timing, position, size and an enter/exit animation
  from VideoFlow's transition presets;
- export to MP4, **downloaded** — the same thing today's video editor does;
- the project saved and reopened.

**Cycle 2 — the library door:** the export becomes a Studio library asset. This needs Go: today
`cmd/aura/serve_studio.go:163-171` lists images only (`ListRecentImages`) and finalizes uploads as
`ModalityImage` and nothing else. Until that changes, "save to library" cannot be honest, so cycle 1
does not offer it.

**Cycle 3 — the agent door:** the commands become tools, with the receipts and the preview.

**Out, measured but unbuilt:** speed with pitch preservation (`signalsmith-stretch`, MIT, verified:
8.000 s and 2.000 s with the 440 Hz tone intact), transitions between clips, colour adjustment,
captions, audio ducking, templates.

**Never:** a request that leaves the origin, a watermark, a paywalled export, a stock marketplace in
the rail.

## Where it lives

- **Quick trim stays exactly as it is.** The `Edit` button everywhere it is today keeps opening the
  single-clip editor: it opens in a moment, works on a phone, and its rotate-only export still takes
  the copy path (52–65 ms, measured live).
- **`Open in the editor`** is the new entrance, in the Studio. From the quick editor there is one way
  forward into it, so a trim can grow into a montage.

## The project

Data only: no live objects, no DOM handles, no bytes.

```ts
interface VideoProject {
  readonly id: string;
  readonly name: string;
  readonly size: { readonly width: number; readonly height: number };
  readonly fps: number;                          // VideoJSON needs it; 30 unless a source says otherwise
  readonly sources: readonly ProjectSource[];    // { id, assetId, kind, duration, size, fps }
  readonly tracks: readonly ProjectTrack[];
}

interface VideoItem {                            // the video lane
  readonly id: string;
  readonly sourceId: string;
  readonly duration: number;                     // its own length; position comes from the order
  readonly sourceStart: number;
  readonly muted: boolean;
}

interface OverlayItem {                          // a text or image lane
  readonly id: string;
  readonly anchor: { readonly clipId: string; readonly offset: number };
  readonly duration: number;
  readonly props: Readonly<Record<string, unknown>>;
}
```

A source names an Aura asset id; bytes are fetched when the editor opens and when it renders.

**Where the project is stored:** as an asset of its own — a `.json` document through the presign
and finalize path that already exists, listed by name in the Studio. No new table, no migration, no
route. It is the cheapest home that survives a reload, and it is the same door the export will use
in cycle 2.

## Lane semantics

The review's sharpest finding: without this paragraph two implementers write two editors.

- **The video lane is a sequence, not a canvas.** Clips abut; there are no gaps and no overlaps. A
  clip's position is its index, not a timestamp. `trim`, `split` and `removeRange` therefore ripple:
  every later clip moves, and the project gets shorter or longer.
- **`moveClip` means insert**, before or after another clip — never free positioning. A drag that
  ends over another clip is an insertion at that point, which is why the drop is not a collision.
- **Overlays are anchored to a clip**, `{ clipId, offset }`, and they ripple with it. A title over
  shot 2 stays over shot 2 when shot 1 is trimmed — the property the scenes model would have given
  for free and lanes must be taught.
- **Collisions apply to overlay lanes only**: two titles cannot cover the same instant on one lane.
  A third title needs a third lane, which the editor adds on demand.
- Removing a clip removes the overlays anchored to it, and says so before doing it.

## Commands

Every change is a command: a pure function from project to project, arguments as JSON.

`addClip`, `trimClip`, `splitAt`, `removeRange`, `moveClip`, `setMuted`, `addText`, `addImage`,
`setProperty`, `removeItem`.

Three consumers, which is the point: the UI calls them; history records immer patches, one step per
gesture, because a drag applies on release; and in cycle 3 each one is already a tool with the same
arguments and the same validation.

A command refuses rather than guesses, with a translated sentence: a trim past the source, a split
on a boundary, two overlays on one instant of one lane.

**Live feedback is not a command.** A drag in progress and a scrub are view state; only the release
writes a command. That is the honest version of "everything is a command", and it is the rule that
keeps the UI from mutating around the model.

## Sources that cannot be played

The previous cycle already met this: Firefox drops HEVC silently. VideoFlow is worse — a layer whose
source fails to decode is disabled and the render continues (`initLayers`), so the export is black
frames with no error.

So `addClip` probes first, with the mediabunny code the editor already ships: a source the browser
cannot decode is refused at the door with the same sentence the single-clip editor uses. An asset
deleted under a saved project opens as a missing item, named, and export refuses until it is removed
or replaced.

## Files

New module `web/src/videoStudio/`, beside — not inside — `web/src/mediaEdit/`.

| File | Holds |
|---|---|
| `project.ts` | the types above, the pure queries (total duration, item at a time, ripple arithmetic) |
| `commands.ts` | the commands; split by concern if it approaches 600 lines |
| `history.ts` | immer patches, undo/redo, one step per gesture |
| `videoflow.ts` | the only file that knows VideoFlow: project → `VideoJSON`, the render, the four rules, one decoded buffer per source |
| `Timeline.tsx` | lanes, ruler, playhead, zoom, drag and trim on `dnd-timeline` |
| `Stage.tsx` | the preview and the selection box |
| `Inspector.tsx` | the selected item's properties |
| `VideoStudio.tsx` | the workspace |

**Dependencies to declare** (none of them is in `web/package.json` today, and Konva is only there
transitively through Filerobot): `@videoflow/core`, `@videoflow/renderer-browser`, `dnd-timeline`,
`immer`. **Konva is not used.** Overlays are moved and sized from the inspector's fields and a plain
DOM drag on the stage, which also removes a second live-feedback path.

Aura's rules hold: no file over 600 lines, every string through i18next in English and Italian,
fonts and workers served from the origin.

## Export

The browser renders through VideoFlow, which uses the mediabunny already in the bundle, and the file
is downloaded. Two honest limits:

- a composition is a **re-encode**; the copy path that rotates an MP4 in 52 ms exists only when
  nothing is drawn over the video, which is the quick editor's case;
- a headless render is possible because the project is JSON, but which way it goes — VideoFlow's
  server renderer under Playwright, or the agent writing the project and a browser rendering it —
  is a measurement for the cycle that needs it.

## The phone

Lanes are the harder model on a 390 px screen, and the editor does only what it can do honestly:

- the stage, the inspector and the fields work, so a selected clip can be trimmed and an overlay
  timed without touching the lanes;
- the lanes scroll and their zoom is on buttons. `dnd-timeline` has no pinch gesture, and this spec
  does not promise one;
- touch drag and trim were measured on an emulated viewport, never on a device. Until someone runs
  it on a phone, that is what the verification says;
- the quick editor remains the phone's first answer for one clip, and the entrance says so.

The 44 px touch floor from the previous cycle applies.

## Testing

- **Pure functions (vitest):** every command's edge — a split exactly on a boundary, a trim past the
  source, a ripple that moves the overlays with their clip, an insert at either end, two overlays
  claiming one instant, a drag collapsing into one undo step.
- **Components (vitest + Testing Library):** a drag calls the right command with the right numbers,
  the inspector writes the property it names, the playhead follows. Contrast is checked by the
  automated walk that fixed the photo editor's palette, not by eye.
- **End to end (Playwright):** two clips and a title, exported, then read back with mediabunny —
  duration, size, and the title present in the frames where it belongs; an undecodable source
  refused at `addClip`; and a network log with zero off-origin requests.
- **The measurement that gates the plan:** two clips with audio, one split, one muted, rendered and
  probed, with peak memory recorded.

## What this design does not settle

- Whether a headless render belongs on a server or in a browser someone opens.
- How long a project may get before memory is the limit. The spike measured 4 s of 320×180.
- Speed, transitions, colour and captions: measured or catalogued, deliberately unbuilt.
- Safari and iOS, and touch on a real device.
