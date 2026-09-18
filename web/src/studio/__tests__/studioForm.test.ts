import { describe, expect, it } from 'vitest';
import type { StudioImageRef, StudioModel, StudioRecord } from '../studioApi';
import type { StudioDraft } from '../studioForm';
import {
  cheapestOptions,
  draftFromRecord,
  estimateCost,
  isActive,
  maxImages,
  reconcileDraft,
  requestBody,
} from '../studioForm';

// veoLite declares its durations and resolutions OUT of order on purpose: a form that takes
// the first entry rather than the cheapest one would pick 8 s at 1080p — eight times the bill
// of the real default. The price matrix is the one studioVideoPrices builds: every declared
// resolution against every audio choice the model allows.
const veoLite: StudioModel = {
  id: 'google/veo-3.1-lite',
  name: 'Google: Veo 3.1 Lite',
  description: 'a video model',
  durations: [8, 4, 6],
  resolutions: ['1080p', '720p'],
  aspect_ratios: ['16:9', '9:16'],
  frame_images: ['first_frame', 'last_frame'],
  audio: true,
  seed: true,
  prices: [
    { resolution: '1080p', audio: true, usd_per_second: 0.08 },
    { resolution: '1080p', audio: false, usd_per_second: 0.05 },
    { resolution: '720p', audio: true, usd_per_second: 0.05 },
    { resolution: '720p', audio: false, usd_per_second: 0.03 },
  ],
};

const seedream: StudioModel = {
  id: 'bytedance/seedream-4',
  name: 'Seedream 4',
  aspect_ratios: ['1:1', '3:2'],
  audio: false,
  seed: false,
  reference_max: 4,
  image_min_usd: 0.05,
  image_max_usd: 0.05,
};

// seedream without the `reference_max` key at all — the shape the server sends for a model
// whose catalog row declares no input_references ceiling. It cannot be spelled as a spread
// with `reference_max: undefined`, which is a different thing under exactOptionalPropertyTypes.
const seedreamNoReferences: StudioModel = {
  id: 'bytedance/seedream-4',
  aspect_ratios: ['1:1', '3:2'],
  audio: false,
  seed: false,
  image_min_usd: 0.05,
};

const cat: StudioImageRef = { id: 'asset-cat', file_name: 'cat.png', mime_type: 'image/png' };
const dog: StudioImageRef = { id: 'asset-dog', file_name: 'dog.png', mime_type: 'image/png' };
const hat: StudioImageRef = { id: 'asset-hat', file_name: 'hat.png', mime_type: 'image/png' };
const bat: StudioImageRef = { id: 'asset-bat', file_name: 'bat.png', mime_type: 'image/png' };
const rat: StudioImageRef = { id: 'asset-rat', file_name: 'rat.png', mime_type: 'image/png' };
const library = [cat, dog, hat, bat, rat];

function videoDraft(over: Partial<StudioDraft> = {}): StudioDraft {
  return {
    kind: 'video',
    model: veoLite.id,
    prompt: 'a cat in a hat',
    options: cheapestOptions(veoLite),
    images: [],
    endFrame: undefined,
    ...over,
  };
}

describe('cheapestOptions', () => {
  it('takes the shortest duration and the lowest measured resolution, not the first declared', () => {
    const options = cheapestOptions(veoLite);

    expect(options.resolution).toBe('720p');
    expect(options.duration).toBe(4);
    expect(options.aspectRatio).toBe('16:9');
    expect(options.audio).toBe(false);
    expect(options.seed).toBeUndefined();
  });

  it('measures the 1K/2K/4K labels by height, the way the server clamp does', () => {
    const labelled: StudioModel = { ...veoLite, resolutions: ['4K', '1K', '2K'] };

    expect(cheapestOptions(labelled).resolution).toBe('1K');
  });

  it('ignores a resolution it cannot measure rather than preferring it', () => {
    // An unmeasurable label sorts nowhere; picking it because it came first would send the
    // provider a resolution nobody costed.
    const odd: StudioModel = { ...veoLite, resolutions: ['cinema', '1080p'] };

    expect(cheapestOptions(odd).resolution).toBe('1080p');
  });

  it('leaves an image model without a resolution, a duration, audio or a seed', () => {
    const options = cheapestOptions(seedream);

    expect(options.aspectRatio).toBe('1:1');
    expect(options.resolution).toBe('');
    expect(options.duration).toBeUndefined();
    expect(options.audio).toBe(false);
    expect(options.seed).toBeUndefined();
  });

  it('prefers 16:9 wherever the model declares it, not wherever it lists first', () => {
    const portraitFirst: StudioModel = { ...veoLite, aspect_ratios: ['9:16', '16:9'] };

    expect(cheapestOptions(portraitFirst).aspectRatio).toBe('16:9');
  });

  it('falls back to the first declared ratio when 16:9 is not offered', () => {
    const portrait: StudioModel = { ...veoLite, aspect_ratios: ['9:16', '1:1'] };

    expect(cheapestOptions(portrait).aspectRatio).toBe('9:16');
  });
});

