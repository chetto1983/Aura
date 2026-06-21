import { describe, expect, it } from 'vitest';
import { cn } from '../utils';

describe('cn', () => {
  it('joins truthy class fragments', () => {
    expect(cn('a', 'b')).toBe('a b');
  });

  it('drops falsy values and respects conditional objects', () => {
    expect(cn('a', false, null, undefined, { b: true, c: false })).toBe('a b');
  });

  it('merges conflicting tailwind utilities (last wins)', () => {
    expect(cn('px-2', 'px-4')).toBe('px-4');
  });
});
