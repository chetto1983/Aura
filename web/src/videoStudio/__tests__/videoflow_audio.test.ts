import type VideoFlow from '@videoflow/core';
import type { VideoJSON } from '@videoflow/core';
import { describe, expect, it, vi } from 'vitest';
import type { AudioItem, VideoItem, VideoProject } from '../project';
import { addAudioItems, playsCleaned, unheardSources, withVolumes } from '../videoflow_audio';

// The audio half of the compile: what reaches VideoFlow for each sound, and what is written into
// the compiled JSON for each audible layer — the only place its mixer reads a volume (S1).

const urls = { assetUrl: (id: string) => `/api/assets/${id}/content` };

function project(items: AudioItem[] = [], clip: Partial<VideoItem> = {}): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 30,
    sources: [
      {
        id: 'src-a',
        assetId: 'a',
        kind: 'video',
        duration: 10,
        size: { width: 320, height: 180 },
        hasAudio: true,
      },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      { id: 'src-i', assetId: 'i', kind: 'image', duration: 0, size: { width: 8, height: 6 } },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false, ...clip },
      { id: 'still', sourceId: 'src-i', duration: 2, sourceStart: 0, muted: false, volume: 0.5 },
    ],
    overlays: [],
    audio: [{ id: 'lane', items }],
  };
}

function sound(over: Partial<AudioItem> = {}): AudioItem {
  return {
    id: 'bed',
    sourceId: 'src-m',
    anchor: { clipId: 'clip-1', offset: 1 },
    sourceStart: 2,
    duration: 4,
    volume: 1,
    muted: false,
    ...over,
  };
}

type Layer = VideoJSON['layers'][number];

function layer(name: string, settings: Record<string, unknown> = {}): Layer {
  return {
    id: name,
    type: 'audio',
    settings: { name, enabled: true, startTime: 0, sourceDuration: 4, sourceStart: 0, ...settings },
    properties: {},
    animations: [],
  };
}

function json(...layers: Layer[]): VideoJSON {
  return {
    name: 'demo',
    duration: 6,
    width: 320,
    height: 180,
    fps: 30,
    backgroundColor: '#000',
    layers,
  };
}

function flowSpy() {
  const addAudio = vi.fn();
  return { addAudio, flow: { addAudio } as unknown as VideoFlow };
}

describe('addAudioItems', () => {
  it('adds one audio layer per sound at its window, in source seconds, cut at the film end', () => {
    const { addAudio, flow } = flowSpy();
    // The film is 6 s; the sound starts at 1 s and plays 2 s at double speed.
    addAudioItems(flow, project([sound({ speed: 2 })]), urls);
    expect(addAudio).toHaveBeenCalledWith(
      { mute: false },
      {
        name: 'bed',
        source: '/api/assets/m/content',
        startTime: 1,
        sourceStart: 2,
        sourceDuration: 4,
        speed: 2,
      },
    );
    addAudio.mockClear();
    addAudioItems(flow, project([sound({ duration: 8, sourceStart: 0 })]), urls);
    // Eight source seconds from 1 s run past the film's 6 s: only five of them play.
    expect(addAudio.mock.calls[0]?.[1]).toMatchObject({
      startTime: 1,
      sourceDuration: 5,
      speed: 1,
    });
  });

  it('carries mute where the mixer reads it', () => {
    const { addAudio, flow } = flowSpy();
    addAudioItems(flow, project([sound({ muted: true })]), urls);
    expect(addAudio.mock.calls[0]?.[0]).toEqual({ mute: true });
  });

  it('adds nothing for a sound with no window, and nothing for a project with no lanes', () => {
    const { addAudio, flow } = flowSpy();
    const { audio: _audio, ...legacy } = project();
    addAudioItems(flow, legacy, urls);
    addAudioItems(flow, project([sound({ anchor: { clipId: 'gone', offset: 0 } })]), urls);
    expect(addAudio).not.toHaveBeenCalled();
  });

  it('is loud about a sound whose source the project lost', () => {
    const { flow } = flowSpy();
    expect(() => {
      addAudioItems(flow, project([sound({ sourceId: 'lost' })]), urls);
    }).toThrow(/lost/);
  });
});

