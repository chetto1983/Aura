import type { DiscardedTrack, InputTrack } from 'mediabunny';
import { describe, expect, it } from 'vitest';
import {
  blockingDiscards,
  conversionOptions,
  editableKind,
  editedBase,
  editedName,
  imageExtension,
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
      { type: 'audio', codec: 'unknown', reason: 'no_encodable_target_codec' },
    ]);
  });
});
