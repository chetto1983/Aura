import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { LOCAL_FONTS, toVideoJSON } from '../videoflow';
import type { VideoProject } from '../project';
import { calls, installFakes, project, removeFakes, urls } from './videoflowFakes';

// The shape we hand VideoFlow is the whole contract of this module, so the builder is mocked and
// the calls are recorded: what the adapter says to `addVideo` / `addText` / `addImage` is what
// spike 107 and 108 measured against, and it is what breaks if the adapter drifts. What the export
// then does with the renderer is videoflow_export.test.ts's subject.

vi.mock('@videoflow/core', async () => ({
  default: (await import('./videoflowFakes')).FakeVideoFlow,
}));
vi.mock('@videoflow/renderer-browser', async () => ({
  default: (await import('./videoflowFakes')).FakeRenderer,
}));

beforeEach(installFakes);
afterEach(removeFakes);

describe('toVideoJSON', () => {
  it('places every clip at the start its lane gives it, with the cut nudge', async () => {
    await toVideoJSON(project(), urls);

    expect(calls.videos).toHaveLength(2);
    expect(calls.videos[0]).toMatchObject({
      settings: { startTime: 0, sourceStart: 0.0001, sourceDuration: 4 },
    });
    expect(calls.videos[1]).toMatchObject({
      settings: { startTime: 4, sourceStart: 4.0001, sourceDuration: 3 },
    });
    expect(calls.compiled).toBe(1);
  });

  it('carries mute per clip, in the properties the mixer reads', async () => {
    await toVideoJSON(project(), urls);

    expect(calls.videos[0]?.props.mute).toBe(false);
    expect(calls.videos[1]?.props.mute).toBe(true);
    // `muted` in a layer's SETTINGS is a no-op (spike 108 §6): writing it would read as a mute
    // that never happens.
    expect(calls.videos[1]?.settings).not.toHaveProperty('muted');
  });

  it('mutes a cleaned clip’s picture, whose sound now plays from its cleaned copy', async () => {
    const base = project();
    await toVideoJSON(
      {
        ...base,
        sources: base.sources.map((source) =>
          source.id === 'src-a' ? { ...source, denoisedAssetId: 'asset-a-clean' } : source,
        ),
        video: base.video.map((clip) => (clip.id === 'clip-1' ? { ...clip, denoise: true } : clip)),
      },
      urls,
    );
    expect(calls.videos[0]?.props.mute).toBe(true);
    expect(calls.audios.map((audio) => audio.settings.name)).toEqual(['clip-1#clean']);
  });

  it('fades a cleaned clip’s sound with its picture: its sound layer takes the clip’s transitions', async () => {
    // The mixer fades a layer's volume only through that layer's own transitions (mixer.js
    // applyAudioKeyframes; the `fade` preset multiplies volume): a sound layer without them would
    // cut hard where the picture fades.
    const base = project();
    const [first, second] = base.video;
    if (first === undefined || second === undefined) throw new Error('project fixture lost a clip');
    await toVideoJSON(
      {
        ...base,
        sources: base.sources.map((source) =>
          source.id === 'src-a' ? { ...source, denoisedAssetId: 'asset-a-clean' } : source,
        ),
        video: [
          { ...first, denoise: true, transitionIn: 'fade', transitionInDuration: 0.5 },
          {
            ...second,
            junctionFromClipId: 'clip-1',
            junctionTransition: 'crossfade',
            junctionDuration: 1,
          },
        ],
      },
      urls,
    );
    const sound = calls.audios.find((audio) => audio.settings.name === 'clip-1#clean');
    expect(sound?.settings).toMatchObject({
      transitionIn: { transition: 'fade', duration: 0.5 },
      transitionOut: { transition: 'fade', duration: 1 },
    });
    expect(sound?.settings.transitionIn).toEqual(calls.videos[0]?.settings.transitionIn);
    expect(sound?.settings.transitionOut).toEqual(calls.videos[0]?.settings.transitionOut);
  });

  it('carries the selected clip rotation and fit into VideoFlow, and no static volume', async () => {
    const base = project();
    const first = base.video[0];
    const second = base.video[1];
    if (first === undefined || second === undefined) throw new Error('project fixture lost a clip');
    const styled: VideoProject = {
      ...base,
      video: [
        {
          ...first,
          rotation: 90,
          fit: 'contain',
          volume: 0.4,
          flipX: true,
          brightness: 1.2,
          contrast: 0.8,
          saturation: 1.3,
          hue: 20,
          blur: 0.5,
          animation: 'fadeIn',
          speed: 2,
          transitionIn: 'blurResolve',
          transitionOut: 'glitchResolve',
        },
        second,
      ],
    };
    await toVideoJSON(styled, urls);
    // A static `volume` never reaches the mixer (S1: 0.5 and 1 both measured −24.08 dBFS); the
    // clip's volume is a keyframe in the compiled JSON now (videoflow_audio.test.ts).
    expect(calls.videos[0]?.props).not.toHaveProperty('volume');
    expect(calls.videos[0]?.props).toMatchObject({
      rotation: 90,
      fit: 'contain',
      scale: [-1, 1],
      filterBrightness: 1.2,
      filterContrast: 0.8,
      filterSaturate: 1.3,
      filterHueRotate: 20,
      filterBlur: 0.5,
      opacity: [
        { time: 0, value: 0 },
        { time: 0.5, value: 1 },
      ],
    });
    expect(calls.videos[0]?.settings).toMatchObject({
      speed: 2,
      sourceDuration: 4,
      transitionIn: { transition: 'blurResolve', duration: 1 },
      transitionOut: { transition: 'glitchResolve', duration: 1 },
    });
  });

  it('overlaps adjacent clips and applies one transition to both edges', async () => {
    const base = project();
    const first = base.video[0];
    const second = base.video[1];
    if (first === undefined || second === undefined) throw new Error('project fixture lost a clip');
    const transitioned: VideoProject = {
      ...base,
      video: [
        first,
        {
          ...second,
          junctionFromClipId: 'clip-1',
          junctionTransition: 'crossfade',
          junctionDuration: 1,
        },
      ],
    };
    await toVideoJSON(transitioned, urls);

    expect(calls.videos[0]?.settings).toMatchObject({
      startTime: 0,
      transitionOut: { transition: 'fade', duration: 1 },
    });
    expect(calls.videos[1]?.settings).toMatchObject({
      startTime: 3,
      transitionIn: { transition: 'fade', duration: 1 },
    });
    expect(calls.waits).toEqual([6]);
  });

  it('uses VideoFlow shape layers for fade-to-white without covering overlays', async () => {
    const base = project();
    const first = base.video[0];
    const second = base.video[1];
    if (first === undefined || second === undefined) throw new Error('project fixture lost a clip');
    await toVideoJSON(
      {
        ...base,
        video: [
          first,
          {
            ...second,
            junctionFromClipId: 'clip-1',
            junctionTransition: 'fadeWhite',
            junctionDuration: 1,
          },
        ],
      },
      urls,
    );

    expect(calls.shapes).toHaveLength(1);
    expect(calls.shapes[0]).toMatchObject({
      props: {
        fill: '#ffffff',
        opacity: [
          { time: 0, value: 0 },
          { time: 0.5, value: 1 },
          { time: 1, value: 0 },
        ],
      },
      settings: { startTime: 3, sourceDuration: 1, shapeType: 'rectangle' },
    });
    expect(calls.texts[0]?.settings).toMatchObject({ startTime: 3.5 });
  });

  it('resolves an overlay against its clip, not against the timeline', async () => {
    await toVideoJSON(project(), urls);

    expect(calls.texts).toHaveLength(1);
    expect(calls.texts[0]).toMatchObject({
      props: { text: 'AURA' },
      settings: { startTime: 4.5, sourceDuration: 1 },
    });
  });

  it('clips an overlay to the end of the clip it hangs on', async () => {
    const base = project();
    const overrun: VideoProject = {
      ...base,
      overlays: [
        {
          id: 'lane-1',
          items: [
            {
              id: 'title',
              kind: 'text',
              anchor: { clipId: 'clip-2', offset: 2 },
              duration: 10,
              props: { text: 'AURA' },
            },
          ],
        },
      ],
    };
    await toVideoJSON(overrun, urls);

    expect(calls.texts[0]?.settings).toMatchObject({ startTime: 6, sourceDuration: 1 });
  });

  it('resolves every source through the cockpit asset route, and nothing off-origin', async () => {
    await toVideoJSON(project(), urls);

    const sources = [...calls.videos, ...calls.images].map((call) => call.settings.source);
    expect(sources).toEqual(['/api/assets/asset-a/download', '/api/assets/asset-a/download']);
    for (const href of Object.values(LOCAL_FONTS)) expect(href.startsWith('/')).toBe(true);
  });

  it('gives a still in the video lane an image layer, with no source window to nudge', async () => {
    const base = project();
    const withStill: VideoProject = {
      ...base,
      video: [
        ...base.video,
        { id: 'clip-3', sourceId: 'src-still', duration: 2, sourceStart: 0, muted: true },
      ],
    };
    await toVideoJSON(withStill, urls);

    expect(calls.videos).toHaveLength(2);
    expect(calls.images).toHaveLength(1);
    expect(calls.images[0]?.settings).toMatchObject({
      source: '/api/assets/asset-still/download',
      startTime: 7,
      sourceDuration: 2,
    });
    expect(calls.images[0]?.settings).not.toHaveProperty('sourceStart');
  });

  it('refuses a clip whose source the project has lost', async () => {
    const base = project();
    const orphan: VideoProject = {
      ...base,
      video: [{ id: 'clip-1', sourceId: 'gone', duration: 4, sourceStart: 0, muted: false }],
    };

    await expect(toVideoJSON(orphan, urls)).rejects.toThrow(/gone/);
  });
});
