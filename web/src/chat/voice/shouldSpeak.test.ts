import { describe, expect, it } from 'vitest';
import { shouldSpeak } from './shouldSpeak';

describe('shouldSpeak (composer auto-speak gate)', () => {
  it.each([
    [false, false, false],
    [true, false, false],
    [false, true, true],
    [true, true, false],
  ])('shouldSpeak(voiceMode=%s, dictated=%s) === %s', (voiceMode, turnWasDictated, expected) => {
    expect(shouldSpeak(voiceMode, turnWasDictated)).toBe(expected);
  });

  it('a dictated composer turn reads its reply aloud', () => {
    expect(shouldSpeak(false, true)).toBe(true);
  });

  it('voice mode SUPPRESSES it — the hands-free overlay speaks the reply itself', () => {
    // A plain OR here played every answer twice, once per path.
    expect(shouldSpeak(true, true)).toBe(false);
    expect(shouldSpeak(true, false)).toBe(false);
  });
});
