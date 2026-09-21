import { describe, expect, it } from 'vitest';
import { editableKind, editableKindForFileName, editedBase, imageExtension } from '../editRules';

describe('media edit rules', () => {
  it('offers only formats the editors preserve', () => {
    expect(editableKind('image/png')).toBe('image');
    expect(editableKind('video/mp4; codecs=avc1')).toBe('video');
    expect(editableKind('image/gif')).toBeUndefined();
    expect(editableKindForFileName('clip.WEBM')).toBe('video');
    expect(editableKindForFileName('clip.mov')).toBeUndefined();
  });

  it('names image output without duplicating the old extension', () => {
    expect(imageExtension('image/jpeg')).toBe('jpeg');
    expect(imageExtension('image/webp')).toBe('webp');
    expect(editedBase('photo.png', 'edited')).toBe('photo-edited');
  });
});