describe('estimateCost', () => {
  it('prices the exact cell of the matrix the operator is about to pay for', () => {
    const cheap = cheapestOptions(veoLite);

    expect(estimateCost(veoLite, cheap)).toBeCloseTo(0.12, 10);
    expect(
      estimateCost(veoLite, { ...cheap, resolution: '1080p', duration: 8, audio: true }),
    ).toBeCloseTo(0.64, 10);
  });

  it('reads an image model as one image, not as a per-second rate', () => {
    expect(estimateCost(seedream, cheapestOptions(seedream))).toBeCloseTo(0.05, 10);
  });

  it('says unknown rather than free when the price or the duration is missing', () => {
    const cheap = cheapestOptions(veoLite);
    const unpriced: StudioModel = { ...veoLite, prices: [] };

    // An unknown price is not a free one: 0 on a Generate button is a lie about the bill.
    expect(estimateCost(unpriced, cheap)).toBeUndefined();
    expect(estimateCost(veoLite, { ...cheap, duration: undefined })).toBeUndefined();
    expect(estimateCost(veoLite, { ...cheap, resolution: '4K' })).toBeUndefined();
    // The audio cell is a different price, so an audio choice the matrix never costed is
    // unknown too.
    expect(estimateCost({ ...veoLite, prices: [] }, { ...cheap, audio: true })).toBeUndefined();
  });
});

describe('reconcileDraft', () => {
  it('keeps every value the new model still declares', () => {
    const chosen = videoDraft({
      options: { resolution: '1080p', duration: 6, aspectRatio: '9:16', audio: true, seed: 7 },
    });

    expect(reconcileDraft(chosen, veoLite).options).toEqual({
      resolution: '1080p',
      duration: 6,
      aspectRatio: '9:16',
      audio: true,
      seed: 7,
    });
  });

  it('falls back to the cheapest for a value the new model does not declare', () => {
    const chosen = videoDraft({
      options: { resolution: '4K', duration: 12, aspectRatio: '21:9', audio: true, seed: 7 },
    });

    const narrow: StudioModel = { ...veoLite, resolutions: ['720p'], durations: [4] };
    expect(reconcileDraft(chosen, narrow).options).toEqual({
      resolution: '720p',
      duration: 4,
      aspectRatio: '16:9',
      audio: true,
      seed: 7,
    });
  });

  it('drops audio and the seed when the new model declares neither', () => {
    const chosen = videoDraft({
      options: { resolution: '720p', duration: 4, aspectRatio: '16:9', audio: true, seed: 7 },
    });

    const silent: StudioModel = { ...veoLite, audio: false, seed: false };
    const options = reconcileDraft(chosen, silent).options;

    expect(options.audio).toBe(false);
    expect(options.seed).toBeUndefined();
  });

  it('drops the end frame when the model takes no last frame', () => {
    const chosen = videoDraft({ images: [cat], endFrame: dog });

    const startOnly: StudioModel = { ...veoLite, frame_images: ['first_frame'] };
    expect(reconcileDraft(chosen, startOnly).endFrame).toBeUndefined();
    // …and keeps it when the model does take one.
    expect(reconcileDraft(chosen, veoLite).endFrame).toBe(dog);
  });

  it('drops the end frame when there is no start frame to end from', () => {
    const chosen = videoDraft({ images: [], endFrame: dog });

    expect(reconcileDraft(chosen, veoLite).endFrame).toBeUndefined();
  });

  it('truncates the images to what the model accepts', () => {
    const chosen: StudioDraft = {
      kind: 'image',
      model: seedream.id,
      prompt: 'a hat on a cat',
      options: cheapestOptions(seedream),
      images: library,
      endFrame: undefined,
    };

    expect(reconcileDraft(chosen, seedream).images).toEqual([cat, dog, hat, bat]);
    expect(reconcileDraft(chosen, { ...seedream, reference_max: 1 }).images).toEqual([cat]);
    // No declared maximum means the model takes none, not unlimited.
    expect(reconcileDraft(chosen, seedreamNoReferences).images).toEqual([]);
  });

  it('keeps a video draft to the one start frame the model accepts', () => {
    const chosen = videoDraft({ images: [cat, dog] });

    expect(reconcileDraft(chosen, veoLite).images).toEqual([cat]);
    expect(reconcileDraft(chosen, { ...veoLite, frame_images: [] }).images).toEqual([]);
  });

  it('retunes the draft to the model it was reconciled against', () => {
    const chosen = videoDraft();

    expect(reconcileDraft(chosen, { ...veoLite, id: 'google/veo-3.1' }).model).toBe(
      'google/veo-3.1',
    );
  });
});

describe('maxImages', () => {
  it('is the reference ceiling for an image model and one start frame for a video', () => {
    expect(maxImages(seedream, 'image')).toBe(4);
    expect(maxImages(veoLite, 'video')).toBe(1);
    expect(maxImages({ ...veoLite, frame_images: [] }, 'video')).toBe(0);
    expect(maxImages(seedreamNoReferences, 'image')).toBe(0);
  });
});

