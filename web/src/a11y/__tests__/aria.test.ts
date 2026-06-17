import { describe, expect, it } from 'vitest';
import { ariaInvalid } from '../aria';

describe('ARIA token helpers', () => {
  it('maps invalid state to explicit aria-invalid tokens', () => {
    expect(ariaInvalid(true)).toBe('true');
    expect(ariaInvalid(false)).toBeUndefined();
  });
});
