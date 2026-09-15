import { describe, expect, it } from 'vitest';
import {
  imageModelMeta,
  modelRowMeta,
  videoModelMeta,
  type MediaLabels,
} from '../mediaModelCatalogFormat';

const labels: MediaLabels = {
  references: (max: number) => String(max) + ' reference images',
  duration: (min: number, max: number) => String(min) + '–' + String(max) + ' s',
  imageToVideo: 'Image-to-video',
};

describe('media model row labels', () => {
  it('shows per-second prices and never token prices as per-image prices', () => {
    expect(
      videoModelMeta(
        {
          kind: 'video',
          id: 'minimax/hailuo-3-max',
          has_price: true,
          second_min_usd: 0.05,
          second_max_usd: 0.08,
          duration_min: 5,
          duration_max: 15,
          resolutions: ['480p', '768p'],
          image_to_video: true,
        },
        labels,
      ),
    ).toContain('$0.05–$0.08/s');
    const label = imageModelMeta(
      { kind: 'image', id: 'microsoft/mai-image-2.6', has_price: false, reference_max: 5 },
      labels,
    );
    expect(label).toContain('5');
    expect(label).not.toContain('/image');
  });

  it('writes every declared video capability in one row, in a fixed order', () => {
    expect(
      videoModelMeta(
        {
          kind: 'video',
          id: 'minimax/hailuo-3-max',
          has_price: true,
          second_min_usd: 0.05,
          second_max_usd: 0.08,
          duration_min: 5,
          duration_max: 15,
          resolutions: ['768p', '480p'],
          image_to_video: true,
        },
        labels,
      ),
    ).toBe('$0.05–$0.08/s · 5–15 s · 768p, 480p · Image-to-video');
  });

  it('prices an image per image, one price when the range is a single point', () => {
    expect(
      imageModelMeta(
        {
          kind: 'image',
          id: 'vendor/range',
          has_price: true,
          image_min_usd: 0.02,
          image_max_usd: 0.04,
          reference_max: 3,
        },
        labels,
      ),
    ).toBe('$0.02–$0.04/image · 3 reference images');
    expect(
      imageModelMeta(
        {
          kind: 'image',
          id: 'vendor/flat',
          has_price: true,
          image_min_usd: 0.04,
          image_max_usd: 0.04,
        },
        labels,
      ),
    ).toBe('$0.04/image');
  });

  it('keeps the sub-cent digits of a media price instead of rounding to cents', () => {
    expect(
      imageModelMeta(
        {
          kind: 'image',
          id: 'vendor/a',
          has_price: true,
          image_min_usd: 0.039,
          image_max_usd: 0.039,
        },
        labels,
      ),
    ).toBe('$0.039/image');
    expect(
      imageModelMeta(
        {
          kind: 'image',
          id: 'vendor/b',
          has_price: true,
          image_min_usd: 0.035,
          image_max_usd: 0.039,
        },
        labels,
      ),
    ).toBe('$0.035–$0.039/image');
    expect(
      videoModelMeta(
        {
          kind: 'video',
          id: 'vendor/c',
          has_price: true,
          second_min_usd: 0.0125,
          second_max_usd: 0.125,
        },
        labels,
      ),
    ).toBe('$0.0125–$0.125/s');
    expect(
      videoModelMeta(
        {
          kind: 'video',
          id: 'vendor/d',
          has_price: true,
          second_min_usd: 0.1234567,
          second_max_usd: 1.5,
        },
        labels,
      ),
    ).toBe('$0.1235–$1.5/s');
    expect(
      imageModelMeta(
        {
          kind: 'image',
          id: 'vendor/e',
          has_price: true,
          image_min_usd: 0.000038,
          image_max_usd: 0.000038,
        },
        labels,
      ),
    ).toBe('$0.000038/image');
  });

  it('keeps a zero price, because a free model is a real price and an unknown one is not', () => {
    expect(
      imageModelMeta(
        { kind: 'image', id: 'vendor/free', has_price: true, image_min_usd: 0, image_max_usd: 0 },
        labels,
      ),
    ).toBe('$0/image');
    expect(
      videoModelMeta(
        { kind: 'video', id: 'vendor/free', has_price: true, second_min_usd: 0, second_max_usd: 0 },
        labels,
      ),
    ).toBe('$0/s');
  });

  it('says nothing about a capability the catalogue did not declare', () => {
    expect(imageModelMeta({ kind: 'image', id: 'vendor/bare', has_price: false }, labels)).toBe('');
    expect(
      imageModelMeta(
        { kind: 'image', id: 'vendor/none', has_price: false, reference_max: 0 },
        labels,
      ),
    ).toBe('');
    expect(videoModelMeta({ kind: 'video', id: 'vendor/bare', has_price: false }, labels)).toBe('');
    expect(
      videoModelMeta(
        {
          kind: 'video',
          id: 'vendor/partial',
          has_price: false,
          second_min_usd: 0.1,
          second_max_usd: 0.2,
          duration_min: 4,
          resolutions: [],
          image_to_video: false,
        },
        labels,
      ),
    ).toBe('');
  });

  it('drops a price whose range is incomplete rather than inventing its missing end', () => {
    expect(
      imageModelMeta(
        { kind: 'image', id: 'vendor/half', has_price: true, image_min_usd: 0.02 },
        labels,
      ),
    ).toBe('');
    expect(
      videoModelMeta(
        { kind: 'video', id: 'vendor/half', has_price: true, second_max_usd: 0.2 },
        labels,
      ),
    ).toBe('');
  });

  it('routes each catalogue row to its own formatter', () => {
    expect(
      modelRowMeta(
        {
          id: 'z-ai/glm-5.3',
          context_window: 204_800,
          input_per_1m: 0.14,
          output_per_1m: 0.28,
          has_price: true,
        },
        'no token charge',
        labels,
      ),
    ).toBe('205K ctx · $0.14 / $0.28');
    expect(modelRowMeta({ id: 'gemma-4-12b', has_price: false }, 'no token charge', labels)).toBe(
      'no token charge',
    );
    expect(
      modelRowMeta(
        { kind: 'image', id: 'microsoft/mai-image-2.6', has_price: false, reference_max: 5 },
        'no token charge',
        labels,
      ),
    ).toBe('5 reference images');
    expect(
      modelRowMeta(
        { kind: 'video', id: 'vendor/i2v', has_price: false, image_to_video: true },
        'no token charge',
        labels,
      ),
    ).toBe('Image-to-video');
  });
});