describe('withVolumes', () => {
  it('writes a clip volume as a keyframe at the layer own sourceStart, nudge included', () => {
    const out = withVolumes(
      project([], { volume: 0.5 }),
      json(layer('clip-1', { sourceStart: 4.0001 })),
    );
    expect(out.layers[0]?.animations).toEqual([
      { property: 'volume', keyframes: [{ time: 4.0001, value: 0.5 }] },
    ]);
  });

  it('leaves a clip at full volume, a muted clip and a still exactly as compiled', () => {
    const untouched = json(layer('clip-1'), layer('still'));
    expect(withVolumes(project(), untouched)).toEqual(untouched);
    expect(withVolumes(project([], { volume: 0.5, muted: true }), untouched)).toEqual(untouched);
  });

  it('leaves a muted sound alone, and a clip whose source has no sound', () => {
    const silent: VideoProject = {
      ...project([sound({ muted: true, volume: 0.5 })], { volume: 0.5 }),
      sources: project().sources.map((source) =>
        source.id === 'src-a' ? { ...source, hasAudio: false } : source,
      ),
    };
    const compiled = json(layer('clip-1'), layer('bed'));
    expect(withVolumes(silent, compiled)).toEqual(compiled);
  });

  it('writes a sound fade in source seconds read off the layer settings', () => {
    const out = withVolumes(
      project([sound({ fadeIn: 1 })]),
      json(layer('bed', { sourceStart: 2, speed: 2, sourceDuration: 4 })),
    );
    const frames = out.layers[0]?.animations[0]?.keyframes ?? [];
    expect(frames[0]).toEqual({ time: 2, value: 0 });
    // The fade lasts one timeline second: two source seconds at double speed.
    expect(frames.at(-1)?.time).toBeCloseTo(4, 6);
  });

  it('replaces a volume animation instead of adding a second, and leaves nameless layers alone', () => {
    const compiled = json(
      {
        ...layer('clip-1'),
        animations: [
          { property: 'volume', keyframes: [{ time: 0, value: 1 }] },
          { property: 'pan', keyframes: [] },
        ],
      },
      { ...layer('wash'), settings: { enabled: true, startTime: 0, sourceDuration: 1 } },
    );
    const out = withVolumes(project([], { volume: 0.25 }), compiled);
    expect(out.layers[0]?.animations.map((animation) => animation.property)).toEqual([
      'pan',
      'volume',
    ]);
    expect(out.layers[1]).toEqual(compiled.layers[1]);
  });
});

/** The project with its sources cleaned once: both have a denoised copy on record. */
function cleaned(items: AudioItem[] = [], clip: Partial<VideoItem> = {}): VideoProject {
  const base = project(items, clip);
  return {
    ...base,
    sources: base.sources.map((source) =>
      source.id === 'src-a' || source.id === 'src-m'
        ? { ...source, denoisedAssetId: `${source.assetId}-clean` }
        : source,
    ),
  };
}

describe('noise reduction in the compile', () => {
  it('plays a denoised sound from its cleaned copy, and an undenoised one from its source', () => {
    const { addAudio, flow } = flowSpy();
    addAudioItems(flow, cleaned([sound({ denoise: true })]), urls);
    expect(addAudio.mock.calls[0]?.[1]).toMatchObject({
      name: 'bed',
      source: '/api/assets/m-clean/content',
    });
    addAudio.mockClear();
    addAudioItems(flow, cleaned([sound()]), urls);
    expect(addAudio.mock.calls[0]?.[1]).toMatchObject({ source: '/api/assets/m/content' });
  });

  it('plays a sound whose cleaning never finished from its source', () => {
    const { addAudio, flow } = flowSpy();
    addAudioItems(flow, project([sound({ denoise: true })]), urls);
    expect(addAudio.mock.calls[0]?.[1]).toMatchObject({ source: '/api/assets/m/content' });
  });

  it('gives a cleaned clip a sound layer of its own, on the clip’s window', () => {
    const { addAudio, flow } = flowSpy();
    addAudioItems(flow, cleaned([], { denoise: true, sourceStart: 2, speed: 2 }), urls);
    expect(addAudio).toHaveBeenCalledWith(
      { mute: false },
      {
        name: 'clip-1#clean',
        source: '/api/assets/a-clean/content',
        startTime: 0,
        sourceStart: 2,
        sourceDuration: 4,
        speed: 2,
      },
    );
  });

  it('knows which clips play their cleaned copy', () => {
    const base = cleaned([], { denoise: true });
    const [clip, still] = base.video;
    if (clip === undefined || still === undefined) throw new Error('fixture');
    expect(playsCleaned(base, clip)).toBe(true);
    expect(playsCleaned(base, { ...clip, muted: true })).toBe(false);
    expect(playsCleaned(base, { ...clip, denoise: false })).toBe(false);
    expect(playsCleaned(project([], { denoise: true }), clip)).toBe(false);
    expect(playsCleaned(base, { ...still, denoise: true })).toBe(false);
  });

  it('moves a cleaned clip’s volume onto its sound layer, and leaves the muted picture alone', () => {
    const out = withVolumes(
      cleaned([], { denoise: true, volume: 0.5 }),
      json(layer('clip-1', { sourceStart: 0.0001 }), layer('clip-1#clean', { sourceStart: 0 })),
    );
    expect(out.layers[0]?.animations).toEqual([]);
    expect(out.layers[1]?.animations).toEqual([
      { property: 'volume', keyframes: [{ time: 0, value: 0.5 }] },
    ]);
  });
});