describe('requestBody', () => {
  it('sends a video body naming only what the model declares', () => {
    const body = requestBody(
      videoDraft({
        options: { resolution: '1080p', duration: 8, aspectRatio: '9:16', audio: true, seed: 7 },
        images: [cat],
        endFrame: dog,
      }),
      veoLite,
    );

    expect(body.kind).toBe('video');
    expect(body.body).toStrictEqual({
      model: 'google/veo-3.1-lite',
      prompt: 'a cat in a hat',
      duration: 8,
      resolution: '1080p',
      aspect_ratio: '9:16',
      audio: true,
      seed: 7,
      first_frame_asset_id: 'asset-cat',
      last_frame_asset_id: 'asset-dog',
    });
  });

  it('omits audio and the seed a model does not declare', () => {
    const silent: StudioModel = { ...veoLite, audio: false, seed: false };

    const body = requestBody(
      videoDraft({
        options: { resolution: '720p', duration: 4, aspectRatio: '16:9', audio: true, seed: 7 },
      }),
      silent,
    );

    // Asking a silent model for silence earns a "not supported" note about a choice the
    // operator never made — so the field is absent, not false.
    expect(body.kind).toBe('video');
    expect(body.body).toStrictEqual({
      model: 'google/veo-3.1-lite',
      prompt: 'a cat in a hat',
      duration: 4,
      resolution: '720p',
      aspect_ratio: '16:9',
    });
  });

  it('sends audio off explicitly when the model can make sound', () => {
    // mediagen.CheapestVideoInput fills an omitted audio with false only when the model can
    // generate audio; the form says the same thing rather than leaving the provider its own
    // preference. toStrictEqual, so a `seed: undefined` key would fail rather than pass.
    expect(requestBody(videoDraft(), veoLite)).toStrictEqual({
      kind: 'video',
      body: {
        model: 'google/veo-3.1-lite',
        prompt: 'a cat in a hat',
        duration: 4,
        resolution: '720p',
        aspect_ratio: '16:9',
        audio: false,
      },
    });
  });

  it('sends an image body carrying the reference ids', () => {
    const body = requestBody(
      {
        kind: 'image',
        model: seedream.id,
        prompt: 'a hat on a cat',
        options: cheapestOptions(seedream),
        images: [cat, dog],
        endFrame: undefined,
      },
      seedream,
    );

    // The tag is what stops this body reaching createStudioVideo: the two shapes overlap
    // structurally, so without it the wrong pairing typechecks.
    expect(body.kind).toBe('image');
    expect(body.body).toStrictEqual({
      model: 'bytedance/seedream-4',
      prompt: 'a hat on a cat',
      aspect_ratio: '1:1',
      reference_asset_ids: ['asset-cat', 'asset-dog'],
    });
  });
});

describe('draftFromRecord', () => {
  const videoRecord: StudioRecord = {
    id: 'job-1',
    kind: 'video',
    status: 'completed',
    model: 'google/veo-3.1-lite',
    prompt: 'a cat in a hat',
    used: {
      duration: 8,
      resolution: '720p',
      aspect_ratio: '16:9',
      audio: true,
      seed: 7,
      first_frame_asset_id: 'asset-cat',
      last_frame_asset_id: 'asset-dog',
    },
    created_at: '2026-09-17T10:00:00Z',
  };

  it('rebuilds what was actually asked for, resolving the frames against the library', () => {
    expect(draftFromRecord(videoRecord, library)).toEqual({
      kind: 'video',
      model: 'google/veo-3.1-lite',
      prompt: 'a cat in a hat',
      options: {
        resolution: '720p',
        duration: 8,
        aspectRatio: '16:9',
        audio: true,
        seed: 7,
      },
      images: [cat],
      endFrame: dog,
    });
  });

  it('drops an image the library no longer carries rather than inventing a reference', () => {
    const draft = draftFromRecord(videoRecord, [dog]);

    expect(draft.images).toEqual([]);
    expect(draft.endFrame).toBe(dog);
  });

  it('rebuilds an image record from its reference ids', () => {
    const draft = draftFromRecord(
      {
        id: 'job-2',
        kind: 'image',
        status: 'completed',
        model: 'bytedance/seedream-4',
        prompt: 'a hat on a cat',
        used: { aspect_ratio: '3:2', reference_asset_ids: ['asset-dog', 'asset-cat'] },
        created_at: '2026-09-17T11:00:00Z',
      },
      library,
    );

    expect(draft.images).toEqual([dog, cat]);
    expect(draft.options.aspectRatio).toBe('3:2');
    expect(draft.options.resolution).toBe('');
    expect(draft.options.duration).toBeUndefined();
    expect(draft.options.audio).toBe(false);
    expect(draft.options.seed).toBeUndefined();
    expect(draft.endFrame).toBeUndefined();
  });
});

describe('isActive', () => {
  it('is true only while the generation has not finished', () => {
    expect(isActive({ status: 'pending' })).toBe(true);
    expect(isActive({ status: 'in_progress' })).toBe(true);
    expect(isActive({ status: 'completed' })).toBe(false);
    expect(isActive({ status: 'failed' })).toBe(false);
    expect(isActive({ status: '' })).toBe(false);
  });
});
