// videoflow_keyframes.ts — every property the Studio animates, moved to where VideoFlow's renderer
// reads keyframes: the compiled layer's `animations`, on the layer's source clock.
//
// The builder takes a layer's initial properties as values. Handed a keyframe array, `compile()`
// stores the whole array as the value of ONE step keyframe (core dist/VideoFlow.js:629-638) and
// writes a property with one keyframe out as a static value (:856-864). The renderer then reads
// that array as a vector and unit-converts each keyframe object to the number NaN — `opacity`
// declares no unit, so `parseFloat('[object Object]')` is what survives (renderer-browser
// dist/layers/RuntimeBaseLayer.js:311-323, 472-494) — and a NaN opacity draws as full opacity:
// the canvas ignores a NaN `globalAlpha` (LayerRasterizer.js:909), and CSS rejects the
// `opacity: NaN NaN` the DOM path writes (RuntimeBaseLayer.js:770). A clip's fade-out was
// measured that way on an exported frame (spikes/video-mcp-render/FINDINGS.md S1.3: RGB
// (233, 100, 90) where 0.4 over black allows 102); a title's fade and a junction's dip to black
// go through the same path, as videoflow_keyframes.test.ts shows on the renderer's own layers.
//
// The Studio writes these keyframes on the layer's own film clock: seconds since the layer came on
// screen. The renderer looks keyframes up in absolute source seconds (`sourceTimeAtFrame`,
// RuntimeBaseLayer.js:175-186), so each time is carried through the layer's own `sourceStart` and
// `speed`, cut nudge included — the numbers `withVolumes` maps a volume curve through.

import type { VideoJSON } from '@videoflow/core';

type Layer = VideoJSON['layers'][number];

interface Keyframe {
  readonly time: number;
  readonly value: unknown;
}

/** VideoFlow's own test for a keyframed property (core dist/layers/BaseLayer.js:231), made strict:
 *  a vector such as `scale: [-1, 1]` is an array too, and stays a value. */
function isKeyframes(value: unknown): value is readonly Keyframe[] {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.every(
      (frame: unknown) =>
        typeof frame === 'object' &&
        frame !== null &&
        'value' in frame &&
        'time' in frame &&
        typeof frame.time === 'number',
    )
  );
}

function animated(layer: Layer): Layer {
  const entries = Object.entries(layer.properties);
  const keyed = entries.filter((entry): entry is [string, readonly Keyframe[]] =>
    isKeyframes(entry[1]),
  );
  if (keyed.length === 0) return layer;
  const sourceStart = layer.settings.sourceStart ?? 0;
  const speed = Math.abs(Number(layer.settings.speed ?? 1)) || 1;
  return {
    ...layer,
    properties: Object.fromEntries(entries.filter(([, value]) => !isKeyframes(value))),
    animations: [
      ...layer.animations,
      ...keyed.map(([property, frames]) => ({
        property,
        keyframes: frames.map((frame) => ({
          time: sourceStart + frame.time * speed,
          value: frame.value,
        })),
      })),
    ],
  };
}

/** The compiled JSON with every keyframed property in its layer's `animations`. */
export function withKeyframes(json: VideoJSON): VideoJSON {
  return { ...json, layers: json.layers.map(animated) };
}
