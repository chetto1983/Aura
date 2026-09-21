import type { DiscardedTrack, InputTrack } from 'mediabunny';
import { describe, expect, it } from 'vitest';
import {
  blockingDiscards,
  conversionOptions,
  editableKind,
  editableKindForFileName,
  editedBase,
  editedName,
  imageExtension,
  trimLength,
  typedRange,
  videoContainer,
} from '../editRules';

function discarded(
  type: string,
  codec: string | null,
  reason: DiscardedTrack['reason'],
): DiscardedTrack {
  return {
    track: { type, codec } as unknown as InputTrack,
    reason,
    trackOptions: {},
  };
}

describe('editableKind', () => {
  it.each([
    ['image/png', 'image'],
    ['image/jpeg', 'image'],
    ['IMAGE/WEBP', 'image'],
    ['video/mp4', 'video'],
    ['video/webm; codecs="vp9,opus"', 'video'],
  ])('%s is %s', (mime, kind) => {
    expect(editableKind(mime)).toBe(kind);
  });

  it.each(['image/svg+xml', 'image/gif', 'video/quicktime', 'application/octet-stream', ''])(
    '%j is not editable',
    (mime) => {
      expect(editableKind(mime)).toBeUndefined();
    },
  );

  it.each([
    ['photo.PNG', 'image'],
    ['holiday.jpeg', 'image'],
    ['clip.mp4', 'video'],
    ['archive.tar', undefined],
  ])('recognises %s from its Garage name', (name, kind) => {
    expect(editableKindForFileName(name)).toBe(kind);
  });
});

describe('names and containers', () => {
  it('keeps the base name and swaps the extension', () => {
    expect(editedName('beach.mp4', 'edited', 'mp4')).toBe('beach-edited.mp4');
    expect(editedName('my.holiday.webm', 'modificato', 'webm')).toBe('my.holiday-modificato.webm');
    expect(editedName('.hidden', 'edited', 'png')).toBe('.hidden-edited.png');
    expect(editedBase('photo.jpeg', 'modificata')).toBe('photo-modificata');
  });

  it('follows the source type', () => {
    expect(imageExtension('image/jpeg')).toBe('jpeg');
    expect(imageExtension('image/webp')).toBe('webp');
    expect(imageExtension('image/png')).toBe('png');
    expect(videoContainer('video/webm')).toBe('webm');
    expect(videoContainer('video/mp4')).toBe('mp4');
  });
});

describe('conversionOptions', () => {
  it('trims on the copy path that keeps the start frame exact', () => {
    expect(conversionOptions({ start: 2.5, end: 6, rotation: 0, mute: false })).toEqual({
      trim: { start: 2.5, end: 6 },
      copy: { boundaryPolicy: 'expand' },
      video: { rotate: 0 },
    });
  });

  it('adds crop, rotation and a muted audio track only when asked', () => {
    const crop = { left: 280, top: 0, width: 720, height: 720 };
    expect(conversionOptions({ start: 0, end: 4, rotation: 90, crop, mute: true })).toEqual({
      trim: { start: 0, end: 4 },
      copy: { boundaryPolicy: 'expand' },
      video: { rotate: 90, crop },
      audio: { discard: true },
    });
  });
});

describe('blockingDiscards', () => {
  it('lets through what the operator discarded', () => {
    expect(blockingDiscards([discarded('audio', 'aac', 'discarded_by_user')])).toEqual([]);
  });

  it('blocks a track this browser cannot process', () => {
    expect(
      blockingDiscards([
        discarded('video', 'hevc', 'undecodable_source_codec'),
        discarded('audio', null, 'no_encodable_target_codec'),
        discarded('subtitle', 'webvtt', 'max_track_count_of_type_reached'),
      ]),
    ).toEqual([
      { type: 'video', codec: 'hevc', reason: 'undecodable_source_codec' },
      { type: 'audio', codec: null, reason: 'no_encodable_target_codec' },
    ]);
  });
});

describe('typedRange', () => {
  it('keeps a typed start inside the clip and a tenth before the end', () => {
    expect(typedRange({ start: 2, end: 6 }, 'start', -3, 10)).toEqual({ start: 0, end: 6 });
    expect(typedRange({ start: 2, end: 6 }, 'start', 9, 10)).toEqual({ start: 5.9, end: 6 });
    expect(typedRange({ start: 2, end: 6 }, 'start', 3.5, 10)).toEqual({ start: 3.5, end: 6 });
  });

  it('keeps a typed end inside the clip and a tenth after the start', () => {
    expect(typedRange({ start: 2, end: 6 }, 'end', 40, 10)).toEqual({ start: 2, end: 10 });
    expect(typedRange({ start: 2, end: 6 }, 'end', 1, 10)).toEqual({ start: 2, end: 2.1 });
    expect(typedRange({ start: 2, end: 6 }, 'end', 7.25, 10)).toEqual({ start: 2, end: 7.25 });
  });

  it('keeps both bounds inside a clip shorter than a tenth', () => {
    expect(typedRange({ start: 0, end: 0.05 }, 'start', 1, 0.05)).toEqual({ start: 0, end: 0.05 });
    expect(typedRange({ start: 0, end: 0.05 }, 'end', 5, 0.05)).toEqual({ start: 0, end: 0.05 });
    expect(typedRange({ start: 0, end: 0.05 }, 'end', 0, 0.05)).toEqual({ start: 0, end: 0.05 });
  });

  it('treats a duration that is not a positive number as nothing to trim', () => {
    expect(trimLength(Number.NaN)).toBe(0);
    expect(trimLength(-1)).toBe(0);
    expect(trimLength(Infinity)).toBe(0);
    expect(typedRange({ start: 0, end: 0 }, 'end', 3, Number.NaN)).toEqual({ start: 0, end: 0 });
  });
});