/** The fixture with speech heard in the clip's source and ducking on the bed. */
function ducked(over: Partial<AudioItem> = {}, clip: Partial<VideoItem> = {}): VideoProject {
  const base = project([sound({ ducking: { amountDb: -12, ramp: 0.5 }, ...over })], clip);
  return {
    ...base,
    sources: base.sources.map((source) =>
      source.id === 'src-a' ? { ...source, speech: [[1, 2]] } : source,
    ),
  };
}

function volumeFrames(out: VideoJSON) {
  return (
    out.layers[0]?.animations.find((animation) => animation.property === 'volume')?.keyframes ?? []
  );
}

describe('ducking in the compile', () => {
  const floor = 10 ** (-12 / 20);

  it('lowers a sound under the speech the clips carry, on the sound’s own clock', () => {
    // The clip's speech is 1–2 s of the film; the bed starts at 1 s, so it is ducked from its first
    // instant for one second, then comes back over half a second.
    const out = withVolumes(ducked(), json(layer('bed', { sourceStart: 2, sourceDuration: 4 })));
    const keyframes = volumeFrames(out);
    expect(keyframes[0]?.time).toBe(2);
    expect(keyframes[0]?.value).toBeCloseTo(floor, 6);
    const halfway = keyframes.find((keyframe) => Math.abs(keyframe.time - 3.25) < 1e-6);
    expect(halfway?.value).toBeCloseTo(1 - 0.5 * (1 - floor), 6);
    expect(keyframes.at(-1)?.value).toBeCloseTo(1, 6);
  });

  it('maps speech through the clip’s trim and speed', () => {
    // Trimmed by 1 s and at double speed, source 1–2 s plays at film 0–0.5 s and its ramp is over
    // at 1 s, which is where the bed starts (offset 2 source seconds at 2×): flat. Ignoring the
    // trim (0.5–1 s) or the speed (0–1 s) would put speech under the bed's first instant.
    const out = withVolumes(
      ducked({ anchor: { clipId: 'clip-1', offset: 2 } }, { sourceStart: 1, speed: 2 }),
      json(layer('bed', { sourceStart: 2, sourceDuration: 3 })),
    );
    expect(volumeFrames(out)).toEqual([]);
  });

  it('does not duck under a muted clip, under itself, or under another ducking sound', () => {
    const compiled = json(layer('bed', { sourceStart: 2, sourceDuration: 4 }));
    expect(volumeFrames(withVolumes(ducked({}, { muted: true }), compiled))).toEqual([]);
    // The bed's own source has speech at 2–4 s, and a second sound plays that source from the
    // film's start: the bed ducks under that sound, never under itself — and not at all once that
    // sound ducks too.
    const other = sound({ id: 'other', anchor: { clipId: 'clip-1', offset: 0 }, sourceStart: 0 });
    const withOther = (over: Partial<AudioItem>): VideoProject => {
      const base = ducked({}, { muted: true });
      return {
        ...base,
        sources: base.sources.map((source) =>
          source.id === 'src-m' ? { ...source, speech: [[2, 4]] } : source,
        ),
        audio: [...(base.audio ?? []), { id: 'lane-2', items: [{ ...other, ...over }] }],
      };
    };
    expect(
      volumeFrames(withVolumes(withOther({ ducking: { amountDb: -6, ramp: 1 } }), compiled)),
    ).toEqual([]);
    expect(volumeFrames(withVolumes(withOther({}), compiled))).not.toEqual([]);
  });

  it('keeps a sound flat when nothing it ducks under has speech', () => {
    const quiet = project([sound({ ducking: { amountDb: -12, ramp: 0.5 } })]);
    const heard: VideoProject = {
      ...quiet,
      sources: quiet.sources.map((source) =>
        source.id === 'src-a' ? { ...source, speech: [] } : source,
      ),
    };
    expect(volumeFrames(withVolumes(heard, json(layer('bed', { sourceStart: 2 }))))).toEqual([]);
  });

  it('names the sources a ducking sound has not heard yet, once each', () => {
    const base = project([
      sound({ ducking: { amountDb: -12, ramp: 0.5 } }),
      sound({ id: 'voice', anchor: { clipId: 'clip-1', offset: 3 }, sourceStart: 0, duration: 1 }),
    ]);
    expect(unheardSources(base, 'bed').map((source) => source.id)).toEqual(['src-a', 'src-m']);
    expect(unheardSources(ducked(), 'bed').map((source) => source.id)).toEqual([]);
    const muted = project([sound()], { muted: true });
    expect(unheardSources(muted, 'bed')).toEqual([]);
  });
});
