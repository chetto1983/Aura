# Unified video editor controls

Measured 2026-09-21 against the live editor at `https://clideo.com/editor/` with a real synthetic
MP4 loaded at 1100 x 599. The image editor remains unchanged. Every video entry opens the same
multi-track workspace.

## Measured layout and controls

- 54 px title bar, 70 px tool rail, 370 px contextual panel, fitted preview and lower timeline.
- Selected-video tabs: Transform, Animations, Adjust, Audio when the source has audio, Speed and
  Time.
- Transform: Fill, Fit, Crop presets, horizontal/vertical flip and quarter-turn rotation.
- Animations: In/Out selector and 72 x 89 preset cards. Aura uses the transition presets shipped
  by the installed Apache-2.0 VideoFlow renderers: fade, blur resolve, zoom, slide up/down, glitch,
  wipe and light sweep.
- Adjust: opacity, brightness, contrast, saturation, hue and blur.
- Audio: volume and mute. Speed: 0.25x to 4x with common presets. Time: source start/end and
  remove-range fields.
- Timeline thumbnails, trim handles, playhead, zoom, split, remove, undo/redo, save and export stay
  connected to the existing Aura project/command model.

## Reuse decision

Aura keeps `@videoflow/core`, `@videoflow/renderer-dom`, `@videoflow/renderer-browser`,
`dnd-timeline`, Radix/shadcn controls and Lucide. The first-party
`@videoflow/react-video-editor` was evaluated from repository commit `606cc0b` under
`D:/tmp/videoflow-react-video-editor-reference`; it is source-available under a two-tier license,
not Apache-2.0, so it is not copied or added as a dependency. Its public behavior and panel
contract were used only as reference.

`https://github.com/100mslive/react-native-video-plugin` is MIT but targets React Native plus the
100ms conferencing SDK and implements virtual background/blur, not a web editing timeline. It is
therefore not an Aura dependency. Its blur behavior maps to VideoFlow's existing web filter.

Blender's official video-editing page was used as a professional feature checklist: preview,
waveforms/scopes, audio mixing and scrubbing, multi-track media, speed, adjustment layers,
transitions, keyframes and filters. This change implements the controls supported by Aura's current
browser renderer; it does not claim Blender scopes, masking, 32-track parity or background
segmentation.

`MartinDelophy/ai-video-editor` was inspected at MIT commit `d5e9b3f` under
`D:/tmp/ai-video-editor-reference`. It is an application-level custom timeline rather than a
published React control, so Aura does not add it as a dependency or copy its 3,600-line timeline.
It confirms the same interaction model observed in Clideo: one contiguous Visuals track for video
and still-image clips, repeated still/video thumbnails, timed picture-in-picture rows above it,
independent audio/caption rows, a single playhead crossing every row, and zoom controls outside the
media lane. Aura implements that contract through the already-installed `dnd-timeline`: base media
stays sequential, text/image overlays occupy separate rows, image overlays show their asset
thumbnail, and selection/trim grips are visible without consuming timeline width.

## Evidence and limits

The browser E2E drives crop, rotation, brightness, mute, VideoFlow fade/blur transitions, 2x speed
and the final Time tab on desktop Chrome and mobile Chrome. A separate E2E trims and exports the
result, then probes its duration. The full media-edit suite also re-runs the untouched photo editor.

This proves the four-second 720p fixture and the current browser renderer. It does not establish
long-project memory, real-device thermal behavior, vectorscopes, waveform editing or third-party
stock libraries.
