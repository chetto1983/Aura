import type { VideoJSON } from '@videoflow/core';
import {
  createBuiltinLayerTypeRegistry,
  type ILayerRenderer,
  type RuntimeBaseLayer,
} from '@videoflow/renderer-browser';
import { describe, expect, it } from 'vitest';
import type { VideoItem, VideoProject } from '../project';
import { toVideoJSON } from '../videoflow';

// The compile against the REAL VideoFlow, and every animated layer read back the way its renderer
// reads it before drawing: `getPropertiesAtFrame`, then the transitions — `renderFrame`'s own two
// calls (renderer-browser dist/layers/RuntimeBaseLayer.js). videoflow.test.ts mocks the builder,
// so it proves what we HAND VideoFlow; a fade handed over in a shape VideoFlow does not read passed
// there for as long as it existed (spikes/video-mcp-render/FINDINGS.md S1.3).

const urls = { assetUrl: (id: string) => `/api/assets/${id}/download` };

function film(video: VideoItem[], overlays: VideoProject['overlays'] = []): VideoProject {
  return {
    id: 'p',
    name: 'fades',
    size: { width: 320, height: 180 },
    fps: 30,
    sources: [
      { id: 'src', assetId: 'a', kind: 'video', duration: 60, size: { width: 320, height: 180 } },
    ],
    video,
    overlays,
  };
}

/** A layer of the compiled film as the renderer builds it, from VideoFlow's own registry. */
function runtimeLayer(json: VideoJSON, name: string): RuntimeBaseLayer {
  const layer = json.layers.find((candidate) => candidate.settings.name === name);
  if (layer === undefined) throw new Error(`no layer named ${name}`);
  const registry = createBuiltinLayerTypeRegistry('videoflow_keyframes.test');
  const renderer: ILayerRenderer = {
    layers: [],
    getPropertyDefinition: (type) => registry.getPropertyDefinition(type),
    loadFont: () => Promise.resolve(),
    createRuntimeLayer: (json_) =>
      registry.createRuntimeLayer(json_, json.fps, json.width, json.height, renderer),
  };
  return renderer.createRuntimeLayer(layer);
}

/** The opacity the renderer draws a layer at, `seconds` into the film. */
function opacityAt(json: VideoJSON, name: string, seconds: number): number {
  const layer = runtimeLayer(json, name);
  const frame = Math.round(seconds * json.fps);
  const props = layer.applyTransitions(frame, layer.getPropertiesAtFrame(frame));
  return Number(props.opacity);
}

describe('fades in the compiled film', () => {
  it('fades a clip out over its last half second, from wherever it starts in its source', async () => {
    // The spike's shape: a 21 s clip that crossfades in from the one before, starts 30 s into its
    // source, and fades out. It starts at 20 s, so at 40.8 s it is 0.3 s into its 0.5 s fade.
    const json = await toVideoJSON(
      film([
        { id: 'clip-b', sourceId: 'src', duration: 21, sourceStart: 0, muted: true },
        {
          id: 'clip-c',
          sourceId: 'src',
          duration: 21,
          sourceStart: 30,
          muted: true,
          fadeOut: true,
          junctionFromClipId: 'clip-b',
          junctionTransition: 'crossfade',
          junctionDuration: 1,
        },
      ]),
      urls,
    );

    expect(opacityAt(json, 'clip-c', 40.8)).toBeCloseTo(0.4, 5);
    expect(opacityAt(json, 'clip-c', 40.4)).toBeCloseTo(1, 5);
    // The crossfade into it still works: halfway through the junction it is half there.
    expect(opacityAt(json, 'clip-c', 20.5)).toBeCloseTo(0.5, 5);
  });

  it('fades a sped-up clip over half a second of film, not of source', async () => {
    // 4 s of source at 2× is 2 s on screen: the fade-out runs from 1.5 s to 2 s of film.
    const json = await toVideoJSON(
      film([
        {
          id: 'fast',
          sourceId: 'src',
          duration: 4,
          sourceStart: 1,
          muted: true,
          speed: 2,
          fadeOut: true,
        },
      ]),
      urls,
    );

    expect(opacityAt(json, 'fast', 1.8)).toBeCloseTo(0.4, 5);
    expect(opacityAt(json, 'fast', 1.2)).toBeCloseTo(1, 5);
  });

  it('fades a title in the way its inspector wrote it', async () => {
    const json = await toVideoJSON(
      film(
        [{ id: 'clip-1', sourceId: 'src', duration: 4, sourceStart: 0, muted: true }],
        [
          {
            id: 'lane-1',
            items: [
              {
                id: 'title',
                kind: 'text',
                anchor: { clipId: 'clip-1', offset: 1 },
                duration: 2,
                // Inspector.tsx `fadeFor('fadeIn', 2)`: the window's own clock, 0 at its start.
                props: {
                  text: 'AURA',
                  opacity: [
                    { time: 0, value: 0 },
                    { time: 0.5, value: 1 },
                  ],
                },
              },
            ],
          },
        ],
      ),
      urls,
    );

    // 0.2 s into its 0.5 s fade-in; every instant here is a whole frame at 30 fps.
    expect(opacityAt(json, 'title', 1.2)).toBeCloseTo(0.4, 5);
    expect(opacityAt(json, 'title', 2)).toBeCloseTo(1, 5);
  });

  it('dips to black through a fade-to-black junction', async () => {
    const json = await toVideoJSON(
      film([
        { id: 'clip-1', sourceId: 'src', duration: 4, sourceStart: 0, muted: true },
        {
          id: 'clip-2',
          sourceId: 'src',
          duration: 4,
          sourceStart: 4,
          muted: true,
          junctionFromClipId: 'clip-1',
          junctionTransition: 'fadeBlack',
          junctionDuration: 1,
        },
      ]),
      urls,
    );

    // The junction runs 3 s to 4 s; the wash rises for half of it and peaks in the middle.
    expect(opacityAt(json, 'junction-clip-2', 3.2)).toBeCloseTo(0.4, 5);
    expect(opacityAt(json, 'junction-clip-2', 3.5)).toBeCloseTo(1, 5);
  });

  it('leaves no keyframes in a layer’s static properties, where the renderer cannot read them', async () => {
    const json = await toVideoJSON(
      film([
        {
          id: 'clip-1',
          sourceId: 'src',
          duration: 4,
          sourceStart: 2,
          muted: true,
          fadeIn: true,
          fadeOut: true,
          flipX: true,
        },
      ]),
      urls,
    );
    const [clip] = json.layers;

    expect(clip?.properties).not.toHaveProperty('opacity');
    // A vector is an array too, and stays a value.
    expect(clip?.properties.scale).toEqual([-1, 1]);
    expect(clip?.animations.find((animation) => animation.property === 'opacity')).toEqual({
      property: 'opacity',
      keyframes: [
        { time: expect.closeTo(2.0001, 9) as number, value: 0 },
        { time: expect.closeTo(2.5001, 9) as number, value: 1 },
        { time: expect.closeTo(5.5001, 9) as number, value: 1 },
        { time: expect.closeTo(6.0001, 9) as number, value: 0 },
      ],
    });
  });
});
