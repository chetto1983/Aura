import { describe, expect, it } from 'vitest';
import { shouldSpeak } from './shouldSpeak';

describe('shouldSpeak (Telegram ShouldSpeak parity)', () => {
  it.each([
    [false, false, false],
    [true, false, true],
    [false, true, true],
    [true, true, true],
  ])('shouldSpeak(%s, %s) === %s', (voiceMode, turnWasDictated, expected) => {
    expect(shouldSpeak(voiceMode, turnWasDictated)).toBe(expected);
  });

  it('is a pure OR — a dictated turn auto-speaks even with voice mode off', () => {
    expect(shouldSpeak(false, true)).toBe(true);
    expect(shouldSpeak(true, false)).toBe(true);
  });
});
