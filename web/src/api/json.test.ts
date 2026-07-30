import { describe, expect, it } from 'vitest';
import { isTrue } from './json';

describe('isTrue', () => {
  it('accepts only the JSON boolean true', () => {
    expect(isTrue(true)).toBe(true);
    expect(isTrue(false)).toBe(false);
    expect(isTrue('true')).toBe(false);
    expect(isTrue(1)).toBe(false);
    expect(isTrue(null)).toBe(false);
  });
});
